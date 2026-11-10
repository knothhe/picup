#!/usr/bin/env python3
"""Build and validate the current macOS installer, then print its paths as JSON.

The source tree is the current working directory. This lets release.py invoke
this helper against an existing release tag without changing that tag's source.
Python 3.9+, Make, and the project's Go toolchain are required.
"""
import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import tarfile
import time
import zipfile
from urllib.parse import unquote, urlsplit

from build_flags import build_environment
from build_manifest import packaging_configuration
from dev_signing import SIGNING_VARIABLES
from macos_signing import configuration as signing_configuration, verify_application, SigningError


class PackageError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise PackageError(message)


def run(*args, cwd, capture=False, env=None):
    if env is None:
        env = dict(os.environ)
        env.pop('MYGO_UPDATER_PRIVATE_KEY', None)
    result = subprocess.run(
        args, cwd=cwd, text=True,
        stdout=subprocess.PIPE if capture else sys.stderr,
        stderr=subprocess.PIPE if capture else sys.stderr, env=env,
    )
    if result.returncode:
        detail = f'\n{result.stderr.strip()}' if capture and result.stderr else ''
        raise PackageError(f"Command failed ({result.returncode}): {' '.join(map(str, args))}{detail}")
    return result.stdout.strip() if capture else None


def manifest(root, version):
    require(re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?', version),
            f'Invalid release version: {version}')
    path = root / 'mygo.json'
    require(path.is_file() and not path.is_symlink(), 'Missing or symlinked mygo.json.')
    data = json.loads(path.read_text())
    require(data.get('version') == version,
            f"mygo.json version {data.get('version')!r} does not match requested version {version!r}.")
    require(data.get('name') == 'Picup', 'The release package must be named Picup in mygo.json.')
    return data


def host_platform(go, root):
    environment = json.loads(run(go, 'env', '-json', 'GOHOSTOS', 'GOHOSTARCH', 'GOOS', 'GOARCH',
                                 cwd=root, capture=True))
    goos, goarch = environment.get('GOHOSTOS'), environment.get('GOHOSTARCH')
    require(goos == 'darwin', 'Release installer packaging currently supports macOS hosts only.')
    require(goarch in ('amd64', 'arm64'), f'Unsupported macOS host architecture: {goarch!r}.')
    require((environment.get('GOOS'), environment.get('GOARCH')) == (goos, goarch),
            'Cross compilation is not supported for release installers; unset GOOS and GOARCH and build on the target Mac.')
    return goos, goarch


def validate_app(app, version, root, arch=None, formal=True):
    require(app.is_dir() and not app.is_symlink(), f'Missing application bundle: {app}')
    info_path = app / 'Contents' / 'Info.plist'
    require(info_path.is_file(), f'Missing application metadata: {info_path}')
    with info_path.open('rb') as stream:
        info = plistlib.load(stream)
    for key in ('CFBundleVersion', 'CFBundleShortVersionString'):
        require(info.get(key) == version,
                f'Application {key} {info.get(key)!r} does not match requested version {version!r}.')
    name, identifier, cli_name = ('Picup', 'com.knothhe.picup', 'picup') if formal else (
        'Picup Dev', 'com.knothhe.picup.dev', 'picup-dev')
    require(info.get('CFBundleExecutable') == name, 'Unexpected application executable in Info.plist.')
    require(info.get('CFBundleIdentifier') == identifier, 'Unexpected application identity in Info.plist.')
    gui = app / 'Contents' / 'MacOS' / name
    cli = app / 'Contents' / 'Resources' / cli_name
    for executable in (gui, cli):
        require(executable.is_file() and os.access(executable, os.X_OK),
                f'Missing or non-executable application program: {executable}')
        if arch:
            actual = run('/usr/bin/lipo', '-archs', str(executable), cwd=root, capture=True).split()
            require(actual == [{'arm64': 'arm64', 'amd64': 'x86_64'}[arch]],
                    f'Application architecture mismatch: {executable.name}')
    notices = app / 'Contents/Resources/THIRD_PARTY_NOTICES.txt'
    require(notices.is_file() and notices.stat().st_size > 0, 'Missing bundled third-party notices.')
    output = run(str(cli), '--version', cwd=root, capture=True)
    require(output == f'picup {version}',
            f'Bundled CLI version {output!r} does not match requested version {version!r}.')
    for executable in (gui, cli):
        metadata = run(os.environ.get('GO', 'go'), 'version', '-m', str(executable), cwd=root, capture=True)
        development = False
        native_keychain = False
        for line in metadata.splitlines():
            if line.strip() == 'build\tCGO_ENABLED=1':
                native_keychain = True
            if line.strip().startswith('build\t-tags='):
                tags = line.strip().split('=', 1)[1].replace('"', '').replace("'", '')
                development = 'picup_dev' in re.split(r'[,\s]+', tags)
        require(development != formal, 'Formal application contains a development-tagged binary.' if formal
                else 'Unsigned CI application is missing its development data namespace.')
        require(native_keychain,
                f'Application binary lacks CGO_ENABLED=1 and native R2 Keychain support: {executable.name}')


def validate_update_config(data):
    updates = data.get('updates', {})
    require(isinstance(updates, dict), 'Invalid updater configuration in mygo.json.')
    require(updates.get('github') == 'knothhe/picup' and updates.get('deltas') == 0
            and updates.get('tagPrefix', 'v') == 'v' and not updates.get('privateKey'),
            'Release updates must use knothhe/picup, v tags, full archives and no private key path.')
    try:
        key = base64.b64decode(updates.get('publicKey', ''), validate=True)
    except (ValueError, TypeError):
        key = b''
    require(len(key) == 32, 'Missing or invalid updater public key in mygo.json.')
    return updates


def signing_environment():
    """Load a caller-provided key or the private local macOS signer.

    Return a separate environment only for MyGo; never mutate this process's
    environment or pass signing material to the other build/validation tools.
    An explicitly empty variable is a failure, including a missing CI secret.
    """
    environment = dict(os.environ)
    name = 'MYGO_UPDATER_PRIVATE_KEY'
    if name in environment:
        require(bool(environment[name].strip()), 'Explicit MYGO_UPDATER_PRIVATE_KEY is empty; signing is unavailable.')
        return environment
    path = Path.home() / 'Library/Application Support/picup-release/mygo-update.key'
    require(hasattr(os, 'O_NOFOLLOW'), 'Secure local signing-key access is unavailable; provide MYGO_UPDATER_PRIVATE_KEY.')
    flags = os.O_RDONLY | os.O_NOFOLLOW | getattr(os, 'O_CLOEXEC', 0) | getattr(os, 'O_NONBLOCK', 0)
    try:
        descriptor = os.open(path, flags)
        with os.fdopen(descriptor, 'rb') as stream:
            metadata = os.fstat(stream.fileno())
            require(stat.S_ISREG(metadata.st_mode) and stat.S_IMODE(metadata.st_mode) == 0o600
                    and metadata.st_uid == os.getuid(),
                    'The local update signing key must be an owned regular file with mode 0600, without a symlink.')
            raw = stream.read(4097)
    except OSError:
        raise PackageError('No safe local update signing key is available; provide MYGO_UPDATER_PRIVATE_KEY or use --unsigned.')
    require(len(raw) <= 4096, 'The local update signing key is invalid.')
    try:
        key = raw.decode('utf-8').strip()
    except UnicodeDecodeError:
        raise PackageError('The local update signing key is invalid.')
    require(bool(key), 'The local update signing key is empty.')
    environment[name] = key
    return environment


def validate_update_files(root, data, version, platform, app, go):
    directory = app.parent
    archive = directory / f'picup-{version}-{platform}.tar.gz'
    feed = directory / f'update-{platform}.json'
    for path in (archive, feed):
        require(path.is_file() and not path.is_symlink() and path.stat().st_size > 0,
                f'Missing signed update asset: {path.name}')
    require(feed.stat().st_size <= 1024 * 1024, 'Update manifest is too large.')
    info = json.loads(feed.read_text())
    require(isinstance(info, dict), 'Update manifest must be a JSON object.')
    require(info.get('version') == version, 'Update manifest version does not match the application.')
    require(not info.get('deltas') and not info.get('previous'), 'Release policy requires full update archives only.')
    require(type(info.get('size')) is int and info['size'] == archive.stat().st_size,
            'Update manifest size does not match its archive.')
    require(isinstance(info.get('url'), str) and isinstance(info.get('signature'), str)
            and isinstance(info.get('date'), str), 'Update manifest fields are invalid.')
    address = urlsplit(info['url'])
    require(address.scheme == 'https' and address.netloc == 'github.com' and not address.query
            and not address.fragment and unquote(address.path) ==
            f'/knothhe/picup/releases/download/v{version}/{archive.name}',
            'Update manifest points outside this exact release archive.')
    try:
        date = datetime.datetime.fromisoformat(info.get('date', '').replace('Z', '+00:00'))
    except (ValueError, TypeError):
        raise PackageError('Update manifest date is invalid.')
    require(date.tzinfo is not None, 'Update manifest date needs a time zone.')
    run(go, 'run', './cmd/verify-update', str(archive), data['updates']['publicKey'],
        info.get('signature', ''), cwd=root)
    # Verify that MyGo archived the application just validated above, rather
    # than another architecture, stale bundle or a package of another identity.
    expected = {f'Picup.app/{path}': app / path for path in (
        'Contents/Info.plist', 'Contents/MacOS/Picup', 'Contents/Resources/picup',
        'Contents/Resources/THIRD_PARTY_NOTICES.txt')}
    seen = set()
    with tarfile.open(archive, 'r:gz') as content:
        for member in content:
            path = Path(member.name)
            require(not path.is_absolute() and '..' not in path.parts and path.parts
                    and path.parts[0] == 'Picup.app' and member.name not in seen,
                    'Invalid or duplicate path in update archive.')
            seen.add(member.name)
            if member.name in expected:
                require(member.isfile(), 'Update archive replaces an application file with a link.')
                with content.extractfile(member) as stream:
                    digest = hashlib.file_digest(stream, 'sha256').hexdigest() if hasattr(hashlib, 'file_digest') else hashlib.sha256(stream.read()).hexdigest()
                require(digest == hashlib.sha256(expected[member.name].read_bytes()).hexdigest(),
                        f'Update archive contains a different application file: {member.name}')
    require(set(expected) <= seen, 'Update archive is missing required application files.')
    return [archive, feed]


def copy_asset(source, destination):
    descriptor, temporary = tempfile.mkstemp(prefix='.' + destination.name + '-', dir=destination.parent)
    os.close(descriptor)
    try:
        shutil.copyfile(source, temporary)
        os.replace(temporary, destination)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def write_zip(app, destination):
    """Preserve Unix modes and store symlinks as links without following them."""
    with zipfile.ZipFile(destination, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        def add(path):
            metadata = path.lstat()
            name = path.relative_to(app.parent).as_posix()
            mode = metadata.st_mode
            if stat.S_ISDIR(mode):
                name += '/'
            timestamp = time.localtime(metadata.st_mtime)[:6]
            timestamp = max((1980, 1, 1, 0, 0, 0), min(timestamp, (2107, 12, 31, 23, 59, 58)))
            entry = zipfile.ZipInfo(name, timestamp)
            entry.create_system = 3
            entry.external_attr = (mode & 0xFFFF) << 16
            entry.compress_type = zipfile.ZIP_DEFLATED
            if stat.S_ISLNK(mode):
                archive.writestr(entry, os.fsencode(os.readlink(path)))
            elif stat.S_ISDIR(mode):
                entry.external_attr |= 0x10
                archive.writestr(entry, b'')
                for child in sorted(path.iterdir()):
                    add(child)
            elif stat.S_ISREG(mode):
                with path.open('rb') as source, archive.open(entry, 'w') as target:
                    while True:
                        chunk = source.read(1024 * 1024)
                        if not chunk:
                            break
                        target.write(chunk)
            else:
                raise PackageError(f'Unsupported file type in application bundle: {path}')
        add(app)


def package_release(root, version, output_dir, unsigned=False):
    root = Path(root).resolve()
    data = manifest(root, version)
    if not unsigned:
        validate_update_config(data)
    compile_environment = build_environment('picup_dev' if unsigned else '', formal=not unsigned)
    if unsigned:
        # This route explicitly produces an unsigned CI package, even when
        # invoked on a developer's Mac with a saved local certificate.
        compile_environment['CI'] = '1'
        for variable in SIGNING_VARIABLES:
            compile_environment.pop(variable, None)
    environment = dict(compile_environment, MYGO_UPDATER_PRIVATE_KEY='') if unsigned else signing_environment()
    environment['GOFLAGS'] = compile_environment.get('GOFLAGS', '')
    if 'CGO_ENABLED' in compile_environment:
        environment['CGO_ENABLED'] = compile_environment['CGO_ENABLED']
    go = os.environ.get('GO', 'go')
    goos, goarch = host_platform(go, root)
    signing = None if unsigned else signing_configuration()
    # Use the same build and MyGo packaging steps as make package, without a DMG.
    # MyGo atomically replaces only this platform's directory after a successful
    # build, so failed builds and old packages from other platforms cannot leak in.
    run('make', 'build', 'BUILD_TAGS=picup_dev' if unsigned else 'BUILD_TAGS=', cwd=root, env=compile_environment)
    arguments = [go, 'tool', 'mygo', 'build', '-skip-dmg']
    if unsigned:
        arguments.append('-debug')  # MyGo disables updater eligibility in this build.
    if signing and signing.profile:
        # MyGo notarizes its DMG and staples the app before creating update
        # archives. Only ZIP/update assets are collected for publication.
        arguments.remove('-skip-dmg')
    with packaging_configuration(root, unsigned, signing) as config_dir:
        if config_dir != root:
            arguments.append(str(config_dir))
        run(*arguments, cwd=root, env=environment)
    manifest(root, version)
    app = (root / 'build/dev/dist' / f'{goos}-{goarch}' / 'Picup Dev.app') if unsigned else (
        root / data.get('out', 'dist') / f'{goos}-{goarch}' / 'Picup.app')
    validate_app(app, version, root, goarch, formal=not unsigned)
    if signing:
        verify_application(app, signing)
    update_assets = [] if unsigned else validate_update_files(root, data, version, f'{goos}-{goarch}', app, go)
    output_dir = Path(output_dir).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    suffix = '-ci-unsigned' if unsigned else ''
    target = output_dir / f'Picup-{version}-{goos}-{goarch}{suffix}.zip'
    descriptor, temporary = tempfile.mkstemp(prefix=f'.{target.stem}-', suffix='.zip', dir=output_dir)
    os.close(descriptor)
    try:
        write_zip(app, temporary)
        if signing:
            # Verify the distributable's extracted bytes, permissions, resource
            # seals and stapled ticket, rather than only its build directory.
            with tempfile.TemporaryDirectory(prefix='picup-final-zip-') as extracted:
                run('/usr/bin/ditto', '-x', '-k', temporary, extracted, cwd=root)
                verify_application(Path(extracted) / 'Picup.app', signing)
        os.replace(temporary, target)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    collected = [str(target)]
    for asset in update_assets:
        destination = output_dir / asset.name
        copy_asset(asset, destination)
        collected.append(str(destination))
    return collected


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True, help='Version that must match mygo.json and the built application')
    parser.add_argument('--output-dir', default='build/release', help='Directory for the versioned ZIP installer')
    parser.add_argument('--unsigned', action='store_true', help='Build a CI test ZIP with application updates disabled; no signing key needed')
    args = parser.parse_args(argv)
    try:
        artifacts = package_release(Path.cwd(), args.version, args.output_dir, unsigned=args.unsigned)
    except (PackageError, SigningError, OSError, ValueError, tarfile.TarError, plistlib.InvalidFileException) as error:
        print(f'error: {error}', file=sys.stderr)
        return 1
    print(json.dumps(artifacts))
    return 0


if __name__ == '__main__':
    sys.exit(main())
