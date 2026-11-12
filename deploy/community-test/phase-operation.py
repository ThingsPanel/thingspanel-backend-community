import json,subprocess,hashlib,pathlib,fcntl,datetime
out={'at':datetime.datetime.utcnow().isoformat()+'Z','scope':'read-only HTTP running executable build info and checkout status; no deployment'}
lock=open('/opt/tp-community-actions/deployment.lock','r')
try:fcntl.flock(lock,fcntl.LOCK_SH|fcntl.LOCK_NB)
except BlockingIOError:print(json.dumps({'blocked':'deployment lock busy'}));raise SystemExit()
def run(a):return subprocess.check_output(a,timeout=15).decode().strip()
def inline(data,pos):
 n=0;shift=0
 for i in range(10):
  c=data[pos];pos+=1;n|=(c&127)<<shift
  if c<128:return data[pos:pos+n].decode(errors='replace'),pos+n
  shift+=7
 raise ValueError('bad length')
out['containers']=[]
for cid in run(['docker','ps','-q']).split():
 c=json.loads(run(['docker','inspect',cid]))[0]
 aliases=[a for n in c['NetworkSettings']['Networks'].values() for a in (n.get('Aliases') or [])]
 if 'tp-plugin-http-adapter' not in aliases:continue
 im=json.loads(run(['docker','image','inspect',c['Image']]))[0]
 row={'name':c['Name'],'image_id':c['Image'],'image_ref':c['Config']['Image'],'pid':c['State']['Pid'],'entry_executable':c.get('Path'),'working_dir':c['Config'].get('WorkingDir'),'image_created':im.get('Created'),'repo_digests':im.get('RepoDigests'),'oci':{k:v for k,v in ((im.get('Config')or {}).get('Labels')or {}).items() if k.startswith('org.opencontainers.image.')},'restart_count':c['RestartCount']}
 exe=pathlib.Path('/proc')/str(c['State']['Pid'])/'exe';data=exe.read_bytes();row['binary_sha256']=hashlib.sha256(data).hexdigest();row['binary_bytes']=len(data)
 i=data.find(b'\xff Go buildinf:')
 if i>=0 and data[i+15]&2:
  version,pos=inline(data,i+32);module,pos=inline(data,pos);row['go_version']=version
  row['build_info']=[x for x in module.splitlines() if x.startswith(('path\t','mod\t','build\tvcs','build\tGOOS','build\tGOARCH','build\tCGO_ENABLED','build\t-buildmode','build\t-compiler'))]
 else:row['build_info_unavailable']=True
 out['containers'].append(row)
p='/home/plugin/thingspanel-adapter-http'
out['checkout']={'path':p,'head':run(['git','-C',p,'rev-parse','HEAD']),'status':run(['git','-C',p,'status','--porcelain']),'diff_stat':run(['git','-C',p,'diff','--stat'])}
print(json.dumps(out))
