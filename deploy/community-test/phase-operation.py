import json,pathlib
p=pathlib.Path('/tmp/qa-q06-upgrade-direct-qlkwmotp/result.json')
print(json.dumps(json.loads(p.read_text())))
