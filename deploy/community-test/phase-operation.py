import json,subprocess,pathlib
r=subprocess.check_output(['docker','ps','--format','{{json .}}']).decode()
print(json.dumps({'containers':[{'name':x.get('Names'),'image':x.get('Image'),'ports':x.get('Ports')} for x in [json.loads(v) for v in r.splitlines()]],'storage_files':[str(x) for x in pathlib.Path('/opt/tp-community-main-20261028').glob('*storage*')]}))
