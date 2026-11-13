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

## R2E 发现的范围外问题（2026-11-12）
- [ ] `deploy/local-develop/build.py` 的 `encore_command` 在 `repo / 'encore-api'` 里直接 `encore build docker`；如果 `repo` 是 git worktree 会因 Encore CLI 的 `WorkspaceRoot` 死循环而永久卡住（见 `docs/tasks/R2E-ENCORE-BUILD-DONE.md`）。建议给它加超时，或改成先导出干净副本（参考 `scripts/build-encore-image.sh`）。
- [ ] 同一处 `--services` 只列了 7 个，CI（`encore-image-build.yml`）是 10 个（多 `marketinstall,northbound,platformconfig`）；本地与发行版镜像内容不一致，需要确认是否有意。
- [ ] Encore CLI 当前 v1.58.6，v1.58.8 已发布；`WorkspaceRoot` 的 `.git` 为文件时死循环，应向上游 encoredev/encore 报告，并确认新版是否已修。
## R2G 发现（2026-11-12）
- [ ] `encore-image-build.yml` 的 `--services` 清单里没有 `system`（`GET /api/system/info`）；`deploy/community-test/deploy_encore.py` 的 `native_services` 也没有 `system` 和 `brokerscope`。镜像要带上它们，社区测试部署才能用。
- [ ] 设备数据仍在旧平台库，`brokerscope` 只是只读借道；设备迁到 `device_metadata` 后，把 `internal/brokerscope.Query` 改为读新表（A5 的终态）。
- [ ] `encore-api/devicemetadata/legacydb.go` 用裸 pgx 连 `TP_LEGACY_DATABASE_URL`，不在 GORM/`relational` 的约束内；迁移用途，随旧库退役删除。
- [ ] `relational.EnvSource` 的键是 `TP_RELATIONAL_<驱动>_PLATFORM_*`，平台（Gin，viper）与 Encore 的前缀协调仍未做（同 PROJECT-AUDIT 的 A1 备注）。


- [R-series, 2026-11-12] `go test ./internal/api/` 里 `TestTelemetryHTTPDeviceAuthorization` 在 develop 0b618560（R0 之前）就失败，与 R0–R2 无关，未修。
- [R4S, 2026-11-12] schema 校验的 CHECK 文本规范化只覆盖 PG 14.5 观察到的 varchar `IN` 数组还原形态；换 PG 15/16 后若出现新的还原形态，需要在 `canonicalizeRestoredArrayAny` 补充并加测试。更稳的长期做法是用语义比较（如试探性插入或 `pg_constraint.conbin` 解析），不在本卡范围。
- [R5a, 2026-11-12] 安装器 `tests/test_plugin_bundle_activation.py` 有 7 个测试在安装器 develop 上就失败（"Downloaded plugin helper does not match the installer pin: tools/plugin_catalog.py"），与 R5a 无关，未修。
- [R5a, 2026-11-12] `deploy/community-test/deploy_encore.py` 的 `native_services` 清单不含 `brokerscope`（R2G 说明里已提；CI 工作流和 `scripts/build-encore-image.sh` 已补）。
- [R5a, 2026-11-12] Agent 的 `system.json` 由 Agent 二进制生成：升级 Agent 后需重新 `agent_host.py up --system` 才会刷新配置；R5b 的升级编排要包含这一步。
- [R5b, 2026-11-13] `system upgrade` 的 `verifying` 阶段 kill -9 和 `needs_human` 路径只有单元测试，没在真实栈上演练（原因见 `docs/tasks/R5b-DONE.md`）；要真实覆盖，需要一个“健康但不被 compose 依赖”的慢组件，或在健康门里加最小停留时间。
- [R5b, 2026-11-13] `update.py` 没有把 `--release-source/--release-trust-root` 传给 `agent_host.py`；升级发布源和信任根如何接进 install/upgrade 脚本待负责人决定。
- [R5b, 2026-11-13] 后端 `CleanSystemDataByCron`（数据保留，默认 15 天）会删除旧时间戳的遥测，做备份/恢复/升级演练的合成数据必须用当前时间戳。

