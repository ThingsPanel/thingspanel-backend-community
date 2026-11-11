import collections,json,pathlib,subprocess
query="""BEGIN READ ONLY; SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT d.voucher, coalesce(c.voucher_type,'') AS voucher_type, d.device_config_id IS NOT NULL AS has_template FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id)t; ROLLBACK;"""
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','tp_upgrade_rehearsal_20261111160912'],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
if p.returncode:raise RuntimeError('Read-only voucher shape query failed')
counts=collections.Counter()
for r in json.loads(p.stdout):
 raw=r['voucher']
 try:v=json.loads(raw)
 except Exception:counts['invalid-json']+=1;continue
 if not isinstance(v,dict):counts['non-object']+=1;continue
 keys=set(v);unknown=keys-{'username','password'}
 def shape(k):
  if k not in v:return 'absent'
  x=v[k]
  if not isinstance(x,str):return 'not-string'
  if x=='':return 'empty'
  if len(x.encode())>8192:return 'oversized'
  return 'nonempty'
 kind=r['voucher_type'] if r['voucher_type'] in ('BASIC','ACCESSTOKEN','') else 'other'
 counts[json.dumps({'template':r['has_template'],'voucher_type':kind,'username':shape('username'),'password':shape('password'),'unknown_fields':bool(unknown)},sort_keys=True)]+=1
out={'shapes':[dict(shape=json.loads(k),count=v) if k.startswith('{') else dict(shape=k,count=v) for k,v in counts.most_common()], 'source_mutated':False}
mem={l.split(':')[0]:l.split(':')[1].strip() for l in pathlib.Path('/proc/meminfo').read_text().splitlines()}
out['memory_available']=mem['MemAvailable']
out['disk']=subprocess.check_output(['df','-h','/opt']).decode()
out['containers']=subprocess.check_output(['docker','ps','--format','{{.Names}} {{.Status}}']).decode().splitlines()
print(json.dumps(out))
