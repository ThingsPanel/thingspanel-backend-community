import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('runtime', Path(__file__).with_name('runtime.py'))
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class FixtureSafety(unittest.TestCase):
    def test_rejects_system_and_damaged_volume_paths(self):
        for path in ('/tmp/tp', '/Volumes/500G/tp', '/Volumes/80G/../500G/tp'):
            with self.assertRaises(ValueError):
                runtime.safe_path(path)

    def test_no_docker_access_before_disk_capacity_check(self):
        usage = type('Usage', (), {'free': 3 * 1024**3})()
        with patch.object(runtime.shutil, 'disk_usage', return_value=usage), patch.object(runtime, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'System disk'):
                runtime.preflight(Path('/Volumes/80G/test'))
            run.assert_not_called()

    def test_database_target_does_not_accept_external_names(self):
        with patch.object(runtime, 'compose') as compose:
            with self.assertRaises(ValueError):
                runtime.sql(Path('/Volumes/80G/test'), 'production', 'SELECT 1')
            compose.assert_not_called()


if __name__ == '__main__':
    unittest.main()

class WiringSafety(unittest.TestCase):
    def test_native_wiring_has_five_databases_and_loopback_ports(self):
        import sys
        import tempfile
        sys.path.insert(0, str(Path(__file__).parent))
        import wiring
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'private').mkdir()
            front = root / 'front'
            front.mkdir()
            (front / 'nginx.conf').write_text('http { server { location /api { proxy_pass http://backend:9999; } } }')
            manifest = {'backend': {'sha': 'a' * 40}, 'frontend': {'sha': 'b' * 40, 'path': str(front)}}
            def certificate(*args, **kwargs):
                (root / 'private/internal.crt').write_text('test-certificate')
                (root / 'private/internal.key').write_text('test-key')
            with patch.object(wiring.subprocess, 'run', side_effect=certificate):
                config = wiring.generate(root, manifest, 'test-password', {'services': {}})
            import json
            native = json.loads((root / 'private/runtime.encore.json').read_text())
            self.assertEqual(len(native['sql_servers'][0]['databases']), 5)
            for service in config['services'].values():
                self.assertTrue(all(port.startswith('127.0.0.1:') for port in service.get('ports', [])))
            policy = json.loads((root / 'private/services.json').read_text())[0]
            self.assertEqual(policy['allowed_device_ids'], [])
            self.assertEqual(policy['policy']['publish'], [])
