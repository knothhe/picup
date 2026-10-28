"""Exercise CI source/provenance gates in temporary repositories, without publishing."""
import base64
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import re
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
            'GITHUB_RUN_ID': '12345', 'GITHUB_RUN_ATTEMPT': '2', 'GITHUB_JOB': 'build'})
        environment.start()
        self.addCleanup(environment.stop)
        self.git('init', '-b', 'main')
        self.git('config', 'user.name', 'CI Test')
        self.git('config', 'user.email', 'ci@example.com')
        self.git('config', 'commit.gpgsign', 'false')
        self.git('config', 'tag.gpgsign', 'false')
        self.git('remote', 'add', 'origin', 'https://github.com/example/picup.git')
        self.version = '1.2.3'
        self.tag = 'v' + self.version
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
        self.directory = self.root / 'build/release'
        self.directory.mkdir(parents=True)
        self.create_assets()

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True,
                                       stderr=subprocess.STDOUT).strip()

    def create_assets(self):
        self.names = ci_release.expected_names(self.version)
        (self.directory / self.names[0]).write_bytes(b'formal app fixture, validated by package_release')
        (self.directory / self.names[1]).write_bytes(b'full signed update archive fixture')
        self.feed = {'version': self.version, 'size': (self.directory / self.names[1]).stat().st_size,
                     'url': f'https://github.com/example/picup/releases/download/{self.tag}/{self.names[1]}',
                     'signature': base64.b64encode(bytes(64)).decode(), 'date': '2026-10-28T12:00:00Z'}
        self.save_feed()

    def save_feed(self):
        (self.directory / self.names[2]).write_text(json.dumps(self.feed))

    def record(self):
        return ci_release.record(self.tag, self.directory, 'update-only')

    def verify(self, attempt=2):
        return ci_release.verify_record(self.tag, self.directory, 'update-only', attempt, 'build')

    def test_validate_emits_exact_tag_version_and_commit_without_mutation(self):
        output = self.root / 'build/output.txt'
        output.write_text('previous=value\n')
        with mock.patch('sys.stdout', new=io.StringIO()) as stdout:
            self.assertEqual(ci_release.main(['validate', '--tag', self.tag, '--github-output', str(output)]), 0)
        expected = {'tag': self.tag, 'version': self.version, 'commit': self.commit}
        self.assertEqual(json.loads(stdout.getvalue()), expected)
        self.assertEqual(output.read_text(), 'previous=value\n' + ''.join(f'{key}={value}\n' for key, value in expected.items()))
        self.assertEqual(self.git('status', '--porcelain'), '')

    def test_validate_rejects_non_semver_lightweight_and_wrong_head(self):
        for tag in ('v01.2.3', 'other1.2.3'):
            with self.subTest(tag=tag), self.assertRaises(ci_release.release.ReleaseError):
                ci_release.identity(tag)
        self.git('tag', 'v1.2.4')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'annotated'):
            ci_release.identity('v1.2.4')
        (self.root / 'source.txt').write_text('another commit')
        self.git('add', 'source.txt')
        self.git('commit', '-m', 'Change source')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'HEAD'):
            ci_release.identity(self.tag)

    def test_validate_rejects_tag_outside_main_and_changed_version_or_repository(self):
        with mock.patch.dict(os.environ, {'GITHUB_REPOSITORY': 'another/picup'}):
            with self.assertRaisesRegex(ci_release.release.ReleaseError, 'repository'):
                ci_release.identity(self.tag)
        self.manifest['version'] = '1.2.4'
        (self.root / 'mygo.json').write_text(json.dumps(self.manifest))
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'version or identity'):
            ci_release.identity(self.tag)
        self.git('add', 'mygo.json')
        self.git('commit', '-m', 'Change version on another branch')
        self.git('tag', '-a', 'v1.2.4', '-m', 'Release v1.2.4')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'ancestor'):
            ci_release.identity('v1.2.4')

    def test_record_binds_exact_bytes_identity_and_ci_source(self):
        data = self.record()
        self.assertEqual(data['commit'], self.commit)
        self.assertEqual(data['source'], {'run_id': '12345', 'run_attempt': 2, 'job': 'build'})
        self.assertEqual([item['name'] for item in data['files']], list(self.names))
        for item in data['files']:
            path = self.directory / item['name']
            self.assertEqual(item['size'], path.stat().st_size)
            self.assertEqual(item['sha256'], hashlib.sha256(path.read_bytes()).hexdigest())
        self.verify()

    def test_fixed_and_compatibility_modes_are_preserved_and_cannot_be_relabelled(self):
        for mode in ci_release.SIGNING_MODES:
            with self.subTest(mode=mode):
                data=ci_release.record(self.tag,self.directory,mode)
                self.assertEqual(data['signing_mode'],mode)
                ci_release.verify_record(self.tag,self.directory,mode,2,'build')
                for other in set(ci_release.SIGNING_MODES)-{mode}:
                    with self.assertRaisesRegex(ci_release.release.ReleaseError,'identity'):
                        ci_release.verify_record(self.tag,self.directory,other,2,'build')
        with self.assertRaisesRegex(ci_release.release.ReleaseError,'Unknown CI signing mode'):
            ci_release.record(self.tag,self.directory,'automatic')

    def test_cli_accepts_explicit_fixed_identity_mode(self):
        with mock.patch('sys.stdout',new=io.StringIO()) as stdout:
            self.assertEqual(ci_release.main(['record','--tag',self.tag,'--directory',str(self.directory),
                                              '--signing-mode','fixed-identity']),0)
        self.assertEqual(json.loads(stdout.getvalue())['signing_mode'],'fixed-identity')

    def test_missing_extra_empty_and_symlinked_assets_are_rejected(self):
        target = self.directory / self.names[0]
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
        extra = self.directory / 'old-darwin-amd64.zip'
        extra.write_bytes(content)
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'unexpected'):
            self.record()

    def test_feed_must_reference_this_full_release_archive(self):
        original = copy.deepcopy(self.feed)
        for change in ({'version': '1.2.4'}, {'size': 1}, {'deltas': [{'url': 'delta'}]},
                       {'url': self.feed['url'].replace('example/picup', 'another/picup')},
                       {'url': self.feed['url'] + '?token=unexpected'}, {'signature': 'invalid'},
                       {'date': '2026-10-28'}):
            with self.subTest(change=change):
                self.feed = {**original, **change}
                self.save_feed()
                with self.assertRaises(ci_release.release.ReleaseError):
                    self.record()

    def test_record_tampering_wrong_arch_commit_version_signing_and_source_are_rejected(self):
        original = self.record()
        changes = ({'arch': 'amd64'}, {'channel': 'dev'}, {'os': 'linux'}, {'commit': 'a' * 40},
                   {'tag': 'v1.2.4'}, {'version': '1.2.4'}, {'signing_mode': 'developer-id'}, {'schema': True},
                   {'source': {'run_id': 'another-run', 'run_attempt': 2, 'job': 'build'}},
                   {'source': {'run_id': '12345', 'run_attempt': 1, 'job': 'build'}},
                   {'source': {'run_id': '12345', 'run_attempt': 2, 'job': 'checks'}},
                   {'files': original['files'] + [original['files'][0]]})
        for change in changes:
            with self.subTest(change=change):
                (self.directory / ci_release.RECORD).write_text(json.dumps({**original, **change}))
                with self.assertRaises(ci_release.release.ReleaseError):
                    self.verify()

    def test_content_tampering_and_future_build_attempt_are_rejected(self):
        self.record()
        path = self.directory / self.names[0]
        content = path.read_bytes()
        path.write_bytes(bytes([content[0] ^ 1]) + content[1:])
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'hashes differ'):
            self.verify()
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'Invalid build attempt'):
            self.verify(attempt=3)

    def test_prior_build_attempt_is_reusable_in_same_run_with_explicit_attempt(self):
        with mock.patch.dict(os.environ, {'GITHUB_RUN_ATTEMPT': '1'}):
            self.record()
        self.verify(attempt=1)
        with self.assertRaises(ci_release.release.ReleaseError):
            self.verify(attempt=2)

    def public_release(self):
        return {'url': f'https://github.com/example/picup/releases/tag/{self.tag}', 'isDraft': False,
                'assets': [{'name': item['name'], 'size': item['size'], 'digest': 'sha256:' + item['sha256']}
                           for item in ci_release.file_records([self.directory / name for name in self.names])]}

    def mock_publish(self, remote=None, corrupt=None):
        self.record()
        result = remote or self.public_release()
        original_run = ci_release.release.run
        def run(*args, **kwargs):
            if args[0] != 'gh':
                return original_run(*args, **kwargs)
            if args[1] == 'api':
                return SimpleNamespace(stdout=json.dumps({'object': {'type': 'tag', 'sha': self.git('rev-parse', 'refs/tags/' + self.tag)}}))
            self.assertEqual(args[1:3], ('release', 'view'))
            return SimpleNamespace(stdout=json.dumps(result))
        def download(request, timeout):
            self.assertEqual(timeout, 30)
            self.assertFalse(any(name.lower() == 'authorization' for name, _ in request.header_items()))
            self.assertTrue(request.full_url.startswith(f'https://github.com/example/picup/releases/download/{self.tag}/'))
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

    def test_publish_reuses_canonical_notes_and_verifies_all_public_files(self):
        mocks, opened = self.mock_publish()
        notes = self.root / 'build/notes.md'
        notes.write_text(ci_release.release.release_notes('example/picup', 'v', self.version, self.commit, self.tag))
        with mock.patch('sys.stdout', new=io.StringIO()) as stdout:
            ci_release.publish(self.tag, self.directory, 'update-only', 2, notes_file=notes)
        self.assertIn('all three arm64 assets verified', stdout.getvalue())
        self.assertEqual(opened.call_count, 3)
        args = mocks['publish_github_release'].call_args.args
        self.assertEqual(args[:4], ('example/picup', self.tag, self.version, notes.read_text()))
        self.assertEqual([path.name for path in args[4]], list(self.names))

    def test_public_byte_corruption_is_failure(self):
        mocks, opened = self.mock_publish(corrupt=self.names[1])
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'Public asset differs'):
            ci_release.publish(self.tag, self.directory, 'update-only', 2)
        self.assertEqual(mocks['publish_github_release'].call_count, 1)
        self.assertEqual(opened.call_count, 2)

    def test_wrong_remote_digest_is_not_a_completed_release(self):
        remote = self.public_release()
        remote['assets'][0]['digest'] = 'sha256:' + 'f' * 64
        _, opened = self.mock_publish(remote=remote)
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'differs from the verified local file'):
            ci_release.publish(self.tag, self.directory, 'update-only', 2)
        opened.assert_not_called()

    def test_existing_release_extra_asset_blocks_mutation(self):
        remote = self.public_release()
        remote['assets'].append({'name': 'old-darwin-amd64.zip', 'size': 1, 'digest': 'sha256:' + 'a' * 64})
        mocks, opened = self.mock_publish(remote=remote)
        mocks['published_tags'].return_value = [self.tag]
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'unexpected assets'):
            ci_release.publish(self.tag, self.directory, 'update-only', 2)
        mocks['publish_github_release'].assert_not_called()
        opened.assert_not_called()

    def test_still_draft_is_not_reported_as_a_completed_release(self):
        remote = self.public_release()
        remote['isDraft'] = True
        _, opened = self.mock_publish(remote=remote)
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'still a draft'):
            ci_release.publish(self.tag, self.directory, 'update-only', 2)
        opened.assert_not_called()

    def test_mismatched_notes_or_remote_tag_stops_before_publish(self):
        mocks, _ = self.mock_publish()
        notes = self.root / 'build/notes.md'
        notes.write_text('invented notes')
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'canonical'):
            ci_release.publish(self.tag, self.directory, 'update-only', 2, notes_file=notes)
        mocks['publish_github_release'].assert_not_called()
        original = mocks['run'].side_effect
        def changed_tag(*args, **kwargs):
            if args[:2] == ('gh', 'api'):
                return SimpleNamespace(stdout=json.dumps({'object': {'type': 'tag', 'sha': 'f' * 40}}))
            return original(*args, **kwargs)
        mocks['run'].side_effect = changed_tag
        with self.assertRaisesRegex(ci_release.release.ReleaseError, 'Remote annotated tag differs'):
            ci_release.publish(self.tag, self.directory, 'update-only', 2)
        mocks['publish_github_release'].assert_not_called()


class WorkflowSigningTests(unittest.TestCase):
    """Keep private-material boundaries and selected modes aligned across jobs."""

    def setUp(self):
        root=Path(__file__).resolve().parent.parent
        self.workflows={name:(root/'.github/workflows'/name).read_text()
                        for name in ('macos.yml','release.yml')}

    def jobs(self,workflow):
        body=workflow.split('\njobs:\n',1)[1]
        starts=list(re.finditer(r'^  ([a-z-]+):\n',body,re.MULTILINE))
        return {match[1]:body[match.start():starts[index+1].start() if index+1<len(starts) else len(body)]
                for index,match in enumerate(starts)}

    def steps(self,job):
        return re.split(r'^      - name: ',job,flags=re.MULTILINE)[1:]

    def test_daily_private_material_is_only_in_manual_signed_main_job(self):
        jobs=self.jobs(self.workflows['macos.yml'])
        self.assertEqual(set(jobs),{'macos','signed-updates'})
        self.assertNotIn('secrets.',jobs['macos'])
        signed=jobs['signed-updates']
        self.assertIn("    if: github.event_name == 'workflow_dispatch' && inputs.signed_updates && github.ref == 'refs/heads/main'\n",signed)
        self.assertIn('    needs: macos\n',signed)
        self.assertIn('SIGNING_MODE: ${{ needs.macos.outputs.signing_mode }}',signed)
        self.assertIn('Picup-signed-updates-${{ needs.macos.outputs.signing_mode }}-',signed)
        release=self.jobs(self.workflows['release.yml'])
        self.assertNotIn('secrets.',release['checks'])
        self.assertNotIn('secrets.',release['publish'])
        self.assertIn('SIGNING_MODE: ${{ needs.checks.outputs.signing_mode }}',release['build'])
        self.assertIn('SIGNING_MODE: ${{ needs.checks.outputs.signing_mode }}',release['publish'])
        self.assertIn('--signing-mode "$SIGNING_MODE"',release['build'])
        self.assertIn('--signing-mode "$SIGNING_MODE"',release['publish'])

    def test_modes_inject_only_their_required_secrets_and_pass_the_same_mode_to_wrapper(self):
        for name,source in self.workflows.items():
            jobs=self.jobs(source)
            job=jobs['signed-updates' if name=='macos.yml' else 'build']
            private=[step for step in self.steps(job) if 'secrets.' in step]
            self.assertEqual(len(private),3)
            for mode in ci_release.SIGNING_MODES:
                with self.subTest(workflow=name,mode=mode):
                    selected=[step for step in private if f"if: env.SIGNING_MODE == '{mode}'" in step]
                    self.assertEqual(len(selected),1)
                    step=selected[0]
                    self.assertIn('secrets.MYGO_UPDATER_PRIVATE_KEY',step)
                    if mode=='update-only':
                        self.assertIn('run: make package-release',step)
                        self.assertNotIn('secrets.MACOS_',step)
                    else:
                        self.assertIn('secrets.MACOS_SIGNING_CERTIFICATE',step)
                        self.assertIn('secrets.MACOS_SIGNING_CERTIFICATE_PASSWORD',step)
                        self.assertIn(f'--mode {mode} -- make package-release',step)
                    self.assertEqual('secrets.MACOS_NOTARY_' in step,mode=='developer-id')

    def test_mode_defaults_precedence_and_unknown_value_failure(self):
        expression="(vars.PICUP_APPLE_DISTRIBUTION == 'true' || inputs.apple_distribution == true) && 'developer-id' || inputs.signing_mode || vars.PICUP_SIGNING_MODE || 'fixed-identity'"
        for name,source in self.workflows.items():
            with self.subTest(workflow=name):
                self.assertIn(expression,source)
                self.assertIn('options: [fixed-identity, update-only, developer-id]\n        default: fixed-identity',source)
                jobs=self.jobs(source)
                check=jobs['macos' if name=='macos.yml' else 'checks']
                step=next(step for step in self.steps(check) if 'id: mode' in step)
                script=step.split('        run: |\n',1)[1]
                for mode in (*ci_release.SIGNING_MODES,'unknown-mode'):
                    with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temporary:
                        output=Path(temporary)/'output'
                        output.touch()
                        result=subprocess.run(['bash','-e','-c',script],
                            env=dict(os.environ,SIGNING_MODE=mode,GITHUB_OUTPUT=str(output)),
                            text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
                        expected=mode in ci_release.SIGNING_MODES
                        self.assertEqual(result.returncode==0,expected)
                        self.assertEqual(output.read_text(),f'signing_mode={mode}\n' if expected else '')


if __name__ == '__main__':
    unittest.main()
