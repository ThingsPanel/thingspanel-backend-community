#!/usr/bin/env python3
"""Fail-closed, opt-in local integration fixture. No existing stack is adopted."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess

PROJECT = 'tp-develop-local'
DATABASES = {'device_metadata': 'devicemetadata/migrations', 'automation': 'automation/migrations',
             'notification': 'l3-core/notification/migrations', 'file_storage': 'fileassets/migrations',
             'connector_runtime': 'l3-core/connectorcatalog/migrations'}
PORTS = [18080, 19999, 24000, 28091, 15555, 16379, 11883]


def run(args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def safe_path(value):
    path = Path(value).expanduser().resolve()
    if not str(path).startswith('/Volumes/80G/') or '500G' in str(path):
        raise ValueError('All runtime/source paths must resolve underneath /Volumes/80G; 500G is forbidden')
    return path


def source(path):
    path = safe_path(path)
    if run(['git', '-C', str(path), 'status', '--porcelain']):
        raise ValueError('Source checkout must be clean')
    return {'path': str(path), 'sha': run(['git', '-C', str(path), 'rev-parse', 'HEAD'])}


def preflight(root):
    safe_path(root)
    if shutil.disk_usage('/').free < 6 * 1024**3:
        raise ValueError('System disk needs 6 GiB free even with external caches; no builds or startup allowed')
    if shutil.disk_usage('/Volumes/80G').free < 12 * 1024**3:
        raise ValueError('80G needs at least 12 GiB free')
    # Inspecting the daemon does not prove its VM disk resides on 80G.
    if os.environ.get('TP_DOCKER_STORAGE_VERIFIED') != '/Volumes/80G':
        raise ValueError('Verify Docker VM disk and build cache are on 80G, then set TP_DOCKER_STORAGE_VERIFIED=/Volumes/80G')
    info = json.loads(run(['docker', 'info', '--format', '{{json .}}']))
    if not info.get('ServerVersion'):
        raise ValueError('Docker daemon unavailable')


def compose(root, *args, **kwargs):
    return run(['docker', 'compose', '--project-name', PROJECT, '--project-directory', str(root),
                '-f', str(root / 'compose.json'), *args], **kwargs)


def prepare(root, backend, frontend):
    if root.exists():
        raise ValueError('Runtime directory must be new; refusing to overwrite/adopt existing data')
    backend, frontend = source(backend), source(frontend)
    root.mkdir(mode=0o700, parents=True)
    for name in ('postgres', 'redis', 'mqtt', 'cache', 'private', 'files'):
        (root / name).mkdir(mode=0o700)
    password = secrets.token_hex(32)
    (root / 'private/postgres-password').write_text(password)
    manifest = {'project': PROJECT, 'backend': backend, 'frontend': frontend, 'root': str(root),
                'status': 'wired-unaccepted'}
    (root / 'manifest.json').write_text(json.dumps(manifest, indent=2))
    services = {
        'postgres': {'image': 'timescale/timescaledb:latest-pg14',
                     'environment': {'POSTGRES_USER': 'postgres', 'POSTGRES_DB': 'ThingsPanel',
                                     'POSTGRES_PASSWORD_FILE': '/run/secrets/postgres-password'},
                     'ports': ['127.0.0.1:15555:5432'],
                     'secrets': ['postgres-password'],
                     'volumes': [str(root / 'postgres') + ':/var/lib/postgresql/data'],
                     'healthcheck': {'test': ['CMD-SHELL', 'pg_isready -U postgres -d ThingsPanel'], 'interval': '5s', 'retries': 24}},
        'redis': {'image': 'redis:6.2.7-alpine', 'ports': ['127.0.0.1:16379:6379'],
                  'volumes': [str(root / 'redis') + ':/data']}}
    # No arbitrary MQTT broker: native owner/plugin identities are required.
    config = {'name': PROJECT, 'services': services,
              'secrets': {'postgres-password': {'file': str(root / 'private/postgres-password')}}}
    from wiring import generate
    config = generate(root, manifest, password, config)
    (root / 'compose.json').write_text(json.dumps(config, indent=2))
    for path in root.rglob('*'):
        if path.is_file():
            path.chmod(0o600)
    print('Prepared isolated native fixture; no services started. Lock images before startup.')


def sql(root, database, statement):
    if database not in ['postgres', 'ThingsPanel', *DATABASES]:
        raise ValueError('Unapproved fixture database')
    # The exact Compose project owns the only allowed target; never accept a DSN.
    return compose(root, 'exec', '-T', 'postgres', 'psql', '-X', '-qAt', '-U', 'postgres',
                   '-d', database, '-v', 'ON_ERROR_STOP=1', input=statement)


def bootstrap(root):
    preflight(root)
    manifest = json.loads((root / 'manifest.json').read_text())
    if manifest['project'] != PROJECT or manifest['root'] != str(root):
        raise ValueError('Fixture ownership mismatch')
    backend = source(manifest['backend']['path'])
    if backend != manifest['backend']:
        raise ValueError('Pinned backend source changed')
    raw = compose(root, 'ps', '--format', 'json')
    try:
        containers = json.loads(raw)
    except json.JSONDecodeError:
        containers = [json.loads(line) for line in raw.splitlines() if line.strip()]
    if not isinstance(containers, list):
        containers = [containers]
    pg = [c for c in containers if c.get('Service') == 'postgres']
    if len(pg) != 1:
        raise ValueError('Exactly one isolated postgres must already be running')
    inspection = json.loads(run(['docker', 'inspect', pg[0]['ID']]))[0]
    if inspection['Config']['Labels'].get('com.docker.compose.project') != PROJECT:
        raise ValueError('Container project mismatch')
    mounts = inspection['Mounts']
    if not any(m['Destination'] == '/var/lib/postgresql/data' and m['Source'] == str(root / 'postgres') for m in mounts):
        raise ValueError('Postgres data mount mismatch')
    repo = Path(backend['path'])
    # Fresh-only bootstrap: partial failures require a new fixture, never automatic destructive cleanup.
    if sql(root, 'ThingsPanel', "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';") != '0':
        raise ValueError('Community database is not empty; refusing migration/adoption')
    sql(root, 'ThingsPanel', 'CREATE TABLE casbin_rule(id bigserial PRIMARY KEY,ptype varchar(100),v0 varchar(100),v1 varchar(100),v2 varchar(100),v3 varchar(100),v4 varchar(100),v5 varchar(100)); CREATE TABLE sys_version(version_number INT PRIMARY KEY,version varchar(255) NOT NULL); INSERT INTO sys_version VALUES(0,\'0.0.0\');')
    version = int(re.search(r'VERSION_NUMBER\s*=\s*(\d+)', (repo / 'pkg/global/global.go').read_text())[1])
    ledger = []
    def apply(db, paths):
        for path in paths:
            content = path.read_text()
            sql(root, db, 'BEGIN; SET LOCAL lock_timeout=\'30s\';\n' + content + '\nCOMMIT;')
            ledger.append({'database': db, 'file': str(path.relative_to(repo)), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()})
            (root / 'migration-ledger.json').write_text(json.dumps(ledger, indent=2))
    apply('ThingsPanel', [repo / 'sql' / f'{n}.sql' for n in range(1, version + 1)])
    sql(root, 'ThingsPanel', f"UPDATE sys_version SET version_number={version},version='local-develop';")
    apply('ThingsPanel', sorted((repo / 'sql/broker').glob('[0-9]*.sql'), key=lambda p: int(p.name.split('_')[0])))
    for db, directory in DATABASES.items():
        if sql(root, 'postgres', f"SELECT count(*) FROM pg_database WHERE datname='{db}';") != '0':
            raise ValueError('Native database already exists; refusing adoption: ' + db)
        sql(root, 'postgres', f'CREATE DATABASE "{db}";')
        sql(root, db, 'CREATE EXTENSION IF NOT EXISTS pgcrypto;')
        paths = sorted((repo / 'encore-api' / directory).glob('*.up.sql'), key=lambda p: int(p.name.split('_')[0]))
        if not paths:
            raise ValueError('Missing native migrations: ' + db)
        apply(db, paths)
        latest = int(paths[-1].name.split('_')[0])
        sql(root, db, f'CREATE TABLE schema_migrations(version bigint PRIMARY KEY,dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES({latest},false);')
    (root / 'bootstrap.complete').write_text(manifest['backend']['sha'])
    print('Fresh fixture SQL applied; backend component migrations still execute at backend startup.')



def load_fixture(root):
    manifest = json.loads((root / 'manifest.json').read_text())
    if manifest['project'] != PROJECT or manifest['root'] != str(root):
        raise ValueError('Fixture ownership mismatch')
    for name in ('backend', 'frontend'):
        if source(manifest[name]['path']) != manifest[name]:
            raise ValueError('Pinned source changed: ' + name)
    return manifest


def lock_images(root):
    preflight(root)
    manifest = load_fixture(root)
    config = json.loads((root / 'compose.json').read_text())
    lock = {}
    for name, service in config['services'].items():
        image = json.loads(run(['docker', 'image', 'inspect', service['image']]))[0]
        expected = manifest['frontend']['sha'] if name == 'frontend' else manifest['backend']['sha']
        if name not in ('postgres', 'redis') and (image['Config'].get('Labels') or {}).get('org.opencontainers.image.revision') != expected:
            raise ValueError('Image source revision label mismatch: ' + name)
        lock[name] = {'requested': service['image'], 'id': image['Id']}
        service['image'] = image['Id']
        service['pull_policy'] = 'never'
    (root / 'images.lock.json').write_text(json.dumps(lock, indent=2))
    (root / 'compose.json').write_text(json.dumps(config, indent=2))
    print('Locked already-local images by immutable ID; no images pulled.')


def up(root, stage):
    preflight(root)
    load_fixture(root)
    if not (root / 'images.lock.json').exists():
        raise ValueError('Run lock-images after building exact pinned sources; no implicit pulls allowed')
    lock = json.loads((root / 'images.lock.json').read_text())
    for name, item in lock.items():
        if json.loads(run(['docker', 'image', 'inspect', item['id']]))[0]['Id'] != item['id']:
            raise ValueError('Missing locked local image: ' + name)
    existing = run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + PROJECT])
    for container in existing.splitlines():
        inspected = json.loads(run(['docker', 'inspect', container]))[0]
        if inspected['Config']['Labels'].get('com.docker.compose.project.working_dir') != str(root):
            raise ValueError('Project name already belongs to another runtime; refusing replacement')
    import socket
    # Do not prohibit the old 8080/9999 stack: it remains running and untouched.
    services = {'foundation': ['postgres', 'redis'], 'native': ['broker-owner', 'mosquitto', 'backend', 'encore-api', 'notification-core', 'automation-scheduler', 'frontend']}[stage]
    if stage == 'native' and not (root / 'bootstrap.complete').exists():
        raise ValueError('Bootstrap must complete before native startup')
    for port in ([15555, 16379] if stage == 'foundation' else [18080,19999,24000,28091,11883]):
        with socket.socket() as sock:
            try:
                sock.bind(('127.0.0.1', port))
            except OSError:
                raise ValueError('Fixture port already occupied; refusing replacement: ' + str(port))
    compose(root, 'up', '-d', '--no-build', '--pull', 'never', *services)
    print('Started fixture stage; integration acceptance still required: ' + stage)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare', 'preflight', 'bootstrap', 'build', 'up', 'lock-images'])
    parser.add_argument('--root', required=True)
    parser.add_argument('--stage', choices=['foundation', 'native'], default='foundation')
    parser.add_argument('--backend')
    parser.add_argument('--frontend')
    args = parser.parse_args()
    root = safe_path(args.root)
    if args.action == 'prepare':
        prepare(root, args.backend, args.frontend)
    elif args.action == 'preflight':
        preflight(root)
    elif args.action == 'lock-images':
        lock_images(root)
    elif args.action == 'up':
        up(root, args.stage)
    elif args.action == 'bootstrap':
        bootstrap(root)
    else:
        raise ValueError('Build execution is intentionally disabled pending coordinated disk/I/O window; see README build commands')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error))
