import json,pathlib
print(json.dumps(json.loads(pathlib.Path('/tmp/qa-q06-upgrade-direct-2mb72cu5/result.json').read_text())))
