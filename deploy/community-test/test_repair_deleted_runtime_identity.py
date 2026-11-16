import ast
import importlib.util
import pathlib
import sys
import types
import unittest
from unittest.mock import patch

sys.modules.setdefault('fcntl', types.SimpleNamespace(LOCK_EX=2,LOCK_NB=4,flock=lambda *args:None))
path=pathlib.Path(__file__).with_name('repair_deleted_runtime_identity.py')
spec=importlib.util.spec_from_file_location('repair_identity',path)
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class RecoveryTests(unittest.TestCase):
    def test_python36_compatible(self):
        ast.parse(path.read_text(encoding='utf-8'),feature_version=(3,6))
    def test_unknown_mode_fails_before_filesystem_or_database(self):
        with patch.object(module.sys,'argv',['repair','--device','other']), patch.object(module.pathlib.Path,'open') as opened:
            with self.assertRaisesRegex(RuntimeError,'Invalid mode'): module.main()
            opened.assert_not_called()
    def test_sql_errors_never_expose_private_output(self):
        process=types.SimpleNamespace(returncode=1,stdout=b'password=secret',stderr=b'credential')
        with patch.object(module.subprocess,'run',return_value=process):
            with self.assertRaises(RuntimeError) as error: module.sql('ThingsPanel','BEGIN;')
            self.assertNotIn('secret',str(error.exception));self.assertNotIn('credential',str(error.exception))
    def test_fixed_identity_and_build_scope(self):
        self.assertEqual(module.DEVICE,'758f9efd-67d6-4e16-8a36-441d8972135c')
        self.assertEqual(module.BUILD,'443db33bac1c6ec04e353abb65ec57d9a491aa7f')

if __name__=='__main__':unittest.main()
