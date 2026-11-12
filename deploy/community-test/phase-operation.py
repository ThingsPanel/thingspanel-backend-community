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


import time
started_utc=datetime.datetime.utcnow().isoformat()+'Z'
out['rule_id']='c67841b0-59ee-4308-af75-45c313deb12d';out['steps']=[]
for value in [38,38,20]:
 r=call(ha,'/api/states/sensor.qa_q08_model_20261112',{'state':str(value),'attributes':{'friendly_name':'QA-Q08-MODEL-20261112','unit_of_measurement':'C','device_class':'temperature'}},auth=ha_token)
 time.sleep(3)
 notice=call('http://c.thingspanel.cn','/api/v2/notifications?page=1&pageSize=50')
 entries=notice.get('result',{}).get('data',{}).get('items',[])
 out['steps'].append({'at':datetime.datetime.now().isoformat(),'input':value,'notifications':[i for i in entries if i.get('createdAt','')>=started_utc],'ha_status':r.get('http_status'),'ha_state':r.get('result',{}).get('state'),'current':call(base,'/api/v1/telemetry/datas/current/0f68b2e0-37b3-79f3-7ebd-4d2ffaf732a8'),'alarms':call('http://c.thingspanel.cn','/automation-core/v1/alarms?policy_id='+out['rule_id']+'&limit=20')})
for step in out['steps']:
 pass
r=call('http://c.thingspanel.cn','/api/v2/notifications?page=1&pageSize=50');items=r.get('result',{}).get('data',{}).get('items',[]);out['notifications']=[i for i in items if i.get('createdAt','')>=started_utc]
out['notification_details']={i['id']:call('http://c.thingspanel.cn','/api/v2/notifications/'+i['id']) for i in out['notifications']}
expected=pathlib.Path('/tmp/qa-q08-session-gk9qwhrb');out['temporary_session_cleanup']=[]
if key.parent.resolve()==expected.resolve():
 for name in ['fixture.private.json','private.pem','public.pem']:
  target=expected/name
  if target.is_file():target.unlink();out['temporary_session_cleanup'].append(name)
 try:expected.rmdir();out['temporary_directory_removed']=True
 except OSError:out['temporary_directory_removed']=False
print(json.dumps(out))
