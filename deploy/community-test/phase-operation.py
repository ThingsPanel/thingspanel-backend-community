"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='2b20f814-1e21-e93a-cc6a-03a142ece68a'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)

row=query('ThingsPanel',"SELECT row_to_json(d) FROM (SELECT id,tenant_id,name,is_enabled,voucher FROM devices WHERE id='"+device+"')d")
if row['tenant_id']!=tenant or row['name']!='新版测试温度传感器' or row['is_enabled']!='enabled':raise RuntimeError('Test device identity changed')
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
 for i,(temperature,signal) in enumerate([(20,0),(38.6111,1),(20,0)],1):
  payload={'temperature':temperature,'phase_upgrade_20261111':signal}
  sock.sendall(frame(0x32,encoded('devices/telemetry')+struct.pack('!H',i)+json.dumps(payload).encode()))
  kind,ack=read_frame(sock)
  if kind!=0x40 or ack!=struct.pack('!H',i):raise RuntimeError('MQTT publish was not acknowledged')
  events.append({'temperature':temperature,'phase_signal':signal,'qos1_ack':True})
  time.sleep(10)
finally:
 try:sock.sendall(b'\xe0\x00')
 except Exception:pass
 sock.close()
alarms=query('automation_main_20261028',"SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,policy_id,subject_id,state,opened_at,cleared_at FROM alarm_episodes WHERE tenant_id='"+tenant+"' AND subject_id='"+device+"' AND opened_at>='"+started+"' ORDER BY opened_at)t")
event_ids=query('automation_main_20261028',"SELECT coalesce(json_agg(id),'[]'::json) FROM auto_outbox WHERE tenant_id='"+tenant+"' AND kind='notification' AND scope_key IN (SELECT 'alarm:'||id FROM alarm_episodes WHERE tenant_id='"+tenant+"' AND subject_id='"+device+"' AND opened_at>='"+started+"')")
for _ in range(15):
 deliveries=query('notification_main_20261028',"SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT r.id AS request_id,r.source_type,r.source_id,r.intake_status,r.blocked_reason,d.id AS delivery_id,d.dispatch_status,d.delivery_status,d.attempt_count,(d.provider_message_id IS NOT NULL) AS provider_receipt,d.error_code FROM notification_requests r LEFT JOIN notification_deliveries d ON d.tenant_id=r.tenant_id AND d.request_id=r.id WHERE r.tenant_id='"+tenant+"' AND r.created_at>='"+started+"' ORDER BY r.created_at)t")
 # The ingress source identity is the durable automation outbox event ID.
 deliveries=[d for d in deliveries if d['source_id'] in event_ids or d['source_id'] in {a['id'] for a in alarms}]
 if deliveries and all(d['dispatch_status'] not in ('queued','sending') for d in deliveries):break
 time.sleep(2)
result={'started_at':started,'device_id':device,'mqtt':events,'alarms':alarms,'notifications':deliveries,'phone_arrival_verified':False}
print(json.dumps(result))
if not alarms or not deliveries or not any(d['dispatch_status']=='accepted' for d in deliveries):raise SystemExit(1)
