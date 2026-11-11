import json,pathlib,subprocess

def run(args,timeout=60):
    p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
    if p.returncode:raise RuntimeError('Read-only resource inspection failed: '+args[0])
    return p.stdout.decode()

roots=['/opt/tp-community-actions','/root/.cache','/var/log','/home/thingspanel','/opt/tp-community-main-20261028']
out={'memory':run(['free','-b']),'disk':run(['df','-B1','/opt']),
     'containers':run(['docker','stats','--no-stream','--format','{{.Name}} {{.MemUsage}} {{.CPUPerc}}']),
     'processes':run(['ps','-eo','pid,comm,rss','--sort=-rss']),
     'sizes':{p:run(['du','-x','-B1','--max-depth=1',p]) for p in roots if pathlib.Path(p).exists()},
     'swaps':pathlib.Path('/proc/swaps').read_text()}
q="SELECT json_build_object('shared_buffers',current_setting('shared_buffers'),'work_mem',current_setting('work_mem'),'maintenance_work_mem',current_setting('maintenance_work_mem'),'max_connections',current_setting('max_connections'),'connections',(SELECT count(*) FROM pg_stat_activity),'active_queries',(SELECT count(*) FROM pg_stat_activity WHERE state='active'),'rehearsal_connections',(SELECT count(*) FROM pg_stat_activity WHERE datname LIKE 'tp_upgrade_rehearsal_%'))"
out['postgres']=json.loads(run(['docker','exec','timescaledb','psql','-X','-q','-At','-U','postgres','-d','ThingsPanel','-c',q]))
print(json.dumps(out))
