# ThingsPanel 后端整理报告与任务书（2026-11-04，第三版）

> **第三版的变化**：时序库的可选项定为 TDengine，不做 InfluxDB；插件任务的目标定为“一键安装 Home Assistant 插件，把 HA 数据打通”；目标架构写成明确条文（§1）；清场、数据库、插件三项已有的进展吸收进来；“补全缺失功能”暂不做。
> **依据**：本机仓库实测（2026-11-04），两份执行人汇报（数据库兼容、插件安装），以及 `/Volumes/500G/docs/` 下 4 份参考文档：《Encore 改造任务书 11-01》《插件发布与一键安装设计 11-01》《双团队并行统筹 11-01》《双团队成果复查与统一收尾 11-02》。
> **配套文件**：清场任务书 `docs/CLEANUP-TASKBOOK-2026-11-04.md`；工作规则写在仓库根目录 `CLAUDE.md`。
> **最新进度和交接**：见 **§7**（2026-11-04 晚更新）。接手的人先读 §7，再读 §1 和 §2。**剩余任务的派工提示语**见 `docs/DISPATCH-PROMPTS-2026-11-04.md`。

---

## 0. 结论

- **当前只有两件事要收口：数据库兼容、HA 插件一键安装。** 数据库这边代码基本做完了，差审查和合并。插件这边底座（安装器、Agent、签名目录、一条命令安装）已经有了，**卡在最后一步**：后端已经故意封死了旧的匿名插件接口，新的“受管插件运行时身份”只写完代码，还没跑过，所以真实插件拿不到设备配置，上报失败。
- **清场已完成大半**：本地分支从 48 个减到 13 个，打了 41 个 `archive/*` 标签，worktree 从 25 个减到 9 个，系统盘可用空间从 12 GiB 增加到 23 GiB。剩余项见 §4 的 T0。
- **补全缺失功能暂不做**：设备、模板、分组的功能已经在 1.3 里，1.3 已合入主分支（develop 完整包含 `origin/main`）。357 条路由的台账是 Gin 退役的债务，不是功能缺失。
- **已定（2026-11-04，用户）：方案乙，SQL 翻译器整套废弃，按 GORM 重做 MySQL 兼容，不留过渡层，不留技术债**（§2）。`wp/d1-dbx-returning` 不合入，只作为参考：它的差分测试场景和 MySQL 语义清单可以复用，翻译器和 RETURNING 模拟不能复用。
- **工作方式**：本会话作为项目总监和技术总监，负责决策、派工、审查。具体开发由单独的 Sonnet 会话完成，每个任务一个会话。

---

## 1. 目标架构

> **基于 Encore 的 Go 物联网平台架构：控制面与数据面分离；关系型数据库采用 GORM；其他数据库通过专用 Adapter 接入；业务能力通过插件化机制扩展。**
> 发行版只有一种：**PostgreSQL + TimescaleDB**。其他组件通过配置替换。

### 1.1 结构图

```
                 ┌──────────────────────── 控制面（Encore 服务） ────────────────────────┐
  前端 / API ───▶│ 认证租户(l1-access) · 设备元数据(devicemetadata) · 物模型(thingmodel)    │
                 │ 自动化(automation) · 通知核心(l3-core) · 市场安装(marketinstall)       │
                 │ 平台配置(platformconfig) · 插件管理（安装计划、运行时身份、代次）       │
                 └───────┬───────────────────────────────────────────────┬───────────┘
                         │ 配置、身份、路由下发                            │ 安装指令
                         ▼                                               ▼
 ┌──────────────── 数据面 ────────────────────────────┐        ┌──── 主机执行 ─────┐
 │ MQTT Broker（Mosquitto + broker 控制插件）            │        │ tp-agent：只负责   │
 │ 上行接入 uplink → 可信受理账本 → 当前值 / 历史写入     │◀──────▶│ 拉包、验包、启停    │
 │ 下行命令 downlink → 命令账本 → 回执                   │ 插件流量 │ 插件容器           │
 └───────────────┬──────────────────────────┬─────────┘        └────────┬──────────┘
                 ▼                          ▼                           ▼
  ┌── 关系库：GORM ──────────┐  ┌── 专用 Adapter ──────────────┐  ┌── 插件（独立进程/容器）──┐
  │ PostgreSQL（默认）        │  │ 时序：TimescaleDB（默认）│TDengine│  │ 南向：HA / Modbus / HTTP… │
  │ MySQL（可配置替换）        │  │ 缓存：Redis                    │  │ 北向：northbound          │
  └──────────────────────────┘  │ 消息：MQTT Broker 驱动          │  │ 通知：SMTP / App 推送…     │
                                └────────────────────────────────┘  └──────────────────────────┘
```

### 1.2 五条架构规则（所有任务都要遵守）

| # | 规则 | 具体含义 | 依据 |
|---|---|---|---|
| A1 | **控制面与数据面分离** | 控制面是 Encore 服务，负责“定规则、发身份、管配置”。数据面（Broker、上行、下行）负责“搬数据”，在控制面短暂不可用时仍按已下发的规则运行。数据面不直接改控制面的表；控制面不在数据面的热路径上同步调用 | Encore 任务书 §2.1 |
| A2 | **关系库只用 GORM** | 普通增删改查一律通过已有的 GORM/DAL 实现。特殊语义（JSON 查询、条件 upsert、队列领取、锁）在**所属模块的 repository 里**分别写小范围的 PG 和 MySQL 实现。不新增第二套 ORM，不做万能 SQL 接口 | Encore 任务书 §2.3 |
| A3 | **其他数据库走专用 Adapter** | 时序只管**遥测历史**（批量写、范围读、聚合、删除、健康检查、关闭），用普通 Go 接口实现，不新增服务进程。最新值、属性、回执仍归关系库。通过 `storage.history_backend` 切换 | Encore 任务书 §2.4；`internal/app/history_backend.go` |
| A4 | **业务能力用插件扩展** | 南向、北向、通知能力做成独立容器插件。由 Agent 安装启停；插件以**受管运行时身份**（服务 + 租户 + 代次，可撤销）访问平台。不提供匿名入口，也不让所有插件共用一个管理员账号 | 插件设计 §1.2、§5.1、§7.4；`docs/legacy-plugin-runtime-guard-v1.md` |
| A5 | **不做 API 桥** | 老 Gin 由 Encore 逐步替换。不新增“Encore 回调老 Gin”的转换层（如 `communityclient`、`automationcompat`）。已有的桥随 Gin 退役一起删除 | 用户决定 2026-11-04 |

---

## 2. 已定：废弃 SQL 翻译器，按 GORM 重做（方案乙）

**背景**：MySQL 兼容原本靠 `pkg/sqldialect`（运行时把 PG 的 SQL 文本翻译成 MySQL 的 SQL）和 `encore-api/pkg/dbx`（模拟 RETURNING、映射错误码）实现。develop 里已经有它们，用在 `initialize/platform_driver.go`（平台的 gorm 流量）、`encore-api/automation/store`、`encore-api/platformconfig/store`、`encore-api/devicemetadata`；`wp/d1-dbx-returning` 又把它推广到了其余 Encore 模块。这违反 11-01 任务书 §2.3 的“禁止 SQL 字符串翻译器”，也违反 A2“关系库只用 GORM”。

**决定（用户，2026-11-04）**：整套废弃，不留过渡层。

**重做原则**：
1. 所有关系库访问都用 **GORM**：平台根模块用 `gorm.io/driver/postgres` 和 `gorm.io/driver/mysql`；Encore 各模块在需要多引擎的地方，也改成**应用自管的 GORM 连接**（Encore `sqldb` 只支持 PG）。
2. GORM 表达不了的语义（`FOR UPDATE SKIP LOCKED` 队列领取、条件 upsert、咨询锁、递归查询、JSON 查询），在**所属模块的 repository 里**分别写 PG 和 MySQL 两个显式实现，用方言枚举选择。**禁止**运行时改写 SQL 文本。
3. 公共底座只做四件事：连接与配置（`relational.driver`）、方言标识、错误分类（唯一冲突、不存在、超时、死锁）、迁移协调。不做万能 SQL 接口，也不做第二套 ORM。
4. 迁移：PG 的已执行迁移**一字不改**；MySQL 有自己的 baseline 和增量迁移文件，作为普通文件提交和维护。**不在构建或运行时从 PG 迁移自动生成**；可以一次性借助脚本起草，之后人工审查并提交。
5. 可复用 `wp/d1-dbx-returning` 的部分：PG 对 MySQL 的差分测试场景（devicemetadata 各 store、跨租户 52 步、网关守卫、校验函数 74 个取值等）作为验收用例；MySQL 触发器和 CHECK 替代的语义清单；冒烟脚本 `smoke_channels.sh`、`smoke_history_chain.sh`。
6. 完成后从仓库中删除 `pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx` 及翻译驱动，并加一个检查，防止它们回来。

---

## 3. 现状（截至 2026-11-04）

| 方向 | 已完成、有证据 | 未完成或卡点 | 位置 |
|---|---|---|---|
| **清场** | 分支 48→13；`archive/*` 标签 41 个；worktree 25→9；系统盘可用 12→23 GiB；`docs/WORKSPACES.md` 初稿 | 主目录仍停在 `main`，有 255 个未提交改动，**rescue 提交没做**；归档分支里还有 5 个没删、3 个 worktree 没移除；`WORKSPACES.md` 还是清理前的快照；规则没提交进 develop | 后端仓库 |
| **数据库兼容** | Encore 各模块（devicemetadata、物模型、北向、文件、通知、connectorcatalog、marketinstall、automation）都能在 MySQL 上运行，并且有 PG 对 MySQL 的差分测试；平台中 PG 专用的 SQL 已清扫；`cmd/*` 工具遇到 MySQL 配置会拒绝；`smoke_channels.sh` 和 `smoke_history_chain.sh` 在 PG、MySQL、PG+TDengine、MySQL+TDengine 四种组合上都通过 | broker 控制面仍只支持 PG（你决定暂缓）；`encore build docker` 在本机空转，镜像没构建出来；命令下发到运行时没测；casbin 完整策略只有 3 条；**发现一个 OTA bug，还没修**（见 T1b） | `wp/d1-dbx-returning`（29 个提交，`9cd62bb9`）；核查入口 `docs/MYSQL-PROFILE-STATE-2026-11-04.md` |
| **时序库** | `storage.history_backend` 已支持 `postgres`（Timescale）和 `tdengine`；TDengine 的写、读、聚合、删除都在真实 TDengine 3.4 上测过；安装器有可选的 tdengine profile（默认关闭） | TDengine 仍标着 `experimental=true`，“没有在运行中的完整栈上验证过” | `internal/tdenginehistory/`、`internal/app/history_backend.go` |
| **插件一键安装** | 安装器两个档位（PG+Timescale、PG+TDengine）的新装、升级、中断恢复、搬迁、组件重启、签名验签安装都在真实容器上跑通；**Agent**（`tp-agent`，替平台启停插件容器）的激活、撤销、旧代次拦截、失联租约到期都在真实 Docker 上通过；签名插件目录工具、一条命令安装工具 `tools/plugin_install.py` 已完成；真实 HTTP 适配器能被 Agent 装起来；修了一个让所有协议插件都连不上的后端 bug（uuid 隐式转换，有回归测试） | **一键闭环没通**：后端封死了旧的 5 个匿名插件入口（返回 503 `PLUGIN_RUNTIME_IDENTITY_REQUIRED`），真实插件拿不到设备配置；受管运行时身份在后端和安装器两侧**都只是 WIP，没跑过**；HA 插件用的是 `device-connector-sdk-go v0.1.0`，还不会带新身份；正式信任根不存在；v1.3.0 已发布的镜像组合不通 | 后端 `claude/wp-plugin-install-e5e1f9`（2 个提交）；安装器 `thingspanel-installer-wp-i1` 的分支 `wp/i1-pg-profiles`（19 个提交，`6e5f356`）；SDK 在 `thingspanel-connector-foundation/sdk`；HA 插件在 `thingspanel-device-connector-homeassistant` |
| **补全缺失功能** | 357 条路由台账、少量 Encore 候选实现 | **暂不做** | `claude/wp-missing-api-5613cb`（冻结） |

---

## 4. 任务总览与施工顺序

| 编号 | 任务 | 依赖 | 预计 |
|---|---|---|---|
| **T0** | 清场收尾 | — | 半天 |
| **T1-0** | 关系库底座设计 + 翻译器拆除清单 | — | 1–2 天 |
| **T1-A** | 平台根模块回到纯 GORM（PG/MySQL） | T1-0 审定 | 2–3 天 |
| **T1-B** | devicemetadata 改用 GORM（PG/MySQL） | T1-0 审定 | 4–5 天（可能再拆） |
| **T1-C** | 其余 Encore 模块改用 GORM（按 T1-0 的清单拆成若干张小卡） | T1-0 审定 | 视清单而定 |
| **T1-D** | 删除翻译器 + 防回归检查 + 四组合冒烟 | T1-A、T1-B、T1-C | 1 天 |
| ~~T1b~~ | ~~修 OTA 推送 bug~~（已取消：OTA 不进社区版） | — | — |
| **T2** | 双库切换：文档 + MySQL 栈界面冒烟 | T1-D | 1–2 天 |
| **T3** | TDengine 从实验转为可用的可选时序库 | T1-A | 2 天 |
| **T4** | 后端：受管插件运行时身份收口 | — | 2 天 |
| **T5** | 安装器与 SDK：签发身份并带上身份 | T4 的接口说明 | 2 天 |
| **T6** | HA 插件打包进签名目录 | —（可提前做） | 1 天 |
| **T7** | HA 一键安装端到端：数据打通 | T4、T5、T6 | 2 天 |
| **T8** | 安装器单一发行版（PG+TimescaleDB） | T2、T3、T7 | 1–2 天 |

```
第 1 波（现在）：T0 清场收尾 ┃ T1-0 关系库底座设计 ┃ T4 后端插件身份 ┃ T6 HA 打包（HA 插件仓库）
第 2 波：        T1-A 平台根 GORM ┃ T1-B devicemetadata GORM ┃ T5 安装器和 SDK 身份 ┃ T1b OTA
第 3 波：        T1-C 其余 Encore 模块 ┃ T7 HA 端到端 ┃ T3 TDengine 转正（等 T1-A）
第 4 波：        T1-D 删除翻译器 → T2 双库切换 → T8 安装器单一发行版
```

**限额**：后端仓库同一时间最多 2 条改代码的线，而且**不能碰同一目录**。第 1 波是 T1-0（只建底座包，写设计）和 T4。第 2 波开始前，T4 必须已经交付；如果没有，T1-A 和 T1-B 只先开一个。T1-A 和 T3 都会碰 `internal/storage`、`internal/dal`，不能并行。T5、T6 在其他仓库，不计入后端的限额。

**派工方式**：每个任务开一个独立的 Sonnet 会话，按 §5 的描述执行。会话完成后，由总监会话审查交付；审查通过、用户同意后，才合入 develop。

---

## 5. 任务描述（逐个派发）

### 通用前言（每个任务都附上）

> 你在 ThingsPanel 项目里工作。先读后端仓库根目录的 `CLAUDE.md`（工作规则），再读 `docs/PROJECT-AUDIT-2026-11-04.md` 的 §1（目标架构和 5 条规则）和 §3（现状）。只做下面这一个任务，范围外的问题记进 `docs/TODO.md`。从对应仓库的 `develop` 切出 `wp/<编号>-<短名>`，在自己的 worktree 里改，并登记到 `docs/WORKSPACES.md`。不推送、不合并、不发布。完成后写 `docs/tasks/<编号>-DONE.md`：分支名、`git log --oneline develop..HEAD`、测试命令与结果（跳过的要说明原因）、没做完的部分。不要把单元测试通过写成端到端通过。

---

### T0 清场收尾

- **目标**：把清场剩下的几步做完。完整步骤见 `docs/CLEANUP-TASKBOOK-2026-11-04.md`（已标出哪些做完了）。
- **剩余工作**：
  1. **C2 主目录救援**：先检查 `configs/conf*.yml` 有没有密码；证据目录打包到 `/Volumes/80G/archive/`；其余改动提交到 `rescue/main-worktree-20261104`；主目录切到 develop。注意：根目录的 `CLAUDE.md`、`docs/PROJECT-AUDIT-2026-11-04.md`、`docs/CLEANUP-TASKBOOK-2026-11-04.md`、`docs/WORKSPACES.md` 这 4 个文件，要放进 C7 的规则分支，**不要只留在 rescue 里**。
  2. 已打标签但还没删的分支：`codex/community-roles-superadmin-device-read`、`codex/northbound-community-20261021`、`feature/market-solution-bundle-v1`、`thingmodel`、`wip/d1-dbx-returning`。先核对每个都有对应的 `archive/*` 标签，再删。
  3. 归档分支对应的 worktree：`~/.codex/worktrees/native-command-validation`（`thingmodel-upgrade-v1`）、`thingspanel-runtime-gateway`（`runtime-tenant-source-binding`）、`thingspanel-runtime-read-context`（`runtime-read-context`）。工作区干净就移除，然后删分支。
  4. 用当前状态重写 `docs/WORKSPACES.md`（现在那份是清理前的快照）。安装器、前端、kernel 三个仓库也要登记进去。
  5. **C7**：在 develop 上开 `wp/t0-work-rules`，提交 `CLAUDE.md`、本报告、清场任务书、`WORKSPACES.md`，并在 `AGENTS.md` 里加一节引用 `CLAUDE.md`。
- **完成标准**：主目录在 develop 上且 `git status` 干净；`git worktree list` ≤ 6 条；本地分支只剩 develop、main、进行中的 `wp/*`、rescue；规则分支等待审查。
- **铁律**：每一步删除前都要用户确认。

---

### T1-0 关系库底座设计 + 翻译器拆除清单

- **目标**：按 §2 的重做原则，定下关系库访问的统一做法，建好公共底座的最小骨架，列出完整的拆除和重做清单，作为 T1-A、T1-B、T1-C 的依据。**本任务不改业务模块。**
- **步骤**：
  1. **盘点**：列出 develop 和 `wp/d1-dbx-returning` 中每一处依赖 `pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx`、翻译驱动（`initialize/platform_driver.go` 等）、`sqldb.NewDatabase/Named` 的地方。按模块统计：文件数、SQL 语句数，以及其中 GORM 能直接表达的有多少、需要写 PG/MySQL 两个实现的有多少（并说明原因：锁、SKIP LOCKED、upsert、递归、JSON、RETURNING）。
  2. **设计**：写 `docs/RELATIONAL-ADAPTER-DESIGN.md`，内容包括：
     - `relational.driver` 配置怎么读、在哪里只解析一次；
     - 每个逻辑资源一个连接池，由谁创建、谁关闭；
     - Encore 模块如何拿到自管的 GORM 连接（替换 `sqldb` 的方式），同时保留原有资源身份和迁移历史映射；
     - 方言枚举和 repository 内双实现的写法范例：领取队列、条件 upsert、咨询锁各写一个；
     - 错误分类；
     - MySQL 迁移文件的目录约定和执行者，每个 schema 只能有一个迁移执行者。
  3. **骨架**：新建公共底座包（建议 `pkg/relational`，根模块和 Encore 模块怎么共享由你在设计中定，**不许复制两份**）：配置、`Open` 返回 `*gorm.DB`、方言枚举、错误分类，并配单元测试。不写任何 SQL 改写逻辑。
  4. **拆卡**：把 T1-A、T1-B、T1-C 拆成每张不超过 3 天的小卡。每张卡写明：改哪些文件；验收用 `wp/d1-dbx-returning` 里的哪些差分测试场景；和其他卡有没有文件冲突。
- **交付**：设计文档、骨架包和测试、拆卡清单 `docs/T1-CARDS.md`、`docs/tasks/T1-0-DONE.md`。**交付后先由总监审定，再开 T1-A、T1-B、T1-C。**

### T1-A 平台根模块回到纯 GORM

- **目标**：平台根模块（`initialize`、`internal/dal`、`internal/service`、`internal/storage`、`internal/repo`、`cmd`）在 PG 和 MySQL 上都只通过 GORM 访问；去掉翻译驱动。
- **步骤**：按 `docs/T1-CARDS.md` 中 T1-A 的卡片执行。`gorm.Open` 根据 `relational.driver` 选 postgres 或 mysql 驱动；平台所有 PG 专用 SQL（批量遥测冲突、条件属性 upsert、DO NOTHING 读行数、源出站队列领取、菜单 JSON 数组匹配等）改成 GORM 写法，或在所属 repository 中写双实现；平台基础 schema 和种子作为 MySQL 迁移文件提交。
- **验收**：develop 现有的 PG 测试一个不少地通过；`wp/d1-dbx-returning` 中对应的平台差分测试全部移植过来并通过；`scripts/smoke_mvp1.sh` 在两个库上都通过。

### T1-B devicemetadata 改用 GORM

- **目标**：`encore-api/devicemetadata` 在 PG 和 MySQL 上通过自管的 GORM 连接运行，不再依赖 `sqldb`、`dbx` 和翻译器。
- **步骤**：按 T1-CARDS 执行，预计会拆成 store、分组、设备、模板和凭据、导出和命令和回执、守卫触发器等几张卡。网关树守卫、模板模型选择、CHECK 替代等 MySQL 触发器作为 MySQL 迁移文件手工维护；跨租户保护在 MySQL 上不能依赖 PG 的 RLS，要在 repository 层显式加 tenant 条件。
- **验收**：`wp/d1-dbx-returning` 中 devicemetadata 的全部差分场景（存储 19、分组 35、设备 23、模板 18、凭据 17、导出 22、模拟器 19、回执 15、命令 19、跨租户 52、网关守卫 20、模板选择 15、校验函数 74 个取值）都移植过来并在两个库上通过；`encore test ./devicemetadata/...` 全绿。

### T1-C 其余 Encore 模块改用 GORM

- **范围**：automation、platformconfig、thingmodel、northbound、fileassets、l3-core（通知）、connectorcatalog、marketinstall。按 T1-CARDS 每个模块一张或多张卡。
- **验收**：每个模块对应的差分测试（物模型 31、北向 24、文件 48、通知 31、connectorcatalog 15，以及 automation 已有的 76 步和 80 步）都移植过来并通过；空库启动测试通过。

### T1-D 删除翻译器 + 防回归检查

- **目标**：仓库中不再有运行时 SQL 改写。
- **步骤**：删除 `pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx`、翻译驱动及其测试；加一个检查（测试或 CI 脚本），禁止再引入这些包或同类代码；在 PG、MySQL、PG+TDengine、MySQL+TDengine（broker 仍用 PG）四种组合上跑一遍 `smoke_channels.sh` 和 `smoke_history_chain.sh`。
- **完成后**：`wp/d1-dbx-returning` 打 `archive/` 标签后删除。

### T1b 修 OTA 推送统计到自己的 bug —— **已取消（2026-11-04，用户决定）**

- **决定**：社区版不包含 OTA，OTA 归企业版。T1b 的修复**不进社区版**。补丁和说明保存在共享笔记目录（`T1b-ota-push-self-count-fix.patch`、`T1b-STOPPED.md`），本地分支 `enterprise/ota-push-self-count-fix`（`00ebd2f1`）**不合并、不推送**。
- **来源说明**：这个任务是总监根据数据库兼容执行人的汇报（“发现 OTA 推送 bug”）临时加进来的，当时没有核对社区版的范围。今后凡是新增任务，先核对它在不在社区版范围内。
- **后续**（见 §7）：企业版接收补丁；企业版确认接收后，再单独开任务“从社区版移除 OTA”。

---

### T2 双库切换：文档 + MySQL 栈界面冒烟

- **目标**：用户只改配置就能把关系库从 PG 换成 MySQL，并且有一份照着做就能成功的文档。
- **背景**：两条冒烟脚本已经在四种组合上通过，所以本任务**不再重复写脚本**，只补文档和界面验证。
- **步骤**：
  1. 把散落在各处的开关（平台 `db.driver`、各 Encore 服务的 MySQL 环境变量、`storage.history_backend`）汇总成一张表，写进 `docs/DATABASE-SWITCH.md`。顺序是：新装时怎么选；已有 PG 数据怎么办（本期**不支持在线搬迁**，要写明）。
  2. 在 MySQL 栈上用浏览器走一遍：登录、设备管理、设备模板、设备分组、物模型、设备详情的遥测数据。逐页记下有没有 404 或 500。
  3. 照着文档从零搭一次 MySQL 栈，确认文档没有缺步骤。
- **范围外**：数据搬迁工具；broker 控制面。
- **完成标准**：文档写完并经过一次照做验证；界面冒烟结论表里没有 500，有的话逐条记进 TODO 并说明原因。

---

### T3 TDengine 从实验转为可用的可选时序库

- **目标**：把 `storage.tdengine.experimental=true` 这道开关去掉，让 TDengine 成为一个**有验证、可配置**的时序库选项。默认仍是 TimescaleDB。
- **背景**：TDengine adapter 已经存在；PG+TDengine 的历史链冒烟（上传、历史、分页、统计、导出、删除清理）已经通过。拒绝的原因只剩一条：“没有在运行中的完整栈上验证过”。
- **步骤**：
  1. 列出所有读写遥测历史的入口（历史查询、导出、统计、聚合、清理、看板），逐个确认都走同一个 history backend，**没有绕过它直接查 PG 的地方**。发现绕过的，改成走 adapter。
  2. 在完整的 PG+TDengine 栈（安装器的 tdengine profile）上，用前端走一遍设备遥测历史页面、导出和统计，跑完冒烟脚本。
  3. 核对 Encore 任务书 §2.4 里冻结的语义，并在 TimescaleDB 和 TDengine 上做差分：毫秒精度、UTC、同 key 同时间冲突、空桶、diff 定义、区间端点。
  4. 去掉 experimental 开关，把配置说明写进 `docs/DATABASE-SWITCH.md` 的时序库一节。
- **范围外**：InfluxDB（已取消）；Timescale 和 TDengine 之间的历史数据搬迁。
- **完成标准**：上面三类验证都有记录；`history_backend=tdengine` 不再需要设置 experimental 就能启动。

---

### T4 后端：受管插件运行时身份收口

- **目标**：用正规的插件身份替代被封死的 5 个匿名入口（heartbeat、device/config、devices、service/access/list、service/access），让插件能重新拿到设备配置。
- **背景**：`docs/legacy-plugin-runtime-guard-v1.md` 记录了封死的原因和恢复条件。分支 `claude/wp-plugin-install-e5e1f9` 有 2 个提交：一个是 brokercontrol uuid 修复（有测试，可以保留）；另一个是 WIP：签发和吊销 API、bearer 中间件（绑定服务、租户、代次，高代次吊销低代次）、按身份的租户和协议过滤设备配置。这个 WIP 能编译，但**没有测试，表是懒建的，没有迁移，没跑过**。
- **步骤**：
  1. rebase 到最新 develop（如果 T1 已经合入，就基于合入后的 develop）。
  2. 懒建表改成正式迁移，PG 和 MySQL 各一份，**不改已执行的迁移**。
  3. 把 5 个入口改为“**带有效受管身份就放行，否则仍然返回 503**”。匿名访问保持关闭，不加任何可以重新打开匿名入口的开关。
  4. 补测试：
     - 签发、吊销；
     - 高代次吊销低代次；
     - 失联租约到期后拒绝；
     - 跨租户拒绝；
     - 协议不匹配拒绝；
     - 身份失效后 5 个入口全部拒绝；
     - 用户 token 或调用方伪造的请求头不能冒充插件身份。
  5. 写 `docs/plugin-runtime-identity.md`，交给 T5 使用：谁签发身份、怎么交给插件、插件怎么带上（请求头格式）、怎么轮换。
- **范围外**：安装器和 SDK 的改动（T5）；broker 侧的 MQTT 凭据细分（如果插件的 MQTT 连接也需要独立凭据，先在文档里写明现状，记进 TODO）。
- **完成标准**：PG 和 MySQL 上测试全绿；接口文档写完。

---

### T5 安装器与 SDK：签发身份并带上身份

- **目标**：Agent 安装插件时，安装器向后端申请一个当前代次的身份，注入插件容器；插件通过 SDK 带着这个身份调用平台。
- **背景**：
  - 安装器分支 `wp/i1-pg-profiles`（`/Users/junhong/Downloads/code/thingspanel-installer-wp-i1`）的最新提交 `6e5f356` 是 WIP：“按代次签发受管运行时身份”，还没跑过。它之前的 Agent、目录、一条命令安装都已经验证过。
  - SDK 在 `thingspanel-connector-foundation/sdk`，HA 插件依赖 `device-connector-sdk-go v0.1.0`。
- **步骤**：
  1. 安装器：按 T4 的接口说明完成 `6e5f356`：安装、升级时签发新代次；卸载时吊销；重装时旧代次失效。
  2. Agent：把身份以环境变量或挂载文件的方式注入容器，**不写进镜像，也不写进日志**。
  3. SDK：调用平台的 5 个插件入口时带上身份；收到 503 `PLUGIN_RUNTIME_IDENTITY_REQUIRED` 时明确报错，不要无限重试。发一个新的 SDK 版本（例如 `v0.2.0`），在本地 tag 即可，不推送。
  4. 用已经验证过的 HTTP 适配器（`plugins/http-adapter.json`）回归：安装 → 拿到身份 → 取设备配置成功；卸载 → 再次调用被拒绝。
- **范围外**：正式信任根和 GitHub 签名（本期用本地测试密钥，在文档里写明）；前端安装按钮。
- **完成标准**：HTTP 适配器在新身份下安装、取配置、卸载、拒绝 4 步都有日志。

---

### T6 HA 插件打包进签名目录

- **目标**：让 Home Assistant 插件变成能被 `tools/plugin_install.py` 一条命令安装的成品。
- **仓库**：`/Users/junhong/Downloads/code/thingspanel-device-connector-homeassistant`（connectorKey=`homeassistant`，已有 Dockerfile）。
- **步骤**：
  1. 本地构建多架构镜像（至少 linux/amd64；如果本机是 arm，再加 linux/arm64），记录镜像摘要。
  2. 参照 `plugins/http-adapter.json`，在安装器里写 `plugins/homeassistant.json`：镜像摘要、端口、健康检查、必填配置（`HA_BASE_URL`、`HA_ACCESS_TOKEN` 由用户填写）、自动填写的项目（`CONNECTOR_SERVICE_IDENTIFIER`、`CONNECTOR_INSTANCE_ID`、`THINGSPANEL_BACKEND_URL`、MQTT 地址，**注意不要再指向 `gmqtt`**）。
  3. 用 `tools/plugin_catalog.py` 和本地测试密钥签名，生成目录，并验签通过。
  4. HA 插件升级 SDK 的工作留给 T5 完成后再做；本任务先保证镜像和目录条目正确。
- **完成标准**：目录验签通过；能用 `plugin_install.py` 让 Agent 把 HA 容器拉起来并通过健康检查（此时取设备配置可能仍是 503，属于预期）。

---

### T7 HA 一键安装端到端：数据打通

- **目标**：**一条命令装好 HA 插件后，HA 里的数据能进到 ThingsPanel，ThingsPanel 能控制 HA 设备。**这是本期插件方向唯一的验收。
- **依赖**：T4、T5（HA 插件已升级到新 SDK）、T6。
- **准备**：用 Docker 起一个 Home Assistant，开启 demo 集成或虚拟实体（虚拟灯、虚拟温湿度传感器），生成测试用的 long-lived token，**不提交，用完撤销**。平台用干净的 develop 栈（PG+Timescale，安装器装出来的）。
- **测试步骤**：
  1. `plugin_install.py` 一条命令安装 HA 插件，只填 HA 地址和 token。**不登录服务器手改配置，不重启主平台。**
  2. 插件发现 HA 实体，在平台上生成设备。
  3. 上行：在 HA 中改变传感器的值 → 平台的当前值和历史数据里都能看到。
  4. 下行：在平台上对灯下发开和关 → HA 状态跟着变 → 平台收到回执。
  5. 重复执行安装命令 → 不会出现第二个实例，也不会出现重复设备。
  6. 卸载 → 身份失效、容器停止；已有设备和历史数据保留；再用旧身份调用平台被拒绝。
  7. （可选）在 PG+TDengine 栈上重做第 3 步。
- **范围外**：前端“安装”按钮（插件设计第 2 阶段再做）；公开市场；其他插件。
- **完成标准**：第 1 到 6 步都有日志或截图（放证据目录，仓库只写索引）。失败的步骤写明原因，**不要在本任务里扩大范围去修**，另开 TODO。

---

### T8 安装器单一发行版（PG+TimescaleDB）

- **目标**：发行物只有一种：PostgreSQL + TimescaleDB + Encore + Agent。TDengine 和 MySQL 作为“改配置替换”的选项写进文档，不作为独立发行物。
- **背景**：安装器的两个档位已经在真实容器上验证过新装、升级、中断恢复和搬迁。11-02 复查发现的 F01（下载失败会破坏旧配置）要确认是否已经修复。
- **步骤**：
  1. 默认安装 PG+Timescale；tdengine profile 保留为可选配置（默认关闭，现在已经是这样）。
  2. 复核 F01：中途下载失败时，旧的 Compose 和配置不能被破坏。没修好就修。
  3. 解决 Encore 镜像构建：本机 `encore build docker` 空转，改走仓库自带的 Linux 容器构建通道，或者换一台机器构建，记录镜像摘要。
  4. 写《更换组件指南》：换关系库（引用 T2）、换时序库（引用 T3）、装或换插件（引用 T7）。
  5. 在干净机器上完成新装和升级各一次，然后执行 T7 的第 1 到 4 步。
- **范围外**：Windows 包；MySQL 发行版；v1.3.0 已发布镜像组合的修复（记入遗留，后续发版时一起处理）。
- **完成标准**：新装、升级、HA 插件安装三条路径都有日志；指南经用户审阅。

---

## 6. 暂缓与后续路线（现在不派工）

| 项 | 说明 |
|---|---|
| 补全缺失功能 / Gin 退役 | 以 357 条路由台账作为退役清单。先让前端改调 Encore，再关掉对应的 Gin 路由，最后删除 API 桥代码。等 T0–T8 完成后再看 |
| 插件市场界面、作者发布、公开市场 | 插件设计第 2 阶段 |
| 正式信任根、GitHub 签名 | 本期用本地测试密钥；对外发布前必须补上 |
| broker 控制面 MySQL 化 | 暂缓 |
| Windows 包、MySQL 发行版、InfluxDB | 不做 |
| 11-02 复查的其他缺口（F02 父子设备、F03 A08 安装幂等、F04 租户最新遥测、F07 ThingsVis、F08 Horizon、F09 前端） | 不在本期两个目标里，保留在原清单中。其中 F03 与插件安装有关，T7 如果碰到要记录下来 |
| 已归档的分支（`archive/*` 标签） | 等 Gin 退役时再评估是否还有价值 |

---

## 7. 进度与交接（2026-11-04 晚）

> 接手的人只读这一节就能开工。每张待办卡都写了：前置、范围、完成标准。派工时，在卡片前面附上 §5 的“通用前言”。

### 7.1 已完成并合入（本地 develop，**均未推送**）

| 任务 | 结果 | 位置 |
|---|---|---|
| T0 清场 | 分支 48→9，worktree 25→9；主目录改动全部存入 `rescue/main-worktree-20261104`；规则文件 `CLAUDE.md` 已入库 | 后端 develop `07caf005`；`docs/tasks/T0-DONE.md` |
| T1-0 关系库设计 | 设计、盘点、26 张卡、骨架包 `pkg/relational` | 后端 develop `10368778`；`docs/RELATIONAL-ADAPTER-DESIGN.md`、`docs/T1-CARDS.md` |
| T4 插件运行时身份 | 5 个插件入口只放行受管身份，否则返回 503 | 后端 develop `563e2497`；`docs/plugin-runtime-identity.md` |
| T5 + T6 + T7 + T7c 插件安装链 | Agent、签名目录、一条命令安装、按代次签发和吊销身份、`binding: none`、自动登记服务插件、稳定网络别名；HA 插件周期同步设备 | 安装器 develop `4470874`（18 项单测通过） |
| T7b HA 数据打通 | 用 develop 重建 Broker 镜像后，**在真实 Home Assistant 上上行、下行都通过**（ESP-01S 灯：平台下发 on→off→on，回执 `result:0`；状态变化写入历史） | 后端 develop `704256a1`；`docs/tasks/T7b-DONE.md` |

合并后，develop 编译和 vet 都通过；`internal/middleware`、`sql`、`internal/pluginidentity`、`pkg/relational` 的单测通过。需要真实数据库的测试这次没有重跑，以各任务分支上的记录为准。

### 7.2 进行中

| 任务 | 状态 | 位置 |
|---|---|---|
| T1-A0 关系库公共接线 | 已写完迁移执行器、锁、Encore 粘合层，代码还没提交。Encore 带 replace 能编译通过（O2 不需要后备方案）；镜像打包失败的原因是网络拉取 CA 证书超时，不阻塞。只剩“惰性连接”实测、提交和交付说明 | 会话 `t1-a0-wiring`，worktree `.claude/worktrees/wp-t1-a0-wiring-42b3c3` |

### 7.3 总监决定（2026-11-04 晚，按用户授权代为决策）

1. **OTA 不进社区版**（用户决定）。T1b 取消，补丁交给企业版。等企业版确认接收后，再开“社区版移除 OTA”的任务。
2. **插件一键安装的边界**：“一条命令装好插件”由安装器负责；“选择要接入的 HA 实体、建成设备”是用户在平台界面里完成的正常配置步骤，与插件设计文档 §7.4“现场信息由用户补齐”一致，**不要求自动化**。但安装器必须自动完成其余的机器步骤：迁移、Broker 设备范围刷新、重启，见卡 P1。
3. **合并**：审查通过的成果合入各仓库本地 develop。**推送远端、发布镜像、发布 SDK 只由用户执行**（见 7.6）。
4. **证据目录**：`/Volumes/80G` 写入只有约 5 MB/s，证据暂放 `/Volumes/500G/archive/evidence/`；Go 缓存已改到 `/Volumes/500G/.gocache`。
5. **HA 令牌明文进日志**是安全问题，排在后端插件方向的第一位（卡 P2）。令牌在数据库里的加密存储另开卡（P2b），因为它需要先设计密钥管理。

### 7.4 与最初设计相比，还差什么

| 方向 | 最初目标 | 现在 | 还差 |
|---|---|---|---|
| 插件一键安装（HA） | 一条命令安装，数据打通 | 安装通了，数据也通了，但还要手工执行迁移、刷新 Broker 范围、重启 | P1、P2、P3、P4 |
| 关系库（方案乙） | 全部改用 GORM，删除翻译器 | 底座设计完成，A0 接近完成 | A1–A4、B1–B6、C1–C11、D1–D3（约 26 张卡，见 `docs/T1-CARDS.md`） |
| 双库切换 | 改配置就能换 PG 或 MySQL | 未开始 | T2（等 T1-D） |
| 时序库 | TDengine 作为可选项，去掉实验开关 | 未开始 | T3（等 A1–A3） |
| 发行版 | 只有 PG+TimescaleDB 一种 | 安装器链已合入 | T8（等 P1、T2、T3） |
| 补全缺失功能 / Gin 退役 | — | 暂停 | 不动 |

### 7.5 待办卡（按优先级排；后端同一时间最多 2 条改代码的线）

**P1 安装器：把 HA 接入剩下的机器步骤全部自动化**（安装器仓库，不占后端的线；前置：无）
- 安装和升级流程自动执行后端 PG 迁移（含 `sql/33.sql`）和 `sql/broker/001–017`，重复执行安全。
- 修正 `config/native/prepare.py --refresh-devices`：平台服务账号 `tp-community-platform` 的 `allowed_device_ids/allowed_tenant_ids` 必须包含服务接入点（插件）设备，否则下行命令会被 Broker 静默丢弃（T7b 发现 3）。
- 平台新增或删除插件设备后，自动刷新范围，再依次重启 broker-owner → mosquitto（触发方式：安装器命令或 Agent 周期任务，二选一，写进设计说明）。
- **完成标准**：在新栈上，只做“一条命令安装 + 在平台界面选 HA 实体建设备”，不执行任何手工命令，上行和下行都通过（可以用 T7b 的 demo HA）。

**P2 后端：HA 令牌不再泄露**（后端，占一条线；前置：无）
- 创建接入点时，请求日志和操作日志里的 `voucher` 要脱敏；设备列表接口不再用字符串拼接把 voucher 放进 URL（改为结构化传参或正确编码）；错误日志不打印 voucher。
- **完成标准**：带一个假令牌走完创建接入点、设备列表、出错路径三条链路，检查日志和操作日志表，`grep` 令牌结果为 0；有回归测试。

**P2b 后端：voucher 加密存储**（先出设计，不写代码；前置：P2）
- 内容：`service_access.voucher` 明文存储改为加密存储或引用，写清楚密钥管理方案，交总监审定。

**P3 后端：Broker 丢弃命令时要能看到失败**（后端，占一条线；前置：P2 合入）
- 现状：命令被 Broker 丢弃时，状态仍然是 `broker_acknowledged`、显示“成功”。改为：在约定时间内没有设备回执，就标为超时失败，界面上可以看到。
- **完成标准**：复现“平台账号不含插件设备”的场景，命令最终状态显示失败或超时，而不是成功；有测试。

**P4 发布（只能由用户执行，需要账号权限）**
- 把 SDK `v0.2.0`（`/Users/junhong/Downloads/code/thingspanel-device-connector-sdk-go`）推到 GitHub；然后 HA 插件删掉 `go.mod` 里的本地 `replace`，合入 HA 仓库主分支。
- 用 develop 构建并发布 `thingspanel-mosquitto`、后端、HA 插件的多架构镜像（amd64 + arm64）。
- 建正式信任根，签名目录（本期用的是本地测试密钥）。

**P5 HA 插件小改进**（HA 仓库；前置：P4 中的 SDK 发布）
- 启动时推送一次当前状态（现在只在状态变化时上报，刚接入的设备在变化前没有数据）；同步时处理设备删除。

**关系库线**：按 `docs/T1-CARDS.md` 的排期执行。A0 交付并合入后，开 A1（平台启动与连接）和 B1（devicemetadata 基础），注意两线不能和 P2、P3 同时占满后端的线（同一时间最多 2 条）。**建议顺序**：先开 P2 + A1，P2 完成后接 P3，A1 完成后开 B1。

**之后**：T3（等 A1–A3）→ T1-D → T2 → T8。

**企业版 / OTA**：企业版接收 T1b 补丁；确认接收后，开“社区版移除 OTA”任务（路由、API、服务、DAL、模型、`query/gen.go`、MQTT 订阅、`sql/1.sql` 中的三张表需要单独评估迁移方式）。

### 7.6 需要用户本人做的事

1. P4 全部（推送 SDK、发布镜像、建正式信任根）。
2. 确认删除：`~/.codex/worktrees/` 下 3 个空目录；主目录里两个已归档的证据目录 `docs/gmqtt-evidence-2026-10-19/`、`-20/`。
3. 检查 `/Volumes/80G` 盘的健康状况（写入只有约 5 MB/s）。
4. 企业版 OTA 补丁交给谁接收。

### 7.7 当前 worktree 与分支（合并后可以清理）

- 后端：`.claude/worktrees/` 下 T0、T1-0、T4、T7b 的 worktree 都已合入，**可以删除**（删除前列清单确认）；`wp-db-compat-f6f630`（d1 参考，等 T1-D 完成后归档）；`wp-missing-api-5613cb`（暂停）；`wp-plugin-install-e5e1f9`（已被 T4 取代，可以归档）。
- 安装器：`thingspanel-installer-wp-i1`、`-wp-t5`、`-wp-t6` 都已合入安装器 develop，可以删除。
- HA 插件：`.claude/worktrees/t6`（分支 `wp/t6-ha-package`，**没合入**，等 P4 的 SDK 发布）。
- SDK：新的本地仓库，等 P4 推送。
- 以上变化都要同步到 `docs/WORKSPACES.md`。

### 7.8 进度更新（2026-11-04 夜）

| 卡 | 状态 | 位置 |
|---|---|---|
| T1-A0 关系库公共接线 | ✅ 已合入 | 后端 develop `95c94150` |
| P2 HA 令牌不再泄露 | ✅ 已合入：新增 `pkg/logredact`，请求日志、操作日志表、访问日志、错误日志全部脱敏，设备列表的 voucher 改为 URL 编码；真实 PG 回归测试由总监复跑通过 | 后端 develop `c603f9b1` |
| P1 安装器自动化 | ✅ 已合入：`--bootstrap` 自动执行 `sql/33.sql`；平台 Broker 账号的范围包含插件设备；增删设备后由监视进程自动刷新范围并按顺序重启；真实栈上无人工干预，上行和下行都通过；总监复跑 107 + 18 项测试通过 | 安装器 develop `27840cd` |

P1 留下的已知局限：
- 监视进程不是系统服务，机器重启后要等下一次 `renew` 才会被拉起；
- `docker-compose.yml` 仍引用旧的 v1.3.0 Broker 镜像，等 P4 发布新镜像后再改；
- 插件设备上限 256 台。

**下一批可以派的卡**（见 `docs/DISPATCH-PROMPTS-2026-11-04.md`）：**P3**（P2 已合入，前置已满足）、**T1-A1**、P2b（只出设计）、E3（清理工作目录）。另外新增一张待排的小卡：把 P1 的监视进程改成 launchd/systemd 服务，排在 T8 之前。

### 7.9 监督复核（2026-11-04）

- T1-A1 已审查并合入本地 develop `7bc4aca1`。负责人复跑 `go test ./initialize/... ./internal/app/...`、`pkg/relational` 全包测试及独立 MySQL 8.4 启动/旧回执导入/鉴权失败清理测试，通过。PG 启动实测采用局部 fixture，未验证完整 Timescale 栈或生产升级；详见 `docs/tasks/T1-A1-DONE.md`。
- A1 只完成平台启动接线。平台与 Encore 的环境变量前缀尚需协调，devicemetadata 旧 driver 尚待 B 卡处理；legacy MQTT 遥测仍经 PostgreSQL 专属历史存储，MySQL 遥测链待后续 A3，不能宣称整栈可用。
- 对已合入 P2 的独立复审发现表单编码、URL userinfo 等脱敏绕过；补救分支 `wp/p2-fix-redaction` 正在修复和复审，P2 不应据 §7.8 的描述视为安全问题全部关闭。
- 对已合入 P1 的复审发现 scope 写入后若重启失败，下次刷新可能因配置未变化而不再重试；并发 scope 更新和 watcher 启动未全程互斥。安装器补救任务进行中。另外缺少 `33.sql` 仍按旧镜像兼容跳过，不能识别新镜像漏打包。
- 共享 Go 缓存 `/Volumes/500G/.gocache` 写入曾报 `illegal byte sequence`；相关工作改用任务独立缓存，未清理或覆盖原缓存。
- P1 补救已审查并合入安装器本地 develop `a540634`：保存待重启状态、重启失败后重试，并串行化范围更新与 watcher 启动；负责人复跑 93 项相关测试通过。新镜像缺失 `33.sql` 无法识别的问题仍未解决，补救尚未重新跑真实 HA 端到端。
