"""Validate complete offline migration evidence against freshly read contents.

An inventory, backup receipt, partial converter receipt, or equal row counts is
not deployment approval. This module does not create acceptance receipts.
"""
import re

SOURCE_TABLES = {
    'devices', 'device_configs', 'device_templates', 'groups', 'r_group_device',
    'products', 'users', 'roles', 'casbin_rule', 'app_push_devices',
    'device_model_telemetry', 'device_model_attributes', 'device_model_events',
    'device_model_commands', 'device_model_custom_commands', 'device_topic_mappings',
    'data_scripts', 'alarm_config', 'scene_info', 'scene_automations',
    'notification_groups', 'notification_tenant_default_policies',
}
NATIVE_TABLES = {
    'devices', 'device_access_templates', 'device_access_bindings',
    'device_credentials', 'device_groups', 'device_group_members',
    'gateway_memberships', 'thing_models', 'thing_model_items',
    'thing_model_snapshots', 'thing_model_resources',
}
COVERAGE = ('devices', 'templates', 'groups', 'group_members',
            'gateway_relations', 'template_relations')
REQUIRED_CHECKS = ('tenant_ownership', 'template_configuration', 'model_semantics',
                   'credential_continuity', 'runtime_identity', 'gateway_hierarchy',
                   'notification_and_automation_bindings')


def fingerprint_query(tables):
    # Tables must be selected from fixed owner schemas, never receipt input.
    if not set(tables) <= SOURCE_TABLES | NATIVE_TABLES:
        raise ValueError('unrecognized acceptance table')
    queries = []
    for name in sorted(tables):
        queries.append("SELECT '" + name + "' AS name,count(*) AS rows,"
                       "md5(coalesce(string_agg(md5(to_jsonb(t)::text),'' "
                       "ORDER BY md5(to_jsonb(t)::text)),'')) AS fingerprint "
                       'FROM public."' + name + '" t')
    return "SELECT coalesce(json_agg(t),'[]'::json) FROM (" + ' UNION ALL '.join(queries) + ') t'


def valid_manifest(rows, required):
    if not isinstance(rows, list) or len(rows) != len(required):
        return None
    result = {}
    for row in rows:
        if not isinstance(row, dict) or set(row) != {'name', 'rows', 'fingerprint'}:
            return None
        if (row['name'] not in required or row['name'] in result
                or type(row['rows']) is not int or row['rows'] < 0
                or not isinstance(row['fingerprint'], str)
                or not re.fullmatch('[0-9a-f]{32}', row['fingerprint'])):
            return None
        result[row['name']] = row
    return result


def acceptance_issues(receipt, source, native, native_database):
    if not isinstance(receipt, dict) or receipt.get('status') != 'complete-reconciled':
        return ['complete migration acceptance is missing; partial conversions cannot authorize cutover']
    issues = []
    if receipt.get('source_database') != 'ThingsPanel' or receipt.get('native_database') != native_database:
        issues.append('migration acceptance belongs to different databases')
    for field in ('source_archive_sha256', 'quarantine_archive_sha256'):
        value = receipt.get(field)
        if not isinstance(value, str) or not re.fullmatch('[0-9a-f]{64}', value):
            issues.append('migration acceptance lacks verified archive identities')
    for label, actual, required in (('source', source, SOURCE_TABLES), ('native', native, NATIVE_TABLES)):
        current = valid_manifest(actual, required)
        expected = valid_manifest(receipt.get(label + '_manifest'), required)
        if current is None or expected is None or current != expected:
            issues.append(label + ' content differs from full migration acceptance; reconcile again')
    coverage = receipt.get('coverage', {})
    if not isinstance(coverage, dict):
        coverage = {}
    for name in COVERAGE:
        item = coverage.get(name)
        if (not isinstance(item, dict) or set(item) != {'source', 'converted', 'quarantined', 'pending'}
                or any(type(v) is not int or v < 0 for v in item.values())
                or item['pending'] != 0 or item['source'] != item['converted'] + item['quarantined']):
            issues.append('migration accounting is incomplete for ' + name)
    source_rows = valid_manifest(source, SOURCE_TABLES)
    native_rows = valid_manifest(native, NATIVE_TABLES)
    for name, source_table, native_table in (
            ('devices', 'devices', 'devices'),
            ('templates', 'device_configs', 'device_access_templates'),
            ('groups', 'groups', 'device_groups'),
            ('group_members', 'r_group_device', 'device_group_members')):
        item = coverage.get(name)
        if (source_rows is not None and native_rows is not None and isinstance(item, dict)
                and (item.get('source') != source_rows[source_table]['rows']
                     or item.get('converted') != native_rows[native_table]['rows'])):
            issues.append('migration accounting does not match database rows for ' + name)
    checks = receipt.get('checks', {})
    if not isinstance(checks, dict):
        checks = {}
    for name in REQUIRED_CHECKS:
        if checks.get(name) is not True:
            issues.append('migration semantic verification is missing for ' + name)
    return issues
