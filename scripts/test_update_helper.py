import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
HELPER = ROOT / 'patches/mygo-v0.7.3/install-after-exit.sh'


class UpdateHelperTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="picup-helper-'")
        self.root = Path(self.temporary.name).resolve() / "space ' dollar$ and\nnewline"
        self.root.mkdir()
        self.target = self.root / 'Picup.app'
        self.work = self.root / '.Picup.app.update-fixture'
        self.new = self.work / 'app/Picup.app'
        self.old = self.work / 'old'
        self.target.mkdir()
        (self.target / 'marker').write_text('old application')
        self.new.mkdir(parents=True)
        (self.new / 'marker').write_text('new application')
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.open_log = self.root / 'opened.json'
        self.move_log = self.root / 'moves.jsonl'
        shim = '''import json, os, pathlib, signal, subprocess, sys
arguments = sys.argv[1:]
paths = arguments[1:] if arguments[:1] == ['-n'] else arguments
source, destination = paths
with open(os.environ['PICUP_TEST_MOVE_LOG'], 'a') as log:
    log.write(json.dumps(arguments) + '\\n')
case = os.environ['PICUP_TEST_CASE']
target, candidate, backup = (os.environ[key] for key in ['PICUP_TEST_TARGET', 'PICUP_TEST_NEW', 'PICUP_TEST_OLD'])
if source == target and case == 'first-move-fails':
    sys.exit(1)
if source == candidate and case in ['new-fails', 'double-failure', 'target-appears', 'restore-target-appears-inside-move']:
    if case == 'target-appears':
        pathlib.Path(target).mkdir()
        pathlib.Path(target, 'marker').write_text('unrelated new target')
    sys.exit(1)
if source == backup and case == 'double-failure':
    sys.exit(1)
if source == candidate and case == 'target-appears-inside-move':
    pathlib.Path(target).mkdir()
    pathlib.Path(target, 'marker').write_text('unrelated new target')
if source == target and case == 'backup-appears-inside-move':
    pathlib.Path(backup).mkdir()
    pathlib.Path(backup, 'marker').write_text('unrelated backup path')
if source == backup and case == 'restore-target-appears-inside-move':
    pathlib.Path(target).mkdir()
    pathlib.Path(target, 'marker').write_text('unrelated new target')
result = subprocess.run(['/bin/mv', *arguments]).returncode
if source == target and case == 'interrupt-after-old' and result == 0:
    os.kill(os.getppid(), signal.SIGTERM)
sys.exit(result)
'''
        self.write_program(self.bin / 'mv', shim)
        self.write_program(self.bin / 'open', '''import json, os, pathlib, sys
pathlib.Path(os.environ['PICUP_TEST_OPEN_LOG']).write_text(json.dumps(sys.argv[1:]))
''')

    def tearDown(self):
        self.temporary.cleanup()

    def write_program(self, filename, body):
        filename.write_text('#!' + sys.executable + '\n' + body)
        filename.chmod(0o755)

    def run_helper(self, case='success', relaunch=False):
        if relaunch:
            (self.work / 'relaunch').touch()
        environment = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ.get('PATH', ''),
                           PICUP_TEST_CASE=case, PICUP_TEST_TARGET=str(self.target), PICUP_TEST_NEW=str(self.new),
                           PICUP_TEST_OLD=str(self.old), PICUP_TEST_OPEN_LOG=str(self.open_log),
                           PICUP_TEST_MOVE_LOG=str(self.move_log))
        # Rewrite the absolute system opener only in this fixture, so no real
        # application or LaunchServices registration can be touched by tests.
        script = HELPER.read_text().replace('/usr/bin/open', shlex.quote(str(self.bin / 'open')))
        return subprocess.run(['/bin/sh', '-c', script, 'mygo-update', '99999999',
                               str(self.target), str(self.new), str(self.work),
                               str(self.target), '--args', "quoted ' argument", 'line\nbreak'],
                              env=environment, text=True, capture_output=True, timeout=10)

    def moves(self):
        return [json.loads(line) for line in self.move_log.read_text().splitlines()]

    def test_double_failure_retains_the_only_old_backup_and_candidate(self):
        result = self.run_helper('double-failure', relaunch=True)
        self.assertTrue((self.old / 'marker').exists(), 'double move failure deleted the only old application backup')
        self.assertEqual((self.old / 'marker').read_text(), 'old application')
        self.assertEqual((self.new / 'marker').read_text(), 'new application')
        self.assertFalse(self.target.exists())
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.open_log.exists(), 'failed recovery tried to launch an absent or unknown target')
        self.assertIn(str(self.work), result.stderr)
        recovery = (self.work / 'RECOVERY.txt').read_text()
        self.assertIn(str(self.old), recovery)
        self.assertIn(str(self.target), recovery)

    def test_success_installs_and_cleans_its_work_directory(self):
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.target / 'marker').read_text(), 'new application')
        self.assertFalse(self.work.exists())
        self.assertFalse(self.open_log.exists())

    def test_success_relaunch_preserves_original_open_arguments(self):
        result = self.run_helper(relaunch=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(self.open_log.read_text()),
                         [str(self.target), '--args', "quoted ' argument", 'line\nbreak'])
        self.assertFalse(self.work.exists())

    def test_failed_new_move_restores_old_and_can_relaunch_it(self):
        result = self.run_helper('new-fails', relaunch=True)
        self.assertEqual((self.target / 'marker').read_text(), 'old application')
        self.assertFalse(self.work.exists())
        self.assertTrue(self.open_log.exists())
        self.assertIn('restored', result.stderr)

    def test_first_move_failure_preserves_both_apps_and_recovery_record(self):
        result = self.run_helper('first-move-fails', relaunch=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.target / 'marker').read_text(), 'old application')
        self.assertEqual((self.new / 'marker').read_text(), 'new application')
        self.assertTrue((self.work / 'RECOVERY.txt').is_file())
        self.assertFalse(self.open_log.exists())

    def test_existing_backup_is_never_replaced_or_cleaned(self):
        self.old.mkdir()
        (self.old / 'marker').write_text('previous recovery backup')
        result = self.run_helper(relaunch=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.old / 'marker').read_text(), 'previous recovery backup')
        self.assertEqual((self.target / 'marker').read_text(), 'old application')
        self.assertEqual((self.new / 'marker').read_text(), 'new application')
        self.assertFalse(self.move_log.exists())
        self.assertFalse(self.open_log.exists())

    def test_target_appearing_during_failure_is_not_overwritten_or_opened(self):
        result = self.run_helper('target-appears', relaunch=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.target / 'marker').read_text(), 'unrelated new target')
        self.assertEqual((self.old / 'marker').read_text(), 'old application')
        self.assertEqual((self.new / 'marker').read_text(), 'new application')
        self.assertFalse(self.open_log.exists())
        self.assertEqual(len(self.moves()), 2, 'recovery attempted a move into a newly occupied target')

    def test_termination_after_old_move_preserves_the_backup(self):
        result = self.run_helper('interrupt-after-old', relaunch=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.old / 'marker').read_text(), 'old application')
        self.assertEqual((self.new / 'marker').read_text(), 'new application')
        self.assertTrue((self.work / 'RECOVERY.txt').is_file())
        self.assertFalse(self.open_log.exists())

    def test_target_race_inside_move_cannot_be_mistaken_for_success(self):
        result = self.run_helper('target-appears-inside-move', relaunch=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.target / 'marker').read_text(), 'unrelated new target')
        self.assertEqual((self.old / 'marker').read_text(), 'old application')
        nested = self.target / self.new.name
        self.assertIn(str(nested), (self.work / 'RECOVERY.txt').read_text())
        self.assertEqual((nested / 'marker').read_text(), 'new application')
        self.assertFalse(self.open_log.exists())

    def test_backup_race_inside_move_retains_and_identifies_the_nested_old_app(self):
        result = self.run_helper('backup-appears-inside-move', relaunch=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.old / 'marker').read_text(), 'unrelated backup path')
        nested = self.old / self.target.name
        self.assertEqual((nested / 'marker').read_text(), 'old application')
        self.assertEqual((self.new / 'marker').read_text(), 'new application')
        self.assertIn('Old application backup: ' + str(nested), (self.work / 'RECOVERY.txt').read_text())
        self.assertFalse(self.open_log.exists())

    def test_restore_race_inside_move_retains_and_identifies_the_nested_old_app(self):
        result = self.run_helper('restore-target-appears-inside-move', relaunch=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.target / 'marker').read_text(), 'unrelated new target')
        nested = self.target / self.old.name
        self.assertEqual((nested / 'marker').read_text(), 'old application')
        self.assertEqual((self.new / 'marker').read_text(), 'new application')
        self.assertIn('Old application backup: ' + str(nested), (self.work / 'RECOVERY.txt').read_text())
        self.assertFalse(self.open_log.exists())

    def test_recovery_command_quotes_paths_and_can_restore_after_error_is_resolved(self):
        self.run_helper('double-failure')
        recovery = (self.work / 'RECOVERY.txt').read_text()
        command = recovery.split('Recovery command (only when the target is absent):\n', 1)[1].strip()
        result = subprocess.run(['/bin/sh', '-c', command], capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.target / 'marker').read_text(), 'old application')
        self.assertTrue((self.new / 'marker').exists(), 'manual recovery unexpectedly deleted the signed candidate')


if __name__ == '__main__':
    unittest.main()
