"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)



out={'null_tenant_devices':query('ThingsPanel',"SELECT count(*) FROM devices WHERE tenant_id IS NULL OR trim(tenant_id)=''")}
for name in ['tp-community-main-47-broker-mosquitto-1','tp-community-main-47-broker-owner-1']:
 p=subprocess.run(['docker','logs','--since','20m','--tail','300',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
 lines=p.stdout.decode(errors='replace').splitlines()
 selected=[]
 for line in lines:
  if any(x in line.lower() for x in ['denied','invalid','error','reject','failed','d18556d6','5ed19dd2']):
   if any(x in line.lower() for x in ['password','token','secret','credential','authorization']):continue
   selected.append(line[:700])
 out[name]=selected[-30:]
print(json.dumps(out))
