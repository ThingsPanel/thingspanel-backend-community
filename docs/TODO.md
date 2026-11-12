# TODO

- [ ] 目录整理
- [ ] 数据转发
- [ ] 安装器 compose 仍传 `GOTP_STORAGE_TDENGINE_EXPERIMENTAL`；后端已不再读取，后续可从 compose 清理该无效环境变量。

## PH1-S1 发现（2026-11-05）
- 后端：Broker 对平台命令发布返回 PUBACK 拒绝码（如 rc135）时，命令状态仍为 `broker_acknowledged`，应直接 `failed` 并带原因。
- 安装器：全新目录 `prepare.py --bootstrap` 因 `$` 校验失败（未插值模式下密码带 `${…}`）；`--verify` 硬编码 19091/503 端口。
- Encore：`encore build docker` 对 `encore-api/` 在本机 CPU 100% 无输出挂起（≥21 分钟），`encore check` 正常；前端设备页依赖 Encore `/api/v2/*`。
- 新建设备后需 `--sync-scope`，否则平台命令被 Broker 拒绝。

## 数据库阶段待办
- [ ] `TestTelemetryHTTPDeviceAuthorization/owner_key` 在基线 `origin/develop@c0ab14d1` 上同样失败（`code=40103 allowed=true`），属于原有问题，和本阶段无关。
- [ ] 方言分支散落在函数里（如 `internal/dal/telemetry_datas.go`、`internal/automationguard/guard.go`、`internal/dal/insert_if_absent.go` 中的 `Dialector.Name()=="mysql"`）；按架构规则 A2，后续收拢到所属 repository 的 PG/MySQL 显式实现。
- [ ] `telemetry_datas.go` 的 MySQL `ON DUPLICATE KEY UPDATE ... VALUES(col)` 写法在 MySQL 8.0.20 后不推荐，后续改为行别名写法。

## R2G 发现（2026-11-12）
- [ ] `encore-image-build.yml` 的 `--services` 清单里没有 `system`（`GET /api/system/info`）；`deploy/community-test/deploy_encore.py` 的 `native_services` 也没有 `system` 和 `brokerscope`。镜像要带上它们，社区测试部署才能用。
- [ ] 设备数据仍在旧平台库，`brokerscope` 只是只读借道；设备迁到 `device_metadata` 后，把 `internal/brokerscope.Query` 改为读新表（A5 的终态）。
- [ ] `encore-api/devicemetadata/legacydb.go` 用裸 pgx 连 `TP_LEGACY_DATABASE_URL`，不在 GORM/`relational` 的约束内；迁移用途，随旧库退役删除。
- [ ] `relational.EnvSource` 的键是 `TP_RELATIONAL_<驱动>_PLATFORM_*`，平台（Gin，viper）与 Encore 的前缀协调仍未做（同 PROJECT-AUDIT 的 A1 备注）。

