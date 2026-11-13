import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('acceptance', Path(__file__).with_name('encore_acceptance_candidate.py'))
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


class AcceptanceGateTest(unittest.TestCase):
    def transcript(self, path, omit=None, extras=(), omit_package=None, wrong_package=None):
        required = candidate.specification()['required_tests']
        rows = [{'Action': 'pass', 'Test': n, 'Package': ('wrong/package' if n == wrong_package else p)}
                for n, p in required.items() if n != omit]
        rows += [{'Action': 'pass', 'Package': p} for p in set(required.values()) if p != omit_package]
        rows += list(extras)
        path.write_text('\n'.join(json.dumps(row) for row in rows), encoding='utf-8')

    def test_no_missing_skipped_wrong_package_or_truncated_pg_tests(self):
        required = candidate.specification()['required_tests']
        name = 'TestPostgresOwnerUplinkContractUsesPublishedSnapshotAndTenant'
        with tempfile.TemporaryDirectory() as temp:
            log = Path(temp) / 'tests.jsonl'
            self.transcript(log)
            self.assertEqual(candidate.test_gate([log])['required_test_count'], len(required))
            variants = [dict(omit=name), dict(wrong_package=name), dict(omit_package=required[name]),
                        dict(extras=[{'Action': 'skip', 'Test': name, 'Package': required[name]}]),
                        dict(extras=[{'Action': 'fail', 'Package': required[name]}]),
                        dict(extras=[{'Action': 'skip', 'Test': name + '/real_db', 'Package': required[name]}])]
            for kwargs in variants:
                with self.subTest(kwargs=kwargs):
                    self.transcript(log, **kwargs)
                    with self.assertRaises(ValueError):
                        candidate.test_gate([log])

    def test_explicit_empty_dependency_does_not_waive_required_test(self):
        extras = [{'Action': 'output', 'Package': 'encore.app/automation/unused', 'Output': '[no tests to run]'},
                  {'Action': 'skip', 'Package': 'encore.app/automation/unused'}]
        with tempfile.TemporaryDirectory() as temp:
            log = Path(temp) / 'tests.jsonl'
            self.transcript(log, extras=extras)
            self.assertEqual(candidate.test_gate([log])['skipped'], [])
            self.transcript(log, omit='TestQ09SameRuntimeLiveRecoveryCannotShareExecutionLease', extras=extras)
            with self.assertRaises(ValueError):
                candidate.test_gate([log])

    def test_wrong_preimage_postimage_or_dependency_cannot_overlay(self):
        old, new = b'old', b'new'
        item = {'name': 'guard', 'base': 'a'*40, 'source': 'b'*40,
                'files': {'encore-api/example.go': {'before': candidate.bootstrap.digest(old), 'after': candidate.bootstrap.digest(new)}}}
        def git(_repo, *args):
            if args[0] == 'diff': return b'encore-api/example.go\n'
            if args[0] == 'ls-tree': return b'encore-api/example.go\n'
            return old if args[1].startswith('a'*40) else new
        with patch.object(candidate.bootstrap, 'commit'), patch.object(candidate.bootstrap, 'git', side_effect=git):
            source = {'encore-api/example.go': old}
            candidate.apply_overlay('.', source, item)
            self.assertEqual(source['encore-api/example.go'], new)
            with self.assertRaises(ValueError): candidate.apply_overlay('.', {'encore-api/example.go': b'drift'}, item)
            item['files']['encore-api/example.go']['after'] = '0'*64
            with self.assertRaises(ValueError): candidate.apply_overlay('.', {'encore-api/example.go': old}, item)
        with patch.object(candidate.bootstrap, 'commit'), patch.object(candidate.bootstrap, 'git', return_value=b'encore-api/unreviewed.go\n'):
            with self.assertRaises(ValueError): candidate.apply_overlay('.', {}, item)

    def test_spec_blocks_module_migration_and_path_escape(self):
        original = candidate.specification()
        for name in ['../outside.go', '/outside.go', 'encore-api/go.mod', 'encore-api/x/migrations/62.sql', 'pkg/relational/driver.go']:
            changed = json.loads(json.dumps(original))
            changed['overlays'][0]['files'] = {name: {'before': None, 'after': '0'*64}}
            with tempfile.TemporaryDirectory() as temp:
                path = Path(temp) / 'spec.json'; path.write_text(json.dumps(changed))
                with patch.object(candidate, 'SPEC_PATH', path), self.assertRaises(ValueError): candidate.specification()

    def test_workflow_is_manual_build_only_with_actual_composed_pg_gate(self):
        root = Path(__file__).resolve().parents[2]
        workflow = (root / '.github/workflows/deploy-encore.yml').read_text(encoding='utf-8')
        self.assertIn('workflow_dispatch:', workflow)
        for unsafe in ['push:', 'pull_request:', 'schedule:', 'secrets.', 'ssh ', 'scp ', 'docker push', 'docker compose', 'ghcr.io']:
            self.assertNotIn(unsafe, workflow)
        self.assertIn('encore_acceptance_candidate.py compose', workflow)
        self.assertIn('working-directory: ${{ runner.temp }}/encore-candidate/encore-api', workflow)
        self.assertIn('encore test -p=1 -count=1 -json ./automation/...', workflow)
        self.assertIn('./thingmodel/app', workflow)
        self.assertIn('test-gate candidate-output/automation-tests.jsonl', workflow)
        self.assertIn('set -euo pipefail', workflow)

    def test_self_consistent_rewritten_source_manifest_is_not_accepted(self):
        spec = candidate.specification()
        source = {'encore-api/x.go': '0'*64}
        manifest = {'services': candidate.bootstrap.SERVICES, 'migrations': {str(i): '0'*64 for i in range(61)},
                    'overlays': spec['overlays'], 'required_tests': spec['required_tests'],
                    'overlay_spec_sha256': candidate.bootstrap.digest(candidate.SPEC_PATH.read_bytes()),
                    'source_sha256': source,
                    'composition_sha256': candidate.bootstrap.digest(json.dumps(source, sort_keys=True).encode())}
        with patch.object(candidate.bootstrap, 'verify_source', return_value=manifest):
            with self.assertRaisesRegex(ValueError, 'reviewed runtime'):
                candidate.verify_source('.')


if __name__ == '__main__':
    unittest.main()
