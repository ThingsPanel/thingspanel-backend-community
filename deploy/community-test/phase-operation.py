import json,pathlib,subprocess,urllib.request,hashlib
backend='d53b0dfe7f470d8ee5ead5c5753024923367f3ee'
frontend='f99a71549ee43ed105252a8ea4863e1f2d0e2f10'
root=pathlib.Path('/opt/tp-community-actions')
v=json.load(urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=10))
assert v['data']['version']=='dev-'+backend,'Backend main revision is not running'
r=json.loads((root/'encore-releases'/backend/'deployment.json').read_text())
assert r['commit']==backend
for name in ['tp-community-main-47-encore-api-1','tp-community-main-47-notification-core-1']:
 i=json.loads(subprocess.check_output(['docker','inspect',name]))[0]
 assert i['State']['Running'] and i['Image']==r['image_id']
s=root/'incoming/deploy_frontend.py'
assert hashlib.sha256(s.read_bytes()).hexdigest()=='4f4fa73e36186c7aa1136ea6c600c481ae60a781466dbd10a9d98f3098f75e4d'
p=subprocess.run(['python3',str(s),frontend],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=180)
if p.returncode:
 print(json.dumps({'status':'failed','step':'frontend-activation','output':p.stdout[-4000:],'error':p.stderr[-2000:]}));raise SystemExit(1)
req=urllib.request.Request('http://127.0.0.1/SOURCE_COMMIT',headers={'Host':'c.thingspanel.cn','Cache-Control':'no-cache'})
assert urllib.request.urlopen(req,timeout=10).read().decode().strip()==frontend
print(json.dumps({'status':'main-stack-deployed','frontend':frontend,'backend':backend,'encore_image':r['image_id']}))
