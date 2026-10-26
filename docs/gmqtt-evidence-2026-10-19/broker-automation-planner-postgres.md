# Broker automation planner PostgreSQL regression

`internal/service/broker_automation_postgres_test.go` runs each case against a real isolated PostgreSQL database and a per-test private schema. It verifies:

- Matching telemetry, attribute, and event evidence can atomically create a durable command request, command log, and accepted action receipt; the ingress effect then becomes completed.
- Re-running a completed effect and racing two planner workers does not duplicate the command, log, or action receipt.
- A changed/frozen route, receipt/trigger epoch mismatch, or trigger older than the two-minute execution window fences the effect without creating a command.
- Cross-tenant forged scene/condition records do not cross the effect tenant boundary. A command proof with a foreign tenant is rejected.
- Changing or disabling the scene policy fences a previously minted automation command proof.
- Unsupported action types produce an `unsupported` audit receipt and no durable command.
- A failure while inserting the action receipt rolls back planned command/log rows and increments retry state. Retries stop after five attempts; when the injected failure is removed before that bound, retry commits once.

Validation used the isolated QA PostgreSQL DSN from `/tmp/tp-mqtt-20261019/qa-dsn` without printing it:

```sh
TEST_BROKER_DATABASE_DSN="$(cat /tmp/tp-mqtt-20261019/qa-dsn)" go test -race -count=1 -run 'TestPostgresAutomation' ./internal/service
```

Result: PASS against private schemas. Only the new regression test file and this evidence note were added; production implementation was not changed.
