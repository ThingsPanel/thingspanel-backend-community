"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)


print(json.dumps({
'commands':query('ThingsPanel',"SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,state,last_error,route_cell,route_epoch,dispatched_at,completed_at,convert_from(payload,'UTF8')::jsonb->'message' AS message FROM broker_command_requests WHERE device_id='"+device+"' ORDER BY created_at DESC LIMIT 5)t"),
'bindings':query('ThingsPanel',"SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT device_id,tenant_id,broker_cell,route_epoch FROM broker_transport_bindings WHERE device_id='"+device+"')t"),
'containers':subprocess.check_output(['docker','ps','--format','{{.Names}} {{.Image}} {{.Status}}']).decode()
}))
