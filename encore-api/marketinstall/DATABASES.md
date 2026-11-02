# A08 关系库 owner 合同与验收

## 当前范围

`marketinstall/app` 仍是唯一安装编排状态机，`marketinstall/store` 仍只管理安装意图、步骤、代次、租约和回执；业务数据与回执仍由原 provider 管理。本次增加同一个 Store 的 MySQL 实现，不创建第二个安装业务模块。

现有 `service.go` 仍声明 Encore `sqldb.NewDatabase("market_install", ...)` 并调用 PostgreSQL `store.New`。本次 owner 适配器不能单独证明正式服务已经切换到 MySQL，也不能证明 PostgreSQL 不可用时的完整 profile 可用。运行时配置、Encore 资源声明、正式安装器及统一迁移入口由集成负责人串行接线；在该接线和验收完成前，既有 profile 守卫继续保留。

## 接口与数据语义

| 入口 | 合同 |
| --- | --- |
| `store.New(db)` | 保留原 PostgreSQL 连接及 owner；原 `migrations/0001_installations.up.sql` 与 `0002_request_metadata.up.sql` 字节不改。 |
| `store.OpenMySQL(ctx, dsn)` | 受信部署配置提供明确数据库；只建连接并核验能力，不迁移、不启 worker、不修改业务 owner。返回的 `Store.DB` 由调用方关闭。 |
| `Store.MigrateMySQL(ctx)` | 显式迁移 owner 入口；适用于迁移角色。维护独立 `market_install_schema_migrations`，不采用其它 owner 的账本。 |
| `Store.VerifyMySQLSchema(ctx)` | 启动 worker 前的必需门禁，只读核对版本、摘要、表与约束；不会偷偷运行迁移。 |
| `Get / GetByID / Create / Claim / Dispatch / Confirm / Hold / Resume` | 两个方言共用现有 app 与事务流程。`Get` 始终按 tenant、subject、原 key 精确查询；没有按 tenant 单独退化查找。 |

MySQL 的语法能力下限为 8.0.16，目标验收版本为 8.4；MariaDB、5.x 与未经验证的新主版本拒绝。连接固定 READ COMMITTED 事务、UTC、严格 SQL 模式、自动提交、外键与唯一检查，禁止批量语句及参数插值。每个新连接都请求 `ClientFoundRows`，因此合法重复写计入匹配行数；未匹配任何行仍是 `ErrLeaseLost`。具体版本通过情况以测试日志为准。

MySQL 的 operation ID、tenant 和 subject 为 `VARBINARY(1024)`，request key 为 `VARBINARY(128)`，step ID 为 `VARBINARY(200)`。大小写、空格及特殊符号保持原样，不按默认不区分大小写的 collation 合并。超长数据由严格模式拒绝，不截断后写入。业务生成的 UUID 不变。

MySQL plan 与 receipt 使用带 `JSON_VALID` 检查的 LONGTEXT，保留原 `UseNumber` 摘要合同中的整数精度和数字词法。PostgreSQL 既有 JSONB 不重写；若输入数字经 JSONB 规范化后引起原摘要不一致，须单独复现、登记并按 owner 新迁移处理，不修改历史 SQL 或默默改摘要合同。

领取使用行锁、`SKIP LOCKED` 和递增 generation；不在数据库事务里调用 HTTP provider。开始处理回执时先取得对应 tenant/subject/generation 的行锁，再读服务器当前时钟校验租约，避免锁等待前的旧时间允许过期租约继续写。generation 已到 int64 上限时拒绝领取。

MySQL 重复键只识别驱动的 1062，并在 READ COMMITTED 事务中读回该 tenant/key 的已提交记录；原 subject 或请求摘要改变返回冲突。其它 SQL 错误不能转成原键不存在。重复插入后的读回不升级为另一个排他锁，避免多个重复请求同时从唯一键共享锁升级造成死锁。

任一中间写、最终状态写或 Commit 返回错误，都继续作为错误返回。提交回包丢失时，数据库可能已提交；调用方保留原 key 并查询。未知不能触发成功通知，也不能据此生成新实例。暂停、恢复、最终完成均保留现有代次与状态条件。

## 单 schema 单执行者与中断恢复

共用窄接口为 `pkg/ownermigration.WithMySQLSchemaLock(ctx, db, callback)`。callback 获得同一专用 `*sql.Conn` 及服务器 `SELECT DATABASE()` 返回的准确数据库名；迁移中不得切换连接或自行创建另一个 pool 执行 DDL。

锁名为 `thingspanel:schema:` 加准确 UTF-8 数据库名 SHA256 的前 40 位小写十六进制。不得使用客户端别名、小写化后的近似库名或 owner 名。该锁跨同一 schema 的 MySQL owners 共用，版本账本分别保留。已有 ThingsVis 仍归属 Prisma 工具，不合并其历史。

锁持有至 schema 核验和 owner 版本回执持久化完成。取不到锁返回 busy；物理连接死亡不会自动换到无锁连接继续迁移。释放锁的确认未知时丢弃物理连接，并返回 outcome unknown。

每个 MySQL migration 文件只有一个原子 InnoDB DDL。执行前先提交 `applying` 记录及文件 SHA256。中断后，只有在拿到同一锁、文件摘要未变、且实际列、类型、默认值、索引、唯一约束、CHECK 的启用状态和表达式、外键归属均匹配时，才把已创建的目标表确认成该版本结果。没有目标表且仍为原 `applying` 时可继续同一 DDL。结构不匹配、未知版本、摘要变化、无 owner 回执的既存表都拒绝自动采用，不删除数据，不以 `IF NOT EXISTS` 代替核验。

当前结构合同校验明确针对列与数据库约束；数据库管理员在迁移之外添加的触发器或改变服务端执行策略不构成受支持的业务扩展。迁移角色与应用角色须按正式部署方案分离，应用角色无权任意变更 owner schema。

## 真实数据库回归命令

凭证通过隔离测试配置文件注入，不能写入仓库、命令日志或公开 DTO。

```bash
cd encore-api
go test -race -count=1 -v ./marketinstall/app ./marketinstall/store ./pkg/ownermigration
```

`TP_A08_STORE_TEST_DSN` 是已有 PostgreSQL owner 测试通道；`TP_A08_MYSQL_TEST_DSN` 是新增独立 MySQL 8.4 fixture 通道。两个服务器上的基础库名必须以 `tp_takeover_` 或 `tp_a08_` 开头。PG 每个用例创建自己的随机 schema；MySQL 每个用例创建自己的 `tp_a08_mysql_<随机值>` 数据库。cleanup 只删除本次生成对象。

MySQL fixture 角色在专用测试容器内需要创建数据库、表、触发器及终止本用例连接的权限。不能把这些测试权限加给生产运行角色。PG 故障用例仍用原始 migration 文件建私有 schema，运行测试二进制时其工作目录需可读 `../migrations`。

| 验收点 | 实际测试入口 |
| --- | --- |
| 原 PG 七个零行事务回滚边界 | `TestPostgresZeroRows*` |
| PG deferred constraint 在 Commit 阶段真实失败 | `TestPostgresActualDeferredCommitErrorDoesNotReportCompletion` |
| MySQL 七个实际 SQL SIGNAL 写失败边界 | `TestMySQLWriteErrorsRollbackWholeTransaction` |
| 同原键 20 路并发、异摘要/subject 冲突、不同实例 | `TestRelationalConcurrentCreateOriginalKeyAndConflict`、`TestRelationalOpaqueIdentityAndSeparateInstances` |
| 双 worker、过期代次、零匹配 CAS、锁等待过期 | `TestRelationalTwoWorkersClaimDifferentRowsAndFenceExpiredWork`、`TestRelationalLeaseCannotSurviveAWaitForItsRowLock` |
| 未知结果暂停、原键恢复、前序回执与整数精度 | `TestRelationalUnknownPauseResumePreservesOriginalStep`、`TestMySQLPlanPreservesNumberLexemesAndClosedDBIsNotAbsence` |
| 真实 COMMIT 已成功而响应被代理丢弃 | `TestMySQLCommittedCreateReplyLossQueriesOriginalKey`、`TestMySQLCommittedConfirmReplyLossIsUnknownUntilQuery` |
| 迁移并发、物理连接死亡、SIGKILL 前后边界 | `TestMySQLMigrationConcurrentExecutorsUseOneSchemaLock`、`TestMySQLMigrationConnectionDeathDoesNotSwitchToUnlockedSession`、`TestMySQLMigrationSIGKILLBeforeDDLAndAfterDDL` |
| 错误摘要、未知版本、弱化 schema 与受控恢复 | `TestMySQLMigrationRejectsWrongChecksumVersionAndSchemaDrift`、`TestMySQLMigrationReconcilesOnlyItsExactDirtyResult` |

COMMIT 丢回包代理仅在明确的隔离明文 TCP 测试通道上运行，不降级 TLS。SIGKILL 用例运行独立真实进程，在数据库短触发器暂停的实际 DDL 边界杀进程，再由原 owner runner 核对恢复。它们不是业务 provider 的完整可靠性验收；平台业务授权、Agent 执行、SDK 接收方围栏及四 profile 产品链仍须在共同门禁中独立报告。

缺 DSN、TLS 模式不适用或 Windows 平台时明确 skip，不计入实际通过。Go 对“所有子测试都 skip”的外层可能打印 PASS，测试统计须按实际执行的断言与子用例统计。
