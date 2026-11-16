import copy
import hashlib
import io
import json
import pathlib
import tarfile
import tempfile
import unittest
import recover_verified_transfer as recovery

SOURCE_RUN = 43819027477
SOURCE_SHA = "443db33bac1c6ec04e353abb65ec57d9a491aa7f"


class RecoveryGuardTests(unittest.TestCase):
    def fixture(self):
        run = {'id': SOURCE_RUN, 'head_sha': SOURCE_SHA, 'head_branch': 'main',
               'event': 'workflow_dispatch', 'path': '.github/workflows/deploy.yml',
               'repository': {'full_name': 'owner/repo'}, 'status': 'completed', 'conclusion': 'failure'}
        steps = [{'name': n, 'conclusion': 'success'} for n in recovery.REQUIRED]
        steps += [{'name': recovery.TRANSFER, 'conclusion': 'failure'}]
        steps += [{'name': n, 'conclusion': 'skipped'} for n in recovery.DEPLOY_STEPS]
        jobs = [{'name': 'build_backend', 'conclusion': 'success'},
                {'name': 'native / deploy', 'status': 'completed', 'conclusion': 'failure', 'steps': steps}]
        return run, jobs

    def test_only_reviewed_failed_run(self):
        run, jobs = self.fixture()
        recovery.validate_run(run, jobs, 'owner/repo', SOURCE_RUN)
        for field, value in [('id', 1), ('head_sha', 'bad-sha'), ('head_branch', 'develop'),
                             ('event', 'push'), ('path', '.github/workflows/other.yml')]:
            bad = copy.deepcopy(run)
            bad[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                recovery.validate_run(bad, jobs, 'owner/repo', SOURCE_RUN)

    def test_other_run_uses_its_own_revision_and_invalid_ids_stop_early(self):
        from unittest.mock import patch
        run, jobs = self.fixture()
        run['id'] = 999
        run['head_sha'] = 'b' * 40
        recovery.validate_run(run, jobs, 'owner/repo', 999)
        for value in ['', '0', '-1', '123; command', '1.5']:
            with patch.dict('os.environ', {'GITHUB_REF': 'refs/heads/main', 'SOURCE_RUN_ID': value}), \
                    patch.object(recovery, 'api') as api, self.assertRaises(ValueError):
                recovery.main()
            api.assert_not_called()

    def test_failed_checks_or_started_deployment_rejected(self):
        run, jobs = self.fixture()
        for name in recovery.REQUIRED + recovery.DEPLOY_STEPS:
            bad = copy.deepcopy(jobs)
            step = next(s for s in bad[1]['steps'] if s['name'] == name)
            step['conclusion'] = 'failure' if name in recovery.REQUIRED else 'success'
            with self.subTest(name=name), self.assertRaises(ValueError):
                recovery.validate_run(run, bad, 'owner/repo', SOURCE_RUN)

    def test_business_diff_rejected(self):
        recovery.validate_changes(['deploy/community-test/artifact_transfer.py', '.github/workflows/recover.yml'])
        for path in ['internal/service/a.go', 'encore-api/main.go', 'deploy/other.py']:
            with self.assertRaises(ValueError):
                recovery.validate_changes([path])

    def test_tar_requires_exact_source_and_digest(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / 'fixture.tar.gz'
            for corrupt in [False, True]:
                data = b'valid release'
                manifest = {'commit': SOURCE_SHA, 'sha256': {'binary': hashlib.sha256(data).hexdigest()}}
                with tarfile.open(path, 'w:gz') as archive:
                    for name, value in [('manifest.json', json.dumps(manifest).encode()), ('binary', b'wrong' if corrupt else data)]:
                        info = tarfile.TarInfo(name)
                        info.size = len(value)
                        archive.addfile(info, io.BytesIO(value))
                if corrupt:
                    with self.assertRaises(ValueError):
                        recovery.verify_tar(path, SOURCE_SHA)
                else:
                    recovery.verify_tar(path, SOURCE_SHA)
                    with self.assertRaises(ValueError):
                        recovery.verify_tar(path, 'b' * 40)


if __name__ == '__main__':
    unittest.main()
