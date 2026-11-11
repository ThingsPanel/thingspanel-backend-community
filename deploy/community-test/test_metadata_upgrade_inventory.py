import copy
import unittest
from metadata_upgrade_inventory import inventory, legacy_principal_id

A='11111111-1111-4111-8111-111111111111'
B='22222222-2222-4222-8222-222222222222'
C='33333333-3333-4333-8333-333333333333'


class MetadataInventoryTest(unittest.TestCase):
    def fixture(self):
        return {'devices':[{'id':A,'tenant_id':'tenant-a','device_number':'number-1',
            'is_enabled':'enabled','activate_flag':'active','device_config_id':B}],
            'templates':[{'id':B,'tenant_id':'tenant-a'}],
            'groups':[{'id':C,'tenant_id':'tenant-a','parent_id':None}],
            'members':[{'group_id':C,'device_id':A,'tenant_id':'tenant-a'}], 'tenant_ids':['tenant-a']}

    def test_clean_inventory_is_not_migration_completion(self):
        source=self.fixture();before=copy.deepcopy(source)
        report=inventory(source)
        self.assertTrue(report['ready_for_conversion'])
        self.assertFalse(report['migration_complete'])
        self.assertEqual(before,source)

    def test_cross_tenant_references_and_unknown_states_are_not_repaired(self):
        source=self.fixture()
        source['templates'][0]['tenant_id']='tenant-b'
        source['members'][0]['tenant_id']='tenant-b'
        source['devices'][0]['is_enabled']=''
        report=inventory(source)
        for key in ('cross_tenant_template','cross_tenant_membership','administrative_state_requires_mapping'):
            self.assertEqual(1,report['issues'][key]['count'])
        self.assertEqual('',source['devices'][0]['is_enabled'])

    def test_existing_normalization_collision_blocks_cutover(self):
        self.assertEqual(legacy_principal_id('a-b'),legacy_principal_id('ab'))
        source=self.fixture();source['tenant_ids']=['a-b','ab']
        self.assertEqual(2,inventory(source)['issues']['tenant_normalization_collision']['count'])

    def test_cycles_and_unowned_rows_are_reported_without_recursion(self):
        source=self.fixture();source['groups'][0]['parent_id']=C;source['devices'][0]['tenant_id']=''
        issues=inventory(source)['issues']
        self.assertEqual(1,issues['hierarchy_cycle']['count'])
        self.assertEqual(1,issues['missing_tenant']['count'])

    def test_legacy_group_root_sentinel_is_not_an_orphan(self):
        source=self.fixture();source['groups'][0]['parent_id']='0'
        self.assertTrue(inventory(source)['ready_for_conversion'])
        source['devices'][0]['parent_id']='0'
        self.assertIn('missing_gateway',inventory(source)['issues'])


if __name__=='__main__':unittest.main()
