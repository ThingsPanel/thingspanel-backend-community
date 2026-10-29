"""Run an existing, root-authorized source grant renewal without user sessions."""
import datetime
import json
import os
import pathlib
import subprocess

ROOT=pathlib.Path('/opt/tp-community-main-20261028')
POLICY=ROOT/'notification-renew-policy.private.json'
RECEIPT=ROOT/'notification-renew-result.json'
BINARY=pathlib.Path('/opt/tp-community-actions/notification-identity/notification-source-provision')

def main():
    os.umask(0o077)
    if os.geteuid()!=0:raise SystemExit('Notification renewal requires deployment root')
    environment=os.environ.copy()
    config=json.loads((ROOT/'compose.encore.private.json').read_text())
    environment.update(config['services']['notification-core']['environment'])
    environment.update(NOTIFICATION_PROVISION_EXPECTED_DB='notification_main_20261028',NOTIFICATION_PROVISION_EXPECTED_HOST='127.0.0.1:5432')
    result=subprocess.run([str(BINARY),'--service-policy',str(POLICY),'--ttl','8h'],env=environment,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=30)
    if result.returncode:
        # Do not copy process output, config, credentials or a database URI to logs.
        temporary=RECEIPT.with_suffix('.tmp')
        temporary.write_text(json.dumps({'status':'failed','observed_at_utc':datetime.datetime.utcnow().isoformat()+'Z'}))
        temporary.replace(RECEIPT)
        raise SystemExit('Notification service identity renewal failed')
    print('Existing scoped notification identity renewed')

if __name__=='__main__':main()
