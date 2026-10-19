# Native Mosquitto, Owner, and PostgreSQL ingress evidence

Date: 2026-10-20

This opt-in integration test exercises the community ingress path with the locally built Mosquitto 2.1.2 binary, a freshly compiled native plugin, the real Go/Paho adapter, the broker Owner HTTP handler, PostgreSQL receipt processing, and a child Redis server. It is an integration check for this local checkout, not a production-duration or deployment acceptance claim.

Run it with:

```sh
TEST_MQTT_NATIVE_E2E=1 go test ./internal/app -run '^TestBrokerIngressNativeMosquittoOwnerPostgresE2E$' -count=1 -v
```

The default local inputs are `/tmp/tp-mqtt-20261019/qa-dsn`, `/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto`, `/tmp/tp-mqtt-20261019/mosquitto/include`, `/opt/homebrew/opt/cjson/include`, and `/opt/homebrew/bin/redis-server`. `MOSQUITTO_BIN`, `MOSQUITTO_INCLUDE_DIR`, `CJSON_INCLUDE_DIR`, and `REDIS_SERVER` override the binary/include paths. The test skips with a specific missing-dependency reason if an input is unavailable; the PostgreSQL DSN is read from the QA-only file and is never included in output.

The test creates a random private PostgreSQL schema and applies the relevant application tables and broker SQL migrations 001–010 there. It starts Redis on an ephemeral loopback port with persistence disabled, builds the plugin into a temporary directory, starts a private Mosquitto process on a dynamic loopback port, and starts the real Owner HTTP handler with a temporary token. It restores the process globals after the test and does not modify the shared broker or public database schema.

The evidence covers these separate outcomes:

- A device publish is accepted by Mosquitto and receives the broker-level PUBACK. In a comparison path, a real Paho subscriber's injected rejecting acceptor creates no ledger receipt or event row and sends no application-subscriber PUBACK. This injected failure is a simulated acceptor failure boundary; it is not a database outage injection.
- In the real application path, the receipt reaches `processed`, the event row and effects intent are written, and the application subscriber PUBACK is observed in the Mosquitto log.
- Re-publishing the same explicit event `source_event_id` with a new MQTT packet leaves one receipt, one event row, and one effects intent while producing a fresh application acknowledgement.
- An ordinary attribute sample is recorded under its existing payload contract. Glue-level coverage also verifies that an ordinary attribute field named `source_event_id` remains business payload rather than becoming the event idempotency key.
- An actual MQTT telemetry publish passes through the production adapter, uplink bus, `TelemetryUplink`, and storage batch writer. PostgreSQL assertions cover tenant/device-scoped history and current value, a `telemetry_sample` receipt at the authenticated cell and route epoch, telemetry activity, and the drained state-projection queue.
- That telemetry receipt matches a configured TEL automation policy. The planner creates a durable machine command; the real device receives the expected command topic and unframed JSON payload, and its MQTT response advances the command to `device_reported_success`.
- A valid command ACK updates its own command row. An ACK targeting a command owned by another device leaves that row unchanged, creates no additional accepted receipt, and receives no application-subscriber PUBACK.

The asserted ACK evidence comes from Mosquitto's log lines for the actual client IDs, while receipt and business outcomes are queried independently from PostgreSQL. These checks demonstrate broker transport acknowledgement, application acceptance acknowledgement, ledger terminal processing, and database business writes as separate boundaries. The native TEL command path reaches a real MQTT device client and receives its ACK; it does not establish physical actuator behavior on hardware. ATTR ingress is verified for its persisted business row, while ATTR automation matching remains covered only by PostgreSQL planner tests.

Most recent local run:

```text
TEST_MQTT_NATIVE_E2E=1 go test ./internal/app -run '^TestBrokerIngressNativeMosquittoOwnerPostgresE2E$' -count=1 -v
PASS (5.64s)
```
