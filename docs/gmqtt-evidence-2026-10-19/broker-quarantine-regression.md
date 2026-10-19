# Broker ingress quarantine regression evidence

Date: 2026-10-19

The adapter regression verifies that `commandResponseScope` returns a transient database error without classifying it as `brokercontract.ErrUnauthorized`. A trusted command ACK with an unauthorized scope is durably rejected through the ingress rejector; when the first quarantine write fails, the subscriber does not ACK and retries the same frame. Once the write succeeds, the subscriber ACKs exactly once and the business acceptor is never called.

The application boundary test uses a private PostgreSQL schema and the QA database. It calls the real `brokerIngressAcceptor` and `brokerledger.Store`, records a binary payload rejection twice, and verifies one quarantine row with `occurrences=2`, zero accepted receipts, and zero business effects. The schema is dropped by the fixture. The QA DSN is read from `/tmp/tp-mqtt-20261019/qa-dsn` and is never emitted.

Validated:

```text
go test -race -count=1 ./internal/adapter/mqttadapter ./internal/app -run 'Test(CommandResponseScopeDatabaseFailureIsTransient|QuarantineMustCommitBeforeManualAckAndDoesNotAcceptBusiness|BrokerIngressAcceptorRejectPersistsWithoutBusinessAcceptance)$'
PASS; app PostgreSQL test ran against the isolated QA database (not skipped).
```

The native Mosquitto/Owner/PostgreSQL/Redis integration ran with `TEST_MQTT_NATIVE_E2E=1` and passed after correcting two fixture assumptions: durable command IDs are UUIDs (short legacy IDs must take the scoped legacy lookup), and the community command log's pending status is `0` (the test had seeded the unsupported string `pending`). The test then verified that a foreign-device command ACK creates one quarantine row, leaves the foreign command at status `0`, creates no accepted business receipt, and receives a subscriber PUBACK only after quarantine commit. The passing command was:

```text
TEST_MQTT_NATIVE_E2E=1 go test -race -count=1 ./internal/app -run '^TestBrokerIngressNativeMosquittoOwnerPostgresE2E$' -v
PASS (4.09s)
```

The same native run started the production `UplinkServiceWrapper` and `StatusUplink` on the real bus. After attaching a heartbeat configuration, it verified the broker connection projection set the device online, wrote exactly one online `device_status_history` row, and drained that device's projection queue key. The fixture keeps `device_config_id` empty through ordinary data ingress so the existing no-transform event path remains unchanged. Automation emitted a missing optional fixture-table log; it did not affect projection or test success.
