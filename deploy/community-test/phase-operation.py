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

f=json.loads(pathlib.Path(enc['key_directory']).joinpath('fixture.private.json').read_text());voucher=f['voucher'];out['pages']=[]
for page in [1,2,3]:
 out['pages'].append(call(base,'/api/v1/service/access/device/list?'+urllib.parse.urlencode({'page':page,'page_size':10,'voucher':voucher})))
for entity,value in [('sensor.qa_q08_temperature_20261112','38'),('sensor.qa_q08_compare_20261112','32')]:
 r=call(ha,'/api/states/'+entity,{'state':value,'attributes':{'friendly_name':'QA-Q08-'+entity.split('.')[1],'unit_of_measurement':'°C','device_class':'temperature'}},ha_token);out[entity]={'status':r.get('http_status'),'state':r.get('result',{}).get('state')}
light=call(ha,'/api/states/light.office_rgbw_lights',auth=ha_token);out['light_before']={'status':light.get('http_status'),'state':light.get('result',{}).get('state')};f['light_before']=out['light_before']['state'];pathlib.Path(enc['key_directory']).joinpath('fixture.private.json').write_text(json.dumps(f))
import time;time.sleep(4)
out['telemetry']={}
for id in ['bb9e5ff3-d73e-6efe-5dda-ae216fb44179','147d134e-6b82-2aea-e2b4-2424011ed23a']:
 out['telemetry'][id]=call(base,'/api/v1/telemetry/datas/current/'+id)
print(json.dumps(out))
