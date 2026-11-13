"""Read-only tenant group recovery inventory. This program has NO apply mode.

It preserves the offlineupgrade canonical identity and explicit resolution rules,
but never invokes (or relaxes) the rehearsal-only Apply*PG functions. API reads
are not a transaction snapshot or authority to import runtime identities.
"""
import argparse
import hashlib
import json
import pathlib
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timezone


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':'),
                                     ensure_ascii=False).encode()).hexdigest()


def canonical_identity(kind, value):
    """Same namespace and UUID-preservation contract as tenantidentity.Go."""
    if not isinstance(value, str) or not value.strip():
        raise ValueError('missing source identity')
    try:
        return str(uuid.UUID(value))
    except ValueError:
        return str(uuid.uuid5(uuid.NAMESPACE_OID, 'thingspanel/community/' + kind + '/' + value))


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class ReadClient:
    def __init__(self, session):
        parsed = urllib.parse.urlsplit(session['base'])
        if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ('', '/'):
            raise ValueError('session requires a single origin')
        self.base = session['base'].rstrip('/')
        self.token = session['token']
        self.http = urllib.request.build_opener(NoRedirect())

    def get(self, path, missing=False):
        if not path.startswith('/api/') or path.startswith('//') or urllib.parse.urlsplit(path).netloc:
            raise ValueError('only same-origin API reads are allowed')
        req = urllib.request.Request(self.base + path, method='GET', headers={
            'Authorization': 'Bearer ' + self.token, 'x-token': self.token})
        try:
            with self.http.open(req, timeout=20) as response:
                raw = response.read(8 * 1024 * 1024 + 1)
                if len(raw) > 8 * 1024 * 1024:
                    raise ValueError('API response exceeds inventory bound')
                value = json.loads(raw)
        except urllib.error.HTTPError as error:
            if missing and error.code == 404:
                return None
            raise ValueError('API read failed: HTTP ' + str(error.code) + ' ' + path) from None
        if not isinstance(value, dict) or ('code' in value and value['code'] != 200):
            raise ValueError('API envelope rejected: ' + path)
        return value.get('data', value)


def pages(client, path, native=False):
    result, expected = [], None
    for page in range(1, 101):
        separator = '&' if '?' in path else '?'
        data = client.get(path + separator + 'page=' + str(page) + ('&pageSize=100' if native else '&page_size=100'))
        total = data.get('total')
        if not isinstance(total, int) or total < 0 or expected is not None and expected != total:
            raise ValueError('unstable or invalid pagination')
        expected = total
        batch = data.get('items' if native else 'list') or []
        if not isinstance(batch, list):
            raise ValueError('invalid page items')
        result.extend(batch)
        if len(result) == total:
            return result
        if not batch or len(result) > total:
            raise ValueError('incomplete pagination')
    raise ValueError('inventory page bound exceeded')


def resolution_map(entries, key):
    result = {}
    for entry in entries:
        identity = entry.get('source_id')
        if not identity or identity in result or key not in entry:
            raise ValueError('invalid duplicate approved resolution')
        result[identity] = entry
    return result


def plan_groups(groups, tenant, approvals, native_groups):
    parents = resolution_map(approvals.get('parent_resolutions', []), 'original_parent_id')
    names = resolution_map(approvals.get('name_resolutions', []), 'original_name')
    rows, blockers = {}, []
    for group in groups:
        identity = group['id']
        if identity in rows or group.get('tenant_id') != tenant:
            raise ValueError('duplicate or foreign source group')
        rows[identity] = group
    native = {row['id']: row for row in native_groups}
    native_names = {(row.get('parentId'), row['name']) for row in native_groups}
    planned, sibling_names = [], set()
    for identity, row in sorted(rows.items()):
        original_parent, original_name = row.get('parent_id'), row['name']
        parent = original_parent if original_parent not in (None, '', '0') else None
        name = original_name
        if identity in parents:
            if parents[identity]['original_parent_id'] != original_parent or parent in rows:
                blockers.append({'code': 'APPROVED_PARENT_CHANGED', 'group_id': identity})
            else:
                parent = None
        if parent and parent not in rows:
            blockers.append({'code': 'UNAPPROVED_DANGLING_PARENT', 'group_id': identity})
        if identity in names:
            if names[identity]['original_name'] != original_name:
                blockers.append({'code': 'APPROVED_NAME_CHANGED', 'group_id': identity})
            else:
                name = names[identity]['name']
        if not isinstance(name, str) or not name.strip() or len(name) > 128:
            blockers.append({'code': 'INVALID_NATIVE_NAME', 'group_id': identity})
        key = (parent, name)
        if key in sibling_names:
            blockers.append({'code': 'UNRESOLVED_SIBLING_NAME', 'group_id': identity})
        sibling_names.add(key)
        target = canonical_identity('group', identity)
        if target in native:
            blockers.append({'code': 'TARGET_GROUP_EXISTS_REQUIRES_PROVENANCE', 'group_id': identity})
        if (canonical_identity('group', parent) if parent else None, name) in native_names:
            blockers.append({'code': 'TARGET_SIBLING_NAME_CONFLICT', 'group_id': identity})
        planned.append({'source_id': identity, 'id': target,
                        'tenant_id': canonical_identity('tenant', tenant),
                        'parent_id': canonical_identity('group', parent) if parent else None,
                        'name': name, 'original_parent_id': original_parent,
                        'original_name': original_name, 'source_row_sha256': digest(row)})
    by_id = {row['id']: row for row in planned}
    for row in planned:
        seen, parent = {row['id']}, row['parent_id']
        while parent in by_id:
            if parent in seen:
                blockers.append({'code': 'GROUP_CYCLE', 'group_id': row['source_id']})
                break
            seen.add(parent)
            parent = by_id[parent]['parent_id']
    return planned, blockers


def collect(client, tenant, approvals):
    groups = pages(client, '/api/v1/device/group')
    native_groups = pages(client, '/api/v2/device-groups', True)
    planned, blockers = plan_groups(groups, tenant, approvals, native_groups)
    edges, wanted = [], set()
    for group in groups:
        for member in pages(client, '/api/v1/device/group/relation/list?group_id=' + urllib.parse.quote(group['id'], safe='')):
            edges.append({'group_id': group['id'], 'device_id': member['id']})
            wanted.add(member['id'])
    if len({(x['group_id'], x['device_id']) for x in edges}) != len(edges):
        raise ValueError('duplicate source membership')
    devices, templates, pending = {}, {}, list(sorted(wanted))
    while pending:
        identity = pending.pop()
        if identity in devices:
            continue
        if len(devices) >= 10000:
            raise ValueError('dependency closure exceeds bound')
        row = client.get('/api/v1/device/detail/' + urllib.parse.quote(identity, safe=''))
        if row.get('id') != identity or row.get('tenant_id') != tenant:
            blockers.append({'code': 'DEVICE_IDENTITY_OR_TENANT_UNCONFIRMED', 'device_id': identity})
            devices[identity] = {'source_id': identity, 'unresolved': True}
            continue
        target = canonical_identity('device', identity)
        existing = client.get('/api/v2/devices/' + target, missing=True)
        if existing is not None:
            blockers.append({'code': 'TARGET_DEVICE_EXISTS_REQUIRES_PROVENANCE', 'device_id': identity})
        # Never emit voucher, protocol_config, access secrets or raw API bodies.
        fields = ('name', 'device_number', 'device_config_id', 'parent_id', 'sub_device_addr',
                  'is_enabled', 'activate_flag', 'protocol', 'access_way', 'service_access_id')
        devices[identity] = dict((key, row.get(key)) for key in fields)
        devices[identity].update(source_id=identity, id=target, source_row_sha256=digest(row),
                                 membership_device=identity in wanted,
                                 preserve_existing_runtime=True, credential_write_planned=False,
                                 runtime_identity_authority='UNCONFIRMED_NO_IMPORT_ALLOWED')
        if row.get('is_enabled') not in ('enabled', 'disabled'):
            blockers.append({'code': 'LEGACY_ENABLEMENT_REQUIRES_EXPLICIT_SEMANTICS', 'device_id': identity})
        parent = row.get('parent_id')
        if parent not in (None, '', '0'):
            pending.append(parent)
        template_id = row.get('device_config_id')
        if template_id and template_id not in templates:
            template = client.get('/api/v1/device_config/' + urllib.parse.quote(template_id, safe=''))
            if template.get('id') != template_id or template.get('tenant_id') != tenant:
                blockers.append({'code': 'TEMPLATE_IDENTITY_OR_TENANT_UNCONFIRMED', 'template_id': template_id})
            templates[template_id] = {'source_id': template_id, 'source_row_sha256': digest(template),
                                      'device_type': template.get('device_type'),
                                      'protocol_type': template.get('protocol_type'),
                                      'native_configuration_validated': False}
    if digest(groups) != digest(pages(client, '/api/v1/device/group')):
        blockers.append({'code': 'SOURCE_GROUPS_CHANGED_DURING_READ'})
    blockers.extend({'code': code} for code in (
        'API_INVENTORY_NOT_TRANSACTIONAL_ARCHIVE',
        'ACCESS_AND_RUNTIME_OWNER_CONVERSION_NOT_IMPLEMENTED',
        'NATIVE_CREDENTIAL_LIFECYCLE_FOR_IMPORTED_DEVICES_NOT_VERIFIED',
        'TENANT_SCOPED_LIVE_APPLY_NOT_IMPLEMENTED'))
    return {'mode': 'tenant-group-recovery-dry-run', 'checked_at': datetime.now(timezone.utc).isoformat(),
            'source_tenant': tenant, 'native_tenant': canonical_identity('tenant', tenant),
            'writes_performed': 0, 'ready_to_apply': False, 'migration_complete': False,
            'counts': {'groups': len(groups), 'member_edges': len(edges),
                       'member_devices': len(wanted), 'device_dependency_closure': len(devices),
                       'templates': len(templates), 'existing_native_groups': len(native_groups)},
            'approval_sha256': digest(approvals), 'source_groups_sha256': digest(groups),
            'groups': planned, 'members': edges, 'devices': list(devices.values()),
            'templates': list(templates.values()), 'blockers': blockers,
            'semantic_effects': {
                'legacy_runtime_ownership': 'Must retain existing owner ID, broker route epoch, protocol/access and device number. No register/activation/rotation calls during import.',
                'credentials': 'Keep existing credential authority and bytes; no generated replacement, no fictitious native credential. Mutation remains blocked until imported lifecycle is supported.',
                'native_main_list': 'Import adds all dependency devices to the normal native device list, not merely a hidden group-member overlay. Preserve enable/activation meaning; validate reads and controls through confirmed runtimeRef.',
                'membership': 'All endpoints must exist with tenant composite FK. Empty group-only restoration is incomplete.',
                'source_preservation': 'No changes to old groups, members, devices, templates, credentials, routes or queues.'}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--session', type=pathlib.Path, required=True)
    parser.add_argument('--tenant', required=True)
    parser.add_argument('--approvals', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    args = parser.parse_args()
    result = collect(ReadClient(json.loads(args.session.read_text(encoding='utf-8-sig'))),
                     args.tenant, json.loads(args.approvals.read_text(encoding='utf-8-sig')))
    with args.output.open('x', encoding='utf-8') as output:
        json.dump(result, output, ensure_ascii=False, indent=2)
    print(json.dumps({'ready_to_apply': False, 'writes_performed': 0, 'counts': result['counts'],
                      'blocker_codes': sorted({item['code'] for item in result['blockers']})}))


if __name__ == '__main__':
    main()
