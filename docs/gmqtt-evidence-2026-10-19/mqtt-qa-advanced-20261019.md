# Extended live MQTT QA evidence

Run date: 2026-10-19. The test broker is the pinned Mosquitto 2.1.2 macOS arm64 build on `127.0.0.1:32884`; a fixed-grant HTTP oracle binds only `127.0.0.1:18084`. The plugin was freshly compiled from the current shared Go source into `/tmp/tp-mqtt-20261019/build/tp-mosquitto-next.so`. No production service, database or Redis connection was used.

## Results

- MQTT 3.1.1 and MQTT 5 immediate abnormal-close Will: PASS. Each device has a fresh `ConnectionNonce`; the fixture derives `ConnectionID` from that nonce. Both the trusted ingress envelope and original wire topic subscriber received the Will, and the original wire payload bytes matched exactly. The ingress metadata carried the same connection ID as online state seq 1 and offline state seq 2.
- MQTT 3.1.1 and MQTT 5 graceful DISCONNECT: PASS; no Will was delivered.
- MQTT 3.1.1 and MQTT 5 internal-topic forged Will: PASS; neither trusted ingress nor raw Will reached the subscribed service.
- Expired grant and refresh-revoked grant immediate Wills: PASS; no Will reached the authorized raw or ingress subscriptions.
- MQTT 5 delayed Will with a seven-second delay: fail-closed observation only; no delivery occurred after the five-second closed-principal window. Delayed Will is unsupported. Wills restored after broker restart were not tested and remain unsupported.
- Connection identity: PASS. Reusing a wire ClientID for a new authenticated connection produced a new nonce and connection ID. Refresh for one live connection preserved the original nonce and ID.
- Service filter authorization and shared delivery: PASS for MQTT 3.1.1 and MQTT 5. A service granted `$tp/ingress/v1/#` can request a narrower device filter; an out-of-scope wire filter and the legacy `$tp/ingress/v1/#` internal prefix are denied. A separately granted shared group delivered the rewritten `tp-internal/ingress/v1/...` envelope. The unlisted group was denied. This verifies the revised non-dollar internal prefix against Mosquitto 2.1.2's shared-subscription matcher.
- Platform-retained behavior for MQTT 3.1.1 and MQTT 5: PASS across two devices and three topics. A live subscriber saw retained writes and clears with retain=false; a new subscriber saw stored snapshots with retain=true before clearing; empty retained publishes removed each snapshot; later subscribers received no replay. Device-specific snapshot filters prevented cross-device delivery. MQTT 5 SUBSCRIBE explicitly used RAP=0; MQTT 3.1.1 has no RAP option.

The packet-level runner exits nonzero on any failed assertion. `mqtt-qa-advanced-20261019.raw.ndjson` and `mqtt-qa-advanced-20261019.assertions.json` hold its stage ledger. The judge marks every case `INCOMPLETE` at application `accepted`, terminal outcome and business side effects because this fixture does not run the database-backed durable acceptor. Broker acknowledgements remain separate from acceptance.

The pinned native broker and plugin hashes, QA binary, raw ledger and assertions are listed in `mqtt-qa-advanced-20261019.sha256`. `go test -race -count=1 ./test/mosquitto-community ./internal/brokercontract ./internal/mosquittodriver` passed after the QA additions.

QoS 2 remains unsupported/not tested. Linux container image build and Linux plugin ABI remain under verification. This is isolated local QA, not production certification.
