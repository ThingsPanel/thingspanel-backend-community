import json,subprocess,socket,yaml,pathlib
q="""SELECT json_build_object(
'tenant','63a4b529',
'registered',count(*),
'marked_active',count(*) FILTER(WHERE d.active),
'active_unexpired',count(*) FILTER(WHERE d.active AND d.expires_at>now()),
'eligible_before_session_check',count(*) FILTER(WHERE d.active AND d.expires_at>now() AND u.status='N' AND u.auth_version=d.auth_version),
'latest_binding',max(d.updated_at),'latest_expiry',max(d.expires_at))
FROM app_push_devices d LEFT JOIN users u ON u.id=d.user_id AND u.tenant_id=d.tenant_id
WHERE d.tenant_id='63a4b529'"""
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel'],input=('BEGIN READ ONLY;'+q+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
if p.returncode:raise RuntimeError('Read-only binding inspection failed')
out=json.loads(p.stdout)
q="SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT d.session_hash,u.email='test@test.cn' AS selected_member FROM app_push_devices d JOIN users u ON u.id=d.user_id AND u.tenant_id=d.tenant_id WHERE d.tenant_id='63a4b529' AND d.active AND d.expires_at>now() AND u.status='N' AND u.auth_version=d.auth_version)t"
p=subprocess.run(['docker','exec','timescaledb','psql','-X','-q','-At','-U','postgres','-d','ThingsPanel','-c',q],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
if p.returncode:raise RuntimeError('Binding inspection failed')
rows=json.loads(p.stdout)
cfg=yaml.safe_load(pathlib.Path('/home/thingspanel/thingspanel-backend-community/configs/conf.yml').read_text())['db']['redis']
host,port=cfg['addr'].rsplit(':',1)
s=socket.create_connection((host,int(port)),timeout=10);f=s.makefile('rb')
def cmd(*args):
 b=[str(a).encode() for a in args];s.sendall(('*%d\r\n'%len(b)).encode()+b''.join(('$%d\r\n'%len(x)).encode()+x+b'\r\n' for x in b))
 line=f.readline();kind=line[:1];value=line[1:-2]
 if kind==b'-':raise RuntimeError('Private Redis lookup rejected')
 if kind==b'$':
  n=int(value)
  if n<0:return None
  v=f.read(n);f.read(2);return v.decode()
 if kind==b':':return int(value)
 return value.decode()
if cfg.get('password'):cmd('AUTH',cfg['password'])
cmd('SELECT',cfg.get('db',0))
out['candidate_sessions']=[{'selected_test_member':r['selected_member'],'live':cmd('GET',r['session_hash'])=='1','ttl_seconds':cmd('TTL',r['session_hash'])} for r in rows]
s.close()
print(json.dumps(out))
