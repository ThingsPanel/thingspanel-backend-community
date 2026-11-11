"""Read-only rollout preflight: counts and configuration names, never secrets."""
import datetime,json,pathlib,re,subprocess
root=pathlib.Path('/opt/tp-community-main-20261028')
out={'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'runtime':{}}
databases=[]
for name in ['runtime.encore.private.json','runtime.notification.private.json']:
 data=json.loads((root/name).read_text())
 item={'keys':sorted(data),'hosted_services':[], 'hosted_gateways':data.get('hosted_gateways',[])}
 for svc in data.get('hosted_services',[]):
  item['hosted_services'].append(svc if isinstance(svc,str) else svc.get('name'))
 discovery=data.get('service_discovery',{})
 item['service_discovery_names']=sorted(discovery) if isinstance(discovery,dict) else []
 item['databases']=[]
 for server in data.get('sql_servers',[]):
  for physical,db in server.get('databases',{}).items():
   item['databases'].append({'database':physical,'logical_name':db.get('name')})
   if db.get('name')=='device_metadata':databases.append(physical)
 out['runtime'][name]=item
if not databases or len(set(databases))!=1:raise RuntimeError('Ambiguous metadata database')
database=databases[0]
if not re.fullmatch('[a-z0-9_]+',database):raise RuntimeError('Invalid database identifier')
query="""BEGIN READ ONLY; SET LOCAL statement_timeout='15s';
SELECT json_build_object(
 'models',(SELECT count(*) FROM thing_models),
 'snapshots',(SELECT count(*) FROM thing_model_snapshots),
 'templates',(SELECT count(*) FROM device_templates),
 'devices',(SELECT count(*) FROM devices),
 'cross_tenant_items',(SELECT count(*) FROM thing_model_items i JOIN thing_models m ON m.id=i.thing_model_id WHERE i.tenant_id<>m.tenant_id),
 'cross_tenant_snapshots',(SELECT count(*) FROM thing_model_snapshots s JOIN thing_models m ON m.id=s.thing_model_id WHERE s.tenant_id<>m.tenant_id),
 'template_model_mismatch',(SELECT count(*) FROM device_templates t JOIN thing_models m ON m.id=t.thing_model_id WHERE t.tenant_id<>m.tenant_id),
 'template_snapshot_mismatch',(SELECT count(*) FROM device_templates t JOIN thing_model_snapshots s ON s.id=t.thing_model_snapshot_id WHERE t.tenant_id<>s.tenant_id OR t.thing_model_id<>s.thing_model_id),
 'product_template_mismatch',(SELECT count(*) FROM products p JOIN device_templates t ON t.id=p.device_template_id WHERE p.tenant_id<>t.tenant_id),
 'device_template_mismatch',(SELECT count(*) FROM devices d JOIN device_templates t ON t.id=d.device_template_id WHERE d.tenant_id<>t.tenant_id),
 'device_product_mismatch',(SELECT count(*) FROM devices d JOIN products p ON p.id=d.product_id WHERE d.tenant_id<>p.tenant_id)
); ROLLBACK;
"""
result=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=query,universal_newlines=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=25)
if result.returncode:out['metadata_preflight_error']=result.stderr[:1600]
else:out['metadata_preflight']=json.loads(result.stdout)
def command(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=30)
 if p.returncode: return None
 return p.stdout.strip()
out['disk']=command(['df','-h','/opt'])
out['containers']=json.loads('['+','.join(command(['docker','ps','--format','{{json .}}']).splitlines())+']')
out['databases']=command(['docker','exec','timescaledb','psql','-X','-U','postgres','-At','-c',"SELECT datname FROM pg_database WHERE NOT datistemplate"])
out['native_schema']=command(['docker','exec','timescaledb','psql','-X','-U','postgres','-d',database,'-At','-c',"SELECT json_agg(t) FROM (SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('devices','device_templates','device_groups','device_group_members','device_template_bindings','device_access_bindings') ORDER BY table_name,ordinal_position)t"])
out['schema_migrations']=command(['docker','exec','timescaledb','psql','-X','-U','postgres','-d',database,'-At','-c','SELECT row_to_json(t) FROM schema_migrations t'])
out['nginx_paths']=[]
for path in pathlib.Path('/home/thingspanel/nginx').rglob('*.conf'):
 if path.is_file():
  lines=[line.strip() for line in path.read_text(errors='replace').splitlines() if re.match(r'^\s*(location|proxy_pass|root|server_name|listen)\s',line)]
  out['nginx_paths'].append({'path':str(path),'directives':lines})
out['metadata_counts']=command(['docker','exec','timescaledb','psql','-X','-U','postgres','-d',database,'-At','-c',"SELECT json_build_object('devices',(SELECT count(*) FROM devices),'access_templates',(SELECT count(*) FROM device_access_templates),'models',(SELECT count(*) FROM thing_models),'groups',(SELECT count(*) FROM device_groups),'bindings',(SELECT count(*) FROM device_access_bindings));"])
out['community_schema']=command(['docker','exec','timescaledb','psql','-X','-U','postgres','-d','ThingsPanel','-At','-c',"SELECT json_agg(t) FROM (SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('devices','device_config','device_groups','device_group_relations','users') ORDER BY table_name,ordinal_position)t"])
out['community_counts']=command(['docker','exec','timescaledb','psql','-X','-U','postgres','-d','ThingsPanel','-At','-c',"SELECT json_build_object('devices',(SELECT count(*) FROM devices),'tenants',(SELECT count(distinct tenant_id) FROM devices),'native_models_bound',(SELECT count(*) FROM device_config WHERE thing_model_id IS NOT NULL));"])
out['mounts']={}
for cid in (command(['docker','ps','-q']) or '').splitlines():
 info=json.loads(command(['docker','inspect',cid]))[0]
 name=info['Name'].lstrip('/')
 if 'encore-api' in name or 'notification-core' in name:
  env=dict(x.split('=',1) for x in info['Config']['Env'] if '=' in x)
  out['mounts'][name]={'environment_keys':sorted(env),'native_auth_enabled':env.get('TP_AUTH_MODE')=='native','mounts':[m['Source'] for m in info.get('Mounts',[])]}
out['nginx_service']=command(['systemctl','show','nginx','--property=ExecStart'])
for confroot in ['/etc/nginx','/opt/nginx']:
 for path in pathlib.Path(confroot).rglob('*.conf'):
  if path.is_file():
   lines=[line.strip() for line in path.read_text(errors='replace').splitlines() if re.match(r'^\s*(location|proxy_pass|root|server_name|listen)\s',line)]
   out['nginx_paths'].append({'path':str(path),'directives':lines})
out['containers']=[{k:x[k] for k in ['Names','Image','State']} for x in out['containers']]
def community(q):
 return command(['docker','exec','timescaledb','psql','-X','-U','postgres','-d','ThingsPanel','-At','-c',q])
out['community_tables']=community("SELECT json_agg(table_name) FROM information_schema.tables WHERE table_schema='public'")
out['community_device_counts']=community("SELECT json_agg(t) FROM (SELECT tenant_id,device_type, count(*) FROM (SELECT tenant_id, coalesce(protocol,'')||':'||coalesce(access_way,'') AS device_type FROM devices) d GROUP BY tenant_id,device_type)t")
out['community_device_config_schema']=community("SELECT json_agg(t) FROM (SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema='public' AND (table_name ILIKE '%config%' OR table_name ILIKE '%group%') ORDER BY table_name,ordinal_position)t")
out['community_config_root_keys']=command(['python3','-c',"import yaml,json; d=yaml.safe_load(open('/home/thingspanel/thingspanel-backend-community/configs/conf.yml')); print(json.dumps({'keys':list(d),'relational_keys':list(d.get('relational',{}))}))"])
def community_snapshot(q):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel'],input=q,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=30)
 if p.returncode:return {'error':'inventory_query_failed'}
 return json.loads(p.stdout)
out['migration_inventory']=community_snapshot("""BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object(
'devices',(SELECT count(*) FROM devices),
'templates',(SELECT count(*) FROM device_configs),
'groups',(SELECT count(*) FROM groups),
'group_members',(SELECT count(*) FROM r_group_device),
'devices_without_tenant',(SELECT count(*) FROM devices WHERE coalesce(btrim(tenant_id),'')=''),
'invalid_device_ids',(SELECT count(*) FROM devices WHERE id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
'missing_device_numbers',(SELECT count(*) FROM devices WHERE coalesce(btrim(device_number),'')=''),
'duplicate_device_number_groups',(SELECT count(*) FROM (SELECT tenant_id,device_number FROM devices WHERE coalesce(btrim(device_number),'')<>'' GROUP BY tenant_id,device_number HAVING count(*)>1)x),
'orphan_device_templates',(SELECT count(*) FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id WHERE coalesce(d.device_config_id,'')<>'' AND c.id IS NULL),
'cross_tenant_device_templates',(SELECT count(*) FROM devices d JOIN device_configs c ON c.id=d.device_config_id WHERE d.tenant_id<>c.tenant_id),
'templates_without_native_model',(SELECT count(*) FROM device_configs WHERE coalesce(native_thing_model_id,'')=''),
'invalid_group_ids',(SELECT count(*) FROM groups WHERE id !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
'orphan_group_members',(SELECT count(*) FROM r_group_device r LEFT JOIN groups g ON g.id=r.group_id LEFT JOIN devices d ON d.id=r.device_id WHERE g.id IS NULL OR d.id IS NULL),
'cross_tenant_group_members',(SELECT count(*) FROM r_group_device r JOIN groups g ON g.id=r.group_id JOIN devices d ON d.id=r.device_id WHERE g.tenant_id<>d.tenant_id OR r.tenant_id<>d.tenant_id),
'enabled_values',(SELECT json_agg(x) FROM (SELECT is_enabled,count(*) FROM devices GROUP BY is_enabled)x),
'template_types',(SELECT json_agg(x) FROM (SELECT device_type,protocol_type,voucher_type,device_conn_type,count(*) FROM device_configs GROUP BY device_type,protocol_type,voucher_type,device_conn_type)x)
); COMMIT;""")
print(json.dumps(out,ensure_ascii=False))
