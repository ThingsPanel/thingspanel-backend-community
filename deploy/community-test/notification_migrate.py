"""Inspect the existing test deployment without exporting credentials."""
import json
import pathlib
import re
import subprocess
import sys


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


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('A maintenance mode is required')
    if sys.argv[1] == 'inspect':
        inspect()
    elif sys.argv[1] == 'repair-route':
        repair_snapshot_route()
    else:
        raise SystemExit('Unsupported maintenance mode')
