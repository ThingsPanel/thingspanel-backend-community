# T1-A0 公共接线 完成说明

分支：`claude/wp-t1-a0-wiring-42b3c3`（基于 T1-0 提交 `50f3e13d`，交付前已 rebase 到最新 develop）。未推送、未合并。

`git log --oneline develop..HEAD`：

```
T1-A0: relational wiring (migrate, LockTx, reltest, relationaldb, go.mod replace)
```

## 做了什么
- `go.mod`、`encore-api/go.mod`（+`go.sum`）：各加一行 `require` 和一行 `replace`（根模块 `./pkg/relational`，encore-api `../pkg/relational`）。**没有用复制**。
- `pkg/relational/migrate.go`：MySQL 迁移执行器 `Migrate` / `Verify` / `LoadMigrations`，账本 `tp_schema_migrations(resource, version, name, sha256, applied_at)`，库级互斥锁，SHA-256 校验，`-- @@` 分隔语句。PG 上调用返回 `ErrUnsupportedDialect`。
- `pkg/relational/schema_lock.go`：库级锁合同从 `encore-api/pkg/ownermigration/mysql_lock.go` **移入**（`SchemaLockName`、`WithSchemaLock`、`ErrBusy`、`ErrOutcomeUnknown`）；旧文件只剩转发，D1 删除。
- `pkg/relational/lock.go`：`LockTx`（O3 例外，只此一个动作）。PG `pg_advisory_xact_lock(hashtextextended)`；MySQL 在 `tp_advisory_locks` 上 `INSERT … ON DUPLICATE KEY UPDATE` 取行排他锁。多键去重排序；不在事务内返回 `ErrNotInTransaction`。`AdvisoryLocksDDL` 常量供各库 baseline 迁移使用。
- `pkg/relational/registry.go`：新增 `CloseOne`。
- `pkg/relational/reltest/`：差分框架（每个场景在 PG 与 MySQL 各建临时库，逐步比较返回值与错误类别 `Classify`，输出报告；缺任一引擎则 `t.Skip` 并写原因）。
- `encore-api/pkg/relationaldb/`：`Open(resource, *sqldb.Database)`、`Get`、`Close`，包级一次性解析 `relational.driver`；MySQL 模式不触碰 Encore 的 `sqldb` 声明。

## 设计设想被实测推翻的一处
设计文档 4.4 的范例（`INSERT IGNORE` + `SELECT … FOR UPDATE`）在并发下会死锁：40 轮×2 个事务反向取 3 把锁，MySQL 80 个里 72 个报 1213。原因是等待者先拿重复键的共享锁再一起升级。改为单条 `INSERT … ON DUPLICATE KEY UPDATE lock_key = lock_key` 后 PG、MySQL 均为 0 失败。**设计文档 4.4 的范例代码需据此更正**（本卡未改文档）。

## 测试（自起容器 `t1a0-pg`=postgres:16、`t1a0-mysql`=mysql:8.4，用完已 `docker rm -fv`）
- `cd pkg/relational && go test -count=1 ./...`：全部通过（含原有 T1-0 测试、真实库分类测试）。
  - `TestLockTxDifferential`：8 步 PG/MySQL 逐步一致（事务外拒绝、空键拒绝、持有时阻塞/提交后放行、回滚释放、异键互不阻塞、同键重入、反向顺序 40 轮不死锁）。
  - migrate：并发两个执行者只有一个执行、另一个 `ErrBusy`；已登记文件被改动 → `ErrChecksumMismatch` 且不执行；`Verify` 版本落后 → `ErrBehind` 且只读；幂等重跑；乱序文件被拒；PG 拒绝。
- `cd encore-api && go test ./pkg/relationaldb/`：通过（MySQL 模式不请求 PG 池、重复 Open 报错、Close 幂等；PG 模式借用连接池且 Close 不关闭它）。
- `encore test ./pkg/relationaldb/...`（在 encore-api 内）：`ok encore.app/pkg/relationaldb`。
- `encore-api` 与根模块 `go build ./...`：根模块通过；encore-api 仅有 `fileassets/recovery.go: undefined: RecoverInterruptedUploads`，该文件和符号不在本卡改动内，**本卡之前就存在**（未在 develop 上另行复现，仅据"文件未改、符号全库不存在"判断）。

## 两项实测
1. **`encore build docker` 与 `../pkg/relational` 的 replace**：用最小 Encore 应用（目录布局同 `app/` + `pkg/relational`，`replace … => ../pkg/relational`，含 `sqldb` 声明）测试。
   - 编译阶段**通过**（两次 `compiling Encore application` 成功；首次 13 分钟，主要是 Encore 内部 `go mod tidy` 与冷缓存，没有空转）。**O2 不需要启用后备方案。**
   - 默认 `scratch` 底座的镜像打包失败，原因是拉取 `https://curl.se/ca/cacert.pem` TLS 握手超时（网络问题，与 replace 无关）。改用 `--base postgres:16 --arch arm64` 后镜像**成功导出**。
   - 注意：必须传 `--config`（infra 配置），否则 `Missing Resource Configurations: Databases` 失败。
   - 本机另一个坑：Go 缓存放在外接盘 `/Volumes/80G/.gocache` 时读盘极慢，多个会话的 `link` 进程长时间卡住（表现为 `go test` 无输出）；用 `/Volumes/500G/.gocache` 正常。
2. **惰性连接假设：成立。** 镜像里的探针服务声明了 `sqldb.NewDatabase("probe")`，infra 配置中 PG 主机指向不可达的 `10.255.255.1:5432`，环境 `TP_RELATIONAL_DRIVER=mysql` + MySQL 连接。服务正常启动、监听，`POST /probe` 两次均经 MySQL 完成写入与读回（返回 `Dialect: mysql`，计数 1→2），MySQL 里有 `probe_items` 两行和 `tp_schema_migrations` 一行（由 `relational.Migrate` 执行）。探针源码放在共享笔记目录 `t1a0-probe/`，不在仓库内。

## 没做 / 注意
- 业务模块、翻译器、`copy_in_sync_test.go` 均未动（D1 删除）；`ownermigration` 保留转发层，marketinstall 仍通过它调用。
- 未做 Windows、PG 模式下的 Encore 镜像运行实测（只测了 MySQL 模式运行；PG 模式只有单元/借池测试）。
- 未更正设计文档 4.4（见上）；需在 A2 落地前改。
- 根模块 `go.mod` 的 `require` 目前没有被根模块代码引用，不要在 A1 前对根模块执行 `go mod tidy`，否则会被删掉。
