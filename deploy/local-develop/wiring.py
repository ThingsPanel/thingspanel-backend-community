"""Generate private native wiring without contacting Docker or existing installs."""
import base64
import json
from pathlib import Path
import secrets
import subprocess


def write(path, value, raw=False):
    path.write_text(value if raw else json.dumps(value, indent=2))
    path.chmod(0o600)


def keyring():
    return base64.b64encode(json.dumps({'main': base64.b64encode(secrets.token_bytes(32)).decode()}).encode()).decode()


def generate(root, manifest, password, config):
    private = root / 'private'
    token, jwt, automation = [secrets.token_hex(32) for _ in range(3)]
    redis_password = secrets.token_hex(32)
    write(private / 'redis-password', redis_password, True)
    redis = config['services'].setdefault('redis', {})
    redis['command'] = ['redis-server', '--requirepass', redis_password]
    redis['environment'] = {'REDISCLI_AUTH': redis_password}
    redis['healthcheck'] = {'test': ['CMD', 'redis-cli', 'ping'], 'interval': '5s', 'retries': 24}
    mqtt_user, mqtt_password = 'tp-local-platform', secrets.token_hex(32)
    deployment, cell, instance, client = 'local-develop', 'local-develop', 'local-mosquitto', 'tp-local-backend'
    write(private / 'plugin-token', token, True)
    write(private / 'owner-tokens.json', [{'token': token, 'broker_cell': cell, 'broker_instance': instance}])
    write(private / 'services.json', [{'username': mqtt_user, 'password': mqtt_password,
        'broker_cell': cell, 'broker_instance': instance, 'allowed_client_ids': [client, 'tp-community-readiness'],
        'allowed_device_ids': [], 'allowed_tenant_ids': [],
        'principal': {'tenant_id': 'platform', 'device_id': 'community-backend', 'principal_kind': 'service'},
        'policy': {'publish': [], 'subscribe': ['tp-internal/ingress/v1/#', 'tp-internal/state/v1/#'],
                   'max_payload': 1048576, 'max_qos': 1, 'retain_profile': 'live-ingress'},
        'policy_version': 'local-v1', 'mapping_version': 'standard-v1'}])
    # Broker image runs uid 1883. Container root bind mounts retain host modes;
    # launch the private fixture owner as root rather than making secrets world-readable.
    subprocess.run(['openssl', 'req', '-x509', '-nodes', '-newkey', 'rsa:2048', '-days', '30',
        '-subj', '/CN=frontend', '-addext', 'subjectAltName=DNS:frontend',
        '-keyout', str(private / 'internal.key'), '-out', str(private / 'internal.crt')],
        check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    write(private / 'internal-ca.pem', (private / 'internal.crt').read_text(), True)
    for name, services in [('encore', ['authn', 'thingmodel', 'devicemetadata', 'fileassets', 'connectorcatalog', 'automation']),
                           ('notification', ['authn', 'notification'])]:
        write(private / f'runtime.{name}.json', {
            'metadata': {'app_id': 'thingspanel-community', 'env_name': deployment, 'env_type': 'development',
                         'cloud': 'local', 'base_url': 'http://encore-api:14000' if name == 'encore' else 'http://notification-core:18091'},
            'hosted_services': services, 'hosted_gateways': [],
            'sql_servers': [{'host': 'postgres:5432', 'tls_config': {'disable_tls': True},
                'databases': {db: {'name': db, 'username': 'postgres', 'password': {'$env': 'TP_LOCAL_DB_PASSWORD'}}
                              for db in ['device_metadata', 'automation', 'notification', 'file_storage', 'connector_runtime']}}]})
    mount = lambda file, dest: str(private / file) + ':' + dest + ':ro'
    backend_sha, frontend_sha = manifest['backend']['sha'], manifest['frontend']['sha']
    images = {'backend': 'tp-local-backend:' + backend_sha, 'frontend': 'tp-local-frontend:' + frontend_sha,
              'encore': 'tp-local-encore:' + backend_sha, 'broker': 'tp-local-broker:' + backend_sha}
    common = {'TP_AUTH_MODE': 'legacy-jwt', 'TP_JWT_KEY': jwt, 'TP_LOCAL_DB_PASSWORD': password,
              'TP_COMMUNITY_BASE_URL': 'http://backend:9999', 'TP_COMMUNITY_SESSION_BASE_URL': 'http://backend:9999',
              'TP_AUTOMATION_SERVICE_TOKEN': automation, 'TP_SOURCE_DEPLOYMENT_ID': deployment,
              'ENCORE_INFRA_CONFIG_PATH': '/run/config/runtime.json'}
    # Reserved pending credentials are NOT activated: no verified scoped service identity exists.
    account = {'email': 'local-native@example.invalid', 'password': secrets.token_urlsafe(32)}
    write(private / 'native-account.json', account)
    encore_env = dict(common, PORT='14000', ENCORE_LISTEN_ADDR='0.0.0.0:14000',
        TP_RUNTIME_OUTBOX_DISPATCH='on', TP_BACKGROUND_WORKERS='on',
        METADATA_CREDENTIAL_KEYRING_B64=keyring(), METADATA_CREDENTIAL_ACTIVE_KEY_REF='main')
    config['services'].update({
        'broker-owner': {'image': images['broker'], 'command': ['owner'], 'user': '0:0',
            'environment': {'TP_BROKER_OWNER_LISTEN': '127.0.0.1:9801',
                'TP_BROKER_OWNER_DATABASE_DSN': f'host=postgres user=postgres password={password} dbname=ThingsPanel port=5432 sslmode=disable',
                'TP_BROKER_OWNER_TOKENS_FILE': '/run/secrets/owner-tokens.json',
                'TP_BROKER_OWNER_SERVICES_FILE': '/run/secrets/services.json'},
            'ports': ['127.0.0.1:11883:1883'], 'networks': {'default': {'aliases': ['mosquitto']}},
            'volumes': [mount('owner-tokens.json', '/run/secrets/owner-tokens.json'), mount('services.json', '/run/secrets/services.json')]},
        'mosquitto': {'image': images['broker'], 'entrypoint': ['/bin/sh', '-ec'], 'command': ['cp /run/secrets/plugin-token /tmp/plugin-token; chown 1883:1883 /tmp/plugin-token /mosquitto/data; chmod 0400 /tmp/plugin-token; exec /usr/local/sbin/mosquitto -c /mosquitto/config/mosquitto.conf'], 'user': '0:0',
            'network_mode': 'service:broker-owner', 'depends_on': ['broker-owner'],
            'environment': {'TP_BROKER_OWNER_URL': 'http://127.0.0.1:9801', 'TP_BROKER_TOKEN_FILE': '/tmp/plugin-token',
                            'TP_BROKER_CELL': cell, 'TP_BROKER_INSTANCE': instance},
            'volumes': [str(root / 'mqtt') + ':/mosquitto/data', mount('plugin-token', '/run/secrets/plugin-token'), mount('services.json', '/run/secrets/services.json')]},
        'backend': {'image': images['backend'], 'ports': ['127.0.0.1:19999:9999'],
            'environment': {'GOTP_DB_PSQL_HOST': 'postgres', 'GOTP_DB_PSQL_PORT': '5432', 'GOTP_DB_PSQL_USERNAME': 'postgres',
                'GOTP_TELEMETRY_ENABLED': 'false', 'GOTP_DB_PSQL_PASSWORD': password, 'GOTP_DB_PSQL_DBNAME': 'ThingsPanel', 'GOTP_DB_REDIS_ADDR': 'redis:6379', 'GOTP_DB_REDIS_PASSWORD': redis_password,
                'GOTP_MQTT_SERVER': 'mosquitto', 'GOTP_MQTT_ACCESS_ADDRESS': 'mosquitto:1883', 'GOTP_MQTT_BROKER': 'mosquitto:1883',
                'GOTP_MQTT_INPUT_PROFILE': 'ingress_v1', 'GOTP_MQTT_BROKER_CELL': cell, 'GOTP_MQTT_BROKER_INSTANCE': instance,
                'GOTP_MQTT_CLIENT_ID': client, 'GOTP_MQTT_USER': mqtt_user, 'GOTP_MQTT_PASS': mqtt_password,
                'GOTP_JWT_KEY': jwt, 'GOTP_AUTOMATION_MODE': 'native', 'GOTP_AUTOMATION_BASE_URL': 'http://encore-api:14000',
                'GOTP_AUTOMATION_TOKEN': automation, 'GOTP_AUTOMATION_DEPLOYMENT_ID': deployment,
                'TP_THINGMODEL_NATIVE_URL': 'http://encore-api:14000'},
            'volumes': [str(root / 'files') + ':/go/src/app/files']},
        'encore-api': {'image': images['encore'], 'ports': ['127.0.0.1:24000:14000'], 'environment': encore_env,
            'volumes': [mount('runtime.encore.json', '/run/config/runtime.json')]},
        'notification-core': {'image': images['encore'], 'ports': ['127.0.0.1:28091:18091'],
            'environment': dict(common, PORT='18091', ENCORE_LISTEN_ADDR='0.0.0.0:18091',
                NOTIFICATION_ENCORE_INFRA_CONFIG_PATH='/run/config/runtime.json', NOTIFICATION_KEYRING_B64=keyring(),
                NOTIFICATION_ACTIVE_KEY_REF='main', NOTIFICATION_LEGACY_BASE_URL='https://frontend:8443',
                SSL_CERT_FILE='/run/config/internal-ca.pem'),
            'volumes': [mount('runtime.notification.json', '/run/config/runtime.json'), mount('internal-ca.pem', '/run/config/internal-ca.pem')]},
        'frontend': {'image': images['frontend'], 'ports': ['127.0.0.1:18080:8080'],
            'volumes': [str(root / 'nginx.conf') + ':/etc/nginx/nginx.conf:ro',
                        mount('internal.crt', '/run/config/internal.crt'), mount('internal.key', '/run/config/internal.key')]}})
    write(private / 'tick.sh', '#!/bin/sh\nset -eu\nwhile true; do wget -qO /tmp/tick-result --header="Authorization: Bearer $TP_AUTOMATION_SERVICE_TOKEN" --header="X-Automation-Deployment: $TP_SOURCE_DEPLOYMENT_ID" --post-data=\"{}\" http://encore-api:14000/internal/automation/tick && touch /tmp/last-tick-ok; sleep 5; done\n', True)
    config['services']['automation-scheduler'] = {'image': images['backend'], 'entrypoint': ['/bin/sh', '/run/config/tick.sh'], 'environment': {'TP_AUTOMATION_SERVICE_TOKEN': automation, 'TP_SOURCE_DEPLOYMENT_ID': deployment}, 'volumes': [mount('tick.sh', '/run/config/tick.sh')]}
    config['services']['broker-owner']['depends_on'] = {'postgres': {'condition': 'service_healthy'}}
    config['services']['broker-owner']['healthcheck'] = {'test': ['CMD', '/usr/local/bin/tp-broker-readiness', 'owner-liveness'], 'interval': '5s', 'retries': 24}
    config['services']['mosquitto']['depends_on'] = {'broker-owner': {'condition': 'service_healthy'}}
    config['services']['backend']['depends_on'] = {'postgres': {'condition': 'service_healthy'}, 'redis': {'condition': 'service_started'}, 'mosquitto': {'condition': 'service_started'}}
    for service in ('encore-api', 'notification-core', 'automation-scheduler'):
        config['services'][service]['depends_on'] = ['backend']
    config['services']['frontend']['depends_on'] = ['backend', 'encore-api', 'notification-core']
    # Preserve develop's exact native API route list. Remove only unavailable ThingsVis
    # locations to avoid nginx startup DNS failures; dashboard remains outside this core fixture.
    nginx = (Path(manifest['frontend']['path']) / 'nginx.conf').read_text()
    import re
    nginx = re.sub(r'        location[^\n]*\{[^{}]*proxy_pass http://thingsvis-[^{}]*\}\n', '', nginx)
    if re.search(r'proxy_pass\s+http://thingsvis-', nginx):
        raise ValueError('Unrecognized ThingsVis nginx block; refuse incomplete gateway rewrite')
    if not re.search(r'listen\s+8443\s+ssl', nginx):
        end = nginx.rfind('}')
        if end < 0:
            raise ValueError('Invalid source nginx HTTP structure')
        private_tls = '''
    server {
        listen 8443 ssl;
        server_name frontend;
        ssl_certificate /run/config/internal.crt;
        ssl_certificate_key /run/config/internal.key;
        ssl_protocols TLSv1.2 TLSv1.3;
        access_log off;
        location /api/v1/notification/ {
            proxy_pass http://backend:9999;
            proxy_set_header x-token $http_x_token;
            proxy_set_header Authorization $http_authorization;
            proxy_set_header X-Notification-Deployment $http_x_notification_deployment;
        }
        location / { return 404; }
    }
'''
        nginx = nginx[:end] + private_tls + nginx[end:]
    write(root / 'nginx.conf', nginx, True)
    write(root / 'images.requested.json', images)
    return config
