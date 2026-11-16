"""Read-only runtime identity and queue diagnostics; never print credentials."""
import fcntl
import hashlib
import json
import os
import re
import datetime
import pathlib
import shutil
import subprocess
import tarfile
import urllib.parse
import urllib.request
import urllib.error


def broker_preflight(infos):
    broker = next((i for i in infos if i['Name'].endswith('-broker-mosquitto-1')), None)
    owner = next((i for i in infos if i['Name'].endswith('-broker-owner-1')), None)
    result = {'pair_present': bool(broker and owner)}
    if not broker or not owner:
        return result
    env = dict(x.split('=', 1) for x in (broker['Config'].get('Env') or []) if '=' in x)
    result['owner_network_mode'] = owner.get('HostConfig', {}).get('NetworkMode')
    owner_env = dict(x.split('=', 1) for x in (owner['Config'].get('Env') or []) if '=' in x)
    result['owner_listen'] = owner_env.get('TP_BROKER_OWNER_LISTEN')
    result['broker_network_mode'] = broker.get('HostConfig', {}).get('NetworkMode')
    try:
        endpoint = urllib.parse.urlsplit(env.get('TP_BROKER_OWNER_URL', ''))
        result['broker_owner_endpoint'] = {'scheme': endpoint.scheme, 'hostname': endpoint.hostname, 'port': endpoint.port}
    except ValueError:
        result['broker_owner_endpoint'] = {'invalid': True}
    token = env.get('TP_BROKER_TOKEN_FILE', '')
    mounts = [m for m in broker.get('Mounts', []) if token and (token == m['Destination'] or token.startswith(m['Destination'].rstrip('/') + '/'))]
    result['token_mount_mapped'] = bool(mounts)
    result['token_host_readable_by_deployer'] = False
    if mounts:
        mount = max(mounts, key=lambda m: len(m['Destination']))
        path = pathlib.Path(mount['Source']) / token[len(mount['Destination']):].lstrip('/')
        result['token_host_readable_by_deployer'] = path.is_file() and os.access(str(path), os.R_OK)
    bindings = [p for values in owner['NetworkSettings']['Ports'].values() for p in (values or [])]
    result['owner_host_bindings'] = [{'host_ip': p['HostIp'], 'host_port': p['HostPort']} for p in bindings]
    result['owner_ipv4_binding_count'] = sum(p['HostIp'] in ('0.0.0.0', '127.0.0.1') for p in bindings)
    result['identity_fields_present'] = all(bool(env.get(k)) for k in ('TP_BROKER_CELL', 'TP_BROKER_INSTANCE'))
    return result


def run(args, data=None):
    result = subprocess.run(args, input=data, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=30)
    if result.returncode:
        raise RuntimeError('Read-only diagnostic command failed')
    return result.stdout.decode().strip()


def transfer_diagnostics(incoming, command=run):
    """Only artifact metadata and an allowlisted process name; never arguments."""
    packages = []
    if incoming.is_dir() and not incoming.is_symlink():
        for path in incoming.iterdir():
            if not re.fullmatch(r'(?:encore|backend|broker|frontend)-[0-9a-f]{40}\.(?:tar\.gz|json)', path.name):
                continue
            if path.is_symlink() or not path.is_file():
                continue
            try:
                info = path.stat()
            except OSError:
                continue
            packages.append({'name': path.name, 'bytes': info.st_size,
                             'modified_at': datetime.datetime.fromtimestamp(info.st_mtime, datetime.timezone.utc).isoformat()})
    processes = []
    allowed = {'scp', 'sftp-server', 'sshd', 'ssh', 'python3', 'python', 'docker', 'docker-compose', 'psql', 'go', 'gzip', 'tar'}
    try:
        lines = command(['ps', '-eo', 'comm=,etimes=']).splitlines()
        for line in lines:
            parts = line.split()
            if len(parts) == 2 and parts[0] in allowed and parts[1].isdigit():
                processes.append({'comm': parts[0], 'elapsed_seconds': int(parts[1])})
        process_error = False
    except (RuntimeError, OSError, subprocess.TimeoutExpired):
        process_error = True
    return {'incoming_packages': sorted(packages, key=lambda x: x['name']),
            'processes': processes, 'process_snapshot_unavailable': process_error}


def approved_cache_sizes(command=run):
    result = []
    for name in ('/opt/tp-community-actions/encore-build-cache', '/root/.cache/encore-build'):
        path = pathlib.Path(name)
        item = {'path': name, 'exists': path.exists()}
        if path.is_symlink() or path.resolve() != path.absolute():
            item['unsafe_path'] = True
        elif path.is_dir():
            try:
                value = command(['du', '-sk', '--', str(path)]).split()[0]
                item['allocated_bytes'] = int(value) * 1024
            except (ValueError, IndexError, RuntimeError, OSError, subprocess.TimeoutExpired):
                item['size_unavailable'] = True
        result.append(item)
    return result


def historical_package_copies(root, active_commits):
    """Read-only approval inventory; does not remove artifacts or print manifests."""
    rows = []
    incoming = root / 'incoming'
    for package in sorted(incoming.glob('*.tar.gz')):
        match = re.fullmatch(r'(encore|backend)-([0-9a-f]{40})\.tar\.gz', package.name)
        if not match or match.group(2) in active_commits or package.is_symlink() or package.resolve() != package.absolute():
            continue
        kind, commit = match.groups()
        release = root / (kind + '-releases') / commit
        marker = release / ('deployment.json' if kind == 'encore' else 'result.json')
        if not marker.is_file() or marker.is_symlink() or release.is_symlink() or release.resolve() != release.absolute():
            continue
        row = {'package': str(package), 'bytes': package.stat().st_size,
               'release': str(release), 'success_marker': str(marker), 'verified_copy': False}
        try:
            completed = json.loads(marker.read_text())
            if (completed.get('commit') != commit
                    or (kind == 'backend' and completed.get('status') != 'deployed')
                    or (kind == 'encore' and completed.get('image') != 'tp-community-encore:source-' + commit)):
                row['reason'] = 'success_marker_identity_unconfirmed'
                rows.append(row)
                continue
            manifest = json.loads((release / 'manifest.json').read_text())
            if (manifest.get('commit') != commit or not isinstance(manifest.get('sha256'), dict)
                    or not manifest['sha256']
                    or not ({'image.json', 'image.tar.gz'} if kind == 'encore' else {'ThingsPanel-Go'}) <= set(manifest['sha256'])):
                raise ValueError('manifest identity')
            with tarfile.open(str(package), 'r:gz') as archive:
                archived = json.load(archive.extractfile('manifest.json'))
                members = archive.getmembers()
                if (any(m.issym() or m.islnk() or not (m.isfile() or m.isdir()) for m in members)
                        or len({m.name for m in members}) != len(members)
                        or {m.name for m in members if m.isfile()} != set(manifest['sha256']) | {'manifest.json'}):
                    raise ValueError('archive member mismatch')
            if archived != manifest:
                raise ValueError('archive manifest mismatch')
            count, total = 0, 0
            for name, expected in manifest['sha256'].items():
                path = release / name
                if (not name or pathlib.PurePosixPath(name).is_absolute() or '..' in pathlib.PurePosixPath(name).parts
                        or path.is_symlink() or path.resolve() != path.absolute() or not path.is_file()
                        or not re.fullmatch('[0-9a-f]{64}', expected)):
                    raise ValueError('unsafe manifest member')
                digest = hashlib.sha256()
                with path.open('rb') as stream:
                    for block in iter(lambda: stream.read(1024 * 1024), b''):
                        digest.update(block)
                if digest.hexdigest() != expected:
                    raise ValueError('release copy mismatch')
                count += 1
                total += path.stat().st_size
            row.update({'verified_copy': True, 'verified_files': count, 'release_content_bytes': total})
        except (ValueError, OSError, KeyError, TypeError, tarfile.TarError):
            row['reason'] = 'copy_validation_failed'
        rows.append(row)
    return rows


def safe_startup_errors(tail):
    errors = []
    for match in re.finditer(r'migration checksum drift for ([a-zA-Z0-9_./-]+\.sql): recorded ([0-9a-f]{64}), source ([0-9a-f]{64})', tail):
        errors.append({'type':'migration_checksum_drift','sql_file':match.group(1),'recorded':match.group(2),'source':match.group(3)})
    for match in re.finditer(r'database version (\d+) is newer than program version (\d+)', tail):
        errors.append({'type':'database_version_newer','database_version':int(match.group(1)),'program_version':int(match.group(2))})
    for text in ('password authentication failed', 'no pg_hba.conf entry', 'connection refused', 'address already in use',
                 'too many connections', 'remaining connection slots', 'out of memory', 'no space left on device',
                 'permission denied', 'migration ledger must contain exactly one', 'database configuration is incomplete'):
        if text in tail:
            errors.append({'type':text.replace(' ','_')})
    if re.search(r'(database|relation|column|role) [^\n]{0,120}does not exist', tail):
        errors.append({'type':'database_object_missing'})
    for match in re.finditer(r'(?:execute|read|record) legacy migration (\d+)', tail):
        errors.append({'type':'legacy_migration_error','version':int(match.group(1))})
    for match in re.finditer(r'device identity reservations do not match (\d+) current devices', tail):
        errors.append({'type':'device_identity_reservation_mismatch','count':int(match.group(1))})
    for fixed in ('device identity writer fence is missing or has changed', 'device identity writer fence function has changed',
                  'dictionary schema is missing or incompatible', 'database migration outcome is unknown',
                  'legacy migration final version was not recorded exactly once', 'schema verification failed',
                  'create component migration ledger', 'read component migration ledger', '检查数据库版本失败'):
        if fixed in tail:
            errors.append({'type':fixed.replace(' ','_')})
    for match in re.finditer(r'(?:required )?schema (?:column|constraint) (public\.[a-z_]+\.[a-z_]+) (is missing|has type|nullability|has character length|must default to 0|does not match the native binding contract)', tail):
        errors.append({'type':'schema_contract_mismatch','field':match.group(1),'failure':match.group(2)})
    for name in ('permanent device identity', 'service access receipt', 'telemetry history intent', 'market installation',
                 'existing platform dictionary', 'native binding', 'automation command guard', 'app push', 'auth'):
        if 'initialize '+name+' schema' in tail:
            errors.append({'type':'initialization_failed','component':name})
    return errors


def database_ledger_diagnostics(command=run):
    def query(database, sql):
        return command(['docker','exec','-i','timescaledb','psql','-X','-q','-At','-v','ON_ERROR_STOP=1','-U','postgres','-d',database],
                       ("BEGIN READ ONLY; SET LOCAL statement_timeout='5s'; "+sql+'; COMMIT;').encode())
    result=[]
    try:
        databases=query('postgres', "SELECT datname FROM pg_database WHERE datallowconn AND NOT datistemplate").splitlines()
        for database in databases:
            if not re.fullmatch('[a-zA-Z0-9_]+',database):
                continue
            present=query(database,"SELECT json_build_object('legacy',to_regclass('public.sys_version') IS NOT NULL,'components',to_regclass('public.native_component_migration_ledger') IS NOT NULL)")
            flags=json.loads(present)
            if not flags['legacy'] and not flags['components']:
                continue
            row={'database':database}
            if flags['legacy']:
                row['legacy_versions']=json.loads(query(database,"SELECT coalesce(json_agg(version_number),'[]'::json) FROM public.sys_version"))
            if flags['components']:
                row['component_digests']=json.loads(query(database,"SELECT coalesce(json_agg(t),'[]'::json) FROM (SELECT module_name,file_name,checksum FROM public.native_component_migration_ledger ORDER BY module_name,file_name) t"))
            if database == 'ThingsPanel':
                row['identity_conflicts']=json.loads(query(database,"""SELECT coalesce(json_agg(t),'[]'::json) FROM (
                    SELECT d.id,d.tenant_id AS device_tenant_id,r.tenant_id AS reservation_tenant_id,
                    r.state AS reservation_state,r.deleted_at AS reservation_deleted_at,d.is_enabled,d.is_online,
                    o.native_tenant_id,o.tenant_id AS owner_tenant_id,o.active AS owner_active,o.deleting AS owner_deleting,
                    o.lifecycle_generation
                    FROM devices d LEFT JOIN device_id_reservations r ON r.id=d.id
                    LEFT JOIN native_runtime_registrations o ON o.device_id=d.id
                    WHERE r.id IS NULL OR r.tenant_id<>d.tenant_id OR r.state<>'active'
                    ORDER BY d.id LIMIT 20) t"""))
            result.append(row)
    except (ValueError,RuntimeError,OSError):
        result.append({'read_unavailable':True})
    return result


def backend_process_diagnostics(command=run):
    result = {'processes': [], 'local_http': [], 'listeners': []}
    try:
        processes = json.loads(command(['/root/.local/share/pnpm/pm2', 'jlist']))
        for process in processes:
            if process.get('name') != 'thingspanel-backend-community':
                continue
            env = process.get('pm2_env', {})
            row = {key: env.get(key) for key in ('status', 'restart_time', 'unstable_restarts', 'exit_code', 'pm_exec_path', 'pm_cwd', 'exec_interpreter', 'pm_uptime', 'created_at')}
            row['pid'] = process.get('pid')
            args = env.get('args', [])
            row['args_count'] = len(args) if isinstance(args, list) else None
            row['config_argument'] = None
            if isinstance(args, list):
                for i, arg in enumerate(args[:-1]):
                    if arg in ('-config', '--config') and re.fullmatch(r'[/a-zA-Z0-9_.-]+', str(args[i+1])):
                        row['config_argument'] = args[i+1]
            categories = set()
            safe_errors = []
            log_metadata = []
            for key in ('pm_err_log_path', 'pm_out_log_path'):
                path = pathlib.Path(env.get(key, '/nonexistent'))
                if not path.is_file() or path.is_symlink():
                    continue
                with path.open('rb') as stream:
                    stream.seek(max(0, path.stat().st_size - 65536))
                    tail = stream.read(65536).decode(errors='replace').lower()
                stamps = re.findall(r'\d{4}[-/]\d{2}[-/]\d{2}[ t]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:z|[+-]\d{2}:?\d{2})?', tail)
                errors = safe_startup_errors(tail)
                log_metadata.append({'stream':key,'mtime_epoch':path.stat().st_mtime,'tail_last_timestamp':stamps[-1] if stamps else None,
                                     'mtime_after_pm_start':path.stat().st_mtime >= (env.get('pm_uptime') or 0)/1000.0,
                                     'timestamp_scope':'tail_only_not_proof_of_current_startup','matched_error_count':len(errors)})
                safe_errors.extend(errors)
                for category, pattern in [('panic', 'panic:'), ('permission_denied', 'permission denied'),
                        ('address_in_use', 'address already in use'), ('connection_refused', 'connection refused'),
                        ('config_error', 'config'), ('database_error', 'database'), ('migration_error', 'migration'),
                        ('syntax_error', 'syntax error'), ('missing_file', 'no such file'), ('fatal', 'fatal')]:
                    if pattern in tail:
                        categories.add(category)
                for code in re.findall(r'sqlstate[ :]+([0-9a-z]{5})', tail):
                    categories.add('SQLSTATE_' + code.upper())
            row['recent_log_categories'] = sorted(categories)
            row['startup_errors'] = safe_errors
            row['log_metadata'] = log_metadata
            result['processes'].append(row)
    except (ValueError, OSError, RuntimeError):
        result['pm2_unavailable'] = True
    for path in ('/health', '/api/v1/sys_version'):
        row = {'path': path}
        try:
            with urllib.request.urlopen('http://127.0.0.1:9999' + path, timeout=5) as response:
                row['status'] = response.status
                if path.endswith('sys_version'):
                    version = json.load(response).get('data', {}).get('version')
                    row['version'] = version if isinstance(version, str) and re.fullmatch('dev-[0-9a-f]{40}', version) else 'unrecognized'
        except urllib.error.HTTPError as error:
            row['status'] = error.code
        except (ValueError, OSError) as error:
            row['unavailable'] = True
            reason = str(getattr(error, 'reason', error)).lower()
            row['failure_kind'] = 'connection_refused' if 'refused' in reason else ('timeout' if 'timed out' in reason else type(error).__name__)
        result['local_http'].append(row)
    try:
        for line in command(['ss', '-H', '-lnt']).splitlines():
            columns = line.split()
            if len(columns) >= 4 and columns[3].rsplit(':', 1)[-1] in ('80', '9999', '14000'):
                result['listeners'].append(columns[3])
    except (RuntimeError, OSError):
        result['listeners_unavailable'] = True
    try:
        nginx = command(['nginx', '-T'])
        locations = sorted(set(re.findall(r'location\s+(?:[=~^]+\s+)?(/(?:api|automation-core|v1)[a-zA-Z0-9_/.-]*)', nginx)))
        upstreams = []
        for value in re.findall(r'proxy_pass\s+([^;\s]+)', nginx):
            parsed = urllib.parse.urlsplit(value)
            if parsed.scheme in ('http', 'https') and parsed.hostname in ('127.0.0.1', 'localhost') and not parsed.username:
                upstreams.append({'host': parsed.hostname, 'port': parsed.port})
        result['nginx_api_locations'] = locations
        result['nginx_local_upstreams'] = upstreams
    except (RuntimeError, OSError, ValueError):
        result['nginx_unavailable'] = True
    return result


def encore_base_diagnostics(config, infos):
    """Only credential-free address metadata and the health HTTP status."""
    result = {'available': False}
    try:
        value = config.get('metadata', {}).get('base_url', '')
        parsed = urllib.parse.urlsplit(value)
        if (parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username
                or parsed.password or parsed.query or parsed.fragment
                or not re.fullmatch(r'[A-Za-z0-9_.:-]+', parsed.hostname)
                or not re.fullmatch(r'/[A-Za-z0-9_./-]*|', parsed.path)):
            return result
        result.update({'available': True, 'scheme': parsed.scheme, 'host': parsed.hostname,
                       'port': parsed.port, 'path': parsed.path})
        owner = next((item for item in infos if
                      (item['Config'].get('Labels') or {}).get('com.docker.compose.service') == 'encore-api'), None)
        if owner is None:
            result['probe'] = 'container_unavailable'
            return result
        completed = subprocess.run(['docker', 'exec', owner['Id'], 'wget', '-S', '-O', '/dev/null', '-T', '5', value.rstrip('/') + '/v1/health'],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)
        matches = re.findall(rb'HTTP/[0-9.]+ ([0-9]{3})', completed.stderr)
        result['http_status'] = int(matches[-1]) if matches else None
        result['probe'] = 'completed' if matches else 'unavailable'
    except (ValueError, OSError, subprocess.TimeoutExpired):
        result['probe'] = 'unavailable'
    return result


def main():
    root = pathlib.Path('/opt/tp-community-main-20261028')
    host = transfer_diagnostics(pathlib.Path('/opt/tp-community-actions/incoming'))
    host['approved_build_caches'] = approved_cache_sizes()
    with pathlib.Path('/opt/tp-community-actions/deployment.lock').open('r') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except BlockingIOError:
            print(json.dumps({'read_only': True, 'deployment_lock_busy': True,
                              'database_checks_skipped': True, 'host': host,
                              'disk_free_bytes': shutil.disk_usage(str(root)).free}))
            return
        host['backend_process'] = backend_process_diagnostics()
        host['database_ledgers'] = database_ledger_diagnostics()
        config = json.loads((root / 'runtime.encore.private.json').read_text())
        databases = [name for server in config['sql_servers']
                     for name, spec in server['databases'].items()
                     if spec.get('name') == 'device_metadata']
        if len(databases) != 1:
            raise RuntimeError('Native device database identity is ambiguous')

        def sql(query):
            query = "BEGIN READ ONLY; SET LOCAL statement_timeout='15s'; " + query + '; COMMIT;'
            return json.loads(run(['docker', 'exec', '-i', 'timescaledb', 'psql',
                                   '-X', '-q', '-At', '-v', 'ON_ERROR_STOP=1',
                                   '-U', 'postgres', '-d', databases[0]], query.encode()))

        host['qa_deleted_native_identity'] = sql("""SELECT coalesce(json_agg(t),'[]'::json) FROM (
            SELECT id,tenant_id,deleted_at FROM devices
            WHERE id='758f9efd-67d6-4e16-8a36-441d8972135c'::uuid) t""")
        queue = sql("""SELECT COALESCE(json_agg(t),'[]'::json) FROM (
            SELECT o.kind,o.state,count(*) AS count,min(o.created_at) AS oldest
            FROM device_runtime_outbox o JOIN metadata_operations m
              ON m.tenant_id=o.tenant_id AND m.id=o.operation_id
            WHERE o.state IN ('pending','failed') AND m.state='pending'
            GROUP BY o.kind,o.state ORDER BY o.kind,o.state) t""")
        prefix = sql("""SELECT COALESCE(json_agg(t),'[]'::json) FROM (
            SELECT o.id,o.kind,o.state,o.last_error,o.attempts,o.created_at,m.updated_at AS operation_updated_at
            FROM device_runtime_outbox o JOIN metadata_operations m
              ON m.tenant_id=o.tenant_id AND m.id=o.operation_id
            WHERE o.state IN ('pending','failed') AND m.state='pending'
            ORDER BY o.created_at,o.id LIMIT 50) t""")
        containers = []
        infos = []
        for container in run(['docker', 'ps', '-q']).splitlines():
            info = json.loads(run(['docker', 'inspect', container]))[0]
            infos.append(info)
            env = dict(x.split('=', 1) for x in (info['Config'].get('Env') or []) if '=' in x)
            containers.append({'name': info['Name'].lstrip('/'), 'image': info['Config']['Image'],
                               'image_id': info['Image'], 'running': info['State']['Running'],
                               'health': (info['State'].get('Health') or {}).get('Status'),
                               'compose_files': (info['Config'].get('Labels') or {}).get('com.docker.compose.project.config_files'),
                               'workers': {key: env[key] for key in (
                                   'TP_BACKGROUND_WORKERS', 'TP_RUNTIME_OUTBOX_DISPATCH') if key in env}})
        host['encore_api_base'] = encore_base_diagnostics(config, infos)
        active_commits = {image['image'].split('source-')[-1] for image in containers if 'source-' in image['image']}
        try:
            with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version', timeout=5) as response:
                backend_version = json.load(response).get('data', {}).get('version', '')
            if not re.fullmatch('dev-[0-9a-f]{40}', backend_version):
                raise ValueError('backend identity unavailable')
            active_commits.add(backend_version[4:])
            host['backend_version'] = backend_version
        except (ValueError, OSError):
            # Never propose a backend package while its current version is unknown.
            active_commits.update(p.name[8:-7] for p in pathlib.Path('/opt/tp-community-actions/incoming').glob('backend-*.tar.gz'))
            host['backend_identity_unavailable'] = True
        host['historical_package_copies'] = historical_package_copies(pathlib.Path('/opt/tp-community-actions'), active_commits)
        import_requirements = []
        for package in pathlib.Path('/opt/tp-community-actions/incoming').glob('broker-*.tar.gz'):
            try:
                with tarfile.open(str(package), mode='r|gz') as archive:
                    expanded = sum(m.size for m in archive if m.isfile())
                import_requirements.append({'artifact': package.name, 'compressed_bytes': package.stat().st_size,
                                            'additional_import_bytes_required': expanded * 2 + 256 * 1024 * 1024})
            except (OSError, tarfile.TarError):
                import_requirements.append({'artifact': package.name, 'invalid_archive': True})
        docker_root = run(['docker', 'info', '--format', '{{.DockerRootDir}}'])
        print(json.dumps({'read_only': True, 'deployment_lock_busy': False, 'database_checks_skipped': False, 'host': host, 'device_database': databases[0],
                          'queue': queue, 'oldest_50': prefix, 'containers': containers,
                          'broker_preflight': broker_preflight(infos), 'broker_import_requirements': import_requirements,
                          'docker_disk_free_bytes': shutil.disk_usage(docker_root).free,
                          'disk_free_bytes': shutil.disk_usage(str(root)).free}))


if __name__ == '__main__':
    main()
