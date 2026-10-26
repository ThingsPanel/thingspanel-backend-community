# Linux Mosquitto QA snapshot — 2026-10-20

This records a frozen native-plugin test snapshot. It is not release certification or a production run.

## Artifact identity

- Architecture: Linux/arm64; Mosquitto 2.1.2; QA config includes `sys_interval 1`.
- ASan/UBSan image ID: `sha256:4b95f645e1b08f8715d533e67ee1ae8b338ab051b8092047cda631a55f71bc9e`.
- ASan/UBSan broker binary SHA-256: `75bc6fb81a002f549ea3536fb6d4d52424203a0c38891fa11a8cb3c2385f2b4a`.
- ASan/UBSan plugin SHA-256: `57e2ba0ced173defcbc7058ecc9e6dd55e2954a633b85ecf14c6dd3e7491aa5b`.
- ASan/UBSan runner SHA-256: `3bbbfed5ff39229ccc14b251d6927f02d251f2fb6811f8a7ddbd27fafa9c35ec`.
- Native source manifest: [native-source-files.sha256](native-source-files.sha256), manifest SHA-256 `c5263d2952ca19e710760a436c74b0e7e10fbc3a577aaa1f90b69d5915d26263`.
- Non-ASan full-matrix image ID: `sha256:a7b5857c8116811c45b6c9681d9473cbc44e1b5d148a849fc1da4116e90bcb97`.
- Non-ASan broker binary SHA-256: `9e52fb96e67d5d6c16e1429371d265e83b68d8d4d20fece72f560a8db281ae09`.
- Non-ASan plugin SHA-256: `96aa13d17997ab348b0e7ce9ad91221c701afcfe766a4549a59f18340c254bc8`.
- Non-ASan runner SHA-256: `d7cdb77047b8d4daef9a2ec947eaf7a59ed3b6bc2948d919697511498c815cc0`.

The source snapshot predates root's later opt-in strict runtime-fencing change. It does not test that branch. The manifest is retained as the build-time snapshot; the current shared checkout now differs in `cmd/tp-mosquitto-plugin/main.go`, `internal/mosquittodriver/command.go`, and `internal/mosquittodriver/driver.go` after that later change. Do not validate the old run by comparing its manifest to today's mutable checkout; use the immutable image, binary, plugin and runner IDs above. Re-run a bounded Linux real-MQTT test for the new strict-fencing source before applying those results to it.

## Results

- ASan/UBSan 10k-message / 1k-reconnect step: PASS. 10,000 broker ACKs and 10,000 ingress observations; 1,000 successful reconnects; concurrency 16; owner max overlap 6; elapsed 1m13.298s. This is a bounded sanitizer smoke/scale step.
- ASan/UBSan 100k-message / 10k-reconnect step: PASS. 100,000 broker ACKs and ingress observations; 10,000 successful reconnects; concurrency 16; owner max overlap 16; message phase 53.316s; reconnect phase 636.894s; total 11m30.211s. Reconnect duration p50/p95/p99/max: 1018/1036/1051/1089 ms. Fixture recorded 10,032 auth calls, including connection refreshes.
- Grep found no ASan/UBSan diagnostic in either retained raw log. Leak detection was disabled; this is not leak-free evidence.
- For both ASan stress reports, business `accepted`, `terminal`, and `sideeffects` remain unobserved. `broker_ack` and `ingress_seen` are separate observations and do not prove SQL acceptance.
- Non-ASan MQTT 3/5 full matrix passed the runnable protocol and ACL checks, including a 1 MiB payload and 65,535-byte topic roundtrip. Overall result remains **INCOMPLETE** because immediate cancellation of a disconnected pre-CONNACK auth request is unsupported by the frozen Mosquitto 2.1.2 callback path. Delayed/restored Will scenarios also retain documented unsupported coverage. The failure is not reclassified as a stress failure or acceptance success.
- The later strict runtime-fencing branch requires its own smaller Linux real-MQTT run. These artifacts do not cover it.

Small JSON reports and the 10k-step owner fixture record are kept directly here. Large logs and the 100k-step owner record are stored as deterministic gzip; original bytes remain in `/tmp/tp-mqtt-20261019/build/latest-plugin-2026-10-20/`.

To verify compressed content SHA-256 values, run from the repository root:

```sh
gzip -dc docs/gmqtt-evidence-2026-10-19/latest-linux-plugin-2026-10-20/asan-100k-10k.raw.log.gz | shasum -a 256
gzip -dc docs/gmqtt-evidence-2026-10-19/latest-linux-plugin-2026-10-20/asan-10k-1k.raw.log.gz | shasum -a 256
gzip -dc docs/gmqtt-evidence-2026-10-19/latest-linux-plugin-2026-10-20/asan-100k-10k.owner.json.gz | shasum -a 256
```

Expected uncompressed SHA-256 values, in order: `db13876f8849b50e43fb005d26e92dc78fe35ed9c996c3698447afa237704f8f`, `9547b9edfd46ad53ef9ac7433698bb504031b4f861c96e08d53bea529ad8eb60`, `4705a9e995a4fb39ac191f65a8b67e57191d994d7a450f526a924b7d37034068`.

## Paired full-stack baseline preparation

The read-only inventory and 60-second health profile are in [paired-baseline-feasibility-2026-10-20.md](paired-baseline-feasibility-2026-10-20.md). No old/new full-stack trial was started: dedicated QA Redis and a runner that carries event IDs through Adapter/Uplink/Storage to SQL are missing.
