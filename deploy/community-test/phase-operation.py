import pathlib,json,urllib.request,time,datetime
c=json.loads(pathlib.Path('/opt/tp-community-main-20261028/ha-runtime/ha-demo.credentials.private.json').read_text());token=c['long_lived_token'];base='http://127.0.0.1:58123';S='sensor.qa_q08_model_20261112';D='light.office_rgbw_lights';B='sensor.qa_q08_compare_20261112';out={'started':datetime.datetime.utcnow().isoformat()+'Z','steps':[]}
def call(entity,body=None):
 r=urllib.request.Request(base+'/api/states/'+entity,data=json.dumps(body).encode()if body is not None else None,headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
 with urllib.request.urlopen(r,timeout=6)as a:j=json.load(a)
 return {k:j.get(k)for k in ['entity_id','state','last_changed','last_updated']}
out['before']={e:call(e)for e in [S,D,B]};assert out['before'][S]['state']=='20' and out['before'][D]['state']=='on'
try:
 for v in [20,38,38,20]:
  row={'value':v,'at':datetime.datetime.utcnow().isoformat()+'Z','provider_write':call(S,{'state':str(v),'attributes':{'friendly_name':'QA-Q08-model-20261112','unit_of_measurement':'C'}})};time.sleep(5);row['states']={e:call(e)for e in [S,D,B]};out['steps'].append(row)
finally:
 out['cleanup_sensor']=call(S,{'state':'20','attributes':{'friendly_name':'QA-Q08-model-20261112','unit_of_measurement':'C'}});out['finished']=datetime.datetime.utcnow().isoformat()+'Z'
print(json.dumps(out))
