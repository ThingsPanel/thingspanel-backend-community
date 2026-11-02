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
