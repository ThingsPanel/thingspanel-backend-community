import json,subprocess
q="""SELECT json_build_object(
'tenant','63a4b529',
'registered',count(*),
'marked_active',count(*) FILTER(WHERE d.active),
'active_unexpired',count(*) FILTER(WHERE d.active AND d.expires_at>now()),
'eligible_before_session_check',count(*) FILTER(WHERE d.active AND d.expires_at>now() AND u.status='N' AND u.auth_version=d.auth_version),
'latest_binding',max(d.updated_at),'latest_expiry',max(d.expires_at))
FROM app_push_devices d LEFT JOIN users u ON u.id=d.user_id AND u.tenant_id=d.tenant_id
WHERE d.tenant_id='63a4b529'"""
p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d','ThingsPanel'],input=('BEGIN READ ONLY;'+q+';COMMIT;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
if p.returncode:raise RuntimeError('Read-only binding inspection failed')
print(json.dumps(json.loads(p.stdout)))
