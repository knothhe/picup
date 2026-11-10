"""Exercise the MyGo adaptation in disposable Git repositories, without network access."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


SCRIPT = Path(__file__).with_name('release.py')
NOTES_SCRIPT = Path(__file__).with_name('release_notes.py')
SPEC = importlib.util.spec_from_file_location('picup_release', SCRIPT)
release_module = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release_module)


FAKE_PACKAGE = r'''import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument('--version', required=True)
parser.add_argument('--output-dir', required=True)
args = parser.parse_args()
version = json.loads(Path('mygo.json').read_text())['version']
event = {'tool': 'package', 'version': args.version, 'source_version': version,
         'source': Path('source.txt').read_text(), 'cwd': str(Path.cwd()),
         'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
                 if Path('.git').exists() else None,
         'tags': subprocess.check_output(['git', 'tag', '--list'], text=True).splitlines()
                 if Path('.git').exists() else []}
with open(os.environ['TEST_RELEASE_EVENTS'], 'a') as stream:
    stream.write(json.dumps(event) + '\n')
if os.environ.get('TEST_PACKAGE_FAIL'):
    raise SystemExit('Deliberate package failure')
if version != args.version:
    raise SystemExit('Package source version differs from target')
output = Path(os.environ.get('TEST_PACKAGE_OUTSIDE_DIR', args.output_dir))
output.mkdir(parents=True, exist_ok=True)
assets = []
for arch in os.environ.get('TEST_PACKAGE_ARCHES', 'arm64').split(','):
    asset = output / f'Picup-{args.version}-darwin-{arch}.zip'
    with zipfile.ZipFile(asset, 'w') as archive:
        archive.writestr(zipfile.ZipInfo('source.txt', (1980, 1, 1, 0, 0, 0)), event['source'])
        archive.writestr(zipfile.ZipInfo('version.txt', (1980, 1, 1, 0, 0, 0)), version)
    assets.append(str(asset.resolve()))
print(json.dumps(assets))
'''


FAKE_GH = r'''import json
import hashlib
import os
from pathlib import Path
import subprocess
import sys
from urllib.parse import unquote
import zipfile

args = sys.argv[1:]
state_path = Path(os.environ['TEST_GH_STATE'])
state = json.loads(state_path.read_text())
event = {'tool': 'gh', 'args': args}
if args[:2] in (['release', 'upload'], ['release', 'create']):
    event['assets'] = []
    for name in args[3:]:
        path = Path(name)
        if path.is_file():
            with zipfile.ZipFile(path) as archive:
                event['assets'].append({'name': path.name, 'size': path.stat().st_size,
                                        'digest': 'sha256:' + hashlib.sha256(path.read_bytes()).hexdigest(),
                                        'source': archive.read('source.txt').decode(),
                                        'version': archive.read('version.txt').decode()})
with open(os.environ['TEST_RELEASE_EVENTS'], 'a') as stream:
    stream.write(json.dumps(event) + '\n')
if args[:2] == ['auth', 'status']:
    raise SystemExit(0)
if args[0] == 'api' and any('/git/ref/tags/' in value for value in args):
    endpoint = next(value for value in args if '/git/ref/tags/' in value)
    tag = unquote(endpoint.split('/git/ref/tags/', 1)[1])
    sha = subprocess.check_output([os.environ['TEST_REAL_GIT'], '--git-dir',
                                   os.environ['TEST_GIT_REMOTE'], 'rev-parse',
                                   'refs/tags/' + tag], text=True).strip()
    print(json.dumps({'object': {'sha': sha, 'type': 'tag'}}))
    raise SystemExit(0)
if args[0] == 'api' and any(value.endswith('/releases') for value in args):
    if os.environ.get('TEST_GH_FAIL_LISTING'):
        raise SystemExit('Deliberate release listing failure')
    if '--jq' in args:
        print('\n'.join(state))
    else:
        print(json.dumps([{'tag_name': tag, **release} for tag, release in state.items()]))
    raise SystemExit(0)
if args[:2] == ['release', 'view']:
    tag = args[2]
    if tag not in state:
        raise SystemExit(1)
    print(json.dumps(state[tag]))
    raise SystemExit(0)
if args[:2] == ['release', 'create']:
    tag = args[2]
    if tag in state:
        raise SystemExit('Release already exists')
    if '--notes-file' in args and args[args.index('--notes-file') + 1] == '-':
        sys.stdin.read()
    state[tag] = {'url': 'https://github.com/example/picup/releases/tag/' + tag, 'isDraft': '--draft' in args,
                  'assets': [] if os.environ.get('TEST_GH_DROP_ASSETS') else event['assets']}
    state_path.write_text(json.dumps(state))
    print(state[tag]['url'])
    raise SystemExit(0)
if args[:2] == ['release', 'upload']:
    tag = args[2]
    if '--clobber' in args:
        raise SystemExit('Existing release assets must not be overwritten')
    for asset in event['assets']:
        if any(old['name'] == asset['name'] for old in state[tag]['assets']):
            raise SystemExit('Asset already exists')
        state[tag]['assets'].append(asset)
    state_path.write_text(json.dumps(state))
    raise SystemExit(0)
if args[:2] == ['release', 'edit']:
    if '--draft=false' not in args:
        raise SystemExit('Unexpected release edit')
    state[args[2]]['isDraft'] = False
    state_path.write_text(json.dumps(state))
    raise SystemExit(0)
raise SystemExit('Unexpected gh invocation: ' + repr(args))
'''


FAKE_GIT = r'''import json
import os
import sys

args = sys.argv[1:]
if args and args[0] in ('ls-remote', 'push'):
    with open(os.environ['TEST_RELEASE_EVENTS'], 'a') as stream:
        stream.write(json.dumps({'tool': 'git', 'args': args}) + '\n')
    args = [os.environ['TEST_GIT_REMOTE'] if value == 'https://github.com/example/picup.git'
            or (value == 'origin' and args[0] == 'push') else value for value in args]
os.execv(os.environ['TEST_REAL_GIT'], [os.environ['TEST_REAL_GIT'], *args])
'''


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = os.environ.copy()
        self.git('init', '-b', 'main')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'commit.gpgsign', 'false')
        self.git('config', 'tag.gpgsign', 'false')
        shutil.copyfile(SCRIPT, self.root / 'release.py')
        shutil.copyfile(NOTES_SCRIPT, self.root / 'release_notes.py')
        self.manifest = {'name': 'Picup', 'version': '0.1.0', 'resources': ['build/bin/picup'],
                         'macos': {'minimumSystemVersion': '13.0'}}
        (self.root / 'mygo.json').write_text(json.dumps(self.manifest, indent=2) + '\n')
        self.config = {'version_file': 'mygo.json', 'branch': 'main', 'remote': 'origin',
                       'tag_prefix': 'v', 'checks': [], 'github_release': False,
                       'release_assets': False}
        self.save_config()
        self.git('add', 'release.py', 'release_notes.py', 'mygo.json', '.release.json')
        self.git('commit', '-m', 'Initial project')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True, stderr=subprocess.STDOUT).strip()

    def save_config(self):
        (self.root / '.release.json').write_text(json.dumps(self.config) + '\n')

    def release(self, *args, success=True):
        result = subprocess.run([sys.executable, 'release.py', *args], cwd=self.root,
                                text=True, capture_output=True, env=self.env)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result

    def configure_assets(self):
        tools_temp = tempfile.TemporaryDirectory()
        self.addCleanup(tools_temp.cleanup)
        self.tools = Path(tools_temp.name)
        bin_path = self.tools / 'bin'
        bin_path.mkdir()
        for name, source in [('gh', FAKE_GH), ('git', FAKE_GIT)]:
            path = bin_path / name
            path.write_text(f'#!{sys.executable}\n' + source)
            path.chmod(0o755)
        self.state_path = self.tools / 'releases.json'
        self.state_path.write_text('{}')
        self.events_path = self.tools / 'events.jsonl'
        self.events_path.touch()
        self.remote = self.tools / 'remote.git'
        self.git('init', '--bare', str(self.remote))
        self.git('remote', 'add', 'origin', 'https://github.com/example/picup.git')
        self.env.update({'PATH': str(bin_path) + os.pathsep + self.env['PATH'],
                         'TEST_REAL_GIT': shutil.which('git'),
                         'TEST_GIT_REMOTE': str(self.remote),
                         'TEST_GH_STATE': str(self.state_path),
                         'TEST_RELEASE_EVENTS': str(self.events_path)})
        (self.root / 'package_release.py').write_text(FAKE_PACKAGE)
        (self.root / 'source.txt').write_text('tagged source')
        (self.root / '.gitignore').write_text('/build/\n')
        self.config.update({'github_release': True, 'release_assets': True})
        self.save_config()
        self.git('add', '.release.json', '.gitignore', 'package_release.py', 'source.txt')
        self.git('commit', '-m', 'Configure release packages')
        self.git('push', str(self.remote), 'main')

    def configure_actions(self, checks=None):
        self.configure_assets()
        self.config.update({'release_executor': 'github-actions', 'github_release': False,
                            'release_assets': False, 'checks': checks or []})
        self.save_config()
        self.git('add', '.release.json')
        self.git('commit', '-m', 'Move release builds and publication to Actions')
        self.git('push', str(self.remote), 'main')

    def notes(self, tag, success=True):
        result = subprocess.run([sys.executable, 'release_notes.py', 'example/picup', tag],
                                cwd=self.root, text=True, capture_output=True, env=self.env)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result

    def events(self, tool=None, command=None):
        events = [json.loads(line) for line in self.events_path.read_text().splitlines()]
        return [event for event in events if (tool is None or event['tool'] == tool)
                and (command is None or event.get('args', [])[:len(command)] == command)]

    def release_state(self):
        return json.loads(self.state_path.read_text())

    def test_initial_then_upgrade_and_resume(self):
        initial_head = self.git('rev-parse', 'HEAD')
        original = (self.root / 'mygo.json').read_bytes()
        plan = json.loads(self.release('initial', '--no-push').stdout)
        self.assertEqual(plan['release_type'], 'initial')
        self.assertFalse(plan['create_version_commit'])
        self.assertEqual(self.git('tag'), '')
        self.release('initial', '--no-push', '--apply')
        self.assertEqual(self.git('rev-parse', 'HEAD'), initial_head)
        self.assertEqual((self.root / 'mygo.json').read_bytes(), original)
        self.assertIn('Release-Type: initial', self.git('tag', '-n99'))
        self.release('0.1.0', '--no-push', '--resume', '--apply')
        plan = json.loads(self.release('minor', '--no-push').stdout)
        self.assertEqual(plan['files'], ['mygo.json'])
        self.assertEqual(plan['target'], '0.2.0')
        self.assertEqual((self.root / 'mygo.json').read_bytes(), original)
        self.release('minor', '--no-push', '--apply')
        expected = dict(self.manifest, version='0.2.0')
        self.assertEqual(json.loads((self.root / 'mygo.json').read_text()), expected)
        self.assertEqual(self.git('log', '-1', '--format=%s'), 'chore(release): v0.2.0')
        self.assertEqual(self.git('diff-tree', '--no-commit-id', '--name-only', '-r', 'HEAD'), 'mygo.json')
        self.assertEqual(self.git('cat-file', '-t', 'refs/tags/v0.2.0'), 'tag')
        self.release('0.2.0', '--resume', '--no-push', '--apply')

    def test_atomic_push_to_local_remote(self):
        remote = self.root / 'remote.git'
        self.git('init', '--bare', str(remote))
        # Keep the local test remote out of the release working tree.
        (self.root / '.git/info/exclude').write_text('/remote.git/\n')
        self.git('remote', 'add', 'origin', str(remote))
        self.git('push', '-u', 'origin', 'main')
        self.release('initial', '--apply')
        self.release('patch', '--apply')
        self.assertEqual(self.git('rev-parse', 'HEAD'), self.git('--git-dir', str(remote), 'rev-parse', 'main'))
        self.assertEqual(self.git('rev-parse', 'v0.1.1'), self.git('--git-dir', str(remote), 'rev-parse', 'v0.1.1'))
        self.release('initial', success=False)

    def test_check_failure_preserves_version(self):
        self.config['checks'] = [[sys.executable, '-c', 'raise SystemExit(1)']]
        self.save_config()
        self.git('add', '.release.json')
        self.git('commit', '-m', 'Configure failing check')
        result = self.release('patch', '--apply', '--no-push', success=False)
        self.assertIn('Stopped before updating the version', result.stderr)
        self.assertEqual(json.loads((self.root / 'mygo.json').read_text()), self.manifest)
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(self.git('status', '--porcelain'), '')

    def test_check_side_effect_stops_before_version_update(self):
        self.config['checks'] = [[sys.executable, '-c', "from pathlib import Path; Path('unexpected').write_text('change')"]]
        self.save_config()
        self.git('add', '.release.json')
        self.git('commit', '-m', 'Configure side effect')
        result = self.release('patch', '--apply', '--no-push', success=False)
        self.assertIn('Checks changed the working tree', result.stderr)
        self.assertTrue((self.root / 'unexpected').exists())
        self.assertEqual(json.loads((self.root / 'mygo.json').read_text()), self.manifest)
        self.assertEqual(self.git('tag'), '')

    def test_reject_equal_lower_and_dirty_versions(self):
        for target in ('0.1.0', '0.0.9', '0.1.0+build.1', '01.2.3'):
            self.release(target, '--no-push', success=False)
        (self.root / 'untracked').write_text('user work')
        self.assertIn('Working tree must be clean', self.release('patch', '--no-push', success=False).stderr)

    def test_actions_initial_and_upgrade_previews_are_read_only_and_identify_executor(self):
        self.configure_actions()
        before = {command: self.git(*command.split()) for command in
                  ('rev-parse HEAD', 'status --porcelain', 'show-ref')}
        original = (self.root / 'mygo.json').read_bytes()
        for mode, target in (('initial', '0.1.0'), ('patch', '0.1.1')):
            plan = json.loads(self.release(mode).stdout)
            self.assertEqual(plan['release_executor'], 'github-actions')
            self.assertEqual(plan['target'], target)
            self.assertEqual(plan['github_repo'], 'example/picup')
            self.assertFalse(plan['github_release'])
            self.assertIsNone(plan['package'])
            self.assertEqual(plan['package_source'], 'v' + target)
            self.assertEqual(plan['create_version_commit'], mode != 'initial')
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release']), [])
        self.assertEqual((self.root / 'mygo.json').read_bytes(), original)
        for command, expected in before.items():
            self.assertEqual(self.git(*command.split()), expected)

    def test_actions_initial_and_upgrade_only_check_version_tag_and_atomic_push(self):
        check = ('import json, os; from pathlib import Path; '
                 'event = {"tool": "check", "version": json.loads(Path("mygo.json").read_text())["version"]}; '
                 'stream = open(os.environ["TEST_RELEASE_EVENTS"], "a"); '
                 'stream.write(json.dumps(event) + "\\n"); stream.close()')
        self.configure_actions([[sys.executable, '-c', check]])
        head = self.git('rev-parse', 'HEAD')
        initial = self.release('initial', '--apply')
        self.assertEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertIn('Git phase complete', initial.stdout)
        self.assertNotIn('Release complete:', initial.stdout)
        self.release('patch', '--apply')
        self.assertEqual(self.git('log', '-1', '--format=%s'), 'chore(release): v0.1.1')
        self.assertEqual(json.loads((self.root / 'mygo.json').read_text())['version'], '0.1.1')
        self.assertEqual(len(self.events('check')), 2)
        for tag in ('v0.1.0', 'v0.1.1'):
            self.assertEqual(self.git('cat-file', '-t', 'refs/tags/' + tag), 'tag')
            self.assertEqual(self.git('rev-parse', tag),
                             self.git('--git-dir', str(self.remote), 'rev-parse', tag))
        self.assertTrue(all('--atomic' in event['args'] for event in self.events('git', ['push'])))
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('gh', ['release']), [])
        self.assertEqual(self.release_state(), {})
        self.assertFalse((self.root / 'build').exists())

    def test_actions_resume_does_not_bump_build_publish_or_dispatch(self):
        self.configure_actions()
        self.release('initial', '--apply')
        head = self.git('rev-parse', 'HEAD')
        original = (self.root / 'mygo.json').read_bytes()
        self.events_path.write_text('')
        result = self.release('0.1.0', '--resume', '--apply')
        self.assertIn('does not trigger a new workflow run', result.stdout)
        self.assertIn('no workflow was dispatched', result.stdout)
        self.assertEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertEqual((self.root / 'mygo.json').read_bytes(), original)
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('gh'), [])
        self.assertEqual(self.release_state(), {})

    def test_actions_rejects_local_publication_and_backfill_configuration(self):
        self.configure_actions()
        for field in ('github_release', 'release_assets'):
            self.config[field] = True
            self.save_config()
            result = self.release('patch', '--no-push', success=False)
            self.assertIn('github-actions requires', result.stderr)
            self.config[field] = False
        self.save_config()
        result = self.release('patch', '--github-release', success=False)
        self.assertIn('github-actions requires', result.stderr)
        result = self.release('0.1.0', '--assets-only', success=False)
        self.assertIn('workflow_dispatch with the exact existing tag', result.stderr)
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh'), [])

    def test_actions_initial_release_listing_failure_prevents_tag_and_push(self):
        self.configure_actions()
        self.env['TEST_GH_FAIL_LISTING'] = '1'
        self.release('initial', '--apply', success=False)
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release']), [])

    def test_actions_check_failure_stops_before_version_commit_tag_or_push(self):
        self.configure_actions([[sys.executable, '-c', 'raise SystemExit(1)']])
        head = self.git('rev-parse', 'HEAD')
        result = self.release('patch', '--apply', success=False)
        self.assertIn('Stopped before updating the version', result.stderr)
        self.assertEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertEqual(json.loads((self.root / 'mygo.json').read_text()), self.manifest)
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('git', ['push']), [])

    def test_standalone_notes_use_exact_tag_history_and_preserve_dirty_working_tree(self):
        self.release('initial', '--no-push', '--apply')
        (self.root / 'feature.txt').write_text('new feature')
        self.git('add', 'feature.txt')
        self.git('commit', '-m', 'feat: Add tagged feature')
        feature = self.git('rev-parse', '--short', 'HEAD')
        self.release('patch', '--no-push', '--apply')
        (self.root / 'feature.txt').write_text('later feature')
        self.git('add', 'feature.txt')
        self.git('commit', '-m', 'Unreleased work after target tag')
        (self.root / 'mygo.json').write_text(json.dumps(dict(self.manifest, version='0.9.0')))
        (self.root / 'untracked').write_text('user work')
        before = {command: self.git(*command.split()) for command in
                  ('rev-parse HEAD', 'status --porcelain', 'show-ref')}
        result = self.notes('v0.1.1')
        self.assertIn('https://github.com/example/picup/compare/v0.1.0...v0.1.1', result.stdout)
        self.assertIn(f'- feat: Add tagged feature (`{feature}`)', result.stdout)
        self.assertNotIn('chore(release)', result.stdout)
        self.assertNotIn('Unreleased work', result.stdout)
        for command, expected in before.items():
            self.assertEqual(self.git(*command.split()), expected)

    def test_standalone_notes_reject_tag_version_mismatch_and_shallow_history(self):
        self.git('tag', '-a', 'v0.2.0', '-m', 'Wrong version')
        self.assertIn('Tagged source version differs', self.notes('v0.2.0', success=False).stderr)
        self.release('initial', '--no-push', '--apply')
        with tempfile.TemporaryDirectory() as temporary:
            clone = Path(temporary) / 'clone'
            subprocess.run(['git', 'clone', '--depth=1', self.root.as_uri(), str(clone)],
                           check=True, text=True, capture_output=True)
            result = subprocess.run([sys.executable, 'release_notes.py', 'example/picup', 'v0.1.0'],
                                    cwd=clone, text=True, capture_output=True, env=self.env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('complete Git history and tags', result.stderr)

    def test_asset_preview_does_not_build_or_publish(self):
        self.configure_assets()
        plan = json.loads(self.release('initial').stdout)
        self.assertEqual(plan['target'], '0.1.0')
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.assertEqual(self.events('gh', ['release', 'upload']), [])
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(self.git('status', '--porcelain'), '')

    def test_initial_and_upgrade_build_before_tag_and_publish(self):
        self.configure_assets()
        initial_head = self.git('rev-parse', 'HEAD')
        self.release('initial', '--apply')
        self.assertEqual(self.git('rev-parse', 'HEAD'), initial_head)
        self.release('patch', '--apply')
        packages = self.events('package')
        self.assertEqual([event['source_version'] for event in packages], ['0.1.0', '0.1.1'])
        self.assertEqual([event['version'] for event in packages], ['0.1.0', '0.1.1'])
        self.assertEqual(packages[0]['tags'], [])
        self.assertEqual(packages[1]['tags'], ['v0.1.0'])
        self.assertEqual(packages[1]['head'], initial_head)
        self.assertEqual(self.git('log', '-1', '--format=%s'), 'chore(release): v0.1.1')
        events = self.events()
        for package in packages:
            start = events.index(package)
            push = next(index for index in range(start + 1, len(events))
                        if events[index]['tool'] == 'git' and events[index]['args'][0] == 'push')
            create = next(index for index in range(push + 1, len(events))
                          if events[index]['tool'] == 'gh' and events[index]['args'][:2] == ['release', 'create'])
            self.assertLess(start, push)
            self.assertLess(push, create)
            self.assertEqual(events[create]['assets'][0]['version'], package['version'])
        self.assertEqual(self.events('gh', ['release', 'upload']), [])
        for version in ('0.1.0', '0.1.1'):
            asset = self.release_state()['v' + version]['assets'][0]
            self.assertEqual(asset['name'], f'Picup-{version}-darwin-arm64.zip')
            self.assertEqual(asset['version'], version)
            self.assertEqual(asset['source'], 'tagged source')
        self.assertEqual(self.git('status', '--porcelain'), '')

    def test_package_failure_stops_before_commit_tag_or_push(self):
        self.configure_assets()
        head = self.git('rev-parse', 'HEAD')
        self.env['TEST_PACKAGE_FAIL'] = '1'
        self.release('patch', '--apply', success=False)
        self.assertEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(json.loads((self.root / 'mygo.json').read_text())['version'], '0.1.1')
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.assertEqual(self.release_state(), {})

    def test_package_output_outside_release_directory_is_rejected(self):
        self.configure_assets()
        head = self.git('rev-parse', 'HEAD')
        self.env['TEST_PACKAGE_OUTSIDE_DIR'] = str(self.tools)
        self.release('patch', '--apply', success=False)
        self.assertEqual(len(self.events('package')), 1)
        self.assertEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release', 'create']), [])

    def test_commit_hook_source_change_stops_before_tag_or_publish(self):
        self.configure_assets()
        head = self.git('rev-parse', 'HEAD')
        hook = self.root / '.git' / 'hooks' / 'pre-commit'
        hook.write_text(f'#!{sys.executable}\n'
                        'from pathlib import Path\n'
                        'import subprocess\n'
                        "Path('source.txt').write_text('source changed by commit hook')\n"
                        "subprocess.check_call(['git', 'add', '--', 'source.txt'])\n")
        hook.chmod(0o755)
        self.release('patch', '--apply', success=False)
        package, = self.events('package')
        self.assertEqual(package['source'], 'tagged source')
        self.assertEqual(package['version'], '0.1.1')
        self.assertNotEqual(self.git('rev-parse', 'HEAD'), head)
        self.assertEqual(self.git('rev-parse', 'HEAD^'), head)
        self.assertEqual(self.git('show', 'HEAD:source.txt'), 'source changed by commit hook')
        self.assertEqual(self.git('status', '--porcelain'), '')
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.assertEqual(self.release_state(), {})

    def test_resume_builds_tag_source_and_uploads_only_missing_assets(self):
        self.configure_assets()
        self.env['TEST_PACKAGE_ARCHES'] = 'arm64,amd64'
        self.release('initial', '--apply')
        state = self.release_state()
        state['v0.1.0']['assets'] = [asset for asset in state['v0.1.0']['assets']
                                   if asset['name'].endswith('-amd64.zip')]
        self.state_path.write_text(json.dumps(state))
        self.events_path.write_text('')
        self.release('0.1.0', '--resume', '--apply')
        package, = self.events('package')
        self.assertNotEqual(package['cwd'], str(self.root))
        self.assertEqual(package['source'], 'tagged source')
        self.assertEqual(package['version'], '0.1.0')
        upload, = self.events('gh', ['release', 'upload'])
        self.assertEqual([asset['name'] for asset in upload['assets']], ['Picup-0.1.0-darwin-arm64.zip'])
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.events_path.write_text('')
        self.release('0.1.0', '--resume', '--apply')
        self.assertEqual(len(self.events('package')), 1)
        self.assertEqual(self.events('gh', ['release', 'upload']), [])
        self.assertEqual(len(self.release_state()['v0.1.0']['assets']), 2)

    def test_missing_asset_after_create_is_detected_and_resume_repairs_it(self):
        self.configure_assets()
        self.env['TEST_GH_DROP_ASSETS'] = '1'
        self.release('initial', '--apply', success=False)
        self.assertEqual(self.git('tag'), 'v0.1.0')
        self.assertEqual(self.release_state()['v0.1.0']['assets'], [])
        self.assertTrue(self.release_state()['v0.1.0']['isDraft'])
        self.assertEqual(self.events('gh', ['release', 'edit']), [])
        self.assertEqual(self.git('rev-parse', 'v0.1.0'),
                         self.git('--git-dir', str(self.remote), 'rev-parse', 'v0.1.0'))
        del self.env['TEST_GH_DROP_ASSETS']
        self.events_path.write_text('')
        self.release('0.1.0', '--resume', '--apply')
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.assertEqual(len(self.events('gh', ['release', 'upload'])), 1)
        self.assertEqual(len(self.release_state()['v0.1.0']['assets']), 1)
        self.assertFalse(self.release_state()['v0.1.0']['isDraft'])

    def test_same_name_wrong_digest_stops_without_overwrite_or_publication(self):
        self.configure_assets()
        self.release('initial', '--apply')
        state = self.release_state()
        state['v0.1.0']['isDraft'] = True
        state['v0.1.0']['assets'][0]['digest'] = 'sha256:' + '0' * 64
        self.state_path.write_text(json.dumps(state))
        self.events_path.write_text('')
        result = self.release('0.1.0', '--resume', '--apply', success=False)
        self.assertIn('differs from the verified local file', result.stderr)
        self.assertEqual(self.events('gh', ['release', 'upload']), [])
        self.assertEqual(self.events('gh', ['release', 'edit']), [])
        self.assertTrue(self.release_state()['v0.1.0']['isDraft'])

    def test_assets_only_uses_old_tag_with_newer_dirty_working_tree(self):
        self.configure_assets()
        self.release('initial', '--apply')
        remote_head = self.git('--git-dir', str(self.remote), 'rev-parse', 'main')
        manifest = dict(self.manifest, version='0.2.0')
        (self.root / 'mygo.json').write_text(json.dumps(manifest) + '\n')
        (self.root / 'source.txt').write_text('newer committed source')
        self.git('add', 'mygo.json', 'source.txt')
        self.git('commit', '-m', 'Advance source beyond released tag')
        self.git('checkout', '-b', 'development')
        (self.root / 'source.txt').write_text('uncommitted source')
        (self.root / 'untracked').write_text('user work')
        before = {command: self.git(*command.split()) for command in
                  ('rev-parse HEAD', 'status --porcelain', 'show-ref')}
        original_version = (self.root / 'mygo.json').read_bytes()
        state = self.release_state()
        state['v0.1.0']['assets'] = []
        self.state_path.write_text(json.dumps(state))
        self.events_path.write_text('')
        self.release('0.1.0', '--assets-only', '--apply')
        package, = self.events('package')
        self.assertEqual(package['source_version'], '0.1.0')
        self.assertEqual(package['source'], 'tagged source')
        self.assertNotEqual(package['cwd'], str(self.root))
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.assertEqual(self.release_state()['v0.1.0']['assets'][0]['source'], 'tagged source')
        for command, expected in before.items():
            self.assertEqual(self.git(*command.split()), expected)
        self.assertEqual((self.root / 'mygo.json').read_bytes(), original_version)
        self.assertEqual(self.git('--git-dir', str(self.remote), 'rev-parse', 'main'), remote_head)

    def test_assets_only_previews_then_creates_missing_release_from_tag(self):
        self.configure_assets()
        self.git('tag', '-a', 'v0.1.0', '-m', 'Release v0.1.0\n\nRelease-Type: initial')
        self.git('push', str(self.remote), 'refs/tags/v0.1.0')
        before = {command: self.git(*command.split()) for command in
                  ('rev-parse HEAD', 'status --porcelain', 'show-ref')}
        plan = json.loads(self.release('0.1.0', '--assets-only').stdout)
        self.assertEqual(plan['release_type'], 'assets-only')
        self.assertEqual(plan['source_commit'], before['rev-parse HEAD'])
        self.assertFalse(plan['push'])
        self.assertEqual(self.events('package'), [])
        self.assertFalse((self.root / 'build').exists())
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.assertEqual(self.release_state(), {})
        for command, expected in before.items():
            self.assertEqual(self.git(*command.split()), expected)
        self.release('0.1.0', '--assets-only', '--apply')
        package, = self.events('package')
        self.assertNotEqual(package['cwd'], str(self.root))
        self.assertEqual(package['source_version'], '0.1.0')
        create, = self.events('gh', ['release', 'create'])
        self.assertEqual(create['args'][2], 'v0.1.0')
        self.assertEqual(create['assets'], self.release_state()['v0.1.0']['assets'])
        self.assertEqual(create['assets'][0]['name'], 'Picup-0.1.0-darwin-arm64.zip')
        self.assertEqual(create['assets'][0]['source'], 'tagged source')
        self.assertEqual(self.events('gh', ['release', 'upload']), [])
        self.assertEqual(self.events('git', ['push']), [])
        for command, expected in before.items():
            self.assertEqual(self.git(*command.split()), expected)

    def test_assets_only_rejects_different_remote_tag_object(self):
        self.configure_assets()
        self.release('initial', '--apply')
        self.git('tag', '-f', '-a', 'v0.1.0', '-m', 'A different annotation')
        self.events_path.write_text('')
        self.release('0.1.0', '--assets-only', '--apply', success=False)
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release', 'upload']), [])

    def test_release_listing_failure_never_creates_a_release(self):
        self.configure_assets()
        self.env['TEST_GH_FAIL_LISTING'] = '1'
        self.release('initial', '--apply', success=False)
        self.assertEqual(self.git('tag'), '')
        self.assertEqual(self.events('package'), [])
        self.assertEqual(self.events('git', ['push']), [])
        self.assertEqual(self.events('gh', ['release', 'create']), [])
        self.assertEqual(self.release_state(), {})


class PublicationOrderingTests(unittest.TestCase):
    def test_public_backfill_verifies_archive_before_exposing_feed(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            assets = [root / name for name in (
                'Picup-0.1.0-darwin-arm64.zip', 'picup-0.1.0-darwin-arm64.tar.gz',
                'update-darwin-arm64.json')]
            for asset in assets:
                asset.write_bytes(('fixture ' + asset.name).encode())
            commands = []
            uploaded = []

            def command(*args, **kwargs):
                commands.append(args)
                if args[:3] == ('gh', 'release', 'view'):
                    state = []
                    for path in uploaded:
                        digest = 'sha256:' + hashlib.sha256(path.read_bytes()).hexdigest()
                        if path.name.endswith('.tar.gz'):
                            digest = 'sha256:' + '0' * 64
                        state.append({'name': path.name, 'size': path.stat().st_size, 'digest': digest})
                    return subprocess.CompletedProcess(args, 0, json.dumps({
                        'url': 'https://github.com/knothhe/picup/releases/tag/v0.1.0',
                        'isDraft': False, 'assets': state,
                    }), '')
                if args[:3] == ('gh', 'release', 'upload'):
                    uploaded.extend(path for path in assets if str(path) in args)
                    return subprocess.CompletedProcess(args, 0, '', '')
                self.fail('unexpected publication command: ' + repr(args))

            with mock.patch.object(release_module, 'published_tags', return_value=['v0.1.0']):
                with mock.patch.object(release_module, 'run', side_effect=command):
                    with self.assertRaisesRegex(release_module.ReleaseError, 'differs from the verified local file'):
                        release_module.publish_github_release('knothhe/picup', 'v0.1.0', '0.1.0', 'notes', assets)
            self.assertEqual(uploaded, assets[:2])
            self.assertFalse(any(str(assets[2]) in args for args in commands))
            self.assertFalse(any(args[:3] == ('gh', 'release', 'edit') for args in commands))


if __name__ == '__main__':
    unittest.main()
