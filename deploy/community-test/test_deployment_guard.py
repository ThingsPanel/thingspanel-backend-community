import json
import pathlib
import tempfile
import unittest
from deployment_guard import restore_services_on_failure


class RolloutRecoveryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.compose = pathlib.Path(self.tmp.name) / 'compose.private.json'
        self.original = b'{"services":{"notification-core":{"image":"verified-old","environment":{"TOKEN":"fixture"}}}}'
        self.compose.write_bytes(self.original)

    def test_failed_migration_restarts_previous_services(self):
        calls = []
        with self.assertRaisesRegex(RuntimeError, 'migration rejected'):
            with restore_services_on_failure(self.compose, ['encore-api', 'notification-core'], calls.append):
                raise RuntimeError('migration rejected')
        self.assertEqual(self.compose.read_bytes(), self.original)
        self.assertEqual(calls[0][-2:], ['encore-api', 'notification-core'])
        self.assertIn('--no-deps', calls[0])
        self.assertIn('--no-build', calls[0])
        self.assertEqual(calls[0][calls[0].index('--pull') + 1], 'never')

    def test_failed_health_check_restores_image_and_private_settings(self):
        calls = []
        def recover(command):
            calls.append(command)
            self.assertEqual(self.compose.read_bytes(), self.original)
        with self.assertRaisesRegex(RuntimeError, 'health failed'):
            with restore_services_on_failure(self.compose, ['notification-core'], recover):
                self.compose.write_text(json.dumps({'services': {'notification-core': {'image': 'candidate'}}}))
                raise RuntimeError('health failed')
        self.assertEqual(len(calls), 1)

    def test_success_keeps_candidate_and_does_not_restart_services(self):
        calls = []
        with restore_services_on_failure(self.compose, ['notification-core'], calls.append):
            self.compose.write_bytes(b'candidate-config')
        self.assertEqual(self.compose.read_bytes(), b'candidate-config')
        self.assertEqual(calls, [])

    def test_recovery_failure_is_reported_after_configuration_is_restored(self):
        def fail(command):
            raise OSError('fixture daemon unavailable')
        with self.assertRaisesRegex(RuntimeError, 'service recovery also failed'):
            with restore_services_on_failure(self.compose, ['notification-core'], fail):
                self.compose.write_bytes(b'candidate-config')
                raise RuntimeError('health failed')
        self.assertEqual(self.compose.read_bytes(), self.original)


if __name__ == '__main__':
    unittest.main()
