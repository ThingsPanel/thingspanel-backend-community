import json,pathlib,subprocess,re,urllib.request,yaml
pm=json.loads(subprocess.check_output(['/root/.local/share/pnpm/pm2','jlist']))
p=next(x for x in pm if x['name']=='thingspanel-backend-community');e=p['pm2_env']
secret=[]
def walk(v,key=''):
 if isinstance(v,dict):
  for k,x in v.items():walk(x,str(k))
 elif isinstance(v,list):
  for x in v:walk(x,key)
 elif isinstance(v,str) and len(v)>4 and re.search('pass|token|secret|jwt|keyring',key,re.I):secret.append(v)
walk(e);walk(yaml.safe_load(pathlib.Path('/home/thingspanel/thingspanel-backend-community/configs/conf.yml').read_text()))
logs=[]
for key in ['pm_err_log_path','pm_out_log_path']:
 path=pathlib.Path(e[key])
 if path.exists():
  raw=path.read_bytes()[-3000000:].decode(errors='replace')
  lines=raw.splitlines()
  selected=[v for v in lines if re.search(r'2026-11-13 14:(19|20|21):',v) and not re.search(r'broker state projection|message dispatch|ingress denied',v)]
  text='\n'.join(selected[-80:])
  for value in sorted(secret,key=len,reverse=True):text=text.replace(value,'[redacted]')
  text=re.sub(r'(?i)(password|token|secret|jwt.key)[=: ]+[^ ,;]+',r'\1=[redacted]',text)
  logs.append({'kind':key,'errors':text[-9000:]})
try:
 v=json.load(urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=5)).get('data',{}).get('version')
except Exception:v='unavailable'
print(json.dumps({'pid':p.get('pid'),'status':e.get('status'),'version':v,'logs':logs}))
