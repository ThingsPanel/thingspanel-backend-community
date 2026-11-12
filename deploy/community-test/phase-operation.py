import pathlib,json,subprocess,time
out={'samples':[]}
for round in range(3):
 sample={'time':time.time(),'containers':[],'controllers':[]}
 for n in ['tp-community-main-47-broker-owner-1','tp-community-main-47-broker-mosquitto-1']:
  x=json.loads(subprocess.check_output(['docker','inspect',n]))[0]
  sample['containers'].append({'name':n,'id':x['Id'],'image':x['Image'],'status':x['State']['Status'],'started':x['State']['StartedAt'],'health':x['State'].get('Health',{}).get('Status'),'restarts':x['RestartCount']})
 for n in ['tp-http-scope.service','tp-ha-scope.service']:
  x=subprocess.check_output(['systemctl','show',n,'--property=ActiveState,ExecMainStartTimestampMonotonic,ExecMainStatus']).decode();sample['controllers'].append({'name':n,'state':x.strip()})
 out['samples'].append(sample)
 if round<2:time.sleep(25)
print(json.dumps(out))
