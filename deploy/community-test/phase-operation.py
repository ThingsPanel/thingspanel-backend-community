import copy,fcntl,json,os,pathlib,secrets,socket,subprocess,time,urllib.request,urllib.error
root=pathlib.Path('/opt/tp-community-actions')
sha='03cff017df1c13436b484d576df5bead0ef81a46'
release=root/'native-rehearsal'/sha
source=pathlib.Path('/opt/tp-community-main-20261028')
db='tp_upgrade_rehearsal_20261111165830'
role='tp_upgrade_readonly_20261111'
name='tp-native-readonly-check-03cff017'
os.umask(0o077)
lock=(root/'deployment.lock').open('a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
def run(args,data=None):
 p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
 if p.returncode:
  (release/'readonly-probe-failure.private.log').write_bytes(p.stderr)
  raise RuntimeError('Read-only candidate probe failed; private log retained')
 return p.stdout.decode().strip()
def sql(database,query):
 return run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],query.encode())
def request(base,path,token=None,payload=None):
 headers={'Content-Type':'application/json'}
 if token:headers.update({'Authorization':'Bearer '+token,'x-token':token})
 req=urllib.request.Request(base+path,headers=headers,data=json.dumps(payload).encode() if payload is not None else None)
 try:
  with urllib.request.urlopen(req,timeout=15) as response:return response.status,json.load(response)
 except urllib.error.HTTPError as error:return error.code,None
def accounting():
 return json.loads(sql(db,"SELECT json_build_object('devices',(SELECT count(*) FROM devices),'groups',(SELECT count(*) FROM device_groups),'members',(SELECT count(*) FROM device_group_members),'access',(SELECT count(*) FROM device_access_bindings))"))
if int(sql(db,'SELECT version FROM schema_migrations'))!=40:raise RuntimeError('Verified rehearsal schema changed')
before=accounting()
if before['devices']!=48511 or before['groups']!=67:raise RuntimeError('Verified rehearsal identity count changed')
containers=run(['docker','ps','--format','{{.ID}} {{.Names}}'])
with socket.socket() as sock:
 if sock.connect_ex(('127.0.0.1',11400))==0:raise RuntimeError('Rehearsal port is occupied')
credential=release/'readonly-db.private.json'
if credential.exists():
 private=json.loads(credential.read_text())
 if private['role']!=role or private['database']!=db:raise RuntimeError('Rehearsal credential identity mismatch')
else:
 if sql('postgres',"SELECT count(*) FROM pg_roles WHERE rolname='"+role+"'")!='0':raise RuntimeError('Existing role is not owned by this rehearsal')
 private={'role':role,'database':db,'password':secrets.token_hex(32)}
 credential.write_text(json.dumps(private))
if sql('postgres',"SELECT count(*) FROM pg_roles WHERE rolname='"+role+"'")=='0':
 sql('postgres','CREATE ROLE '+role+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD '"+private['password']+"'; ALTER ROLE "+role+" SET default_transaction_read_only=on; ALTER ROLE "+role+" SET statement_timeout='15s';")
sql(db,'GRANT CONNECT ON DATABASE '+db+' TO '+role+'; GRANT USAGE ON SCHEMA public TO '+role+'; GRANT SELECT ON ALL TABLES IN SCHEMA public TO '+role+';')
config=json.loads((source/'runtime.encore.private.json').read_text())
config['hosted_services']=['authn','thingmodel','devicemetadata']
server=copy.deepcopy(config['sql_servers'][0])
server['databases']={db:{'name':'device_metadata','username':role,'password':{'$env':'DB_DEVICE_METADATA_PASSWORD'}}}
config['sql_servers']=[server]
runtime_path=release/'readonly-runtime.private.json';runtime_path.write_text(json.dumps(config))
live=json.loads((source/'compose.encore.private.json').read_text())['services']['encore-api']
env={k:v for k,v in live['environment'].items() if k in ('TP_AUTH_MODE','TP_JWT_KEY','TP_COMMUNITY_SESSION_BASE_URL')}
env.update({'ENCORE_INFRA_CONFIG_PATH':str(runtime_path),'ENCORE_LISTEN_ADDR':'127.0.0.1:11400','PORT':'11400',
 'GOMAXPROCS':'1','GOMEMLIMIT':'96MiB','DB_DEVICE_METADATA_PASSWORD':private['password'],
 'TP_COMMUNITY_BASE_URL':'http://127.0.0.1:9','TP_BACKGROUND_WORKERS':'off','TP_RUNTIME_OUTBOX_DISPATCH':'off'})
compose={'services':{'readonly-check':{'container_name':name,'image':'tp-community-encore:source-'+sha,
 'network_mode':'host','restart':'no','mem_limit':'128m','cpus':0.25,'environment':env,
 'extra_hosts':live.get('extra_hosts',[]),
 'volumes':[{'type':'bind','source':str(runtime_path),'target':str(runtime_path),'read_only':True}],
 'logging':{'driver':'json-file','options':{'max-size':'2m','max-file':'1'}},
 'labels':{'thingspanel.upgrade.scope':'readonly-rehearsal','thingspanel.upgrade.source':sha}}}}
compose_path=release/'readonly-compose.private.json';compose_path.write_text(json.dumps(compose))
args=['docker','compose','-p','tp-native-readonly-20261111','-f',str(compose_path)]
results=[]
try:
 run(args+['up','-d','--no-build'])
 for _ in range(25):
  try:
   status,body=request('http://127.0.0.1:11400','/api/device-metadata/healthz')
   if status==200 and body.get('status')=='ok':break
  except (OSError,ValueError):pass
  time.sleep(1)
 else:raise RuntimeError('Read-only candidate health check did not become ready')
 status,_=request('http://127.0.0.1:11400','/api/v2/device-groups')
 if status!=401:raise RuntimeError('Read-only candidate accepted unauthenticated request')
 qa=json.loads((source/'qa-tenant.private.json').read_text())
 status,login=request('http://127.0.0.1:9999','/api/v1/login',payload={'email':qa['email'],'password':qa['password']})
 token=(login.get('data') or {}).get('token') if login else None
 if not token:raise RuntimeError('Acceptance tenant login failed')
 for path in ['/api/v2/device-groups','/api/v2/devices?page=1&pageSize=5','/api/v2/thing-models']:
  status,body=request('http://127.0.0.1:11400',path,token)
  results.append({'path':path,'status':status,'data_type':type(body.get('data')).__name__ if isinstance(body,dict) else None})
  if status!=200:raise RuntimeError('Read-only candidate request failed: '+path+' status='+str(status))
 stats=run(['docker','stats','--no-stream','--format','{{.MemUsage}}',name])
finally:
 logs=subprocess.run(['docker','logs',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=30)
 (release/'readonly-service.private.log').write_bytes(logs.stdout)
 run(args+['stop','--timeout','10'])
after=accounting()
if before!=after or run(['docker','ps','--format','{{.ID}} {{.Names}}'])!=containers:raise RuntimeError('Read-only rehearsal changed original state')
print(json.dumps({'commit':sha,'read_only_role':True,'database':db,'checks':results,'container_memory':stats,
 'rehearsal_counts_unchanged':after,'original_running_containers_unchanged':True,'candidate_stopped_after_checks':True,
 'full_device_migration_complete':False}))
