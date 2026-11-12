# R1 交付说明：健康门（**仅后端部分**）（2026-11-12）

分支 `wp/R1-health-gate`，从后端 `develop`（0b618560）切出。**不叠在其他分支上。**
状态：**R1 只完成了后端一半**。Agent 的 system 角色（socket/token、`/v1/system/status|verify`、`tp-agentctl system ...`）在安装器仓库，按用户决定“先把安装器分支 `codex/matter-agent-generic-storage` 合入 develop 再做”，**尚未开始**。所以 R1 的完成标准（停库后 `verify` 指出哪个组件失败；plugin token 调 system 被拒）**没有达成，也没有验证**。

## 做了什么
1. `internal/health`：并行无关、各自超时（2 秒）、不在第一个失败处停下的探针执行器；失败原因是固定代码，**不含错误文本、地址、密码**；探针 panic 视为失败。
2. `/health`（`internal/api/system.go`）：现在检查 ① 数据库 `Ping` ② Redis `Ping` ③（仅 PostgreSQL）`sys_version` 账本版本等于程序的 `global.VERSION_NUMBER`。全部通过 → 原来的 200 包裹格式；任何一项失败 → HTTP **503** + 每个组件的 `name/ok/reason`。容器 healthcheck（`wget /health`）因此会在库或 Redis 掉线时变为 unhealthy，这是预期行为。
3. Encore `system` 服务：`GET /api/system/info`（需登录，仅 `SYS_ADMIN`），返回平台版本（环境变量 `TP_PLATFORM_VERSION`，缺省 `dev`）和启动时间。**只报告，不执行任何操作。**

## 与 R0 盘点的更正
R0 写“健康门检查 `sys_version` 等于程序版本，要小心 `SYSTEM_VERSION` 默认 `dev`”——**不准确**。实际的库表版本账本是 `sys_version.version_number`，对比的是 `global.VERSION_NUMBER`（当前 25），与发布字符串 `SYSTEM_VERSION` 无关。另外编号 26–35 的 `sql/*.sql` 不由这个账本管理，而是各自组件的 receipt（如 33↔pluginidentity、34/35↔plugininstall）。因此 `/health` 的“schema”**只覆盖旧账本**，其他迁移来源**还没有汇总**——`/api/system/info` 现在也不返回它们。这是留给后续的缺口。

## 测试（全部是单元测试，不是整栈验收）
```
go test ./internal/health/                         # 5 个用例：全部失败时都列出、超时、panic、版本不一致、错误文本不泄漏
go test ./internal/api/ -run TestHealthCheck       # 无库无 Redis 时 503 且点名 database、redis
cd encore-api && go test ./system/                 # 角色判断
cd encore-api && encore check                      # 见下
```
结果见提交时的运行记录；`encore check` 的结论写在文末。

## 没做 / 未验证
- Agent system 角色（见上，被冲突决定阻塞）。
- 没有在真实栈上停库验证 `/health` 的 503 和容器 healthcheck 的联动。
- `/api/system/info` 没有在真实 Encore 运行时下用真实 token 调用过，只有角色判断的单元测试。
- encore-api / notification-core / mosquitto 的 compose healthcheck 还没补（在安装器仓库的 compose 里，R1 的 Agent 部分一起做）。
- 与 T1-A1 的冲突：本卡只改了 `internal/api/system.go` 的 `HealthCheck`，没动启动流程。

## `encore check` 结论
在本机运行超过 120 秒未返回（整个 Encore 应用较大），**没有拿到结果**，因此 `encore:api` 注解是否被 Encore 解析通过**未验证**；只有 `go vet`/`go test ./system/` 通过。需要在能跑完 `encore check` 的环境里补做。
