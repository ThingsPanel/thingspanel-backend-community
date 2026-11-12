import pathlib,hashlib,json,subprocess,urllib.request
root=pathlib.Path('/opt/tp-community-actions/mqtt-publisher-5fb4c68e11b4ce3632abad32197a345e3d0f0f89-stage-20261112100920')
tool=root/'mqtt_publisher_release.py'
assert hashlib.sha256(tool.read_bytes()).hexdigest()=='70e2e2f16b09d4cc4a07777fc89b5265c41cb832d914e16c5b84fa16eac9199c'
result={'operation':'approved-mqtt-only-apply','directory':str(root)}
p=subprocess.run(['python3',str(tool),'apply',str(root)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=360)
if p.returncode:
 result['status']='apply-failed';result['error']=p.stderr.strip().splitlines()[-1] if p.stderr.strip() else 'apply process failed'
else:result.update(json.loads(p.stdout))
try:
 with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=10) as r:result['sys_version']=json.load(r)
except Exception as e:result['version_read_error']=type(e).__name__
print(json.dumps(result))
