"""Replace only the already hosted automation/API container; preserve all owners and data."""
import fcntl,hashlib,json,os,pathlib,shutil,subprocess,sys,time,urllib.request
sys.path.insert(0,'/opt/tp-community-actions/incoming')
from deploy_control_plane import unpack,verify_files,validate_runtime,DATABASES
from encore_migrations import pending_migrations,source_migrations
sha='ece5c7ef870acd54b712123831d615ed6e3858c2'
expected='54563749439c45bb340d32ba573cd42f059063b4'
root=pathlib.Path('/opt/tp-community-actions')
runtime=pathlib.Path('/opt/tp-community-main-20261028')
os.umask(0o077)
def run(args,**kw):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=240,**kw)
 if p.returncode:raise RuntimeError('Operation failed: '+args[0])
 return p.stdout
def sql(database,statement):
 raw=run(['docker','exec','-i','timescaledb','psql','-X','-qAt','-U','postgres','-d',database,'-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;\n'+statement+';\nCOMMIT;\n').encode())
 return json.loads(raw)
def get(path,token=None):
 headers={'Host':'c.thingspanel.cn','Cache-Control':'no-cache'}
 if token:headers.update({'x-token':token,'Authorization':'Bearer '+token})
 return urllib.request.urlopen(urllib.request.Request('http://127.0.0.1'+path,headers=headers),timeout=15).read()
def ledger():
 return {database:sql(database,"SELECT json_build_object('schema',(SELECT json_agg(t ORDER BY version) FROM schema_migrations t),'ledger',(SELECT json_agg(t ORDER BY version) FROM deployment_schema_ledger t))") for database in DATABASES}
with (root/'deployment.lock').open('a') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX)
 if shutil.disk_usage(str(root)).free<1024**3:raise RuntimeError('Insufficient rollback capacity')
 compose=runtime/'compose.encore.private.json'
 original=compose.read_bytes();config=json.loads(original)
 old_image='tp-community-control-plane:source-'+expected
 if config['services']['encore-api']['image']!=old_image:raise RuntimeError('API image changed')
 configs={name:json.loads((runtime/filename).read_text()) for name,filename in [('encore-api','runtime.encore.private.json'),('notification-core','runtime.notification.private.json')]}
 validate_runtime(configs)
 release=root/'automation-fix-releases'/sha
 if release.exists():raise RuntimeError('Release exists; inspect before retry')
 release.mkdir(parents=True)
 bundle=release/'bundle';bundle.mkdir()
 unpack(root/'incoming'/('control-plane-backend-'+sha+'.tar.gz'),bundle)
 manifest=json.loads((bundle/'manifest.json').read_text())
 if manifest['commit']!=sha or manifest['profile']!='control-plane':raise RuntimeError('Candidate mismatch')
 verify_files(bundle,manifest)
 before=ledger()
 for database,(_,folder) in DATABASES.items():
  state=before[database]
  if pending_migrations(source_migrations(bundle/'encore-api'/folder/'migrations'),state['ledger'],state['schema']):raise RuntimeError('Schema changes forbidden in this patch')
 (release/'ledger.before.json').write_text(json.dumps(before,sort_keys=True))
 (release/'compose.before.private.json').write_bytes(original)
 backup=release/'automation.before.private.dump'
 with backup.open('wb') as output:
  p=subprocess.run(['docker','exec','timescaledb','pg_dump','-U','postgres','-Fc','automation_main_20261028'],stdout=output,stderr=subprocess.PIPE,timeout=180)
 if p.returncode or backup.stat().st_size==0:raise RuntimeError('Backup failed')
 with backup.open('rb') as stream:run(['docker','exec','-i','timescaledb','pg_restore','--list'],stdin=stream)
 version=get('/api/v1/sys_version')
 frontend=get('/SOURCE_COMMIT')
 run(['docker','load','-i',str(bundle/'image.tar.gz')])
 image='tp-community-control-plane:source-'+sha
 if json.loads(run(['docker','image','inspect',image]))[0]['Id']!=manifest['image_id']:raise RuntimeError('Image checksum mismatch')
 config['services']['encore-api']['image']=image
 args=['docker','compose','-f',str(compose)]
 def write_config(value):
  tmp=compose.with_name('compose.wait-next.private.json');tmp.write_bytes(value);os.replace(str(tmp),str(compose))
 def verify():
  qa=json.loads((runtime/'qa-tenant.private.json').read_text())
  req=urllib.request.Request('http://127.0.0.1:9999/api/v1/login',data=json.dumps({'email':qa['email'],'password':qa['password']}).encode(),headers={'Content-Type':'application/json'})
  token=json.load(urllib.request.urlopen(req,timeout=15)).get('data',{}).get('token')
  if not token:raise RuntimeError('Readiness login failed')
  for path in ['/api/v2/thing-models','/automation-core/v1/rules?limit=1','/automation-core/v1/alarms?limit=1','/api/v2/notification-groups']:
   value=json.loads(get(path,token))
   if value.get('code',200) not in [0,200]:raise RuntimeError('Readiness API failed')
 try:
  write_config(json.dumps(config,indent=2).encode())
  run(args+['up','-d','--no-deps','encore-api'])
  for attempt in range(30):
   try:
    cid=run(args+['ps','-q','encore-api']).decode().strip()
    instance=json.loads(run(['docker','inspect',cid]))[0]
    if instance['Image']!=manifest['image_id'] or not instance['State']['Running']:raise RuntimeError('Image not running')
    verify();break
   except Exception:
    if attempt==29:raise
    time.sleep(2)
  if get('/api/v1/sys_version')!=version or get('/SOURCE_COMMIT')!=frontend:raise RuntimeError('Other owner changed')
  if ledger()!=before:raise RuntimeError('Migration ledger changed')
 except Exception:
  write_config(original);run(args+['up','-d','--no-deps','encore-api'])
  (release/'rollback.json').write_text(json.dumps({'restored_image':old_image}))
  raise
 result={'commit':sha,'previous':expected,'service':'encore-api','image_id':manifest['image_id'],'migration_ledger_unchanged':True,'database_migrations':0,'gin_unchanged':True,'frontend_unchanged':True,'notification_image_unchanged':True,'backup_bytes':backup.stat().st_size}
 (release/'deployment.json').write_text(json.dumps(result));print(json.dumps(result))
