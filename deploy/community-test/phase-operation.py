import json,subprocess,urllib.request,urllib.error
def cmd(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
 return {'exit':p.returncode,'stdout':p.stdout.decode(errors='replace')[:16000]}
out={'containers':cmd(['docker','ps','--format','{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}']),'memory':cmd(['free','-m']),'disk':cmd(['df','-h','/opt']),'http':[]}
for port,path in [(19090,'/health'),(19091,'/health'),(1505,'/health'),(503,'/health'),(8481,'/health')]:
 try:
  r=urllib.request.urlopen('http://127.0.0.1:'+str(port)+path,timeout=4)
  out['http'].append({'port':port,'status':r.status,'body':r.read(500).decode(errors='replace')})
 except urllib.error.HTTPError as e:out['http'].append({'port':port,'status':e.code})
 except Exception as e:out['http'].append({'port':port,'error':type(e).__name__})
print(json.dumps(out))
