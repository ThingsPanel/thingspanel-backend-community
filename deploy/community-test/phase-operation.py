import pathlib,json,subprocess,hashlib,re
out={}
for unit in ['tp-http-scope.service','tp-ha-scope.service']:
 x=subprocess.check_output(['systemctl','show',unit,'--property=ExecStart','--value']).decode();out[unit]={'exec':x.strip()}
 match=re.search(r'(/[^ ;]+scope[^ ;]*\.py)',x)
 if not match:continue
 p=pathlib.Path(match.group(1));lines=p.read_text().splitlines();safe=[]
 for i,l in enumerate(lines):
  if re.search('password|secret|token|DSN',l,re.I) and not ('creds[' in l or 'mqtt.private.json' in l):l='[credential-related line omitted]'
  safe.append(str(i+1)+': '+l)
 out[unit].update({'path':str(p),'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'source':safe})
print(json.dumps(out))
