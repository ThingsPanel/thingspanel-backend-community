# Linux command-frame MQTT QA (2026-10-19)

This is an isolated fixture run against the locally built Mosquitto 2.1.2 Linux/arm64 image. It is not a database-backed owner, production broker, or end-to-end device installation test.

## Fixed artifact identity

- Mosquitto source: tag `v2.1.2`, commit `99fa50f30e325609394c324c8ff71cfbbe95d8ab`.
- Broker image ID: `sha256:c103ceeb61129b0dae7e5c54d8545587b225956ceaed313e32f720aec122360e`.
- QA plugin image ID: `sha256:1b260b072d2dd9dbfc723ef4fa3bf7db8603159751169cc0c1b2972b4d40f62b`.
- Plugin SHA-256: `e93d4f165f40724b3fcdc296eed679114a93f137e3b7dadd9d503c1f4d09469a`.
- QA runner SHA-256: `9fc086f9af59c9d650859651847532dd20061e7da918f30d35ac2cf09e599bad`.
- Runtime toolchain: `go1.26.2 linux/arm64`; module declares Go 1.23.0 / toolchain 1.23.8.
- Owner fixture and broker share a container network namespace; owner HTTP binds only to `127.0.0.1:18084`; MQTT binds only to `127.0.0.1:1883`.

## Command-frame observations

The runner exercised MQTT 3.1.1 and MQTT 5 independently. A fixed service grant published a standard command storage frame on `devices/command/device-a/{command-id}`. The device subscribed to `devices/command/device-a/+`. Both protocol runs verified that the subscriber received the original opaque binary payload byte-for-byte and did not receive the internal frame marker. A device-b principal was denied the device-a filter. A stale route-epoch frame received a broker QoS1 acknowledgement but was not delivered to the device. A bare, unframed canonical command was denied and not delivered.

After the plugin's two-stage authorization fix, Linux MQTT 3.1.1 and MQTT 5 both passed persistent-session queue checks: an unexpired frame was delivered with exact wire bytes after reconnect; an expired offline frame and a wrong-route-epoch offline frame were not delivered; a queued frame was not delivered after reconnecting the same session under a different device principal; and a queued frame was not delivered after reconnecting under a grant without command-subscribe permission. Immediate command delivery, cross-device filter denial, online stale-route suppression, and bare command denial also passed.

The broker QA config enables local persistence with a one-second autosave. For both protocol versions, the test queued a frame, waited for autosave, restarted Mosquitto, and checked the resumed persistent session. A frame that expired during the restart returned no payload. In separate restored sessions, replay under a different device principal and under a device grant without command-subscribe permission returned no payload. These checks prove broker/plugin enforcement across the isolated persistence restart; they do not model an OS crash, power loss, external owner state, or concurrent device handover.

The fixture confirms broker/plugin behavior only. Owner-derived tenant/device scopes against live DB state, application acceptance, device actuation, and side effects remain unverified. The command-Will injection check is included in the MQTT 3/5 Will matrix and verified that an abrupt-close private command frame did not reach the command subscriber.

## Other results and limitation

The same Linux run completed the existing ingress, ACL, state, retained, Will, and shared-subscription checks. It reported the expected pre-CONNACK disconnect cancellation limitation: on this upstream 2.1.2 path the client close does not immediately cancel the owner request; the hard timeout bounds the request instead. Delayed Will remains unsupported. One earlier run in this build sequence failed the MaxPending assertion; subsequent runs passed it. Therefore the concurrency-cap result is timing-sensitive and requires a barrier-controlled overlap test before acceptance. Before the two-stage command ACL fix, offline messages were not queued; after the fix, MQTT3/5 persistent queue, TTL, and controlled broker restart checks pass.

Raw stage ledger and assertions:

- `mqtt-community-linux-arm64-command-qa.raw.ndjson`
- `mqtt-community-linux-arm64-command-qa.assertions.json`

Their SHA-256 values are recorded in `mqtt-community-linux-arm64-command-qa.sha256`. The stage ledger intentionally leaves `accepted`, `terminal`, and `sideeffects` unobserved. Broker PUBACK is not treated as business acceptance.
