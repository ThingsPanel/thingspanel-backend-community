import pathlib,hashlib,json,subprocess,urllib.request
root=pathlib.Path('/opt/tp-community-actions/mqtt-publisher-204b48feb04ee7a85b1788ba361f8cfdab88f5a5-stage-20261113-lockfix')
tool=root/'mqtt_publisher_release.py'
assert hashlib.sha256(tool.read_bytes()).hexdigest()=='e627db91ed1f0e8cc2cdeaac3c7d9e330e4e7d7034c6fc1ffe11e2016ba49fa2'
result={'operation':'approved-plugin-lock-fix-apply','directory':str(root)}
p=subprocess.run(['python3',str(tool),'apply',str(root)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=360)
if p.returncode:
 result['status']='apply-failed';result['error']=p.stderr.strip().splitlines()[-1] if p.stderr.strip() else 'apply process failed'
else:result.update(json.loads(p.stdout))
try:
 with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=10) as r:result['sys_version']=json.load(r)
except Exception as e:result['version_read_error']=type(e).__name__
print(json.dumps(result))
