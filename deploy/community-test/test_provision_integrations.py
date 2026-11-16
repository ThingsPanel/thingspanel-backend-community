import ast
import hashlib
import contextlib
import importlib.util
import io
import json
import pathlib
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

if sys.platform == 'win32':
    sys.modules.setdefault('fcntl', types.SimpleNamespace(LOCK_EX=2, flock=lambda *args: None))
spec = importlib.util.spec_from_file_location('integrations', pathlib.Path(__file__).with_name('provision_integrations.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

NGINX = 'server {\n listen 80 default_server;\n root /home/thingspanel/nginx/www/dist;\n}\n'
KEYS = {'access': 'GK'+'a'*32, 'secret': 'b'*64, 'rpc': 'c'*64}


class IntegrationTests(unittest.TestCase):
    def test_host_python_compatibility(self):
        ast.parse(pathlib.Path(m.__file__).read_text(), feature_version=(3, 6))

    def test_sso_preserves_other_secrets_and_is_idempotent(self):
        source = 'AUTH_SECRET=existing\nDATABASE_URL=existing\n'
        updated, endpoint = m.sso_env(source)
        self.assertEqual(endpoint, m.PLATFORM_ENDPOINT)
        self.assertTrue(updated.startswith(source))
        self.assertEqual((updated, endpoint), m.sso_env(updated))
        with self.assertRaises(RuntimeError):
            m.sso_env('THINGSPANEL_API_URL=a\nexport THINGSPANEL_API_URL=b')

    def test_sso_keeps_valid_operator_endpoint_and_rejects_unsafe_roots(self):
        source = 'AUTH_SECRET=keep\nexport THINGSPANEL_API_URL="https://platform.example:9443/" # operator\n'
        self.assertEqual(m.sso_env(source), (source, 'https://platform.example:9443/'))
        for value in ('https://user:password@host', 'https://host/api/v1', 'https://host?token=a', '$(secret)', 'http://host:invalid', 'https://host a'):
            with self.assertRaises(RuntimeError):
                m.sso_env('THINGSPANEL_API_URL='+value)

    def test_recovery_continues_after_config_and_service_failures(self):
        writes=[]
        def write(path, value):
            writes.append(path)
            if path == 'first': raise OSError('private diagnostic must not escape')
        with patch.object(m, 'atomic_write', write):
            self.assertEqual(m.restore_configurations({'first':b'a','second':b'b'}), ['configuration_0'])
        self.assertEqual(writes, ['first','second'])
        calls=[]
        def run(args, env=None):
            calls.append(args[0])
            if args[0] in ('nginx','docker'): raise RuntimeError('secret stderr')
        with patch.object(m, 'run', run):
            self.assertEqual(m.restore_services('http://old'), ['nginx','encore'])
        self.assertEqual(calls, ['nginx','docker','pm2','pm2'])

    def test_existing_provider_is_not_replaced(self):
        for kind in ('s3', 'gcs'):
            config = {'object_storage': [{'type': kind, 'endpoint': 'operator', 'buckets': {m.BUCKET: {'name': 'custom'}}}], 'secrets': {'x': 'keep'}}
            updated, created = m.runtime_storage(config, KEYS)
            self.assertFalse(created)
            self.assertEqual(updated, config)

    def test_new_provider_preserves_other_runtime_resources(self):
        config = {'object_storage': [], 'sql_servers': ['keep'], 'secrets': {'x': 'keep'}}
        updated, created = m.runtime_storage(config, KEYS)
        self.assertTrue(created)
        self.assertEqual(updated['sql_servers'], config['sql_servers'])
        self.assertEqual(updated['secrets'], config['secrets'])
        self.assertEqual(config['object_storage'], [])
        self.assertEqual(m.runtime_storage(updated, KEYS), (updated, False))

    def test_proxy_preserves_signature_and_rejects_modified_route(self):
        result = m.nginx_storage(NGINX)
        self.assertEqual(result, m.nginx_storage(result))
        self.assertIn('proxy_pass http://127.0.0.1:3900;', result)
        self.assertIn('proxy_set_header Host $http_host;', result)
        self.assertIn('access_log off;', result)
        with self.assertRaises(RuntimeError):
            m.nginx_storage(result.replace('access_log off;', 'access_log on;'))
        with self.assertRaises(RuntimeError):
            m.nginx_storage(NGINX.replace('default_server', ''))

    def test_loaded_routes_redact_headers_and_bind_only_unclaimed_ip(self):
        source = 'server {\n listen 80;\n server_name c.thingspanel.cn;\n root /home/thingspanel/nginx/www/dist;\n proxy_set_header Authorization secret;\n}\n'
        dump = '# configuration file /etc/nginx/conf.d/community.conf:\n'+source+'\n# configuration file /etc/nginx/conf.d/thingsvis.conf:\nserver {listen 80 default_server; server_name thingsvis.example; root /var/www/studio;}\n'
        hosts = m.loaded_vhosts(dump)
        self.assertNotIn('secret', json.dumps(hosts))
        updated, plan = m.prepare_nginx(source, hosts)
        self.assertIn('server_name c.thingspanel.cn 47.92.253.145;', updated)
        self.assertTrue(plan['default_server_unchanged'])
        refreshed = m.loaded_vhosts(dump.replace(source, updated))
        self.assertEqual(m.prepare_nginx(updated, refreshed)[0], updated)
        hosts[1]['server_name'] = ['47.92.253.145']
        with self.assertRaises(RuntimeError): m.prepare_nginx(source, hosts)
        for port in ('8081', '8085', '443 ssl'):
            hosts[1]['listen'] = [port]
            self.assertIn('server_name c.thingspanel.cn 47.92.253.145;', m.prepare_nginx(source, hosts)[0])
            self.assertIsNone(m.existing_ip_route(hosts))
        hosts[1]['listen'] = ['80']
        with patch.object(m, 'checked_path', lambda p: p):
            self.assertEqual(m.existing_ip_route(hosts).as_posix(), '/etc/nginx/conf.d/thingsvis.conf')

    def test_persistent_storage_loopback_and_durability(self):
        service = m.storage_compose()['services']['storage']
        self.assertEqual(service['ports'], ['127.0.0.1:3900:3900'])
        self.assertEqual(service['image'], m.LOCAL_IMAGE)
        self.assertEqual(service['pull_policy'], 'never')
        self.assertNotIn('tmpfs', service)
        self.assertIn('metadata_fsync = true', m.garage_config(KEYS))
        self.assertIn('data_fsync = true', m.garage_config(KEYS))

    def test_existing_ip_vhost_keeps_default_and_all_other_routes(self):
        source = 'server {\n listen 80 default_server;\n server_name 47.92.253.145 other.example;\n location /thingsvis {proxy_pass http://127.0.0.1:8000;}\n}\n'
        updated = m.add_bound_ip_location(source)
        self.assertIn('server_name 47.92.253.145 other.example;', updated)
        self.assertIn('location /thingsvis {proxy_pass http://127.0.0.1:8000;}', updated)
        self.assertEqual(updated.replace(m.MANAGED_LOCATION, ''), source)
        self.assertEqual(m.add_bound_ip_location(updated), updated)

    def test_storage_bundle_requires_pinned_identity_and_content(self):
        with tempfile.TemporaryDirectory() as tmp:
            package = pathlib.Path(tmp).resolve()/'image.tar.gz'
            package.write_bytes(b'archive fixture')
            manifest = {'image_ref':m.IMAGE,'local_image':m.LOCAL_IMAGE,'image_id':'sha256:'+'a'*64,
                        'architecture':'amd64','image_bytes':1024,'bytes':package.stat().st_size,
                        'sha256':hashlib.sha256(package.read_bytes()).hexdigest()}
            m.validate_storage_bundle(package,manifest)
            for key, value in (('image_ref','another'),('image_bytes',m.STORAGE_IMAGE_LIMIT+1),('architecture','arm64'),('sha256','b'*64)):
                with self.assertRaises(RuntimeError):m.validate_storage_bundle(package,dict(manifest,**{key:value}))

    def test_loaded_image_accepts_engine_size_accounting_not_identity_changes(self):
        manifest = {'image_id':'sha256:'+'a'*64,'image_bytes':69426824}
        for size in (27669457,69426824):
            m.validate_loaded_storage_image({'Id':manifest['image_id'],'Architecture':'amd64','Size':size},manifest)
        for image_id, size in (('sha256:'+'b'*64,27669457),(manifest['image_id'],m.STORAGE_IMAGE_LIMIT+1)):
            with self.assertRaises(RuntimeError):
                m.validate_loaded_storage_image({'Id':image_id,'Architecture':'amd64','Size':size},manifest)

    def test_failed_restart_restores_all_original_configs_without_data_delete(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp).resolve()
            runtime = root/'runtime.encore.private.json'
            env = root/'.env'
            nginx = root/'nginx.conf'
            lock = root/'lock'
            lock.touch()
            runtime.write_text(json.dumps({'object_storage': [{'type': 's3', 'buckets': {m.BUCKET: {'name': 'existing'}}}]}))
            env.write_text('AUTH_SECRET=keep\n')
            nginx.write_text(NGINX)
            originals = {p: p.read_bytes() for p in (runtime, env, nginx)}
            calls = []
            def run(args, env=None):
                calls.append(args)
                if args == ['pm2', 'jlist']:
                    return json.dumps([{'name': 'thingsvis-server', 'pm2_env': {'THINGSPANEL_API_URL': 'http://old'}}])
                if args == ['nginx', '-T']:
                    return '# configuration file /etc/nginx/conf.d/community.conf:\n'+NGINX
                if args[:2] == ['pm2', 'restart'] and env['THINGSPANEL_API_URL'] == m.PLATFORM_ENDPOINT:
                    raise RuntimeError('injected restart failure')
                return ''
            with patch.multiple(m, ROOT=root, LOCK=lock, NGINX=nginx, SSO_ENV_PATHS=(env,)), patch.object(m, 'run', run), patch.object(sys, 'argv', ['helper', '--apply']), contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(RuntimeError, 'previous configurations restored'):
                    m.main()
            for path, data in originals.items():
                self.assertEqual(path.read_bytes(), data)
            self.assertFalse(any('rm' in args or 'down' in args for args in calls))


if __name__ == '__main__':
    unittest.main()
