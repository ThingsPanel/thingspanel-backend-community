# Community broker production image

This profile builds Mosquitto 2.1.2 and `libtp-mosquitto-plugin.so` from the same source headers, then packages only the broker executable, plugin, fixed broker configuration, owner executable, and small readiness tooling. The image also contains the broker-owner executable; Compose runs it as a separate process using the same image. Both processes run as UID/GID 1883. The final container root filesystem can be read-only; only `/mosquitto/data` needs persistent write access.

The source commit is `99fa50f30e325609394c324c8ff71cfbbe95d8ab`. The Debian and Go OCI index digests, with their platform manifests, are recorded in `base-images.manifest`. The build pins the index digests. Debian apt packages are still fetched from the live Debian repositories during a build, so preserve the resulting image digest and build SBOM as release evidence.

## Build and inspect

Run these commands from the repository root after reviewing the source commit and base image manifest:

```sh
docker buildx build --platform linux/arm64 --load \
  -f deploy/mosquitto-community/Dockerfile.production \
  -t thingspanel-community-broker:reviewed .
docker image inspect thingspanel-community-broker:reviewed \
  --format '{{.Id}} {{json .RepoDigests}}'
docker save thingspanel-community-broker:reviewed | shasum -a 256
syft thingspanel-community-broker:reviewed -o spdx-json=community-broker.spdx.json
```

Record the image ID, archive checksum, SBOM, source commit, build platform, and builder version with the release. Inspect the final filesystem and confirm that it contains no repository checkout, source tree, token, service policy, DSN, or TLS key. The plugin `.so` is compiled with CGO against `/opt/mosquitto/include`, copied from the Mosquitto source build in the same Docker build.

## Stage runtime secrets

Provide three files through the deployment secret manager:

* Owner token JSON: `[]brokercontrol.TokenBinding`, binding the secret token to the chosen logical broker cell and broker instance.
* Service policy JSON: `[]brokercontrol.ServiceCredential`, with finite `AllowedClientIDs`, tenant/device scope for every service that can publish, and explicit subscriptions for `tp-internal/ingress/v1/#` and `tp-internal/state/v1/#` as needed.
* The raw plugin token corresponding to the owner token JSON entry.

The Compose example uses file-backed secrets for demonstration. Set their host ownership to numeric `1883:1883` and permissions to `0440`; verify readability as UID 1883 before launch. File-backed Compose secrets can retain host ownership and mode, so do not assume Compose rewrites them. Never put secret contents in the repository, image, or shell history. Supply `TP_BROKER_OWNER_DATABASE_DSN` through the deployment secret/environment mechanism supported by the target runtime.

The owner and broker share a network namespace. The plugin calls the owner at `http://127.0.0.1:9801`, which satisfies the driver's loopback-only cleartext HTTP rule. No owner port is published on the host. The broker's example listener is plaintext MQTT on 1883; place it on a private network or behind an MQTT-aware TLS termination layer before accepting remote device traffic. TLS passthrough requires a separately reviewed Mosquitto TLS listener and certificate configuration, which this example does not provide. The example deliberately leaves port publishing and the external TLS boundary to the operator.

The broker configuration has `allow_anonymous false` and an explicit mandatory plugin path. Do not override the config with a listener that omits either setting. Run with a read-only root filesystem, dropped capabilities, `no-new-privileges`, and a writable persistent `/mosquitto/data` volume.

## Readiness and authentication check

The owner healthcheck verifies only that its local TCP listener accepts connections; that is liveness and does not establish schema readiness. The broker healthcheck uses Paho with the explicitly allowlisted readiness client identity, authenticates against the running broker, and waits for a SUBACK on `tp-internal/state/v1/#`. Add `tp-community-readiness` (or the configured readiness client ID) to one service's `AllowedClientIDs`, and grant that service the state filter. This proves the owner grant path and an authorized subscription are working. It does not prove that a device can authenticate or publish.

Before production exposure, run a separate device-level acceptance using a dedicated test voucher and verify both the expected authorized topic and a denied topic. Avoid testing on shared production listeners. Capture the broker/plugin version, image digest, connection result, SUBACK, and the application receipt separately; a device PUBACK is protocol acknowledgement and does not prove application processing or physical effect.

## QA migration and rollback drill

Run `deploy/mosquitto-community/drill-migration-rollback-qa.sh` with `TP_BROKER_MIGRATION_DSN` pointing only at the dedicated database named `brokerqa`. The script creates a random `mqtt_qa_drill_*` schema, applies and reapplies all numbered migrations, checks the required columns and `broker_safe_voucher_v1(text)`, starts a temporary anonymous MQTT listener bound only to `127.0.0.1` on an OS-assigned port, performs a local subscribe/publish smoke test, then drops only its generated schema and stops only its child broker. `MOSQUITTO_BIN`, `MOSQUITTO_PUB`, and `MOSQUITTO_SUB` may select local test binaries. It never selects a device, changes a transport binding, accesses production ports 1883/9999, or exercises the production owner/auth path.

This drill validates schema repeatability and cleanup, not application deployment or route rollback. There is currently no supported broker route-control API/CLI in this repository to list/change a binding, drain a cell, atomically advance `route_epoch` and activate a destination, or roll traffic back with a fresh epoch. The `brokercontrol` owner resolves and validates bindings; it does not manage deployment transitions. Do not use ad hoc SQL updates as an operational substitute. Production cutover and rollback therefore remain a release blocker until an authorized, audited, monotonic-epoch control plane and its recovery procedure are implemented and exercised.

## Database and release evidence

Install and review the broker schema migrations `sql/broker/001_broker_owner_tables.sql` through the latest numbered file (currently `011_safe_device_voucher.sql`) using the normal database migration process before enabling ingress. The owner and application must use the intended tenant schema/search path. The binary does not auto-migrate production databases. Validate required columns, `broker_safe_voucher_v1(text)`, and indexes before enabling the `ingress_v1` input profile. Migration 011 creates the voucher expression index with ordinary `CREATE INDEX` when `devices` exists; schedule it in a maintenance window because index creation takes a write lock. If the broker schema is installed before the core `devices` table, rerun migration 011 after core bootstrap to create that index.

`check-broker-schema.sh --schema public` is read-only by default and returns nonzero when required columns are missing. Its apply mode is test-only: numbered migrations run in order only with `--apply --target qa --confirm-qa-apply`, a schema named `mqtt_qa_*`, and a connected database named exactly `brokerqa`. Production migration remains an explicit operator-controlled step through the normal migration process; this checker refuses apply for `public` and any other database. Reapplying the current `IF NOT EXISTS` DDL migrations to the same isolated schema is supported. The checker has no migration ledger or checksums and validates the required-column contract, not index definitions or production data; keep reviewing migration SQL and index state through the normal deployment process.

For each release retain the migration revision, image digest, checksum, SPDX SBOM, resolved base image manifest, secret file ownership/readability check, authenticated readiness result, device authorization test, and first application-level receipt. Keep receipt processing, broker acknowledgement, device acknowledgement, and physical command completion as separate evidence states. An `expired` or `broker_acknowledged` command state alone does not establish a physical outcome.

The runtime image preserves the pinned Mosquitto source license files at `/usr/share/licenses/mosquitto/LICENSE.txt`, `/usr/share/licenses/mosquitto/epl-v20`, and `/usr/share/licenses/mosquitto/edl-v10`; the repository `LICENSE` text is included as `/usr/share/licenses/mosquitto/thingspanel-community-LICENSE`. These are the original texts copied into the image.
