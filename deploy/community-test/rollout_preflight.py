"""Read-only prerequisites for the coordinated Community native cutover.

Run before stopping Gin or switching the frontend. Passing these necessary
checks does not replace the offline conversion and reconciliation acceptance.
No credentials, device payloads or connection strings are printed.
"""
import json
import pathlib
import subprocess
import sys
from upgrade_acceptance import SOURCE_TABLES, NATIVE_TABLES, fingerprint_query, acceptance_issues


REQUIRED_DATABASES = {'device_metadata', 'automation', 'notification',
                      'file_storage', 'connector_runtime'}
REQUIRED_SERVICES = {'authn', 'thingmodel', 'devicemetadata', 'fileassets',
                     'connectorcatalog', 'automation'}


def configuration_issues(platform, runtime):
    issues = []
    relational = platform.get('relational', {})
    driver = relational.get('driver')
    if driver not in ('postgres', 'mysql'):
        issues.append('platform relational.driver is missing or invalid; retired db.psql keys are not read by main')
    else:
        shared = relational.get(driver, {})
        target = shared.get('platform', {})
        # Match pkg/relational: resource fields override shared fields; a DSN
        # is an alternative, and an empty password is valid for peer auth.
        if not target.get('dsn') and not all(target.get(key) or shared.get(key) for key in ('database', 'username')):
            issues.append('platform relational database configuration is incomplete')
    databases = [db.get('name') for server in runtime.get('sql_servers', [])
                 for db in server.get('databases', {}).values()]
    for name in sorted(REQUIRED_DATABASES):
        if databases.count(name) != 1:
            issues.append('runtime requires exactly one database mapping for ' + name)
    services = {s if isinstance(s, str) else s.get('name')
                for s in runtime.get('hosted_services', [])}
    for name in sorted(REQUIRED_SERVICES - services):
        issues.append('runtime is not hosting ' + name)
    return issues


def metadata_issues(source, target):
    issues = []
    # Counts are only an early blocker, never evidence of content reconciliation.
    for name in ('devices', 'templates', 'groups', 'group_members'):
        if source[name] > 0 and target[name] == 0:
            issues.append('legacy ' + name + ' exist but native ' + name + ' are empty')
    if target['incomplete_devices']:
        issues.append('native devices have incomplete identity or access bindings')
    if target['incomplete_templates']:
        issues.append('native templates have no validated current configuration or actor')
    return issues


def sql_json(database, query):
    p = subprocess.run(['docker', 'exec', '-i', 'timescaledb', 'psql', '-X', '-q', '-At',
                        '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', database],
                       input='BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL timezone=\'UTC\'; SET LOCAL statement_timeout=\'15s\';' + query + '; ROLLBACK;',
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                       universal_newlines=True, timeout=25)
    if p.returncode:
        # Server stderr may contain user data. Do not forward it into Actions logs.
        raise RuntimeError('read-only metadata prerequisite query failed')
    return json.loads(p.stdout)


def inspect_host():
    import yaml
    app = pathlib.Path('/home/thingspanel/thingspanel-backend-community')
    root = pathlib.Path('/opt/tp-community-main-20261028')
    platform = yaml.safe_load((app / 'configs/conf.yml').read_text())
    runtime = json.loads((root / 'runtime.encore.private.json').read_text())
    issues = configuration_issues(platform, runtime)
    matches = [physical for server in runtime.get('sql_servers', [])
               for physical, db in server.get('databases', {}).items()
               if db.get('name') == 'device_metadata']
    if len(matches) != 1:
        return {'ready': False, 'issues': issues}
    source = sql_json('ThingsPanel', """SELECT json_build_object(
      'devices',(SELECT count(*) FROM devices),
      'templates',(SELECT count(*) FROM device_configs),
      'groups',(SELECT count(*) FROM groups),
      'group_members',(SELECT count(*) FROM r_group_device))""")
    target = sql_json(matches[0], """SELECT json_build_object(
      'devices',(SELECT count(*) FROM devices WHERE deleted_at IS NULL),
      'templates',(SELECT count(*) FROM device_access_templates WHERE deleted_at IS NULL),
      'groups',(SELECT count(*) FROM device_groups WHERE deleted_at IS NULL),
      'group_members',(SELECT count(*) FROM device_group_members),
      'incomplete_devices',(SELECT count(*) FROM devices d WHERE d.deleted_at IS NULL AND (
        d.device_number IS NULL OR d.name IS NULL OR d.device_kind IS NULL OR d.activation_state IS NULL
        OR d.created_by IS NULL OR d.updated_by IS NULL OR NOT EXISTS (
          SELECT 1 FROM device_access_bindings b WHERE b.tenant_id=d.tenant_id AND b.device_id=d.id))),
      'incomplete_templates',(SELECT count(*) FROM device_access_templates WHERE deleted_at IS NULL AND (
        configuration IS NULL OR created_by IS NULL OR updated_by IS NULL)))""")
    issues.extend(metadata_issues(source, target))
    acceptance_path = pathlib.Path('/opt/tp-community-actions/upgrade-acceptance.private.json')
    if not acceptance_path.is_file():
        issues.extend(acceptance_issues(None, None, None, matches[0]))
    else:
        if acceptance_path.stat().st_mode & 0o077:
            issues.append('migration acceptance must be private')
        else:
            acceptance = json.loads(acceptance_path.read_text())
            source_manifest = sql_json('ThingsPanel', fingerprint_query(SOURCE_TABLES))
            native_manifest = sql_json(matches[0], fingerprint_query(NATIVE_TABLES))
            issues.extend(acceptance_issues(acceptance, source_manifest, native_manifest, matches[0]))
    return {'ready': not issues, 'scope': 'prerequisites-and-full-migration-content-acceptance',
            'source_counts': source, 'native_counts': target, 'issues': issues}


if __name__ == '__main__':
    try:
        report = inspect_host()
    except Exception:
        report = {'ready': False, 'issues': ['Unable to verify coordinated rollout prerequisites; running services were not changed']}
    print(json.dumps(report, sort_keys=True))
    sys.exit(0 if report['ready'] else 1)
