# Linux/arm64 sanitizer stress attempt (2026-10-20)

The isolated ASan/UBSan run delivered the requested 10,000 QoS 1 telemetry messages and the service subscriber received and validated all 10,000 ingress envelopes across MQTT 3.1.1 and MQTT 5. No AddressSanitizer or UndefinedBehaviorSanitizer diagnostic appeared in the captured output.

The reconnect portion did not complete: two authenticated connect/disconnect cycles succeeded, then the third MQTT 3.1.1 CONNECT was rejected with CONNACK reason code `0x05` (not authorized). This makes the 10k/1k tier **INCONCLUSIVE**. No 100k/10k tier was started. The failure is an authentication/session behavior issue, not a sanitizer report; its root cause is not established by this run.

The test ran in a dedicated Linux/arm64 container with loopback-only broker and fixture. It reused the previously built pinned Mosquitto v2.1.2 ASan/UBSan broker and Go c-shared plugin; only the standalone QA client and launcher were mounted from the workspace. No existing broker, production port, or public backend was touched.

- Mosquitto source: tag `v2.1.2`, commit `99fa50f30e325609394c324c8ff71cfbbe95d8ab`.
- Sanitizer image ID: `sha256:4c22bcb5493287169e5ec816cc38e9d919b557f281f85bea0eedae2b701d6db5` (local image identity, not a registry digest).
- Broker binary SHA-256: `75bc6fb81a002f549ea3536fb6d4d52424203a0c38891fa11a8cb3c2385f2b4a`.
- Go c-shared plugin SHA-256: `d966a2ce6a28277a61c4072b06f1ffd38f7d93f739d62b4de1abe896cdd93b14`.
- QA runner SHA-256: `491c6f72a0fa7af5b70f51c74d53b7b3c6b1206bdd25d28fd48e8a347b1e591b` (linux/arm64, static Go binary).
- Leak detection was disabled for this Go c-shared sanitizer run; this is not evidence of leak freedom.
- The 10,000 message PUBACK and ingress observations do not establish database acceptance, terminal processing, or physical side effects.

The full captured broker/client output is `mqtt-community-linux-arm64-asan-stress-10k-1k.raw.log`; compact stage evidence and verdict are in the adjacent `.raw.ndjson` and `.assertions.json` files. Checksums are listed in the adjacent `.sha256` file.
