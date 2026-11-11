"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)

row=query('ThingsPanel',"SELECT row_to_json(d) FROM (SELECT id,tenant_id,name,is_enabled,voucher,device_number FROM devices WHERE id='"+device+"')d")
if row['tenant_id']!=tenant or row['name']!='QA-FULL-20261111-MQTT' or row['is_enabled']!='enabled':raise RuntimeError('Test device identity changed')
voucher=json.loads(row['voucher']) if isinstance(row['voucher'],str) else row['voucher']
if not isinstance(voucher,dict) or 'username' not in voucher or 'password' not in voucher:raise RuntimeError('Unexpected test voucher mode')

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


import urllib.request,urllib.error
qa=json.loads(pathlib.Path('/opt/tp-community-main-20261028/qa-tenant.private.json').read_text())
def http(path,payload=None,token=None):
 h={'Content-Type':'application/json'}
 if token:h['x-token']=token
 req=urllib.request.Request('http://127.0.0.1:9999'+path,headers=h,data=json.dumps(payload).encode() if payload is not None else None)
 try:
  with urllib.request.urlopen(req,timeout=15) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:
  try:return e.code,json.load(e)
  except ValueError:return e.code,{}
status,login=http('/api/v1/login',{'email':qa['email'],'password':qa['password']})
token=(login.get('data') or {}).get('token')
if status!=200 or not token:raise RuntimeError('QA tenant login failed')
import base64
claims=json.loads(base64.urlsafe_b64decode(token.split('.')[1]+'==='))
print(json.dumps({'qa_email_matches':qa['email']=='test@test.cn','claims_tenant':claims.get('tenant_id'), 'claim_keys':sorted(claims)}))
raise SystemExit(0)
results=[];received=[];seq=10
sock=socket.create_connection(('127.0.0.1',1883),timeout=10)
def consume(deadline):
 while time.time()<deadline:
  sock.settimeout(max(0.1,deadline-time.time()))
  try:kind,body=read_frame(sock)
  except socket.timeout:return
  if kind>>4==3:
   n=struct.unpack('!H',body[:2])[0];topic=body[2:2+n].decode();offset=2+n
   qos=(kind>>1)&3
   if qos:
    packet=body[offset:offset+2];offset+=2
    sock.sendall(frame(0x40,packet))
   try:payload=json.loads(body[offset:])
   except ValueError:payload={'invalid_json':True}
   received.append({'topic':topic,'payload':payload})
   if topic.startswith('devices/attributes/set/'+row['device_number']+'/') or topic.startswith('devices/command/'+row['device_number']+'/'):
    message_id=topic.split('/')[-1]
    response={'result':0,'message':'QA simulated device accepted','ts':int(time.time())}
    if 'method' in payload:response['method']=payload['method']
    response_topic=('devices/attributes/set/response/' if '/attributes/' in topic else 'devices/command/response/')+message_id
    sock.sendall(frame(0x30,encoded(response_topic)+json.dumps(response).encode()))
  elif kind not in (0x40,0x90,0xd0):results.append({'unexpected_packet':kind})
def publish(topic,payload):
 global seq
 seq+=1;sock.sendall(frame(0x32,encoded(topic)+struct.pack('!H',seq)+json.dumps(payload).encode()))
 consume(time.time()+2)
try:
 body=encoded('MQTT')+bytes([4,0xc2])+struct.pack('!H',60)+encoded('qa-functional-'+uuid.uuid4().hex[:10])+encoded(voucher['username'])+encoded(voucher['password'])
 sock.sendall(frame(0x10,body));kind,body=read_frame(sock)
 if kind!=0x20 or body!=b'\x00\x00':raise RuntimeError('QA MQTT authorization failed')
 topics=['devices/telemetry/control/'+row['device_number'],'devices/attributes/set/'+row['device_number']+'/+','devices/command/'+row['device_number']+'/+']
 sock.sendall(frame(0x82,struct.pack('!H',1)+b''.join(encoded(t)+b'\x01' for t in topics)))
 kind,ack=read_frame(sock)
 if kind!=0x90 or any(c==128 for c in ack[2:]):raise RuntimeError('QA MQTT subscription failed')
 publish('devices/attributes/qa-20261112',{'qa_firmware':'20261112.1','qa_enabled':True})
 publish('devices/event/qa-20261112',{'method':'qa_self_check','params':{'result':'ok','run':'20261112'}})
 for label,path,payload in [
 ('telemetry-control','/api/v1/telemetry/datas/pub',{'device_id':device,'value':json.dumps({'qa_control':12})}),
 ('attribute-set','/api/v1/attribute/datas/pub',{'device_id':device,'value':json.dumps({'qa_mode':'test'})}),
 ('command','/api/v1/command/datas/pub',{'device_id':device,'identify':'qa_self_check','value':json.dumps({'run':'20261112'})})]:
  status,res=http(path,payload,token);results.append({'operation':label,'http_status':status,'code':res.get('code'),'message':res.get('message'),'data':res.get('data')})
  consume(time.time()+3)
 time.sleep(2)
 status,res=http('/api/v1/attribute/datas/'+device,token=token)
 results.append({'operation':'read-attributes','http_status':status,'response':res})
finally:
 try:sock.sendall(b'\xe0\x00')
 except Exception:pass
 sock.close()
print(json.dumps({'device_id':device,'operations':results,'device_received':received,'scope':'new QA MQTT device only; simulated device response, not physical hardware'}))
