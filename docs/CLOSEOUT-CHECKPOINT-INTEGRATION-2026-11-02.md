# Closeout checkpoint integration (2026-11-02)

Source checkpoint: `6936f205b0be0f6802d3a7a28b21d79d334b5a96`; integrated on `7370b49ad5a7948539ec299dd8009007640f0e29` by cherry-pick with provenance. This is a development integration, not release acceptance. No existing user database is migrated during this work.

## Scheduler ownership

The existing external `tp-automation-tick.timer` remains the default. `GOTP_AUTOMATION_SCHEDULER` unset or `external` registers no process scheduler. Only explicit `process` enables it, and only with `GOTP_AUTOMATION_MODE=native`. Invalid source values fail startup. Before selecting `process`, disable external tick sources and select one Community instance. The process is sequential internally; multiple processes are not a proven singleton. Legacy mode retains legacy cron; native mode never registers legacy automation jobs.

## Required migrations and upgrade effects

Community `VERSION_NUMBER` remains 25. After historical base migrations, `PgInit` invokes component migrations independently of `sys_version`:

- Device identity: embedded `sql/31.sql`, module `device-identity`, advisory transaction lock and `devices` SHARE ROW EXCLUSIVE lock; backfills existing IDs, then installs the INSERT/DELETE/identity UPDATE fence atomically. It reserves current IDs only; it cannot reconstruct IDs deleted before cutover. Startup verifies reservation ownership, column/constraint and exact trigger/function state. Conflicts or drift stop startup. A large existing device table can delay startup and block device mutations. No existing database was used for this validation.
- Market installation: embedded `sql/30.sql` plus `sql/32.sql`, module `market-install`; authorization and config receipt tables belong to Community. Credentials are not stored in those rows. This migration is independent of the selected legacy/Encore install owner.
- Encore A08: `marketinstall/migrations/0001_installations.up.sql` and `0002_request_metadata.up.sql` belong to the separate logical `market_install` database. Store reads require migration 0002. Never apply these to Community or `device_metadata`.
- Existing native binding component (`sql/27.sql`) and Auth/AppPush components remain required. `PgConnect` now only connects; `PgInit` applies component migrations after base tables exist. Standalone callers of `PgConnect` needing components must initialize them explicitly.

The test deployment has explicit `TP_DEPLOY_MARKETINSTALL=true` opt-in to include the service and its migration resource. Default deployment service/resource lists remain unchanged. Opt-in requires pre-provisioned `market_install` runtime mapping and existing deployment/schema ledgers, which are checked before stopping services. This integration does not provision or migrate an existing database, and does not enable market-install traffic.

## Community devices writer inventory

- `internal/dal/devices.go`: all production creation uses `createDevicesWithDefaultRootGroupTx`, which reserves IDs before `tx.Device.Create` in the same transaction. Callers include public create/batch, service auto-registration, child auto-registration and device authentication auto-registration.
- Public deletion uses service-wide dependency cleanup and `DeleteDeviceWithTx`, which tombstones before physical delete in that transaction. The older exported `dal.DeleteDevice` now also locks and tombstones in the same transaction; it is not a replacement for service dependency cleanup.
- Binding/status/config updates do not mutate ID or tenant; the database fence rejects any identity mutation. SQL/plugins bypassing the DAL are deliberately rejected unless using the reservation contract. Migration cutover waits for prior writers and backfills their committed devices.
- `internal/dal/app_push_devices.go` saves `AppPushDevice`, not `model.Device`.
- Generated `internal/query/devices.gen.go` is a primitive, not an independently authorized writer; new callers must use the DAL transaction. Test fixtures explicitly seed reservations, whereas fence tests use the actual SQL trigger.
- Encore `devicemetadata` writes its separate `device_metadata` database and is not covered by this Community reservation table. Sharing these tables without reconciling ownership is unsupported. Runtime synchronization into Community still requires end-to-end acceptance.

## Functional batch boundaries

1. Scheduler: tick transport, opt-in worker, mode-gated legacy cron, lifecycle and authenticated OwnerTick tests.
2. Market install: live-session scoped authorization, immutable plan, A08 provider and ledger metadata, native model create/publish/readback, config receipt and ThingsVis receipt verification. Default `TP_MARKETINSTALL_OWNER` remains legacy. Encore requires Redis plus dedicated provider/native/ThingsVis credentials, trusted ThingsVis identity mapping and matching endpoint protocols.
3. Device identity and latest telemetry: permanent ID create/delete fence plus tenant current-data query; latest telemetry requires active reservations and only supports local current storage (`grpc.tptodb_type` empty/NONE). External TSDB profiles continue to return unavailable.

## Validation

Verified in dedicated instances only, never against an existing user database:

- PostgreSQL 18.4 (Homebrew, new cluster) and PostgreSQL 14.5 (new container from cached `registry.cn-hangzhou.aliyuncs.com/thingspanel/timescaledb:14`, separate data directory and port): `TestDeviceIdentitySchemaPostgres` and `TestMarketInstallSchemaPostgres`, all nine subcases pass without skips on both versions. This covers backfill, replay/tombstones, concurrent startup, exact fence/ledger drift rejection, rollback and preservation of existing fixture data.
- Both PG versions plus a fresh Redis instance: `TestDevicePermanentIdentityPostgres`, `TestDeviceIdentityDatabaseWriterFencePostgres`, `TestTenantLatestTelemetryOwnerPostgres`, `TestMarketInstallAuthorizationPostgresRedis`, `TestMarketInstallConfigReceiptPostgres`, `TestInstallationWireHashVector` all pass without skips. Illegal writers must fail specifically with SQLSTATE 23514. The original fence fixture omitted required `is_online`; it was repaired without weakening the product schema. Reusing the compiled service test requires working directory `internal/service` because existing fixtures load relative SQL files.
- `go test ./internal/automationbridge -count=1` passes; `TestNativeAutomationSchedulerLifecycle` passes for default external/no worker, explicit process, legacy and invalid source, with retry/cancel/log containment checks.
- In `encore-api`, `go test ./marketinstall/app ./marketinstall/store -count=1` passes (store has no tests; this is compile coverage only). Both owners pass the fixed wire hash/key vector, including an integer above 2^53. This does not exercise A08 PostgreSQL store transactions.
- Python `test_encore_migrations.py`: six planning/checksum/dirty/downgrade tests pass. Go formatting, Python syntax and `git diff --check` pass.

QA data/logs and the reusable service test executable are under `/Volumes/80G/.tp-closeout-qa-g7p0llmb`; `TMPDIR` and `GOTMPDIR` both pointed to its `tmp` directory, with the prescribed 80G module/build caches. No files on 500G were read or written for the integration. All three newly created test instances were cleaned up after validation: the PG18 and Redis processes stopped and the PG14 container was removed; QA logs and new fixture data were retained. Test databases contained ordinary fixture tables and plpgsql, not enabled Timescale hypertables: PG14 compatibility is verified, Timescale-specific storage behavior is not.

Still unaccepted: full market bundle → actual Encore thingmodel endpoint → config → ThingsVis and real device effects; A08 PostgreSQL store and actual OwnerTick integration; whole Encore application build/code generation and release/deployment. The local `encore check` command starts infrastructure and the application, so it was not invoked against existing local runtime state. External TSDB history, pre-cutover deleted IDs, runtime synchronization into Community, multi-source/multi-instance scheduler concurrency, and full empty-database application boot remain outside these results. Sequential or controlled-owner tests do not prove those paths.
