"""Validate public certificate matching without using the system keychain."""
import base64
from datetime import datetime, timezone
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import macos_signing


class FrozenDateTime(datetime):
    @classmethod
    def now(cls, tz=None):
        return cls(2030, 1, 1, tzinfo=timezone.utc)


def pem(der):
    return ('-----BEGIN CERTIFICATE-----\n' + base64.b64encode(der).decode() +
            '\n-----END CERTIFICATE-----')


def metadata(before='Jan  1 00:00:00 2026 GMT', after='Jan  1 00:00:00 2036 GMT',
             usage='Code Signing'):
    return (f'notBefore={before}\nnotAfter={after}\nCertificate:\n'
            '            X509v3 Extended Key Usage: critical\n'
            f'                {usage}\n            X509v3 Key Usage:\n'
            '                Digital Signature\n')


class SigningIdentityTests(unittest.TestCase):
    def discover(self, certificates, listing, details=None, keychain=''):
        calls = []
        paths = []
        def command(*arguments, **options):
            calls.append(arguments)
            if arguments[1] == 'find-identity':
                return listing
            if arguments[1] == 'find-certificate':
                return '\n'.join(pem(der) for der in certificates)
            self.assertEqual(arguments[:2], ('/usr/bin/openssl', 'x509'))
            path = Path(arguments[arguments.index('-in') + 1])
            paths.append(path)
            der = macos_signing.ssl.PEM_cert_to_DER_cert(path.read_text())
            return (details or {}).get(der, metadata())
        with mock.patch.object(macos_signing, 'command', side_effect=command), mock.patch.object(
                macos_signing, 'datetime', FrozenDateTime):
            identities = macos_signing.discover_identities(keychain)
        self.assertTrue(all(not path.exists() for path in paths))
        return identities, calls

    def test_explicit_keychain_limits_both_identity_and_public_certificate_discovery(self):
        certificate = b'isolated-keychain-certificate'
        digest = hashlib.sha1(certificate).hexdigest().upper()
        identities, calls = self.discover([certificate], f'1) {digest} "Local"',
                                          keychain='/temporary.keychain-db')
        self.assertEqual(len(identities), 1)
        self.assertEqual(calls[0][-1], '/temporary.keychain-db')
        self.assertEqual(calls[1][-1], '/temporary.keychain-db')
        with mock.patch.object(macos_signing, 'discover_identities', return_value=identities) as discover:
            selected = macos_signing.configuration({'PICUP_MACOS_SIGNING_IDENTITY': digest,
                'PICUP_MACOS_SIGNING_KEYCHAIN': '/temporary.keychain-db'})
        discover.assert_called_once_with('/temporary.keychain-db')
        self.assertEqual(selected.signing_keychain, '/temporary.keychain-db')

    def test_security_dump_accepts_bare_cli_identifier_but_keeps_certificate_constraint_strict(self):
        signing = macos_signing.Signing('A' * 40, 'Local')
        def command(*arguments, **options):
            if '--verbose=4' in arguments:
                return 'Identifier=picup'
            if '--requirements' in arguments:
                return 'designated => identifier picup and certificate leaf = H"' + 'a' * 40 + '"'
            return ''
        with mock.patch.object(macos_signing, 'command', side_effect=command):
            macos_signing.verify_designated_requirement(Path('picup'), signing, 'Picup', is_cli=True)
        invalid = 'designated => identifier picup and certificate leaf = H"' + 'a' * 40 + '" or true'
        with mock.patch.object(macos_signing, 'command', side_effect=lambda *args, **kwargs:
                'Identifier=picup' if '--verbose=4' in args else invalid):
            with self.assertRaisesRegex(macos_signing.SigningError, 'stable certificate identity'):
                macos_signing.verify_designated_requirement(Path('picup'), signing, 'Picup', is_cli=True)

    def test_untrusted_identity_is_matched_by_der_and_gets_complete_metadata(self):
        first, second, unrelated = b'first-certificate', b'second-certificate', b'unrelated-certificate'
        first_hash = hashlib.sha1(first).hexdigest().upper()
        second_hash = hashlib.sha1(second).hexdigest().upper()
        listing = (f'1) {second_hash} "Zulu Local" (CSSMERR_TP_NOT_TRUSTED)\n'
                   f'2) {first_hash} "Alpha Local"\n'
                   f'3) {first_hash} "Alpha Local"\n0 valid identities found')
        identities, calls = self.discover([unrelated, second, first, first], listing)
        self.assertEqual([identity.identity_hash for identity in identities], [first_hash, second_hash])
        self.assertEqual(identities[1].certificate_sha256, hashlib.sha256(second).hexdigest())
        self.assertEqual(identities[1].not_before, '2026-01-01T00:00:00+00:00')
        self.assertEqual(identities[1].not_after, '2036-01-01T00:00:00+00:00')
        self.assertEqual(calls[0], ('/usr/bin/security', 'find-identity', '-p', 'codesigning'))
        self.assertEqual(calls[1], ('/usr/bin/security', 'find-certificate', '-a', '-p'))
        self.assertEqual(len([call for call in calls if call[0] == '/usr/bin/openssl']), 2)

    def test_unpaired_or_wrong_der_certificate_is_not_selected_by_name(self):
        identities, _ = self.discover([b'another-certificate'], '1) ' + 'A' * 40 + ' "Same Name"')
        self.assertEqual(identities, [])

    def test_identity_labels_preserve_verbatim_quotes_backslashes_and_trust_words(self):
        certificate = b'quoted-label-certificate'
        digest = hashlib.sha1(certificate).hexdigest().upper()
        name = 'Local "Quoted" \\ Label (CSSMERR_TP_CERT_REVOKED)'
        listing = (f'  1) {digest} "{name}" (CSSMERR_TP_NOT_TRUSTED)\n'
                   f'ignored non-identity line {digest} "Wrong label"\n')
        identities, _ = self.discover([certificate], listing)
        self.assertEqual(len(identities), 1)
        self.assertEqual(identities[0].name, name)
        with mock.patch.object(macos_signing, 'discover_identities', return_value=identities):
            self.assertEqual(macos_signing.configuration({'PICUP_MACOS_SIGNING_IDENTITY': name}), identities[0])

    def test_dates_and_code_signing_eku_are_enforced(self):
        certificate = b'certificate'
        digest = hashlib.sha1(certificate).hexdigest().upper()
        listing = f'1) {digest} "Local"'
        cases = (
            metadata(after='Dec 31 23:59:59 2029 GMT'),
            metadata(before='Jan  2 00:00:00 2030 GMT'),
            metadata(usage='TLS Web Server Authentication'),
            metadata().replace('X509v3 Extended Key Usage:', 'X509v3 Subject Alternative Name:'),
        )
        for details in cases:
            with self.subTest(metadata=details):
                identities, _ = self.discover([certificate], listing, {certificate: details})
                self.assertEqual(identities, [])
        identities, _ = self.discover([certificate], listing, {
            certificate: metadata(usage='TLS Web Client Authentication, 1.3.6.1.5.5.7.3.3')})
        self.assertEqual(len(identities), 1)

    def test_revoked_identity_is_not_offered(self):
        certificate = b'revoked-certificate'
        digest = hashlib.sha1(certificate).hexdigest().upper()
        identities, calls = self.discover([certificate], f'1) {digest} "Local" (CSSMERR_TP_CERT_REVOKED)')
        self.assertEqual(identities, [])
        self.assertEqual(len(calls), 1)

    def test_malformed_certificate_validity_fails_without_returning_an_identity(self):
        certificate = b'certificate'
        digest = hashlib.sha1(certificate).hexdigest().upper()
        for details in (metadata(before='not a date'), metadata().replace('notAfter=', 'missing=')):
            with self.subTest(metadata=details), self.assertRaisesRegex(
                    macos_signing.SigningError, 'validity period'):
                self.discover([certificate], f'1) {digest} "Local"', {certificate: details})

    def test_explicit_sha256_mismatch_fails_before_signing(self):
        identity = macos_signing.Signing('A' * 40, 'Local', certificate_sha256='b' * 64,
                                         not_before='2026-01-01T00:00:00+00:00',
                                         not_after='2036-01-01T00:00:00+00:00')
        with mock.patch.object(macos_signing, 'discover_identities', return_value=[identity]), mock.patch.object(
                macos_signing, 'command') as command:
            with self.assertRaisesRegex(macos_signing.SigningError, 'SHA-256 pin'):
                macos_signing.configuration({'PICUP_MACOS_SIGNING_IDENTITY': identity.identity_hash,
                                            'PICUP_MACOS_CERTIFICATE_SHA256': 'c' * 64})
            self.assertEqual(macos_signing.configuration({
                'PICUP_MACOS_SIGNING_IDENTITY': identity.name,
                'PICUP_MACOS_CERTIFICATE_SHA256': 'B' * 64}), identity)
        command.assert_not_called()

    @unittest.skipUnless(Path('/usr/bin/openssl').is_file(), 'system openssl is required')
    def test_system_openssl_reads_an_isolated_self_signed_code_signing_certificate(self):
        # This test identity exists only in its temporary directory; no keychain
        # import or trust command is run, and no private material leaves it.
        with tempfile.TemporaryDirectory(prefix='picup-test-certificate-') as temporary:
            directory = Path(temporary)
            config = directory / 'openssl.cnf'
            config.write_text('[req]\nprompt=no\ndistinguished_name=dn\nx509_extensions=ext\n'
                              '[dn]\nCN=Picup Isolated Test Identity\n'
                              '[ext]\nbasicConstraints=critical,CA:FALSE\n'
                              'keyUsage=critical,digitalSignature\nextendedKeyUsage=codeSigning\n')
            certificate = directory / 'certificate.pem'
            subprocess.run(['/usr/bin/openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                            '-keyout', str(directory / 'test-key.pem'), '-out', str(certificate),
                            '-days', '1', '-config', str(config)], check=True,
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            exported = certificate.read_text()
            der = macos_signing.ssl.PEM_cert_to_DER_cert(exported)
            digest = hashlib.sha1(der).hexdigest().upper()
            original_command = macos_signing.command
            security_calls = []
            def command(*arguments, **options):
                if arguments[0] == '/usr/bin/security':
                    security_calls.append(arguments)
                    if arguments[1] == 'find-identity':
                        return f'1) {digest} "Picup Isolated Test Identity" (CSSMERR_TP_NOT_TRUSTED)'
                    self.assertEqual(arguments[1:], ('find-certificate', '-a', '-p'))
                    return exported
                return original_command(*arguments, **options)
            with mock.patch.object(macos_signing, 'command', side_effect=command):
                identities = macos_signing.discover_identities()
            self.assertEqual(len(identities), 1)
            self.assertEqual(identities[0].identity_hash, digest)
            self.assertEqual(identities[0].certificate_sha256, hashlib.sha256(der).hexdigest())
            self.assertLess(datetime.fromisoformat(identities[0].not_before), datetime.now(timezone.utc))
            self.assertGreater(datetime.fromisoformat(identities[0].not_after), datetime.now(timezone.utc))
            self.assertEqual(len(security_calls), 2)


if __name__ == '__main__':
    unittest.main()
