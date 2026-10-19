# Trusted telemetry final-batch fence: PostgreSQL evidence

Date: 2026-10-20. These tests cover the final durable boundary for trusted telemetry batches in `internal/storage`; ordinary telemetry remains best-effort before this database commit. The fixture creates a random PostgreSQL schema, applies the checked-in broker migrations through 010, creates minimal legacy telemetry/device tables, and drops the schema on exit. The QA DSN is read from `/tmp/tp-mqtt-20261019/qa-dsn` or `TP_TELEMETRY_BROKER_TEST_DSN` and is never printed.

The regression suite verifies:

- A valid gateway/child sample commits telemetry history and current values, a processed `telemetry_sample` receipt, TEL automation effect, `broker_telemetry_activity`, and the projection queue.
- Old route epochs, non-active binding phase, wrong tenant binding, and a child whose current parent no longer matches the authenticated transport device are rejected without business rows or receipt/effect/activity/queue writes.
- An injected automation-effect insert error rolls back history, current value, receipt, effect, activity, and projection queue in the same transaction.
- Replaying a receipt does not duplicate its effect; an older sample remains in history but cannot replace the newer current value or activity snapshot.
- A mixed legacy/trusted batch still writes through the independent legacy path while trusted items create only their own receipt/effect path.
- A controlled GORM query barrier pauses the trusted transaction after it locks the final transport binding. A concurrent route-epoch update remains blocked until the telemetry transaction commits, then proceeds.

Validation:

```text
go test -race -count=1 ./internal/storage -run '^(TestTrustedTelemetry|TestTrustedAndLegacy)' -v
PASS (6 tests; real isolated PostgreSQL; approximately 3.56s)
```

This covers the storage transaction and route fence under the fixture schema. It does not prove the earlier MQTT/bus acknowledgement is durable: the application currently acknowledges ordinary telemetry before this final batch commit. It also does not certify production schema deployment, full workload capacity, or physical/automation action execution.
