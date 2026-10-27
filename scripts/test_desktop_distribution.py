"""Exercise channel derivation and signing failure paths without system changes."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import build_manifest
import macos_signing
import macos_signing_ci


class DesktopDistributionTests(unittest.TestCase):
    def extraction_prefix(self, arguments):
        options=[argument for argument in arguments if argument.startswith('--extract-certificates')]
        if not options:
            return None
        # codesign's optional prefix must be attached with '='; a separate
        # argument is instead interpreted as another code object to inspect.
        self.assertEqual(len(options),1)
        self.assertTrue(options[0].startswith('--extract-certificates='))
        self.assertEqual(len(arguments),4)
        self.assertEqual(arguments[2],options[0])
        return options[0].split('=',1)[1]

    def test_development_configuration_has_independent_identity_resources_and_no_feed(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            original={'name':'Picup','identifier':'com.knothhe.picup','version':'0.1.1',
                      'macos':{'minimumSystemVersion':'13.0'},'updates':{'github':'knothhe/picup'}}
            source=json.dumps(original)
            (root/'mygo.json').write_text(source)
            with mock.patch.object(build_manifest,'verify_development_icon'):
                with build_manifest.packaging_configuration(root,True) as directory:
                    data=json.loads((directory/'mygo.json').read_text())
                    self.assertEqual(data['name'],'Picup Dev')
                    self.assertEqual(data['identifier'],'com.knothhe.picup.dev')
                    self.assertNotIn('updates',data)
                    self.assertEqual(data['macos']['infoPlist']['PicupDevMaintenanceProtocol'],'1')
                    self.assertIn('/build/dev/',data['out'])
                    self.assertTrue(data['resources'][0].endswith('/picup-dev'))
                self.assertFalse(directory.exists())
            self.assertEqual((root/'mygo.json').read_text(),source)

    def test_checked_in_development_icons_match_source_generator_parameters_and_all_files(self):
        build_manifest.verify_development_icon(Path(__file__).resolve().parent.parent)

    def test_explicit_identity_must_be_unique_and_notary_requires_developer_id(self):
        digest='A'*40
        identity=macos_signing.Signing(digest,'Local Signing',certificate_sha256='b'*64,
                                     not_before='2026-01-01T00:00:00+00:00',
                                     not_after='2036-01-01T00:00:00+00:00')
        with mock.patch.object(macos_signing,'discover_identities',return_value=[identity]):
            signing=macos_signing.configuration({'PICUP_MACOS_SIGNING_IDENTITY':digest})
            self.assertEqual(signing.identity_hash,digest)
            self.assertEqual(signing.certificate_sha256,identity.certificate_sha256)
            for environment in ({'PICUP_MACOS_SIGNING_IDENTITY':'missing'},
                                {'PICUP_MACOS_SIGNING_IDENTITY':digest,'PICUP_MACOS_NOTARY_PROFILE':'profile'},
                                {'PICUP_MACOS_SIGNING_IDENTITY':''},
                                {'PICUP_MACOS_NOTARY_PROFILE':'profile'}):
                with self.subTest(environment=environment),self.assertRaises(macos_signing.SigningError):
                    macos_signing.configuration(environment)
        self.assertIsNone(macos_signing.configuration({}))

    def test_final_signature_verification_checks_all_owned_executables_and_rejects_wrong_certificate(self):
        certificate=b'test-certificate-der'
        signing=macos_signing.Signing(hashlib.sha1(certificate).hexdigest().upper(),'Fixture',
                                     certificate_sha256=hashlib.sha256(certificate).hexdigest())
        targets=[]
        def command(*arguments,**options):
            prefix=self.extraction_prefix(arguments)
            if prefix is not None:
                targets.append(arguments[-1])
                Path(prefix+'0').write_bytes(certificate if len(targets)<3 else b'wrong-cli-certificate')
            if '--verbose=4' in arguments:return 'Identifier=com.knothhe.picup'
            if '--requirements' in arguments:return 'designated => identifier "com.knothhe.picup" and anchor H"'+signing.identity_hash+'"'
        with mock.patch.object(macos_signing,'command',side_effect=command):
            with self.assertRaisesRegex(macos_signing.SigningError,'different identity'):
                macos_signing.verify_application(Path('/temporary/Picup.app'),signing)
        self.assertEqual(len(targets),3)

    def test_direct_development_builds_sign_and_verify_gui_and_cli_with_fixed_identifiers(self):
        certificate=b'local-dev-certificate'
        signing=macos_signing.Signing(hashlib.sha1(certificate).hexdigest().upper(),'Local Signing',
                                     certificate_sha256=hashlib.sha256(certificate).hexdigest())
        commands=[]
        def command(*arguments,**options):
            commands.append(arguments)
            identifier=('picup-dev' if arguments[-1].endswith('/picup-dev')
                        else 'com.knothhe.picup.dev')
            prefix=self.extraction_prefix(arguments)
            if prefix is not None:
                Path(prefix+'0').write_bytes(certificate)
            if '--verbose=4' in arguments:return 'Identifier='+identifier
            if '--requirements' in arguments:
                return f'designated => identifier "{identifier}" and anchor H"{signing.identity_hash}"'
        with mock.patch.object(macos_signing,'command',side_effect=command):
            macos_signing.sign_development_binaries(Path('build/dev'),signing)
        signed=[args for args in commands if '--sign' in args]
        self.assertEqual(signed,[
            ('/usr/bin/codesign','--force','--sign',signing.identity_hash,'--identifier',
             'picup-dev','build/dev/bin/picup-dev'),
            ('/usr/bin/codesign','--force','--sign',signing.identity_hash,'--identifier',
             'com.knothhe.picup.dev','build/dev/bin/picup-gui'),
        ])
        self.assertEqual(len([args for args in commands if '--test-requirement' in args]),2)
        self.assertTrue(all(args[-2].startswith('=identifier ')
                            for args in commands if '--test-requirement' in args))
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            (root/'mygo.json').write_text(json.dumps({'macos':{}}))
            with mock.patch.object(build_manifest,'verify_development_icon'):
                with build_manifest.packaging_configuration(root,True,signing) as directory:
                    manifest=json.loads((directory/'mygo.json').read_text())
            self.assertEqual(manifest['identifier'],signed[1][-2])
            self.assertEqual(manifest['macos']['signingIdentity'],signing.identity_hash)

    def test_direct_development_signing_stops_on_wrong_certificate_or_pin(self):
        certificate=b'local-dev-certificate'
        for digest,pin,message in (
                ('A'*40,'','different identity'),
                (hashlib.sha1(certificate).hexdigest().upper(),'B'*64,'SHA-256 pin')):
            signing=macos_signing.Signing(digest,'Local Signing',certificate_sha256=pin)
            commands=[]
            def command(*arguments,**options):
                commands.append(arguments)
                prefix=self.extraction_prefix(arguments)
                if prefix is not None:
                    Path(prefix+'0').write_bytes(certificate)
            with self.subTest(message=message),mock.patch.object(macos_signing,'command',side_effect=command):
                with self.assertRaisesRegex(macos_signing.SigningError,message):
                    macos_signing.sign_development_binaries(Path('build/dev'),signing)
            self.assertEqual(len([args for args in commands if '--sign' in args]),1)

    def test_local_ca_requirement_uses_only_certificates_at_their_exported_chain_positions(self):
        certificates = [b'local signing leaf', b'local intermediate CA', b'local root CA']
        hashes = [hashlib.sha1(der).hexdigest().upper() for der in certificates]
        signing = macos_signing.Signing(hashes[0], 'Local CA-issued Signing',
                                       certificate_sha256=hashlib.sha256(certificates[0]).hexdigest())
        identifier = 'com.knothhe.picup.dev'
        cases = (
            ('certificate root', hashes[2], True),
            ('certificate 1', hashes[1], True),
            ('certificate root', 'B' * 40, False),
            ('certificate 1', hashes[2], False),
        )
        for anchor, digest, accepted in cases:
            with self.subTest(anchor=anchor, digest=digest):
                def command(*arguments, **options):
                    prefix = self.extraction_prefix(arguments)
                    if prefix is not None:
                        for index, certificate in enumerate(certificates):
                            Path(prefix + str(index)).write_bytes(certificate)
                    if '--verbose=4' in arguments:
                        return 'Identifier=' + identifier
                    if '--requirements' in arguments:
                        return f'designated => identifier "{identifier}" and {anchor} = H"{digest}"'
                with mock.patch.object(macos_signing, 'command', side_effect=command) as execute:
                    if accepted:
                        macos_signing.verify_signed_code(Path('picup-gui'), signing, 'Picup Dev')
                        self.assertIn('certificate leaf = H"' + hashes[0] + '"', execute.call_args.args[-2])
                    else:
                        with self.assertRaisesRegex(macos_signing.SigningError, 'stable certificate identity'):
                            macos_signing.verify_signed_code(Path('picup-gui'), signing, 'Picup Dev')
                    # A valid chain cannot override the separately pinned leaf.
                    with self.assertRaisesRegex(macos_signing.SigningError, 'SHA-256 pin'):
                        macos_signing.verify_signed_code(
                            Path('picup-gui'), macos_signing.Signing(hashes[0], signing.name,
                                certificate_sha256='B' * 64), 'Picup Dev')

    def test_development_verification_rejects_unstable_or_unexpected_identities(self):
        signing=macos_signing.Signing('A'*40,'Local Signing')
        identifier='com.knothhe.picup.dev'
        for actual,requirement in (
                (identifier,'cdhash H"'+'B'*40+'"'),
                (identifier,f'identifier "{identifier}"'),
                (identifier,f'identifier "{identifier}" and anchor apple generic'),
                ('another.app',f'identifier "another.app" and anchor H"{signing.identity_hash}"')):
            def command(*arguments,**options):
                if '--verbose=4' in arguments:return 'Identifier='+actual
                if '--requirements' in arguments:return 'designated => '+requirement
            with self.subTest(actual=actual,requirement=requirement),mock.patch.object(
                    macos_signing,'command',side_effect=command):
                with self.assertRaises(macos_signing.SigningError):
                    macos_signing.verify_designated_requirement(Path('picup-gui'),signing,'Picup Dev')

    def test_apple_development_default_requirement_keeps_its_certificate_identity(self):
        signing=macos_signing.Signing('A'*40,'Apple Development: Local Developer (TEAM123456)')
        requirement=('identifier "com.knothhe.picup.dev" and anchor apple generic and '
                     'certificate leaf[subject.CN] = "'+signing.name+'" and '
                     'certificate 1[field.1.2.840.113635.100.6.2.1] /* exists */')
        def command(*arguments,**options):
            if '--verbose=4' in arguments:return 'Identifier=com.knothhe.picup.dev'
            if '--requirements' in arguments:return 'designated => '+requirement
        with mock.patch.object(macos_signing,'command',side_effect=command) as execute:
            macos_signing.verify_designated_requirement(Path('picup-gui'),signing,'Picup Dev')
        self.assertIn('certificate leaf = H"'+signing.identity_hash+'"',execute.call_args.args[-2])

    @unittest.skipUnless(Path('/usr/bin/csreq').is_file(), 'system csreq is required')
    def test_apple_development_certificate_names_match_system_requirement_literals(self):
        names = (
            'Apple Development: 张三 (TEAM123456)',
            'Apple Development: Two  Spaces (TEAM123456)',
            'Apple Development: Quote "A" \\ Backslash (TEAM123456)',
            'Apple Development: Name /* literal annotation */ (TEAM123456)',
        )
        for name in names:
            with self.subTest(name=name):
                signing = macos_signing.Signing('A' * 40, name)
                source = ('identifier "com.knothhe.picup.dev" and anchor apple generic and '
                          'certificate leaf[subject.CN] = 0x' + name.encode('utf-8').hex() + ' and '
                          'certificate 1[field.1.2.840.113635.100.6.2.1]')
                # Compile and dump with the real requirement tool; this neither
                # signs code nor imports certificates into a system keychain.
                requirement = subprocess.run(
                    ['/usr/bin/csreq', '-r', '=' + source, '-t'], check=True,
                    env=dict(os.environ, LC_ALL='C'), text=True,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout.strip()
                def command(*arguments, **options):
                    if '--verbose=4' in arguments:
                        return 'Identifier=com.knothhe.picup.dev'
                    if '--requirements' in arguments:
                        return 'designated => ' + requirement
                with mock.patch.object(macos_signing, 'command', side_effect=command):
                    macos_signing.verify_designated_requirement(Path('picup-gui'), signing, 'Picup Dev')
                    with self.assertRaises(macos_signing.SigningError):
                        macos_signing.verify_designated_requirement(
                            Path('picup-gui'), macos_signing.Signing('A' * 40, name + ' Other'), 'Picup Dev')

    def test_ci_private_material_and_search_list_are_cleaned_on_import_failure(self):
        import base64
        environment={'MACOS_SIGNING_CERTIFICATE':base64.b64encode(b'fixture-p12').decode(),
                     'MACOS_SIGNING_CERTIFICATE_PASSWORD':'fixture-password',
                     'PICUP_MACOS_CERTIFICATE_SHA256':'a'*64}
        commands=[]
        def command(arguments,capture=False):
            commands.append(arguments)
            if arguments[1:3]==['list-keychains','-d'] and '-s' not in arguments:return '"/original.keychain"'
            if arguments[1]=='import':raise ValueError('fixture import failed')
            return ''
        with tempfile.TemporaryDirectory() as temporary:
            environment['RUNNER_TEMP']=temporary
            with mock.patch.object(macos_signing_ci,'run',side_effect=command):
                with self.assertRaisesRegex(ValueError,'fixture import failed'):
                    with macos_signing_ci.signing_environment(environment):
                        self.fail('entered build after failed import')
            self.assertEqual(list(Path(temporary).iterdir()),[])
        self.assertTrue(any('delete-keychain' in args for args in commands))
        self.assertTrue(any(args[-2:]==['-s','/original.keychain'] for args in commands))

    def test_ci_only_build_child_receives_updater_key_and_private_inputs_are_removed(self):
        import base64
        environment={'MACOS_SIGNING_CERTIFICATE':base64.b64encode(b'fixture-p12').decode(),
                     'MACOS_SIGNING_CERTIFICATE_PASSWORD':'fixture-password',
                     'PICUP_MACOS_CERTIFICATE_SHA256':'a'*64,'MYGO_UPDATER_PRIVATE_KEY':'update-test-key'}
        def command(arguments,capture=False):
            if arguments[1]=='list-keychains' and '-s' not in arguments:return '"/original.keychain"'
            if arguments[1]=='find-identity':return '1) '+'A'*40+' "Developer ID Application: Fixture (TEST)"'
            return ''
        with mock.patch.object(macos_signing_ci,'run',side_effect=command):
            with macos_signing_ci.signing_environment(environment) as child:
                self.assertEqual(child['MYGO_UPDATER_PRIVATE_KEY'],'update-test-key')
                self.assertEqual(child['PICUP_MACOS_SIGNING_IDENTITY'],'A'*40)
                self.assertFalse(any(name in child for name in macos_signing_ci.PRIVATE_VARIABLES))


if __name__=='__main__':
    unittest.main()
