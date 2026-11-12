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

# Restore only the previously documented demo lamp state, through the platform.
import time
fixture=json.loads((key.parent/'fixture.private.json').read_text())
prior=fixture.get('light_before')
# Do not guess fixture shape or change an unexpected device state.
if isinstance(prior,dict):prior=prior.get('state')
current=call(ha,'/api/states/light.office_rgbw_lights',auth=ha_token)
out['original_state']=prior
out['before_state']=current.get('result',{}).get('state')
if prior!='on':out['blocked']='original state not verified as on'
elif out['before_state']=='on':out['restore']='already at original state; no command'
elif out['before_state']!='off':out['blocked']='unexpected live state; no command'
else:
 r=call(base,'/api/v1/command/datas/pub',{'device_id':'40fd7d06-6f29-6903-9dc0-4e736357c34f','identify':'switch','value':'true','idempotency_key':'qa-q08-restore-office-firstpass-v2'})
 data=r.get('result',{}).get('data') or {};mid=data.get('message_id');out['submit']={'http_status':r.get('http_status'),'code':r.get('result',{}).get('code'),'message_id':mid,'status':data.get('status')}
 if mid:
  for _ in range(10):
   time.sleep(1)
   status=call(base,'/api/v1/command/datas/status/'+mid)
   d=status.get('result',{}).get('data',{});out['command_status']={k:d.get(k) for k in ['message_id','status','error_message']}
   actual=call(ha,'/api/states/light.office_rgbw_lights',auth=ha_token);out['after_state']=actual.get('result',{}).get('state')
   if out['after_state']=='on' and d.get('status')=='device_reported_success':break
out['sensor_sequence']=[]
for av,bv in [(20,31),(38,32)]:
 row={'input':[av,bv],'ha':[],'platform':[]}
 for entity,value in [('sensor.qa_q08_temperature_20261112',av),('sensor.qa_q08_compare_20261112',bv)]:
  r=call(ha,'/api/states/'+entity,{'state':str(value),'attributes':{'friendly_name':'QA-Q08-'+entity,'unit_of_measurement':'C'}},auth=ha_token)
  row['ha'].append({'http':r.get('http_status'),'state':r.get('result',{}).get('state')})
 time.sleep(3)
 for id in ['bb9e5ff3-d73e-6efe-5dda-ae216fb44179','147d134e-6b82-2aea-e2b4-2424011ed23a']:
  row['platform'].append(call(base,'/api/v1/telemetry/datas/current/'+id))
 out['sensor_sequence'].append(row)
print(json.dumps(out))
