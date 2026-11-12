"""Authorized MQTT smoke on the existing test sensor; never exports its voucher."""
import datetime,json,pathlib,socket,struct,subprocess,time,uuid
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
tenant='63a4b529'

def query(database,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN READ ONLY;'+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only acceptance query failed')
 return json.loads(p.stdout)


import re
out={'commands':query('ThingsPanel',"SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,device_id,target_device_id,state,last_error,route_cell,route_epoch,convert_from(payload,'UTF8')::jsonb->'message' AS message FROM broker_command_requests WHERE id IN ('5e574249-61ab-4eca-8751-9cb7e42b1ad6','a6ad0493-975a-47bd-9e04-40d7351e5534'))t"),'containers':[],'logs':{}}
for name in subprocess.check_output(['docker','ps','--format','{{.Names}}']).decode().splitlines():
 if not any(w in name for w in ['broker-owner','broker-mosquitto','encore']):continue
 x=json.loads(subprocess.check_output(['docker','inspect',name]))[0];labels=x['Config'].get('Labels') or {}
 out['containers'].append({'name':name,'image':x['Image'],'image_name':x['Config']['Image'],'status':x['State']['Status'],'labels':{k:v for k,v in labels.items() if k.startswith('com.docker.compose.') or k in ['org.opencontainers.image.revision','org.opencontainers.image.source']}})
 if 'encore' in name:continue
 p=subprocess.run(['docker','logs','--since','12m','--tail','1600',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
 lines=p.stdout.decode(errors='replace').splitlines()
 out['logs'][name]=[l[:900] for l in lines if any(w in l.lower() for w in ['not author','denied','target-v1','failed','error']) and not any(w in l.lower() for w in ['password','token','secret','credential','authorization'])][-70:]
proc=json.loads(subprocess.check_output(['/root/.local/share/pnpm/pm2','jlist']));backend=next(x for x in proc if x['name']=='thingspanel-backend-community')
for key in ['pm_out_log_path','pm_err_log_path']:
 p=pathlib.Path(backend['pm2_env'][key]);f=p.open('rb');f.seek(max(0,p.stat().st_size-180000));lines=f.read().decode(errors='replace').splitlines()
 out['logs'][key]=[l[:900] for l in lines if any(w in l.lower() for w in ['publish','broker','target-v1','a6ad0493','5e574249']) and not any(w in l.lower() for w in ['password','token','secret','credential','authorization'])][-45:]
print(json.dumps(out))
