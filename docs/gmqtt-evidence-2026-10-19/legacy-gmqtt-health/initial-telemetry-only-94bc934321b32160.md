# Legacy GMQTT isolated health run: initial telemetry-only evidence

This archived run exercises the old GMQTT image through the legacy Adapter, Uplink, Storage batch, and a private PostgreSQL schema. It is not a performance comparison, SLO result, or production certification.

The run used the fixed image `registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-gmqtt:v1.1.7` (`linux/arm64`, image ID `sha256:c3ad43c34675210012ffd5559dfe3574af4a42ea6f65465f6d1b32cfe4119660`), an isolated Docker network and Redis, and a generated PostgreSQL role limited to the QA schema. The broker port was published only on loopback. The profile was 100 device connections, one QA tenant, 100 telemetry messages/s for 5 seconds, 90/9/1 payload sizes of 512 B/4 KiB/64 KiB, QoS 1, CleanSession=true.

The archived ledger reports 500/500 sent, broker PUBACK, Adapter/Uplink observations, and terminal SQL rows; wrong-device identities, duplicate observations, and extra SQL rows were zero. The legacy path has no durable acceptance transaction, so accepted is explicitly `N/A`; PUBACK is not treated as acceptance. Effect execution is unobserved.

This first successful ledger is **telemetry-only with fixture gaps**, not a clean full-stack health pass. `StatusUplink` logged repeated `expected_datas`-missing SQL errors because the private schema initially omitted that application table. A later fixture change adds the empty `model.ExpectedData` table; a clean rerun is required before declaring the isolated legacy health check pass. A subsequent run also saw intermittent MQTT CONNECT EOF before the Adapter connected. In the successful run, a raw MQTT 3.1.1 probe returned CONNACK `20020000`; in failed runs the same probe returned EOF. The cause is not yet established. The current harness now waits for a successful raw CONNACK rather than relying on TCP accept as readiness; this hardening has not yet been validated because shared-tree compilation is currently blocked by unrelated edits.

Raw ledger: `initial-telemetry-only-94bc934321b32160.ndjson.gz`.

- Raw NDJSON SHA-256: `1539166a08faf7cc4c5f16167363951dc908390bc1ab4db1370d1abc96f07123`
- Deterministic gzip SHA-256: `053ca8eacd03b9103190d2b33175c037b38ca71509eca5215031cf5c09493abd`
- Recompute: `gzip -n -9 -c /tmp/tp-mqtt-20261019/build/legacy-gmqtt-full-stack-load-94bc934321b32160.ndjson | shasum -a 256`
- Source file: `internal/app/broker_ingress_legacy_gmqtt_e2e_test.go`; this was an initial dirty-worktree run, not a frozen-source certification.
