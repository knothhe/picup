"""Test private-material boundaries without exporting the user's keychain."""
import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import shlex
import ssl
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import macos_signing
import release_signing as setup


class ReleaseSigningTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.root = self.directory / 'repo'
        self.root.mkdir()
        self.backups = self.directory / 'backups'
        self.identity = macos_signing.Signing('A' * 40, 'Local Test Signing',
            certificate_sha256='b' * 64, not_after='2036-01-01T00:00:00+00:00')
        self.calls = []
        self.present = []
        self.fail_secret = None

    def args(self, **changes):
        values = dict(identity=self.identity.identity_hash, repo=None, keychain='/test.keychain-db',
                      p12=None, password_file=None, backup_dir=str(self.backups),
                      apply=False, replace_identity=False)
        values.update(changes)
        return argparse.Namespace(**values)

    def tool(self, arguments, **options):
        self.calls.append((arguments, options))
        if arguments[0] == 'git':
            return b'git@github.com:example/picup.git\n'
        if arguments[:3] == ['gh', 'secret', 'list']:
            return json.dumps([{'name': name} for name in self.present]).encode()
        if arguments[:3] == ['gh', 'secret', 'set']:
            if arguments[3] == self.fail_secret:
                raise setup.SetupError('gh upload failed (private diagnostics suppressed)')
            return b''
        if arguments[1] == 'export':
            Path(arguments[arguments.index('-o') + 1]).write_bytes(b'encrypted source')
            return b''
        raise AssertionError(f'Unexpected tool: {arguments[0]} {arguments[1]}')

    def package(self, source, password, signing, directory):
        self.assertEqual(source.read_bytes(), b'encrypted source')
        path = directory / 'identity.p12'
        setup.private_write(path, b'validated single-identity P12')
        return path, 'private-test-password'

    def configure(self, args):
        with mock.patch.object(setup, 'run', side_effect=self.tool), mock.patch.object(
                setup.macos_signing, 'discover_identities', return_value=[self.identity]), mock.patch.object(
                setup, 'single_identity_package', side_effect=self.package) as package, mock.patch.object(
                setup, 'validate_import') as validate, mock.patch('sys.stdout', new=io.StringIO()) as output:
            setup.configure(args, self.root)
            return output.getvalue(), package, validate

    def test_preview_does_not_export_upload_back_up_or_write_pin(self):
        output, package, validate = self.configure(self.args())
        self.assertIn('Preview only', output)
        self.assertIn(self.identity.certificate_sha256, output)
        self.assertEqual([call[0][0] for call in self.calls], ['git', 'gh'])
        package.assert_not_called()
        validate.assert_not_called()
        self.assertFalse((self.root / 'config').exists())
        self.assertFalse(self.backups.exists())

    def test_apply_uploads_only_single_p12_via_stdin_then_writes_public_pin(self):
        output, package, validate = self.configure(self.args(apply=True))
        validate.assert_called_once()
        uploads = [call for call in self.calls if call[0][:3] == ['gh', 'secret', 'set']]
        self.assertEqual([call[0][3] for call in uploads], list(reversed(setup.SECRET_NAMES)))
        self.assertEqual(uploads[0][1]['input'], b'private-test-password')
        self.assertEqual(base64.b64decode(uploads[1][1]['input']), b'validated single-identity P12')
        self.assertNotIn('private-test-password', output)
        self.assertNotIn('private-test-password', str([call[0] for call in self.calls]))
        self.assertEqual(setup.read_pin(self.root), self.identity.certificate_sha256)
        backup = next(self.backups.iterdir())
        self.assertEqual(backup.stat().st_mode & 0o777, 0o700)
        self.assertEqual((backup / 'identity.p12').stat().st_mode & 0o777, 0o600)
        self.assertEqual((backup / 'password').stat().st_mode & 0o777, 0o600)
        self.assertFalse(package.call_args[0][3].exists())

    def test_unknown_existing_secrets_or_changed_pin_require_explicit_replacement(self):
        self.present = list(setup.SECRET_NAMES)
        with self.assertRaisesRegex(setup.SetupError, '--replace-identity'):
            self.configure(self.args(apply=True))
        self.assertFalse(any(call[0][0] == '/usr/bin/security' for call in self.calls))
        setup.write_pin(self.root, 'c' * 64)
        with self.assertRaisesRegex(setup.SetupError, '--replace-identity'):
            self.configure(self.args(apply=True))
        self.assertEqual(setup.read_pin(self.root), 'c' * 64)
        self.configure(self.args(apply=True, replace_identity=True))
        self.assertEqual(setup.read_pin(self.root), self.identity.certificate_sha256)

    def test_same_pin_can_be_reuploaded_without_rotating_identity(self):
        setup.write_pin(self.root, self.identity.certificate_sha256)
        self.present = list(setup.SECRET_NAMES)
        self.configure(self.args(apply=True))
        self.assertEqual(setup.read_pin(self.root), self.identity.certificate_sha256)

    def test_partial_upload_keeps_backup_and_old_pin_and_reports_uploaded_name(self):
        setup.write_pin(self.root, self.identity.certificate_sha256)
        self.fail_secret = setup.CERTIFICATE_SECRET
        with self.assertRaisesRegex(setup.SetupError, 'Uploaded: MACOS_SIGNING_CERTIFICATE_PASSWORD') as error:
            self.configure(self.args(apply=True))
        self.assertNotIn('private-test-password', str(error.exception))
        self.assertEqual(setup.read_pin(self.root), self.identity.certificate_sha256)
        self.assertEqual(len(list(self.backups.iterdir())), 1)

    def test_failed_import_never_uploads_or_writes_pin(self):
        with mock.patch.object(setup, 'run', side_effect=self.tool), mock.patch.object(
                setup.macos_signing, 'discover_identities', return_value=[self.identity]), mock.patch.object(
                setup, 'single_identity_package', side_effect=self.package), mock.patch.object(
                setup, 'validate_import', side_effect=setup.SetupError('import failed')), mock.patch('sys.stdout', new=io.StringIO()):
            with self.assertRaisesRegex(setup.SetupError, 'import failed'):
                setup.configure(self.args(apply=True), self.root)
        self.assertFalse(any(call[0][:3] == ['gh', 'secret', 'set'] for call in self.calls))
        self.assertFalse(self.backups.exists())
        self.assertIsNone(setup.read_pin(self.root))

    def test_remote_mismatch_is_rejected_before_secret_or_keychain_access(self):
        with mock.patch.object(setup, 'run', side_effect=self.tool):
            with self.assertRaisesRegex(setup.SetupError, 'origin push'):
                setup.configure(self.args(repo='another/picup', apply=True), self.root)
        self.assertEqual(len(self.calls), 1)

    def test_private_inputs_require_regular_owned_mode_0600_files_without_links(self):
        path = self.directory / 'input'
        setup.private_write(path, b'private')
        self.assertEqual(setup.private_read(path), b'private')
        path.chmod(0o644)
        with self.assertRaisesRegex(setup.SetupError, '0600'):
            setup.private_read(path)
        path.chmod(0o600)
        link = self.directory / 'link'
        link.symlink_to(path)
        with self.assertRaises(OSError):
            setup.private_read(link)

    def test_private_backup_cannot_be_written_in_repository(self):
        path = self.directory / 'package'
        path.write_bytes(b'private')
        with self.assertRaisesRegex(setup.SetupError, 'outside'):
            setup.save_backup(self.root, path, 'password', self.identity, self.root / 'backups')

    def test_errors_suppress_private_tool_output_and_do_not_inherit_signing_material(self):
        result = subprocess.CompletedProcess([], 1, stdout=b'private stdout', stderr=b'private stderr')
        with mock.patch.dict(os.environ, {'MACOS_SIGNING_CERTIFICATE': 'private',
                'MYGO_UPDATER_PRIVATE_KEY': 'private'}), mock.patch.object(
                subprocess, 'run', return_value=result) as tool:
            with self.assertRaises(setup.SetupError) as error:
                setup.run(['security', 'import', '-P', 'private-password'])
        self.assertNotIn('private-password', str(error.exception))
        self.assertNotIn('private stdout', str(error.exception))
        self.assertNotIn('private stderr', str(error.exception))
        self.assertNotIn('MACOS_SIGNING_CERTIFICATE', tool.call_args.kwargs['env'])
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', tool.call_args.kwargs['env'])

    def test_github_host_is_fixed_to_the_validated_origin_host(self):
        result = subprocess.CompletedProcess([], 0, stdout=b'[]', stderr=b'')
        with mock.patch.dict(os.environ, {'GH_HOST': 'enterprise.example'}), mock.patch.object(
                subprocess, 'run', return_value=result) as tool:
            setup.run(['gh', 'secret', 'list', '--repo', 'example/picup'])
        self.assertEqual(tool.call_args.kwargs['env']['GH_HOST'], 'github.com')

    def test_symlinked_configuration_is_rejected_before_remote_changes(self):
        (self.root / 'config').symlink_to(self.directory, target_is_directory=True)
        with self.assertRaisesRegex(setup.SetupError, 'symlink'):
            self.configure(self.args(apply=True))
        self.assertEqual(len(self.calls), 1)  # Only read the local origin URL.

    def test_import_failure_still_restores_search_list_and_deletes_temporary_keychain(self):
        calls = []
        def run(arguments, **options):
            calls.append(arguments)
            if arguments[1:3] == ['list-keychains', '-d'] and '-s' not in arguments:
                return b'"/original.keychain-db"'
            if arguments[1] == 'create-keychain':
                Path(arguments[-1]).touch()
            if arguments[1] == 'import':
                raise setup.SetupError('import failed')
            return b''
        with mock.patch.object(setup, 'run', side_effect=run):
            with self.assertRaisesRegex(setup.SetupError, 'import failed'):
                setup.validate_import(self.directory / 'package.p12', 'password', self.identity, self.directory)
        self.assertEqual(calls[-2][1:], ['list-keychains', '-d', 'user', '-s', '/original.keychain-db'])
        self.assertEqual(calls[-1][1], 'delete-keychain')

    def test_failed_search_list_restore_does_not_skip_keychain_deletion(self):
        deleted = []
        def run(arguments, **options):
            if arguments[1] == 'list-keychains':
                if '-s' in arguments:
                    raise setup.SetupError('restore failed')
                return b'"/original.keychain-db"'
            if arguments[1] == 'delete-keychain':
                deleted.append(arguments[-1])
            return b''
        with mock.patch.object(setup, 'run', side_effect=run), mock.patch.object(
                macos_signing, 'discover_identities', return_value=[self.identity]):
            with self.assertRaisesRegex(setup.SetupError, 'restore keychain search list'):
                setup.validate_import(self.directory / 'package.p12', 'password', self.identity, self.directory)
        self.assertEqual(len(deleted), 1)


@unittest.skipUnless(Path(setup.OPENSSL).exists(), 'OpenSSL fixture tool unavailable')
class PackagePairingTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.keys = []
        self.certificates = []
        config = self.directory / 'openssl.cnf'
        config.write_text('[req]\nprompt=no\ndistinguished_name=dn\nx509_extensions=ext\n'
                          '[dn]\nCN=Picup Fixture\n[ext]\nbasicConstraints=critical,CA:FALSE\n'
                          'keyUsage=critical,digitalSignature\nextendedKeyUsage=codeSigning\n')
        for index in range(2):
            key, certificate = self.directory / f'key{index}.pem', self.directory / f'cert{index}.pem'
            setup.run([setup.OPENSSL, 'req', '-new', '-x509', '-newkey', 'rsa:2048', '-nodes',
                       '-keyout', key, '-out', certificate, '-days', '2', '-config', config])
            self.keys.append(key.read_text())
            self.certificates.append(certificate.read_text())
        der = ssl.PEM_cert_to_DER_cert(self.certificates[1])
        self.identity = macos_signing.Signing(hashlib.sha1(der).hexdigest().upper(), 'Picup Fixture',
                                             certificate_sha256=hashlib.sha256(der).hexdigest())

    def normalize(self, pem):
        real_run = setup.run
        directory = self.directory / 'normalization'
        directory.mkdir()
        source = directory / 'source.p12'
        source.write_bytes(b'fixture input')
        def run(arguments, **options):
            if arguments[1:3] == ['pkcs12', '-in']:
                Path(arguments[arguments.index('-out') + 1]).write_text(pem)
                return b''
            return real_run(arguments, **options)
        with mock.patch.object(setup, 'run', side_effect=run):
            return setup.single_identity_package(source, 'fixture-password', self.identity, directory)

    def test_multi_identity_export_is_reduced_to_selected_certificate_and_matching_private_key(self):
        package, password = self.normalize(''.join(self.certificates + self.keys))
        decoded = self.directory / 'decoded.pem'
        setup.run([setup.OPENSSL, 'pkcs12', '-in', package, '-passin', 'env:PICUP_P12_PASSWORD',
                   '-nodes', '-out', decoded], environment={'PICUP_P12_PASSWORD': password})
        result = decoded.read_text()
        self.assertIn(self.certificates[1].strip(), result)
        self.assertNotIn(self.certificates[0].strip(), result)
        self.assertEqual(result.count('BEGIN PRIVATE KEY'), 1)
        self.assertEqual(package.stat().st_mode & 0o777, 0o600)

    def test_wrong_certificate_or_unmatched_private_key_is_rejected(self):
        for pem in (self.certificates[0] + self.keys[0], self.certificates[1] + self.keys[0]):
            with self.subTest(pem=pem[:20]), self.assertRaises(setup.SetupError):
                self.normalize(pem)
            shutil.rmtree(self.directory / 'normalization')

    @unittest.skipUnless(sys.platform == 'darwin' and os.environ.get('PICUP_TEST_KEYCHAIN_IMPORT') == '1',
                         'Explicit isolated macOS import/signing verification')
    def test_macos_import_and_fixed_signature_using_only_temporary_keychain(self):
        package, password = self.normalize(''.join(self.certificates + self.keys))
        original = shlex.split(setup.run(['/usr/bin/security', 'list-keychains', '-d', 'user']).decode())
        keychain = self.directory / 'signing-test.keychain-db'
        try:
            setup.run(['/usr/bin/security', 'create-keychain', '-p', 'fixture-keychain-password', keychain])
            setup.run(['/usr/bin/security', 'unlock-keychain', '-p', 'fixture-keychain-password', keychain])
            setup.run(['/usr/bin/security', 'import', package, '-k', keychain, '-f', 'pkcs12',
                       '-P', password, '-A'])  # Only generated fixture keys; allow test export without UI.
            # security export is aggregate: exercise a real two-identity export,
            # reduce it, and prove the reduced package imports as one identity.
            unrelated = self.directory / 'unrelated.p12'
            setup.run([setup.OPENSSL, 'pkcs12', '-export', '-in', self.directory / 'cert0.pem',
                       '-inkey', self.directory / 'key0.pem', '-out', unrelated,
                       '-passout', 'env:PICUP_P12_PASSWORD', '-keypbe', 'PBE-SHA1-3DES',
                       '-certpbe', 'PBE-SHA1-3DES', '-macalg', 'sha1'],
                      environment={'PICUP_P12_PASSWORD': 'fixture-password'})
            setup.run(['/usr/bin/security', 'import', unrelated, '-k', keychain, '-f', 'pkcs12',
                       '-P', 'fixture-password', '-A'])
            exported = self.directory / 'aggregate.p12'
            setup.run(['/usr/bin/security', 'export', '-k', keychain, '-t', 'identities',
                       '-f', 'pkcs12', '-P', 'fixture-password', '-o', exported])
            reduced = self.directory / 'reduced'
            reduced.mkdir(mode=0o700)
            selected, selected_password = setup.single_identity_package(
                exported, 'fixture-password', self.identity, reduced)
            setup.validate_import(selected, selected_password, self.identity, reduced)
            setup.run(['/usr/bin/security', 'set-key-partition-list', '-S', 'apple-tool:,apple:,codesign:',
                       '-s', '-k', 'fixture-keychain-password', keychain])
            binary = self.directory / 'picup'
            # Build fresh code: re-signing Apple's /usr/bin/true can retain its
            # explicit Apple designated requirement, unlike a new Go binary.
            setup.run(['/usr/bin/xcrun', 'clang', '-x', 'c', '-o', binary, '-'],
                      input=b'int main(void) { return 0; }\n')
            setup.run(['/usr/bin/codesign', '--force', '--sign', self.identity.identity_hash,
                       '--keychain', keychain, '--identifier', 'picup', '--options', 'runtime', '--timestamp', binary])
            try:
                macos_signing.verify_signed_code(binary, self.identity, is_cli=True)
            except macos_signing.SigningError as error:
                details = macos_signing.command('/usr/bin/codesign', '--display', '--requirements', '-',
                                               str(binary), capture=True, diagnostics=True)
                self.fail(f'{error}\nPublic designated requirement: {details}')
        finally:
            try:
                setup.run(['/usr/bin/security', 'list-keychains', '-d', 'user', '-s', *original])
            finally:
                if keychain.exists():
                    setup.run(['/usr/bin/security', 'delete-keychain', keychain])


if __name__ == '__main__':
    unittest.main()
