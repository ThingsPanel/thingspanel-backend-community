# Native automation command end-to-end evidence

The opt-in `TestDurableCommandNativeMosquittoPostgresE2E` includes an event-driven automation scenario in the isolated Mosquitto/owner/Paho/PostgreSQL fixture. A real device publishes an event with a matching field condition. The reliable ingress path commits the event receipt and automation effect, the planner commits a durable command, command log, and policy/action audit, the real device receives the transformed command payload, and its ACK advances the request to `device_reported_success`.

The native ingress E2E now also verifies the full telemetry route: real MQTT telemetry enters `TelemetryUplink`, persists through the production storage batch writer with a tenant/device/cell/epoch-scoped `telemetry_sample` receipt and activity, matches a TEL policy, and produces a durable command that the real Paho device receives and ACKs to `device_reported_success`. These runs verify EVT and TEL through native ingress to device ACK.

The test verifies that the durable command caller is `automation:<scene-id>`, its intent is `automation.command`, and its embedded policy hash matches the action audit. No human user ID is used as the command caller. Replaying the same source event does not produce another action receipt or MQTT delivery.

Validation command:

```sh
TEST_MQTT_NATIVE_E2E=1 go test -count=1 -run '^TestDurableCommandNativeMosquittoPostgresE2E$' ./internal/app
```

Result: PASS with the real native broker, owner, Paho device and isolated PostgreSQL fixture.

Coverage distinction: EVT and TEL each have a complete native ingress-to-device-ACK scenario. ATTR ingress and business persistence are covered by the native ingress E2E, and ATTR condition matching is covered only by PostgreSQL planner tests; a native ATTR-to-command scenario has not been verified.
