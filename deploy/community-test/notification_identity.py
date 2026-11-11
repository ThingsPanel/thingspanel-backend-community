"""Automatic installation identity wiring. Never provisions tenant credentials.

Called by deployment under its exclusive lock. Tenant authorization remains in
the notification owner's plugin grants, not in environment variables or files.
"""
import json
import os
import pathlib
import secrets
import stat

RETIRED_ENV = {
    'TP_NOTIFICATION_TOKEN', 'TP_NOTIFICATION_TOKENS_JSON',
    'NOTIFICATION_SOURCE_BEARER_TOKEN', 'NOTIFICATION_PROJECTION_BEARER_TOKEN',
    'NOTIFICATION_MEMBER_READER_TENANT_IDS',
}


def private_json(path, value):
    path = pathlib.Path(path)
    if path.is_symlink():
        raise RuntimeError('Refusing symlink for notification private configuration')
    temporary = path.with_name(path.name + '.next')
    fd = os.open(str(temporary), os.O_CREAT | os.O_TRUNC | os.O_WRONLY | getattr(os, 'O_NOFOLLOW', 0), 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, indent=2)
        stream.flush()
        os.fsync(stream.fileno())
    temporary.chmod(0o600)
    os.replace(str(temporary), str(path))


def clean_environment(environment):
    return {key: value for key, value in environment.items() if key not in RETIRED_ENV}


def prepare_notification_identity(runtime):
    """Idempotently generate one installation identity and wire all consumers."""
    runtime = pathlib.Path(runtime)
    compose_path = runtime / 'compose.encore.private.json'
    config = json.loads(compose_path.read_text())
    core = config['services']['notification-core']['environment']
    automation = config['services']['encore-api']['environment']
    deployments = {value for value in [core.get('NOTIFICATION_SOURCE_DEPLOYMENT_ID'),
                    automation.get('TP_SOURCE_DEPLOYMENT_ID')] if value}
    if len(deployments) > 1:
        raise RuntimeError('Notification and automation deployment identities disagree')
    identity_path = runtime / 'notification-service.identity.private.json'
    if identity_path.exists():
        if identity_path.is_symlink():
            raise RuntimeError('Notification identity must be a regular private file')
        if os.name != 'nt' and (identity_path.stat().st_uid != os.getuid() or stat.S_IMODE(identity_path.stat().st_mode) & 0o077):
            raise RuntimeError('Notification installation identity permissions are unsafe')
        identity = json.loads(identity_path.read_text())
        if (identity.get('version') != 1 or not identity.get('deployment_id')
                or any(not isinstance(identity.get(key), str) or len(identity[key]) < 32
                       or any(c.isspace() for c in identity[key]) for key in ['service_token', 'reader_token'])):
            raise RuntimeError('Invalid notification installation identity')
        if deployments and identity['deployment_id'] not in deployments:
            raise RuntimeError('Persisted notification installation identity mismatch')
    else:
        readers = {value for value in [core.get('NOTIFICATION_LEGACY_READER_TOKEN'),
                    automation.get('TP_NOTIFICATION_SOURCE_READER_TOKEN')] if value}
        if len(readers) > 1:
            raise RuntimeError('Existing private directory identities disagree')
        identity = {'version': 1, 'deployment_id': next(iter(deployments), 'community-' + secrets.token_hex(12)),
                    'service_token': secrets.token_urlsafe(48),
                    'reader_token': next(iter(readers), secrets.token_urlsafe(48))}
        private_json(identity_path, identity)
    for service in config['services'].values():
        environment = service.get('environment')
        if isinstance(environment, dict):
            service['environment'] = clean_environment(environment)
    core = config['services']['notification-core']['environment']
    automation = config['services']['encore-api']['environment']
    core.update(NOTIFICATION_SERVICE_TOKEN=identity['service_token'],
                NOTIFICATION_SOURCE_DEPLOYMENT_ID=identity['deployment_id'],
                NOTIFICATION_LEGACY_READER_TOKEN=identity['reader_token'])
    automation.update(NOTIFICATION_SERVICE_TOKEN=identity['service_token'],
                      TP_SOURCE_DEPLOYMENT_ID=identity['deployment_id'],
                      TP_NOTIFICATION_SOURCE_READER_TOKEN=identity['reader_token'],
                      TP_NOTIFICATION_URL='http://127.0.0.1:18091')
    private_json(compose_path, config)
    return {
        'NOTIFICATION_SERVICE_TOKEN': identity['service_token'],
        'NOTIFICATION_SOURCE_DEPLOYMENT_ID': identity['deployment_id'],
        'NOTIFICATION_ENCORE_BASE_URL': 'http://127.0.0.1:18091',
        'NOTIFICATION_SOURCE_BRIDGE_ENABLED': 'true',
        'NOTIFICATION_MEMBER_READER_TOKEN': identity['reader_token'],
        'NOTIFICATION_MEMBER_READER_DEPLOYMENTS': identity['deployment_id'],
    }


def backend_process_spec(process, notification_env):
    """Build an exact PM2 environment; update-env would retain deleted keys."""
    old = process['pm2_env']
    environment = dict(old.get('env', {}))
    environment.update({key: value for key, value in old.items()
                        if key.upper() == key and isinstance(value, (str, int, float))})
    environment = clean_environment(environment)
    environment.update(notification_env)
    spec = {'name': process['name'], 'script': old['pm_exec_path'],
            'cwd': old['pm_cwd'], 'interpreter': old.get('exec_interpreter', 'none'),
            'env': environment}
    for key in ['args', 'node_args', 'autorestart', 'restart_delay', 'max_memory_restart',
                'min_uptime', 'max_restarts', 'kill_timeout', 'wait_ready', 'listen_timeout',
                'merge_logs', 'log_date_format']:
        if key in old:
            spec[key] = old[key]
    return spec


def retire_notification_renewal(runtime, actions_root, run, units=pathlib.Path('/etc/systemd/system')):
    """Remove only the retired notification issuer/timers and their private data."""
    runtime, actions_root = pathlib.Path(runtime).resolve(), pathlib.Path(actions_root).resolve()
    units = pathlib.Path(units)
    # Both the original tenant and the later App migration had independent timers.
    for timer in units.glob('tp-notification*renew*.timer'):
        service = timer.with_suffix('.service')
        if service.exists() and not any(name in service.read_text() for name in ['notification_renew.py', 'notification-source-provision']):
            continue
        run(['systemctl', 'disable', '--now', timer.name])
        if service.exists():
            run(['systemctl', 'stop', service.name])
        timer.unlink()
        if service.exists():
            service.unlink()
    run(['systemctl', 'daemon-reload'])
    for policy in list(runtime.rglob('*renew*policy*.json')):
        if runtime not in policy.resolve().parents:
            raise RuntimeError('Retired policy path is outside the notification runtime')
        data = json.loads(policy.read_text())
        credential_file = data.get('credentials_file')
        if credential_file:
            credential_file = pathlib.Path(credential_file).resolve()
            if runtime not in credential_file.parents:
                raise RuntimeError('Retired credential path is outside the notification runtime')
            if credential_file.is_file():
                credential_file.unlink()
        policy.unlink()
    for name in ['notification-source-provision', 'notification_renew.py']:
        tool = actions_root / 'notification-identity' / name
        if tool.is_file():
            tool.unlink()
