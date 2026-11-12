"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)


out={}
for device in ['5ed19dd2-4422-dfe3-2a54-13e180f120ed','eac72acb-63a5-f582-906f-88d828abb64e','373fb5d9-a73a-e948-dcb5-5ab07aed6dec']:
 out[device]={'commands':query('ThingsPanel',"SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,state,last_error,route_cell,route_epoch,dispatched_at,completed_at FROM broker_command_requests WHERE device_id='"+device+"' ORDER BY created_at DESC LIMIT 3)t"),'device':query('ThingsPanel',"SELECT row_to_json(t) FROM (SELECT id,tenant_id,is_enabled,activate_flag FROM devices WHERE id='"+device+"')t")}
for name in ['tp-community-main-47-broker-mosquitto-1','tp-community-main-47-broker-owner-1']:
 p=subprocess.run(['docker','logs','--since','10m','--tail','300',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=15)
 lines=p.stdout.decode(errors='replace').splitlines();out[name]=[l[:600] for l in lines if any(w in l.lower() for w in ['error','fail','denied','author','qa-q04']) and not any(w in l.lower() for w in ['password','token','secret','credential','authorization'])][-45:]
print(json.dumps(out))
