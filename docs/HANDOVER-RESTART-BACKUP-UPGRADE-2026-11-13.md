# 交接：重启 · 备份 · 恢复 · 升级（R0–R7）——测试与收尾（2026-11-13）

> **给谁**：接手测试、扫尾并合入发布版本的同事。
> **一句话**：代码 R0–R7 已全部合入三个仓库的 `develop` 并推送，每张卡都在隔离的真实整栈上演练过。**但还不能发布**：缺发布构建、签名发布链，另有若干路径没在真实环境验证。本文告诉你剩什么、怎么测、怎么发。
> **规划原文**：[RESTART-BACKUP-UPGRADE-PLAN-2026-11-12.md](RESTART-BACKUP-UPGRADE-PLAN-2026-11-12.md)（设计、取舍、HA 对照）。**逐卡交付说明**：`docs/tasks/R0-DONE.md` … `R7-DONE.md`（演练过程、发现的问题、没做的部分都写在里面，以它们为准）。
> **用户文档**（已发布到文档站）：`thingspanel.github.io` 仓库 `source` 分支，路径 `docs/user-guide/system-admin-operation-manual/system-managment/system_operations.md`（中文在 `i18n/zh-Hans/…` 同路径）。

---

## 1. 现在是什么状态

| 卡 | 内容 | 状态 |
|---|---|---|
| R0 | 盘点、A4 修订、规划 | 已合并 |
| R1 | 健康门 + Agent system 角色骨架 | 已合并 |
| R2 | 配置检查 + 重启 + Broker 范围同步并入 Agent | 已合并 |
| R3 | 备份 | 已合并 |
| R4 | 恢复（+ `R4S` 修后端约束文本比较缺陷） | 已合并 |
| R5a | 配置事务移植到 Go + Agent 生产接线 | 已合并 |
| R5b | 升级 + 自动回退 | 已合并 |
| R6 | 界面（⋮ 菜单、重启对话框、更新列表、备份页、维护遮罩） | 已合并 |
| R7 | 统一更新列表（含插件升级） | 已合并 |

合并后的 develop：后端 `930e4ec7`、安装器 `dd9dbbd`、前端 `a0432f20`。

### 一句话讲清楚架构

宿主上只有一个管理者 **tp-agent**（Go，在安装器仓库 `agent/`，不在平台 compose 栈里），分两个**隔离的角色**：
- `plugin` 角色：插件生命周期（原有，未改）+ Broker 范围同步（原 P1 监视进程搬进来）；
- `system` 角色（新）：配置检查、重启、备份、恢复、升级。**独立的 socket 和 token**，只接受固定动作，镜像版本只能来自已校验签名的发布清单。socket/token 目录（`.tp-agent/system-api/`）**只有 encore-api 容器只读挂载**，Gin 后端和插件容器看不到。

界面 → nginx → Encore `system` 服务（`/api/system/*`，仅超级管理员）→ Agent 的 system socket → Docker Engine API / `pg_dump` / `pg_restore`。

---

## 2. 代码在哪里

| 位置 | 内容 |
|---|---|
| 安装器 `agent/system_service.go`、`system_manage.go`、`system_updates.go` | system 角色的 HTTP 接口与更新检查 |
| 安装器 `agent/system_restart.go`、`system_docker.go` | 重启（control/full）、Docker Engine API 封装 |
| 安装器 `agent/backup.go`、`restore.go` | 备份包格式、PG Adapter、恢复流程 |
| 安装器 `agent/upgrade.go`、`upgrade_live.go`、`config_txn.go` | 升级 + 回退状态机、配置文件事务（移植自 `update.py`） |
| 安装器 `agent/system_provision.go` | `-provision-system`：生成 `system.json` 和 system token |
| 安装器 `agent/cmd/tp-agentctl` | 命令行：`system status\|verify\|restart\|upgrade\|backup …` |
| 安装器 `tools/agent_host.py` | 启动 Agent：`up --system --release-source … --release-trust-root …` |
| 安装器 `niginx.conf`、`docker-compose.yml` | 维护页、`/api/system/` 路由、前端依赖修复 |
| 后端 `encore-api/system/` | Encore 管理 API（`api.go`、`system.go`） |
| 后端 `encore-api/internal/systemagent/` | Agent system socket 客户端 |
| 后端 `internal/health/`、`/health` | 健康门探针 |
| 后端 `thingspanel-go --check-config` | 只校验配置，不连外部服务 |
| 后端 `encore-api/brokerscope/` | Broker 范围（原 Gin `/internal/broker-scope` 改写为 Encore） |
| 前端 | ⋮ 菜单、重启对话框、更新列表、备份页、维护遮罩（说明在前端仓库 `docs/R6-FRONTEND-DONE.md`） |

演练证据（不在仓库里）：`/Volumes/KIOXIA/archive/evidence/2026-11-12-R*/`、`2026-11-13-R*/`，含脚本（`stack.sh`、`lib.sh`）、日志和界面截图。演练栈和镜像都已清理，**需要按里面的脚本重新搭**。

---

## 3. 发布前必须解决（阻塞项，按优先级）

| # | 事项 | 说明 | 谁 |
|---|---|---|---|
| **B1** | **CI 的 Encore 镜像缺服务** | `.github/workflows/encore-image-build.yml` 的 `--services` 只有 authn、thingmodel、devicemetadata、fileassets、connectorcatalog，**缺 automation、notification、marketinstall、northbound、platformconfig、brokerscope、system**。按现状发布的 Encore 镜像里**没有 `system` 服务，界面的所有系统功能都不可用**。本地脚本 `scripts/build-encore-image.sh` 是全的，可对照补 CI。R2G 说明里“工作流已补 brokerscope”与 develop 实际内容不符 | 开发 |
| **B2** | **发布三样镜像/二进制** | 新前端镜像、含 `system` 服务的 Encore 镜像、新 Agent 二进制（多架构 amd64+arm64）。`docker-compose.yml` 里的镜像标签要同步 | 有发布权限的人 |
| **B3** | **签名发布清单和信任根接进安装脚本** | `update.py`/`upgrade.sh` **没有**把 `--release-source/--release-trust-root` 传给 `agent_host.py`。现在启用升级，必须手动用这些参数重跑 `agent_host.py up --system`。没有正式信任根（本期用本地测试密钥）。**没有这一步，用户的“检查更新”什么都查不到** | 负责人决定方案 |
| **B4** | **新插件目录和镜像怎么分发到现场** | 现在是手工把签名目录放进 Agent 挂载目录。分发链路（安装器/发布流程）没做 | 负责人决定 |
| **B5** | **升级 Agent 后要重新 `up --system`** | `system.json` 由 Agent 二进制生成，升级 Agent 后配置不会自动刷新。升级编排/安装脚本要包含这一步 | 开发 |
| **B6** | **全新安装和“旧版本升级上来”没整体验证过** | `update.py` 里 `start_plugin_agent` 加 `--system` 只有单元测试；演练里是手工用 `agent_host.py` 启动的。没在全新机器装过，没验证旧版本升级时 `.env` 新增 `TP_BROKER_SCOPE_TOKEN` 的完整过程 | 测试 |
| **B7** | `docker:27-cli` 在升级预检时从 Docker Hub 拉 | 升级的辅助容器用这个镜像，**没有离线模式**。内网/离线环境要预先导入，或改成随发布携带 | 开发/运维 |

---

## 4. 怎么测

### 4.1 环境

- **优先用安装器一条命令装出来的环境**测，不要手工 `docker run` 拼栈（那测的是搭环境的能力）。至少两台：一台全新安装，一台先装 v1.3.0 再升级。
- 必须是 Linux + Docker。Windows 没测；macOS 只在 OrbStack 上跑过。
- 备份会读 `backups/`（安装目录下，0700）；确认磁盘有足够空间（需要预估大小的 2 倍 + 64 MB）。
- 数据：**造数据时用当前时间戳**。后端有数据保留定时任务（默认 15 天，`CleanSystemDataByCron`），旧时间戳的遥测会被删光，造成“恢复/回退后数据变 0”的假象（R5b 踩过）。
- 命令行（排查用）：`tp-agentctl -socket <system.sock> -token-file <token> system status|verify|restart scope=control|full|upgrade to=vX.Y.Z|backup create|list|verify|restore name=…|delete name=…`。system socket 在安装目录 `.tp-agent/system-api/`。

### 4.2 测试清单

| # | 场景 | 操作 | 期望 | 开发阶段验证情况 |
|---|---|---|---|---|
| 1 | 全新安装 | 干净机器一条命令安装 | 超管在“系统设置”右上角 ⋮ 看到“检查更新”“重启系统”，有“备份”标签 | ❌ **没测** |
| 2 | 旧版本升级上来 | 装 v1.3.0 → 升级到本版本 | 数据、设备、账号都在；Agent 以 system 角色运行 | ❌ **没测** |
| 3 | 重启平台服务 | 保持一个 MQTT 连接，点“重启平台服务” | 约 40 秒完成；维护页显示进度并自动刷新；设备连接不掉线 | ✅ 隔离栈 |
| 4 | 全栈重启 | 选“全栈重启” | 设备掉线约 10 秒后自动重连 | ✅ 隔离栈（约 9 秒） |
| 5 | 配置错误 | 故意改坏配置，打开重启对话框 | 配置检查失败，重启按钮不可用，**一个容器都不动** | ✅ |
| 6 | 立即备份 | 备份页点“立即备份” | 出现新的一行，校验通过 | ✅（接口层 + 隔离栈）；界面点击未测 |
| 7 | 恢复 | 备份 → 新建设备 → 恢复 | 新设备消失，其余数据在，设备能重新上行，登录正常 | ✅ 命令行 + 隔离栈（约 73 秒，300 万行）；**界面点击恢复/删除没测过** |
| 8 | 正常升级 | 检查更新 → 升级 | 先自动备份；维护页进度；完成后页脚版本变化，数据不变 | ✅ 隔离栈（52 秒，含迁移） |
| 9 | 坏版本升级 | 发布一个迁移报错或起不来的版本，再升级 | 提示“已自动回退”，版本、数据回到原样，半成品表不存在 | ✅（三种失败方式都演练过） |
| 10 | 插件升级 | 升级真实 HA 插件 | 版本变化，HA 设备继续上报，旧代次被吊销 | ⚠️ 只用**替身插件**测过，**真实 HA 没测** |
| 11 | 权限 | 租户账号 | 看不到菜单；直接调 `/api/system/*` 返回 403 | ✅ |
| 12 | 断电 | 升级/恢复中途重启宿主机 | 开机后自动回退或继续，不留半成品；Agent 自动拉起 | ⚠️ 只用 `kill -9` 在 `applying`/`starting`/`rolling_back` 测过；**宿主真断电没测**；Agent 容器带 `--restart unless-stopped` |
| 13 | 升级中刷新浏览器 | 升级过程刷新页面 | 仍显示维护页，不白屏 | ✅ |
| 14 | 回退也失败 | 注入回退失败 | 状态 `needs_human`，维护标记保留，所有备份保留，界面有说明文字 | ⚠️ **只有单元测试**，真实栈没演练、界面没见过 |
| 15 | 备份期间被杀 | 备份中 `kill -9` Agent | 不会留下“看起来完整”的坏包；重启后 `list` 只列完整包 | ✅ |
| 16 | 版本兼容 | 用比当前**更高**版本的备份恢复 | 拒绝并提示 | 单元测试；真实“旧备份恢复后向前迁移”没演练（没有两个真实版本） |
| 17 | 数据丢失测量 | 升级/恢复期间持续上报 | 记录丢了多少条、停机多久，写进文档 | ❌ **从没测过**（只有停机时长约 73 秒/52 秒） |
| 18 | PG 版本 | 在 PG 15/16 上做恢复 | 后端能启动 | ⚠️ `R4S` 只在 **PG 14.5** 验证（见 6.2） |
| 19 | 上传文件恢复 | 备份后新增文件再恢复 | 新增文件**不会被删除**（覆盖式恢复，已知行为）；确认文档说明与实际一致 | ❌ 演练时上传卷是空的 |

**重点测 1、2、10、12、14、17、18**——都是开发阶段没碰到真实环境的。

---

## 5. 已知缺陷与设计局限（别当成 bug 再查一遍）

1. **插件升级失败不能回退**：旧代次先被吊销，新版本起不来就停在 `failed`，只能重试。Agent 只认当前目录，且代次模型不允许新旧并存。要做回退需要目录保留上一版条目。
2. **插件 `agent_prepare` 失败被当作“Agent 离线”重试（最多 40 轮）**，重试期间旧代次已吊销、插件停着，每轮代次 +1。这是原有行为，R7 只用“镜像预检”挡住最常见原因（镜像缺失返回 409 `IMAGE_UNAVAILABLE`，插件保持运行）。
3. **恢复不会重建插件容器**：备份里有插件实例清单，但恢复不据此重建。
4. **每次增删设备，所有 MQTT 设备掉线约 2 秒**（范围变化要重启 broker-owner 和 mosquitto）。原 P1 就有，要消除需要 Broker 支持热加载范围。
5. **`automation-scheduler` 不处理 SIGTERM**（入口是 shell），每次重启多等约 25–30 秒宽限期（安装器 `tick.sh`/入口问题）。
6. **失败升级最坏约 7 分钟**：新版本一直不健康时，健康门等满预算（默认 5 分钟）才回退。可用 `upgrade.verify_seconds` 调。
7. **健康门只看某一刻整栈健康**，不监视之后的崩溃；回退用快照还原，**快照之后到回退之间写入的设备数据会丢**；维护窗口内上报没有缓冲。
8. **备份只在本机、界面不能下载**；不含密钥文件（`.env`、broker token、私钥证书、Agent `secrets.json`）；没有自动备份、没有加密、没有异地存储。
9. **恢复是覆盖式**：备份之后新增的上传文件不会被删除。安全快照放在同一块磁盘上。
10. **版本比较只认 `vX.Y.Z`**，开发标签只能字符串相等；备份时后端没在运行，版本记为 `unknown`，恢复时按“不能自动判断”处理。
11. **Encore `/api/system/*` 没有审计表**，只有日志里一行 “system action”。
12. **更新对话框里插件升级成功后那一行仍显示“旧→新”（按钮置灰）**，没有重新拉取目录（前端小问题）。
13. 统一更新列表是前端把 Encore `/api/system/updates` 和 Gin `/api/v1/plugin/catalog` 拼起来的；插件安装迁到 Encore 后要合成一个接口。
14. Agent 只支持 Linux；没测 Windows；没测“发布包里 compose 的服务集合变化（新增/删除服务）”；`thingsvis-*` 服务在演练栈里没部署，没验证。

---

## 6. 需要特别留意

### 6.1 与本任务无关、但测试时会碰到的失败
- 后端 `go test ./internal/api/` 的 `TestTelemetryHTTPDeviceAuthorization` 在 R0 之前就失败，与本任务无关。
- 安装器 `tests/test_plugin_bundle_activation.py` 有 7 个测试在安装器 develop 上就失败（插件 helper 摘要与安装器 pin 不符），与本任务无关。
- 演练中发现 `tp-base`、`thingspanel-community-v130` 两套栈的后端容器反复重启（计数 140+），早于演练，没动。

### 6.2 `R4S`：后端表约束文本比较
后端启动时拿 `pg_get_constraintdef` 的文本和固定字符串比较，`pg_restore` 之后同一个约束的显示形式会变（约 16 个），导致**任何“导出再恢复”都会撞上**，包括人工恢复。`wp/R4S-schema-contract` 已修（规范化只覆盖 PG 14.5 观察到的形态）。**换 PG 15/16 若出现新形态，要在 `canonicalizeRestoredArrayAny` 补充并加测试**；更稳的长期做法是语义比较（`pg_constraint.conbin` 或试探性插入）。

### 6.3 仓库纪律
- 项目规则见后端 `CLAUDE.md`：新工作用 `wp/<编号>-<短名>` 分支和独立 worktree；证据不进仓库；测试要诚实，单元测试通过不能说成整栈验收通过；删除目录/分支/卷之前先列清单确认。
- 演练期间的 worktree、分支、容器、卷已清理；登记见 `docs/WORKSPACES.md`。

---

## 7. 建议的收尾顺序

1. **B1 → B2**：补 CI 服务清单，构建并发布三样东西（先在测试环境用 `scripts/build-encore-image.sh` 本地构建一套自测）。
2. **B3、B4、B5**（需要负责人拍板的部分先问清楚）：把发布源和信任根接进 `install/upgrade` 脚本，升级 Agent 后自动重跑 `up --system`。
3. 用 §4 的清单在**全新**和**旧版本升级**两台环境上完整走一遍，重点补 1、2、10、12、14、17、18。
4. 发现的缺陷记进 `docs/TODO.md`，不要开新线乱修；每个修复一张小卡。
5. 合入发布分支前：确认 §5 的局限与用户文档里的“重要提示”一致（尤其是“备份只在本机”“不含密钥文件”“恢复会丢失备份之后的数据”）。
6. 发布后更新 `docs/WORKSPACES.md`，写一份 `docs/tasks/R-RELEASE-DONE.md`（做了什么、怎么验证的、没做什么）。

---

## 8. 第二期（不在本期，别接）

定时自动备份与保留策略界面、备份加密（之后才能把密钥文件放进备份）、异地存储（NAS/S3）、备份下载、部分备份（不含历史数据）和部分恢复、配置热加载（快速重载）、重启主机/关机/安全模式、Agent 自我更新、维护窗口内缓冲上报、MySQL/TDengine 的备份 Adapter、零停机升级、Windows、审计表。
