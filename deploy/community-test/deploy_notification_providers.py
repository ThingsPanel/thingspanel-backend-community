"""Upgrade the two existing official providers on the community test host."""
import fcntl
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import time
import urllib.request

ROOT = pathlib.Path('/opt/tp-community-main-20261028')
ACTIONS = pathlib.Path('/opt/tp-community-actions')
UNIT = 'tp-notification-smtp.service'
PUSH = 'tp-notification-app-unipush-1'


def run(*cmd):
    # Compose output can include expanded private configuration on failures.
    result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
    if result.returncode:
        raise RuntimeError('Provider deployment command failed: ' + cmd[0])
    return result.stdout.decode().strip()


def write_atomic(path, data, mode):
    tmp = path.with_name(path.name + '.next')
    tmp.write_bytes(data)
    tmp.chmod(mode)
    os.replace(str(tmp), str(path))


def smtp_process():
    pid = run('systemctl', 'show', UNIT, '-p', 'MainPID').split('=')[-1]
    if not pid.isdigit() or pid == '0':
        raise RuntimeError('SMTP service is not running')
    return pathlib.Path('/proc') / pid


def smtp_env(proc):
    return dict(entry.split('=', 1) for entry in (proc/'environ').read_bytes().decode().split('\0') if '=' in entry)


def probe(env, port, save=False):
    address = 'http://127.0.0.1:' + str(port)
    headers = {'Authorization': 'Bearer ' + env['NOTIFICATION_PLUGIN_TOKEN']}
    def request(path, body=None):
        req = urllib.request.Request(address + path, headers=headers)
        if body is not None:
            req.method = 'PUT'
            req.add_header('Content-Type', 'application/json')
            req.data = json.dumps(body).encode()
        return urllib.request.urlopen(req, timeout=15).read()
    view = json.loads(request('/v1/deployment-settings'))
    if set(view) != {'address', 'version', 'hasSecret'}:
        raise RuntimeError('Unexpected settings response')
    if save and view['address']:
        # Exercise a real save with the already approved SMTP destinations only.
        value = json.loads(request('/v1/deployment-settings', {'address':view['address'],'version':view['version']}))
        if value['version'] != view['version'] + 1 or json.loads(request('/v1/deployment-settings')) != value:
            raise RuntimeError('SMTP settings did not persist')
    if view['address']:
        request('/healthz')


def wait_probe(env, port):
    for _ in range(15):
        try:
            probe(env, port)
            return
        except OSError:
            time.sleep(1)
    raise RuntimeError('Provider settings endpoint failed to start')


def deploy(release):
    proc = smtp_process()
    binary = ROOT/'notification-smtp'
    if (proc/'exe').resolve() != binary or binary.is_symlink():
        raise RuntimeError('Unrecognized SMTP service layout')
    info = json.loads(run('docker','inspect',PUSH))[0]
    labels = info['Config']['Labels']
    compose = ROOT/'notification-app-migration/compose.private.json'
    if labels.get('com.docker.compose.project.config_files') != str(compose) or labels.get('com.docker.compose.service') != 'unipush' or info['Path']!='/notification-unipush':
        raise RuntimeError('Unrecognized App push deployment')
    project = labels['com.docker.compose.project']
    config = json.loads(compose.read_text())
    before = compose.read_bytes()
    service = config['services']['unipush']
    if not isinstance(service.get('environment', {}), dict):
        raise RuntimeError('Unsupported App push environment')
    uid,gid = info['Config']['User'].split(':')
    bindings = info['NetworkSettings']['Ports'].get('5009/tcp') or []
    local = [b for b in bindings if b['HostIp']=='127.0.0.1']
    if not local:
        raise RuntimeError('Expected loopback App push port')
    push_port = int(local[0]['HostPort'])
    push_env = dict(x.split('=',1) for x in info['Config']['Env'] if '=' in x)
    env = smtp_env(proc)
    smtp_port = int(env['NOTIFICATION_PLUGIN_LISTEN'].rsplit(':',1)[-1])
    state = ROOT/'notification-provider-state'
    state.mkdir(exist_ok=True)
    state.chmod(0o711)
    for name, user, group in [('smtp',proc.stat().st_uid,proc.stat().st_gid),('unipush',int(uid),int(gid))]:
        path=state/name
        path.mkdir(mode=0o700,exist_ok=True)
        os.chown(str(path),user,group)
    dropdir = pathlib.Path('/etc/systemd/system')/(UNIT+'.d')
    dropdir.mkdir(exist_ok=True)
    drop = dropdir/'provider-settings.conf'
    prior = drop.read_bytes() if drop.exists() else None
    backup = release/'smtp.previous'
    shutil.copy2(str(binary),str(backup))
    # Keep the container executable in a traversable operator-owned directory.
    next_push = ROOT/('notification-unipush-'+release.name)
    write_atomic(next_push,(release/'unipush').read_bytes(),0o755)
    service['volumes']=[v for v in service.get('volumes',[]) if not(isinstance(v,str) and len(v.split(':'))>1 and v.split(':')[1] in ('/notification-unipush','/var/lib/notification'))]
    service['volumes'].extend([str(next_push)+':/notification-unipush:ro',str(state/'unipush')+':/var/lib/notification'])
    service.setdefault('environment',{})['NOTIFICATION_SETTINGS_FILE']='/var/lib/notification/provider.json'
    args = ['docker','compose','-p',project,'-f',str(compose)]
    try:
        write_atomic(drop,('[Service]\nEnvironment=NOTIFICATION_SETTINGS_FILE='+str(state/'smtp/provider.json')+'\n').encode(),0o644)
        write_atomic(binary,(release/'smtp').read_bytes(),0o755)
        run('systemctl','daemon-reload')
        run('systemctl','restart',UNIT)
        wait_probe(env,smtp_port)
        write_atomic(compose,json.dumps(config,indent=2).encode(),0o600)
        run(*(args+['config','--quiet']))
        run(*(args+['up','-d','--no-deps','unipush']))
        wait_probe(push_env,push_port)
        probe(env,smtp_port,save=True)
        print('SMTP settings read/save/read and App push settings read passed; no notification sent')
    except Exception:
        write_atomic(compose,before,0o600)
        write_atomic(binary,backup.read_bytes(),0o755)
        if prior is None:
            if drop.exists():drop.unlink()
        else:
            write_atomic(drop,prior,0o644)
        run('systemctl','daemon-reload')
        run('systemctl','restart',UNIT)
        run(*(args+['up','-d','--no-deps','unipush']))
        raise


if __name__=='__main__':
    sha=sys.argv[1]
    if not re.fullmatch('[a-f0-9]{40}',sha):
        raise SystemExit('Invalid release revision')
    release=ACTIONS/'provider-releases'/sha
    for name in ('smtp','unipush'):
        if hashlib.sha256((release/name).read_bytes()).hexdigest()!=(release/(name+'.sha256')).read_text().strip():
            raise SystemExit('Invalid provider artifact')
    with (ACTIONS/'deployment.lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        deploy(release)
