import json,pathlib
p=pathlib.Path('/tmp/qa-q06-upgrade-direct-fu2vwbhq/result.json')
d=json.loads(p.read_text())
print(json.dumps(d))
