import copy
import unittest
import json
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

from rollout_preflight import configuration_issues, metadata_issues, validate_rollout_mode, load_rollout_mode, inspect_host


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

    def mode(self):
        return {'mode': 'new-devices-preserve-legacy', 'source_database': 'ThingsPanel',
                'native_database': 'native_fixture', 'legacy_data_preserved': True}

    def test_explicit_mode_requires_exact_identity_and_boolean_acknowledgement(self):
        self.assertEqual('new-devices-preserve-legacy', validate_rollout_mode(self.mode(), 'native_fixture'))
        for change in ({'mode': 'skip'}, {'source_database': 'other'},
                       {'native_database': 'other'}, {'legacy_data_preserved': False},
                       {'legacy_data_preserved': 1}, {'unexpected': True}):
            with self.subTest(change=change):
                with self.assertRaises(ValueError):
                    validate_rollout_mode(dict(self.mode(), **change), 'native_fixture')
        with self.assertRaises(ValueError):
            validate_rollout_mode(dict(self.mode(), native_database='ThingsPanel'), 'ThingsPanel')

    def test_missing_mode_keeps_default_and_insecure_mode_is_rejected(self):
        path = MagicMock()
        path.is_symlink.return_value = False
        path.exists.return_value = False
        with patch('rollout_preflight.ROLLOUT_MODE_PATH', path):
            self.assertIsNone(load_rollout_mode('native_fixture'))
            path.exists.return_value = True
            path.is_file.return_value = True
            path.stat.return_value = SimpleNamespace(st_mode=0o100644, st_size=100)
            with self.assertRaises(ValueError): load_rollout_mode('native_fixture')
            path.stat.return_value = SimpleNamespace(st_mode=0o100600, st_size=100)
            path.read_text.return_value = json.dumps(self.mode())
            self.assertEqual('new-devices-preserve-legacy', load_rollout_mode('native_fixture'))
            path.read_text.return_value = '{broken'
            with self.assertRaises(ValueError): load_rollout_mode('native_fixture')
            path.is_symlink.return_value = True
            with self.assertRaises(ValueError): load_rollout_mode('native_fixture')

    def test_new_device_mode_still_rejects_incomplete_native_resources(self):
        source = dict(devices=48000, templates=25, groups=5, group_members=40)
        target = dict(devices=0, templates=0, groups=0, group_members=0,
                      incomplete_devices=0, incomplete_templates=0)
        self.assertEqual([], metadata_issues(source, target, preserve_legacy=True))
        self.assertEqual(4, len(metadata_issues(source, target)))
        target.update(incomplete_devices=1, incomplete_templates=1)
        self.assertEqual(2, len(metadata_issues(source, target, preserve_legacy=True)))

    def test_host_new_device_mode_keeps_configuration_gate_without_claiming_migration(self):
        runtime = copy.deepcopy(self.runtime)
        dbs = runtime['sql_servers'][0]['databases']
        dbs['native_fixture'] = dbs.pop('device_metadata')
        source = dict(devices=48000, templates=25, groups=5, group_members=40)
        target = dict(devices=0, templates=0, groups=0, group_members=0,
                      incomplete_devices=0, incomplete_templates=0)
        yaml = SimpleNamespace(safe_load=lambda _: self.platform)
        with patch.dict('sys.modules', {'yaml': yaml}), patch('pathlib.Path.read_text', return_value=json.dumps(runtime)), patch('rollout_preflight.sql_json', side_effect=[source, target, source, target]), patch('rollout_preflight.load_rollout_mode', return_value='new-devices-preserve-legacy'), patch('rollout_preflight.acceptance_issues') as acceptance:
            result = inspect_host()
            self.assertTrue(result['ready'])
            self.assertEqual('new-devices-preserve-legacy', result['scope'])
            self.assertFalse(result['legacy_data_migrated'])
            acceptance.assert_not_called()
            self.platform = {'db': {'psql': {}}}
            result = inspect_host()
            self.assertFalse(result['ready'])
            self.assertIn('relational.driver', result['issues'][0])

    def test_host_default_still_requires_complete_migration_acceptance(self):
        source = dict(devices=1, templates=1, groups=1, group_members=1)
        target = dict(source, incomplete_devices=0, incomplete_templates=0)
        yaml = SimpleNamespace(safe_load=lambda _: self.platform)
        with patch.dict('sys.modules', {'yaml': yaml}), patch('pathlib.Path.read_text', return_value=json.dumps(self.runtime)), patch('pathlib.Path.is_file', return_value=False), patch('rollout_preflight.sql_json', side_effect=[source, target]), patch('rollout_preflight.load_rollout_mode', return_value=None):
            result = inspect_host()
            self.assertFalse(result['ready'])
            self.assertEqual('prerequisites-and-full-migration-content-acceptance', result['scope'])
            self.assertTrue(any('complete migration acceptance is missing' in x for x in result['issues']))


if __name__ == '__main__':
    unittest.main()
