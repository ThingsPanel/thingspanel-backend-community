# Linux broker image strict runtime smoke — 2026-10-20

This is isolated QA evidence for the frozen Linux production image. It is not a deployment or a physical-device acceptance result.

## Frozen image identity

- Image: `thingspanel-community-broker:ingestion-20261020`
- Image ID: `sha256:ac1a075beb345ea8475d385d2b37d0af9a5bde5cbdcc66ae906f0f061b07fa8e`
- Platform: `linux/arm64`
- Mosquitto source commit: `99fa50f30e325609394c324c8ff71cfbbe95d8ab` (Mosquitto 2.1.2)
- Source checkout HEAD at freeze: `0e3ba8281f09c815dabcd03c634d17af845e950b`
- Frozen build-context SHA-256: `9bc17da9f2a25947e7ecabbb9eef590018904981074c520b18a39e22581cc5b0`
- Frozen context file count: 747

The file-level source hash manifest is [available here](gmqtt-evidence-2026-10-20-context-manifest.json), and the redacted build/runtime inspection record is [available here](gmqtt-evidence-2026-10-20-build.json).

## Runtime verification

The test-only Docker fixture ran the exact image above with its bundled Linux Mosquitto executable and c-shared plugin. It used UID/GID 1883, a read-only root filesystem, a UID/GID 1883 tmpfs for broker data, and read-only mounts for a generated QA token, broker config, and a short-lived self-signed QA CA. The broker reached the existing real Owner `RuntimeHandler` through a TLS reverse proxy at `host.docker.internal`; certificate verification remained enabled. Route fencing was enabled in both broker and backend configuration.

These checks passed against the isolated PostgreSQL/Redis QA fixtures:

- `TestBrokerIngressNativeMosquittoOwnerPostgresE2E`: real broker/plugin, MQTT clients, Owner HTTP, Adapter, durable ledger/effects and dispatcher path; the runtime fence gathered all four runtime acknowledgements and a post-fence PUBLISH was denied without a new receipt.
- `TestBrokerIngressNativeReceiptSurvivesSubscriberDisconnectBeforeAck`: the service subscriber committed a receipt and disconnected before MQTT ACK; the same persistent session redelivered through the image plugin, returned the same receipt, and processing/effect counts remained one.

The test broker config explicitly set `persistence false` and used tmpfs. This validates the runtime ABI/control path and ingress redelivery behavior; it does not validate on-disk broker persistence or reboot recovery. The QA TLS proxy validates certificate-verified Owner RPC to the image plugin, while the separate HTTPS Owner smoke recorded in the build JSON only validates the Owner API. Neither result constitutes production deployment, multi-cell migration completion, customer traffic proof, or physical-device behavior.

The earlier Linux ASan run used an older source/image hash and does not cover this image’s pre-connect route registration or `ingestion_id` changes.

## SBOM and license inventory

Syft 1.54.1 was downloaded to a temporary tools directory and verified against both GitHub release asset metadata and the official checksum file before extraction. It scanned the local image by immutable image ID without publishing data. SPDX 2.3 contains 129 packages and 3,176 files; the Syft JSON contains 128 artifacts (93 Debian package artifacts and 35 Go-module artifact entries across image executables). The four Mosquitto/community license files listed in the production image build record were individually copied from an unstarted container and verified nonempty by SHA-256.

The full SPDX and Syft outputs, source Go module graph, c-shared plugin build metadata, and tool/image provenance are in [the SBOM evidence directory](gmqtt-evidence-2026-10-20/sbom/). Syft recognizes the plugin's local `project` module and Go standard library, with no external Go module dependency in that c-shared build; the additional 272-entry `go list -m all` inventory is the broader frozen-context module graph and does not claim every module was linked into the plugin. This SBOM is an inventory, not a vulnerability scan or a legal conclusion.
