# Runtime tenant source owner — unaccepted candidate

## Current decision

`codex/runtime-tenant-source-binding` contains the gateway candidate and native tenant-fence dependency. No migration has been executed by this work. No Go/Encore compilation or PostgreSQL regression has run in this session.

**Production source registration is paused and returns `ErrRegistrationPaused`.** Two actual live administrators can prove the Community source identity. They do not by themselves prove ownership of an existing native UUID's records, nor that every native writer respects that source after a concurrent registration. `reviewReference` and its digest are audit input, not evidence replacing either invariant. No flag or environment variable bypasses the registration pause.

The wide DB writer-guard attempt was withdrawn from active Go/SQL. It is preserved only as [an unaccepted experimental patch](experiments/runtime-source-writer-guard-unaccepted.patch), based on `a9d5a5bd`; it must not be applied or deployed as a safety fix. Active migration 0040 does not install those cross-table triggers or change common SQL transaction helpers.

## Implemented candidate boundaries

- `internal/runtimetenant`: immutable one-to-one native UUID/source identity record, exact deployment/provider/namespace/instance/origin, terminal revocation, append-only audit and unique constraints. Normalization is consistency checking only.
- Local `cmd/runtime-tenant-source-provision`: two independent actual live sessions, direct platform administrator without tenant plus tenant administrator; delegated sessions denied. Native authentication uses `authcore.Check`; legacy uses the existing signed-token/live-session AuthHandler. Both additionally verify the same identity at the actual configured Community source's session-context endpoint. No public registration API, caller-supplied claims, persisted session tokens, or automatic registration exists. Provisioning remains paused after these checks.
- Current HTTP runtime reads/controls require exact signed raw provenance matching the reviewed record. The actual cached Community client's origin must also match. Community receives the raw tenant path; its existing strict token/raw/device/reservation fence is unchanged.
- New device runtime outbox intents require a binding in the enqueue transaction and freeze its ID/generation under a tenant-qualified FK. Dispatch rechecks active binding, frozen proof, deployment tuple and client origin. Historical rows lacking proof are skipped and never backfilled.
- A single existing global runtime credential still serves only its actual Community tenant. A mapping does not grant that credential access to another tenant. Multiple tenant credentials need a separately reviewed credential owner.

These consumers fail closed without an approved record. Disposable schema tests may insert explicitly isolated fixture records to test FK/immutability; that is **not** successful production registration or end-to-end acceptance.

## Why the empty-tenant race is not yet certified

A `SELECT count(*)` and mapping UNIQUE index do not prevent a request from caching a colliding canonical ID before registration and writing later. Sorting table SHARE locks can prove an empty snapshot at registration and wait out existing RowExclusive writers, but cannot alone establish all later writers' raw provenance. A correct unique owner must coordinate both registration and every affected transaction/DB edge, including old snapshots and background workers.

The withdrawn experiment attempted permanent proven/unproven guards plus BEFORE-write triggers. Static review already found broad compatibility concerns: nullable global `meta_item_keys` rows, background jobs with no authenticated user, direct store/migration writes, and future tenant-bearing tables. It has no PostgreSQL proof and is not an accepted solution. The original SQL helpers and business write contracts have been retained.

## Static per-table impact inventory

This inventory is from migration source, not a verified running database. `device_templates` is renamed to `device_access_templates` by migration 0023. All rows, including soft-deleted rows and old intents, matter to an empty-tenant proof.

| Tables | Existing owner / unresolved scope |
|---|---|
| `thing_models`, `thing_model_items`, `thing_model_snapshots` | Generic `store.go` and model owners; not all calls pass through native metadata transaction helpers. |
| `device_access_templates`, `device_access_template_revisions` | Access template/native template owners; includes historical revisions. |
| `products`, `devices` | Generic store plus native device owners. A cached normalized principal must be rechecked at the actual transaction boundary. |
| `meta_item_keys` | **Nullable tenant ID**, including global/built-in rows. A blanket non-null tenant guard breaks valid writes. |
| `thing_model_audit_log`, `audit_log` | Audit producers; `audit_log` already has RLS. An empty scan must not interpret RLS-hidden rows as absent. |
| `migration_log` | `migration_service.go` begins its own SQL transactions for migrate/rollback; cannot infer a verified raw tenant from a normalized UUID alone. |
| `custom_command_controls` | Existing controls/store owner; broad guards would change its transaction contract. |
| `thing_model_command_receipts`, `thing_model_outbox`, `thing_model_resources` | Thing-model delivery/receipt/resource owners, separate from device runtime outbox provenance. |
| `device_groups`, `device_group_members` | `beginTenantGroupTx`; read-only snapshot and write provenance both need explicit certification. |
| `metadata_idempotency`, `metadata_audit_events` | `runMetadataWriteTx`; replay paths and error/rollback paths also write these tables. |
| `metadata_operations`, `metadata_operation_items`, `metadata_artifacts` | Shared operation/artifact lifecycle, including async exports; not only device runtime changes. |
| `device_access_bindings`, `gateway_memberships` | Runtime/provider binding and topology owners; exact namespace/provider/instance must remain authoritative. |
| `device_credentials` | Credential write/receipt paths; secret ownership/AAD and native tenant source proof are separate checks. |
| `device_runtime_outbox` | Enqueue proof exists in the candidate; dispatcher and receipt completion need matched transaction-owner certification before a global write guard is enabled. |
| `device_command_executions` | Foreground command acceptance/status/cancellation. Async or late status writes must preserve original authority. |
| `device_simulator_profiles`, `device_simulation_sessions`, `device_simulation_runs` | Simulator lifecycle. Safe actual simulation actor/ingest proof is a separate unresolved capability. |
| `metadata_downloads`, `metadata_export_chunks` | Export artifact creation/chunks/retention; background authority cannot be invented from a tenant UUID. |

New `runtime_tenant_sources` and `runtime_tenant_source_audit` belong to the operator source owner. They must not be accidentally included in a generic business-owner auto-registration loop.

## System/background and migration paths

- `native_runtime_dispatch.go`: private device outbox cron has no current user. Frozen event FK/generation is the evidence available to its runtime call. Historical intents remain paused.
- `native_reading_export_async.go`: `claimAsyncExport`, fail/backoff/release, slice save and finish use direct DB transactions/updates. Current operation selection/checkpoint does **not** freeze the reviewed source binding. `communityHistorySource.Page` therefore refuses a background call without current signed provenance; async export functionality is not complete under the new source gate.
- `native_reading_export.go`: expired-download purge performs a global direct DELETE. A blanket per-user source trigger would break retention without a separate trusted maintenance owner.
- `migration_service.go`: migrate template, migrate topic, rollback and batch migrate use their own DB transactions. Neither a review-reference string nor a normalized tenant is proof authorizing source reassignment.
- `store.go`: legacy store methods include direct SQL writes; common native v2 transaction helpers alone do not cover this surface.
- Schema migrations and fixtures also write tenant-bearing data. Future schema changes must join any eventual source-ownership invariant; a catalog scan today is not proof about tomorrow's tables.

## Required acceptance sequence after the I/O window

1. Compile the combined gateway/native-fence/source candidate using caches and temporary directories on the approved 80G volume, with no writes to 500G. Run focused authority, ambiguous-manifest, signed-session and exact-raw negative tests. Current registration must remain closed.
2. Run isolated source-schema PostgreSQL tests under `TEST_RUNTIME_TENANT_DATABASE_DSN`, and gateway PostgreSQL tests under `TEST_BROKER_DATABASE_DSN`, only in fresh disposable schemas. Unique collision, revocation, immutable proof and append-only audit tests establish those schema properties, not full owner safety.
3. Separately design/certify the empty-tenant owner against every table/entry above: absent row, soft-deleted row, RLS-hidden row, nullable global row, direct writer, stale snapshot, first writer/registrar race, colliding raw tenants, cancellation, lock timeout and commit-unknown cases. Register only if every relevant path is proven. No automatic adoption or proof backfill.
4. Only after that owner is accepted: create actual Community tenants through the real owner, obtain real current operator/tenant sessions, provision an empty native tenant, then create/import correctly scoped native records. Exercise native runtime reads and supported mutations through the real raw-scoped gateway, comparing both native receipts and actual Community effects. Wrong raw/API key/provider/instance/environment, revoked source, mismatched origin and legacy outbox must cause zero foreign side effects.
5. Existing native tenants need a distinct reviewed migration that establishes resource-level historical ownership. Actual-source runtimeRef/device ownership evidence, immutable inventory snapshots, unknown/collision rejection and an explicit operator decision are needed; no such migration is implemented here.

The command lifecycle gate, safe simulation actor, async export proof and historical source migration remain separate open delivery gates. No mocked or manually seeded source record should be presented as real operator onboarding acceptance.
