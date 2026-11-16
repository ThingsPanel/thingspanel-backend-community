"""Idempotent test-host SSO and private file storage wiring. Dry-run by default.
No Prisma/schema changes, no existing object data or keys are deleted.
"""
import argparse
import copy
import fcntl
import hashlib
import json
import os
import pathlib
import re
import secrets
import shlex
import shutil
import subprocess
import time
import urllib.request
import urllib.parse

ROOT = pathlib.Path('/opt/tp-community-main-20261028')
LOCK = pathlib.Path('/opt/tp-community-actions/deployment.lock')
STORE = ROOT / 'fileassets-storage'
IMAGE = 'dxflrs/garage@sha256:677e5b758e4425a325b811c425c026bf35a4bf42f7ddbaa773de3f4fd779a5a5'
LOCAL_IMAGE = 'tp-fileassets-garage:verified-677e5b758e4425a325b811c425c026bf35a4bf42f7ddbaa773de3f4fd779a5a5'
STORAGE_ARTIFACT_KEY = IMAGE.split('sha256:', 1)[1][:40]
PUBLIC_ENDPOINT = 'http://47.92.253.145'
PLATFORM_ENDPOINT = 'http://127.0.0.1:9999'
BUCKET = 'template-image-assets'
STORAGE_IMAGE_LIMIT = 72 * 1024 * 1024
STORAGE_HEADROOM = 384 * 1024 * 1024
NGINX = pathlib.Path('/etc/nginx/conf.d/community.conf')
MARKER = '# thingspanel-managed-private-fileassets'
SSO_ENV_PATHS = (pathlib.Path('/opt/thingsvis/server/apps/server/.env'), pathlib.Path('/opt/thingsvis/server/.env'))
LAST_COMMAND = 'preflight'


def run(args, env=None):
    global LAST_COMMAND
    LAST_COMMAND = ' '.join(args[:2])
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, timeout=120)
    if p.returncode:
        raise RuntimeError('command failed: ' + args[0])
    return p.stdout.decode()


def checked_path(path):
    if path.is_symlink() or path.resolve() != path.absolute():
        raise RuntimeError('managed path must not use symlinks')
    return path


def atomic_write(path, value):
    checked_path(path)
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temp = path.with_name(path.name + '.next')
    checked_path(temp)
    with temp.open('wb') as f:
        os.chmod(str(temp), 0o600)
        f.write(value if isinstance(value, bytes) else value.encode('utf-8'))
        f.flush()
        os.fsync(f.fileno())
    os.replace(str(temp), str(path))


def sso_env(source):
    lines = source.splitlines()
    indexes = [i for i, line in enumerate(lines) if re.match(r'^\s*(?:export\s+)?THINGSPANEL_API_URL\s*=', line)]
    if len(indexes) > 1:
        raise RuntimeError('ambiguous SSO configuration')
    endpoint = PLATFORM_ENDPOINT
    if indexes:
        raw = lines[indexes[0]].split('=', 1)[1].strip()
        try:
            parts = shlex.split(raw, comments=True)
            endpoint = (parts[0] if parts else '') or PLATFORM_ENDPOINT
            parsed = urllib.parse.urlsplit(endpoint)
            valid = len(parts) <= 1 and parsed.scheme in ('http', 'https') and parsed.hostname and not parsed.username and not parsed.password and parsed.path in ('', '/') and not parsed.query and not parsed.fragment
            parsed.port
            if not valid or any(c.isspace() for c in endpoint) or any(c in endpoint for c in '$`\\'):
                raise ValueError()
        except ValueError:
            raise RuntimeError('invalid SSO root endpoint') from None
        if raw and parts and parts[0]:
            return source, endpoint
    value = 'THINGSPANEL_API_URL=' + endpoint
    if indexes:
        lines[indexes[0]] = value
    else:
        lines.append(value)
    return '\n'.join(lines) + '\n', endpoint


def runtime_storage(config, keys):
    result = copy.deepcopy(config)
    providers = result.get('object_storage') or []
    matching = [p for p in providers if BUCKET in (p.get('buckets') or {})]
    if matching:
        # Existing operator-managed S3/GCS is authoritative; never replace keys,
        # endpoint or bucket. The helper's IP is exclusively a test-host default.
        if len(matching) != 1:
            raise RuntimeError('ambiguous image bucket configuration')
        return result, False
    providers.append({'type': 's3', 'region': 'garage', 'endpoint': PUBLIC_ENDPOINT,
                      'access_key_id': keys['access'], 'secret_access_key': keys['secret'],
                      'buckets': {BUCKET: {'name': BUCKET}}})
    result['object_storage'] = providers
    return result, True


def loaded_vhosts(dump):
    """Return only routing metadata, never headers, upstream credentials or logs."""
    result = []
    sections = re.split(r'^# configuration file ([/A-Za-z0-9_.-]+):\s*$', dump, flags=re.M)
    token_pattern = r'#[^\n]*|"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|[{};]|[^\s{};#]+'
    for index in range(1, len(sections), 2):
        filename = sections[index]
        if not filename.startswith('/etc/nginx/'):
            raise RuntimeError('nginx loaded configuration is outside reviewed root')
        tokens = [t for t in re.findall(token_pattern, sections[index+1]) if not t.startswith('#')]
        i = 0
        while i < len(tokens)-1:
            if tokens[i:i+2] != ['server', '{']:
                i += 1
                continue
            depth, i, directive = 1, i+2, []
            entry = {'file': filename, 'listen': [], 'server_name': [], 'root': []}
            while i < len(tokens) and depth:
                token = tokens[i]; i += 1
                if token == '{': depth += 1; directive = []
                elif token == '}': depth -= 1; directive = []
                elif depth == 1 and token == ';':
                    if directive and directive[0] in ('listen', 'server_name', 'root'):
                        values = [t.strip('"\'') for t in directive[1:]]
                        if any(not re.fullmatch(r'[A-Za-z0-9_./:*\[\]-]+', v) for v in values):
                            raise RuntimeError('unsupported nginx routing directive')
                        entry[directive[0]].append(' '.join(values))
                    directive = []
                elif depth == 1: directive.append(token)
            if depth: raise RuntimeError('unbalanced loaded nginx server')
            result.append(entry)
    if not result: raise RuntimeError('loaded nginx virtual hosts unavailable')
    return result


def prepare_nginx(source, hosts):
    matches = [h for h in hosts if h['file'] == NGINX.as_posix() and '/home/thingspanel/nginx/www/dist' in h['root']]
    if len(matches) != 1:
        raise RuntimeError('loaded community virtual host is ambiguous')
    selected = matches[0]
    owners = [h for h in hosts if any('47.92.253.145' in n.split() for n in h['server_name']) and
              any(re.search(r'^(?:80|0\.0\.0\.0:80|\*:80)(?: |$)', value) for value in h['listen'])]
    if any(h is not selected for h in owners):
        raise RuntimeError('IPv4 hostname already belongs to another virtual host')
    if not any(re.search(r'^(?:80|0\.0\.0\.0:80|\*:80)(?: |$)', value) for value in selected['listen']):
        raise RuntimeError('community virtual host does not listen on IPv4 port 80')
    changed = False
    if not owners:
        root = re.search(r'\broot\s+/home/thingspanel/nginx/www/dist\s*;', source)
        if root is None: raise RuntimeError('community root missing')
        start = source.rfind('server {', 0, root.start())
        if start < 0: raise RuntimeError('community server syntax unsupported')
        match = re.search(r'\bserver_name\s+([^;]+);', source[start:root.start()])
        if match is None:
            raise RuntimeError('community server_name must precede root for guarded update')
        end = start + match.end()-1
        source = source[:end] + ' 47.92.253.145' + source[end:]
        changed = True
    return nginx_storage(source), {'add_server_name': '47.92.253.145' if changed else None,
                                  'add_location': '/template-image-assets/' if MARKER not in source else None,
                                  'default_server_unchanged': True}


def existing_ip_route(hosts):
    owners = [h for h in hosts if any('47.92.253.145' in n.split() for n in h['server_name']) and
              any(re.search(r'^(?:80|0\.0\.0\.0:80|\*:80)(?: |$)', value) for value in h['listen'])]
    if len(owners) > 1:
        raise RuntimeError('IPv4 port 80 host binding is ambiguous')
    if not owners:
        return None
    path = pathlib.Path(owners[0]['file'])
    if not re.fullmatch(r'/etc/nginx/conf.d/[A-Za-z0-9_.-]+\.conf', path.as_posix()):
        raise RuntimeError('IPv4 host configuration is outside reviewed conf.d')
    return checked_path(path)


def add_bound_ip_location(source):
    if MARKER in source:
        if source.count(MARKER) != 1 or MANAGED_LOCATION.strip() not in source:
            raise RuntimeError('modified managed storage route')
        return source
    if re.search(r'location[^\n{]*template-image-assets', source):
        raise RuntimeError('unmanaged image bucket route exists')
    matches = [m for m in re.finditer(r'\bserver_name\s+([^;]+);', source) if '47.92.253.145' in m.group(1).split()]
    if len(matches) != 1:
        raise RuntimeError('IP server block is ambiguous')
    start = source.rfind('server {', 0, matches[0].start())
    if start < 0: raise RuntimeError('IP server syntax unsupported')
    opening = source.index('{', start)
    depth, end = 1, opening+1
    while end < len(source) and depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    if depth or not opening < matches[0].start() < end:
        raise RuntimeError('IP server braces ambiguous')
    return source[:end-1] + MANAGED_LOCATION + source[end-1:]


def nginx_storage(source):
    if MARKER in source:
        if source.count(MARKER) != 1 or MANAGED_LOCATION.strip() not in source:
            raise RuntimeError('ambiguous managed storage route')
        return source
    if re.search(r'location[^\n{]*template-image-assets', source):
        raise RuntimeError('unmanaged image bucket route exists')
    # Select the existing community server by its established document root.
    root = re.search(r'\broot\s+/home/thingspanel/nginx/www/dist\s*;', source)
    if root is None:
        raise RuntimeError('community document root not found')
    before = source[:root.start()]
    start = before.rfind('server {')
    if start < 0:
        raise RuntimeError('community server block not found')
    opening = source.index('{', start)
    depth = 1
    end = opening + 1
    while end < len(source) and depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    if depth or not opening < root.start() < end:
        raise RuntimeError('community server block is ambiguous')
    block = source[opening:end]
    # Require this same server to serve the tested IPv4 host, not a new vhost.
    if not (re.search(r'listen\s+[^;]*default_server', block) or
            re.search(r'server_name\s+[^;]*\b47\.92\.253\.145\b', block)):
        raise RuntimeError('community server must explicitly own the IPv4/default host')
    return source[:end-1] + MANAGED_LOCATION + source[end-1:]


MANAGED_LOCATION = '''
    # thingspanel-managed-private-fileassets
    location ^~ /template-image-assets/ {
        proxy_pass http://127.0.0.1:3900;
        proxy_set_header Host $http_host;
        proxy_http_version 1.1;
        client_max_body_size 11m;
        proxy_cache off;
        access_log off;
        add_header Cache-Control "private, no-store" always;
    }
'''


def restore_configurations(originals):
    # Attempt every restoration even when one reload fails. Never touch object
    # directories, bucket identity, or newly created durable storage on rollback.
    errors = []
    for index, (path, content) in enumerate(originals.items()):
        try:
            atomic_write(path, content)
        except Exception:
            errors.append('configuration_' + str(index))
    return errors


def restore_services(old_api):
    rollback_env = os.environ.copy()
    rollback_env['THINGSPANEL_API_URL'] = old_api
    errors = []
    actions = [('nginx', lambda: (run(['nginx', '-t']), run(['nginx', '-s', 'reload']))),
               ('encore', lambda: run(['docker','compose','-f',str(ROOT/'compose.encore.private.json'),'up','-d','--no-deps','--force-recreate','--pull','never','encore-api'])),
               ('thingsvis', lambda: run(['pm2','restart','thingsvis-server','--update-env'], env=rollback_env)),
               ('pm2_save', lambda: run(['pm2','save']))]
    for name, action in actions:
        try:
            action()
        except Exception:
            errors.append(name)
    return errors


def garage_config(keys):
    return '''metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"
replication_factor = 1
metadata_fsync = true
data_fsync = true
block_ram_buffer_max = "32MiB"
rpc_bind_addr = "127.0.0.1:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "''' + keys['rpc'] + '''"
[s3_api]
s3_region = "garage"
api_bind_addr = "0.0.0.0:3900"
'''


def storage_compose():
    return {'name': 'tp-fileassets', 'services': {'storage': {
        'image': LOCAL_IMAGE, 'pull_policy': 'never', 'restart': 'unless-stopped', 'command': ['/garage', 'server', '--single-node', '--default-bucket'],
        'ports': ['127.0.0.1:3900:3900'], 'env_file': [str(STORE / 'garage.private.env')],
        'volumes': [str(STORE / 'garage.private.toml') + ':/etc/garage.toml:ro',
                    str(STORE / 'data') + ':/var/lib/garage/data', str(STORE / 'meta') + ':/var/lib/garage/meta'],
        'logging': {'driver': 'json-file', 'options': {'max-size': '5m', 'max-file': '2'}}}}}


def validate_storage_bundle(package, manifest):
    if (manifest.get('image_ref') != IMAGE or manifest.get('local_image') != LOCAL_IMAGE
            or not re.fullmatch(r'sha256:[0-9a-f]{64}', manifest.get('image_id', ''))
            or manifest.get('architecture') != 'amd64'
            or not 0 < manifest.get('image_bytes', 0) <= STORAGE_IMAGE_LIMIT
            or package.is_symlink() or package.resolve() != package.absolute()
            or not package.is_file() or package.stat().st_size != manifest.get('bytes')
            or not 0 < package.stat().st_size <= STORAGE_IMAGE_LIMIT * 2):
        raise RuntimeError('invalid pinned storage image artifact')
    digest = hashlib.sha256()
    with package.open('rb') as stream:
        for block in iter(lambda: stream.read(1024*1024), b''):
            digest.update(block)
    if digest.hexdigest() != manifest.get('sha256'):
        raise RuntimeError('storage image artifact checksum mismatch')


def import_storage_image(commit):
    if commit != STORAGE_ARTIFACT_KEY:
        raise RuntimeError('verified storage artifact revision required')
    incoming = pathlib.Path('/opt/tp-community-actions/incoming')
    package = checked_path(incoming / ('storage-'+commit+'.tar.gz'))
    manifest = json.loads(checked_path(incoming / ('storage-'+commit+'.json')).read_text())
    validate_storage_bundle(package, manifest)
    run(['docker', 'load', '-i', str(package)])
    actual = json.loads(run(['docker','image','inspect',LOCAL_IMAGE,'--format','{{json .}}']))
    validate_loaded_storage_image(actual, manifest)


def validate_loaded_storage_image(actual, manifest):
    # Docker classic reports uncompressed filesystem Size; containerd-backed
    # stores may report compressed bytes. Content identity, not size equality,
    # binds the loaded image to the CI-verified digest.
    if (actual.get('Id') != manifest['image_id'] or actual.get('Architecture') != 'amd64'
            or not 0 < actual.get('Size', 0) <= STORAGE_IMAGE_LIMIT):
        raise RuntimeError('loaded storage image identity mismatch')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--storage-commit')
    args = parser.parse_args()
    os.umask(0o077)
    with LOCK.open('r') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        runtime_path = checked_path(ROOT / 'runtime.encore.private.json')
        env_path = next((p for p in SSO_ENV_PATHS if p.is_file()), None)
        if env_path is None:
            raise RuntimeError('existing ThingsVis private env file not found')
        checked_path(env_path)
        config = json.loads(runtime_path.read_text())
        existing = any(BUCKET in (p.get('buckets') or {}) for p in (config.get('object_storage') or []))
        hosts = loaded_vhosts(run(['nginx', '-T']))
        print(json.dumps({'read_only': True, 'loaded_vhosts': hosts}), flush=True)
        bound_path = existing_ip_route(hosts)
        nginx_path = bound_path or NGINX
        nginx_before = checked_path(nginx_path).read_text()
        if existing:
            nginx_after, route_plan = nginx_before, {}
        elif bound_path:
            nginx_after = add_bound_ip_location(nginx_before)
            route_plan = {'file': str(nginx_path), 'add_location': '/template-image-assets/' if MARKER not in nginx_before else None,
                          'server_name_unchanged': True, 'default_server_unchanged': True}
        else:
            nginx_after, route_plan = prepare_nginx(nginx_before, hosts)
        pm2 = json.loads(run(['pm2', 'jlist']))
        process = [p for p in pm2 if p.get('name') == 'thingsvis-server']
        if len(process) != 1:
            raise RuntimeError('ThingsVis process identity is ambiguous')
        env_before = env_path.read_text()
        env_after, platform_endpoint = sso_env(env_before)
        print(json.dumps({'dry_run': not args.apply, 'existing_object_provider_preserved': existing,
                          'sso_env_update': env_before != env_after, 'storage_install': not existing,
                          'loaded_vhosts': hosts, 'nginx_change_plan': route_plan,
                          'disk_free_bytes': shutil.disk_usage(str(ROOT)).free}))
        if not args.apply:
            return
        # Pinned image has 69,426,824 uncompressed filesystem bytes (Docker's
        # containerd store reports compressed Size instead). Budget three full
        # 72 MiB copies plus durable-service headroom, never compressed Size alone.
        if not existing and shutil.disk_usage(str(ROOT)).free < STORAGE_HEADROOM + 3 * STORAGE_IMAGE_LIMIT:
            raise RuntimeError('insufficient storage bootstrap reserve')
        backup = ROOT / 'integration-wiring-backups' / str(time.time_ns() if hasattr(time, 'time_ns') else int(time.time()*1000000))
        backup.mkdir(parents=True, mode=0o700)
        originals = {runtime_path: runtime_path.read_bytes(), env_path: env_path.read_bytes(), nginx_path: nginx_path.read_bytes()}
        for index, (path, content) in enumerate(originals.items()):
            (backup / str(index)).write_bytes(content)
        old_api = process[0].get('pm2_env', {}).get('THINGSPANEL_API_URL', '')
        try:
            if not existing:
                import_storage_image(args.storage_commit)
                if shutil.disk_usage(str(ROOT)).free < STORAGE_HEADROOM + STORAGE_IMAGE_LIMIT:
                    raise RuntimeError('insufficient post-pull storage growth reserve')
                checked_path(STORE).mkdir(parents=True, mode=0o700, exist_ok=True)
                key_path = checked_path(STORE / 'keys.private.json')
                if not key_path.exists():
                    atomic_write(key_path, json.dumps({'access': 'GK'+secrets.token_hex(16), 'secret': secrets.token_hex(32), 'rpc': secrets.token_hex(32)}))
                keys = json.loads(key_path.read_text())
                if not re.fullmatch(r'GK[0-9a-f]{32}', keys['access']) or any(not re.fullmatch(r'[0-9a-f]{64}',keys[k]) for k in ('secret','rpc')):
                    raise RuntimeError('invalid persisted storage identity')
                for directory in ('data','meta'):
                    checked_path(STORE / directory).mkdir(mode=0o700, exist_ok=True)
                for path, value in ((STORE/'garage.private.toml', garage_config(keys)),
                                    (STORE/'garage.private.env', 'GARAGE_DEFAULT_ACCESS_KEY='+keys['access']+'\nGARAGE_DEFAULT_SECRET_KEY='+keys['secret']+'\nGARAGE_DEFAULT_BUCKET='+BUCKET+'\n')):
                    if not path.exists(): atomic_write(path,value)
                    elif path.read_text()!=value: raise RuntimeError('existing storage configuration differs; preserved')
                compose = STORE/'compose.private.json'
                if not compose.exists(): atomic_write(compose,json.dumps(storage_compose(),indent=2))
                elif json.loads(compose.read_text())!=storage_compose(): raise RuntimeError('existing storage service differs; preserved')
                run(['docker','compose','-f',str(compose),'up','-d','--pull','never'])
                for attempt in range(20):
                    try:
                        run(['docker','compose','-f',str(compose),'exec','-T','storage','/garage','health'])
                        break
                    except RuntimeError:
                        if attempt==19: raise
                        time.sleep(1)
                atomic_write(nginx_path,nginx_after)
                run(['nginx','-t']);run(['nginx','-s','reload'])
                updated,_ = runtime_storage(config,keys)
                atomic_write(runtime_path,json.dumps(updated,indent=2))
                run(['docker','compose','-f',str(ROOT/'compose.encore.private.json'),'up','-d','--no-deps','--force-recreate','--pull','never','encore-api'])
            atomic_write(env_path,env_after)
            process_env = os.environ.copy();process_env['THINGSPANEL_API_URL']=platform_endpoint
            run(['pm2','restart','thingsvis-server','--update-env'],env=process_env);run(['pm2','save'])
            for target in ('http://127.0.0.1:14000/v1/health','http://127.0.0.1:8000/api/v1/health'):
                for attempt in range(30):
                    try:
                        with urllib.request.urlopen(target,timeout=3) as response:
                            if response.status!=200: raise RuntimeError('service not healthy')
                        break
                    except Exception:
                        if attempt==29: raise RuntimeError('integration health failed')
                        time.sleep(1)
            memory = next(int(line.split()[1])*1024 for line in pathlib.Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemAvailable:'))
            print(json.dumps({'applied':True,'secrets_printed':False,'existing_data_preserved':True,
                              'disk_free_bytes':shutil.disk_usage(str(ROOT)).free, 'memory_available_bytes':memory}))
        except Exception as failure:
            print(json.dumps({'integration_failure': {'command': LAST_COMMAND,
                              'type': type(failure).__name__,
                              'timeout': isinstance(failure, subprocess.TimeoutExpired)}}), flush=True)
            errors = restore_configurations(originals) + restore_services(old_api)
            # Never remove persistent storage, keys or objects on rollback.
            if errors:
                raise RuntimeError('integration wiring failed; recovery incomplete: ' + ','.join(errors)) from None
            raise RuntimeError('integration wiring failed; previous configurations restored') from None

if __name__ == '__main__':
    main()
