# 数据库兼容第一阶段完成说明

分支：`develop`（本地，未推送）  基线：`origin/develop@c0ab14d1`  当前提交：`2c03050b`

## 改了什么

- 合入 T1-A2（PG/MySQL DAL、repo、service）和 T1-A3（MySQL 遥测与存储），并合入 T3（TDengine 可选历史库，不再需要 experimental 开关）。
- `internal/dal/device_groups.go`、`internal/dal/r_group_device.go`：用 GORM 当前方言引用 `groups` 表，避免 MySQL 8 保留字语法错误；覆盖设备分组路径、层级、子分组及分组设备计数查询。
- `internal/dal/device_groups_mysql_reserved_test.go`：增加真实 MySQL 回归测试。
- `docs/DATABASE-SWITCH.md`：记录关系库和历史库选择方式；`docs/TODO.md`：保留 API 基线测试失败及安装器遗留变量。

## 验证

- MySQL 8.4：`TestDeviceGroupRawQueriesQuoteReservedTableOnMySQL` PASS；`scripts/smoke_mvp1.sh` 登录、租户信息、分组、设备 CRUD、设备列表、看板计数和趋势均通过。设备列表的 `groups` 语法错误已消失。
- PostgreSQL 16：`scripts/smoke_mvp1.sh` 相同核心步骤通过。临时 fixture 缺旧迁移表时，按 `sql/1.sql` 的定义补建 `telemetry_current_datas` 后复跑通过；数据库初始化也按 `global.VERSION_NUMBER=25` 补录了临时 `sys_version`，未改迁移文件。
- `go test -count=1 ./internal/app/...` PASS；TDengine 定向测试 5 PASS、3 SKIP（均需 broker 集成测试 DSN）。
- `go test -count=1 ./internal/service` PASS；`go test -count=1 ./internal/repo` PASS。
- `go test ./... -run '^$'` 根 Go 模块编译 PASS。
- `go test -count=1 ./internal/api`：1 个既有失败 `TestTelemetryHTTPDeviceAuthorization/owner_key`；API 文件与 `origin/develop` 相同，失败已登记 TODO，非本阶段数据库变更。
- SQL 依赖禁用引用检查新增行为 0 处；PG 已执行迁移文件差分为空；`git diff --check` PASS。
- CSV 下载修复将 Blob URL 回收延后 1 秒并把隐藏链接挂入页面。浏览器下载事件收到 `telemetry_data.csv`，本机文件内容可读；前端 `pnpm build` PASS，类型检查通过。

## 未完成 / 保留事项

- `smoke_mvp1.sh` 把菜单 404 和分组关系缺少 `GroupId` 标为提示项；没有把它们算成数据库核心烟测失败，也未在本范围内扩展修复。
- 浏览器下载事件与本地 Blob 流程验证成功；由于临时完整栈登录态过期，未在完整 TDengine 页面中重做组件级点击验收。修复代码已完成构建。
- 不推送远端。临时验收容器、数据卷及相关 worktree 均保留，未删除。
