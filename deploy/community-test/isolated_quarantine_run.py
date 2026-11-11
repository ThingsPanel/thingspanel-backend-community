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

"""Preserve explicitly approved anomalies from a verified isolated restore.

This creates a private, immutable archive and a receipt. It never updates a
database, invents tenant ownership, or expands an approval to other anomalies.
Relationship decisions archive all participating rows, but only the specified
relationship is excluded from a future conversion; the device is not excluded.
"""
import hashlib
import json
import os
import pathlib
import re
import subprocess


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False,
                                    separators=(',', ':')).encode()).hexdigest()


def build_archive(source, approval, receipt):
    if receipt.get('status') != 'restored-and-verified':
        raise ValueError('verified restore required')
    if approval.get('source_archive_sha256') != receipt.get('archive_sha256'):
        raise ValueError('approval belongs to a different source snapshot')
    if approval.get('disposition') != 'preserve-for-later-claim':
        raise ValueError('explicit preservation disposition required')
    devices = {row['id']: row for row in source['devices']}
    templates = {row['id']: row for row in source['templates']}
    groups = {row['id']: row for row in source['groups']}
    archived, seen = [], set()
    for decision in approval['entries']:
        reason = decision['reason']
        identity = decision.get('device_id') or decision.get('template_id') or decision.get('group_id')
        key = (reason, identity, decision.get('group_id'))
        if key in seen:
            raise ValueError('duplicate approval')
        seen.add(key)
        if reason in ('missing-template-tenant', 'missing-group-tenant'):
            kind, resource = ('template', templates.get(identity)) if reason == 'missing-template-tenant' else ('group', groups.get(identity))
            if resource is None or (resource.get('tenant_id') or '').strip():
                raise ValueError('approved unowned resource changed')
            if kind == 'template' and any(d.get('device_config_id') == identity for d in devices.values()):
                raise ValueError('approved unreferenced template is now referenced')
            if kind == 'group' and (any(r['group_id'] == identity for r in source['members'])
                                    or any(g.get('parent_id') == identity for g in groups.values())):
                raise ValueError('approved empty group is now referenced')
            rows = {kind: resource}
            excluded = {'entity': kind, 'id': identity}
        else:
            device = devices.get(identity)
            if device is None:
                raise ValueError('approved device is absent')
            rows = {'device': device}
        if reason in ('missing-template-tenant', 'missing-group-tenant'):
            pass
        elif reason == 'missing-device-tenant':
            if (device.get('tenant_id') or '').strip():
                raise ValueError('device ownership changed')
            excluded = {'entity': 'device', 'id': identity}
        elif reason == 'cross-tenant-template':
            template = templates.get(decision['template_id'])
            if (template is None or device.get('device_config_id') != template['id']
                    or device.get('tenant_id') != decision['device_tenant']
                    or template.get('tenant_id') != decision['template_tenant']
                    or device.get('tenant_id') == template.get('tenant_id')):
                raise ValueError('approved template relationship changed')
            rows['template'] = template
            excluded = {'entity': 'device-template-relationship', 'device_id': identity,
                        'template_id': template['id']}
        elif reason == 'cross-tenant-group-member':
            group = groups.get(decision['group_id'])
            members = [r for r in source['members'] if r['device_id'] == identity
                       and r['group_id'] == decision['group_id']]
            if (group is None or len(members) != 1
                    or device.get('tenant_id') != decision['device_tenant']
                    or group.get('tenant_id') != decision['group_tenant']
                    or members[0].get('tenant_id') != decision['member_tenant']
                    or len({device.get('tenant_id'), group.get('tenant_id'), members[0].get('tenant_id')}) == 1):
                raise ValueError('approved group relationship changed')
            rows.update(group=group, member=members[0])
            excluded = {'entity': 'group-membership', 'device_id': identity, 'group_id': group['id']}
        elif reason in ('cross-tenant-gateway', 'self-referencing-gateway'):
            parent = devices.get(decision['parent_id'])
            if (parent is None or device.get('parent_id') != parent['id']
                    or device.get('tenant_id') != decision['device_tenant']
                    or parent.get('tenant_id') != decision['parent_tenant']):
                raise ValueError('approved gateway relationship changed')
            if reason == 'cross-tenant-gateway' and device.get('tenant_id') == parent.get('tenant_id'):
                raise ValueError('gateway relationship no longer crosses tenants')
            if reason == 'self-referencing-gateway' and device['id'] != parent['id']:
                raise ValueError('gateway relationship is no longer self-referencing')
            rows['parent_device'] = parent
            excluded = {'entity': 'gateway-relationship', 'device_id': identity, 'parent_id': parent['id']}
        else:
            raise ValueError('unsupported approval reason')
        archived.append({'decision': decision, 'excluded_from_conversion': excluded,
                         'original_rows': rows, 'original_rows_sha256': digest(rows)})
    if not archived:
        raise ValueError('empty approval')
    return {'format_version': 1, 'state': 'preserved-awaiting-claim',
            'source_archive_sha256': receipt['archive_sha256'],
            'source_database': receipt['rehearsal_database'],
            'approval': approval, 'approval_sha256': digest(approval), 'entries': archived}


def write_private_archive(directory, archive):
    directory = pathlib.Path(directory)
    os.umask(0o077)
    path = directory / ('quarantine-' + archive['approval_sha256'] + '.private.json')
    if path.exists():
        if json.loads(path.read_text(encoding='utf-8')) != archive:
            raise RuntimeError('existing archive differs; do not overwrite')
    else:
        with path.open('x', encoding='utf-8') as out:
            json.dump(archive, out, ensure_ascii=False, indent=2)
            out.flush()
            os.fsync(out.fileno())
    if os.name != 'nt' and path.stat().st_mode & 0o077:
        raise RuntimeError('quarantine file must be private')
    # Return only counts and hashes. Full original rows can contain credentials.
    return {'state': archive['state'], 'approved_entries': len(archive['entries']),
            'excluded_devices': sum(e['excluded_from_conversion']['entity'] == 'device' for e in archive['entries']),
            'excluded_templates': sum(e['excluded_from_conversion']['entity'] == 'template' for e in archive['entries']),
            'excluded_groups': sum(e['excluded_from_conversion']['entity'] == 'group' for e in archive['entries']),
            'excluded_relationships': sum(e['excluded_from_conversion']['entity'] in ('device-template-relationship','group-membership','gateway-relationship') for e in archive['entries']),
            'approval_sha256': archive['approval_sha256'], 'archive_sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
            'source_archive_sha256': archive['source_archive_sha256'],
            'source_mutated': False, 'migration_complete': False}


def preserve_approved(directory, approval):
    directory = pathlib.Path(directory)
    receipt = json.loads((directory / 'restore-verification.json').read_text())
    database = receipt['rehearsal_database']
    if receipt.get('status') != 'restored-and-verified' or not re.fullmatch(r'tp_upgrade_rehearsal_[0-9]+', database):
        raise RuntimeError('verified isolated restore required')
    if approval.get('source_archive_sha256') != receipt.get('archive_sha256'):
        raise ValueError('approval belongs to a different source snapshot')
    # IDs are validated before interpolation. Values originate in an explicit
    # approval, not in mutable application input. All reads share one snapshot.
    def literals(field):
        values = sorted({e[field] for e in approval['entries'] if field in e})
        if any(not re.fullmatch(r'[a-zA-Z0-9_-]{1,128}', v) for v in values):
            raise ValueError('invalid selection identity')
        return ','.join("'" + v + "'" for v in values) or 'NULL'
    device_ids, template_ids, group_ids, parent_ids = (literals(k) for k in ('device_id','template_id','group_id','parent_id'))
    query = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='30s';
SELECT json_build_object(
 'devices',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM devices WHERE id IN ({device_ids},{parent_ids}) OR device_config_id IN ({template_ids}) ORDER BY id)t),
 'templates',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM device_configs WHERE id IN ({template_ids}) ORDER BY id)t),
 'groups',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM groups WHERE id IN ({group_ids}) OR parent_id IN ({group_ids}) ORDER BY id)t),
 'members',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM r_group_device WHERE group_id IN ({group_ids}) ORDER BY group_id,device_id)t)
); ROLLBACK;"""
    p = subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1',
                        '-U','postgres','-d',database],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
    if p.returncode:
        raise RuntimeError('private quarantine read failed')
    return write_private_archive(directory, build_archive(json.loads(p.stdout), approval, receipt))

"""Plan native tenant identities using the existing ThingModel v2 namespace.

This is a pure reconciliation step. It never rewrites a source tenant, changes
ownership, or grants access. A source snapshot must include every raw tenant ID
and every existing native owner before its plan can be accepted for conversion.
"""
import hashlib
import json
import uuid


def canonical_identity(kind, raw):
    if not raw or not raw.strip():
        raise ValueError('missing identity requires explicit quarantine')
    # Authentication already disallows surrounding whitespace; do not turn it
    # into another identity silently during an offline conversion.
    if raw != raw.strip():
        raise ValueError('identity has surrounding whitespace')
    try:
        identity = uuid.UUID(raw)
    except ValueError:
        identity = uuid.uuid5(uuid.NAMESPACE_OID, 'thingspanel/community/' + kind + '/' + raw)
    if identity.int == 0:
        raise ValueError('zero identity')
    return str(identity)


def tenant_identity_plan(raw_ids, existing_native_owners):
    rows, owners, errors = [], {}, []
    for raw in sorted(set(raw_ids)):
        if not raw or not raw.strip():
            # Missing-tenant resources are accounted for separately; there is
            # deliberately no shared/default tenant for them.
            continue
        try:
            native = canonical_identity('tenant', raw)
        except ValueError:
            errors.append({'reason':'invalid_source_tenant','source_id':raw})
            continue
        if native in owners:
            errors.append({'reason':'canonical_identity_collision','source_ids':[owners[native],raw]})
        owners[native] = raw
        rows.append({'source_id':raw,'native_id':native})
    for owner in sorted(set(existing_native_owners)):
        if owner not in owners:
            errors.append({'reason':'existing_native_owner_unresolved','native_id':owner})
    return {'format_version':1,'mapping':'thingspanel/community/tenant UUIDv5 OID; UUID passthrough',
            'ready_for_conversion':not errors,'migration_complete':False,'entries':rows,
            'errors':errors,'entries_sha256':hashlib.sha256(json.dumps(rows,sort_keys=True,separators=(',',':')).encode()).hexdigest()}

root=pathlib.Path('/opt/tp-community-actions/upgrade-rehearsal/20261111160912')
approval={'disposition': 'preserve-for-later-claim', 'source_archive_sha256': '85ff3b5ce1c505e99a18420f5e94f270995e3f1dc55ca96a873188bbd52c4cdc', 'entries': [{'reason': 'missing-device-tenant', 'device_id': '0cce2885-2c20-f6a2-29e6-397b654d16a2'}, {'reason': 'missing-device-tenant', 'device_id': '3c4c15db-e461-22ff-07a2-9ce9ceb678a7'}, {'reason': 'cross-tenant-template', 'device_id': 'be638374-bae1-a74d-624d-e20bc00f59b8', 'template_id': '302a09e1-ab15-3c39-ee45-dd342eccfd20', 'device_tenant': 'f6482861', 'template_tenant': 'd616bcbb'}, {'reason': 'cross-tenant-group-member', 'device_id': '912c290a-8c36-f67f-bffc-70bcfd81a411', 'group_id': '1900798e-e72b-5b3d-f1eb-c2221c7c95aa', 'device_tenant': '63a4b529', 'group_tenant': 'd616bcbb', 'member_tenant': '63a4b529'}, {'reason': 'missing-template-tenant', 'template_id': '1ff8e532-5ead-b7cd-8aba-20223531c2cb'}, {'reason': 'missing-template-tenant', 'template_id': 'dd2188bf-f550-7b3c-ecbd-d3ec470bd000'}, {'reason': 'missing-template-tenant', 'template_id': '70adfe7d-8536-cc29-d34d-018cfa4a5bc8'}, {'reason': 'missing-template-tenant', 'template_id': '273160b0-aeb7-59bf-b3bd-22b87e13ed74'}, {'reason': 'missing-template-tenant', 'template_id': '462f7173-940e-95c2-f207-0d168d732b74'}, {'reason': 'missing-template-tenant', 'template_id': 'fe1738cb-6d58-0570-6be6-dc0c6b61d4d8'}, {'reason': 'missing-group-tenant', 'group_id': '82faecce-66c3-4a8f-1aae-220ed2536273'}, {'reason': 'cross-tenant-gateway', 'device_id': '681df01d-69f8-f4bd-6a83-0f7ccb9c2def', 'parent_id': '20fd11e8-0599-4df2-8574-04dd3c8b1c5b', 'device_tenant': 'test1', 'parent_tenant': '63a4b529'}, {'reason': 'self-referencing-gateway', 'device_id': '1b1c7008-16af-4305-de1a-985c9054d79f', 'parent_id': '1b1c7008-16af-4305-de1a-985c9054d79f', 'device_tenant': '63a4b529', 'parent_tenant': '63a4b529'}]}
out={"quarantine":preserve_approved(root,approval),"inventory":read_verified_restore(root)}
receipt=json.loads((root/'restore-verification.json').read_text())
def inspect(database,query):
 p=subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],input=('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout=\'30s\';'+query+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
 if p.returncode:raise RuntimeError('isolated deep inventory failed')
 return json.loads(p.stdout)
out['details']=inspect(receipt['rehearsal_database'],"""SELECT json_build_object(
 'unowned_templates',(SELECT json_agg(t) FROM (SELECT c.id,c.name,c.tenant_id,c.device_type,c.protocol_type,(SELECT count(*) FROM devices d WHERE d.device_config_id=c.id) devices FROM device_configs c WHERE coalesce(c.tenant_id,'')='')t),
 'unowned_groups',(SELECT json_agg(t) FROM (SELECT g.id,g.name,g.parent_id,(SELECT count(*) FROM r_group_device r WHERE r.group_id=g.id) members FROM groups g WHERE coalesce(g.tenant_id,'')='')t),
 'gateway_anomalies',(SELECT json_agg(t) FROM (SELECT d.id,d.name,d.tenant_id,d.parent_id,p.name parent_name,p.tenant_id parent_tenant,p.parent_id parent_parent,d.protocol,c.device_type FROM devices d JOIN devices p ON p.id=d.parent_id LEFT JOIN device_configs c ON c.id=d.device_config_id WHERE d.tenant_id<>p.tenant_id OR d.id=d.parent_id OR d.id IN ('02db9549-ad9b-50be-4b47-e3b53c2f7b8f','1b1c7008-16af-4305-de1a-985c9054d79f','b8e6e1d6-281c-055b-1f48-e230e82249f5','ca674548-93f3-cf06-fd3a-5e299ac83395'))t),
 'collision_tenants',(SELECT json_agg(t) FROM (SELECT v.tenant,(SELECT count(*) FROM users u WHERE u.tenant_id=v.tenant) users,(SELECT count(*) FROM devices d WHERE d.tenant_id=v.tenant) devices,(SELECT count(*) FROM device_configs c WHERE c.tenant_id=v.tenant) templates,(SELECT count(*) FROM groups g WHERE g.tenant_id=v.tenant) groups FROM (VALUES ('KlkRL8qX'),('uOr8IGlG')) v(tenant))t),
 'device_shapes',(SELECT json_agg(t) FROM (SELECT d.is_enabled,d.protocol,d.access_way,c.device_type,c.protocol_type,c.voucher_type,count(*) count FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id GROUP BY d.is_enabled,d.protocol,d.access_way,c.device_type,c.protocol_type,c.voucher_type ORDER BY count(*) DESC)t),
 'model_shapes',(SELECT json_agg(t) FROM (SELECT coalesce(native_thing_model_id,'')<>'' native_bound,coalesce(device_template_id,'')<>'' legacy_bound,count(*) count FROM device_configs GROUP BY 1,2)t),
 'source_config_schema',(SELECT json_agg(t) FROM (SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('device_templates','service_access','telemetry_data','attribute_data') ORDER BY table_name,ordinal_position)t)
)""")
out['native_tenants']=inspect('native_main_20261028',"""SELECT json_build_object(
 'models',(SELECT json_agg(t) FROM (SELECT tenant_id,count(*) count FROM thing_models GROUP BY tenant_id)t),
 'snapshots',(SELECT json_agg(t) FROM (SELECT tenant_id,count(*) count FROM thing_model_snapshots GROUP BY tenant_id)t)
)""")
disk=subprocess.run(['df','-B1','/opt'],stdout=subprocess.PIPE,check=True,universal_newlines=True).stdout
out['disk']=disk
raw_tenants=inspect(receipt['rehearsal_database'],"""SELECT json_agg(tenant_id) FROM (
 SELECT tenant_id FROM users UNION SELECT tenant_id FROM devices UNION SELECT tenant_id FROM device_configs
 UNION SELECT tenant_id FROM groups UNION SELECT tenant_id FROM device_templates UNION SELECT tenant_id FROM service_access
) t WHERE tenant_id IS NOT NULL""")
native_tables=inspect('native_main_20261028',"""SELECT json_agg(table_name) FROM information_schema.columns WHERE table_schema='public' AND column_name='tenant_id'""")
if any(not re.fullmatch(r'[a-zA-Z_][a-zA-Z0-9_]*',name) for name in native_tables):raise RuntimeError('invalid owner table name')
owner_union=' UNION '.join('SELECT tenant_id::text AS tenant_id FROM public."'+name+'" WHERE tenant_id IS NOT NULL' for name in native_tables)
native_owners=inspect('native_main_20261028','SELECT coalesce(json_agg(tenant_id),\'[]\'::json) FROM ('+owner_union+') t')
plan=tenant_identity_plan(raw_tenants,native_owners)
plan['source_archive_sha256']=receipt['archive_sha256']
plan['existing_native_owners']=native_owners
planpath=root/'tenant-identity-plan.v1.private.json'
if planpath.exists():
 if json.loads(planpath.read_text())!=plan:raise RuntimeError('identity plan differs; retain original')
else:
 with planpath.open('x',encoding='utf-8') as outplan:json.dump(plan,outplan,ensure_ascii=False,indent=2)
out['identity_plan']={k:v for k,v in plan.items() if k not in ('entries','existing_native_owners')}
out['identity_plan']['source_tenants']=len(plan['entries'])
out['identity_plan']['existing_native_tenants']=len(native_owners)

print(json.dumps(out,ensure_ascii=False))
