# ThingsPanel 多数据库收尾：MVP 分阶段与并行工作包（2026-11-03）

给要参与的同事看的任务说明。总任务来源：`HANDOFF-next-owner-20261102.md`（证据目录
`/Users/junhong/Downloads/code/thingspanel-takeover-evidence-20261102/`）。
本文只讲怎么拆、谁做什么、怎么验收。**主框架和主流程由主线（Claude 会话）负责**，其余工作包彼此独立，可以并行。

规则（所有人）
- 分支从 `develop` 切，一个工作包一个分支，命名 `wp/<编号>-<短名>`；完成并通过验收后合回 `develop`，**不要推送、不要碰 `main`、不要发布生产**。
- 不改已执行的 PG 迁移（`sql/1.sql`…`sql/32.sql` 和各组件目录）。MySQL 迁移只追加新文件。
- 不要手改 `sql/mysql/012_platform_base.sql`（由 `scripts/pg2mysql_platform.py` 生成）。
- `pkg/sqldialect`（翻译器）有两份：根模块 `pkg/sqldialect` 和 `encore-api/pkg/sqldialect`，必须字节一致（有测试守着）。改一份就复制到另一份。
- 测试要诚实：不能通过的写进报告，不能拼接不同版本的结果，不能声称 J-G 通过除非满足交接说明里的全部条件。
- 不要把密码、令牌写进文件或提交。

## 一、现状（已在 `develop`，`9276d126`）

| 已完成 | 怎么证明的 |
|---|---|
| 历史存储抽象、TDengine 写读聚合删除 | 真实 TDengine 3.4 |
| MySQL 核心切片：用户、设备、鉴权、账号、会话、设备身份、服务接入、设备状态历史 | PG 对 MySQL 差分测试 |
| automation 全部存储层（claim、租约、告警、executor、迁移记录） | 156 步剧本，PG 与 MySQL 8.4 逐步一致，含并发抢占 |
| 平台根 schema：82 张基础表 + 视图 + 咨询锁表（其中 72 张由生成器产出，含兜底的 `telemetry_datas`） | `initialize/mysql_platform_base_schema_test.go` 对比列、类型、可空、主键/唯一/普通索引 |
| 全部 MySQL 表默认大小写敏感（`utf8mb4_bin`），行为对齐 PG | LIKE 测试、外键 |
| 翻译器接到平台 gorm 连接下面：`?` 占位符、`::` 转型、`ON CONFLICT DO NOTHING`、`make_interval`、`pg_advisory_xact_lock`、`FILTER`、保留字 | `pkg/sqldialect` 单测 + 全部 MySQL 套件 |

**MVP-1 的主流程已经在 MySQL 上跑通（主线完成，2026-11-03）：** `db.driver=mysql` 时平台自己应用全部 MySQL 迁移（含种子数据）并启动；
`scripts/smoke_mvp1.sh` 覆盖登录、用户、分组、设备增删改查、设备列表、看板统计，`scripts/smoke_mvp1_ingest.sh` 覆盖 MQTT 遥测上报 → 当前值 → 历史 → 删除设备。
历史遥测在没有 TDengine 时落在 MySQL 自己的 `telemetry_datas`（兜底，仅用于开发和小规模）；生产的 MySQL 档位仍应配 TDengine。
**还没验证的：** 界面（前端）、TDengine 路径在 MySQL 平台上的整链、插件安装、broker 控制面（本次 MQTT 走的是平台自己订阅 `devices/telemetry` 的旧通道，没有 broker 控制面）。

## 二、本地测试环境（每个人先做这个）

需要 Docker、Go、Python 3。

```bash
scripts/dev-databases.sh up              # 起 4 个临时库（PG+Timescale 参照库、MySQL 8.4、普通 PG、自动化用 MySQL）
eval "$(scripts/dev-databases.sh env)"   # 导出 TEST_PG_SCHEMA_DSN、TP_MYSQL_TEST_*、TP_AUTO_SCHEMA_* 
go test -p 1 ./initialize/ ./internal/dal/ ./internal/service/ ./pkg/...   # -p 1: the packages share one MySQL database
(cd encore-api && go test ./automation/... ./pkg/sqldialect/ ./platformconfig/... ./internal/authcore/...)
scripts/dev-databases.sh down            # 用完删掉
```

没有设环境变量时，双库测试会 **跳过** 而不是失败，所以看到 `ok` 之前先确认没被跳过（加 `-v` 看 SKIP）。

差分测试的写法（照抄）：`encore-api/automation/store/mysql_runs_contract_test.go`、`mysql_owners_contract_test.go`。
同一份剧本函数分别在 PG 和 MySQL 上跑，每一步记一行文字，最后逐行比较；并发的地方用多 goroutine 抢，比较成功者数量。

## 三、MVP 分阶段

| 阶段 | 目标 | 验收 |
|---|---|---|
| **MVP-0（已具备）** | PG + Timescale 默认路径可用，存储层 MySQL 可用 | 现有测试全绿 |
| **MVP-1：MySQL 平台能启动并登录（后端已达成，前端未验证）** | `db.driver=mysql` + TDengine 历史，平台起得来；登录、租户、设备增删改查、设备列表、遥测上报进 TDengine；PG 完全不启动 | 起一套 MySQL+TDengine 栈，用测试账号登录，建设备，用 MQTT 上报一条遥测，在界面看到 |
| **MVP-2：四种库组合的安装器** | 唯一 installer 能产出四种组合（PG+Timescale、PG+TDengine、MySQL+Timescale、MySQL+TDengine），新装、升级、中断恢复 | 每种组合在干净机器上装起来并通过 MVP-1 的验收 |
| **MVP-3：Encore owner 与 broker 控制面在 MySQL 下可用** | devicemetadata、thingmodel、northbound、l3-core、fileassets、broker 控制面 | 物模型、模板、北向、通知、插件安装在 MySQL 栈下通过 |
| **MVP-4：J-G0～J-G4** | 真实插件安装、设备上报、故障恢复、旧实例围栏、四种组合、installer 新装升级恢复；35 包 / 100 API 卡 / 357 路由台账；甲乙独立报告；联合验收报告；遗留清单 | 同一组固定源码和制品上跑完，失败和跳过如实记录 |

建议先把 MVP-1 做通再测试，其余并行推进。

## 四、需要产品/负责人先拍板的 3 件事（影响工作包范围）

1. **“MySQL+Timescale”的含义。** Timescale 是 PG 扩展，MySQL 里没有。我的默认建议：关系数据在 MySQL，历史遥测放在**独立的、只存历史的 PG+Timescale 实例**（`storage.history_backend` 指向它）。若不接受，这个组合只能删。
2. **broker 控制面是否要支持 MySQL。** “MySQL+TDengine 且 PG 不可用”要求它不依赖 PG。我的建议：要支持（WP-H），否则该组合不成立。
3. **用 Encore `sqldb` 的 owner（devicemetadata、thingmodel、fileassets、northbound）在 MySQL 档位怎么办。** 我的建议：数据层改成 `database/sql` + 翻译器（automation 已经证明可行），不是关闭功能。

## 五、工作包

标注：**主线** = 由 Claude 主会话做；其余可由任何人认领。每个包写明「做什么 / 在哪 / 怎么验收 / 依赖」。

### WP-A 平台启动接线 【主线，已完成：见上文 MVP-1】
- 做什么：`initialize` 里 MySQL 迁移运行器按顺序应用 001–013（每个 owner 自有账本，已有 `applyMySQLMigrations`）；放开 `db.driver=mysql` 的启动；`global.DB` 设为 MySQL 的 gorm 句柄；启动自检（保留 `RequireNoTemporaryTablePrivilege`）。
- 在哪：`initialize/platform_driver.go`、`initialize/mysql_migration.go`、`main.go`。
- 验收：`db.driver=mysql` 时进程启动成功，`/api/v1/...` 登录通过；PG 默认路径完全不变。
- 依赖：WP-B。

### WP-B 种子数据移植 【已完成：`scripts/pg2mysql_seed.py` + `014_platform_seed.sql`，71 行与 PG 逐列一致；casbin 基础策略需要一个参考安装导出，当前只有 sql/25.sql 的 3 条】
- 做什么：PG 迁移里的种子数据要在 MySQL 有等价：`sys_ui_elements`（菜单）、`sys_dict`、`sys_dict_language`、`sys_function`、`data_policy`、`logo`、`notification_services_config`、`service_plugins`、`boards`、默认管理员用户、casbin 规则（`sql/3.sql`、`sql/25.sql`、`sql/29.sql` 等里的 INSERT/UPDATE/DO 块）。casbin 表由适配器启动时自动创建，规则要在建表后补。
- 做法：在装好全部 PG 迁移的参照库里按表导出行，生成 `sql/mysql/014_platform_seed.sql`（可重复执行：`INSERT ... ON DUPLICATE KEY UPDATE` 或先检查）。写一个生成脚本放 `scripts/`，不要手抄。
- 验收：新增测试逐表比较 PG 与 MySQL 的种子行（主键 + 全部列）；`TestMySQLPlatformSchemaCovers...` 仍然通过。
- 依赖：无。

### WP-C 平台 PG 专用 SQL 清扫（非 broker）
- 做什么：翻译器覆盖了大部分，剩下需要人工处理的：
  1. 带 `Where` 条件的 `clause.OnConflict`（gorm 会生成不可用的 MySQL）。
  2. 读受影响行数的 gorm `DoNothing` 插入，改用 `pkg/dbdialect.InsertIfAbsent`（约 17 处）。
  3. `unnest(ARRAY[to_regclass(...)])` 这种表存在性检查（`internal/dal/notification_group_route_guard.go`、`notification_source_outbox.go`）→ 查 `information_schema`。
  4. `internal/dal/telemetry_datas.go` 的批量 `ON CONFLICT ... DO UPDATE`。
  5. `cmd/*` 运维工具：先列清单，明确哪些支持 MySQL、哪些声明仅 PG。
- 方法：先跑 `grep` 清单（PG 关键字：`::`、`jsonb`、`RETURNING`、`ANY(`、`unnest`、`to_regclass`、`pg_`），每处要么改成可移植写法，要么加到翻译器并补单测。**每改一个 service/dal 文件，加一个双库差分测试。**
- 验收：清单归零或有书面理由；`go test ./internal/...` 全绿（带双库环境变量）。
- 依赖：无（可按目录再拆成多人：`internal/dal`、`internal/service`、`internal/repo`、`cmd`）。

### WP-D devicemetadata MySQL 化（最大）
- 做什么：`encore-api/devicemetadata`（约 365 条 SQL、32 个迁移）。按 automation 的做法：`Store{DB, MySQL bool}`，翻译器兜底，翻译不了的写 MySQL 分支，**行级安全（RLS，PG 用 `set_config('app.current_tenant_id')`）在 MySQL 没有，必须改成每条语句显式带 `tenant_id` 条件**，并写测试证明跨租户读不到。
- 参考：`encore-api/automation/store/*`、`encore-api/pkg/sqldialect`、`scripts/pg2mysql_automation.py`（迁移转换器，遇到不认识的结构会中止）。
- 验收：`mysql_*_contract_test.go` 双库差分 + 租户隔离测试。
- 依赖：无。

### WP-E thingmodel MySQL 化 / WP-F northbound / WP-G l3-core（通知等）/ WP-G2 fileassets
- 同 WP-D 的方法，各自独立。thingmodel 13 个文件、northbound 15 个、l3-core 66 个、fileassets 另计。
- 每个包交付：MySQL 迁移文件（追加到 `sql/mysql/` 或该 owner 自己的目录并登记到 `sql/mysql_embed.go`）、`NewMySQL` 构造、双库差分测试、一页说明。

### WP-H broker 控制面 MySQL 化 【先决策 §四.2】
- 范围：`internal/brokercontrol`、`brokerstate`、`brokerledger`、`brokercommands`、`brokerroute`、`brokermapper`、`mosquittodriver`、`internal/storage/telemetry_broker.go`、`internal/uplink/reliable.go`、`internal/service/broker_automation.go`，以及 `sql/broker/001–017`（约 180 处 PG 专用 SQL，大量 `?::jsonb`、`to_jsonb(o)->'x'`、`UPDATE ... FROM`、咨询锁）。
- 难点：`to_jsonb(row)` 和 `UPDATE ... FROM` 翻译器不支持，需要写 MySQL 分支；证明“重复上报只算一次”的幂等账本在 MySQL 下语义不变。
- 验收：双库差分 + 重放/乱序/提交未知的故障测试。

### WP-I 安装器四种库组合 【MVP-2】
- 仓库：`/Users/junhong/Downloads/code/thingspanel-installer`（`develop`），签名清单已接入（见 `STATUS-takeover-session-20261102.md`）。
- 做什么：compose 模板与配置生成支持四种组合；新装、升级、中断恢复、搬迁；`config/native/update.py` 事务不变。
- 验收：每种组合在干净环境装起来；中途杀进程后能恢复；升级保留数据。
- 依赖：WP-A（MVP-1）出来后才能装 MySQL 组合；PG 组合可先做。

### WP-J Windows 包
- 状态：未构建。需要先授权一个测试用 HTTPS 分发地址；包由主线或你指定的人构建，同事只负责在 Windows 上安装验证。
- 验收：干净 Windows 上安装、启动、登录、卸载。

### WP-K 验收台账与联合报告 【MVP-4】
- 做什么：35 包 / 100 API 卡 / 357 路由台账（含 89 张未完成的卡）；J-G0～J-G4 的执行脚本和固定版本清单（六仓 HEAD、制品 SHA256）；甲乙队独立报告；联合验收报告；真实遗留清单。
- 材料：`HANDOFF-next-owner-20261102.md`、`team-a/`、`team-b-*/`、`test-specs/`。
- 约束：所有结果必须来自同一组固定源码和制品。

### WP-L 前端（乙队工作在 develop 上重做）
- 仓库：`/Users/junhong/Downloads/code/thingspanel-frontend-community`（`develop` = `1da66eca`，目前只合了类型检查清理）。乙队的功能分支和 develop 冲突 80 处，需要在 develop 上重做而不是硬合。
- 验收：`pnpm typecheck && pnpm build` 通过，页面在 MVP-1 栈上可用。

### WP-M 界面冒烟
- 前提：`/Volumes/80G` 已挂载，并有一套**新集成版本**的栈（当前 `127.0.0.1:18080` 跑的不是新版本，不能用于 J-G）。
- 做什么：用授权的本地测试账号登录，按 `test-specs/` 走设备、分组、物模型、模板、看板、个人中心；个人中心第三方账号区域现在报“操作失败，请重试”，需要定位。

## 六、依赖关系

```
WP-B ─► WP-A ─► MVP-1 ─► WP-I(MySQL 组合) ─► MVP-2
WP-C ──────────┘
WP-D/E/F/G/G2/H ───────────────► MVP-3 ─► WP-K ─► MVP-4
WP-L、WP-J、WP-M 可与上面并行
```

## 七、每个工作包交付时请附上

1. 分支名与 `git log --oneline develop..HEAD`。
2. 新增/修改的测试名，以及在哪组环境变量下跑的、结果（含被跳过的）。
3. 已知没做的部分和已知不一致的地方（例如 MySQL JSON 会重排键、没有行级安全、`INSERT ... ON DUPLICATE KEY` 的行数语义）。
4. 如果改了翻译器：两份拷贝一致的测试截图/输出。
