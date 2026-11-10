# MariaDB 可选关系库支持

## 结论

平台根模块可用 MariaDB 10.11 LTS 和 11.4 LTS 替换 MySQL 8 关系库。配置仍使用 `relational.driver: mysql`，再设 `relational.mysql.flavor: mariadb`；未设置 flavor 时维持 MySQL 行为。默认发行版仍为 PostgreSQL + TimescaleDB。

采用独立 flavor，而不是新建 `mariadb` driver：MariaDB 使用 MySQL wire protocol 和 GORM MySQL dialector，共用大部分驱动、连接池与迁移；只有迁移语法及少数 repository SQL 语义需要区分。代价是需维护少量 `sql/mariadb/` 覆盖和 flavor 专属实现。

## 实现内容

- `pkg/relational` 解析、验证 flavor；连接时校验服务端 flavor，MariaDB 仅接受 10.11.x、11.4.x，且 GORM 仍看到 `mysql` dialect。
- `sql/mysql/` 保持不变；为 001、002、004、007、011、012 增加 `sql/mariadb/` 覆盖。其余 003、005、008–010、013–018 共用 MySQL-family SQL。
- MariaDB 使用 `LOCK IN SHARE MODE` 替代不支持的 `FOR SHARE`；device trend 使用 MariaDB 的 `max_recursive_iterations` 查询级设置，以及两参数 `LAG`。这些差异留在 relational dialector 或所属 repository。
- `device_topic_mappings` 的长组合唯一键在 MariaDB 使用由 repository 维护的 SHA-256 列；MySQL 继续使用已有生成列。
- 更新关系库配置文档、迁移测试夹具与 flavor 差异测试。PG 已执行迁移没有修改，也没有运行时改写 SQL。

## 验证结果

| 验收项 | MariaDB 10.11.19 | MariaDB 11.4.13 | MySQL 8.4 |
|---|---:|---:|---:|
| `initialize` 实库迁移与初始化测试 | PASS | PASS | PASS |
| `internal/dal`, `repo`, `service`, `app`, `storage`, `automationguard` | PASS | PASS | PASS |
| 名称含 `PostgresMySQL` 的 PG16 双库对照 | PASS，0 SKIP | PASS，0 SKIP | PASS，0 SKIP |
| `scripts/smoke_mvp1.sh` 正式断言步骤 | PASS | PASS | 未作为本轮 smoke 目标 |
| `go build ./...` | PASS | 同一代码版本 | 同一代码版本 |
| `pkg/relational` 测试 | PASS | PASS | PASS |

两个 MariaDB smoke 均完成登录、用户详情、分组和设备 CRUD、设备列表、看板计数与趋势。脚本标记为 informational 的菜单请求返回 404，分组关系列表提示缺少 GroupId；这些接口不在脚本的通过断言内。

本轮核心测试使用了隔离的 PostgreSQL 16/TimescaleDB、MySQL 8.4、MariaDB 10.11、MariaDB 11.4 实例。差异测试日志确认 25 个 `PostgresMySQL` 测试事件全部通过，未跳过。依赖 TDengine 外部 URL 的专门 storage 子测试未纳入本轮验收；它们不影响已通过的 package suite。

MariaDB 对照运行还发现并修复了 `LAG(status, 1, 0)` 不兼容，以及 GORM 生成 `FOR SHARE` 的差异。MariaDB JSON 列按 LONGTEXT 别名处理，schema/seed 对照按 JSON 内容规范化。

`encore-api/marketinstall/store` 是另一个独立 MySQL owner，不随平台根模块的 flavor 自动切换。对它的 MariaDB 探测发现 owner schema verifier 拒绝列结构差异；本次保留其 MySQL-only 门禁，并在 `docs/TODO.md` 留项，没有将其计入平台根模块验收。

## 证据与工作现场

证据目录：`/Volumes/KIOXIA/archive/evidence/2026-11-10-MARIADB/phase2/`。目录内含验收摘要、三引擎双库 JSON 报告、两次 smoke 输出和 PostgreSQL fixture 启动记录；初始化与目标包通过结果汇总在 `acceptance-summary.txt`，未保留完整控制台日志。不含测试账号密码。MariaDB smoke 测试账号密码只重置在本轮自建临时数据库中，未写入仓库。

工作分支：`wp/mariadb-driver-flavor`；worktree：`.claude/worktrees/wp-mariadb-driver-flavor`。没有推送或合并。容器与匿名数据卷仍保留，等待负责人查看清单后决定是否清理。
