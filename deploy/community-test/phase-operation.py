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


fixture=json.loads((key.parent/'fixture.private.json').read_text());original=fixture.get('light_before');original=original.get('state') if isinstance(original,dict) else original
before=call(ha,'/api/states/light.office_rgbw_lights',auth=ha_token);out['light_original']=original;out['light_before']=before.get('result',{}).get('state')
if original=='on' and out['light_before']=='off':
 r=call(ha,'/api/services/light/turn_on',{'entity_id':'light.office_rgbw_lights'},auth=ha_token);out['cleanup_direct_ha_status']=r.get('http_status')
import time;time.sleep(1)
after=call(ha,'/api/states/light.office_rgbw_lights',auth=ha_token);out['light_after']=after.get('result',{}).get('state');out['cleanup_only_not_platform_control']=True
entity='sensor.qa_q08_model_20261112';r=call(ha,'/api/states/'+entity,{'state':'25','attributes':{'friendly_name':'QA-Q08-MODEL-20261112','unit_of_measurement':'C','device_class':'temperature'}},auth=ha_token);out['new_qa_sensor']={'entity':entity,'http':r.get('http_status'),'state':r.get('result',{}).get('state')}
print(json.dumps(out))
