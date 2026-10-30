#!/usr/bin/env python3
"""Verify native tagged builds and publish ZIPs with one cross-platform feed.

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
import shutil
import stat
import sys
import tempfile
from urllib.parse import quote, unquote, urlsplit
from urllib.request import Request, urlopen

import release


RECORD = 'ci-release.json'
FEED = 'update-feed.json'
TARGETS = ('darwin-arm64', 'linux-amd64')
BUILD_JOBS = {'darwin-arm64': 'macos', 'linux-amd64': 'linux'}
SIGNING_MODES = ('fixed-identity', 'update-only', 'developer-id')
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


def expected_names(version, targets=TARGETS):
    return (*(f'Picup-{version}-{target}.zip' for target in targets), FEED)


def assets(directory, version, targets=TARGETS):
    directory = Path(directory)
    require(directory.is_dir() and not directory.is_symlink(), 'Package directory is missing or symlinked.')
    names = expected_names(version, targets)
    require({path.name for path in directory.iterdir()} - {RECORD} == set(names),
            'Release directory has missing or unexpected assets; only the exact target set is allowed.')
    paths = [directory / name for name in names]
    for path in paths:
        require(not path.is_symlink() and stat.S_ISREG(path.stat().st_mode) and path.stat().st_size > 0,
                f'Required asset is empty, symlinked or not a regular file: {path.name}')
    return paths


def validate_feed(directory, repo, tag, version, targets=TARGETS):
    directory = Path(directory)
    feed = directory / FEED
    require(feed.stat().st_size <= 1024 * 1024, 'Update feed is too large.')
    data = json.loads(feed.read_text())
    require(isinstance(data, dict) and set(data) <= {'schema', 'version', 'date', 'notes', 'platforms'}
            and type(data.get('schema')) is int and data['schema'] == 1
            and data.get('version') == version and isinstance(data.get('platforms'), dict)
            and set(data['platforms']) == set(targets) and isinstance(data.get('notes', ''), str),
            'Update feed schema, version or target set differs from the release.')
    try:
        date = datetime.datetime.fromisoformat(data.get('date', '').replace('Z', '+00:00'))
    except (ValueError, TypeError):
        raise release.ReleaseError('Update feed date is invalid.') from None
    require(date.tzinfo is not None, 'Update feed date is invalid.')
    for target in targets:
        asset = data['platforms'][target]
        archive = directory / expected_names(version, (target,))[0]
        require(isinstance(asset, dict) and set(asset) == {'url', 'size', 'sha256', 'signature'},
                'Update feed platform asset fields are invalid.')
        require(type(asset.get('size')) is int and asset['size'] == archive.stat().st_size,
                'Update feed size differs from the ZIP.')
        require(asset.get('sha256') == file_records([archive])[0]['sha256'],
                'Update feed SHA-256 differs from the ZIP.')
        address = urlsplit(asset.get('url', ''))
        require(address.scheme == 'https' and address.netloc == 'github.com' and not address.query
                and not address.fragment and unquote(address.path) ==
                f'/{repo}/releases/download/{tag}/{archive.name}',
                'Update feed must reference this exact public release ZIP.')
        try:
            signature = base64.b64decode(asset.get('signature', ''), validate=True)
        except (ValueError, TypeError):
            signature = b''
        require(len(signature) == 64, 'Update feed signature is invalid.')
    return data


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


def record(tag, directory, target, signing_mode):
    require(signing_mode in SIGNING_MODES, 'Unknown CI signing mode.')
    require(target in TARGETS, 'Unknown CI target.')
    require(target != 'linux-amd64' or signing_mode == 'update-only', 'Linux uses update-only signing.')
    require(run_identity()['job'] == BUILD_JOBS[target], 'CI build job does not match the target.')
    selected, repo, _ = identity(tag)
    paths = assets(directory, selected['version'], (target,))
    validate_feed(directory, repo, tag, selected['version'], (target,))
    goos, goarch = target.split('-')
    data = {'schema': 1, **selected, 'channel': 'release', 'os': goos, 'arch': goarch,
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


def verify_record(tag, directory, target, signing_mode, build_attempt):
    require(signing_mode in SIGNING_MODES, 'Unknown CI signing mode.')
    require(target in TARGETS, 'Unknown CI target.')
    require(target != 'linux-amd64' or signing_mode == 'update-only', 'Linux uses update-only signing.')
    selected, repo, prefix = identity(tag)
    paths = assets(directory, selected['version'], (target,))
    record_path = Path(directory) / RECORD
    require(record_path.is_file() and not record_path.is_symlink() and record_path.stat().st_size <= 1024 * 1024,
            'Missing or invalid CI build record.')
    data = json.loads(record_path.read_text())
    require(isinstance(data, dict) and type(data.get('schema')) is int
            and isinstance(data.get('source'), dict) and type(data['source'].get('run_attempt')) is int
            and isinstance(data.get('files'), list)
            and all(isinstance(item, dict) and type(item.get('size')) is int for item in data['files']),
            'Invalid CI build record schema or field types.')
    current = run_identity()
    require(type(build_attempt) is int and 0 < build_attempt <= current['run_attempt'], 'Invalid build attempt.')
    goos, goarch = target.split('-')
    expected = {'schema': 1, **selected, 'channel': 'release', 'os': goos, 'arch': goarch,
                'signing_mode': signing_mode,
                'source': {'run_id': current['run_id'], 'run_attempt': build_attempt, 'job': BUILD_JOBS[target]},
                'files': file_records(paths)}
    require(data == expected, 'CI build record identity, source attempt, exact asset set or content hashes differ.')
    validate_feed(directory, repo, tag, selected['version'], (target,))
    return selected, repo, prefix, paths


def aggregate(tag, input_directory, directory, signing_mode, build_attempts):
    """Collect both exact, verified build outputs before creating the public feed."""
    inputs = Path(input_directory)
    require(inputs.is_dir() and not inputs.is_symlink() and {path.name for path in inputs.iterdir()} == set(TARGETS),
            'Build inputs have missing or unexpected platforms.')
    require(set(build_attempts) == set(TARGETS), 'Build attempts must identify every required platform.')
    selected, repo, prefix = identity(tag)
    verified = []
    feeds = []
    for target in TARGETS:
        mode = signing_mode if target.startswith('darwin-') else 'update-only'
        _, _, _, paths = verify_record(tag, inputs / target, target, mode, build_attempts[target])
        verified.append(paths[0])
        feeds.append(validate_feed(inputs / target, repo, tag, selected['version'], (target,)))
    output = Path(directory)
    require(not output.is_symlink(), 'Release output must not be a symlink.')
    output.mkdir(parents=True, exist_ok=True)
    require(not any(output.iterdir()), 'Release aggregation output must be empty.')
    feed = {'schema': 1, 'version': selected['version'],
            'date': max(datetime.datetime.fromisoformat(item['date'].replace('Z', '+00:00'))
                        for item in feeds).astimezone(datetime.timezone.utc).isoformat().replace('+00:00', 'Z'),
            'notes': release.release_notes(repo, prefix, selected['version'], selected['commit'], tag),
            'platforms': {target: item['platforms'][target] for target, item in zip(TARGETS, feeds)}}
    for source in verified:
        shutil.copyfile(source, output / source.name)
    (output / FEED).write_text(json.dumps(feed, indent=2) + '\n')
    paths = assets(output, selected['version'])
    validate_feed(output, repo, tag, selected['version'])
    verify_archives(output, selected['version'])
    return selected, repo, prefix, paths


def verify_archives(directory, version):
    """Recheck each final ZIP's signature, identity and native binary headers."""
    for target in TARGETS:
        goos, goarch = target.split('-')
        release.run(os.environ.get('GO', 'go'), 'run', './cmd/verify-update',
                    '--feed', str(Path(directory) / FEED),
                    '--archive', str(Path(directory) / expected_names(version, (target,))[0]),
                    '--version', version, '--os', goos, '--arch', goarch)


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


def publish(tag, input_directory, directory, signing_mode, build_attempts, notes_file=None):
    selected, repo, prefix, paths = aggregate(tag, input_directory, directory, signing_mode, build_attempts)
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
    print(f'Public release, both platform ZIPs and unified feed verified: {public["url"]}', flush=True)


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
            command.add_argument('--signing-mode', choices=SIGNING_MODES, required=True)
        if name == 'record':
            command.add_argument('--target', choices=TARGETS, required=True)
        if name == 'publish':
            command.add_argument('--input-directory', type=Path, default=Path('build/downloads'))
            command.add_argument('--macos-build-attempt', type=int, required=True)
            command.add_argument('--linux-build-attempt', type=int, required=True)
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
            print(json.dumps(record(args.tag, args.directory, args.target, args.signing_mode)))
        else:
            publish(args.tag, args.input_directory, args.directory, args.signing_mode,
                    {'darwin-arm64': args.macos_build_attempt, 'linux-amd64': args.linux_build_attempt}, args.notes_file)
        return 0
    except (release.ReleaseError, OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        print(f'CI release: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
