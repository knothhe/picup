"""Exercise channel derivation and signing failure paths without system changes."""
import base64
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
    def ci_identity(self, name='Picup Local Code Signing', pin='a' * 64):
        return macos_signing.Signing('A' * 40, name, certificate_sha256=pin,
                                    not_before='2026-01-01T00:00:00+00:00',
                                    not_after='2036-01-01T00:00:00+00:00')

    def ci_environment(self):
        return {'MACOS_SIGNING_CERTIFICATE': base64.b64encode(b'fixture-p12').decode(),
                'MACOS_SIGNING_CERTIFICATE_PASSWORD': 'fixture-password',
                'PICUP_MACOS_CERTIFICATE_SHA256': 'a' * 64}

    def ci_command(self, identity, commands):
        def command(arguments, capture=False):
            commands.append(arguments)
            if arguments[1] == 'list-keychains' and '-s' not in arguments:
                return '"/original.keychain"'
            if arguments[1] == 'create-keychain':
                Path(arguments[-1]).touch()
            if arguments[1] == 'find-identity':
                return f'1) {identity.identity_hash} "{identity.name}"'
            return ''
        return command

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
        identity=self.ci_identity()
        environment.update(PICUP_MACOS_SIGNING_IDENTITY='stale identity',
                           PICUP_MACOS_NOTARY_PROFILE='stale profile',
                           PICUP_MACOS_NOTARY_KEYCHAIN='/stale.keychain',
                           PICUP_MACOS_SIGNING_KEYCHAIN='/another.keychain')
        commands=[]
        with mock.patch.object(macos_signing_ci,'run',side_effect=self.ci_command(identity,commands)), \
                mock.patch.object(macos_signing,'discover_identities',return_value=[identity]) as discover:
            with macos_signing_ci.signing_environment(environment) as child:
                self.assertEqual(child['MYGO_UPDATER_PRIVATE_KEY'],'update-test-key')
                self.assertEqual(child['PICUP_MACOS_SIGNING_IDENTITY'],'A'*40)
                self.assertTrue(Path(child['PICUP_MACOS_SIGNING_KEYCHAIN']).is_file())
                self.assertEqual(set(key for key in child if key.startswith('PICUP_MACOS_')),
                                 {'PICUP_MACOS_SIGNING_IDENTITY','PICUP_MACOS_CERTIFICATE_SHA256',
                                  'PICUP_MACOS_SIGNING_KEYCHAIN'})
                self.assertIn(child['PICUP_MACOS_SIGNING_KEYCHAIN'],str(discover.call_args))
                self.assertFalse(any(name in child for name in macos_signing_ci.PRIVATE_VARIABLES))
        self.assertNotIn('-v',next(args for args in commands if args[1]=='find-identity'))

    def test_ci_fixed_identity_rejects_wrong_pin_or_unusable_certificate_before_build(self):
        for identities,message in (([self.ci_identity(pin='b'*64)],'SHA-256 pin'),
                                   ([], 'one valid existing')):
            with self.subTest(message=message),tempfile.TemporaryDirectory() as temporary:
                environment=self.ci_environment()
                environment['RUNNER_TEMP']=temporary
                commands=[]
                with mock.patch.object(macos_signing_ci,'run',
                        side_effect=self.ci_command(self.ci_identity(),commands)), \
                        mock.patch.object(macos_signing,'discover_identities',return_value=identities):
                    with self.assertRaisesRegex(macos_signing.SigningError,message):
                        with macos_signing_ci.signing_environment(environment):
                            self.fail('entered build before imported certificate validation')
                self.assertTrue(any(args[1]=='delete-keychain' for args in commands))
                self.assertEqual(list(Path(temporary).iterdir()),[])

    def test_ci_developer_id_mode_and_notarization_require_developer_id(self):
        for mode,notary in (('developer-id',False),('fixed-identity',True)):
            environment=self.ci_environment()
            if notary:
                environment.update(MACOS_NOTARY_KEY_BASE64=base64.b64encode(b'fixture-p8').decode(),
                                   MACOS_NOTARY_KEY_ID='fixture-id',MACOS_NOTARY_ISSUER='fixture-issuer')
            commands=[]
            with self.subTest(mode=mode,notary=notary), \
                    mock.patch.object(macos_signing_ci,'run',
                        side_effect=self.ci_command(self.ci_identity(),commands)), \
                    mock.patch.object(macos_signing,'discover_identities') as discover:
                with self.assertRaisesRegex(ValueError,'Developer ID Application certificate'):
                    with macos_signing_ci.signing_environment(environment,mode=mode):
                        self.fail('entered Developer ID build with a self-signed certificate')
                discover.assert_not_called()
            self.assertTrue(any(args[1]=='delete-keychain' for args in commands))

    def test_ci_developer_id_and_optional_notary_use_only_temporary_credentials(self):
        identity=self.ci_identity(name='Developer ID Application: Fixture (ABCDEFGHIJ)')
        for notary in (False,True):
            environment=self.ci_environment()
            if notary:
                environment.update(MACOS_NOTARY_KEY_BASE64=base64.b64encode(b'fixture-p8').decode(),
                                   MACOS_NOTARY_KEY_ID='fixture-id',MACOS_NOTARY_ISSUER='fixture-issuer')
            commands=[]
            with self.subTest(notary=notary), \
                    mock.patch.object(macos_signing_ci,'run',side_effect=self.ci_command(identity,commands)), \
                    mock.patch.object(macos_signing,'discover_identities',return_value=[identity]):
                with macos_signing_ci.signing_environment(environment,mode='developer-id') as child:
                    self.assertEqual('PICUP_MACOS_NOTARY_PROFILE' in child,notary)
                    self.assertFalse(any(key in child for key in macos_signing_ci.PRIVATE_VARIABLES))
                    if notary:
                        self.assertEqual(child['PICUP_MACOS_NOTARY_KEYCHAIN'],child['PICUP_MACOS_SIGNING_KEYCHAIN'])
                        self.assertEqual(child['PICUP_MACOS_NOTARY_PROFILE'],'picup-ci-notary')
            self.assertEqual(any(args[1]=='notarytool' for args in commands),notary)

    def test_ci_missing_certificate_or_password_fails_before_system_operations(self):
        for missing in ('MACOS_SIGNING_CERTIFICATE','MACOS_SIGNING_CERTIFICATE_PASSWORD'):
            environment=self.ci_environment()
            environment.pop(missing)
            with self.subTest(missing=missing),mock.patch.object(macos_signing_ci,'run') as command:
                with self.assertRaisesRegex(ValueError,'requires certificate, password'):
                    with macos_signing_ci.signing_environment(environment):
                        self.fail('entered build without signing material')
                command.assert_not_called()

    def test_ci_cleanup_launch_failure_still_attempts_all_items_and_removes_private_files(self):
        import base64
        environment={'MACOS_SIGNING_CERTIFICATE':base64.b64encode(b'fixture-p12').decode(),
                     'MACOS_SIGNING_CERTIFICATE_PASSWORD':'fixture-private-password',
                     'PICUP_MACOS_CERTIFICATE_SHA256':'a'*64}
        for build_fails in (False, True):
            for delete_fails in (False, True):
                commands=[]
                def command(arguments,capture=False):
                    commands.append(arguments)
                    if arguments[1]=='list-keychains' and '-s' not in arguments:
                        return '"/original.keychain"'
                    if arguments[1]=='find-identity':
                        return '1) '+'A'*40+' "Developer ID Application: Fixture (TEST)"'
                    if arguments[1]=='create-keychain':Path(arguments[-1]).touch()
                    if arguments[1]=='list-keychains' and arguments[4:]==['-s','/original.keychain']:
                        raise OSError('fixture-private-password: restore launch failure')
                    if arguments[1]=='delete-keychain' and delete_fails:
                        raise OSError('fixture-private-password: deletion launch failure')
                    return ''
                with self.subTest(build_fails=build_fails,delete_fails=delete_fails), \
                        tempfile.TemporaryDirectory() as temporary:
                    environment['RUNNER_TEMP']=temporary
                    with mock.patch.object(macos_signing_ci,'run',side_effect=command), \
                            mock.patch.object(macos_signing,'discover_identities',return_value=[self.ci_identity()]):
                        with self.assertRaisesRegex(ValueError,'CI signing cleanup failed') as failure:
                            with macos_signing_ci.signing_environment(environment):
                                if build_fails:
                                    raise ValueError('fixture build failed')
                    self.assertTrue(any(args[1]=='delete-keychain' for args in commands))
                    self.assertEqual(list(Path(temporary).iterdir()),[])
                    self.assertIn('restore keychain search list',str(failure.exception))
                    self.assertEqual('delete temporary keychain' in str(failure.exception),delete_fails)
                    self.assertNotIn('fixture-private-password',str(failure.exception))

    def test_ci_repository_pin_requires_exact_schema_and_lowercase_der_hash(self):
        pin = 'a' * 64
        invalid = (
            [],
            {'schema': 1},
            {'schema': True, 'certificateSHA256': pin},
            {'schema': 1.0, 'certificateSHA256': pin},
            {'schema': 2, 'certificateSHA256': pin},
            {'schema': 1, 'certificateSHA256': pin, 'identity': 'another identity'},
            {'schema': 1, 'certificateSHA256': 'A' * 64},
            {'schema': 1, 'certificateSHA256': 'a' * 63},
            {'schema': 1, 'certificateSHA256': 'g' * 64},
            {'schema': 1, 'certificateSHA256': None},
        )
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'macos-signing.json'
            with mock.patch.object(macos_signing_ci, 'PIN_PATH', path):
                for data in invalid:
                    with self.subTest(data=data):
                        path.write_text(json.dumps(data))
                        with self.assertRaisesRegex(ValueError, 'Invalid config/macos-signing.json'):
                            macos_signing_ci.repository_certificate_pin()
                path.write_text(json.dumps({'schema': 1, 'certificateSHA256': pin}))
                self.assertEqual(macos_signing_ci.repository_certificate_pin(), pin)

    def test_ci_cli_rejects_missing_or_unreadable_pin_before_keychain_or_build(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'macos-signing.json'
            with mock.patch.object(macos_signing_ci, 'PIN_PATH', path), \
                    mock.patch.object(macos_signing_ci, 'signing_environment') as signing, \
                    mock.patch.object(macos_signing_ci.subprocess, 'run') as build, \
                    mock.patch.object(macos_signing_ci.sys, 'stderr') as stderr:
                self.assertEqual(macos_signing_ci.main(['--', 'make', 'package-release']), 1)
                self.assertIn('committed config/macos-signing.json', stderr.write.call_args_list[0].args[0])
                for contents in (b'not-json', b'\xff', b'{"schema":1}'):
                    with self.subTest(contents=contents):
                        path.write_bytes(contents)
                        stderr.reset_mock()
                        self.assertEqual(macos_signing_ci.main(['--', 'make', 'package-release']), 1)
                        self.assertIn('Invalid config/macos-signing.json', stderr.write.call_args_list[0].args[0])
                signing.assert_not_called()
                build.assert_not_called()

    def test_ci_cli_uses_committed_pin_instead_of_environment_override(self):
        pin = 'a' * 64
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'macos-signing.json'
            path.write_text(json.dumps({'schema': 1, 'certificateSHA256': pin}))
            child = {'PICUP_MACOS_CERTIFICATE_SHA256': pin}
            with mock.patch.object(macos_signing_ci, 'PIN_PATH', path), \
                    mock.patch.dict(os.environ, {'PICUP_MACOS_CERTIFICATE_SHA256': 'b' * 64}, clear=True), \
                    mock.patch.object(macos_signing_ci, 'signing_environment') as signing, \
                    mock.patch.object(macos_signing_ci.subprocess, 'run', return_value=mock.Mock(returncode=0)) as build:
                signing.return_value.__enter__.return_value = child
                self.assertEqual(macos_signing_ci.main(['--', 'make', 'package-release']), 0)
                self.assertEqual(signing.call_args.args[0]['PICUP_MACOS_CERTIFICATE_SHA256'], pin)
                self.assertEqual(signing.call_args.kwargs,{'mode':'fixed-identity'})
                self.assertEqual(os.environ['PICUP_MACOS_CERTIFICATE_SHA256'], 'b' * 64)
                build.assert_called_once_with(['make', 'package-release'], env=child)


if __name__=='__main__':
    unittest.main()
