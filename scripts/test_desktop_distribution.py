"""Exercise channel derivation and signing failure paths without system changes."""
import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import build_manifest
import macos_signing
import macos_signing_ci


class DesktopDistributionTests(unittest.TestCase):
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
        with mock.patch.object(macos_signing,'command',return_value=f'1) {digest} "Local Signing"'):
            signing=macos_signing.configuration({'PICUP_MACOS_SIGNING_IDENTITY':digest})
            self.assertEqual(signing.identity_hash,digest)
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
            if '--extract-certificates' in arguments:
                targets.append(arguments[-1])
                prefix=arguments[arguments.index('--extract-certificates')+1]
                Path(prefix+'0').write_bytes(certificate if len(targets)<3 else b'wrong-cli-certificate')
            if '--verbose=4' in arguments:return 'Identifier=com.knothhe.picup'
            if '--requirements' in arguments:return 'designated => identifier "com.knothhe.picup" and anchor H"'+signing.identity_hash+'"'
        with mock.patch.object(macos_signing,'command',side_effect=command):
            with self.assertRaisesRegex(macos_signing.SigningError,'different identity'):
                macos_signing.verify_application(Path('/temporary/Picup.app'),signing)
        self.assertEqual(len(targets),3)

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
