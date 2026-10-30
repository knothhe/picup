#!/usr/bin/env python3
"""Build a native Picup ZIP and its signed update-feed.json, printing paths as JSON.

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
import struct
import subprocess
import sys
import tempfile
import time
import zipfile

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
    require(goos in ('darwin', 'linux'), 'Release packaging supports macOS and Linux hosts only.')
    require(goarch in ('amd64', 'arm64'), f'Unsupported host architecture: {goarch!r}.')
    require((environment.get('GOOS'), environment.get('GOARCH')) == (goos, goarch),
            'Cross compilation is not supported for release packages; unset GOOS and GOARCH and build on the target platform.')
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
        validate_binary(executable, root, formal, 'darwin', arch)


def validate_binary(executable, root, formal, goos, arch):
    metadata = run(os.environ.get('GO', 'go'), 'version', '-m', str(executable), cwd=root, capture=True)
    settings = {}
    for line in metadata.splitlines():
        if line.strip().startswith('build\t'):
            key, separator, value = line.strip()[6:].partition('=')
            if separator:
                settings[key] = value.strip('"\'')
    tags = re.split(r'[,\s]+', settings.get('-tags', ''))
    development = 'picup_dev' in tags
    require(development != formal, 'Formal application contains a development-tagged binary.' if formal
            else 'Unsigned CI application is missing its development data namespace.')
    if arch:
        require(settings.get('GOOS') == goos and settings.get('GOARCH') == arch,
                f'Application binary has unexpected Go platform metadata: {executable.name}')
    if goos == 'darwin':
        require(settings.get('CGO_ENABLED') == '1',
                f'Application binary lacks CGO_ENABLED=1 and native R2 Keychain support: {executable.name}')
    else:
        with executable.open('rb') as stream:
            header = stream.read(20)
        require(len(header) == 20 and header[:6] == b'\x7fELF\x02\x01'
                and struct.unpack('<H', header[18:20])[0] == {'amd64': 62, 'arm64': 183}[arch],
                f'Application is not a native {arch} Linux ELF executable: {executable.name}')


def linux_metadata(version, arch, formal=True):
    return {'schema': 1, 'name': 'Picup' if formal else 'Picup Dev',
            'identifier': 'com.knothhe.picup' if formal else 'com.knothhe.picup.dev',
            'version': version, 'os': 'linux', 'arch': arch,
            'channel': 'release' if formal else 'development'}


def validate_linux_app(app, version, root, arch, formal=True):
    require(app.is_dir() and not app.is_symlink(), f'Missing Linux application: {app}')
    cli_name = 'picup' if formal else 'picup-dev'
    expected = {'picup-gui', cli_name, 'icon.png', 'THIRD_PARTY_NOTICES.txt', 'picup-release.json'}
    require({path.name for path in app.iterdir()} == expected, 'Unexpected or missing Linux application files.')
    for path in app.iterdir():
        require(path.is_file() and not path.is_symlink() and path.stat().st_size > 0,
                f'Missing, empty or symlinked application file: {path.name}')
    require(json.loads((app / 'picup-release.json').read_text()) == linux_metadata(version, arch, formal),
            'Linux application metadata does not match the release.')
    for name in ('picup-gui', cli_name):
        executable = app / name
        require(os.access(executable, os.X_OK), f'Non-executable application program: {name}')
        validate_binary(executable, root, formal, 'linux', arch)
    output = run(str(app / cli_name), '--version', cwd=root, capture=True)
    require(output == f'picup {version}', 'Bundled CLI version does not match the Linux application.')


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

    Return a separate environment only for the signer; never mutate this process's
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


def signed_feed(root, data, version, platform, archive, go, environment, compile_environment):
    # Compile without the key. Only the finished signer process receives it.
    with tempfile.TemporaryDirectory(prefix='picup-zip-signer-') as temporary:
        signer = Path(temporary) / 'sign-update'
        run(go, 'build', '-trimpath', '-o', str(signer), './cmd/sign-update',
            cwd=root, env=compile_environment)
        result = json.loads(run(str(signer), '--archive', str(archive),
                                cwd=root, capture=True, env=environment))
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    require(isinstance(result, dict) and type(result.get('size')) is int
            and result['size'] == archive.stat().st_size and result.get('sha256') == digest,
            'ZIP signer returned mismatched archive size or digest.')
    try:
        signature = base64.b64decode(result.get('signature', ''), validate=True)
    except (TypeError, ValueError):
        signature = b''
    require(len(signature) == 64, 'ZIP signer returned an invalid Ed25519 signature.')
    result = {key: result[key] for key in ('size', 'sha256', 'signature')}
    result['url'] = f'https://github.com/{data["updates"]["github"]}/releases/download/v{version}/{archive.name}'
    return {'schema': 1, 'version': version,
            'date': datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00', 'Z'),
            'platforms': {platform: result}}


def build_linux_app(root, data, version, arch, go, environment, unsigned):
    directory = root / ('build/dev' if unsigned else 'build')
    output = directory / 'dist' / f'linux-{arch}'
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.picup-linux-', dir=output) as temporary:
        app = Path(temporary) / ('Picup Dev' if unsigned else 'Picup')
        app.mkdir()
        cli = 'picup-dev' if unsigned else 'picup'
        # MyGo's Linux packager gives both the GUI and companion CLI the name
        # picup and always writes a tarball. Build the same production identity
        # directly and keep the GUI's separate name in our ZIP layout.
        name = 'Picup Dev' if unsigned else 'Picup'
        identifier = 'com.knothhe.picup.dev' if unsigned else 'com.knothhe.picup'
        ldflags = f'-s -w -X "github.com/egoist/mygo.packageName={name}" '
        ldflags += f'-X github.com/egoist/mygo.packageVersion={version} '
        ldflags += f'-X github.com/egoist/mygo.packageIdentifier={identifier}'
        gui_environment = environment if unsigned else build_environment('mygo_noinspector', formal=True)
        if not unsigned:
            ldflags += ' -X github.com/egoist/mygo.production=1'
        run(go, 'build', '-trimpath', '-ldflags', ldflags,
            '-o', str(app / 'picup-gui'), './cmd/picup-gui', cwd=root, env=gui_environment)
        for source, name in ((directory / 'bin' / cli, cli),
                             (directory / 'THIRD_PARTY_NOTICES.txt', 'THIRD_PARTY_NOTICES.txt'),
                             (root / ('assets/dev/icon-dev-256.png' if unsigned else 'assets/icon.png'), 'icon.png')):
            require(source.is_file() and not source.is_symlink(), f'Missing Linux application resource: {source}')
            shutil.copy2(source, app / name)
        (app / 'picup-release.json').write_text(json.dumps(linux_metadata(version, arch, not unsigned), indent=2) + '\n')
        validate_linux_app(app, version, root, arch, not unsigned)
        target = output / app.name
        if target.exists():
            require(target.is_dir() and not target.is_symlink(), f'Unsafe existing Linux application: {target}')
            shutil.rmtree(target)
        os.replace(app, target)
    return target


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
    environment = None if unsigned else signing_environment()
    go = os.environ.get('GO', 'go')
    goos, goarch = host_platform(go, root)
    signing = None if unsigned or goos != 'darwin' else signing_configuration()
    # Use the same build and MyGo packaging steps as make package, without a DMG.
    # MyGo atomically replaces only this platform's directory after a successful
    # build, so failed builds and old packages from other platforms cannot leak in.
    run('make', 'build', 'BUILD_TAGS=picup_dev' if unsigned else 'BUILD_TAGS=', cwd=root, env=compile_environment)
    if goos == 'darwin':
        arguments = [go, 'tool', 'mygo', 'build', '-skip-dmg']
        if unsigned:
            arguments.append('-debug')
        if signing and signing.profile:
            # MyGo's notarization step uses an intermediate DMG and staples
            # the app. Only the final ZIP is collected for publication.
            arguments.remove('-skip-dmg')
        with packaging_configuration(root, unsigned, signing, disable_updates=True) as config_dir:
            arguments.append(str(config_dir))
            run(*arguments, cwd=root, env=compile_environment)
        app = (root / 'build/dev/dist' / f'{goos}-{goarch}' / 'Picup Dev.app') if unsigned else (
            root / data.get('out', 'dist') / f'{goos}-{goarch}' / 'Picup.app')
        validate_app(app, version, root, goarch, formal=not unsigned)
    else:
        app = build_linux_app(root, data, version, goarch, go, compile_environment, unsigned)
    manifest(root, version)
    if signing:
        verify_application(app, signing)
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
    if not unsigned:
        feed = signed_feed(root, data, version, f'{goos}-{goarch}', target,
                           go, environment, compile_environment)
        feed_path = output_dir / 'update-feed.json'
        descriptor, temporary = tempfile.mkstemp(prefix='.update-feed-', dir=output_dir)
        try:
            with os.fdopen(descriptor, 'w') as stream:
                json.dump(feed, stream, indent=2)
                stream.write('\n')
            os.replace(temporary, feed_path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
        run(go, 'run', './cmd/verify-update', '--feed', str(feed_path),
            '--archive', str(target), '--version', version, '--os', goos, '--arch', goarch,
            cwd=root, env=compile_environment)
        collected.append(str(feed_path))
    return collected


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True, help='Version that must match mygo.json and the built application')
    parser.add_argument('--output-dir', default='build/release', help='Directory for the versioned ZIP installer')
    parser.add_argument('--unsigned', action='store_true', help='Build a CI test ZIP with application updates disabled; no signing key needed')
    args = parser.parse_args(argv)
    try:
        artifacts = package_release(Path.cwd(), args.version, args.output_dir, unsigned=args.unsigned)
    except (PackageError, SigningError, OSError, ValueError, plistlib.InvalidFileException) as error:
        print(f'error: {error}', file=sys.stderr)
        return 1
    print(json.dumps(artifacts))
    return 0


if __name__ == '__main__':
    sys.exit(main())
