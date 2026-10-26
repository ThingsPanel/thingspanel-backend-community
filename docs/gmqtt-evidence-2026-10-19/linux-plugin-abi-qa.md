# Linux/arm64 plugin ABI and MQTT QA

Date: 2026-10-19. Built an isolated Linux/arm64 test image against the pinned Mosquitto 2.1.2 source headers and runtime image. The Go module declares `go 1.23.0` and `toolchain go1.23.8`; this build used `go1.26.2 linux/arm64` from `golang:1.26.2-bookworm`. CGO flags were `-I/opt/mosquitto/include -I/usr/include/cjson`; `libcjson-dev` was installed only in the build stage.

- Broker image ID: `sha256:c103ceeb61129b0dae7e5c54d8545587b225956ceaed313e32f720aec122360e` (`linux/arm64`).
- QA image ID: `sha256:57f7f6e327742f1b042a91aedd500a815371e37c17d0c9e9c40606a585fe8c03` (`linux/arm64`). These local image IDs are not registry digests.
- Linux Mosquitto executable SHA-256: `9e52fb96e67d5d6c16e1429371d265e83b68d8d4d20fece72f560a8db281ae09`.
- Linux c-shared plugin SHA-256: `f95649de8ef409fc3db984e105fd9fd08a2e4e32aee7877b856b4fb5543feb20`.
- Linux plugin loads into the broker and registers auth, ACL, message-in/out, tick, connect, disconnect and offline callbacks. The broker binds only to container loopback 127.0.0.1:1883; the fixed-grant fixture binds 127.0.0.1:18084 in the same network namespace. A random per-run token is mode 0640 and readable by only the broker group after privilege drop.

## Results

- MQTT 3.1.1 and MQTT 5 binary/Unicode/empty ingress and ACL checks: PASS.
- MQTT 3.1.1 and MQTT 5 narrow filter, explicit shared group delivery, denied unlisted group: PASS.
- State, nonce-bound connection identity, immediate/graceful/forged/expired/revoked Will, retained snapshot/clear/RAP0: PASS.
- Owner delayed request longer than 2-second driver timeout is canceled; pending capacity becomes available and an ignored late response does not affect a new socket using the same ClientID: PASS.
- Immediate pre-CONNACK disconnect cancellation: UNSUPPORTED on this Mosquitto 2.1.2 delayed-auth path. Upstream `context.c` closes delayed-auth clients without DISCONNECT/CLIENT_OFFLINE callbacks; the request remains bounded by the driver's hard timeout. This is recorded as an unsupported check and causes the QA command to exit nonzero after all later checks and the ledger complete.
- Database-backed command queue, durable acceptance, terminal outcome and business side effects are not covered by the fixed-grant fixture. QoS 2 and restored Will remain unsupported/unverified.

The raw NDJSON, independent assertions and SHA-256 manifest are in `mqtt-community-linux-arm64.*`. Every case remains `INCOMPLETE` for application acceptance, terminal outcome and side effects; broker ACK does not imply acceptance. See `shared-subscription-prefix-control.md` for the separate no-plugin prefix control.

## Sanitizer status

`go help build` reports `-asan` support for `linux/arm64` with GCC 7+ or Clang 9+. The current successful build is a normal build, not a sanitizer build. Sanitizer instrumentation, runtime compatibility with Mosquitto, and 10,000-connection/100,000-message load gates have not been run. No performance or memory-safety certification is claimed.
