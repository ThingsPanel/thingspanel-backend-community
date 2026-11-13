import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('candidate',Path(__file__).with_name('build_gin_integrated_candidate.py'))
candidate=importlib.util.module_from_spec(spec);spec.loader.exec_module(candidate)

class CandidateGuards(unittest.TestCase):
 def test_nested_git_overrides_host_newline_policy_without_mutating_parent(self):
  with patch.dict(os.environ, {'GIT_CONFIG_COUNT':'1','GIT_CONFIG_KEY_0':'core.autocrlf','GIT_CONFIG_VALUE_0':'true'}):
   self.assertEqual(candidate.run(['git','config','--get','core.autocrlf']).strip(),b'false')
   self.assertEqual(os.environ['GIT_CONFIG_VALUE_0'],'true')
   self.assertEqual(os.environ['GIT_CONFIG_COUNT'],'1')
 def test_exact_delta_keeps_unrelated_sources(self):
  result=candidate.verify_only_changes({'a':'old','retained':'same'},{'a':'new','retained':'same','b':'new'}, {'a','b'})
  self.assertEqual([x['path'] for x in result],['a','b'])
  with self.assertRaisesRegex(ValueError,'Unexpected'):
   candidate.verify_only_changes({'a':'old','retained':'same'},{'a':'new','retained':'changed'},{'a'})
 def test_missing_expected_patch_is_not_success(self):
  with self.assertRaisesRegex(ValueError,'Unexpected'):
   candidate.verify_only_changes({'a':'old'},{'a':'old'},{'a'})
 def test_other_owner_change_refused_even_if_allowlisted(self):
  for name in ['encore-api/x.go','sql/migration.sql','notification-adapters/a.go']:
   with self.assertRaisesRegex(ValueError,'another owner'):
    candidate.verify_only_changes({}, {name:'hash'}, {name})
 def test_required_pg_pass_is_not_satisfied_by_skip_or_missing(self):
  with tempfile.TemporaryDirectory(prefix='gin-candidate-test-') as tmp:
   path=Path(tmp)/'tests.jsonl'
   rows=[{'Test':name,'Action':'pass'} for name in candidate.REQUIRED_PG_TESTS]
   path.write_text('\n'.join(json.dumps(x) for x in rows));candidate.verify_tests(path)
   rows[0]['Action']='skip';path.write_text('\n'.join(json.dumps(x) for x in rows))
   with self.assertRaisesRegex(ValueError,'missing, skipped or failed'):candidate.verify_tests(path)
   path.write_text('')
   with self.assertRaisesRegex(ValueError,'missing, skipped or failed'):candidate.verify_tests(path)
 def test_scope_is_fixed_to_eleven_gin_four_excluded_encore(self):
  self.assertEqual(len(candidate.COMMAND_FILES),11)
  self.assertTrue(all(x.startswith('internal/') for x in candidate.COMMAND_FILES))
  self.assertEqual(len(candidate.EXCLUDED_ENCORE_FILES),4)
  self.assertFalse(set(candidate.COMMAND_FILES)&set(candidate.EXCLUDED_ENCORE_FILES))

if __name__=='__main__':unittest.main()
