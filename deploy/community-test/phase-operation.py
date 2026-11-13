import pathlib,json,subprocess,os,urllib.parse,hashlib
pid=2825348
expected='/home/thingspanel/thingspanel-backend-community/thingspanel-backend-community'
assert os.readlink('/proc/'+str(pid)+'/exe')==expected
env=dict(x.split('=',1) for x in pathlib.Path('/proc/'+str(pid)+'/environ').read_text().split('\0') if '=' in x)
c=json.loads(subprocess.check_output(['docker','inspect','tp-community-main-47-encore-api-1']).decode())[0]
assert c['Id']=='4daa00b71d4c0ca3d55a755f9cb90c342902dc1cc1d0822d2542f3e03deb50ab'
other=dict(x.split('=',1) for x in c['Config']['Env'] if '=' in x)
url=urllib.parse.urlsplit(env.get('TP_THINGMODEL_NATIVE_URL',''))
a=env.get('GOTP_AUTOMATION_TOKEN','').strip();b=other.get('TP_AUTOMATION_SERVICE_TOKEN','').strip()
ad=env.get('GOTP_AUTOMATION_DEPLOYMENT_ID','').strip();bd=other.get('TP_SOURCE_DEPLOYMENT_ID','').strip()
out={'changes':False,'pid':pid,'encore_id':c['Id'],'native_url_present':bool(url.scheme and url.hostname),'native_url':{'scheme':url.scheme,'host':url.hostname,'port':url.port,'path':url.path,'query_or_userinfo_present':bool(url.query or url.username or url.password)},'token_both_configured':len(a)>=32 and len(b)>=32,'token_equal':bool(a and a==b),'deployment_both_configured':bool(ad and bd),'deployment_equal':bool(ad and ad==bd),'encore_ports':c['NetworkSettings']['Ports'],'secret_values_exported':False}
print(json.dumps(out))
