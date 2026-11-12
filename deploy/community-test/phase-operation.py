import pathlib,hashlib,json,subprocess,urllib.request
root=pathlib.Path('/opt/tp-community-actions/mqtt-publisher-1af192946db407e05909827ce627f1f82755640e-stage-20261113-qa-fixes')
tool=root/'mqtt_publisher_release.py'
assert hashlib.sha256(tool.read_bytes()).hexdigest()=='fa7f0f0efed0741403ea2dfe95e3c32f93e3a57bbf77827458f551ad6e448b21'
result={'operation':'approved-diagnostics-http-deadline-apply','directory':str(root)}
p=subprocess.run(['/usr/bin/python3.11',str(tool),'apply',str(root)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=360)
if p.returncode:
 result['status']='apply-failed';result['error']=p.stderr.strip().splitlines()[-1] if p.stderr.strip() else 'apply process failed'
else:result.update(json.loads(p.stdout))
try:
 with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=10) as r:result['sys_version']=json.load(r)
except Exception as e:result['version_read_error']=type(e).__name__
print(json.dumps(result))

