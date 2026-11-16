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
    def test_backend_diagnostics_do_not_emit_environment_or_log_content(self):
        import json, urllib.error
        with tempfile.TemporaryDirectory() as directory:
            log=pathlib.Path(directory)/'error.log';log.write_text('secret-password panic: SQLSTATE 42703')
            def command(args):
                if args[-1]=='jlist':
                    return json.dumps([{'name':'thingspanel-backend-community','pid':3,'pm2_env':{'status':'errored','env':{'TOKEN':'secret-token'},'args':['--password','secret-password'],'pm_err_log_path':str(log)}}])
                if args[0]=='ss':return 'LISTEN 0 50 127.0.0.1:9999 0.0.0.0:*'
                if args[0]=='nginx':return 'location /api/ {proxy_pass http://127.0.0.1:9999; # secret-token}'
                raise AssertionError(args)
            with patch.object(module.urllib.request,'urlopen',side_effect=OSError()):
                result=module.backend_process_diagnostics(command)
            self.assertEqual(result['processes'][0]['recent_log_categories'],['SQLSTATE_42703','panic'])
            self.assertNotIn('secret',str(result));self.assertNotIn('TOKEN',str(result))
            self.assertEqual(result['listeners'],['127.0.0.1:9999'])
    def test_historical_inventory_requires_exact_manifest_copy_and_excludes_active(self):
        import hashlib, json, tarfile, io
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory); (root/'incoming').mkdir()
            commit = 'b'*40; release=root/'encore-releases'/commit; release.mkdir(parents=True)
            content=b'fixture'; (release/'image.tar.gz').write_bytes(content)
            (release/'image.json').write_bytes(b'{}')
            manifest={'commit':commit,'sha256':{'image.tar.gz':hashlib.sha256(content).hexdigest(),'image.json':hashlib.sha256(b'{}').hexdigest()}}
            (release/'manifest.json').write_text(json.dumps(manifest))
            (release/'deployment.json').write_text(json.dumps({'commit':commit,'image':'tp-community-encore:source-'+commit}))
            package=root/'incoming'/('encore-'+commit+'.tar.gz')
            with tarfile.open(package,'w:gz') as archive:
                archive.add(release/'manifest.json',arcname='manifest.json')
                archive.add(release/'image.tar.gz',arcname='image.tar.gz')
                archive.add(release/'image.json',arcname='image.json')
            rows=module.historical_package_copies(root,set())
            self.assertEqual(len(rows),1); self.assertTrue(rows[0]['verified_copy'])
            self.assertEqual(rows[0]['verified_files'],2)
            self.assertEqual(module.historical_package_copies(root,{commit}),[])
            (release/'image.tar.gz').write_bytes(b'corrupt')
            self.assertFalse(module.historical_package_copies(root,set())[0]['verified_copy'])
            (release/'manifest.json').write_text(json.dumps({'commit':commit,'sha256':{}}))
            self.assertFalse(module.historical_package_copies(root,set())[0]['verified_copy'])
            (release/'deployment.json').unlink()
            self.assertEqual(module.historical_package_copies(root,set()),[])
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
