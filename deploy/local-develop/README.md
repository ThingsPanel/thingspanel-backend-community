# Local develop fixture — incomplete, not accepted

This is a fail-closed foundation for a new local integration environment. It does
not start or build anything. `build` and `up` deliberately reject execution until
MQTT owner/plugin identities, five-database Encore runtime, notification identity,
gateway routes and immutable images have been wired and reviewed.

Use clean, fixed develop source checkouts whose **resolved paths** are on 80G.
The manifest records both source SHAs; initialization checks the backend SHA again.
Never point this tool at the existing 8080/9999 installation. It accepts no DSN,
existing volume, custom project name, or arbitrary database target. 500G and system
disk paths are rejected, including symlink resolutions.

```sh
python3 deploy/local-develop/runtime.py prepare \
  --root /Volumes/80G/tp-develop-integration/new-fixture \
  --backend /Volumes/80G/path/to/clean-backend \
  --frontend /Volumes/80G/path/to/clean-frontend
```

`prepare` creates a fresh private directory, a random database password, a manifest
and a PostgreSQL/Redis-only Compose configuration. These files are outside Git,
mode 0600 under mode 0700 directories. No credentials are read from an existing
installation. MQTT data directory is reserved, but no substitute anonymous broker
is provided. Redis has no external binding beyond loopback and is on this fixture's
private Compose network.

Before any Docker operation, verify Docker Desktop/Colima's actual VM disk and
build cache reside on 80G, then set `TP_DOCKER_STORAGE_VERIFIED=/Volumes/80G`.
This is a human attestation, not an automated proof; `docker info` cannot establish
the macOS backing-file location. The script requires 6 GiB system free space and
12 GiB external free space. Current low system space is therefore a hard blocker.

After the remaining launcher work is reviewed, foundation services may be started
with the generated Compose configuration and **exact** project `tp-develop-local`.
`bootstrap` requires that isolated PostgreSQL already exist and verifies its
Compose label and exact external data mount. It accepts only an empty community
database and absent native databases. It applies community SQL in numeric order,
then Broker SQL, then five Encore migration directories. It records checksums and
sets Encore schema_migrations. It never cleans a failed fixture or adopts a
partially initialized one; use a new empty runtime directory after investigating.
Backend-owned component migrations still run on first application startup.

## Remaining blockers

- Replace provisional base image tags with verified architecture-compatible digests
  and require all images locally available before startup; no automatic pulls.
- Build outputs, Go/pnpm/Encore caches and temporary files must all remain on 80G;
  verify Docker storage before enabling builds. Current script exposes no builder.
- Complete native MQTT owner secrets, service device allowlists and broker image.
- Add Encore/notification runtime, five DB password mappings, credential keyrings,
  Community API identity, scheduler and the develop frontend gateway routes.
- Add independent ThingsVis/protocol adapters if those modules are in acceptance.
- Run a fresh-database migration trial, login, device and MQTT command round trip,
  automation, notification and dashboard acceptance. None has been executed.

Validation: `python3 -m unittest discover -s deploy/local-develop -p 'test_*.py'`.
