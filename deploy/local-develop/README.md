# Isolated local develop runtime — wired, not accepted

`runtime.py` creates a new private fixture on 80G. It never reads an existing
installation's credentials or database. Both clean source checkout paths must
resolve to 80G; their immutable SHAs are recorded and rechecked. 500G and system
paths are rejected, including symlinks. All published ports bind 127.0.0.1:
frontend 18080, backend 19999, Encore 24000, notifications 28091, PostgreSQL
15555, Redis 16379 and MQTT 11883. The old 8080/9999 project is untouched.

```sh
python3 deploy/local-develop/runtime.py prepare --root /Volumes/80G/tp-develop-integration/new-fixture \
  --backend /Volumes/80G/path/to/backend --frontend /Volumes/80G/path/to/frontend
```

Generated private files contain random PostgreSQL/MQTT/JWT/automation credentials,
metadata/notification encryption keyrings, internal TLS and dedicated native
account credentials. They are mode 0600, outside Git under a 0700 root. Broker
starts with empty device permission scope. Its root wrapper copies the token to
0400 uid1883 storage before Mosquitto drops privileges; owner remains root in this
local private fixture. Do not use this profile as a production hardening template.

## Build window and immutable images

Build execution remains disabled in the CLI until the operator coordinates disk
and I/O capacity. No builds, migrations or startup have been tested. Before any
Docker operation, verify its actual VM disk/build cache reside on 80G and set
`TP_DOCKER_STORAGE_VERIFIED=/Volumes/80G`. This is explicit operator attestation;
`docker info` cannot prove the macOS backing-file location. Every phase checks
6 GiB free on system storage and 12 GiB on 80G.

Build from the pinned clean checkouts only. All Go/pnpm/Encore caches and TMPDIR
must point beneath the fixture cache directory. Do not build from the canonical
dirty checkout. Use sequential builds, GOMAXPROCS=1 and GOFLAGS=-p=1.

- Backend: root Dockerfile, tag `tp-local-backend:<backend-sha>` and label
  `org.opencontainers.image.revision=<backend-sha>`. The current root Dockerfile
  copies the full checkout into the image: budget its storage before building.
- Broker: `deploy/mosquitto-community/Dockerfile.production`, tag
  `tp-local-broker:<backend-sha>` and same revision label. Native architecture.
- Frontend: install using the pinned pnpm lock, build production `pnpm build`;
  use `deploy/release/Dockerfile`, tag `tp-local-frontend:<frontend-sha>` with
  frontend revision label. Production mode avoids the hardcoded remote dev API.
- Encore: CLI 1.58.6 in `encore-api`, run `encore build docker
  tp-local-encore:<backend-sha> --services=authn,thingmodel,devicemetadata,fileassets,connectorcatalog,automation,notification --skip-config`.
  Add a derived metadata-only Dockerfile `FROM <built-image-id>` plus the backend
  revision label if Encore did not set it. Never relabel an unrelated image.
- Foundation images: `timescale/timescaledb:latest-pg14` and `redis:6.2.7-alpine`
  are initial lookup names only. Obtain compatible reviewed images during the
  coordinated window, then `lock-images` resolves **already local** images to
  immutable IDs, checks application revision labels, and sets `pull_policy=never`.
  Runtime never automatically pulls. Preserve the generated images.lock.json.

## Explicit phase order

Run each command with `--root <fixture>`:

1. `preflight` — storage and Docker availability.
2. `lock-images` — all foundation/application images must already exist locally.
3. `up --stage foundation` — only new PostgreSQL and Redis.
4. `bootstrap` — verifies exact Compose project and external PG mount, requires
   empty community DB and absent five native DBs, applies core numbered SQL then
   Broker SQL then five Encore directories in numeric order, records checksums
   and Encore schema_migrations. A failure requires investigation and a fresh
   fixture; no automatic cleanup/adoption. Backend component migrations run on
   first backend startup. Completion marker is written only after all SQL.
5. `up --stage native` — broker owner/plugin, backend, Encore, notifications,
   scheduler and frontend. Port conflicts stop execution rather than replacing
   anything. Startup is not readiness or integration acceptance.

## Required acceptance and outstanding integration

- Health/login through 18080, plus old 8080/9999 identity/data-volume unchanged.
- Native Community authentication is **blocked**, not replaced with an ordinary
  tenant user. Existing Casbin middleware allows unregistered routes; therefore a
  TENANT_USER with no roles does not prove least privilege. `provision-native`
  rejects execution before any I/O, and generated Encore environment activates
  neither password credentials nor API keys. A verified scoped service-identity
  contract is required before native device runtime calls can authenticate.
- Broker platform target scope refresh is implemented independently. Write an
  explicit `private/broker-scope.json` (0600) containing `{"tenant_id":"<fixture-tenant>"}`,
  then run `refresh-broker --device-id <id>` for every intended fixture device.
  PostgreSQL/backend project ownership, exact PG mount and backend loopback binding
  are verified first. The selected tenant must have one active tenant administrator.
  Every requested device must be in that tenant, active/enabled, standalone, direct
  MQTT and free of protocol-service access. Any missing or unsupported target
  rejects the complete update; maximum 256, no duplicates. No IDs means revoke all
  platform publish scope. Only the durable CMD topic family is enabled. The file
  replacement is atomic and idempotent; restart only fixture broker-owner and
  mosquitto to activate it. No restart is performed by the refresh command.
- MQTT telemetry/history/command acknowledgement and device credential activation;
  native devices/templates/groups/files, automation tick and notification delivery.
  Notification channels/source identities still require real tenant authorization.
- ThingsVis and protocol adapters are not included. Their proxy blocks are removed
  from the copied develop nginx to avoid unresolved-upstream startup failure.
  Dashboard/protocol acceptance therefore remains pending; never claim full-stack
  acceptance from the core fixture. Nothing points those modules at an old stack.

Validation: `python3 -m unittest discover -s deploy/local-develop -p 'test_*.py'`.
