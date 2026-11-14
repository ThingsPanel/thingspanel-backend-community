"""Upgrade the installed broker pair under the caller's deployment lock."""
import hashlib
import json
import pathlib
import shutil
import tarfile
import time
import urllib.request


def required_import_bytes(image):
    # Docker may retain both imported blobs and unpacked layers. Stream headers
    # rather than reading the compressed archive or layers into memory.
    with tarfile.open(str(image), mode='r|gz') as archive:
        expanded = sum(member.size for member in archive if member.isfile())
    return expanded * 2 + 256 * 1024 * 1024


def check_import_capacity(image, run):
    docker_root = pathlib.Path(run(['docker', 'info', '--format', '{{.DockerRootDir}}']))
    needed = required_import_bytes(image)
    if shutil.disk_usage(str(docker_root)).free < needed:
        raise RuntimeError('Insufficient Docker disk space for broker import; need %d bytes' % needed)
    return needed


def credential_environment(broker, owner):
    env = dict(item.split('=', 1) for item in broker['Config']['Env'])
    token_path = env['TP_BROKER_TOKEN_FILE']
    mounts = [m for m in broker['Mounts'] if token_path == m['Destination'] or token_path.startswith(m['Destination'].rstrip('/') + '/')]
    if not mounts:
        raise RuntimeError('Installed broker token has no host mount')
    mount = max(mounts, key=lambda m: len(m['Destination']))
    host_token = pathlib.Path(mount['Source']) / token_path[len(mount['Destination']):].lstrip('/')
    ports = owner['NetworkSettings']['Ports']
    bindings = [p for values in ports.values() for p in (values or []) if p['HostIp'] in ('127.0.0.1', '0.0.0.0')]
    if len(bindings) != 1:
        raise RuntimeError('Owner must expose one unambiguous host port')
    return {'TP_BROKER_OWNER_URL': 'http://127.0.0.1:' + bindings[0]['HostPort'],
            'TP_BROKER_TOKEN_FILE': str(host_token),
            'TP_BROKER_CELL': env['TP_BROKER_CELL'], 'TP_BROKER_INSTANCE': env['TP_BROKER_INSTANCE']}


def upgrade_broker(root, release, sha, run):
    incoming = root / 'incoming'
    manifest = json.loads((incoming / ('broker-' + sha + '.json')).read_text())
    image = incoming / ('broker-' + sha + '.tar.gz')
    digest = hashlib.sha256()
    with image.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    if manifest['commit'] != sha or digest.hexdigest() != manifest['sha256']:
        raise RuntimeError('Broker artifact verification failed')
    check_import_capacity(image, run)
    containers = json.loads(run(['docker', 'inspect', 'tp-community-main-47-broker-owner-1', 'tp-community-main-47-broker-mosquitto-1']))
    owner, broker = containers
    environment = credential_environment(broker, owner)
    labels = owner['Config']['Labels']
    files = labels['com.docker.compose.project.config_files'].split(',')
    project = labels['com.docker.compose.project']
    args = ['docker', 'compose', '-p', project]
    for file in files:
        args += ['-f', file]
    config = json.loads(run(args + ['config', '--format', 'json']))
    services = [c['Config']['Labels']['com.docker.compose.service'] for c in containers]
    before = release / 'broker.before.private.json'
    before.write_text(json.dumps(config))
    before.chmod(0o600)
    run(['docker', 'load', '-i', str(image)])
    tag = 'tp-community-broker:source-' + sha
    if json.loads(run(['docker', 'image', 'inspect', tag]))[0]['Id'] != manifest['image_id']:
        raise RuntimeError('Broker image identity mismatch')
    for service in services:
        config['services'][service]['image'] = tag
        config['services'][service].pop('build', None)
    next_config = release / 'broker.next.private.json'
    next_config.write_text(json.dumps(config))
    next_config.chmod(0o600)
    command = ['docker', 'compose', '-p', project, '-f', str(next_config)]
    try:
        run(command + ['up', '-d', '--no-deps', '--force-recreate'] + services)
        new = json.loads(run(['docker', 'inspect'] + [c['Name'].lstrip('/') for c in containers]))
        if any(n['Id'] == o['Id'] or n['Image'] != manifest['image_id'] or not n['State']['Running'] for o, n in zip(containers, new)):
            raise RuntimeError('Broker replacement not confirmed')
        secret = pathlib.Path(environment['TP_BROKER_TOKEN_FILE']).read_text().strip()
        payload = json.dumps({'broker_cell': environment['TP_BROKER_CELL'], 'broker_instance': environment['TP_BROKER_INSTANCE']}).encode()
        for attempt in range(30):
            try:
                request = urllib.request.Request(environment['TP_BROKER_OWNER_URL'] + '/v1/credential-leases/ready', data=payload,
                    headers={'Authorization': 'Bearer ' + secret, 'Content-Type': 'application/json'})
                with urllib.request.urlopen(request, timeout=2) as response:
                    if json.load(response) == {'protocol': 1, 'ready': True}:
                        return environment
            except Exception:
                pass
            time.sleep(1)
        raise RuntimeError('Broker credential protocol readiness failed')
    except Exception:
        run(['docker', 'compose', '-p', project, '-f', str(before), 'up', '-d', '--no-deps', '--force-recreate'] + services)
        raise
