import json,pathlib,subprocess,urllib.request
sha='7c66feee6c60e0d65e277c1571609e615ce42672'
root=pathlib.Path('/opt/tp-community-actions')
backend=json.loads((root/'backend-releases'/sha/'result.json').read_text())
native=json.loads((root/'encore-releases'/sha/'deployment.json').read_text())
assert backend['commit']==native['commit']==sha
with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=10) as response:version=json.load(response)['data']['version']
assert version=='dev-'+sha
with urllib.request.urlopen('http://127.0.0.1:14000/v1/health',timeout=10) as response:health=json.load(response)
assert health['status']=='ok'
containers=json.loads(subprocess.check_output(['docker','inspect','tp-community-main-47-encore-api-1','tp-community-main-47-notification-core-1']))
services=[]
for c in containers:
 assert c['State']['Running'] and c['Config']['Image']=='tp-community-encore:source-'+sha
 services.append({'name':c['Name'],'image':c['Config']['Image'],'running':c['State']['Running']})
print(json.dumps({'status':'PASS','backend_commit':sha,'native_commit':sha,'backend_version':version,'native_health':health['status'],'private_config_unchanged':backend['private_config_unchanged'],'services':services}))
