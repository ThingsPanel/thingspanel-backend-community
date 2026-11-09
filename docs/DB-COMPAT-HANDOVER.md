# 数据库兼容（PG / MySQL / TDengine）— 接手说明

> **给接手的人**：从头到尾读一遍再动手。每一节都很短。读不懂的地方先问，**不要猜**。
> 日期：2026-11-09。基线：`develop` @ `c0ab14d1`（已推到远端）。
> 这件事之前做了很久没做成，越做越乱（原因在 §5）。这次的做法是：**一张卡一张卡做，每张卡都有能跑的验收命令，做不完就停下来报告，不往里加东西。**

---

## 任务清单（Checklist）

按顺序做。打勾前必须满足该卡“完成标准”里的**每一条**。每张卡的细节在 §7。

**开工前（半天）**
- [ ] **P0-1** 读完本文、`CLAUDE.md`、`docs/T1-CARDS.md` §0（[§7.0](#70-开工前p0)）
- [ ] **P0-2** 搭好测试数据库，跑通一次差分测试示例（[§8](#8-环境与命令速查)）
- [ ] **P0-3** 负责人对 §10 的决定给出答复（没答复就按“默认”执行）

**线 1：平台根模块 + 时序（串行）**
- [ ] **DB-1** T1-A2 收尾：接手半成品分支，对齐 develop，补完，交付（[§7.1](#71-db-1t1-a2-收尾平台-dal--repo--service)）
- [ ] **DB-2** T3：TDengine 去掉“实验”标记 + 处理 tptodb 旧分支（[§7.2](#72-db-2t3tdengine-转正)）
- [ ] **DB-3** T1-A3：平台存储与遥测（[§7.3](#73-db-3t1-a3平台存储与遥测)）
- [ ] **DB-4** T1-A4：平台验收（[§7.4](#74-db-4t1-a4平台验收)）

**线 2：devicemetadata（串行，同一个分支）**
- [ ] **DB-5** T1-B1 基础与 store（[§7.5](#75-db-5-至-db-10t1-b1--b6devicemetadata)）
- [ ] **DB-6** T1-B2 分组
- [ ] **DB-7** T1-B3 设备、网关、模板绑定
- [ ] **DB-8** T1-B4 模板与凭据
- [ ] **DB-9** T1-B5a 导出 → T1-B5b 命令、回执、模拟器
- [ ] **DB-10** T1-B6 收尾、跨租户、校验函数

**线 1 空出来后：其余 Encore 模块（一次只做一张）**
- [ ] **DB-11** C2 platformconfig、C11 authcore
- [ ] **DB-12** C3 northbound、C4 fileassets + C7 connectorcatalog
- [ ] **DB-13** C5 / C6 notification
- [ ] **DB-14** C1 thingmodel（要等 B1 合入）
- [ ] **DB-15** C8 marketinstall
- [ ] **DB-16** C9 / C10 automation

**最后（全部合入后）**
- [ ] **DB-17** T1-D1 删除翻译器
- [ ] **DB-18** T1-D2 防回归检查
- [ ] **DB-19** T1-D3 四组合冒烟 + T2 切换文档

> 同一时间**最多 2 条改代码的线**（`CLAUDE.md`）。线 1 和线 2 可以同时进行；C 系列只能在某条线空出来后接着做。

---

## 1. 一句话说清楚

ThingsPanel 现在只能稳定用 **PostgreSQL（+TimescaleDB）**。目标是：**改一个配置，就能把关系库换成 MySQL；时序库可以选 TimescaleDB 或 TDengine**。实现方式**只能是 GORM**，GORM 表达不了的少数语义在所属模块里写 PG、MySQL 两份显式代码。**禁止**任何“运行时把 PG 的 SQL 文本改写成 MySQL”的做法。

### 红线（违反任何一条，交付直接退回）

1. **不准新增**对这些包的引用：`pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx`、`pkg/dbdialect`。它们是要删除的“翻译器”。
2. **不准**对 SQL 字符串做 `strings.Replace`/正则替换后再执行；不准包装数据库驱动去改写 SQL。
3. **不准**写“万能 SQL 接口”或第二套 ORM。公共包 `pkg/relational` 只做：连接、方言、错误分类、迁移、`LockTx`、差分测试框架。
4. **PG 已执行过的迁移文件一个字都不能改**。MySQL 用自己的迁移文件（目录和执行者见 `docs/RELATIONAL-ADAPTER-DESIGN.md` §6）。
5. 业务代码里**不准**读 `relational.*` 配置或 `TP_RELATIONAL_*` 环境变量；方言只能用 `relational.DialectOf(db)` 从连接上取。
6. 不推送、不合并、不发布；不用 `git stash`；删除任何东西先问（`CLAUDE.md`）。
7. 测试被跳过（`SKIP`）**不等于**通过。差分测试必须**两个库都真的跑了**。

---

## 2. 现在是什么状态（2026-11-09 核实）

### 2.1 总表

| 部分 | 状态 | 证据（你可以自己复查） |
|---|---|---|
| 公共底座 `pkg/relational` | ✅ 完成：连接、方言、错误分类、MySQL 迁移执行器（带锁和校验和）、`LockTx`（PG 咨询锁 / MySQL 锁行）、差分测试框架 `reltest` | `pkg/relational/`；`docs/tasks/T1-A0-DONE.md`；`cd pkg/relational && go test ./...` 通过（没配数据库时真实库测试会跳过） |
| 平台启动与连接 | ✅ 完成：按 `relational.driver` 选 PG 或 MySQL，MySQL 迁移走 `relational.Migrate` | `initialize/platform_driver.go`；`docs/tasks/T1-A1-DONE.md` |
| 平台 DAL/repo/service（A2） | 🟡 **做了一半，停在 2026-11-05**：分支 `wp/t1-a2-platform-dal-service` @ `3b68fe6a`（提交名就叫 `wip: checkpoint`），改了 21 个文件（+1131/−216），**比 develop 落后 64 个提交**，还有 2 个未提交文件，**没有完成说明** | worktree `.claude/worktrees/wp-t1-a2-platform-dal-service`；`git -C <它> status` |
| 平台存储与遥测（A3）、平台验收（A4） | ❌ 没开始 | — |
| devicemetadata（B1–B6） | ❌ 没开始。这个包是最大的一块：303 条 SQL，77 条要写两份 | 翻译器引用：**33 个源文件、19 个测试文件**都在这里 |
| 其他 Encore 服务（C1–C11） | ❌ 没开始。这些服务现在**只支持 PG**（用 Encore 的 `sqldb`） | 7 个 `sqldb.NewDatabase` 声明：notification、file_storage、market_install、connector_runtime、device_metadata（thingmodel 共用）、automation、northbound |
| Encore 侧的 GORM 连接 `encore-api/pkg/relationaldb` | ✅ 已写好，但**还没有任何 Encore 服务在用** | `grep -rl relationaldb encore-api --include=*.go` |
| 删除翻译器（D1–D3） | ❌ 没开始。翻译器仍被引用：devicemetadata 33、`internal/dal` 2（`dbdialect`）、`platformconfig/store` 2、`automation/store` 1（均为非测试文件） | 见 §8 的检查命令 |
| 时序：TDengine | 🟡 代码完整（10 个读写删方法，有真实 TDengine 差分测试），但**还挂着“实验”开关**，不设 `storage.tdengine.experimental=true` 启动会被拒绝 | `internal/app/history_backend.go:43`、`internal/app/storage.go:113` |
| 时序：旧的 tptodb gRPC 读分支 | 🟡 **仍在代码里**，和新的 `HistoryBackend` 两套并存 | `internal/app/grpc_service.go`、`internal/dal/telemetry_datas.go`、`internal/dal/telemetry_current_datas.go`、`internal/service/device_latest_telemetry.go`、`third_party/grpc/tptodb_client/` |
| 只支持 PG 的残留 | 🟡 已知 2 处：MySQL 下租户最新遥测汇总直接返回 503；保留期清理里执行 `VACUUM FULL` | `internal/service/device_latest_telemetry.go:27`、`internal/dal/telemetry_datas.go:289` |
| MySQL 迁移文件 | 平台：`sql/mysql/` 共 18 个（001–018）。Encore：develop 上只有 `devicemetadata/mysqlschema/`（旧位置、旧写法）；其他服务的 MySQL schema 只在 d1 分支里有**草稿** | `ls sql/mysql`；见 §2.3 |
| broker 控制面 | 只支持 PG，**负责人已决定这次不做** | `internal/broker*`、`sql/broker` |

### 2.2 构建与测试的基线（2026-11-09 实测）

| 命令 | 结果 | 注意 |
|---|---|---|
| 根目录 `go build ./...` | ✅ 通过 | |
| `cd encore-api && go build ./...` | ❌ 报 `fileassets/recovery.go:18: undefined: RecoverInterruptedUploads` | **这不是你的问题**：Encore 服务方法做定时任务入口时，包级函数由 Encore 工具链生成，普通 `go build` 看不到。**encore-api 一律用 `encore test` 检查**，不要用 `go build` |
| `cd encore-api && encore test ./pkg/relationaldb/... -count=1` | ✅ 7 秒通过 | 编译检查就用 `encore test ./<包>/...` |
| `cd encore-api && encore check` | ⚠️ 编译能过（登记了 272 个接口），但随后因缺通知服务的隐私配置而退出，**然后卡 8 分钟等健康检查超时** | **不要用 `encore check` 当编译检查**，白等 |
| `cd pkg/relational && go test ./...` | ✅ 通过 | 没配数据库环境变量时，真实库测试会跳过 |

### 2.3 能参考的旧成果：d1 分支（**只拿场景，不拿代码**）

`wp/d1-dbx-returning` @ `9cd62bb9`（worktree `.claude/worktrees/wp-db-compat-f6f630`）是以前用“翻译器”做的 MySQL 版本，**已被否决**，不会合并。但它有两样东西很值钱：

1. **差分测试场景**：每一步调用什么业务接口、期望什么结果。d1 相对 develop **新增的** 23 个测试文件：devicemetadata 13 个、`internal/dal` 3 个、automation / fileassets / connectorcatalog / notification / marketinstall / northbound / thingmodel 各 1 个。**另外**，automation、platformconfig 的一批 MySQL 契约测试**已经在 develop 上**（见 §7.6），不用从 d1 搬。
2. **MySQL schema 草稿**：各服务 `mysqlschema/*.sql`（含触发器、CHECK 替代）、冒烟脚本 `scripts/smoke_channels.sh`、`scripts/smoke_history_chain.sh`、经验文档 `docs/MYSQL-PROFILE-STATE-2026-11-04.md`。

取法：`git show wp/d1-dbx-returning:<路径>`。**只复制场景和期望值**，把 `dbx.DB` 换成 `*gorm.DB`、`$1` 换成 `?`、`dbxtest.Compare` 换成 `reltest` 的入口。**不要复制任何翻译器代码。**

---

## 3. 应该做到什么状态

### 3.1 最终状态（全部卡做完）

| 组合 | 最终要求 |
|---|---|
| PG + TimescaleDB（默认） | 和今天一样能用，所有现有 PG 测试不少一个地通过 |
| PG + TDengine | **不需要“实验”开关**就能启动；历史页面、导出、统计在整栈上验证过 |
| MySQL + （历史存储见 §10 决定 A） | 平台和全部 Encore 服务能在 MySQL 上启动和工作；每个模块有 PG/MySQL 差分测试 |
| MySQL + TDengine | 无 PG 也能完成“上报 → 历史 → 统计 → 导出 → 删设备清理”（broker 控制面除外，它仍需 PG） |

以及：
- 仓库里 `grep -rn 'pkg/sqldialect\|pkg/dbx\|pkg/dbdialect' --include='*.go'` **结果为空**（今天是 38 个非测试文件），且有一个自动检查防止它们回来；
- `docs/DATABASE-SWITCH.md`：用户照着做，只改配置就能切换（不含数据搬迁）。

### 3.2 每个阶段的“完成”是什么（可单独交付）

| 阶段 | 完成意味着 |
|---|---|
| 线 1 前半（DB-1、DB-2） | 平台根模块的非遥测 SQL 不再依赖翻译器；TDengine 正式可选 |
| 线 1 全部（到 DB-4） | 平台根模块在 PG 和 MySQL 上都通过 `scripts/smoke_mvp1.sh` |
| 线 2（DB-5 至 DB-10） | devicemetadata 不再引用 `dbx`，d1 的全部 devicemetadata 场景在两个库上通过 |
| C 系列 | 每个服务在 MySQL 上能启动，差分场景通过 |
| D 系列 | 翻译器删除，四组合冒烟通过 |

---

## 4. 工作范围

### 做
- 平台根模块：`initialize`、`internal/dal`、`internal/repo`、`internal/service`、`internal/storage`、`internal/uplink`、`internal/tdenginehistory`、`internal/automationguard`、`internal/adapter`、`cmd/*` 中的 SQL。
- Encore 服务：devicemetadata、thingmodel、platformconfig、northbound、fileassets、notification、connectorcatalog、marketinstall、automation、authcore（`encore-api/internal`）。
- 每个服务的 MySQL 迁移文件（`<服务>/migrations-mysql/`）和 `PG-MAP.txt`。
- TDengine 去实验标记；tptodb 旧分支按 §10 决定处理。
- 删除翻译器、加防回归检查、四组合冒烟、切换文档。

### 不做（遇到了写进 `docs/TODO.md`，不要顺手修）
- broker 控制面支持 MySQL（`internal/broker*`、`sql/broker`、`cmd/tp-broker-*`、`internal/storage/telemetry_broker.go`）。MySQL 配置下这些必须**拒绝启动**，不是改造。
- MySQL + TimescaleDB 组合（除非 §10 决定 A 改了）。
- 达梦、Kingbase、PolarDB、InfluxDB。
- PG ↔ MySQL、Timescale ↔ TDengine 的数据搬迁工具。
- 补全缺失接口（357 路由台账，已暂停）、OTA（不进社区版）。
- 前端改动（除非某个页面在 MySQL 上报 500，且原因在后端 SQL）。

---

## 5. 存在哪些问题（为什么之前没做成）

### 5.1 历史教训
1. **走了捷径被推翻**：d1 用“运行时翻译 SQL”把四种组合都跑通了，但违反“只用 GORM”的规则，2026-11-04 整套废弃。**教训：规则不是建议。**
2. **线开太多、目录太乱**：同时开多条线、多个 worktree，工作做了一半没人收尾（A2 就是这样停在半路的）。**教训：一次一张卡，做完交付再开下一张。**
3. **工作量被低估**：Encore 一共 833 条 SQL（这是下限），153 条要写两份；devicemetadata 一个包就 303 条。**教训：卡超过 3 天就停下拆小。**
4. **决定没拍板就开工**：MySQL 档的历史存哪、tptodb 去留、导出命令能否首发不支持……悬而未决让卡卡住。**教训：§10 先答复。**
5. **环境把人拖住**：Go 缓存放在慢外置盘导致编译卡死；别的会话把共享测试数据库容器删掉；`encore check` 白等 8 分钟。**教训：用 §8 的固定做法，测试库用自己名字的容器。**
6. **“测过了”不可信**：跳过的测试被当成通过；单元测试被说成端到端。**教训：完成说明必须贴出两个库都运行了的证据。**

### 5.2 当前的具体问题
| # | 问题 | 影响 | 处理在 |
|---|---|---|---|
| 1 | A2 半成品落后 develop 64 个提交，还有 2 个未提交文件 | 直接合会冲突；不知道哪些做完了 | DB-1 |
| 2 | TDengine 仍要“实验”开关 | 用户不敢用 | DB-2 |
| 3 | tptodb 旧读分支和新 `HistoryBackend` 并存，且启动时无条件初始化 gRPC 客户端 | 两套机制，容易绕过新接口 | DB-2，看 §10 决定 B |
| 4 | MySQL 下租户最新遥测汇总 503、保留期 `VACUUM FULL` | MySQL 档功能缺口 | DB-3 |
| 5 | 设计文档 `RELATIONAL-ADAPTER-DESIGN.md` §4.4 的示例代码是“每个包各写一个锁”，但实际已经有 `relational.LockTx` | 照抄会写出重复代码 | **用 `relational.LockTx`**，不要照抄示例 |
| 6 | Encore 服务现在都用 Encore 的 `sqldb`（只支持 PG） | 每个服务都要换成 `relationaldb` 给的 `*gorm.DB` | B、C 系列 |
| 7 | automation 的 MySQL schema 有两个来源（平台 `sql/mysql/010,011` 与 d1 草稿） | 可能出现两个迁移执行者 | C9，看 §10 决定 D |
| 8 | d1 的已知覆盖缺口：命令“提交到运行时下发”、casbin 完整策略 | 移植过来也一样缺 | 各卡完成说明里如实写 |

---

## 6. 这次怎么解决

### 6.1 原则
- **一张卡 = 一个分支 = 一个 worktree = 一份完成说明。** 分支名按 `CLAUDE.md` 和 `docs/T1-CARDS.md` §0：`wp/t1-<卡号>-<短名>`（例如 `wp/t1-a3-storage`）；T3 用 `wp/t3-tdengine`。worktree 放 `.claude/worktrees/`，当天登记 `docs/WORKSPACES.md`。本文的 DB-1…DB-19 只是清单序号，**分支名和完成说明用原卡号**（T1-A2、T3、T1-B1 …）。
- **每张卡不超过 3 天。** 到第 3 天还没完成，停下来，把已做的提交成 `wip:`，写清楚还差什么，交给负责人拆卡。
- **先机械改写、再双实现**：每个文件先把 `$1` → `?`、`::类型` 去掉、`= ANY($1)` → `IN ?` 这类机械改写做完、编译通过，再逐条处理需要两份实现的 SQL。
- **差分测试是唯一的验收标准**：场景从 d1 搬，两个库逐步比较，结果一致才算过。

### 6.2 一张卡的标准流程（每张卡都照这个做）
1. 读本文 §7 对应的卡，再读 `docs/T1-CARDS.md` 里这张卡的原文（文件清单、验收场景）。
2. 建 worktree：`git worktree add .claude/worktrees/wp-t1-<卡号>-<短名> -b wp/t1-<卡号>-<短名> develop`，登记 `docs/WORKSPACES.md`。
3. **步骤 0**：数一下这张卡范围内的 SQL 条数和“需双实现”条数，和 `docs/RELATIONAL-INVENTORY.md` 对比，差异写进完成说明。
4. 机械改写 → 编译通过（根模块 `go build ./...`；Encore `encore test ./<包>/... -run '^$'`）。
5. 逐条处理双实现：写在**所属模块的 repository 里**，`switch relational.DialectOf(db)`，PG 一个类型、MySQL 一个类型（范例：`docs/RELATIONAL-ADAPTER-DESIGN.md` §4.2、§4.3；锁一律用 `relational.LockTx`）。
6. 从 d1 搬差分场景到 `reltest`，起测试库（§8），**两个库都跑**。
7. 跑这张卡范围内原有的 PG 测试，一个不能少。
8. 跑红线检查（§8 最后一段）。
9. 写完成说明 `docs/tasks/<原卡号>-DONE.md`，例如 `docs/tasks/T1-A2-DONE.md`（模板见 §9），提交，**不推送不合并**，通知负责人审查。

### 6.3 什么时候必须停下来问
- 需要改不在本卡清单里的文件；
- 某个测试失败，你看不懂原因超过半天；
- 发现 PG 和 MySQL 的行为差异，不知道哪个是“对的”；
- 需要改 PG 已执行过的迁移文件；
- 需要 §10 里还没答复的决定；
- 卡已经做了 3 天。

---

## 7. 任务卡

### 7.0 开工前（P0）
- **P0-1 读文档**：本文 → `CLAUDE.md` → `docs/T1-CARDS.md` §0、§1 → `docs/RELATIONAL-ADAPTER-DESIGN.md` §0、§4、§5、§6 → `docs/MULTI-DB-ANALYSIS-2026-11-05.md` §0、§2。
- **P0-2 环境**：按 §8 起测试库并设好全部变量，跑 `cd pkg/relational && go test -count=1 -v ./... 2>&1 | grep -E -- '--- (PASS|SKIP|FAIL)'`，确认**没有一行是 SKIP**。
- **P0-3 决定**：把 §10 发给负责人，拿到答复后写进完成说明。

### 7.1 DB-1：T1-A2 收尾（平台 DAL / repo / service）
- **背景**：A2 已做一半（§2.1）。不要从头再写。
- **步骤**：
  1. 进入现有 worktree `.claude/worktrees/wp-t1-a2-platform-dal-service`，**先把 2 个未提交文件提交成一个 `wip:` 提交**（`internal/repo/a2_relational_test.go`、`internal/repo/market_install_authorization_repo.go`）。
  2. 对照 `docs/T1-CARDS.md` 的 T1-A2 一节，列一张表：清单里每个文件/每条双实现，已完成 / 没完成。
  3. 把分支 `rebase` 到最新 develop（落后 64 个提交），解决冲突。**冲突解决不了就停下问。**
  4. 补完剩下的部分。重点：`notification_source_outbox.go` 的队列领取（设计 §4.2 范例）、`native_automation_command.go` 的 `GREATEST` upsert、`InsertIfAbsent` 迁入 `internal/dal`（WIP 里已建 `internal/dal/insert_if_absent.go`，`pkg/dbdialect` 两个文件已删）、`board.go` 的数组/时间。
  5. 验收用的 6 个平台 MySQL 测试（2026-11-09 已核对位置）：
     - **develop 已有 4 个**，直接用 GORM 的 MySQL 驱动、不依赖翻译器：`internal/dal/device_identity_mysql_test.go`、`service_access_operation_mysql_test.go`、`ui_menu_mysql_test.go`、`board_trend_mysql_test.go`。要做的是确认它们在你的改动后两库都真的跑、都通过。
     - **要从 d1 搬 2 个**：`internal/dal/notification_source_outbox_mysql_test.go`（含两个 worker 互斥）、`internal/dal/do_nothing_rows_mysql_test.go`。搬的时候只要场景和期望值，句柄换成 `*gorm.DB`。
- **完成标准**：
  - `grep -rn 'pkg/dbdialect' --include='*.go' internal/ pkg/` 为空；
  - `go test -count=1 -v ./internal/dal/... ./internal/repo/... ./internal/service/... ./internal/automationguard/... 2>&1 | grep -E -- '--- (SKIP|FAIL)'` 的输出里，**本卡的 MySQL/差分测试一个都不能是 SKIP 或 FAIL**（其他与本卡无关的 SKIP 在完成说明里列出原因）；
  - 完成说明里有“清单对照表”和两库都跑了的测试输出摘要。
- **不许做**：动 `internal/dal/telemetry_*.go`（那是 DB-3 的）、动 OTA 相关文件。

### 7.2 DB-2：T3，TDengine 转正
- **前置**：DB-1 已合入（两张卡都动 `internal/dal`，不能并行）。
- **步骤**（`docs/PROJECT-AUDIT-2026-11-04.md` T3 一节为准）：
  1. 列出所有读写遥测历史的入口（历史查询、导出、统计、聚合、清理、看板），确认都走 `HistoryBackend`，没有绕过它直接查 PG。绕过的改成走接口。
  2. 在完整的 PG+TDengine 栈（安装器的 tdengine profile）上，用前端走一遍设备历史页、导出、统计。
  3. 语义差分：毫秒精度、UTC、同 key 同时间、空桶、diff 定义、区间端点——六项在 TimescaleDB 和 TDengine 上逐项对照，写成表。
  4. 去掉 `storage.tdengine.experimental` 开关（`internal/app/history_backend.go:43` 的检查、`storage.go:113` 的读取、对应测试），在 `docs/DATABASE-SWITCH.md` 写时序库一节。
  5. tptodb：按 §10 决定 B 处理。默认做法：只在 `grpc.tptodb_type` 不是 `NONE` 时才初始化 gRPC 客户端（`internal/app/grpc_service.go`），并给 `internal/dal` 里的 tptodb 分支加测试。
- **完成标准**：不设 `experimental` 也能用 `history_backend=tdengine` 启动；六项语义表写完；TDengine 测试在设置了 `TP_TDENGINE_TEST_URL` 时全绿（**没设置时会全部跳过，必须设置**）。

### 7.3 DB-3：T1-A3，平台存储与遥测
- **前置**：DB-2 已合入。
- **范围**：`internal/storage`、`internal/uplink`、`internal/tdenginehistory`、`cmd/*`、`internal/dal/telemetry_*.go`、`internal/dal/device_latest_telemetry.go`（约 45 条语句，约 5 条双实现）。**`internal/storage/telemetry_broker.go` 不动**（broker 范围），但要证明 MySQL 下它不会被启动。
- **必须一起解决**：§5.2 第 4 条的两个 PG 残留（MySQL 下的 503、`VACUUM FULL`）。
- **完成标准**：`internal/dal/telemetry_current_mysql_test.go`、`internal/storage/history_intent_mysql_test.go`（develop 已有）两库通过；`internal/dal/telemetry_batch_conflict_mysql_test.go` 从 d1 搬过来并两库通过；`history_deletion` 的领取（`FOR UPDATE OF d SKIP LOCKED`）补一个新的差分场景；`internal/storage` 原有测试全过。

### 7.4 DB-4：T1-A4，平台验收
- `scripts/smoke_mvp1.sh` 在 PG 和 MySQL 两个库上都通过；`go test ./... -run '^$'` 全树编译；列出平台根模块里仍引用翻译器的文件（应为 0）。

### 7.5 DB-5 至 DB-10：T1-B1 … B6（devicemetadata）
- **一个分支** `wp/t1-b-devicemetadata`（`docs/T1-CARDS.md` §3 指定的名字），每张卡是这个分支上的一组连续提交，每组结束时包必须能编译；每张卡仍各写一份完成说明 `docs/tasks/T1-B<n>-DONE.md`。
- **第一步（B1）最关键**：在 `devicemetadata/db.go` 用 `encore-api/pkg/relationaldb.Open("device_metadata", encoreDB)` 取 `*gorm.DB`，替换 `dbx` 句柄；把 `mysqlschema/` 改成 `migrations-mysql/` 并按设计 §6 的约定登记；咨询锁用 `relational.LockTx`。
- 每张卡的文件清单和验收场景（存储 19 步、分组 35 步、设备 23 步、模板 18、凭据 17、导出 22、模拟器 19、回执 15、命令 19、跨租户 52、网关守卫 20、模板选择 15、校验函数 74 个取值）：见 `docs/T1-CARDS.md` §3。
- **完成标准（B6 结束时）**：`grep -rln 'pkg/dbx\|pkg/sqldialect' encore-api/devicemetadata` 为空；`encore test ./devicemetadata/...` 全绿；上述全部场景两库通过。

### 7.6 DB-11 至 DB-16：其余 Encore 模块
- 每个模块一张卡，文件清单与验收场景见 `docs/T1-CARDS.md` §4（**以卡片原文为准**）。
- **怎么拿 `*gorm.DB`，每个模块不一样**（2026-11-09 核对）：

| 模块 | 现在的写法 | 改成 |
|---|---|---|
| notification、fileassets、northbound、connectorcatalog、automation、marketinstall | `sqldb.NewDatabase("<资源名>", …)` | `relationaldb.Open("<资源名>", <原 sqldb 变量>)`；PG 迁移目录 `migrations/` 不动 |
| thingmodel | `sqldb.Named("device_metadata")`（`thingmodel/db.go:7`），和 devicemetadata 共用一个库 | `relationaldb.Get("device_metadata")`，**必须等 B1 先登记这个句柄** |
| platformconfig、authcore（`encore-api/internal/authcore`） | 没有自己的 `sqldb` 变量，读平台库 | 用 `relationaldb` 的 `platform` 资源；platformconfig 在 MySQL 下只做只读核对（见卡 C2 原文） |

- **MySQL schema 从哪来**：d1 上**只有 6 个服务有 `mysqlschema/` 草稿**：automation、devicemetadata、fileassets、connectorcatalog、notification、northbound。人工审查后放进 `<服务>/migrations-mysql/`，写 `PG-MAP.txt`。marketinstall 用的是自己的 `store/mysql_migrations`（已有手写 MySQL store，放后面做、改动最小）；thingmodel 用 devicemetadata 的库，没有单独 schema；platformconfig 用平台的 `sql/mysql/009_platformconfig_dictionary.sql`（账本模块名不得改）。
- **差分场景从哪来**：
  - 要**从 d1 搬**的：物模型 31、北向 24、文件 48、通知 31、connectorcatalog 15。
  - **develop 上已经有、要原地改写**的：automation 的 `store/mysql_rules_contract_test.go`、`mysql_runs_contract_test.go`、`mysql_owners_contract_test.go`、`mysql_schema_test.go`，platformconfig 的 `store/mysql_contract_test.go`。其中有的还在引用翻译器（如 automation 的 rules 契约测试、platformconfig 的契约测试、`translatability_report_test.go`），要改成走 GORM；`translatability` 这类“测翻译器”的测试直接删。
- automation 先等 §10 决定 D。

### 7.7 DB-17 至 DB-19：收尾
- **D1**：删除 `pkg/sqldialect/`、`encore-api/pkg/sqldialect/`、`encore-api/pkg/dbx/`、`pkg/dbdialect/`、`encore-api/pkg/ownermigration/`（锁已在 `pkg/relational`）、各服务旧 `mysqlschema/`；`go build ./...`、`encore test ./...` 全绿。
- **D2**：加检查（测试或脚本 + CI），禁止导入已删除的包、禁止 SQL 字符串替换后执行、禁止业务包读 `relational.*` 配置；**检查本身要有“故意违规→必须报错”的反例测试**。
- **D3 + T2**：从 d1 取两个冒烟脚本，在 PG、MySQL、PG+TDengine、MySQL+TDengine 四种组合上各跑一次；写 `docs/DATABASE-SWITCH.md`（新装怎么选、已有 PG 数据不支持在线搬迁），并照着文档从零搭一次 MySQL 栈，用浏览器走登录、设备、模板、分组、物模型、遥测页面，记录有没有 404/500。

---

## 8. 环境与命令速查

**测试数据库**（自己用、用完就删，不要用别人的容器）：
```bash
scripts/dev-databases.sh up          # 起 4 个容器：tp-dev-pg-oracle(56001) tp-dev-mysql(56002) tp-dev-pg-plain(56003) tp-dev-mysql-auto(56004)
eval "$(scripts/dev-databases.sh env)"
scripts/dev-databases.sh down        # 用完删掉
```
**`dev-databases.sh env` 不够，还要多设一个变量**（有两个平台测试用它，脚本没导出）：
```bash
export TEST_DBDIALECT_PG_DSN="$TEST_PG_SCHEMA_DSN"   # 2026-11-09 核对：internal/dal/device_identity_mysql_test.go、telemetry_current_mysql_test.go 读它；它们自己建表。指向 56001 未实测，跑不通就告诉负责人
```
各类测试读的环境变量（**少一个就会跳过，而且跳过的提示各不相同**）：

| 测试 | 读的变量 | 跳过时打印 |
|---|---|---|
| `pkg/relational/reltest` 差分框架 | `TEST_PG_SCHEMA_DSN`、`TP_MYSQL_TEST_HOST/PORT/USER/PASSWORD/DB` | `differential test needs both engines` |
| `internal/dal/device_identity_mysql_test.go`、`telemetry_current_mysql_test.go` | `TEST_DBDIALECT_PG_DSN`、`TP_MYSQL_TEST_*` | `TEST_DBDIALECT_PG_DSN and TP_MYSQL_TEST_* required` |
| 其他 `internal/dal/*mysql*_test.go`、`internal/storage/*mysql*_test.go` | `TEST_PG_SCHEMA_DSN` 和/或 `TP_MYSQL_TEST_*` | 各自的提示 |
| automation 的 MySQL 测试 | `TP_AUTO_SCHEMA_PG_DSN`、`TP_AUTO_SCHEMA_MYSQL_DSN` | 各自的提示 |
| TDengine 测试 | `TP_TDENGINE_TEST_URL`（部分还要 `TP_TELEMETRY_BROKER_TEST_DSN`） | 各自的提示 |

**判断“真的跑了”的唯一办法**：用 `go test -v -run <测试名> ./<包>/` 跑，看每个测试是 `--- PASS` 而**不是** `--- SKIP`。不要靠找某一句提示。
> 如果别的会话也在用 `tp-dev-*` 这几个名字，会互相删容器（以前发生过）。开工前 `docker ps -a | grep tp-dev-` 看一眼；冲突就和负责人商量换名。

**编译检查**：
```bash
go build ./...                                        # 根模块
cd encore-api && encore test ./<包>/... -run '^$'      # Encore 模块，不要用 go build，也不要用 encore check
```

**Go 缓存**：只用全局的 `/Volumes/KIOXIA/.gocache`、`/Volumes/KIOXIA/.gomodcache`（`go env` 已经设好），**不要再按任务新建缓存目录**。

**红线检查**（每张卡交付前跑）：
```bash
# 1) 不许新增翻译器引用：只看你这次新增的行，输出必须为 0
git diff develop -U0 -- '*.go' | grep '^+' | grep -c -E 'pkg/(sqldialect|dbx|dbdialect)'
# 2) 只改了本卡清单里的文件
git diff develop --stat
# 3) PG 迁移一个字没动：输出必须为空（这几个写法只匹配 PG 迁移，不会误报 sql/mysql/ 下的新文件）
git diff develop -- ':(glob)sql/[0-9]*.sql' ':(glob)sql/broker/*.sql' ':(glob)sql/service_access/*.sql' ':(glob)encore-api/**/migrations/*.sql'
```

**证据**：日志、截图放 `/Volumes/KIOXIA/archive/evidence/<日期>-<原卡号>/`，仓库只写一行索引。`CLAUDE.md` 第 7 条写的是 `/Volumes/80G/…`，**那块盘已经不用了**（负责人 2026-11-04 决定改放外置盘，现为 KIOXIA），以本文为准。**不要把密码、令牌写进日志或文档。**

---

## 9. 完成说明模板（`docs/tasks/<原卡号>-DONE.md`，例如 `T1-A2-DONE.md`）

```markdown
# <原卡号> <卡名> 完成说明

分支：wp/t1-<卡号>-<短名>   基线：develop@<提交>   本卡提交：<git log --oneline develop..HEAD>
负责人决定（§10）：A=… B=… C=… D=…

## 步骤 0：SQL 条数
| 文件 | 语句数 | 需双实现 | 盘点文档的数 | 差异原因 |

## 做了什么
（逐文件一句话；每条双实现写清 PG 怎么写、MySQL 怎么写、为什么）

## 验证
- 命令：……
- PG：通过 N 个；MySQL：通过 N 个；跳过 N 个（逐个写原因）
- 差分场景：<场景名> <步数>，两库逐步一致（贴 reltest 报告摘要）
- 红线检查三条命令的输出

## 没做 / 已知缺口
（如实写；不能写“端到端通过”除非真的跑了整栈）
```

**负责人审查时看什么**：① 改动只在卡的文件清单内；② 红线检查三条为空；③ 测试输出里两个库都真的运行了；④ 完成说明的“没做”是否诚实；⑤ PG 原有测试数量没有变少。

---

## 10. 需要负责人先答复的决定

没答复就按“默认”做，并在完成说明里写“按默认”。

| 编号 | 问题 | 默认（建议） | 影响哪张卡 |
|---|---|---|---|
| **A** | MySQL 档的遥测历史存哪？现在 MySQL 档会写进 MySQL 的普通表（没有 hypertable、没有压缩） | **MySQL 档推荐配 TDengine**；“历史写 MySQL 表”只作开发/小规模用，文档写明。MySQL+TimescaleDB 这次不做 | DB-3、DB-19 |
| **B** | tptodb 旧读分支删除，还是保留？（企业版默认在用它） | **保留为可选**：只在 `grpc.tptodb_type` 非 `NONE` 时初始化，补测试；不删 | DB-2 |
| **C** | devicemetadata 的导出、命令、模拟器（B5a/B5b）在 MySQL 档能否先返回“不支持”？ | **不允许**，完整做 | DB-9 |
| **D** | automation 的 MySQL schema 由谁执行？（平台 `sql/mysql/010,011` 和服务自己各有一份） | **automation 服务是唯一执行者**；若平台不读写 automation 的表，就从平台的 MySQL 模块列表里去掉 `mysql-automation` | DB-16 |
| **E** | 这次 MySQL 档要不要随下一个版本发布？ | 不随版本发布（`CLAUDE.md`：MySQL 只是“可配置替换”选项，发行版只有 PG+TimescaleDB）；做完后作为可选配置写进文档 | 全部 |

---

## 11. 资料索引

| 文档 | 用途 | 状态 |
|---|---|---|
| `docs/T1-CARDS.md` | 26 张卡的原始清单：每张卡改哪些文件、验收用哪些 d1 场景 | **有效**（A0、A1 已完成；A2 半成品） |
| `docs/RELATIONAL-ADAPTER-DESIGN.md` | 底座设计、双实现写法、迁移约定 | 有效；§4.4 示例改用 `relational.LockTx` |
| `docs/RELATIONAL-INVENTORY.md` | 每个模块的 SQL 条数和双实现原因 | 有效（数字是下限；根模块已脱离翻译器，比它写的更新） |
| `docs/MULTI-DB-ANALYSIS-2026-11-05.md` | 新老代码对比、企业版情况、卡片调整建议 | **有效，强烈建议读 §0、§2、§4** |
| `docs/PROJECT-AUDIT-2026-11-04.md` §1.2、§2、T1/T2/T3 | 架构规则、废弃翻译器的决定、T2/T3 原始任务书 | 有效；§3 写“d1 各模块都能在 MySQL 运行”指的是**翻译器版本**，不算数 |
| `docs/tasks/T1-0-DONE.md`、`T1-A0-DONE.md`、`T1-A1-DONE.md` | 已完成部分的说明和实测结论 | 有效 |
| `docs/MYSQL-MVP-WORK-PLAN-2026-11-03.md` | 翻译器方案时期的计划 | **过期**，不要按它做 |
| d1 分支 `docs/MYSQL-PROFILE-STATE-2026-11-04.md` | d1 的缺陷清单：`SKIP LOCKED` 无索引锁全部候选、`DISTINCT ON`、`COLLATE "C"`、`before` 保留字、1062 分类等 | **经验有效**，代码不用 |
