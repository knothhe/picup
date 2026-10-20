"""Test installer validation and ZIP integrity without building or publishing."""
import contextlib
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
        self.data = {'name': 'Picup', 'version': self.version, 'out': 'build/dist'}
        (self.root / 'mygo.json').write_text(json.dumps(self.data))
        self.app = self.root / 'build/dist/darwin-arm64/Picup.app'
        self.output = self.root / 'build/release'
        self.environment = {'GOHOSTOS': 'darwin', 'GOHOSTARCH': 'arm64', 'GOOS': 'darwin', 'GOARCH': 'arm64'}

    def make_app(self, version=None, cli_version=None):
        version = version or self.version
        contents = self.app / 'Contents'
        (contents / 'MacOS').mkdir(parents=True, exist_ok=True)
        (contents / 'Resources').mkdir(exist_ok=True)
        (contents / 'Info.plist').write_bytes(plistlib.dumps({
            'CFBundleVersion': version, 'CFBundleShortVersionString': version,
            'CFBundleExecutable': 'Picup',
        }))
        gui = contents / 'MacOS/Picup'
        gui.write_bytes(b'GUI executable fixture')
        gui.chmod(0o755)
        cli = contents / 'Resources/picup'
        cli.write_text(f'#!/bin/sh\nprintf "picup {cli_version or version}\\n"\n')
        cli.chmod(0o755)
        (contents / 'Resources/icon').write_bytes(b'icon fixture')
        framework = contents / 'Frameworks/Example.framework/Versions/A'
        framework.mkdir(parents=True, exist_ok=True)
        (framework / 'Example').write_bytes(b'framework fixture')
        current = framework.parent / 'Current'
        if not current.is_symlink():
            current.symlink_to('A', target_is_directory=True)
        binary = framework.parent.parent / 'Example'
        if not binary.is_symlink():
            binary.symlink_to('Versions/Current/Example')

    def run_command(self, *args, cwd, capture=False):
        if args[1:3] == ('env', '-json'):
            return json.dumps(self.environment)
        if args == ('make', 'build'):
            return None
        if args[1:] == ('tool', 'mygo', 'build', '-skip-dmg'):
            self.make_app()
            return None
        return self.original_run(*args, cwd=cwd, capture=capture)

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
        self.assertEqual(artifacts, [str(self.output / 'Picup-0.1.0-darwin-arm64.zip')])
        self.assertEqual(commands.call_args_list[1], mock.call('make', 'build', cwd=self.root))
        self.assertEqual(commands.call_args_list[2], mock.call('go', 'tool', 'mygo', 'build', '-skip-dmg', cwd=self.root))
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
        def fail_build(*args, cwd, capture=False):
            if args == ('make', 'build'):
                raise package.PackageError('build failed')
            return self.run_command(*args, cwd=cwd, capture=capture)
        with self.patch_commands(fail_build):
            with self.assertRaisesRegex(package.PackageError, 'build failed'):
                package.package_release(self.root, self.version, self.output)
        self.assertFalse(self.output.exists())

    def test_packaging_failure_never_packages_stale_app(self):
        self.make_app()
        def fail_package(*args, cwd, capture=False):
            if args[1:] == ('tool', 'mygo', 'build', '-skip-dmg'):
                raise package.PackageError('packaging failed')
            return self.run_command(*args, cwd=cwd, capture=capture)
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
        build.assert_called_once_with(Path.cwd(), self.version, str(self.output))


if __name__ == '__main__':
    unittest.main()
