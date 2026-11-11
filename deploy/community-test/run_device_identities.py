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
exclusions=[e['excluded_from_conversion'] for e in quarantine['entries']]
query="""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL timezone='UTC';
SELECT json_build_object('devices',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM devices ORDER BY id)t),
 'templates',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,tenant_id,device_type FROM device_configs ORDER BY id)t));ROLLBACK;"""
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',receipts[0]['rehearsal_database']],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
if p.returncode:raise RuntimeError('cannot read isolated source devices')
rows=json.loads(p.stdout)
bundle={'source_database':receipts[0]['rehearsal_database'],'target_database':receipts[1]['rehearsal_database'],
'source_archive_sha256':expected_source,'target_archive_sha256':expected_target,'quarantine_sha256':expected_quarantine,
'devices':rows['devices'],'templates':rows['templates'],
'excluded_devices':[e['id'] for e in exclusions if e['entity']=='device'],
'excluded_gateway_relations':[{'device_id':e['device_id'],'target_id':e['parent_id']} for e in exclusions if e['entity']=='gateway-relationship'],
'excluded_template_relations':[{'device_id':e['device_id'],'target_id':e['template_id']} for e in exclusions if e['entity']=='device-template-relationship'],
'name_from_number':['c6c71b94-7809-d39b-05f3-f3e549b3862b','bb679a5a-fe5e-189d-688c-5d92337aa3d4','681df01d-69f8-f4bd-6a83-0f7ccb9c2def']}
bundle_path=target/'device-identities.v1.private.json'
if bundle_path.exists():
 if json.loads(bundle_path.read_text())!=bundle:raise RuntimeError('existing conversion source changed')
else:
 with bundle_path.open('x',encoding='utf-8') as f:json.dump(bundle,f,ensure_ascii=False)
settings=yaml.safe_load((source/'platform.next.private.yml').read_text())['relational']['postgres']['platform']
env=os.environ.copy()
env.update(PGHOST=str(settings.get('host') or 'localhost'),PGPORT=str(settings.get('port') or 5432),PGUSER=str(settings['username']),PGPASSWORD=str(settings.get('password') or ''),PGDATABASE=bundle['target_database'],PGCONNECT_TIMEOUT='8',PGSSLMODE='disable')
binary=target/'metadata-upgrade-devices'
checks=[]
for attempt in range(2):
 with bundle_path.open('rb') as stdin:
  result=subprocess.run([str(binary)],stdin=stdin,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=330)
 if result.returncode:
  (target/'device-identities-failure.private.log').write_bytes(result.stderr)
  # Do not export database error text or source rows.
  known=('invalid source device row','invalid private conversion input','unresolved gateway relationship','requires reviewed display name','invalid source coordinates','unknown template device kind','template contradicts gateway hierarchy','template contradicts parent relation','additional metadata must be an object','nested custom field requires explicit conversion','invalid extended metadata','conflicting custom field values','target device identities differ','post-insert full device identity reconciliation failed','missing source timestamp','invalid name','duplicate sibling group name','cannot connect to isolated target','cross-tenant group parent','target groups differ','conversion provenance differs','conversion audit identity differs')
  detail=result.stderr.decode(errors='replace')
  print(json.dumps({'state':'failed-no-cutover','categories':[k for k in known if k in detail] or ['private-log-review-required']}))
  raise SystemExit(1)
 checks.append(json.loads(result.stdout))
receipt={'device_identity_conversion':checks[0],'retry_verification':checks[1],'bundle_sha256':hashlib.sha256(bundle_path.read_bytes()).hexdigest(),'cutover_completed':False,'source_mutated':False}
receipt_path=target/'device-identities-verification.json'
if not receipt_path.exists():
 with receipt_path.open('x',encoding='utf-8') as f:json.dump(receipt,f,indent=2)
print(json.dumps(receipt))
