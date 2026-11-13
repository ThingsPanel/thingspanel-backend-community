import subprocess,json
sql="SELECT coalesce(json_agg(json_build_object('operation_id',operation_id,'kind',kind,'state',state,'last_error',last_error,'attempts',attempts,'desired',desired)),'[]'::json) FROM device_runtime_outbox WHERE device_id='66b336f2-abb7-4521-81bb-6c93d7bcc67b';"
raw=subprocess.check_output(['docker','exec','timescaledb','psql','-X','-At','-U','postgres','-d','native_main_20261028','-c',sql]);rows=json.loads(raw)
for r in rows:
 d=r.pop('desired');r['change']=d.get('change');r['credentialId']=d.get('credentialId');r['replacementCredentialId']=d.get('replacementCredentialId')
print(json.dumps({'outbox':rows}))
