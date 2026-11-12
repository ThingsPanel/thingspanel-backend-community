import subprocess,json,datetime
out={'at':datetime.datetime.utcnow().isoformat()+'Z','mode':'readonly timer unit definitions','units':{}}
for unit in ['tp-http-scope.timer','tp-ha-scope.timer']:
 p=subprocess.run(['systemctl','cat','--no-pager',unit],stdout=subprocess.PIPE,stderr=subprocess.PIPE,universal_newlines=True,timeout=5)
 out['units'][unit]={'returncode':p.returncode,'unit_definition':p.stdout}
print(json.dumps(out))
