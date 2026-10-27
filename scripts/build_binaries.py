#!/usr/bin/env python3
"""Build matching Picup CLI/GUI channels; optionally package the formal app."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

from build_flags import build_environment, BuildFlagsError
from build_manifest import packaging_configuration, verify_development_icon
from dev_signing import configuration as development_signing
from macos_signing import configuration, sign_development_binaries, verify_application, SigningError


def build(package=False, skip_dmg=False, development=False):
    tags = 'picup_dev' if development else ('' if package else os.environ.get('BUILD_TAGS', 'picup_dev'))
    environment = build_environment(tags, formal=package and not development)
    is_dev = 'picup_dev' in environment.get('GOFLAGS', '').split('-tags=')[-1].split(',')
    if is_dev:
        verify_development_icon(Path.cwd())
    signing = (development_signing(Path.cwd(), environment) if is_dev else
               configuration(environment) if package else None)
    macos_target = sys.platform == 'darwin' and (environment.get('GOOS') or 'darwin') == 'darwin'
    if is_dev and signing and not macos_target:
        raise SigningError('Signing Picup Dev requires a macOS host and target; unset GOOS or use a Mac.')
    go = environment.get('GO', 'go')
    directory = Path('build/dev' if is_dev else 'build')
    (directory / 'bin').mkdir(parents=True, exist_ok=True)
    cli = 'picup-dev' if is_dev else 'picup'
    commands = [
        [go, 'run', './internal/buildinfo/cmd/notices', '-output', str(directory / 'THIRD_PARTY_NOTICES.txt')],
        [go, 'build', '-trimpath', '-o', str(directory / 'bin' / cli), './cmd/picup'],
        [go, 'build', '-trimpath', '-o', str(directory / 'bin/picup-gui'), './cmd/picup-gui'],
    ]
    for command in commands:
        subprocess.run(command, env=environment, check=True)
    if is_dev and signing:
        sign_development_binaries(directory, signing)
    if package:
        with packaging_configuration(Path.cwd(), is_dev, signing) as config_dir:
            arguments = [go, 'tool', 'mygo', 'build']
            if skip_dmg and not (signing and signing.profile):
                arguments.append('-skip-dmg')
            if is_dev:
                arguments.append('-debug')
            if config_dir != Path.cwd():
                arguments.append(str(config_dir))
            subprocess.run(arguments, env=environment, check=True)
        if signing:
            host = subprocess.check_output([go, 'env', 'GOHOSTOS', 'GOHOSTARCH'], text=True).split()
            app = directory / 'dist' / '-'.join(host) / ('Picup Dev.app' if is_dev else 'Picup.app')
            verify_application(app, signing, 'Picup Dev' if is_dev else 'Picup', cli)
    return directory / 'bin/picup-gui'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', action='store_true', help='Build the formal, untagged application package')
    parser.add_argument('--skip-dmg', action='store_true')
    parser.add_argument('--development', action='store_true', help='Package the isolated Picup Dev application')
    parser.add_argument('--run', action='store_true', help='Run the exact GUI executable built by this invocation')
    args = parser.parse_args()
    try:
        executable=build(args.package, args.skip_dmg, args.development)
        if args.run:
            return subprocess.run([str(executable.resolve())]).returncode
    except (BuildFlagsError, SigningError, ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f'build: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
