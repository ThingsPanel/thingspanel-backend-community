import json,urllib.request,urllib.error,subprocess
out=[]
for port,protocol,kind in [(19091,'HTTP','1'),(1505,'ModbusTCP','1'),(503,'MODBUS_RTU','2'),(503,'MODBUS_RTU','3')]:
 for form in ['CFG','VCR','VCRT']:
  u='http://127.0.0.1:'+str(port)+'/api/v1/form/config?protocol_type='+protocol+'&device_type='+kind+'&form_type='+form
  item={'protocol':protocol,'device_type':kind,'form_type':form}
  try:
   r=urllib.request.urlopen(u,timeout=4);item.update(status=r.status,body=json.loads(r.read()))
  except urllib.error.HTTPError as e:item.update(status=e.code,body=e.read(500).decode(errors='replace'))
  except Exception as e:item['error']=type(e).__name__
  out.append(item)
print(json.dumps(out))
