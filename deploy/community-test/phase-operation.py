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
 started=time.monotonic()
 try:
  with urllib.request.urlopen(req,timeout=10) as r:grant=json.load(r);status=r.status
  out['auth_probes'].append({'target':target,'http_status':status,'allow':grant.get('allow'),'deny_code':grant.get('deny_code'),'elapsed_ms':round((time.monotonic()-started)*1000,1),'wire_client_id':body['wire_client_id'],'grant':grant})
 except urllib.error.HTTPError as e:out['auth_probes'].append({'target':target,'http_status':e.code})
print(json.dumps({'mqtt_auth':out}))

import json,pathlib,base64,subprocess,urllib.request,urllib.error,urllib.parse,datetime
enc={'chunks': ['M/WCA5cGMG3od4xHgmv2BovIQeJyhaNfMw9jnxo9UIuGxOJrxYiG6C0Af32yMQKUwNnsjazEd/601hyrZU1lExu+6Sy6DLUGYKUsXX3LN82GJvWiJkY9NhajzUfezR1bwx3ZUc6jfsoHvjLY7AtWjug65jLU9bEM4n83LZhbGn4CEMl5wMoVbsEhQoFR3FKdIqRBDunEgiYFYjQGyZIVWo+CHL8GcEHevWw1e4WNX0sJtNmT2DQc08TxMtM0jxJHGScUhW0DBd10xw4KKS+we5zPs05jFp8NLOfwdQP/WCpughCGnjuZDwkQwV4ZHrGoeL7G2JVbWDlbDRwt+XNBsw==', 'thSOC/FiGkJL4V2HpIAHltXEnk2WojpLUGE/6u0Qbspsl2Q8z+PHAfBboNADZOWNrQiEBWmO7oXwWEleyaJ5wZIhjmq1qTcQ0k3MDROGxcB8zcDARl8xrd6sBhkZhxbPextVQi7Qj42pdJZDZ1/b/iub29y66FDzeQ8+zULVU+U56SG87IwKuY6E9qLXOjr7l9DKOZnNCO0VF5mrVCYi7PANv8fzzuIluAkqSqr0UJqQWd2lyqCREiLsYz4yh28skhLL0tzCUw7d9lXOETrSJQf8tN+HrNMhN3397MvFs1F2TgpzXZMZcjHPl+BDePFSqs76qevTvN0qZkcSzV891Q=='], 'key_directory': '/tmp/qa-q08-session-gk9qwhrb'}
key=pathlib.Path(enc['key_directory'])/'private.pem'
token=b''.join(subprocess.check_output(['openssl','pkeyutl','-decrypt','-inkey',str(key),'-pkeyopt','rsa_padding_mode:oaep','-pkeyopt','rsa_oaep_md:sha256','-pkeyopt','rsa_mgf1_md:sha256'],input=base64.b64decode(c)) for c in enc['chunks']).decode()
cred=json.loads(pathlib.Path('/opt/tp-community-main-20261028/ha-runtime/ha-demo.credentials.private.json').read_text());ha_token=cred['long_lived_token'];out={'at':datetime.datetime.now().isoformat()}
def call(base,path,body=None,auth=None,method=None):
 req=urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,headers={'Content-Type':'application/json','Authorization':'Bearer '+(auth or token),'x-token':auth or token},method=method)
 try:
  with urllib.request.urlopen(req,timeout=15) as r:return {'http_status':r.status,'result':json.load(r)}
 except urllib.error.HTTPError as e:return {'http_status':e.code,'error':e.read().decode()[:300].replace(ha_token,'[redacted]').replace(token,'[redacted]')}
 except Exception as e:return {'error':type(e).__name__}
base='http://127.0.0.1:9999';ha='http://127.0.0.1:58123'

light=call(ha,'/api/states/light.office_rgbw_lights',auth=ha_token);out['light_after_command']={'status':light.get('http_status'),'state':light.get('result',{}).get('state')}
out['current']={}
ids=['bb9e5ff3-d73e-6efe-5dda-ae216fb44179','147d134e-6b82-2aea-e2b4-2424011ed23a','40fd7d06-6f29-6903-9dc0-4e736357c34f'];out['service_scope']=[]
p=pathlib.Path('/opt/tp-community-main-20261028/active-broker-services.private.json');policies=json.loads(p.read_text());policies=policies if isinstance(policies,list) else policies.get('services',[])
for item in policies:
 covered=[i for i in ids if i in item.get('allowed_device_ids',[])];out['service_scope'].append({'role':item.get('role'),'plugin_socket':item.get('plugin_socket',False),'target_scoped':item.get('target_scoped_publisher',False),'targets_count':len(item.get('allowed_device_ids',[])),'qa_covered':covered})
for id in ids:out['current'][id]=call(base,'/api/v1/telemetry/datas/current/'+id)
import re
out['plugin_logs']=[]
for cid in subprocess.check_output(['docker','ps','-q']).decode().split():
 data=json.loads(subprocess.check_output(['docker','inspect',cid]))[0];aliases=sum([(v.get('Aliases') or []) for v in data['NetworkSettings']['Networks'].values()],[])
 if 'encore' in data['Name'].lower():
  labels=data['Config'].get('Labels') or {}
  out.setdefault('encore_provenance',[]).append({'name':data['Name'],'image_tag':data['Config']['Image'],'image_id':data['Image'],'revision':labels.get('org.opencontainers.image.revision'),'compose_files':labels.get('com.docker.compose.project.config_files'),'compose_workdir':labels.get('com.docker.compose.project.working_dir')})
 if not any(a in aliases for a in ['tp-plugin-homeassistant','tp-plugin-http-adapter']):continue
 if 'tp-plugin-homeassistant' in aliases:
  env=dict(v.split('=',1) for v in data['Config']['Env'] if '=' in v)
  out['ha_environment_presence']={k:bool(env.get(k)) for k in ['TP_MQTT_BROKER','MQTT_BROKER','HA_MQTT_PASSWORD','HOMEASSISTANT_MQTT_PASSWORD','HA_DEVICE_SYNC_INTERVAL_SECONDS']}
  out['ha_image_revision']=(data['Config'].get('Labels') or {}).get('org.opencontainers.image.revision')
  for network in data['NetworkSettings']['Networks'].values():
   ip=network.get('IPAddress')
   if not ip:continue
   try:
    with urllib.request.urlopen('http://'+ip+':9002/sync-status',timeout=4) as rr:sync=json.load(rr)
    out['ha_sync_status']={k:sync.get(k) for k in ['state','stop_reason','last_sync_at','syncs','devices','device_numbers']};break
   except Exception:pass
 p=subprocess.run(['docker','logs','--since','150m','--tail','4000',cid],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=10)
 lines=p.stdout.decode(errors='replace').splitlines();selected=[]
 for line in lines:
  if any(w in line for w in ['35180','qa_q08_','bb9e5ff3','147d134e','40fd7d06','direct control failed','state stream','device sync','telemetry MQTT','telemetry published','not authorised','not authorized']):
   if re.search('secret|token|voucher|credential|password',line,re.I):continue
   selected.append(line[:700])
 out['plugin_logs'].append({'name':data['Name'],'image':data['Config']['Image'],'matches':selected[-30:]})
print(json.dumps(out))
