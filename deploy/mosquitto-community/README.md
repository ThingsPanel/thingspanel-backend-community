# Community Mosquitto 2.1.2 QA profile

This directory provides an isolated local QA broker and a repeatable Linux image build from the pinned source checkout. The local profile is not a production configuration: it permits anonymous MQTT, binds only to loopback, and uses a dedicated port and data volume. It contains no credentials.

## Pinned source and artifacts

- Source: `/tmp/tp-mqtt-20261019/mosquitto`
- Tag / commit: `v2.1.2` / `99fa50f30e325609394c324c8ff71cfbbe95d8ab`
- Local macOS arm64 binary: `/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto`
- Broker plugin headers: `/tmp/tp-mqtt-20261019/mosquitto/include` (`mosquitto_broker.h`, `mosquitto_plugin.h`)
- Include flag for the current source checkout: `-I/tmp/tp-mqtt-20261019/mosquitto/include`
- Linux plugin runtime mount target: `/mosquitto/plugins/libtp-mosquitto-plugin.so`

The upstream Docker Hub tag `eclipse-mosquitto:2.1.2` returned `manifest unknown` on 2026-10-19, so this profile builds from the pinned source. The local macOS executable is only for host-side PoC; a plugin `.so` must be built for the Linux container architecture and must use this pinned broker ABI.

## Run the Go grant fixture and native plugin PoC

The fixed-grant HTTP fixture binds only loopback and creates a random service token file with mode `0600`. It never prints the token. It deliberately supports `qa-deny`, `qa-timeout`, `qa-slow-*`, and `qa-refresh-deny` test identities; these are fixture cases, not production authentication.

```sh
go build -o /tmp/tp-mqtt-20261019/build/mqtt-community-qa ./test/mosquitto-community
/tmp/tp-mqtt-20261019/build/mqtt-community-qa fixture -listen 127.0.0.1:18084 -token-file /tmp/tp-mqtt-20261019/build/fixture.token
```

Build the current native plugin against the pinned source headers, then in another terminal start the plugin-enabled broker with the generated token-file path:

```sh
CGO_CFLAGS='-I/tmp/tp-mqtt-20261019/mosquitto/include -I/opt/homebrew/opt/cjson/include' \
  go build -tags mosquitto_plugin -buildmode=c-shared \
  -o /tmp/tp-mqtt-20261019/build/tp-mosquitto-next.so ./cmd/tp-mosquitto-plugin

TP_BROKER_OWNER_URL=http://127.0.0.1:18084 \
TP_BROKER_TOKEN_FILE=/tmp/tp-mqtt-20261019/build/fixture.token \
TP_BROKER_CELL=qa-community \
TP_BROKER_INSTANCE=mosquitto-plugin-qa \
TP_BROKER_MAX_PENDING=2 \
TP_BROKER_MAX_SESSIONS=128 \
/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto -c deploy/mosquitto-community/mosquitto.plugin.native.conf -d
```

Run the independent MQTT 3.1.1 and MQTT 5 packet-level checks:

```sh
/tmp/tp-mqtt-20261019/build/mqtt-community-qa test -broker 127.0.0.1:32884 -fixture http://127.0.0.1:18084
```

The tool checks live ingress envelope and byte round-trip, mixed-filter SUBACK authorization, internal/cross-device/retained publish denial, fixed-grant denial, bounded owner timeout/cancellation, MaxPending release, late response with reused ClientID, HTTP timeout, MaxPending, and refresh denial against an established subscription. Mosquitto 2.1.2's delayed-auth socket-close path bypasses DISCONNECT/CLIENT_OFFLINE callbacks, so immediate cancellation on pre-CONNACK disconnect is explicitly reported unsupported; the request is bounded by the driver's two-second timeout. It uses standard-library MQTT wire packets so both protocol levels are exercised without another client dependency. The HTTP fixture is a test oracle only; it does not prove a database owner integration.

The extended run tests immediate Will for MQTT 3.1.1 and MQTT 5: a nonce-bound grant emits one trusted ingress envelope and preserves the original Will bytes on its explicitly allowed wire topic. The matching offline state keeps the connection ID and uses sequence 2 after online sequence 1. Graceful disconnect does not emit Will; forged internal Will, expired grant, and refresh-revoked grant are denied. A delayed Will beyond the five-second closed-principal window was observed to fail closed; delayed Will is unsupported. Delayed Wills after broker restart remain unsupported and untested.

The extended run also verifies that a service can request a narrower filter within its grant, that an explicit shared group receives the rewritten `tp-internal/ingress/v1/...` event, and that an unlisted group is denied. The old `$tp/ingress/v1/#` internal namespace is denied. Mosquitto 2.1.2's topic tokenizer does not deliver `$`-prefixed internal topics to shared subscriptions; the contract uses the protected non-dollar `tp-internal/` prefix and explicitly denies device access to it.

Platform-retained checks cover multiple topics on two devices, live forwarding with MQTT 5 RAP=0, retained snapshots on new subscriptions, zero-byte clears, and no snapshot replay after clear. MQTT 3.1.1 has no RAP field and follows its native retain forwarding semantics. The fixture grants exact snapshot filters to keep device separation explicit.

The runner writes stage evidence as raw NDJSON; run the independent judge to create per-case assertions:

```sh
/tmp/tp-mqtt-20261019/build/mqtt-community-qa test -broker 127.0.0.1:32884 -fixture http://127.0.0.1:18084 -raw /tmp/tp-mqtt-20261019/build/run.raw.ndjson
/tmp/tp-mqtt-20261019/build/mqtt-community-qa ledger /tmp/tp-mqtt-20261019/build/run.raw.ndjson /tmp/tp-mqtt-20261019/build/run.assertions.json
```

The ledger tracks `planned`, `sent`, `broker_ack`, `ingress_seen`, `accepted`, `terminal`, and `sideeffects` independently. The judge keeps the result `INCOMPLETE` while downstream acceptance, terminal outcome, or side effects are unobserved. A QoS acknowledgement never implies application acceptance.

## Run the macOS arm64 PoC

The currently running host instance reads `mosquitto.native.conf`, listens only on `127.0.0.1:32883`, and writes its data/log/PID under `/tmp/tp-mqtt-20261019/build`.

```sh
/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto -c deploy/mosquitto-community/mosquitto.native.conf -d
DYLD_LIBRARY_PATH=/tmp/tp-mqtt-20261019/build/install/lib /tmp/tp-mqtt-20261019/build/install/bin/mosquitto_pub -h 127.0.0.1 -p 32883 -q 1 -r -t qa/smoke -m v2.1.2
DYLD_LIBRARY_PATH=/tmp/tp-mqtt-20261019/build/install/lib /tmp/tp-mqtt-20261019/build/install/bin/mosquitto_sub -h 127.0.0.1 -p 32883 -t qa/smoke -C 1 -W 3
```

For the portable Linux/Compose profile, build the image from the fixed source tree:

```sh
docker build --platform linux/arm64 \
  --build-arg MOSQUITTO_COMMIT=99fa50f30e325609394c324c8ff71cfbbe95d8ab \
  -f "$PWD/deploy/mosquitto-community/Dockerfile" \
  -t local/tp-mosquitto-community:2.1.2 \
  /tmp/tp-mqtt-20261019/mosquitto
docker compose -f deploy/mosquitto-community/compose.yaml up -d
docker compose -f deploy/mosquitto-community/compose.yaml ps
```

Compose publishes only `127.0.0.1:32883` by default. Set `TP_MQTT_PORT` to another unused loopback port if needed. It uses a dedicated named data volume and does not reference or restart GMQTT. The mounted `mosquitto.local.conf` is for isolated tests only.

## Plugin integration and rollback

For Linux/arm64 c-shared plugin QA, build the isolated ABI test image and run its MQTT checks:

```sh
deploy/mosquitto-community/build-linux-plugin-qa.sh
docker run --rm tp-mosquitto-plugin-qa:linux-arm64
```

The helper uses Go 1.26.2, fixed Mosquitto 2.1.2 headers and `libcjson-dev`; the fixture and broker share one container network namespace and both bind loopback. A per-run random token is group-readable only by the `mosquitto` group so it remains readable after Mosquitto drops privileges. The Linux `.so` loaded and registered callbacks; the MQTT matrix completed with one explicit unsupported immediate pre-CONNACK cancellation gap, so the command returns nonzero after writing its ledger. The image runs a local test oracle, not the database-backed owner integration. For production, mount the plugin read-only to `/mosquitto/plugins/libtp-mosquitto-plugin.so` and use `mosquitto.production.conf.example`: anonymous access stays disabled and the plugin path is explicit. Keep credentials in a protected runtime secret/config mechanism readable by the broker UID; none are embedded here.

To stop the Compose QA broker while retaining its test data, run:

```sh
docker compose -f deploy/mosquitto-community/compose.yaml down
```

The command intentionally preserves `tp-mosquitto-community-qa-20261019-data`. Do not use `down -v` unless the test owner explicitly intends to discard that isolated volume. To stop the native PoC, read `/tmp/tp-mqtt-20261019/build/mosquitto.pid`, verify that PID owns listener 32883, then send it `TERM`; do not signal a process selected only by a generic `mosquitto` name.
