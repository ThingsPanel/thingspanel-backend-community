import json,pathlib,subprocess,urllib.request,urllib.error
device='5ed19dd2-4422-dfe3-2a54-13e180f120ed'
qa=json.loads(pathlib.Path('/opt/tp-community-main-20261028/qa-tenant.private.json').read_text())
def request(path,token=None,data=None,method=None):
 headers={'Content-Type':'application/json'}
 if token:headers['x-token']=token
 req=urllib.request.Request('http://127.0.0.1:9999/api/v1/'+path,data=None if data is None else json.dumps(data).encode(),headers=headers,method=method)
 try:return json.load(urllib.request.urlopen(req,timeout=15))
 except urllib.error.HTTPError as e:return json.loads(e.read())
token=request('login',data={'email':qa['email'],'password':qa['password']}).get('data',{}).get('token')
if not token:raise RuntimeError('Isolated tenant login failed')
import base64
claims=json.loads(base64.urlsafe_b64decode(token.split('.')[1]+'==='))
if claims.get('tenant_id')=='63a4b529':raise RuntimeError('Negative test requires another tenant')
query='device_id='+device+'&page=1&page_size=10'
paths=['attribute/datas/'+device,'attribute/datas/key?device_id='+device+'&key=qa_firmware','attribute/datas/set/logs?'+query,'event/datas?'+query,'command/datas/set/logs?'+query,'telemetry/datas/current/'+device]
out=[]
for path in paths:
 code=request(path,token).get('code');out.append({'path':path,'code':code})
 if code!=201001:raise RuntimeError('Device read isolation failed: '+path)
sql="BEGIN READ ONLY; SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,device_id,key FROM attribute_datas WHERE device_id='"+device+"' AND key='qa_firmware')t; COMMIT;"
def row():return json.loads(subprocess.check_output(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel'],input=sql.encode(),timeout=20))
before=row()
if len(before)!=1:raise RuntimeError('QA attribute fixture missing')
code=request('attribute/datas/'+before[0]['id'],token,method='DELETE').get('code')
if code!=201001 or row()!=before:raise RuntimeError('Attribute mutation isolation failed')
out.append({'operation':'foreign_attribute_delete','code':code,'fixture_unchanged':True})
result={'checks':out,'status':'passed'}

expected_ledger=[{'module_name': 'auth', 'file_name': 'auth/001_identity.sql', 'checksum': '754c39ea31eada66a9197ebaa714f110b60f86857844e4b8d1499e56b98316b9', 'applied_at': '2026-10-28T17:01:36.86119+08:00'}, {'module_name': 'auth', 'file_name': 'auth/002_self_unlink.sql', 'checksum': '9073383a885af83010390016ebd67b7e22fc2484ffa19e8035b62cb67690142f', 'applied_at': '2026-10-28T17:01:36.86119+08:00'}, {'module_name': 'auth', 'file_name': 'auth/003_provider_settings.sql', 'checksum': 'c077232a1fae64c6cbf81be857e3de9b0f08b2c3832c3e70142b08fee34878ca', 'applied_at': '2026-10-28T17:01:36.86119+08:00'}, {'module_name': 'native-binding', 'file_name': '26.sql', 'checksum': '88e569c1a60f5a912f94443988747c623e17a0db923cbcc0f38d75ab1989dc9c', 'applied_at': '2026-10-28T17:01:37.091449+08:00'}, {'module_name': 'native-binding', 'file_name': '27.sql', 'checksum': 'd1b4171c0e097141752743b7f4b04802f443021b6b25cb98103f6358ba279275', 'applied_at': '2026-10-28T17:01:37.091449+08:00'}, {'module_name': 'automation-command-guard', 'file_name': '28.sql', 'checksum': 'e19c6aa7989a398c76dc8494c9441aa32e15ca6d82fe0ec3db219321113f24ff', 'applied_at': '2026-10-28T17:01:37.156948+08:00'}]
q="BEGIN READ ONLY; SELECT coalesce(json_agg(t),'[]'::json) FROM native_component_migration_ledger t; COMMIT;"
actual=json.loads(subprocess.check_output(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel'],input=q.encode(),timeout=20))
if sorted(actual,key=lambda v:(v['module_name'],v['file_name']))!=sorted(expected_ledger,key=lambda v:(v['module_name'],v['file_name'])):raise RuntimeError('Migration ledger differs from predeployment inspection')
result.update(ledger_matches_predeployment_inspection=True,ledger_entries=len(actual));print(json.dumps(result))
