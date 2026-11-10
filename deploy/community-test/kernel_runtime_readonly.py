"""Read-only runtime provenance audit. No sends, credentials, migrations or restarts."""
import pathlib,subprocess,json,hashlib,datetime,re
def run(args,timeout=30):
    p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=timeout)
    if p.returncode: return None
    return p.stdout.strip()
def fingerprint(path):
    p=pathlib.Path(path)
    try:
        data=p.read_bytes()
        return {'sha256':hashlib.sha256(data).hexdigest(),'bytes':len(data),'has_pushPreview_symbol':b'pushPreview' in data}
    except OSError:return None
out={'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'containers':[],'source_files':[],'deployments':[]}
ids=run(['docker','ps','-q']) or ''
for cid in ids.splitlines():
    raw=run(['docker','inspect',cid])
    if not raw:continue
    d=json.loads(raw)[0];name=d['Name'].lstrip('/')
    if not any(k in name.lower()+' '+d['Config']['Image'].lower() for k in ['encore','notification','unipush','smtp']):continue
    x={'name':name,'image_ref':d['Config']['Image'],'image_id':d['Image'],'started_at':d['State'].get('StartedAt'),'status':d['State']['Status']}
    im=run(['docker','image','inspect',d['Image']])
    if im:
        info=json.loads(im)[0];labels=info.get('Config',{}).get('Labels') or {}
        x['labels']={k:labels[k] for k in ['org.opencontainers.image.source','org.opencontainers.image.revision','org.opencontainers.image.version'] if k in labels}
        x['image_created']=info.get('Created')
    x['mounts']=[{'source':m.get('Source'),'destination':m.get('Destination'),'type':m.get('Type')} for m in d.get('Mounts',[])]
    pid=d['State'].get('Pid',0)
    if pid:
        exe='/proc/'+str(pid)+'/exe';x['binary']=fingerprint(exe)
        try:x['executable']=str(pathlib.Path(exe).resolve())
        except OSError:pass
        go='/usr/local/go/bin/go'
        if pathlib.Path(go).exists():
            meta=run([go,'version','-m',exe])
            if meta:x['go_build']=[line.strip() for line in meta.splitlines() if line.startswith(('\tpath\t','\tmod\t')) or any(k in line for k in ['vcs.revision=','vcs.modified=','vcs.time='])]
    out['containers'].append(x)
# Existing immutable deployment receipts only; never read private runtime configuration.
base=pathlib.Path('/opt/tp-community-actions/encore-releases')
if base.exists():
    for p in sorted(base.glob('*/deployment.json')):
        try:
            d=json.loads(p.read_text())
            out['deployments'].append({'directory_revision':p.parent.name,**{k:d[k] for k in ['commit','image','image_id','timestamp','revision','source_commit','sha'] if k in d}})
        except Exception:pass
# Bounded scan of application source trees. Skip caches, databases, Git and private configs.
roots=[p for p in pathlib.Path('/opt').iterdir() if p.is_dir() and ('tp-' in p.name or 'thingspanel' in p.name or 'unipush' in p.name)]
queue=[(p,0) for p in roots];visited=0
while queue and visited<18000:
    folder,depth=queue.pop();visited+=1
    try:children=list(folder.iterdir())
    except OSError:continue
    for p in children:
        if p.is_symlink():continue
        if p.is_dir():
            if depth<7 and not any(k in p.name.lower() for k in ['cache','node_modules','.git','postgres','timescale','redis','volumes','data','vendor']):queue.append((p,depth+1))
        elif p.name=='provider.go' and p.parent.name in ['unipush','smtp']:
            data=p.read_bytes();x={'path':str(p),'sha256':hashlib.sha256(data).hexdigest(),'git_blob':hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest(),'has_pushPreview':b'func pushPreview(' in data}
            out['source_files'].append(x)
out['source_scan_directories']=visited
print(json.dumps(out,ensure_ascii=False,indent=2))
