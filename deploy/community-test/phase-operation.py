"""Deploy the verified interim security binary, preserving config/schema owners."""
import fcntl,hashlib,json,os,pathlib,shutil,subprocess,tarfile,time,urllib.request,urllib.error
sha='850a085dda33d4af16ecb589e497cf0e0a4b6804'
baseline='70bd7a1d31c11a02f3b32a106b0290ebe722b580'
expected_binary='919ad10be49caa4aadce6d49fecba233a59307ec44ae5f3445ed3a81abb0cfe4'
expected_config='6865acfbe72f78f4a7d017b0fe70ddfb899df2b21d547a7f357658bbf809ed03'
root=pathlib.Path('/opt/tp-community-actions')
base=pathlib.Path('/home/thingspanel/thingspanel-backend-community')
exe=base/'thingspanel-backend-community'
config=base/'configs/conf.yml'
pm2='/root/.local/share/pnpm/pm2'
name='thingspanel-backend-community'
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def run(args):return subprocess.check_output(args,stderr=subprocess.STDOUT,timeout=40)
def get(path,token=None,data=None):
 headers={'Content-Type':'application/json'}
 if token:headers['x-token']=token
 req=urllib.request.Request('http://127.0.0.1:9999'+path,data=None if data is None else json.dumps(data).encode(),headers=headers)
 try:return json.load(urllib.request.urlopen(req,timeout=15))
 except urllib.error.HTTPError as e:return json.loads(e.read())
def ledger():
 q="BEGIN READ ONLY; SELECT coalesce(json_agg(t ORDER BY module_name,file_name),'[]'::json) FROM native_component_migration_ledger t; COMMIT;"
 raw=subprocess.check_output(['docker','exec','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel','-c',q],timeout=30)
 return hashlib.sha256(raw).hexdigest()
def verify(version,check_guard=False):
 if get('/api/v1/sys_version').get('data',{}).get('version')!=version:raise RuntimeError('Version health check failed')
 qa=json.loads(pathlib.Path('/opt/tp-community-main-20261028/qa-tenant.private.json').read_text())
 token=get('/api/v1/login',data={'email':qa['email'],'password':qa['password']}).get('data',{}).get('token')
 if not token:raise RuntimeError('Isolated QA login failed')
 if get('/api/v1/device?page=1&page_size=1',token).get('code') not in (0,200):raise RuntimeError('Authenticated device query failed')
 if check_guard and get('/api/v1/attribute/datas/5ed19dd2-4422-dfe3-2a54-13e180f120ed',token).get('code')!=201001:raise RuntimeError('Cross-tenant attribute guard did not deny access')
os.umask(0o077)
with (root/'deployment.lock').open('a') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX)
 if exe.is_symlink() or digest(exe)!=expected_binary or digest(config)!=expected_config:raise RuntimeError('Online binary/config changed')
 process=next(x['pm2_env'] for x in json.loads(run([pm2,'jlist'])) if x['name']==name)
 if process['pm_exec_path']!=str(exe) or process['pm_cwd']!=str(base) or process['status']!='online':raise RuntimeError('Unexpected active device process')
 verify('dev-'+baseline)
 before_ledger=ledger()
 if shutil.disk_usage(str(root)).free<1024**3:raise RuntimeError('Insufficient rollback capacity')
 release=root/'device-safety-releases'/sha
 if release.exists():raise RuntimeError('Release already exists; inspect before retry')
 release.mkdir(parents=True)
 package=root/'incoming'/('device-safety-'+sha+'.tar.gz')
 with tarfile.open(str(package)) as archive:
  members=archive.getmembers()
  if {m.name for m in members}!={'ThingsPanel-Go','RELEASE_PROFILE.json','SHA256SUMS'} or len(members)!=3 or any(not m.isfile() or m.size>300*1024*1024 for m in members):raise RuntimeError('Unexpected package layout')
  for m in members:(release/m.name).write_bytes(archive.extractfile(m).read())
 checks={line.split()[1]:line.split()[0] for line in (release/'SHA256SUMS').read_text().splitlines()}
 if set(checks)!={'ThingsPanel-Go','RELEASE_PROFILE.json'} or any(digest(release/p)!=v for p,v in checks.items()):raise RuntimeError('Package checksum mismatch')
 profile=json.loads((release/'RELEASE_PROFILE.json').read_text())
 if profile.get('profile')!='device-safety' or profile.get('baseline')!=baseline or profile.get('source_commit')!=sha or profile.get('database_owner_changed') is not False or profile.get('device_implementation_changed') is not False:raise RuntimeError('Wrong build profile')
 binary=release/'ThingsPanel-Go';binary.chmod(0o755)
 if run([str(binary),'--version']).decode().strip()!='device-safety-'+sha:raise RuntimeError('Wrong compiled version')
 backup=release/'previous-binary';shutil.copy2(str(exe),str(backup))
 temporary=exe.with_name(exe.name+'.safety-next')
 if temporary.exists():raise RuntimeError('Unexpected staged executable')
 def replace(source):
  shutil.copy2(str(source),str(temporary));temporary.chmod(0o755);os.replace(str(temporary),str(exe))
 try:
  replace(binary);run([pm2,'restart',name])
  ready=False
  for attempt in range(30):
   try:verify('device-safety-'+sha,True);ready=True;break
   except Exception:time.sleep(2)
  if not ready:raise RuntimeError('New device process did not pass readiness')
  if digest(config)!=expected_config or ledger()!=before_ledger:raise RuntimeError('Configuration or migration ledger unexpectedly changed')
 except Exception:
  replace(backup);run([pm2,'restart',name])
  (release/'rollback.json').write_text(json.dumps({'binary_restored':digest(exe)==expected_binary}))
  raise
 receipt={'source_commit':sha,'profile':'device-safety','baseline':baseline,'binary_sha256':digest(exe),'config_unchanged':True,'migration_ledger_unchanged':True,'cross_tenant_attribute_read':'denied','previous_binary':str(backup)}
 (release/'deployment.json').write_text(json.dumps(receipt));print(json.dumps(receipt))
