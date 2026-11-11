# Default SMTP adapter deployment

This directory is the manual adapter-only deployment profile for the fixed `thingspanel.smtp` plugin. For a fresh notification Core deployment with SMTP included and first-start registration enabled by default, use [`deployments/notification/`](../../../deployments/notification/). This profile leaves Core bootstrap under explicit operator control and creates only the global plugin registration when enabled. It creates no tenant grant, account, or send. A `SYS_ADMIN` must grant the plugin to a selected tenant, then configure that tenant's own encrypted SMTP account. The account remains disabled until explicitly enabled.

## Prepare the adapter

Create an operator-owned private Docker network shared with the Encore Core container. Set `NOTIFICATION_PRIVATE_NETWORK` to that exact network name. Provide a TLS certificate and key whose SAN matches `PLUGIN_TLS_HOST`; mount them read-only. The Core container must trust the certificate chain and its exact plugin origin/CIDR policy must allow the chosen endpoint. This sample keeps the host mapping loopback-only; deployments may use an internal network ingress with the same verified TLS identity.

Create an adapter environment file outside this source tree and restrict it to the service operator (for example, mode `0600`). It must contain:

```dotenv
NOTIFICATION_PLUGIN_TOKEN=<operator-generated random token>
NOTIFICATION_SMTP_ALLOWED_SERVERS=<exact-approved-host:port list>
NOTIFICATION_SMTP_ALLOWED_CIDRS=<operator-approved private ranges, if required>
NOTIFICATION_PLUGIN_UI_ORIGINS=<exact management UI origins>
```

Do not put tenant SMTP passwords in this file. The adapter receives a tenant account's decrypted configuration only for a Core validate or delivery request. The adapter's server-side SMTP allowlist is operator-controlled and must include only the SMTP servers the service is permitted to contact.

Build and start the service from this directory:

```sh
export SMTP_PLUGIN_ENV_FILE=/absolute/private/path/smtp-plugin.env
export PLUGIN_TLS_HOST=smtp-plugin.internal.example
export PLUGIN_TLS_CERT_FILE=/absolute/private/path/tls.crt
export PLUGIN_TLS_KEY_FILE=/absolute/private/path/tls.key
export PLUGIN_TLS_HOST_PORT=5443
export NOTIFICATION_PRIVATE_NETWORK=notification-private
docker compose -f compose.yaml up --build -d
```

The plugin's origin is `https://smtp-plugin.internal.example` (the configured host, without a path). Configure that exact origin and its approved network range in the Core allowlist. Enable `NOTIFICATION_DEFAULT_SMTP_BOOTSTRAP=true` only after the TLS endpoint is reachable and the Core allowlist is active; provide the same origin and adapter token in the Core's private runtime environment. Bootstrap verifies the manifest is the fixed `thingspanel.smtp` plugin, stores the authentication credential encrypted, and leaves grants and instances untouched. If the plugin is already registered, a restart leaves its existing registration and key unchanged.

`POST /v1/validate` checks the configuration schema and operator allowlist. It does not authenticate to the SMTP server or prove message delivery. The UI's explicit “test send” action queues a real message, so avoid it in setup checks. A tenant may not write or validate account credentials through the tenant API; the platform administrator uses the selected-tenant account page. SMTP provider credentials stay encrypted in each tenant-scoped account.

The checked-in Compose file contains no certificate, private key, token, or SMTP account. Do not commit rendered Compose output or operator environment files.
