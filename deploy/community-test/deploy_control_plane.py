"""Explicit first-stage deployment: existing device owner and databases stay put.

No migrations, device import, credential reissue, Gin restart or database switch.
Both service images and the composed frontend are verified before replacement.
Private configuration and previous releases remain available for rollback.
"""
import fcntl
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import time
import urllib.request
from encore_migrations import pending_migrations, source_migrations

SERVICES = {'encore-api': {'authn', 'thingmodel', 'automation'},
            'notification-core': {'authn', 'notification'}}
DATABASES = {'native_main_20261028': ('device_metadata', 'devicemetadata'),
             'automation_main_20261028': ('automation', 'automation'),
             'notification_main_20261028': ('notification', 'l3-core/notification')}
DEVICE_SOURCE = 'a46f1dd8866b3c7d89328c0bc7b722e46c703581'


def capability_route(source):
    marker = 'location /api {'
    if source.count(marker) != 1 or 'location = /api/v1/auth/login-options' in source:
        raise ValueError('Authentication capability route needs explicit review')
    route = ('# Staged capability discovery; password/session owner remains Community.\n'
             '    location = /api/v1/auth/login-options {\n'
             '        proxy_pass http://127.0.0.1:14000;\n'
             '        add_header Cache-Control "no-store" always;\n'
             '    }\n\n    ')
    return source.replace(marker, route + marker)


def validate_runtime(configs):
    for service, expected in SERVICES.items():
        value = configs[service]
        if set(value.get('hosted_services', [])) != expected:
            raise ValueError('Control-plane deployment cannot change hosted service ownership')
        mappings = {physical: db['name'] for server in value.get('sql_servers', [])
                    for physical, db in server.get('databases', {}).items()}
        if mappings != {name: row[0] for name, row in DATABASES.items()}:
            raise ValueError('Control-plane deployment cannot switch resource databases')


def validate_frontend(profile, commit):
    if (profile.get('profile') != 'control-plane' or profile.get('source_commit') != commit
            or profile.get('device_source_commit') != DEVICE_SOURCE
            or profile.get('device_owner') != 'community'):
        raise ValueError('Frontend is not the approved device-preserving build')
    paths = {entry['path'] for entry in profile.get('overrides', [])}
    required = {'src/views/device/manage/index.vue', 'src/views/device/details/index.vue',
                'src/views/device/config/index.vue', 'src/views/device/config-detail/index.vue',
                'src/views/device/grouping/index.vue'}
    if not required <= paths or any(path.endswith('give-an-alarm.vue') for path in paths):
        raise ValueError('Device pages or the updated alarm page are not preserved')


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def unpack(archive, destination):
    with tarfile.open(str(archive)) as tf:
        for member in tf.getmembers():
            p = pathlib.PurePosixPath(member.name)
            if p.is_absolute() or '..' in p.parts or not (member.isfile() or member.isdir()):
                raise ValueError('Unsafe release archive')
            member.uid, member.gid, member.uname, member.gname = os.getuid(), os.getgid(), '', ''
        tf.extractall(str(destination))


def verify_files(root, manifest):
    for name, expected in manifest['sha256'].items():
        p = pathlib.PurePosixPath(name)
        if p.is_absolute() or '..' in p.parts or digest(root / name) != expected:
            raise ValueError('Release checksum mismatch')


def deploy(backend_sha, frontend_sha):
    if not all(re.fullmatch('[0-9a-f]{40}', sha) for sha in (backend_sha, frontend_sha)):
        raise ValueError('Invalid release identity')
    os.umask(0o077)
    root = pathlib.Path('/opt/tp-community-actions')
    runtime = pathlib.Path('/opt/tp-community-main-20261028')
    target = pathlib.Path('/home/thingspanel/nginx/www/dist')
    lock = (root / 'deployment.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX)
    release = root / 'control-plane-releases' / (backend_sha + '-' + frontend_sha)
    if release.exists():
        raise RuntimeError('Release directory exists; inspect its recorded result before retrying')
    if shutil.disk_usage(str(root)).free < 1024 ** 3:
        raise RuntimeError('At least 1 GiB free disk is required; no files were removed')
    release.mkdir(parents=True, mode=0o700)

    def run(args, data=None, timeout=180):
        p = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
        if p.returncode:
            (release / 'failure.private.log').write_bytes(p.stdout + p.stderr)
            raise RuntimeError('Deployment command failed; details retained privately')
        return p.stdout

    def sql(database, query):
        return json.loads(run(['docker', 'exec', '-i', 'timescaledb', 'psql', '-X', '-q', '-At',
                              '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', database],
                             ('BEGIN READ ONLY; SET LOCAL statement_timeout=\'30s\';' + query + ';COMMIT;').encode()))

    def get(path, token=None):
        headers = {'Host': 'c.thingspanel.cn', 'Cache-Control': 'no-cache'}
        if token:
            headers.update({'Authorization': 'Bearer ' + token, 'x-token': token})
        with urllib.request.urlopen(urllib.request.Request('http://127.0.0.1' + path, headers=headers), timeout=15) as r:
            return r.read()

    bundle = release / 'backend'
    bundle.mkdir()
    unpack(root / 'incoming' / ('control-plane-backend-' + backend_sha + '.tar.gz'), bundle)
    manifest = json.loads((bundle / 'manifest.json').read_text())
    if manifest['commit'] != backend_sha or manifest['profile'] != 'control-plane':
        raise ValueError('Backend release identity mismatch')
    verify_files(bundle, manifest)
    site = release / 'site'
    site.mkdir()
    unpack(root / 'incoming' / ('control-plane-frontend-' + frontend_sha + '.tar.gz'), site)
    fm = json.loads((site / 'manifest.json').read_text())
    if fm['commit'] != frontend_sha or fm['profile'] != 'control-plane':
        raise ValueError('Frontend release identity mismatch')
    verify_files(site, fm)
    profile = json.loads((site / 'RELEASE_PROFILE.json').read_text())
    validate_frontend(profile, frontend_sha)
    if not (site / 'index.html').is_file() or not (site / 'mp-device/index.html').is_file():
        raise ValueError('Missing application entry')
    compose = runtime / 'compose.encore.private.json'
    original = compose.read_bytes()
    config = json.loads(original)
    if any(config['services'][name].get('environment', {}).get('TP_AUTH_MODE') != 'legacy-jwt' for name in SERVICES):
        raise ValueError('This stage preserves the existing Community session owner')
    configs = {name: json.loads((runtime / filename).read_text()) for name, filename in
               [('encore-api', 'runtime.encore.private.json'), ('notification-core', 'runtime.notification.private.json')]}
    validate_runtime(configs)
    for database, (_, folder) in DATABASES.items():
        schema = sql(database, "SELECT json_build_object('schema',(SELECT json_agg(t) FROM schema_migrations t),'ledger',(SELECT json_agg(t) FROM deployment_schema_ledger t))")
        if pending_migrations(source_migrations(bundle / 'encore-api' / folder / 'migrations'), schema['ledger'], schema['schema']):
            raise ValueError('First-stage deployment requires an already compatible schema; migrations are not applied here')
    baseline = sql('ThingsPanel', "SELECT json_build_object('devices',(SELECT count(*) FROM devices),'templates',(SELECT count(*) FROM device_configs),'groups',(SELECT count(*) FROM groups),'members',(SELECT count(*) FROM r_group_device))")
    if baseline['devices'] < 48513:
        raise ValueError('Unexpected source device count; inspect before deployment')
    original_version = json.loads(get('/api/v1/sys_version'))
    source_frontend = get('/SOURCE_COMMIT').decode().strip()
    if source_frontend != DEVICE_SOURCE:
        raise ValueError('Online frontend has changed since the approved device baseline')
    # Backup all affected owner databases without touching the source platform DB.
    backups = {}
    for database in DATABASES:
        path = release / (database + '.private.dump')
        with path.open('wb') as output:
            result = subprocess.run(['docker', 'exec', 'timescaledb', 'pg_dump', '-U', 'postgres', '-Fc', database],
                                    stdout=output, stderr=subprocess.PIPE, timeout=180)
        if result.returncode or path.stat().st_size == 0:
            raise RuntimeError('Owner database backup failed')
        with path.open('rb') as input_stream:
            checked = subprocess.run(['docker', 'exec', '-i', 'timescaledb', 'pg_restore', '--list'],
                                     stdin=input_stream, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=60)
        if checked.returncode:
            raise RuntimeError('Owner backup archive is not readable')
        backups[database] = digest(path)
    (release / 'compose.before.private.json').write_bytes(original)
    run(['docker', 'load', '-i', str(bundle / 'image.tar.gz')], timeout=240)
    image = 'tp-community-control-plane:source-' + backend_sha
    inspect = json.loads(run(['docker', 'image', 'inspect', image]))[0]
    if inspect['Id'] != manifest['image_id']:
        raise ValueError('Loaded image identity mismatch')
    for name in SERVICES:
        config['services'][name]['image'] = image
    args = ['docker', 'compose', '-f', str(compose)]
    run(['nginx', '-t'])
    nginx = pathlib.Path('/etc/nginx/conf.d/community.conf')
    nginx_before = nginx.read_bytes()
    nginx_next = capability_route(nginx_before.decode('utf-8')).encode('utf-8')
    (release / 'nginx.before.private.conf').write_bytes(nginx_before)
    previous = os.readlink(str(target)) if target.is_symlink() else None
    backup = target.with_name('dist.before-control-plane-' + frontend_sha)
    link = target.with_name('dist.next-control-plane-' + frontend_sha)
    if os.path.lexists(str(link)) or (previous is None and backup.exists()):
        raise ValueError('Frontend rollback path already exists')
    switched = False

    def replace_config(content):
        temporary = compose.with_name('compose.control-plane-next.private.json')
        temporary.write_bytes(content)
        os.replace(str(temporary), str(compose))

    def login_tenant():
        qa = json.loads((runtime / 'qa-tenant.private.json').read_text())
        req = urllib.request.Request('http://127.0.0.1:9999/api/v1/login',
                                     data=json.dumps({'email': qa['email'], 'password': qa['password']}).encode(),
                                     headers={'Content-Type': 'application/json'})
        token = json.load(urllib.request.urlopen(req, timeout=15)).get('data', {}).get('token')
        if not token:
            raise RuntimeError('Tenant login failed')
        return token

    token = login_tenant()

    def verify_tenant():
        for path in ['/api/v1/device?page=1&page_size=1', '/api/v2/thing-models',
                     '/api/v2/notification-groups', '/automation-core/v1/rules?limit=5',
                     '/automation-core/v1/alarms?limit=5']:
            data = json.loads(get(path, token))
            if isinstance(data, dict) and data.get('code', 200) not in (0, 200):
                raise RuntimeError('Tenant business API rejected the request')

    try:
        replace_config(json.dumps(config, indent=2).encode())
        run(args + ['up', '-d', '--no-deps'] + list(SERVICES))
        for attempt in range(45):
            try:
                for name in SERVICES:
                    cid = run(args + ['ps', '-q', name]).decode().strip()
                    state = json.loads(run(['docker', 'inspect', cid]))[0]
                    if state['Image'] != manifest['image_id'] or not state['State']['Running']:
                        raise RuntimeError('Service image is not running')
                verify_tenant()
                break
            except Exception:
                if attempt == 44:
                    raise
                time.sleep(2)
        if json.loads(get('/api/v1/sys_version')) != original_version:
            raise RuntimeError('Existing device owner changed during staged deployment')
        nginx.write_bytes(nginx_next)
        run(['nginx', '-t'])
        run(['nginx', '-s', 'reload'])
        capabilities = json.loads(get('/api/v1/auth/login-options'))
        if capabilities.get('data', {}).get('bindings', {}).get('enabled') is not False:
            raise RuntimeError('Disabled identity binding capability was not advertised')
        # Make only public frontend files traversable, keeping backups/configuration private.
        root.chmod(0o755)
        release.parent.chmod(0o755)
        release.chmod(0o711)
        site.chmod(0o755)
        for path in site.rglob('*'):
            path.chmod(0o755 if path.is_dir() else 0o644)
        if previous is None:
            os.rename(str(target), str(backup))
        os.symlink(str(site), str(link))
        os.replace(str(link), str(target))
        switched = True
        if (get('/SOURCE_COMMIT').decode().strip() != frontend_sha or
                get('/index.html') != (site / 'index.html').read_bytes() or
                json.loads(get('/RELEASE_PROFILE.json')) != profile):
            raise RuntimeError('Public frontend identity verification failed')
        after = sql('ThingsPanel', "SELECT json_build_object('devices',(SELECT count(*) FROM devices),'templates',(SELECT count(*) FROM device_configs),'groups',(SELECT count(*) FROM groups),'members',(SELECT count(*) FROM r_group_device))")
        if after != baseline:
            raise RuntimeError('Device owner counts changed; inspect concurrent changes')
        result = {'status': 'deployed-control-plane', 'backend_commit': backend_sha,
                  'frontend_commit': frontend_sha, 'device_frontend_source': DEVICE_SOURCE,
                  'device_owner_version': original_version, 'device_counts': after,
                  'resource_databases_unchanged': True, 'database_migrations_applied': 0,
                  'backup_sha256': backups, 'full_native_device_cutover': False}
        (release / 'deployment.json').write_text(json.dumps(result, indent=2))
        print(json.dumps(result))
    except Exception:
        nginx.write_bytes(nginx_before)
        run(['nginx', '-t'])
        run(['nginx', '-s', 'reload'])
        replace_config(original)
        run(args + ['up', '-d', '--no-deps'] + list(SERVICES))
        if switched or (previous is None and backup.exists()):
            if previous:
                os.symlink(previous, str(link))
                os.replace(str(link), str(target))
            elif backup.exists():
                if target.is_symlink():
                    target.unlink()
                os.rename(str(backup), str(target))
        (release / 'rollback.json').write_text(json.dumps({'services_restored': True, 'frontend_restored': True}))
        raise


if __name__ == '__main__':
    deploy(sys.argv[1], sys.argv[2])
