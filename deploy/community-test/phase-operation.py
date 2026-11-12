import pathlib,json,urllib.request,datetime
x=json.loads(pathlib.Path('/opt/tp-community-main-20261028/ha-runtime/ha-demo.credentials.private.json').read_text());out={'at':datetime.datetime.utcnow().isoformat()+'Z','entities':{}}
for e in ['light.office_rgbw_lights','sensor.qa_q08_model_20261112','sensor.qa_q08_compare_20261112']:
 r=urllib.request.Request('http://127.0.0.1:58123/api/states/'+e,headers={'Authorization':'Bearer '+x['long_lived_token']})
 with urllib.request.urlopen(r,timeout=5)as a:j=json.load(a)
 out['entities'][e]={k:j.get(k)for k in ['entity_id','state','last_changed','last_updated']}
print(json.dumps(out))
