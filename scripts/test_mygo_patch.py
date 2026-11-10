import hashlib
import importlib.util
import json
import os
from contextlib import nullcontext
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
sys.dont_write_bytecode = True
import mygo_patch

GO_OUTPUT = mygo_patch._go_output


class MyGoPatchTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='picup patch tests ')
        self.root = Path(self.temporary.name)
        self.module = self.root / 'original module'
        self.module.mkdir()
        self.original = self.module / 'updater.go'
        self.source = (mygo_patch.ORIGINAL_DOWNLOAD + '\n' + mygo_patch.ORIGINAL_EXTRACT + '\nup.current, target, progress)\nprogress func(downloaded, total int64)) error {\n\tpub, err := update.ParsePublicKey(key)\n').encode() + b'package mygo\nconst installAfterExit = `' + mygo_patch.ORIGINAL_HELPER.encode() + b'`\n'
        self.original.write_bytes(self.source)
        packaging = self.module / 'cmd/mygo/updates.go'
        packaging.parent.mkdir(parents=True)
        self.packaging_source = mygo_patch.ORIGINAL_WRITE.encode()
        packaging.write_bytes(self.packaging_source)
        (self.module / 'go.mod').write_text('module github.com/egoist/mygo\n\ngo 1.27.3\n')
        self.app = self.root / 'app'
        self.app.mkdir()
        (self.app / 'go.mod').write_text('module example.org/app\n\ngo 1.27.3\n')
        self.workspace = self.root / 'go.work'
        self.workspace.write_text('go 1.27.3\n\nuse ./app\n')
        self.helper = self.root / 'helper.sh'
        self.helper.write_bytes(b'pid=$1 target=$2 new=$3 work=$4\nexit 42\n')
        self.metadata = {'Path': mygo_patch.MODULE, 'Version': mygo_patch.VERSION, 'Dir': str(self.module)}
        self.patches = [
            patch.object(mygo_patch, 'UPDATES_SHA256', hashlib.sha256(self.packaging_source).hexdigest()),
            patch.object(mygo_patch, 'UPDATER_SHA256', hashlib.sha256(self.source).hexdigest()),
            patch.object(mygo_patch, 'HELPER_PATH', self.helper),
            patch.object(mygo_patch, '_go_output', side_effect=self.go_output),
        ]
        for item in self.patches:
            item.start()
        self.calls = []

    def tearDown(self):
        for item in reversed(self.patches):
            item.stop()
        self.temporary.cleanup()

    def go_output(self, arguments, env):
        self.calls.append((arguments, dict(env)))
        if arguments == ['env', 'GOFLAGS']:
            return '-trimpath'
        if arguments == ['env', 'GOWORK']:
            return str(self.workspace)
        if arguments == ['work', 'edit', '-json', str(self.workspace)]:
            return json.dumps({'Go': '1.27.3', 'Use': [{'DiskPath': './app'}]})
        if arguments[:2] == ['work', 'edit']:
            environment = dict(os.environ, GOFLAGS='', GOWORK='off')
            return GO_OUTPUT(arguments, environment)
        if arguments == ['list', '-m', '-json', mygo_patch.MODULE]:
            return json.dumps(self.metadata)
        raise AssertionError(arguments)

    def workspace_copy(self, environment):
        workspace = Path(environment['GOWORK'])
        return workspace, workspace.parent / 'mygo/updater.go'

    def test_exact_patch_leaves_module_cache_unchanged_and_cleans_private_copy(self):
        parent = {'GOFLAGS': '-trimpath', 'ARBITRARY': 'inherited'}
        with mygo_patch.patched_environment(parent) as environment:
            workspace, patched = self.workspace_copy(environment)
            self.assertNotEqual(patched, self.original)
            self.assertIn(self.helper.read_bytes(), patched.read_bytes())
            self.assertIn(b'extractor.Extract(ctx, m.URL, archive, unpacked)', patched.read_bytes())
            self.assertIn(b'ed25519.Sign(key, sum.Sum(nil))', (patched.parent / 'cmd/mygo/updates.go').read_bytes())
            self.assertNotIn(mygo_patch.ORIGINAL_HELPER.encode(), patched.read_bytes())
            self.assertEqual(self.original.read_bytes(), self.source)
            self.assertEqual(environment['ARBITRARY'], 'inherited')
            self.assertEqual(parent, {'GOFLAGS': '-trimpath', 'ARBITRARY': 'inherited'})
            self.assertIn(str(self.app), workspace.read_text())
            self.assertEqual(environment['GOFLAGS'], '-trimpath')
        self.assertFalse(workspace.parent.exists())
        self.assertEqual(self.original.read_bytes(), self.source)

    def test_private_copy_is_cleaned_after_build_failure(self):
        with self.assertRaisesRegex(RuntimeError, 'build failed'):
            with mygo_patch.patched_environment({'GOFLAGS': ''}) as environment:
                workspace, _ = self.workspace_copy(environment)
                raise RuntimeError('build failed')
        self.assertFalse(workspace.parent.exists())

    def test_source_hash_drift_and_nonunique_helper_fail_closed(self):
        self.original.write_bytes(self.source + b'// drift\n')
        with self.assertRaisesRegex(mygo_patch.PatchError, 'pinned'):
            with mygo_patch.patched_environment({'GOFLAGS': ''}):
                self.fail('source drift accepted')
        for source in (b'package mygo\n', self.source + self.source):
            self.original.write_bytes(source)
            with patch.object(mygo_patch, 'UPDATER_SHA256', hashlib.sha256(source).hexdigest()):
                with self.assertRaisesRegex(mygo_patch.PatchError, 'uniquely'):
                    with mygo_patch.patched_environment({'GOFLAGS': ''}):
                        self.fail('helper mismatch accepted')

    def test_unsupported_version_and_replacement_are_refused(self):
        for changes in ({'Version': 'v0.7.4'}, {'Replace': {'Dir': str(self.module)}}, {'Path': 'example.org/fork'}):
            self.metadata.update(changes)
            with self.assertRaisesRegex(mygo_patch.PatchError, 'unreplaced'):
                with mygo_patch.patched_environment({'GOFLAGS': ''}):
                    self.fail('unsupported dependency accepted')
            self.metadata = {'Path': mygo_patch.MODULE, 'Version': mygo_patch.VERSION, 'Dir': str(self.module)}

    def test_empty_cache_downloads_only_selected_version(self):
        self.metadata.pop('Dir')
        original_output = self.go_output
        def output(arguments, env):
            if arguments == ['mod', 'download', '-json', mygo_patch.MODULE + '@' + mygo_patch.VERSION]:
                return json.dumps(dict(self.metadata, Dir=str(self.module)))
            return original_output(arguments, env)
        with patch.object(mygo_patch, '_go_output', side_effect=output):
            with mygo_patch.patched_environment({'GOFLAGS': ''}) as environment:
                self.assertTrue(self.workspace_copy(environment)[0].is_file())

    def test_existing_overlay_and_modfile_are_refused_before_module_lookup(self):
        for flags in ('-overlay=/some/file.json', "'-overlay=/some path/file.json'", '-trimpath -overlay /file.json', '--overlay=x', '-modfile=x', '--modfile=x'):
            with self.assertRaisesRegex(mygo_patch.PatchError, 'existing Go overlay'):
                with mygo_patch.patched_environment({'GOFLAGS': flags}):
                    self.fail('conflicting overlay accepted')
        self.assertEqual(self.calls, [])

    def test_global_flags_are_preserved_and_global_overlay_is_refused(self):
        with mygo_patch.patched_environment({}) as environment:
            self.assertIn('-trimpath', mygo_patch._split_flags(environment['GOFLAGS']))
        self.assertEqual(self.calls[0][0], ['env', 'GOFLAGS'])
        with patch.object(mygo_patch, '_go_output', return_value='-overlay=/global/overlay.json'):
            with self.assertRaisesRegex(mygo_patch.PatchError, 'existing Go overlay'):
                with mygo_patch.patched_environment({}):
                    self.fail('global overlay accepted')

    def test_explicit_empty_flags_override_global_configuration(self):
        with mygo_patch.patched_environment({'GOFLAGS': ''}):
            pass
        self.assertNotIn(['env', 'GOFLAGS'], [arguments for arguments, _ in self.calls])

    def test_go_flags_quote_spaces_and_quotes_without_shell_expansion(self):
        flags = ['-trimpath', '-tags=one,two', '-pkgdir=/path with spaces/pkg', '-ldflags=-X main.note=$value`literal`', "-o=/path's quote"]
        encoded = ' '.join(mygo_patch._quote_flag(flag) for flag in flags)
        self.assertEqual(mygo_patch._split_flags(encoded), flags)
        original_temporary = tempfile.TemporaryDirectory
        def temporary(**kwargs):
            return original_temporary(dir=self.root, **kwargs)
        with patch.object(mygo_patch.tempfile, 'TemporaryDirectory', side_effect=temporary):
            with mygo_patch.patched_environment({'GOFLAGS': encoded}) as environment:
                actual = mygo_patch._split_flags(environment['GOFLAGS'])
                self.assertEqual(actual, flags)
                self.assertIn('picup patch tests ', environment['GOWORK'])
                self.assertTrue(self.workspace_copy(environment)[0].is_file())
        with self.assertRaises(mygo_patch.PatchError):
            mygo_patch._quote_flag('both\'" quote types')
        with self.assertRaises(mygo_patch.PatchError):
            mygo_patch._split_flags("'-unterminated")

    def test_invalid_raw_string_patch_is_refused(self):
        for content in (b'', b'exit 1', b'echo `date`\n', b'echo \x00\n'):
            self.helper.write_bytes(content)
            with self.assertRaisesRegex(mygo_patch.PatchError, 'raw string'):
                with mygo_patch.patched_environment({'GOFLAGS': ''}):
                    self.fail('invalid raw string accepted')

    def test_failed_go_preflight_has_no_environment_or_argv_in_error(self):
        with patch.object(mygo_patch.subprocess, 'run', return_value=subprocess.CompletedProcess([], 7, '', 'secret diagnostics')):
            with self.assertRaises(mygo_patch.PatchError) as raised:
                GO_OUTPUT(['list', '-m', '-json', mygo_patch.MODULE], {'SECRET': 'do not print'})
            self.assertNotIn('secret', str(raised.exception))

    def test_wrapper_returns_go_exit_code_and_preserves_cwd(self):
        spec = importlib.util.spec_from_file_location('go_patched_wrapper', ROOT / 'scripts/go-patched.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        before = Path.cwd()
        with mygo_patch.patched_environment({'GOFLAGS': ''}) as environment:
            with patch.object(sys, 'argv', ['go-patched.py', 'test', './...']), patch.object(wrapper, 'patched_environment', return_value=nullcontext(environment)), patch.object(wrapper.subprocess, 'run', return_value=subprocess.CompletedProcess([], 37)) as command:
                self.assertEqual(wrapper.main(), 37)
        self.assertEqual(command.call_args.args, (['go', 'test', './...'],))
        self.assertNotIn('cwd', command.call_args.kwargs)
        self.assertEqual(Path.cwd(), before)
        workspace, _ = self.workspace_copy(command.call_args.kwargs['env'])
        self.assertFalse(workspace.exists())

    def test_workspace_relative_paths_and_other_directives_keep_their_meaning(self):
        self.workspace.write_text('go 1.27.3\n\ntoolchain go1.27.3\n\ngodebug panicnil=1\n\nuse ./app\n\nreplace example.org/local v1.0.0 => ./local\n')
        original_output = self.go_output
        def output(arguments, env):
            if arguments == ['work', 'edit', '-json', str(self.workspace)]:
                return json.dumps({'Go': '1.27.3', 'Toolchain': 'go1.27.3', 'Godebug': [{'Key': 'panicnil', 'Value': '1'}], 'Use': [{'DiskPath': './app'}], 'Replace': [{'Old': {'Path': 'example.org/local', 'Version': 'v1.0.0'}, 'New': {'Path': './local'}}]})
            return original_output(arguments, env)
        with patch.object(mygo_patch, '_go_output', side_effect=output):
            with mygo_patch.patched_environment({'GOFLAGS': ''}) as environment:
                text = self.workspace_copy(environment)[0].read_text()
                self.assertIn(str(self.root / 'local'), text)
                self.assertIn(str(self.app), text)
                self.assertIn('toolchain go1.27.3', text)
                self.assertIn('godebug panicnil=1', text)
                self.assertIn('example.org/local v1.0.0', text)

    def test_module_mode_gets_a_private_workspace_without_changing_main_module(self):
        original_output = self.go_output
        def output(arguments, env):
            if arguments == ['env', 'GOWORK']:
                return 'off'
            if arguments == ['list', '-m', '-json']:
                return json.dumps({'Dir': str(self.app), 'GoVersion': '1.27.3'})
            return original_output(arguments, env)
        before = (self.app / 'go.mod').read_bytes()
        with patch.object(mygo_patch, '_go_output', side_effect=output):
            with mygo_patch.patched_environment({'GOFLAGS': ''}) as environment:
                self.assertIn(str(self.app), self.workspace_copy(environment)[0].read_text())
        self.assertEqual((self.app / 'go.mod').read_bytes(), before)

    def test_wrapper_refuses_compiler_overrides(self):
        spec = importlib.util.spec_from_file_location('go_patched_conflict', ROOT / 'scripts/go-patched.py')
        wrapper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(wrapper)
        with patch.object(sys, 'argv', ['go-patched.py', 'build', '--overlay=/tmp/evil.json']), patch.object(wrapper.subprocess, 'run') as command:
            self.assertEqual(wrapper.main(), 1)
            command.assert_not_called()


class MyGoDMGIntegrationTests(unittest.TestCase):
    def test_final_dmg_is_signed_in_place_without_extra_update_archive(self):
        with mygo_patch.patched_environment(dict(os.environ, GOFLAGS='')) as environment:
            mirror = Path(environment['GOWORK']).parent / 'mygo'
            fixture = mirror / 'cmd/mygo/picup_dmg_test.go'
            fixture.write_bytes((ROOT / 'scripts/testdata/mygo-dmg/updates_test.go').read_bytes())
            result = subprocess.run([mygo_patch.go_command(environment), 'test', 'github.com/egoist/mygo/cmd/mygo', '-run', '^TestPicupSignedDMGContract$'],
                                    cwd=ROOT, env=environment, text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
