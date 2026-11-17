"""Inspect database availability without depending on SQL or exposing credentials."""
import json
import pathlib
import shutil
import subprocess


def command(args):
    return subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          universal_newlines=True, timeout=30)


def main():
    result = {'read_only': True, 'disk_free_bytes': shutil.disk_usage('/').free}
    result['memory'] = [line for line in pathlib.Path('/proc/meminfo').read_text().splitlines()
                        if line.startswith(('MemAvailable:', 'SwapTotal:', 'SwapFree:'))]
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
    for name in ('/opt/tp-community-actions/incoming', '/opt/tp-community-actions/frontend-releases',
                 '/var/lib/docker/containers', '/var/log'):
        size = command(['du', '-sk', name])
        if size.returncode == 0:
            result.setdefault('directory_bytes', {})[name] = int(size.stdout.split()[0]) * 1024
    print(json.dumps(result))


if __name__ == '__main__':
    main()
