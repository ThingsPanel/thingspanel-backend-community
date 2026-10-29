import importlib.util
import io
import json
import pathlib
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('automation_tick',pathlib.Path(__file__).with_name('automation_tick.py'))
worker=importlib.util.module_from_spec(spec);spec.loader.exec_module(worker)

class TickBoundaryTest(unittest.TestCase):
    def test_worker_uses_service_identity_without_tenant_or_arbitrary_url(self):
        response=io.BytesIO(json.dumps({'replayed':0,'tick':{'pending_examined':0,'outbox_drained':1,'schedule_executed':1}}).encode());response.status=200
        with patch.object(worker.urllib.request,'urlopen',return_value=response) as http:
            worker.tick({'token':'s'*32,'deployment_id':'deployment-a','base_url':'http://untrusted.invalid'})
        request=http.call_args[0][0]
        self.assertEqual(request.full_url,'http://127.0.0.1:14000/internal/automation/tick')
        self.assertEqual(request.get_header('Authorization'),'Bearer '+'s'*32)
        self.assertFalse(request.has_header('X-automation-tenant'))

    def test_empty_success_body_is_not_a_scheduler_receipt(self):
        response=io.BytesIO(b'{}');response.status=200
        with patch.object(worker.urllib.request,'urlopen',return_value=response):
            with self.assertRaises(ValueError):worker.tick({'token':'s'*32,'deployment_id':'deployment-a'})

if __name__=='__main__':unittest.main()
