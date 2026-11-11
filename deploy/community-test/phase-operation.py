import json,pathlib,subprocess,hashlib,yaml
root=pathlib.Path('/opt/tp-community-main-20261028')
live=pathlib.Path('/home/thingspanel/thingspanel-backend-community/configs/conf.yml')
candidate=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/20261111160912/platform.next.private.yml')
def sql(q):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel'],input=('BEGIN READ ONLY;'+q+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read only upgrade inspection failed')
 return json.loads(p.stdout)
c=yaml.safe_load(live.read_text());n=yaml.safe_load(candidate.read_text())
p=json.loads(subprocess.check_output(['/root/.local/share/pnpm/pm2','jlist']))
p=next(x for x in p if x['name']=='thingspanel-backend-community')['pm2_env']
print(json.dumps({'config_sha256':hashlib.sha256(live.read_bytes()).hexdigest(),
 'staged_same_except_database':{k:v for k,v in c.items() if k not in ('db','relational')}=={k:v for k,v in n.items() if k not in ('db','relational')},
 'mqtt_profile':c.get('mqtt',{}).get('input_profile'),
 'overrides':{k:v for k,v in p.items() if k in ('GOTP_MQTT_INPUT_PROFILE','GOTP_AUTOMATION_MODE','TP_AUTH_OWNER','TP_PLATFORMCONFIG_OWNER')},
 'ledger':sql("SELECT coalesce(json_agg(t),'[]'::json) FROM native_component_migration_ledger t"),
 'version':sql("SELECT json_agg(t) FROM sys_version t"),
 'resources':subprocess.check_output(['df','-h','/opt']).decode()}))
