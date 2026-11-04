# T1-0 交付说明：关系库底座设计 + 翻译器拆除清单

- **分支**：`claude/wp-t1-0-relational-b140f7`（从 `develop` `49b7f278` 切出；任务书建议的 `wp/t1-0-relational` 命名未使用，因为会话已在该分支的 worktree 里，内容相同，需要时可改名）
- **状态**：待总监审定。未推送、未合并。**没有改任何业务模块**，也没有改根 `go.mod` 和 `encore-api/go.mod`。
- **提交**：见 `git log --oneline develop..HEAD`（一个提交）。

## 做了什么

| 交付 | 位置 |
| --- | --- |
| 1. 盘点（按模块：文件数、SQL 语句数、GORM 可直接表达数、需双实现数及原因；develop 与 d1 的依赖点；`sqldb` 声明；自开连接池） | `docs/RELATIONAL-INVENTORY.md` |
| 2. 设计（`relational.driver` 只在一处解析；每个逻辑资源一个池及谁创建谁关闭；Encore 模块换成自管 GORM 连接并保留资源身份和迁移历史；队列领取 / 条件 upsert / 咨询锁三个双实现范例；错误分类；MySQL 迁移目录约定与单一执行者） | `docs/RELATIONAL-ADAPTER-DESIGN.md` |
| 3. 骨架包：配置、`Open`/`Adopt` 返回 `*gorm.DB`、方言枚举、错误分类、连接池登记表，附单元测试和真实库测试。**不含任何 SQL 改写** | `pkg/relational/`（独立 Go 模块） |
| 4. 拆卡：26 张卡，每张 ≤ 3 天，写明文件、d1 验收场景、冲突、能否并行 | `docs/T1-CARDS.md` |
| 5. 本文 | `docs/tasks/T1-0-DONE.md` |

## 关键结论（审定时请先看这几条）

1. **共享方式**：`pkg/relational` 是独立 Go 模块，两个 `go.mod` 各加一行 `require` + 一行 `replace`，只有一份源码。已用 `go list -modfile` 验证两个模块都能解析（不改仓库）。**没验证**：`encore build docker` 对 `../pkg/relational` 的 `replace` 是否接受，留给 T1-A0 实测，后备方案是把模块挪进 `encore-api/pkg/relational/`。
2. **工作量分布**：平台根模块（不含 broker）137 条语句里只有 **15 条**需要双实现，develop 已有两个现成范例（`dbdialect.InsertIfAbsent`、`upsertCurrentMySQL`）；Encore 模块 833 条里 **153 条**需要双实现，其中 devicemetadata 一个包 77 条（`store.go` 一个文件 78 条语句）。所以 T1-B 占大头（18 天）。
3. **devicemetadata 不能并行拆**：它是单个 Go 包、包级句柄，所以 B1–B6 是同一分支上的串行卡，分支内允许新旧句柄并存（合并前清零）。
4. **需要你决定的事**见设计文档第 9 节：O1 配置键是否保留兼容（建议不保留）、O2 Encore 构建失败时的后备、O3 咨询锁是否收进 `pkg/relational`（建议收，这是对“不做万能接口”的一个明确例外，需要你点头）、O4 平台账本是否合并（建议不）、O5 是否允许混用引擎（建议不）。另有 T1-CARDS 第 7 节的 O6（automation 的 MySQL schema 在 `sql/mysql/010,011` 和 d1 的 `automation/mysqlschema` 里是同一份内容，两个执行者会冲突）。
5. **一个必须复验的假设**：设计依赖“Encore 对已声明的 `sqldb` 库是惰性连接”，来源是 d1 的记录，我没有复验。T1-A0 的验收里有专门一项。若不成立，B/C 要等你决定（双发行 / build tag）。

## 怎么验证的

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| 骨架单元测试 | `cd pkg/relational && go test -count=1 ./...` | 通过 |
| 真实库测试 | `scripts/dev-databases.sh up`，`eval "$(scripts/dev-databases.sh env)"`，`go test -count=1 -v ./...` | `TestIntegrationClassifyOnRealServers` 在 **PG 16（TimescaleDB 镜像）和 MySQL 8.4** 上均通过：唯一冲突、无行、300 ms 语句超时、死锁（两事务反向锁两行，恰好一个受害者）分类正确；`UPDATE ... SET n = n` 的 `RowsAffected` 在两库都是 1（`clientFoundRows` 生效） |
| `go vet`、`gofmt -l` | `pkg/relational` 内 | 无输出 |
| 两个模块的依赖解析 | `go list -modfile=<临时 go.mod> -deps <包>`（根模块和 `encore-api`） | 都能解析到 `pkg/relational` 与 `gorm.io/gorm` |
| 测试数据库清理 | `scripts/dev-databases.sh down` | 已执行 |

## 没做 / 没验证

- **没有**把 `pkg/relational` 接进两个 `go.mod`（T1-A0 做），所以根模块和 `encore-api` 的构建、测试**未受影响也未重跑**；只验证了依赖能解析，没有在 `encore-api` 里编译过它（该模块依赖多、本机 Go 缓存在慢盘，且按任务范围不改 `go.mod`）。
- 迁移执行器（`Migrate`/`Verify`）和差分测试框架（`reltest`）、`encore-api/pkg/relationaldb` 只在设计里，未实现（T1-A0）。
- 盘点的语句数来自字符串字面量扫描，**低估真实量**（拼接构造的语句、GORM 链式里的 SQL 片段、没有 `FROM` 的咨询锁调用不计），口径在盘点文档第 0 节。`lock_skip` 一律按“双实现”计，偏保守。
- 没有登记 `docs/WORKSPACES.md`：本 worktree 里没有这个文件（它在总监的规则分支 T0 里，尚未进 develop）。请 T0 合入后补登记：路径 `.claude/worktrees/wp-t1-0-relational-b140f7`，分支 `claude/wp-t1-0-relational-b140f7`，任务 T1-0，预计审定后删除。
- 没有执行 `docs/TODO.md` 记录：范围外问题 → 未发现需要记录的。
- d1 的 29 个提交我只读了与设计相关的文件（`platform_driver.go`、`dbx`、`mysqlowner`、`MYSQL-PROFILE-STATE`、各 `*_mysql_contract_test.go` 的名称），没有逐提交审查。差分测试的步数（19/35/23/…）取自任务书和 d1 状态文档，我没有逐个重数。

## 证据

无大文件入库。盘点脚本是临时的（规则写在盘点文档第 0 节），不入库。
