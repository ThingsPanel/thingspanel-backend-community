import pathlib,json,re,urllib.request
r=pathlib.Path('/home/plugin/access-service-modbus-tcp');out={}
for f,lo,hi in [('internal/platform/platform.go',1,95),('internal/platform/platform.go',470,510),('internal/bootstrap/platform.go',1,100),('internal/service/modbus_service.go',1,170),('internal/handler/handler.go',1,135),('go.mod',1,80)]:
 p=r/f
 if not p.exists():continue
 lines=p.read_text(errors='replace').splitlines();out[f+':'+str(lo)]=[str(i+1)+': '+l for i,l in enumerate(lines) if lo<=i+1<=hi and not re.search(r'password|secret|token|api.?key',l,re.I)]
for port in [1506,1507]:
 try:
  x=urllib.request.urlopen('http://127.0.0.1:'+str(port)+'/api/monitor/stats',timeout=2);out[str(port)]=json.loads(x.read())
 except Exception as e:out[str(port)]=type(e).__name__
print(json.dumps(out))
