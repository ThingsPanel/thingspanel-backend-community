import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('candidate',Path(__file__).with_name('encore_isolation_candidate.py'))
candidate=importlib.util.module_from_spec(spec);spec.loader.exec_module(candidate)

class CandidateGateTest(unittest.TestCase):
    def log(self, path, omit=None, extra=None):
        rows=[{'Action':'pass','Test':name,'Package':'encore.app/automation'} for name in candidate.REQUIRED_TESTS if name!=omit]
        rows.extend(extra or [])
        path.write_text('\n'.join(json.dumps(r) for r in rows),encoding='utf-8')

    def test_missing_skipped_and_package_fail_cannot_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            path=Path(tmp)/'test.jsonl';name=next(iter(candidate.REQUIRED_TESTS))
            self.log(path);self.assertEqual(candidate.test_gate([path])['missing'],[])
            for omit,extra in [(name,[]),(None,[{'Action':'skip','Test':name}]),(None,[{'Action':'fail','Package':'encore.app/automation'}])]:
                self.log(path,omit,extra)
                with self.assertRaises(ValueError):candidate.test_gate([path])

    def test_actual_encore_package_skip_is_not_a_skipped_test(self):
        # Verbatim go-test JSON from build 43349723299; no test files in this
        # dependency package. Do not relax any required named-test assertion.
        fixture = '''{"Time":"2026-11-13T02:08:12.577498685Z","Action":"start","Package":"encore.app/automation/runtime"}
{"Time":"2026-11-13T02:08:12.577511323Z","Action":"output","Package":"encore.app/automation/runtime","Output":"?   \\tencore.app/automation/runtime\\t[no test files]\\n"}
{"Time":"2026-11-13T02:08:12.5775158Z","Action":"skip","Package":"encore.app/automation/runtime","Elapsed":0}'''
        rows=[json.loads(line) for line in fixture.splitlines()]
        with tempfile.TemporaryDirectory() as tmp:
            path=Path(tmp)/'test.jsonl';self.log(path,extra=rows)
            report=candidate.test_gate([path])
            self.assertEqual(report['skipped'],[])
            self.assertEqual(report['packages_without_matching_tests'],['encore.app/automation/runtime'])
            self.log(path,omit=next(iter(candidate.REQUIRED_TESTS)),extra=rows)
            with self.assertRaises(ValueError):candidate.test_gate([path])
            self.log(path,extra=rows+[{'Action':'skip','Package':'encore.app/automation/runtime','Test':'TestActuallySkipped'}])
            with self.assertRaises(ValueError):candidate.test_gate([path])
            self.log(path,extra=[rows[-1]])
            with self.assertRaises(ValueError):candidate.test_gate([path])

    def test_mutated_and_extra_source_cannot_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);file=root/'encore-api/a.go';file.parent.mkdir();file.write_bytes(b'original')
            (root/'pkg/relational').mkdir(parents=True)
            (root/'composition.json').write_text(json.dumps({'source_sha256':{'encore-api/a.go':candidate.digest(b'original')}}))
            candidate.verify_source(root)
            file.write_bytes(b'changed')
            with self.assertRaises(ValueError):candidate.verify_source(root)
            file.write_bytes(b'original');(root/'encore-api/extra.go').write_text('unexpected')
            with self.assertRaises(ValueError):candidate.verify_source(root)

    def test_symbolic_or_partial_commit_rejected_before_git(self):
        for value in ['main','ce9097ab','a'*39,'a'*40+'; echo bad']:
            with self.assertRaises(ValueError):candidate.commit('.',value)

if __name__=='__main__':unittest.main()
