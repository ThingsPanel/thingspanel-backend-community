# Cutover operation audit increment

This is isolated component and native route-regression evidence, not a production certificate. Scope: community only. Candidate base at execution: `27e4fa6d8fb5b476533fd8773c99241eb2a64f9d`; uncommitted audit sources are frozen in `root-source-sha256.json`.

The reviewed-plan CLI previously persisted actor/raw plan SHA only for Freeze. It now binds Prepare, Activate and Rebase to a fresh store copy carrying the expected operation, caller-supplied actor and raw plan SHA. The store validates the actual private operation label and inserts an `operator_apply` event in the same SQL transaction after the operation. An audit insert failure rolls back the phase and prior phase event. Fence ACK and runtime-sync paths cannot be relabeled through this facade. Internal stores without the facade retain their existing behavior. Actor/hash labels are not signed human identity or approval proofs. Successful idempotent retries may add audit events.

## Executed validation

- Luna: real isolated PostgreSQL `brokerqa`, two packages `internal/brokercontrol` and `cmd/tp-broker-cutover`, `go test -json -race -p 1 -count=1`: 39 test-level passes, zero fail/skip; both packages pass. See `RESULTS.txt` and original `two-packages-race.json`. Prepare/Rebase/Activate audit fields, operation mismatch, store copy isolation and trigger-rejected audit insertion rollback are covered. CLI fixture fence ACKs are Store API calls, not real runtime barrier evidence.
- Root: `TEST_BROKER_DATABASE_DSN=<private file> TEST_MQTT_NATIVE_MIGRATION_E2E=1 env -u TP_NATIVE_E2E_DOCKER_IMAGE go test -json -race -p 1 -count=1 -timeout 3m ./internal/app -run ^TestBrokerMigrationNativeTwoCellRoundTrip$`: actual pass, zero fail/skip, 16.52s test time. This reuses the exact two test files previously certified in `../two-cell-native/README.md`, against the changed migration store. Two native Mosquitto processes, two child backend runtimes, actual four-role fences, actual target connections and pending-to-processed receipts pass A/1→B/2→A/3. The synthetic target-ready marker and other limits of that prior evidence still apply. This does not test CLI audit through the native test; the CLI/transaction assertions are the separate PostgreSQL tests above.
- Root: `go build -p 1 ./...` exit 0, see `go-build-status.json`; empty build output is normal.

DSN remained in a mode-0600 private file and is absent from evidence. Private schemas/processes were cleaned by tests. Root checked all seven source hashes after the runs. No new SQL migration or wire protocol change was made.

The earlier Linux production image remains a frozen older artifact: it was not rebuilt after this production store change. These results do not update that image's certification, nor establish 72-hour stability, paired performance gain, real-device compatibility, full operational audit certification or gray rollout readiness.
