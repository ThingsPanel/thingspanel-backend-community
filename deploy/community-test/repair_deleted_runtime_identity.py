"""Restricted recovery of the recorded QA deletion identity mismatch; dry-run by default."""
import datetime, fcntl, hashlib, json, os, pathlib, subprocess, sys, time, urllib.request
DEVICE='758f9efd-67d6-4e16-8a36-441d8972135c'
TENANT='63a4b529'
NATIVE_TENANT='48c68fbd-b8b8-5c24-b524-a67edbd2d6e7'
BUILD='443db33bac1c6ec04e353abb65ec57d9a491aa7f'
ROOT=pathlib.Path('/opt/tp-community-actions')
APP=pathlib.Path('/home/thingspanel/thingspanel-backend-community')

def run(args, data=None):
    p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    if p.returncode: raise RuntimeError('Recovery command failed; no private command output emitted')
    return p.stdout.decode().strip()

def sql(database, statement):
    return run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],statement.encode())

def main():
    apply=sys.argv[1:]==['--apply']
    if sys.argv[1:] not in ([],['--apply']): raise RuntimeError('Invalid mode')
    os.umask(0o077)
    with (ROOT/'deployment.lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        binary=APP/'thingspanel-backend-community'
        manifest=json.loads((ROOT/'backend-releases'/BUILD/'manifest.json').read_text())
        if manifest.get('commit')!=BUILD or hashlib.sha256(binary.read_bytes()).hexdigest()!=manifest['sha256']['ThingsPanel-Go']:
            raise RuntimeError('Current backend must be the verified rolled-back build')
        if run([str(binary),'--version'])!='dev-'+BUILD: raise RuntimeError('Wrong current version')
        config=json.loads(pathlib.Path('/opt/tp-community-main-20261028/runtime.encore.private.json').read_text())
        databases=[name for server in config['sql_servers'] for name,spec in server['databases'].items() if spec.get('name')=='device_metadata']
        if len(databases)!=1: raise RuntimeError('Ambiguous native database')
        native=json.loads(sql(databases[0],"BEGIN READ ONLY; SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,tenant_id,deleted_at FROM devices WHERE id='"+DEVICE+"') t; COMMIT;"))
        if len(native)!=1 or native[0]['tenant_id']!=NATIVE_TENANT or not native[0]['deleted_at']:
            raise RuntimeError('Native deleted identity guard failed')
        old=json.loads(sql('ThingsPanel',"BEGIN READ ONLY; SELECT row_to_json(r) FROM device_id_reservations r WHERE id='"+DEVICE+"'; COMMIT;"))
        if old.get('tenant_id')!=TENANT or old.get('state')!='tombstoned' or not old.get('deleted_at'):
            raise RuntimeError('Reservation is not the expected tombstone')
        stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
        audit=ROOT/('identity-recovery-'+stamp+'.private.json')
        with audit.open('x') as stream: json.dump({'device_id':DEVICE,'reservation_before':old,'native_before':native,'apply':apply},stream)
        transaction="""BEGIN; SET LOCAL statement_timeout='15s'; SET LOCAL lock_timeout='5s';
SELECT pg_advisory_xact_lock(hashtextextended('%s',72311031));
DO $guard$ DECLARE n integer; BEGIN
PERFORM 1 FROM devices WHERE id='%s' FOR UPDATE;
PERFORM 1 FROM device_id_reservations WHERE id='%s' FOR UPDATE;
PERFORM 1 FROM native_runtime_registrations WHERE device_id='%s' FOR UPDATE;
SELECT count(*) INTO n FROM devices d LEFT JOIN device_id_reservations r ON r.id=d.id
 WHERE r.id IS NULL OR r.tenant_id<>d.tenant_id OR r.state<>'active';
IF n<>1 THEN RAISE EXCEPTION 'Conflict count changed'; END IF;
IF NOT EXISTS(SELECT 1 FROM devices d JOIN device_id_reservations r ON r.id=d.id JOIN native_runtime_registrations o ON o.device_id=d.id
 WHERE d.id='%s' AND d.tenant_id='%s' AND r.tenant_id=d.tenant_id AND o.tenant_id=d.tenant_id
 AND r.state='tombstoned' AND r.deleted_at IS NOT NULL AND d.is_enabled='disabled' AND d.is_online=0
 AND o.native_tenant_id='%s' AND o.active=false AND o.deleting=true AND o.lifecycle_generation=1)
 THEN RAISE EXCEPTION 'Identity guard changed'; END IF;
IF NOT EXISTS(SELECT 1 FROM native_runtime_lifecycle_events WHERE device_id='%s' AND generation=1 AND applied=true)
 THEN RAISE EXCEPTION 'Deletion event not applied'; END IF;
IF EXISTS(SELECT 1 FROM native_runtime_credential_leases WHERE device_id='%s' AND revoked_ack_at IS NULL AND expires_at>now())
 THEN RAISE EXCEPTION 'Live lease remains'; END IF;
UPDATE device_id_reservations SET state='active',deleted_at=NULL WHERE id='%s' AND tenant_id='%s' AND state='tombstoned';
GET DIAGNOSTICS n=ROW_COUNT; IF n<>1 THEN RAISE EXCEPTION 'Expected one representation update'; END IF;
END $guard$;
%s;"""%(DEVICE,DEVICE,DEVICE,DEVICE,DEVICE,TENANT,NATIVE_TENANT,DEVICE,DEVICE,DEVICE,TENANT,'COMMIT' if apply else 'ROLLBACK')
        sql('ThingsPanel',transaction)
        if not apply:
            print(json.dumps({'status':'dry-run-passed','device_id':DEVICE,'business_build':BUILD}));return
        run(['/root/.local/share/pnpm/pm2','restart','thingspanel-backend-community'])
        for attempt in range(60):
            try:
                with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version',timeout=3) as response: version=json.load(response).get('data',{}).get('version')
                with urllib.request.urlopen('http://127.0.0.1:9999/health',timeout=3) as response: healthy=response.status==200
                if version=='dev-'+BUILD and healthy:
                    print(json.dumps({'status':'restored','device_id':DEVICE,'business_build':BUILD}));return
            except (OSError,ValueError): pass
            time.sleep(2)
        raise RuntimeError('Representation repaired, but backend health needs inspection; no further mutation attempted')

if __name__=='__main__': main()
