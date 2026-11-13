import json,pathlib,subprocess
root=pathlib.Path('/opt/tp-community-main-20261028')
r=json.loads((root/'runtime.encore.private.json').read_text())
c=json.loads((root/'compose.encore.private.json').read_text())
print(json.dumps({'hosted_services':r.get('hosted_services'), 'servers':[{'host':s.get('host'),'databases':{k:{'name':v.get('name'),'username':v.get('username'),'credential_kind':type(v.get('password')).__name__} for k,v in s.get('databases',{}).items()}} for s in r['sql_servers']], 'services':{k:{'environment_keys':list(v.get('environment',{})),'volumes':v.get('volumes',[]),'image':v.get('image')} for k,v in c['services'].items()}}))
