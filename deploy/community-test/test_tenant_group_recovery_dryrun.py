import json
import unittest
from urllib.parse import urlsplit, parse_qs

import tenant_group_recovery_dryrun as tool


class FakeClient:
    def __init__(self):
        self.calls = []
        self.groups = [{'id': 'group-a', 'tenant_id': 'tenant-a', 'name': 'Old group', 'parent_id': '0'}]
        self.devices = {
            'child': {'id': 'child', 'tenant_id': 'tenant-a', 'parent_id': 'gateway',
                      'device_config_id': 'template-a', 'name': 'Child', 'voucher': 'SECRET'},
            'gateway': {'id': 'gateway', 'tenant_id': 'tenant-a', 'device_config_id': 'template-a',
                        'name': 'Gateway', 'voucher': 'SECRET'},
        }

    def get(self, path, missing=False):
        self.calls.append(path)
        base = urlsplit(path).path
        if base == '/api/v1/device/group':
            return {'total': 1, 'list': self.groups}
        if base == '/api/v2/device-groups':
            return {'total': 0, 'items': []}
        if base == '/api/v1/device/group/relation/list':
            return {'total': 1, 'list': [{'id': 'child'}]}
        if base.startswith('/api/v1/device/detail/'):
            return self.devices[base.rsplit('/', 1)[-1]]
        if base.startswith('/api/v2/devices/') and missing:
            return None
        if base.startswith('/api/v1/device_config/'):
            return {'id': 'template-a', 'tenant_id': 'tenant-a', 'device_type': '2',
                    'protocol_type': 'MQTT', 'template_secret': 'SECRET'}
        raise AssertionError('unexpected endpoint ' + path)


class TenantGroupRecoveryDryRunTests(unittest.TestCase):
    def test_identity_matches_go_contract_and_preserves_uuid(self):
        value = '13979f65-079c-4d52-8b89-3c1a514f1a71'
        self.assertEqual(tool.canonical_identity('device', value), value)
        self.assertNotEqual(tool.canonical_identity('tenant', 'same'), tool.canonical_identity('device', 'same'))
        with self.assertRaises(ValueError):
            tool.canonical_identity('group', '')

    def test_dependencies_and_secrets_never_become_fake_ready(self):
        client = FakeClient()
        result = tool.collect(client, 'tenant-a', {})
        self.assertEqual(result['counts']['member_devices'], 1)
        self.assertEqual(result['counts']['device_dependency_closure'], 2)
        self.assertEqual(result['counts']['templates'], 1)
        self.assertFalse(result['ready_to_apply'])
        self.assertFalse(result['migration_complete'])
        self.assertEqual(result['writes_performed'], 0)
        self.assertNotIn('SECRET', json.dumps(result))
        self.assertTrue(all(not row['credential_write_planned'] for row in result['devices']))
        self.assertEqual(result['members'], [{'group_id': 'group-a', 'device_id': 'child'}])

    def test_foreign_group_rejected(self):
        with self.assertRaises(ValueError):
            tool.plan_groups(FakeClient().groups, 'other', {}, [])

    def test_approved_parent_and_name_archived_exactly(self):
        row = {'id': 'a', 'tenant_id': 't', 'parent_id': 'missing', 'name': 'group11'}
        approvals = {'parent_resolutions': [{'source_id': 'a', 'original_parent_id': 'missing'}],
                     'name_resolutions': [{'source_id': 'a', 'original_name': 'group11', 'name': 'group11 (a)'}]}
        groups, issues = tool.plan_groups([row], 't', approvals, [])
        self.assertEqual(issues, [])
        self.assertIsNone(groups[0]['parent_id'])
        self.assertEqual(groups[0]['original_parent_id'], 'missing')
        self.assertEqual(groups[0]['original_name'], 'group11')
        self.assertEqual(groups[0]['name'], 'group11 (a)')
        row['parent_id'] = 'different'
        self.assertIn('APPROVED_PARENT_CHANGED', [x['code'] for x in tool.plan_groups([row], 't', approvals, [])[1]])

    def test_unapproved_changes_cycle_and_target_collision_block(self):
        rows = [{'id': 'a', 'tenant_id': 't', 'parent_id': 'b', 'name': 'x'},
                {'id': 'b', 'tenant_id': 't', 'parent_id': 'a', 'name': 'x'}]
        issues = tool.plan_groups(rows, 't', {}, [{'id': tool.canonical_identity('group', 'a'), 'name': 'existing'}])[1]
        self.assertIn('GROUP_CYCLE', [x['code'] for x in issues])
        self.assertIn('TARGET_GROUP_EXISTS_REQUIRES_PROVENANCE', [x['code'] for x in issues])
        rows[0]['parent_id'] = rows[1]['parent_id'] = 'missing'
        issues = tool.plan_groups(rows, 't', {}, [])[1]
        self.assertIn('UNAPPROVED_DANGLING_PARENT', [x['code'] for x in issues])
        self.assertIn('UNRESOLVED_SIBLING_NAME', [x['code'] for x in issues])

    def test_foreign_dependency_is_not_imported(self):
        client = FakeClient()
        client.devices['gateway']['tenant_id'] = 'other'
        result = tool.collect(client, 'tenant-a', {})
        self.assertIn('DEVICE_IDENTITY_OR_TENANT_UNCONFIRMED', [x['code'] for x in result['blockers']])
        self.assertTrue(next(x for x in result['devices'] if x['source_id'] == 'gateway')['unresolved'])

    def test_pagination_cannot_silently_truncate(self):
        class Unstable:
            def get(self, path):
                page = int(parse_qs(urlsplit(path).query)['page'][0])
                return {'total': 200 if page == 1 else 201, 'list': [{}] * 100}
        with self.assertRaises(ValueError):
            tool.pages(Unstable(), '/api/v1/device/group')

    def test_transport_has_no_cross_origin_or_write_capability(self):
        client = tool.ReadClient({'base': 'http://c.thingspanel.cn', 'token': 'fixture'})
        for path in ('https://evil.test/api/x', '//evil.test/api/x', '/other'):
            with self.assertRaises(ValueError):
                client.get(path)
        self.assertIsNone(tool.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://evil.test'))
        self.assertFalse(hasattr(client, 'post'))


if __name__ == '__main__':
    unittest.main()
