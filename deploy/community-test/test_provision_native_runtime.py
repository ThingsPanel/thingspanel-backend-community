import hashlib
import io
import sys
import tarfile
import types
import unittest
from unittest.mock import patch

# These unit tests inspect archive bytes only; they never acquire a server lock.
try:
    import fcntl
except ImportError:
    sys.modules.setdefault('fcntl', types.ModuleType('fcntl'))

from provision_native_runtime import migrations


class NativeRuntimeMigrationArchiveTest(unittest.TestCase):
    directory = 'fileassets/migrations'

    def name(self, filename):
        return 'encore-api/' + self.directory + '/' + filename

    def archive(self, entries):
        content = io.BytesIO()
        with tarfile.open(fileobj=content, mode='w:gz') as archive:
            for name, raw in entries.items():
                entry = tarfile.TarInfo(name)
                entry.size = len(raw)
                archive.addfile(entry, io.BytesIO(raw))
        content.seek(0)
        return tarfile.open(fileobj=content, mode='r:gz')

    def manifest(self, entries):
        return {'sha256': {name: hashlib.sha256(raw).hexdigest()
                           for name, raw in entries.items()}}

    def test_raw_tar_bytes_are_hashed_without_newline_normalization(self):
        first = self.name('0001_initial.up.sql')
        second = self.name('0002_more.up.sql')
        entries = {second: 'SELECT \'中文\';\n'.encode('utf-8'),
                   first: b'CREATE TABLE fixture(id bigint);\r\n'}
        with self.archive(entries) as archive:
            rows = migrations(archive, self.manifest(entries), self.directory)
        self.assertEqual([1, 2], [row[0] for row in rows])
        self.assertEqual(entries[first].decode(), rows[0][3])
        self.assertEqual(hashlib.sha256(entries[first]).hexdigest(), rows[0][2])
        self.assertNotEqual(hashlib.sha256(entries[first].replace(b'\r\n', b'\n')).hexdigest(), rows[0][2])
        self.assertEqual(entries[second].decode(), rows[1][3])

    def test_tampered_archive_content_is_rejected(self):
        name = self.name('0001_initial.up.sql')
        original = {name: b'CREATE TABLE expected(id bigint);\n'}
        with self.archive({name: b'DROP TABLE expected;\n'}) as archive:
            with self.assertRaisesRegex(RuntimeError, 'checksum mismatch'):
                migrations(archive, self.manifest(original), self.directory)

    def test_same_numeric_version_with_different_names_is_rejected(self):
        entries = {self.name('0001_first.up.sql'): b'SELECT 1;\n',
                   self.name('1_second.up.sql'): b'SELECT 2;\n'}
        with self.archive(entries) as archive:
            with self.assertRaisesRegex(RuntimeError, 'duplicate migrations'):
                migrations(archive, self.manifest(entries), self.directory)

    def test_missing_resource_migrations_are_rejected(self):
        entries = {'encore-api/other/migrations/0001_initial.up.sql': b'SELECT 1;\n'}
        with self.archive(entries) as archive:
            with self.assertRaisesRegex(RuntimeError, 'Missing'):
                migrations(archive, self.manifest(entries), self.directory)

    def test_manifest_migration_missing_from_tar_is_rejected(self):
        expected = {self.name('0001_initial.up.sql'): b'SELECT 1;\n'}
        with self.archive({}) as archive:
            with self.assertRaises((KeyError, RuntimeError)):
                migrations(archive, self.manifest(expected), self.directory)

    def test_invalid_migration_filename_is_rejected_without_database_access(self):
        entries = {self.name('not_a_version.up.sql'): b'SELECT 1;\n'}
        with self.archive(entries) as archive, patch('provision_native_runtime.sql') as sql:
            with self.assertRaisesRegex(RuntimeError, 'Invalid migration name'):
                migrations(archive, self.manifest(entries), self.directory)
            sql.assert_not_called()


if __name__ == '__main__':
    unittest.main()
