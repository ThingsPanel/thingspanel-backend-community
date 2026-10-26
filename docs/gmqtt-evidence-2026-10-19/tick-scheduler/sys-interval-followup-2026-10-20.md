# Tick scheduling and `sys_interval` follow-up (2026-10-20)

## Result

On the frozen Linux/arm64 ASan/UBSan Mosquitto 2.1.2 image, adding only `sys_interval 1` to the QA broker config reduced the idle authenticated CONNECT time from the earlier ~5.02 s observation to 1.001–1.017 s in ten sequential MQTT 3.1.1 / MQTT 5 connect-disconnects. All ten succeeded; total handshake loop time was 10.047 s. This matches the configured one-second fallback interval. This small sample is not a production SLO or P99 certification.

After the concurrent host tests ended, an idle-host confirmation completed ten more sequential MQTT 3.1.1 / MQTT 5 connect-disconnects in 10.141 s (1.003–1.026 s each). With the same config, the existing-subscription refresh-denial case passed in 5.532 s. The owner fixture saw the device's initial authentication and its second refresh request about five seconds later, both on the same connection nonce/ID. The test published once before refresh and verified that a service publish after the denied refresh was not delivered to the already established subscription. This confirms that the periodic refresh path ran and revoked delivery in this isolated case.

The first ten-connect run overlapped concurrent PostgreSQL race tests reported by the parent agent, so it is not treated as an idle-host measurement. The confirmation run was performed after those tests ended. Both runs are small samples and do not establish a statistical P99. The `sys_interval 1` setting also schedules `$SYS` updates every second; this run did not measure `$SYS` message volume, CPU, or persistence overhead. Include those in paired performance testing before making it a required production setting.

After the idle confirmation, the bounded pressure runner completed 10,000 QoS 1 messages with 10,000 broker acknowledgements and 10,000 ingress observations, plus 1,000 authenticated reconnects at configured concurrency 16. The reconnect stage took 64.291 s; the whole run took 69.036 s. Owner telemetry contained all 1,000 reconnects (plus refresh calls), with maximum observed owner-request overlap 6. This was the frozen earlier plugin snapshot listed below, before the parent's later 1 MiB input/2 MiB envelope guard changes. It is count/sanitizer evidence for that exact snapshot, not a PASS for the newer plugin, not a 100k/10k run, and not a latency or business-acceptance claim. The ingress listener received every test envelope; the run did not check durable database acceptance or downstream effects.

## Comparison evidence

The preceding unchanged-config ASan tick probe on the same image measured idle CONNECT at 5.023 s and a traffic-source CONNECT at 5.029 s. A third CONNECT while that client sent PINGREQ every 100 ms completed in 53.2 ms after three PINGREQs. The fixture's owner HTTP responses were about 1 ms. A normal, non-sanitized QA image completed the corresponding idle/source/target connects in 110.0 / 109.0 / 50.5 ms. This points to a loop scheduling delay in the sanitizer run; it does not alone prove the exact scheduler root cause.

Pinned source inspection is consistent with the configuration result: `plugin_tick.c:39-45` only updates the next event after a due tick callback runs; `loop.c:203-210` resets the event horizon each iteration and calls `sys_tree__update`; `sys_tree.c:231-236` schedules a next event based on `sys_interval`. No broker source, plugin source, image, or production config was changed for this test.

## Reproduction scope

- Mosquitto source: v2.1.2, commit `99fa50f30e325609394c324c8ff71cfbbe95d8ab`.
- Frozen ASan/UBSan local image: `sha256:4c22bcb5493287169e5ec816cc38e9d919b557f281f85bea0eedae2b701d6db5`, linux/arm64. Broker binary SHA-256 `75bc6fb81a002f549ea3536fb6d4d52424203a0c38891fa11a8cb3c2385f2b4a`; plugin SHA-256 `d966a2ce6a28277a61c4072b06f1ffd38f7d93f739d62b4de1abe896cdd93b14`.
- Only the standalone QA runner, launcher, and broker config were mounted. The fixture and broker ran in one isolated container, listening on loopback only. No existing broker, public port, backend, or database was used.
- Runner built from QA sources for linux/arm64 with `CGO_ENABLED=0`; the ten-connect runner SHA-256 was `e7de1c05141dbe56ba513d046bace172f35593bca04d9ac2560ebaf36f7919ce`, and the concurrent stress runner SHA-256 was `de2bd4a4003cd8fbcdae7cb0e27788c5e74e1ae83324232b4d7921a21d70f918`.
- Test-only config is `mosquitto.sys-interval-1.qa.conf`; it differs from the prior QA config by `sys_interval 1`.
- Ten-connect reports and owner observations are in `sys-interval-1-idle10.*` and `sys-interval-1-idle10-empty-confirm.*`; refresh-denial report and owner observations are in `sys-interval-1-refresh-revocation.*`; concurrent stress evidence is in `reconnect32-concurrency16.*` and `stress-10k-1k-concurrency16.*`. The earlier idle-vs-busy comparison is in `asan-tick-probe-*` and `native-tick-probe-*`.

## Checksums

See `SHA256SUMS` in this directory for all captured reports, raw container logs, and configs.
