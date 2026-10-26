# Read-only broker audit CLI

Build from the repository root:

```sh
go build -o ./tp-broker-audit ./cmd/tp-broker-audit
```

The command accepts an explicit tenant and returns a bounded JSON page across ingress receipts, durable commands, effects, quarantine records, connection observations, and automation actions. It uses a read-only PostgreSQL transaction, sets a five-second statement/lock timeout, checks required schema columns, and never writes or starts a background worker.

Provision the DB DSN as a mounted secret readable by the operator running the CLI (for example, mode `0600`), or inject `TP_BROKER_AUDIT_DSN` through the deployment secret manager. Use the secret-file form so shell history and process command arguments do not contain the DSN:

```sh
./tp-broker-audit --dsn-file /run/secrets/broker_audit_dsn --tenant TENANT_ID --limit 50
```

Follow the returned opaque cursor for the next page:

```sh
./tp-broker-audit --dsn-file /run/secrets/broker_audit_dsn --tenant TENANT_ID --limit 50 --cursor 'CURSOR_FROM_PREVIOUS_JSON'
```

`--tenant` is mandatory. `--limit` must be 1–100 (default 50). The cursor encodes `(created_at, kind-prefixed id)` and should be passed back unchanged. The output contains record kind, scoped IDs, status, a count, time, related ID, route epoch and a fixed quarantine reason category. It omits voucher/token values, raw topics/bodies/payloads, metadata, command parameters, arbitrary last errors and action detail. Treat the IDs themselves as operational data and protect the output accordingly.

A missing required table or column returns the fixed message `broker audit schema is incomplete` and exit code 78. The command does not migrate schemas; install the numbered migrations through the normal controlled migration process. Connection errors, query errors and timeouts use fixed messages so driver errors cannot echo DSNs or SQL arguments. Exit codes: 0 success, 64 arguments/DSN setup, 69 connection, 75 timeout, 78 incomplete schema, 1 query or output failure.

Use a least-privilege PostgreSQL role with SELECT access to the broker audit tables and metadata. The read-only transaction is enforced by PostgreSQL as an additional safeguard; avoid granting this CLI role any write privileges.
