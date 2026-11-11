import base64,datetime,fcntl,gc,hashlib,json,os,pathlib,subprocess,yaml
os.umask(0o077)
root=pathlib.Path('/opt/tp-community-actions')
lock=(root/'deployment.lock').open('a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
source=root/'upgrade-rehearsal/20261111160912'
target=root/'upgrade-rehearsal/native-20261111165830'
expected_source='85ff3b5ce1c505e99a18420f5e94f270995e3f1dc55ca96a873188bbd52c4cdc'
expected_target='719051f6012cba76d8d50f7a04e5e6f7d24fe7e4b5088ed14d818e6ec6bc14db'
expected_quarantine='7d69297e535d358c4982fdab91d0124eb88806fb1307c8e207fd75f87de47a0d'
def digest(path):
 h=hashlib.sha256()
 with path.open('rb') as stream:
  for block in iter(lambda:stream.read(1024*1024),b''):h.update(block)
 return h.hexdigest()
receipts=[]
for path,expected in ((source,expected_source),(target,expected_target)):
 r=json.loads((path/'restore-verification.json').read_text())
 if r['status']!='restored-and-verified' or r['archive_sha256']!=expected or digest(path/'source.private.dump')!=expected:
  raise RuntimeError('Verified source/target archive changed')
 receipts.append(r)
archive=source/'quarantine-dd5aabb8c0068d8e67551a7069c4b014eb0241563f0daf21db829fa43dd57eb8.private.json'
if digest(archive)!=expected_quarantine:raise RuntimeError('Quarantine receipt changed')
excluded=[e['excluded_from_conversion']['id'] for e in json.loads(archive.read_text())['entries'] if e['excluded_from_conversion']['entity']=='device']
query="""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object(
'devices',(SELECT json_agg(t) FROM (SELECT id,tenant_id,device_config_id,parent_id,protocol,access_way,service_access_id,voucher FROM devices ORDER BY id)t),
'templates',(SELECT json_agg(t) FROM (SELECT id,tenant_id,protocol_type,device_type,voucher_type FROM device_configs ORDER BY id)t));ROLLBACK;"""
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',receipts[0]['rehearsal_database']],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
if p.returncode:raise RuntimeError('Cannot read isolated credential source')
rows=json.loads(p.stdout)
bundle_path=target/'credentials.v1.private.json'
at=datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='microseconds')
if bundle_path.exists():at=json.loads(bundle_path.read_text())['recorded_at']
bundle={'source_database':receipts[0]['rehearsal_database'],'target_database':receipts[1]['rehearsal_database'],
 'source_archive_sha256':expected_source,'target_archive_sha256':expected_target,'quarantine_sha256':expected_quarantine,
 'recorded_at':at,'devices':rows['devices'],'templates':rows['templates'],'excluded_devices':excluded}
if bundle_path.exists():
 if json.loads(bundle_path.read_text())!=bundle:raise RuntimeError('Credential source bundle drifted; refusing replacement')
else:
 with bundle_path.open('x',encoding='utf8') as stream:json.dump(bundle,stream,ensure_ascii=False)
keypath=target/'credential-keyring.private.json'
if not keypath.exists():
 with keypath.open('x') as stream:
  json.dump({'active':'offline-20261111','keys':{'offline-20261111':base64.b64encode(os.urandom(32)).decode()}},stream)
if keypath.stat().st_mode&0o077:raise RuntimeError('Credential keyring permissions must remain private')
keyring=json.loads(keypath.read_text())
settings=yaml.safe_load((source/'platform.next.private.yml').read_text())['relational']['postgres']['platform']
env=dict(os.environ,PGHOST=str(settings.get('host') or 'localhost'),PGPORT=str(settings.get('port') or 5432),PGUSER=str(settings['username']),PGPASSWORD=str(settings.get('password') or ''),PGDATABASE=bundle['target_database'],PGCONNECT_TIMEOUT='8',PGSSLMODE='disable',GOMAXPROCS='1',GOMEMLIMIT='128MiB')
env.update(METADATA_CREDENTIAL_ACTIVE_KEY_REF=keyring['active'],METADATA_CREDENTIAL_KEYRING_B64=base64.b64encode(json.dumps(keyring['keys']).encode()).decode())
del rows,bundle,p;gc.collect()
binary=target/'metadata-upgrade-credentials'
checks=[]
for attempt in range(2):
 with bundle_path.open('rb') as stdin:
  result=subprocess.run([str(binary)],stdin=stdin,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=360)
 if result.returncode:
  (target/'credentials-failure.private.log').write_bytes(result.stderr)
  known=['violates check constraint','violates foreign key constraint','target owner differs','provenance differs','coverage differs','material differs','fingerprint differs','lifecycle differs','could not protect','cannot connect','microsecond recording time','GLIBC']
  detail=result.stderr.decode(errors='replace')
  print(json.dumps({'state':'failed-no-cutover','categories':[k for k in known if k in detail] or ['private-log-review-required']}))
  raise SystemExit(1)
 checks.append(json.loads(result.stdout))
receipt={'credential_conversion':checks[0],'retry_verification':checks[1],'bundle_sha256':digest(bundle_path),'source_mutated':False,'cutover_completed':False,'keyring_scope':'isolated native rehearsal; retained for eventual verified cutover'}
path=target/'credentials-verification.json'
if not path.exists():
 with path.open('x') as stream:json.dump(receipt,stream,indent=2)
print(json.dumps(receipt))
