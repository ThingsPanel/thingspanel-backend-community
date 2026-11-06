import copy
import json
import pathlib
import tempfile
import unittest

from notification_identity import (RETIRED_ENV, backend_process_spec,
                                   prepare_notification_identity, retire_notification_renewal)


class NotificationIdentityTests(unittest.TestCase):
    def fixture(self, root):
        config = {'services': {
            'encore-api': {'environment': {'TP_SOURCE_DEPLOYMENT_ID': 'installation-a',
                'TP_NOTIFICATION_SOURCE_READER_TOKEN': 'reader-fixture-' + 'x' * 32,
                'TP_NOTIFICATION_TOKENS_JSON': '{"old-tenant":"old-token"}',
                'TP_NOTIFICATION_TOKEN': 'wrong-global-token'}},
            'notification-core': {'environment': {'NOTIFICATION_SOURCE_DEPLOYMENT_ID': 'installation-a',
                'NOTIFICATION_LEGACY_READER_TOKEN': 'reader-fixture-' + 'x' * 32,
                'PROVIDER_PRIVATE_SETTING': 'preserve-provider-configuration'}}}}
        (root / 'compose.encore.private.json').write_text(json.dumps(config))
        return config

    def test_install_upgrade_and_restart_require_no_tenant_list(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            before = self.fixture(root)
            backend = prepare_notification_identity(root)
            self.assertEqual(backend, prepare_notification_identity(root))
            config = json.loads((root / 'compose.encore.private.json').read_text())
            core, auto = [config['services'][s]['environment'] for s in ['notification-core', 'encore-api']]
            self.assertEqual(core['NOTIFICATION_SERVICE_TOKEN'], auto['NOTIFICATION_SERVICE_TOKEN'])
            self.assertEqual(core['NOTIFICATION_SERVICE_TOKEN'], backend['NOTIFICATION_SERVICE_TOKEN'])
            self.assertGreaterEqual(len(core['NOTIFICATION_SERVICE_TOKEN']), 32)
            self.assertEqual(core['PROVIDER_PRIVATE_SETTING'], before['services']['notification-core']['environment']['PROVIDER_PRIVATE_SETTING'])
            self.assertFalse(RETIRED_ENV.intersection(auto))
            self.assertNotIn('tenant', json.dumps(json.loads((root / 'notification-service.identity.private.json').read_text())))

    def test_scope_mismatch_fails_without_replacing_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            self.fixture(root)
            prepare_notification_identity(root)
            previous = (root / 'notification-service.identity.private.json').read_bytes()
            config = json.loads((root / 'compose.encore.private.json').read_text())
            config['services']['encore-api']['environment']['TP_SOURCE_DEPLOYMENT_ID'] = 'other-installation'
            (root / 'compose.encore.private.json').write_text(json.dumps(config))
            with self.assertRaises(RuntimeError):
                prepare_notification_identity(root)
            self.assertEqual(previous, (root / 'notification-service.identity.private.json').read_bytes())

    def test_pm2_recreation_removes_nested_and_inherited_old_tokens(self):
        process = {'name': 'thingspanel-backend-community', 'pm2_env': {
            'pm_exec_path': '/app/backend', 'pm_cwd': '/app', 'exec_interpreter': 'none',
            'args': ['-config', '/app/private.yml'], 'DATABASE_PASSWORD': 'fixture-private',
            'NOTIFICATION_SOURCE_BEARER_TOKEN': 'old-source',
            'env': {'NOTIFICATION_MEMBER_READER_TENANT_IDS': 'old-tenant',
                    'NOTIFICATION_PROJECTION_BEARER_TOKEN': 'old-projection'}}}
        original = copy.deepcopy(process)
        spec = backend_process_spec(process, {'NOTIFICATION_SERVICE_TOKEN': 'new-identity'})
        self.assertFalse(RETIRED_ENV.intersection(spec['env']))
        self.assertEqual(spec['env']['DATABASE_PASSWORD'], 'fixture-private')
        self.assertEqual(spec['args'], original['pm2_env']['args'])
        self.assertEqual(process, original)

    def test_retirement_removes_old_timers_and_pairs_but_preserves_provider_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            runtime, actions, units = [root / name for name in ['runtime', 'actions', 'units']]
            for path in [runtime, actions, units]:
                path.mkdir()
            pair = runtime / 'source.private.json'
            pair.write_text('{"sourceCredential":"fixture-retired"}')
            (runtime / 'notification-renew-policy.private.json').write_text(json.dumps({'credentials_file': str(pair)}))
            provider = runtime / 'provider.private.json'
            provider.write_text('{"provider":"preserved"}')
            timer = units / 'tp-notification-renew.timer'
            timer.write_text('[Timer]')
            timer.with_suffix('.service').write_text('ExecStart=notification_renew.py')
            other_timer = units / 'tp-notification-provider-renew.timer'
            other_timer.write_text('[Timer]')
            other_timer.with_suffix('.service').write_text('ExecStart=provider-refresh')
            commands = []
            retire_notification_renewal(runtime, actions, commands.append, units)
            self.assertFalse(pair.exists())
            self.assertFalse(timer.exists())
            self.assertFalse(list(runtime.glob('*renew*')))
            self.assertTrue(provider.exists())
            self.assertTrue(other_timer.exists())
            self.assertIn(['systemctl', 'disable', '--now', timer.name], commands)


if __name__ == '__main__':
    unittest.main()
