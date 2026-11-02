# Community runtime tenant scope

The native Community adapter must call `/api/v1/runtime-scoped/:tenantID/<suffix>` for runtime capabilities. Login continues using its existing authentication owner. There is no fallback to the legacy capability URL.

The path tenant must be nonempty and exactly equal to the validated session/API-key tenant. A `SYS_ADMIN` claim does not bypass this rule. An explicit allowlist is registered in `router/runtime_scoped.go`; existing public routes retain their original registration.

## Ownership and concurrency

Read/debug handlers hold PostgreSQL SHARE locks on the target device and its active `device_id_reservations` row until the handler finishes. Device tenant, `is_enabled=enabled`, `activate_flag=active`, and reservation lifecycle must match. Command receipt ownership is resolved from the server-side durable request or legacy command log; caller-provided device IDs cannot authorize a receipt.

Mutation owners use their own transaction, never a separate gateway SHARE lock that they would need to upgrade:

- Device update reuses the existing configuration/parent ancestry validator and tenant-qualified update, with a runtime-only device UPDATE lock and active reservation check. The legacy `id=EMPTY` number lookup is rejected for this internal capability.
- Device delete reuses `LockTenantDeviceForDelete` and tenant-qualified dependent cleanup/tombstone in the same transaction.
- Voucher rotation locks the tenant device and active reservation before uniqueness checking and updating that same tenant row.
- Subdevice removal locks the tenant child and verifies the existing parent belongs to that tenant before clearing the binding. Only the authorized frozen parent can be notified.
- Expected-data delete resolves its device on the server, then locks the tenant device and rechecks record ID, tenant, device association, and pending state under UPDATE lock before deletion.
- Command publication and expected-data creation require `mqtt.input_profile=ingress_v1`; legacy publication does not establish an atomic fence. Their existing durable owner remains authoritative.

Duplicate identity query values, mixed path/query identifiers, duplicate JSON keys, case aliases, and conflicting body identifiers fail before the capability handler. JSON bodies are limited to 1 MiB and restored for ordinary binding.

## Simulation boundary

`POST telemetry/datas/simulation/send` currently responds 503. The legacy helper accepts caller-selected broker/port/topic, uses device credentials, logs credentials at debug level, and does not carry a tenant/route-epoch proof through later ingestion. A read-time device check is insufficient to authorize the eventual asynchronous write.

A real replacement requires an independent server-issued simulation actor/proof. The HTTP owner must record trusted tenant/device/caller version/current route cell and epoch/payload digest/expiry/nonce, forbid transport overrides, derive a fixed internal topic, and authorize only an active tenant device. Broker authorization and `mqttadapter.HandleIngressMessage` need a dedicated simulation path: verify the actor/proof, never treat JSON as identity, and atomically consume the one-time proof with `brokerledger.Store.Accept`. Existing ledger acceptance and worker route-epoch checks remain required. Plugin context cannot be reused: it represents an actual authenticated socket connection. This capability must remain disabled until the broker owner, adapter, and proof acceptance contract are deployed together.

## Verification

Unit regressions cover tenant/SYS_ADMIN rejection, identity confusion, restored bodies, and explicit simulation refusal. PostgreSQL regressions use only fresh disposable schemas from `TEST_BROKER_DATABASE_DSN`, covering target lifecycle, receipt ownership, writer success/foreign rejection, and lock contention against concurrent tenant transfer. They never modify the configured application database. A skipped PostgreSQL test does not establish concurrent safety; run the isolated suite before acceptance.
