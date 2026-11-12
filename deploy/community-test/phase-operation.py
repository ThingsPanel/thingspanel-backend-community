import subprocess,json,pathlib,fcntl,hashlib,urllib.request,datetime
out={'at':datetime.datetime.utcnow().isoformat()+'Z','scope':'read-only HTTP deployed image/source and own QA fixture final status'}
lock=open('/opt/tp-community-actions/deployment.lock','a')
try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
except BlockingIOError:print(json.dumps({'status':'BLOCKED','reason':'deployment lock busy'}));raise SystemExit()
ids=subprocess.check_output(['docker','ps','-q']).decode().split();out['http']=[]
for cid in ids:
 c=json.loads(subprocess.check_output(['docker','inspect',cid]))[0];name=c['Name'];aliases=[a for n in c['NetworkSettings']['Networks'].values()for a in (n.get('Aliases')or [])]
 if 'tp-plugin-http-adapter' not in aliases and 'http' not in name.lower():continue
 im=json.loads(subprocess.check_output(['docker','image','inspect',c['Image']]))[0];labels=(im.get('Config')or {}).get('Labels')or {};row={'name':name,'container_id':c['Id'],'image_id':c['Image'],'image_reference':c['Config']['Image'],'oci':{k:v for k,v in labels.items()if k in ['org.opencontainers.image.revision','org.opencontainers.image.source','org.opencontainers.image.version']},'started_at':c['State']['StartedAt'],'restart_count':c['RestartCount'],'mounts':[{'source':m['Source'],'destination':m['Destination']}for m in c.get('Mounts',[])]}
 r=subprocess.run(['docker','exec',cid,'sha256sum','/app/tp-plugin-http'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True);row['binary_sha256']=r.stdout.strip().split()[0] if r.returncode==0 else None;out['http'].append(row)
p=pathlib.Path('/home/plugin/thingspanel-adapter-http');out['checkout_path']=str(p);out['checkout_exists']=p.exists()
if p.exists():
 r=subprocess.run(['git','-C',str(p),'rev-parse','HEAD'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True);out['checkout_head']=r.stdout.strip() if r.returncode==0 else None
for name,path in [('HTTP_direct','/tmp/qa-q06-http-direct-ytbyq507/result.json'),('HA_faults','/tmp/qa-q08-ha-fault-lczmrqjv/result.json')]:
 p=pathlib.Path(path);out[name]=json.loads(p.read_text()) if p.exists() else {'missing':True}
print(json.dumps(out))
