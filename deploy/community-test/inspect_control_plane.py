import json,pathlib,subprocess,hashlib
root=pathlib.Path('/opt/tp-community-main-20261028')
out={'runtime':{},'compose':{},'schemas':{}}
for name in ('runtime.encore.private.json','runtime.notification.private.json'):
 r=json.loads((root/name).read_text())
 out['runtime'][name]={'hosted_services':r.get('hosted_services'), 'sha256':hashlib.sha256((root/name).read_bytes()).hexdigest(), 'databases':{physical:db['name'] for server in r.get('sql_servers',[]) for physical,db in server.get('databases',{}).items()}}
c=json.loads((root/'compose.encore.private.json').read_text())
for name,s in c['services'].items():
 out['compose'][name]={'image':s.get('image'),'ports':s.get('ports'),'network_mode':s.get('network_mode'),'environment_keys':sorted(s.get('environment',{})),'volumes':s.get('volumes')}
for database in ('native_main_20261028','automation_main_20261028','notification_main_20261028'):
 query="SELECT json_build_object('schema',(SELECT json_agg(t) FROM schema_migrations t),'ledger',(SELECT json_agg(t) FROM deployment_schema_ledger t))"
 p=subprocess.run(['docker','exec','timescaledb','psql','-X','-q','-At','-U','postgres','-d',database,'-c',query],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 out['schemas'][database]=json.loads(p.stdout) if p.returncode==0 else {'error':'schema query failed'}
out['nginx_routes']=[line.strip() for line in pathlib.Path('/etc/nginx/conf.d/community.conf').read_text().splitlines() if any(k in line for k in ('location ','proxy_pass ','root ','alias '))]
out['disk']=subprocess.check_output(['df','-h','/opt']).decode()
print(json.dumps(out))
