import pathlib,subprocess,json,urllib.request
root=pathlib.Path('/opt/tp-community-actions');sha='d53b0dfe7f470d8ee5ead5c5753024923367f3ee'
v=json.load(urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=5))
assert v['data']['version']=='dev-'+sha
p=subprocess.run(['python3',str(root/'incoming/deploy_encore.py'),sha],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=700)
if p.returncode:
 print(json.dumps({'status':'failed','step':'encore-activation','stdout':p.stdout[-3000:],'error':p.stderr[-4000:]}));raise SystemExit(1)
r=json.loads((root/'encore-releases'/sha/'deployment.json').read_text())
print(json.dumps({'status':'encore-main-deployed','commit':r['commit'],'image_id':r['image_id'],'migrations_applied':r['migrations_applied']}))
