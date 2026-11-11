# T1-A3 完成说明

分支：`wp/t1-a3-storage`  基线：`develop@d7fbfebf`（已包含 T1-A2、T3）

## 改了什么

- `internal/dal/telemetry_datas.go`：批量遥测重复键改为识别 PG/MySQL 唯一键错误，并分别用 `ON CONFLICT` / `ON DUPLICATE KEY UPDATE` 回写；MySQL 正确引用 `key`。
- `internal/dal/device_latest_telemetry.go`、`internal/service/device_latest_telemetry.go`：最新遥测汇总放行 MySQL，并按方言引用保留字 `key`。
- `internal/dal/telemetry_datas.go`：仅 PostgreSQL 执行 `VACUUM FULL`，MySQL 删除后跳过该语句。
- `internal/storage/history_deletion.go`、`internal/storage/history_intent_mysql.go`：历史删除领取按方言生成锁语法；MySQL 使用 `FOR UPDATE SKIP LOCKED`。
- 新增批量冲突、MySQL 最新遥测、跳过 VACUUM 和历史删除并发回归；`telemetry_broker.go` 未改。
- `docs/WORKSPACES.md`：登记本 worktree，并更新 A2/T3 的本地合并状态。

## 验证

- 双库批量冲突、当前值读取、最新遥测查询、MySQL 跳过 VACUUM：4 个定向用例通过，0 跳过、0 失败。
- MySQL 历史意图对比 PostgreSQL 并写入 TDengine：4 个随机种子通过；MySQL 幂等/重试/删除队列通过。
- PostgreSQL 与 MySQL 历史删除领取锁：两方均证明锁住的记录会被跳过，释放后可完成。
- `go test -count=1 ./internal/storage/...`：通过。
- `go test -count=1 ./internal/dal/...`：通过。
- `go test -count=1 ./internal/uplink/...`、`./internal/tdenginehistory/...`：通过。
- `go test -count=1 ./internal/app/... -run '^TestBrokerIngressControlPlaneRejectsMySQLPlatform$'`：通过；MySQL 平台不会启动 PostgreSQL 专属 broker 控制面 worker。
- 验证使用隔离测试容器。初始脚本数据库缺少基础表，已在同一临时 PG 容器新建 `tp_phase1_oracle`，按 `sql/1.sql` 至 `sql/33.sql` 和 service-access 迁移补齐后复跑；此前工作区、其他栈和业务数据未动。

## 没做 / 发现的问题

- 没修改 PG 迁移或 `internal/storage/telemetry_broker.go`。
- TDengine 页面 CSV 下载问题仍记录在 T3 完成说明；属于前端浏览器 Blob 下载路径，本卡没有改前端。
- A4 的 MySQL 与 PostgreSQL 整体平台冒烟及最终总结尚未完成。
