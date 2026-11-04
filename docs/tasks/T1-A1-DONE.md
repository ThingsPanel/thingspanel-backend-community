# T1-A1 完成记录

日期：2026-11-04
分支：`wp/t1-a1-platform-start`
精确基线：`develop@85b04e7b339cc0b7d4ff78ba6ec5e7de1640e792`
提交列表：

- `cc247c39` `feat: route platform startup through relational adapter`（rebase 后的 A1 实现）
- `29cd73c2` `fix: fail MySQL startup when Casbin initialization fails`（恢复 MySQL 鉴权失败关闭与回归）
状态：已 rebase 最新 develop 并提交，等待负责人审查；未推送、未合并。

## 1. 修改方案与原因

平台启动之前由独立 PostgreSQL/MySQL 路径选库，MySQL 另有翻译驱动和手工迁移账本。现在 `initialize.RelationalInit` 只通过 `relational.driver` 解析平台资源，再由 `relational.Open` 建立连接池；这与 A0 的连接和迁移底座一致，避免选库、池生命周期和 MySQL DDL 再维护第二套机制。

MySQL 已有数据库升级时，不能把已执行 SQL 再跑一遍，也不能仅因表存在就伪造迁移记录。启动先读取旧账本，按原 module/file 对应到唯一当前迁移；验证旧 checksum（保留旧代码对 LF/CRLF 的兼容语义）后，将其导入关系账本的规范 checksum。缺项、重名/版本冲突、映射不唯一、未知迁移或摘要不匹配都会失败关闭。旧账本仍以原 module/file 名和已验证迁移状态保留，`platformconfig-dictionary` 名称不变。新旧执行器由各自锁串行化；A0 关系锁不重入，导入释放锁后才调用 Migrate。旧锁获取/释放结果不确定时丢弃物理连接并报 `ErrOutcomeUnknown`，防止持锁连接回池。

## 2. 模块变化与验证

| 模块 | 修改前 | 修改后 / 场景 | 验证 |
| --- | --- | --- | --- |
| 平台数据库启动 | `db.psql.*`、`db.mysql.*`、MySQL 翻译驱动各自打开连接 | 只读 `relational.driver` 和对应 `relational.<engine>.platform.*`；PG 保留重连窗口、版本检查和迁移顺序；MySQL 接入 `relational.Migrate` | MySQL 8.4 tmpfs 实例真实启动、导入兼容账本、核对缺失 trigger 时拒绝启动；PG 实例真实调用 `RelationalInit` 并确认关闭后 pool Ping 失败 |
| 连接关闭 | 平台 pool 没有统一的应用 shutdown 所有权 | `Application.Shutdown` 关闭平台 `Registry`，成功后清空匹配的 `global.DB`；MySQL Casbin 初始化失败会让启动失败并走同一 registry 清理路径 | `TestRelationalInitPostgresOwnsConfiguredPool`；MySQL startup 用例注入 Casbin 错误，检查 global DB、registry 和 pool 均清理 |
| broker 控制面 | MySQL 配置可能绕过默认 PG 假设 | 仅 `mqtt.input_profile=ingress_v1` 的 broker ingress 和 durable command worker 检查必须为 PostgreSQL；默认空 profile 与 `legacy` 不启动 PG 专属 worker，普通 MQTT 不被该守卫误禁 | broker 入口方言拒绝测试通过；legacy/default 两种入口测试通过。独立 `cmd/tp-broker-owner` 继续使用自己的 `TP_BROKER_OWNER_DATABASE_DSN` PG DSN，不受平台 driver 影响 |
| SQL 日志 | PG 使用 `GetSQLLogWriter`，输出遵循文件/控制台配置 | RelationalInit 在共享 opener 上恢复该 writer、log level、慢查询阈值和 NotFound 忽略选项 | 定向包测试与编译通过；日志目标由代码路径确认 |
| MySQL schema owner / O6 | 容易把表名相似误认为平台拥有完整 automation schema | 实际调用链 `internal/automationguard` 只读写 `automation_scope_generation`、`automation_command_guard`，其 DDL 在 `012_platform_base.sql`。`010/011` 的 `auto_rules` 等由 Encore automation service 使用，平台不调用；故移除 `mysql-automation` 平台 owner，保留 guard 表于 platform-base | ownership 测试检查真实平台调用范围和 migration 文件 |
| 示例配置 | 平台连接字段在 `db.psql.*` | 示例文件改为 `relational.postgres.platform.*`，旧键不再 fallback；Redis 配置不变 | `go test ./initialize/... ./internal/app/...` 全通过；读取器/旧键回归测试通过 |

### 最小 MySQL 配置示例（不含凭据）

```yaml
relational:
  driver: mysql
  mysql:
    platform:
      host: 127.0.0.1
      port: 3306
      database: thingspanel
      username: thingspanel_app
      password: "" # 从密钥管理注入；平台 Viper 环境键为 GOTP_RELATIONAL_MYSQL_PLATFORM_PASSWORD
```

平台进程环境变量使用 Viper 的 `GOTP_` 前缀，例如 `GOTP_RELATIONAL_DRIVER=mysql`。这是明确的配置契约变更：部署须把旧 `db.psql.*`/`db.mysql.*` 和相关环境变量迁移到新键；旧键不再读取。

## 3. 验证结果和限制

- `go test ./initialize/... ./internal/app/...`：通过。
- `(cd pkg/relational && go test -count=1 ./...)`：通过。
- 独占 MySQL 8.4 容器（tmpfs，无宿主卷）：`TestRelationalInitMySQLImportsOldReceiptsAndDoesNotReplayAppliedDDL` 通过；该用例还注入 MySQL Casbin 初始化错误，确认 startup 失败、global DB 清空、Registry 移除 pool、pool 已关闭，再正常重试成功。MySQL runner 并发启动/回执导入、checksum drift/缺失 schema、旧锁互斥三组通过。实例没有 `CREATE TEMPORARY TABLES` 权限。首次空库迁移在 `log_bin_trust_function_creators=0` 时按守卫拒绝（MySQL 1419），仅对这个临时实例设为 1 后完整启动通过。没有复用或清理其他容器/数据。
- 独占 PostgreSQL 16 容器（tmpfs，无宿主卷）：`RelationalInit` 实际启动/关闭生命周期通过；DeviceIdentity、HistoryIntent、MarketInstall、ServiceAccess 四组真实 PG schema 测试通过。
- PG startup fixture 是局部测试基线：基于 `sql/1.sql` 并跳过需要 Timescale 扩展的 `create_hypertable`，应用 `sql/19.sql` 的市场旧表，并将 `sys_version` 预置为 25，以隔离验证 A1 平台启动路径。它不代表完整生产 schema、Timescale 验证或从历史版本完整升级。
- 完整相关包测试因没有配置的外部 PostgreSQL/业务测试 DSN，未运行依赖生产型 baseline 的额外 DB 场景（如 broker 全栈 E2E、MySQL/PG 差分基线）。普通包测试仍通过；上列真实 DB 用例覆盖 A1 连接、启动迁移、升级回执和关闭生命周期。
- 首次 Go test 链接在本机约 30–90 秒；观察到的等待是编译/链接，不是 schema lock 阻塞。真实迁移用例均在 0.1–2 秒完成。

## 4. 配置边界、未完成项与影响

- 本卡完成平台进程配置契约，但 Encore 的 `relational.EnvSource()` 使用 `TP_RELATIONAL_*`，平台 Viper 使用 `GOTP_RELATIONAL_*`。A1 没有修改 Encore 进程配置；要让一份部署配置切换 driver，发布/部署协调仍须给两类进程设置一致值，后续卡应统一环境变量映射并覆盖完整启动。
- `encore-api/devicemetadata` 的旧 `TP_DEVICE_METADATA_DB_DRIVER` 读取仍存在，属于 T1-B 卡，不在 A1 改动范围；此处不声称全仓配置已统一。
- 独立 broker owner 仍固定 PostgreSQL 并读取自身 DSN，是独立进程/数据库所有者；平台配置里的 MySQL 不能被视为它的 MySQL 支持。
- 旧 MySQL ledger 会保留可兼容的 module/file 证据，规范化摘要导入关系账本；无法映射或校验失败时不会声称升级成功。MySQL 真实验证覆盖隔离新实例中旧回执状态的导入与“不重放”的保护行为，不覆盖现场业务库备份/回滚演练。

## 5. 工作现场与证据

- Worktree：`.claude/worktrees/wp-t1-a1-platform-start`
- 分支基于 `develop@85b04e7b339cc0b7d4ff78ba6ec5e7de1640e792`；目录已于 2026-11-04 登记到 `docs/WORKSPACES.md`。Rebase 无冲突，保留了 develop 上的 P2 和进度更新，并保留本分支 A1 worktree 登记。
- 原工作区、共享 PG/P2 容器和其他 worktree 未修改；测试实例为 `t1a1-mysql-20261104`、`t1a1-pg-20261104`，均为本卡独占 tmpfs 实例。未删除容器或卷，等待负责人审查决定清理。
- 命令与摘要证据（不含凭据）存放在 `/Volumes/500G/archive/evidence/2026-11-04-T1-A1/`。
