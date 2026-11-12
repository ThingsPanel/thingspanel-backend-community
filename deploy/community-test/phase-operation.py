import base64,hashlib,json,pathlib,subprocess,os
os.umask(0o077)
commit='7da24b0d2bf75e987c5aaaa092a0d60acc784055'
mode='plan'
script_bytes=base64.b64decode(ENCODED)
expected_tool='f97b2794266be5ed43a0cd8e31472996928ab9eeb80a2bed395f51dc11998934'
expected_archive=None
root=pathlib.Path('/opt/tp-community-actions')
archive=root/'incoming'/('control-plane-frontend-'+commit+'.tar.gz')
tools=root/'qa-frontend-tools';tools.mkdir(exist_ok=True,mode=0o700)
path=tools/('release-'+commit+'.py')
def digest(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  for b in iter(lambda:f.read(1024*1024),b''):h.update(b)
 return h.hexdigest()
result={'operation':'frontend-qa-'+mode,'commit':commit}
try:
 assert hashlib.sha256(script_bytes).hexdigest()==expected_tool
 assert not path.is_symlink()
 if path.exists():assert digest(path)==expected_tool
 else:path.write_bytes(script_bytes)
 assert archive.is_file() and not archive.is_symlink()
 result['archive_sha256']=digest(archive)
 if expected_archive:assert result['archive_sha256']==expected_archive,'Candidate archive changed after plan'
 args=['/usr/bin/python3.11',str(path)]
 if mode=='apply':args+=['--apply','--bundle',str(archive),'--bundle-sha256',expected_archive]
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=150)
 result['exit_code']=p.returncode
 if p.returncode:result.update(status='FAIL',error=p.stderr.strip().splitlines()[-1] if p.stderr.strip() else 'operation failed')
 else:result.update(status='PASS',result=json.loads(p.stdout))
except Exception as e:result.update(status='FAIL',error=type(e).__name__+': '+str(e))
print(json.dumps(result))
