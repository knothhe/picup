"""Exercise the MyGo adaptation in disposable Git repositories, without network access."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name('release.py')


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git('init', '-b', 'main')
        self.git('config', 'user.email', 'test@example.com')
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'commit.gpgsign', 'false')
        self.git('config', 'tag.gpgsign', 'false')
        shutil.copyfile(SCRIPT, self.root / 'release.py')
        self.manifest = {'name': 'Picup', 'version': '0.1.0', 'resources': ['build/bin/picup'],
                         'macos': {'minimumSystemVersion': '13.0'}}
        (self.root / 'mygo.json').write_text(json.dumps(self.manifest, indent=2) + '\n')
        self.config = {'version_file': 'mygo.json', 'branch': 'main', 'remote': 'origin',
                       'tag_prefix': 'v', 'checks': [], 'github_release': False}
        self.save_config()
        self.git('add', 'release.py', 'mygo.json', '.release.json')
        self.git('commit', '-m', 'Initial project')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True, stderr=subprocess.STDOUT).strip()

    def save_config(self):
        (self.root / '.release.json').write_text(json.dumps(self.config) + '\n')

    def release(self, *args, success=True):
        result = subprocess.run([sys.executable, 'release.py', *args], cwd=self.root,
                                text=True, capture_output=True)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result

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


if __name__ == '__main__':
    unittest.main()
