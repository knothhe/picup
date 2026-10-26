"""Test installer validation and ZIP integrity without building or publishing."""
import contextlib
import base64
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import stat
import tarfile
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

    def make_app(self, version=None, cli_version=None):
        version = version or self.version
        contents = self.app / 'Contents'
        (contents / 'MacOS').mkdir(parents=True, exist_ok=True)
        (contents / 'Resources').mkdir(exist_ok=True)
        (contents / 'Info.plist').write_bytes(plistlib.dumps({
            'CFBundleVersion': version, 'CFBundleShortVersionString': version,
            'CFBundleExecutable': 'Picup', 'CFBundleIdentifier': 'com.knothhe.picup',
        }))
        gui = contents / 'MacOS/Picup'
        gui.write_bytes(b'GUI executable fixture')
        gui.chmod(0o755)
        cli = contents / 'Resources/picup'
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
        update = self.app.parent / f'picup-{version}-darwin-arm64.tar.gz'
        with tarfile.open(update, 'w:gz') as archive:
            archive.add(self.app, arcname='Picup.app')
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
        if args[1:5] == ('tool', 'mygo', 'build', '-skip-dmg'):
            self.tags = 'picup_dev' if '-debug' in args else ''
            self.make_app()
            return None
        if args[1:3] == ('version', '-m'):
            return '\tbuild\t-tags=' + self.tags
        if args[:2] == ('/usr/bin/lipo', '-archs'):
            return 'arm64'
        if args[1:3] == ('run', './cmd/verify-update'):
            if args[-1] == 'invalid':
                raise package.PackageError('signature verification failed')
            return None
        return self.original_run(*args, cwd=cwd, capture=capture, env=env)

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
            'Picup-0.1.0-darwin-arm64.zip', 'picup-0.1.0-darwin-arm64.tar.gz', 'update-darwin-arm64.json')])
        self.assertEqual(commands.call_args_list[1].args, ('make', 'build', 'BUILD_TAGS='))
        self.assertNotIn('MYGO_UPDATER_PRIVATE_KEY', commands.call_args_list[1].kwargs['env'])
        build = commands.call_args_list[2]
        self.assertEqual(build.args, ('go', 'tool', 'mygo', 'build', '-skip-dmg'))
        self.assertEqual(build.kwargs['env']['MYGO_UPDATER_PRIVATE_KEY'], 'private-test-key')
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
            if args[1:] == ('tool', 'mygo', 'build', '-skip-dmg'):
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
        with mock.patch.dict(os.environ, {'MYGO_UPDATER_PRIVATE_KEY': ''}):
            with self.patch_commands() as commands:
                with mock.patch.object(package, 'signing_environment', side_effect=AssertionError('unsigned builds must not load a key')):
                    artifacts = package.package_release(self.root, self.version, self.output, unsigned=True)
        self.assertEqual(artifacts, [str(self.output / 'Picup-0.1.0-darwin-arm64-ci-unsigned.zip')])
        build = commands.call_args_list[2]
        self.assertIn('-debug', build.args)
        self.assertEqual(build.kwargs['env']['MYGO_UPDATER_PRIVATE_KEY'], '')
        self.assertFalse(any('./cmd/verify-update' in call.args for call in commands.call_args_list))

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

    def test_update_metadata_and_signature_must_match_exact_app(self):
        self.make_app()
        feed = self.app.parent / 'update-darwin-arm64.json'
        baseline = json.loads(feed.read_text())
        for change in ({'version': '0.0.9'}, {'size': 1}, {'signature': 'invalid'},
                       {'url': baseline['url'].replace('knothhe/picup', 'other/app')},
                       {'deltas': [{'from': '0.0.9'}]}, {'date': 'yesterday'}):
            with self.subTest(change=change):
                feed.write_text(json.dumps(dict(baseline, **change)))
                with self.patch_commands():
                    with self.assertRaises(package.PackageError):
                        package.validate_update_files(self.root, self.data, self.version,
                                                      'darwin-arm64', self.app, 'go')
        feed.write_text(json.dumps(baseline))
        (self.app / 'Contents/MacOS/Picup').write_bytes(b'different compiled app')
        with self.patch_commands():
            with self.assertRaisesRegex(package.PackageError, 'different application file'):
                package.validate_update_files(self.root, self.data, self.version,
                                              'darwin-arm64', self.app, 'go')


if __name__ == '__main__':
    unittest.main()
