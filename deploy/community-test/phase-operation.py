import json,pathlib,subprocess
p=pathlib.Path('/opt/tp-community-main-20261028/compose.env')
env={}
for line in p.read_text().splitlines():
 if '=' in line and not line.startswith('#'):
  k,v=line.split('=',1);env[k]=v
print(json.dumps({'runtime_client':{k:bool(env.get(k)) for k in ['TP_COMMUNITY_BASE_URL','TP_COMMUNITY_API_KEY','TP_COMMUNITY_EMAIL','TP_COMMUNITY_PASSWORD','TP_AUTOMATION_SERVICE_TOKEN','TP_SOURCE_DEPLOYMENT_ID']}}))
