"""Read runtime identity state and probe scope checks without sending notifications."""
import datetime, hashlib, json, pathlib, subprocess, urllib.request, urllib.error
ROOT=pathlib.Path('/opt/tp-community-main-20261028')
ACTIONS=pathlib.Path('/opt/tp-community-actions')
RETIRED={'TP_NOTIFICATION_TOKEN','TP_NOTIFICATION_TOKENS_JSON','NOTIFICATION_SOURCE_BEARER_TOKEN','NOTIFICATION_PROJECTION_BEARER_TOKEN','NOTIFICATION_MEMBER_READER_TENANT_IDS'}
def run(args):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=40)
 if p.returncode:raise RuntimeError('Audit command failed: '+args[0])
 return p.stdout.decode()
def request(path,token,body=None,dep=None):
 headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'}
 if dep:headers['X-Notification-Deployment']=dep
 req=urllib.request.Request('http://127.0.0.1:18091'+path,data=json.dumps(body).encode() if body is not None else None,headers=headers)
 try:
  with urllib.request.urlopen(req,timeout=8) as res:return res.status,json.load(res)
 except urllib.error.HTTPError as err:return err.code,{}
identity=json.loads((ROOT/'notification-service.identity.private.json').read_text())
compose_path=ROOT/'compose.encore.private.json';compose=json.loads(compose_path.read_text())
envs={}
for name in ['encore-api','notification-core']:
 cid=run(['docker','compose','-f',str(compose_path),'ps','-q',name]).strip()
 d=json.loads(run(['docker','inspect',cid]))[0]
 envs[name]=dict(v.split('=',1) for v in d['Config']['Env'] if '=' in v)
pm=json.loads(run(['/root/.local/share/pnpm/pm2','jlist']))
envs['backend']=next(p['pm2_env'] for p in pm if p['name']=='thingspanel-backend-community')
result={'identity_matches_all_services':all(e.get('NOTIFICATION_SERVICE_TOKEN')==identity['service_token'] for e in envs.values()),'retired_runtime_keys':{n:sorted(RETIRED.intersection(e)) for n,e in envs.items()},'retired_policy_files':len(list(ROOT.rglob('*renew*policy*.json')))}
result['retired_timers']=[]
for p in pathlib.Path('/etc/systemd/system').glob('tp-notification*renew*.timer'):
 service=p.with_suffix('.service')
 if not service.exists() or any(n in service.read_text() for n in ['notification_renew.py','notification-source-provision']):result['retired_timers'].append(p.name)
runtime=json.loads((ROOT/'runtime.encore.private.json').read_text())
database=next(n for n,d in runtime['sql_servers'][0]['databases'].items() if d['name']=='notification')
def sql(statement):return run(['docker','exec','timescaledb','psql','-X','-U','postgres','-d',database,'-At','-v','ON_ERROR_STOP=1','-c',statement]).strip()
result['active_unmanaged_source_credentials']=int(sql("SELECT count(*) FROM notification_credentials c WHERE purpose IN ('source','projection') AND revoked_at IS NULL AND expires_at>now() AND NOT EXISTS(SELECT 1 FROM notification_managed_credentials m WHERE m.credential_id=c.id)"))
tenants=sql('SELECT DISTINCT g.tenant_id FROM notification_plugin_grants g JOIN notification_plugin_registrations p ON p.id=g.registration_id WHERE g.enabled AND p.enabled ORDER BY g.tenant_id').splitlines()
result['granted_tenant_count']=len(tenants);result['lease_probes']=[]
for tenant in tenants:
 item={'tenant_id':tenant}
 for purpose in ['source','projection']:
  status,data=request('/internal/notification/source-credentials',identity['service_token'],{'tenantId':tenant,'purpose':purpose},identity['deployment_id'])
  lease=data.get('data',{});item[purpose+'_status']=status
  item[purpose+'_scope_valid']=status==200 and lease.get('tenantId')==tenant and lease.get('deploymentId')==identity['deployment_id'] and lease.get('purpose')==purpose and len(lease.get('credential',''))>=32
  if status==200:
   # Invalid empty event never reaches dispatch. Projection credentials must fail authentication.
   check,_=request('/api/v2/notification-ingress/events',lease['credential'],{})
   item[purpose+'_ingress_status']=check
 result['lease_probes'].append(item)
status,_=request('/internal/notification/source-credentials',identity['service_token'],{'tenantId':'nonexistent-audit-tenant','purpose':'source'},identity['deployment_id']);result['unknown_tenant_status']=status
result['passed']=result['identity_matches_all_services'] and not any(result['retired_runtime_keys'].values()) and result['retired_policy_files']==0 and not result['retired_timers'] and result['active_unmanaged_source_credentials']==0 and status==403 and bool(tenants) and all(p['source_scope_valid'] and p['projection_scope_valid'] and p.get('source_ingress_status')==400 and p.get('projection_ingress_status')==401 for p in result['lease_probes'])
print(json.dumps(result,indent=2))
if not result['passed']:raise SystemExit('Managed notification acceptance failed')
