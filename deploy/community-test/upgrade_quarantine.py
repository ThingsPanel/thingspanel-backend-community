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
        reason, identity = decision['reason'], decision['device_id']
        key = (reason, identity, decision.get('group_id'))
        if key in seen:
            raise ValueError('duplicate approval')
        seen.add(key)
        device = devices.get(identity)
        if device is None:
            raise ValueError('approved device is absent')
        rows = {'device': device}
        if reason == 'missing-device-tenant':
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
            'excluded_relationships': sum(e['excluded_from_conversion']['entity'] != 'device' for e in archive['entries']),
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
    device_ids, template_ids, group_ids = (literals(k) for k in ('device_id','template_id','group_id'))
    query = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='30s';
SELECT json_build_object(
 'devices',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM devices WHERE id IN ({device_ids}) ORDER BY id)t),
 'templates',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM device_configs WHERE id IN ({template_ids}) ORDER BY id)t),
 'groups',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM groups WHERE id IN ({group_ids}) ORDER BY id)t),
 'members',(SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT * FROM r_group_device WHERE group_id IN ({group_ids}) AND device_id IN ({device_ids}) ORDER BY group_id,device_id)t)
); ROLLBACK;"""
    p = subprocess.run(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1',
                        '-U','postgres','-d',database],input=query.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
    if p.returncode:
        raise RuntimeError('private quarantine read failed')
    return write_private_archive(directory, build_archive(json.loads(p.stdout), approval, receipt))
