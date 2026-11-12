import pathlib,json,subprocess,datetime
r=json.loads(pathlib.Path('/opt/tp-community-main-20261028/runtime.encore.private.json').read_text())
dbs=[physical for server in r.get('sql_servers',[]) for physical,db in server.get('databases',{}).items() if db.get('name')=='automation']
if len(set(dbs))!=1:raise RuntimeError('Automation database not unique; no query')
ids=['bff1f6b3-a861-4105-b97a-d1673b22b7f8','92950988-faab-40f8-b422-d0054c16c53d','6b7a749b-b81a-4421-bac5-e4d07f13f8dd','11fdf021-2667-490b-82d2-dc1e34b787ae','27272b50-de0a-49f1-8c41-d56d48c3c4e0','769f470f-83b6-4b22-9a1f-64d0d46871ca']
sql="SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT o.id,o.scope_key,o.idempotency_key,o.state,to_jsonb(o)->>'accepted_ref' AS accepted_ref,to_jsonb(o)->>'provider_id' AS provider_id,o.payload#>>'{variables,episode_id}' AS episode_id,o.payload#>>'{variables,transition_id}' AS transition_id,o.payload#>>'{variables,policy_id}' AS policy_id,o.payload#>>'{variables,policy_key}' AS policy_key,o.payload#>>'{variables,kind}' AS transition_kind FROM auto_outbox o WHERE o.tenant_id='63a4b529' AND o.id IN ("+','.join("'"+x+"'" for x in ids)+") ORDER BY o.created_at)t"
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','postgres','-d',dbs[0]],input=("BEGIN READ ONLY;SET LOCAL statement_timeout='5s';"+sql+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
if p.returncode:
 print(json.dumps({'at':datetime.datetime.now().isoformat(),'read_only_error':p.stderr.decode(errors='replace')[:900]}));raise SystemExit(0)
print(json.dumps({'at':datetime.datetime.now().isoformat(),'requested_ids':ids,'rows':json.loads(p.stdout)}))
