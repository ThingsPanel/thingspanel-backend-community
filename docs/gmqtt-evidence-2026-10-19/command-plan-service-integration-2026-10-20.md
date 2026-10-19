# Command plan service integration — 2026-10-20

`TestCommandPlanServicePreviewAuthorizeSubmitLifecycle` runs against a random private PostgreSQL schema, a child `redis-server` on an ephemeral loopback port, and an `httptest` Encore semantic API. It traverses the service methods and actual command-store transaction; it does not start a broker or publish to a device.

The regression verifies:

- Preview records a plan without creating a command request or command log. A fresh online connection is required. Changes to connection evidence or the pinned published revision fence the old preview.
- An enabled custom `device_topic_mappings` row rejects preparation. A disabled row does not block the standard MQTT command path. Adding an enabled custom mapping after acceptance makes the dispatch final guard reject the command.
- A disabled user cannot authorize. Submit without the recorded authorization is rejected. Repeated authorization returns the same authorization ID.
- An injected `command_set_logs` insert failure rolls back the command request, log, and plan consumption while preserving the authorization. Retrying with the same key creates one command ID; retrying that same key returns the same ID, while a different key cannot consume the plan again.

Related fail-closed compatibility checks call the legacy attribute set/get, telemetry control, OTA create/publish, and expected-data create/send methods with nil arguments and `global.DB == nil` under `mqtt.input_profile=ingress_v1`; each returns before database access or side effects. The MQTT adapter's raw legacy publish entry also rejects ingress_v1. These checks document an explicit unsupported feature boundary for ingress_v1; they do not mark those command families not applicable.

Executed checks:

```text
TEST_BROKER_DATABASE_DSN=<isolated QA DSN> go test -race -p 1 ./internal/service -run 'TestCommandPlanServicePreviewAuthorizeSubmitLifecycle|TestIngressV1RejectsUnsupportedLegacyDownlinks|TestLegacyDownlinkCapability|TestReadCommandFingerprint' -count=1
ok project/internal/service
go test -race -p 1 ./internal/api -run 'TestCommandPlan' -count=1
ok project/internal/api
go test -race -p 1 ./internal/adapter/mqttadapter -run 'TestIngressV1RejectsRawLegacyPublish' -count=1
ok project/internal/adapter/mqttadapter
TEST_BROKER_DATABASE_DSN=<isolated QA DSN> go test -race -p 1 ./internal/brokerstate -count=1
ok project/internal/brokerstate
go test ./router/apps -run '^$'
? project/router/apps [no test files]
```

The PostgreSQL-backed commands were run with `TEST_BROKER_DATABASE_DSN` read from the QA-only `/tmp/tp-mqtt-20261019/qa-dsn` file. The successful outputs above contain no `SKIP` result. The service test observes durable acceptance and the command log only. It does not demonstrate MQTT delivery, PUBACK, device acknowledgement, physical execution, or automation effects.
