import json,pathlib
p=pathlib.Path('/opt/tp-community-main-20261028/qa-platform-admin.private.json')
print(json.dumps({'exists':p.is_file(),'keys':list(json.loads(p.read_text())) if p.is_file() else []}))
