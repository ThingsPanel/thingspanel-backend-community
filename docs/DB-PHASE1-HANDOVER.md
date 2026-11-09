# 数据库第一阶段 移交说明

写给接手测试的同事。先看“一句话状态”和“怎么测”，其余需要时再查。

## 一句话状态
关系库 MySQL 兼容（T1-A2 / T1-A3）和 TDengine 转正（T3）已经做完并合入后端 `develop`，**已推送**到 `origin/develop`（`82ad259e`）。默认仍是 PostgreSQL，行为不变。前端 CSV 下载修复已合入**本地**前端 `develop`（`47cd7e86`），**还没推送**。

## 做了什么
| 内容 | 状态 |
|---|---|
| 平台可用 MySQL 8 作关系库（`relational.driver: mysql`），仍只用 GORM，特殊语义分别写 PG / MySQL 两份 | 完成 |
| MySQL 遥测与存储（批量写冲突、最新值、清理） | 完成 |
| TDengine 作为可选历史库，去掉“实验”开关（`storage.history_backend: tdengine`） | 完成 |
| MySQL 8 把 `groups` 当保留字导致设备列表失败 | 已修复，有回归测试 |
| 前端遥测 CSV 下载被浏览器取消 | 已修复（延后 1 秒再释放下载地址） |

## 验证到什么程度
- 根模块编译通过；`internal/dal / repo / service / app / storage / automationguard` 测试通过。
- 用真实 PG 16 和 MySQL 8.4 重跑双库测试：30 PASS、37 SKIP、0 FAIL；名称带 MySQL 的测试全部通过、无跳过。
- 两个库上的冒烟（`scripts/smoke_mvp1.sh`：登录、分组、设备增改查删、设备列表、看板计数和趋势）通过。
- 前端 CSV：浏览器收到下载事件、文件已落盘，前端构建通过。
- **没做到的**：完整 TDengine 页面上点“导出”按钮的实际验收（登录态过期）；前端合入 `develop` 后没有重新构建。
- 37 个 SKIP 都是只针对 PG 的旧专项测试，缺专用 DSN 或 Redis，不是这次改动引起的。
- 证据（不在仓库）：`/Volumes/KIOXIA/archive/evidence/2026-11-09-DB-PHASE1-fix/`。完整说明见 `docs/tasks/DB-PHASE1-DONE.md`。

## 怎么测
1. **拿代码**：后端 `origin/develop`；前端在本机 `thingspanel-frontend-community` 的 `develop`，需要先推送才能给别人。
2. **构建后端镜像**：仓库根目录 `docker build -t thingspanel-backend:dbp1-82ad259e .`（约 5 分钟）。
3. **MySQL 方案**：配置示例见 `docs/tasks/T1-A1-DONE.md` 的“最小 MySQL 配置示例”；环境变量前缀 `GOTP_`，如 `GOTP_RELATIONAL_DRIVER=mysql`。平台启动还需要 Redis 和 MQTT。**不要用** `mqtt.input_profile=ingress_v1`（只支持 PG）。
4. **TDengine 方案**：见 `docs/DATABASE-SWITCH.md`。需要配 `storage.history_backend`、`storage.tdengine.url/user/password/database`。TDengine 连不上会直接启动失败，不会悄悄退回 PG。
5. **冒烟**：`scripts/smoke_mvp1.sh <平台地址> <邮箱> <密码>`。
6. **测试账号**：种子租户账号 `tenant@tenant.cn` 没人知道密码。在你自己起的临时库里用 bcrypt 重置，步骤见分支 `wp/db-handover` 的 `docs/DB-PHASE1-DISPATCH-PROMPT.md` 里的“卡点 B3-3”。只能改自己起的库，密码不要写进仓库。
7. **跑双库测试**要设环境变量，否则会全部跳过：`TP_MYSQL_TEST_HOST/PORT/USER/PASSWORD/DB`，以及 `TEST_PG_SCHEMA_DSN`、`TEST_DBDIALECT_PG_DSN`、`TEST_AUTOMATION_GUARD_DSN`、`NOTIFICATION_TEST_DSN`。

## 已知问题（报 bug 前先看）
- `TestTelemetryHTTPDeviceAuthorization/owner_key` 失败：在基线 `c0ab14d1` 上同样失败，是原有问题。
- 新建设备后要先执行 `--sync-scope`，否则平台命令会被 Broker 拒绝。
- 冒烟脚本里的“菜单 404”“分组关系缺 `GroupId`”是已知提示项，不算失败。
- 技术债（已记入 `docs/TODO.md`）：MySQL 分支散落在函数里，不符合 A2 的“各写一份显式实现”；`ON DUPLICATE KEY UPDATE ... VALUES(col)` 在 MySQL 8.0.20 后不推荐。
- 安装器 compose 仍会传 `GOTP_STORAGE_TDENGINE_EXPERIMENTAL`，后端已不读，无害，以后清理。

## 还需要负责人做的事
1. 推送前端 `develop`（本地 `47cd7e86`，只比 `b9643a2a` 多这一个 CSV 提交），并重新构建前端确认通过。
2. 用 `origin/develop` 重新构建镜像，起一套同事能访问的测试环境。
3. 确认删除遗留的 `dbp1-tdengine-*` 验收栈、6 个数据卷、网络和 `/Volumes/KIOXIA/tmp/dbp1-tdengine-stack`（含本地凭据）。见 `docs/WORKSPACES.md` 末尾。
4. 不进 `main`：等 T8（安装器单一发行版）做完一起发版。
5. 分支 `wp/db-handover`（仅交接文档，已推送）合不合都行。

## 规则提醒
同时最多 2 条改代码的工作线；范围外的问题写进 `docs/TODO.md`，不要另开分支修；跳过的测试要写明原因，不能把单元测试通过说成端到端验收通过。
