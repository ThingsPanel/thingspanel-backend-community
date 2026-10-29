import pathlib
import tempfile
import unittest
from encore_migrations import source_migrations, pending_migrations


class MigrationPlanTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        (self.root / '1_init.up.sql').write_text('CREATE TABLE example(id int);')
        (self.root / '2_extra.up.sql').write_text('ALTER TABLE example ADD name text;')
        self.source = source_migrations(self.root)
        self.ledger = [{k: self.source[0][k] for k in ['version', 'filename', 'checksum']}]
        self.schema = [{'version': 1, 'dirty': False}]

    def test_only_unapplied_migrations_are_returned(self):
        self.assertEqual(pending_migrations(self.source, self.ledger, self.schema), self.source[1:])

    def test_changed_applied_sql_is_rejected(self):
        self.ledger[0]['checksum'] = 'changed'
        with self.assertRaisesRegex(RuntimeError, 'changed'):
            pending_migrations(self.source, self.ledger, self.schema)

    def test_dirty_or_inconsistent_schema_is_rejected(self):
        for schema in [[], [{'version': 1, 'dirty': True}], [{'version': 2, 'dirty': False}]]:
            with self.assertRaises(RuntimeError):
                pending_migrations(self.source, self.ledger, schema)

    def test_downgrade_is_rejected(self):
        ledger = [{k: row[k] for k in ['version', 'filename', 'checksum']} for row in self.source]
        with self.assertRaisesRegex(RuntimeError, 'downgrade'):
            pending_migrations(self.source[:1], ledger, [{'version': 2, 'dirty': False}])

    def test_existing_version_gaps_are_valid(self):
        (self.root / '2_extra.up.sql').rename(self.root / '3_extra.up.sql')
        source = source_migrations(self.root)
        self.assertEqual(pending_migrations(source, self.ledger, self.schema)[0]['version'], 3)

    def test_duplicate_versions_are_rejected(self):
        (self.root / '1_duplicate.up.sql').write_text('SELECT 1;')
        with self.assertRaisesRegex(RuntimeError, 'unique'):
            source_migrations(self.root)


if __name__ == '__main__':
    unittest.main()
