import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('mqtt_release', Path(__file__).with_name('mqtt_publisher_release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class PublisherConfiguration(unittest.TestCase):
    def fixture(self):
        return [dict(username='platform', password='private', broker_cell='cell', broker_instance='node',
                     allowed_client_ids=['app', 'readiness'], allowed_device_ids=['old-target'], allowed_tenant_ids=['old-tenant'],
                     principal={'principal_kind': 'service', 'tenant_id': 'system', 'device_id': 'application'},
                     policy={'publish': ['devices/command/+/+'], 'subscribe': ['tp-internal/ingress/v1/#']})], dict(user='platform', **{'pass': 'private'}, client_id='app')

    def test_updates_only_application_identity_and_both_switches(self):
        services, mqtt = self.fixture()
        plugin = {'username': 'plugin', 'plugin_socket': True, 'password': 'other-private'}
        services.append(plugin)
        original = copy.deepcopy(services)
        updated, env = release.publisher_settings(services, mqtt, {'UNRELATED': 'keep'})
        self.assertEqual(services, original)
        self.assertEqual(updated[1], plugin)
        self.assertTrue(updated[0]['target_scoped_publisher'])
        self.assertEqual(updated[0]['target_tenant_scope'], 'platform')
        self.assertEqual(updated[0]['allowed_device_ids'], [])
        self.assertEqual(updated[0]['allowed_tenant_ids'], [])
        self.assertEqual(updated[0]['policy']['subscribe'], original[0]['policy']['subscribe'])
        self.assertEqual(updated[0]['policy']['publish'], release.PUBLISH_FAMILIES)
        self.assertEqual(env, {'UNRELATED': 'keep', 'GOTP_MQTT_TARGET_SCOPED_PUBLISHER': 'true'})

    def test_rejects_unverified_identity_and_unreviewed_policy(self):
        mutations = [lambda s: s.update(password='wrong'), lambda s: s.update(plugin_socket=True),
                     lambda s: s.update(allow_any_client_id=True), lambda s: s.update(allowed_client_ids=['other']),
                     lambda s: s.update(principal={'principal_kind': 'device'}),
                     lambda s: s['policy'].update(publish=['#'])]
        for mutation in mutations:
            services, mqtt = self.fixture()
            mutation(services[0])
            with self.assertRaises(ValueError):
                release.publisher_settings(services, mqtt, {})
        services, mqtt = self.fixture()
        with self.assertRaises(ValueError):
            release.publisher_settings(services * 2, mqtt, {})
        with self.assertRaises(ValueError):
            release.publisher_settings(services, mqtt, {'GOTP_MQTT_PASS': 'overridden-wrong'})

    def test_retains_effective_pm2_identity_over_config(self):
        services, mqtt = self.fixture()
        mqtt['pass'] = 'stale-file-password'
        _, env = release.publisher_settings(services, mqtt, {'GOTP_MQTT_PASS': 'private'})
        self.assertEqual(env['GOTP_MQTT_PASS'], 'private')


class ReleaseIntegrity(unittest.TestCase):
    def test_baseline_requires_reviewed_version_and_both_immutable_images(self):
        current = {'Image': release.DEPLOYED_BROKER_IMAGE}
        release.verify_runtime_baseline(release.DEPLOYED_BACKEND, current, current)
        for version, owner, broker in [
                ('another-release', current, current),
                (release.DEPLOYED_BACKEND, {'Image': 'changed-owner'}, current),
                (release.DEPLOYED_BACKEND, current, {'Image': 'changed-broker'})]:
            with self.assertRaisesRegex(ValueError, 'baseline changed'):
                release.verify_runtime_baseline(version, owner, broker)

    def test_controller_snapshot_checks_content_and_previously_absent_files(self):
        with tempfile.TemporaryDirectory() as folder:
            controller, helper = Path(folder) / 'scope.py', Path(folder) / 'helper.py'
            controller.write_text('reviewed controller')
            snapshot = {str(controller): release.digest(controller), str(helper): None}
            with patch.object(release, 'SCOPE_CONTROLLER_FILES', snapshot):
                self.assertEqual(release.scope_controller_preflight(), snapshot)
                helper.write_text('unexpected replacement helper')
                with self.assertRaises(ValueError):
                    release.scope_controller_preflight()
                helper.unlink()
                controller.write_text('changed controller')
                with self.assertRaises(ValueError):
                    release.scope_controller_preflight()

    def bundle(self, root):
        profile = {'source_commit': 'a' * 40, 'encore_changed': False, 'database_owner_changed': False,
                   'expected_backend_version': release.DEPLOYED_BACKEND,
                   'sources': {'broker': {'baseline': release.BROKER_BASE}, 'backend': {'retained_patches': [release.HTTP_PATCH,release.HTTP_FIX]}}}
        (root / 'ThingsPanel-Go').write_bytes(b'candidate')
        (root / 'broker-image.tar.gz').write_bytes(b'image')
        (root / 'RELEASE_PROFILE.json').write_text(json.dumps(profile))
        manifest = {'profile': 'mqtt-target-publisher', 'commit': 'a' * 40,
                    'image_size_bytes': 1024,
                    'tool_sha256': release.digest(release.__file__),
                    'sha256': {p.name: release.digest(p) for p in root.iterdir()}}
        (root / 'manifest.json').write_text(json.dumps(manifest))
        return manifest

    def test_bundle_rejects_tampering_and_missing_http_preservation(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            manifest = self.bundle(root)
            self.assertEqual(release.verify_bundle(root)['commit'], 'a' * 40)
            (root / 'ThingsPanel-Go').write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'checksum'):
                release.verify_bundle(root)
            (root / 'ThingsPanel-Go').write_bytes(b'candidate')
            path = root / 'RELEASE_PROFILE.json'
            profile = json.loads(path.read_text())
            profile['sources']['backend']['retained_patches'] = []
            path.write_text(json.dumps(profile))
            manifest['sha256'][path.name] = release.digest(path)
            (root / 'manifest.json').write_text(json.dumps(manifest))
            with self.assertRaisesRegex(ValueError, 'HTTP'):
                release.verify_bundle(root)

    def test_private_stage_does_not_overwrite_existing_evidence(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / 'state.json'
            release.private(path, {'original': True})
            with self.assertRaises(ValueError):
                release.private(path, {'original': False})
            self.assertEqual(json.loads(path.read_text()), {'original': True})

    def test_restore_can_restart_process_missing_after_failed_start(self):
        calls = []
        def run(args, *unused):
            calls.append([str(v) for v in args])
            return b'[]' if args[-1] == 'jlist' else b''
        with patch.object(release, 'run', side_effect=run):
            state = {'pm2': 'pm2'}
            release.stop_backend(state)
            release.start_backend(Path('/release'), state, 'before')
        self.assertFalse(any('delete' in c or 'stop' in c for c in calls))
        self.assertTrue(any('start' in c and str(Path('/release') / 'backend.before.private.json') in c for c in calls))

    def test_apply_failure_restores_policy_binary_compose_and_environment(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'ThingsPanel-Go').write_bytes(b'candidate')
            proc = {'name': 'thingspanel-backend-community', 'pm2_env': {'pm_exec_path': 'binary', 'pm_cwd': 'cwd', 'env': {'KEEP': 'yes'}}}
            state = dict(staged_files={}, project='project', working_dir='cwd', services=['owner', 'broker'], containers=['owner', 'broker'],
                         images_before=['old', 'old'], container_ids=['owner-id', 'broker-id'], compose_files={},
                         process_spec_sha256=release.value_digest(release.process_spec(proc)), pm2='pm2', binary='binary',
                         binary_before_sha256='x', config_path='config', config_sha256='x', policy_path='policy', policy_sha256='x',
                         next_version='next', old_version='before')
            (root / 'stage.private.json').write_text(json.dumps(state))
            actions = []
            def run(args, *unused):
                args = [str(a) for a in args]
                if args[-1] == 'jlist': return json.dumps([proc]).encode()
                if args[:3] == ['docker', 'image', 'inspect']: return b'[{"Id":"new-image"}]'
                if args[-1] == '--version': return b'next'
                actions.append(args)
                return b''
            def compose(root, state, suffix):
                actions.append(['compose', suffix])
                if suffix == 'next': raise RuntimeError('candidate broker failed')
            with patch.object(release, 'verify_bundle', return_value={'image_id': 'new-image', 'commit': 'a'*40}), \
                 patch.object(release, 'digest', return_value='x'), patch.object(release, 'run', side_effect=run), \
                 patch.object(release, 'inspect', side_effect=lambda c: {'Image': 'old', 'Id': c+'-id'}), \
                 patch.object(release, 'resource_preflight'), \
                 patch.object(release, 'replace_file', side_effect=lambda src,dst: actions.append(['replace', Path(src).name, dst])), \
                 patch.object(release, 'compose_up', side_effect=compose), patch.object(release, 'wait_ready') as ready:
                with self.assertRaisesRegex(RuntimeError, 'previous runtime restored'):
                    release.apply_release(root)
                ready.assert_called_once_with(state, 'before')
            self.assertIn(['compose', 'before'], actions)
            self.assertIn(['replace', 'services.before.private.json', 'policy'], actions)
            self.assertIn(['replace', 'backend.before', 'binary'], actions)
            self.assertTrue(any('start' in a and str(root/'backend.before.private.json') in a for a in actions))

    def test_process_spec_preserves_other_deployments_environment(self):
        process = {'name':'thingspanel-backend-community','pm2_env':{'pm_exec_path':'binary','pm_cwd':'cwd',
                   'env':{'HTTP_TOKEN':'retained'}, 'GOTP_HTTP_ENABLED':'true', 'kill_timeout':12000}}
        spec = release.process_spec(process)
        self.assertEqual(spec['env']['HTTP_TOKEN'], 'retained')
        self.assertEqual(spec['env']['GOTP_HTTP_ENABLED'], 'true')
        self.assertEqual(spec['kill_timeout'], 12000)

    def test_capacity_rejects_low_memory_before_docker_access(self):
        with patch.object(Path, 'read_text', return_value='MemAvailable: 1024 kB\n'), patch.object(release, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'memory'):
                release.resource_preflight(Path('/candidate'), {'image_size_bytes':1024})
            run.assert_not_called()

    def test_capacity_reserves_image_and_binary_rollback_space(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            (root/'ThingsPanel-Go').write_bytes(b'candidate')
            usage=type('Usage',(),{'free':1024})()
            with patch.object(Path,'read_text',return_value='MemAvailable: 1048576 kB\n'), \
                 patch.object(release,'run',return_value=str(root).encode()), patch.object(release.shutil,'disk_usage',return_value=usage):
                with self.assertRaisesRegex(ValueError,'rollback disk'):
                    release.resource_preflight(root,{'image_size_bytes':1024**3})

    def test_stale_explicit_rollback_does_not_replace_newer_binary(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            state={'staged_files':{},'binary':'live-binary'}
            (root/'stage.private.json').write_text(json.dumps(state))
            with patch.object(release,'verify_bundle',return_value={'sha256':{'ThingsPanel-Go':'candidate'}}), \
                 patch.object(release,'digest',return_value='third-party-new-release'), patch.object(release,'replace_file') as replace:
                with self.assertRaisesRegex(ValueError,'stale rollback'):
                    release.apply_release(root,rollback_only=True)
                replace.assert_not_called()


if __name__ == '__main__':
    unittest.main()
