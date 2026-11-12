"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)



import base64,urllib.request,urllib.error
out={}
ids=['373fb5d9-a73a-e948-dcb5-5ab07aed6dec','40fd7d06-6f29-6903-9dc0-4e736357c34f']
quoted=','.join("'"+x+"'" for x in ids)
root=pathlib.Path('/opt/tp-community-main-20261028')
services=json.loads((root/'active-broker-services.private.json').read_text());app=next(x for x in services if x.get('target_tenant_scope')=='platform')
# Authentication probe creates only ordinary service client identity evidence;
# it publishes no command and does not modify any credential or scope.
binding=next(x for x in json.loads((root/'active-broker-tokens.private.json').read_text()) if x['broker_cell']==app['broker_cell'] and x['broker_instance']==app['broker_instance'])

def enc(v):
 b=v.encode();return struct.pack('!H',len(b))+b
def frame(t,b):
 n=len(b);r=b''
 while True:
  v=n%128;n//=128;r+=bytes([v|(128 if n else 0)])
  if not n:break
 return bytes([t])+r+b
out={'at':datetime.datetime.now().isoformat(),'probes':[]}
for target in ids+['eac72acb-63a5-f582-906f-88d828abb64e']:
 client=app['allowed_client_ids'][0]+'--target-v1--'+base64.urlsafe_b64encode(target.encode()).decode().rstrip('=')
 start=time.monotonic()
 try:
  with socket.create_connection(('127.0.0.1',1883),timeout=8) as sock:
   sock.sendall(frame(16,enc('MQTT')+bytes([4,194])+struct.pack('!H',30)+enc(client)+enc(app['username'])+enc(app['password'])))
   response=sock.recv(4);sock.sendall(bytes([224,0]))
  out['probes'].append({'target':target,'connack_hex':response.hex(),'elapsed_ms':round((time.monotonic()-start)*1000,1)})
 except Exception as e:out['probes'].append({'target':target,'error':type(e).__name__})
print(json.dumps(out))
