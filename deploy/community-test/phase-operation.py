import fcntl,hashlib,json,os,pathlib,shutil,subprocess,tarfile,urllib.request
root=pathlib.Path('/opt/tp-community-actions')
sha='7e5a3afea76c27fb22b705e054a2d25ceeea0ba8'
os.umask(0o077)
lock=(root/'deployment.lock').open('a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
release=root/'native-rehearsal'/sha
release.mkdir(parents=True,exist_ok=True)
def run(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
 if p.returncode:
  (release/'failure.private.log').write_bytes(p.stderr)
  raise RuntimeError('Candidate staging failed; private log retained')
 return p.stdout.decode().strip()
before=run(['docker','ps','--format','{{.ID}} {{.Names}}'])
with tarfile.open(str(root/'incoming'/('encore-'+sha+'.tar.gz'))) as archive:
 members=archive.getmembers()
 if len({m.name for m in members})!=len(members):raise RuntimeError('Duplicate archive entries')
 for member in members:
  if (member.issym() or member.islnk() or not (member.isfile() or member.isdir())
      or not str((release/member.name).resolve()).startswith(str(release.resolve())+'/')):
   raise RuntimeError('Unsafe candidate archive')
  member.uid,member.gid=os.getuid(),os.getgid();member.uname=member.gname=''
 manifest=json.load(archive.extractfile('manifest.json'))
 image=json.load(archive.extractfile('image.json'))
 if manifest['commit']!=sha or image['commit']!=sha:raise RuntimeError('Candidate source mismatch')
 need=sum(m.size for m in members)+2*image['uncompressed_bytes']+1024**3
 if shutil.disk_usage(str(root)).free<need:raise RuntimeError('Insufficient verified image staging space')
 archive.extractall(str(release),members=members)
for name,digest in manifest['sha256'].items():
 p=release/name
 if not str(p.resolve()).startswith(str(release.resolve())+'/'):raise RuntimeError('Unsafe manifest entry')
 h=hashlib.sha256()
 with p.open('rb') as stream:
  for block in iter(lambda:stream.read(1024*1024),b''):h.update(block)
 if h.hexdigest()!=digest:raise RuntimeError('Candidate checksum mismatch')
run(['docker','load','-i',str(release/'image.tar.gz')])
tag='tp-community-encore:source-'+sha
info=json.loads(run(['docker','image','inspect',tag]))[0]
if info['Id']!=image['image_id']:raise RuntimeError('Candidate image identity mismatch')
runtime=pathlib.Path('/opt/tp-community-main-20261028')
config=json.loads((runtime/'runtime.encore.private.json').read_text())
svc=json.loads((runtime/'compose.encore.private.json').read_text())['services']['encore-api']
if run(['docker','ps','--format','{{.ID}} {{.Names}}'])!=before:raise RuntimeError('Running services changed')
with urllib.request.urlopen('http://127.0.0.1:14000/v1/health',timeout=5) as response:
 if json.load(response).get('status')!='ok':raise RuntimeError('Live service health failed')
print(json.dumps({'source_commit':sha,'image_id':info['Id'],'uncompressed_bytes':image['uncompressed_bytes'],
 'services':image['services'],'staged_only':True,'running_containers_unchanged':True,
 'runtime_keys':sorted(config),'sql_servers_shape':[{'keys':sorted(s),'databases':{k:sorted(v) for k,v in s['databases'].items()}} for s in config['sql_servers']],
 'compose_keys':sorted(svc),'environment_names':sorted(svc.get('environment',{})), 'volumes':svc.get('volumes',[]),
 'disk_free_bytes':shutil.disk_usage(str(root)).free}))
