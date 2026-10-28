#!/usr/bin/env python3
"""Print the existing release-note format for an exact local release tag."""
import argparse
import json
import os
from pathlib import Path
import re
import sys

from release import ReleaseError, git, parse, release_notes, require


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('repository', help='GitHub owner/repository')
    parser.add_argument('tag', help='Exact existing release tag')
    args = parser.parse_args(argv)
    require(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', args.repository) is not None,
            'Repository must be a GitHub owner/repository.')
    root = git('rev-parse', '--show-toplevel')
    os.chdir(root)
    config = json.loads(Path('.release.json').read_text()) if Path('.release.json').exists() else {}
    require(isinstance(config, dict), '.release.json must contain an object.')
    prefix = config.get('tag_prefix', 'v')
    require(isinstance(prefix, str) and not prefix.startswith('-'), 'Invalid tag_prefix.')
    require(args.tag.startswith(prefix), 'Release tag does not match configured tag_prefix.')
    target = args.tag[len(prefix):]
    parse(target)
    git('check-ref-format', 'refs/tags/' + args.tag)
    commit = git('rev-parse', '--verify', 'refs/tags/' + args.tag + '^{commit}')
    filename = config.get('version_file', 'mygo.json')
    require(filename in ('mygo.json', 'package.json', 'VERSION'), 'Invalid version_file.')
    source = git('show', f'{commit}:{filename}')
    version = source.strip() if filename == 'VERSION' else json.loads(source).get('version')
    require(version == target, 'Tagged source version differs from the release tag.')
    print(release_notes(args.repository, prefix, target, commit, args.tag), end='')


if __name__ == '__main__':
    try:
        main()
    except (ReleaseError, OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        print(f'ERROR: {error}', file=sys.stderr)
        sys.exit(1)
