"""Inspect database availability without depending on SQL or exposing credentials."""
import json
import pathlib
import re
import shutil
import subprocess


def command(args):
    return subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          universal_newlines=True, timeout=30)


def main():
    result = {'read_only': True, 'disk_free_bytes': shutil.disk_usage('/').free}
    result['memory'] = [line for line in pathlib.Path('/proc/meminfo').read_text().splitlines()
                        if line.startswith(('MemAvailable:', 'SwapTotal:', 'SwapFree:'))]
    releases = pathlib.Path('/opt/tp-community-actions/backend-releases')
    failures = sorted(releases.glob('*/broker-upgrade.failure.private.log'),
                      key=lambda path: path.stat().st_mtime, reverse=True)
    if failures:
        path = failures[0]
        # Return diagnostic categories only. PostgreSQL DETAIL can contain row data.
        message = path.read_bytes()[-16384:].decode('utf-8', 'replace').lower()
        result['broker_upgrade_failure'] = {
            'revision': path.parent.name,
            'causes': [category for category in (
                'lock timeout', 'statement timeout', 'deadlock detected', 'already exists',
                'does not exist', 'permission denied', 'syntax error', 'no space left',
                'violates', 'must be owner', 'cannot change', 'is not unique', 'ambiguous',
                'connection refused', 'connection to server', 'out of shared memory'
            ) if category in message],
            'objects': sorted(set(re.findall(
                r'"((?:broker_|native_runtime_)[a-z0-9_]+)"', message))),
        }
    info = command(['docker', 'inspect', 'timescaledb'])
    if info.returncode:
        result['database_container_missing'] = True
    else:
        data = json.loads(info.stdout)[0]
        state = data['State']
        result['database'] = {key: state.get(key) for key in
                              ('Status', 'Running', 'Restarting', 'OOMKilled', 'ExitCode', 'StartedAt', 'FinishedAt')}
        result['database']['image'] = data['Config']['Image']
        result['mounts'] = [{'source': m['Source'], 'destination': m['Destination'],
                             'free_bytes': shutil.disk_usage(m['Source']).free}
                            for m in data.get('Mounts', []) if pathlib.Path(m['Source']).exists()]
        logs = command(['docker', 'logs', '--tail', '150', 'timescaledb'])
        text = logs.stdout + logs.stderr
        result['database_log_signals'] = {s: s.lower() in text.lower() for s in
            ('No space left on device', 'Permission denied', 'database system is ready',
             'database system is shut down', 'out of memory', 'PANIC', 'invalid checkpoint',
             'could not write', 'could not open', 'read-only file system')}
        result['database_log_size'] = pathlib.Path(data['LogPath']).stat().st_size if data.get('LogPath') and pathlib.Path(data['LogPath']).exists() else None
        if state.get('Running'):
            ready = command(['docker', 'exec', 'timescaledb', 'pg_isready', '-U', 'postgres'])
            result['database_ready'] = ready.returncode == 0
    result['containers'] = command(['docker', 'ps', '-a', '--format', '{{.Names}}: {{.Status}}']).stdout.splitlines()
    units = command(['systemctl','list-units','--type=service','--all','--no-legend','--plain'])
    result['provider_units'] = [line.split()[0] for line in units.stdout.splitlines()
                                if line.split() and any(n in line.split()[0].lower() for n in ('smtp','unipush','notification'))]
    pid = command(['systemctl','show','tp-notification-smtp.service','-p','MainPID','--value']).stdout.strip()
    if pid.isdigit() and pathlib.Path('/proc',pid,'exe').exists():
        result['smtp_process'] = str(pathlib.Path('/proc',pid,'exe').resolve())
        result['smtp_env_keys'] = [key.decode() for entry in pathlib.Path('/proc',pid,'environ').read_bytes().split(b'\0')
                                   for key in [entry.split(b'=',1)[0]] if re.fullmatch(b'NOTIFICATION_[A-Z_]+', key)]
    push = command(['docker','inspect','tp-notification-app-unipush-1'])
    if push.returncode == 0:
        container = json.loads(push.stdout)[0]
        labels = container['Config'].get('Labels') or {}
        result['unipush_installation'] = {
            'executable': container['Path'], 'compose_file': labels.get('com.docker.compose.project.config_files'),
            'service': labels.get('com.docker.compose.service'), 'user': container['Config'].get('User'),
            'directory_http': any(e.startswith('NOTIFICATION_UNIPUSH_DIRECTORY_URL=http://') for e in container['Config']['Env']),
            'env_keys': [e.split('=',1)[0] for e in container['Config']['Env'] if e.startswith('NOTIFICATION_')],
        }
    native = pathlib.Path('/opt/tp-community-main-20261028/runtime.notification.private.json')
    if native.is_file():
        conf = json.loads(native.read_text())
        for server in conf.get('sql_servers', []):
            for db, spec in server.get('databases', {}).items():
                if spec.get('name') == 'notification' and re.fullmatch(r'[A-Za-z0-9_]+',db):
                    data = command(['docker','exec','timescaledb','psql','-XAt','-U','postgres','-d',db,
                        '-c',"SELECT plugin_id || ' ' || origin FROM notification_plugin_registrations WHERE plugin_id IN ('thingspanel.smtp','thingspanel.unipush')"])
                    result['provider_origins'] = [line for line in data.stdout.splitlines()
                        if re.fullmatch(r'thingspanel\.(smtp|unipush) https?://[a-zA-Z0-9.:_-]+', line)]
    for name in ('/opt/tp-community-actions/incoming', '/opt/tp-community-actions/frontend-releases',
                 '/var/lib/docker/containers', '/var/log'):
        size = command(['du', '-sk', name])
        if size.returncode == 0:
            result.setdefault('directory_bytes', {})[name] = int(size.stdout.split()[0]) * 1024
    print(json.dumps(result))


if __name__ == '__main__':
    main()
