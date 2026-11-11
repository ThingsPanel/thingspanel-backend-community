import copy
import unittest

from upgrade_acceptance import (SOURCE_TABLES, NATIVE_TABLES, COVERAGE, REQUIRED_CHECKS,
                                acceptance_issues, fingerprint_query)


class AcceptanceTest(unittest.TestCase):
    def setUp(self):
        self.source = [dict(name=n, rows=2, fingerprint='a' * 32) for n in sorted(SOURCE_TABLES)]
        self.native = [dict(name=n, rows=2, fingerprint='b' * 32) for n in sorted(NATIVE_TABLES)]
        self.receipt = dict(status='complete-reconciled', source_database='ThingsPanel',
            native_database='isolated-fixture', source_archive_sha256='c' * 64,
            quarantine_archive_sha256='d' * 64, source_manifest=copy.deepcopy(self.source),
            native_manifest=copy.deepcopy(self.native),
            coverage={k: dict(source=2, converted=2, quarantined=0, pending=0) for k in COVERAGE},
            checks={k: True for k in REQUIRED_CHECKS})

    def issues(self):
        return acceptance_issues(self.receipt, self.source, self.native, 'isolated-fixture')

    def test_complete_receipt_matches_current_content(self):
        self.assertEqual([], self.issues())

    def test_backup_and_partial_conversion_are_never_acceptance(self):
        for receipt in (None, {}, {'status': 'restored-and-verified'}, {'migration_complete': False}):
            self.assertTrue(acceptance_issues(receipt, self.source, self.native, 'isolated-fixture'))

    def test_same_count_changed_content_is_rejected(self):
        self.source[0]['fingerprint'] = 'e' * 32
        self.assertTrue(self.issues())
        self.source = copy.deepcopy(self.receipt['source_manifest'])
        self.native[0]['fingerprint'] = 'e' * 32
        self.assertTrue(self.issues())

    def test_pending_missing_and_false_semantic_checks_are_rejected(self):
        self.receipt['coverage']['devices']['pending'] = 1
        self.assertTrue(self.issues())
        self.receipt['coverage']['devices']['pending'] = 0
        self.receipt['checks']['credential_continuity'] = False
        self.assertTrue(self.issues())
        self.receipt['checks']['credential_continuity'] = True
        del self.receipt['coverage']['gateway_relations']
        self.assertTrue(self.issues())

    def test_false_accounting_or_incomplete_manifest_is_rejected(self):
        self.receipt['coverage']['devices'] = dict(source=0, converted=0, quarantined=0, pending=0)
        self.assertTrue(self.issues())
        self.receipt['coverage']['devices'] = dict(source=2, converted=2, quarantined=0, pending=0)
        self.receipt['source_manifest'].pop()
        self.assertTrue(self.issues())

    def test_database_mismatch_and_untrusted_table_names_are_rejected(self):
        self.assertTrue(acceptance_issues(self.receipt, self.source, self.native, 'wrong-target'))
        with self.assertRaises(ValueError):
            fingerprint_query(['devices; DELETE FROM users'])


if __name__ == '__main__':
    unittest.main()
