import importlib.util
import pathlib
import sys
import tempfile
import types
import unittest
from unittest.mock import patch, mock_open

fake_fcntl = types.SimpleNamespace(LOCK_SH=1, LOCK_NB=2, flock=lambda *a: None)
with patch.dict(sys.modules, {'fcntl': fake_fcntl}):
    spec = importlib.util.spec_from_file_location('inspect_runtime', pathlib.Path(__file__).with_name('inspect_runtime.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

class TransferDiagnosticsTest(unittest.TestCase):
    def test_only_safe_artifact_metadata_and_process_names(self):
        with tempfile.TemporaryDirectory() as d:
            root = pathlib.Path(d)
            name = 'broker-' + 'a' * 40 + '.tar.gz'
            (root / name).write_bytes(b'123')
            (root / 'credentials.private.json').write_text('SECRET')
            calls = []
            def command(args):
                calls.append(args)
                return 'scp 12\npython3 80\nunknown-secret-name 20\npython3 secret-token\n'
            result = module.transfer_diagnostics(root, command)
            self.assertEqual([p['name'] for p in result['incoming_packages']], [name])
            self.assertEqual(result['incoming_packages'][0]['bytes'], 3)
            self.assertEqual(result['processes'], [{'comm':'scp','elapsed_seconds':12},{'comm':'python3','elapsed_seconds':80}])
            self.assertEqual(calls, [['ps','-eo','comm=,etimes=']])
            self.assertNotIn('SECRET', str(result))
    def test_busy_lock_never_reads_database_or_private_config(self):
        with patch.object(module, 'transfer_diagnostics', return_value={'incoming_packages': []}), \
             patch.object(module.pathlib.Path, 'open', mock_open()), \
             patch.object(module.fcntl, 'flock', side_effect=BlockingIOError()), \
             patch.object(module.shutil, 'disk_usage', return_value=types.SimpleNamespace(free=123)), \
             patch.object(module, 'run') as run, \
             patch.object(module.pathlib.Path, 'read_text') as read, \
             patch('builtins.print') as output:
            module.main()
            run.assert_not_called()
            read.assert_not_called()
            payload = module.json.loads(output.call_args.args[0])
            self.assertTrue(payload['deployment_lock_busy'])
            self.assertTrue(payload['database_checks_skipped'])
    def test_process_failure_keeps_artifact_snapshot(self):
        with tempfile.TemporaryDirectory() as d:
            result = module.transfer_diagnostics(pathlib.Path(d), lambda args: (_ for _ in ()).throw(RuntimeError()))
            self.assertTrue(result['process_snapshot_unavailable'])

if __name__ == '__main__':
    unittest.main()
