# Controlled broker route cutover CLI

`tp-broker-cutover` is a thin operator wrapper around `brokercontrol.MigrationStore`. It accepts four reviewed plan operations (`freeze`, `prepare`, `activate`, `rebase`) and a read-only `members` query. It has no operation for runtime registration, barrier ACK, forced advancement, or direct SQL changes. Database access is the operator authorization boundary; `--actor` is recorded by the existing freeze transition.

Build from the community repository with `go build -o tp-broker-cutover ./cmd/tp-broker-cutover`. Supply the operator database DSN through `TP_BROKER_CUTOVER_DSN`, or use `--dsn-file` with a regular file whose mode is exactly `0600` and size is at most 8 KiB. The command limits each invocation to five seconds, sets a two-second database lock timeout and a two-second statement timeout, and uses at most two database connections. It never prints a DSN or underlying database error. `members` reads member records and digest but obtains the existing transaction-scoped advisory lock; it is data-read-only, not compatible with a database role that is forbidden from taking advisory locks.

First inspect the source roster and use its exact digest in a reviewed freeze plan:

```sh
tp-broker-cutover members --cell CELL_A
cat > freeze-plan.json <<'JSON'
{
  "version": "1",
  "operation": "freeze",
  "tenant_id": "TENANT_ID",
  "device_id": "DEVICE_ID",
  "change_id": "00000000-0000-4000-8000-000000000001",
  "expected_cell": "CELL_A",
  "target_cell": "CELL_B",
  "expected_epoch": 7,
  "members_sha256": "COPY_THE_FRESH_MEMBERS_DIGEST_HERE"
}
JSON
shasum -a 256 freeze-plan.json
tp-broker-cutover apply --plan-file freeze-plan.json --sha256 REVIEWED_FILE_SHA256 --actor OPERATOR_ID
```

The command hashes the original plan bytes; changing whitespace or any field requires reviewing and hashing the file again. Freeze is idempotent only for the same change ID and parameters. Only the freeze transition persists the actor and raw plan hash in the owner change record/event. Prepare, activate, and rebase return the reviewed hash in CLI output but the current owner schema does not persist that invocation's actor/hash; do not describe those stages as fully audited plan execution. Retain the exact plan file and a protected `0600` operator execution log for each stage. Full operational audit certification remains pending owner audit support.

Runtime participants must synchronize and acknowledge through the existing Owner protocol. Once all real acknowledgements and fresh target membership are present, a reviewed `prepare` plan can call `PrepareTarget`. After the target has a real fresh connection and accepted trusted ingress in the new epoch, a reviewed `activate` plan can call `Activate`. The CLI cannot create either proof. `rebase` explicitly replaces the barrier manifest and clears all prior acknowledgements; it requires reviewed old/new member digests and a reason SHA.

Example prepare plan:

```json
{"version":"1","operation":"prepare","tenant_id":"TENANT_ID","device_id":"DEVICE_ID","change_id":"00000000-0000-4000-8000-000000000001","target_ready_sha256":"64_LOWERCASE_HEX_CHARACTERS"}
```

Example activate plan:

```json
{"version":"1","operation":"activate","tenant_id":"TENANT_ID","device_id":"DEVICE_ID","change_id":"00000000-0000-4000-8000-000000000001"}
```

Before use, review `tp-broker-route get --tenant TENANT_ID --change-id CHANGE_ID` and the corresponding `events` output. A failed barrier stays paused; do not imitate a runtime ACK or advance it with SQL. The CLI does not provide the operator write workflow for membership retirement, and a two-cell gray rollout/rollback has not yet been demonstrated end to end. A successful command result confirms only that the requested database transition committed.
