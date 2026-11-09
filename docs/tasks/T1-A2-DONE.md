# T1-A2 完成说明

分支：`wp/t1-a2-platform-dal-service`  基线：`develop@c0ab14d1`  提交：见本文件提交

## 改了什么

- `internal/dal`：把本卡平台 DAL 的关系库访问迁到 GORM，并按 PG/MySQL 处理需双实现语义；增加差分测试覆盖队列双 worker 领取、幂等插入、菜单 JSON、设备趋势等场景。
- `internal/repo`：完成市场授权、原生模型导入等仓储的双库处理；授权读回和封存以 `id + plan_digest` 定位，再核对完整身份与精确计划，避开 JSON 列与文本参数的跨库比较差异。
- `internal/service/native_automation_command.go`：保留 PG/MySQL 都可用的自动化命令 upsert。
- `internal/automationguard`：用 GORM 实现守卫并加入双库并发差分覆盖。
- `pkg/dbdialect`：删除已迁入 DAL 的 `InsertIfAbsent` 实现。
- `docs/WORKSPACES.md`：登记本任务 worktree。
- `docs/tasks/T1-A2-DONE.md`：记录范围、验证和未覆盖内容。

## 验证

- `go build ./...`：通过。
- `go test -count=1 -v ./internal/dal/ ./internal/repo/ ./internal/automationguard/ -run 'MySQL|Postgres'`：16 个命名差分场景通过；首次运行授权计划测试失败，修正后该场景单独复跑通过。无 SKIP。
- `go test -count=1 ./internal/dal/... ./internal/repo/... ./internal/service/... ./internal/automationguard/...`：通过。
- `grep -rln --include='*.go' 'pkg/dbdialect' internal pkg | grep -v '^pkg/dbdialect'`：无输出。
- `git diff develop -U0 -- '*.go' | grep '^+' | grep -c -E 'pkg/(sqldialect|dbx|dbdialect)'`：0。
- `git diff develop --stat`：改动限定在 T1-A2 范围和 worktree 登记、交付说明。
- PG 已执行迁移差分（`sql/[0-9]*.sql`、`sql/broker/*.sql`、`sql/service_access/*.sql`、`encore-api/**/migrations/*.sql`）：空。

测试使用 `scripts/dev-databases.sh` 创建的本地临时 PG/MySQL 容器；按环境说明确认此前无同名容器及端口冲突。没有 SKIP 项。

## 没做 / 发现的问题

- T1-A2 清单中的队列领取、自动化 upsert、`InsertIfAbsent` 和 board 趋势均已有双库实现与差分覆盖；DO NOTHING 行数由 `TestInsertIfAbsentPostgresMySQL` 覆盖，无需从旧分支搬运测试。
- T1-A3 遥测、TDengine 转正、PG/MySQL 平台冒烟不属于本卡，尚未完成。
