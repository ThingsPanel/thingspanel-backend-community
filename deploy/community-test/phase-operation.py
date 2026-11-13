"""Server Python 3.6. Prepare private import inputs only; no database writes."""
import os,json,pathlib,hashlib,uuid,re,datetime,decimal
DEFAULT_ROOT='/opt/tp-community-actions/group-migration/20261113T100038Z'
OLD=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/native-20261111165830')
Q=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/20261111160912/quarantine-dd5aabb8c0068d8e67551a7069c4b014eb0241563f0daf21db829fa43dd57eb8.private.json')
QSHA='7d69297e535d358c4982fdab91d0124eb88806fb1307c8e207fd75f87de47a0d'
def sha(b):return hashlib.sha256(b).hexdigest()
def read(p):return json.loads(p.read_text(encoding='utf8'),parse_float=decimal.Decimal)
def encode(x):
 if isinstance(x,decimal.Decimal):return str(x)
 if isinstance(x,dict):return '{'+','.join(json.dumps(k,ensure_ascii=False)+':'+encode(v) for k,v in sorted(x.items()))+'}'
 if isinstance(x,list):return '['+','.join(encode(v) for v in x)+']'
 return json.dumps(x,ensure_ascii=False,allow_nan=False)
def write(p,obj):
 b=encode(obj).encode('utf8')
 if p.exists():
  if p.read_bytes()!=b:raise RuntimeError('existing prepared file differs: '+p.name)
 else:
  with p.open('xb') as f:f.write(b)
 os.chmod(str(p),0o600);return sha(b)
def keyed(rows):
 d={}
 for x in rows:
  if not x.get('id') or x['id'] in d:raise RuntimeError('archive identity missing or duplicated')
  d[x['id']]=x
 return d
def canonical(raw):
 try:return str(uuid.UUID(raw))
 except ValueError:return str(uuid.uuid5(uuid.NAMESPACE_OID,'thingspanel/community/tenant/'+raw))
def main():
 os.umask(0o077)
 root=pathlib.Path(os.environ.get('GROUP_MIGRATION_ROOT',DEFAULT_ROOT));source=root/'source.private.json';target=root/'target.private.json'
 src=read(source);dst=read(target)
 for key in ['groups','devices','templates','members']:
  if not isinstance(src.get(key),list):raise RuntimeError('source collection missing')
 for key in ['schema','groups','devices','bindings','members']:
  if key not in dst:raise RuntimeError('target collection missing')
 if sha(Q.read_bytes())!=QSHA:raise RuntimeError('approved quarantine digest mismatch')
 approved=read(OLD/'group-conversion.v3.private.json');devices_approved=read(OLD/'device-identities.v1.private.json');quarantine=read(Q)
 resolutions={'name_resolutions':approved['name_resolutions'],'parent_resolutions':approved['parent_resolutions'],'name_from_number':devices_approved['name_from_number'],'excluded_members':[{'group_id':e['excluded_from_conversion']['group_id'],'device_id':e['excluded_from_conversion']['device_id']} for e in quarantine['entries'] if e['excluded_from_conversion']['entity']=='group-membership']}
 resolution_sha=write(root/'resolutions.private.json',resolutions)
 compose=read(pathlib.Path('/opt/tp-community-main-20261028/compose.encore.private.json'));deployment=compose['services']['encore-api']['environment']['TP_SOURCE_DEPLOYMENT_ID']
 if not isinstance(deployment,str) or not deployment or '${' in deployment:raise RuntimeError('explicit source deployment required')
 stamp=os.environ.get('GROUP_MIGRATION_RECORDED_AT')
 if not stamp:
  stamp=datetime.datetime.strptime(root.name,'%Y%m%dT%H%M%SZ').strftime('%Y-%m-%dT%H:%M:%SZ')
 datetime.datetime.strptime(stamp,'%Y-%m-%dT%H:%M:%SZ')
 groups=keyed(src['groups']);devices=keyed(src['devices']);templates=keyed(src['templates'])
 tenants=sorted(set(g.get('tenant_id') for g in groups.values() if g.get('tenant_id')))
 (root/'inputs').mkdir(exist_ok=True)
 report={'source_archive_sha256':sha(source.read_bytes()),'target_archive_sha256':sha(target.read_bytes()),'resolutions_sha256':resolution_sha,'without_tenant_archived':sum(not g.get('tenant_id') for g in groups.values()),'tenants':[]}
 for tenant in tenants:
  if not re.match(r'^[A-Za-z0-9_-]{1,128}$',tenant):raise RuntimeError('unsafe tenant filename')
  gs=sorted([g for g in groups.values() if g.get('tenant_id')==tenant],key=lambda x:x['id']);gids=set(g['id'] for g in gs)
  ms=[{k:m[k] for k in ['tenant_id','group_id','device_id']} for m in src['members'] if m['group_id'] in gids]
  if any(m['tenant_id']!=tenant for m in ms):raise RuntimeError('membership owner differs')
  ex=[x for x in resolutions['excluded_members'] if x['group_id'] in gids];pairs=set((x['group_id'],x['device_id']) for x in ex)
  for pair in pairs:
   if not any((m['group_id'],m['device_id'])==pair for m in ms) or pair[1] not in devices or devices[pair[1]].get('tenant_id')==tenant:raise RuntimeError('approved member exclusion no longer matches archive')
  queue=[m['device_id'] for m in ms if (m['group_id'],m['device_id']) not in pairs];ids=set()
  while queue:
   id=queue.pop()
   if id in ids:continue
   d=devices.get(id)
   if d is None or d.get('tenant_id')!=tenant:raise RuntimeError('missing or foreign device dependency')
   ids.add(id)
   if d.get('parent_id'):queue.append(d['parent_id'])
  tids=set(devices[id]['device_config_id'] for id in ids if devices[id].get('device_config_id'))
  if any(id not in templates or templates[id].get('tenant_id')!=tenant for id in tids):raise RuntimeError('missing or foreign template dependency')
  names=[x for x in resolutions['name_resolutions'] if x['source_id'] in gids];parents=[x for x in resolutions['parent_resolutions'] if x['source_id'] in gids]
  for x in names:
   if groups[x['source_id']]['name']!=x['original_name']:raise RuntimeError('approved name source differs')
  for x in parents:
   if groups[x['source_id']].get('parent_id')!=x['original_parent_id'] or x['original_parent_id'] in groups:raise RuntimeError('approved parent source differs')
  bundle={'source_database':'ThingsPanel','target_database':os.environ.get('GROUP_MIGRATION_TARGET_DATABASE','native_main_20261028'),'source_deployment_id':deployment,'source_tenant_id':tenant,'source_archive_sha256':report['source_archive_sha256'],'target_archive_sha256':report['target_archive_sha256'],'resolutions_sha256':resolution_sha,'recorded_at':stamp,'groups':gs,'devices':[devices[id] for id in sorted(ids)],'templates':[templates[id] for id in sorted(tids)],'members':ms,'name_resolutions':names,'parent_resolutions':parents,'name_from_number':[id for id in resolutions['name_from_number'] if id in ids],'excluded_members':ex}
  digest=write(root/'inputs'/(tenant+'.private.json'),bundle)
  report['tenants'].append({'source_tenant_id':tenant,'tenant_id':canonical(tenant),'groups':len(gs),'devices':len(ids),'templates':len(tids),'members':len(ms)-len(ex),'excluded_members':len(ex),'input_sha256':digest})
  if tenant=='63a4b529':
   nmap={x['source_id']:x['name'] for x in names};pmap=set(x['source_id'] for x in parents)
   expected={'tenant_id':canonical(tenant),'source_archive_sha256':report['source_archive_sha256'],'groups':[{'id':g['id'],'name':nmap.get(g['id'],g['name']),'parentId':None if g['id'] in pmap or g.get('parent_id') in (None,'','0') else g['parent_id']} for g in gs],'member_edges':[{'groupId':m['group_id'],'deviceId':m['device_id']} for m in ms if (m['group_id'],m['device_id']) not in pairs],'closure_device_ids':sorted(ids),'foreign_group_ids':sorted(g['id'] for g in groups.values() if g.get('tenant_id') and g['tenant_id']!=tenant),'qa_device_id':'66b336f2-abb7-4521-81bb-6c93d7bcc67b'}
   report['expected_test_sha256']=write(root/'expected-test.safe.json',expected)
 write(root/'input-preparation.safe.json',report)
 print(json.dumps(report,sort_keys=True))
 return report
if __name__=='__main__':main()
