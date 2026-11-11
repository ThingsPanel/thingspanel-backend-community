import json
import os
import pathlib
import subprocess
import tempfile
import unittest
import uuid

from upgrade_rehearsal import Rehearsal, compare_manifests, identifier


class RehearsalContractTest(unittest.TestCase):
    def test_changed_count_or_fingerprint_rejects_restore(self):
        expected = [{'name': 'devices', 'rows': 2, 'fingerprint': 'abc'}]
        compare_manifests(expected, expected)
        for actual in ([], [{'name': 'devices', 'rows': 3, 'fingerprint': 'abc'}],
                       [{'name': 'devices', 'rows': 2, 'fingerprint': 'changed'}]):
            with self.assertRaises(RuntimeError):
                compare_manifests(expected, actual)

    def test_source_cannot_be_target(self):
        with self.assertRaises(ValueError):
            Rehearsal('db', 'postgres', 'ThingsPanel', 'ThingsPanel', '/unused')
        for unsafe in ('x; DROP TABLE devices', 'public.devices', 'x"'):
            with self.assertRaises(ValueError):
                identifier(unsafe)


@unittest.skipUnless(os.environ.get('UPGRADE_REHEARSAL_FIXTURE_CONTAINER'), 'dedicated PostgreSQL fixture required')
class SnapshotRestoreTest(unittest.TestCase):
    def test_exported_snapshot_survives_concurrent_source_write(self):
        container = os.environ['UPGRADE_REHEARSAL_FIXTURE_CONTAINER']
        self.assertEqual(container, 'tp-rollout-alarm-fixture')
        user = 'automation_receipt_test'
        suffix = uuid.uuid4().hex[:12]
        source = 'tp_upgrade_fixture_' + suffix
        target = 'tp_upgrade_rehearsal_' + suffix
        subprocess.run(['docker', 'exec', container, 'createdb', '-U', user, source], check=True, capture_output=True)
        def write(sql):
            subprocess.run(['docker', 'exec', '-i', container, 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1',
                            '-U', user, '-d', source], input=sql.encode(), check=True, capture_output=True)
        write("CREATE TABLE devices(id text PRIMARY KEY, voucher text); INSERT INTO devices VALUES ('before','fixture-private'); CREATE TABLE history(id int); INSERT INTO history VALUES(1),(2);")
        with tempfile.TemporaryDirectory(prefix='tp-restore-fixture-') as temp:
            job = Rehearsal(container, user, source, target, pathlib.Path(temp) / 'snapshot')
            original = job.command
            def concurrent_write(args, *a, **kw):
                if 'pg_dump' in args:
                    write("INSERT INTO devices VALUES ('after','fixture-private-2')")
                return original(args, *a, **kw)
            job.command = concurrent_write
            report = job.run()
            self.assertEqual('restored-and-verified', report['status'])
            self.assertEqual('2', job.query(source, 'SELECT count(*) FROM devices'))
            self.assertEqual('1', job.query(target, 'SELECT count(*) FROM devices'))
            self.assertNotIn('fixture-private', json.dumps(report))
            self.assertEqual(2, len(report['tables']))
        # Dedicated fixture databases are retained for inspection; no DROP operation.


if __name__ == '__main__':
    unittest.main()
