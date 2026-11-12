import pathlib,json,re,urllib.request,subprocess
out={}
r=pathlib.Path('/home/plugin/access-service-modbus-tcp')
for f in ['internal/service/modbus_service.go','internal/modbus/connection/retry.go']:
 p=r/f;lines=p.read_text(errors='replace').splitlines();selected=[]
 for i,l in enumerate(lines):
  if any(s in l for s in ['deviceSyncLoop','NewTicker','time.Minute','time.Second','retryInterval','maxRetries','MaxRetries']):
   selected.extend(str(j+1)+': '+lines[j] for j in range(max(0,i-3),min(len(lines),i+12)) if not re.search('secret|password|token',lines[j],re.I))
 out[f]=list(dict.fromkeys(selected))
for root in ['/home/plugin','/home/thingspanel','/opt']:
 p=pathlib.Path(root)
 out[root]=[str(x) for x in p.glob('*') if 'http' in x.name.lower()]
for port,path in [(1506,'/api/monitor/stats'),(19090,'/health'),(19091,'/health'),(8481,'/health')]:
 try:
  u=urllib.request.urlopen('http://127.0.0.1:'+str(port)+path,timeout=2); raw=u.read();out[str(port)]=json.loads(raw) if port==1506 else {'status':u.status,'body':raw[:100].decode(errors='replace')}
 except Exception as e:out[str(port)]=type(e).__name__
print(json.dumps(out))
