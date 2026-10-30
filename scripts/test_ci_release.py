"""Exercise release source, native provenance and all-or-nothing feed publication."""
import base64
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

import ci_release


class CIReleaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        original = Path.cwd()
        os.chdir(self.root)
        self.addCleanup(os.chdir, original)
        environment = mock.patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/picup',
            'GITHUB_RUN_ID': '12345', 'GITHUB_RUN_ATTEMPT': '2', 'GITHUB_JOB': 'macos'})
        environment.start()
        self.addCleanup(environment.stop)
        self.git('init', '-b', 'main')
        self.git('config', 'user.name', 'CI Test')
        self.git('config', 'user.email', 'ci@example.com')
        self.git('config', 'commit.gpgsign', 'false')
        self.git('config', 'tag.gpgsign', 'false')
        self.git('remote', 'add', 'origin', 'https://github.com/example/picup.git')
        self.version, self.tag = '1.2.3', 'v1.2.3'
        self.manifest = {'name': 'Picup', 'version': self.version,
            'updates': {'github': 'example/picup', 'publicKey': base64.b64encode(bytes(32)).decode()}}
        (self.root / 'mygo.json').write_text(json.dumps(self.manifest))
        (self.root / '.release.json').write_text(json.dumps({'version_file': 'mygo.json',
            'branch': 'main', 'remote': 'origin', 'tag_prefix': 'v'}))
        (self.root / '.gitignore').write_text('/build/\n')
        self.git('add', 'mygo.json', '.release.json', '.gitignore')
        self.git('commit', '-m', 'feat: add release fixtures')
        self.commit = self.git('rev-parse', 'HEAD')
        self.git('tag', '-a', self.tag, '-m', 'Release ' + self.tag)
        self.git('update-ref', 'refs/remotes/origin/main', self.commit)
        self.inputs = self.root / 'build/downloads'
        self.directory = self.root / 'build/release'
        self.names = ci_release.expected_names(self.version)
        self.attempts = {target: 2 for target in ci_release.TARGETS}
        for target in ci_release.TARGETS:
            directory = self.inputs / target
            directory.mkdir(parents=True)
            name = ci_release.expected_names(self.version, (target,))[0]
            content = ('native package fixture for ' + target).encode()
            (directory / name).write_bytes(content)
            feed = {'schema': 1, 'version': self.version, 'date': '2026-10-28T12:00:00Z',
                'platforms': {target: {'url': f'https://github.com/example/picup/releases/download/{self.tag}/{name}',
                    'size': len(content), 'sha256': hashlib.sha256(content).hexdigest(),
                    'signature': base64.b64encode(bytes(64)).decode()}}}
            (directory / ci_release.FEED).write_text(json.dumps(feed))
        # These tests isolate provenance/aggregation; real ZIP cryptographic and
        # executable identity verification is exercised by the Go updater tests.
        verifier = mock.patch.object(ci_release, 'verify_archives')
        self.archive_verifier = verifier.start()
        self.addCleanup(verifier.stop)

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True,
                                       stderr=subprocess.STDOUT).strip()

    def record(self, target='darwin-arm64', mode='update-only'):
        with mock.patch.dict(os.environ, {'GITHUB_JOB': ci_release.BUILD_JOBS[target]}):
            return ci_release.record(self.tag, self.inputs / target, target, mode)

    def record_both(self, mode='update-only'):
        for target in ci_release.TARGETS:
            self.record(target, mode if target.startswith('darwin-') else 'update-only')

    def verify(self, attempt=2, mode='update-only'):
        return ci_release.verify_record(self.tag, self.inputs / 'darwin-arm64', 'darwin-arm64', mode, attempt)

    def aggregate(self, mode='update-only'):
        return ci_release.aggregate(self.tag, self.inputs, self.directory, mode, self.attempts)

    def feed(self, target='darwin-arm64'):
        return json.loads((self.inputs / target / ci_release.FEED).read_text())

    def save_feed(self, feed, target='darwin-arm64'):
        (self.inputs / target / ci_release.FEED).write_text(json.dumps(feed))

    def test_validate_emits_exact_source_without_mutation(self):
        output = self.root / 'build/output.txt'
        output.write_text('previous=value\n')
        with mock.patch('sys.stdout', new=io.StringIO()) as stdout:
            self.assertEqual(ci_release.main(['validate', '--tag', self.tag, '--github-output', str(output)]), 0)
        expected = {'tag': self.tag, 'version': self.version, 'commit': self.commit}
        self.assertEqual(json.loads(stdout.getvalue()), expected)
        self.assertEqual(output.read_text(), 'previous=value\n' + ''.join(f'{k}={v}\n' for k, v in expected.items()))
        self.assertEqual(self.git('status', '--porcelain'), '')

    def test_invalid_tag_wrong_head_main_ancestry_and_repository_are_rejected(self):
        for tag in ('v01.2.3', 'other1.2.3'):
            with self.subTest(tag=tag), self.assertRaises(ci_release.release.ReleaseError):
                ci_release.identity(tag)
        self.git('tag', 'v1.2.4')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'annotated'):
            ci_release.identity('v1.2.4')
        with mock.patch.dict(os.environ, {'GITHUB_REPOSITORY': 'another/picup'}):
            with self.assertRaisesRegex(ci_release.release.ReleaseError, 'repository'):
                ci_release.identity(self.tag)
        self.manifest['version'] = '1.2.4'
        (self.root / 'mygo.json').write_text(json.dumps(self.manifest))
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'version or identity'):
            ci_release.identity(self.tag)
        self.git('add', 'mygo.json')
        self.git('commit', '-m', 'Another source commit')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'HEAD'):
            ci_release.identity(self.tag)
        self.git('tag', '-a', 'v1.2.4-new', '-m', 'Release outside main')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'ancestor'):
            ci_release.identity('v1.2.4-new')

    def test_record_binds_native_identity_and_bytes(self):
        for target in ci_release.TARGETS:
            data = self.record(target)
            self.assertEqual(data['os'] + '-' + data['arch'], target)
            self.assertEqual(data['source'], {'run_id': '12345', 'run_attempt': 2, 'job': ci_release.BUILD_JOBS[target]})
            self.assertEqual([item['name'] for item in data['files']], list(ci_release.expected_names(self.version, (target,))))
            ci_release.verify_record(self.tag, self.inputs / target, target, 'update-only', 2)

    def test_macos_signing_mode_cannot_be_relabelled(self):
        for mode in ci_release.SIGNING_MODES:
            self.record(mode=mode)
            self.verify(mode=mode)
            for other in set(ci_release.SIGNING_MODES) - {mode}:
                with self.assertRaisesRegex(ci_release.release.ReleaseError, 'identity'):
                    self.verify(mode=other)
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'Linux uses'):
            self.record('linux-amd64', 'fixed-identity')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'job does not match'):
            ci_release.record(self.tag, self.inputs / 'linux-amd64', 'linux-amd64', 'update-only')

    def test_missing_extra_empty_and_symlinked_assets_are_rejected(self):
        target = self.inputs / 'darwin-arm64' / self.names[0]
        content = target.read_bytes()
        target.unlink()
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'missing or unexpected'):
            self.record()
        target.write_bytes(b'')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'empty'):
            self.record()
        target.unlink()
        linked = self.root / 'build/elsewhere.zip'
        linked.write_bytes(content)
        target.symlink_to(linked)
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'symlinked'):
            self.record()
        target.unlink()
        target.write_bytes(content)
        (target.parent / 'legacy.tar.gz').write_bytes(content)
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'unexpected'):
            self.record()

    def test_feed_target_version_date_hash_size_signature_and_url_are_checked(self):
        original = self.feed()
        for change in ({'version': '1.2.4'}, {'schema': True}, {'date': '2026-10-28'},
                       {'platforms': {}}, {'deltas': []}):
            self.save_feed({**original, **change})
            with self.subTest(change=change), self.assertRaises(ci_release.release.ReleaseError):
                self.record()
        for change in ({'size': True}, {'size': 1}, {'sha256': 'f' * 64}, {'signature': 'invalid'},
                       {'url': original['platforms']['darwin-arm64']['url'] + '?token=bad'},
                       {'url': original['platforms']['darwin-arm64']['url'].replace('example/picup', 'other/picup')}):
            feed = copy.deepcopy(original)
            feed['platforms']['darwin-arm64'].update(change)
            self.save_feed(feed)
            with self.subTest(change=change), self.assertRaises(ci_release.release.ReleaseError):
                self.record()

    def test_record_replay_tampering_and_wrong_attempt_are_rejected(self):
        original = self.record()
        for change in ({'arch': 'amd64'}, {'channel': 'dev'}, {'commit': 'a' * 40}, {'schema': True},
                       {'source': {'run_id': 'another-run', 'run_attempt': 2, 'job': 'macos'}},
                       {'source': {'run_id': '12345', 'run_attempt': 2, 'job': 'linux'}},
                       {'files': original['files'] + [original['files'][0]]}):
            (self.inputs / 'darwin-arm64' / ci_release.RECORD).write_text(json.dumps({**original, **change}))
            with self.subTest(change=change), self.assertRaises(ci_release.release.ReleaseError):
                self.verify()
        self.record()
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'Invalid build attempt'):
            self.verify(attempt=3)
        path = self.inputs / 'darwin-arm64' / self.names[0]
        path.write_bytes(b'tampered native ZIP')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'hashes differ'):
            self.verify()

    def test_prior_successful_build_attempt_is_reusable_in_the_same_run(self):
        with mock.patch.dict(os.environ, {'GITHUB_RUN_ATTEMPT': '1'}):
            self.record()
        self.verify(attempt=1)
        with self.assertRaises(ci_release.release.ReleaseError):
            self.verify(attempt=2)

    def test_aggregate_produces_only_two_zips_and_one_feed(self):
        self.record_both('fixed-identity')
        selected, _, _, paths = self.aggregate('fixed-identity')
        self.assertEqual(selected['commit'], self.commit)
        self.assertEqual([path.name for path in paths], list(self.names))
        feed = ci_release.validate_feed(self.directory, 'example/picup', self.tag, self.version)
        self.assertEqual(set(feed['platforms']), set(ci_release.TARGETS))
        self.assertEqual(feed['notes'], ci_release.release.release_notes('example/picup', 'v', self.version, self.commit, self.tag))
        self.archive_verifier.assert_called_once_with(self.directory, self.version)

    def test_missing_platform_or_changed_feed_stops_before_aggregation(self):
        self.record_both()
        shutil.rmtree(self.inputs / 'linux-amd64')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'missing or unexpected platforms'):
            self.aggregate()
        self.assertFalse(self.directory.exists())
        self.archive_verifier.assert_not_called()

    def test_other_platform_record_cannot_substitute_for_a_missing_build(self):
        self.record_both()
        mac_record = self.inputs / 'darwin-arm64' / ci_release.RECORD
        shutil.copyfile(mac_record, self.inputs / 'linux-amd64' / ci_release.RECORD)
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'identity'):
            self.aggregate()
        self.assertFalse(self.directory.exists())

    def test_merged_feed_does_not_bypass_final_archive_verification(self):
        self.record_both()
        self.archive_verifier.side_effect = ci_release.release.ReleaseError('invalid Ed25519 signature')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'Ed25519'):
            self.aggregate()

    def public_release(self):
        return {'url': f'https://github.com/example/picup/releases/tag/{self.tag}', 'isDraft': False,
                'assets': [{'name': item['name'], 'size': item['size'], 'digest': 'sha256:' + item['sha256']}
                    for item in ci_release.file_records([self.directory / name for name in self.names])]}

    def mock_publish(self, mutate_remote=None, corrupt=None):
        self.record_both()
        # Build the expected immutable output, then let publish aggregate fresh.
        self.aggregate()
        result = self.public_release()
        if mutate_remote:
            mutate_remote(result)
        shutil.rmtree(self.directory)
        original_run = ci_release.release.run
        def run(*args, **kwargs):
            if args[0] != 'gh':
                return original_run(*args, **kwargs)
            if args[1] == 'api':
                return SimpleNamespace(stdout=json.dumps({'object': {'type': 'tag', 'sha': self.git('rev-parse', 'refs/tags/' + self.tag)}}))
            return SimpleNamespace(stdout=json.dumps(result))
        def download(request, timeout):
            self.assertEqual(timeout, 30)
            self.assertFalse(any(name.lower() == 'authorization' for name, _ in request.header_items()))
            name = request.full_url.rsplit('/', 1)[1]
            data = (self.directory / name).read_bytes()
            if name == corrupt:
                data = b'wrong bytes'
            stream = io.BytesIO(data)
            stream.geturl = lambda: 'https://release-assets.githubusercontent.com/verified-test'
            return stream
        stack = mock.patch.multiple(ci_release.release, published_tags=mock.DEFAULT,
            publish_github_release=mock.DEFAULT, run=mock.DEFAULT)
        mocks = stack.start()
        self.addCleanup(stack.stop)
        mocks['published_tags'].return_value = []
        mocks['run'].side_effect = run
        downloader = mock.patch.object(ci_release, 'urlopen', side_effect=download)
        opened = downloader.start()
        self.addCleanup(downloader.stop)
        return mocks, opened

    def publish(self, notes=None):
        return ci_release.publish(self.tag, self.inputs, self.directory, 'update-only', self.attempts, notes)

    def test_publish_reuses_canonical_notes_and_verifies_all_public_files(self):
        mocks, opened = self.mock_publish()
        notes = self.root / 'build/notes.md'
        notes.write_text(ci_release.release.release_notes('example/picup', 'v', self.version, self.commit, self.tag))
        with mock.patch('sys.stdout', new=io.StringIO()) as stdout:
            self.publish(notes)
        self.assertIn('both platform ZIPs and unified feed verified', stdout.getvalue())
        self.assertEqual(opened.call_count, 3)
        args = mocks['publish_github_release'].call_args.args
        self.assertEqual(args[:4], ('example/picup', self.tag, self.version, notes.read_text()))
        self.assertEqual([path.name for path in args[4]], list(self.names))

    def test_public_byte_corruption_and_remote_digest_are_failures(self):
        _, opened = self.mock_publish(corrupt=self.names[1])
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'Public asset differs'):
            self.publish()
        self.assertEqual(opened.call_count, 2)

    def test_wrong_remote_digest_prevents_public_verification(self):
        _, opened = self.mock_publish(lambda remote: remote['assets'][0].update(digest='sha256:' + 'f' * 64))
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'differs from the verified local file'):
            self.publish()
        opened.assert_not_called()

    def test_existing_extra_asset_blocks_mutation(self):
        mocks, opened = self.mock_publish(lambda remote: remote['assets'].append({'name': 'legacy.tar.gz', 'size': 1}))
        mocks['published_tags'].return_value = [self.tag]
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'unexpected assets'):
            self.publish()
        mocks['publish_github_release'].assert_not_called()
        opened.assert_not_called()

    def test_still_draft_is_not_reported_as_completed(self):
        _, opened = self.mock_publish(lambda remote: remote.update(isDraft=True))
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'still a draft'):
            self.publish()
        opened.assert_not_called()

    def test_mismatched_notes_stop_before_publish(self):
        mocks, _ = self.mock_publish()
        notes = self.root / 'build/notes.md'
        notes.write_text('invented notes')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'canonical'):
            self.publish(notes)
        mocks['publish_github_release'].assert_not_called()


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(__file__).resolve().parent.parent
        self.source = (self.root / '.github/workflows/release.yml').read_text()
        body = self.source.split('\njobs:\n', 1)[1]
        starts = list(re.finditer(r'^  ([a-z-]+):\n', body, re.MULTILINE))
        self.jobs = {match[1]: body[match.start(): starts[index + 1].start() if index + 1 < len(starts) else len(body)]
                     for index, match in enumerate(starts)}

    def test_only_tag_push_release_workflow_remains(self):
        self.assertEqual([path.name for path in (self.root / '.github/workflows').glob('*.yml')], ['release.yml'])
        events = self.source.split('\non:\n', 1)[1].split('\npermissions:', 1)[0]
        self.assertEqual(events.strip(), "push:\n    tags: ['v*']")
        self.assertEqual(set(self.jobs), {'source', 'macos', 'linux', 'publish'})
        self.assertIn('needs: [source, macos, linux]', self.jobs['publish'])
        self.assertNotIn('.tar.gz', self.source)
        self.assertNotIn('update-darwin-', self.source)

    def test_secrets_only_enter_native_packaging_after_checks(self):
        self.assertNotIn('secrets.', self.jobs['source'])
        self.assertNotIn('secrets.', self.jobs['publish'])
        for target in ('macos', 'linux'):
            job = self.jobs[target]
            self.assertLess(job.index('make test'), job.index('secrets.'))
            self.assertIn('go-version-file: go.mod', job)
        self.assertNotIn('secrets.MACOS_', self.jobs['linux'])
        self.assertEqual(self.source.count('contents: write'), 1)
        for action in re.findall(r'uses: (\S+)', self.source):
            self.assertRegex(action, r'@[a-f0-9]{40}$')

    def test_each_macos_mode_only_has_its_required_material(self):
        steps = re.split(r'^      - name: ', self.jobs['macos'], flags=re.MULTILINE)[1:]
        for mode in ci_release.SIGNING_MODES:
            selected = [step for step in steps if f"if: env.SIGNING_MODE == '{mode}'" in step]
            self.assertEqual(len(selected), 1)
            step = selected[0]
            self.assertIn('secrets.MYGO_UPDATER_PRIVATE_KEY', step)
            self.assertEqual('secrets.MACOS_SIGNING_CERTIFICATE' in step, mode != 'update-only')
            self.assertEqual('secrets.MACOS_NOTARY_' in step, mode == 'developer-id')

    def test_publisher_downloads_exact_successful_artifact_ids_and_attempts(self):
        publish = self.jobs['publish']
        for platform in ('macos', 'linux'):
            self.assertIn(f'artifact-ids: ${{{{ needs.{platform}.outputs.artifact_id }}}}', publish)
            self.assertIn(f'needs.{platform}.outputs.attempt', publish)
        self.assertNotIn('pattern:', publish)
        self.assertIn('build/downloads/darwin-arm64', publish)
        self.assertIn('build/downloads/linux-amd64', publish)


if __name__ == '__main__':
    unittest.main()
