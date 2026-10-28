#!/usr/bin/env python3
"""Validate tagged CI packages and publish the exact verified arm64 asset set.

The packaging job verifies app identity and Ed25519 signatures. The record here
binds those verified bytes to that job; hashes alone are not signing evidence.
"""
import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
from urllib.parse import quote, unquote, urlsplit
from urllib.request import Request, urlopen

import release


RECORD = 'ci-release.json'
require = release.require


def identity(tag):
    for name in ('.release.json', 'mygo.json'):
        path = Path(name)
        require(path.is_file() and not path.is_symlink(), f'Missing or symlinked authority file: {name}')
    config = json.loads(Path('.release.json').read_text())
    require(isinstance(config, dict), 'Release configuration must be an object.')
    require(config.get('version_file') == 'mygo.json', 'CI releases require mygo.json as version source.')
    require(config.get('branch', 'main') == 'main' and config.get('remote', 'origin') == 'origin',
            'CI releases require the origin/main release branch.')
    prefix = config.get('tag_prefix', 'v')
    require(isinstance(prefix, str) and tag.startswith(prefix), 'Release tag has the wrong prefix.')
    version = tag[len(prefix):]
    release.parse(version)
    release.git('check-ref-format', 'refs/tags/' + tag)
    require(release.git('cat-file', '-t', 'refs/tags/' + tag) == 'tag', 'Release requires an annotated tag.')
    commit = release.git('rev-parse', 'refs/tags/' + tag + '^{commit}')
    require(release.git('rev-parse', 'HEAD') == commit, 'HEAD does not match the exact release tag commit.')
    ancestor = release.run('git', 'merge-base', '--is-ancestor', commit, 'refs/remotes/origin/main', check=False)
    require(ancestor.returncode == 0, 'Release tag commit must be an ancestor of fetched origin/main.')
    data = json.loads(Path('mygo.json').read_text())
    tagged = json.loads(release.git('show', commit + ':mygo.json'))
    require(data == tagged and data.get('name') == 'Picup' and data.get('version') == version,
            'Release tag and authoritative mygo.json version or identity differ.')
    repo = release.github_repo(release.git('remote', 'get-url', 'origin'))
    require(os.environ.get('GITHUB_REPOSITORY') == repo, 'CI repository differs from the source remote.')
    require(data.get('updates', {}).get('github') == repo, 'Updater repository differs from the release repository.')
    return {'tag': tag, 'version': version, 'commit': commit}, repo, prefix


def expected_names(version):
    return (f'Picup-{version}-darwin-arm64.zip', f'picup-{version}-darwin-arm64.tar.gz',
            'update-darwin-arm64.json')


def assets(directory, version):
    directory = Path(directory)
    require(directory.is_dir() and not directory.is_symlink(), 'Package directory is missing or symlinked.')
    names = expected_names(version)
    require({path.name for path in directory.iterdir()} - {RECORD} == set(names),
            'Release directory has missing or unexpected assets; only the exact arm64 set is allowed.')
    paths = [directory / name for name in names]
    for path in paths:
        require(not path.is_symlink() and stat.S_ISREG(path.stat().st_mode) and path.stat().st_size > 0,
                f'Required asset is empty, symlinked or not a regular file: {path.name}')
    return paths


def validate_feed(directory, repo, tag, version):
    directory = Path(directory)
    archive = directory / expected_names(version)[1]
    feed = directory / 'update-darwin-arm64.json'
    require(feed.stat().st_size <= 1024 * 1024, 'Update feed is too large.')
    data = json.loads(feed.read_text())
    require(isinstance(data, dict) and data.get('version') == version,
            'Update feed version differs from the release tag.')
    require(type(data.get('size')) is int and data['size'] == archive.stat().st_size,
            'Update feed size differs from the full archive.')
    require(not data.get('deltas') and not data.get('previous'), 'CI release requires a full update archive.')
    address = urlsplit(data.get('url', ''))
    require(address.scheme == 'https' and address.netloc == 'github.com' and not address.query
            and not address.fragment and unquote(address.path) ==
            f'/{repo}/releases/download/{tag}/{archive.name}',
            'Update feed must reference this exact public release archive.')
    try:
        signature = base64.b64decode(data.get('signature', ''), validate=True)
        date = datetime.datetime.fromisoformat(data.get('date', '').replace('Z', '+00:00'))
    except (ValueError, TypeError):
        raise release.ReleaseError('Update feed signature or date is invalid.') from None
    require(len(signature) == 64 and date.tzinfo is not None, 'Update feed signature or date is invalid.')


def file_records(paths):
    result = []
    for path in paths:
        digest = hashlib.sha256()
        with path.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
        result.append({'name': path.name, 'size': path.stat().st_size, 'sha256': digest.hexdigest()})
    return result


def run_identity():
    run_id = os.environ.get('GITHUB_RUN_ID', '')
    attempt = os.environ.get('GITHUB_RUN_ATTEMPT', '')
    job = os.environ.get('GITHUB_JOB', '')
    require(re.fullmatch(r'[1-9][0-9]*', run_id) is not None
            and re.fullmatch(r'[1-9][0-9]*', attempt) is not None and bool(job),
            'CI run ID, attempt and job must be present.')
    return {'run_id': run_id, 'run_attempt': int(attempt), 'job': job}


def record(tag, directory, signing_mode):
    require(signing_mode in ('update-only', 'developer-id'), 'Unknown CI signing mode.')
    selected, repo, _ = identity(tag)
    paths = assets(directory, selected['version'])
    validate_feed(directory, repo, tag, selected['version'])
    data = {'schema': 1, **selected, 'channel': 'release', 'os': 'darwin', 'arch': 'arm64',
            'signing_mode': signing_mode, 'source': run_identity(), 'files': file_records(paths)}
    target = Path(directory) / RECORD
    with tempfile.NamedTemporaryFile(mode='w', prefix='.ci-release-', dir=target.parent, delete=False) as stream:
        temporary = Path(stream.name)
        json.dump(data, stream, indent=2)
        stream.write('\n')
    try:
        os.replace(temporary, target)
    finally:
        temporary.unlink(missing_ok=True)
    return data


def verify_record(tag, directory, signing_mode, build_attempt, build_job):
    require(signing_mode in ('update-only', 'developer-id'), 'Unknown CI signing mode.')
    selected, repo, prefix = identity(tag)
    paths = assets(directory, selected['version'])
    target = Path(directory) / RECORD
    require(target.is_file() and not target.is_symlink() and target.stat().st_size <= 1024 * 1024,
            'Missing or invalid CI build record.')
    data = json.loads(target.read_text())
    require(isinstance(data, dict) and type(data.get('schema')) is int
            and isinstance(data.get('source'), dict) and type(data['source'].get('run_attempt')) is int
            and isinstance(data.get('files'), list)
            and all(isinstance(item, dict) and type(item.get('size')) is int for item in data['files']),
            'Invalid CI build record schema or field types.')
    current = run_identity()
    require(type(build_attempt) is int and 0 < build_attempt <= current['run_attempt'], 'Invalid build attempt.')
    expected = {'schema': 1, **selected, 'channel': 'release', 'os': 'darwin', 'arch': 'arm64',
                'signing_mode': signing_mode,
                'source': {'run_id': current['run_id'], 'run_attempt': build_attempt, 'job': build_job},
                'files': file_records(paths)}
    require(data == expected, 'CI build record identity, source attempt, exact asset set or content hashes differ.')
    validate_feed(directory, repo, tag, selected['version'])
    return selected, repo, prefix, paths


def verify_public_files(repo, tag, version, paths):
    with tempfile.TemporaryDirectory(prefix='picup-public-release-') as temporary:
        directory = Path(temporary)
        for path, expected in zip(paths, file_records(paths)):
            url = f'https://github.com/{repo}/releases/download/{quote(tag, safe="")}/{quote(path.name, safe="")}'
            # Public verification deliberately has no GH_TOKEN/Authorization header.
            request = Request(url, headers={'User-Agent': 'Picup-CI-release-verifier'})
            digest = hashlib.sha256()
            size = 0
            with urlopen(request, timeout=30) as response, (directory / path.name).open('xb') as output:
                require(urlsplit(response.geturl()).scheme == 'https', 'Public asset redirected outside HTTPS.')
                while True:
                    chunk = response.read(min(1024 * 1024, expected['size'] - size + 1))
                    if not chunk:
                        break
                    size += len(chunk)
                    require(size <= expected['size'], f'Public asset exceeds verified size: {path.name}')
                    digest.update(chunk)
                    output.write(chunk)
            require(size == expected['size'] and digest.hexdigest() == expected['sha256'],
                    f'Public asset differs from verified build bytes: {path.name}')
        validate_feed(directory, repo, tag, version)


def publish(tag, directory, signing_mode, build_attempt, build_job='build', notes_file=None):
    selected, repo, prefix, paths = verify_record(tag, directory, signing_mode, build_attempt, build_job)
    notes = release.release_notes(repo, prefix, selected['version'], selected['commit'], tag)
    if notes_file is not None:
        require(Path(notes_file).read_text() == notes, 'Release notes file differs from the canonical tagged history.')
    remote_tag = json.loads(release.run('gh', 'api', '--hostname', 'github.com',
                                       f'repos/{repo}/git/ref/tags/{quote(tag, safe="")}').stdout)
    require(remote_tag.get('object', {}).get('type') == 'tag'
            and remote_tag['object'].get('sha') == release.git('rev-parse', 'refs/tags/' + tag),
            'Remote annotated tag differs from the validated source; never move a release tag.')
    names = {path.name for path in paths}
    if tag in release.published_tags(repo):
        existing = json.loads(release.run('gh', 'release', 'view', tag, '--repo', repo,
                                          '--json', 'url,assets,isDraft').stdout)
        require({asset['name'] for asset in existing['assets']} <= names,
                'Existing release has unexpected assets; inspect it before resuming.')
    release.publish_github_release(repo, tag, selected['version'], notes, paths)
    public = json.loads(release.run('gh', 'release', 'view', tag, '--repo', repo,
                                    '--json', 'url,assets,isDraft').stdout)
    require(public.get('isDraft') is False and {asset['name'] for asset in public['assets']} == names,
            'Release is still a draft or does not have the exact required asset set.')
    release.verify_release_assets(public, paths, complete=True)
    verify_public_files(repo, tag, selected['version'], paths)
    print(f'Public release and all three arm64 assets verified: {public["url"]}', flush=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    for name in ('validate', 'record', 'publish'):
        command = commands.add_parser(name)
        command.add_argument('--tag', required=True)
        if name == 'validate':
            command.add_argument('--github-output', type=Path)
        else:
            command.add_argument('--directory', type=Path, default=Path('build/release'))
            command.add_argument('--signing-mode', choices=('update-only', 'developer-id'), required=True)
        if name == 'publish':
            command.add_argument('--build-attempt', type=int, required=True)
            command.add_argument('--build-job', default='build')
            command.add_argument('--notes-file', type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == 'validate':
            selected, _, _ = identity(args.tag)
            if args.github_output:
                with args.github_output.open('a') as stream:
                    for key, value in selected.items():
                        stream.write(f'{key}={value}\n')
            print(json.dumps(selected))
        elif args.command == 'record':
            print(json.dumps(record(args.tag, args.directory, args.signing_mode)))
        else:
            publish(args.tag, args.directory, args.signing_mode, args.build_attempt, args.build_job, args.notes_file)
        return 0
    except (release.ReleaseError, OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        print(f'CI release: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
