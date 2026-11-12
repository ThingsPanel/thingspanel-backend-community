import pathlib,json,time
out={}
for name,path in [('HTTP_direct','/tmp/qa-q06-http-direct-ytbyq507/result.json'),('HA_faults','/tmp/qa-q08-ha-fault-lczmrqjv/result.json')]:
 p=pathlib.Path(path);out[name]=json.loads(p.read_text()) if p.exists() else {'missing':True}
print(json.dumps(out))
