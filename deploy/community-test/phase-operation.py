import subprocess,json,re
out={}
for name in ['tp-community-main-47-broker-mosquitto-1','tp-community-main-47-broker-owner-1']:
 p=subprocess.run(['docker','logs','--since','30m','--tail','1000',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT);text=p.stdout.decode(errors='replace')
 out[name]={'exit':p.returncode,'reason_counts':{word:len(re.findall(re.escape(word),text,re.I)) for word in ['denied','unframed','expired','mapping','route','not authorised','command','error']}}
sql="SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='public' AND (table_name LIKE 'broker%route%' OR table_name LIKE 'broker%connection%' OR table_name='broker_command_requests') ORDER BY table_name,ordinal_position;"
p=subprocess.check_output(['docker','exec','timescaledb','psql','-X','-At','-U','postgres','-d','ThingsPanel','-c',sql]);out['schema']=p.decode().splitlines()
print(json.dumps(out))
