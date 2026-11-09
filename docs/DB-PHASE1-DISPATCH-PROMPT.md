# 派工提示词：数据库兼容 第一阶段

> 用法：把下面“提示词正文”整段发给干活的人（或 AI 会话）。
> 这份提示词里的卡点都是 2026-11-09 **实际跑过**才写进来的，不是猜的。
> 配套说明：[`DB-COMPAT-HANDOVER.md`](DB-COMPAT-HANDOVER.md)。

---

## 提示词正文

你接手 ThingsPanel 后端的“数据库兼容 第一阶段”。仓库：`/Users/junhong/Downloads/code/thingspanel-backend-community`。

### 你只做两件事
- **线 A**：让 TDengine 成为正式可选的时序库（去掉“实验”开关）。
- **线 B**：让平台根模块在 MySQL 上跑起来（补完做了一半的 T1-A2，做 T1-A3，最后冒烟）。

**除此之外什么都不要做。** 特别是：不碰 `encore-api/` 下的任何服务、不删翻译器、不重新设计、不重新盘点、不写长文档。看到别的问题，写一行进 `docs/TODO.md` 就继续。

### 开工前必须做的 4 件事
1. 读 `CLAUDE.md`（工作规则）和 `docs/DB-COMPAT-HANDOVER.md`（只有 176 行）。**其他文档需要时再查，不要先全读。**
2. 确认没人在用测试库：`docker ps -a | grep tp-dev-` 应该没有输出；端口 56001–56004 没被占（`lsof -nP -iTCP:56001 -sTCP:LISTEN` 等）。有冲突就停下问我。
3. **不要碰正在运行的容器**。机器上跑着别人的栈（名字带 `tp-ha-ui-acceptance-`、`pui-stack`、`ph1-s1`、`mvp-ha`、`thingspanel-community-v130`、`ha-demo` 的都是），一个都不能停、不能删。
4. 每开一个分支，当天在 `docs/WORKSPACES.md` 加一行登记。

### 绝对不能做（做了就整份退回）
1. 新增对 `pkg/sqldialect`、`encore-api/pkg/sqldialect`、`encore-api/pkg/dbx`、`pkg/dbdialect` 的引用；对 SQL 字符串做替换后执行。
2. 改 PG 已执行过的迁移文件（`sql/1.sql`…`sql/35.sql`、`sql/broker/`、`sql/service_access/`、`encore-api/*/migrations/`）。
3. 推送、合并、`git stash`、删除别人的东西。
4. 把 SKIP 的测试说成通过。

---

### 线 B 第 1 步：接手 T1-A2（先做这个，最有把握）

**现状（我已核实）**：分支 `wp/t1-a2-platform-dal-service`，worktree `.claude/worktrees/wp-t1-a2-platform-dal-service`。它落后 develop 64 个提交，但 **develop 这期间没改过它涉及的目录，合并不会冲突**（我在临时目录试合过，干净；合完能编译）。大部分已经做完：下面这些测试在合并后**两个库都已经 PASS**：`TestSourceOutboxClaimTwoWorkersPostgresMySQL`（两个 worker 抢同一队列）、`TestInsertIfAbsentPostgresMySQL`、`TestAutomationGuardPostgresMySQL`、`TestUiMenuJSONArrayPostgresMySQL`、`TestDeviceTrendPostgresMySQL`、`TestDeviceIDReservationsBehaveTheSameOnMySQLAsOnPostgres`、`TestServiceAccessOperationsBehaveTheSameOnMySQLAsOnPostgres`、`TestMenuByAuthorityMatchesPostgresOnMySQL` 等。**只剩 1 个失败**（见卡点 B1-2）。

做法：
```bash
cd .claude/worktrees/wp-t1-a2-platform-dal-service
git add internal/repo/a2_relational_test.go internal/repo/market_install_authorization_repo.go
git commit -m "wip: market install authorization dual dialect (in progress)"
git rebase develop
go build ./...
```
然后起测试库（见“测试库”一节），跑：
```bash
go test -count=1 -v ./internal/dal/ ./internal/repo/ ./internal/automationguard/ -run 'MySQL|Postgres' 2>&1 | grep -E -- '--- (PASS|SKIP|FAIL)'
```

**卡点 B1-1：rebase 会动 `docs/WORKSPACES.md`**。这是唯一两边都改过的文件，如果冲突，两边的行都保留即可。

**卡点 B1-2：唯一失败的测试 `TestMarketInstallAuthorizationCreateAndLegacySealPostgresMySQL`，原因是 JSON 列比较。**
- 半成品为了去掉 PG 专用写法，把 `internal/repo/market_install_authorization_repo.go` 里两处 `plan=CAST(? AS jsonb)` 改成了 `plan=?`。结果**两个库都错了**：PG 报 “installation plan integrity failure”，MySQL 报 “record not found”。
- 原因：PG 里 `jsonb` 列不能直接和文本参数比；MySQL 里 JSON 列和字符串参数比较，不是按 JSON 值比的，所以查不到。
- 正确做法：这正是“GORM 表达不了、要写两份”的情况。在这个文件里按方言写：PG 用 `plan = CAST(? AS jsonb)`，MySQL 用 `plan = CAST(? AS JSON)`；方言用 `relational.DialectOf(tx)` 判断。或者更简单：这两处本来就有 `plan_digest` 和精确文本两道检查，评估一下能否**不比较 `plan` 列本身**（只比 `id` + `plan_digest`）。两种做法选一种，在完成说明里写清楚为什么。
- **改完必须两个库都 PASS**，并且跑一遍 `go test ./internal/repo/...` 确认没打坏 PG 原有测试。

**卡点 B1-3：d1 分支里的旧测试不能原样拿来用**。
- `internal/dal/do_nothing_rows_mysql_test.go`（d1 才有）用了 `sqldialect.OpenMySQL`——那是翻译器，**禁止**。搬过来后要改成直接用 `gorm.io/driver/mysql` 打开，照 develop 里 `internal/dal/ui_menu_mysql_test.go` 第 28 行的 `platformMySQL(t)` 写法。
- `notification_source_outbox` 的测试 **不用再从 d1 搬**，A2 里已经有等价的差分测试并且通过了。
- 先确认：A2 现有测试是否已经覆盖了“DO NOTHING 返回行数”这个场景（`TestInsertIfAbsentPostgresMySQL` 很可能已覆盖）。已覆盖就不搬，在完成说明里写一句。

**卡点 B1-4：对照清单补漏**。打开 `docs/T1-CARDS.md` 的 “T1-A2” 一节，把“需双实现约 8 条”逐条对照：`notification_source_outbox.go` 队列领取（已完成）、`native_automation_command.go` 的 `GREATEST` upsert、`device_identity`/`service_access_operation` 的 `InsertIfAbsent`（已完成）、`board.go` 的数组/时间。每条在完成说明里写“已完成/本次完成/没做及原因”。

完成标准：
- `grep -rln --include='*.go' 'pkg/dbdialect' internal pkg | grep -v '^pkg/dbdialect'` 无输出；
- 上面那条 `go test -v` 里**没有 FAIL，相关测试没有 SKIP**；
- `go test -count=1 ./internal/dal/... ./internal/repo/... ./internal/service/... ./internal/automationguard/...` 通过；
- 写 `docs/tasks/T1-A2-DONE.md`，提交，告诉我。

---

### 线 B 第 2 步：T1-A3 平台存储与遥测（第 1 步合入后才做）
- 分支 `wp/t1-a3-storage`，从 develop 切。范围以 `docs/T1-CARDS.md` 的 “T1-A3” 一节为准，**但不碰 `internal/tdenginehistory`**（线 A 在用）。
- 必须一起修两个 PG 残留：`internal/service/device_latest_telemetry.go:27`（MySQL 下直接返回 503）、`internal/dal/telemetry_datas.go:289`（`VACUUM FULL` 在 MySQL 下要跳过）。
- 不碰 `internal/storage/telemetry_broker.go`，但写一个测试或日志证据证明 MySQL 配置下它不会被启动。
- **卡点 B2-1**：develop 已有 `internal/dal/telemetry_current_mysql_test.go`、`internal/storage/history_intent_mysql_test.go`，确认两库 PASS。
- **卡点 B2-2**：d1 的 `internal/dal/telemetry_batch_conflict_mysql_test.go` 要搬，但它用了 `openTranslatedMySQL()`（翻译器，禁止），换成 `platformMySQL(t)` 那种直接打开。它还会**临时替换全局变量 `global.DB` 和 `query.SetDefault`**——这种测试不能和别的测试并行，函数里不要加 `t.Parallel()`，跑完一定要恢复原值（原文件里的 `defer` 要保留）。
- 完成标准：这 3 个测试两库 PASS；`go test -count=1 ./internal/storage/... ./internal/dal/...` 通过；写 `docs/tasks/T1-A3-DONE.md`。

### 线 B 第 3 步：平台在 MySQL 上冒烟（第 2 步合入后）
`scripts/smoke_mvp1.sh` 需要一个**正在运行的平台**和一个租户账号：`scripts/smoke_mvp1.sh <平台地址> <邮箱> <密码>`。

**卡点 B3-1：平台启动依赖 MySQL、Redis、MQTT 三样**。MQTT 连不上，平台会直接启动失败（`internal/app/mqtt_service.go` 里 `MqttInit` 出错就返回）。所以本机跑要起 3 个自己的容器（名字加你的前缀，跑完删）：
```bash
docker run -d --name dbp1-mysql -e MYSQL_ROOT_PASSWORD=pw -e MYSQL_DATABASE=thingspanel -p 56102:3306 mysql:8.4
docker run -d --name dbp1-redis -p 56179:6379 redis:7-alpine
docker run -d --name dbp1-mqtt  -p 56183:1883 eclipse-mosquitto:2 mosquitto -c /mosquitto-no-auth.conf
```
**卡点 B3-2：配置**。复制 `configs/conf.yml` 成一个临时文件，用 `go run . -config <临时文件>` 启动。要改：`relational.driver: mysql` 和 `relational.mysql.platform.*`（写法见 `docs/tasks/T1-A1-DONE.md` 的“最小 MySQL 配置示例”）、`db.redis.addr`、MQTT 地址。也可以用环境变量，前缀是 `GOTP_`，点换成下划线，例如 `GOTP_RELATIONAL_DRIVER=mysql`。**不要用 `mqtt.input_profile=ingress_v1`**（那条路只支持 PG）。
**卡点 B3-3：账号**。MySQL 的种子数据（`sql/mysql/014_platform_seed.sql`）里有默认租户账号 `tenant@tenant.cn`，密码问我，**不要写进任何文件**。
- PG 那一次同理，把 `relational.driver` 换成 `postgres`，用 `tp-dev-pg-oracle`（56001）或自己的 PG 容器。
- 完成标准：两次冒烟每一步都 `ok`；输出存到证据目录；写 `docs/tasks/DB-PHASE1-DONE.md`（第一阶段总结，一页）。

---

### 线 A：TDengine 转正（可以和线 B 同时做，改的文件不重叠）

**A-1 在完整栈上验证（先做）**
- **卡点 A1-1：后端镜像**。必须用 develop 代码构建的后端镜像：`docker build -t thingspanel-backend:dbp1-<develop短哈希> .`（在仓库根目录，约 5 分钟）。
- **卡点 A1-2：起栈**。用安装器仓库 `/Users/junhong/Downloads/code/thingspanel-installer-develop-review` 的 `docker-compose.yml`：TDengine 服务在 `tdengine` profile 里，默认不启动。要设：`COMPOSE_PROFILES=tdengine`、`TP_HISTORY_BACKEND=tdengine`、`TP_TDENGINE_EXPERIMENTAL=true`、`TP_BACKEND_IMAGE=<上面构建的镜像>`。**另开一个目录、另起一个 compose 项目名**（例如 `-p dbp1-tdengine`），端口不要和已有栈撞。安装器第一次起栈要先做初始化（`config/native/prepare.py`），这一步不熟就**停下问我**，不要瞎试。
- 用浏览器：登录、建设备、用 MQTT 客户端上报几条数据，看**设备历史、导出、统计**三处。截图放证据目录。
- 有任何一处不正常：记录现象和后端日志，**停下报告**，不要自己扩大范围修。

**A-2 去掉开关（A-1 通过后）**
- 只改：`internal/app/history_backend.go`（第 43 行附近的拒绝逻辑和 `Experimental` 字段）、`internal/app/storage.go`（第 113 行读取开关）、`internal/app/history_backend_test.go`（相关用例）。
- 新建 `docs/DATABASE-SWITCH.md`，写“时序库”一小节：设哪个配置选 TDengine、要填哪些项（`storage.history_backend`、`storage.tdengine.url/user/password/database`）。
- **卡点 A2-1**：安装器的 compose 里还会传 `GOTP_STORAGE_TDENGINE_EXPERIMENTAL`，后端不读了也无害。**不要去改安装器仓库**，写一行 TODO 即可。
- **卡点 A2-2**：TDengine 的测试要设 `TP_TDENGINE_TEST_URL` 才会跑，不设就**全部 SKIP**。安装器 compose 里的 TDengine **没有把端口映射到本机**，测试连不上它，所以另起一个只给测试用的：
  ```bash
  docker run -d --name dbp1-tdengine -p 56041:6041 tdengine/tdengine:3.3.6.0     # 本机没有这个镜像，第一次会下载
  export TP_TDENGINE_TEST_URL=http://127.0.0.1:56041                              # 测试里用户名密码写死为 root / taosdata
  go test -count=1 -v ./internal/app/ ./internal/storage/ -run 'TDengine|Tdengine' 2>&1 | grep -E -- '--- (PASS|SKIP|FAIL)'
  ```
  `internal/tdenginehistory` 的测试还要 `TP_TELEMETRY_BROKER_TEST_DSN`，这一阶段不碰那个包，不用跑。用完 `docker rm -fv dbp1-tdengine`。
- 完成标准：`go test -count=1 ./internal/app/...` 通过；不设 `experimental` 时 `history_backend=tdengine` 能启动；写 `docs/tasks/T3-DONE.md`。

---

### 测试库（线 B 用）
```bash
scripts/dev-databases.sh up                       # 约 20 秒，起 tp-dev-pg-oracle(56001) tp-dev-mysql(56002) tp-dev-pg-plain(56003) tp-dev-mysql-auto(56004)
eval "$(scripts/dev-databases.sh env)"
export TEST_DBDIALECT_PG_DSN="$TEST_PG_SCHEMA_DSN" # 脚本没导出它；2026-11-09 实测指向 56001 可用
# …跑测试…
scripts/dev-databases.sh down                     # 用完一定删
```
**判断测试真的跑了**：只认 `--- PASS`。变量少一个测试就会 `--- SKIP`，而且跳过提示各不相同，不要靠找某句提示判断。

### 其他坑
- **不要**对 `encore-api` 跑 `go build`（会报一个和你无关的 `RecoverInterruptedUploads` 错误），也**不要**跑 `encore check`（会卡 8 分钟）。这一阶段你本来就不碰 `encore-api`。
- Go 缓存用全局的（`go env GOCACHE` 已经设好），不要自己新建缓存目录。
- 外置盘偶尔会出 I/O 错误（数据库日志里有 `PANIC` / `I/O error`）。遇到了先 `docker start` 对应容器，再继续；别以为是你代码的问题，但要在完成说明里记一笔。
- 证据（日志、截图）放 `/Volumes/KIOXIA/archive/evidence/<日期>-<卡号>/`，不要放进仓库，不要包含密码和令牌。

### 交付前跑这三条，结果贴进完成说明
```bash
git diff develop -U0 -- '*.go' | grep '^+' | grep -c -E 'pkg/(sqldialect|dbx|dbdialect)'      # 必须是 0
git diff develop --stat                                                                          # 只改了本项范围的文件
git diff develop -- ':(glob)sql/[0-9]*.sql' ':(glob)sql/broker/*.sql' ':(glob)sql/service_access/*.sql' ':(glob)encore-api/**/migrations/*.sql'   # 必须为空
```

### 完成说明格式（每项一份，越短越好）
```markdown
# <卡号> 完成说明
分支：…  基线：develop@…  提交：…
## 改了什么（每个文件一句话）
## 验证（命令 + PASS/SKIP/FAIL 计数；每个 SKIP 写原因）
## 没做 / 发现的问题
```

### 必须停下来问我的情况
- 要改范围外的文件；
- 一个测试失败，半天看不懂；
- PG 和 MySQL 结果不一样，不知道哪个对；
- 安装器初始化、账号密码、端口冲突；
- 任何一项做到第 3 天还没完：把已做的提交成 `wip:`，写清楚还差什么，停下。

### 顺序小结
1. 线 B 第 1 步（A2 收尾）——最先做，最有把握，1–2 天；
2. 同时可以开线 A 的 A-1（起 TDengine 栈）；
3. A2 合入后做线 B 第 2 步（A3）；A-1 通过后做 A-2；
4. 最后线 B 第 3 步（冒烟），写第一阶段总结。
