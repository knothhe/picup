"""Test installer validation and ZIP integrity without building or publishing."""
import contextlib
import base64
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import stat
import tempfile
import unittest
from unittest import mock
import zipfile


SPEC = importlib.util.spec_from_file_location('package_release', Path(__file__).with_name('package_release.py'))
package = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(package)


class PackageReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.version = '0.1.0'
        self.data = {'name': 'Picup', 'identifier': 'com.knothhe.picup', 'version': self.version,
                     'out': 'build/dist', 'macos': {},
                     'updates': {'github': 'knothhe/picup', 'deltas': 0,
                                 'publicKey': base64.b64encode(bytes(32)).decode()}}
        self.home = self.root / 'home'
        self.home.mkdir()
        environment = mock.patch.dict(os.environ, {'HOME': str(self.home), 'MYGO_UPDATER_PRIVATE_KEY': 'private-test-key'})
        environment.start()
        self.addCleanup(environment.stop)
        (self.root / 'mygo.json').write_text(json.dumps(self.data))
        self.app = self.root / 'build/dist/darwin-arm64/Picup.app'
        self.output = self.root / 'build/release'
        self.environment = {'GOHOSTOS': 'darwin', 'GOHOSTARCH': 'arm64', 'GOOS': 'darwin', 'GOARCH': 'arm64'}
        self.tags = ''
        self.cgo = '1'

    def make_app(self, version=None, cli_version=None):
        version = version or self.version
        contents = self.app / 'Contents'
        (contents / 'MacOS').mkdir(parents=True, exist_ok=True)
        (contents / 'Resources').mkdir(exist_ok=True)
        development = 'picup_dev' in self.tags
        name,identifier,cli_name = ('Picup Dev','com.knothhe.picup.dev','picup-dev') if development else ('Picup','com.knothhe.picup','picup')
        (contents / 'Info.plist').write_bytes(plistlib.dumps({
            'CFBundleVersion': version, 'CFBundleShortVersionString': version,
            'CFBundleExecutable': name, 'CFBundleIdentifier': identifier,
        }))
        gui = contents / 'MacOS' / name
        gui.write_bytes(b'GUI executable fixture')
        gui.chmod(0o755)
        cli = contents / 'Resources' / cli_name
        cli.write_text(f'#!/bin/sh\nprintf "picup {cli_version or version}\\n"\n')
        cli.chmod(0o755)
        (contents / 'Resources/icon').write_bytes(b'icon fixture')
        (contents / 'Resources/THIRD_PARTY_NOTICES.txt').write_text('Third-party license fixture')
        framework = contents / 'Frameworks/Example.framework/Versions/A'
        framework.mkdir(parents=True, exist_ok=True)
        (framework / 'Example').write_bytes(b'framework fixture')
        current = framework.parent / 'Current'
        if not current.is_symlink():
            current.symlink_to('A', target_is_directory=True)
        binary = framework.parent.parent / 'Example'
        if not binary.is_symlink():
            binary.symlink_to('Versions/Current/Example')

    def run_command(self, *args, cwd, capture=False, env=None):
        if args[1:3] == ('env', '-json'):
            return json.dumps(self.environment)
        if args[:2] == ('make', 'build'):
            if self.environment['GOHOSTOS'] == 'linux':
                directory = self.root / 'build'
                (directory / 'bin').mkdir(parents=True, exist_ok=True)
                (directory / 'bin/picup').write_bytes(self.elf())
                (directory / 'bin/picup').chmod(0o755)
                (directory / 'THIRD_PARTY_NOTICES.txt').write_text('License fixture')
                (self.root / 'assets').mkdir(exist_ok=True)
                (self.root / 'assets/icon.png').write_bytes(b'icon fixture')
            return None
        if args[1:4] == ('tool', 'mygo', 'build'):
            config = json.loads((Path(args[-1]) / 'mygo.json').read_text())
            self.assertNotIn('updates', config)
            self.tags = 'picup_dev' if '-debug' in args else ''
            if self.tags:
                self.app = self.root / 'build/dev/dist/darwin-arm64/Picup Dev.app'
            self.make_app()
            return None
        if args[1:3] == ('version', '-m'):
            return ('\tbuild\t-tags=' + self.tags + '\n\tbuild\tCGO_ENABLED=' + self.cgo
                    + '\n\tbuild\tGOOS=' + self.environment['GOHOSTOS']
                    + '\n\tbuild\tGOARCH=' + self.environment['GOHOSTARCH'])
        if args[:2] == ('/usr/bin/lipo', '-archs'):
            return 'arm64'
        if args[1:3] == ('build', '-trimpath'):
            target = Path(args[args.index('-o') + 1])
            if args[-1] == './cmd/picup-gui':
                target.write_bytes(self.elf())
                target.chmod(0o755)
            return None
        if Path(args[0]).name == 'sign-update':
            archive = Path(args[-1])
            return json.dumps({'size': archive.stat().st_size,
                               'sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
                               'signature': base64.b64encode(bytes(64)).decode()})
        if args[1:3] == ('run', './cmd/verify-update'):
            return None
        if self.environment['GOHOSTOS'] == 'linux' and args[-1] == '--version':
            return f'picup {self.version}'
        return self.original_run(*args, cwd=cwd, capture=capture, env=env)

    def elf(self, arch='amd64'):
        header = bytearray(64)
        header[:6] = b'\x7fELF\x02\x01'
        header[18:20] = {'amd64': b'\x3e\x00', 'arm64': b'\xb7\x00'}[arch]
        return bytes(header)

    def patch_commands(self, side_effect=None):
        self.original_run = package.run
        return mock.patch.object(package, 'run', side_effect=side_effect or self.run_command)

    def test_package_preserves_executables_companion_and_framework_symlinks(self):
        # A stale package on another platform must never become an attachment.
        stale = self.root / 'build/dist/darwin-amd64/Picup.app'
        stale.mkdir(parents=True)
        (stale / 'stale').write_text('old build')
        with self.patch_commands() as commands:
            artifacts = package.package_release(self.root, self.version, self.output)
        self.assertEqual(artifacts, [str(self.output / name) for name in (
            'Picup-0.1.0-darwin-arm64.zip', 'update-feed.json')])
        self.assertEqual(commands.call_args_list[1].args, ('make', 'build', 'BUILD_TAGS='))
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', commands.call_args_list[1].kwargs['env'])
        build = commands.call_args_list[2]
        self.assertEqual(build.args[:5], ('go', 'tool', 'mygo', 'build', '-skip-dmg'))
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', build.kwargs['env'])
        if package.sys.platform == 'darwin':
            self.assertEqual(build.kwargs['env']['CGO_ENABLED'], '1')
            self.assertEqual(commands.call_args_list[1].kwargs['env']['CGO_ENABLED'], '1')
        with zipfile.ZipFile(artifacts[0]) as archive:
            self.assertIsNone(archive.testzip())
            names = archive.namelist()
            self.assertFalse(any('stale' in name for name in names))
            self.assertTrue(all(name.startswith('Picup.app/') for name in names))
            for name in ('Contents/MacOS/Picup', 'Contents/Resources/picup'):
                info = archive.getinfo(f'Picup.app/{name}')
                mode = info.external_attr >> 16
                self.assertTrue(stat.S_ISREG(mode))
                self.assertEqual(mode & 0o777, 0o755)
            link = archive.getinfo('Picup.app/Contents/Frameworks/Example.framework/Versions/Current')
            self.assertTrue(stat.S_ISLNK(link.external_attr >> 16))
            self.assertEqual(archive.read(link), b'A')
            self.assertEqual(archive.read('Picup.app/Contents/Frameworks/Example.framework/Example'),
                             b'Versions/Current/Example')
            self.assertFalse(any('/Current/' in name for name in names))
        feed = json.loads(Path(artifacts[1]).read_text())
        self.assertEqual(feed['schema'], 1)
        self.assertEqual(feed['version'], self.version)
        self.assertEqual(set(feed['platforms']), {'darwin-arm64'})
        asset = feed['platforms']['darwin-arm64']
        self.assertEqual(asset['url'], f'https://github.com/knothhe/picup/releases/download/v{self.version}/Picup-{self.version}-darwin-arm64.zip')
        self.assertEqual(asset['size'], Path(artifacts[0]).stat().st_size)
        self.assertEqual(asset['sha256'], hashlib.sha256(Path(artifacts[0]).read_bytes()).hexdigest())
        self.assertEqual(json.loads((self.root / 'mygo.json').read_text()), self.data)
        self.assertEqual({path.name for path in self.output.iterdir()},
                         {'Picup-0.1.0-darwin-arm64.zip', 'update-feed.json'})
        verification = commands.call_args_list[-1]
        self.assertEqual(verification.args, ('go', 'run', './cmd/verify-update', '--feed',
                         str(self.output / 'update-feed.json'), '--archive', artifacts[0],
                         '--version', self.version, '--os', 'darwin', '--arch', 'arm64'))
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', verification.kwargs['env'])

    def test_manifest_mismatch_stops_before_build(self):
        with self.patch_commands() as commands:
            with self.assertRaisesRegex(package.PackageError, 'mygo.json version'):
                package.package_release(self.root, '0.2.0', self.output)
        commands.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_rejects_cross_compilation_and_unsupported_host(self):
        for change in ({'GOOS': 'linux'}, {'GOARCH': 'amd64'},
                       {'GOHOSTOS': 'windows', 'GOOS': 'windows'}):
            with self.subTest(change=change):
                original = dict(self.environment)
                self.environment.update(change)
                with self.patch_commands() as commands:
                    with self.assertRaises(package.PackageError):
                        package.package_release(self.root, self.version, self.output)
                self.assertEqual(commands.call_count, 1)
                self.environment = original

    def test_build_failure_never_packages_stale_app(self):
        self.make_app()
        def fail_build(*args, cwd, capture=False, env=None):
            if args[:2] == ('make', 'build'):
                raise package.PackageError('build failed')
            return self.run_command(*args, cwd=cwd, capture=capture, env=env)
        with self.patch_commands(fail_build):
            with self.assertRaisesRegex(package.PackageError, 'build failed'):
                package.package_release(self.root, self.version, self.output)
        self.assertFalse(self.output.exists())

    def test_packaging_failure_never_packages_stale_app(self):
        self.make_app()
        def fail_package(*args, cwd, capture=False, env=None):
            if args[1:4] == ('tool', 'mygo', 'build'):
                raise package.PackageError('packaging failed')
            return self.run_command(*args, cwd=cwd, capture=capture, env=env)
        with self.patch_commands(fail_package):
            with self.assertRaisesRegex(package.PackageError, 'packaging failed'):
                package.package_release(self.root, self.version, self.output)
        self.assertFalse(self.output.exists())

    def test_rejects_each_bundle_version_and_wrong_companion_version(self):
        self.make_app()
        info_path = self.app / 'Contents/Info.plist'
        for key in ('CFBundleVersion', 'CFBundleShortVersionString'):
            with self.subTest(key=key):
                self.make_app()
                info = plistlib.loads(info_path.read_bytes())
                info[key] = '0.0.9'
                info_path.write_bytes(plistlib.dumps(info))
                with self.assertRaisesRegex(package.PackageError, key):
                    package.validate_app(self.app, self.version, self.root)
        self.make_app(cli_version='0.0.9')
        with self.assertRaisesRegex(package.PackageError, 'Bundled CLI version'):
            package.validate_app(self.app, self.version, self.root)

    def test_rejects_missing_or_non_executable_companion(self):
        self.make_app()
        cli = self.app / 'Contents/Resources/picup'
        cli.chmod(0o644)
        with self.assertRaisesRegex(package.PackageError, 'non-executable'):
            package.validate_app(self.app, self.version, self.root)
        cli.unlink()
        with self.assertRaisesRegex(package.PackageError, 'Missing'):
            package.validate_app(self.app, self.version, self.root)

    def test_main_prints_only_json_paths_on_stdout(self):
        stdout = io.StringIO()
        expected = [str(self.output / 'Picup-0.1.0-darwin-arm64.zip')]
        with mock.patch.object(package, 'package_release', return_value=expected) as build:
            with contextlib.redirect_stdout(stdout):
                self.assertEqual(package.main(['--version', self.version, '--output-dir', str(self.output)]), 0)
        self.assertEqual(json.loads(stdout.getvalue()), expected)
        build.assert_called_once_with(Path.cwd(), self.version, str(self.output), unsigned=False)

    def test_formal_package_requires_private_key_before_building(self):
        with mock.patch.dict(os.environ, {'MYGO_UPDATER_PRIVATE_KEY': ''}):
            with self.patch_commands() as commands:
                with self.assertRaisesRegex(package.PackageError, 'Explicit MYGO_UPDATER_PRIVATE_KEY is empty'):
                    package.package_release(self.root, self.version, self.output)
        commands.assert_not_called()

    def test_unsigned_ci_package_disables_updater_and_does_not_use_signing_key(self):
        with mock.patch.dict(os.environ, {'MYGO_UPDATER_PRIVATE_KEY': '',
                'PICUP_MACOS_SIGNING_IDENTITY': 'local-certificate',
                'PICUP_MACOS_NOTARY_PROFILE': 'local-notary'}):
            with self.patch_commands() as commands:
                with mock.patch.object(package, 'signing_environment', side_effect=AssertionError('unsigned builds must not load a key')):
                    with mock.patch('build_manifest.verify_development_icon'):
                        self.data['macos'] = {}
                        (self.root / 'mygo.json').write_text(json.dumps(self.data))
                        artifacts = package.package_release(self.root, self.version, self.output, unsigned=True)
        self.assertEqual(artifacts, [str(self.output / 'Picup-0.1.0-darwin-arm64-ci-unsigned.zip')])
        build = commands.call_args_list[2]
        self.assertIn('-debug', build.args)
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', build.kwargs['env'])
        for call in (commands.call_args_list[1], build):
            self.assertEqual(call.kwargs['env']['CI'], '1')
            self.assertTrue(all(variable not in call.kwargs['env']
                                for variable in package.SIGNING_VARIABLES))
        self.assertFalse(any('./cmd/sign-update' in call.args for call in commands.call_args_list))
        with zipfile.ZipFile(artifacts[0]) as archive:
            info = plistlib.loads(archive.read('Picup Dev.app/Contents/Info.plist'))
            self.assertEqual(info['CFBundleIdentifier'],'com.knothhe.picup.dev')

    def test_default_local_signer_is_loaded_only_into_finished_signer_environment(self):
        key = self.home / 'Library/Application Support/picup-release/mygo-update.key'
        key.parent.mkdir(parents=True)
        key.write_text('local-test-private-key\n')
        key.chmod(0o600)
        with mock.patch.dict(os.environ):
            os.environ.pop('MYGO_UPDATER_PRIVATE_KEY', None)
            with self.patch_commands() as commands:
                package.package_release(self.root, self.version, self.output)
            self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', os.environ)
        signer = next(call for call in commands.call_args_list if Path(call.args[0]).name == 'sign-update')
        self.assertEqual(signer.kwargs['env']['MYGO_UPDATER_PRIVATE_KEY'], 'local-test-private-key')
        self.assertTrue(all('MYGO_UPDATER_PRIVATE_KEY' not in (call.kwargs.get('env') or {})
                            for call in commands.call_args_list if call != signer))

    def test_environment_signer_takes_precedence_and_explicit_blank_never_falls_back(self):
        with mock.patch.object(package.os, 'open', side_effect=AssertionError('local signer should not be read')):
            self.assertEqual(package.signing_environment()['MYGO_UPDATER_PRIVATE_KEY'], 'private-test-key')
            with mock.patch.dict(os.environ, {'MYGO_UPDATER_PRIVATE_KEY': ''}):
                with self.assertRaisesRegex(package.PackageError, 'Explicit MYGO_UPDATER_PRIVATE_KEY is empty'):
                    package.signing_environment()

    def test_local_signer_rejects_symlinks_unsafe_mode_and_missing_file(self):
        key = self.home / 'Library/Application Support/picup-release/mygo-update.key'
        key.parent.mkdir(parents=True)
        with mock.patch.dict(os.environ):
            os.environ.pop('MYGO_UPDATER_PRIVATE_KEY', None)
            with self.assertRaisesRegex(package.PackageError, 'No safe local'):
                package.signing_environment()
            key.write_text('local-test-private-key')
            key.chmod(0o644)
            with self.assertRaisesRegex(package.PackageError, 'mode 0600'):
                package.signing_environment()
            target = key.with_name('actual.key')
            key.rename(target)
            target.chmod(0o600)
            key.symlink_to(target)
            with self.assertRaisesRegex(package.PackageError, 'No safe local'):
                package.signing_environment()

    def test_other_tools_never_inherit_explicit_signing_environment(self):
        result = mock.Mock(returncode=0, stdout='ok', stderr='')
        with mock.patch.object(package.subprocess, 'run', return_value=result) as execute:
            package.run('go', 'env', cwd=self.root, capture=True)
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', execute.call_args.kwargs['env'])

    def test_formal_package_rejects_dev_goflags_before_loading_signer(self):
        with mock.patch.dict(os.environ, {'GOFLAGS': '-mod=readonly -tags=extra,picup_dev'}):
            with mock.patch.object(package, 'signing_environment', side_effect=AssertionError('must not read key')):
                with self.assertRaisesRegex(ValueError, 'must not use picup_dev'):
                    package.package_release(self.root, self.version, self.output)

    def test_formal_app_rejects_a_development_tagged_gui_or_cli(self):
        self.make_app()
        self.tags = 'extra,picup_dev'
        with self.patch_commands():
            with self.assertRaisesRegex(package.PackageError, 'development-tagged'):
                package.validate_app(self.app, self.version, self.root)

    def test_package_rejects_explicit_cgo_zero_before_reading_signer(self):
        with mock.patch.dict(os.environ, {'GOOS': 'darwin', 'CGO_ENABLED': '0'}):
            with mock.patch.object(package, 'signing_environment', side_effect=AssertionError('must not read key')):
                with self.assertRaisesRegex(ValueError, 'CGO_ENABLED=1.*Keychain'):
                    package.package_release(self.root, self.version, self.output)

    def test_app_rejects_gui_or_cli_without_native_keychain_support(self):
        self.make_app()
        for broken_binary in ('Picup', 'picup'):
            with self.subTest(binary=broken_binary):
                def response(*args, **kwargs):
                    if args[1:3] == ('version', '-m') and Path(args[3]).name == broken_binary:
                        return '\tbuild\tCGO_ENABLED=0'
                    return self.run_command(*args, **kwargs)
                with self.patch_commands(response):
                    with self.assertRaisesRegex(package.PackageError, 'native R2 Keychain support'):
                        package.validate_app(self.app, self.version, self.root)

    def test_linux_zip_has_distinct_native_gui_cli_and_release_identity(self):
        self.environment = {'GOHOSTOS': 'linux', 'GOHOSTARCH': 'amd64', 'GOOS': 'linux', 'GOARCH': 'amd64'}
        with self.patch_commands() as commands:
            artifacts = package.package_release(self.root, self.version, self.output)
        self.assertEqual([Path(path).name for path in artifacts], ['Picup-0.1.0-linux-amd64.zip', 'update-feed.json'])
        self.assertFalse(any(call.args[1:4] == ('tool', 'mygo', 'build') for call in commands.call_args_list))
        gui_build = next(call for call in commands.call_args_list if call.args[-1] == './cmd/picup-gui')
        self.assertIn('github.com/egoist/mygo.production=1', gui_build.args[gui_build.args.index('-ldflags') + 1])
        self.assertIn('mygo_noinspector', gui_build.kwargs['env']['GOFLAGS'])
        with zipfile.ZipFile(artifacts[0]) as archive:
            self.assertEqual(set(archive.namelist()), {'Picup/', 'Picup/picup-gui', 'Picup/picup',
                             'Picup/icon.png', 'Picup/THIRD_PARTY_NOTICES.txt', 'Picup/picup-release.json'})
            self.assertEqual(json.loads(archive.read('Picup/picup-release.json')),
                             package.linux_metadata(self.version, 'amd64'))
            for name in ('picup-gui', 'picup'):
                self.assertEqual(archive.read(f'Picup/{name}')[:4], b'\x7fELF')
                self.assertEqual((archive.getinfo(f'Picup/{name}').external_attr >> 16) & 0o777, 0o755)
        self.assertEqual(set(json.loads(Path(artifacts[1]).read_text())['platforms']), {'linux-amd64'})
        self.assertEqual({path.suffix for path in self.output.iterdir()}, {'.zip', '.json'})

    def test_linux_rejects_wrong_elf_architecture_and_go_build_platform(self):
        self.environment = {'GOHOSTOS': 'linux', 'GOHOSTARCH': 'amd64', 'GOOS': 'linux', 'GOARCH': 'amd64'}
        executable = self.root / 'picup-gui'
        executable.write_bytes(self.elf('arm64'))
        with self.patch_commands():
            with self.assertRaisesRegex(package.PackageError, 'native amd64 Linux ELF'):
                package.validate_binary(executable, self.root, True, 'linux', 'amd64')
        executable.write_bytes(self.elf())
        with self.patch_commands() as commands:
            commands.side_effect = lambda *args, **kwargs: '\tbuild\tGOOS=darwin\n\tbuild\tGOARCH=amd64'
            with self.assertRaisesRegex(package.PackageError, 'platform metadata'):
                package.validate_binary(executable, self.root, True, 'linux', 'amd64')

    def test_signer_failure_never_writes_a_feed(self):
        def fail_signer(*args, **kwargs):
            if Path(args[0]).name == 'sign-update':
                raise package.PackageError('signing key does not match')
            return self.run_command(*args, **kwargs)
        with self.patch_commands(fail_signer):
            with self.assertRaisesRegex(package.PackageError, 'signing key does not match'):
                package.package_release(self.root, self.version, self.output)
        self.assertFalse((self.output / 'update-feed.json').exists())

    def test_final_zip_verification_failure_rejects_package(self):
        def fail_verification(*args, **kwargs):
            if args[1:3] == ('run', './cmd/verify-update'):
                raise package.PackageError('archive signature does not match')
            return self.run_command(*args, **kwargs)
        with self.patch_commands(fail_verification):
            with self.assertRaisesRegex(package.PackageError, 'signature does not match'):
                package.package_release(self.root, self.version, self.output)

    def test_signer_metadata_must_match_the_final_zip(self):
        for change in ({'size': 1}, {'sha256': '0' * 64}, {'signature': 'invalid'}):
            with self.subTest(change=change):
                def invalid_signer(*args, **kwargs):
                    result = self.run_command(*args, **kwargs)
                    if Path(args[0]).name == 'sign-update':
                        return json.dumps(dict(json.loads(result), **change))
                    return result
                with self.patch_commands(invalid_signer):
                    with self.assertRaises(package.PackageError):
                        package.package_release(self.root, self.version, self.output)
                self.assertFalse((self.output / 'update-feed.json').exists())


if __name__ == '__main__':
    unittest.main()
