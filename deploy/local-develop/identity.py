"""Fixture-only identities. Never imports existing admin credentials or broad roles."""
import json
import os
import re
import urllib.request
import urllib.error


def literal(value):
    return "'" + str(value).replace("'", "''") + "'"


def guard(root, runtime):
    runtime.preflight(root)
    runtime.load_fixture(root)
    for service in ('postgres', 'backend'):
        container = runtime.compose(root, 'ps', '-q', service)
        if not container or '\n' in container:
            raise ValueError('Expected one running fixture container: ' + service)
        info = json.loads(runtime.run(['docker', 'inspect', container]))[0]
        labels = info['Config']['Labels']
        if labels.get('com.docker.compose.project') != runtime.PROJECT or labels.get('com.docker.compose.project.working_dir') != str(root):
            raise ValueError('Container ownership mismatch')
        if service == 'postgres' and not any(m['Destination'] == '/var/lib/postgresql/data' and m['Source'] == str(root / 'postgres') for m in info['Mounts']):
            raise ValueError('Postgres mount mismatch')
        if service == 'backend':
            ports = info['NetworkSettings']['Ports'].get('9999/tcp', [])
            if ports != [{'HostIp': '127.0.0.1', 'HostPort': '19999'}]:
                raise ValueError('Backend loopback binding mismatch')


def api(path, body, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['x-token'] = token
    request = urllib.request.Request('http://127.0.0.1:19999/api/v1/' + path,
        data=json.dumps(body).encode(), headers=headers, method='POST')
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


def provision(root, runtime):
    # Existing Community Casbin allows routes without g2 registrations. A normal
    # TENANT_USER therefore cannot prove a least-privilege service boundary.
    raise ValueError('Native account provisioning is blocked: Community lacks a verified scoped service-identity contract; no user, roles or API keys created')


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
