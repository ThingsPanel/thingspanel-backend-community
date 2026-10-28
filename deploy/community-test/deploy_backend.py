"""Deploy a verified backend bundle to the community test server (Python 3.6+)."""
import fcntl,hashlib,json,os,pathlib,re,shutil,subprocess,sys,tarfile,time,urllib.request

ROOT=pathlib.Path('/opt/tp-community-actions')
APP=pathlib.Path('/home/thingspanel/thingspanel-backend-community')
PM='/root/.local/share/pnpm/pm2'
SHA=sys.argv[1]
if not re.fullmatch('[0-9a-f]{40}',SHA):raise SystemExit('Invalid source revision')
os.umask(0o077);ROOT.mkdir(mode=0o700,exist_ok=True)
lock=(ROOT/'deployment.lock').open('a');fcntl.flock(lock,fcntl.LOCK_EX)
release=ROOT/'backend-releases'/SHA
if release.exists():raise SystemExit('This release already exists; inspect before redeployment')
release.mkdir(parents=True,mode=0o700)

def run(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 if p.returncode:
  (release/'failure.private.log').write_bytes(p.stdout)
  raise RuntimeError('Deployment command failed; details retained privately')
 return p.stdout.decode().strip()

package=ROOT/'incoming'/('backend-'+SHA+'.tar.gz')
with tarfile.open(str(package)) as tf:
 for m in tf.getmembers():
  dest=(release/m.name).resolve()
  if m.issym() or m.islnk() or not str(dest).startswith(str(release.resolve())+'/'):raise RuntimeError('Unsafe package entry')
 tf.extractall(str(release))
manifest=json.loads((release/'manifest.json').read_text())
if manifest['commit']!=SHA:raise RuntimeError('Revision mismatch')
for name,expected in manifest['sha256'].items():
 p=release/name
 if p.resolve().parent!=release.resolve() and not str(p.resolve()).startswith(str(release.resolve())+'/'):raise RuntimeError('Unsafe checksum path')
 if hashlib.sha256(p.read_bytes()).hexdigest()!=expected:raise RuntimeError('Checksum mismatch: '+name)
binary=release/'ThingsPanel-Go';binary.chmod(0o755)
version='dev-'+SHA
if run([str(binary),'--version'])!=version:raise RuntimeError('Build version mismatch')
config=APP/'configs/conf.yml'
if not config.is_file():raise RuntimeError('Existing private runtime configuration required')
config_sha=hashlib.sha256(config.read_bytes()).hexdigest()
target=APP/'thingspanel-backend-community'
shutil.copy2(str(target),str(release/'backend.before'))
resources=['sql','static','configs/casbin.conf','configs/messages.yaml','configs/messages_str.yaml']
before=release/'resources.before';before.mkdir()
existing=[]
for name in resources:
 p=APP/name;backup=before/name
 if p.exists():
  backup.parent.mkdir(parents=True,exist_ok=True)
  if p.is_dir():shutil.copytree(str(p),str(backup))
  else:shutil.copy2(str(p),str(backup))
  existing.append(name)
def install_file(src,dest):
 dest.parent.mkdir(parents=True,exist_ok=True)
 temp=dest.with_name(dest.name+'.deploy-next');shutil.copy2(str(src),str(temp));os.replace(str(temp),str(dest))
def install_resources(source):
 for name in resources:
  p=source/name
  if p.is_dir():
   for f in p.rglob('*'):
    if f.is_file():install_file(f,APP/name/f.relative_to(p))
  elif p.is_file():install_file(p,APP/name)
def wait_version(expected):
 for _ in range(45):
  try:
   with urllib.request.urlopen('http://127.0.0.1:9999/health',timeout=3) as response:
    if response.status!=200:raise RuntimeError('Health failed')
   with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=3) as response:
    if json.load(response).get('data',{}).get('version')==expected:return
  except Exception:pass
  time.sleep(1)
 raise RuntimeError('Running version did not match deployed source')
old_version=run([str(target),'--version'])
try:
 install_resources(release)
 run([PM,'stop','thingspanel-backend-community'])
 install_file(binary,target)
 run([PM,'restart','thingspanel-backend-community'])
 wait_version(version)
 if hashlib.sha256(config.read_bytes()).hexdigest()!=config_sha:raise RuntimeError('Private configuration changed')
 run([PM,'save'])
except Exception:
 run([PM,'stop','thingspanel-backend-community'])
 install_file(release/'backend.before',target);install_resources(before)
 run([PM,'restart','thingspanel-backend-community']);wait_version(old_version)
 (release/'rollback.json').write_text(json.dumps({'restored_version':old_version}));raise
result={'status':'deployed','commit':SHA,'version':version,'previous_version':old_version,'private_config_unchanged':True,'rollback_directory':str(release)}
(release/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
