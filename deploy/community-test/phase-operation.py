import subprocess,json,pathlib
name='tp-community-main-47-broker-owner-1';info=json.loads(subprocess.check_output(['docker','inspect',name]))[0];out={'scopes':[]};device='66b336f2-abb7-4521-81bb-6c93d7bcc67b'
def scan(x,source):
 if isinstance(x,dict):
  if 'allowed_device_ids' in x:
   ids=x.get('allowed_device_ids') or [];out['scopes'].append({'source':source,'plugin_socket':x.get('plugin_socket',False),'devices_count':len(ids),'contains_qa_device':device in ids,'publish_count':len((x.get('policy') or {}).get('publish',[]))})
  for v in x.values():scan(v,source)
 elif isinstance(x,list):
  for v in x:scan(v,source)
for m in info.get('Mounts',[]):
 p=pathlib.Path(m['Source'])
 if p.is_file() and p.stat().st_size<2000000:
  try:scan(json.loads(p.read_text()),m['Destination'])
  except Exception:pass
 elif p.is_dir():
  for q in p.glob('*.json'):
   if q.stat().st_size<2000000:
    try:scan(json.loads(q.read_text()),m['Destination']+'/'+q.name)
    except Exception:pass
print(json.dumps(out))
