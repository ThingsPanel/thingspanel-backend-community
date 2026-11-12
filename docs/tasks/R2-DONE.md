# R2 交付说明（**未完成，中途停下等决定**）（2026-11-12）

后端分支 `wp/R2-check-config`（叠在 `wp/R1-health-gate` 上，提交 969996b5）；安装器分支 `wp/R2-restart`（叠在 `wp/R1-system-role` 上，提交 8180f08）。

## 已做
1. **`thingspanel-go --check-config`**（后端）：检查配置文件/生产配置能否加载、自动化桥所需环境、RSA 密钥能否读取；**不连数据库、Redis、Broker**；成功输出 `config ok` 退出 0，失败退出 1，错误不打印配置值。测试：缺失文件、损坏 YAML 被拒绝；仓库自带配置通过。
2. **`system restart`（Agent）**：`POST /v1/system/restart {scope: control|full}`，后台执行，同一时间只允许一个系统操作（第二个返回忙）。流程：预检（在运行中的 backend 容器内执行配置里写死的检查命令，失败则**一个容器都不碰**，状态为 aborted）→ 维护标记 → 按相反的启动顺序停控制面（full 再停 mosquitto、broker-owner）→ 启动（full 先 broker-owner、mosquitto，再控制面）→ 整栈健康门（默认最多 5 分钟）。全部通过 Docker Engine API 的 stop/start/exec，不调用 compose、不重建容器、不接受请求里的命令或镜像。`status.json`（无密钥，只有步骤、百分比、固定错误码、出错的组件名）和维护标记文件 `active` 写进配置里的 `status_dir`；成功后标记移除，失败时保留。backend 本来就停着时，跳过配置检查并在状态里记 `check_skipped_backend_down`，仍然继续重启（因为重启就是恢复手段）。`tp-agentctl system restart scope=control|full`。
3. 单元测试（Linux 容器，`-race`，原有测试全部通过）：控制面/全栈的停启顺序、配置检查失败不动任何容器、backend 停着时的行为、启动失败点名组件并保留维护标记、同时只能一个操作、scope 校验、没开启操作时路由拒绝。

## 方案（用户已选“后端算范围”）与已做的第二批
3. **后端 `GET /internal/broker-scope`**（后端 f5dbe388）：需要 `Authorization: Bearer $GOTP_BROKER_SCOPE_TOKEN`（环境变量未设或短于 32 字符时路由不存在，返回 404；错误 token 401；常量时间比较）。用 GORM 算出 `targets`（直连 MQTT 设备）和 `plugins`（协议/服务接入设备），条件与安装器原 SQL 逐条一致，每个列表最多 1000 条，不含任何凭据。没有挂在前端 nginx 的任何 location 下（nginx 只转发 `/api`、`/files` 等），所以不对外。测试（内存 SQLite）：十种设备组合分到正确的列表；认证的各种错误。**路线选择说明**：设备表属于旧 Gin 后端，所以沿用已有的 `/internal/automation/*` 同类内部路由，而不是写在 Encore 里；如果你要求必须在 Encore，需要另议。
4. **Agent Broker 范围同步（Go）**（安装器 6a703c4）：`scope_sync.go`。从平台拉范围 → 用 `.env` 里的 Broker 账号构造 `services.json`（**对同一输入与 Python `scope_services` 输出逐字段相同**，由 Python 生成的黄金文件校验）→ 与现有文件语义比较 → 变化才写（临时文件 + 原子替换，0440，root 时 chown 1883）→ 先写 pending 标记 → 停启 broker-owner 再停启 mosquitto → 删 pending。重启中途失败，pending 保留，下一轮重试。使用安装器同一个 `.thingspanel-update/lock`（flock，非阻塞，忙就下次）；installer journal 未处于 `complete/recovered` 时不碰配置；与系统操作（重启）互斥，不会同时进行；范围超过 256 台拒绝。`system status` 返回最近一次同步结果（固定码）。不接触数据库，也不需要数据库账号。

## 没做（以及为什么停）
- **生产接线没做：Agent 容器如何带上 system 参数启动。** 启动 Agent 的是 `tools/agent_host.py`，而它和 `tools/scope_sync.py` 都被 `config/native/update.py` 的 `PLUGIN_HELPER_PROFILES` 按 SHA-256 钉住（并随签名发布）。改这两个文件，必须新增一份 helper 摘要配置、保留旧配置、重新走发布签名链；同时 `plugin_install.py`、`update.py` 里多处还会启动 **Python 版**监视进程，必须一起关掉，否则会出现两个监视进程同时重启 Broker。这已经触及发布流程，我没有在这张卡里动。**需要用户决定**：是否授权在 R2 里按这套规则修改并更新摘要，还是并入 R5a（把 update.py 事务移植到 Go）一起处理。
- **维护页**（nginx 在维护期间对 `/api` 返回 503，静态页读 `status.json`）：未做。Agent 侧的 `status.json` 和 `active` 标记已写，缺 nginx 配置与 compose 只读挂载，也与前端卡 R6 有交集。
- **后端 `--check-config` 在真实镜像里**：v1.3.0 镜像没有这个参数，真实栈需要本地构建一个后端镜像。
- **真实整栈演练：一项都没做。** 已知困难（来自 PH1-S1 交付说明）：Encore 镜像在本机构建一直卡住（4 次尝试，最长 21 分钟，未找到根因），所以此前的栈也只能是“后端 + Broker + PG + Redis”的缩减栈；安装器 develop 在全新目录 `prepare.py --bootstrap` 有已知 bug，需要绕过；隔离栈需要改端口。**所以“整栈”（含 Encore）的演练做不了。** 缩减栈（无 Encore）可以做：控制面重启时 MQTT 设备连接不断开、配置写错拒绝重启、设备增删后范围自动同步。**需要用户决定**：缩减栈演练是否可作为 R2 验收，并如实标注“不含 Encore”。
- 所以 **R2 的完成标准仍然一条也没有达成**。
