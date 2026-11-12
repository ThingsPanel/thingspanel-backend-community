import pathlib,json,time,os
p=pathlib.Path('/tmp/qa-q06-http-direct-j2db7osw/result.json')
end=time.time()+15
while not p.exists() and time.time()<end:time.sleep(1)
out={'direct':json.loads(p.read_text()) if p.exists() else {'no_result':True}}
r=pathlib.Path('/opt/tp-community-main-20261028/ha-runtime')
out['ha_candidate_paths']=[str(x) for x in r.rglob('*') if x.is_file() and len(x.relative_to(r).parts)<=3 and any(k in x.name.lower() for k in ['cred','demo','token','secret','config'])][:70]
print(json.dumps(out))
