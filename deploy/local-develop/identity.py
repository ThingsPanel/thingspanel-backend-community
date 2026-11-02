"""Fixture-only identities. Broad test-tenant API keys are explicitly limited to a fresh owned fixture."""
import json
import os
import re
import urllib.request
import urllib.error


def literal(value):
    return "'" + str(value).replace("'", "''") + "'"


def guard(root, runtime):
    runtime.preflight(root)
    if root.stat().st_mode & 0o077 or (root / 'private').stat().st_mode & 0o077:
        raise ValueError('Fixture and private directory must be mode 0700')
    for path in [root / 'manifest.json', root / 'compose.json', *(root / 'private').iterdir()]:
        if path.is_symlink() or (path.is_file() and path.stat().st_mode & 0o077):
            raise ValueError('Private fixture files must be regular owner-only files')
    manifest = runtime.load_fixture(root)
    if not (root / 'bootstrap.complete').exists() or not re.fullmatch(r'[a-f0-9]{64}', manifest.get('runtime_id', '')):
        raise ValueError('Fresh fixture bootstrap ownership proof is required')
    network_ids = set()
    for service in ('postgres', 'backend'):
        container = runtime.compose(root, 'ps', '-q', service)
        if not container or '\n' in container:
            raise ValueError('Expected one running fixture container: ' + service)
        info = json.loads(runtime.run(['docker', 'inspect', container]))[0]
        labels = info['Config']['Labels']
        if labels.get('com.docker.compose.project') != runtime.PROJECT or labels.get('com.docker.compose.project.working_dir') != str(root):
            raise ValueError('Container ownership mismatch')
        if info['HostConfig'].get('NetworkMode') == 'host' or len(info['NetworkSettings']['Networks']) != 1:
            raise ValueError('Unexpected fixture network mode/membership')
        network_id = next(iter(info['NetworkSettings']['Networks'].values()))['NetworkID']
        network_ids.add(network_id)
        network = json.loads(runtime.run(['docker', 'network', 'inspect', network_id]))[0]
        if network.get('Labels', {}).get('com.docker.compose.project') != runtime.PROJECT:
            raise ValueError('Network belongs to another project')
        if service == 'postgres' and not any(m['Destination'] == '/var/lib/postgresql/data' and m['Source'] == str(root / 'postgres') for m in info['Mounts']):
            raise ValueError('Postgres mount mismatch')
        if service == 'postgres':
            if info['NetworkSettings']['Ports'].get('5432/tcp', []) != [{'HostIp': '127.0.0.1', 'HostPort': '15555'}]:
                raise ValueError('Postgres loopback binding mismatch')
        if service == 'backend':
            env = dict(item.split('=', 1) for item in info['Config']['Env'] if '=' in item)
            if env.get('GOTP_DB_PSQL_HOST') != 'postgres' or env.get('GOTP_DB_PSQL_DBNAME') != 'ThingsPanel' or env.get('GOTP_DB_PSQL_PORT') != '5432':
                raise ValueError('Backend database target mismatch')
            ports = info['NetworkSettings']['Ports'].get('9999/tcp', [])
            if ports != [{'HostIp': '127.0.0.1', 'HostPort': '19999'}]:
                raise ValueError('Backend loopback binding mismatch')

    if len(network_ids) != 1:
        raise ValueError('Backend and postgres do not share the same private fixture network')
    owner = runtime.sql(root, 'ThingsPanel', 'SELECT runtime_id FROM local_fixture_owner;')
    if owner != manifest['runtime_id']:
        raise ValueError('Database was not bootstrapped by this fresh fixture')


def api(path, body, token=None, method='POST'):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['x-token'] = token
    request = urllib.request.Request('http://127.0.0.1:19999/api/v1/' + path,
        data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
    # Refuse proxy environment and redirects: credentials only reach verified loopback.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        with opener.open(request, timeout=15) as response:
            result = json.loads(response.read(1024 * 1024))
    except (urllib.error.URLError, json.JSONDecodeError):
        raise ValueError('Fixture auth API failed; response suppressed to protect credentials') from None
    if result.get('code') != 200:
        raise ValueError('Fixture auth API rejected operation; response suppressed')
    return result.get('data')


def rows(root, runtime, where):
    return json.loads(runtime.sql(root, 'ThingsPanel', "SELECT coalesce(json_agg(t),'[]') FROM (SELECT id,email,authority,status,tenant_id FROM users WHERE " + where + ')t;'))


def atomic(path, value):
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(value, indent=2))
    temporary.chmod(0o600)
    os.replace(temporary, path)


def key_rows(root, runtime, name):
    return json.loads(runtime.sql(root, 'ThingsPanel', "SELECT coalesce(json_agg(t),'[]') FROM (SELECT id,tenant_id,created_id,status,api_key,name FROM open_api_keys WHERE name=" + literal(name) + ')t;'))


def account_login(root, runtime):
    account = json.loads((root / 'private/native-account.json').read_text())
    found = rows(root, runtime, 'email=' + literal(account['email']))
    if len(found) != 1 or found[0]['authority'] != 'TENANT_ADMIN' or found[0]['status'] != 'N':
        raise ValueError('Test account is not an active tenant administrator')
    if account.get('id') and (account['id'] != found[0]['id'] or account['tenant_id'] != found[0]['tenant_id']):
        raise ValueError('Test account ownership changed')
    account.update(id=found[0]['id'], tenant_id=found[0]['tenant_id'])
    token = api('login', {'email': account['email'], 'password': account['password']})['token']
    return account, token


def configure_key(root, key):
    path = root / 'compose.json'
    config = json.loads(path.read_text())
    env = config['services']['encore-api']['environment']
    if env.get('TP_COMMUNITY_BASE_URL') != 'http://backend:9999' or env.get('TP_COMMUNITY_SESSION_BASE_URL') != 'http://backend:9999':
        raise ValueError('Refusing to install credentials for a non-fixture Community destination')
    env.pop('TP_COMMUNITY_EMAIL', None)
    env.pop('TP_COMMUNITY_PASSWORD', None)
    if key:
        env['TP_COMMUNITY_API_KEY'] = key
    else:
        env.pop('TP_COMMUNITY_API_KEY', None)
    atomic(path, config)


def initialize_administrator(root, runtime):
    # Caller has already proven fresh fixture/container/database ownership.
    path = root / 'private/local-administrator.json'
    if not path.exists():
        if rows(root, runtime, "authority='SYS_ADMIN'"):
            raise ValueError('Existing administrator has no fixture-owned credential; refusing adoption')
        atomic(path, {'email': 'local-admin-' + runtime.secrets.token_hex(12) + '@example.invalid',
                      'password': runtime.secrets.token_urlsafe(32) + 'Aa1!'})
    account = json.loads(path.read_text())
    admins = rows(root, runtime, "authority='SYS_ADMIN'")
    if not admins:
        api('tenant/super-admin/init', {'email': account['email'], 'password': account['password'],
                                     'confirm_password': account['password']})
        admins = rows(root, runtime, "authority='SYS_ADMIN'")
    if len(admins) != 1 or admins[0]['email'] != account['email'] or admins[0]['status'] != 'N':
        raise ValueError('Administrator identity mismatch; refusing privileged login')
    return api('login', {'email': account['email'], 'password': account['password']})['token']


def provision(root, runtime):
    guard(root, runtime)
    private = root / 'private'
    statefile = private / 'native-key.json'
    if statefile.exists():
        state = json.loads(statefile.read_text())
        if state.get('revoked'):
            raise ValueError('Fixture key was revoked; no automatic resurrection or reissuance')
    else:
        state = {'name': 'local-fixture-' + runtime.secrets.token_hex(16), 'revoked': False}
        atomic(statefile, state)  # Persist operation identity before the first HTTP write.
    account = json.loads((private / 'native-account.json').read_text())
    found = rows(root, runtime, 'email=' + literal(account['email']))
    if not found:
        token = initialize_administrator(root, runtime)
        api('user', {'email': account['email'], 'password': account['password'],
            'phone_number': 'local-' + runtime.hashlib.sha256(account['email'].encode()).hexdigest()[:16],
            'name': 'Local test tenant', 'userRoles': [], 'remark': 'Broad identity for isolated local integration only'}, token)
    account, token = account_login(root, runtime)
    atomic(private / 'native-account.json', account)
    keys = key_rows(root, runtime, state['name'])
    if not keys:
        api('open/keys', {'tenant_id': account['tenant_id'], 'name': state['name']}, token)
        keys = key_rows(root, runtime, state['name'])
    if len(keys) != 1 or keys[0]['tenant_id'] != account['tenant_id'] or keys[0]['created_id'] != account['id'] or keys[0]['status'] != 1:
        raise ValueError('API key result is absent, ambiguous, inactive or outside test tenant; refusing adoption')
    key = keys[0]
    if state.get('id') and state['id'] != key['id']:
        raise ValueError('Previously issued key identity changed')
    state.update(id=key['id'], tenant_id=key['tenant_id'], created_id=key['created_id'], api_key=key['api_key'])
    atomic(statefile, state)
    configure_key(root, key['api_key'])
    atomic(private / 'broker-scope.json', {'tenant_id': account['tenant_id']})
    print('Issued/verified broad TENANT_ADMIN API key for one fresh local test tenant. Recreate only fixture Encore to load it. Not a production service identity.')


def revoke(root, runtime):
    guard(root, runtime)
    path = root / 'private/native-key.json'
    if not path.exists():
        configure_key(root, None)
        print('No fixture key issued; local runtime credential removed.')
        return
    state = json.loads(path.read_text())
    account, token = account_login(root, runtime)
    keys = key_rows(root, runtime, state['name'])
    if len(keys) != 1 or keys[0]['tenant_id'] != account['tenant_id'] or keys[0]['created_id'] != account['id'] or (state.get('id') and keys[0]['id'] != state['id']):
        raise ValueError('Key ownership mismatch; refusing unrelated-key revocation')
    if keys[0]['status'] == 1:
        api('open/keys', {'id': keys[0]['id'], 'status': 0}, token, 'PUT')
    after = key_rows(root, runtime, state['name'])
    if len(after) != 1 or after[0]['status'] != 0:
        raise ValueError('Key revocation not confirmed')
    state['revoked'] = True
    state.pop('api_key', None)
    atomic(path, state)
    configure_key(root, None)
    print('Fixture key revocation confirmed in primary database; local credential removed. No services restarted.')


def scoped_policy(policy, targets, tenant):
    if not tenant or len(targets) > 256:
        raise ValueError('Invalid tenant or broker target scope exceeds 256 devices')
    ids = []
    for target in targets:
        if target.get('tenant_id') != tenant or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', target.get('id', '')):
            raise ValueError('Broker target tenant/id mismatch')
        ids.append(target['id'])
    if len(set(ids)) != len(ids):
        raise ValueError('Duplicate broker device IDs')
    policy['allowed_device_ids'] = sorted(ids)
    policy['allowed_tenant_ids'] = [tenant] if ids else []
    policy['policy']['publish'] = ['devices/command/+/+', 'gateway/command/+/+'] if ids else []
    # Only supported durable CMD family; legacy control/OTA families remain denied.
    return policy


def refresh(root, runtime, device_ids):
    guard(root, runtime)
    scopefile = root / 'private/broker-scope.json'
    if not scopefile.exists() or scopefile.stat().st_mode & 0o077:
        raise ValueError('Supply private/broker-scope.json mode 0600 with an explicit fixture tenant_id')
    tenant = json.loads(scopefile.read_text()).get('tenant_id')
    if not tenant or not re.fullmatch(r'[A-Za-z0-9_-]{1,128}', tenant):
        raise ValueError('Invalid explicit fixture tenant scope')
    current = rows(root, runtime, "tenant_id=" + literal(tenant) + " AND authority='TENANT_ADMIN' AND status='N'")
    if len(current) != 1:
        raise ValueError('Tenant must have exactly one active fixture administrator')
    if len(device_ids) > 256 or len(set(device_ids)) != len(device_ids):
        raise ValueError('Duplicate or excessive broker device targets')
    targets = []
    if device_ids:
        statement = "SELECT coalesce(json_agg(t),'[]') FROM (SELECT d.id,d.tenant_id FROM devices d LEFT JOIN device_configs c ON c.id=d.device_config_id AND c.tenant_id=d.tenant_id WHERE d.tenant_id=" + literal(tenant) + " AND d.id IN (" + ','.join(literal(i) for i in device_ids) + ") AND d.is_enabled='enabled' AND d.activate_flag='active' AND (d.parent_id IS NULL OR d.parent_id='') AND (d.service_access_id IS NULL OR d.service_access_id='') AND coalesce(d.access_way,'A')<>'B' AND coalesce(c.protocol_type,'MQTT') IN ('MQTT','') ORDER BY d.id)t;"
        targets = json.loads(runtime.sql(root, 'ThingsPanel', statement))
    if {t['id'] for t in targets} != set(device_ids):
        raise ValueError('Requested device missing, inactive, wrong tenant or unsupported protocol')
    path = root / 'private/services.json'
    policies = json.loads(path.read_text())
    if len(policies) != 1 or policies[0]['username'] != 'tp-local-platform':
        raise ValueError('Unexpected broker service policy; refusing overwrite')
    policy = scoped_policy(policies[0], targets, tenant)
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps([policy], indent=2))
    temporary.chmod(0o600)
    os.replace(temporary, path)
    print('Updated finite broker device scope. Restart fixture broker-owner and mosquitto to activate; no restart performed.')
