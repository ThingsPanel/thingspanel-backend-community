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
states=call(ha,'/api/states',auth=ha_token);out['ha_status']=states.get('http_status')
if out['ha_status']!=200:print(json.dumps(out));raise SystemExit()
out['demo_light_entities']=[x['entity_id'] for x in states['result'] if x['entity_id'].startswith('light.')]
for entity,value in [('sensor.qa_q08_temperature_20261112','20'),('sensor.qa_q08_compare_20261112','31')]:
 r=call(ha,'/api/states/'+entity,{'state':value,'attributes':{'friendly_name':'QA-Q08-'+entity.split('.')[1],'unit_of_measurement':'°C','device_class':'temperature'}},ha_token);out[entity]={'status':r.get('http_status')}
voucher=json.dumps({'base_url':'http://tp-main47-ha-demo:8123','token':ha_token})
body={'operation_key':'qa-q08-ha-20261112-create-v1','name':'QA-Q08-HA-20261112','service_plugin_id':'b97f192e-a149-46e4-8b09-d088a1d6499d','voucher':voucher,'service_access_config':'{}'}
a=call(base,'/api/v1/service/access',body);out['create']=a
if a.get('result',{}).get('code')==200:
 sid=a['result']['data']['id'];state={'service_access_id':sid,'voucher':voucher};p=pathlib.Path(enc['key_directory'])/'fixture.private.json';p.write_text(json.dumps(state));p.chmod(0o600);out['fixture_path']=str(p)
 out['replay']=call(base,'/api/v1/service/access',body);conf=dict(body);conf['name']='QA-Q08-CONFLICT';out['conflict']=call(base,'/api/v1/service/access',conf)
 out['discovery']=call(base,'/api/v1/service/access/device/list?'+urllib.parse.urlencode({'page':1,'page_size':100,'voucher':voucher}))
print(json.dumps(out))
