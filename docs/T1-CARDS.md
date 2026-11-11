# T1 拆卡清单（T1-A 平台根 / T1-B devicemetadata / T1-C 其余 Encore 模块 / T1-D 删除翻译器）

> 日期：2026-11-04。状态：**待总监审定**。依据：[RELATIONAL-ADAPTER-DESIGN.md](RELATIONAL-ADAPTER-DESIGN.md)、[RELATIONAL-INVENTORY.md](RELATIONAL-INVENTORY.md)。
> 每张卡 **≤ 3 天**。超过 3 天停下拆小（`CLAUDE.md` 限额）。共 **26 张卡，约 55.5 人天**；两条改代码的线并行，加上最后串行的 D 系列，日历时间约 **30–32 个工作日**。

## 0. 先读：所有卡片共用的规则

1. **分支与目录**：每张卡 `wp/t1-<卡号>-<短名>`，从 `develop`（或前置卡所在分支）切出，放在 `.claude/worktrees/` 下，当天登记 `docs/WORKSPACES.md`。不推送、不合并。
2. **步骤 0（每张卡都做）**：复核本卡范围内的 SQL 语句数和“需双实现”数（口径见盘点文档第 0 节），把差异写进完成说明；产出“双实现清单”（哪些语句、什么原因、用哪种写法：GORM 链式 / 带 `?` 的同一份 SQL / repository 内双实现）。
3. **禁止项**：不新增对 `pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx`、`pkg/dbdialect` 的调用；不写运行时 SQL 改写；不在 `pkg/relational` 里放业务 SQL。碰到的文件里原有的这些调用，**在该卡内改掉**（不是留给 T1-D）。
4. **并存规则（仅限同一分支内）**：devicemetadata 这类单个大包无法一次改完。卡片分支内允许旧的 `dbx.DB` 句柄和新的 `*gorm.DB` 句柄**暂时并存**，但：每张卡改完自己列出的文件后，这些文件里不能再有 `dbx` 导入；整个系列的最后一张卡必须把旧句柄清零；**系列没做完不得合入 develop**。这不是过渡层，是分支内的施工中间态。
5. **差分测试**：从 `wp/d1-dbx-returning` 取场景（只取场景和期望值，不取 `dbx`/翻译器代码），移植成 `reltest` 场景（T1-A0 交付的框架）。每个场景在 PG 和 MySQL 上都要通过。没有真实库的环境必须 `t.Skip` 并写明原因。
6. **MySQL 迁移文件**：放 `<service>/migrations-mysql/`，手写手审，文件头注明起草来源；同步维护 `PG-MAP.txt`（设计文档 6.2）。可以参考 d1 的 `mysqlschema/*.sql`，但不能带过来 `mysqlowner`、`dbx`。
7. **测试数据库**：`scripts/dev-databases.sh up`，`eval "$(scripts/dev-databases.sh env)"`，结束 `down`。
8. **完成说明**：`docs/tasks/<卡号>-DONE.md`，写分支名、`git log --oneline`、测试命令与结果（跳过的写原因）、没做完的。不把单元测试通过说成端到端通过。

### 0.1 d1 参考材料的取法

d1 分支在 `/Users/junhong/Downloads/code/thingspanel-backend-community/.claude/worktrees/wp-db-compat-f6f630`（`wp/d1-dbx-returning`，`9cd62bb9`）。用 `git show wp/d1-dbx-returning:<路径>` 或直接读该目录；**只读**，不合并、不 cherry-pick 提交（提交里混着翻译器代码）。可整文件复制的只有：测试场景、`mysqlschema/*.sql` 起草稿、`scripts/smoke_channels.sh`、`scripts/smoke_history_chain.sh`。

## 1. 依赖与并行总图

```
T1-A0 公共接线 ──┬── T1-A1 ── A2 ── A3 ── A4                       （线 1：平台根，串行）
(2d, 必须最先)   │      └────────────── C2 platformconfig、C11 authcore（需要 A1）
                 └── T1-B1 ── B2 ── B3 ── B4 ── B5a ── B5b ── B6   （线 2：devicemetadata，同一包，串行）
                        └── C1 thingmodel（需要 B1）
 C3 northbound、C4 fileassets、C5/C6 notification、C7 connectorcatalog、C8 marketinstall、C9/C10 automation
   ：只需要 A0，彼此文件不相交；线 1 或线 2 空出来后接着做（任一时刻最多 2 条改代码的线）
 T1-D1 ── D2 ── D3：A、B、C 全部完成后
```

**同时最多 2 条改代码的线**（`CLAUDE.md`）。建议排期：

| 阶段 | 线 1 | 线 2 | 备注 |
| --- | --- | --- | --- |
| 1（2 天） | A0 | — | A0 改两个 `go.mod`，必须独占 |
| 2 | A1 → A2 → A3 → A4（9 天） | B1 → B2 → B3 → B4 → B5a → B5b → B6（18 天） | 审计文档第 2 波；**T4 必须已交付**，否则 A1/B1 只先开一个（T4 动 `internal/api`、`internal/middleware`、`router`、`sql/`，A1 可能动 `sql/mysql/`） |
| 3 | 线 1 空出后：C2、C11、C3、C4、C7、C8 | 线 2 完成 B1 之后随时可穿插 C1（每次只占一条线，不并发） | 审计文档第 3 波；C 系列不依赖 B2–B6 |
| 4 | C5、C6、C9、C10 | | |
| 5 | D1 → D2 → D3（4 天） | | 审计文档第 4 波 |

**与其他任务的冲突**：A2/A3 动 `internal/dal`、`internal/storage`，与 **T3（TDengine）不能并行**（审计文档 §4 已指出）；A1 动 `sql/mysql/`，T4 动 `sql/` 下 PG 文件，目录不同但同一父目录，合并前确认。T1b（OTA）动 `internal/dal/ota_upgrade_tasks.go`、`internal/service/ota.go`，与 A2 有文件冲突，**T1b 必须先合入，或排在 A2 之后**。

---

## 2. T1-A 平台根模块（线 1）

### T1-A0 公共接线（2 天，必须最先，独占 `go.mod`）

- **目标**：让两个 Go 模块都能用 `pkg/relational`；补上迁移执行和差分测试框架；验证 Encore 构建。
- **改哪些文件**：
  - `go.mod`、`go.sum`、`encore-api/go.mod`、`encore-api/go.sum`（各加一行 `require` + 一行 `replace`）
  - 新增 `pkg/relational/migrate.go`（读 `embed.FS` 目录，库级互斥锁，`tp_schema_migrations` 账本，SHA-256 校验，只读核对 `Verify`；锁合同沿用 `encore-api/pkg/ownermigration/mysql_lock.go`，**把它移进来，不复制**；旧文件留到 D1 删除）
  - 新增 `pkg/relational/reltest/`（差分测试框架：同一场景在 PG 和 MySQL 各跑一遍，逐步比较，输出报告；取代 d1 的 `pkg/dbxtest`）
  - 新增 `encore-api/pkg/relationaldb/`（Encore 粘合：包级 `sync.Once` 读 `relational.driver`，`Open(resource, *sqldb.Database)`、`Get`、`Close`）
  - 若 O3 通过：`pkg/relational/lock.go`（`LockTx`，设计文档 4.4）
- **验收**（不用 d1 场景，这张卡建框架）：
  - `pkg/relational` 单元测试 + 真实库测试通过；`go test ./...` 在根模块和 `encore-api` 模块都不因依赖解析失败；
  - `encore test ./pkg/relationaldb/...` 通过；`encore build docker` 的结果（成功，或失败原因）写进完成说明；若 Encore 拒绝 `../` 的 `replace`，按设计文档 O2 的后备方案处理；
  - **复验设计文档 3.3 的假设**：`relational.driver=mysql` 且 PG 不可达时，一个声明了 `sqldb` 的服务能启动并通过 MySQL 完成一次读写；
  - `migrate` 的测试：两个并发执行者只有一个执行、另一个得到 `ErrBusy`；已登记文件内容被改动时拒绝启动；`Verify` 在版本落后时失败。
- **冲突**：全部后续卡都依赖它。与线上其他任务无文件冲突（只动 `go.mod`/`go.sum` 与新增文件）。
- **并行**：**不能**。必须单独先做完并合入 B/A 系列使用的分支基线。

### T1-A1 平台启动与连接（2 天）

- **目标**：平台进程只通过 `relational.driver` 选库；`global.DB` 由 `relational.Open` 产生；去掉翻译驱动。
- **改哪些文件**：`initialize/platform_driver.go`（删除翻译驱动、`LoadMysqlConfig`、`MysqlConnect`，改为 `RelationalInit`）、`initialize/pg_init.go`（`db.psql.*` 并入 `relational.postgres.*`）、`initialize/mysql_platform_schema.go`、`initialize/mysql_*.go`（迁移部分接入 `relational.Migrate`，**保留现有账本模块名**，设计文档 O4）、`internal/app`（关停时 `Registry.Close`）、`configs/conf*.yml`（键名）、`main.go`（若需要）。
- **不做**：不改 `internal/dal`、`internal/storage` 里的 SQL（A2/A3）。`internal/broker*` 在 MySQL 配置下保持“拒绝启动”的现有守卫，并加一个测试证明。
- **验收**：
  - 平台现有 PG 测试一个不少地通过（`go test ./initialize/... ./internal/app/...`）；
  - d1 的平台启动相关测试移植通过：`initialize` 下 `mysql_platform_schema` 的幂等启动两次、`RequireNoTemporaryTablePrivilege`、`log_bin_trust_function_creators` 报错提示（d1 `platform_driver.go` 同名逻辑，保留语义，不保留代码）；
  - **检查 `sql/mysql/010,011`（automation）与 Encore `automation` 的 MySQL 库是否同一 schema**（设计文档 O6，见第 7 节），在完成说明里给出结论和处理。
- **冲突**：`sql/mysql/`（T4 相邻）、`configs/`、`initialize/`。与 B/C 系列无冲突。T1b 无冲突。
- **并行**：必须在 A0 之后；与线 2 的 B1 可并行（目录不相交）。

### T1-A2 平台 DAL / repo / service（3 天）

- **目标**：`internal/dal`（除遥测）、`internal/repo`、`internal/service`、`internal/automationguard`、`internal/adapter` 里的裸 SQL 在 PG/MySQL 都只通过 GORM；`pkg/dbdialect.InsertIfAbsent` 迁入 `internal/dal`（repository 内双实现），错误判断改用 `relational.Classify`。
- **范围**：约 74 条语句（`internal/dal` 除遥测 51、`repo` 5、`service` 12、`automationguard` 5、`adapter` 1），其中需双实现约 8 条：`notification_source_outbox.go` 队列领取（设计文档 4.2 的范例，**本卡落地它**）、`internal/service/native_automation_command.go` 的 `GREATEST` upsert、`device_identity`/`service_access_operation` 的 `InsertIfAbsent`、`board.go` 的数组/时间。
- **改哪些文件**：`internal/dal/*.go`（非 `telemetry_*`）、`internal/repo/*.go`、`internal/service/*.go`（仅含 SQL 的 6 个）、`internal/automationguard/*.go`、`internal/adapter/*.go`、`pkg/dbdialect/`（迁走后留空，由 D1 删除）。
- **验收**（d1 平台差分，移植到 `reltest`）：`internal/dal/notification_source_outbox_mysql_test.go`（源出站队列领取，含两 worker 互斥）、`do_nothing_rows_mysql_test.go`（DO NOTHING 行数）、`device_identity_mysql_test.go`、`service_access_operation_mysql_test.go`、`ui_menu_mysql_test.go`（菜单 JSON 数组匹配）、`board_trend_mysql_test.go`；develop 现有 `internal/dal`、`internal/service`、`internal/repo` 测试全部通过。
- **冲突**：与 **T1b（OTA）**：`internal/dal/ota_upgrade_tasks.go`、`internal/service/ota.go`；与 **T3**：`internal/dal`。
- **并行**：A1 之后串行；不与 A3 并行（同一目录 `internal/dal`）。

### T1-A3 平台存储与遥测（3 天）

- **目标**：`internal/storage`、`internal/uplink`、`internal/tdenginehistory`、`cmd/*`、`internal/dal/telemetry_*.go`、`device_latest_telemetry.go` 只通过 GORM。
- **范围**：约 45 条语句（`storage` 18、`cmd` 12、`tdenginehistory` 8、`dal` 遥测 6、`uplink` 1），需双实现约 5 条：当前值条件 upsert（设计文档 4.3 的范例，**本卡落地它**，直接承接 develop 的 `upsertCurrentMySQL`）、批量遥测冲突、`history_intent` 的 `RETURNING`、`uplink` 的 JSON、`cmd` 的时间运算。
- **改哪些文件**：`internal/storage/*.go`（`telemetry_broker.go` 属 broker 暂缓范围，**保持原样**并确认 MySQL 下不被启动）、`internal/uplink/*.go`、`internal/tdenginehistory/*.go`、`cmd/*.go`、`internal/dal/telemetry_*.go`、`internal/dal/device_latest_telemetry.go`。
- **验收**：`internal/dal/telemetry_batch_conflict_mysql_test.go`、`telemetry_current_mysql_test.go`、`internal/storage/history_intent_mysql_test.go`（2 个场景）；`history_deletion` 的领取（`FOR UPDATE OF d SKIP LOCKED`）用新增差分场景覆盖；develop 现有 `internal/storage` 测试全部通过。
- **冲突**：**T3**（`internal/storage`，不可并行）；`internal/dal` 内文件与 A2 不相交但同目录，A2 合入后再开。
- **并行**：A2 之后串行。

### T1-A4 平台验收（1 天）

- **目标**：把 A1–A3 在整体上跑一遍，不引入新功能。
- **改哪些文件**：基本只有 `docs/tasks/T1-A-DONE.md`；`scripts/smoke_mvp1.sh` 如需参数化库类型则小改。
- **验收**：`scripts/smoke_mvp1.sh` 在 PG 和 MySQL 两个库上都通过；`go test ./... -run '^$'` 全树编译；`go test ./internal/service ./internal/api ./internal/repo`（现有候选 CI 包）通过；列出仍引用翻译器包的平台文件（应为 0）。
- **冲突**：无。**并行**：A3 之后。

---

## 3. T1-B devicemetadata（线 2，同一包，串行；分支 `wp/t1-b-devicemetadata`，B 卡为该分支上的连续提交组）

devicemetadata 在 develop 有 48 个含 SQL 的源文件、303 条语句、77 条需双实现；`store.go` 一个文件就占 78 条。包是单个 Go 包，句柄是包级 `db`，**无法拆成互相独立的并行卡**，所以按文件分组串行。每张卡结束时包必须能编译、已改的文件无 `dbx` 导入（并存规则）。

### T1-B1 基础与 store（3 天）

- **改哪些文件**：`db.go`、`service.go`（`initService`/`Shutdown`）、`store.go`（78 条）、`models.go`、`tenant_sql_tx.go`、`metadata_write_tx.go`、`legacydb.go`、`thingmodel_helpers.go`、`mysqlschema/` → `migrations-mysql/`（把 d1 的 `001_schema`、`002_guards`、`003_seed`、`004_support`、`005_validators` 当起草稿，逐个审查后提交）、`PG-MAP.txt`、咨询锁表 `tp_advisory_locks`。
- **验收**：d1 `store_mysql_contract_test.go`（**存储 19 步**）；`mysql_contract_test.go`（`TestMySQLThingModelTenantContract`）；`mysql_profile_boot_test.go`（空库启动两次）；d1 `mysqlschema` 里的 schema/守卫对比测试（`schema_test.go`、`guards_test.go`、`translatability_test.go` 中与翻译器无关的部分——schema 对比保留，translatability 删除）；`encore test ./devicemetadata/...` 中该卡范围的 PG 测试通过。
- **冲突**：与 C1（thingmodel 共用库）：C1 必须在 B1 之后。**并行**：前置 A0；之后 B2–B6 都依赖它。

### T1-B2 分组（2 天）

- **改哪些文件**：`group_delete.go`、`group_members.go`、`group_members_raw.go`、`group_move.go`、`group_read.go`、`group_read_raw.go`、`group_update.go`、`group_write.go`、`group_tenant_tx.go`（合计约 44 条语句，需双实现约 18 条：咨询锁、`FOR UPDATE`、`RETURNING`、层级树锁）。
- **验收**：d1 `group_mysql_contract_test.go`（**分组 35 步**）。**冲突**：无（文件不相交）。**并行**：B1 之后串行。

### T1-B3 设备、网关、模板绑定（3 天）

- **改哪些文件**：`native_device_*.go`（`administrative_state_write`、`delete`、`diagnostics`、`initial_groups`、`number_check`、`operation`、`read`、`renumber`、`write`、`write_raw`、`conditional_write`、`statistics`、`groups_read`）、`native_gateway_membership_read.go`、`native_gateway_membership_write.go`、`native_device_template_binding_read.go`、`native_device_template_binding_write.go`、`native_custom_fields.go`、`native_connection_info.go`、`custom_control_service.go`、`automation_directory.go`（约 61 条语句，双实现约 17 条，集中在 `native_device_write.go`）。
- **验收**：d1 `device_mysql_contract_test.go`（**设备 23 步**）、`guards_mysql_contract_test.go` 中的 `TestGatewayGuardsBehaveTheSameOnPostgresAndMySQL`（**网关守卫 20 步**，依赖 B1 的守卫迁移）。
- **冲突**：无。**并行**：B2 之后串行。

### T1-B4 模板与凭据（3 天）

- **改哪些文件**：`access_template_model.go`、`access_template_store.go`、`native_template_*.go`（`copy`、`delete`、`list`、`record`、`references`、`write`、`write_raw`、`read`、`validation`、`input`）、`native_credential_*.go`（`lifecycle`、`read`、`secret_write`、`write`、`secret`）、`native_runtime_credential.go`、`native_processing_script.go`（若含 SQL）（约 34 条语句，双实现约 6 条）。
- **验收**：d1 `template_credential_mysql_contract_test.go`（**模板 18 步 + 凭据 17 步**）、`guards_mysql_contract_test.go` 中的 `TestTemplateModelSelectionBehavesTheSameOnPostgresAndMySQL`（**模板模型选择 15 步**）。
- **冲突**：无。**并行**：B3 之后串行。

### T1-B5a 读数导出（2 天）

- **改哪些文件**：`native_reading_export.go`、`native_reading_export_async.go`（14 条，6 条双实现：异步导出的 `SKIP LOCKED` 领取，d1 实测需要专门索引；本卡合计约 19 条）、`native_reading_aggregate.go`、`native_status_history.go`。
- **验收**：d1 `export_mysql_contract_test.go`（**导出 22 步，含并发领取**）；领取语句 `EXPLAIN` 断言无 filesort。
- **冲突**：无。**并行**：B4 之后串行。

### T1-B5b 命令、回执、模拟器、调试会话（3 天）

- **改哪些文件**：`native_command_cancellation.go`、`native_command_execution.go`、`native_runtime_dispatch.go`、`native_runtime_receipt.go`、`native_runtime_read.go`、`native_device_simulator.go`（11 条，4 条双实现）、`native_debug_session.go`、`mqtt.go`（若含 SQL）（约 32 条语句，双实现约 7 条）。
- **验收**：d1 `command_mysql_contract_test.go`（**命令 19 步**，d1 只覆盖插入/列表/详情一侧，“提交到运行时下发”需要运行时后端，**仍不覆盖，写明**）、`receipt_mysql_contract_test.go`（**回执 15 步**）、`simulator_mysql_contract_test.go`（**模拟器 19 步**）。
- **冲突**：无。**并行**：B5a 之后串行。

### T1-B6 收尾（2 天）

- **改哪些文件**：`migration_service.go`（21 条，旧库数据搬迁；MySQL 下是否支持由本卡决定，**不支持就明确拒绝并测试**）、剩余文件、删除包内全部 `dbx`/`sqldialect` 导入与 `UseDatabase`、`mysqlschema/` 残余、`README-mysql.md`、`MYSQL-REFUSED-STATEMENTS.txt` 的处理（从文件名看是翻译器的拒绝语句清单；执行人确认后随翻译器一起**删除**）。
- **验收**：d1 `cross_tenant_mysql_contract_test.go`（**跨租户 52 步**）、`validators_mysql_contract_test.go`（**校验函数 74 个取值**，即 CHECK 替代）；`encore test ./devicemetadata/...` **全绿**（PG）；MySQL 下全部 B 系列差分场景合并跑一遍；`grep -rn 'pkg/dbx\|pkg/sqldialect' encore-api/devicemetadata` 为空。
- **冲突**：无。**并行**：B5b 之后。

---

## 4. T1-C 其余 Encore 模块

每张卡只依赖 A0（除标注）。彼此文件不相交，**可并行，但受“最多 2 条改代码的线”限制**。

### T1-C1 thingmodel（1.5 天，需要 B1）

- **改哪些文件**：`encore-api/thingmodel/{db.go,postgres.go,receipt_postgres.go,service.go,install_provider.go,transport.go}`（22 条，10 条双实现：JSON 10、`ILIKE` 2）。`db.go` 改为 `relationaldb.Get("device_metadata")`。
- **验收**：d1 `thingmodel/mysql_contract_test.go`（**物模型 31 步**）；`encore test ./thingmodel/...`。
- **冲突**：与 B 系列无文件冲突，但要求 B1 已登记 `device_metadata` 句柄。**并行**：可与 A 系列并行；与 B 线二选一占线。

### T1-C2 platformconfig（1.5 天，需要 A1）

- **改哪些文件**：`encore-api/platformconfig/store/{postgres.go,schema.go}`、`service.go`（26 条，1 条双实现）。读平台库，使用 `relationaldb` 的 `platform` 资源；MySQL 时只做**只读核对**（`relational.Verify`，平台进程是执行者）。
- **验收**：d1 `platformconfig/store/mysql_contract_test.go` 的三个测试：迁移校验和（`TestMySQLMigrationChecksumMatchesTheShippedFile`）、字典 store（`TestDictionaryStoreBehavesTheSameOnMySQLAsOnPostgres`）、就绪判断（`TestMySQLReadinessFollowsTheReceiptAndTheLiveSchema`）。
- **冲突**：`sql/mysql/009_platformconfig_dictionary.sql` 的账本模块名 `platformconfig-dictionary` 不得改（A1 保留）。**并行**：A1 之后。

### T1-C3 northbound（2 天）

- **改哪些文件**：`encore-api/northbound/{db.go,config.go,connections.go,deliveries.go,inbox.go,overview.go,pipelines.go,subscriptions.go,transform.go,worker.go,secrets.go}` + `migrations-mysql/`（59 条，17 条双实现：JSON 12、时间 5、投递领取 1；44 条含 `::`，机械改写量最大的一个模块）。
- **验收**：d1 `northbound/mysql_contract_test.go`（**北向 24 步**）；`encore test ./northbound/...`（develop 已有 `northbound_test.go`；d1 另补了 1 个，可参考；执行人核对 PG 侧覆盖是否足够，不足就补最小的）。
- **冲突**：无。**并行**：A0 之后。

### T1-C4 fileassets（2 天）

- **改哪些文件**：`encore-api/fileassets/{service.go,store.go,lease_store.go,objects_store.go,receipt_store.go,recovery.go,upload_intent_store.go,workflow.go}` + `migrations-mysql/`（13 条语句全部带 `::`，4 条双实现：JSON 2、恢复领取 1、RETURNING 1；`receipt_store.go` 有咨询锁）。
- **验收**：d1 `fileassets/mysql_contract_test.go`（**文件 48 步**）；`encore test ./fileassets/...`。
- **冲突**：无。**并行**：A0 之后。

### T1-C5 notification（上）：身份、分组、授权、凭据（3 天）

- **改哪些文件**：`encore-api/l3-core/notification/{db.go,store_identity.go,store_group.go,store_plugin_grants.go,store_credentials.go,store_credentials_pair.go,store_query.go,store_idempotency.go,store_validation.go}` + `migrations-mysql/`（约 75 条语句，双实现约 2 条）。
- **验收**：d1 `notification/mysql_contract_test.go` 的前半场景（**通知 31 步**中身份/分组/授权/凭据部分；执行人在步骤 0 里把 31 步按本卡/C6 切分并写明）。
- **冲突**：与 C6 同包不同文件。**并行**：与 C6 串行（同一场景函数）。

### T1-C6 notification（下）：回执、领取、重放、回收（3 天）

- **改哪些文件**：`.../notification/{store_receipt.go,store_receipt_credentials.go,store_replay.go,store_reaper.go,store_submission.go,store_worker.go,receipt_recovery.go,submission_query.go,worker_claim.go}`、`encore-api/cmd/notification-migrate`、`notification-source-provision`（只读检查，MySQL 配置下**拒绝**，沿用 d1 行为）（约 65 条语句，双实现约 8 条：`SKIP LOCKED` 领取 8；另有 `store_receipt_credentials.go` 的咨询锁调用，盘点脚本不计入语句）。
- **验收**：d1 同一测试的后半场景，**合计 31 步全部通过**；`notification/contract/contract_test.go`；`encore test ./l3-core/notification/...`。
- **冲突**：与 C5 同包。**并行**：C5 之后。

### T1-C7 connectorcatalog（1 天）

- **改哪些文件**：`encore-api/l3-core/connectorcatalog/provider_catalog_store.go` + `migrations-mysql/`（4 条，1 条双实现：JSON；一处咨询锁）。
- **验收**：d1 `connectorcatalog/mysql_contract_test.go`（**15 步**）。**冲突**：无。**并行**：A0 之后。

### T1-C8 marketinstall（2 天）

- **目标**：develop 已有手写的 MySQL store（`store/mysql*.go`，用 `database/sql`），最接近目标形态。本卡把它改成 `*gorm.DB`，合并 PG 与 MySQL 的 store 构造，目录改名 `store/mysql_migrations` → `migrations-mysql`，迁移执行改用 `relational.Migrate`（保留 `market_install_schema_migrations` 账本的既有语义与校验和，或按设计文档统一账本——**执行人先读 `DATABASES.md` 再决定，并在完成说明里写明**）。
- **改哪些文件**：`encore-api/marketinstall/{service.go}`、`store/{store.go,mysql.go,mysql_migrate.go,mysql_schema.go,dialect.go,plan.go,progress.go}`、`store/mysql_migrations/`（28 条，1 条双实现，但已经有 7 个 MySQL 专用测试）。
- **验收**：develop 现有 `store/mysql_*_test.go` 全部通过（`mysql_claim_test.go` 双 worker 领取、`mysql_fault_test.go`、`mysql_silent_write_test.go`、`mysql_commit_reply_test.go`、`mysql_migration_test.go`、`mysql_migration_intent_test.go`、`mysql_postgres_contract_test.go`），d1 `marketinstall/mysql_boot_test.go`（空库启动两次）。
- **冲突**：`encore-api/pkg/ownermigration` 的使用方（A0 已把锁合同移入 `pkg/relational`）。**并行**：A0 之后。

### T1-C9 automation（上）：规则、告警、绑定、蓝图、包、引用（3 天）

- **改哪些文件**：`encore-api/automation/store/{store.go,alarm_intake.go,alarm_store.go,binding_store.go,blueprint_store.go,bundle_store.go,reference_store.go,request_store.go,query_extra.go,helpers.go,tx.go}`、`service.go`、`migrations-mysql/`（约 65 条）。**先解决设计文档 O6**：automation 的 MySQL schema 只能有一个执行者；d1 的 `automation/mysqlschema` 是 `sql/mysql/010,011` 的**拷贝**，必须消除重复。
- **验收**：d1 `automation/store/mysql_rules_contract_test.go`（审计文档称 automation 已有“76 步和 80 步”两个场景，本卡场景为规则部分，执行人核对对应关系并写明）、`mysql_owners_contract_test.go`（**告警、执行器、迁移场景**）、`mysql_schema_test.go`（schema 与 PG 一致、部分唯一索引不变量）、`automation/mysql_boot_test.go`；develop 现有 automation 测试（`a02`…`a31`）全部通过。
- **冲突**：与 A1 对 `sql/mysql/010,011` 的处理（O6）。**并行**：A0 之后；与 C10 同包，**串行**。

### T1-C10 automation（下）：运行时、租约、定时、调度、迁移（3 天）

- **改哪些文件**：`.../store/{runtime_store.go（38 条）,run_extra.go,run_lease.go,run_timers.go,schedule_store.go（19 条）,executor_store.go,migration_store.go}`、`owner_tick.go`、`owner_uplink.go`（约 107 条语句，双实现约 20 条：领取 10、`RETURNING` 8、时间 7，有重叠）。
- **验收**：d1 `automation/store/mysql_runs_contract_test.go`（运行时部分，对应上面的 76/80 步之一）、`automation/executor/contract_test.go`；develop 现有 `owner_tick_test.go`、`owner_uplink_test.go`、`a28_executor_test.go` 等通过。
- **冲突**：与 C9 同包。**并行**：C9 之后。

### T1-C11 authcore 与 encore-api/internal（1.5 天，需要 A1）

- **改哪些文件**：`encore-api/internal/authcore/{runtime.go,dialect.go,accounts.go,compatibility.go,provider_settings.go,signup.go}`（45 条，4 条双实现；`signup.go` 有咨询锁），`encore-api/l1-access/authn`（若含库访问）。把 `sql.Open("pgx")`/`sql.Open("mysql")` 换成 `relationaldb` 的 `platform` 资源，删除自带的 `dialect.go` 分支。
- **验收**：develop 现有 `accounts_dialect_test.go`、`provider_settings_dialect_test.go`、`accounts_test.go`、`core_test.go` 在两个库上通过；`encore test ./internal/authcore/...`。
- **冲突**：`encore-api/internal/communityclient`（A5 API 桥，**不在本任务范围**，不碰）。**并行**：A1 之后。

---

## 5. T1-D 删除翻译器 + 防回归（线 1，A、B、C 全部完成后）

### T1-D1 删除（1 天）

- **改哪些文件**：删除 `pkg/sqldialect/`（含 `copy_in_sync_test.go`）、`encore-api/pkg/sqldialect/`、`encore-api/pkg/dbx/`、`encore-api/pkg/dbxtest/`（若已被 `reltest` 取代）、`encore-api/pkg/mysqlowner/`、`encore-api/pkg/ownermigration/`（已移入 `pkg/relational`）、`pkg/dbdialect/`、`encore-api/*/mysqlschema/`、`devicemetadata/MYSQL-REFUSED-STATEMENTS.txt`；清理 `go.mod`/`go.sum`。
- **验收**：`go build ./...`（根模块）、`encore test ./...`（`encore-api`）全绿；`grep -rn 'pkg/sqldialect\|pkg/dbx\|pkg/dbdialect' --include='*.go'` 为空。
- **冲突**：无（前置卡都已完成）。**并行**：不能。

### T1-D2 防回归检查（1 天）

- **改哪些文件**：新增 `pkg/relational/guard/guard_test.go`（或 `scripts/check_no_sql_rewrite.sh` + 一个 Go 测试）与 CI 接线 `.github/workflows/`：禁止导入已删除的包；禁止 `sql.Register` 包装驱动；禁止对 SQL 字符串做 `strings.Replace/ReplaceAll/Regexp.Replace*` 后传给 `Exec/Query/Raw`；禁止业务包读 `relational.*` 配置键或 `TP_RELATIONAL_*`；`PG-MAP.txt` 完整性（每个 PG 迁移文件都有对应行）；`migrations-mysql` 已登记文件内容不得改动（对照账本校验和的测试）。
- **验收**：检查本身有“应当失败”的正反例测试（往临时目录写一个违规文件，检查必须报错）；在当前树上通过。
- **冲突**：`.github/workflows/`。**并行**：D1 之后。

### T1-D3 四组合冒烟（2 天）

- **目标**：在 PG、MySQL、PG+TDengine、MySQL+TDengine（broker 仍用 PG）四种组合上跑 `smoke_channels.sh` 和 `smoke_history_chain.sh`。
- **改哪些文件**：从 d1 取 `scripts/smoke_channels.sh`、`scripts/smoke_history_chain.sh`（整文件复制，按 `relational.*` 新键名改环境变量）；`docs/tasks/T1-D-DONE.md`。
- **验收**：四组合 × 两个脚本全部通过（8 次运行记录写进完成说明；证据日志放 `/Volumes/80G/archive/evidence/<日期>-T1-D/`，仓库只写一行索引）。TDengine 档位仍标 `experimental=true`，结果如实写明。
- **完成后**：`wp/d1-dbx-returning` 打 `archive/` 标签后删除（先列清单给用户确认，`CLAUDE.md` 第 12 条）。
- **冲突**：与 T3（TDengine 转正）的文档可能重叠。**并行**：D2 之后。

---

## 6. 汇总表

| 卡 | 范围 | 天 | 前置 | 同目录冲突 | 能否并行 |
| --- | --- | --- | --- | --- | --- |
| A0 | go.mod ×2、`pkg/relational` 扩展、`encore-api/pkg/relationaldb` | 2 | — | 全部 | 否，最先 |
| A1 | `initialize`、`configs`、`internal/app` | 2 | A0 | `sql/mysql`（T4 邻近） | 与 B1 可并行 |
| A2 | `internal/dal`（非遥测）、`repo`、`service`、`automationguard`、`adapter` | 3 | A1 | T1b、T3 | 否（A3 同目录） |
| A3 | `internal/storage`、`uplink`、`tdenginehistory`、`cmd`、`dal/telemetry_*` | 3 | A2 | T3 | 否 |
| A4 | 验收 | 1 | A3 | — | 否 |
| B1 | devicemetadata 基础、store、迁移基线 | 3 | A0 | C1 | 与 A 系列并行 |
| B2 | 分组 | 2 | B1 | — | 否（同包） |
| B3 | 设备、网关、模板绑定 | 3 | B2 | — | 否 |
| B4 | 模板、凭据 | 3 | B3 | — | 否 |
| B5a | 导出 | 2 | B4 | — | 否 |
| B5b | 命令、回执、模拟器 | 3 | B5a | — | 否 |
| B6 | 收尾、跨租户、校验函数 | 2 | B5b | — | 否 |
| C1 | thingmodel | 1.5 | B1 | — | 是 |
| C2 | platformconfig | 1.5 | A1 | — | 是 |
| C3 | northbound | 2 | A0 | — | 是 |
| C4 | fileassets | 2 | A0 | — | 是 |
| C5 | notification 上 | 3 | A0 | C6（同包） | 否（与 C6 串行） |
| C6 | notification 下 | 3 | C5 | — | 否 |
| C7 | connectorcatalog | 1 | A0 | — | 是 |
| C8 | marketinstall | 2 | A0 | — | 是 |
| C9 | automation 上 | 3 | A0 | A1（O6） | 否（C10 同包） |
| C10 | automation 下 | 3 | C9 | — | 否 |
| C11 | authcore | 1.5 | A1 | — | 是 |
| D1 | 删除 | 1 | A、B、C 全部 | — | 否 |
| D2 | 防回归 | 1 | D1 | `.github/workflows` | 否 |
| D3 | 四组合冒烟 | 2 | D2 | T3 文档 | 否 |

小计：A 11 天，B 18 天，C 22.5 天，D 4 天，合计约 55.5 人天。

## 7. 风险与待决事项（需要总监知道）

1. **O2：Encore 构建是否接受跨目录 `replace`**（A0 实测）。失败则把 `pkg/relational` 物理位置移到 `encore-api/pkg/relational/`，不影响其余设计。
2. **3.3 的“惰性连接”假设**（A0 复验）：MySQL 模式下 Encore 仍声明了 PG 资源，若 Encore 启动时强制连接，B/C 全部要等总监决定（双发行 / build tag）。
3. **O6（automation 的 schema 有两份来源）**：`sql/mysql/010,011`（平台账本模块 `mysql-automation`）与 d1 `automation/mysqlschema` 是同一份内容。若 MySQL 部署把平台库和 `automation` 资源放进同一个数据库，会出现两个执行者。**建议：默认每个 Encore 资源一个 MySQL 数据库（与 PG 的隔离一致）；A1 核对平台是否自己读写 automation 表（`internal/automationguard` 有 5 条语句），若不读写就从平台模块列表里去掉 `mysql-automation`，automation 服务成为唯一执行者。** A1 和 C9 都要在完成说明里写结论。
4. **broker 控制面仍只支持 PG**（审计文档 §3，用户已决定暂缓）：A 系列不动 `internal/broker*`、`sql/broker`、`cmd/tp-broker-*`、`internal/storage/telemetry_broker.go`；MySQL 配置下必须拒绝启动这些，A1 写测试证明。
5. **d1 的覆盖缺口会原样带过来**：命令“提交到运行时下发”未覆盖、OTA 推送未覆盖（T1b 另修）、casbin 完整策略只有 3 条。各卡完成说明必须写明，**不能说成端到端通过**。
6. **工作量估计的不确定性**：盘点的语句数是下限（见盘点文档第 0 节）；`devicemetadata` 和 `automation` 最可能超出估计。任何一张卡超过 3 天，停下来拆，不往里加东西。
7. **配置键改名（O1）会影响安装器仓库**（T8）：需要同一个发布窗口。
