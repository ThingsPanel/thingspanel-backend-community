# 数据库兼容 — 接手说明（第一阶段：只做两件事）

> 日期：2026-11-09。基线：`develop` @ `c0ab14d1`（已推到远端）。
> **这一阶段只交付两样东西：① TDengine 成为正式可选的时序库；② 平台根模块能在 MySQL 上跑起来。** 两样都已经做了一大半，补完就能用。
> 其余（devicemetadata 等 Encore 服务改 MySQL、删除翻译器）是几十人天的大工程，**这一阶段一律不碰**，见 §2。

---

## 1. 第一阶段清单

两条线可以**同时**做，它们改的文件不重叠（已核对）。预计 6–8 个工作日。

**线 A：TDengine 转正（约 2 天）**
- [ ] **A-1** 在 PG+TDengine 栈上，用前端走一遍设备历史页、导出、统计，记录结果（§4.1）
- [ ] **A-2** 去掉“实验”开关，写一小节配置说明（§4.2）

**线 B：平台根模块上 MySQL（约 5–6 天，串行）**
- [ ] **B-1** 接手做了一半的 T1-A2，补完交付（§4.3，≤3 天）
- [ ] **B-2** T1-A3 平台存储与遥测（§4.4，≤3 天）
- [ ] **B-3** `scripts/smoke_mvp1.sh` 在 PG 和 MySQL 上都通过（§4.5）

**第一阶段完成 = 上面 5 项都打勾**：
- 不设任何“实验”开关就能 `history_backend=tdengine` 启动，前端历史功能在 TDengine 上验证过；
- 平台根模块在 MySQL 上通过冒烟，平台根模块里不再引用翻译器；
- 现有 PG 测试一个不少地通过。

---

## 2. 这一阶段不做（遇到了写进 `docs/TODO.md`，不要顺手做）

| 不做 | 为什么 |
|---|---|
| devicemetadata、thingmodel、notification、automation 等 **Encore 服务**改 MySQL（原卡 B1–B6、C1–C11） | 833 条 SQL、约 40 人天，是下一阶段的事；这一阶段平台根模块先跑通 |
| 删除翻译器（原卡 D1–D3） | 要等所有 Encore 服务改完 |
| TDengine 的六项语义差分表、tptodb 旧分支的处理 | 不影响“能用”，后补 |
| MySQL 档的遥测历史放哪的正式方案 | 这一阶段：MySQL 档的历史就写 MySQL 普通表（开发、小规模用），文档写明即可 |
| broker 控制面支持 MySQL、达梦/Kingbase/PolarDB、数据搬迁、补接口、OTA | 负责人已决定不做 |
| 写长篇设计、重新盘点、重新拆卡 | 已经有了（§9），直接用 |

---

## 3. 为什么这样排

之前做了几天只推进一点点，原因不是代码难，而是：

1. **一开始就摊开整个大工程**：目标是“四种数据库组合 × 10 个模块 × 833 条 SQL × 26 张卡、约 55 人天”，**中途没有一个能单独交付的成果**，要全部做完才有用。
2. **计划写得比代码多**：设计、盘点、拆卡、分析共约 1900 行文档；同期真正交付的只有公共底座和平台启动（约 1500 行代码）。每张小卡还要先重数 SQL、写长篇说明、多轮审查。
3. **方向改了一次**：11-03 用“SQL 翻译器”两天做出了能跑的 MySQL 版本（8000 多行），11-04 因为违反“只用 GORM”的规则整体作废，从头再来。
4. **半路被插队**：11-05 优先级切到默认栈和 HA 插件，T1-A2 做到一半停下，至今 4 天没人碰，现在已落后 develop 64 个提交。
5. **环境反复卡人**：Go 缓存放在慢盘上编译卡死、共享测试库被别的会话删掉、`encore check` 白等 8 分钟。

所以这次的做法：**只做已经接近完成、补完就能用的部分；每件事都有一条能跑的验收命令；不做的事明确写出来。**

---

## 4. 每项怎么做

> 每项都是：**一个分支、一个 worktree、一份简短的完成说明**。分支名照 `CLAUDE.md`：`wp/<卡号>-<短名>`（例如 `wp/t3-tdengine`、`wp/t1-a3-storage`）。开工当天在 `docs/WORKSPACES.md` 登记。不推送、不合并，做完交负责人审。

### 4.1 A-1 在 PG+TDengine 栈上验证（约 1 天）
- **现状**：TDengine 的读写删代码完整，在真实 TDengine 上有差分测试；唯一欠的是“没在完整运行的栈上验证过”。
- **做**：用安装器的 `tdengine` profile（安装器仓库 `docker-compose.yml` 里已有 `tdengine` 服务，默认关闭）起一个 PG+TDengine 栈，设 `storage.history_backend=tdengine`、临时设 `storage.tdengine.experimental=true`。用前端：建设备、上报几条数据，看**设备历史页、导出、统计**三处能否正常显示。
- **完成标准**：三处都正常，截图和日志放证据目录；有问题就记录现象，交负责人，**不要自己扩大范围去修**。

### 4.2 A-2 去掉“实验”开关（约 1 天，A-1 通过后做）
- **只改 3 个文件**：`internal/app/history_backend.go`（第 43 行的拒绝检查）、`internal/app/storage.go`（第 113 行读取开关）、`internal/app/history_backend_test.go`（对应用例）。
- **再写**：`docs/DATABASE-SWITCH.md` 的“时序库”一小节：怎么选 TDengine、要配哪些项。
- **完成标准**：`go test ./internal/app/...` 通过；不设 `experimental` 时 `history_backend=tdengine` 能启动。

### 4.3 B-1 接手 T1-A2（平台 DAL / repo / service，≤3 天）
- **现状**：已做了一半。分支 `wp/t1-a2-platform-dal-service` @ `3b68fe6a`（worktree `.claude/worktrees/wp-t1-a2-platform-dal-service`），改了 21 个文件，**落后 develop 64 个提交**，还有 2 个未提交文件。**不要从头写。**
- **做**：
  1. 先把 2 个未提交文件提交成 `wip:`（`internal/repo/a2_relational_test.go`、`internal/repo/market_install_authorization_repo.go`）。
  2. `git rebase develop`。冲突解决不了就停下问。
  3. 对照 `docs/T1-CARDS.md` 的 T1-A2 一节，补完没做的。要点：`notification_source_outbox.go` 的队列领取、`native_automation_command.go` 的 upsert、`board.go`。
  4. 测试：develop 已有的 4 个 MySQL 测试（`internal/dal/` 下 `device_identity_mysql_test.go`、`service_access_operation_mysql_test.go`、`ui_menu_mysql_test.go`、`board_trend_mysql_test.go`）跑通；从 d1 分支搬 2 个（`notification_source_outbox_mysql_test.go`、`do_nothing_rows_mysql_test.go`，取法见 §5）。
- **完成标准**：`grep -rn 'pkg/dbdialect' --include='*.go' internal pkg` 为空；上面 6 个测试在两个库上都是 PASS（不是 SKIP）；`go test ./internal/dal/... ./internal/repo/... ./internal/service/...` 通过。
- **不碰**：`internal/dal/telemetry_*.go`（B-2 的）。

### 4.4 B-2 T1-A3 平台存储与遥测（≤3 天，B-1 合入后做）
- **范围**：`internal/storage`、`internal/uplink`、`cmd/*`、`internal/dal/telemetry_*.go`、`internal/dal/device_latest_telemetry.go`。文件清单以 `docs/T1-CARDS.md` 的 T1-A3 一节为准，**但 `internal/tdenginehistory` 不碰**（线 A 在用 TDengine，避免冲突）。
- **必须一起修**：MySQL 下租户最新遥测汇总直接返回 503（`internal/service/device_latest_telemetry.go:27`）；保留期清理里的 `VACUUM FULL`（`internal/dal/telemetry_datas.go:289`）在 MySQL 下跳过。
- **不碰**：`internal/storage/telemetry_broker.go`（broker 不做 MySQL），但要确认 MySQL 配置下它不被启动。
- **测试**：develop 已有 `internal/dal/telemetry_current_mysql_test.go`、`internal/storage/history_intent_mysql_test.go`；从 d1 搬 `internal/dal/telemetry_batch_conflict_mysql_test.go`。
- **完成标准**：这 3 个测试两库 PASS；`go test ./internal/storage/...` 通过。

### 4.5 B-3 平台在 MySQL 上冒烟（约 1 天）
- `scripts/smoke_mvp1.sh` 分别对 PG 和 MySQL 平台跑一次，都通过；`go build ./...` 通过；`grep -rln --include='*.go' -E 'pkg/(sqldialect|dbx|dbdialect)' initialize internal cmd` 为空。
- 这一步就是第一阶段的收尾，写一份阶段完成说明 `docs/tasks/DB-PHASE1-DONE.md`。

---

## 5. 环境与命令（照抄就行）

**测试数据库**（自己的容器，用完删）：
```bash
docker ps -a | grep tp-dev-              # 先看有没有别人在用同名容器；有就问负责人
scripts/dev-databases.sh up
eval "$(scripts/dev-databases.sh env)"
export TEST_DBDIALECT_PG_DSN="$TEST_PG_SCHEMA_DSN"   # 脚本没导出这个，有 2 个平台测试要用（指向 56001 未实测，跑不通就问）
scripts/dev-databases.sh down            # 用完
```

**怎么确认测试真的跑了**：加 `-v`，看每个测试是 `--- PASS` 而不是 `--- SKIP`：
```bash
go test -count=1 -v ./internal/dal/ -run 'MySQL|Postgres' 2>&1 | grep -E -- '--- (PASS|SKIP|FAIL)'
```
变量少一个，测试就会**静默跳过**，而且各测试的跳过提示不一样，所以只认 `--- PASS`。

**从 d1 分支取测试文件**（d1 是旧的“翻译器”版本，只拿测试场景，不拿代码）：
```bash
git show wp/d1-dbx-returning:internal/dal/notification_source_outbox_mysql_test.go > internal/dal/notification_source_outbox_mysql_test.go
```
取来后把里面的 `dbx`、`sqldialect` 引用改掉（改成 `*gorm.DB`），能编译、两库 PASS 才算。

**编译**：根模块 `go build ./...`。（本阶段不碰 `encore-api`；如果必须检查，用 `cd encore-api && encore test ./<包>/... -run '^$'`，**不要用 `go build` 也不要用 `encore check`**，前者会报一个和你无关的错，后者会卡 8 分钟。）

**Go 缓存**：用全局的（`go env` 已设好），不要新建按任务命名的缓存目录。

**证据**：放 `/Volumes/KIOXIA/archive/evidence/<日期>-<卡号>/`，仓库只写一行索引（`CLAUDE.md` 里的 80G 盘已不用）。不要把密码、令牌写进去。

---

## 6. 红线（违反就退回）

1. 不新增对 `pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx`、`pkg/dbdialect` 的引用；不对 SQL 字符串做替换后再执行。
2. GORM 表达不了的少数语句，在**所属文件里**写 PG、MySQL 两个版本，用 `relational.DialectOf(db)` 选；锁用 `relational.LockTx`。写法范例：`docs/RELATIONAL-ADAPTER-DESIGN.md` §4.2、§4.3。
3. PG 已执行的迁移文件一个字不改。
4. 不推送、不合并、不用 `git stash`；删东西先问。
5. SKIP 不等于通过。

**交付前跑这三条**：
```bash
git diff develop -U0 -- '*.go' | grep '^+' | grep -c -E 'pkg/(sqldialect|dbx|dbdialect)'      # 必须是 0
git diff develop --stat                                                                          # 只改了本项范围的文件
git diff develop -- ':(glob)sql/[0-9]*.sql' ':(glob)sql/broker/*.sql' ':(glob)sql/service_access/*.sql' ':(glob)encore-api/**/migrations/*.sql'   # 必须为空
```

---

## 7. 完成说明（每项一份，越短越好）

`docs/tasks/<卡号>-DONE.md`：
```markdown
# <卡号> 完成说明
分支：…  基线：develop@…  提交：…
## 改了什么（每个文件一句话）
## 验证（贴命令和 PASS/SKIP/FAIL 的计数；SKIP 写原因）
## 没做 / 发现的问题（如实写）
```
负责人审查只看：改动在范围内；三条交付检查的输出；测试真的 PASS；“没做”写得诚实。

---

## 8. 什么时候必须停下来问

- 要改本项范围外的文件；
- 一个测试失败，半天看不懂原因；
- PG 和 MySQL 结果不同，不知道哪个对；
- 一项做到第 3 天还没完。**停下，把已做的提交成 `wip:`，写清楚还差什么。**

---

## 9. 资料（需要时再查，不要先全读）

| 文档 | 什么时候看 |
|---|---|
| `docs/T1-CARDS.md` 的 T1-A2、T1-A3 两节 | 做 B-1、B-2 时查文件清单 |
| `docs/RELATIONAL-ADAPTER-DESIGN.md` §4.2、§4.3 | 写 PG/MySQL 两个版本时照着写 |
| `docs/PROJECT-AUDIT-2026-11-04.md` 的 T3 一节 | 做线 A 时参考 |
| `docs/MULTI-DB-ANALYSIS-2026-11-05.md` | 想了解全局和下一阶段时看 |
| `docs/tasks/T1-A0-DONE.md`、`T1-A1-DONE.md` | 已完成部分的说明 |
| `docs/MYSQL-MVP-WORK-PLAN-2026-11-03.md` | **过期**，不要按它做 |

### 下一阶段（第一阶段做完再说）
Encore 服务逐个改 MySQL（devicemetadata 最大，约 18 天）→ 删除翻译器 → 四组合冒烟 → 切换文档。卡片已在 `docs/T1-CARDS.md` 拆好；到时候按同样的方式，每次只挑一两张能单独交付的。
