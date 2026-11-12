import json,subprocess,pathlib,re
out={}
p=subprocess.run(['/root/.local/share/pnpm/pm2','jlist'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
out['pm2']=[]
for x in json.loads(p.stdout):
 e=x.get('pm2_env',{});name=x.get('name','')
 if any(k in name.lower() for k in ['http','modbus','chirp']):
  i={'name':name,'status':e.get('status'),'restarts':e.get('restart_time'),'cwd':e.get('pm_cwd'),'executable':e.get('pm_exec_path'),'logs':[]}
  for k in ['pm_out_log_path','pm_err_log_path']:
   path=e.get(k)
   if path and pathlib.Path(path).is_file():
    s='\n'.join(pathlib.Path(path).read_text(errors='replace').splitlines()[-300:])
    i['logs'].append({'kind':k,'counts':{v:s.lower().count(v.lower()) for v in ['401','403','unauthorized','forbidden','connection refused','invalid','panic:','GetDevice','16fb8743','device list','设备列表','获取设备','认证']}})
  out['pm2'].append(i)
p=subprocess.run(['ss','-lntp'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
out['listeners']=[l for l in p.stdout.decode().splitlines() if re.search(r':(19090|19091|1505|503|502|35102)\b',l)]
print(json.dumps(out))
