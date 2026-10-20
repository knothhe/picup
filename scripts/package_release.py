#!/usr/bin/env python3
"""Build and validate the current macOS installer, then print its paths as JSON.

The source tree is the current working directory. This lets release.py invoke
this helper against an existing release tag without changing that tag's source.
Python 3.9+, Make, and the project's Go toolchain are required.
"""
import argparse
import json
import os
from pathlib import Path
import plistlib
import re
import stat
import subprocess
import sys
import tempfile
import time
import zipfile


class PackageError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise PackageError(message)


def run(*args, cwd, capture=False):
    result = subprocess.run(
        args, cwd=cwd, text=True,
        stdout=subprocess.PIPE if capture else sys.stderr,
        stderr=subprocess.PIPE if capture else sys.stderr,
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


def validate_app(app, version, root):
    require(app.is_dir() and not app.is_symlink(), f'Missing application bundle: {app}')
    info_path = app / 'Contents' / 'Info.plist'
    require(info_path.is_file(), f'Missing application metadata: {info_path}')
    with info_path.open('rb') as stream:
        info = plistlib.load(stream)
    for key in ('CFBundleVersion', 'CFBundleShortVersionString'):
        require(info.get(key) == version,
                f'Application {key} {info.get(key)!r} does not match requested version {version!r}.')
    require(info.get('CFBundleExecutable') == 'Picup', 'Unexpected application executable in Info.plist.')
    gui = app / 'Contents' / 'MacOS' / 'Picup'
    cli = app / 'Contents' / 'Resources' / 'picup'
    for executable in (gui, cli):
        require(executable.is_file() and os.access(executable, os.X_OK),
                f'Missing or non-executable application program: {executable}')
    output = run(str(cli), '--version', cwd=root, capture=True)
    require(output == f'picup {version}',
            f'Bundled CLI version {output!r} does not match requested version {version!r}.')


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


def package_release(root, version, output_dir):
    root = Path(root).resolve()
    data = manifest(root, version)
    go = os.environ.get('GO', 'go')
    goos, goarch = host_platform(go, root)
    # Use the same build and MyGo packaging steps as make package, without a DMG.
    # MyGo atomically replaces only this platform's directory after a successful
    # build, so failed builds and old packages from other platforms cannot leak in.
    run('make', 'build', cwd=root)
    run(go, 'tool', 'mygo', 'build', '-skip-dmg', cwd=root)
    manifest(root, version)
    app = root / data.get('out', 'dist') / f'{goos}-{goarch}' / 'Picup.app'
    validate_app(app, version, root)
    output_dir = Path(output_dir).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    target = output_dir / f'Picup-{version}-{goos}-{goarch}.zip'
    descriptor, temporary = tempfile.mkstemp(prefix=f'.{target.stem}-', suffix='.zip', dir=output_dir)
    os.close(descriptor)
    try:
        write_zip(app, temporary)
        os.replace(temporary, target)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    return [str(target)]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True, help='Version that must match mygo.json and the built application')
    parser.add_argument('--output-dir', default='build/release', help='Directory for the versioned ZIP installer')
    args = parser.parse_args(argv)
    try:
        artifacts = package_release(Path.cwd(), args.version, args.output_dir)
    except (PackageError, OSError, ValueError, plistlib.InvalidFileException) as error:
        print(f'error: {error}', file=sys.stderr)
        return 1
    print(json.dumps(artifacts))
    return 0


if __name__ == '__main__':
    sys.exit(main())
