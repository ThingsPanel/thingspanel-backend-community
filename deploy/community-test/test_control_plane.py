import copy
import sys
import unittest
from unittest.mock import MagicMock

if sys.platform == 'win32':
    sys.modules.setdefault('fcntl', MagicMock())
from deploy_control_plane import DATABASES, DEVICE_SOURCE, SERVICES, validate_frontend, validate_runtime


class ControlPlaneBoundaryTests(unittest.TestCase):
    def runtime(self):
        return {key: {'hosted_services': sorted(services), 'sql_servers': [{'databases': {
            name: {'name': row[0]} for name, row in DATABASES.items()}}]}
                for key, services in SERVICES.items()}

    def test_current_ownership_is_valid(self):
        validate_runtime(self.runtime())

    def test_native_device_hosting_is_rejected(self):
        value = self.runtime()
        value['encore-api']['hosted_services'].append('devicemetadata')
        with self.assertRaises(ValueError):
            validate_runtime(value)

    def test_copied_database_switch_is_rejected(self):
        value = self.runtime()
        value['encore-api']['sql_servers'][0]['databases']['copied_native'] = value['encore-api']['sql_servers'][0]['databases'].pop('native_main_20261028')
        with self.assertRaises(ValueError):
            validate_runtime(value)

    def test_default_full_native_frontend_is_rejected(self):
        with self.assertRaises(ValueError):
            validate_frontend({'source_commit': 'a' * 40, 'profile': 'full-native'}, 'a' * 40)

    def test_preserved_device_pages_require_new_alarm_entry(self):
        value = {'profile': 'control-plane', 'source_commit': 'a' * 40,
                 'device_source_commit': DEVICE_SOURCE, 'device_owner': 'community',
                 'overrides': [{'path': 'src/views/device/' + path + '/index.vue'} for path in
                               ['manage', 'details', 'config', 'config-detail', 'grouping']]}
        validate_frontend(value, 'a' * 40)
        old_alarm = copy.deepcopy(value)
        old_alarm['overrides'].append({'path': 'src/views/device/details/modules/give-an-alarm.vue'})
        with self.assertRaises(ValueError):
            validate_frontend(old_alarm, 'a' * 40)
        with self.assertRaises(ValueError):
            validate_frontend(value, 'b' * 40)


if __name__ == '__main__':
    unittest.main()
