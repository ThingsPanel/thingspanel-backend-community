# Command context PostgreSQL regression — 2026-10-20

This isolated PostgreSQL regression exercises the read-only state fingerprint used when preparing a durable command. Each test uses a private random schema on the QA database; it does not modify the shared `public` schema or contact a broker/device.

`CommandContext` requires the caller's transaction to hold the current transport binding `FOR UPDATE`. The tests verify a stable 32-byte fingerprint for unchanged fresh-online evidence, a changed fingerprint after a new telemetry receipt or connection tombstone, and rejection of expired telemetry, stale route epochs, draining routes, cross-tenant lookups, reparented children, and children without their own heartbeat. Repeated fingerprint reads leave `next_check_at`, receipts, status history, and effects unchanged. A service-level PostgreSQL test covers enabled/active logical-device ancestry and rejects a disabled or inactive logical device/root/ancestor.

The state evidence suite also checks closed-connection freshness: a recent accepted close can establish offline, while old, future-dated, or stale-received closes remain unknown; a stale open alongside a fresh close also remains unknown. `InspectCurrent` leaves schedules, receipts, histories, and effects unchanged. The gateway-child projection fixture includes an actual active root device, as required by root-path validation.

The focused state and related command-path tests passed:

```text
TEST_BROKER_DATABASE_DSN=<isolated QA DSN> go test -race -p 1 ./internal/brokerstate -count=1
ok project/internal/brokerstate
TEST_BROKER_DATABASE_DSN=<isolated QA DSN> go test -race -p 1 ./internal/service -run 'TestReadCommandFingerprint' -count=1
ok project/internal/service
go test -race -p 1 ./internal/api -run 'TestCommandPlan' -count=1
ok project/internal/api
TEST_BROKER_DATABASE_DSN=<isolated QA DSN> go test -race -p 1 ./internal/brokercommands -run 'TestPostgresPlanCapacity' -count=1
ok project/internal/brokercommands
```

The prior gateway-child projection failure was caused by a test fixture that created a transport binding and child but omitted the active root gateway device. The fixture now creates that root device; the full brokerstate race suite passes with the production root-validation rule unchanged.

The typed `brokersemantic.ValidateCommand` is available for the exercised supported BOOL command schema. Other semantic forms remain subject to its explicit fail-closed subset; these results do not establish support for every published command schema.

These are database-level consistency tests only. They do not validate HTTP authorization, command publication, broker receipt, or physical device execution.
