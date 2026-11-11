"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)

row=query('ThingsPanel',"SELECT row_to_json(d) FROM (SELECT id,tenant_id,name,is_enabled,voucher FROM devices WHERE id='"+device+"')d")
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

started=datetime.datetime.now(datetime.timezone.utc).isoformat()
events=[]
sock=socket.create_connection(('127.0.0.1',1883),timeout=10)
try:
 client='phase-acceptance-'+uuid.uuid4().hex[:12]
 body=encoded('MQTT')+bytes([4,0xc2])+struct.pack('!H',30)+encoded(client)+encoded(voucher['username'])+encoded(voucher['password'])
 sock.sendall(frame(0x10,body))
 kind,body=read_frame(sock)
 if kind!=0x20 or body!=b'\x00\x00':raise RuntimeError('Broker did not authorize the existing test device')
 payload={'qa_probe_20261111':23.11}
 sock.sendall(frame(0x32,encoded('devices/telemetry')+struct.pack('!H',1)+json.dumps(payload).encode()))
 kind,ack=read_frame(sock)
 if kind!=0x40 or ack!=struct.pack('!H',1):raise RuntimeError('MQTT publish was not acknowledged')
 events.append({'field':'qa_probe_20261111','value':23.11,'qos1_ack':True})
 time.sleep(4)
finally:
 try:sock.sendall(b'\xe0\x00')
 except Exception:pass
 sock.close()
print(json.dumps({'device_id':device,'mqtt':events,'scope':'new-device MQTT first report; storage and UI verified separately'}))
