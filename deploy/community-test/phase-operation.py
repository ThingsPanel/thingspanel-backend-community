import pathlib,json,hashlib,subprocess,re
p=pathlib.Path('/opt/tp-community-main-20261028/http-runtime/scope.py');lines=p.read_text().splitlines();keep=set()
for i,l in enumerate(lines):
 if any(x in l for x in ['allowed_device_ids','allowed_tenant_ids','target_','flock','while ','write','replace','sleep','broker-owner','services']):keep.update(range(max(0,i-6),min(len(lines),i+9)))
source=[]
for i in sorted(keep):
 l=lines[i]
 if re.search(r'password|secret|token|DSN',l,re.I):l='[credential-related line omitted]'
 source.append(str(i+1)+': '+l)
units=subprocess.check_output(['systemctl','list-timers','--all','--no-pager']).decode();units=[l for l in units.splitlines() if any(x in l.lower() for x in ['http','scope','broker'])]
print(json.dumps({'path':str(p),'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'source':source,'timers':units}))
