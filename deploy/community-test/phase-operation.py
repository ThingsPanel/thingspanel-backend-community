import json,pathlib,subprocess,os,fcntl,time,urllib.request
root=pathlib.Path('/opt/tp-community-actions');compose=pathlib.Path('/opt/tp-community-main-20261028/compose.encore.private.json')
os.umask(0o077)
with (root/'deployment.lock').open('a') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX)
 old=compose.read_bytes();cfg=json.loads(old)
 expected='sha256:0792ed39bfbad709484e5279b8130d2b3e27e84e70b59c427ba6d46e12761a26'
 info=json.loads(subprocess.check_output(['docker','inspect','tp-community-main-47-encore-api-1']))[0]
 assert info['Image']==expected and info['State']['Running']
 backup=root/'native-workers.private.before.json'
 if not backup.exists():backup.write_bytes(old);backup.chmod(0o600)
 env=cfg['services']['encore-api']['environment']
 env['TP_BACKGROUND_WORKERS']='on';env['TP_RUNTIME_OUTBOX_DISPATCH']='on'
 temp=compose.with_name('compose.native-workers.private.json');temp.write_text(json.dumps(cfg));temp.chmod(0o600)
 check=subprocess.run(['docker','compose','-f',str(temp),'config','--quiet'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 assert check.returncode==0,'Worker compose validation failed'
 os.replace(str(temp),str(compose))
 args=['docker','compose','-f',str(compose),'up','-d','--no-deps','encore-api']
 result=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120)
 good=False
 if result.returncode==0:
  for n in range(40):
   try:
    data=json.load(urllib.request.urlopen('http://127.0.0.1:14000/v1/health',timeout=3))
    if data.get('status')=='ok':good=True;break
   except Exception:pass
   time.sleep(1)
 if not good:
  compose.write_bytes(old);subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120);raise RuntimeError('Native workers activation failed and previous config restored')
 print(json.dumps({'status':'enabled','image_id':expected,'workers':['metadata-background','runtime-outbox']}))
