# Broker state time-boundary regression

The real PostgreSQL regression in `internal/brokerstate/state_test.go` verifies both freshness cutoffs used by `Snapshot`:

- An online observation whose `observed_at` is 10 seconds in the future is `unknown`.
- An online observation whose `received_at` is 61 seconds old is `unknown`.

For each case, the test begins with the legacy device row marked online and confirms `Project` leaves it online, reports no change, and creates no status-history row. This prevents stale or implausibly future data from being interpreted as an offline transition.

Validation used the isolated QA PostgreSQL DSN from `/tmp/tp-mqtt-20261019/qa-dsn` without printing it:

```sh
TEST_BROKER_DATABASE_DSN="$(cat /tmp/tp-mqtt-20261019/qa-dsn)" go test -race -count=1 -v ./internal/brokerstate
```

Result: PASS; all package tests ran against private per-test schemas, including both new boundary subtests. No implementation code was changed.
