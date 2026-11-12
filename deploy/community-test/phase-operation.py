import json,pathlib,base64,subprocess,urllib.request,urllib.error,urllib.parse,datetime
enc={'chunks': ['M/WCA5cGMG3od4xHgmv2BovIQeJyhaNfMw9jnxo9UIuGxOJrxYiG6C0Af32yMQKUwNnsjazEd/601hyrZU1lExu+6Sy6DLUGYKUsXX3LN82GJvWiJkY9NhajzUfezR1bwx3ZUc6jfsoHvjLY7AtWjug65jLU9bEM4n83LZhbGn4CEMl5wMoVbsEhQoFR3FKdIqRBDunEgiYFYjQGyZIVWo+CHL8GcEHevWw1e4WNX0sJtNmT2DQc08TxMtM0jxJHGScUhW0DBd10xw4KKS+we5zPs05jFp8NLOfwdQP/WCpughCGnjuZDwkQwV4ZHrGoeL7G2JVbWDlbDRwt+XNBsw==', 'thSOC/FiGkJL4V2HpIAHltXEnk2WojpLUGE/6u0Qbspsl2Q8z+PHAfBboNADZOWNrQiEBWmO7oXwWEleyaJ5wZIhjmq1qTcQ0k3MDROGxcB8zcDARl8xrd6sBhkZhxbPextVQi7Qj42pdJZDZ1/b/iub29y66FDzeQ8+zULVU+U56SG87IwKuY6E9qLXOjr7l9DKOZnNCO0VF5mrVCYi7PANv8fzzuIluAkqSqr0UJqQWd2lyqCREiLsYz4yh28skhLL0tzCUw7d9lXOETrSJQf8tN+HrNMhN3397MvFs1F2TgpzXZMZcjHPl+BDePFSqs76qevTvN0qZkcSzV891Q=='], 'key_directory': '/tmp/qa-q08-session-gk9qwhrb'}
key=pathlib.Path(enc['key_directory'])/'private.pem'
token=b''.join(subprocess.check_output(['openssl','pkeyutl','-decrypt','-inkey',str(key),'-pkeyopt','rsa_padding_mode:oaep','-pkeyopt','rsa_oaep_md:sha256','-pkeyopt','rsa_mgf1_md:sha256'],input=base64.b64decode(c)) for c in enc['chunks']).decode()
cred=json.loads(pathlib.Path('/opt/tp-community-main-20261028/ha-runtime/ha-demo.credentials.private.json').read_text());ha_token=cred['long_lived_token'];out={'at':datetime.datetime.now().isoformat()}
def call(base,path,body=None,auth=None,method=None):
 req=urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,headers={'Content-Type':'application/json','Authorization':'Bearer '+(auth or token),'x-token':auth or token},method=method)
 try:
  with urllib.request.urlopen(req,timeout=15) as r:return {'http_status':r.status,'result':json.load(r)}
 except urllib.error.HTTPError as e:return {'http_status':e.code,'error':e.read().decode()[:300].replace(ha_token,'[redacted]').replace(token,'[redacted]')}
 except Exception as e:return {'error':type(e).__name__}
base='http://127.0.0.1:9999';ha='http://127.0.0.1:58123'

light=call(ha,'/api/states/light.office_rgbw_lights',auth=ha_token);out['light_after_command']={'status':light.get('http_status'),'state':light.get('result',{}).get('state')}
out['current']={}
ids=['bb9e5ff3-d73e-6efe-5dda-ae216fb44179','147d134e-6b82-2aea-e2b4-2424011ed23a','40fd7d06-6f29-6903-9dc0-4e736357c34f'];out['service_scope']=[]
p=pathlib.Path('/opt/tp-community-main-20261028/active-broker-services.private.json');policies=json.loads(p.read_text());policies=policies if isinstance(policies,list) else policies.get('services',[])
for item in policies:
 covered=[i for i in ids if i in item.get('allowed_device_ids',[])];out['service_scope'].append({'role':item.get('role'),'plugin_socket':item.get('plugin_socket',False),'target_scoped':item.get('target_scoped_publisher',False),'targets_count':len(item.get('allowed_device_ids',[])),'qa_covered':covered})
for id in ids:out['current'][id]=call(base,'/api/v1/telemetry/datas/current/'+id)
import re
out['plugin_logs']=[]
for cid in subprocess.check_output(['docker','ps','-q']).decode().split():
 data=json.loads(subprocess.check_output(['docker','inspect',cid]))[0];aliases=sum([(v.get('Aliases') or []) for v in data['NetworkSettings']['Networks'].values()],[])
 if not any(a in aliases for a in ['tp-plugin-homeassistant','tp-plugin-http-adapter']):continue
 p=subprocess.run(['docker','logs','--since','45m','--tail','1200',cid],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=10)
 lines=p.stdout.decode(errors='replace').splitlines();selected=[]
 for line in lines:
  if any(w in line for w in ['35180','qa_q08_','bb9e5ff3','147d134e','40fd7d06','direct control failed']):
   if re.search('secret|token|voucher|credential|password',line,re.I):continue
   selected.append(line[:700])
 out['plugin_logs'].append({'name':data['Name'],'image':data['Config']['Image'],'matches':selected[-30:]})
print(json.dumps(out))
