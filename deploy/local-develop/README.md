# Isolated local develop runtime — wired, not accepted

`runtime.py` creates a new private fixture on 80G. It never reads an existing
installation's credentials or database. Both clean source checkout paths must
resolve to 80G; their immutable SHAs are recorded and rechecked. 500G and system
paths are rejected, including symlinks. All published ports bind 127.0.0.1:
frontend 18080, backend 19999, Encore 24000, notifications 28091, PostgreSQL
15555, Redis 16379 and MQTT 11883. The old 8080/9999 project is untouched.

```sh
python3 deploy/local-develop/runtime.py prepare --root $TP_LOCAL_WORKSPACE_ROOT/tp-develop-integration/new-fixture \
  --backend $TP_LOCAL_WORKSPACE_ROOT/path/to/backend --frontend $TP_LOCAL_WORKSPACE_ROOT/path/to/frontend
```

Generated private files contain random PostgreSQL/MQTT/JWT/automation credentials,
metadata/notification encryption keyrings, internal TLS and dedicated native
account credentials. They are mode 0600, outside Git under a 0700 root. Broker
starts with empty device permission scope. Its root wrapper copies the token to
0400 uid1883 storage before Mosquitto drops privileges; owner remains root in this
local private fixture. Do not use this profile as a production hardening template.

## Build window and immutable images

The build command is now executable, but must only be invoked in the coordinated
I/O window. **No real build has been run or accepted.** Before Docker operations,
verify its actual VM disk/build cache reside on 80G and set
`TP_DOCKER_STORAGE_VERIFIED=$TP_LOCAL_WORKSPACE_ROOT`. This is operator attestation; Docker info
cannot prove the macOS backing-file location. Each phase starts/finishes with 6 GiB
free system and 12 GiB free 80G checks. A running build is polled every 2 seconds
and its process group is terminated if either volume falls below 4 GiB. These
checks reduce risk but cannot guarantee protection from sudden unrelated writes.

Use clean pinned source checkouts on 80G. For each command append `--root <fixture>`:

```sh
python3 deploy/local-develop/runtime.py build --root <fixture> --phase backend --runtime-image <local-v1.3-backend-image>
python3 deploy/local-develop/runtime.py build --root <fixture> --phase frontend --runtime-image <local-nginx-or-frontend-image>
python3 deploy/local-develop/runtime.py build --root <fixture> --phase encore
```

- Backend uses host Go to cross-compile CGO-disabled **linux/selected-architecture** develop binary
  directly to 80G, with trimpath and the full source SHA in SYSTEM_VERSION. No new
  Docker image layer is produced. Its binary and matching source configs/sql/static
  snapshot are mounted read-only over an already-local linux/selected-architecture v1.3 runtime
  image. The old image supplies only runtime filesystem/libraries; it is **not**
  represented as develop. Binary/resource SHA256, exact source SHA, immutable base
  image ID and required read-only mount paths are checked again before image lock
  and startup. Runtime binary/asset compatibility still needs actual acceptance.
  Resources are snapshot-once; use a fresh fixture after interrupted resource copy.
- Frontend exports the tracked pinned tree into 80G (checks file hashes on reuse),
  runs frozen-lockfile pnpm install with store on 80G, then normal production
  `pnpm build` including typecheck. dist is read-only mounted over the supplied
  local linux/selected-architecture nginx image; develop nginx supplies routing. Full asset hash
  inventory is checked before startup. This avoids a new frontend Docker layer.
- Encore requires official CLI **1.58.6**, runs its official `encore build docker`
  with the seven actual services, and verifies linux/selected-architecture output. It still creates
  Docker image/cache data, so Docker external storage proof is mandatory. Cache,
  Go module/build directories, TMPDIR and XDG cache are directed to 80G. Exact SHA,
  CLI version and output image ID are persisted; no unrelated image is relabeled.
  The CLI's actual daemon/tool cache behavior must be inspected in the build window.
- All phases run sequentially with GOMAXPROCS=1/GOFLAGS=-p=1. Output logs live in
  fixture cache. A failed phase does not activate its runtime configuration. A
  successful phase invalidates images.lock.json so images must be relocked.
- Broker source-image build remains explicit/manual using the develop
  `deploy/mosquitto-community/Dockerfile.production` and native linux/selected-architecture, tagged
  `tp-local-broker:<backend-sha>` with revision label. This expensive C/CGo phase
  is not silently substituted with an old broker; it still needs its own budget.
- PostgreSQL/Redis initial image lookup tags are resolved by `lock-images` to
  already-local immutable IDs. It checks source labels for non-overlay application
  images and generated build provenance for overlays/Encore, then sets
  pull_policy=never. Runtime never pulls images implicitly.

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
- **Explicit local-test exception:** `provision-native` uses the existing formal
  Community API-key/TENANT_ADMIN contract, with broad authority inside a single
  new test tenant. This is not a scoped production service identity and is not a
  production recommendation. The operator authorized this only for a new isolated
  database with loopback ports and random private credentials.
- After bootstrap and backend startup, run `provision-native --root <fixture>`.
  It checks the source manifest, completed bootstrap, random ownership nonce in
  this database, exact PG external mount, Compose project/working directory,
  one shared project-owned network, backend DB destination and loopback bindings.
  Imported/previous fixtures lacking the database ownership nonce are refused.
  It logs in to the known fresh database seed, creates a new TENANT_ADMIN with a
  random password through `/api/v1/user`, then logs into that tenant and issues
  its API key through `/api/v1/open/keys`. SQL is used only to reconcile/verify
  existing results; no user, role or key rows are inserted outside owner APIs.
  HTTP has fixed loopback destination, no proxy inheritance and no redirects.
- The key operation name is persisted before issuance; retries reconcile the same
  exact tenant/creator/key and reject ambiguity or inactive/replaced keys. Local
  identity operations are serialized with flock. Key and account secrets remain
  in mode-0600 fixture files. Compose receives `TP_COMMUNITY_API_KEY`; recreate
  only this fixture's Encore service to activate the new configuration.
- `revoke-native --root <fixture>` disables that exact key through the official
  authenticated PUT API and confirms status=0 in the primary database. It removes
  the local runtime key, persists a tombstone, and never automatically resurrects
  or reissues a revoked identity. Repeating revoke verifies the disabled state.
  Existing processes holding the old key are rejected by live DB key validation.
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

## Explicit internal OrbStack bounded mode (no relocation)

Local read-only inspection on 2026-11-02 found context `orbstack`, endpoint
`unix:///Users/junhong/.orbstack/run/docker.sock`, backing file
`~/Library/Group Containers/HUAQ24HBR6.dev.orbstack/data/data.img.raw` on internal
`/System/Volumes/Data` (`/dev/disk3s5`), with about 6.1 GiB free. The file is sparse
(228 GiB logical, about 17 GiB allocated for its data directory); logical size is
not available capacity. `/var/lib/docker` from Docker info is inside the VM and
does not establish host storage. No files were moved or deleted.

For this exact verified local layout only, set both:

```sh
export TP_DOCKER_STORAGE_MODE=internal-bounded
export TP_ALLOW_INTERNAL_BOUNDED_BUILD=yes
```

Every preflight rechecks the context, Unix endpoint, actual backing-file location
and APFS Data volume device. This mode explicitly records **internal** storage;
it does not set or imply the external-storage attestation.

| Phase | Internal Data free before/after | External 80G free before/after |
|---|---:|---:|
| Core runtime / backend overlay | 6 GiB | 12 GiB |
| Frontend install/build overlay | 6 GiB | 12 GiB |
| Official Encore Docker build | 12 GiB | 12 GiB |
| Native Broker image build budget | 16 GiB | 12 GiB |

The build monitor terminates the local process group on observing either volume
below 4 GiB. This is a stop trigger, **not an OS-enforced storage quota**: polling
latency, unrelated writes and daemon-side work can cross that floor before the
local process exits. It does not prune images/caches or kill the Docker daemon.
Consequently no strict guaranteed 4 GiB reserve can be promised without a storage
quota or separated writable storage. At observed 6.1 GiB, backend/frontend phases
have only about 2.1 GiB headroom and remain capacity-sensitive; Encore/Broker are
blocked by preflight. Do not waive these checks to finish a build. No heavy build
has been attempted in this mode.

## Exact provider/instance fence

The tenant-fence change `1a9eb268e1beec847ab197cda4bd46bcfaa56e24` requires a
confirmed runtimeRef to match the configured provider UUID, namespace and service
instance exactly. New fixtures no longer set `TP_RUNTIME_DEVICE_ID_MODE=identity`.
No provider or instance IDs are guessed, and absence of resources is not hidden
by UUID identity fallback.

`configure-runtime-binding --root <fixture>` installs settings only after:

- the pinned backend source includes that reviewed fence;
- the fixture API key is active in the primary database and belongs to the exact
  test tenant/creator/key previously issued;
- connector_runtime contains the actual immutable provider release;
- device_metadata contains at least one actual runtime binding and every binding
  in this single test tenant matches the exact provider/namespace/instance;
- `private/runtime-binding.json` (0600) explicitly records those actual
  `tenant_id`, `provider_id`, `namespace` (`community`) and `service_instance_id`;
- the configured Community destination remains `http://backend:9999`.

It creates no provider or runtime binding. Current fresh SQL bootstrap creates
empty resource tables; publication is a private Encore owner requiring verified
SYS_ADMIN, and no production runtimeRef confirmation writer was found. Therefore
an empty fixture is correctly blocked until an authenticated owner bootstrap is
implemented. Adding that entry point is a separate explicit product contract;
SQL insertion is not a substitute for provider/instance confirmation.

## Explicit system-volume frontend cache clone

When USB random I/O makes fresh installation impractical, the operator may opt in
to a new `~/.codex/local-runtimes/<task>` stage using `--system-frontend-stage`
and `--dependency-seed <verified-existing-node_modules>`. This path never moves or
changes an existing checkout/store. It requires matching root lock SHA, pnpm10.8
isolated layout, matching workspace manifests, same-volume APFS clonefile and
12 GiB free before cloning. clonefile failure does not fall back to a full copy.

The root dependency virtual store **and every workspace-local node_modules** must
be cloned. Root-lock equality alone is insufficient: e.g. workspace axios1.6.7
must not fall back to root axios1.5.1. All root/workspace links must resolve solely
inside the new stage dependency graph or its pinned packages source, generated
.bin absolute paths are rebased, and each declared root/workspace dependency's
actual version must match the existing installed graph. No pnpm install runs
against the shared store. The private stage then runs the normal typecheck/build;
only successful new dist output is copied to the fixture on 80G and recorded in
provenance. Old dist is never copied. The original package cache is trusted by
its matched locked graph; this is not a fresh integrity download of every package.

`--resume-private-frontend-stage` may complete only this task's previously cloned
stage whose provenance SHA, lock SHA and every tracked source file still match the
pinned repository. It adds missing workspace module roots without replacing any
unrecorded dependency directories. This avoids another full root clone and its
APFS metadata cost. Existing build logs are retained with content-hash names
before a subsequent run. A type error from an incomplete workspace dependency
copy must not be attributed to product source until the complete graph is checked.

## Fresh PostgreSQL readiness and first-transaction recovery

PG health uses explicit `pg_isready -h 127.0.0.1`, never its default Unix socket.
The pinned Timescale14 entrypoint starts a temporary server with
`listen_addresses=''`; a same-image isolated tmpfs/no-network regression observed
Unix readiness exit0 while TCP exited2, then final TCP exit0. This prevents schema
bootstrap racing the entrypoint's intentional fast shutdown.

`bootstrap --resume-owned-empty-bootstrap` is a narrow, explicit recovery for
this task's interrupted first transaction. It requires exact project/service,
image ID, working directory, exclusive data/secret bind mounts, loopback port,
nonce, three control relations and their indexes/sequence/column/PK/default/owner
shape, version0/0.0.0, zero Casbin rows, absent five native databases, no ledger or
completion marker and no extra objects/schemas/triggers. Only the image's exact
plpgsql/timescaledb extension objects are allowed. Every mismatch refuses recovery.
No table, row or data directory is dropped/reset/adopted. Earlier logs and failed
fixture remain; guarded recovery starts again from SQL1 after final TCP readiness.

### Workspace portability

Set `TP_LOCAL_WORKSPACE_ROOT` to an existing approved workspace directory (not a disk root). Source checkouts and the new isolated runtime directory must resolve strictly inside it; symlink escape is rejected. Set `TP_DOCKER_STORAGE_VERIFIED` to that resolved directory only after verifying Docker backing storage. Existing free-space floors and the no-adoption rule still apply. The optional `internal-bounded` mode remains specifically for verified OrbStack hosts.

Build architecture defaults to `amd64`; set `TP_LOCAL_TARGET_ARCH=arm64` for ARM hosts/images. Runtime and Encore images must match the chosen architecture. The optional APFS dependency-clone mode and internal-bounded storage verification are macOS-specific; ordinary workspace preparation does not infer or relocate Docker storage. Windows test execution does not verify POSIX 0600 bits; use a private directory with user-only ACLs for actual fixtures.
