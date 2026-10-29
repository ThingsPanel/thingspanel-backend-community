"""One bounded deployment-worker pass; systemd supplies the repetition."""
import datetime
import json
import os
import pathlib
import urllib.request

IDENTITY = pathlib.Path('/opt/tp-community-main-20261028/automation-service.private.json')
RECEIPT = pathlib.Path('/opt/tp-community-actions/automation-scheduler/last-tick.json')

def tick(identity):
    if len(identity.get('token', '')) < 32 or not identity.get('deployment_id'):
        raise ValueError('Missing worker identity')
    # The scheduler is bound to this host, never to an arbitrary credential target.
    request = urllib.request.Request('http://127.0.0.1:14000/internal/automation/tick', data=b'{}', headers={
        'Content-Type': 'application/json', 'Authorization': 'Bearer '+identity['token'],
        'X-Automation-Deployment': identity['deployment_id']})
    with urllib.request.urlopen(request, timeout=30) as response:
        if response.status != 200:
            raise ValueError('Worker refused')
        result = json.load(response)
    if not isinstance(result.get('tick'), dict) or not isinstance(result.get('replayed'), int):
        raise ValueError('Missing worker receipt')
    for key in ['pending_examined', 'outbox_drained', 'schedule_executed']:
        if not isinstance(result['tick'].get(key), int) or result['tick'][key] < 0:
            raise ValueError('Invalid worker counters')
    return result

def main():
    os.umask(0o077)
    receipt = {'observed_at_utc':datetime.datetime.utcnow().isoformat()+'Z'}
    try:
        receipt.update(status='passed', result=tick(json.loads(IDENTITY.read_text())))
    except Exception:
        receipt.update(status='failed')
    temporary = RECEIPT.with_suffix('.tmp')
    temporary.write_text(json.dumps(receipt))
    temporary.replace(RECEIPT)
    if receipt['status'] != 'passed':
        raise SystemExit('Automation scheduler failed; no credentials logged')

if __name__ == '__main__':
    main()
