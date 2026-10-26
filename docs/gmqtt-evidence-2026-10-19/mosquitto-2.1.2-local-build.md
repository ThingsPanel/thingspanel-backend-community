# Mosquitto 2.1.2 local build evidence

Date: 2026-10-19  
Scope: isolated local QA bootstrap only; this is not a community integration certification or production approval.

## Fixed source and build

- Source checkout: `/tmp/tp-mqtt-20261019/mosquitto`
- Tag: `v2.1.2`
- Commit: `99fa50f30e325609394c324c8ff71cfbbe95d8ab`
- Source worktree was clean when checked.
- Host architecture: macOS arm64 (`uname -m` = `arm64`)
- Compiler: AppleClang 21.0.0
- CMake: 4.4.3
- OpenSSL: Homebrew `openssl@3` 3.6.3 (CMake found `/opt/homebrew/Cellar/openssl@3/3.6.3/lib/libcrypto.dylib`)
- cJSON: Homebrew 1.7.18
- Build/install tree: `/tmp/tp-mqtt-20261019/build`
- Broker executable: `/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto`
- Broker SHA-256: see `mosquitto-binary.sha256` in this directory.
- Plugin include directory: `/tmp/tp-mqtt-20261019/mosquitto/include`
- Relevant headers: `mosquitto_broker.h`, `mosquitto_plugin.h`, `mqtt_protocol.h`.
- Go c-shared plugin path for the later Linux container: `/mosquitto/plugins/libtp-mosquitto-plugin.so`.

The upstream pull attempt for `eclipse-mosquitto:2.1.2` returned `manifest unknown`. Therefore the macOS binary was built from the fixed source tag, not from Homebrew Mosquitto 2.0.18.

Configuration used:

```text
CMAKE_BUILD_TYPE=Release
CMAKE_INSTALL_PREFIX=/tmp/tp-mqtt-20261019/build/install
WITH_DOCS=OFF
WITH_TESTS=OFF
WITH_BUNDLED_DEPS=ON
WITH_TLS=ON
WITH_TLS_PSK=ON
WITH_WEBSOCKETS=ON
WITH_WEBSOCKETS_BUILTIN=ON
```

The following broker configuration directives were accepted at startup: `listener`, `allow_anonymous`, `persistence`, `persistence_location`, `pid_file`, `log_dest`, and `log_type`. The exact local PoC file is `deploy/mosquitto-community/mosquitto.native.conf`; it binds only `127.0.0.1:32883`, with isolated test data under `/tmp/tp-mqtt-20261019/build/data/`.

## Runtime smoke evidence

- `/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto -h` reported `mosquitto version 2.1.2`.
- The broker started from the isolated config and listened on loopback port 32883. Existing GMQTT remained on 1883.
- Using the matching 2.1.2 command-line clients, a QoS 1 retained message on `qa/bootstrap/smoke` was published and read back as `mosquitto-2.1.2-ready`.
- Host client commands need `DYLD_LIBRARY_PATH=/tmp/tp-mqtt-20261019/build/install/lib` because the install prefix is outside standard macOS library search paths.
- This proves a local standalone broker and basic MQTT connection/publish/subscribe only. It does not test the ThingsPanel plugin, auth/ACL contract, migrations, durability under faults, capacity, or device compatibility.

## Linux image status

`Dockerfile` builds the same pinned source commit into a Linux image for the host's `linux/arm64` architecture. The build command and loopback-only Compose profile are in `deploy/mosquitto-community/README.md`. Docker Hub did not provide the requested upstream 2.1.2 tag; do not use the host-native macOS binary as a Linux plugin ABI target.

The attempted Docker build was stopped during the slow base-image package download and did not produce or validate a Linux image. Docker build/runtime and Linux plugin ABI remain unverified.

## Plugin MQTT QA evidence

- Plugin rebuilt from the shared current Go source using `CGO_CFLAGS='-I/tmp/tp-mqtt-20261019/mosquitto/include -I/opt/homebrew/opt/cjson/include' go build -tags mosquitto_plugin -buildmode=c-shared -o /tmp/tp-mqtt-20261019/build/tp-mosquitto.so ./cmd/tp-mosquitto-plugin`.
- The pinned native 2.1.2 broker loaded that plugin and listened only on `127.0.0.1:32884`; startup log recorded auth, ACL, message-in, tick, connect and disconnect callbacks. The fixed-grant fixture listened only on `127.0.0.1:18084` and read its random 0600 token file.
- `go build -o /tmp/tp-mqtt-20261019/build/mqtt-community-qa ./test/mosquitto-community` and `go test ./test/mosquitto-community` completed successfully (the package has no Go unit test files; the executable is the live wire-level test tool).
- The live test command `/tmp/tp-mqtt-20261019/build/mqtt-community-qa test -broker 127.0.0.1:32884 -fixture http://127.0.0.1:18084` exited 0. It exercised MQTT 3.1.1 and MQTT 5 ingress for binary, Unicode and empty payloads; trusted envelope identity and base64 round-trip; mixed and reversed allowed/denied SUBACK filters; cross-device, internal-topic and retained publish denial; explicit CONNECT denial; pre-CONNACK disconnect cancellation; owner timeout; two-slot pending-auth bound; and refresh denial preventing delivery to an existing subscriber.
- The same run also subscribed the fixed service principal to `$tp/state/v1/#`; a device connection produced an online observation with sequence 1, and explicit disconnect produced offline sequence 2 with the same connection ID. The service checked principal, broker identity, boot ID and topic for both events.
- Raw per-layer evidence is `mqtt-qa-20261019.raw.ndjson`; independent assertions are `mqtt-qa-20261019.assertions.json`. Every tested ingress and state case is `INCOMPLETE` at application acceptance, terminal outcome, and side effects. `broker_ack=observed` is retained as its own transport layer and is never promoted to acceptance.
- Fixed artifact SHA-256 values are recorded in `mqtt-qa-20261019.sha256`.
- A separate read-only snapshot of the current GMQTT/backend container images, digests, ports, networks, mounts, resources, and checkout HEAD is in `gmqtt-g0-readonly-baseline.md`. No second GMQTT was started.
- The local native/plugin checks do not verify the database-backed owner, durable ingress acceptor, persistence across broker restart, production configuration, Linux image, capacity, or device compatibility.
- **LWT: FAIL / release blocker.** Upstream Mosquitto's Will path does not emit `MESSAGE_IN` and its OFFLINE callback runs before Will queuing; current disconnect handling revokes the grant before that publish. Do not claim Will delivery works until the plugin integration boundary is fixed and tested.
- **QoS 2: unsupported / not accepted.** QA fixture policy caps QoS at 1. No result here establishes QoS 2 behavior or metadata stability.
- **Production certification: NOT DONE.** All evidence above is loopback-only isolated development QA.
