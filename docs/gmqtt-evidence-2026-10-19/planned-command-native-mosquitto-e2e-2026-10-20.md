# Planned command native Mosquitto E2E — 2026-10-20

Opt-in test: `internal/app/broker_command_plans_native_test.go`, `TestPlannedCommandNativeMosquittoPostgresE2E`.

This run used `TEST_MQTT_NATIVE_E2E=1`, the isolated QA PostgreSQL DSN file, a random private PostgreSQL schema with broker migrations 001–013, a child Redis server, the native Mosquitto/plugin fixture (or its configured Docker-image helper), the real application MQTT Adapter/command dispatcher, and a Paho device client. The Encore semantic endpoint was an exact-response `httptest` stub; this is not a live Encore verification.

The test waits for an online observation produced by the real device connection. It verifies preview causes no device delivery, submit without human authorization fails, authorized submit and same-key retry return one command ID, a different key cannot reuse the one-shot plan, the actual device receives exactly one command with the private storage frame absent, and the device's scoped `{"result":0}` ACK changes the same command status to `device_reported_success`. It checks one plan-to-command link, one durable command request, and one command log. It does not claim physical action beyond the protocol-level device ACK.

Relevant checks on the current source tree:

```text
go test -race -p 1 ./internal/app -run '^$'
ok project/internal/app [no tests to run]
go test -race -p 1 ./internal/api -run '^$'
ok project/internal/api [no tests to run]
TEST_MQTT_NATIVE_E2E=1 go test -race -p 1 ./internal/app -run '^TestPlannedCommandNativeMosquittoPostgresE2E$' -count=1 -v
--- PASS: TestPlannedCommandNativeMosquittoPostgresE2E (6.15s)
PASS
ok project/internal/app 8.167s
TEST_BROKER_DATABASE_DSN=<isolated QA DSN> go test -race -p 1 ./internal/service -run 'TestCommandPlanServicePreviewAuthorizeSubmitLifecycle' -count=1
ok project/internal/service 1.858s
TEST_BROKER_DATABASE_DSN=<isolated QA DSN> go test -race -p 1 ./internal/api -run TestBrokerSemanticEvidenceAuthenticatedEncoreLookup -count=1
ok project/internal/api 2.129s
```

The first native test attempt exposed a fixture-only panic because status projection emitted an SSE event while `global.TPSSEManager` was nil. The test now installs and restores a private SSE manager; the rerun above passed under the race detector.
