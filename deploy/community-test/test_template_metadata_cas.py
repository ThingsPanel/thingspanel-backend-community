import importlib.util
import os
from pathlib import Path
import shutil
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('cas',ROOT/'deploy/community-test/apply_template_metadata_cas.py')
cas=importlib.util.module_from_spec(spec);spec.loader.exec_module(cas)

class StageCAS(unittest.TestCase):
 def setUp(self):
  source=os.environ.get('TEMPLATE_CAS_STAGE_SOURCE')
  if not source:self.skipTest('actual composed stage source fixture required')
  self.temp=tempfile.TemporaryDirectory(prefix='q02-cas-');self.addCleanup(self.temp.cleanup)
  self.destination=Path(self.temp.name)
  for name in set(cas.BASE_HASHES)|{'internal/middleware/cors.go'}:
   target=self.destination/name;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(Path(source)/name,target)
 def test_exact_stage_keeps_legacy_owner_and_adds_condition(self):
  owner=(self.destination/'internal/dal/device_config.go').read_bytes()
  result=cas.apply(ROOT,self.destination)
  self.assertEqual((self.destination/'internal/dal/device_config.go').read_bytes(),owner)
  service=(self.destination/'internal/service/device_config.go').read_text(encoding='utf-8')
  self.assertIn('UpdateDeviceConfigConditional',service)
  self.assertNotIn('dal.ErrDeviceBindingOutcomeUnknown',service)
  self.assertNotIn('dal.UpdateDeviceConfigVoucherType(req.Id',service)
  self.assertIn('Strength:"UPDATE"',(self.destination/'internal/dal/device_config_metadata_owner.go').read_text())
  self.assertEqual(len(result['changes']),len(cas.COPIED)+4)
 def test_base_drift_refused_before_mutation(self):
  path=self.destination/'internal/api/device_config.go';path.write_bytes(path.read_bytes()+b'\n')
  with self.assertRaisesRegex(ValueError,'source drift'):cas.apply(ROOT,self.destination)
  self.assertFalse((self.destination/cas.COPIED[0]).exists())

if __name__=='__main__':unittest.main()
