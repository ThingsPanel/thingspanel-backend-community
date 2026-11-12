# R1 交付说明：健康门 + system 角色骨架（2026-11-12）

后端分支 `wp/R1-health-gate`（从后端 `develop` 0b618560 切出）+ 安装器分支 `wp/R1-system-role`（从安装器 develop 切出，**develop 已包含用户合入的 `codex/matter-agent-generic-storage`，合并提交 0ef9603**；安装器 worktree：`thingspanel-installer/.claude/worktrees/wp-r1-system-role`，最后提交 82a33d7）。都没有叠在别的未合入分支上。

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

## 安装器部分（Agent system 角色）
- `agent/system.go`：固定配置（compose 项目名 + 组件清单，文件不能被组/其他人写，未知字段拒绝）、`Verifier`（逐个组件检查：容器存在、运行、Docker 健康状态，可选固定的 HTTP/TCP 探测；不在第一个失败处停；原因是固定代码，不含错误文本/地址）、`wait_seconds` 轮询。
- `agent/system_docker.go`：只读 Docker 客户端，只发“按 compose 项目标签列容器”和“inspect”两个 GET。
- `agent/system_service.go`：独立的 `http.Handler`，**只有** `/v1/system/status`、`/v1/system/verify`；独立 token；请求里不能带命令/镜像/URL（未知字段 400）。
- `cmd/tp-agent`：新增 `-system-socket/-system-token-file/-system-config`（三个必须一起给；两个 token 必须不同，socket/token 文件不能和 plugin 的相同，否则启动失败）。plugin 角色的代码路径一字未改。
- `cmd/tp-agentctl`：`system status|verify [wait_seconds=N]`；栈不健康时退出码为 1。
- `agent/system-config.example.json`、`agent/README.md` 增补。

## 测试
**单元测试**（Linux 容器 `golang:1.22` 内运行，Agent 包是 `//go:build linux`，Mac 上不能直接测）：新增 9 组用例（全部健康、点名全部故障组件且不泄漏错误文本、health 各状态、Docker 读取失败、等待到健康/预算耗尽、配置校验、配置文件权限、system API 认证与固定路由、plugin 处理器无 system 路由且拒绝 system token、token 必须不同）+ 原有测试全部通过（`go test -count=1 . ./cmd/...`；原有 `*_real_test.go` 仍按原来的开关跳过）。

**真实演练**（隔离栈，容器和网络都加 `r1-` 前缀，已清理；日志：`/Volumes/KIOXIA/archive/evidence/2026-11-12-R1/drill.log`、`drill2.log`）：栈由 `r1-postgres`（timescaledb:14，真实健康检查）、`r1-redis`、`r1-backend`（nc 起的假 HTTP 服务）加一个真实的 `tp-agent` 容器组成。
- 健康时 `system verify` 退出 0；
- `docker stop r1-postgres` 后 `verify` 退出 1，并指出 `postgres: failed / not_running`（backend、redis 仍 ok）；再停 backend 则两者都列出；
- 重新启动后 `verify wait_seconds=60` 在约 2.1 秒内转为 ok；
- plugin token 打 system socket → 401；system token 打 plugin socket → 401；plugin socket 上的 `/v1/system/status` → 404。
- 第一次演练里假 backend 因 busybox 缺 `httpd` 起不来（演练环境问题，不是产品问题），换成 nc 后重跑得到上面的结果。

## 没做 / 未验证
- 演练栈是**简化栈**（3 个容器），不是真实的 ThingsPanel 整栈；真实栈上的探测 URL 只写在示例配置里，没有实测（特别是 encore-api / notification-core 的 `/__encore/healthz` 端口，以及 Agent 要加入栈网络）。
- Agent 容器启动 system role 的接线（`tools/agent_host.py` 加参数、生成 system token/config）**留给 R2**（R2 要把 Agent 变成系统服务，一起改）。
- socket 权限 0600 在 Docker Desktop 挂载卷上显示为 777（文件系统不保留权限），代码里用的是 umask 0177，**权限没有在真实 Linux 上验证**。
- 没有为 encore-api / notification-core / mosquitto 往 compose 里加 healthcheck：encore 镜像没有 shell，容器内无法做；改用 Agent 侧的 HTTP/TCP 探测。
- `/health` 的 503 和容器 healthcheck 的联动没有在真实栈验证。
- 另见下面的后端部分“没做”。

## （后端部分）没做 / 未验证
- Agent system 角色（见上，被冲突决定阻塞）。
- 没有在真实栈上停库验证 `/health` 的 503 和容器 healthcheck 的联动。
- `/api/system/info` 没有在真实 Encore 运行时下用真实 token 调用过，只有角色判断的单元测试。
- encore-api / notification-core / mosquitto 的 compose healthcheck 还没补（在安装器仓库的 compose 里，R1 的 Agent 部分一起做）。
- 与 T1-A1 的冲突：本卡只改了 `internal/api/system.go` 的 `HealthCheck`，没动启动流程。

## `encore check` 结论
编译通过（1 分 42 秒），Encore 注册了 273 个接口，说明 `encore:api` 注解被解析通过。随后的“启动应用并等 healthz”一步失败：`notification` 服务报 `notification privacy configuration is unavailable`（本机没有配置隐私/密钥环境变量），与本卡改动无关。所以**编译和注解解析已验证，运行时行为未验证**。
