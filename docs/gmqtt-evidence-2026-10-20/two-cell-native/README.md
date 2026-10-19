# Two-cell native migration QA

This archive records three executions of the opt-in native Mosquitto A→B→A test: the root's first run and an independent Luna run with `-count=2`. The independent run used the managed candidate at commit `dece31a4736136b76e52ebe816127e0fe1e7ba1b`; the two test files were untracked test-only files at that commit and are identified by SHA-256 below. The candidate also contained unrelated untracked files; they were not read or changed for this run.

The repeat command, run from that candidate, was:

```sh
TEST_BROKER_DATABASE_DSN="$(cat /tmp/tp-mqtt-20261019/qa-dsn)" \
TEST_MQTT_NATIVE_MIGRATION_E2E=1 \
env -u TP_NATIVE_E2E_DOCKER_IMAGE \
go test -json -race -p 1 -count=2 -timeout 4m ./internal/app \
  -run '^TestBrokerMigrationNativeTwoCellRoundTrip$'
```

The QA DSN remained in its mode-0600 file and was not emitted in logs. The test created a private `brokerqa` PostgreSQL schema and a private loopback Redis process. It used two native local Mosquitto instances bound to loopback and two independent child backend runtimes. All external resources were cleaned up by the test.

Results:

- Root first run: 1 run, 1 pass, 0 skips, 0 failures; test time 16.81s.
- Independent race repeat: 2 runs, 2 passes, 0 skips, 0 failures; test times 16.85s and 15.79s.
- Each run exercised active A at epoch 1, connecting/active B at epoch 2, and active A again at epoch 3. It asserted fresh four-role runtime rosters, four source fence acknowledgements, source publish denial after fencing, a distinct connection observation for the new cell/epoch, a correctly scoped nonempty pending receipt before activation, and receipt processing plus business-row/effect-intent counts afterward.

The target-ready SHA supplied to `PrepareTarget` is a synthetic fixture marker. This test does not certify operator configuration, storage, consumer readiness, durable broker storage, command delivery, physical device effect, or production migration. It is native two-cell route-transition evidence only.

Source identities for the independent run:

- Candidate HEAD: `dece31a4736136b76e52ebe816127e0fe1e7ba1b`
- `internal/app/broker_migration_cells_native_test.go`: `fae94647e6224b0871e5f37c6c45dd080a5193ddfee71cf407674090e61b41f7`
- `internal/app/broker_migration_roundtrip_native_test.go`: `4ccf8da8544b15b954ba5b2ee1ee112d2d882c747a34904a62246ed631b8ed99`

Raw `go test -json` output and a plain test-output extraction are retained for both the root first run and the independent repeat. The extraction is for reading; the JSON files are the authoritative original tool output.

The first root run preceded the later initial-connection wait, per-cell connection-ID assertions, exact receipt count and detailed phase logging. Its pre-change source SHA was not captured. The two independent executions use the exact source hashes listed above and provide the evidence for those stronger assertions. The A broker/backend stay running throughout this route round trip; no artificial process restart or new-boot generation is claimed.
