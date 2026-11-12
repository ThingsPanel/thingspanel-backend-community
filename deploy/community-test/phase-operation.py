import subprocess,json,pathlib,re
out={'containers':[],'pm2':[],'paths':[]}
p=subprocess.run(['docker','ps','--format','{{.Names}}'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=10)
for n in p.stdout.splitlines():
 if not any(s in n for s in ['ha-agent','ha-bridge','broker-owner','broker-mosquitto']):continue
 x=json.loads(subprocess.run(['docker','inspect',n],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=10).stdout)[0]
 out['containers'].append({'name':n,'image':x['Config']['Image'],'image_id':x['Image'],'mounts':[{'source':m['Source'],'dest':m['Destination'],'rw':m['RW']} for m in x['Mounts']],'env_keys':[s.split('=',1)[0] for s in x['Config'].get('Env',[])],'labels':{k:v for k,v in x['Config'].get('Labels',{}).items() if 'source' in k or 'revision' in k or 'working_dir' in k}})
p=subprocess.run(['/root/.local/share/pnpm/pm2','jlist'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=10)
for x in json.loads(p.stdout):
 e=x.get('pm2_env',{});out['pm2'].append({'name':x.get('name'),'pid':x.get('pid'),'cwd':e.get('pm_cwd'),'exec':e.get('pm_exec_path'),'status':e.get('status'),'restarts':e.get('restart_time')})
for root in ['/opt/tp-community-actions','/opt/tp-community-main-20261028','/opt/tp-main47-ha']:
 p=pathlib.Path(root)
 if p.exists():out['paths'] += [str(x) for x in p.glob('*') if any(s in x.name for s in ['catalog','trust','agent','gin','source','release','broker','ha','private'])]
print(json.dumps(out))
