"""Inventory identity/reference blockers in a verified, isolated source restore.

This is read-only. It does not choose a new owner, rename a device, infer missing
state, or equate a clean inventory with a completed native data conversion.
"""
import collections
import hashlib
import json
import os
import pathlib
import re
import subprocess
import uuid


def legacy_principal_id(raw):
    # Mirrors the currently deployed NormalizePrincipalID only to detect its
    # collisions. This function is never used to grant access or write an ID.
    value = (raw or '').strip()
    if not value:
        return str(uuid.UUID(int=0))
    try:
        return str(uuid.UUID(value))
    except ValueError:
        digits = ''.join(c.lower() for c in value if c in '0123456789abcdefABCDEF')
        digits = digits or value.encode().hex()
        return str(uuid.UUID((digits + '0' * 32)[:32]))


def inventory(source):
    issues = collections.defaultdict(list)
    def issue(code, kind, identity):
        issues[code].append({'kind': kind, 'id': identity})
    def index(kind):
        result = {}
        for row in source[kind]:
            if row['id'] in result:
                issue('duplicate_source_identity', kind, row['id'])
            result[row['id']] = row
        return result
    devices, templates, groups = index('devices'), index('templates'), index('groups')
    tenants = collections.defaultdict(set)
    for raw in source.get('tenant_ids', []):
        if raw:
            tenants[legacy_principal_id(raw)].add(raw)
    for kind in ('devices', 'templates', 'groups'):
        for row in source[kind]:
            raw = row.get('tenant_id') or ''
            if not raw.strip():
                issue('missing_tenant', kind, row['id'])
            else:
                tenants[legacy_principal_id(raw)].add(raw)
            try:
                if str(uuid.UUID(row['id'])) != row['id'] or uuid.UUID(row['id']).int == 0:
                    raise ValueError()
            except ValueError:
                issue('noncanonical_resource_id', kind, row['id'])
    for canonical, originals in tenants.items():
        if len(originals) > 1:
            for raw in sorted(originals):
                issue('tenant_normalization_collision', 'tenant', raw)
    numbers = collections.defaultdict(list)
    for d in devices.values():
        number = d.get('device_number') or ''
        if not number or number != number.strip() or len(number) > 128:
            issue('invalid_device_number', 'devices', d['id'])
        numbers[(d.get('tenant_id'), number)].append(d['id'])
        if d.get('is_enabled') not in ('enabled', 'disabled'):
            issue('administrative_state_requires_mapping', 'devices', d['id'])
        if d.get('activate_flag') not in ('active', 'inactive'):
            issue('activation_state_requires_mapping', 'devices', d['id'])
        template_id = d.get('device_config_id')
        if template_id:
            template = templates.get(template_id)
            if template is None:
                issue('missing_template', 'devices', d['id'])
            elif template.get('tenant_id') != d.get('tenant_id'):
                issue('cross_tenant_template', 'devices', d['id'])
        parent_id = d.get('parent_id')
        if parent_id:
            parent = devices.get(parent_id)
            if parent is None:
                issue('missing_gateway', 'devices', d['id'])
            elif parent.get('tenant_id') != d.get('tenant_id'):
                issue('cross_tenant_gateway', 'devices', d['id'])
    for ids in numbers.values():
        if len(ids) > 1:
            for identity in ids:
                issue('duplicate_device_number', 'devices', identity)
    for row in source['members']:
        identity = row['group_id'] + ':' + row['device_id']
        group, device = groups.get(row['group_id']), devices.get(row['device_id'])
        if group is None or device is None:
            issue('orphan_membership', 'members', identity)
        elif len({row.get('tenant_id'), group.get('tenant_id'), device.get('tenant_id')}) != 1:
            issue('cross_tenant_membership', 'members', identity)
    for kind, rows, parent_field in (('groups', groups, 'parent_id'), ('devices', devices, 'parent_id')):
        for row in rows.values():
            parent_id = row.get(parent_field)
            visited = {row['id']}
            # Legacy groups use the literal "0" for a root; devices do not.
            # See internal/model/groups.gen.go and device_group.go.
            while parent_id and not (kind == 'groups' and parent_id == '0'):
                parent = rows.get(parent_id)
                if parent is None:
                    if kind == 'groups': issue('orphan_group', kind, row['id'])
                    break
                if parent.get('tenant_id') != row.get('tenant_id'):
                    if kind == 'groups': issue('cross_tenant_group_parent', kind, row['id'])
                    break
                if parent_id in visited:
                    issue('hierarchy_cycle', kind, row['id'])
                    break
                visited.add(parent_id)
                if len(visited) > 32:
                    issue('hierarchy_too_deep', kind, row['id'])
                    break
                parent_id = parent.get(parent_field)
    return {'format_version': 2, 'ready_for_conversion': not issues, 'migration_complete': False,
            'source_counts': {key: len(source[key]) for key in ('devices', 'templates', 'groups', 'members')},
            'issues': {key: {'count': len(rows), 'samples': rows[:20]} for key, rows in sorted(issues.items())},
            'administrative_states': dict(collections.Counter(d.get('is_enabled') for d in devices.values())),
            'activation_states': dict(collections.Counter(d.get('activate_flag') for d in devices.values()))}


def read_verified_restore(directory, container='timescaledb', user='postgres'):
    directory = pathlib.Path(directory)
    receipt = json.loads((directory / 'restore-verification.json').read_text())
    database = receipt['rehearsal_database']
    if receipt.get('status') != 'restored-and-verified' or not re.fullmatch(r'tp_upgrade_rehearsal_[0-9]+', database):
        raise RuntimeError('verified isolated restore is required')
    query = """BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='60s';
SELECT json_build_object(
 'devices',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,tenant_id,device_number,device_config_id,parent_id,is_enabled,activate_flag FROM devices ORDER BY id)t),
 'templates',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,tenant_id,device_type,protocol_type,native_thing_model_id,native_thing_model_snapshot_id FROM device_configs ORDER BY id)t),
 'groups',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT id,tenant_id,parent_id FROM groups ORDER BY id)t),
 'members',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT group_id,device_id,tenant_id FROM r_group_device ORDER BY group_id,device_id)t),
 'tenant_ids',(SELECT coalesce(json_agg(tenant_id),'[]'::json) FROM (SELECT DISTINCT tenant_id FROM users)t)
); ROLLBACK;
"""
    p = subprocess.run(['docker','exec','-i',container,'psql','-X','-q','-At','-v','ON_ERROR_STOP=1',
                        '-U',user,'-d',database],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
    if p.returncode: raise RuntimeError('isolated source inventory query failed')
    source = json.loads(p.stdout)
    report = inventory(source)
    report['source_archive_sha256'] = receipt['archive_sha256']
    report['inventory_sha256'] = hashlib.sha256(p.stdout).hexdigest()
    os.umask(0o077)
    # Only IDs, states and relationship references; no passwords or vouchers.
    output = directory / 'identity-inventory.v2.private.json'
    if output.exists():
        if json.loads(output.read_text(encoding='utf-8')) != report:
            raise RuntimeError('existing inventory differs; retain and investigate before creating another plan')
    else:
        with output.open('x', encoding='utf-8') as out:
            json.dump(report,out,ensure_ascii=False,indent=2)
    return report
