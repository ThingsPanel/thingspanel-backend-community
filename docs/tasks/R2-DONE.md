# R2 交付说明（**未完成，中途停下等决定**）（2026-11-12）

后端分支 `wp/R2-check-config`（叠在 `wp/R1-health-gate` 上，提交 969996b5）；安装器分支 `wp/R2-restart`（叠在 `wp/R1-system-role` 上，提交 8180f08）。

## 已做
1. **`thingspanel-go --check-config`**（后端）：检查配置文件/生产配置能否加载、自动化桥所需环境、RSA 密钥能否读取；**不连数据库、Redis、Broker**；成功输出 `config ok` 退出 0，失败退出 1，错误不打印配置值。测试：缺失文件、损坏 YAML 被拒绝；仓库自带配置通过。
2. **`system restart`（Agent）**：`POST /v1/system/restart {scope: control|full}`，后台执行，同一时间只允许一个系统操作（第二个返回忙）。流程：预检（在运行中的 backend 容器内执行配置里写死的检查命令，失败则**一个容器都不碰**，状态为 aborted）→ 维护标记 → 按相反的启动顺序停控制面（full 再停 mosquitto、broker-owner）→ 启动（full 先 broker-owner、mosquitto，再控制面）→ 整栈健康门（默认最多 5 分钟）。全部通过 Docker Engine API 的 stop/start/exec，不调用 compose、不重建容器、不接受请求里的命令或镜像。`status.json`（无密钥，只有步骤、百分比、固定错误码、出错的组件名）和维护标记文件 `active` 写进配置里的 `status_dir`；成功后标记移除，失败时保留。backend 本来就停着时，跳过配置检查并在状态里记 `check_skipped_backend_down`，仍然继续重启（因为重启就是恢复手段）。`tp-agentctl system restart scope=control|full`。
3. 单元测试（Linux 容器，`-race`，原有测试全部通过）：控制面/全栈的停启顺序、配置检查失败不动任何容器、backend 停着时的行为、启动失败点名组件并保留维护标记、同时只能一个操作、scope 校验、没开启操作时路由拒绝。

## 没做（以及为什么停）
- **P1 监视进程并入 Agent：停在这里，需要用户决定。** 现在的 `scope_sync.py` → `prepare.py --sync-scope` 做的事是：用 `.env` 里的 Broker 账号密码，直接对平台库手写 SQL 查 `devices`/`device_configs`，生成 `services.json`（含 MQTT 凭据），变化时再重启 broker-owner → mosquitto，并用安装器的锁文件。把它原样用 Go 搬进 Agent，意味着：Agent 要拿到平台数据库账号并复制设备范围的 SQL（违反“关系库只走 GORM、不手写 SQL”的方向），且 Agent 要写含凭据的 broker 配置。两个方案见汇报。
- **维护页**（nginx 在 `/api` 维护期间返回 503 + 静态维护页轮询 `status.json`）：未做，依赖安装器 nginx 配置和 compose 挂载，也与前端卡 R6 有交集。
- **Agent 作为系统服务、启动 system 角色的接线**（`tools/agent_host.py` 加参数，生成 system token/配置，`restart: unless-stopped`）：未做。
- **真实整栈演练**（含“控制面重启时设备不掉线”实测、配置写错拒绝重启、宿主机重启后自动恢复）：**一项都没做**。可行性已看过：v1.3.0 镜像都在本地，但要用安装器起一整套隔离栈（`r2` 前缀），还要一个带 `--check-config` 的后端镜像（v1.3.0 镜像里没有这个参数，需要本地构建）。
- 所以 **R2 的完成标准一条也没有达成**，这份文档只记录已经写好并通过单元测试的部分。
