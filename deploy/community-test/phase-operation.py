import json,pathlib,subprocess,yaml
name='tp-community-main-47-broker-owner-1'
config=json.loads(subprocess.check_output(['docker','inspect',name]))[0]
env=dict(v.split('=',1) for v in config['Config']['Env'] if '=' in v)
service_path=env['TP_BROKER_OWNER_SERVICES_FILE']
services=json.loads(subprocess.check_output(['docker','exec',name,'cat',service_path]))
live=yaml.safe_load(pathlib.Path('/home/thingspanel/thingspanel-backend-community/configs/conf.yml').read_text())
user=live['mqtt']['user'];out=[]
for s in services:
 if s.get('username')!=user:continue
 out.append({'matches_platform_publisher':True,'plugin_socket':s.get('plugin_socket',False),'allowed_devices_count':len(s.get('allowed_device_ids',[])),'qa_device_allowed':'5ed19dd2-4422-dfe3-2a54-13e180f120ed' in s.get('allowed_device_ids',[]),'qa_tenant_allowed':'63a4b529' in s.get('allowed_tenant_ids',[]),'principal_tenant':s.get('principal',{}).get('tenant_id'),'publish_filters':s.get('policy',{}).get('publish',[])})
print(json.dumps({'publisher_scope':out}))
