#!/usr/bin/env python3
"""Picup SemVer releases, adapted from h2-semver-release for mygo.json.

Python 3.9+, Git; gh for GitHub releases/initial checks.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import quote


class ReleaseError(Exception):
    pass


def run(*args, check=True, input=None):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, input=input)
    if check and result.returncode:
        raise ReleaseError(f"Command failed: {' '.join(args)}\n{result.stderr.strip()}")
    return result


def git(*args):
    return run('git', *args).stdout.strip()


def require(condition, message):
    if not condition:
        raise ReleaseError(message)


def parse(version):
    match = re.fullmatch(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+([0-9A-Za-z.-]+))?', version)
    require(match is not None, f'Invalid SemVer: {version}')
    major, minor, patch, pre, build = match.groups()
    for group in (pre, build):
        if group is not None:
            require(all(group.split('.')), f'Empty SemVer identifier: {version}')
    if pre:
        require(all(not (x.isdigit() and len(x) > 1 and x[0] == '0') for x in pre.split('.')),
                f'Leading zero in prerelease: {version}')
    return (int(major), int(minor), int(patch)), pre


def precedence(version):
    core, pre = parse(version)
    identifiers = tuple((0, int(x)) if x.isdigit() else (1, x) for x in pre.split('.')) if pre else ()
    return core, pre is None, identifiers


def next_version(current, bump):
    core, pre = parse(current)
    if bump in ('major', 'minor', 'patch'):
        require(pre is None, 'For a prerelease, specify the exact next version (for example 1.2.0).')
        numbers = list(core)
        index = ('major', 'minor', 'patch').index(bump)
        numbers[index] += 1
        numbers[index + 1:] = [0] * (2 - index)
        target = '.'.join(map(str, numbers))
    else:
        target = bump
    require(precedence(target) > precedence(current), 'New version must have greater SemVer precedence; build metadata alone is not an upgrade.')
    return target


def json_text(path, data):
    original = path.read_text()
    indent_match = re.search(r'\n([ \t]+)"', original)
    indent = indent_match.group(1) if indent_match else 2
    return json.dumps(data, indent=indent, ensure_ascii=False) + '\n'


def version_files(config):
    filename = config.get('version_file', 'package.json' if Path('package.json').exists() else 'VERSION')
    require(filename in ('package.json', 'VERSION', 'mygo.json'), 'version_file must be package.json, VERSION or mygo.json.')
    path = Path(filename)
    require(path.is_file() and not path.is_symlink(), f'Missing or symlinked version file: {filename}')
    # Picup embeds this same file in its CLI and GUI, so only this file is updated.
    if filename == 'mygo.json':
        data = json.loads(path.read_text())
        current = data.get('version', '')
        parse(current)
        return current, {filename: data}
    package = json.loads(Path('package.json').read_text()) if Path('package.json').exists() else None
    require(not (package and package.get('workspaces')) and not Path('pnpm-workspace.yaml').exists(),
            'Workspace/monorepo detected. Use the project release system or adapt explicitly; this helper supports one package.')
    if filename == 'VERSION':
        require(package is None, 'For a Node project use package.json as the source of truth, not VERSION.')
        current = path.read_text().strip()
        parse(current)
        return current, {filename: None}
    current = package.get('version', '')
    parse(current)
    files = {filename: package}
    for name in ('package-lock.json', 'npm-shrinkwrap.json'):
        lockpath = Path(name)
        if lockpath.exists():
            require(not lockpath.is_symlink(), f'Symlink not supported: {name}')
            lock = json.loads(lockpath.read_text())
            require(lock.get('version') == current, f'{name} version differs from package.json.')
            require(lock.get('lockfileVersion') in (1, 2, 3), f'Unsupported lockfile: {name}')
            if lock.get('lockfileVersion') in (2, 3):
                require(lock.get('packages', {}).get('', {}).get('version') == current,
                        f'{name} root package version differs from package.json.')
            files[name] = lock
    return current, files


def clean():
    require(not git('status', '--porcelain', '--untracked-files=all'), 'Working tree must be clean, including untracked files. Review and commit your changes first.')


def check_state():
    return {
        'head': git('rev-parse', 'HEAD'),
        'branch': run('git', 'symbolic-ref', '--quiet', 'HEAD', check=False).stdout.strip(),
        'refs': git('for-each-ref', '--format=%(refname) %(objectname)', 'refs/heads', 'refs/tags'),
        'index': run('git', 'ls-files', '--stage', '-z').stdout,
        'status': run('git', 'status', '--porcelain=v1', '-z', '--untracked-files=all').stdout,
        'diff': run('git', 'diff', '--binary', '--no-ext-diff', '--no-textconv', '--no-color').stdout,
    }


def run_checks(checks, files, phase):
    if not checks:
        return
    before = check_state()
    versions = {name: Path(name).read_bytes() for name in files}
    for command in checks:
        result = subprocess.run(command)
        after = check_state()
        require(after['head'] == before['head'], 'A check changed HEAD. Inspect repository before continuing.')
        require(after['branch'] == before['branch'], 'A check changed branches.')
        require(after['refs'] == before['refs'], 'A check changed local branches or tags. Inspect repository before continuing.')
        require(after['index'] == before['index'], 'A check changed the index. Inspect repository before continuing.')
        require(all(Path(name).is_file() and not Path(name).is_symlink() and Path(name).read_bytes() == content
                    for name, content in versions.items()), 'A check changed version files. Inspect and resolve manually before retrying.')
        require(after['status'] == before['status'] and after['diff'] == before['diff'],
                'Checks changed the working tree. Inspect and resolve manually before retrying.')
        detail = ('Current version is unchanged' if phase == 'current-version' else
                  'Stopped before updating the version' if phase == 'before-bump' else 'Version edits remain for inspection')
        require(result.returncode == 0, f'Check failed: {command}. {detail}; no release commit/tag was created.')


def write_versions(files, target):
    for name, data in files.items():
        if data is None:
            content = target + '\n'
        else:
            data['version'] = target
            if name != 'package.json' and '' in data.get('packages', {}):
                data['packages']['']['version'] = target
            content = json_text(Path(name), data)
        Path(name).write_text(content)


def github_repo(url, required=True):
    match = re.fullmatch(r'(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([^/]+/[^/]+?)(?:\.git)?/?', url)
    if match is None and not required:
        return None
    require(match is not None, 'GitHub Release requires a standard github.com remote URL; aliases/Enterprise need explicit adaptation.')
    return match.group(1)


def require_unpublished_release(repo, tag):
    # Include visible drafts; a failed listing must not be treated as absence.
    tags = run('gh', 'api', '--hostname', 'github.com', '--paginate',
               f'repos/{repo}/releases', '--jq', '.[].tag_name').stdout.splitlines()
    require(tag not in tags, f'GitHub Release already exists: {tag}. Initial release must be unpublished.')


def initial_tag_message(tag):
    return f'Release {tag}\n\nRelease-Type: initial'


def release_notes(repo, prefix, target, source_head, tag):
    require(git('rev-parse', '--is-shallow-repository') == 'false',
            'Release notes require complete Git history and tags; fetch them before generating notes.')
    candidates = []
    for name in git('tag', '--merged', source_head, '--list').splitlines():
        if not name.startswith(prefix):
            continue
        try:
            version = name[len(prefix):]
            if precedence(version) < precedence(target):
                candidates.append((precedence(version), name))
        except (ReleaseError, ValueError):
            continue
    previous = max(candidates)[1] if candidates else None
    commit_range = f'refs/tags/{previous}..{source_head}' if previous else source_head
    commits = []
    for line in git('log', '--no-merges', '--format=%h%x1f%s', commit_range).splitlines():
        short_hash, subject = line.split('\x1f', 1)
        if not subject.startswith('chore(release): '):
            escaped = re.sub(r'([\\`*_{}\[\]()#+.!|>~-])', r'\\\1', subject)
            commits.append(f'- {escaped} (`{short_hash}`)')
    path = (f'compare/{quote(previous, safe="")}...{quote(tag, safe="")}' if previous
            else f'commits/{quote(tag, safe="")}')
    entries = '\n'.join(commits) if commits else '- No changes since the previous release.'
    return f'**Full Changelog**: https://github.com/{repo}/{path}\n\n### Commits\n\n{entries}\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('version', help='initial (publish current version), major, minor, patch, or exact SemVer')
    parser.add_argument('--apply', action='store_true', help='Execute; without this flag only preview')
    parser.add_argument('--no-push', action='store_true', help='Create local tag only, plus a version commit for upgrades')
    group = parser.add_mutually_exclusive_group()
    group.add_argument('--github-release', dest='github', action='store_true')
    group.add_argument('--no-github-release', dest='github', action='store_false')
    parser.set_defaults(github=None)
    parser.add_argument('--resume', action='store_true', help='Push an existing release at HEAD; exact version required, no bump')
    args = parser.parse_args()
    require(not (args.resume and args.version == 'initial'), '--resume requires the exact existing version, not initial.')
    root = git('rev-parse', '--show-toplevel')
    os.chdir(root)
    config = json.loads(Path('.release.json').read_text()) if Path('.release.json').exists() else {}
    require(isinstance(config, dict), '.release.json must contain an object.')
    require(not set(config) - {'version_file', 'branch', 'remote', 'tag_prefix', 'checks', 'check_phase', 'github_release'}, 'Unknown .release.json field.')
    branch = config.get('branch', 'main')
    remote = config.get('remote', 'origin')
    prefix = config.get('tag_prefix', 'v')
    for name, value in [('branch', branch), ('remote', remote), ('tag_prefix', prefix)]:
        require(isinstance(value, str) and (value or name == 'tag_prefix') and not value.startswith('-'), f'Invalid {name}.')
    github = args.github if args.github is not None else config.get('github_release', False)
    require(isinstance(github, bool), 'github_release must be boolean.')
    require(not (github and args.no_push), 'GitHub Release requires pushing. Use --no-github-release with --no-push.')
    checks = config.get('checks', [])
    require(isinstance(checks, list) and all(isinstance(c, list) and c and all(isinstance(s, str) and s for s in c) for c in checks), 'checks must be arrays of command arguments.')
    check_phase = config.get('check_phase', 'before-bump')
    require(check_phase in ('before-bump', 'after-bump'), 'check_phase must be before-bump or after-bump.')
    clean()
    require(git('symbolic-ref', '--quiet', '--short', 'HEAD') == branch, f'Release must run on branch {branch}.')
    head = git('rev-parse', 'HEAD')
    current, files = version_files(config)
    for name in files:
        git('ls-files', '--error-unmatch', '--', name)
    initial = args.version == 'initial'
    target = args.version if args.resume else current if initial else next_version(current, args.version)
    parse(target)
    tag = prefix + target
    git('check-ref-format', 'refs/tags/' + tag)
    tag_exists = run('git', 'show-ref', '--verify', '--quiet', 'refs/tags/' + tag, check=False).returncode == 0
    if args.resume:
        require(current == target and tag_exists, 'Resume requires the exact current version and an existing local tag.')
        require(git('rev-parse', 'refs/tags/' + tag + '^{commit}') == head, 'Resume tag must point to HEAD.')
        require(git('cat-file', '-t', 'refs/tags/' + tag) == 'tag', 'Resume requires an annotated tag.')
        initial = git('for-each-ref', '--format=%(contents)', 'refs/tags/' + tag) == initial_tag_message(tag)
        require(initial or git('log', '-1', '--format=%s') == f'chore(release): {tag}',
                'HEAD is not the expected release commit and tag has no initial-release marker.')
    else:
        require(not tag_exists, f'Tag already exists: {tag}. Use --resume with the exact version after a partial release.')
    url = None
    if not args.no_push:
        urls = git('remote', 'get-url', '--push', '--all', remote).splitlines()
        require(len(urls) == 1, 'Exactly one push URL is required.')
        url = urls[0]
        # Read the push destination, which may differ from the fetch URL.
        refs = dict((line.split()[1], line.split()[0]) for line in git('ls-remote', url, 'refs/heads/' + branch, 'refs/tags/' + tag).splitlines())
        remote_head = refs.get('refs/heads/' + branch)
        require(remote_head is not None, f'Remote branch {branch} is missing; establish the branch before releasing.')
        require(run('git', 'merge-base', '--is-ancestor', remote_head, head, check=False).returncode == 0,
                'Remote branch is ahead/diverged or unavailable locally. Fetch and reconcile before releasing.')
        remote_tag = refs.get('refs/tags/' + tag)
        if remote_tag:
            require(args.resume and remote_tag == git('rev-parse', 'refs/tags/' + tag), 'Remote tag already exists with a different release; never overwrite it.')
    repo = github_repo(url) if github else None
    initial_repo = github_repo(url, required=False) if initial and not args.no_push else None
    if initial_repo and not args.resume:
        require_unpublished_release(initial_repo, tag)
    phase = 'current-version' if initial else check_phase
    notes = release_notes(repo, prefix, target, f'{head}^' if args.resume and not initial else head, tag) if github else None
    print(json.dumps({'mode': 'execute' if args.apply else 'preview', 'release_type': 'initial' if initial else 'upgrade',
                      'current': current, 'target': target,
                      'branch': branch, 'remote': remote, 'tag': tag, 'files': [] if initial else list(files),
                      'create_version_commit': not initial and not args.resume,
                      'checks': [] if args.resume else checks, 'check_phase': None if args.resume else phase, 'push': not args.no_push,
                      'github_release': github, 'github_repo': repo, 'release_notes': notes,
                      'resume': args.resume}, indent=2), flush=True)
    if not args.apply:
        return
    if github:
        run('gh', 'auth', 'status', '--hostname', 'github.com')
    if not args.resume:
        if not initial and check_phase == 'after-bump':
            write_versions(files, target)
        run_checks(checks, files, phase)
        if not initial:
            if check_phase == 'before-bump':
                write_versions(files, target)
            changed = set(git('diff', '--name-only').splitlines())
            require(changed == set(files) and not git('ls-files', '--others', '--exclude-standard'),
                    'Unexpected changes after updating the version. Inspect and resolve manually before retrying.')
            checked_version, _ = version_files(config)
            require(checked_version == target, 'Release version differs from the target.')
            git('add', '--', *files)
            git('commit', '-m', f'chore(release): {tag}')
        clean()
        require(git('symbolic-ref', '--short', 'HEAD') == branch, 'A hook changed branches.')
        require(version_files(config)[0] == target, 'A commit hook changed the version.')
        git('tag', '-a', tag, '-m', initial_tag_message(tag) if initial else f'Release {tag}')
    if not args.no_push:
        # One transaction; never push unrelated tags or fall back to non-atomic writes.
        git('push', '--atomic', remote, f'HEAD:refs/heads/{branch}', f'refs/tags/{tag}:refs/tags/{tag}')
    if github:
        existing = run('gh', 'release', 'view', tag, '--repo', repo, '--json', 'url', check=False)
        if existing.returncode == 0:
            print(existing.stdout.strip())
        else:
            command = ['gh', 'release', 'create', tag, '--repo', repo, '--verify-tag', '--notes-file', '-', '--title', tag]
            if parse(target)[1] is not None:
                command += ['--prerelease']
            print(run(*command, input=notes).stdout.strip())
    print(f'Release complete: {tag}' + (' (local only)' if args.no_push else ''))


if __name__ == '__main__':
    try:
        main()
    except (ReleaseError, OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        print(f'ERROR: {error}\nStopped without automatic rollback or force-push. Inspect git status/log/tags. If the release tag already exists at HEAD, retry with the exact version --resume --apply.', file=sys.stderr)
        sys.exit(1)
