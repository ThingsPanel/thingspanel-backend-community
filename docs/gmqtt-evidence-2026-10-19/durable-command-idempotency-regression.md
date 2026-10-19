# Durable command idempotency regression evidence

Date: 2026-10-20

The private PostgreSQL regression creates an accepted durable command, moves its stored deadline into the past, and confirms `FindIdempotent` still returns the same row only for the original tenant/caller/intent/key tuple. A changed canonical request conflicts; another caller or tenant receives no matching row.

The native Mosquitto command E2E exercises the public `CommandData` path and confirms concurrent same-key requests yield one receipt and one device delivery, changed params conflict, and the authenticated device ACK reaches terminal success. After ACK, the fixture freezes the caller and confirms retry is rejected; after restoring that caller, it disables and renumbers the device and expires the accepted receipt, then retries the identical original input and receives the original ID with no second MQTT delivery.

Validation:

```text
go test -race -count=1 ./internal/brokercommands -run '^TestPostgresFindIdempotentScopesCallerAndIgnoresAcceptedExpiry$'
PASS against the isolated PostgreSQL QA database.

TEST_MQTT_NATIVE_E2E=1 go test -race -count=1 ./internal/app -run '^TestDurableCommandNativeMosquittoPostgresE2E$' -v
PASS (latest reported run: 4.63s; isolated test invocation) across native Mosquitto, Owner, PostgreSQL, Redis, Paho device/service and the durable command dispatcher.
```
