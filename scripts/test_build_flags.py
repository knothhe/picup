"""Development and formal binaries must select the same namespace everywhere."""
import os
import unittest
from unittest import mock

from build_flags import build_environment, go_flag_words, BuildFlagsError
import build_binaries


class BuildFlagsTests(unittest.TestCase):
    def test_macos_sets_explicit_cgo_for_native_keychain_and_rejects_disabled_build(self):
        for host, target in [('darwin', ''), ('linux', 'darwin')]:
            with self.subTest(host=host, target=target):
                with mock.patch('build_flags.sys.platform', host):
                    with mock.patch.dict(os.environ, {'GOOS': target}, clear=True):
                        self.assertEqual(build_environment()['CGO_ENABLED'], '1')
                    with mock.patch.dict(os.environ, {'GOOS': target, 'CGO_ENABLED': '0'}, clear=True):
                        with self.assertRaisesRegex(BuildFlagsError, 'CGO_ENABLED=1.*Keychain'):
                            build_environment()

    def test_other_targets_keep_cgo_selection(self):
        with mock.patch('build_flags.sys.platform', 'linux'):
            with mock.patch.dict(os.environ, {'GOOS': 'linux', 'CGO_ENABLED': '0'}, clear=True):
                self.assertEqual(build_environment()['CGO_ENABLED'], '0')

    def test_migrates_only_legacy_public_linker_symbols(self):
        raw = "'-ldflags=-X github.com/knothhe/picup/internal/cloudflare.ClientID=public-id -X github.com/knothhe/picup/internal/cloudflare.RedirectURL=http://127.0.0.1:1234/callback -X other.ClientID=keep'"
        with mock.patch.dict(os.environ, {'GOFLAGS': raw}):
            environment = build_environment()
        self.assertEqual(environment['GOFLAGS'], raw.replace('picup/internal/cloudflare.', 'picup/internal/r2oauth.'))
        self.assertIn('-X other.ClientID=keep', environment['GOFLAGS'])

    def test_preserves_quoted_flags_and_merges_existing_tags_without_shell_parsing(self):
        raw = "'-ldflags=-X main.label=word with spaces' '-tags=custom more' -mod=readonly"
        with mock.patch.dict(os.environ, {'GOFLAGS': raw, 'MYGO_UPDATER_PRIVATE_KEY': 'fixture-key'}):
            environment = build_environment('picup_dev,more')
        self.assertEqual(environment['GOFLAGS'], raw + ' -tags=custom,more,picup_dev')
        self.assertEqual(go_flag_words(environment['GOFLAGS'])[-1], '-tags=custom,more,picup_dev')
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', environment)

    def test_formal_routes_reject_explicit_dev_and_clear_implicit_user_defaults(self):
        with mock.patch.dict(os.environ, {'GOFLAGS': '-tags=other,picup_dev'}):
            with self.assertRaises(BuildFlagsError):
                build_environment('', formal=True)
        with mock.patch.dict(os.environ):
            os.environ.pop('GOFLAGS', None)
            self.assertEqual(build_environment('', formal=True)['GOFLAGS'], '')

    def test_bare_build_tags_both_programs_and_packaging_overrides_development_default(self):
        signing = mock.patch.object(build_binaries, 'development_signing', return_value=None)
        signing.start()
        self.addCleanup(signing.stop)
        with mock.patch.dict(os.environ, {'BUILD_TAGS': 'picup_dev', 'GO': 'go', 'GOFLAGS': '-tags=extra'}):
            with mock.patch.object(build_binaries.subprocess, 'run') as execute:
                with mock.patch.object(build_binaries.Path, 'mkdir'):
                    build_binaries.build()
            compiled = [call for call in execute.call_args_list if 'build' in call.args[0]]
            self.assertEqual(len(compiled), 2)
            self.assertTrue(all(call.kwargs['env']['GOFLAGS'].endswith('-tags=extra,picup_dev') for call in compiled))
            if build_binaries.sys.platform == 'darwin':
                self.assertTrue(all(call.kwargs['env']['CGO_ENABLED'] == '1' for call in compiled))
            with mock.patch.object(build_binaries.subprocess, 'run') as execute:
                with mock.patch.object(build_binaries.Path, 'mkdir'):
                    build_binaries.build(package=True, skip_dmg=True)
            self.assertEqual(execute.call_args_list[-1].args[0], ['go', 'tool', 'mygo', 'build', '-skip-dmg'])
            self.assertTrue(all('picup_dev' not in call.kwargs['env']['GOFLAGS'] for call in execute.call_args_list))
            if build_binaries.sys.platform == 'darwin':
                self.assertTrue(all(call.kwargs['env']['CGO_ENABLED'] == '1' for call in execute.call_args_list))


if __name__ == '__main__':
    unittest.main()
