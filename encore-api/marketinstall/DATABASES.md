# A08 关系库 owner 合同与验收

## 当前范围

`marketinstall/app` 仍是唯一安装编排状态机，`marketinstall/store` 仍只管理安装意图、步骤、代次、租约和回执；业务数据与回执仍由原 provider 管理。本次增加同一个 Store 的 MySQL 实现，不创建第二个安装业务模块。

现有 `service.go` 仍声明 Encore `sqldb.NewDatabase("market_install", ...)` 并调用 PostgreSQL `store.New`。本次 owner 适配器不能单独证明正式服务已经切换到 MySQL，也不能证明 PostgreSQL 不可用时的完整 profile 可用。运行时配置、Encore 资源声明、正式安装器及统一迁移入口由集成负责人串行接线；在该接线和验收完成前，既有 profile 守卫继续保留。

## 接口与数据语义

| 入口 | 合同 |
| --- | --- |
| `store.New(db)` | 保留原 PostgreSQL 连接及 owner；原 `migrations/0001_installations.up.sql` 与 `0002_request_metadata.up.sql` 字节不改，追加 `0003_exact_plan.up.sql`。 |
| `store.OpenMySQL(ctx, dsn)` | 受信部署配置提供明确数据库；只建连接并核验能力，不迁移、不启 worker、不修改业务 owner。返回的 `Store.DB` 由调用方关闭。 |
| `Store.MigrateMySQL(ctx)` | 显式迁移 owner 入口；适用于迁移角色。维护独立 `market_install_schema_migrations`，不采用其它 owner 的账本。 |
| `Store.VerifyMySQLSchema(ctx)` | 启动 worker 前的必需门禁，只读核对版本、摘要、表与约束；不会偷偷运行迁移。 |
| `Get / GetByID / Create / Claim / Dispatch / Confirm / Hold / Resume` | 两个方言共用现有 app 与事务流程。`Get` 始终按 tenant、subject、原 key 精确查询；没有按 tenant 单独退化查找。 |

MySQL 的语法能力下限为 8.0.16，目标验收版本为 8.4；MariaDB、5.x 与未经验证的新主版本拒绝。连接固定 READ COMMITTED 事务、UTC、严格 SQL 模式、自动提交、外键与唯一检查，禁止批量语句及参数插值。每个新连接都请求 `ClientFoundRows`，因此合法重复写计入匹配行数；未匹配任何行仍是 `ErrLeaseLost`。行数不代表目标值已经落盘：Create、Claim、Dispatch、Confirm、Hold、Resume 都在原事务中读回相应元数据、状态、代次、租约与回执；任何不一致回滚中间步骤，Commit 确认成功后才返回。具体版本通过情况以测试日志为准。

MySQL 的 operation ID、tenant 和 subject 为 `VARBINARY(1024)`，request key 为 `VARBINARY(128)`，step ID 为 `VARBINARY(200)`。大小写、空格及特殊符号保持原样，不按默认不区分大小写的 collation 合并。超长数据由严格模式拒绝，不截断后写入。业务生成的 UUID 不变。

真实 PostgreSQL 回归已证明旧 JSONB 把 `1e+20` 规范化成 `100000000000000000000`，使原 StepHash 与读回计划不一致。保留红灯日志 `team-b-a08/a08-mysql-pg-owner-v2.log`（SHA256 `764456c9f4de8c261b9e30a9e6f4d734d348ed1d0b188d2298e937947ad07fce`）；修复采用追加迁移，不改变 `app.Digest`、`StepHash` 或历史 SQL。

两库新增 `plan_exact` 与 `plan_encoding`，仅支持 `json-number-lexeme-v1`。both NULL 表示 legacy；明确的空字符串、未知编码、仅一列缺失都不能按 legacy 猜测。新写同事务保存原 `json.Marshal(plan)` 文本、旧兼容列和原 step hashes；写后精确读回，PostgreSQL 兼容列再以 JSONB 语义核对，MySQL 以字节核对。MySQL 旧 plan 和 receipt 已是带 `JSON_VALID` 的 LONGTEXT，原数据继续保留。

原键/ID 的 legacy 状态查询不接管或执行计划。Claim 持原操作行锁后检查全部 step 的连续 ordinal、总数、step ID、稳定 key、原 payload hash 及 artifact identity；全部匹配才 CAS 补写 exact/encoding，并在同一事务领取。旧 PG 指数数据若已失去原词法，摘要不匹配便继续拒绝，保持原 hash、代次与状态，须按原 provider/制品证据人工核对，不能重算新 hash 让它通过。

领取使用行锁、`SKIP LOCKED` 和递增 generation；不在数据库事务里调用 HTTP provider。真实 v3 的 MySQL 双 worker 回归出现两条 queued 记录中一条未领取，PG 同用例通过，红灯保存在 `team-b-a08/a08-mysql-pg-owner-v3-red.log`（SHA256 `88b6fd95650d86fd0fc9a1fd6399aeadb63090dc80910b92410ea0b7e9036f4c`）。原 MySQL OR 条件与 created_at 排序不能由原索引共同满足，排序前扫描和加锁的范围是待验证的原因。

MySQL 追加 0004，用原 state、created_at、lease_until 生成 stored `claim_ready_at`，queued 的可领取时间为创建时间，running 为租约到期时间，paused/completed/rejected 为 NULL。`(claim_ready_at,id)` 索引按到期时间与原 ID 稳定排序，Claim 强制该索引范围读取并 `LIMIT 1 FOR UPDATE SKIP LOCKED`，保持未知/空租约不可执行。运行时 schema 核验包括生成表达式、STORED 属性与完整索引；不能人工另写 readiness 状态。真实回归必须核对生产 SQL 的 EXPLAIN 无 filesort，并在第一条生产 Claim 持锁时证明第二个 worker 可领取另一条记录；不能只以单 worker 成功代替。开始处理回执时先取得对应 tenant/subject/generation 的行锁，再读服务器当前时钟校验租约，避免锁等待前的旧时间允许过期租约继续写。generation 已到 int64 上限时拒绝领取。

MySQL 重复键只识别驱动的 1062，并在 READ COMMITTED 事务中读回该 tenant/key 的已提交记录；原 subject 或请求摘要改变返回冲突。其它 SQL 错误不能转成原键不存在。重复插入后的读回不升级为另一个排他锁，避免多个重复请求同时从唯一键共享锁升级造成死锁。

任一中间写、最终状态写或 Commit 返回错误，都继续作为错误返回。提交回包丢失时，数据库可能已提交；调用方保留原 key 并查询。未知不能触发成功通知，也不能据此生成新实例。暂停、恢复、最终完成均保留现有代次与状态条件。

## 单 schema 单执行者与中断恢复

共用窄接口为 `pkg/ownermigration.WithMySQLSchemaLock(ctx, db, callback)`。callback 获得同一专用 `*sql.Conn` 及服务器 `SELECT DATABASE()` 返回的准确数据库名；迁移中不得切换连接或自行创建另一个 pool 执行 DDL。

锁名为 `thingspanel:schema:` 加准确 UTF-8 数据库名 SHA256 的前 40 位小写十六进制。不得使用客户端别名、小写化后的近似库名或 owner 名。该锁跨同一 schema 的 MySQL owners 共用，版本账本分别保留。已有 ThingsVis 仍归属 Prisma 工具，不合并其历史。

锁持有至 schema 核验和 owner 版本回执持久化完成。取不到锁返回 busy；物理连接死亡不会自动换到无锁连接继续迁移。释放锁的确认未知时丢弃物理连接，并返回 outcome unknown。

每个 MySQL migration 文件只有一个原子 InnoDB DDL。0001/0002 创建表的文件字节保留；0003 是新增 exact-plan 字段的 ALTER，0004 是新增生成时间与领取索引的 ALTER。新增 STORED 列需要维护窗口内重建表，不能宣称在线无阻塞升级；正式 runner 接线前仍不自动运行。执行前先提交 `applying` 记录及文件 SHA256。中断后，只有在拿到同一锁、文件摘要未变、且实际列、类型、默认值、索引、唯一约束、CHECK 的启用状态和表达式、外键归属均匹配时，才把已创建的目标表确认成该版本结果。每版分别核对 before/after schema：尚处于原 before schema 时可继续同一 DDL，已经处于精确 after schema 时只确认原版本回执。仅接受已应用前缀后至多一个 applying 意图；缺少中间版本或较早 dirty 后已有较新版本，均不自动接管。迁移最终回执也读回确认，静默抑制状态写不能当作成功。结构不匹配、未知版本、摘要变化、无 owner 回执的既存表都拒绝自动采用，不删除数据，不以 `IF NOT EXISTS` 代替核验。

当前结构合同校验明确针对列与数据库约束；数据库管理员在迁移之外添加的触发器或改变服务端执行策略不构成受支持的业务扩展。迁移角色与应用角色须按正式部署方案分离，应用角色无权任意变更 owner schema。

## 真实数据库回归命令

凭证通过隔离测试配置文件注入，不能写入仓库、命令日志或公开 DTO。

```bash
cd encore-api
go test -race -count=1 -v ./marketinstall/app ./marketinstall/store ./pkg/ownermigration
```

`TP_A08_STORE_TEST_DSN` 是已有 PostgreSQL owner 测试通道；`TP_A08_MYSQL_TEST_DSN` 是新增独立 MySQL 8.4 fixture 通道。两个服务器上的基础库名必须以 `tp_takeover_` 或 `tp_a08_` 开头。PG 每个用例创建自己的随机 schema；MySQL 每个用例创建自己的 `tp_a08_mysql_<随机值>` 数据库。cleanup 只删除本次生成对象。

MySQL fixture 角色在专用测试容器内需要创建数据库、表、触发器及终止本用例连接的权限。不能把这些测试权限加给生产运行角色。PG 故障用例用原始 0001/0002 加新 0003 建私有 schema；运行测试二进制时其工作目录需可读 `../migrations`。MySQL v2→当前 owner 版本升级用例另外读取未改的 `mysql_migrations/0001_operations.sql` 与 `0002_steps.sql`，从旧结构和原版本回执升级真实数据库。

| 验收点 | 实际测试入口 |
| --- | --- |
| 原 PG 七个零行事务回滚边界 | `TestPostgresZeroRows*` |
| PG deferred constraint 在 Commit 阶段真实失败 | `TestPostgresActualDeferredCommitErrorDoesNotReportCompletion` |
| MySQL 七个实际 SQL SIGNAL 写失败边界 | `TestMySQLWriteErrorsRollbackWholeTransaction` |
| MySQL 七个静默 UPDATE 抑制、部分改动与回执变更 | `TestMySQLSilentWritesCannotAcknowledgeUnpersistedState`、`TestMySQLPartlyChangedTargetAndRewrittenReceiptStillRollback` |
| 同原键 20 路并发、异摘要/subject 冲突、不同实例 | `TestRelationalConcurrentCreateOriginalKeyAndConflict`、`TestRelationalOpaqueIdentityAndSeparateInstances` |
| 双 worker、过期代次、零匹配 CAS、锁等待过期 | `TestRelationalTwoWorkersClaimDifferentRowsAndFenceExpiredWork`、`TestRelationalLeaseCannotSurviveAWaitForItsRowLock` |
| 领取范围索引、真实持锁并发、终态排除 | `TestMySQLClaimRangeDoesNotLockEveryReadyOperation`、`TestMySQLClaimReadinessIncludesExpiredLeasesAndExcludesFinalStates` |
| 未知结果暂停、原键恢复、前序回执与整数精度 | `TestRelationalUnknownPauseResumePreservesOriginalStep`、`TestMySQLPlanPreservesNumberLexemesAndClosedDBIsNotAbsence` |
| 真实 COMMIT 已成功而响应被代理丢弃 | `TestMySQLCommittedCreateReplyLossQueriesOriginalKey`、`TestMySQLCommittedConfirmReplyLossIsUnknownUntilQuery` |
| 已取得 schema 锁但回包丢失 | `TestMySQLAcquiredSchemaLockReplyLossDiscardsUnknownSession` |
| 新计划与 legacy 全步骤核验、原版本2真实升级 | `TestPostgresPlanExponentPreservesDigestAfterDatabaseRoundTrip`、`TestRelationalLegacyPlanAdoptionRequiresEveryOriginalStep`、`TestMySQLAdditiveExactPlanUpgradeFromOriginalVersionTwo` |
| 迁移并发、物理连接死亡、CREATE/exact-plan ALTER/readiness ALTER 三组 SIGKILL 前后边界 | `TestMySQLMigrationConcurrentExecutorsUseOneSchemaLock`、`TestMySQLMigrationConnectionDeathDoesNotSwitchToUnlockedSession`、`TestMySQLMigrationSIGKILLBeforeDDLAndAfterDDL` |
| 错误摘要、未知版本、弱化 schema 与受控恢复 | `TestMySQLMigrationRejectsWrongChecksumVersionAndSchemaDrift`、`TestMySQLMigrationReconcilesOnlyItsExactDirtyResult`、`TestMySQLSilentMigrationReceiptIsNeverReady` |

COMMIT 丢回包代理仅在明确的隔离明文 TCP 测试通道上运行，不降级 TLS。SIGKILL 用例运行独立真实进程，在数据库短触发器暂停的实际 DDL 边界杀进程，再由原 owner runner 核对恢复。它们不是业务 provider 的完整可靠性验收；平台业务授权、Agent 执行、SDK 接收方围栏及四 profile 产品链仍须在共同门禁中独立报告。

缺 DSN、TLS 模式不适用或 Windows 平台时明确 skip，不计入实际通过。Go 对“所有子测试都 skip”的外层可能打印 PASS，测试统计须按实际执行的断言与子用例统计。

## 参考依据

- MySQL 8.4 官方手册：[locking reads / SKIP LOCKED](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html)、[事务隔离](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html)、[原子 DDL](https://dev.mysql.com/doc/refman/8.4/en/atomic-ddl.html)、[实际索引扫描锁范围](https://dev.mysql.com/doc/refman/8.4/en/innodb-locks-set.html)、[排序与 EXPLAIN](https://dev.mysql.com/doc/refman/8.4/en/order-by-optimization.html)、[生成列](https://dev.mysql.com/doc/refman/8.4/en/create-table-generated-columns.html)。
- 固定驱动：[go-sql-driver/mysql v1.7.0](https://github.com/go-sql-driver/mysql/tree/v1.7.0)，`ClientFoundRows` 的语义由真实静默更新与读回回归共同验证。
