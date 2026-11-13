"""Provision missing native resources for an explicitly selected new-device rollout.

Existing databases and credentials are never reset. SQL is read from the verified
main release archive; secrets remain in private server files and stdin to psql.
"""
import base64
import hashlib
import json
import os
import pathlib
import re
import secrets
import subprocess
import sys
import tarfile

from upgrade_platform_config import convert_platform_config

ROOT = pathlib.Path('/opt/tp-community-actions')
RUNTIME = pathlib.Path('/opt/tp-community-main-20261028')


def sql(database, statement):
    result = subprocess.run(['docker', 'exec', '-i', 'timescaledb', 'psql', '-X', '-q', '-At',
        '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', database], input=statement.encode(),
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
    if result.returncode:
        raise RuntimeError('Native resource provisioning SQL failed; no credentials printed')
    return result.stdout.decode().strip()


def private_write(path, data):
    temp = path.with_name(path.name + '.native-next')
    with temp.open('wb') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(str(temp), str(path))


def migrations(archive, manifest, directory):
    result = []
    for name, digest in manifest['sha256'].items():
        if not name.startswith('encore-api/' + directory + '/') or not name.endswith('.up.sql'):
            continue
        filename = pathlib.PurePosixPath(name).name
        if not re.fullmatch(r'[0-9]+_[A-Za-z0-9_]+\.up\.sql', filename):
            raise RuntimeError('Invalid migration name')
        raw = archive.extractfile(name).read()
        if hashlib.sha256(raw).hexdigest() != digest:
            raise RuntimeError('Migration checksum mismatch')
        result.append((int(filename.split('_', 1)[0]), filename, digest, raw.decode()))
    result.sort()
    if not result or len({row[0] for row in result}) != len(result):
        raise RuntimeError('Missing or duplicate migrations')
    return result


def align_target_publisher(platform, services):
    mqtt = platform.get('mqtt', {})
    if mqtt.get('input_profile') != 'ingress_v1':
        return
    if not mqtt.get('username') or not mqtt.get('password') or not mqtt.get('client_id'):
        raise RuntimeError('MQTT service identity is incomplete')
    matching = [entry for entry in services if entry.get('username') == mqtt['username']
                and entry.get('password') == mqtt['password']
                and mqtt['client_id'] in entry.get('allowed_client_ids', [])]
    if len(matching) != 1 or matching[0].get('plugin_socket'):
        raise RuntimeError('MQTT publisher identity does not uniquely match broker owner')
    # Follow the owner's existing authority; never expand an allowlist or invent credentials.
    mqtt['target_scoped_publisher'] = matching[0].get('target_scoped_publisher') is True


def provision_target_publisher(platform):
    if platform.get('mqtt', {}).get('input_profile') != 'ingress_v1':
        return
    process = subprocess.run(['docker', 'inspect', 'tp-community-main-47-broker-owner-1'],
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    if process.returncode:
        raise RuntimeError('Broker owner configuration is unavailable')
    info = json.loads(process.stdout)[0]
    mounts = []
    for mount in info.get('Mounts', []):
        if mount.get('Destination') == '/run/secrets/broker-services.json':
            mounts.append(mount['Source'])
        elif mount.get('Destination') == '/run/secrets':
            mounts.append(str(pathlib.Path(mount['Source']) / 'broker-services.json'))
    if len(mounts) != 1:
        raise RuntimeError('Expected one broker service identity mount')
    services = json.loads(pathlib.Path(mounts[0]).read_text())
    if isinstance(services, dict):
        services = services.get('services')
    if not isinstance(services, list):
        raise RuntimeError('Invalid broker service identity document')
    align_target_publisher(platform, services)


def main(sha):
    import fcntl
    import yaml
    if not re.fullmatch('[0-9a-f]{40}', sha):
        raise ValueError('Invalid revision')
    os.umask(0o077)
    mode = json.loads((ROOT / 'rollout-mode.private.json').read_text())
    if (mode.get('mode') != 'new-devices-preserve-legacy' or
            mode.get('source_database') != 'ThingsPanel' or mode.get('legacy_data_preserved') is not True):
        raise RuntimeError('Explicit new-device rollout mode required')
    with (ROOT / 'deployment.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        paths = [RUNTIME / 'runtime.encore.private.json', RUNTIME / 'compose.encore.private.json',
                 pathlib.Path('/home/thingspanel/thingspanel-backend-community/configs/conf.yml')]
        backup = ROOT / ('native-provision-' + sha)
        backup.mkdir(mode=0o700, exist_ok=True)
        for path in paths:
            saved = backup / (path.name + '.before')
            if not saved.exists():
                with saved.open('xb') as stream:
                    stream.write(path.read_bytes())
        runtime = json.loads(paths[0].read_text())
        compose = json.loads(paths[1].read_text())
        platform = yaml.safe_load(paths[2].read_text())
        servers = runtime['sql_servers']
        if len(servers) != 1:
            raise RuntimeError('Review multiple SQL servers before provisioning')
        databases = servers[0]['databases']
        native = [name for name, value in databases.items() if value.get('name') == 'device_metadata']
        if native != [mode.get('native_database')]:
            raise RuntimeError('Native database does not match rollout mode')
        env = compose['services']['encore-api']['environment']
        with tarfile.open(str(ROOT / 'incoming' / ('encore-' + sha + '.tar.gz'))) as archive:
            manifest = json.load(archive.extractfile('manifest.json'))
            if manifest.get('commit') != sha:
                raise RuntimeError('Release revision mismatch')
            for logical, directory in [('file_storage', 'fileassets/migrations'),
                                       ('connector_runtime', 'l3-core/connectorcatalog/migrations')]:
                matches = [name for name, value in databases.items() if value.get('name') == logical]
                if len(matches) > 1:
                    raise RuntimeError('Ambiguous native resource')
                if matches:
                    continue
                physical = 'tp_' + logical + '_main'
                role = physical
                credentials = ROOT / 'native-resource-credentials'
                credentials.mkdir(mode=0o700, exist_ok=True)
                password_path = credentials / (logical + '.password.private')
                if password_path.exists():
                    password = password_path.read_text()
                else:
                    password = secrets.token_hex(32)
                    private_write(password_path, password.encode())
                if sql('postgres', "SELECT count(*) FROM pg_roles WHERE rolname='" + role + "'") == '0':
                    sql('postgres', 'CREATE ROLE "' + role + '" LOGIN PASSWORD \'' + password + "'")
                if sql('postgres', "SELECT count(*) FROM pg_database WHERE datname='" + physical + "'") == '0':
                    sql('postgres', 'CREATE DATABASE "' + physical + '" OWNER "' + role + '"')
                rows = migrations(archive, manifest, directory)
                if sql(physical, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'") == '0':
                    script = 'BEGIN; SET LOCAL ROLE "' + role + '";\n'
                    script += 'CREATE TABLE deployment_schema_ledger(version bigint PRIMARY KEY,filename text NOT NULL,checksum text NOT NULL);\n'
                    script += 'CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY,dirty boolean NOT NULL);\n'
                    for version, filename, digest, body in rows:
                        script += body + "\nINSERT INTO deployment_schema_ledger VALUES ({},'{}','{}');\n".format(version, filename, digest)
                    script += 'INSERT INTO schema_migrations VALUES ({},false); COMMIT;'.format(rows[-1][0])
                    sql(physical, script)
                actual = json.loads(sql(physical, "SELECT coalesce(json_agg(t ORDER BY version),'[]') FROM deployment_schema_ledger t"))
                expected = [dict(version=v, filename=n, checksum=d) for v, n, d, _ in rows]
                if actual != expected:
                    raise RuntimeError('Existing resource migration history differs')
                key = 'DB_' + logical.upper() + '_PASSWORD'
                env[key] = password
                databases[physical] = {'name': logical, 'username': role, 'password': {'$env': key}}
        for service in ['devicemetadata', 'fileassets', 'connectorcatalog']:
            if service not in {item if isinstance(item, str) else item.get('name') for item in runtime['hosted_services']}:
                runtime['hosted_services'].append(service)
        if not platform.get('relational'):
            converted = convert_platform_config(platform)
            # Keep the retired block until the old process is replaced, and for rollback.
            platform['relational'] = converted['relational']
        if not env.get('METADATA_CREDENTIAL_KEYRING_B64'):
            key = base64.b64encode(secrets.token_bytes(32)).decode()
            env['METADATA_CREDENTIAL_KEYRING_B64'] = base64.b64encode(json.dumps({'main': key}).encode()).decode()
            env['METADATA_CREDENTIAL_ACTIVE_KEY_REF'] = 'main'
        if not env.get('TP_FILE_ASSET_KEYRING'):
            env['TP_FILE_ASSET_KEYRING'] = json.dumps({'activeKeyId': 'main', 'keys': {'main': base64.b64encode(secrets.token_bytes(32)).decode()}})
        env.setdefault('TP_DEVICE_CONNECTION_ENDPOINT', 'mqtt://c.thingspanel.cn:1883')
        env['TP_BACKGROUND_WORKERS'] = 'on'
        env['TP_RUNTIME_OUTBOX_DISPATCH'] = 'on'
        provision_target_publisher(platform)
        previous = [path.read_bytes() for path in paths]
        next_values = [json.dumps(runtime, indent=2).encode(), json.dumps(compose, indent=2).encode(),
                       yaml.safe_dump(platform, allow_unicode=True, default_flow_style=False).encode()]
        try:
            for path, value in zip(paths, next_values):
                private_write(path, value)
        except Exception:
            for path, value in zip(paths, previous):
                private_write(path, value)
            raise
        print(json.dumps({'status': 'provisioned', 'commit': sha, 'legacy_database_unchanged': True,
                          'scope': 'new-devices-preserve-legacy', 'restarted': False}))


if __name__ == '__main__':
    main(sys.argv[1])
