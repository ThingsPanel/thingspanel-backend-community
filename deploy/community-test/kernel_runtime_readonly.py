import subprocess,json,pathlib

def run(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=120)
 return p.stdout.decode(errors='replace')
root=pathlib.Path('/opt/tp-community-actions')
print(json.dumps({
 'disk':run(['df','-h','/']),
 'sizes':run(['du','-x','-h','--max-depth=2','/root','/opt/tp-community-actions','/var/lib/docker','/var/log']),
 'docker':run(['docker','system','df']),
 'incoming':[{ 'name':p.name,'bytes':p.stat().st_size} for p in (root/'incoming').iterdir() if p.is_file() and p.name.endswith('.tar.gz')],
 'backend_receipts':[str(p.parent.name) for p in (root/'backend-releases').glob('*/deployment.json')],
 'containers':run(['docker','ps','--format','{{.Names}} {{.Image}} {{.Status}}']),
},ensure_ascii=False))
