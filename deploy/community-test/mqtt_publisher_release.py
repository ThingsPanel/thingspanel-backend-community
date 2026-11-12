"""Build and deploy the MQTT-only candidate without changing database owners.

The candidate retains the deployed Gin security profile and HA broker baseline.
Stage produces private snapshots; apply is separate, locked and rollback-capable.
"""
import argparse
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import time
import urllib.request

ANCHOR = '8e1c44985669d3f7fedee00888eb91607821a442'
BROKER_BASE = '87ad45b09cbed15594277aaafb85a4258246808e'
DRIVER_BASE = 'd1d01969792fa543b15e8721ee325a2dd603e849'
HTTP_PATCH = '107e064ff191b909558746b661215469a30f4577'
HTTP_FIX = '2a780fe468645cb500a8249670ac61da56aeda9f'
DEPLOYED_BACKEND = 'mqtt-publisher-5fb4c68e11b4ce3632abad32197a345e3d0f0f89'
# Reviewed read-only snapshot, 2026-11-13. A later deployment must be reviewed
# again instead of accepting arbitrary tags or silently relaxing these guards.
DEPLOYED_BROKER_IMAGE = 'sha256:fd7091f1868a6642742315be959bb52db144d1416bf9fcf1a3455d068e24ec50'
SCOPE_CONTROLLER_FILES = {
    '/opt/tp-community-main-20261028/http-runtime/scope.py': '4942a20e28557ff9275580018cfc58fc5eb54a522e7740e9f9d4d8f5607edcb2',
    '/opt/tp-community-main-20261028/ha-runtime/scope.py': '6331a89ee2d148eb58f101615f277059a82c74b2bcd94241b47fab5e0fd80997',
    '/opt/tp-community-main-20261028/http-runtime/broker_scope_roles.py': None,
    '/opt/tp-community-main-20261028/ha-runtime/broker_scope_roles.py': None,
}
PRODUCT_FILES = [
    'internal/brokercontrol/owner.go', 'internal/brokercontrol/README.md',
    'internal/brokercontrol/plugin_owner.go', 'internal/brokercontrol/gorm_repository.go',
    'internal/brokercontrol/owner_test.go', 'internal/brokercontrol/target_plugin_publisher_test.go',
    'internal/brokercontrol/target_plugin_publisher_postgres_test.go',
    'internal/adapter/mqttadapter/adapter.go', 'internal/adapter/mqttadapter/command.go',
    'internal/adapter/mqttadapter/ingress.go', 'internal/adapter/mqttadapter/publisher.go',
    'internal/app/mqtt_service.go', 'internal/brokercontract/target_client.go',
    'internal/brokercontract/target_client_test.go', 'internal/brokercontrol/target_publisher_test.go',
    'internal/brokercontrol/gateway_target_transport_test.go',
    'internal/adapter/mqttadapter/target_publisher.go',
    'internal/adapter/mqttadapter/target_publisher_test.go',
    'internal/adapter/mqttadapter/target_publisher_transport_test.go',
    'internal/service/target_publisher_application_scope_test.go',
]
BROKER_FILES = [p for p in PRODUCT_FILES if p.startswith(('internal/brokercontrol/', 'internal/brokercontract/'))]
PUBLISH_FAMILIES = [prefix + family for prefix in ('devices/', 'gateway/') for family in (
    'command/+/+', 'telemetry/control/+', 'attributes/get/+', 'attributes/set/+/+',
    'attributes/response/+/+', 'event/response/+/+')]


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def value_digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def run(args, cwd=None, data=None):
    p = subprocess.run([str(v) for v in args], cwd=cwd, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if p.returncode:
        # subprocess output may contain environment secrets; never print it.
        raise RuntimeError('Release command failed: ' + str(args[0]))
    return p.stdout


def private(path, value):
    path = Path(path)
    if path.exists() or path.is_symlink():
        raise ValueError('Refusing to overwrite staged evidence')
    with path.open('xb') as stream:
        stream.write(value if isinstance(value, bytes) else json.dumps(value, indent=2).encode())
    path.chmod(0o600)


def compose_sources(repo, target):
    from build_device_safety_profile import build as build_safety
    repo, target = Path(repo).resolve(), Path(target).resolve()
    if target.exists():
        raise ValueError('Source output must be new')
    head = run(['git', 'rev-parse', 'HEAD'], repo).decode().strip()
    for sha in (ANCHOR, BROKER_BASE):
        run(['git', 'merge-base', '--is-ancestor', sha, head], repo)
    if run(['git', 'status', '--porcelain', '--', *PRODUCT_FILES], repo).strip():
        raise ValueError('Candidate source must be committed')
    target.mkdir(parents=True)
    backend = target / 'backend'
    safety = build_safety(repo, backend)
    # Preserve the independently deployed managed HTTP installation boundary.
    http_paths, retained = set(), list(safety['patches'])
    for sha in (HTTP_PATCH, HTTP_FIX):
        paths = run(['git', 'diff', '--name-only', sha + '^', sha, '--', 'internal'], repo).decode().splitlines()
        if not paths or any(not p.startswith('internal/api/') for p in paths):
            raise ValueError('Managed HTTP preservation patch exceeds reviewed scope')
        http_paths.update(paths)
        if sha not in retained:
            http_delta = run(['git', 'diff', sha + '^', sha, '--', 'internal'], repo)
            run(['git', 'apply', '--check'], backend, http_delta)
            run(['git', 'apply'], backend, http_delta)
            retained.append(sha)
    driver_paths = ['internal/mosquittodriver', 'cmd/tp-mosquitto-plugin', 'internal/brokercontract', 'deploy/mosquitto-community']
    if run(['git', 'diff', DRIVER_BASE, BROKER_BASE, '--', *driver_paths], repo):
        raise ValueError('Current driver differs from the reviewed HA source')
    broker = target / 'broker'
    broker.mkdir()
    with tarfile.open(fileobj=io.BytesIO(run(['git', 'archive', BROKER_BASE], repo))) as archive:
        archive.extractall(broker, filter='data')
    profiles = {}
    for name, root, files in [('backend', backend, PRODUCT_FILES), ('broker', broker, BROKER_FILES)]:
        patch = run(['git', 'diff', '--binary', ANCHOR, head, '--', *files], repo)
        if not patch.strip():
            raise ValueError('Candidate contains no MQTT changes')
        run(['git', 'apply', '--check'], root, patch)
        run(['git', 'apply'], root, patch)
        for required in ['target_scoped_publisher', 'target_tenant_scope']:
            if required not in (root / 'internal/brokercontrol/owner.go').read_text():
                raise ValueError('Target publisher contract absent')
        profiles[name] = {'baseline': safety['baseline'] if name == 'backend' else BROKER_BASE,
                          'retained_patches': retained if name == 'backend' else [],
                          'patch_sha256': hashlib.sha256(patch).hexdigest(),
                          'files': {f: digest(root / f) for f in files}}
    report = {'profile': 'mqtt-target-publisher', 'source_commit': head,
              'delta_anchor': ANCHOR, 'sources': profiles,
              'database_owner_changed': False, 'encore_changed': False,
              'expected_backend_version': DEPLOYED_BACKEND, 'existing_driver_source': DRIVER_BASE,
              'retained_http_files': {p: digest(backend / p) for p in http_paths}}
    (target / 'RELEASE_PROFILE.json').write_text(json.dumps(report, indent=2))
    return report


def publisher_settings(services, mqtt, environment):
    """Transform only the existing first-party application identity, fail closed."""
    env = copy.deepcopy(environment)
    effective = {key: env.get('GOTP_MQTT_' + key.upper(), mqtt.get(key)) for key in ('user', 'pass', 'client_id')}
    if not all(isinstance(v, str) and v for v in effective.values()):
        raise ValueError('Complete current application MQTT identity required')
    selected = [i for i, s in enumerate(services) if s.get('username') == effective['user']]
    if len(selected) != 1:
        raise ValueError('Application MQTT identity must match exactly one owner service')
    result = copy.deepcopy(services)
    s = result[selected[0]]
    if (s.get('password') != effective['pass'] or effective['client_id'] not in s.get('allowed_client_ids', [])
            or s.get('plugin_socket') or s.get('allow_any_client_id')
            or s.get('principal', {}).get('principal_kind') != 'service'):
        raise ValueError('Current identity is not the verified first-party publisher')
    if not s.get('broker_cell') or not s.get('broker_instance'):
        raise ValueError('Publisher broker binding is incomplete')
    if any(f not in PUBLISH_FAMILIES for f in s.get('policy', {}).get('publish', [])):
        raise ValueError('Unexpected publisher policy requires separate review')
    s['target_scoped_publisher'] = True
    s['target_tenant_scope'] = 'platform'
    s['allowed_device_ids'], s['allowed_tenant_ids'] = [], []
    s['policy']['publish'] = list(PUBLISH_FAMILIES)
    env['GOTP_MQTT_TARGET_SCOPED_PUBLISHER'] = 'true'
    return result, env


def verify_bundle(root):
    root = Path(root)
    manifest = json.loads((root / 'manifest.json').read_text())
    if manifest.get('profile') != 'mqtt-target-publisher' or set(manifest.get('sha256', {})) != {'ThingsPanel-Go', 'broker-image.tar.gz', 'RELEASE_PROFILE.json'}:
        raise ValueError('Unexpected release bundle')
    if manifest.get('tool_sha256') != digest(__file__):
        raise ValueError('Release tool checksum mismatch')
    if not isinstance(manifest.get('image_size_bytes'), int) or manifest['image_size_bytes'] <= 0:
        raise ValueError('Broker image size is required for capacity preflight')
    for name, sha in manifest['sha256'].items():
        if digest(root / name) != sha:
            raise ValueError('Release checksum mismatch')
    profile = json.loads((root / 'RELEASE_PROFILE.json').read_text())
    if (profile.get('source_commit') != manifest['commit'] or profile.get('encore_changed') is not False
            or profile.get('database_owner_changed') is not False
            or profile['sources']['broker']['baseline'] != BROKER_BASE):
        raise ValueError('Release provenance mismatch')
    if profile.get('expected_backend_version') != DEPLOYED_BACKEND or any(sha not in profile['sources']['backend'].get('retained_patches', []) for sha in (HTTP_PATCH, HTTP_FIX)):
        raise ValueError('Current HTTP release was not preserved')
    return manifest


def resource_preflight(root, manifest):
    root = Path(root)
    info = {}
    for line in Path('/proc/meminfo').read_text().splitlines():
        key, value = line.split(':', 1)
        info[key] = int(value.strip().split()[0]) * 1024
    # The host builds nothing. This is a minimum free runtime cushion, not an
    # estimate that counts disk-backed swap as usable physical memory.
    if info.get('MemAvailable', 0) < 256 * 1024**2:
        raise ValueError('Insufficient available memory for MQTT-only rollout')
    docker_root = run(['docker', 'info', '--format', '{{.DockerRootDir}}']).decode().strip()
    if not docker_root or not Path(docker_root).is_absolute():
        raise ValueError('Docker storage directory is unavailable')
    binary_space = (root / 'ThingsPanel-Go').stat().st_size * 2 + 64 * 1024**2
    image_space = manifest['image_size_bytes'] + 256 * 1024**2
    same_disk = root.stat().st_dev == Path(docker_root).stat().st_dev
    if shutil.disk_usage(root).free < binary_space + (image_space if same_disk else 0):
        raise ValueError('Insufficient release/rollback disk space')
    if not same_disk and shutil.disk_usage(docker_root).free < image_space:
        raise ValueError('Insufficient Docker image disk space')
    return {'memory_available_bytes': info['MemAvailable'], 'additional_disk_required_bytes': binary_space + image_space}


def process_spec(process):
    old = process['pm2_env']
    env = dict(old.get('env', {}))
    env.update({k: v for k, v in old.items() if k.upper() == k and isinstance(v, (str, int, float))})
    spec = {'name': process['name'], 'script': old['pm_exec_path'], 'cwd': old['pm_cwd'],
            'interpreter': old.get('exec_interpreter', 'none'), 'env': env}
    for key in ('args', 'node_args', 'autorestart', 'restart_delay', 'max_memory_restart', 'min_uptime',
                'max_restarts', 'kill_timeout', 'wait_ready', 'listen_timeout', 'merge_logs', 'log_date_format'):
        if key in old:
            spec[key] = old[key]
    return spec


def inspect(container):
    return json.loads(run(['docker', 'inspect', container]))[0]


def file_matches_snapshot(name, expected):
    path = Path(name)
    if expected is None:
        return not path.exists() and not path.is_symlink()
    return not path.is_symlink() and path.is_file() and digest(path) == expected


def verify_runtime_baseline(version, owner, broker):
    if version != DEPLOYED_BACKEND:
        raise ValueError('Backend baseline changed; review release profile before deployment')
    if any(item.get('Image') != DEPLOYED_BROKER_IMAGE for item in (owner, broker)):
        raise ValueError('Broker image baseline changed; review before deployment')


def scope_controller_preflight():
    for name, expected in SCOPE_CONTROLLER_FILES.items():
        if not file_matches_snapshot(name, expected):
            raise ValueError('Managed scope controller compatibility requires review')
    return dict(SCOPE_CONTROLLER_FILES)


def stage(root, owner, broker, pm2):
    import yaml
    root = Path(root).resolve()
    manifest = verify_bundle(root)
    capacity = resource_preflight(root, manifest)
    controller_files = scope_controller_preflight()
    if (root / 'stage.private.json').exists():
        raise ValueError('Release already staged; inspect or use a new directory')
    oi, bi = inspect(owner), inspect(broker)
    labels = oi['Config']['Labels']
    project = labels['com.docker.compose.project']
    files = labels['com.docker.compose.project.config_files'].split(',')
    wd = labels['com.docker.compose.project.working_dir']
    if bi['Config']['Labels'].get('com.docker.compose.project') != project:
        raise ValueError('Broker and owner are not one compose deployment')
    service_names = [item['Config']['Labels']['com.docker.compose.service'] for item in (oi, bi)]
    if len(set(service_names)) != 2:
        raise ValueError('Owner and broker must be distinct services')
    base = ['docker', 'compose', '-p', project]
    for path in files:
        base += ['-f', path]
    rendered = yaml.safe_load(run(base + ['config'], wd))
    next_config = copy.deepcopy(rendered)
    for name in service_names:
        next_config['services'][name]['image'] = manifest['image_id']
        next_config['services'][name]['pull_policy'] = 'never'
    env = dict(v.split('=', 1) for v in oi['Config']['Env'] if '=' in v)
    destination = env['TP_BROKER_OWNER_SERVICES_FILE']
    mounts = [m for m in oi['Mounts'] if m['Destination'] == destination and m['Type'] == 'bind']
    if len(mounts) != 1 or not Path(mounts[0]['Source']).is_file():
        raise ValueError('Owner policy must be a known bind-mounted regular file')
    policy_path = Path(mounts[0]['Source'])
    policy = policy_path.read_bytes()
    processes = json.loads(run([pm2, 'jlist']))
    matches = [p for p in processes if p.get('name') == 'thingspanel-backend-community']
    if len(matches) != 1:
        raise ValueError('Exactly one community backend process required')
    spec = process_spec(matches[0])
    binary = Path(spec['script'])
    current_version = run([binary, '--version']).decode().strip()
    verify_runtime_baseline(current_version, oi, bi)
    conf = Path(spec['cwd']) / 'configs/conf.yml'
    settings = yaml.safe_load(conf.read_bytes())
    next_policy, next_env = publisher_settings(json.loads(policy), settings['mqtt'], spec['env'])
    next_spec = copy.deepcopy(spec)
    next_spec['env'] = next_env
    private(root / 'services.before.private.json', policy)
    private(root / 'services.next.private.json', next_policy)
    private(root / 'compose.before.private.json', rendered)
    private(root / 'compose.next.private.json', next_config)
    private(root / 'backend.before.private.json', {'apps': [spec]})
    private(root / 'backend.next.private.json', {'apps': [next_spec]})
    shutil.copy2(binary, root / 'backend.before')
    state = {'controller_files': controller_files, 'project': project, 'working_dir': wd, 'services': service_names, 'containers': [owner, broker],
             'images_before': [oi['Image'], bi['Image']], 'container_ids': [oi['Id'], bi['Id']],
             'compose_files': {path: digest(path) for path in files}, 'process_spec_sha256': value_digest(spec),
             'pm2': pm2, 'binary': str(binary),
             'binary_before_sha256': digest(binary), 'config_path': str(conf), 'config_sha256': digest(conf),
             'policy_path': str(policy_path), 'policy_sha256': digest(policy_path),
             'old_version': current_version, 'next_version': 'mqtt-publisher-' + manifest['commit'],
             'staged_files': {name: digest(root / name) for name in (
                 'services.before.private.json', 'services.next.private.json', 'compose.before.private.json',
                 'compose.next.private.json', 'backend.before.private.json', 'backend.next.private.json', 'backend.before')}}
    private(root / 'stage.private.json', state)
    return {'status': 'staged', 'commit': manifest['commit'], 'live_configuration_changed': False, 'capacity': capacity}


def replace_file(source, destination):
    destination = Path(destination)
    if destination.is_symlink():
        raise ValueError('Refusing symlink replacement')
    stat = destination.stat()
    temp = destination.with_name(destination.name + '.mqtt-next')
    shutil.copyfile(source, temp)
    os.chmod(temp, stat.st_mode & 0o777)
    os.chown(temp, stat.st_uid, stat.st_gid)
    os.replace(temp, destination)


def start_backend(root, state, suffix):
    pm2 = state['pm2']
    if any(p.get('name') == 'thingspanel-backend-community' for p in json.loads(run([pm2, 'jlist']))):
        run([pm2, 'delete', 'thingspanel-backend-community'])
    run([pm2, 'start', root / ('backend.' + suffix + '.private.json'), '--only', 'thingspanel-backend-community'])


def stop_backend(state):
    if any(p.get('name') == 'thingspanel-backend-community' for p in json.loads(run([state['pm2'], 'jlist']))):
        run([state['pm2'], 'stop', 'thingspanel-backend-community'])


def compose_up(root, state, suffix):
    run(['docker', 'compose', '-p', state['project'], '-f', root / ('compose.' + suffix + '.private.json'),
         'up', '-d', '--no-deps', '--force-recreate', *state['services']], state['working_dir'])


def wait_ready(state, expected):
    deadline = time.monotonic() + 150
    stable_since, stable_identity = None, None
    while time.monotonic() < deadline:
        try:
            identity = []
            for container, mode in zip(state['containers'], ('owner-liveness', 'broker')):
                current = inspect(container)
                if (not current['State']['Running']
                        or current['State'].get('Health', {}).get('Status') != 'healthy'):
                    raise RuntimeError('Broker runtime is not healthy')
                identity.append((current['Id'], current['RestartCount'], current['State']['StartedAt']))
                run(['docker', 'exec', container, '/usr/local/bin/tp-broker-readiness', mode])
            with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version', timeout=3) as response:
                if json.load(response).get('data', {}).get('version') != expected:
                    raise RuntimeError('Backend version did not converge')
            now = time.monotonic()
            if identity != stable_identity:
                stable_since, stable_identity = now, identity
            # Observe at least two complete managed-scope timer periods. A
            # one-shot readiness success hid a subsequent controller restart.
            if stable_since is not None and now - stable_since >= 60:
                return
        except Exception:
            stable_since, stable_identity = None, None
        time.sleep(2)
    raise RuntimeError('MQTT-only release sustained readiness failed')


def apply_release(root, rollback_only=False):
    root = Path(root).resolve()
    manifest = verify_bundle(root)
    state = json.loads((root / 'stage.private.json').read_text())
    for name, sha in state['staged_files'].items():
        if digest(root / name) != sha:
            raise ValueError('Staged rollback/configuration snapshot changed')
    def rollback():
        replace_file(root / 'services.before.private.json', state['policy_path'])
        compose_up(root, state, 'before')
        stop_backend(state)
        replace_file(root / 'backend.before', state['binary'])
        start_backend(root, state, 'before')
        wait_ready(state, state['old_version'])
        run([state['pm2'], 'save'])
    if rollback_only:
        if (digest(state['binary']) != manifest['sha256']['ThingsPanel-Go']
                or digest(state['config_path']) != state['config_sha256']
                or digest(state['policy_path']) != state['staged_files']['services.next.private.json']
                or any(inspect(c)['Image'] != manifest['image_id'] for c in state['containers'])):
            raise ValueError('Live deployment no longer matches this candidate; refusing stale rollback')
        rollback()
        return {'status': 'rolled-back', 'version': state['old_version']}
    resource_preflight(root, manifest)
    current_processes = [p for p in json.loads(run([state['pm2'], 'jlist'])) if p.get('name') == 'thingspanel-backend-community']
    current_containers = [inspect(c) for c in state['containers']]
    if (digest(state['binary']) != state['binary_before_sha256'] or digest(state['config_path']) != state['config_sha256']
            or digest(state['policy_path']) != state['policy_sha256']
            or [c['Image'] for c in current_containers] != state['images_before']
            or [c['Id'] for c in current_containers] != state['container_ids']
            or any(digest(path) != sha for path, sha in state['compose_files'].items())
            or any(not file_matches_snapshot(path, sha) for path, sha in state.get('controller_files', {}).items())
            or len(current_processes) != 1 or value_digest(process_spec(current_processes[0])) != state['process_spec_sha256']):
        raise ValueError('Live deployment changed since staging')
    run(['docker', 'load', '-i', root / 'broker-image.tar.gz'])
    if json.loads(run(['docker', 'image', 'inspect', manifest['image_id']]))[0]['Id'] != manifest['image_id']:
        raise ValueError('Loaded broker image identity mismatch')
    candidate = root / 'ThingsPanel-Go'
    candidate.chmod(0o755)
    if run([candidate, '--version']).decode().strip() != state['next_version']:
        raise ValueError('Candidate binary version mismatch')
    try:
        stop_backend(state)
        replace_file(root / 'services.next.private.json', state['policy_path'])
        compose_up(root, state, 'next')
        replace_file(candidate, state['binary'])
        start_backend(root, state, 'next')
        wait_ready(state, state['next_version'])
        if digest(state['config_path']) != state['config_sha256']:
            raise RuntimeError('Unrelated backend configuration changed')
        run([state['pm2'], 'save'])
    except Exception:
        rollback()
        raise RuntimeError('MQTT-only candidate failed; previous runtime restored')
    return {'status': 'deployed', 'commit': manifest['commit'], 'version': state['next_version'],
            'database_owner_changed': False, 'encore_changed': False}


if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('mode', choices=['sources', 'stage', 'apply', 'rollback'])
    p.add_argument('directory')
    p.add_argument('--owner', default='tp-community-main-47-broker-owner-1')
    p.add_argument('--broker', default='tp-community-main-47-broker-mosquitto-1')
    p.add_argument('--pm2', default='/root/.local/share/pnpm/pm2')
    args = p.parse_args()
    os.umask(0o077)
    if args.mode == 'sources':
        result = compose_sources(Path(__file__).resolve().parents[2], args.directory)
    else:
        import fcntl
        with Path('/opt/tp-community-actions/deployment.lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = stage(args.directory, args.owner, args.broker, args.pm2) if args.mode == 'stage' else apply_release(args.directory, args.mode == 'rollback')
    print(json.dumps(result))
