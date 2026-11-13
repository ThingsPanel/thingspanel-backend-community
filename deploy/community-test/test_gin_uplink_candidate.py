import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

def load(name):
    path=Path(__file__).with_name(name+'.py')
    spec=importlib.util.spec_from_file_location(name,path)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module

candidate=load('build_gin_uplink_candidate')
gate=load('gin_uplink_test_gate')

class UplinkCandidateGuards(unittest.TestCase):
    def test_exact_scope_preserves_other_owner(self):
        self.assertEqual(len(candidate.UPLINK_FILES),38)
        self.assertEqual(len(candidate.COMPAT_FILES),4)
        self.assertEqual(len(candidate.OWNER_FILES),4)
        self.assertFalse(set(candidate.UPLINK_FILES)&set(candidate.COMPAT_FILES))
        self.assertTrue(all(name.startswith('internal/') for name in candidate.UPLINK_FILES+candidate.COMPAT_FILES))
        self.assertEqual(candidate.BASE_TOOL,'efb3f95f45310206fa484efb59402eb4bf7aa62f')

    def test_all_preimages_checked_before_any_write(self):
        with tempfile.TemporaryDirectory(prefix='uplink-overlay-') as tmp:
            root=Path(tmp);(root/'old.go').write_bytes(b'old')
            (root/'new.go').write_bytes(b'other-patch')
            with self.assertRaisesRegex(ValueError,'baseline mismatch'):
                candidate.apply_exact(root,{'old.go':(b'old',b'new'),'new.go':(None,b'addition')},{'old.go','new.go'})
            self.assertEqual((root/'old.go').read_bytes(),b'old')
            self.assertEqual((root/'new.go').read_bytes(),b'other-patch')

    def test_overlay_bytes_exact_and_unrelated_file_preserved(self):
        with tempfile.TemporaryDirectory(prefix='uplink-overlay-') as tmp:
            root=Path(tmp);(root/'old.go').write_bytes(b'old\n');(root/'preserve').write_bytes(b'owner')
            entries={'old.go':(b'old\n',b'new\n'),'new.go':(None,b'addition\n')}
            result=candidate.apply_exact(root,entries,set(entries))
            self.assertEqual((root/'preserve').read_bytes(),b'owner')
            self.assertEqual({r['path'] for r in result},set(entries))
            for name,(_,after) in entries.items():self.assertEqual((root/name).read_bytes(),after)
            with self.assertRaisesRegex(ValueError,'baseline mismatch'):candidate.apply_exact(root,entries,set(entries))

    def test_overlay_rejects_path_escape_and_unreviewed_file(self):
        with tempfile.TemporaryDirectory(prefix='uplink-overlay-') as tmp:
            with self.assertRaisesRegex(ValueError,'exact allowed paths'):candidate.apply_exact(tmp,{'x':(None,b'x')},{'y'})
            with self.assertRaisesRegex(ValueError,'Invalid overlay path'):candidate.apply_exact(tmp,{'../x':(None,b'x')},{'../x'})

    def test_no_skip_gate_requires_every_exact_test_and_completed_package(self):
        with tempfile.TemporaryDirectory(prefix='uplink-gate-') as tmp:
            path=Path(tmp)/'test.jsonl'
            rows=[{'Test':n,'Action':'pass','Package':'project/fixture'} for n in gate.UPLINK_REQUIRED]
            complete={'Action':'pass','Package':'project/fixture'}
            def write(items):path.write_text('\n'.join(json.dumps(r) for r in items),encoding='utf-8')
            write(rows+[complete]);gate.verify(path,gate.UPLINK_REQUIRED)
            write(rows)
            with self.assertRaisesRegex(ValueError,'completed passing package'):gate.verify(path,gate.UPLINK_REQUIRED)
            write(rows[1:]+[complete])
            with self.assertRaisesRegex(ValueError,'Required test'):gate.verify(path,gate.UPLINK_REQUIRED)
            write(rows+[rows[0],complete])
            with self.assertRaisesRegex(ValueError,'duplicated'):gate.verify(path,gate.UPLINK_REQUIRED)
            write(rows+[{'Test':'TestAdditional/subtest','Action':'skip'},complete])
            with self.assertRaisesRegex(ValueError,'skipped test'):gate.verify(path,gate.UPLINK_REQUIRED)
            write(rows+[{'Action':'fail','Package':'project/broken'},complete])
            with self.assertRaisesRegex(ValueError,'failure'):gate.verify(path,gate.UPLINK_REQUIRED)

    def test_gate_and_workflow_include_actual_pg_redis_and_no_remote_operations(self):
        self.assertEqual(len(gate.UPLINK_REQUIRED),97)
        self.assertEqual(len(gate.ASSEMBLY_REQUIRED),10)
        text=(Path(__file__).resolve().parents[2]/'.github/workflows/deploy-encore.yml').read_text(encoding='utf-8')
        for needed in ('redis:7-alpine','apt-get install --no-install-recommends -y redis-server','command -v redis-server','TP_BROKER_LEDGER_TEST_DSN','TP_TELEMETRY_BROKER_TEST_DSN','TEST_DIAGNOSTICS_REDIS_ADDR',candidate.OWNER_SOURCE,candidate.UPLINK_SOURCE,'set -euo pipefail'):
            self.assertIn(needed,text)
        for forbidden in ('secrets.','scp ','ssh ','docker restart','migrate '):self.assertNotIn(forbidden,text)

if __name__=='__main__':unittest.main()
