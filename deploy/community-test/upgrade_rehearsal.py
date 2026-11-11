"""Make and verify an offline PostgreSQL upgrade rehearsal without editing source.

The backup and restore share an exported REPEATABLE READ snapshot. Source row
counts and critical metadata fingerprints are compared with the restored copy.
Archives and error details stay in a private directory on the database host.
This command never switches services, rewrites source rows, or drops databases.
"""
import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys


CRITICAL = {'devices', 'device_configs', 'device_templates', 'groups', 'r_group_device',
            'products', 'users', 'roles', 'casbin_rule', 'app_push_devices',
            'device_model_telemetry', 'device_model_attributes', 'device_model_events',
            'device_model_commands', 'device_model_custom_commands', 'device_topic_mappings',
            'data_scripts', 'alarm_config', 'scene_info', 'scene_automations',
            'notification_groups', 'notification_tenant_default_policies',
            'thing_models', 'thing_model_items', 'thing_model_snapshots',
            'device_access_templates', 'device_access_bindings', 'device_groups',
            'device_group_members'}


def identifier(value):
    if not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]{0,62}', value):
        raise ValueError('invalid database or table identifier')
    return '"' + value + '"'


def table_query(tables):
    queries = []
    for table in sorted(tables):
        quoted = identifier(table)
        fingerprint = ("md5(coalesce(string_agg(md5(to_jsonb(t)::text),'' ORDER BY md5(to_jsonb(t)::text)),''))"
                       if table in CRITICAL else 'NULL::text')
        queries.append("SELECT '" + table + "' AS name,count(*) AS rows," + fingerprint +
                       ' AS fingerprint FROM public.' + quoted + ' t')
    if not queries:
        return "SELECT '[]'"
    return "SELECT replace(coalesce(json_agg(t),'[]'::json)::text,chr(10),'') FROM (" + ' UNION ALL '.join(queries) + ') t'


def compare_manifests(source, restored):
    expected = {row['name']: row for row in source}
    actual = {row['name']: row for row in restored}
    if len(expected) != len(source) or len(actual) != len(restored):
        raise RuntimeError('duplicate table in restore manifest')
    if expected.keys() != actual.keys():
        raise RuntimeError('restored table set differs from source snapshot')
    mismatch = [name for name in sorted(expected) if expected[name] != actual[name]]
    if mismatch:
        raise RuntimeError('restored data differs for tables: ' + ','.join(mismatch))


class Rehearsal:
    def __init__(self, container, user, source, target, directory):
        if not re.fullmatch(r'[A-Za-z0-9_.-]+', container):
            raise ValueError('invalid container')
        identifier(user)
        identifier(source)
        identifier(target)
        if not target.startswith('tp_upgrade_rehearsal_') or source == target:
            raise ValueError('target must be a new dedicated rehearsal database')
        self.container, self.user = container, user
        self.source, self.target, self.directory = source, target, pathlib.Path(directory)
        self.base = ['docker', 'exec', '-i', container]

    def command(self, args, input=None, output=None, timeout=900):
        with (self.directory / 'operations.private.log').open('ab') as log:
            p = subprocess.run(args, input=input, stdout=output or subprocess.PIPE,
                               stderr=log, timeout=timeout)
        if p.returncode:
            raise RuntimeError('rehearsal operation failed; details remain in private host log')
        return p.stdout

    def query(self, database, query):
        script = 'BEGIN READ ONLY; SET LOCAL timezone=\'UTC\';\n' + query + ';\nROLLBACK;\n'
        return self.command(self.base + ['psql', '-X', '-q', '-At', '-v', 'ON_ERROR_STOP=1',
                            '-U', self.user, '-d', database], script.encode()).decode().strip()

    def run(self):
        os.umask(0o077)
        self.directory.mkdir(parents=True, mode=0o700, exist_ok=False)
        if self.query('postgres', "SELECT count(*) FROM pg_database WHERE datname='" + self.target + "'") != '0':
            raise RuntimeError('rehearsal database already exists; refusing to overwrite')
        size = int(self.query(self.source, 'SELECT pg_database_size(current_database())'))
        if shutil.disk_usage(str(self.directory)).free < size * 2 + 1024 ** 3:
            raise RuntimeError('insufficient disk for a retained archive, restored database and safety margin')
        with (self.directory / 'snapshot.private.log').open('wb') as log:
            session = subprocess.Popen(self.base + ['psql', '-X', '-q', '-At', '-v', 'ON_ERROR_STOP=1',
                           '-U', self.user, '-d', self.source], stdin=subprocess.PIPE,
                           stdout=subprocess.PIPE, stderr=log, universal_newlines=True, bufsize=1)
            def request(sql):
                session.stdin.write(sql + ';\n')
                session.stdin.flush()
                line = session.stdout.readline().strip()
                if not line:
                    raise RuntimeError('snapshot session ended unexpectedly')
                return line
            try:
                snapshot = request("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL timezone='UTC'; SELECT pg_export_snapshot()")
                if not re.fullmatch(r'[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+', snapshot):
                    raise RuntimeError('invalid exported snapshot')
                tables = json.loads(request("SELECT coalesce(json_agg(tablename ORDER BY tablename),'[]'::json) FROM pg_tables WHERE schemaname='public'"))
                extensions = json.loads(request("SELECT json_agg(extname) FROM pg_extension"))
                source_manifest = json.loads(request(table_query(tables)))
                archive = self.directory / 'source.private.dump'
                with archive.open('xb') as stream:
                    self.command(self.base + ['pg_dump', '-U', self.user, '-d', self.source,
                        '--format=custom', '--no-owner', '--no-privileges', '--snapshot=' + snapshot], output=stream)
            finally:
                if session.poll() is None:
                    session.stdin.write('ROLLBACK;\n\\q\n')
                    session.stdin.flush()
                    try:
                        session.communicate(timeout=20)
                    except subprocess.TimeoutExpired:
                        session.kill()
                        session.communicate()
        digest = hashlib.sha256()
        with archive.open('rb') as stream:
            for block in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(block)
        # Validate TOC before creating any target database. Dump bytes never go to Actions logs.
        with archive.open('rb') as stream, (self.directory / 'toc.private.txt').open('wb') as toc:
            with (self.directory / 'operations.private.log').open('ab') as log:
                p = subprocess.run(self.base + ['pg_restore', '--list'], stdin=stream, stdout=toc, stderr=log, timeout=60)
                if p.returncode:
                    raise RuntimeError('backup archive TOC verification failed')
        self.command(self.base + ['createdb', '-U', self.user, '--template=template0', self.target])
        timescale = 'timescaledb' in extensions
        if timescale:
            self.command(self.base + ['psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1', '-U', self.user,
                '-d', self.target, '-c', 'CREATE EXTENSION IF NOT EXISTS timescaledb; SELECT timescaledb_pre_restore();'])
        with archive.open('rb') as stream, (self.directory / 'restore.private.log').open('wb') as log:
            p = subprocess.run(self.base + ['pg_restore', '-U', self.user, '-d', self.target,
                '--no-owner', '--no-privileges', '--exit-on-error'], stdin=stream, stdout=log, stderr=log, timeout=1800)
            if p.returncode:
                raise RuntimeError('backup restore failed; source unchanged and partial rehearsal retained')
        restored_tables = json.loads(self.query(self.target, "SELECT coalesce(json_agg(tablename ORDER BY tablename),'[]'::json) FROM pg_tables WHERE schemaname='public'"))
        restored_manifest = json.loads(self.query(self.target, table_query(restored_tables)))
        compare_manifests(source_manifest, restored_manifest)
        report = {'status': 'restored-and-verified', 'source_database': self.source,
                  'rehearsal_database': self.target, 'source_snapshot': snapshot,
                  'archive_sha256': digest.hexdigest(), 'archive_bytes': archive.stat().st_size,
                  'tables': source_manifest, 'timescale_background_jobs_paused': timescale,
                  'application_started': False, 'source_mutated': False}
        (self.directory / 'restore-verification.json').write_text(json.dumps(report, indent=2), encoding='utf-8')
        return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--container', default='timescaledb')
    parser.add_argument('--user', default='postgres')
    parser.add_argument('--source', required=True)
    parser.add_argument('--target', required=True)
    parser.add_argument('--directory', required=True)
    args = parser.parse_args()
    report = Rehearsal(args.container, args.user, args.source, args.target, args.directory).run()
    print(json.dumps({key: report[key] for key in ('status', 'source_database', 'rehearsal_database',
        'archive_sha256', 'archive_bytes', 'timescale_background_jobs_paused', 'application_started', 'source_mutated')}))


if __name__ == '__main__':
    main()
