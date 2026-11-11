# P3 真实 Broker 验收记录

- 分支：`wp/p3-real-broker-20261105`，基于 `develop`。
- 覆盖：真实 Mosquitto + Paho 设备客户端下，正常设备 ACK 后 `GetCommandStatus` 为成功；设备收到第二条命令但不回 ACK，经 durable 超时 sweep 后 API 状态为失败，`command_set_logs` 列表投影为状态 `2` 并带明确超时原因。另断言无命令泄漏到另一设备身份。
- 验证证据：`/Volumes/KIOXIA/archive/evidence/2026-11-05-P3-real-broker/p3-native-e2e-run3.log` 显示 `TestDurableCommandNativeMosquittoPostgresE2E PASS`（7.08s）。本次只更新文档并沿用该既有结果，未重跑数据库测试。
- 证据限制：日志没有记录 Broker/PostgreSQL 的完整版本与运行模式元数据，不能据此复现或声明版本矩阵验收。
- 未覆盖：真实一分钟调度周期（测试回拨 durable 行时间并直接调用 sweep）；强制 Broker ACL 拒绝（只检查未授权身份收不到命令）；HTTP/UI 路径与界面验收。
- `git diff --check`：通过。
- 未推送；已在 2026-11-05 以 `--no-ff` 合入本地 `develop`。P3 worktree 与分支保留待负责人审查，不删除。
