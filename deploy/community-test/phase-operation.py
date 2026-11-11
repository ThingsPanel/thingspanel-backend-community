import json,subprocess,pathlib
def run(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 if p.returncode:raise RuntimeError('Read-only capacity inspection failed')
 return p.stdout.decode()
out={'disk':run(['df','-B1','/opt']),'memory':run(['free','-b']), 'containers':run(['docker','ps','--format','{{.Names}} {{.Image}} {{.Ports}}']), 'docker_usage':run(['docker','system','df'])}
q="SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT datname,pg_database_size(oid) bytes FROM pg_database WHERE NOT datistemplate ORDER BY datname)t"
out['databases']=json.loads(run(['docker','exec','timescaledb','psql','-X','-q','-At','-U','postgres','-d','ThingsPanel','-c',q]))
print(json.dumps(out))
