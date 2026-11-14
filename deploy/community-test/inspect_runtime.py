"""Read-only runtime identity and queue diagnostics; never print credentials."""
import fcntl
import json
import pathlib
import shutil
import subprocess


def run(args, data=None):
    result = subprocess.run(args, input=data, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=30)
    if result.returncode:
        raise RuntimeError('Read-only diagnostic command failed')
    return result.stdout.decode().strip()


def main():
    root = pathlib.Path('/opt/tp-community-main-20261028')
    with pathlib.Path('/opt/tp-community-actions/deployment.lock').open('r') as lock:
        fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
        config = json.loads((root / 'runtime.encore.private.json').read_text())
        databases = [name for server in config['sql_servers']
                     for name, spec in server['databases'].items()
                     if spec.get('name') == 'device_metadata']
        if len(databases) != 1:
            raise RuntimeError('Native device database identity is ambiguous')

        def sql(query):
            query = "BEGIN READ ONLY; SET LOCAL statement_timeout='15s'; " + query + '; COMMIT;'
            return json.loads(run(['docker', 'exec', '-i', 'timescaledb', 'psql',
                                   '-X', '-q', '-At', '-v', 'ON_ERROR_STOP=1',
                                   '-U', 'postgres', '-d', databases[0]], query.encode()))

        queue = sql("""SELECT COALESCE(json_agg(t),'[]'::json) FROM (
            SELECT o.kind,o.state,count(*) AS count,min(o.created_at) AS oldest
            FROM device_runtime_outbox o JOIN metadata_operations m
              ON m.tenant_id=o.tenant_id AND m.id=o.operation_id
            WHERE o.state IN ('pending','failed') AND m.state='pending'
            GROUP BY o.kind,o.state ORDER BY o.kind,o.state) t""")
        prefix = sql("""SELECT COALESCE(json_agg(t),'[]'::json) FROM (
            SELECT o.id,o.kind,o.state,o.created_at,m.updated_at AS operation_updated_at
            FROM device_runtime_outbox o JOIN metadata_operations m
              ON m.tenant_id=o.tenant_id AND m.id=o.operation_id
            WHERE o.state IN ('pending','failed') AND m.state='pending'
            ORDER BY o.created_at,o.id LIMIT 50) t""")
        containers = []
        for container in run(['docker', 'ps', '-q']).splitlines():
            info = json.loads(run(['docker', 'inspect', container]))[0]
            env = dict(x.split('=', 1) for x in info['Config'].get('Env', []) if '=' in x)
            containers.append({'name': info['Name'].lstrip('/'), 'image': info['Config']['Image'],
                               'image_id': info['Image'], 'running': info['State']['Running'],
                               'workers': {key: env[key] for key in (
                                   'TP_BACKGROUND_WORKERS', 'TP_RUNTIME_OUTBOX_DISPATCH') if key in env}})
        print(json.dumps({'read_only': True, 'device_database': databases[0],
                          'queue': queue, 'oldest_50': prefix, 'containers': containers,
                          'disk_free_bytes': shutil.disk_usage(str(root)).free}))


if __name__ == '__main__':
    main()
