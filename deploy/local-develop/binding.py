"""Install only an already-published, database-confirmed Community binding."""
import json
import uuid
from pathlib import Path

FENCE = '1a9eb268e1beec847ab197cda4bd46bcfaa56e24'


def validate(expected, bindings, releases, key):
    provider = expected.get('provider_id', '')
    try:
        parsed = uuid.UUID(provider)
    except (ValueError, AttributeError):
        raise ValueError('Explicit confirmed provider UUID is required') from None
    if str(parsed) != provider or parsed.int == 0:
        raise ValueError('Canonical nonzero provider UUID is required')
    tenant, namespace = expected.get('tenant_id'), expected.get('namespace')
    instance = expected.get('service_instance_id')
    if not tenant or namespace != 'community' or not isinstance(instance, str) or not instance.strip() or instance != instance.strip() or len(instance) > 256:
        raise ValueError('Explicit tenant/community namespace/nonempty exact service instance are required')
    if key.get('revoked') or key.get('tenant_id') != tenant or not key.get('api_key'):
        raise ValueError('Fixture API key does not belong to the exact binding tenant')
    if not releases or any(r.get('provider_id') != provider or not r.get('version') or not r.get('schema_digest') or not r.get('artifact_digest') for r in releases):
        raise ValueError('No authoritative published provider release exists for this UUID')
    if not bindings:
        raise ValueError('No confirmed device runtime binding exists; resource bootstrap is required')
    for binding in bindings:
        if (binding.get('tenant_id'), binding.get('runtime_provider_id'), binding.get('runtime_namespace'), binding.get('service_instance_id')) != (tenant, provider, namespace, instance) or not binding.get('runtime_external_id'):
            raise ValueError('Tenant has absent or conflicting Community provider/instance bindings')
    return {'TP_RUNTIME_COMMUNITY_PROVIDER_ID': provider, 'TP_RUNTIME_COMMUNITY_NAMESPACE': namespace,
            'TP_RUNTIME_COMMUNITY_SERVICE_INSTANCE_ID': instance}


def configure(root, runtime):
    import identity
    identity.guard(root, runtime)
    manifest = runtime.load_fixture(root)
    result = runtime.run(['git', '-C', manifest['backend']['path'], 'merge-base', FENCE, manifest['backend']['sha']])
    if result != FENCE:
        raise ValueError('Pinned source does not include the reviewed tenant fence; no binding installed')
    proof = root / 'private/runtime-binding.json'
    if not proof.is_file() or proof.stat().st_mode & 0o077:
        raise ValueError('Provide owner-only private/runtime-binding.json from actual published resources; IDs are never generated or guessed')
    expected = json.loads(proof.read_text())
    # Validate literal shapes before SQL and compare all actual tenant bindings.
    key = json.loads((root / 'private/native-key.json').read_text())
    if expected.get('tenant_id') != key.get('tenant_id'):
        raise ValueError('Binding tenant differs from fixture API key')
    tenant = identity.literal(expected.get('tenant_id', ''))
    provider = identity.literal(expected.get('provider_id', ''))
    bindings = json.loads(runtime.sql(root, 'device_metadata', "SELECT coalesce(json_agg(t),'[]') FROM (SELECT tenant_id::text,runtime_provider_id::text,runtime_namespace,service_instance_id,runtime_external_id FROM device_access_bindings WHERE tenant_id=" + tenant + ')t;'))
    releases = json.loads(runtime.sql(root, 'connector_runtime', "SELECT coalesce(json_agg(t),'[]') FROM (SELECT provider_id::text,version,schema_digest,artifact_digest FROM provider_releases WHERE provider_id::text=" + provider + ')t;'))
    additions = validate(expected, bindings, releases, key)
    actual = identity.key_rows(root, runtime, key['name'])
    if len(actual) != 1 or actual[0]['id'] != key['id'] or actual[0]['tenant_id'] != expected['tenant_id'] or actual[0]['status'] != 1 or actual[0]['api_key'] != key['api_key']:
        raise ValueError('Fixture API key no longer matches active primary database identity')
    path = root / 'compose.json'
    config = json.loads(path.read_text())
    env = config['services']['encore-api']['environment']
    if env.get('TP_COMMUNITY_BASE_URL') != 'http://backend:9999' or env.get('TP_COMMUNITY_API_KEY') != key['api_key']:
        raise ValueError('Community destination/key does not match this fixture')
    env.pop('TP_RUNTIME_DEVICE_ID_MODE', None)
    env.update(additions)
    identity.atomic(path, config)
    print('Installed exact existing catalog/binding provider and instance; no provider, binding, device or service resources were created.')
