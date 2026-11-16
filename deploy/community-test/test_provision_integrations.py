import ast
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
        hosts[1]['listen'] = ['8081']
        self.assertIn('server_name c.thingspanel.cn 47.92.253.145;', m.prepare_nginx(source, hosts)[0])

    def test_persistent_storage_loopback_and_durability(self):
        service = m.storage_compose()['services']['storage']
        self.assertEqual(service['ports'], ['127.0.0.1:3900:3900'])
        self.assertIn('@sha256:', service['image'])
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
