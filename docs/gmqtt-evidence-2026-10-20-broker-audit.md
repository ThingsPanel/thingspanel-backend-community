# Broker audit CLI QA evidence

Date: 2026-10-20. The `cmd/tp-broker-audit` CLI is intentionally read-only and tenant-scoped. Its report combines only IDs, status, counts, timestamps, related IDs, route epochs, and a fixed quarantine reason category. It never serializes database payload, metadata, topic/body, command parameters, raw reason, action detail, or credentials. Every query runs in a PostgreSQL read-only transaction with five-second context/statement/lock limits.

Validation:

```text
go test -race ./cmd/tp-broker-audit -count=1
PASS (including isolated PostgreSQL QA schema)
```

The PostgreSQL test creates a random private schema, applies migrations 001–009, seeds synthetic rows for each returned kind plus a second tenant, then invokes the CLI path through multiple cursor pages. It verifies all six record kinds, no duplicate page entries, tenant isolation, and absence of synthetic payload/token/parameter/raw-reason strings from the JSON. A PostgreSQL write attempted within a read-only transaction fails. The test also verifies an incomplete private schema returns the fixed exit-78 message. The schema is dropped after the test; QA DSN content is not printed.

This validates CLI behavior against the isolated local QA database. It does not certify production database grants or schema contents. Deploy with a SELECT-only role and controlled migrations; see [BROKER-AUDIT-RUNBOOK.md](../deploy/mosquitto-community/BROKER-AUDIT-RUNBOOK.md).
