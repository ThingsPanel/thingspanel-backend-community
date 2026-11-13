import unittest
import json, pathlib, tempfile, types
from unittest.mock import patch
from provision_native_runtime import align_target_publisher, provision_target_publisher

class PublisherAlignmentTest(unittest.TestCase):
    def configuration(self):
        return {'mqtt': {'input_profile': 'ingress_v1', 'username': 'service', 'password': 'test-secret', 'client_id': 'base'}}
    def service(self):
        return {'username': 'service', 'password': 'test-secret', 'allowed_client_ids': ['base'], 'target_scoped_publisher': True, 'allowed_device_ids': []}
    def test_matches_existing_owner_without_expanding_scope(self):
        config=self.configuration(); service=self.service()
        align_target_publisher(config,[service])
        self.assertTrue(config['mqtt']['target_scoped_publisher'])
        self.assertEqual(service['allowed_device_ids'],[])
        align_target_publisher(config,[service])
        self.assertTrue(config['mqtt']['target_scoped_publisher'])
    def test_does_not_enable_for_finite_owner_or_other_profile(self):
        config=self.configuration(); service=self.service();service['target_scoped_publisher']=False
        align_target_publisher(config,[service]);self.assertFalse(config['mqtt']['target_scoped_publisher'])
        config={'mqtt':{'input_profile':'legacy'}};align_target_publisher(config,[])
        self.assertNotIn('target_scoped_publisher',config['mqtt'])
    def test_rejects_wrong_identity_ambiguous_and_plugin_owner(self):
        for field,value in [('username','other'),('password','other'),('allowed_client_ids',['other']),('plugin_socket',True)]:
            config=self.configuration(); service=self.service();service[field]=value
            with self.assertRaises(RuntimeError):align_target_publisher(config,[service])
            self.assertNotIn('target_scoped_publisher',config['mqtt'])
        with self.assertRaises(RuntimeError):align_target_publisher(self.configuration(),[self.service(),self.service()])
    def test_reads_exact_service_file_or_parent_mount(self):
        with tempfile.TemporaryDirectory() as directory:
            file=pathlib.Path(directory)/'broker-services.json'
            file.write_text(json.dumps([self.service()]))
            for source,destination in [(str(file),'/run/secrets/broker-services.json'),(directory,'/run/secrets')]:
                inspection=[{'Mounts':[{'Source':source,'Destination':destination}]}]
                process=types.SimpleNamespace(returncode=0,stdout=json.dumps(inspection).encode())
                with patch('provision_native_runtime.subprocess.run',return_value=process):
                    config=self.configuration();provision_target_publisher(config)
                    self.assertTrue(config['mqtt']['target_scoped_publisher'])
if __name__=='__main__':unittest.main()
