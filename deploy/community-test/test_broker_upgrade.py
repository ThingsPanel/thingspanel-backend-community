import unittest
import json
import hashlib
import tempfile
import tarfile
import io
from types import SimpleNamespace
from pathlib import Path
from unittest.mock import patch
from broker_upgrade import credential_environment, upgrade_broker, required_import_bytes, check_import_capacity


class CapacityTests(unittest.TestCase):
    def test_streamed_archive_estimate_and_low_disk_rejection(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory) / 'image.tar.gz'
            with tarfile.open(str(image), 'w:gz') as archive:
                entry = tarfile.TarInfo('layer.tar')
                entry.size = 1024
                archive.addfile(entry, io.BytesIO(b'x' * 1024))
            expected = 2048 + 256 * 1024 * 1024
            self.assertEqual(required_import_bytes(image), expected)
            with patch('broker_upgrade.shutil.disk_usage', return_value=SimpleNamespace(free=expected-1)):
                with self.assertRaises(RuntimeError):
                    check_import_capacity(image, lambda args: directory)
            with patch('broker_upgrade.shutil.disk_usage', return_value=SimpleNamespace(free=expected)):
                self.assertEqual(check_import_capacity(image, lambda args: directory), expected)


class BrokerUpgradeTests(unittest.TestCase):
    def setUp(self):
        self.broker = {'Config': {'Env': ['TP_BROKER_TOKEN_FILE=/secrets/token', 'TP_BROKER_CELL=c', 'TP_BROKER_INSTANCE=i']},
                       'Mounts': [{'Source': '/private', 'Destination': '/secrets'}]}
        self.owner = {'NetworkSettings': {'Ports': {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '11883'}]}}}

    def test_maps_existing_identity_without_copying_secret(self):
        env = credential_environment(self.broker, self.owner)
        self.assertEqual(env['TP_BROKER_TOKEN_FILE'].replace('\\', '/'), '/private/token')
        self.assertEqual(env['TP_BROKER_OWNER_URL'], 'http://127.0.0.1:11883')
        self.assertEqual(env['TP_BROKER_INSTANCE'], 'i')

    def test_rejects_missing_mount(self):
        self.broker['Mounts'] = []
        with self.assertRaises(RuntimeError):
            credential_environment(self.broker, self.owner)

    def test_host_network_reuses_explicit_loopback_owner(self):
        self.broker['HostConfig'] = self.owner['HostConfig'] = {'NetworkMode': 'host'}
        self.owner['NetworkSettings']['Ports'] = {}
        self.broker['Config']['Env'].append('TP_BROKER_OWNER_URL=http://127.0.0.1:19876')
        self.assertEqual(credential_environment(self.broker, self.owner)['TP_BROKER_OWNER_URL'], 'http://127.0.0.1:19876')

    def test_host_network_rejects_remote_or_implicit_port(self):
        self.broker['HostConfig'] = self.owner['HostConfig'] = {'NetworkMode': 'host'}
        self.owner['NetworkSettings']['Ports'] = {}
        for endpoint in ['http://example.com:19876', 'http://127.0.0.1', 'http://user:secret@127.0.0.1:19876']:
            self.broker['Config']['Env'] = [e for e in self.broker['Config']['Env'] if not e.startswith('TP_BROKER_OWNER_URL=')]
            self.broker['Config']['Env'].append('TP_BROKER_OWNER_URL=' + endpoint)
            with self.assertRaises(RuntimeError):
                credential_environment(self.broker, self.owner)

    def test_rejects_ambiguous_owner_port(self):
        self.owner['NetworkSettings']['Ports']['9090/tcp'] = [{'HostIp': '127.0.0.1', 'HostPort': '19090'}]
        with self.assertRaises(RuntimeError):
            credential_environment(self.broker, self.owner)


class BrokerReplacementTests(unittest.TestCase):
    def exercise(self, mode):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            incoming = root / 'incoming'
            incoming.mkdir()
            release = root / 'release'
            release.mkdir()
            secret = root / 'token'
            secret.write_text('private-token-must-never-appear-in-commands')
            sha = 'a' * 40
            (incoming / ('broker-' + sha + '.tar.gz')).write_bytes(b'image')
            (incoming / ('broker-' + sha + '.json')).write_text(json.dumps({'commit': sha, 'image_id': 'sha256:new', 'sha256': hashlib.sha256(b'image').hexdigest()}))
            def container(name):
                return {'Id': 'old-' + name, 'Name': '/' + name, 'Image': 'sha256:old',
                        'State': {'Running': True}, 'Config': {'Labels': {
                            'com.docker.compose.project.config_files': '/existing/compose.json',
                            'com.docker.compose.project': 'installed',
                            'com.docker.compose.service': name}}}
            containers = [container('owner'), container('broker')]
            calls = []
            inspect_count = 0
            def run(args):
                nonlocal inspect_count
                calls.append(args)
                if args[:2] == ['docker', 'inspect']:
                    inspect_count += 1
                    if inspect_count == 1:
                        return json.dumps(containers)
                    new = json.loads(json.dumps(containers))
                    for c in new:
                        c['Image'] = 'sha256:new'
                        if mode != 'unchanged':
                            c['Id'] = 'new-' + c['Name']
                    return json.dumps(new)
                if args[:3] == ['docker', 'image', 'inspect']:
                    return json.dumps([{'Id': 'sha256:wrong' if mode == 'wrong-image' else 'sha256:new'}])
                if 'config' in args:
                    return json.dumps({'services': {'owner': {'image': 'old'}, 'broker': {'image': 'old'}}})
                return ''
            env = {'TP_BROKER_OWNER_URL': 'http://127.0.0.1:11883', 'TP_BROKER_TOKEN_FILE': str(secret),
                   'TP_BROKER_CELL': 'cell', 'TP_BROKER_INSTANCE': 'instance'}
            class Response:
                def __enter__(self):
                    return self
                def __exit__(self, *args):
                    pass
                def read(self):
                    return b'{"protocol":1,"ready":true}'
            def urlopen(request, timeout):
                self.assertEqual(request.full_url, 'http://127.0.0.1:11883/v1/credential-leases/ready')
                self.assertEqual(request.get_header('Authorization'), 'Bearer ' + secret.read_text())
                self.assertEqual(json.loads(request.data), {'broker_cell': 'cell', 'broker_instance': 'instance'})
                if mode == 'unready':
                    raise OSError('unavailable')
                return Response()
            with patch('broker_upgrade.check_import_capacity'), patch('broker_upgrade.credential_environment', return_value=env), patch('broker_upgrade.urllib.request.urlopen', side_effect=urlopen), patch('broker_upgrade.time.sleep'):
                if mode == 'success':
                    self.assertEqual(upgrade_broker(root, release, sha, run), env)
                else:
                    with self.assertRaises(RuntimeError):
                        upgrade_broker(root, release, sha, run)
            self.assertNotIn(secret.read_text(), json.dumps(calls))
            starts = [call for call in calls if 'up' in call]
            self.assertEqual(len(starts), 0 if mode == 'wrong-image' else 1 if mode == 'success' else 2)
            if mode in ('unchanged', 'unready'):
                self.assertIn(str(release / 'broker.before.private.json'), starts[-1])

    def test_successful_pair_replacement_and_authenticated_readiness(self):
        self.exercise('success')

    def test_wrong_image_never_replaces_running_services(self):
        self.exercise('wrong-image')

    def test_unchanged_container_rolls_back(self):
        self.exercise('unchanged')

    def test_readiness_failure_rolls_back(self):
        self.exercise('unready')
