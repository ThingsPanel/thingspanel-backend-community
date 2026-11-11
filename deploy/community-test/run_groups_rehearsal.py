import hashlib,json,os,pathlib,subprocess,yaml
os.umask(0o077)
source=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/20261111160912')
target=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/native-20261111165830')
expected_source='85ff3b5ce1c505e99a18420f5e94f270995e3f1dc55ca96a873188bbd52c4cdc'
expected_target='719051f6012cba76d8d50f7a04e5e6f7d24fe7e4b5088ed14d818e6ec6bc14db'
expected_quarantine='7d69297e535d358c4982fdab91d0124eb88806fb1307c8e207fd75f87de47a0d'
receipts=[]
for root,expected in ((source,expected_source),(target,expected_target)):
 receipt=json.loads((root/'restore-verification.json').read_text())
 if receipt['status']!='restored-and-verified' or receipt['archive_sha256']!=expected:raise RuntimeError('restore receipt differs')
 if hashlib.sha256((root/'source.private.dump').read_bytes()).hexdigest()!=expected:raise RuntimeError('private source archive differs')
 receipts.append(receipt)
quarantine_path=source/'quarantine-dd5aabb8c0068d8e67551a7069c4b014eb0241563f0daf21db829fa43dd57eb8.private.json'
if hashlib.sha256(quarantine_path.read_bytes()).hexdigest()!=expected_quarantine:raise RuntimeError('quarantine archive differs')
quarantine=json.loads(quarantine_path.read_text())
excluded=[e['excluded_from_conversion']['id'] for e in quarantine['entries'] if e['excluded_from_conversion']['entity']=='group']
query="""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object('groups',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM groups ORDER BY id)t),
'members',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM r_group_device ORDER BY group_id,device_id)t));ROLLBACK;"""
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',receipts[0]['rehearsal_database']],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
if p.returncode:raise RuntimeError('cannot read isolated source groups')
rows=json.loads(p.stdout)
if any(g.get('parent_id') in excluded for g in rows['groups']) or any(m['group_id'] in excluded for m in rows['members']):raise RuntimeError('quarantined group is referenced')
for e in quarantine['entries']:
 if e['excluded_from_conversion']['entity']=='group':
  original=e['original_rows']['group']
  current=[g for g in rows['groups'] if g['id']==original['id']]
  if current!=[original]:raise RuntimeError('quarantined group changed')
bundle={'source_database':receipts[0]['rehearsal_database'],'target_database':receipts[1]['rehearsal_database'],
'source_archive_sha256':expected_source,'target_archive_sha256':expected_target,'quarantine_sha256':expected_quarantine,
'groups':rows['groups'],'excluded_groups':excluded,'parent_resolutions':[{'source_id': '1900798e-e72b-5b3d-f1eb-c2221c7c95aa', 'original_parent_id': '9f3f04a1-18d2-2eed-192c-cb75c66f241b'}, {'source_id': '866a11b6-aa5c-08a8-1898-5a0c093844ac', 'original_parent_id': '9f3f04a1-18d2-2eed-192c-cb75c66f241b'}, {'source_id': '9fb99b97-a406-7a1e-e2f2-abf96dc935cd', 'original_parent_id': '9f3f04a1-18d2-2eed-192c-cb75c66f241b'}, {'source_id': 'f739ab04-fa41-9bec-e2e2-f18cef64e854', 'original_parent_id': 'd91a4ba7-7dbc-ca9c-d8c5-5ee5c7fc7ac2'}, {'source_id': '82cc65b2-1f44-2d40-0ba2-7c9dbcf52952', 'original_parent_id': 'd91a4ba7-7dbc-ca9c-d8c5-5ee5c7fc7ac2'}, {'source_id': 'a0ffd8c9-e235-cd26-5ede-bf0e5185949a', 'original_parent_id': 'd91a4ba7-7dbc-ca9c-d8c5-5ee5c7fc7ac2'}, {'source_id': 'b040430c-69cf-c8c4-7654-b799d5419ec7', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': 'cf3e32c4-e11a-7f06-745b-cd67eb37485a', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '0ebb8f84-a350-5fa3-3389-52377cbaf126', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '7e8408e8-2dd9-063e-d404-5398ae077712', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '40a84964-3d9a-aa62-0600-49c7b28791f4', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '585f144e-b205-e52e-d46e-04d0e251e261', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': 'b2010b81-2b25-b73f-528f-92f054be35d5', 'original_parent_id': 'd36b3594-70ed-6ed7-58f1-c26009f66408'}, {'source_id': 'f55855ea-efc4-ed49-7025-65189c5df1e5', 'original_parent_id': '5d933fa6-525a-5bf0-6082-ddce2790778d'}, {'source_id': '49665fab-13f1-e4da-3f29-0e36fbd1e6a6', 'original_parent_id': '6fbb9832-1b9c-496b-38f3-b912ef91ca56'}, {'source_id': '1d700a61-8176-9f81-fbd7-316b716c4360', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '314b5543-a451-f8fa-9ad1-7bed7df1e123', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '2f8a1bf9-b68c-abe6-7a18-30b6df68052c', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '09fb1156-6195-0b66-fef4-a833d7fa343b', 'original_parent_id': '3ddf6684-945d-74c7-f8dc-ed502dd235d0'}, {'source_id': '9211013d-1c2d-7985-6d16-1141a1ef5a28', 'original_parent_id': '8614b7ea-340c-b2ab-ec55-76ad7d04f99a'}, {'source_id': 'bb909cc8-7392-abcf-ede3-a39b0c21b3c0', 'original_parent_id': '6856504e-9e10-4559-c12e-97f87c812fef'}, {'source_id': '36f443b1-5d27-348e-d0d2-0f46675c0653', 'original_parent_id': '9e90ec20-5bcc-18eb-0b85-6ecf66535632'}, {'source_id': '25c90c0f-6855-457f-0dee-346871988281', 'original_parent_id': 'ccf27545-c87f-1a4f-1941-c5a04496f4b8'}],'name_resolutions':[{'source_id':'f739ab04-fa41-9bec-e2e2-f18cef64e854','original_name':'group11','name':'group11（f739ab04）'},{'source_id':'b040430c-69cf-c8c4-7654-b799d5419ec7','original_name':'1','name':'1（b040430c）'}]}
bundle_path=target/'group-conversion.v3.private.json'
if bundle_path.exists():
 if json.loads(bundle_path.read_text())!=bundle:raise RuntimeError('existing conversion source changed')
else:
 with bundle_path.open('x',encoding='utf-8') as f:json.dump(bundle,f,ensure_ascii=False)
settings=yaml.safe_load((source/'platform.next.private.yml').read_text())['relational']['postgres']['platform']
env=os.environ.copy()
env.update(PGHOST=str(settings.get('host') or 'localhost'),PGPORT=str(settings.get('port') or 5432),PGUSER=str(settings['username']),PGPASSWORD=str(settings.get('password') or ''),PGDATABASE=bundle['target_database'],PGCONNECT_TIMEOUT='8',PGSSLMODE='disable')
binary=target/'metadata-upgrade-groups'
checks=[]
for attempt in range(2):
 with bundle_path.open('rb') as stdin:
  result=subprocess.run([str(binary)],stdin=stdin,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=150)
 if result.returncode:
  (target/'group-conversion-failure.private.log').write_bytes(result.stderr)
  # Do not export database error text or source rows.
  known=('missing source timestamp','invalid name','duplicate sibling group name','cannot connect to isolated target','cross-tenant group parent','target groups differ','conversion provenance differs','conversion audit identity differs')
  detail=result.stderr.decode(errors='replace')
  print(json.dumps({'state':'failed-no-cutover','categories':[k for k in known if k in detail] or ['private-log-review-required']}))
  raise SystemExit(1)
 checks.append(json.loads(result.stdout))
receipt={'group_conversion':checks[0],'retry_verification':checks[1],'bundle_sha256':hashlib.sha256(bundle_path.read_bytes()).hexdigest(),'cutover_completed':False,'source_mutated':False}
receipt_path=target/'group-conversion-verification.json'
if not receipt_path.exists():
 with receipt_path.open('x',encoding='utf-8') as f:json.dump(receipt,f,indent=2)
print(json.dumps(receipt))
