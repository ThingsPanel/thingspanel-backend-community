# Legacy GMQTT isolated full-stack health smoke

This run verifies a bounded old-stack smoke through legacy GMQTT → MQTT Adapter → Telemetry Uplink → Storage batch → PostgreSQL SQL. It is an engineering health check only; it is not a performance comparison, SLO result, migration certification, or production authorization.

The fixture used the immutable old GMQTT image `registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-gmqtt:v1.1.7`, image ID `sha256:c3ad43c34675210012ffd5559dfe3574af4a42ea6f65465f6d1b32cfe4119660` (`linux/arm64`). GMQTT and a private Redis instance shared a temporary Docker network. The generated DB login had access only to the isolated QA schema and was limited to four connections. The broker was published on loopback only. No existing GMQTT, public backend, device, or Redis instance was used.

The profile was fixed at 100 logical devices, one QA tenant, 100 telemetry messages/s for 5 seconds, payload sizes 512 B/4 KiB/64 KiB at 90/9/1 distribution, QoS 1, and CleanSession=true. The fixture now includes an empty `expected_datas` table required by the legacy status uplink. The run output contained no application `level=error` or SQL errors. A raw MQTT 3.1.1 readiness probe returned successful CONNACK `20020000` before starting the Adapter.

Result: 500/500 planned and sent, 500 broker PUBACK, 500 Adapter→Uplink observations, and 500 SQL rows. Wrong-device identity, duplicate Adapter/Uplink observation, and extra SQL row counters were zero. Accepted remains `N/A` because this legacy path has no durable acceptance transaction; broker acknowledgement is not accepted delivery. Effect execution is unobserved.

The harness was hardened to wait for a valid protocol CONNACK because earlier attempts demonstrated intermittent EOF when readiness checked only TCP accept. This run succeeded after the protocol readiness check, but the earlier EOF cause remains unproven and stability requires repeated runs. The successful run is not a paired old/new comparison.

Re-run:

```sh
TEST_MQTT_LEGACY_GMQTT_E2E=1 TP_LEGACY_GMQTT_LOAD_DURATION=5s go test ./internal/app -run '^TestBrokerIngressLegacyGMQTTOwnerPostgresHealth$' -count=1 -v
```

Evidence identity: workspace HEAD `0e3ba8281f09c815dabcd03c634d17af845e950b` with concurrent dirty changes; legacy harness SHA-256 `19d3326ff108661b3caa917b01623a634c15de174e9989a3ff4250d8102bf29d`. Ledger raw SHA-256 `e87116faeda8210bd8dd9c6908fe3b5725e05fc96f14e5a200b1f2afefff9a92`; deterministic gzip SHA-256 `5362c23f5c20f0bf70e47e74bbcdf3ab2b01da9789e2837d817f3f448b42701d`. Recompute the gzip digest with `gzip -n -9 -c /tmp/tp-mqtt-20261019/build/legacy-gmqtt-full-stack-load-3c9fd78976bc56ce.ndjson | shasum -a 256`.

An earlier 500-row run with the same profile is preserved separately as telemetry-only-with-fixture-gaps. Two earlier attempts saw EOF before valid CONNACK; logs are in `/tmp/tp-mqtt-20261019/build/legacy-gmqtt-06ad623e55913395.log` and `/tmp/tp-mqtt-20261019/build/legacy-gmqtt-d3ab202db944a2af.log`.
