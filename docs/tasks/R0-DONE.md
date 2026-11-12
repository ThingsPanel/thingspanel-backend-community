# R0 交付说明：盘点与审定（2026-11-12）

分支 `wp/R0-restart-plan`（从 `claude/wp-r0-restart-plan-179c91` 改名，基于后端当时的 HEAD `0b618560`）。只写文档，无代码改动。

## 做了什么
1. 规划文档 `docs/RESTART-BACKUP-UPGRADE-PLAN-2026-11-12.md` 提交进仓库（原文，状态为“已定”）。
2. `docs/PROJECT-AUDIT-2026-11-04.md` 的 A4 修订：Agent 是宿主侧唯一的管理者，分 plugin / system 两个隔离的角色。
3. 登记 worktree 到 `docs/WORKSPACES.md`。
4. 下面的盘点表。

## 盘点结果

### 1. Redis 里有没有权威数据
**结论：没有权威数据（依据：读后端所有写入点；不是运行时抽查）。** 写入点：登录 token（`sys_user.go`，带 TTL；`<email>_token` 无 TTL）、登录锁（`login_lock.go`）、邮箱验证码 5 分钟、用户/设备配置/告警缓存、心跳（带 TTL）、`device_stats:<tenant>:<date>` 列表（`croninit/device_stats.go`，48 小时过期，仅首页趋势）、分布式锁、WebSocket 订阅计数。
- 丢失 Redis 的后果：**所有用户需要重新登录**；首页最近 48 小时的设备数量趋势丢失；其余自动重建。
- 未做：没有逐个确认缓存类 key 都能从 PG 重建（抽查 `device_config.go`、`alarm.go` 读取路径为“未命中则回源”，但未穷举）。所以备份第一期不含 Redis 的结论成立，但 R4 演练要观察一次“Redis 为空”的恢复后行为。
- 运行时抽查：本机 `tp-base` 栈的 Redis `dbsize` 为 0，说明不足以作为证据（栈很新）。
- compose 里 Redis 用 `redis_data` 卷、`--requirepass redis`，没开 AOF。

### 2. 各组件健康端点（安装器 develop `docker-compose.yml`）
| 组件 | 现有判断 | 缺口 |
|---|---|---|
| postgres（`timescaledb:14`） | healthcheck `pg_isready` | 无 |
| redis（6.2.7） | healthcheck `redis-cli` | 无 |
| backend（Gin） | healthcheck `wget 127.0.0.1:9999/health`；但 `HealthCheck` 实现只是返回空数据（`internal/api/system.go:18`），**不查库、不查 Redis、不查版本** | R1 扩展 |
| encore-api（14000） | **compose 里没有 healthcheck**；Encore 内有 `GET /api/device-metadata/healthz`（只表示进程起来） | R1 补：用 healthz + 迁移版本 |
| notification-core（18091） | **没有 healthcheck**；无已知 healthz | R1 补，需确认端点 |
| automation-scheduler | healthcheck 检查 `/tmp/last-tick-ok` 文件（tick 成功才写） | 可用 |
| broker-owner | healthcheck `tp-broker-readiness owner-liveness` | 可用 |
| mosquitto | **没有 healthcheck**（只有 `broker` 命令） | R1 补：端口连通 |
| thingsvis-server / studio / modbus_service / http_adapter | 前两者有 healthcheck，后两者没有 | 不属于控制面健康门，R1 决定是否纳入 |
| 插件 | Agent `HealthSpec` | 已有 |

### 3. 迁移来源（全部汇总，R1 的 `/api/system/info` 要报这些）
- 平台：`sql/*.sql`（55 个文件）+ `sys_version` 表；另有 `initialize/native_binding_schema.go`、`auth_schema.go`、`platformconfig_schema.go`、`legacy_migration.go`，各自用独立的 module/file/checksum 记录，**不依赖 sys_version**。
- Encore 各模块（各自 `migrations/`）：`devicemetadata`（0001–0039）、`automation`（1–16）、`fileassets`、`marketinstall`、`northbound`、`l3-core/notification`、`l3-core/connectorcatalog`；以及平台内置的 `plugininstall`、`pluginidentity`（安装器 per-migration marker：33↔pluginidentity，34/35↔plugininstall）。
- 风险：版本信息散落在至少 4 种记录方式里，没有统一汇总。manifest 的 `schema` 字段需要为每一种来源提供读取方法（R1/R3）。
- `SYSTEM_VERSION` 默认值是 `"dev"`，靠 `-ldflags` 注入；历史上出现过上游忘记同步导致显示旧版本的情况（SkillHub 记忆）。健康门里“`sys_version` 等于程序版本”要先确认发布流程会注入，否则会误判。

### 4. `files` 卷位置
- 后端工作目录 `/go/src/app`，相对路径 `./files/`（上传、OTA 包、Excel 导出、日志默认路径）→ 命名卷 `go_files`；`go_configs` → `/go/src/app/configs`。
- 备份范围：`go_files` 全部；`go_configs` 中的配置属于“配置文件”还是卷内容，R3 需确认（compose 当前把它当作可写卷）。
- Encore 的 `fileassets` 数据在 PG 里（已含在 `pg_dump`）；其二进制内容是否另存卷，R3 需确认。

### 5. PG / TimescaleDB 版本
- 镜像 `registry.cn-hangzhou.aliyuncs.com/thingspanel/timescaledb:14`（PostgreSQL 14）；本机运行栈查得 TimescaleDB 扩展 **2.8.1**（`pg_extension`，只读查询 `tp-base-postgres-1`）。
- TimescaleDB 2.8.1 的 `timescaledb_pre_restore()/post_restore()` 均存在，R3/R4 实测。
- 容器内 `pg_dump` 版本与服务端一致（计划 §3.5 的前提成立，R3 验证）。

### 6. tp-agent 哪些代码可以复用（安装器 develop `agent/`，约 5200 行 Go，模块 `thingspanel-installer-agent-state`，go 1.22，无第三方依赖）
| 部件 | 文件 | 复用方式 |
|---|---|---|
| 带系统文件锁的持久状态 | `state.go`：`Store`、`withLock`、私有目录/文件权限检查、原子写 | 锁和原子写可直接复用；**状态内容（Reservation/generation）是插件专用的**，system 操作的 journal 需要新写一个状态类型，不要往 `diskState` 里塞 |
| Docker Engine 适配器 | `docker_prepare.go` | 只走 Docker API、严格安全策略（只读 rootfs、drop all caps 等），**为插件容器定制**，不能直接用于“重启 compose 服务”。system 角色需要另写固定动作的执行器（调用 `docker compose`，参数固定） |
| socket + Bearer token API | `service.go`：`Handler()` 路由表、`authorized()`、`apiRequest/apiResponse` | 骨架可复用；system 需要**独立的 Service 实例、独立 socket 和 token**，不能在同一个路由表里加 `/v1/system/*` 并共用 token |
| 签名目录校验 | `catalog.go` | 可复用于 release manifest 的签名校验（R5a/R5b） |
| 密钥库 | `secrets.go` | 插件专用，system 不用 |
| 命令行 | `cmd/tp-agentctl` | 加 `system` 子命令 |
| 进程入口 | `cmd/tp-agent/main.go` | 需要启动第二个 listener |
| 测试 | `*_real_test.go`（真实 Docker，默认跳过需环境变量） | 参考其隔离方式做 R2/R4/R5b 演练 |
- P1 监视进程目前是 Python（`tools/scope_sync.py`，用 `update.py` 的锁，由 `agent_host.py` 启动），要并入 Agent 就是**用 Go 重写** + 复用 `config/native/prepare.py --sync-scope` 的“范围计算”逻辑或同样移植。这比计划里写的“搬进来”工作量大，R2 注意。

## 检查：安装器分支 `codex/matter-agent-generic-storage`

状态：未合入 develop；基于当前 develop（c551440）向前 11 个提交，最后提交 2026-11-12 13:14，仍在活跃。改动 32 个文件。

**结论：会冲突，且冲突集中在 R2 和 R5a，需要用户决定顺序。**

| 本计划的卡 | 与该分支重叠的位置 | 程度 |
|---|---|---|
| R1（system 角色骨架） | `agent/service.go`（该分支加了 `/v1/renew` 路由和 `SplitInstance`）；`agent/cmd/tp-agent/main.go` 未动 | 低：只是文本上相邻，易解决 |
| R2（P1 监视进程并入 Agent） | `tools/agent_host.py` 和 `update.py` 里该分支新增了 `ensure_scope_watcher(deferred=...)`、`start_scope_watcher_after_activation`；`docker-compose.yml` 给 backend 挂载 `./.tp-agent/run` 并加 `TP_AGENT_SOCKET`；**这些正是 R2 要拆走的代码** | **高：不先合入就会重复改同一段** |
| R5a（把 `update.py` 事务移植到 Go） | `config/native/update.py` +483 行：插件 bundle 的准备/激活/待定指针/校验（`prepare_optional_plugin`、`activate_native` 新签名、`reconcile_activation` 新签名）、`tests/test_reconcile_activation.py`、`test_update_transaction.py` | **高：要移植的对象本身还在变** |
| R3/R4 | `agent/storage.go`（新增，插件的命名卷/tmpfs）；`plugins/homeassistant.json`；`docker_prepare.go` 少量 | 低 |
| R7（插件升级） | `tools/plugin_catalog.py`、`plugin_bundle.py`、catalog 签名与信任根 `plugin_catalog_trust.pem` | 中：插件升级流程在该分支里被改写 |

另一个风险：该分支声明要 backend 能连 Agent socket（`/run/tp-agent`）——这是 **plugin 角色**的 socket。R1 的 system socket 必须**不**挂给 backend 容器，否则就违反“plugin 调用方拿不到 system token”。需要在设计里明确 system socket 只给 Encore system API（R6）使用的挂载方式。

## 没做的部分 / 停下来的原因
- R1 起的工作**没有开始**。按任务说明“发现与别的工作线冲突就停下来”，R0 之后停在这里。
- 没有做真实栈上的 Redis 行为验证（见上）。
- 没有改动安装器仓库任何东西（也没有为 R0 新建安装器 worktree；R0 对安装器只读）。

## 需要用户决定
1. 先把 `codex/matter-agent-generic-storage` 合入安装器 develop（推荐，R2/R5a 才能在稳定基线上做），还是让 R1–R5a 叠在该分支上，还是暂停那条分支？
2. system socket 的挂载方式（见上）是否接受“只给 Encore system API 容器挂载，不给 backend”。
