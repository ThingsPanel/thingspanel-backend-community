# Community runtime tenant scope

The native Community adapter must call `/api/v1/runtime-scoped/:tenantID/<suffix>` for runtime capabilities. Login continues using its existing authentication owner. There is no fallback to the legacy capability URL.

The path tenant must be nonempty and exactly equal to the validated session/API-key tenant. Only a validated `TENANT_ADMIN` session or the existing API-key authentication contract (which produces that authority) can call these internal capabilities. Same-tenant `TENANT_USER` and `SYS_ADMIN` sessions are rejected; a platform administrator must obtain the appropriate tenant administrator session. Legacy Casbin policy absence is never proof of permission. An explicit allowlist is registered in `router/runtime_scoped.go`; existing public routes retain their original registration.

## Ownership and concurrency

Scoped read SQL, TSDB gRPC, and metadata reads carry the bounded request context; the dedicated fence connection is acquired with that deadline. The fence transaction stays alive until the callback exits, so canceling a request cannot release its authorization lock while the callback is still running. Canceled callbacks cannot return data. Debug Redis calls use the configured client network timeouts and bounded result limits.

Read/debug handlers hold PostgreSQL SHARE locks on the target device and its active `device_id_reservations` row until the handler finishes. Device tenant, `is_enabled=enabled`, `activate_flag=active`, and reservation lifecycle must match. Command receipt ownership is resolved from the server-side durable request or legacy command log; caller-provided device IDs cannot authorize a receipt.

Mutation owners use their own transaction, never a separate gateway SHARE lock that they would need to upgrade:

- Device update reuses the existing configuration/parent ancestry validator and tenant-qualified update, with a runtime-only device UPDATE lock and active reservation check. The legacy `id=EMPTY` number lookup is rejected for this internal capability.
- Device delete reuses `LockTenantDeviceForDelete` and tenant-qualified dependent cleanup/tombstone in the same transaction.
- Voucher rotation locks the tenant device and active reservation before uniqueness checking and updating that same tenant row.
- Subdevice removal locks the tenant child and verifies the existing parent belongs to that tenant before clearing the binding. Only the authorized frozen parent can be notified.
- Expected-data delete resolves its device on the server, then locks the tenant device and rechecks record ID, tenant, device association, and pending state under UPDATE lock before deletion.
- Command publication is gated with 503 until its durable owner atomically verifies active device reservation and activation lifecycle as well as tenant/cell/epoch. The scoped API wrapper is prepared, but an unlocked precheck is not sufficient to enable it. Expected-data creation locks the active device and reservation in its own admission transaction and requires `mqtt.input_profile=ingress_v1`; legacy publication does not establish an atomic fence.

Duplicate identity query values, mixed path/query identifiers, duplicate JSON keys, case aliases, and conflicting body identifiers fail before the capability handler. JSON bodies are limited to 1 MiB and restored for ordinary binding.

## Simulation boundary

`POST telemetry/datas/simulation/send` currently responds 503. The legacy helper accepts caller-selected broker/port/topic, uses device credentials, logs credentials at debug level, and does not carry a tenant/route-epoch proof through later ingestion. A read-time device check is insufficient to authorize the eventual asynchronous write.

A real replacement requires an independent server-issued simulation actor/proof. The HTTP owner must record trusted tenant/device/caller version/current route cell and epoch/payload digest/expiry/nonce, forbid transport overrides, derive a fixed internal topic, and authorize only an active tenant device. Broker authorization and `mqttadapter.HandleIngressMessage` need a dedicated simulation path: verify the actor/proof, never treat JSON as identity, and atomically consume the one-time proof with `brokerledger.Store.Accept`. Existing ledger acceptance and worker route-epoch checks remain required. Plugin context cannot be reused: it represents an actual authenticated socket connection. This capability must remain disabled until the broker owner, adapter, and proof acceptance contract are deployed together.

## Verification

Unit regressions cover tenant/SYS_ADMIN rejection, identity confusion, restored bodies, and explicit simulation refusal. PostgreSQL regressions use only fresh disposable schemas from `TEST_BROKER_DATABASE_DSN`, covering target lifecycle, receipt ownership, writer success/foreign rejection, and lock contention against concurrent tenant transfer. They never modify the configured application database. A skipped PostgreSQL test does not establish concurrent safety; run the isolated suite before acceptance.

## Export follow-up

The native runtime page reader does not request Excel export. The scoped page capability explicitly refuses `export_excel=true`; the existing public export API is unchanged. A complete scoped export needs a separate bounded job owner: immutable tenant/device/caller/epoch scope, tenant-qualified job/artifact IDs, hard row/byte/time limits, finite SQL/TSDB pages with request/job cancellation, streaming output with context checks, temporary output followed by atomic publish only after scope/job-state revalidation, explicit pending/running/cancelled/failed/completed lifecycle and retention cleanup. It must release row locks between job pages and reacquire/revalidate the same immutable lifecycle/epoch for each page; a device change or revoked session fails the job and discards unpublished output. The download owner must check tenant and completed job state. This job/worker/artifact lifecycle is not implemented by the scoped page route and remains a separate capability.

## Tenant identity integration gate

The Community authentication claim and device/reservation tenant IDs can be raw 8-hex identifiers, while native device metadata/outbox tenants are normalized UUIDs. The historical native normalization strips/pads characters and is not globally collision-free. The gateway deliberately requires exact raw tenant equality and uses that authenticated raw tenant for every Community database predicate. A normalized native UUID is not currently accepted as an alias. Consequently a native client that supplies only the normalized UUID cannot use this gateway for a short-ID tenant yet; it fails closed.

Enabling that combination requires a trusted, unique raw/native mapping plus preserved raw provenance for foreground and outbox calls, or a collision-safe authoritative identity migration covering all tenant creation and runtime binding owners. A normalization equality test or an unlocked enumeration of existing users is not sufficient to establish that invariant during concurrent tenant creation. This gap remains open and must be resolved before claiming end-to-end native runtime acceptance.

## Current validation status

All changes are saved on `codex/community-runtime-scoped-gateway`. Formatting/diff checks completed. Go compilation, unit regressions, and disposable PostgreSQL regressions have not run in this change session: builds were queued behind another agent and then prohibited when the system disk fell below the agreed stop threshold. These sources remain unaccepted candidates. The command owner lifecycle gate, raw/native tenant identity mapping, safe simulation actor path, and bounded export job lifecycle are explicit remaining integration work.
