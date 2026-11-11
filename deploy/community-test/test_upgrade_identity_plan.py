import unittest
import uuid

from upgrade_identity_plan import canonical_identity, tenant_identity_plan


class IdentityPlanTest(unittest.TestCase):
    def test_existing_model_owners_are_preserved_and_legacy_collision_separated(self):
        plan=tenant_identity_plan(['63a4b529','d616bcbb','KlkRL8qX','uOr8IGlG'],
            ['48c68fbd-b8b8-5c24-b524-a67edbd2d6e7','15ed9916-6e08-5428-9548-301bcef62492'])
        self.assertTrue(plan['ready_for_conversion'])
        self.assertEqual(4,len({r['native_id'] for r in plan['entries']}))
        self.assertFalse(plan['migration_complete'])

    def test_unresolved_existing_native_owner_blocks_plan(self):
        plan=tenant_identity_plan(['63a4b529'],['80000000-0000-0000-0000-000000000000'])
        self.assertFalse(plan['ready_for_conversion'])
        self.assertEqual('existing_native_owner_unresolved',plan['errors'][0]['reason'])

    def test_uuid_alias_collision_blocks_plan_and_no_default_tenant_is_invented(self):
        native=canonical_identity('tenant','63a4b529')
        plan=tenant_identity_plan(['63a4b529',native,''],[])
        self.assertFalse(plan['ready_for_conversion'])
        self.assertEqual(2,len(plan['entries']))
        self.assertEqual('canonical_identity_collision',plan['errors'][0]['reason'])
        for raw in ('', ' ', ' tenant ', str(uuid.UUID(int=0))):
            with self.assertRaises(ValueError):canonical_identity('tenant',raw)

    def test_mapping_is_deterministic_and_retains_complete_case_sensitive_identity(self):
        self.assertEqual(tenant_identity_plan(['ab','a-b','ab'],[]),tenant_identity_plan(['a-b','ab'],[]))
        self.assertNotEqual(canonical_identity('tenant','TENANT-a'),canonical_identity('tenant','tenant-a'))
        self.assertNotEqual(canonical_identity('tenant','same'),canonical_identity('subject','same'))


if __name__=='__main__':unittest.main()
