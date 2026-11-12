import pathlib,json,time,os
p=pathlib.Path('/tmp/qa-q06-http-direct-ytbyq507/result.json');end=time.time()+35
while not p.exists() and time.time()<end:time.sleep(1)
print(json.dumps({'direct':json.loads(p.read_text()) if p.exists() else {'no_result_yet':True},'listener_pid_exists':pathlib.Path('/proc/2785021').exists(),'log_bytes':pathlib.Path('/tmp/qa-q06-http-direct-ytbyq507/server.log').stat().st_size}))
