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
print(json.dumps(out,ensure_ascii=False))
