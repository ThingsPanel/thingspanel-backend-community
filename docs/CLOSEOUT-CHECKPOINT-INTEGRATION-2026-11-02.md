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

Pending isolated verification. This file will record exact results and remaining gaps before delivery. Full market bundle → real thingmodel → config → ThingsVis and real device side effects remain unaccepted until exercised with their actual owners. Sequential/mock tests do not prove multi-source scheduler concurrency or production deployment.
