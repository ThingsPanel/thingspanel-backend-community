import json,pathlib
print(json.dumps(json.loads(pathlib.Path('/tmp/qa-q06-upgrade-direct-83rzgd3p/result.json').read_text())))
