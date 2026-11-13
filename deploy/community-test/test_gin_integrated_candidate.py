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

 def test_directory_overlay_checks_all_preimages_before_write(self):
  with tempfile.TemporaryDirectory(prefix='directory-overlay-') as tmp:
   root=Path(tmp);owner=root/candidate.DIRECTORY_FILES[0];owner.parent.mkdir(parents=True);owner.write_bytes(b'old-owner')
   entries={name:(b'old-owner' if name==candidate.DIRECTORY_FILES[0] else None,b'new-'+name.encode()) for name in candidate.DIRECTORY_FILES}
   unexpected=root/candidate.DIRECTORY_FILES[2];unexpected.write_bytes(b'unexpected-existing-test')
   with self.assertRaisesRegex(ValueError,'baseline mismatch'):candidate.apply_directory_files(root,entries)
   self.assertEqual(owner.read_bytes(),b'old-owner')
   self.assertFalse((root/candidate.DIRECTORY_FILES[1]).exists())
   unexpected.unlink()
   changed=candidate.apply_directory_files(root,entries)
   self.assertEqual({x['path'] for x in changed},set(candidate.DIRECTORY_FILES))
   self.assertEqual(owner.read_bytes(),entries[candidate.DIRECTORY_FILES[0]][1])
 def test_directory_overlay_refuses_added_unreviewed_file(self):
  with tempfile.TemporaryDirectory(prefix='directory-scope-') as tmp:
   entries={name:(None,b'new') for name in candidate.DIRECTORY_FILES}
   entries['internal/app/bootstrap.go']=(None,b'new')
   with self.assertRaisesRegex(ValueError,'exactly the three'):candidate.apply_directory_files(Path(tmp),entries)
   self.assertEqual(list(Path(tmp).rglob('*')),[])
 def test_directory_tests_are_required_and_package_skip_is_not_test_skip(self):
  with tempfile.TemporaryDirectory(prefix='directory-gate-') as tmp:
   path=Path(tmp)/'log.jsonl'
   rows=[{'Action':'skip','Package':'unused-package'}]+[{'Package':'project/internal/api','Test':n,'Action':'pass'} for n in candidate.REQUIRED_PG_TESTS]
   path.write_text('\n'.join(json.dumps(x) for x in rows));candidate.verify_tests(path)
   for name in candidate.REQUIRED_DIRECTORY_TESTS:
    filtered=[x for x in rows if x.get('Test')!=name]
    path.write_text('\n'.join(json.dumps(x) for x in filtered))
    with self.assertRaisesRegex(ValueError,'missing, skipped or failed'):candidate.verify_tests(path)
    filtered.append({'Test':name,'Action':'skip'});path.write_text('\n'.join(json.dumps(x) for x in filtered))
    with self.assertRaisesRegex(ValueError,'missing, skipped or failed'):candidate.verify_tests(path)
   rows.append({'Test':'TestUnexpected/child','Action':'skip'});path.write_text('\n'.join(json.dumps(x) for x in rows))
   with self.assertRaisesRegex(ValueError,'skipped test'):candidate.verify_tests(path)
 def test_deployed_snapshot_mismatch_is_closed(self):
  with self.assertRaisesRegex(ValueError,'deployed 1702'):candidate.verify_deployed_source({'internal/api/automation_owner.go':'changed'})
 def test_directory_scope_has_no_uplink_or_compat_patch(self):
  self.assertEqual(len(candidate.DIRECTORY_FILES),3)
  self.assertEqual(candidate.DIRECTORY_SOURCE,'c4197017533974a624a3fa0982a0f79cd6443ec7')
  self.assertEqual(len(candidate.REQUIRED_DIRECTORY_TESTS),3)
  self.assertFalse(set(candidate.DIRECTORY_FILES)&set(candidate.COMMAND_FILES))

 def test_generated_profile_normalizes_only_verified_newlines(self):
  with tempfile.TemporaryDirectory(prefix='profile-newlines-') as tmp:
   path=Path(tmp)/'RELEASE_PROFILE.json';raw=b'{\n  "reviewed": true\n}'
   with patch.object(candidate,'DEPLOYED_BACKEND_PROFILE_SHA256',candidate.digest(raw)):
    path.write_bytes(raw.replace(b'\n',b'\r\n'));candidate.normalize_generated_backend_profile(path)
    self.assertEqual(path.read_bytes(),raw)
    candidate.normalize_generated_backend_profile(path)
    self.assertEqual(path.read_bytes(),raw)
    path.write_bytes(b'{"reviewed":false}')
    with self.assertRaisesRegex(ValueError,'deployed CI bytes'):candidate.normalize_generated_backend_profile(path)
    self.assertEqual(path.read_bytes(),b'{"reviewed":false}')

if __name__=='__main__':unittest.main()
