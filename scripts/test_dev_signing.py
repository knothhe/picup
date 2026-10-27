"""Exercise local certificate choice and persistence without accessing a keychain."""
import io
import json
from contextlib import ExitStack
from dataclasses import replace
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import dev_signing
import macos_signing


FIRST = macos_signing.Signing('A' * 40, 'Apple Development: Local Developer (TEAMID)',
                            certificate_sha256='a' * 64, not_before='2026-01-01T00:00:00Z',
                            not_after='2030-01-01T00:00:00Z')
SECOND = macos_signing.Signing('B' * 40, 'Developer ID Application: Local Developer (TEAMID)',
                             certificate_sha256='b' * 64, not_before='2026-02-01T00:00:00Z',
                             not_after='2031-02-01T00:00:00Z')


class DevSigningTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.cache = self.root / dev_signing.CACHE_FILENAME
        contexts = ExitStack()
        self.addCleanup(contexts.close)
        contexts.enter_context(mock.patch.object(dev_signing.sys, 'platform', 'darwin'))
        contexts.enter_context(mock.patch.object(dev_signing.sys, 'stderr', io.StringIO()))
        self.terminal = contexts.enter_context(mock.patch.object(dev_signing.sys, 'stdin'))
        self.terminal.isatty.return_value = True
        self.discover = contexts.enter_context(mock.patch.object(
            macos_signing, 'discover_identities', return_value=[FIRST, SECOND]))

    def write_cache(self, signing=FIRST, version=2):
        data = {'version': version, 'identity_hash': signing.identity_hash, 'name': signing.name}
        if version == 2:
            data['certificate_sha256'] = signing.certificate_sha256
        self.cache.write_text(json.dumps(data))

    def test_first_selection_shows_fingerprints_dates_and_caches_only_public_fields(self):
        with mock.patch('builtins.input', return_value='2'):
            chosen = dev_signing.configuration(self.root, {})
        self.assertEqual(chosen, SECOND)
        self.assertEqual(json.loads(self.cache.read_text()), {
            'version': 2, 'identity_hash': SECOND.identity_hash, 'name': SECOND.name,
            'certificate_sha256': SECOND.certificate_sha256})
        self.assertEqual(self.cache.stat().st_mode & 0o777, 0o600)
        output = dev_signing.sys.stderr.getvalue()
        self.assertLess(output.index(FIRST.name), output.index(SECOND.name))
        self.assertIn('SHA-1: ' + SECOND.identity_hash, output)
        self.assertIn('SHA-256: ' + SECOND.certificate_sha256, output)
        self.assertIn('Valid from: ' + SECOND.not_before, output)
        self.assertIn('Valid until: ' + SECOND.not_after, output)
        self.assertEqual(output.count('2) '), 1)

    def test_one_certificate_still_requires_explicit_selection(self):
        self.discover.return_value = [FIRST]
        with mock.patch('builtins.input', side_effect=['', '2', '1']) as prompt:
            self.assertEqual(dev_signing.configuration(self.root, {}), FIRST)
        self.assertEqual(prompt.call_count, 3)

    def test_saved_fingerprint_is_reused_without_a_terminal(self):
        self.write_cache()
        self.terminal.isatty.return_value = False
        with mock.patch('builtins.input') as prompt:
            self.assertEqual(dev_signing.configuration(self.root, {}), FIRST)
        prompt.assert_not_called()

    def test_missing_selected_certificate_never_falls_back_to_another(self):
        self.write_cache()
        self.discover.return_value = [SECOND]
        original = self.cache.read_bytes()
        with self.assertRaisesRegex(macos_signing.SigningError, 'make dev-signing'):
            dev_signing.configuration(self.root, {})
        self.assertEqual(self.cache.read_bytes(), original)

    def test_invalid_cache_requires_reselection_and_preserves_original(self):
        missing_pin = json.dumps({'version': 2, 'identity_hash': FIRST.identity_hash, 'name': FIRST.name})
        for source in ('broken json', '[]', '{}', '{"version":3}', missing_pin,
                       '{"version":1,"identity_hash":42,"name":"Local"}',
                       json.dumps({'version': True, 'identity_hash': FIRST.identity_hash, 'name': FIRST.name})):
            with self.subTest(source=source):
                self.cache.write_text(source)
                with self.assertRaisesRegex(macos_signing.SigningError, 'make dev-signing'):
                    dev_signing.configuration(self.root, {})
                self.assertEqual(self.cache.read_text(), source)

    def test_reselection_replaces_corrupt_cache_after_a_choice(self):
        self.cache.write_text('broken json')
        with mock.patch('builtins.input', return_value='1'):
            self.assertEqual(dev_signing.configuration(self.root, {}, reselect=True), FIRST)
        self.assertEqual(json.loads(self.cache.read_text())['identity_hash'], FIRST.identity_hash)

    def test_cancel_eof_and_interrupt_leave_previous_selection_unchanged(self):
        self.write_cache()
        original = self.cache.read_bytes()
        for result in ('q', EOFError(), KeyboardInterrupt()):
            with self.subTest(result=result):
                options = {'side_effect': result} if isinstance(result, BaseException) else {'return_value': result}
                with mock.patch('builtins.input', **options):
                    with self.assertRaisesRegex(macos_signing.SigningError, 'cancelled'):
                        dev_signing.configuration(self.root, {}, reselect=True)
                self.assertEqual(self.cache.read_bytes(), original)

    def test_first_selection_without_terminal_has_actionable_error(self):
        self.terminal.isatty.return_value = False
        with self.assertRaisesRegex(macos_signing.SigningError, 'make dev-signing in a terminal'):
            dev_signing.configuration(self.root, {})
        self.assertFalse(self.cache.exists())

    def test_no_certificates_does_not_silently_use_ad_hoc_signing(self):
        self.discover.return_value = []
        with self.assertRaisesRegex(macos_signing.SigningError, 'No valid macOS code-signing identity'):
            dev_signing.configuration(self.root, {})
        self.assertFalse(self.cache.exists())

    def test_ci_ignores_local_cache_and_never_discovers_certificates(self):
        self.cache.write_text('invalid local cache')
        for ci in ('1', 'true', 'yes', 'github'):
            with self.subTest(ci=ci):
                self.assertIsNone(dev_signing.configuration(self.root, {'CI': ci}))
        self.discover.assert_not_called()

    def test_non_macos_does_not_discover_or_read_local_cache(self):
        self.cache.write_text('invalid local cache')
        with mock.patch.object(dev_signing.sys, 'platform', 'linux'):
            self.assertIsNone(dev_signing.configuration(self.root, {}))
        self.discover.assert_not_called()

    def test_macos_cross_build_does_not_discover_or_read_local_cache(self):
        self.cache.write_text('invalid local cache')
        self.assertIsNone(dev_signing.configuration(self.root, {'GOOS': 'linux'}))
        self.discover.assert_not_called()

    def test_explicit_identity_takes_precedence_without_overwriting_cached_choice(self):
        self.write_cache()
        original = self.cache.read_bytes()
        self.assertEqual(dev_signing.configuration(self.root, {
            'PICUP_MACOS_SIGNING_IDENTITY': SECOND.identity_hash}), SECOND)
        self.assertEqual(self.cache.read_bytes(), original)

    def test_explicit_identity_can_be_saved_by_reselect_command_without_a_terminal(self):
        self.terminal.isatty.return_value = False
        self.assertEqual(dev_signing.configuration(self.root, {
            'PICUP_MACOS_SIGNING_IDENTITY': SECOND.identity_hash}, reselect=True), SECOND)
        self.assertEqual(json.loads(self.cache.read_text())['identity_hash'], SECOND.identity_hash)

    def test_explicit_signing_configuration_is_preserved_in_ci(self):
        self.assertEqual(dev_signing.configuration(self.root, {
            'CI': '1', 'PICUP_MACOS_SIGNING_IDENTITY': FIRST.identity_hash}), FIRST)
        self.assertFalse(self.cache.exists())

    def test_notary_and_pin_without_explicit_identity_do_not_inherit_cache(self):
        self.write_cache()
        for variable in dev_signing.SIGNING_VARIABLES[1:]:
            with self.subTest(variable=variable):
                with self.assertRaisesRegex(macos_signing.SigningError, 'requires PICUP_MACOS_SIGNING_IDENTITY'):
                    dev_signing.configuration(self.root, {variable: 'configured'})
        self.discover.assert_not_called()

    def test_legacy_cache_upgrades_only_the_same_identity_without_prompting(self):
        self.write_cache(version=1)
        self.terminal.isatty.return_value = False
        with mock.patch('builtins.input') as prompt:
            self.assertEqual(dev_signing.configuration(self.root, {}), FIRST)
        prompt.assert_not_called()
        self.assertEqual(json.loads(self.cache.read_text()), {
            'version': 2, 'identity_hash': FIRST.identity_hash, 'name': FIRST.name,
            'certificate_sha256': FIRST.certificate_sha256})

    def test_legacy_cache_is_preserved_when_its_identity_is_unavailable(self):
        self.write_cache(version=1)
        self.discover.return_value = [SECOND]
        original = self.cache.read_bytes()
        with self.assertRaisesRegex(macos_signing.SigningError, 'make dev-signing'):
            dev_signing.configuration(self.root, {})
        self.assertEqual(self.cache.read_bytes(), original)

    def test_same_sha1_with_different_sha256_does_not_replace_the_saved_identity(self):
        self.write_cache()
        self.discover.return_value = [replace(FIRST, certificate_sha256='c' * 64), SECOND]
        original = self.cache.read_bytes()
        with self.assertRaisesRegex(macos_signing.SigningError, 'SHA-256 fingerprint'):
            dev_signing.configuration(self.root, {})
        self.assertEqual(self.cache.read_bytes(), original)

    def test_malformed_sha256_requires_reselection_and_preserves_original(self):
        for pin in ('', 'short', 'g' * 64, 42, None):
            with self.subTest(pin=pin):
                self.cache.write_text(json.dumps({
                    'version': 2, 'identity_hash': FIRST.identity_hash,
                    'name': FIRST.name, 'certificate_sha256': pin}))
                original = self.cache.read_bytes()
                with self.assertRaisesRegex(macos_signing.SigningError, 'make dev-signing'):
                    dev_signing.configuration(self.root, {})
                self.assertEqual(self.cache.read_bytes(), original)

    def test_failed_legacy_upgrade_preserves_old_cache_and_cleans_temporary_file(self):
        self.write_cache(version=1)
        original = self.cache.read_bytes()
        with mock.patch.object(dev_signing.os, 'replace', side_effect=OSError('fixture failure')):
            with self.assertRaises(OSError):
                dev_signing.configuration(self.root, {})
        self.assertEqual(self.cache.read_bytes(), original)
        self.assertEqual(list(self.root.iterdir()), [self.cache])

    def test_failed_atomic_replace_preserves_old_cache_and_removes_temporary_file(self):
        self.write_cache()
        original = self.cache.read_bytes()
        with mock.patch('builtins.input', return_value='2'), mock.patch.object(
                dev_signing.os, 'replace', side_effect=OSError('fixture failure')):
            with self.assertRaises(OSError):
                dev_signing.configuration(self.root, {}, reselect=True)
        self.assertEqual(self.cache.read_bytes(), original)
        self.assertEqual(list(self.root.iterdir()), [self.cache])


if __name__ == '__main__':
    unittest.main()
