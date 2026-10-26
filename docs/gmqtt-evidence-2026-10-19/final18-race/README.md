# Candidate 18-package race/build evidence

This folder preserves the full PostgreSQL/native-MQTT run, its one environment-sensitive test failure, the targeted fixture-only rerun, and the earlier route-activation failure/diagnosis. It does not represent one all-green 18-package run.

## Frozen candidate and environment

- Workspace HEAD: `ad2cbd7f5e7fac399b0a3e55840c349d4b707d20`; candidate contained the explicitly frozen, uncommitted source snapshot; no parallel source edits occurred during the full run. The later CLI test-only change is recorded separately.
- Source list: root manifest had 359 entries. `source-manifest-before-cutover-test-fix.json` SHA-256: `f812ef21b7c1ae62d4242151af62d5bafe1ad68ea5cb48dd3d16f16ed3037eec`. After the single CLI test-fixture adjustment, `source-manifest-after-cutover-test-fix.json` SHA-256: `538dacf1463a6833af571e4fa0abbe42d3ae776e86ceb716a79f36be39cd2e05`.
- Go toolchain and host: `go1.26.2`, `darwin/arm64`.
- QA PostgreSQL DSN came from `/tmp/tp-mqtt-20261019/qa-dsn` and was exported silently to `TEST_BROKER_DATABASE_DSN`, `TP_TELEMETRY_BROKER_TEST_DSN`, `TP_BROKER_LEDGER_TEST_DSN`, and `TP_BROKER_COMMANDS_TEST_DSN`. The DSN value was not recorded in logs.
- `TEST_MQTT_NATIVE_E2E=1`; `TP_NATIVE_E2E_DOCKER_IMAGE`, legacy opt-in, and long-load settings were unset. Thus native MQTT/PG tests used the local Mosquitto binary/plugin path, not a second Docker-image run. The native test cases completed against isolated QA PG schemas and local QA Redis.
- Package build cache cleanup was not performed by this run. Available disk before the final build was 1.8 GiB.

## Full 18-package race run

The command was a single `go test -json -race -p 1 -count=1` run over the original 17 packages plus `./cmd/tp-broker-cutover`, with `umask 077`. The complete JSON event stream is `final18-full-race-failed-umask-test.json`; `final18-full-race-failed-umask-test.log` is the output-event extraction from that same invocation. JSON SHA-256: `e9f79e9ce01b3752516cde8e047e39ec0272e00c6fb3767424c8d2cda0006f1d`; extracted log SHA-256: `8c7bbf8a88f9bf8f5c884b66b1b5d0ea28a42bac0145a47f32f29d6713eca114`.

Seventeen package summaries passed, including `brokerledger`, `brokercontrol`, native ingress, planned/durable native command E2E, telemetry evidence API, storage trusted-telemetry receipt activity, and cell-scoped worker tests. The only test failure was `TestCutoverDSNFileRequirementsAndErrorRedaction`: the test requested a 0640 file through `os.WriteFile`, but the run's secure `umask 077` created it as 0600, so its “permissive mode must be rejected” precondition was not actually established. This was a test-fixture defect; production code was not changed to accommodate it.

Exactly three tests skipped:

- `TestBrokerIngressNativeSIGKILLChild` — private subprocess helper, intentionally skips as a standalone test.
- `TestBrokerIngressNativeReceiptSurvivesSIGKILLBeforeAck` — frozen Linux image not provided to this local-Mac run; the parent separately reported two frozen-image runs passing.
- `TestBrokerIngressLegacyGMQTTOwnerPostgresHealth` — legacy opt-in intentionally disabled.

The new telemetry evidence API, trusted telemetry writer, native planned-command flow, route activation, and cell-scope tests did not skip. The native MQTT tests that were required for this run passed.

## Targeted cutover test-fixture rerun

The parent corrected only `cmd/tp-broker-cutover/main_test.go` to `Chmod(0640)` after `WriteFile`, making the permissive-mode fixture deterministic under restrictive umask. With `umask 077`, the package-only `go test -json -race -p 1 -count=1 ./cmd/tp-broker-cutover` passed, including its real PostgreSQL freeze/barrier test. JSON SHA-256: `bc6e984c430023e8103a1ba4af7482d29910dfb3db70c8e28fbec13be5b7e156`; log SHA-256: `2d88a0dc3fa1d0b5c45f484a1762a41bd2d5d2ae7e1ee823b52ef3b6af2dbfa7`.

This targeted rerun plus the initial full run must not be described as a single all-green 18-package run. No second full run was performed.

## Route-activation clock diagnosis and fix validation

`candidate17-preclock-full-race-failed.json` and `.log` preserve the previous full candidate run. It had one real failure: `TestPostgresMigrationActivationGatesLedgerProcessing` returned `ErrTargetUnproven`. That specific failure did not reproduce in a 10-run diagnostic repeat on the then-current candidate: all 10 runs passed, with `connected=true` and `ingress=true`. In those runs, app time was about 0.56–0.82 ms ahead of PostgreSQL `clock_timestamp()`, and receipt `created_at - prepared_at` was +4.887–10.241 ms. These samples do not prove that clock skew caused the earlier failure.

The code review established the relevant transaction ordering: the receipt is accepted while holding the transport binding lock and must match the exact target cell and new epoch, which only become admissible after `PrepareTarget` commits. Receipt wall-clock timestamps therefore are not causal evidence and can be skewed across hosts. `Activate` no longer compares receipt `created_at` to DB `prepared_at`; it retains exact tenant/device/cell/new-epoch, pending-state, and nonempty-payload checks. Real connection proof still requires a fresh online observation with PostgreSQL-written `received_at >= prepared_at`. The production observation writer sets `received_at=now()` in SQL.

The updated ledger fixture sets receipt time two minutes before preparation and still requires an actual target connection; the control regression gives an old-epoch receipt a future wall timestamp and confirms it cannot prove target access. Full `brokerledger` and `brokercontrol` race packages passed against real QA PostgreSQL after the fix. JSON SHA-256: `09e34b9f56ca18e273184f991cd097e46a93c8fbaa83562952ff289979a857e8`; log SHA-256: `4d79ff8992a3351bbb5bf6fff457e046caadf43251cb10b3b0780e4bee5c4c31`. The 10-run diagnostic log is `activation-clock-diagnostic-count10.log`, SHA-256 `6b8d32e99769363ae3d2c52d823c1cd1dd6961943e703b15f54ce3b800017845`.

## Build

`go build -p 1 ./...` completed successfully after the targeted CLI fixture rerun. Its captured output file is empty on success: `go-build-p1-all.log`.
