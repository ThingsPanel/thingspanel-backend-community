import copy
import unittest

from rollout_preflight import configuration_issues, metadata_issues


class RolloutPreflightTest(unittest.TestCase):
    def setUp(self):
        self.platform = {'relational': {'driver': 'postgres', 'postgres': {'platform': {
            'database': 'fixture', 'username': 'fixture', 'password': 'fixture-only'}}}}
        self.runtime = {'hosted_services': ['authn', 'thingmodel', 'devicemetadata',
            'fileassets', 'connectorcatalog', 'automation'], 'sql_servers': [{'databases': {
                name: {'name': name} for name in ('device_metadata', 'automation',
                    'notification', 'file_storage', 'connector_runtime')}}]}

    def test_old_host_is_rejected_before_process_replacement(self):
        old = copy.deepcopy(self.runtime)
        old['hosted_services'] = ['authn', 'thingmodel', 'automation']
        old['sql_servers'][0]['databases'].pop('file_storage')
        old['sql_servers'][0]['databases'].pop('connector_runtime')
        issues = configuration_issues({'db': {'psql': {'password': 'never-print-this'}}}, old)
        self.assertEqual(6, len(issues))
        self.assertNotIn('never-print-this', str(issues))

    def test_duplicate_resource_mapping_is_ambiguous(self):
        self.assertEqual([], configuration_issues(self.platform, self.runtime))
        self.runtime['sql_servers'].append({'databases': {'other': {'name': 'device_metadata'}}})
        self.assertEqual(1, len(configuration_issues(self.platform, self.runtime)))

    def test_empty_native_catalog_does_not_hide_source_devices(self):
        source = dict(devices=48000, templates=25, groups=5, group_members=40)
        target = dict(devices=0, templates=0, groups=0, group_members=0,
                      incomplete_devices=0, incomplete_templates=0)
        self.assertEqual(4, len(metadata_issues(source, target)))
        target.update(source)
        target['incomplete_devices'] = 1
        self.assertEqual(1, len(metadata_issues(source, target)))

    def test_supported_shared_settings_and_dsn_are_not_rejected(self):
        shared={'relational':{'driver':'postgres','postgres':{'username':'fixture','platform':{'database':'fixture'}}}}
        dsn={'relational':{'driver':'postgres','postgres':{'platform':{'dsn':'private-connection-string'}}}}
        self.assertEqual([],configuration_issues(shared,self.runtime))
        self.assertEqual([],configuration_issues(dsn,self.runtime))


if __name__ == '__main__':
    unittest.main()
