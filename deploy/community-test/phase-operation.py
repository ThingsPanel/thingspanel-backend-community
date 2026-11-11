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
def row():return json.loads(subprocess.check_output(['docker','exec','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel','-c',sql],timeout=20))
before=row()
if len(before)!=1:raise RuntimeError('QA attribute fixture missing')
code=request('attribute/datas/'+before[0]['id'],token,method='DELETE').get('code')
if code!=201001 or row()!=before:raise RuntimeError('Attribute mutation isolation failed')
out.append({'operation':'foreign_attribute_delete','code':code,'fixture_unchanged':True})
print(json.dumps({'checks':out,'status':'passed'}))
