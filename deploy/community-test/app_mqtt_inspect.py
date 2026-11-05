import json, pathlib, subprocess, urllib.request, urllib.error
ROOT = pathlib.Path('/opt/tp-community-main-20261028')
def run(args):
    p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=45)
    if p.returncode: raise RuntimeError('Diagnostic command failed: '+args[0])
    return p.stdout.decode()
def envof(name):
    d=json.loads(run(['docker','inspect',name]))[0]
    return dict(x.split('=',1) for x in d['Config']['Env'] if '=' in x)
def probe(url,token,dep):
    req=urllib.request.Request(url,headers={'Authorization':'Bearer '+token,'X-Notification-Deployment':dep})
    try:
        with urllib.request.urlopen(req,timeout=8) as resp:
            d=json.load(resp)
            return {'status':resp.status,'configured':d.get('configured'),'policy_id':d.get('policy_id'),'device_count':len(d.get('deviceIds',[]))}
    except urllib.error.HTTPError as e:return {'status':e.code}
    except Exception as e:return {'error_type':type(e).__name__}
compose=json.loads((ROOT/'compose.encore.private.json').read_text())
runtime=json.loads((ROOT/'runtime.encore.private.json').read_text())
services={}
for s in ['encore-api','notification-core']:
    cid=run(['docker','compose','-f',str(ROOT/'compose.encore.private.json'),'ps','-q',s]).strip()
    services[s]=envof(cid)
pm=json.loads(run(['/root/.local/share/pnpm/pm2','jlist']))
be=next(p['pm2_env'] for p in pm if p['name']=='thingspanel-backend-community')
out={'services':{},'backend':{k:be.get(k) for k in ['AUTOMATION_ENGINE_MODE','AUTOMATION_COMPAT_MODE','AUTOMATION_CORE_BASE_URL','AUTOMATION_COMPAT_SUNSET','NOTIFICATION_SOURCE_DEPLOYMENT_ID']}}
for s,e in services.items():
    out['services'][s]={k:e.get(k) for k in ['TP_COMMUNITY_BASE_URL','TP_NOTIFICATION_URL','TP_SOURCE_DEPLOYMENT_ID']}
    out['services'][s]['configured']={k:bool(e.get(k)) for k in ['TP_NOTIFICATION_SOURCE_READER_TOKEN','TP_NOTIFICATION_TOKEN','TP_AUTOMATION_SERVICE_TOKEN']}
e=services['encore-api'];dep=e.get('TP_SOURCE_DEPLOYMENT_ID','automation-rewrite')
out['reader_matches_backend']=bool(e.get('TP_NOTIFICATION_SOURCE_READER_TOKEN')) and e.get('TP_NOTIFICATION_SOURCE_READER_TOKEN')==be.get('NOTIFICATION_MEMBER_READER_TOKEN')
out['policy_probe']=probe('http://127.0.0.1:9999/api/v1/notification/source-policies/63a4b529?alias=cf1dd014-7450-5c2a-bfd2-8ec9deab2505',e.get('TP_NOTIFICATION_SOURCE_READER_TOKEN',''),dep)
out['member_probe']=probe('http://127.0.0.1:9999/api/v1/notification/source-app-members/a9c24ba0-738d-9477-6053-34ed8a8cd3c2?tenantId=63a4b529',be.get('NOTIFICATION_MEMBER_READER_TOKEN',''),be.get('NOTIFICATION_SOURCE_DEPLOYMENT_ID',''))
db=next(name for name,d in runtime['sql_servers'][0]['databases'].items() if d['name']=='automation')
sql="SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,state,attempt,last_error,payload->>'resolution_error' AS resolution_error,payload->>'policy_id' AS policy_id,payload->>'group_revision' AS group_revision FROM auto_outbox WHERE scope_key='alarm:9a4df489-6690-49d0-b7b9-981669e897f1') t;"
out['outbox']=json.loads(run(['docker','exec','timescaledb','psql','-X','-U','postgres','-d',db,'-At','-c',sql]))
print(json.dumps(out,ensure_ascii=False,indent=2))
