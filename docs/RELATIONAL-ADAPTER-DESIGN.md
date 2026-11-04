# 关系库底座设计（T1-0）

> 日期：2026-11-04。状态：**待总监审定**。依据：`docs/PROJECT-AUDIT-2026-11-04.md` §1（A1–A5）、§2（方案乙，翻译器整套废弃）。
> 配套：[RELATIONAL-INVENTORY.md](RELATIONAL-INVENTORY.md)（盘点数据）、[T1-CARDS.md](T1-CARDS.md)（拆卡）、骨架包 `pkg/relational/`。
> 本文只做设计和骨架，不改任何业务模块。

## 0. 一页纸摘要

| 问题 | 决定 |
| --- | --- |
| 公共底座放哪、怎么共享 | `pkg/relational` 是**独立的 Go 模块**（自带 `go.mod`），根模块 `project` 和 `encore-api` 模块各自 `require` + `replace` 指向它。**只有一份源码**，删除 `copy_in_sync_test.go` 这类“拷贝两份再比对”的做法。已验证两个模块都能解析（第 1 节） |
| 底座做什么 | 四件事：配置（`relational.driver`）、`Open/Adopt` 返回 `*gorm.DB`、方言枚举、错误分类。另在 T1-A0 补两件**机制性**的事：迁移执行（带库级互斥）和差分测试框架。**不改写 SQL 文本，不做万能 SQL 接口，不做第二套 ORM** |
| `relational.driver` 在哪解析 | 每个进程**启动时解析一次**：平台进程在 `initialize`，Encore 进程在 `encore-api/pkg/relationaldb`。都调用 `relational.ResolveDialect`，其余代码只拿到已经解析好的 `*gorm.DB`，用 `relational.DialectOf(db)` 取方言，**不再读任何配置** |
| 连接池 | 每个**进程里的每个逻辑资源一个池**，由 `relational.Registry` 登记，重复登记报错。谁 `Open` 谁关闭；`Adopt`（借用 Encore 自己的池）的不关 |
| Encore 模块怎么办 | 保留 `sqldb.NewDatabase("device_metadata", ...)` 声明和 `migrations/` 目录**不动**（PG 的资源身份、迁移历史原样保留，PG 迁移仍由 Encore 执行）；PG 模式下 `relational.Adopt(sqldb.Stdlib())`，MySQL 模式下 `relational.Open(MySQL DSN)`。模块里的 store 只拿 `*gorm.DB` |
| 双实现 | repository 里 `switch relational.DialectOf(db)`，两个实现是**同一接口的两个类型**，各写各的 SQL。第 4 节有三个完整范例：队列领取、条件 upsert、咨询锁 |
| 错误 | `relational.Classify` → `KindUnique / KindNotFound / KindTimeout / KindDeadlock / KindForeignKey`。取代 `dbx.IsUniqueViolation` 及各模块手写的 1062/23505 判断 |
| MySQL 迁移 | 每个 Encore 服务的 MySQL 迁移放在 `<service>/migrations-mysql/`（**不能**放进 `migrations/`，Encore 的 PG 执行器会把它当 PG 迁移执行）；平台放 `sql/mysql/`。每个 schema 只有一个执行者；PG 已执行的迁移一字不改 |

## 1. 包结构与共享方式

### 1.1 为什么不能再“复制两份”

根模块（`module project`，go 1.23）和 `encore-api`（`module encore.app`，go 1.24，依赖 `encore.dev`）是两个互相不能导入的 Go 模块。翻译器因此被逐字节复制了两份，用测试保证一致。那是错误的做法：任何一次修改都要改两处。

### 1.2 方案：独立模块 + `replace`

```
pkg/relational/            module github.com/ThingsPanel/thingspanel-backend-community/pkg/relational
  go.mod  go.sum           只依赖 gorm、gorm 的 postgres/mysql 驱动、pgx、go-sql-driver/mysql；不依赖 encore.dev
  dialect.go config.go open.go registry.go errors.go          （T1-0 已交付，含单元测试）
  migrate.go reltest/                                         （T1-A0 交付：迁移执行、差分测试框架）

go.mod            require .../pkg/relational v0.0.0   replace .../pkg/relational => ./pkg/relational
encore-api/go.mod require .../pkg/relational v0.0.0   replace .../pkg/relational => ../pkg/relational
```

- **已验证**（用 `go list -modfile` 在不改仓库的前提下试过）：两个模块加上 `require` + `replace` 后，都能解析并列出该包及其依赖（`gorm.io/gorm`），`encore-api` 的 `go.mod` 只多一行 `require` 和一行 `replace`。
- **没有验证、必须在 T1-A0 里验证**：`encore build docker` 和 `encore test` 在存在 `../` 形式的 `replace` 时能否正常构建（d1 记录显示本机 `encore build docker` 会空转）。**验收项**：T1-A0 必须在 `encore-api` 里跑通 `encore test ./pkg/relationaldb/...`，并产出一次 `encore build docker` 的结果（成功或明确的失败原因）。如果 Encore 构建拒绝 `encore-api` 目录之外的 `replace`（`../pkg/relational`），**后备方案**：把这个模块的物理位置挪到 `encore-api/pkg/relational/`（仍是独立 Go 模块、仍只有一份源码），根模块改成 `replace ... => ./encore-api/pkg/relational`。Encore 应用目录内的 `replace` 目标是自包含的，预期可行，但同样要在 T1-A0 里实测。见第 9 节 O2。**不允许退回到复制。**
- 版本漂移：根模块用 gorm v1.25.7，`encore-api` 用 pgx v5.7.6。`pkg/relational/go.mod` 只写最低版本，由使用方的 MVS 决定最终版本；T1-A0 在两个模块里各跑 `go test ./...` 做兼容确认。

### 1.3 谁可以导入 `pkg/relational`

任何模块都可以。它**不得**导入 `project/...` 或 `encore.app/...`，也不得导入 `encore.dev`。Encore 专用的粘合代码放在 `encore-api/pkg/relationaldb`（只做“把 `sqldb.Database` 变成 `relational.Adopt` 的入参”，约 60 行，T1-A0 交付）。

## 2. 配置与连接

### 2.1 配置键

一个部署只有**一个** `relational.driver`（`postgres` 默认 | `mysql`）。一个部署里所有关系库资源用同一种引擎；不支持“平台用 PG、自动化用 MySQL”这样的混搭（避免组合爆炸，也与“发行版只有一种”一致）。

```
relational.driver                                postgres | mysql（空 = postgres；其他值 = 启动失败）
relational.<engine>.<resource>.dsn               一个资源的完整 DSN（Encore 模块用这个）
relational.<engine>.<field>                      该引擎所有资源共用的结构化项：
                                                 host port username password time_zone open_conns idle_conns
                                                 conn_max_lifetime log_level slow_threshold_ms
relational.<engine>.<resource>.<field>           同上，针对单个资源（database 通常写在这里）
```

`<engine>` 是 `postgres` 或 `mysql`；`<resource>` 是逻辑资源名（`platform`、`device_metadata`、`automation`、`northbound`、`file_storage`、`notification`、`connector_runtime`、`market_install`）。**逻辑资源名永远不从数据库名推导**；它是连接池登记键、配置键、迁移账本键。

读取方式：骨架里的 `relational.Source`（`func(key string) string`）。平台进程传 viper（沿用现有 `GOTP_` 环境变量覆盖规则），Encore 进程传 `relational.EnvSource()`（键 `relational.mysql.device_metadata.dsn` → 环境变量 `TP_RELATIONAL_MYSQL_DEVICE_METADATA_DSN`）。同一份解析代码、同一份默认值、同一份校验。

### 2.2 “只在一处解析”

```
平台进程   main → initialize.RelationalInit()
              src := viper-backed Source
              target, err := relational.ResolveTarget(src, "platform")   ← 此处读 relational.driver
              db, err := relational.Open(ctx, target)
              global.Relational.Register("platform", db, owned=true)
              global.DB = db            （迁移期间保留的全局句柄，T1-A 不改变它的使用方式）

Encore 进程 encore-api/pkg/relationaldb（包级 sync.Once）
              dialect := relational.ResolveDialect(relational.EnvSource())   ← 此处读 relational.driver
              Open(resource, pg *sqldb.Database) → *gorm.DB
                  postgres: relational.Adopt(Postgres, pg.Stdlib(), opts)        owned=false
                  mysql:    relational.Open(ctx, ResolveTarget(env, resource))   owned=true
```

业务代码（repository、service、handler）**只收到 `*gorm.DB`**。需要分支时调用 `relational.DialectOf(db)`，它读的是 GORM 句柄的 dialector，不读配置。T1-D 的检查会禁止业务包直接 `viper.GetString("relational...")` 或 `os.Getenv("TP_RELATIONAL_...")`。

旧键 `db.driver`、`db.psql.*`、`db.mysql.*`、`TP_*_DB_DRIVER`、`TP_*_MYSQL_DSN` 被 `relational.*` 取代。是否保留一个版本的读取兼容，是决定项 O1。**我的建议：不保留**（用户已决定“不留过渡层”；MySQL 档位尚未发布；`configs/conf*.yml` 和安装器模板在同一个发布里改，T8 负责安装器侧）。

### 2.3 每个逻辑资源一个池：谁创建、谁关闭

| 进程 | 资源 | 创建者 | 关闭者 | 方式 |
| --- | --- | --- | --- | --- |
| 平台 | `platform` | `initialize.RelationalInit` | `main` 的关停流程（`Registry.Close`） | `Open`，owned |
| Encore | `device_metadata`、`automation`、…（PG） | **Encore 运行时** | Encore 运行时 | `Adopt(sqldb.Stdlib())`，not owned |
| Encore | 同上（MySQL） | 拥有该资源的服务的 `initService` | 该服务的 `Shutdown`（`relationaldb.Close(resource)`） | `Open`，owned |
| Encore | `platform`（`authcore`、`platformconfig`、`devicemetadata/legacydb`） | 使用它的服务，各自一个池 | 同服务 `Shutdown` | `Open`；**不再用 `pgxpool` 或自己的 `sql.Open`** |

规则：
1. `Registry.Register` 对同名资源第二次登记返回错误；因此“同一资源两个池”在启动时就失败，不会悄悄漏连接。
2. `Registry.Close` 只关 owned 的池，借来的池归原主。
3. 共享同一数据库的两个逻辑资源（thingmodel 用 `device_metadata`）**不另开池**：thingmodel 用 `relationaldb.Get("device_metadata")` 取已登记的句柄，不登记。
4. 同一个平台库被平台进程和 Encore 进程各开一个池是允许的（两个进程，各一个）；池大小在各自配置里设置，总和不得超过库的 `max_connections`（T2 文档写明）。

### 2.4 骨架里的 API（已实现，有单元测试和真实库测试）

```go
package relational

type Dialect string                       // Postgres | MySQL
func ParseDialect(raw string) (Dialect, error)       // ""→Postgres；未知值报错，不静默
func DialectOf(db *gorm.DB) (Dialect, error)

type Source func(key string) string       // viper / EnvSource / MapSource
func ResolveDialect(src Source) (Dialect, error)      // 唯一读取 relational.driver 的地方
func ResolveTarget(src Source, resource string) (Target, error)

type Target struct { Name; Dialect; DSN; Host; Port; Database; Username; Password; TimeZone; Pool PoolOptions }
func Open(ctx context.Context, t Target) (*gorm.DB, error)          // 建池 + ping；调用方负责关闭
func Adopt(d Dialect, pool *sql.DB, p PoolOptions) (*gorm.DB, error) // 借用别人的池；不关闭
func CloseDB(db *gorm.DB) error

type Registry struct{...}                 // Register(name, db, owned) / Get / Names / Close

type Kind int                             // KindOther/Unique/NotFound/Timeout/Deadlock/ForeignKey
func Classify(err error) Kind
func Is(err error, k Kind) bool
func IsUnique(err error, constraint string) bool
func (k Kind) Retryable() bool
```

安全与行为细节（均有测试）：MySQL 连接强制 `parseTime=true`、`clientFoundRows=true`（`RowsAffected` 统计**匹配**行而非**变更**行，与 PG 一致；否则幂等 UPDATE 会被误判为冲突）、`charset=utf8mb4`；DSN 由结构化字段用驱动的 `Config.FormatDSN()` 或 `url.URL` 生成，用户名密码里的特殊字符会被正确转义；连接错误信息会把 DSN 和密码替换成 `***`；非法 MySQL DSN 的报错不回显 DSN。

## 3. Encore 模块如何换成自管的 GORM 连接

### 3.1 保留什么

| 事物 | 处理 |
| --- | --- |
| `sqldb.NewDatabase("device_metadata", {Migrations: "./migrations"})` 声明 | **保留，一字不改**。Encore 要求资源在包级静态声明；这决定了 PG 的资源身份、`encore run` 本地起库、测试起库 |
| `migrations/` 下的 PG 迁移文件、Encore 的 `schema_migrations` 账本 | **保留，一字不改**。PG 迁移的执行者仍是 Encore |
| 资源名字符串（`"device_metadata"`） | 同时作为 `relational` 的逻辑资源名，用于 `Registry`、配置键、MySQL 账本键 |
| `thingmodel` 的 `sqldb.Named("device_metadata")` | 保留；改成从 `relationaldb.Get("device_metadata")` 取句柄 |

### 3.2 换什么

每个服务的 `db.go` 变成：

```go
// 示意：encore-api/devicemetadata/db.go
var encoreDB = sqldb.NewDatabase("device_metadata", sqldb.DatabaseConfig{Migrations: "./migrations"}) // 不变

func initService() (*Service, error) {
    gdb, err := relationaldb.Open("device_metadata", encoreDB) // PG: Adopt(encoreDB.Stdlib()); MySQL: Open(DSN)
    if err != nil { return nil, err }
    if relational.MustDialectOf(gdb) == relational.MySQL {
        if err := relational.Migrate(ctx, gdb, "device_metadata", mysqlMigrations, "migrations-mysql"); err != nil { // T1-A0
            return nil, err
        }
    }
    return &Service{db: gdb}, nil
}
func (s *Service) Shutdown(context.Context) { relationaldb.Close("device_metadata") }
```

包级全局 `var db dbx.DB` 和 `UseDatabase` 一并删除，改成服务结构体的字段或构造函数参数（现有代码很多处直接用包级 `db`；T1-B/T1-C 的卡片按文件逐步改，分支内允许新旧句柄并存，合并前清零，见 T1-CARDS 的“并存规则”）。

### 3.3 MySQL 模式下 Encore 仍声明了 PG 资源，会怎样

d1 的记录显示：Encore 进程在只有一个不可解析的 PG 主机时可以启动，`healthz` 返回 200，需要库的处理器返回 500，说明 Encore 对声明的库是**惰性连接**。**这是一个必须在 T1-A0 复验的假设**（写一个测试：PG 主机不可达 + `relational.driver=mysql`，服务能启动并通过 MySQL 响应一个读写请求）。若假设不成立，后备：构建时用 build tag 区分 PG 与 MySQL 两个发行二进制（仍不是翻译层，但需要总监决定）。

## 4. 方言枚举与 repository 内双实现

### 4.1 写法规则

1. **能用 GORM 链式或同一份带 `?` 的 SQL 就只写一份。** `FOR UPDATE`、`WITH [RECURSIVE]`（不含 `RETURNING`）、简单 `ON CONFLICT DO UPDATE` 都属于这一类。时间运算优先在 Go 里算好再作为参数传。
2. 一份写不下时，在**所属 repository 包内**定义一个小接口，写两个类型，构造函数里 `switch relational.DialectOf(db)`。**不准**在一个函数里用 `if dialect == MySQL { sql = "..." }` 拼出两种 SQL 文本（那就是换了个位置的翻译器），也不准把分支放到 `pkg/relational` 里做成“通用”。
3. 两个实现必须有**同一个差分测试**：同一场景在 PG 和 MySQL 上跑，比较每一步的返回和最终表状态（框架见 T1-A0 的 `reltest`；场景取自 `wp/d1-dbx-returning`）。没有差分测试的双实现不接收。
4. 接口只暴露业务动作（`Claim`、`UpsertCurrent`、`LockKey`），不暴露 SQL 或方言。

### 4.2 范例一：队列领取（`SKIP LOCKED` + `UPDATE ... RETURNING`）

取自 `internal/dal/notification_source_outbox.go` 现状：PG 用 `WITH picked AS (... FOR UPDATE SKIP LOCKED) UPDATE ... FROM picked ... RETURNING o.*`。MySQL 没有 `UPDATE ... FROM` 和 `RETURNING`，必须“锁候选行 → 按 id 更新 → 读回”，在一个事务里。

```go
// 示意（不是已编译代码）：internal/dal/source_outbox_claim.go
type sourceOutboxClaimer interface {
    Claim(ctx context.Context, now time.Time, lease time.Duration, maxAttempts, limit int) ([]SourceOutboxRecord, error)
}

func newSourceOutboxClaimer(db *gorm.DB) sourceOutboxClaimer {
    switch relational.MustDialectOf(db) {
    case relational.Postgres:
        return pgOutboxClaimer{db}
    default: // relational.MySQL
        return myOutboxClaimer{db}
    }
}

// PG：一条语句。租约令牌由 Go 生成（两个实现用同样的来源，不用 gen_random_uuid）。
func (c pgOutboxClaimer) Claim(ctx context.Context, now time.Time, lease time.Duration, maxAttempts, limit int) (out []SourceOutboxRecord, err error) {
    token := uuid.New()
    err = c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := expireExhausted(tx, now, maxAttempts); err != nil { return err } // 两个实现共用的普通 UPDATE
        return tx.Raw(`WITH picked AS (
                SELECT id FROM notification_source_outbox
                WHERE ((state IN ('pending','retry_wait') AND next_attempt_at <= ?) OR (state = 'leased' AND lease_until < ?)) AND attempts < ?
                ORDER BY next_attempt_at, created_at FOR UPDATE SKIP LOCKED LIMIT ?)
            UPDATE notification_source_outbox o
               SET state='leased', attempts=o.attempts+1, lease_token=?, lease_until=?, updated_at=?
              FROM picked WHERE o.id = picked.id RETURNING o.*`,
            now, now, maxAttempts, limit, token, now.Add(lease), now).Scan(&out).Error
    })
    return out, err
}

// MySQL：三步，同一事务。候选查询必须能走 (state, next_attempt_at, created_at) 索引，
// 否则 SKIP LOCKED 会把所有候选行都锁住（d1 实测的缺陷）；该索引在 migrations-mysql 里建，
// 并由测试 EXPLAIN 断言“无 filesort”。
func (c myOutboxClaimer) Claim(ctx context.Context, now time.Time, lease time.Duration, maxAttempts, limit int) (out []SourceOutboxRecord, err error) {
    token := uuid.New()
    err = c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := expireExhausted(tx, now, maxAttempts); err != nil { return err }
        var ids []string
        if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
            Model(&SourceOutboxRecord{}).
            Where("((state IN ? AND next_attempt_at <= ?) OR (state = ? AND lease_until < ?)) AND attempts < ?",
                []string{"pending", "retry_wait"}, now, "leased", now, maxAttempts).
            Order("next_attempt_at, created_at").Limit(limit).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
            return err
        }
        if err := tx.Model(&SourceOutboxRecord{}).Where("id IN ?", ids).
            Updates(map[string]any{"state": "leased", "attempts": gorm.Expr("attempts + 1"),
                "lease_token": token, "lease_until": now.Add(lease), "updated_at": now}).Error; err != nil {
            return err
        }
        return tx.Where("id IN ?", ids).Find(&out).Error // 读回；锁仍由本事务持有，其他 worker 不会改它们
    })
    return out, err
}
```

差分测试：两个 goroutine 同时 `Claim(limit=1)` 对 2 条 pending 记录，两个库上都必须各领到不同的一条（d1 中 marketinstall 的“双 worker 领取”和 notification 的 `worker_claim` 场景是现成的）。

### 4.3 范例二：条件 upsert（“只在时间更新时覆盖”）

取自 `internal/storage` 的“当前值”：PG 是 `ON CONFLICT (device_id, key) DO UPDATE SET ... WHERE existing.ts <= excluded.ts`；MySQL 没有 `DO UPDATE ... WHERE`。develop 已经有 MySQL 写法（`upsertCurrentMySQL`），原样作为这个模式的样板。

```go
// 示意：internal/storage/current_upsert.go
type currentUpserter interface{ Upsert(ctx context.Context, rows []TelemetryCurrentData) error }

func newCurrentUpserter(db *gorm.DB) currentUpserter {
    if relational.MustDialectOf(db) == relational.MySQL { return myCurrent{db} }
    return pgCurrent{db}
}

// PG：GORM 的 OnConflict 自带 Where，不需要原始 SQL。
func (u pgCurrent) Upsert(ctx context.Context, rows []TelemetryCurrentData) error {
    return u.db.WithContext(ctx).Clauses(clause.OnConflict{
        Columns:   []clause.Column{{Name: "device_id"}, {Name: "key"}},
        DoUpdates: clause.AssignmentColumns([]string{"ts", "bool_v", "number_v", "string_v", "tenant_id"}),
        Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "telemetry_current_datas.ts <= excluded.ts"}}},
    }).CreateInBatches(rows, 500).Error
}

// MySQL：逐列 IF(new.ts >= old.ts, new.x, old.x)，ts 放在最后赋值（MySQL 从左到右对更新后的行求值）。
// 现有 upsertCurrentMySQL 的写法保留，只是挪进 myCurrent 并加上差分测试。
```

关键的行为差异（差分测试必须覆盖）：同一批里重复键、旧时间戳不覆盖、相等时间戳覆盖、`RowsAffected` 的含义（MySQL 的 `ON DUPLICATE KEY UPDATE` 返回 1 表示插入、2 表示更新、0 表示无变化，`clientFoundRows` 下又有不同；因此**不要从 `RowsAffected` 推断业务结论**，要读回）。

### 4.4 范例三：咨询锁（事务级互斥）

PG：`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`，随事务结束释放。MySQL 的 `GET_LOCK` 是**会话级**，不随事务结束释放，且与连接池交互危险，**不能**当作替身。显式实现：一张锁表，事务里 `INSERT IGNORE` + `SELECT ... FOR UPDATE` 锁一行，随事务提交或回滚释放。

```go
// 示意：每个使用咨询锁的 repository 包里都需要。8+ 处使用，见决定项 O3：是否收进 pkg/relational。
type txLocker interface{ Lock(ctx context.Context, tx *gorm.DB, key string) error }

func newTxLocker(db *gorm.DB) txLocker {
    if relational.MustDialectOf(db) == relational.MySQL { return rowLocker{} }
    return advisoryLocker{}
}

func (advisoryLocker) Lock(ctx context.Context, tx *gorm.DB, key string) error {
    return tx.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, key).Error
}

func (rowLocker) Lock(ctx context.Context, tx *gorm.DB, key string) error {
    sum := sha256.Sum256([]byte(key))               // 固定长度，避免超长键和排序规则问题
    k := hex.EncodeToString(sum[:])                 // CHAR(64) ascii_bin
    if err := tx.WithContext(ctx).Exec(`INSERT IGNORE INTO tp_advisory_locks (lock_key) VALUES (?)`, k).Error; err != nil {
        return err
    }
    return tx.WithContext(ctx).Exec(`SELECT lock_key FROM tp_advisory_locks WHERE lock_key = ? FOR UPDATE`, k).Error
}
```

锁表 `tp_advisory_locks(lock_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY)` 作为每个 MySQL schema 的 baseline 迁移的一部分（d1 的 `mysqlowner.AdvisoryLocksTable` 已有定义）。**必须在事务内调用**；`rowLocker` 在事务外调用没有意义，函数应在 `tx.Statement.ConnPool` 不是事务时返回错误（卡片里加断言）。**取锁顺序**：同一事务取多把锁时，两个实现都必须按键排序后取，避免 MySQL 行锁死锁（PG 咨询锁同理）。

### 4.5 仍然需要手写 MySQL 触发器/CHECK 的部分

d1 的“网关树守卫”“模板模型选择”“CHECK 替代（校验函数 74 个取值）”在 MySQL 上是触发器。它们是 **schema 的一部分**，进 `migrations-mysql/`，由人手写、人审、提交。数据库端守卫不替代 repository 层的显式检查：跨租户保护在 MySQL 上不能依赖 PG 的行级安全，**每条查询和写入的 repository 代码都要显式带 `tenant_id` 条件**（PG 上保留 RLS 作为第二道防线）。

## 5. 错误分类

`relational.Classify(err)` 把驱动错误归成与引擎无关的类别。repository 只判断类别，不判断厂商错误码。

| Kind | PostgreSQL | MySQL | 通用 | 业务上怎么处理 |
| --- | --- | --- | --- | --- |
| `KindUnique` | `23505`（`ConstraintName`） | `1062`（消息里的 `table.index`） | — | 幂等重放、编号冲突；用 `IsUnique(err, "约束名")` 区分是哪个唯一键 |
| `KindNotFound` | — | — | `sql.ErrNoRows`、`gorm.ErrRecordNotFound` | 映射到 404 或“不存在”；注意 `Raw().Scan()` 找不到行**不报错**，要用 `Take` 或检查 `RowsAffected` |
| `KindTimeout` | `57014`（语句取消）、`55P03`（锁不可用） | `1205`（锁等待超时）、`3024`（语句超时）、`1317` | `context.DeadlineExceeded`、`context.Canceled`、`net.Error.Timeout()` | 返回“暂时不可用”，调用方可重试 |
| `KindDeadlock` | `40P01`、`40001`（序列化失败） | `1213` | — | 整个事务可重试（最多 3 次，调用方负责，底座不自动重试） |
| `KindForeignKey` | `23503` | `1451`、`1452` | — | 引用冲突 |
| `KindOther` | 其余 | 其余 | 其余 | 原样返回 |

`Kind.Retryable()` 对 `Deadlock` 和 `Timeout` 为真。已用真实 PG 16 和 MySQL 8.4 验证了唯一冲突、无行、语句超时（300 ms 截止）、死锁（两个事务反向锁两行）四种场景的分类，见 `pkg/relational/integration_test.go`。

替换关系：`dbx.IsUniqueViolation`、各 store 里手写的 `isDuplicateKey`（`automation/store/mysql_support.go`）、`devicemetadata` 的 `wrapDBErr` 里按厂商码判断的部分、`pkg/dbdialect` 里的 1062 判断，全部改用 `relational.Classify`。

## 6. 迁移约定

### 6.1 总原则

1. **PG 已执行的迁移一字不改**（包括 `encore-api/*/migrations/*.up.sql`、`sql/1..32.sql`、`sql/*/` 下的组件迁移）。要改 PG 的 schema，只能追加新文件。
2. MySQL 有自己的 baseline 和增量迁移，**作为普通文件手写、人审、提交**。**不在构建或运行时从 PG 迁移自动生成**。可以一次性用 `scripts/pg2mysql_*.py` 起草，之后文件头注明“起草于何时、由哪个脚本、已人工审查”，从此只人工维护。
3. **每个 schema 只有一个迁移执行者**。执行者在启动时持库级互斥锁（`GET_LOCK`，锁名由数据库名的 SHA-256 得出，沿用 `ownermigration.WithMySQLSchemaLock` 的合同），其他读写同一 schema 的服务只做**只读核对**（版本不匹配就拒绝启动，不会偷偷迁移）。

### 6.2 目录约定

| 位置 | PG | MySQL |
| --- | --- | --- |
| Encore 服务 `<svc>` | `encore-api/<svc>/migrations/NNNN_name.up.sql`（Encore 执行，**不改**） | `encore-api/<svc>/migrations-mysql/NNNN_name.up.sql`（`//go:embed migrations-mysql/*.sql`） |
| 平台 | `sql/N.sql` 及 `sql/<component>/`（现状不变） | `sql/mysql/NNN_name.sql`（现有 001–014，保留） |
| 对照清单 | — | `encore-api/<svc>/migrations-mysql/PG-MAP.txt`：每行 `pg:<版本> -> mysql:<版本>` 或 `pg:<版本> -> pg-only(<原因>)` |

**为什么 MySQL 目录不能放进 `migrations/`**：Encore 会把该目录下所有 `N_name.up.sql` 当 PG 迁移执行。目录名里的连字符对 `go:embed` 没有问题。

文件规则：`NNNN_name.up.sql` 里用一行 `-- @@` 分隔多条语句（沿用 d1 `mysqlowner.Split` 的约定，因为 MySQL 驱动默认不允许一次执行多条语句，且触发器体里有分号）；每个文件整体幂等或由账本保证只执行一次；文件头必须有 `-- pg: 0034_xxx.up.sql` 或 `-- pg: baseline-through 0033` 注释；**禁止**在迁移里引用 `DATABASE()` 之外的库名。

### 6.3 账本与“迁移历史映射”

- PG：Encore 自己的账本（每个资源库里的 `schema_migrations`）。保持不变，所以资源身份和迁移历史“原样保留”。
- MySQL：每个 MySQL 库一张 `tp_schema_migrations(resource, version, name, sha256, applied_at)`，主键 `(resource, version)`；执行器在互斥锁内按版本顺序执行未登记的文件，**文件内容的 SHA-256 与已登记的不一致就拒绝启动**（等价于“已执行的迁移一字不改”）。平台现有的 `initialize` 账本（`mysql-platform-base`、`platformconfig-dictionary` 等模块名，被 `platformconfig` 反向读取）**保持原样**，T1-A1 决定是否合并，默认不动，避免触碰已有历史。
- 版本对照：`PG-MAP.txt` 记录 PG 的每个迁移在 MySQL 侧对应哪个文件。**T1-D 的检查**：每个 PG 迁移文件必须在 `PG-MAP.txt` 里有一行；新增 PG 迁移而没有 MySQL 对应项或 `pg-only` 说明，检查失败。这就是“迁移历史映射”的落地。

### 6.4 谁是执行者

| Schema | PG 执行者 | MySQL 执行者 | 只读核对者 |
| --- | --- | --- | --- |
| 平台库（`platform`） | 现状不变 | 平台进程 `initialize` | `platformconfig`、`authcore`、`devicemetadata/legacydb`（Encore 进程） |
| `device_metadata` | Encore | `devicemetadata` 服务 `initService` | `thingmodel` |
| `automation`、`northbound`、`file_storage`、`notification`、`connector_runtime`、`market_install` | Encore | 各自拥有它的服务 | — |

两个服务**共用同一个 MySQL 库**时（例如全部放进一个库），互斥锁按库名，不会冲突，账本按 `resource` 区分。

## 7. 测试策略

- **单元测试**：`pkg/relational` 自带（配置解析、DSN 生成、错误分类、登记表所有权）。
- **真实库测试**：`pkg/relational/integration_test.go`，需要 `scripts/dev-databases.sh up` 与 `eval "$(scripts/dev-databases.sh env)"`，没有环境变量时跳过并写明原因。
- **差分测试**：T1-A0 交付 `pkg/relational/reltest`：同一场景函数在 PG 和 MySQL 上各跑一遍，记录每一步的返回值和关键表状态，逐步比较，不一致即失败，并输出报告。它取代 d1 的 `dbxtest`（后者依赖 `dbx.DB`）。d1 的场景（devicemetadata 存储 19、分组 35 …）按卡片逐个移植成 `reltest` 场景。
- **诚实原则**（`CLAUDE.md` 第 9 条）：没有真实库的环境，差分测试必须 `t.Skip` 并写明原因；单元测试通过不等于差分通过，更不等于端到端通过。

## 8. 禁止项与防回归（交给 T1-D 实现）

仓库里不得出现：对 `pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx`、`pkg/dbdialect`（其 `InsertIfAbsent` 迁入各 repository）的导入；`sql.Register` 包装驱动（`sqldialect.OpenMySQL` 同类）；对 SQL 字符串做 `strings.Replace`/正则改写后再执行的代码；业务包直接读 `relational.*` 配置键或 `TP_RELATIONAL_*` 环境变量；`encore-api/*/mysqlschema/` 目录（改成 `migrations-mysql/`）。检查形式：一个 Go 测试（扫描源码 import 与字符串）加一个 CI 脚本；规则细节在 T1-D 卡里。

## 9. 需要总监决定的事项

| 编号 | 问题 | 我的建议 | 不决定的后果 |
| --- | --- | --- | --- |
| **O1** | 配置键 `db.driver / db.psql.* / db.mysql.*` 与 `TP_*_DB_DRIVER` 是否保留一个版本的读取兼容？ | **不保留**，同一发布内改 `configs/conf*.yml` 和安装器模板（T8）。MySQL 档位尚未发布，PG 档位的键只是改名 | T1-A1 不能开工（要先定键） |
| **O2** | 若 `encore build docker` 不接受 `../pkg/relational` 的 `replace`：把模块物理位置挪进 `encore-api/pkg/relational/`，还是别的办法？ | 先在 T1-A0 复验；若失败，选“挪进 `encore-api/pkg/relational/`”（路径变一下，仍是单一源码、单一模块，两个 go.mod 各改一行 `replace`） | T1-A0 的验收不通过 |
| **O3** | 咨询锁（8+ 处）的“PG 咨询锁 / MySQL 锁行”双实现，是每个模块各写一份，还是收进 `pkg/relational` 作为 `relational.LockTx(ctx, tx, key)`？ | **收进去**：语义在所有模块完全一致，8 份拷贝比一个 40 行的公共函数更危险；它不是“万能 SQL 接口”，只有一个动作。需要你明确这一例外 | 默认每个模块自己写，会出现多份拷贝 |
| **O4** | 平台现有 MySQL 账本是否并入 `tp_schema_migrations`？ | **不并入**（保留历史，`platformconfig` 还在读它） | 无，默认不动 |
| **O5** | 一个部署是否允许混用引擎？ | **不允许**（一个 `relational.driver`） | 无 |

## 10. 交付清单（T1-0）

- `docs/RELATIONAL-INVENTORY.md`、本文、`docs/T1-CARDS.md`、`docs/tasks/T1-0-DONE.md`
- `pkg/relational/`：`dialect.go config.go open.go registry.go errors.go go.mod go.sum` 与 5 个测试文件。**不含任何 SQL 文本改写**。
- 根 `go.mod` 与 `encore-api/go.mod` **没有改**（`require`/`replace` 在 T1-A0 里加，避免 T1-0 触碰业务模块的依赖）。
