import json,yaml,pathlib
p=pathlib.Path('/home/thingspanel/thingspanel-backend-community/configs/conf.yml')
c=yaml.safe_load(p.read_text());m=c.get('mqtt',{})
print(json.dumps({'target_scoped_publisher':m.get('target_scoped_publisher') is True,'input_profile':m.get('input_profile'),'client_id_configured':bool(m.get('client_id'))}))
