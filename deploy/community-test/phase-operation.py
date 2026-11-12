import pathlib,json,urllib.request,datetime,time
token=json.loads(pathlib.Path('/opt/tp-community-main-20261028/ha-runtime/ha-demo.credentials.private.json').read_text())['long_lived_token']
entities=['light.office_rgbw_lights','sensor.qa_q08_compare_20261112']
out={'started':datetime.datetime.utcnow().isoformat()+'Z','mode':'read-only actual HA states; no provider control','samples':[]}
deadline=time.monotonic()+1
while time.monotonic()<deadline:
 row={'at':datetime.datetime.utcnow().isoformat()+'Z','entities':{}}
 for e in entities:
  try:
   req=urllib.request.Request('http://127.0.0.1:58123/api/states/'+e,headers={'Authorization':'Bearer '+token})
   with urllib.request.urlopen(req,timeout=4) as r:s=json.load(r)
   row['entities'][e]={k:s.get(k) for k in ['entity_id','state','last_changed','last_updated']}
  except Exception as error:row['entities'][e]={'error_type':type(error).__name__}
 out['samples'].append(row);time.sleep(1)
print(json.dumps(out))
