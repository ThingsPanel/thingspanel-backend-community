import json,subprocess
raw=subprocess.check_output(['docker','inspect','tp-community-main-47-encore-api-1'])
info=json.loads(raw)[0]
env=dict(x.split('=',1) for x in info['Config']['Env'] if '=' in x)
print(json.dumps({'runtime_client':{k:bool(env.get(k)) for k in ['TP_COMMUNITY_BASE_URL','TP_COMMUNITY_API_KEY','TP_COMMUNITY_EMAIL','TP_COMMUNITY_PASSWORD','TP_AUTOMATION_SERVICE_TOKEN','TP_SOURCE_DEPLOYMENT_ID']}}))
