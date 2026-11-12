#!/usr/bin/env python3
"""Read-only managed HTTP upgrade preflight. No container/config/API mutations."""
import fcntl, hashlib, json, os, pathlib, shutil, subprocess

def run(args):
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True, timeout=30)
    if p.returncode:
        raise RuntimeError("read command failed: " + args[0])
    return p.stdout

def sha(path):
    f = pathlib.Path(path)
    return hashlib.sha256(f.read_bytes()).hexdigest() if f.is_file() else None

def summary_state(value):
    # State can carry credentials. Only expose known non-secret lifecycle fields.
    allowed = {"instance", "instance_id", "plugin", "profile", "generation", "state", "status", "expires_at", "lease_expires_at", "active_generation", "accepted_generation"}
    rows = []
    def walk(x, path):
        if isinstance(x, dict):
            row = {k:v for k,v in x.items() if k in allowed and isinstance(v, (str,int,float,bool,type(None)))}
            if row: rows.append({"path":path, "lifecycle":row})
            for k,v in x.items():
                if isinstance(v,(dict,list)): walk(v,path+"/"+str(k))
        elif isinstance(x,list):
            for i,v in enumerate(x): walk(v,path+"/"+str(i))
    walk(value, "")
    return rows

lock = open('/opt/tp-community-actions/deployment.lock','r')
fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
agent = json.loads(run(['docker','inspect','tp-main47-ha-agent']))[0]
http = json.loads(run(['docker','inspect','tp-agent-prepared-ac01d43ee3f1859518c788843b51690757b1fdc7']))[0]
mounts = {m['Destination']:m['Source'] for m in agent['Mounts']}
args = agent['Config'].get('Cmd') or []
flags = {}
allowed_flags = {'-state','-socket','-token-file','-catalog','-trust-root','-stack-env','-network','-docker-socket','-sweep-interval'}
for i,a in enumerate(args[:-1]):
    if a in allowed_flags: flags[a]=args[i+1]

def hostpath(containerpath):
    for target,source in sorted(mounts.items(),key=lambda kv:len(kv[0]),reverse=True):
        if containerpath==target or containerpath.startswith(target+'/'):
            return source+containerpath[len(target):]
    return None

def container_view(c):
    return {'name':c['Name'],'id':c['Id'],'image_id':c['Image'],'image_ref':c['Config']['Image'],
            'status':c['State']['Status'],'started_at':c['State'].get('StartedAt'),
            'restart_count':c['RestartCount'],'network_mode':c['HostConfig']['NetworkMode'],
            'networks':{k:{'aliases':v.get('Aliases')} for k,v in c['NetworkSettings']['Networks'].items()}}

result={'read_only':True,'agent':container_view(agent),'http':container_view(http),'agent_flags':flags,
        'agent_mounts':[{k:m[k] for k in ('Source','Destination','RW')} for m in agent['Mounts']],
        'disk_free_bytes':shutil.disk_usage('/opt').free}
mem={}
for line in pathlib.Path('/proc/meminfo').read_text().splitlines():
    key,val=line.split(':',1)
    if key in ('MemAvailable','SwapFree'): mem[key]=int(val.strip().split()[0])*1024
result['memory_bytes']=mem
catpath=hostpath(flags.get('-catalog',''))
if not catpath: raise RuntimeError('cannot map catalog')
cat=json.loads(pathlib.Path(catpath).read_text())
result['catalog']={'path':catpath,'sha256':sha(catpath),'signature_sha256':sha(catpath+'.sig'),
                   'version':cat.get('version'),'agent_sha256':cat.get('agent_sha256'),
                   'plugins':[{k:p.get(k) for k in ('id','version','source_commit','image_id','image_archive_sha256','installer_commit')} for p in cat['plugins']]}
rootpath=hostpath(flags.get('-trust-root',''))
result['trust_root']={'path':rootpath,'sha256':sha(rootpath)}
result['agent_binary']={'path':mounts.get('/tp-agent'),'sha256':sha(mounts.get('/tp-agent',''))}
statepath=hostpath(flags.get('-state',''))
result['state']={'path':statepath,'lifecycle':summary_state(json.loads(pathlib.Path(statepath).read_text()))}
result['existing_tools']=[]
for base in ['/opt/tp-community-main-20261028/ha-runtime/installer','/opt/tp-community-main-20261028/http-runtime/installer']:
    for rel in ['tools/agent_host.py','tools/plugin_catalog.py','tools/scope_sync.py','config/native/plugin-trust.pem','config/native/plugin_catalog_trust.pem']:
        path=base+'/'+rel
        if pathlib.Path(path).is_file(): result['existing_tools'].append({'path':path,'sha256':sha(path)})
result['preconditions']={'agent_running':agent['State']['Running'],'http_running':http['State']['Running'],
                         'catalog_has_http':any(p.get('id')=='http-adapter' for p in cat['plugins']),
                         'agent_binary_matches_catalog':result['agent_binary']['sha256']==cat.get('agent_sha256')}
print(json.dumps(result,indent=2))
