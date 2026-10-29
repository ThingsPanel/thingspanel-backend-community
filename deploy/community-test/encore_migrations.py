"""Validate an append-only migration history before touching resource databases."""
import hashlib
import pathlib
import re


def source_migrations(directory):
    migrations = []
    for path in pathlib.Path(directory).glob('*.up.sql'):
        if not re.fullmatch(r'[0-9]+_[A-Za-z0-9_]+\.up\.sql', path.name):
            raise RuntimeError('Invalid migration filename')
        migrations.append({'version': int(path.name.split('_', 1)[0]),
                           'filename': path.name,
                           'checksum': hashlib.sha256(path.read_bytes()).hexdigest(),
                           'sql': path.read_text(encoding='utf-8')})
    migrations.sort(key=lambda item: item['version'])
    versions = [m['version'] for m in migrations]
    if not versions or versions[0] < 1 or len(set(versions)) != len(versions):
        raise RuntimeError('Migration versions must be positive and unique')
    return migrations


def pending_migrations(source, ledger, schema):
    if len(schema) != 1 or schema[0]['dirty']:
        raise RuntimeError('Resource schema is missing or dirty')
    ledger = sorted(ledger, key=lambda row: row['version'])
    if not ledger or schema[0]['version'] != ledger[-1]['version']:
        raise RuntimeError('Schema version differs from deployment ledger')
    if len(ledger) > len(source):
        raise RuntimeError('Source would downgrade the deployed schema')
    for actual, expected in zip(ledger, source):
        if any(actual[key] != expected[key] for key in ['version', 'filename', 'checksum']):
            raise RuntimeError('Previously applied migration has changed')
    return source[len(ledger):]
