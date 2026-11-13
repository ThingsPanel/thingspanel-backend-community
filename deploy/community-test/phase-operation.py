import json,pathlib,subprocess,os,fcntl
os.umask(0o077)
root=pathlib.Path('/opt/tp-community-actions')
with (root/'deployment.lock').open('a') as lock:
 fcntl.flock(lock,fcntl.LOCK_EX)
 p=pathlib.Path('/etc/nginx/conf.d/community.conf')
 old=p.read_text()
 block='    # Main native device APIs\n    location ~ ^/api/v2/(devices|device-access-templates|device-groups|device-group-tree|device-statistics|device-number-validations|metadata-operations|metadata-downloads|reading-exports|processing-script-tests|file-assets|connection-providers)(/|$) {\n        proxy_pass http://127.0.0.1:14000;\n        proxy_set_header Authorization $http_authorization;\n        proxy_set_header x-token $http_x_token;\n        proxy_set_header Host $host;\n    }\n'
 if block not in old:
  assert old.count('location /api {')==1
  backup=root/'community.before-native-routes.conf'
  if not backup.exists(): backup.write_text(old)
  p.write_text(old.replace('location /api {',block+'    location /api {'))
  check=subprocess.run(['nginx','-t'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  if check.returncode:
   p.write_text(old);raise RuntimeError('nginx validation failed; restored previous file')
  subprocess.check_call(['nginx','-s','reload'])
 print(json.dumps({'native_api_routes_enabled':True,'legacy_routes_preserved':True}))
