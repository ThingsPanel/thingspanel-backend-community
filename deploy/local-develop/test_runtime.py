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
            (front / 'nginx.conf').write_text('http { server { # callback /thingsvis-api\n location /api { proxy_pass http://backend:9999; } } }')
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
            redis = config['services']['redis']
            self.assertTrue(redis['command'][-1])
            self.assertEqual(redis['command'][-1], redis['environment']['REDISCLI_AUTH'])
            self.assertEqual(redis['command'][-1], config['services']['backend']['environment']['GOTP_DB_REDIS_PASSWORD'])
            self.assertEqual(redis['healthcheck']['test'], ['CMD', 'redis-cli', 'ping'])

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

class StorageSafety(unittest.TestCase):
    def setUp(self):
        import sys
        sys.path.insert(0, str(Path(__file__).parent))
        import storage
        self.storage = storage

    def test_internal_bounded_requires_explicit_opt_in(self):
        with patch.dict('os.environ', {'TP_DOCKER_STORAGE_MODE': 'internal-bounded'}, clear=True), patch.object(self.storage, 'orbstack_internal') as locate:
            with self.assertRaisesRegex(ValueError, 'Explicit'):
                self.storage.check(runtime)
            locate.assert_not_called()

    def test_encore_rejects_six_gib_even_with_internal_opt_in(self):
        usage = type('Usage', (), {'free': 6 * 1024**3})()
        with patch.dict('os.environ', {'TP_DOCKER_STORAGE_MODE': 'internal-bounded', 'TP_ALLOW_INTERNAL_BOUNDED_BUILD': 'yes'}, clear=True), patch.object(self.storage, 'orbstack_internal', return_value=Path('/verified/internal/image')), patch.object(self.storage.shutil, 'disk_usage', return_value=usage):
            with self.assertRaisesRegex(ValueError, '12 GiB'):
                self.storage.check(runtime, 'encore')
            self.assertEqual(self.storage.check(runtime, 'backend')['mode'], 'internal-bounded')

    def test_bounded_mode_rejects_remote_or_other_context(self):
        with patch.object(runtime, 'run', return_value='remote'):
            with self.assertRaisesRegex(ValueError, 'local OrbStack'):
                self.storage.orbstack_internal(runtime)

class EncoreBuildContract(unittest.TestCase):
    def test_official_build_explicitly_targets_arm64_and_smallest_base(self):
        import sys
        sys.path.insert(0, str(Path(__file__).parent))
        import build
        command = build.encore_command('local:test')
        self.assertEqual(command[command.index('--arch') + 1], 'arm64')
        self.assertEqual(command[command.index('--os') + 1], 'linux')
        self.assertEqual(command[command.index('--base') + 1], 'scratch')
        self.assertNotIn('--push', command)
        self.assertIn('--skip-config', command)

class RuntimeBindingSafety(unittest.TestCase):
    def setUp(self):
        import sys
        sys.path.insert(0, str(Path(__file__).parent))
        import binding
        self.binding = binding
        self.provider = '123e4567-e89b-12d3-a456-426614174000'
        self.expected = {'provider_id': self.provider, 'tenant_id': 't', 'namespace': 'community', 'service_instance_id': 'confirmed-instance'}
        self.rows = [{'runtime_provider_id': self.provider, 'tenant_id': 't', 'runtime_namespace': 'community', 'service_instance_id': 'confirmed-instance', 'runtime_external_id': 'd'}]
        self.releases = [{'provider_id': self.provider, 'version': 'v1', 'schema_digest': 'digest', 'artifact_digest': 'sha256:digest'}]
        self.key = {'tenant_id': 't', 'api_key': 'secret'}

    def test_empty_new_database_never_generates_provider_ids(self):
        with self.assertRaises(ValueError):
            self.binding.validate(self.expected, [], [], self.key)

    def test_other_tenant_or_service_instance_is_refused(self):
        for field, value in [('tenant_id', 'other'), ('service_instance_id', 'other'), ('runtime_provider_id', 'other')]:
            changed = dict(self.rows[0], **{field: value})
            with self.assertRaises(ValueError):
                self.binding.validate(self.expected, [changed], self.releases, self.key)

    def test_only_exact_confirmed_resources_install_environment(self):
        env = self.binding.validate(self.expected, self.rows, self.releases, self.key)
        self.assertEqual(env['TP_RUNTIME_COMMUNITY_PROVIDER_ID'], self.provider)
        self.assertEqual(env['TP_RUNTIME_COMMUNITY_SERVICE_INSTANCE_ID'], 'confirmed-instance')
        self.assertNotIn('TP_RUNTIME_DEVICE_ID_MODE', env)

class FrontendWorkspaceSeedSafety(unittest.TestCase):
    def test_workspace_dependency_must_not_fall_back_to_root_version(self):
        import sys, tempfile, json
        sys.path.insert(0, str(Path(__file__).parent))
        import frontend_seed
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            repo, old, new = base / 'repo', base / 'old', base / 'new'
            for root in (repo, old, new):
                (root / 'packages/axios').mkdir(parents=True)
            (repo / 'package.json').write_text('{}')
            (repo / 'packages/axios/package.json').write_text(json.dumps({'dependencies': {'axios': '1.6.7'}}))
            (old / 'packages/axios/node_modules/axios').mkdir(parents=True)
            (old / 'packages/axios/node_modules/axios/package.json').write_text('{"version":"1.6.7"}')
            (new / 'node_modules/axios').mkdir(parents=True)
            (new / 'node_modules/axios/package.json').write_text('{"version":"1.5.1"}')
            with self.assertRaisesRegex(ValueError, 'missing'):
                frontend_seed.verify_dependency_versions(repo, old / 'node_modules', new, [Path('packages/axios')])
            (new / 'packages/axios/node_modules/axios').mkdir(parents=True)
            (new / 'packages/axios/node_modules/axios/package.json').write_text('{"version":"1.6.7"}')
            versions = frontend_seed.verify_dependency_versions(repo, old / 'node_modules', new, [Path('packages/axios')])
            self.assertEqual(versions['packages/axios:axios'], '1.6.7')

    def test_system_stage_must_be_in_fresh_task_directory(self):
        import sys
        sys.path.insert(0, str(Path(__file__).parent))
        import frontend_seed
        with self.assertRaisesRegex(ValueError, 'task directory'):
            frontend_seed.prepare(Path('/Volumes/80G/test'), {}, '/tmp/not-authorized', '/tmp/node_modules')

class ActualComposePortsSafety(unittest.TestCase):
    def test_only_selected_phase_ports_are_checked(self):
        config = {'services': {'redis': {'ports': ['127.0.0.1:26379:6379']}, 'broker': {'ports': ['127.0.0.1:21883:1883']}}}
        self.assertEqual(runtime.published_ports(config, ['redis']), [26379])

    def test_nonloopback_reserved_and_duplicate_ports_are_rejected(self):
        for bindings in (['0.0.0.0:26379:6379'], ['127.0.0.1:8080:8080'], ['127.0.0.1:9999:9999'], ['127.0.0.1:26379:6379'] * 2):
            with self.assertRaises(ValueError):
                runtime.published_ports({'services': {'redis': {'ports': bindings}}}, ['redis'])

class BootstrapRecoverySafety(unittest.TestCase):
    def setUp(self):
        import sys
        sys.path.insert(0,str(Path(__file__).parent))
        import bootstrap_recovery
        self.recovery=bootstrap_recovery
        self.snapshot={'database':'ThingsPanel','owner_matches':True,'casbin_rows':0,'versions':[[0,'0.0.0']], 'native_databases':[], 'relations':list(bootstrap_recovery.RELATIONS),'columns':list(bootstrap_recovery.COLUMNS), 'other_objects':[],'extra_schemas':[],'user_triggers':0,'other_constraints':0,'extensions':['plpgsql','timescaledb'],'sequence_owned':True,'listen_addresses':'*','owners_valid':True,'primary_keys':[['casbin_rule','casbin_rule_pkey',[1]],['sys_version','sys_version_pkey',[1]],['local_fixture_owner','local_fixture_owner_pkey',[1]]],'defaults':[["casbin_rule","id","nextval('casbin_rule_id_seq'::regclass)"]]}

    def test_exact_initial_control_state_is_accepted(self):
        self.recovery.validate(self.snapshot)

    def test_partial_migration_foreign_owner_or_extra_objects_reject(self):
        for key,value in [('owner_matches',False),('versions',[[1,'local-develop']]),('casbin_rows',1),('native_databases',['automation']),('other_objects',['function unexpected()']),('extra_schemas',['foreign']),('user_triggers',1),('listen_addresses','')]:
            with self.assertRaises(ValueError): self.recovery.validate(dict(self.snapshot,**{key:value}))
        with self.assertRaises(ValueError): self.recovery.validate(dict(self.snapshot,relations=self.snapshot['relations']+[('users','r')]))


class BootstrapResumeFlow(unittest.TestCase):
    def test_resume_runs_pinned_version_and_all_migrations(self):
        import json, sys, tempfile
        import bootstrap_recovery
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / 'repo'
            (repo / 'pkg/global').mkdir(parents=True)
            (repo / 'pkg/global/global.go').write_text('const VERSION_NUMBER = 1')
            (repo / 'sql/broker').mkdir(parents=True)
            (repo / 'sql/1.sql').write_text('SELECT 11;')
            (repo / 'sql/broker/001_test.sql').write_text('SELECT 12;')
            for path in runtime.DATABASES.values():
                folder = repo / 'encore-api' / path
                folder.mkdir(parents=True)
                (folder / '1_init.up.sql').write_text('SELECT 13;')
            backend = {'path': str(repo), 'sha': 'a' * 40}
            manifest = {'project': runtime.PROJECT, 'root': str(root), 'backend': backend, 'runtime_id': 'b' * 64}
            (root / 'manifest.json').write_text(json.dumps(manifest))
            (root / 'compose.json').write_text(json.dumps({'services': {'postgres': {'image': 'sha256:test'}}}))
            inspection = {'Config': {'Labels': {'com.docker.compose.project': runtime.PROJECT}}, 'Mounts': [{'Destination': '/var/lib/postgresql/data', 'Source': str(root / 'postgres')}]}
            def fake_compose(*args, **kwargs):
                return json.dumps([{'Service': 'postgres', 'ID': 'isolated'}]) if args[1] == 'ps' else ''
            def fake_sql(root, database, statement):
                return '0' if 'FROM pg_database' in statement else ''
            with patch.object(runtime, 'preflight'), patch.object(runtime, 'source', return_value=backend), patch.object(runtime, 'compose', side_effect=fake_compose), patch.object(runtime, 'run', return_value=json.dumps([inspection])), patch.object(runtime, 'sql', side_effect=fake_sql) as sql, patch.object(bootstrap_recovery, 'validate_container'), patch.object(bootstrap_recovery, 'inspect', return_value={'validated': True}), patch.dict(sys.modules, {'runtime': runtime}):
                runtime.bootstrap(root, resume_owned_empty=True)
            statements = [call.args[2] for call in sql.call_args_list]
            self.assertTrue(any('SELECT 11;' in statement for statement in statements))
            self.assertEqual(len(json.loads((root / 'migration-ledger.json').read_text())), 7)
            self.assertEqual((root / 'bootstrap.complete').read_text(), backend['sha'])


class AdministratorInitialization(unittest.TestCase):
    def test_foreign_admin_is_never_adopted(self):
        import identity, tempfile
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); (root/'private').mkdir()
            with patch.object(identity,'rows',return_value=[{'email':'foreign'}]), patch.object(identity,'api') as api:
                with self.assertRaisesRegex(ValueError,'refusing adoption'): identity.initialize_administrator(root,runtime)
                api.assert_not_called()

    def test_formal_init_is_idempotent_and_credentials_stay_private(self):
        import identity, tempfile, json
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory); (root/'private').mkdir()
            def rows(*args):
                path=root/'private/local-administrator.json'
                if not path.exists() or not initialized[0]: return []
                return [{'email':json.loads(path.read_text())['email'],'status':'N'}]
            initialized=[False]
            def api(route, body, *args):
                if route=='tenant/super-admin/init': initialized[0]=True; return {}
                self.assertEqual(route,'login'); return {'token':'private'}
            with patch.object(identity,'rows',side_effect=rows),patch.object(identity,'api',side_effect=api) as call:
                identity.initialize_administrator(root,runtime); identity.initialize_administrator(root,runtime)
                self.assertEqual(sum(c.args[0]=='tenant/super-admin/init' for c in call.call_args_list),1)
            self.assertEqual((root/'private/local-administrator.json').stat().st_mode & 0o777,0o600)
