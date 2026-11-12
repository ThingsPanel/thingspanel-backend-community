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
out['target_records']=query('ThingsPanel',"SELECT json_agg(t) FROM (SELECT d.id,d.tenant_id,d.device_number,(SELECT count(*) FROM devices dx WHERE dx.device_number=d.device_number) AS device_number_count,d.is_enabled,d.activate_flag,d.parent_id,d.device_config_id,d.service_access_id,d.access_way,c.device_type,c.protocol_type,c.tenant_id AS config_tenant_id,(SELECT count(*) FROM device_topic_mappings m WHERE CAST(m.device_config_id AS text)=CAST(d.device_config_id AS text) AND m.enabled=TRUE) AS active_mappings FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id AND c.tenant_id=d.tenant_id WHERE d.id IN ("+quoted+"))t")
out['plugin_matches']=query('ThingsPanel',"SELECT json_agg(t) FROM (SELECT d.id,sp.id AS plugin_id,sp.service_identifier,sp.service_type,sp.service_config::jsonb->>'sub_topic_prefix' AS prefix,sa.tenant_id AS access_tenant FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id LEFT JOIN service_access sa ON sa.id=d.service_access_id LEFT JOIN service_plugins sp ON sp.id=sa.service_plugin_id OR (sp.service_identifier=c.protocol_type AND sp.service_type=1) WHERE d.id IN ("+quoted+"))t")
out['bindings']=query('ThingsPanel',"SELECT json_agg(t) FROM (SELECT device_id,tenant_id,broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE device_id IN ("+quoted+"))t")
root=pathlib.Path('/opt/tp-community-main-20261028')
services=json.loads((root/'active-broker-services.private.json').read_text());app=next(x for x in services if x.get('target_tenant_scope')=='platform')
# Authentication probe creates only ordinary service client identity evidence;
# it publishes no command and does not modify any credential or scope.
binding=next(x for x in json.loads((root/'active-broker-tokens.private.json').read_text()) if x['broker_cell']==app['broker_cell'] and x['broker_instance']==app['broker_instance'])
out['auth_probes']=[]
for target in ids:
 body={'request_id':str(uuid.uuid4()),'provider':'mosquitto','broker_cell':app['broker_cell'],'broker_instance':app['broker_instance'],'connection_nonce':str(uuid.uuid4()),'wire_client_id':app['allowed_client_ids'][0]+'--target-v1--'+base64.urlsafe_b64encode(target.encode()).decode().rstrip('='),'username':app['username'],'password':app['password'],'protocol':4,'peer':'127.0.0.1'}
 req=urllib.request.Request('http://127.0.0.1:9801/v1/authenticate',data=json.dumps(body).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+binding['token']})
 try:
  with urllib.request.urlopen(req,timeout=10) as r:grant=json.load(r);status=r.status
  out['auth_probes'].append({'target':target,'http_status':status,'allow':grant.get('allow'),'deny_code':grant.get('deny_code'),'publish':grant.get('policy',{}).get('publish')})
 except urllib.error.HTTPError as e:out['auth_probes'].append({'target':target,'http_status':e.code})
print(json.dumps(out))
