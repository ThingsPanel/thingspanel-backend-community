import fcntl,json,os,pathlib,signal,subprocess,time,urllib.request
root=pathlib.Path('/opt/tp-community-actions')
lock=(root/'deployment.lock').open('a')
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
target=pathlib.Path('/proc/3397375')
expected='/opt/tp-community-actions/tools/encore-1.58.6/bin/encore'
def run(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Resource check failed')
 return p.stdout.decode().strip()
before=run(['docker','ps','--format','{{.ID}} {{.Names}}'])
def health():
 with urllib.request.urlopen('http://127.0.0.1:14000/v1/health',timeout=5) as response:
  if json.load(response).get('status')!='ok':raise RuntimeError('Native health check failed')
health()
if target.exists():
 words=[a.decode() for a in (target/'cmdline').read_bytes().split(b'\x00') if a]
 if (os.readlink(str(target/'exe'))!=expected or words[:2]!=[expected,'daemon']
     or os.readlink(str(target/'cwd'))!='/opt/tp-community-actions/encore-preflight/encore-api'):
  raise RuntimeError('Observed build daemon identity changed; no process stopped')
 children=(target/'task'/'3397375'/'children').read_text().strip()
 if children:raise RuntimeError('Build daemon has children; no process stopped')
 os.kill(3397375,signal.SIGTERM)
 for _ in range(20):
  if not target.exists():break
  time.sleep(0.5)
 if target.exists():raise RuntimeError('Build daemon did not exit; no forced kill attempted')
time.sleep(2)
health()
after=run(['docker','ps','--format','{{.ID}} {{.Names}}'])
if before!=after:raise RuntimeError('Running container set changed during resource verification')
print(json.dumps({'idle_build_daemon_stopped':True,'running_containers_unchanged':True,
 'native_health':'ok','memory':run(['free','-b']),'disk':run(['df','-B1','/opt'])}))
