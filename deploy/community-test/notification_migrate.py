"""Inspect the existing test deployment without exporting credentials."""
import json
import pathlib
import re
import subprocess
import sys
import ssl
import urllib.request


def command(args):
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    return p.returncode, p.stdout.decode(errors='replace')


def inspect():
    result = {}
    _, raw = command(['docker', 'ps', '--format', '{{.Names}}'])
    containers = []
    for name in raw.splitlines():
        if not any(x in name.lower() for x in ['notification', 'encore', 'smtp']):
            continue
        rc, data = command(['docker', 'inspect', name])
        if rc:
            continue
        d = json.loads(data)[0]
        env = dict(x.split('=', 1) for x in d['Config']['Env'] if '=' in x)
        containers.append({'name': name, 'image': d['Config']['Image'],
                           'networks': list(d['NetworkSettings']['Networks']),
                           'envKeys': list(env),
                           'mounts': [{'src': x['Source'], 'dst': x['Destination']} for x in d['Mounts']]})
    result['containers'] = containers
    runtime = pathlib.Path('/opt/tp-community-main-20261028')
    result['runtimeFiles'] = [x.name for x in runtime.iterdir()] if runtime.exists() else []
    pm2 = '/root/.local/share/pnpm/pm2'
    rc, raw = command([pm2, 'jlist'])
    if rc == 0:
        processes = []
        for p in json.loads(raw):
            e = p['pm2_env']
            selected = {k: e.get(k) for k in ['NOTIFICATION_SOURCE_BRIDGE_ENABLED',
                        'NOTIFICATION_SOURCE_DEPLOYMENT_ID', 'NOTIFICATION_ENCORE_BASE_URL']}
            present = {k: bool(e.get(k)) for k in ['NOTIFICATION_SOURCE_BEARER_TOKEN',
                       'NOTIFICATION_PROJECTION_BEARER_TOKEN', 'NOTIFICATION_ENCORE_CA_FILE']}
            processes.append({'name': p['name'], 'script': e.get('pm_exec_path'),
                              'notification': selected, 'configured': present})
        result['processes'] = processes
    rc, raw = command(['nginx', '-T'])
    result['nginxRouting'] = [x.strip() for x in raw.splitlines()
                              if re.match(r'\s*(?:location |listen |proxy_pass |ssl_certificate)', x)]
    for unit in ['tp-notification-renew.timer', 'tp-notification-renew.service']:
        rc, raw = command(['systemctl', 'is-active', unit])
        result[unit] = raw.strip()
    print(json.dumps(result, ensure_ascii=False, indent=2))


def repair_snapshot_route():
    path = pathlib.Path('/etc/nginx/conf.d/tp-notification-app.conf')
    content = path.read_text()
    location = ' location /api/v1/notification-source/ { proxy_pass http://127.0.0.1:18091; proxy_set_header Authorization $http_authorization; }\n'
    marker = ' location /api/v2/ { proxy_pass http://127.0.0.1:18091; proxy_set_header Authorization $http_authorization; }\n'
    if location not in content:
        if marker not in content:
            raise SystemExit('Unexpected internal notification routing')
        path.write_text(content.replace(marker, marker + location))
        rc, _ = command(['nginx', '-t'])
        if rc:
            path.write_text(content)
            raise SystemExit('Internal routing check failed')
        rc, _ = command(['systemctl', 'reload', 'nginx'])
        if rc:
            raise SystemExit('Internal routing reload failed')
    print(json.dumps({'status': 'internal_snapshot_route_ready'}))


def verify_restore():
    root = pathlib.Path('/opt/tp-community-main-20261028')
    work = root / 'notification-app-migration'
    _, raw = command(['/root/.local/share/pnpm/pm2', 'jlist'])
    process = next(p for p in json.loads(raw) if p['name'] == 'thingspanel-backend-community')['pm2_env']
    if process.get('NOTIFICATION_SOURCE_BRIDGE_ENABLED') != 'true':
        raise SystemExit('Notification bridge is not enabled')
    url = 'http://127.0.0.1:9999/api/v1/notification/source-app-members/a9c24ba0-738d-9477-6053-34ed8a8cd3c2?tenantId=63a4b529'
    request = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + process['NOTIFICATION_MEMBER_READER_TOKEN'],
              'X-Notification-Deployment': process['NOTIFICATION_SOURCE_DEPLOYMENT_ID']})
    with urllib.request.urlopen(request, timeout=10) as response:
        devices = json.load(response)
    pair = json.loads((work / 'source.private.json').read_text())
    url = 'https://127.0.0.1:18446/api/v1/notification-source/group-snapshots/6dafe89c-47fa-4c8d-8fdc-4fbfd876d4cf?tenantId=63a4b529&groupRevision=2'
    request = urllib.request.Request(url, headers={'Authorization': 'Bearer ' + pair['projectionCredential']})
    with urllib.request.urlopen(request, timeout=10, context=ssl.create_default_context(cafile=str(root / 'notification-ca.pem'))) as response:
        snapshot = json.load(response)['data']
    if not snapshot['group']['enabled'] or snapshot['group']['revision'] != 2:
        raise SystemExit('Published snapshot verification failed')
    if not all(i['enabled'] for i in snapshot['instances']) or not all(p['enabled'] for p in snapshot['plugins']):
        raise SystemExit('Published targets are not enabled')
    _, timer = command(['systemctl', 'is-active', 'tp-notification-app-renew.timer'])
    _, renewal = command(['systemctl', 'show', 'tp-notification-app-renew.service', '--property=ExecMainStatus', '--value'])
    receipt = json.loads((work / 'renew-result.json').read_text())
    if timer.strip() != 'active' or renewal.strip() != '0' or receipt.get('status') != 'passed':
        raise SystemExit('Service credential renewal verification failed')
    result = {'status': 'migrated', 'tenant': 'test@test.cn', 'tenantId': '63a4b529',
              'plugin': 'thingspanel.unipush 2.0.0', 'groupId': snapshot['group']['id'],
              'publishedRevision': snapshot['group']['revision'], 'activeAppDeviceCount': len(devices['deviceIds']),
              'sourceIdentityRenewal': 'active', 'identityExpiresAt': receipt.get('expires_at'),
              'defaultPolicyChanged': False, 'phoneNotificationSent': False}
    (work / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2))
    incoming = pathlib.Path('/opt/tp-community-actions/incoming/notification-migration.private.json')
    if incoming.exists():
        incoming.unlink()
    print(json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('A maintenance mode is required')
    if sys.argv[1] == 'inspect':
        inspect()
    elif sys.argv[1] == 'repair-route':
        repair_snapshot_route()
    elif sys.argv[1] == 'verify':
        verify_restore()
    else:
        raise SystemExit('Unsupported maintenance mode')
