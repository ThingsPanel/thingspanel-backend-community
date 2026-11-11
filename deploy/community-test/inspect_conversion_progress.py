import json,pathlib,subprocess,time
out={}
mem={line.split(':')[0]:line.split(':')[1].strip() for line in pathlib.Path('/proc/meminfo').read_text().splitlines()}
out['memory']={k:mem[k] for k in ('MemTotal','MemAvailable','SwapTotal','SwapFree')}
out['processes']=subprocess.check_output(['ps','-eo','comm,rss,pcpu','--sort=-rss']).decode().splitlines()[:12]
out['disk']=subprocess.check_output(['df','-h','/opt']).decode()
out['uptime']=pathlib.Path('/proc/uptime').read_text().split()[0]
query="SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT state,wait_event_type,wait_event,count(*) AS sessions,max(extract(epoch FROM clock_timestamp()-xact_start)) AS max_transaction_seconds FROM pg_stat_activity WHERE datname='tp_upgrade_rehearsal_20261111165830' GROUP BY state,wait_event_type,wait_event)t"
out['target_activity']=json.loads(subprocess.check_output(['docker','exec','timescaledb','psql','-X','-q','-At','-U','postgres','-c',query],timeout=30))
for name in ('device-identities-verification.json','device-identities-failure.private.log'):
 path=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/native-20261111165830')/name
 if path.exists():
  if name.endswith('.json'):out['receipt']=json.loads(path.read_text())
  else:
   log=path.read_text(errors='replace')
   out['error_categories']=[k for k in ('out of memory','signal: killed','context deadline exceeded','invalid source device row','invalid private conversion input','unresolved gateway relationship','requires reviewed display name','unknown template device kind','additional metadata must be an object','post-insert full device identity reconciliation failed','duplicate key','device conversion provenance','target device identities differ') if k in log]
print(json.dumps(out))
