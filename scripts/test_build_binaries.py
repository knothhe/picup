"""Check that every local Dev entry point uses its selected signing identity."""
from contextlib import ExitStack
import os
from pathlib import Path
import subprocess
import unittest
from unittest import mock

import build_binaries
from macos_signing import Signing, SigningError


class DevBuildTests(unittest.TestCase):
    def setUp(self):
        contexts = ExitStack()
        self.addCleanup(contexts.close)
        contexts.enter_context(mock.patch.dict(os.environ, {
            'BUILD_TAGS': 'picup_dev', 'GO': 'go'}, clear=True))
        contexts.enter_context(mock.patch.object(build_binaries.sys, 'platform', 'darwin'))
        contexts.enter_context(mock.patch.object(build_binaries.Path, 'mkdir'))
        contexts.enter_context(mock.patch.object(build_binaries, 'verify_development_icon'))
        self.signing = Signing('A' * 40, 'Local Code Signing')
        self.select = contexts.enter_context(mock.patch.object(
            build_binaries, 'development_signing', return_value=self.signing))
        self.release_select = contexts.enter_context(mock.patch.object(
            build_binaries, 'configuration', return_value=None))
        self.execute = contexts.enter_context(mock.patch.object(
            build_binaries.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)))
        contexts.enter_context(mock.patch.object(
            build_binaries.subprocess, 'check_output', return_value='darwin\narm64\n'))
        self.sign = contexts.enter_context(mock.patch.object(build_binaries, 'sign_development_binaries'))
        self.package = contexts.enter_context(mock.patch.object(build_binaries, 'packaging_configuration'))
        self.package.return_value.__enter__.return_value = Path.cwd()
        self.verify = contexts.enter_context(mock.patch.object(build_binaries, 'verify_application'))

    def test_bare_dev_build_selects_before_compilation_and_signs_both_outputs(self):
        events = []
        self.select.side_effect = lambda *args: events.append('select') or self.signing
        self.execute.side_effect = lambda *args, **kwargs: events.append('compile')
        self.sign.side_effect = lambda *args: events.append('sign')
        self.assertEqual(build_binaries.build(), Path('build/dev/bin/picup-gui'))
        self.assertEqual(events, ['select', 'compile', 'compile', 'compile', 'sign'])
        self.sign.assert_called_once_with(Path('build/dev'), self.signing)
        self.release_select.assert_not_called()

    def test_invalid_certificate_stops_before_compiling_or_replacing_outputs(self):
        self.select.side_effect = SigningError('selected certificate is unavailable')
        with self.assertRaisesRegex(SigningError, 'unavailable'):
            build_binaries.build()
        self.execute.assert_not_called()
        self.sign.assert_not_called()

    def test_empty_target_os_uses_the_native_macos_identity(self):
        with mock.patch.dict(os.environ, {'GOOS': ''}):
            build_binaries.build()
        self.sign.assert_called_once_with(Path('build/dev'), self.signing)

    def test_dev_package_uses_the_same_identity_for_binaries_and_bundle(self):
        build_binaries.build(package=True, skip_dmg=True, development=True)
        self.select.assert_called_once()
        self.sign.assert_called_once_with(Path('build/dev'), self.signing)
        self.package.assert_called_once_with(Path.cwd(), True, self.signing)
        self.verify.assert_called_once_with(
            Path('build/dev/dist/darwin-arm64/Picup Dev.app'), self.signing, 'Picup Dev', 'picup-dev')
        self.assertEqual(self.execute.call_args_list[-1].args[0],
                         ['go', 'tool', 'mygo', 'build', '-skip-dmg', '-debug'])
        self.release_select.assert_not_called()

    def test_run_signs_before_launching_the_exact_gui(self):
        events = []
        self.sign.side_effect = lambda *args: events.append('sign')
        self.execute.side_effect = lambda *args, **kwargs: (
            events.append('run' if len(args[0]) == 1 else 'compile') or
            subprocess.CompletedProcess(args[0], 0))
        with mock.patch.object(build_binaries.sys, 'argv', ['build_binaries.py', '--run']):
            self.assertEqual(build_binaries.main(), 0)
        self.assertEqual(events[-2:], ['sign', 'run'])
        self.assertEqual(self.execute.call_args.args[0],
                         [str(Path('build/dev/bin/picup-gui').resolve())])

    def test_signing_failure_never_packages_an_unsigned_replacement(self):
        self.sign.side_effect = SigningError('codesign failed')
        with self.assertRaisesRegex(SigningError, 'codesign failed'):
            build_binaries.build(package=True, development=True)
        self.package.assert_not_called()

    def test_formal_package_does_not_use_the_local_dev_identity(self):
        build_binaries.build(package=True, skip_dmg=True)
        self.select.assert_not_called()
        self.sign.assert_not_called()
        self.release_select.assert_called_once()
        self.package.assert_called_once_with(Path.cwd(), False, None)

    def test_explicit_ci_unsigned_dev_build_does_not_sign_outputs(self):
        self.select.return_value = None
        with mock.patch.dict(os.environ, {'CI': '1'}):
            build_binaries.build()
        self.assertEqual(self.select.call_args.args[1]['CI'], '1')
        self.sign.assert_not_called()


if __name__ == '__main__':
    unittest.main()
