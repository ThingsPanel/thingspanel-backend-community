"""Stage the retired db.psql configuration as relational.postgres.platform.

Credentials and unrelated service settings are retained, not regenerated. The
original file and candidate stay private; no running configuration is replaced.
"""
import copy
import hashlib
import json
import os
import pathlib


def convert_platform_config(source):
    target = copy.deepcopy(source)
    existing = target.get('relational')
    if existing:
        raise ValueError('relational configuration already exists; do not infer an overwrite')
    legacy = target.get('db', {}).get('psql')
    if not isinstance(legacy, dict) or not legacy.get('dbname') or not legacy.get('username'):
        raise ValueError('complete legacy PostgreSQL configuration required')
    allowed = {'host','port','dbname','username','password','time_zone','idle_conns',
               'open_conns','log_level','slow_threshold','slow_threshold_ms','conn_max_lifetime'}
    if set(legacy) - allowed:
        raise ValueError('unmapped legacy PostgreSQL options require review')
    if 'slow_threshold' in legacy and 'slow_threshold_ms' in legacy and legacy['slow_threshold'] != legacy['slow_threshold_ms']:
        raise ValueError('conflicting slow query threshold options')
    relational = {k:v for k,v in legacy.items() if k not in ('dbname','slow_threshold')}
    if 'slow_threshold' in legacy:
        # The retired initialize/pg_init.go multiplies this by time.Millisecond.
        relational['slow_threshold_ms'] = legacy['slow_threshold']
    if not relational.get('log_level'):
        relational['log_level'] = 1  # Preserve the retired platform logger default.
    relational['database'] = legacy['dbname']
    target['relational'] = {'driver':'postgres','postgres':{'platform':relational}}
    del target['db']['psql']
    return target


def stage_platform_config(source_path, output_directory):
    import yaml
    source_path, output_directory = pathlib.Path(source_path), pathlib.Path(output_directory)
    os.umask(0o077)
    raw = source_path.read_bytes()
    original = yaml.safe_load(raw.decode('utf-8'))
    candidate = convert_platform_config(original)
    candidate_bytes = yaml.safe_dump(candidate,allow_unicode=True,sort_keys=False).encode('utf-8')
    # Verify serialization before writing; no secrets in reports or exception text.
    if yaml.safe_load(candidate_bytes.decode('utf-8')) != candidate:
        raise RuntimeError('candidate configuration serialization changed values')
    files = {'platform.before.private.yml':raw,'platform.next.private.yml':candidate_bytes}
    for name, content in files.items():
        path = output_directory / name
        if path.exists():
            if path.read_bytes() != content:
                raise RuntimeError('staged configuration differs; original and candidate retained')
        else:
            with path.open('xb') as stream:
                stream.write(content)
                stream.flush()
                os.fsync(stream.fileno())
        if os.name != 'nt' and path.stat().st_mode & 0o077:
            raise RuntimeError('staged configuration must be private')
    report = {'state':'staged-only','source_sha256':hashlib.sha256(raw).hexdigest(),
              'candidate_sha256':hashlib.sha256(candidate_bytes).hexdigest(),
              'source_mutated':False,'service_restarted':False,
              'remaining_checks':['process-environment-overrides','resource-connectivity','full-data-reconciliation']}
    path = output_directory / 'platform-config-stage.json'
    if path.exists():
        if json.loads(path.read_text()) != report:
            raise RuntimeError('staging receipt differs')
    else:
        with path.open('x') as stream:json.dump(report,stream,indent=2)
    return report
