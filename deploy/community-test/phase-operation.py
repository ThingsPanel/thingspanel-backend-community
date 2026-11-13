import json,pathlib,subprocess
p=pathlib.Path('/etc/nginx/conf.d/community.conf')
lines=[v.strip() for v in p.read_text().splitlines() if any(k in v for k in ['location','proxy_pass','upstream','server 127.','listen '])]
print(json.dumps({'routes':lines,'backend_health':subprocess.run(['curl','-fsS','--max-time','5','http://127.0.0.1:9999/health'],stdout=subprocess.PIPE,stderr=subprocess.PIPE).returncode==0}))
