#!/usr/bin/env python3
"""Build matching Picup CLI/GUI channels; optionally package the formal app."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

from build_flags import build_environment, BuildFlagsError


def build(package=False, skip_dmg=False):
    environment = build_environment('' if package else os.environ.get('BUILD_TAGS', 'picup_dev'), formal=package)
    go = environment.get('GO', 'go')
    Path('build/bin').mkdir(parents=True, exist_ok=True)
    commands = [
        [go, 'run', './internal/buildinfo/cmd/notices', '-output', 'build/THIRD_PARTY_NOTICES.txt'],
        [go, 'build', '-trimpath', '-o', 'build/bin/picup', './cmd/picup'],
        [go, 'build', '-trimpath', '-o', 'build/bin/picup-gui', './cmd/picup-gui'],
    ]
    if package:
        commands.append([go, 'tool', 'mygo', 'build', *(['-skip-dmg'] if skip_dmg else [])])
    for command in commands:
        subprocess.run(command, env=environment, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--package', action='store_true', help='Build the formal, untagged application package')
    parser.add_argument('--skip-dmg', action='store_true')
    args = parser.parse_args()
    try:
        build(args.package, args.skip_dmg)
    except (BuildFlagsError, OSError, subprocess.CalledProcessError) as error:
        print(f'build: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
