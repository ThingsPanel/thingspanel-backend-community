# Linux/arm64 sanitizer smoke evidence (2026-10-20)

This isolated debug build uses pinned Mosquitto `v2.1.2` commit `99fa50f30e325609394c324c8ff71cfbbe95d8ab`. CMake compiled Mosquitto and its C objects with `-fsanitize=address,undefined -fno-omit-frame-pointer`; the Go c-shared plugin was built with Go `1.26.2 linux/arm64`, `go build -asan`, and matching CGO address/undefined sanitizer flags. The resulting broker and plugin both dynamically link `libasan.so.8` and `libubsan.so.1` in the runtime image.

- Sanitizer image ID: `sha256:4c22bcb5493287169e5ec816cc38e9d919b557f281f85bea0eedae2b701d6db5`.
- Broker binary SHA-256: `75bc6fb81a002f549ea3536fb6d4d52424203a0c38891fa11a8cb3c2385f2b4a`.
- Go c-shared plugin SHA-256: `d966a2ce6a28277a61c4072b06f1ffd38f7d93f739d62b4de1abe896cdd93b14`.
- Runtime: one fixture and broker in a dedicated container, listening only on 127.0.0.1. Existing broker/container ports were not touched.

The sanitizer smoke completed a minimal MQTT 3.1.1 service subscribe, device binary telemetry publish, broker C plugin callback, Go rewrite, and exact ingress envelope assertion. The run emitted no AddressSanitizer or UndefinedBehaviorSanitizer diagnostic. Raw ledger and assertion files are adjacent; SHA-256 values are recorded in `mqtt-community-linux-arm64-asan-smoke.sha256`.

This is a narrow sanitizer smoke only. The full protocol matrix was run on the normal QA profile, not the sanitizer profile. No 100,000-message or 10,000-reconnect stress was run, no production performance conclusion follows, and leak detection was disabled to avoid Go runtime leak-sanitizer incompatibility.
