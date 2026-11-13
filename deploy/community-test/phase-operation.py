import subprocess,json,base64
sql="SELECT coalesce(json_agg(json_build_object('id',id,'state',state,'last_error',last_error,'created_at',created_at,'dispatched_at',dispatched_at,'payload',convert_from(payload,'UTF8')::jsonb)),'[]'::json) FROM broker_command_requests WHERE device_id='66b336f2-abb7-4521-81bb-6c93d7bcc67b';"
raw=subprocess.check_output(['docker','exec','timescaledb','psql','-X','-At','-U','postgres','-d','ThingsPanel','-c',sql])
rows=json.loads(raw)
def topics(x):
 out=[]
 if isinstance(x,dict):
  for k,v in x.items():
   if 'topic' in k.lower() and isinstance(v,str):out.append({k:v})
   elif isinstance(v,(dict,list)):out+=topics(v)
 elif isinstance(x,list):
  for v in x:out+=topics(v)
 return out
for r in rows:
 payload=r.pop('payload');r['payload_keys']=list(payload) if isinstance(payload,dict) else [];r['topics']=topics(payload)
 if isinstance(payload,dict):
  m=payload.get('message') or {};r['routing']={k:m.get(k) for k in ['DeviceID','DeviceNumber','MessageID','DeviceType','ProtocolType','TopicPrefix','Topic','TargetDeviceID']}
  try:
   wire=json.loads(base64.b64decode(payload.get('encoded','')));r['wire']={k:wire.get(k) for k in ['method','params']};r['wire_keys']=list(wire)
  except Exception:r['wire_decode']='unavailable'
print(json.dumps({'commands':rows}))
