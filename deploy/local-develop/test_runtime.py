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

class IdentitySafety(unittest.TestCase):
    def setUp(self):
        import sys
        sys.path.insert(0, str(Path(__file__).parent))
        import identity
        self.identity = identity

    def test_provision_refuses_before_http_when_fixture_guard_fails(self):
        with patch.object(self.identity, 'guard', side_effect=ValueError('not fresh')), patch.object(self.identity, 'api') as api:
            with self.assertRaises(ValueError):
                self.identity.provision(Path('/Volumes/80G/fixture'), runtime)
            api.assert_not_called()

    def test_revoked_fixture_key_is_not_resurrected(self):
        import tempfile
        import json
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'private').mkdir()
            (root / 'private/native-key.json').write_text(json.dumps({'revoked': True}))
            with patch.object(self.identity, 'guard'), patch.object(self.identity, 'api') as api:
                with self.assertRaisesRegex(ValueError, 'revoked'):
                    self.identity.provision(root, runtime)
                api.assert_not_called()

    def test_idempotent_issuance_does_not_create_another_key(self):
        import tempfile
        import json
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'private').mkdir()
            account = {'id': 'u', 'tenant_id': 't', 'email': 'local@example.invalid', 'password': 'private'}
            (root / 'private/native-account.json').write_text(json.dumps(account))
            key = {'id': 'k', 'tenant_id': 't', 'created_id': 'u', 'status': 1, 'api_key': 'secret'}
            with patch.object(self.identity, 'guard'), patch.object(self.identity, 'rows', return_value=[account]), patch.object(self.identity, 'account_login', return_value=(account, 'token')), patch.object(self.identity, 'key_rows', return_value=[key]), patch.object(self.identity, 'configure_key'), patch.object(self.identity, 'api') as api:
                self.identity.provision(root, runtime)
                self.identity.provision(root, runtime)
                api.assert_not_called()

    def test_idempotent_revoke_does_not_reenable_or_repeat_write(self):
        import tempfile
        import json
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'private').mkdir()
            (root / 'private/native-key.json').write_text(json.dumps({'id': 'k', 'name': 'key', 'revoked': True}))
            key = {'id': 'k', 'tenant_id': 't', 'created_id': 'u', 'status': 0}
            with patch.object(self.identity, 'guard'), patch.object(self.identity, 'account_login', return_value=({'id': 'u', 'tenant_id': 't'}, 'token')), patch.object(self.identity, 'key_rows', return_value=[key]), patch.object(self.identity, 'configure_key'), patch.object(self.identity, 'api') as api:
                self.identity.revoke(root, runtime)
                api.assert_not_called()

    def test_broker_scope_rejects_other_tenants_and_duplicates(self):
        for targets in ([{'id': 'one', 'tenant_id': 'other'}],
                        [{'id': 'one', 'tenant_id': 't'}] * 2,
                        [{'id': 'one/#', 'tenant_id': 't'}]):
            with self.assertRaises(ValueError):
                self.identity.scoped_policy({'policy': {}}, targets, 't')

    def test_empty_scope_revokes_all_command_publish(self):
        policy = self.identity.scoped_policy({'policy': {'publish': ['devices/command/+/+']}}, [], 't')
        self.assertEqual(policy['allowed_device_ids'], [])
        self.assertEqual(policy['allowed_tenant_ids'], [])
        self.assertEqual(policy['policy']['publish'], [])

    def test_finite_scope_only_enables_durable_command_family(self):
        policy = self.identity.scoped_policy({'policy': {}}, [{'id': 'd1', 'tenant_id': 't'}], 't')
        self.assertEqual(policy['allowed_device_ids'], ['d1'])
        self.assertEqual(policy['allowed_tenant_ids'], ['t'])
        self.assertEqual(policy['policy']['publish'], ['devices/command/+/+', 'gateway/command/+/+'])

class BuildSafety(unittest.TestCase):
    def setUp(self):
        import sys
        sys.path.insert(0, str(Path(__file__).parent))
        import build
        self.build = build

    def test_build_preflight_failure_never_starts_a_process(self):
        with patch.object(runtime, 'preflight', side_effect=ValueError('low disk')), patch.object(self.build.subprocess, 'Popen') as popen:
            with self.assertRaises(ValueError):
                self.build.execute(['go', 'build'], '.', {}, Path('/Volumes/80G/test'), runtime, 'backend')
            popen.assert_not_called()

    def test_modified_binary_provenance_is_rejected(self):
        import tempfile
        import json
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'artifacts').mkdir()
            binary = root / 'artifacts/ThingsPanel-Go'
            binary.write_bytes(b'original')
            record = {'backend': {'sha': 'abc', 'base_image': 'sha256:base',
                'binary_sha256': self.build.digest(binary), 'resources': {}}}
            (root / 'build-provenance.json').write_text(json.dumps(record))
            binary.write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'binary changed'):
                self.build.verify(root, {'backend': {'sha': 'abc'}}, {'services': {'backend': {'image': 'sha256:base', 'volumes': []}}})

    def test_provenance_cannot_substitute_a_different_source_sha(self):
        import tempfile
        import json
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'build-provenance.json').write_text(json.dumps({'encore-api': {'sha': 'old', 'base_image': 'sha256:base'}}))
            with self.assertRaisesRegex(ValueError, 'SHA/image mismatch'):
                self.build.verify(root, {'backend': {'sha': 'new'}}, {'services': {'encore-api': {'image': 'sha256:base'}}})
