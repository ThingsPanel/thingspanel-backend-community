# R2 交付说明（2026-11-12）— 真实栈演练已做；仍有三项没完成，见末尾

后端分支 `wp/R2-check-config`（叠在 `wp/R1-health-gate` 上，提交 969996b5）；安装器分支 `wp/R2-restart`（叠在 `wp/R1-system-role` 上，提交 8180f08）。

## 已做
1. **`thingspanel-go --check-config`**（后端）：检查配置文件/生产配置能否加载、自动化桥所需环境、RSA 密钥能否读取；**不连数据库、Redis、Broker**；成功输出 `config ok` 退出 0，失败退出 1，错误不打印配置值。测试：缺失文件、损坏 YAML 被拒绝；仓库自带配置通过。
2. **`system restart`（Agent）**：`POST /v1/system/restart {scope: control|full}`，后台执行，同一时间只允许一个系统操作（第二个返回忙）。流程：预检（在运行中的 backend 容器内执行配置里写死的检查命令，失败则**一个容器都不碰**，状态为 aborted）→ 维护标记 → 按相反的启动顺序停控制面（full 再停 mosquitto、broker-owner）→ 启动（full 先 broker-owner、mosquitto，再控制面）→ 整栈健康门（默认最多 5 分钟）。全部通过 Docker Engine API 的 stop/start/exec，不调用 compose、不重建容器、不接受请求里的命令或镜像。`status.json`（无密钥，只有步骤、百分比、固定错误码、出错的组件名）和维护标记文件 `active` 写进配置里的 `status_dir`；成功后标记移除，失败时保留。backend 本来就停着时，跳过配置检查并在状态里记 `check_skipped_backend_down`，仍然继续重启（因为重启就是恢复手段）。`tp-agentctl system restart scope=control|full`。
3. 单元测试（Linux 容器，`-race`，原有测试全部通过）：控制面/全栈的停启顺序、配置检查失败不动任何容器、backend 停着时的行为、启动失败点名组件并保留维护标记、同时只能一个操作、scope 校验、没开启操作时路由拒绝。

## 方案（用户已选“后端算范围”）与已做的第二批
3. **后端 `GET /internal/broker-scope`**（后端 f5dbe388）：需要 `Authorization: Bearer $GOTP_BROKER_SCOPE_TOKEN`（环境变量未设或短于 32 字符时路由不存在，返回 404；错误 token 401；常量时间比较）。用 GORM 算出 `targets`（直连 MQTT 设备）和 `plugins`（协议/服务接入设备），条件与安装器原 SQL 逐条一致，每个列表最多 1000 条，不含任何凭据。没有挂在前端 nginx 的任何 location 下（nginx 只转发 `/api`、`/files` 等），所以不对外。测试（内存 SQLite）：十种设备组合分到正确的列表；认证的各种错误。**路线选择说明**：设备表属于旧 Gin 后端，所以沿用已有的 `/internal/automation/*` 同类内部路由，而不是写在 Encore 里；如果你要求必须在 Encore，需要另议。
4. **Agent Broker 范围同步（Go）**（安装器 6a703c4）：`scope_sync.go`。从平台拉范围 → 用 `.env` 里的 Broker 账号构造 `services.json`（**对同一输入与 Python `scope_services` 输出逐字段相同**，由 Python 生成的黄金文件校验）→ 与现有文件语义比较 → 变化才写（临时文件 + 原子替换，0440，root 时 chown 1883）→ 先写 pending 标记 → 停启 broker-owner 再停启 mosquitto → 删 pending。重启中途失败，pending 保留，下一轮重试。使用安装器同一个 `.thingspanel-update/lock`（flock，非阻塞，忙就下次）；installer journal 未处于 `complete/recovered` 时不碰配置；与系统操作（重启）互斥，不会同时进行；范围超过 256 台拒绝。`system status` 返回最近一次同步结果（固定码）。不接触数据库，也不需要数据库账号。

## 真实栈演练（含 Encore）

栈：安装器 develop（`ba64837`）的 compose，项目名 `r2`，容器 `r2-*`，端口只绑 127.0.0.1:32xxx（用 `docker-compose.override.yml`，不属于产品）。镜像：后端 `r2-backend:dev` = 后端 develop `789a83e1` 宿主机交叉编译叠加在 v1.3.0 基础层（版本号 `r2-789a83e1`），Encore `r2e-encore:dev`（由 R2E 会话构建，见下），Broker 和前端用 v1.3.0。运行的服务：postgres、redis、backend、**encore-api、notification-core**、automation-scheduler、broker-owner、mosquitto、frontend；**未运行**：thingsvis-server/studio、modbus_service、http_adapter。Agent：安装器 develop 的 `tp-agent`，容器 `r2-agent`，开启 system 角色和范围同步（`interval_seconds=10`），通过 `-network` 加入栈网络。证据：`/Volumes/KIOXIA/archive/evidence/2026-11-12-R2/`（任务书写的 500G 卷在本机不存在）。

| 场景 | 结果 |
|---|---|
| 整栈健康门 | `system verify` 对 8 个组件全部 ok（含 encore-api、notification-core 的 `/__encore/healthz`，mosquitto 的 TCP 探测） |
| 控制面重启（默认）+ 一个保持中的 MQTT 连接 | 流程 done，约 39 秒（其中约 25 秒是 `automation-scheduler` 停止：它的入口是 shell，不处理 SIGTERM，等满宽限期）。**postgres、redis、broker-owner、mosquitto 的启动时间不变**；MQTT 连接 70 多秒内零掉线；`/health` 不可用约 1 秒（0.5 秒一次的探测只看到一次 000） |
| 配置写错 | 把运行中 backend 的 `configs/conf.yml` 改成坏 YAML：`--check-config` 退出 1、给出原因（不含配置值）；`system restart` → `aborted / config_invalid / 组件 backend`，**没有任何容器被碰**，维护标记不存在；还原后 `--check-config` 又是 `config ok` |
| 设备范围同步 | 通过平台 API 建超管、租户、设备后，10 秒内 `services.json` 出现该设备和租户，broker-owner → mosquitto 依次重启；Agent 日志 `broker scope sync: applied`；下一轮 `unchanged` |
| 全栈重启 | done；MQTT 连接掉线约 9 秒后自动重连（符合“设备会短暂掉线”） |
| Agent 容器重启（宿主机重启的替代） | 重新起来后范围同步恢复；backend 恰在重启时有一次 `scope_fetch_failed`，下一轮恢复 |

### 演练发现的事
- **每次增删设备都会让所有 MQTT 设备掉线约 2 秒**（范围变化要重启 broker-owner 和 mosquitto）。这是原 P1 监视进程就有的设计，R2 没有引入也没有改善；要消除需要 Broker 支持热加载范围，另开卡。
- `automation-scheduler` 的 shell 入口不转发 SIGTERM，每次停止多等 30 秒宽限期。属于安装器 `tick.sh`/入口问题，记入待办，没有在这里改。
- 第一次范围同步在新栈上是 `applied`（`services.json` 原先不存在），不是内容差异；Go 版输出与 Python 版逐字段一致由黄金文件测试保证。

### 附带产出的两个会话（不属于 R2 提交，各在自己的分支，**未合并未推送**）
- [R2E] Encore 镜像卡住的根因：Encore CLI v1.58.6 在 `.git` 是文件（git worktree）时 `WorkspaceRoot` 死循环；与仓库大小、网络无关。分支 `wp/R2E-encore-build`（9c67ac5a），脚本 `scripts/build-encore-image.sh` 从干净导出构建，约 40 秒。这也解释了为什么 PH1-S1 的 4 次构建全部卡死。
- [R2G] 旧 Gin 的 `/internal/broker-scope` 已重写为 Encore 服务 `brokerscope`，分支 `wp/R2G-scope-encore`（8f681e45），Gin 路由已删，对真实 PostgreSQL 16 的测试通过；**没有**在真正运行的 Encore 进程或本栈里验证。本演练用的是 develop 上仍在用的 Gin 路由。合并该分支后，Agent 配置里的 URL 需要按其交付说明调整。

## 仍然没有完成
- **生产接线**（`tools/agent_host.py` 启动带 system 参数的 Agent、生成 system token/配置、关闭 Python 监视进程）：按用户决定并入 R5a。演练里 Agent 是手工 `docker run` 启动的。
- **维护页**（nginx 在维护期间对 `/api` 返回 503 + 静态页读 `status.json`）：只有 Agent 侧的 `status.json` 和 `active` 标记，nginx/compose 未做；与前端卡 R6 一起做更合适。
- **宿主机真的重启后的自动恢复**：没有测。Agent 容器带 `--restart unless-stopped`，演练只重启了 Agent 容器。
- 演练栈缩减了 thingsvis、modbus、http_adapter；没有用真实设备，MQTT 连接是用平台自己的账号和 `tp-community-readiness` 客户端 ID 建立的探针，不是注册设备。
- 演练栈只**停止**了，没有删除。容器 `r2-*`（11 个，含 `r2-agent`）、卷 `r2_go_configs`、`r2_go_files`、`r2_mosquitto_data`、`r2_postgres_data`、`r2_redis_data`、网络 `r2_thingspanel_network`、镜像 `r2-backend:dev`、目录 `/Volumes/KIOXIA/tmp/r2-work`（135 MB，含临时凭据）等用户确认后再清理。
