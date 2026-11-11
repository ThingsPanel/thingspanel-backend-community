import copy
import pathlib
import tempfile
import unittest

from upgrade_platform_config import convert_platform_config, stage_platform_config


class PlatformConfigUpgradeTest(unittest.TestCase):
    def source(self):
        return {'db':{'psql':{'dbname':'legacy','username':'user','password':'fixture $ : password',
                             'port':5432,'open_conns':20,'time_zone':'Asia/Shanghai'},
                      'redis':{'password':'fixture-redis'}},
                'mqtt':{'broker':'127.0.0.1:1883','enable_shared_subscription':False},
                'automation':{'mode':'native'}}

    def test_preserves_credentials_and_unrelated_values_without_mutating_source(self):
        source=self.source();before=copy.deepcopy(source)
        target=convert_platform_config(source)
        self.assertEqual(before,source)
        self.assertNotIn('psql',target['db'])
        settings=target['relational']['postgres']['platform']
        self.assertEqual('legacy',settings['database'])
        for key,value in source['db']['psql'].items():
            self.assertEqual(value,settings['database' if key=='dbname' else key])
        for key in ('mqtt','automation'):self.assertEqual(source[key],target[key])
        self.assertEqual(source['db']['redis'],target['db']['redis'])

    def test_refuses_existing_config_and_unmapped_options(self):
        source=self.source();source['relational']={'driver':'mysql'}
        with self.assertRaises(ValueError):convert_platform_config(source)

    def test_legacy_millisecond_threshold_and_logger_defaults_are_preserved(self):
        source=self.source();source['db']['psql']['slow_threshold']=250
        settings=convert_platform_config(source)['relational']['postgres']['platform']
        self.assertEqual(250,settings['slow_threshold_ms'])
        self.assertEqual(1,settings['log_level'])
        self.assertNotIn('slow_threshold',settings)
        source['db']['psql']['slow_threshold_ms']=300
        with self.assertRaises(ValueError):convert_platform_config(source)
        source=self.source();source['db']['psql']['unsupported-option']=True
        with self.assertRaises(ValueError):convert_platform_config(source)

    def test_staging_does_not_replace_live_file_and_detects_later_changes(self):
        import yaml
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory);source=root/'live.yml'
            source.write_text(yaml.safe_dump(self.source()),encoding='utf-8')
            before=source.read_bytes()
            receipt=stage_platform_config(source,root)
            self.assertEqual(before,source.read_bytes())
            self.assertEqual(receipt,stage_platform_config(source,root))
            self.assertFalse(receipt['service_restarted'])
            self.assertNotIn('password',str(receipt))
            source.write_bytes(before+b'\n')
            with self.assertRaises(RuntimeError):stage_platform_config(source,root)


if __name__=='__main__':unittest.main()
