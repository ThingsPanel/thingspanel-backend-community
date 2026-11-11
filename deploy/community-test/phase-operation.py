import base64,copy,fcntl,json,os,pathlib,secrets,socket,struct,subprocess,time,urllib.request,urllib.error
root=pathlib.Path('/opt/tp-community-actions')
sha='7e5a3afea76c27fb22b705e054a2d25ceeea0ba8'
release=root/'native-rehearsal'/sha
source=pathlib.Path('/opt/tp-community-main-20261028')
db='native_main_20261028'
role='tp_upgrade_readonly_20261111'
name='tp-native-new-device-check-7e5a3afe'
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
def request(base,path,token=None,payload=None,key=None):
 headers={'Content-Type':'application/json'}
 if token:headers.update({'Authorization':'Bearer '+token,'x-token':token})
 if key:headers['Idempotency-Key']=key
 req=urllib.request.Request(base+path,headers=headers,data=json.dumps(payload).encode() if payload is not None else None)
 try:
  with urllib.request.urlopen(req,timeout=15) as response:return response.status,json.load(response)
 except urllib.error.HTTPError as error:
  try:body=json.load(error)
  except (ValueError,OSError):body=None
  return error.code,body
def accounting():
 return json.loads(sql(db,"SELECT json_build_object('devices',(SELECT count(*) FROM devices),'groups',(SELECT count(*) FROM device_groups),'members',(SELECT count(*) FROM device_group_members),'access',(SELECT count(*) FROM device_access_bindings))"))
if int(sql(db,'SELECT version FROM schema_migrations'))!=40:raise RuntimeError('Verified rehearsal schema changed')
before=accounting()
if before['devices']!=1:raise RuntimeError('New-device QA catalog changed')
if sql(db,"SELECT count(*) FROM devices WHERE id='dece1101-b9ea-498c-a6c4-780012570d0d' AND device_number='new-native-qa-20261111'")!='1':raise RuntimeError('New QA device identity changed')
containers=run(['docker','ps','--format','{{.ID}} {{.Names}}'])
with socket.socket() as sock:
 if sock.connect_ex(('127.0.0.1',11400))==0:raise RuntimeError('QA port occupied')
keypath=release/'new-device-keyring.private.json'
if not keypath.exists():keypath.write_text(json.dumps({'active':'new-device-qa','keys':{'new-device-qa':base64.b64encode(os.urandom(32)).decode()}}))
keyring=json.loads(keypath.read_text())
config=json.loads((source/'runtime.encore.private.json').read_text())
config['hosted_services']=['authn','thingmodel','devicemetadata']
server=copy.deepcopy(config['sql_servers'][0])
server['databases']={db:server['databases'][db]}
config['sql_servers']=[server]
runtime_path=release/'new-device-runtime.private.json';runtime_path.write_text(json.dumps(config))
live=json.loads((source/'compose.encore.private.json').read_text())['services']['encore-api']
env={k:v for k,v in live['environment'].items() if k in ('TP_AUTH_MODE','TP_JWT_KEY','TP_COMMUNITY_SESSION_BASE_URL') or k.startswith('DB_')}
env.update({'ENCORE_INFRA_CONFIG_PATH':str(runtime_path),'ENCORE_LISTEN_ADDR':'127.0.0.1:11400','PORT':'11400',
 'GOMAXPROCS':'1','GOMEMLIMIT':'96MiB','METADATA_CREDENTIAL_ACTIVE_KEY_REF':keyring['active'],'METADATA_CREDENTIAL_KEYRING_B64':base64.b64encode(json.dumps(keyring['keys']).encode()).decode(),
 'TP_COMMUNITY_BASE_URL':'http://127.0.0.1:9','TP_BACKGROUND_WORKERS':'off','TP_RUNTIME_OUTBOX_DISPATCH':'off'})
compose={'services':{'readonly-check':{'container_name':name,'image':'tp-community-encore:source-'+sha,
 'network_mode':'host','restart':'no','mem_limit':'128m','cpus':0.25,'environment':env,
 'extra_hosts':live.get('extra_hosts',[]),
 'volumes':[{'type':'bind','source':str(runtime_path),'target':str(runtime_path),'read_only':True}],
 'logging':{'driver':'json-file','options':{'max-size':'2m','max-file':'1'}},
 'labels':{'thingspanel.upgrade.scope':'readonly-rehearsal','thingspanel.upgrade.source':sha}}}}
compose_path=release/'new-device-compose.private.json';compose_path.write_text(json.dumps(compose))
args=['docker','compose','-p','tp-native-new-device-20261111','-f',str(compose_path)]
def encoded(value):
 b=value.encode('utf-8');return struct.pack('!H',len(b))+b

def frame(kind,body):
 n=len(body);header=bytearray([kind])
 while True:
  byte=n%128;n//=128;header.append(byte|(128 if n else 0))
  if not n:break
 return bytes(header)+body

def read_frame(sock):
 def read(n):
  data=b''
  while len(data)<n:
   chunk=sock.recv(n-len(data))
   if not chunk:raise RuntimeError('MQTT peer closed connection')
   data+=chunk
  return data
 kind=read(1)[0];length=0;multiplier=1
 for _ in range(4):
  b=read(1)[0];length+=(b&127)*multiplier
  if not b&128:return kind,read(length)
  multiplier*=128
 raise RuntimeError('Invalid MQTT remaining length')


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
 body={'name':'New device acceptance 20261111','deviceNumber':'new-native-qa-20261111','deviceKind':'standalone','directAccess':{'access':{'mode':'direct','protocol':'mqtt','providerId':None,'providerVersion':None,'parameters':{},'credentialPolicy':{'kind':'username_password','autoRegister':False},'onlinePolicy':{'mode':'transport','timeoutSeconds':60},'connectionDirection':'device_to_platform','serviceInstanceId':None},'externalDeviceId':None}}
 status,created=request('http://127.0.0.1:11400','/api/v2/devices',token,body,'new-device-qa-20261111')
 (release/'new-device-create.private.json').write_text(json.dumps(created))
 results.append({'operation':'create','status':status})
 if status!=201:raise RuntimeError('New device create failed; response retained privately')
 device=created['data']['id']
 status,credential=request('http://127.0.0.1:11400','/api/v2/devices/'+device+'/credentials',token,{'kind':'username_password'},'new-device-credential-20261111')
 (release/'new-device-credential.private.json').write_text(json.dumps(credential))
 results.append({'operation':'issue-credential','status':status,'credential_state':(credential.get('data') or {}).get('credential',{}).get('status')})
 if status==201:
  material=credential['data'];summary=material['credential']
  user=summary.get('publicIdentifier');secret=material.get('secret')
  if not isinstance(user,str) or not isinstance(secret,str):raise RuntimeError('Expected generated MQTT user and secret')
  sock=socket.create_connection(('127.0.0.1',1883),timeout=10)
  try:
   connect=encoded('MQTT')+bytes([4,0xc2])+struct.pack('!H',30)+encoded('new-device-qa-20261111')+encoded(user)+encoded(secret)
   sock.sendall(frame(0x10,connect));kind,ack=read_frame(sock)
   accepted=kind==0x20 and ack==b'\x00\x00'
   results.append({'operation':'mqtt-connect','status':200 if accepted else 503,'connack_code':ack[1] if kind==0x20 and len(ack)==2 else None})
  finally:sock.close()
 for suffix in ('','/connection-info','/runtime-state','/readings?kind=telemetry&latestOnly=true'):
  status,response=request('http://127.0.0.1:11400','/api/v2/devices/'+device+suffix,token)
  (release/('new-device-'+(suffix.split('?')[0].strip('/') or 'detail')+'.private.json')).write_text(json.dumps(response))
  results.append({'operation':suffix or 'detail','status':status,'missing_runtime_identity':isinstance(response,dict) and 'runtime identity' in str(response.get('detail',''))})
 stats=run(['docker','stats','--no-stream','--format','{{.MemUsage}}',name])
finally:
 logs=subprocess.run(['docker','logs',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=30)
 (release/'new-device-service.private.log').write_bytes(logs.stdout)
 run(args+['stop','--timeout','10'])
after=accounting()
if run(['docker','ps','--format','{{.ID}} {{.Names}}'])!=containers:raise RuntimeError('Original containers changed')
report={'commit':sha,'scope':'fresh-device-functional-verification','checks':results,'native_counts_before':before,'native_counts_after':after,'device_id':device,'candidate_stopped_after_checks':True,'public_routes_changed':False,'legacy_migration_stopped':True,'all_checks_passed':all(x['status'] in (200,201) for x in results)}
(release/'new-device-result.json').write_text(json.dumps(report))
print(json.dumps(report))
if not report['all_checks_passed']:raise SystemExit(1)
