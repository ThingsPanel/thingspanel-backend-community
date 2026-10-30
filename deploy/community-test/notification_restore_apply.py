"""Restore the explicitly authorized test tenant through existing public contracts."""
import datetime
import hashlib
import json
import os
import pathlib
import secrets
import shutil
import ssl
import subprocess
import time
import urllib.error
import urllib.request
import uuid

ROOT = pathlib.Path('/opt/tp-community-main-20261028')
WORK = ROOT / 'notification-app-migration'
COMPOSE = ROOT / 'compose.encore.private.json'
PM = '/root/.local/share/pnpm/pm2'
CORE = 'http://127.0.0.1:18091'
BUSINESS = 'http://127.0.0.1:9999'
PLUGIN = 'https://127.0.0.1:18445'
BRIDGE = 'https://127.0.0.1:18446'
TENANT = '63a4b529'
STAGE = 'preflight'


def save(path, data):
    path = pathlib.Path(path)
    temporary = path.with_suffix(path.suffix + '.tmp')
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding='utf-8')
    temporary.chmod(0o600)
    temporary.replace(path)


def run(args, env=None, data=None, timeout=120):
    p = subprocess.run(args, input=data, env=env, stdout=subprocess.PIPE,
                       stderr=subprocess.STDOUT, timeout=timeout)
    if p.returncode:
        (WORK / 'command-failure.private.log').write_bytes(p.stdout)
        raise RuntimeError('maintenance command failed')
    return p.stdout.decode().strip()


def api(base, path, token=None, body=None, method=None, key=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers.update(Authorization='Bearer ' + token, **{'x-token': token})
    if key:
        headers['Idempotency-Key'] = key
    r = urllib.request.Request(base + path, method=method,
                               data=json.dumps(body, ensure_ascii=False).encode() if body is not None else None,
                               headers=headers)
    try:
        with urllib.request.urlopen(r, timeout=20) as response:
            result = json.load(response)
    except urllib.error.HTTPError as e:
        (WORK / 'api-failure.private.json').write_bytes(e.read())
        raise RuntimeError('API status ' + str(e.code) + ' at ' + path)
    if result.get('code') != 200:
        raise RuntimeError('API envelope rejected at ' + path)
    return result.get('data')


def wait_backend():
    for _ in range(40):
        try:
            with urllib.request.urlopen(BUSINESS + '/health', timeout=2) as r:
                if r.status == 200:
                    return
        except Exception:
            pass
        time.sleep(1)
    raise RuntimeError('business backend health did not recover')


def restart_backend(overrides):
    current = next(p for p in json.loads(run([PM, 'jlist'])) if p['name'] == 'thingspanel-backend-community')
    env = os.environ.copy()
    for k, v in current['pm2_env'].get('env', {}).items():
        if isinstance(v, (str, int, float)):
            env[k] = str(v)
    env.update(overrides)
    run([PM, 'restart', 'thingspanel-backend-community', '--update-env'], env=env)
    wait_backend()
    run([PM, 'save'])


def combined(raw, value):
    if isinstance(raw, str):
        try:
            raw = json.loads(raw)
        except ValueError:
            raw = [x for x in raw.split(',') if x]
    result = list(raw or [])
    if value not in result:
        result.append(value)
    return result


def renew():
    env = os.environ.copy()
    env.update(json.loads(COMPOSE.read_text())['services']['notification-core']['environment'])
    env.update(NOTIFICATION_PROVISION_EXPECTED_DB='notification_main_20261028',
               NOTIFICATION_PROVISION_EXPECTED_HOST='127.0.0.1:5432')
    binary = '/opt/tp-community-actions/notification-identity/notification-source-provision'
    run([binary, '--service-policy', str(WORK / 'renew-policy.private.json'), '--ttl', '8h'], env=env)


def apply():
    global STAGE
    os.umask(0o077)
    WORK.mkdir(mode=0o700, exist_ok=True)
    incoming = pathlib.Path('/opt/tp-community-actions/incoming/notification-migration.private.json')
    payload = json.loads(incoming.read_text())
    if payload['tenant']['email'] != 'test@test.cn' or payload['tenantId'] != TENANT:
        raise RuntimeError('unexpected authorized target')
    compose = json.loads(COMPOSE.read_text())
    core_env = compose['services']['notification-core']['environment']
    deployment = core_env['NOTIFICATION_SOURCE_DEPLOYMENT_ID']
    reader_token = core_env['NOTIFICATION_LEGACY_READER_TOKEN']
    process = next(p for p in json.loads(run([PM, 'jlist'])) if p['name'] == 'thingspanel-backend-community')
    before = process['pm2_env']
    if not (WORK / 'backend-environment-before.private.json').exists():
        save(WORK / 'backend-environment-before.private.json', before)
        save(WORK / 'compose-before.private.json', compose)
    configured_reader = before.get('NOTIFICATION_MEMBER_READER_TOKEN')
    if configured_reader and configured_reader != reader_token:
        raise RuntimeError('existing reader identity mismatch')
    scopes = combined(before.get('NOTIFICATION_MEMBER_READER_TENANT_IDS'), TENANT)
    old_policy = ROOT / 'notification-renew-policy.private.json'
    if old_policy.exists():
        scopes = combined(scopes, json.loads(old_policy.read_text())['tenant_id'])
    overrides = {
        'NOTIFICATION_MEMBER_READER_TOKEN': reader_token,
        'NOTIFICATION_MEMBER_READER_DEPLOYMENTS': ','.join(combined(before.get('NOTIFICATION_MEMBER_READER_DEPLOYMENTS'), deployment)),
        'NOTIFICATION_MEMBER_READER_TENANT_IDS': ','.join(scopes)
    }
    STAGE = 'configure tenant directory'
    restart_backend(overrides)
    admin = api(BUSINESS, '/api/v1/login', body=payload['admin'])['token']
    tenant = api(BUSINESS, '/api/v1/login', body=payload['tenant'])['token']
    who = api(BUSINESS, '/api/v1/user/detail', tenant)
    if who['tenant_id'] != TENANT or who['email'] != 'test@test.cn':
        raise RuntimeError('verified tenant mismatch')
    STAGE = 'internal TLS'
    cert, private = WORK / 'internal.crt', WORK / 'internal.key'
    if not cert.exists():
        config = WORK / 'certificate.cnf'
        config.write_text('[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n[dn]\nCN=notification-internal\n[ext]\nsubjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=critical,CA:TRUE\nkeyUsage=critical,digitalSignature,keyEncipherment,keyCertSign\n')
        run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '365',
             '-config', str(config), '-keyout', str(private), '-out', str(cert)])
        cert.chmod(0o644)
    trust = ROOT / 'notification-ca.pem'
    if cert.read_bytes().strip() not in trust.read_bytes():
        with trust.open('ab') as f:
            f.write(b'\n' + cert.read_bytes())
    adapter_trust = WORK / 'trust.pem'
    adapter_trust.write_bytes(pathlib.Path('/etc/pki/tls/certs/ca-bundle.crt').read_bytes() + b'\n' + trust.read_bytes())
    adapter_trust.chmod(0o644)
    nginx = pathlib.Path('/etc/nginx/conf.d/tp-notification-app.conf')
    nginx.write_text('''server {
 listen 127.0.0.1:18445 ssl;
 ssl_certificate CERT;
 ssl_certificate_key PRIVATE;
 location / { proxy_pass http://127.0.0.1:18093; proxy_set_header Authorization $http_authorization; }
}
server {
 listen 127.0.0.1:18446 ssl;
 ssl_certificate CERT;
 ssl_certificate_key PRIVATE;
 location /api/v2/ { proxy_pass http://127.0.0.1:18091; proxy_set_header Authorization $http_authorization; }
 location /api/v1/notification/ { proxy_pass http://127.0.0.1:9999; proxy_set_header Authorization $http_authorization; }
 location / { return 404; }
}
'''.replace('CERT', str(cert)).replace('PRIVATE', str(private)))
    run(['nginx', '-t'])
    run(['systemctl', 'reload', 'nginx'])
    STAGE = 'start UniPush adapter'
    plugin_token_file = WORK / 'plugin-token.private.json'
    if not plugin_token_file.exists():
        save(plugin_token_file, {'token': secrets.token_urlsafe(36)})
    plugin_token = json.loads(plugin_token_file.read_text())['token']
    binary = pathlib.Path('/opt/tp-community-actions/incoming/notification-unipush-20261030')
    shutil.copyfile(str(binary), str(WORK / 'notification-unipush'))
    (WORK / 'notification-unipush').chmod(0o755)
    (WORK / 'Dockerfile').write_text('FROM scratch\nCOPY notification-unipush /notification-unipush\nUSER 65532:65532\nENTRYPOINT ["/notification-unipush"]\n')
    run(['docker', 'build', '-t', 'tp-notification-unipush:20261030', str(WORK)], timeout=300)
    adapter_env = {
        'NOTIFICATION_PLUGIN_TOKEN': plugin_token,
        'NOTIFICATION_PLUGIN_LISTEN': '127.0.0.1:18093',
        'NOTIFICATION_UNIPUSH_ENDPOINT': payload['push']['endpoint'],
        'NOTIFICATION_UNIPUSH_DIRECTORY_URL': BRIDGE,
        'NOTIFICATION_UNIPUSH_DIRECTORY_TOKEN': reader_token,
        'NOTIFICATION_UNIPUSH_DEPLOYMENT_ID': deployment,
        'NOTIFICATION_UNIPUSH_APP_ID': payload['push']['appId'],
        'NOTIFICATION_UNIPUSH_CLOUD_TOKEN': payload['push']['cloudToken'],
        'SSL_CERT_FILE': '/run/config/trust.pem'
    }
    adapter_compose = WORK / 'compose.private.json'
    save(adapter_compose, {'services': {'unipush': {'image': 'tp-notification-unipush:20261030',
         'network_mode': 'host', 'restart': 'unless-stopped', 'read_only': True,
         'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true'],
         'environment': adapter_env, 'volumes': [str(adapter_trust) + ':/run/config/trust.pem:ro']}}})
    run(['docker', 'compose', '-p', 'tp-notification-app', '-f', str(adapter_compose), 'up', '-d'])
    STAGE = 'notification core allowlist'
    core_env['NOTIFICATION_PLUGIN_ALLOWED_ORIGINS'] = json.dumps(combined(core_env.get('NOTIFICATION_PLUGIN_ALLOWED_ORIGINS'), PLUGIN))
    core_env['NOTIFICATION_PLUGIN_ALLOWED_CIDRS'] = json.dumps(combined(core_env.get('NOTIFICATION_PLUGIN_ALLOWED_CIDRS'), '127.0.0.1/32'))
    save(COMPOSE, compose)
    run(['docker', 'compose', '-p', 'tp-community-main-47', '-f', str(COMPOSE), 'up', '-d', '--no-deps', 'notification-core'])
    for _ in range(40):
        try:
            api(CORE, '/api/v2/notification-plugins', admin)
            break
        except Exception:
            time.sleep(1)
    STAGE = 'issue scoped source identity'
    auth = WORK / 'authorize.private.json'
    save(auth, {'admin': {'token': admin}, 'tenant': {'token': tenant}})
    credentials = WORK / 'source.private.json'
    env = os.environ.copy()
    env.update(core_env)
    env.update(NOTIFICATION_PROVISION_EXPECTED_DB='notification_main_20261028', NOTIFICATION_PROVISION_EXPECTED_HOST='127.0.0.1:5432')
    binary = '/opt/tp-community-actions/notification-identity/notification-source-provision'
    if not credentials.exists():
        run([binary, '--auth-file', str(auth), '--output', str(credentials), '--ttl', '8h'], env=env)
    auth.unlink()
    pair = json.loads(credentials.read_text())
    if pair['allowedTenantIds'] != [TENANT] or pair['deploymentId'] != deployment:
        raise RuntimeError('issued service identity scope mismatch')
    STAGE = 'enable source bridge'
    overrides.update(NOTIFICATION_SOURCE_BRIDGE_ENABLED='true', NOTIFICATION_SOURCE_DEPLOYMENT_ID=deployment,
        NOTIFICATION_ENCORE_BASE_URL=BRIDGE, NOTIFICATION_ENCORE_CA_FILE=str(trust),
        NOTIFICATION_SOURCE_BEARER_TOKEN=pair['sourceCredential'], NOTIFICATION_PROJECTION_BEARER_TOKEN=pair['projectionCredential'])
    save(WORK / 'backend-environment.private.json', overrides)
    restart_backend(overrides)
    policy = api(BUSINESS, '/api/v1/notification-default-policy', tenant)
    STAGE = 'renewal timer'
    save(WORK / 'renew-policy.private.json', {'version': 1, 'deployment_id': deployment,
        'tenant_id': TENANT, 'credentials_file': str(credentials),
        'runtime_file': str(ROOT / 'runtime.notification.private.json'), 'receipt_file': str(WORK / 'renew-result.json')})
    renewal_script = WORK / 'notification_restore_apply.py'
    shutil.copyfile(__file__, str(renewal_script))
    renewal_script.chmod(0o600)
    service = pathlib.Path('/etc/systemd/system/tp-notification-app-renew.service')
    service.write_text('[Unit]\nDescription=Renew existing test tenant App notification identity\n[Service]\nType=oneshot\nExecStart=/usr/bin/python3 ' + str(renewal_script) + ' renew\n')
    pathlib.Path('/etc/systemd/system/tp-notification-app-renew.timer').write_text('[Unit]\nDescription=Keep test App notification identity active\n[Timer]\nOnBootSec=2min\nOnUnitActiveSec=1h\nPersistent=true\n[Install]\nWantedBy=timers.target\n')
    run(['systemctl', 'daemon-reload'])
    run(['systemctl', 'start', 'tp-notification-app-renew.service'])
    run(['systemctl', 'enable', '--now', 'tp-notification-app-renew.timer'])
    STAGE = 'register and authorize plugin'
    context = ssl.create_default_context(cafile=str(adapter_trust))
    with urllib.request.urlopen(PLUGIN + '/v1/manifest', context=context, timeout=10) as r:
        manifest_raw = r.read()
    manifest = json.loads(manifest_raw)
    if manifest['plugin_id'] != 'thingspanel.unipush' or manifest['plugin_version'] != '2.0.0':
        raise RuntimeError('unexpected plugin manifest')
    plugins = api(CORE, '/api/v2/notification-plugins', admin)['items']
    plugin = next((p for p in plugins if p['pluginId'] == 'thingspanel.unipush' and p['pluginVersion'] == '2.0.0'), None)
    if plugin is None:
        plugin = api(CORE, '/api/v2/notification-plugins', admin, body={'pluginId': manifest['plugin_id'],
            'pluginVersion': manifest['plugin_version'], 'origin': PLUGIN, 'authSecret': plugin_token,
            'manifestDigest': 'sha256:' + hashlib.sha256(manifest_raw).hexdigest()}, key='app-migrate-plugin-20261030')
    elif plugin['origin'] != PLUGIN:
        raise RuntimeError('existing plugin origin requires review')
    grants = api(CORE, '/api/v2/notification-plugin-grants?page=1&pageSize=100&pluginRegistrationId=' + plugin['id'], admin)['items']
    grant = next((g for g in grants if g['tenantId'] == TENANT), None)
    if grant is None or not grant['enabled']:
        api(CORE, '/api/v2/notification-plugin-grants', admin,
            body={'pluginRegistrationId': plugin['id'], 'tenantId': TENANT, 'enabled': True, 'expectedVersion': grant['version'] if grant else 0},
            method='PUT', key='app-migrate-grant-20261030')
    STAGE = 'create App channel'
    collection = '/api/v2/platform/notification-tenants/' + TENANT + '/notification-instances'
    instance = api(CORE, collection, admin, body={'pluginRegistrationId': plugin['id'],
        'name': 'App 通知（UniPush，多设备）', 'channel': 'im', 'config': {}, 'providerIdentity': {}}, key='app-migrate-instance-20261030')
    instance = api(CORE, collection + '/' + instance['id'], admin)
    validation = api(CORE, collection + '/validate', admin,
        body={'instanceId': instance['id'], 'expectedVersion': instance['version']}, key='app-migrate-validate-20261030')
    if not validation.get('valid'):
        raise RuntimeError('channel validation failed')
    if not instance['enabled']:
        instance = api(CORE, collection + '/' + instance['id'], admin,
            body={'expectedVersion': instance['version'], 'enabled': True}, method='PUT', key='app-migrate-enable-20261030')
    STAGE = 'create member-based strategy'
    group = api(CORE, '/api/v2/notification-groups', tenant, body={
        'name': 'App 设备告警通知', 'enabled': True, 'bindings': [{
            'bindingId': str(uuid.uuid5(uuid.NAMESPACE_URL, 'thingspanel/app-migration/' + TENANT)),
            'instanceId': instance['id'], 'recipientSource': {'kind': 'member', 'userId': who['id'], 'contactField': 'appDevice'},
            'contentBinding': {'kind': 'text', 'title': '{{subject}}', 'text': '{{text}}'}
        }]}, key='app-migrate-group-20261030')
    STAGE = 'publish alert strategy'
    route = api(BUSINESS, '/api/v1/notification_group/native-publish?nativeGroupId=' + group['id'], tenant)
    if not route.get('published'):
        route = api(BUSINESS, '/api/v1/notification_group/native-publish', tenant,
            body={'operation': 'publish', 'nativeGroupId': group['id'], 'groupRevision': group['revision'],
                  'name': group['name'], 'expectedRouteVersion': route['routeVersion']}, key='app-migrate-publish-20261030')
    result = {'status': 'restored', 'tenantId': TENANT, 'account': who['email'], 'recipientUserId': who['id'],
        'pluginId': plugin['id'], 'instanceId': instance['id'], 'groupId': group['id'],
        'published': route.get('published'), 'defaultPolicyChanged': False,
        'defaultPolicyReadable': True, 'realPhoneNotificationSent': False,
        'adapterSource': 'fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05',
        'observedAtUTC': datetime.datetime.utcnow().isoformat() + 'Z'}
    save(WORK / 'result.json', result)
    incoming.unlink()
    print(json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    import sys
    try:
        if sys.argv[1] == 'renew':
            renew()
        elif sys.argv[1] == 'apply':
            apply()
        else:
            raise RuntimeError('unsupported maintenance mode')
    except Exception as e:
        WORK.mkdir(mode=0o700, exist_ok=True)
        import traceback
        (WORK / 'failure.private.log').write_text(traceback.format_exc())
        print(json.dumps({'status': 'failed', 'stage': STAGE, 'error': str(e)}))
        raise SystemExit(1)
