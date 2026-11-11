# 关系库访问盘点：翻译器、dbx、翻译驱动、sqldb（T1-0 步骤 1）

> 日期：2026-11-04。范围：`develop`（`49b7f278`）和 `wp/d1-dbx-returning`（`9cd62bb9`）。只读盘点，没有改任何业务代码。
> 用法：[RELATIONAL-ADAPTER-DESIGN.md](RELATIONAL-ADAPTER-DESIGN.md) 是设计，[T1-CARDS.md](T1-CARDS.md) 是据此拆出的卡片；本文是它们的数据依据。

## 0. 怎么数的（先读这一节，否则数字会被误读）

- **“SQL 语句”** = 非测试 `.go` 文件里，以 `SELECT/INSERT/UPDATE/DELETE/WITH` 开头、含 `FROM/INTO/SET/JOIN` 的字符串字面量，用脚本扫出来（脚本不入库，规则就是这一句）。不含：用 `fmt.Sprintf`/字符串拼接分段构造的语句、GORM 链式调用里的片段（如 `Where("...")`）、迁移文件里的 DDL。所以**真实数量会略多于表中数字**；表用于判断量级和排序，不是验收口径。验收口径是 T1-D 的检查（仓库里不再引用翻译器包）。
- **脚本漏计的一类**：`SELECT pg_advisory_xact_lock(...)` 这类没有 `FROM` 的调用不被计为语句。咨询锁调用实际有约 10 处（`devicemetadata` 的 `group_members.go`、`group_update.go` 等、`notification/store_receipt_credentials.go`、`connectorcatalog`、`fileassets/receipt_store.go`、`authcore/signup.go`、`cmd/notification-migrate`），都需要双实现，各卡在步骤 0 里单独列出。
- **“可移植/GORM”** = 语句里没有出现下面的“双实现特征”，可以用 `db.Raw/Exec/Scan` 配 `?` 占位符一次写成，或改成 GORM 链式写法。这类语句仍要做机械改写：`$1` 占位符换成 `?`，`::uuid`、`::text`、`::jsonb` 类型转换去掉或改 `CAST`。这是**一次性的源码改写**，不是运行时翻译；“`::`”一列单独计数，方便估工作量。
- **“需双实现”** = 语句里含有下面任一特征（一条语句可有多个原因）：

| 特征 | 为什么不能一份语句跑两个库 |
| --- | --- |
| `returning` | MySQL 没有 `RETURNING`（8.4 也没有）。要拆成“写 + 同事务读回”，或用 `LastInsertId`；`UPDATE ... FROM ... RETURNING` 要先 `SELECT ... FOR UPDATE` 锁定候选行，再 `UPDATE ... WHERE id IN (...)`，再读回 |
| `lock_skip` | 队列领取。`FOR UPDATE SKIP LOCKED` 两库都有（MySQL 8.0.1+），但 PG 常见写法 `WITH picked AS (... SKIP LOCKED) UPDATE ... FROM picked ... RETURNING` 在 MySQL 不成立；而且 d1 实测 MySQL 在 `ORDER BY` 没有可用索引时会锁住全部候选行（见 d1 的 `docs/MYSQL-PROFILE-STATE-2026-11-04.md`），需要专门的索引，必须各写各的 |
| `json` | `jsonb` 运算符（`->>`、`@>`、`?`）、`jsonb_set`、`jsonb_array_elements` 与 MySQL 的 `JSON_EXTRACT/JSON_CONTAINS/JSON_TABLE` 语义、类型保真度都不同（例如 PG 把 `1e+20` 规范成整数，见 `encore-api/marketinstall/DATABASES.md`） |
| `time` | `now() - interval '5 min'`、`make_interval`、`date_trunc`、`extract(epoch ...)`；MySQL 要用 `DATE_SUB/TIMESTAMPDIFF`。在 Go 里算好时间再作为参数传入即可降为可移植——这是首选做法 |
| `arrays` | `= ANY($1)`、`unnest`、数组列。`IN ?` 配切片可以移植；数组列和 `unnest` 不行 |
| `ilike` | MySQL 默认排序规则不区分大小写，行为靠 collation 保证，不能直接写 `ILIKE` |
| `advisory` | `pg_advisory_xact_lock`。MySQL 只有 `GET_LOCK`（会话级，不随事务释放）；需要换成“锁行”的显式实现 |
| `upsert_cond` | 带 `WHERE` 的 `ON CONFLICT DO UPDATE`（“只在时间更新时才覆盖”）。MySQL 没有对应子句，要用 `IF(new.ts >= old.ts, new.x, old.x)` 逐列表达 |

- **不算“双实现”的**（两库同一种写法）：普通增删改查；`FOR UPDATE`（GORM 的 `clause.Locking`）；不含 `RETURNING` 的 `WITH` 和 `WITH RECURSIVE`（MySQL 8 支持）；简单的 `ON CONFLICT (...) DO UPDATE SET x = excluded.x`（GORM 的 `clause.OnConflict` 按方言渲染）。**例外**：`ON CONFLICT DO NOTHING` 在 MySQL 上渲染成 `ON DUPLICATE KEY UPDATE x=x`，在 `clientFoundRows` 连接上对已存在的行也返回 1，“是否真的插入了”的判断会错。develop 已有 `pkg/dbdialect.InsertIfAbsent`（PG 用行数，MySQL 用 1062 错误）处理；它是“repository 内双实现”的现成样板。
- 数字是**保守估计**：一条语句既有可移植又有不可移植的部分，按“需双实现”计；`lock_skip` 一律按双实现计（需要逐条验证索引）。

## 1. 按模块汇总（develop）

列：含 SQL 的源文件数 / SQL 语句数 / 可移植 / 需双实现 / 其中带 `::` 的语句数 / 双实现原因（语句数）。

| 模块 | 源文件 | 语句 | 可移植 | 双实现 | `::` | 双实现的原因 |
| --- | --- | --- | --- | --- | --- | --- |
| `encore-api/devicemetadata` | 48 | 303 | 226 | 77 | 96 | json 47, returning 24, time 13, arrays 3, ilike 3, lock_skip 1 |
| `encore-api/automation` | 16 | 172 | 145 | 27 | 14 | lock_skip 10, json 9, returning 8, time 7, arrays 1, upsert_cond 1 |
| `encore-api/l3-core/notification` | 17 | 140 | 130 | 10 | 2 | lock_skip 8, json 2 |
| `encore-api/northbound` | 9 | 59 | 42 | 17 | 44 | json 12, time 5, lock_skip 1, returning 1 |
| `encore-api/internal`（authcore 等） | 6 | 45 | 41 | 4 | 2 | json 2, time 2 |
| `encore-api/marketinstall` | 6 | 28 | 27 | 1 | 0 | time 1 |
| `encore-api/platformconfig` | 2 | 26 | 25 | 1 | 3 | arrays 1 |
| `encore-api/thingmodel` | 2 | 22 | 12 | 10 | 8 | json 10, ilike 2, time 1 |
| `encore-api/cmd`（运维命令，只支持 PG） | 2 | 19 | 19 | 0 | 0 | — |
| `encore-api/fileassets` | 4 | 13 | 9 | 4 | 13 | json 2, lock_skip 1, returning 1 |
| `encore-api/l3-core/connectorcatalog` | 1 | 4 | 3 | 1 | 2 | json 1 |
| `encore-api/pkg`（owner 迁移辅助） | 1 | 2 | 1 | 1 | 0 | advisory 1 |
| **Encore 小计** | **114** | **833** | **680** | **153** | **184** | |
| `internal/dal` | 13 | 57 | 53 | 4 | 1 | time 2, arrays 1, lock_skip 1, returning 1 |
| `internal/storage` | 6 | 18 | 15 | 3 | 3 | json 3, returning 1, upsert_cond 1 |
| `initialize` | 9 | 18 | 16 | 2 | 4 | arrays 2 |
| `internal/service` | 6 | 12 | 9 | 3 | 1 | json 2, time 1, upsert_cond 1 |
| `cmd` | 3 | 12 | 11 | 1 | 4 | time 1 |
| `internal/tdenginehistory` | 1 | 8 | 8 | 0 | 0 | — |
| `internal/repo` | 1 | 5 | 5 | 0 | 0 | — |
| `internal/automationguard` | 1 | 5 | 4 | 1 | 0 | time 1 |
| `internal/adapter` | 1 | 1 | 1 | 0 | 0 | — |
| `internal/uplink` | 1 | 1 | 0 | 1 | 1 | json 1 |
| **平台根小计（不含 broker）** | **42** | **137** | **122** | **15** | **14** | |
| `internal/brokerstate` | 4 | 23 | 16 | 7 | 6 | json 6, time 1, upsert_cond 1 |
| `internal/brokercontrol` | 5 | 18 | 9 | 9 | 9 | json 4, time 4, returning 1 |
| `internal/brokerledger` | 1 | 6 | 6 | 0 | 0 | — |
| `internal/brokermapper` | 1 | 3 | 2 | 1 | 1 | json 1 |
| `internal/brokercommands` | 2 | 2 | 2 | 0 | 0 | — |
| **broker 控制面小计（暂缓，仍只支持 PG）** | **13** | **52** | **35** | **17** | **16** | |

合计 169 个文件、1022 条语句，可移植 837，需双实现 185（其中 broker 17、`encore-api/pkg` 1 不在重做范围）。

**平台根模块（不含 broker）只有 15 条需要双实现；Encore 模块有 153 条，其中 devicemetadata 一个模块占 77 条（50%）。**

## 2. 谁依赖翻译器、dbx、翻译驱动

### 2.1 develop

翻译器本体：`pkg/sqldialect`（731 行，4 个源文件）、`encore-api/pkg/sqldialect`（**逐字节复制**，靠 `pkg/sqldialect/copy_in_sync_test.go` 保持一致）、`encore-api/pkg/dbx`（287 行，仿 `sqldb` 形状的 `DB/Tx/Rows` 接口，并映射错误码）。

| 模块 | 依赖它们的源文件 | 依赖它们的测试文件 | 说明 |
| --- | --- | --- | --- |
| `initialize` | 1 | 0 | `platform_driver.go` 用 `sqldialect.OpenMySQL` 打开翻译驱动再交给 GORM。**平台根模块对翻译器的唯一依赖点**；但因为所有 GORM 和裸 SQL 都经过它，MySQL 上平台的 PG 风格裸 SQL 全靠它翻译，所以拆除必须同时审计第 1 节平台根的 137 条语句 |
| `encore-api/devicemetadata` | 33 | 19 | 全部 store 通过 `dbx.DB` 访问；`db.go`：`var db dbx.DB = dbx.Encore(encoreDB)`，`UseDatabase` 在 MySQL 时换成翻译池 |
| `encore-api/automation`（`store`） | 1 | 2 | `mysql_support.go`（领取辅助、1062 判断） |
| `encore-api/platformconfig`（`store`） | 2 | 1 | `postgres.go`、`schema.go` |
| 合计 | 37 | 22 | |

develop 里 **marketinstall、thingmodel、northbound、fileassets、l3-core 都没有依赖翻译器和 dbx**（只有 PG 实现，或像 marketinstall 那样已手写 MySQL store）。

### 2.2 `wp/d1-dbx-returning`（29 个提交，135 个文件，+8206/−450）

在 develop 的基础上把 dbx/翻译器推广到所有 Encore 模块，并增加 `encore-api/pkg/mysqlowner`、`pkg/ownermigration` 的 MySQL 锁、`pkg/dbxtest`（347 行，差分测试框架）和各模块的 `mysqlschema/`。

| 模块 | 依赖它们的源文件 | 测试文件 |
| --- | --- | --- |
| `encore-api/devicemetadata` | 41 | 32 |
| `encore-api/fileassets` | 8 | 3 |
| `encore-api/l3-core/connectorcatalog` | 4 | 1 |
| `encore-api/northbound` | 4 | 1 |
| `encore-api/automation` | 3 | 2 |
| `encore-api/thingmodel` | 3 | 1 |
| `encore-api/pkg`（dbx、mysqlowner 等） | 3 | 0 |
| `encore-api/l3-core/notification` | 2 | 1 |
| `encore-api/platformconfig` | 2 | 1 |
| `initialize` | 1 | 0 |
| `internal/dal` | 0 | 1 |
| 合计 | 71 | 43 |

d1 的语句数与 develop 基本相同（1035 条对 1022 条）。**重做的体量以第 1 节为准**；d1 多出来的是“让这些语句在 MySQL 上跑起来”的翻译和适配代码，全部丢弃，只留测试场景和 MySQL 语义清单。

## 3. `sqldb.NewDatabase / Named`（Encore 资源声明）

| 资源名（Encore 资源身份） | 声明位置 | PG 迁移（Encore 执行） | 文件数 | 现有 MySQL 侧 |
| --- | --- | --- | --- | --- |
| `device_metadata` | `encore-api/devicemetadata/db.go` | `devicemetadata/migrations` | 33 | develop：`devicemetadata/mysqlschema`（守卫、模式测试）；d1 增加整套契约测试 |
| `device_metadata`（`Named`，共享） | `encore-api/thingmodel/db.go` | 不单独迁移 | — | 与 devicemetadata 同库 |
| `automation` | `encore-api/automation/service.go` | `automation/migrations` | 15 | develop 用平台 `sql/mysql/010,011`；d1 另有 `automation/mysqlschema`，**是 `sql/mysql/010,011` 的拷贝，需手工保持同步**——这是要消除的重复 |
| `northbound` | `encore-api/northbound/db.go` | `northbound/migrations` | 1 | d1 增加 `northbound/mysqlschema` |
| `file_storage` | `encore-api/fileassets/service.go` | `fileassets/migrations` | 2 | d1 增加 `fileassets/mysqlschema` |
| `notification` | `encore-api/l3-core/notification/db.go` | `notification/migrations` | 4 | d1 增加 `notification/mysqlschema` |
| `connector_runtime` | `encore-api/l3-core/connectorcatalog/provider_catalog_store.go` | `connectorcatalog/migrations` | 2 | d1 增加 `connectorcatalog/mysqlschema` |
| `market_install` | `encore-api/marketinstall/service.go` | `marketinstall/migrations` | 3 | develop 已有手写 MySQL store（`store/mysql*.go`）和 `store/mysql_migrations/0001–0004`，用 `ownermigration.WithMySQLSchemaLock` 保证单执行者，**最接近目标形态** |
| （平台库，非 Encore 资源） | — | 平台 `sql/1..32.sql` | 32 | 平台 `sql/mysql/001–014`（基础表、种子、自动化） |

8 个 Encore 资源声明（9 个包引用，thingmodel 共享 `device_metadata`）。`platformconfig` 不声明资源，与平台共库。

### 3.1 除 `sqldb` 之外自己开的连接池

| 位置 | 做法 | 处理 |
| --- | --- | --- |
| `encore-api/internal/authcore/runtime.go:182,195` | `sql.Open("pgx")` 和 `sql.Open("mysql")`，自带 `dialect.go`，连平台库 | 纳入 T1-C，改用 `relational.Open` 的 `platform` 资源，去掉自己的方言选择 |
| `encore-api/devicemetadata/legacydb.go:29` | `pgxpool.New` 连平台库读旧表 | 纳入 T1-B；MySQL 时用平台库句柄 |
| `encore-api/cmd/auto-upgrade`、`notification-migrate`、`notification-source-provision` | `sql.Open("pgx")` / `pgx.ConnectConfig` | 运维命令，只支持 PG；遇到 MySQL 配置必须拒绝（d1 已做）。T1-D 保留拒绝逻辑 |
| `initialize/pg_init.go`、`initialize/platform_driver.go` | 平台两套各自读 `db.psql.*` / `db.mysql.*`，驱动在 `db.driver` | T1-A 合并成按 `relational.driver` 选择的一处 |
| `encore-api/marketinstall/store/mysql.go:32` | `sql.OpenDB(connector)` | T1-C marketinstall |

## 4. 必须手写 PG/MySQL 两个实现的典型点

只列影响设计的类型。完整清单由各卡片的执行人在“步骤 0”里产出（复核本模块的数字）。

| 类别 | 出现位置 | 备注 |
| --- | --- | --- |
| 队列领取（`SKIP LOCKED`） | `internal/dal/notification_source_outbox.go`（`WITH picked ... UPDATE ... FROM ... RETURNING`）、`internal/storage/history_intent.go`、`history_deletion.go`、`automation`（10）、`l3-core/notification`（8）、`devicemetadata` 导出、`northbound` 投递、`fileassets` 恢复、`marketinstall` 领取 | 设计文档给出完整范例 |
| 条件 upsert | `internal/storage` 当前值（develop 已有 `upsertCurrentMySQL`：`IF(new.ts >= old.ts, ...)`）、`internal/service/native_automation_command.go`（`GREATEST`）、`automation` 1 条 | develop 已有可复用的 MySQL 写法 |
| 咨询锁 | `devicemetadata`（`group_members.go`、`group_update.go` 等）、`l3-core/notification/store_receipt_credentials.go`、`connectorcatalog`、`fileassets/receipt_store.go`、`authcore/signup.go`、`cmd/notification-migrate`（会话级迁移锁） | 事务级互斥，MySQL 用“锁行”实现；设计文档给出完整范例 |
| `RETURNING` | `devicemetadata`（24）、`automation`（8）、`internal/storage`、`fileassets`、`northbound`、`internal/dal` | 先写后读，同一事务 |
| JSON 运算 | `devicemetadata`（47）、`northbound`（12）、`thingmodel`（10）、`automation`（9） | 能把过滤移到 Go 里就移；不能的各写一份 |
| 触发器、`CHECK`、行级安全 | `devicemetadata` 网关树守卫、模板模型选择、CHECK 替代；跨租户保护 | MySQL 侧作为迁移文件手写（d1 有语义清单可复用）；跨租户保护在 MySQL 上不能依赖 RLS，要在 repository 层显式加 `tenant_id` 条件 |

## 5. 对拆卡的结论

1. 平台根模块的工作量**远小于**四个大 Encore 模块：需双实现的平台语句只有 15 条，且 develop 已有 `dbdialect.InsertIfAbsent`、`upsertCurrentMySQL` 两个范例。T1-A 的主体是“去掉翻译驱动后逐条审计 137 条语句 + 平台测试移植”。
2. `devicemetadata` 占 develop 翻译器依赖的绝大部分（33/37 个源文件）、303 条语句（77 条需双实现），d1 差分步骤也最多（约 300）。必须拆成 6–7 张卡。
3. `automation`（172/27）、`notification`（140/10）体量次之，各拆 2–3 张卡。
4. 其余模块各 1 张卡。
5. 每张卡以本表为基线，执行人第一步复核本模块数字并写进完成说明。
