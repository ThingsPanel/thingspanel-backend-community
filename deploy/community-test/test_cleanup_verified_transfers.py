import importlib.util
import pathlib
import re
import types
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('cleanup', pathlib.Path(__file__).with_name('cleanup_verified_transfers.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
m.pathlib = pathlib
m.re = re


class CleanupApprovalTests(unittest.TestCase):
    def test_rejects_active_and_outside_incoming(self):
        sha = 'a'*40
        for name, active in [('/opt/tp-community-actions/incoming/backend-'+sha+'.tar.gz', {sha}), ('/opt/other/backend-'+sha+'.tar.gz', set())]:
            plan = {'files': [{'package': name}]}
            with patch.object(m.pathlib, 'Path', pathlib.PurePosixPath):
                with self.assertRaisesRegex(RuntimeError, 'unapproved transfer target'):
                    m.approved_targets(plan, [], active)

    def test_rejects_missing_or_changed_verified_release(self):
        name = '/opt/tp-community-actions/incoming/backend-'+'b'*40+'.tar.gz'
        plan = {'files': [{'package': name, 'bytes': 1}]}
        for verified in ([], [{'package': name, 'verified_copy': True, 'bytes': 2}]):
            with patch.object(m.pathlib, 'Path', pathlib.PurePosixPath):
                with self.assertRaisesRegex(RuntimeError, 'no longer matches'):
                    m.approved_targets(plan, verified, set())

    def test_duplicate_targets_rejected_before_delete(self):
        row = {'package': '/any'}
        with self.assertRaisesRegex(RuntimeError, 'invalid approval'):
            m.approved_targets({'files': [row, row]}, [], set())


if __name__ == '__main__':
    unittest.main()
