"""Deploy only the Encore and notification services on the community test host."""
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
import urllib.error
from encore_migrations import source_migrations, pending_migrations

ROOT = pathlib.Path('/opt/tp-community-actions')
RUNTIME = pathlib.Path('/opt/tp-community-main-20261028')
TOOLS = ROOT / 'tools/encore-1.58.6'
SHA = sys.argv[1]
if not re.fullmatch('[0-9a-f]{40}', SHA):
    raise SystemExit('Invalid revision')
os.umask(0o077)
lock = (ROOT / 'deployment.lock').open('a')
fcntl.flock(lock, fcntl.LOCK_EX)
release = ROOT / 'encore-releases' / SHA
release.mkdir(parents=True, exist_ok=True)
if shutil.disk_usage(str(ROOT)).free < 1024 ** 3:
    raise RuntimeError('At least 1 GiB free disk required for build')
mem = int(next(line.split()[1] for line in pathlib.Path('/proc/meminfo').read_text().splitlines()
               if line.startswith('MemAvailable:')))
if mem < 500 * 1024:
    raise RuntimeError('Insufficient available memory for bounded build')


def run(args, data=None, cwd=None, env=None, timeout=1800):
    result = subprocess.run(args, input=data, cwd=str(cwd) if cwd else None, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    if result.returncode:
        (release / 'failure.private.log').write_bytes(result.stdout)
        raise RuntimeError('Command failed; details retained in private release log')
    return result.stdout.decode().strip()


package = ROOT / 'incoming' / ('encore-' + SHA + '.tar.gz')
with tarfile.open(str(package)) as archive:
    members = archive.getmembers()
    for member in members:
        dest = (release / member.name).resolve()
        if (member.issym() or member.islnk() or not (member.isfile() or member.isdir())
                or not str(dest).startswith(str(release.resolve()) + '/')):
            raise RuntimeError('Unsafe source bundle')
        # Git must see deployment-owned source, not the CI runner's UID.
        member.uid, member.gid = os.getuid(), os.getgid()
        member.uname = member.gname = ''
    archive.extractall(str(release), members=members)
manifest = json.loads((release / 'manifest.json').read_text())
if manifest['commit'] != SHA:
    raise RuntimeError('Source revision mismatch')
for name, checksum in manifest['sha256'].items():
    path = release / name
    if not str(path.resolve()).startswith(str(release.resolve()) + '/'):
        raise RuntimeError('Unsafe source manifest')
    if hashlib.sha256(path.read_bytes()).hexdigest() != checksum:
        raise RuntimeError('Source checksum mismatch')
source = release / 'encore-api'
env = os.environ.copy()
env.update(ENCORE_INSTALL=str(TOOLS), GOMAXPROCS='1', GOMEMLIMIT='384MiB', GOFLAGS='-p=1',
           GOCACHE=str(ROOT / 'encore-build-cache'), GOPROXY='https://goproxy.cn,direct')
env['PATH'] = str(TOOLS / 'bin') + ':/usr/local/go/bin:' + env.get('PATH', '')
cli = str(TOOLS / 'bin/encore')
if not run([cli, 'version'], env=env).startswith('encore version v1.58.6'):
    raise RuntimeError('Unexpected Encore CLI version')
run(['git', 'init', '-q', str(source)])
image = 'tp-community-encore:source-' + SHA
with (release / 'build.private.log').open('wb') as log:
    result = subprocess.run([cli, 'build', 'docker', image,
                             '--services=authn,thingmodel,automation,notification', '--skip-config'],
                            cwd=str(source), env=env, stdout=log, stderr=subprocess.STDOUT,
                            timeout=1800)
    if result.returncode:
        raise RuntimeError('Encore build failed; old services remain running')
image_id = json.loads(run(['docker', 'image', 'inspect', image]))[0]['Id']

compose = RUNTIME / 'compose.encore.private.json'
before = compose.read_bytes()
config = json.loads(before.decode())
services = ['encore-api', 'notification-core']
old_images = {name: config['services'][name]['image'] for name in services}
runtime = json.loads((RUNTIME / 'runtime.encore.private.json').read_text())
databases = runtime['sql_servers'][0]['databases']
resources = {'device_metadata': 'devicemetadata/migrations', 'automation': 'automation/migrations',
             'notification': 'l3-core/notification/migrations'}
plans = []


def sql(database, script):
    return run(['docker', 'exec', '-i', 'timescaledb', 'psql', '-X', '-U', 'postgres', '-d', database,
                '-At', '-v', 'ON_ERROR_STOP=1'], data=script.encode())


for logical, directory in resources.items():
    matches = [(name, db) for name, db in databases.items() if db['name'] == logical]
    if len(matches) != 1:
        raise RuntimeError('Resource database mapping is ambiguous')
    database, db = matches[0]
    if not re.fullmatch('[a-z0-9_]+', database) or not re.fullmatch('[a-z0-9_]+', db['username']):
        raise RuntimeError('Invalid resource identifier')
    ledger = json.loads(sql(database, 'SELECT coalesce(json_agg(t),\'[]\'::json) FROM deployment_schema_ledger t;'))
    schema = json.loads(sql(database, 'SELECT coalesce(json_agg(t),\'[]\'::json) FROM schema_migrations t;'))
    pending = pending_migrations(source_migrations(source / directory), ledger, schema)
    plans.append((database, db['username'], pending))

compose_args = ['docker', 'compose', '-f', str(compose)]


def wait_healthy(expected):
    for attempt in range(45):
        try:
            for name in services:
                container = run(compose_args + ['ps', '-q', name])
                info = json.loads(run(['docker', 'inspect', container]))[0]
                if not info['State']['Running'] or info['Image'] != expected[name]:
                    raise RuntimeError('Running image mismatch')
            health = json.load(urllib.request.urlopen('http://127.0.0.1:14000/v1/health', timeout=5))
            if health.get('status') != 'ok':
                raise RuntimeError('Encore health failed')
            # Protected notification endpoint: 401 confirms its HTTP/auth stack is alive.
            try:
                urllib.request.urlopen('http://127.0.0.1:18091/api/v2/notification-groups', timeout=5)
            except urllib.error.HTTPError as error:
                if error.code != 401:
                    raise
            else:
                raise RuntimeError('Notification endpoint accepted an unauthenticated request')
            return
        except Exception:
            time.sleep(2)
    raise RuntimeError('Native service verification failed')


def verify_tenant():
    qa = json.loads((RUNTIME / 'qa-tenant.private.json').read_text())
    request = urllib.request.Request('http://127.0.0.1:9999/api/v1/login',
        data=json.dumps({'email': qa['email'], 'password': qa['password']}).encode(),
        headers={'Content-Type': 'application/json'})
    login = json.load(urllib.request.urlopen(request, timeout=10))
    token = (login.get('data') or {}).get('token')
    if not token:
        raise RuntimeError('Independent acceptance tenant login failed')
    for path in ['/api/v2/thing-models', '/api/v2/notification-groups', '/automation-core/v1/rules?limit=5']:
        request = urllib.request.Request('http://127.0.0.1' + path,
            headers={'Host': 'c.thingspanel.cn', 'Authorization': 'Bearer ' + token, 'x-token': token})
        with urllib.request.urlopen(request, timeout=10) as response:
            if response.status != 200:
                raise RuntimeError('Acceptance tenant native API failed')
            json.load(response)


if any(pending for _, _, pending in plans):
    run(compose_args + ['stop'] + services)
for database, role, pending in plans:
    if not pending:
        continue
    script = 'BEGIN; SET LOCAL ROLE "' + role + '";\n'
    for migration in pending:
        script += migration['sql'] + '\n'
        script += "INSERT INTO deployment_schema_ledger VALUES ({},'{}','{}');\n".format(
            migration['version'], migration['filename'], migration['checksum'])
    script += 'UPDATE schema_migrations SET version={}, dirty=false; COMMIT;'.format(pending[-1]['version'])
    sql(database, script)
for name in services:
    config['services'][name]['image'] = image
temp = compose.with_name('compose.encore.next.private.json')
temp.write_text(json.dumps(config, indent=2))
run(['docker', 'compose', '-f', str(temp), 'config', '--quiet'])
os.replace(str(temp), str(compose))
run(compose_args + ['up', '-d', '--no-deps'] + services)
wait_healthy({name: image_id for name in services})
verify_tenant()

# Deployment-scoped worker credentials live only in the existing private config.
# A single systemd oneshot cannot overlap itself, even if one pass is slow.
scheduler = ROOT / 'automation-scheduler'
scheduler.mkdir(mode=0o700, exist_ok=True)
shutil.copyfile(str(ROOT / 'incoming/automation_tick.py'), str(scheduler / 'automation_tick.next.py'))
os.replace(str(scheduler / 'automation_tick.next.py'), str(scheduler / 'automation_tick.py'))
pathlib.Path('/etc/systemd/system/tp-automation-tick.service').write_text('''[Unit]
Description=ThingsPanel native automation scheduler pass
After=network-online.target docker.service

[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /opt/tp-community-actions/automation-scheduler/automation_tick.py
TimeoutStartSec=40
UMask=0077
''')
pathlib.Path('/etc/systemd/system/tp-automation-tick.timer').write_text('''[Unit]
Description=Wake ThingsPanel native automation every five seconds

[Timer]
OnBootSec=10s
OnUnitInactiveSec=5s
AccuracySec=1s
Unit=tp-automation-tick.service

[Install]
WantedBy=timers.target
''')
run(['systemctl', 'daemon-reload'])
run(['systemctl', 'start', 'tp-automation-tick.service'])
run(['systemctl', 'enable', '--now', 'tp-automation-tick.timer'])
receipt = json.loads((scheduler / 'last-tick.json').read_text())
if receipt.get('status') != 'passed':
    raise RuntimeError('Native scheduler acceptance failed')

(release / 'deployment.json').write_text(json.dumps({'commit': SHA, 'image': image, 'image_id': image_id,
    'scheduler': 'tp-automation-tick.timer',
    'previous_images': old_images, 'migrations_applied': {database: [m['version'] for m in pending]
                                                       for database, _, pending in plans}}, indent=2))
print('Encore and notification deployed and verified: ' + SHA)
