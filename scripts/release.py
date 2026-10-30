#!/usr/bin/env python3
"""Picup SemVer releases, adapted from h2-semver-release for mygo.json.

Python 3.9+, Git; gh for GitHub releases/initial checks.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
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


def verify_state(before, versions):
    after = check_state()
    require(after['head'] == before['head'], 'A check changed HEAD. Inspect repository before continuing.')
    require(after['branch'] == before['branch'], 'A check changed branches.')
    require(after['refs'] == before['refs'], 'A check changed local branches or tags. Inspect repository before continuing.')
    require(after['index'] == before['index'], 'A check changed the index. Inspect repository before continuing.')
    require(all(Path(name).is_file() and not Path(name).is_symlink() and Path(name).read_bytes() == content
                for name, content in versions.items()), 'A check changed version files. Inspect and resolve manually before retrying.')
    require(after['status'] == before['status'] and after['diff'] == before['diff'],
            'Checks changed the working tree. Inspect and resolve manually before retrying.')


def run_checks(checks, files, phase):
    if not checks:
        return
    before = check_state()
    versions = {name: Path(name).read_bytes() for name in files}
    for command in checks:
        result = subprocess.run(command)
        verify_state(before, versions)
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
    require(tag not in published_tags(repo), f'GitHub Release already exists: {tag}. Initial release must be unpublished.')


def published_tags(repo):
    return run('gh', 'api', '--hostname', 'github.com', '--paginate',
               f'repos/{repo}/releases', '--jq', '.[].tag_name').stdout.splitlines()


def package_command(target, output):
    return [sys.executable, str(Path(__file__).resolve().with_name('package_release.py')),
            '--version', target, '--output-dir', str(output)]


def extract_source(commit, destination):
    """Extract only this commit's tracked source, preserving safe source symlinks."""
    destination = destination.resolve()
    archive = destination.parent / 'source.tar'
    git('archive', '--format=tar', '--output=' + str(archive), commit)
    destination.mkdir()
    with tarfile.open(archive) as source:
        for member in source:
            path = Path(member.name)
            require(not path.is_absolute() and '..' not in path.parts, 'Unsafe path in source archive.')
            target = destination / path
            require(target.parent.resolve().is_relative_to(destination), 'Source archive escapes its directory.')
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.issym():
                link = Path(member.linkname)
                require(not link.is_absolute() and (target.parent / link).resolve().is_relative_to(destination),
                        'Source archive contains an external symlink.')
                target.parent.mkdir(parents=True, exist_ok=True)
                target.symlink_to(member.linkname)
            else:
                require(member.isfile(), 'Unsupported entry in source archive.')
                target.parent.mkdir(parents=True, exist_ok=True)
                with source.extractfile(member) as content, target.open('xb') as output:
                    shutil.copyfileobj(content, output)
                target.chmod(member.mode & 0o777)


def build_assets(target, files, source_commit=None):
    output = Path.cwd() / 'build' / 'release'
    output.mkdir(parents=True, exist_ok=True)
    before = check_state()
    versions = {name: Path(name).read_bytes() for name in files}
    with tempfile.TemporaryDirectory(prefix='picup-release-') as temporary:
        source = Path.cwd()
        if source_commit:
            source = Path(temporary) / 'source'
            extract_source(source_commit, source)
        requires_updates = bool(json.loads((source / 'mygo.json').read_text()).get('updates'))
        print(f'Building release packages for {target} from {source_commit or "working tree"}...', flush=True)
        result = subprocess.run(package_command(target, output), cwd=source, text=True, stdout=subprocess.PIPE)
    verify_state(before, versions)
    require(result.returncode == 0, 'Package build failed. Inspect version files before retrying; no new release commit/tag was created.')
    assets = json.loads(result.stdout)
    require(isinstance(assets, list) and assets and all(isinstance(path, str) for path in assets),
            'Package command must return a nonempty JSON array of asset paths.')
    paths = [Path(path) for path in assets]
    require(len(set(paths)) == len(paths), 'Package command returned duplicate asset paths.')
    targets = set()
    for path in paths:
        kind = re.fullmatch(r'Picup-' + re.escape(target) + r'-((?:darwin|linux)-(?:arm64|amd64))\.zip', path.name)
        feed = path.name == 'update-feed.json'
        require(path.is_absolute() and path.parent.resolve() == output.resolve() and not path.is_symlink()
                and path.is_file() and path.stat().st_size > 0 and (kind or feed),
                f'Invalid release asset (expected a versioned platform ZIP or update-feed.json in {output}): {path}')
        if kind:
            targets.add(kind.group(1))
    names = {path.name for path in paths}
    # Historical tags without updater configuration keep their original ZIP-only
    # format. New signed releases must never publish a partial update set.
    if requires_updates or any(path.suffix != '.zip' for path in paths):
        require(targets and 'update-feed.json' in names,
                'Package command returned an incomplete signed ZIP/feed asset set.')
        feed = json.loads((output / 'update-feed.json').read_text())
        require(type(feed.get('schema')) is int and feed['schema'] == 1 and feed.get('version') == target
                and isinstance(feed.get('platforms'), dict) and set(feed['platforms']) == targets,
                'Unified update feed differs from the packaged version or platform ZIP set.')
    return paths


def verify_release_assets(release, assets, complete):
    actual = {asset['name']: asset for asset in release['assets']}
    for path in assets:
        asset = actual.get(path.name)
        if asset is None and not complete:
            continue
        asset = asset or {}
        digest = 'sha256:' + hashlib.sha256(path.read_bytes()).hexdigest()
        require(asset.get('size') == path.stat().st_size and asset.get('digest') == digest,
                f'Release asset is missing or differs from the verified local file: {path.name}. '
                'Existing assets were kept; inspect the draft before retrying.')


def publish_github_release(repo, tag, target, notes, assets):
    uploaded = []
    if tag in published_tags(repo):
        existing = json.loads(run('gh', 'release', 'view', tag, '--repo', repo, '--json', 'url,assets,isDraft').stdout)
        was_draft = existing.get('isDraft', False)
        verify_release_assets(existing, assets, complete=False)
        names = {asset['name'] for asset in existing['assets']}
        uploaded = [path for path in assets if path.name not in names]
        for path in assets:
            if path.name in names:
                print(f'Keeping existing release asset: {path.name}', flush=True)
        if uploaded:
            # A resumed public release must never expose a feed before its
            # archive upload completes. Drafts follow the same ordering.
            payloads = [path for path in uploaded if not path.name.startswith('update-')]
            feeds = [path for path in uploaded if path.name.startswith('update-')]
            if payloads:
                run('gh', 'release', 'upload', tag, *(str(path) for path in payloads), '--repo', repo)
            if feeds and not was_draft:
                staged = json.loads(run('gh', 'release', 'view', tag, '--repo', repo,
                                        '--json', 'url,assets,isDraft').stdout)
                verify_release_assets(staged, [path for path in assets if not path.name.startswith('update-')],
                                      complete=True)
            if feeds:
                run('gh', 'release', 'upload', tag, *(str(path) for path in feeds), '--repo', repo)
    else:
        command = ['gh', 'release', 'create', tag, *(str(path) for path in assets),
                   '--repo', repo, '--verify-tag', '--notes-file', '-', '--title', tag, '--draft']
        if parse(target)[1] is not None:
            command += ['--prerelease']
        run(*command, input=notes)
        uploaded = assets
        was_draft = True
    release = json.loads(run('gh', 'release', 'view', tag, '--repo', repo, '--json', 'url,assets,isDraft').stdout)
    verify_release_assets(release, assets, complete=True)
    # New releases are invisible to the updater until every installer, archive
    # and manifest has been uploaded and its exact bytes verified in the draft.
    if was_draft:
        run('gh', 'release', 'edit', tag, '--repo', repo, '--draft=false')
    print(release['url'], flush=True)


def publish_existing_assets(args, config, github, with_assets, remote, prefix):
    require(not args.resume and not args.no_push and github and with_assets,
            '--assets-only requires GitHub Release and release_assets, and cannot be combined with --resume or --no-push.')
    target = args.version
    parse(target)
    tag = prefix + target
    git('check-ref-format', 'refs/tags/' + tag)
    require(git('cat-file', '-t', 'refs/tags/' + tag) == 'tag', 'Asset backfill requires an existing annotated tag.')
    commit = git('rev-parse', 'refs/tags/' + tag + '^{commit}')
    filename = config.get('version_file', 'mygo.json')
    require(filename == 'mygo.json', 'Release assets require mygo.json as the version source.')
    manifest = json.loads(git('show', f'{commit}:mygo.json'))
    require(manifest.get('version') == target, 'Tagged source version differs from the requested version.')
    urls = git('remote', 'get-url', '--push', '--all', remote).splitlines()
    require(len(urls) == 1, 'Exactly one push URL is required.')
    repo = github_repo(urls[0])
    reference = json.loads(run('gh', 'api', '--hostname', 'github.com',
                               f'repos/{repo}/git/ref/tags/{quote(tag, safe="")}').stdout)
    require(reference['object']['sha'] == git('rev-parse', 'refs/tags/' + tag),
            'Remote tag differs from the local tag; never replace a published tag.')
    # A successful listing is required even for preview; permission errors are not absence.
    published_tags(repo)
    notes = release_notes(repo, prefix, target, commit, tag)
    print(json.dumps({'mode': 'execute' if args.apply else 'preview', 'release_type': 'assets-only',
                      'target': target, 'tag': tag, 'source_commit': commit, 'push': False,
                      'github_release': True, 'github_repo': repo, 'release_notes': notes,
                      'package': package_command(target, Path.cwd() / 'build' / 'release')}, indent=2), flush=True)
    if not args.apply:
        return
    run('gh', 'auth', 'status', '--hostname', 'github.com')
    assets = build_assets(target, {}, source_commit=commit)
    publish_github_release(repo, tag, target, notes, assets)
    print(f'Release assets complete: {tag}', flush=True)


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
    parser.add_argument('--assets-only', action='store_true', help='Build an existing tagged version and add missing GitHub Release assets; no Git writes')
    args = parser.parse_args()
    require(not (args.resume and args.version == 'initial'), '--resume requires the exact existing version, not initial.')
    root = git('rev-parse', '--show-toplevel')
    os.chdir(root)
    config = json.loads(Path('.release.json').read_text()) if Path('.release.json').exists() else {}
    require(isinstance(config, dict), '.release.json must contain an object.')
    require(not set(config) - {'version_file', 'branch', 'remote', 'tag_prefix', 'checks', 'check_phase', 'github_release', 'release_assets', 'release_executor'}, 'Unknown .release.json field.')
    executor = config.get('release_executor', 'local')
    require(executor in ('local', 'github-actions'), 'release_executor must be local or github-actions.')
    branch = config.get('branch', 'main')
    remote = config.get('remote', 'origin')
    prefix = config.get('tag_prefix', 'v')
    for name, value in [('branch', branch), ('remote', remote), ('tag_prefix', prefix)]:
        require(isinstance(value, str) and (value or name == 'tag_prefix') and not value.startswith('-'), f'Invalid {name}.')
    configured_github = config.get('github_release', False)
    require(isinstance(configured_github, bool), 'github_release must be boolean.')
    github = args.github if args.github is not None else configured_github
    require(isinstance(github, bool), 'github_release must be boolean.')
    with_assets = config.get('release_assets', False)
    require(isinstance(with_assets, bool), 'release_assets must be boolean.')
    if executor == 'github-actions':
        require(not configured_github and not github and not with_assets,
                'github-actions requires github_release: false and release_assets: false; '
                'local builds/uploads and --github-release cannot be enabled.')
        require(not args.assets_only,
                '--assets-only is unavailable with github-actions. Recover the existing '
                'release workflow with gh run rerun <run-id> --failed; '
                'do not bump the version or move the tag.')
    require(not (github and args.no_push), 'GitHub Release requires pushing. Use --no-github-release with --no-push.')
    if args.assets_only:
        publish_existing_assets(args, config, github, with_assets, remote, prefix)
        return
    require(not with_assets or config.get('version_file') == 'mygo.json', 'Release assets require mygo.json as the version source.')
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
    repo = github_repo(url) if github or (executor == 'github-actions' and url) else None
    initial_repo = github_repo(url, required=False) if initial and not args.no_push else None
    if initial_repo and not args.resume:
        require_unpublished_release(initial_repo, tag)
    phase = 'current-version' if initial else check_phase
    notes = release_notes(repo, prefix, target, f'{head}^' if args.resume and not initial else head, tag) if github else None
    print(json.dumps({'mode': 'execute' if args.apply else 'preview', 'release_type': 'initial' if initial else 'upgrade',
                      'current': current, 'target': target, 'release_executor': executor,
                      'branch': branch, 'remote': remote, 'tag': tag, 'files': [] if initial else list(files),
                      'create_version_commit': not initial and not args.resume,
                      'checks': [] if args.resume else checks, 'check_phase': None if args.resume else phase, 'push': not args.no_push,
                      'github_release': github, 'github_repo': repo, 'release_notes': notes,
                      'package': package_command(target, Path.cwd() / 'build' / 'release') if with_assets else None,
                      'package_source': tag if args.resume or executor == 'github-actions' else 'working-tree',
                      'resume': args.resume}, indent=2), flush=True)
    if not args.apply:
        return
    if github:
        run('gh', 'auth', 'status', '--hostname', 'github.com')
    assets = []
    if not args.resume:
        if not initial and check_phase == 'after-bump':
            write_versions(files, target)
        run_checks(checks, files, phase)
        if not initial and check_phase == 'before-bump':
            write_versions(files, target)
        if with_assets:
            assets = build_assets(target, files)
        if not initial:
            changed = set(git('diff', '--name-only').splitlines())
            require(changed == set(files) and not git('ls-files', '--others', '--exclude-standard'),
                    'Unexpected changes after updating the version. Inspect and resolve manually before retrying.')
            checked_version, _ = version_files(config)
            require(checked_version == target, 'Release version differs from the target.')
            git('add', '--', *files)
            expected_tree = git('write-tree')
            git('commit', '-m', f'chore(release): {tag}')
            require(git('rev-parse', 'HEAD^{tree}') == expected_tree
                    and git('rev-parse', 'HEAD^') == head
                    and git('log', '-1', '--format=%s') == f'chore(release): {tag}',
                    'A commit hook changed the packaged source or release commit. Inspect the commit; no tag was created.')
        clean()
        require(git('symbolic-ref', '--short', 'HEAD') == branch, 'A hook changed branches.')
        require(version_files(config)[0] == target, 'A commit hook changed the version.')
        git('tag', '-a', tag, '-m', initial_tag_message(tag) if initial else f'Release {tag}')
    elif with_assets:
        assets = build_assets(target, files, source_commit=head)
    if not args.no_push:
        # One transaction; never push unrelated tags or fall back to non-atomic writes.
        git('push', '--atomic', remote, f'HEAD:refs/heads/{branch}', f'refs/tags/{tag}:refs/tags/{tag}')
    if github:
        publish_github_release(repo, tag, target, notes, assets)
    if executor == 'github-actions':
        if args.no_push:
            print(f'Git phase complete: {tag} (local only; GitHub Actions was not triggered).')
        else:
            print(f'Git phase complete: {tag} pushed. GitHub Actions handles the build and '
                  'publication; verify its run and Release assets before reporting release success.')
        if args.resume:
            print('Re-pushing a tag already on the remote does not trigger a new workflow run. '
                  'Recover the existing run with gh run rerun <run-id> --failed; '
                  'no workflow was dispatched by this command.')
    else:
        print(f'Release complete: {tag}' + (' (local only)' if args.no_push else ''))


if __name__ == '__main__':
    try:
        main()
    except (ReleaseError, OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        print(f'ERROR: {error}\nStopped without automatic rollback or force-push. Inspect git status/log/tags. If the release tag already exists at HEAD, retry with the exact version --resume --apply.', file=sys.stderr)
        sys.exit(1)
