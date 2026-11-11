import fcntl,json,os,pathlib,re,subprocess,urllib.request
root=pathlib.Path('/opt/tp-community-actions')
lock=(root/'deployment.lock').open('a')
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
approved=['/opt/tp-community-actions/encore-build-cache','/root/.cache/encore-build']
def run(args,env=None):
 p=subprocess.run(args,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90,cwd='/opt/tp-community-actions')
 if p.returncode:raise RuntimeError('Approved cache operation failed')
 return p.stdout.decode().strip()
def health():
 with urllib.request.urlopen('http://127.0.0.1:14000/v1/health',timeout=5) as response:
  if json.load(response).get('status')!='ok':raise RuntimeError('Native health check failed')
health()
before=run(['docker','ps','--format','{{.ID}} {{.Names}}'])
sizes={}
for literal in approved:
 p=pathlib.Path(literal)
 if not p.is_dir() or p.is_symlink() or str(p.resolve())!=literal:
  raise RuntimeError('Approved cache path does not resolve exactly')
 if any(x.is_symlink() for x in p.rglob('*')):raise RuntimeError('Symlink in approved cache')
 for entry in p.iterdir():
  if entry.name not in ('README','trim.txt') and not re.fullmatch('[0-9a-f]{2}',entry.name):
   raise RuntimeError('Unexpected file in approved Go build cache')
 sizes[literal]={'before_bytes':int(run(['du','-s','-B1',literal]).split()[0])}
for literal in approved:
 env=dict(os.environ,GOCACHE=literal,GOTOOLCHAIN='local')
 run(['/usr/local/go/bin/go','clean','-cache'],env)
 sizes[literal]['after_bytes']=int(run(['du','-s','-B1',literal]).split()[0])
health()
if run(['docker','ps','--format','{{.ID}} {{.Names}}'])!=before:
 raise RuntimeError('Running container set changed during cache operation')
print(json.dumps({'cleaned_only_approved_caches':sizes,'running_containers_unchanged':True,'native_health':'ok',
 'memory':run(['free','-b']),'disk':run(['df','-B1','/opt'])}))
