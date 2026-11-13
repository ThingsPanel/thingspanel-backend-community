"""Prepared only. Read-only baseline under the existing deployment lock.

No env/command arguments, credentials, DB queries, writes, restarts or deployments.
Run only after root explicitly assigns an ops window. Python 3.6 compatible.
"""
import datetime
import fcntl
import hashlib
import json
import pathlib
import shutil
import subprocess

ROOT = pathlib.Path('/opt/tp-community-main-20261028')
LOCK = pathlib.Path('/opt/tp-community-actions/deployment.lock')
NAMES = ['tp-community-main-47-encore-api-1',
         'tp-community-main-47-broker-owner-1',
         'tp-community-main-47-broker-mosquitto-1']

def run(args):
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                       universal_newlines=True, timeout=15)
    if p.returncode:
        raise RuntimeError('read-only command failed')
    return p.stdout

def file_record(path):
    p = pathlib.Path(path)
    if not p.exists():
        return {'path': str(p), 'exists': False}
    if p.is_symlink() or not p.is_file():
        return {'path': str(p), 'exists': True, 'regular_file': False}
    h = hashlib.sha256()
    with p.open('rb') as f:
        for chunk in iter(lambda: f.read(1048576), b''):
            h.update(chunk)
    return {'path': str(p), 'exists': True, 'sha256': h.hexdigest(),
            'size': p.stat().st_size}

def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True,
                                    separators=(',', ':')).encode()).hexdigest()

def main():
    out = {'operation': 'Q09-readonly-release-baseline', 'changes': False,
           'at': datetime.datetime.utcnow().isoformat() + 'Z', 'containers': []}
    try:
        lock = LOCK.open('r')
        fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
    except OSError:
        out['status'] = 'BLOCKED-existing-deployment-lock-unavailable'
        print(json.dumps(out)); return 2
    try:
        out['lock_inode'] = LOCK.stat().st_ino
        for name in NAMES:
            c = json.loads(run(['docker', 'inspect', name]))[0]
            cfg, state = c['Config'], c['State']
            labels = cfg.get('Labels') or {}
            image = json.loads(run(['docker', 'image', 'inspect', c['Image']]))[0]
            compose = []
            for ref in labels.get('com.docker.compose.project.config_files', '').split(','):
                if ref:
                    # No arbitrary label-driven reads outside the approved deployment.
                    p = pathlib.Path(ref)
                    if ROOT not in p.parents or '..' in p.parts:
                        raise RuntimeError('unexpected compose reference')
                    compose.append(file_record(p))
            mounts = [{'type': m['Type'], 'source': m['Source'],
                       'destination': m['Destination'], 'rw': m['RW']}
                      for m in c['Mounts']]
            out['containers'].append({
                'name': name, 'id': c['Id'], 'image_id': c['Image'],
                'image_reference': cfg['Image'],
                'repo_digests': image.get('RepoDigests', []),
                'source_revision': (image.get('Config', {}).get('Labels') or {}).get('org.opencontainers.image.revision'),
                'running': state['Running'], 'health': state.get('Health', {}).get('Status'),
                'restart_count': c['RestartCount'], 'started_at': state['StartedAt'],
                'compose_project': labels.get('com.docker.compose.project'),
                'compose_service': labels.get('com.docker.compose.service'),
                'compose_files': compose, 'mount_references': mounts,
                # Config may contain secrets: compare full config only by hash.
                'runtime_contract_sha256': fingerprint({'Env': cfg.get('Env'),
                    'Cmd': cfg.get('Cmd'), 'Entrypoint': cfg.get('Entrypoint'),
                    'HostConfig': c['HostConfig'], 'Mounts': mounts}),
                'mounted_file_hashes': [file_record(m['source']) for m in mounts
                                       if m['type'] == 'bind' and pathlib.Path(m['source']).is_file()]})
        pm = json.loads(run(['pm2', 'jlist']))
        gin = [p for p in pm if p.get('name') == 'thingspanel-backend-community']
        if len(gin) != 1:
            raise RuntimeError('Gin process identity not unique')
        proc = gin[0]; env = proc['pm2_env']
        out['gin_preserve'] = {'pid': proc['pid'], 'status': env.get('status'),
                              'restarts': env.get('restart_time'),
                              'binary': file_record(env['pm_exec_path'])}
        out['compose_expected'] = file_record(ROOT / 'compose.encore.private.json')
        out['controllers_preserve'] = [file_record(ROOT / p) for p in
            ['http-runtime/scope.py', 'ha-runtime/scope.py',
             'http-runtime/broker_scope_roles.py', 'ha-runtime/broker_scope_roles.py']]
        mem = dict(line.split(':', 1) for line in pathlib.Path('/proc/meminfo').read_text().splitlines())
        disk = shutil.disk_usage(str(ROOT))
        out['capacity'] = {'mem_available_kib': int(mem['MemAvailable'].split()[0]),
                           'disk_free_bytes': disk.free}
        out['systemd_jobs'] = run(['systemctl', 'list-jobs', '--no-legend', '--no-pager']).splitlines()
        out['status'] = 'READ_ONLY_COMPLETE-not-deployment-authorization'
    except Exception as e:
        out['status'] = 'INCOMPLETE'; out['error_type'] = type(e).__name__
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN); lock.close(); out['lock_released'] = True
    print(json.dumps(out))
    return 0 if out['status'].startswith('READ_ONLY_COMPLETE') else 1

if __name__ == '__main__':
    raise SystemExit(main())
