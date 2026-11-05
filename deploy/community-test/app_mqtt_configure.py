"""Attach the two existing tenant grants without creating or widening a grant."""
import hashlib, json, os, pathlib, subprocess, time, urllib.request
ROOT=pathlib.Path('/opt/tp-community-main-20261028')
os.umask(0o077)
def run(args):
    p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
    if p.returncode: raise RuntimeError('Configuration command failed: '+args[0])
    return p.stdout.decode()
configfile=ROOT/'compose.encore.private.json'
config=json.loads(configfile.read_text())
runtime=json.loads((ROOT/'runtime.encore.private.json').read_text())
db=next(name for name,d in runtime['sql_servers'][0]['databases'].items() if d['name']=='notification')
env=config['services']['encore-api']['environment']
tokens=json.loads(env.get('TP_NOTIFICATION_TOKENS_JSON','{}'))
for policyfile in [ROOT/'notification-renew-policy.private.json',ROOT/'notification-app-migration/renew-policy.private.json']:
    policy=json.loads(policyfile.read_text())
    pairpath=pathlib.Path(policy['credentials_file']).resolve()
    if ROOT not in pairpath.parents: raise RuntimeError('Unexpected credential path')
    pair=json.loads(pairpath.read_text())
    tenant=policy['tenant_id']
    if pair['allowedTenantIds'] != [tenant] or pair['deploymentId'] != env['TP_SOURCE_DEPLOYMENT_ID']:
        raise RuntimeError('Existing grant scope mismatch')
    digest=hashlib.sha256(pair['sourceCredential'].encode()).hexdigest()
    sql="SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT allowed_tenant_ids,deployment_id FROM notification_credentials WHERE purpose='source' AND token_hash='"+digest+"' AND revoked_at IS NULL AND expires_at>now()) t;"
    rows=json.loads(run(['docker','exec','timescaledb','psql','-X','-U','postgres','-d',db,'-At','-c',sql]))
    if len(rows)!=1 or rows[0]['allowed_tenant_ids'] != [tenant] or rows[0]['deployment_id']!=pair['deploymentId']:
        raise RuntimeError('Existing source credential is not active')
    tokens[tenant]=pair['sourceCredential']
env['TP_NOTIFICATION_TOKENS_JSON']=json.dumps(tokens,separators=(',',':'))
temporary=configfile.with_suffix('.tmp')
temporary.write_text(json.dumps(config,indent=2));temporary.replace(configfile)
run(['docker','compose','-f',str(configfile),'up','-d','--no-deps','encore-api'])
for attempt in range(20):
    try:
        d=json.load(urllib.request.urlopen('http://127.0.0.1:14000/v1/health',timeout=3))
        if d.get('status')=='ok':break
    except Exception:pass
    time.sleep(2)
else:raise RuntimeError('Native service health failed')
print(json.dumps({'status':'configured','tenant_ids':sorted(tokens),'new_grants_created':False,'health':'ok'}))
