"""Test DMG/update validation without building or publishing."""
import contextlib
import base64
import shutil
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location('package_release', Path(__file__).with_name('package_release.py'))
package = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(package)


class PackageReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.version = '0.1.0'
        self.data = {'name': 'Picup', 'version': self.version, 'out': 'build/dist',
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
        self.verify_fails = False
        self.mounts = []
        self.detached = []
        self.patched_calls = []
        patched = mock.patch.object(package, 'patched_environment', side_effect=self.patched_environment)
        patched.start()
        self.addCleanup(patched.stop)

    @contextlib.contextmanager
    def patched_environment(self, env):
        self.patched_calls.append(dict(env))
        yield dict(env, GOWORK='/patched/go.work')

    def image(self, version=None):
        prefix = 'picup-dev' if 'picup_dev' in self.tags else 'picup'
        return self.app.parent / f'{prefix}-{version or self.version}-arm64.dmg'

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
        update = self.image(version)
        update.write_bytes(b'final signed DMG fixture')
        # The fixture "image" contents are the app directory as it is now.
        self.image_contents = self.root / 'image-contents'
        if self.image_contents.exists():
            shutil.rmtree(self.image_contents)
        shutil.copytree(self.app, self.image_contents / self.app.name, symlinks=True)
        if development:
            return
        (self.app.parent / 'update-darwin-arm64.json').write_text(json.dumps({
            'version': version, 'date': '2026-10-26T00:00:00Z',
            'url': f'https://github.com/knothhe/picup/releases/download/v{version}/{update.name}',
            'size': update.stat().st_size, 'signature': base64.b64encode(bytes(64)).decode(),
        }))

    def run_command(self, *args, cwd, capture=False, env=None):
        if args[1:3] == ('env', '-json'):
            return json.dumps(self.environment)
        if args[:2] == ('make', 'build'):
            return None
        if args[1:4] == ('tool', 'mygo', 'build'):
            self.assertNotIn('-skip-dmg', args)
            self.tags = 'picup_dev' if '-debug' in args else ''
            if self.tags:
                self.app = self.root / 'build/dev/dist/darwin-arm64/Picup Dev.app'
            self.make_app()
            return None
        if args[1:3] == ('version', '-m'):
            return '\tbuild\t-tags=' + self.tags + '\n\tbuild\tCGO_ENABLED=' + self.cgo
        if args[:2] == ('/usr/bin/lipo', '-archs'):
            return 'arm64'
        if args[1:3] == ('run', './cmd/verify-update'):
            if self.verify_fails:
                raise package.PackageError('signature verification failed')
            return json.dumps({'version': self.version})
        if args[:2] == ('/usr/bin/hdiutil', 'attach'):
            self.assertEqual(args[2:6], ('-readonly', '-nobrowse', '-noautoopen', '-mountpoint'))
            mount = Path(args[6])
            self.mounts.append(args[7])
            mount.rmdir()
            shutil.copytree(self.image_contents, mount, symlinks=True)
            return ''
        if args[:2] == ('/usr/bin/hdiutil', 'detach'):
            self.detached.append(args[-1])
            return ''
        return self.original_run(*args, cwd=cwd, capture=capture, env=env)

    def patch_commands(self, side_effect=None):
        self.original_run = package.run
        return mock.patch.object(package, 'run', side_effect=side_effect or self.run_command)

    def test_formal_package_outputs_exactly_the_dmg_and_update_feed(self):
        # A stale package on another platform must never become an attachment.
        stale = self.root / 'build/dist/darwin-amd64/Picup.app'
        stale.mkdir(parents=True)
        (stale / 'stale').write_text('old build')
        with self.patch_commands() as commands:
            artifacts = package.package_release(self.root, self.version, self.output)
        self.assertEqual(artifacts, [str(self.output / name) for name in (
            'picup-0.1.0-arm64.dmg', 'update-darwin-arm64.json')])
        self.assertEqual(sorted(path.name for path in self.output.iterdir()),
                         ['picup-0.1.0-arm64.dmg', 'update-darwin-arm64.json'])
        self.assertEqual((self.output / 'picup-0.1.0-arm64.dmg').read_bytes(), b'final signed DMG fixture')
        self.assertEqual(commands.call_args_list[1].args, ('make', 'build', 'BUILD_TAGS='))
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', commands.call_args_list[1].kwargs['env'])
        build = commands.call_args_list[2]
        self.assertEqual(build.args, ('go', 'tool', 'mygo', 'build'))
        self.assertEqual(build.kwargs['env']['MYGO_UPDATER_PRIVATE_KEY'], 'private-test-key')
        self.assertEqual(build.kwargs['env']['GOWORK'], '/patched/go.work')
        # The patch preflight itself never sees the update signing key.
        self.assertEqual(len(self.patched_calls), 1)
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', self.patched_calls[0])
        verify = next(call for call in commands.call_args_list if call.args[1:3] == ('run', './cmd/verify-update'))
        self.assertEqual(verify.args[3:], (
            '-feed', str(self.app.parent / 'update-darwin-arm64.json'),
            '-archive', str(self.image()), '-version', self.version, '-arch', 'arm64',
            '-public-key', self.data['updates']['publicKey']))
        self.assertEqual(self.mounts, [str(self.image())])
        self.assertEqual(len(self.detached), 1)
        if package.sys.platform == 'darwin':
            self.assertEqual(build.kwargs['env']['CGO_ENABLED'], '1')
            self.assertEqual(commands.call_args_list[1].kwargs['env']['CGO_ENABLED'], '1')

    def test_disk_image_must_contain_the_exact_validated_app_and_always_detaches(self):
        self.make_app()
        for relative in ('Contents/MacOS/Picup', 'Contents/Resources/picup', 'Contents/Info.plist',
                         'Contents/Resources/THIRD_PARTY_NOTICES.txt'):
            with self.subTest(file=relative):
                self.make_app()
                (self.image_contents / 'Picup.app' / relative).write_bytes(b'different bytes')
                self.detached.clear()
                with self.patch_commands():
                    with self.assertRaisesRegex(package.PackageError, 'different application file'):
                        package.verify_disk_image(self.image(), self.app, self.root)
                self.assertEqual(len(self.detached), 1)
        self.make_app()
        shutil.rmtree(self.image_contents / 'Picup.app')
        with self.patch_commands():
            with self.assertRaisesRegex(package.PackageError, 'does not contain Picup.app'):
                package.verify_disk_image(self.image(), self.app, self.root)

    def test_signed_disk_image_verifies_image_and_mounted_app(self):
        self.make_app()
        signing = mock.sentinel.signing
        with self.patch_commands():
            with mock.patch.object(package, 'verify_signed_disk_image') as image, \
                    mock.patch.object(package, 'verify_disk_image_application') as application:
                package.verify_disk_image(self.image(), self.app, self.root, signing)
        image.assert_called_once_with(self.image(), signing)
        mounted = application.call_args.args[0]
        self.assertEqual(mounted.name, 'Picup.app')
        self.assertEqual(application.call_args.args[1:], (signing, 'Picup', 'picup'))

    def test_manifest_mismatch_stops_before_build(self):
        with self.patch_commands() as commands:
            with self.assertRaisesRegex(package.PackageError, 'mygo.json version'):
                package.package_release(self.root, '0.2.0', self.output)
        commands.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_rejects_cross_compilation_and_non_macos_host(self):
        for change in ({'GOOS': 'linux'}, {'GOARCH': 'amd64'},
                       {'GOHOSTOS': 'linux', 'GOOS': 'linux'}):
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
            if args[1:] == ('tool', 'mygo', 'build'):
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
        expected = [str(self.output / 'picup-0.1.0-arm64.dmg'), str(self.output / 'update-darwin-arm64.json')]
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
        self.assertEqual(artifacts, [str(self.output / 'picup-dev-0.1.0-arm64-ci-unsigned.dmg')])
        self.assertEqual([path.name for path in self.output.iterdir()], ['picup-dev-0.1.0-arm64-ci-unsigned.dmg'])
        build = commands.call_args_list[2]
        self.assertIn('-debug', build.args)
        self.assertEqual(build.kwargs['env']['MYGO_UPDATER_PRIVATE_KEY'], '')
        for call in (commands.call_args_list[1], build):
            self.assertEqual(call.kwargs['env']['CI'], '1')
            self.assertTrue(all(variable not in call.kwargs['env']
                                for variable in package.SIGNING_VARIABLES))
        self.assertNotIn('-skip-dmg', build.args)
        self.assertEqual(build.kwargs['env']['GOWORK'], '/patched/go.work')
        self.assertFalse(any('./cmd/verify-update' in call.args for call in commands.call_args_list))
        self.assertEqual(self.mounts, [str(self.image())])
        self.assertEqual(len(self.detached), 1)

    def test_default_local_signer_is_loaded_only_into_mygo_environment(self):
        key = self.home / 'Library/Application Support/picup-release/mygo-update.key'
        key.parent.mkdir(parents=True)
        key.write_text('local-test-private-key\n')
        key.chmod(0o600)
        with mock.patch.dict(os.environ):
            os.environ.pop('MYGO_UPDATER_PRIVATE_KEY', None)
            with self.patch_commands() as commands:
                package.package_release(self.root, self.version, self.output)
            self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', os.environ)
        build = next(call for call in commands.call_args_list if call.args[1:4] == ('tool', 'mygo', 'build'))
        self.assertEqual(build.kwargs['env']['MYGO_UPDATER_PRIVATE_KEY'], 'local-test-private-key')
        self.assertTrue(all('MYGO_UPDATER_PRIVATE_KEY' not in (call.kwargs.get('env') or {})
                            for call in commands.call_args_list if call != build))

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

    def test_update_metadata_and_signature_must_match_exact_dmg(self):
        self.make_app()
        feed = self.app.parent / 'update-darwin-arm64.json'
        baseline = json.loads(feed.read_text())
        for change in ({'version': '0.0.9'}, {'size': 1}, {'signature': 1},
                       {'url': baseline['url'].replace('knothhe/picup', 'other/app')},
                       {'url': baseline['url'] + '?x=1'},
                       {'url': baseline['url'].replace('.dmg', '.tar.gz')},
                       {'url': baseline['url'].replace('https:', 'http:')},
                       {'deltas': [{'from': '0.0.9'}]}, {'previous': [{'version': '0.0.9'}]},
                       {'date': 'yesterday'}, {'date': '2026-10-26T00:00:00'}):
            with self.subTest(change=change):
                feed.write_text(json.dumps(dict(baseline, **change)))
                with self.patch_commands():
                    with self.assertRaises(package.PackageError):
                        package.validate_update_files(self.root, self.data, self.version,
                                                      'arm64', self.image(), 'go')
        feed.write_text(json.dumps(baseline))
        self.verify_fails = True
        with self.patch_commands():
            with self.assertRaisesRegex(package.PackageError, 'signature verification failed'):
                package.validate_update_files(self.root, self.data, self.version, 'arm64', self.image(), 'go')
        self.verify_fails = False
        def report(*args, **kwargs):
            if args[1:3] == ('run', './cmd/verify-update'):
                return 'not json'
            return self.run_command(*args, **kwargs)
        with self.patch_commands(report):
            with self.assertRaisesRegex(package.PackageError, 'JSON report'):
                package.validate_update_files(self.root, self.data, self.version, 'arm64', self.image(), 'go')
        with self.patch_commands():
            self.assertEqual(package.validate_update_files(self.root, self.data, self.version, 'arm64',
                                                           self.image(), 'go'),
                             [self.image(), feed])


if __name__ == '__main__':
    unittest.main()
