import os,json,pathlib,subprocess,hashlib,datetime
os.umask(0o077)
root=pathlib.Path('/opt/tp-community-actions/group-migration')/datetime.datetime.utcnow().strftime('%Y%m%dT%H%M%SZ')
root.mkdir(parents=True,exist_ok=False)
def query(db,sql):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',db],input=sql.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
 if p.returncode:raise RuntimeError('private snapshot query failed')
 return json.loads(p.stdout)
src=query('ThingsPanel',"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
WITH RECURSIVE ids(id) AS (
 SELECT DISTINCT device_id FROM r_group_device WHERE group_id IN (SELECT id FROM groups)
 UNION
 SELECT d.parent_id FROM devices d JOIN ids ON ids.id=d.id WHERE d.parent_id IS NOT NULL AND d.parent_id<>'' AND d.parent_id<>'0'
)
SELECT json_build_object('source_database','ThingsPanel','recorded_at',transaction_timestamp(),
 'groups',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM groups ORDER BY id)t),
 'members',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT r.*,g.tenant_id FROM r_group_device r JOIN groups g ON g.id=r.group_id ORDER BY r.group_id,r.device_id)t),
 'devices',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM devices WHERE id IN(SELECT id FROM ids) ORDER BY id)t),
 'templates',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM device_configs WHERE id IN (SELECT device_config_id FROM devices WHERE id IN(SELECT id FROM ids)) ORDER BY id)t));ROLLBACK;""")
tgt=query('native_main_20261028',"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object('target_database',current_database(),'recorded_at',transaction_timestamp(),
 'schema',(SELECT json_agg(t) FROM schema_migrations t),
 'groups',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM device_groups ORDER BY id)t),
 'devices',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM devices ORDER BY id)t),
 'bindings',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM device_access_bindings ORDER BY device_id)t),
 'members',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM device_group_members ORDER BY group_id,device_id)t));ROLLBACK;""")
files={}
for name,data in [('source',src),('target',tgt)]:
 path=root/(name+'.private.json');raw=json.dumps(data,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode();path.write_bytes(raw);files[name]={'path':str(path),'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)}
by={}
for g in src['groups']:
 tenant=g.get('tenant_id') or '<unowned>';by.setdefault(tenant,{'groups':0,'members':0});by[tenant]['groups']+=1
for m in src['members']:by[m.get('tenant_id') or '<unowned>']['members']+=1
receipt={'state':'read-only-consistent-snapshots-created','root':str(root),'files':files,'source_counts':{k:len(src[k]) for k in ['groups','members','devices','templates']},'target_counts':{k:len(tgt[k]) for k in ['groups','members','devices','bindings']},'tenants':by,'source_mutated':False,'target_mutated':False}
(root/'snapshot-receipt.json').write_text(json.dumps(receipt,indent=2));print(json.dumps(receipt))
