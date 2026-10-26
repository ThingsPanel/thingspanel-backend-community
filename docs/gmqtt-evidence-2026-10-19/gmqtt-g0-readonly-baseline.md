# G0 read-only environment baseline

Captured 2026-10-19 from local Docker metadata and the current checkout. This records the existing runtime only; it does not certify compatibility or change service state. Container environment values and credentials were not collected.

## Existing GMQTT

- Container: `thingspanel-install-gmqtt-1`, running since `2026-10-18T02:42:34Z`.
- Image tag/version: `registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-gmqtt:v1.1.7`; process is `./gmqttd start -c /gmqttd/default_config.yml`.
- Local image ID: `sha256:c3ad43c34675210012ffd5559dfe3574af4a42ea6f65465f6d1b32cfe4119660`.
- Registry digest: `registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-gmqtt@sha256:37f3d5195cd850e09af92e9f3b3dc9866527e20ccb2316b2799ef82cd82fa571`.
- Platform: Linux arm64.
- Published ports: container 1883, 8082 and 8083 bind to `0.0.0.0` and `::`; container 8084 and 8883 are not published.
- Network: `thingspanel-install_thingspanel_network`.
- Mount: named volume `thingspanel-install_gmqtt` at `/gmqttd`, read-write.
- Docker resource limits are unset (CPU quota and memory limit are zero). At capture, Docker reported 0.00% CPU, 17.84 MiB memory and 13 processes.

## Connected ThingsPanel backend

- Container: `thingspanel-install-backend-1`, running since `2026-10-18T02:42:36Z`.
- Image tag/version: `registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-go:v1.2.11`.
- Local image ID: `sha256:13cf7d105c092e6d7645e1bfb22f2924773eba4d26a5f8df565e64ea405fd4d8`.
- Registry digest: `registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-go@sha256:8e3679fb0089852ab1bb8edbcc79c5b1284fff2265014f5181451638794b136f`.
- Platform: Linux arm64; container port 9999 publishes to all host interfaces.
- Network: `thingspanel-install_thingspanel_network`.
- Read-write named volumes mount at `/go/src/app/configs` (`thingspanel-install_go_configs`) and `/go/src/app/files` (`thingspanel-install_go_files`).
- Docker resource limits are unset. At capture, Docker reported 0.03% CPU, 29.04 MiB memory and 9 processes.
- Current source checkout HEAD: `0e3ba8281f09c815dabcd03c634d17af845e950b`. This is a source revision, not proof that the running image was built from that commit.

## QA boundary

No secondary GMQTT was started. The running service already publishes broker ports publicly and is connected to the existing installation network; this baseline did not copy its config or data, attach another broker, inspect environment values, or access the configured database/Redis. Existing GMQTT and backend remained running. Read-only raw Docker inspect output was stored outside the repository under `/tmp/tp-mqtt-20261019/build/` with mode `0600` and contains private container metadata.
