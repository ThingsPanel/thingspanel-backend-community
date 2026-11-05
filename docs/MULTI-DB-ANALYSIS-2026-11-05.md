# 多数据库：新老代码对比分析（只读）

> 日期：2026-11-05。基线：`develop@520a3222`（本分支 `claude/wp-multidb-analysis-3e30ab`）。
> 老代码：`origin/v1.1.11`（`6686199f`，2025-11-21）、`origin/0.5.4.1`（`ef91e50e`，2024-06-21）、`origin/0.4.8.2`（`7f050931`，2023-04-26）、`origin/main-1.3.0`（`07124839`）、`origin/main`（`f4569bca`）。
> 追加（§7）：企业版 `thingspanel-backend-enterprise`、`thingspanel-TDengine`。
> 对照物：`wp/d1-dbx-returning@9cd62bb9`、`docs/T1-CARDS.md`、`docs/RELATIONAL-INVENTORY.md`、任务书副本 `docs/closeout-20261102/inputs/ThingsPanel_Encore_Migration_Tasks_2026-11-01.md`。
> 只读分析，没有改任何代码。证据写法：`分支:路径:行号`；没查到的写“未查到”。

## 0. 结论摘要（一页）

**1. 老代码并没有“GORM 多库”，也没有“直连 TDengine”。**
- 关系库：四个老版本**全部只连 PostgreSQL**。`gorm.Open(postgres.Open(...))` 是唯一的打开方式（`v1.1.11:initialize/pg_init.go:152`、`0.5.4.1:initialize/psql/psql.go:63`、`0.4.8.2:initialize/psql/psql.go:69`）。`gorm.io/driver/mysql` 在三个版本的 `go.mod` 里都只是 `// indirect`（如 `v1.1.11:go.mod:141`），没有任何 Go 代码调用 `mysql.Open`（唯一命中是 `cmd/gen/main.go:16` 的注释）。Kingbase、PolarDB 没有驱动，只是字符串。
- 时序：TDengine **从没有被 Go 代码直连**。老代码把读请求通过 gRPC 转给一个**仓库外的服务 tptodb**；平台自己**写入永远走 PG**（`v1.1.11:internal/dal/telemetry_datas.go:24-25` `CreateTelemetrData` → `query.TelemetryData.Create`）。`TSDB/KINGBASE/POLARDB` 三个值只是“把读走 gRPC”的开关（`v1.1.11:internal/dal/telemetry_datas.go:51-52` 等 7 处）。**（社区仓库里看不到 tptodb 服务端；它在企业版仓库 `thingspanel-TDengine`，见 §7：它自己订阅 MQTT/Kafka 并行写 TDengine，没有设备历史删除。）**
- 所以能从老代码“复用”的东西，**几乎为零**：没有 MySQL 方言经验可抄；TDengine 的有价值部分（7 个读接口的语义清单）已经被现在的 `tdenginehistory` 超过。

**2. 现在 develop 比老代码走得远得多，差距集中在 Encore 服务。**
- 平台根模块：启动、连接、迁移已经是 `pkg/relational`（T1-A0/A1 完成；`initialize/platform_driver.go` 已不再引用翻译器）。根模块 CRUD 主体是 GORM-gen（`internal/query` 68 个文件，`internal/dal` 里 418 处 `query.*` 引用），裸 SQL 约 137 条语句，其中真正需要 PG/MySQL 双写的约 15 条（盘点数据，已抽查）。
- Encore 服务：833 条语句，153 条需双写；其中 `devicemetadata` 一个包 303/77。翻译器/`dbx` 仍被 33 个非测试文件引用（几乎全在 `devicemetadata`）。
- 时序：`HistoryBackend` 接口 10 个方法 TDengine 全部实现，写入走“意图+重放”，有真实 TDengine 3.4 的差分测试。`experimental` 开关只是一道 `if`（`internal/app/history_backend.go:43`）。

**3. 对 26 张卡的判断。** 26 张卡的方向正确，**不需要推倒**；但有三处该改：
 (a) **把 T3（TDengine 转正）提到最前面做**：PG+TDengine 不需要任何 T1 卡，T3 只是因为改同样的目录才被排在 A3 之后；先做它，几天内就能得到第一个“可选时序库”交付。
 (b) **漏了一件事**：卡里没有“MySQL + TimescaleDB”的实现——develop 里只有 `history_backend=postgres|tdengine`，所谓 postgres 就是写 `global.DB`。MySQL 档下历史会落进 MySQL 的普通表 `telemetry_datas`（`sql/mysql/012_platform_base.sql:1009`），**不是 Timescale**。需要负责人定：MySQL 档推荐 TDengine，还是新做第二条 PG 连接（任务书 S02）。
 (c) **卡可以缩**：A4 并入 D3，D3 的 TDengine 两格并入 T3，C5+C6 估时下调，A2+A3 可合并。合计省约 5–6 人天。

**4. 工作量（人天，估算，非承诺）。**

| 目标 | 剩余工作 | 估算 |
| --- | --- | --- |
| PG + TDengine 转正 | T3 + 清掉 tptodb 分支 + 文档 | **3–4 天** |
| PG→MySQL 关系库（含平台和全部 Encore 服务） | 26 卡中除 A0/A1 外 | 原计划 51.5 天；调整后约 **45–47 天**（两条线并行约 25–28 个工作日） |
| MySQL + TDengine（无 PG） | 上一行 + 平台 PG 专用尾巴 + broker 暂缓 | MySQL 完成后再加 **2–3 天** |
| MySQL + TimescaleDB | 未设计，需先拍板 | 至少 **+5 天**（见 §5 决定 D1） |

**5. 建议顺序：先 PG+TDengine（3–4 天）→ 再 PG→MySQL（先小模块暖身、devicemetadata 单线推进）→ 最后 MySQL+TDengine。** 验收标准见 §5。

**6. 企业版（§7，追加）：** 企业版同样**没有 MySQL/Kingbase/PolarDB 实现**（只在文档里宣称）。它有的是：外置服务 `thingspanel-TDengine`（自己订阅 MQTT/Kafka 写 TDengine，平台只读，没有设备历史删除）；以及未合入 `main` 的达梦分支（在调用点写 `if db.type=="dm"`，是反例）。**因此没有可直接复用的“多库代码”，但有两个影响决策的事实：企业版默认在用 tptodb，所以我撤回“社区版删除 tptodb”的建议；`thingspanel-TDengine` 的聚合实现可作 T3 差分的第三方参照。**

---

## 1. 问题一：老代码支持到什么程度

### 1.1 关系库

| 版本 | 关系库接入 | 证据 |
| --- | --- | --- |
| `0.4.8.2`（2023-04，beego 时代） | 只 PG；`gorm.Open(postgres.Open)`。casbin 注释里出现 `mysql`，是 gormadapter 示例注释 | `0.4.8.2:initialize/psql/psql.go:69`；`0.4.8.2:initialize/casbin/casbin.go:19,48`（注释） |
| `0.5.4.1`（2024-06） | 只 PG；配置里 `db.psql.dbType: timescaledb` 只是“用不用 Timescale 查询”开关 | `0.5.4.1:conf/app.yml:13`；`0.5.4.1:initialize/psql/psql.go:63`；`0.5.4.1:services/tskv_service.go:403-404`（`dbType == "timescaledb"`） |
| `v1.1.11`（2025-11） | 只 PG | `v1.1.11:initialize/pg_init.go:152`；`go.mod:30` 只有 `gorm.io/driver/postgres` 是直接依赖，`go.mod:141` `gorm.io/driver/mysql // indirect` |
| `main-1.3.0` | 同上；`test/multidb/docker-compose.yml`（2024-06-04，一个 MySQL 容器定义）无任何 Go 代码引用，内容未细读，疑似 gorm-gen 示例 | `main-1.3.0:test/multidb/docker-compose.yml`；`git grep mysql -- '*.go'` 只命中 `cmd/gen/main.go:16` 注释 |
| `origin/main`（2026-11-05） | 不含 `tdengine`/`relational.driver`/`history_backend` 的 Go 代码（`git grep` 无命中）；与本分支相差：main 比 HEAD 多 11 个提交、少 269 个 | `git rev-list --count HEAD..origin/main` |

- **是否全部走 GORM**：不是。`v1.1.11` 是 GORM-gen（`internal/query` 69 个文件）为主，加 19 处 `Raw/Exec`（`internal/dal` 12、`initialize` 6、`internal/diagnostics` 1）。`0.5.4.1` 有 67 处、`0.4.8.2` 有 51 处 `Raw/Exec`（beego+gorm 混合；数字为 `git grep -E 'Raw\(|Exec\('`，含一部分非 SQL 的同名调用，量级参考）。`main-1.3.0` 到 270 处（合入 automation 之后）。
- **PG 专用写法（v1.1.11）很少**：`ON CONFLICT`（`internal/dal/telemetry_datas.go:223`）、`::`（3 处，其中一处是注释、一处在自测工具里）、`EXTRACT(EPOCH...)`（`cmd/iot-platform-autotest/.../db_client.go:169`）。Timescale 只出现在建表脚本：`v1.1.11:sql/1.sql:730 create_hypertable('telemetry_datas','ts',...)`。**业务代码里没有 `time_bucket` 等 Timescale 专有函数**，所以 Timescale 对应用代码是“透明”的。
- **Kingbase / PolarDB**：全仓库唯一出现位置是 `if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB"`，后面只是调用 gRPC（`v1.1.11:internal/dal/telemetry_datas.go:51-52,88-89,123-124,297-298,335-336`、`telemetry_current_datas.go:22-23,52-53`）。**没有这两种库的连接代码**。任务书也这样判断（任务书 §2.4：“配置字符串出现 TSDB/KINGBASE/POLARDB 仅证明存在分支”）。

### 1.2 时序库（TDengine / tptodb）

**接入方式：外部 gRPC 服务，不是直连，也不是配置分支里的驱动。**
- 客户端：`third_party/grpc/tptodb_client/init.go:35-48`（`grpc.Dial(grpc.tptodb_server, insecure)`，全局变量 `TptodbClient`）；配置 `grpc.tptodb_server: 127.0.0.1:50052`、`grpc.tptodb_type: NONE # NONE、TSDB`（`v1.1.11:configs/conf.yml:48-49`）。启动时在 `internal/app/grpc_service.go:31` 无条件 `GrpcTptodbInit()`。
- 接口：proto 定义 8 个 RPC（`SayHello` + 7 个读）：`GetDeviceHistory`、`GetDeviceHistoryWithPageAndPage`、`GetDeviceAttributesHistory`、`GetDeviceAttributesCurrents`、`GetDeviceAttributesCurrentList`、`GetDeviceKVDataWithNoAggregate`、`GetDeviceKVDataWithAggregate`（`v1.1.11:third_party/grpc/tptodb_client/grpc_tptodb/tp_to_db_grpc.pb.go:146-154`）。`v1.1.11` 实际调用其中 5 个（Currents ×3、History、HistoryWithPage、KVNoAggregate、KVAggregate）；`0.5.4.1` 调 7 个（`services/tskv_service.go:830,966,1242,1368,1539,1618,1663,1819`）。
- **覆盖的操作：只有读**（当前值、范围历史、分页历史、KV 无聚合/聚合）。**没有写、没有删除、没有保留期、没有导出接口**。写入始终是 PG（见 §0）。
- 时间线：`0.5.4.1` 里 `main.go:50` 起 gRPC 客户端，最早引入该调用的提交是 `934ddab3`（2023-08-24，“增加批量写入和cassandra兼容；优化vernemq兼容”）；“grpc 支持 7 个接口查询”是 `c47a8346`（2024-06-30，`git log -S tptodb_type origin/main`）。同期 `0.5.4.1` 还有 `dbType == "cassandra"` 的另一条旁路（`services/tskv_service.go:822,952,1233,1362,1532`），共 12 处，现在已不在 develop 里。
- **“后来为什么没了 / 变成现在的样子”**：
  - 其实**没有消失**。develop 里 `third_party/grpc/tptodb_client` 仍在，`internal/dal/telemetry_datas.go`（行 52/89/124/298/336 的分支）和 `telemetry_current_datas.go`（行 22/52）仍保留 TSDB 分支，`internal/app/grpc_service.go:31` 仍无条件初始化。它们和新的 `HistoryBackend` 是**两套并存的时序机制**。
  - 新机制替代它的原因，仓库里有的只是设计意图：任务书 §2.4/§2.5 认为 tptodb 不能保证删除、幂等、跨库一致，要求“封装成普通 adapter 并验证读写/删除契约”，且“业务层散落的 `grpc.tptodb_type` 分支要删除”（任务书 S04）。**当时为什么决定放弃 tptodb 的原始讨论记录：未查到。**
  - 已有的保护：`internal/service/device_latest_telemetry.go:19-21` 在 `tptodb_type` 非空非 NONE 时让租户汇总接口直接返回 503，说明新代码已经把 tptodb 视为“不被支持的旧档”。

### 1.3 对问题一的回答

| 问题 | 回答 |
| --- | --- |
| 老版本支持 MySQL/Kingbase/PolarDB 吗？ | 否。只有 PG；后两者只是配置字符串，且只用来选择“读走 gRPC” |
| 全走 GORM？ | 大部分是 GORM（含 gorm-gen），少量 `Raw/Exec`；因为只有 PG，没有任何“为多库而写”的抽象 |
| 哪里写了 PG 专用 SQL？ | 很少且集中：`ON CONFLICT`（telemetry）、`EXTRACT(EPOCH)`、`::json` 注释、Timescale 的 `create_hypertable`（仅 `sql/1.sql:730`） |
| TDengine 怎么接？ | 经外部 tptodb 的 gRPC，仅读。写入、删除、保留期都不在平台这边 |
| 能抄什么？ | 7 个读接口的“业务读取形状”（当前值/历史/分页/KV 聚合）已被新 `HistoryBackend` 覆盖，无需再抄 |

---

## 2. 问题二：develop 现在的实际状态

### 2.1 平台根模块

| 项 | 数据 | 证据/口径 |
| --- | --- | --- |
| 启动与连接 | 已改为 `relational.driver` + `pkg/relational`；MySQL 迁移走 `relational.Migrate`；翻译驱动已删 | `initialize/platform_driver.go:30-`（`RelationalInit`）；`docs/tasks/T1-A1-DONE.md` §2 |
| GORM-gen CRUD | `internal/query` 68 个文件；`internal/dal` 内 418 处 `query.X.` 引用，`internal/service` 49 处，`internal/uplink` 4 处 | `git grep -E 'query\.[A-Z]\w+\.'`（我的统计） |
| `Raw/Exec` 调用点 | `internal/dal` 68、`initialize` 24、`internal/storage` 18、`internal/service` 17、`cmd` 11、`internal/repo` 6、`internal/uplink` 1 | `git grep -E '\.(Raw|Exec)\('`（调用点，≠语句数，含 DDL 执行） |
| 裸 SQL 语句（盘点口径） | 137 条（不含 broker），可移植 122、需双写 15 | `docs/RELATIONAL-INVENTORY.md` §1。**抽查一致**（dal 双写原因：time 2、arrays 1、lock_skip 1、returning 1；storage：json 3…） |
| 原样粗 grep（含迁移 SQL，仅看量级） | `ON CONFLICT` 23、`jsonb/->>/@>` 27、`::类型` 50、`pg_advisory/SKIP LOCKED` 22（含 broker 与迁移文件） | 我的 `git grep`，**会高估**，不作验收口径 |
| 翻译器依赖 | `pkg/sqldialect` 被 5 个非测试文件引用（全在 `encore-api`：`automation/store/mysql_support.go`、`devicemetadata/db.go`、`platformconfig/store/{postgres,schema}.go`、`dbx/dbx.go`）；根模块 **0 个**；`pkg/dbdialect` 被 `internal/dal/{device_identity,service_access_operation}.go` 引用 | 我的 `git grep`。**这比盘点文档（写于 A1 之前）更新**：根模块已脱离翻译器 |
| 已知的“只支持 PG”残留 | ① `internal/service/device_latest_telemetry.go:26-28` 非 postgres/sqlite 直接 503（MySQL 下租户汇总不可用）；② `internal/dal/telemetry_datas.go:289` `VACUUM FULL telemetry_datas`（保留期里，MySQL 上只会打 warn）；③ `internal/dal/telemetry_datas.go:377-386` 用 `EXPLAIN` 估总数（PG 输出格式）；④ broker 控制面整体 PG-only（已决定暂缓） | 如左 |

纯 GORM 与 PG 专用的比例：按盘点口径，137 条裸 SQL 里 89% 可移植；整个根模块 CRUD 以 GORM-gen 为主，所以**根模块是整个工程里最接近“多库就绪”的部分**。

### 2.2 Encore 各服务（sqldb / 原生 SQL）

数据来自 `docs/RELATIONAL-INVENTORY.md` §1（语句数 / 双写数），我另外核对了依赖面：

| 服务 | 语句 | 双写 | 翻译器/dbx 依赖（我核对，非测试源文件数） |
| --- | --- | --- | --- |
| devicemetadata | 303 | 77 | 约 33（git grep 命中 dbx、sqldialect、dbdialect 的文件，几乎全在此包） |
| automation | 172 | 27 | 1（`store/mysql_support.go`） |
| l3-core/notification | 140 | 10 | 0 |
| northbound | 59 | 17 | 0 |
| internal（authcore） | 45 | 4 | 0（自带 `sql.Open("pgx"/"mysql")` + `dialect.go`） |
| marketinstall | 28 | 1 | 0（**已有手写 MySQL store，最接近目标形态**） |
| platformconfig | 26 | 1 | 2 |
| thingmodel | 22 | 10 | 0 |
| fileassets / connectorcatalog / cmd / pkg | 13 / 4 / 19 / 2 | 4 / 1 / 0 / 1 | 0 |
| **合计** | **833** | **153** | dbx 33 个源文件、22 个测试文件 |

> 这 833 条是“字符串字面量能数出来的下限”（盘点文档第 0 节），拼接出来的语句没算；`devicemetadata` 与 `automation` 最可能超估。

### 2.3 时序：`history_backend` 与 `tdenginehistory`

**架构**：`internal/dal/history_backend.go:13-26` 定义 `HistoryBackend` 接口（10 个方法）。PG/Timescale 是默认实现（直接调用 `dal` 里的 GORM/SQL，写 `global.DB`）。TDengine 通过 REST（`storage.TDengineHistorySink`，`internal/storage/history_tdengine.go`，205 行）+ `internal/tdenginehistory/backend.go`（282 行）。写入不是同步写，而是**关系库里先落“历史意图”，再由重放器投递**（`internal/storage/history_intent*.go`、`history_replayer.go`），删除设备同样先落“删除意图”（`backend.go:158-163`）。

| 操作 | PG/Timescale | TDengine | 说明/证据 |
| --- | --- | --- | --- |
| 批量写 | ✔ 直写 | ✔ 意图→`Append`，幂等 | `history_tdengine.go:144`；`history_tdengine_test.go:62` |
| 范围历史 / 分页 / 导出 | ✔ | ✔ | `backend.go:65,84,101` |
| 统计（单设备/多设备/窗口聚合） | ✔ | ✔ | `backend.go:110,165,229`；`TestTDengineAggregationMatchesPostgres` 等 |
| 按租户计数 | ✔（EXPLAIN 估算） | ✔（精确 COUNT） | `backend.go:123` |
| 删除（设备/key） | ✔ | ✔ 设备删除走持久“删除意图”并重放 | `backend.go:128,158` |
| 保留期 `DeleteByTime` | ✔（含 `VACUUM FULL`） | ✔ `DELETE … WHERE ts<=` | `backend.go:150` |
| 最新值/当前属性 | 关系库 | **仍在关系库**（设计如此，任务书 §2.4） | — |
| tptodb 旧分支 | 与上述并存 | — | §1.2 |

测试：`internal/tdenginehistory/backend_test.go` 有 PG/TDengine 读结果对比（`TestTDengineBackendMatchesPostgres*`、聚合、多设备统计、删除、非法参数）；`internal/storage/history_tdengine_test.go` 有写入幂等、恶意值转义、意图重放、设备删除重放。需要 `TP_TDENGINE_TEST_URL`（和部分测试的 `TP_TELEMETRY_BROKER_TEST_DSN`），**没有这些环境变量时全部 `t.Skip`**。审计文档称已在真实 TDengine 3.4 上跑过（`PROJECT-AUDIT-2026-11-04.md:82`）；我这次**没有重跑**这些测试。

**去掉“实验”标记还差什么**（`internal/app/history_backend.go:43`、`internal/app/storage.go:113`）：
1. 完整栈验证：安装器 `tdengine` profile 上从前端走历史页、导出、统计。*缺证据。*
2. 任务书 §2.4 的语义差分表：毫秒精度、UTC、同 key 同时间、空桶、diff 定义、区间端点。现有测试覆盖了读、聚合、多设备统计，但没有一份按这六项逐项写明“PG 与 TDengine 一致/有意不同”的记录。*缺文档，可能缺 2–3 个用例。*
3. tptodb 旧分支和 `grpc_service` 初始化的处置（删除或封入 adapter）。*当前并存，是隐患。*
4. MySQL 档下的收尾（见 §5 决定 D1）：`device_latest_telemetry.go:26-28`、`VACUUM FULL` 的 PG 判定。
5. 去开关本身：删 `Experimental` 字段、两个测试里对应用例（`history_backend_test.go:15,21`）、写 `DATABASE-SWITCH.md` 时序一节。*半天。*

### 2.4 翻译器残留清单

| 位置 | 内容 | 去留（现有计划） |
| --- | --- | --- |
| `pkg/sqldialect`（4 文件）及其逐字节副本 `encore-api/pkg/sqldialect` | SQL 文本改写 | D1 删除 |
| `encore-api/pkg/dbx`（`dbx.go`、`errors.go`） | 仿 `sqldb` 的句柄 + 错误码映射 | D1 删除；在 B/C 卡内逐文件换成 `*gorm.DB` |
| `encore-api/devicemetadata`：约 33 个源文件 + 19 个测试 + `mysqlschema/` | `dbx.DB` 句柄 | B1–B6 |
| `encore-api/automation/store/mysql_support.go`、`platformconfig/store/{postgres,schema}.go` | 翻译/辅助 | C9、C2 |
| `pkg/dbdialect/insert_if_absent.go` | “是否真的插入”判断（PG 行数 / MySQL 1062） | 不是翻译器，是 repository 内双实现的样板；A2 迁入 `internal/dal` |
| `encore-api/pkg/ownermigration`（2 源文件+4 测试） | 迁移锁转发层 | 锁合同已移入 `pkg/relational`；D1 删 |
| `initialize/platform_driver.go` | 已重写为 `RelationalInit`，**不再引用翻译器** | 完成（A1） |

---

## 3. 问题三：对比表（按模块）

“老”列只对有老代码可比的模块填写；老代码没有的写“无”。

| 模块 | 老代码怎么做 | 现在怎么做 | 能直接复用 | 必须新写 |
| --- | --- | --- | --- | --- |
| 关系库连接/启动 | 单个 `gorm.Open(postgres)`，无抽象 | `pkg/relational`（驱动解析、池注册、迁移、锁、错误分类）+ `RelationalInit` | 老代码：无 | 已完成（A0/A1） |
| 平台 CRUD（GORM-gen） | 同一批 `internal/query` | 同一批代码，已被 PG 使用多时 | **就是它本身**，只需测试 MySQL 行为 | 15 条双写 SQL + 少数 PG-only 尾巴（§2.1） |
| 平台迁移/schema | `sql/*.sql`（PG，含 `create_hypertable`） | PG `sql/1..33` + MySQL `sql/mysql/001–015` + 账本 | develop 已有 | 保持 PG-MAP 同步 |
| 时序读 | tptodb gRPC 7 个读 | `HistoryBackend` 10 方法 | 老：不需要（语义已被新实现超越） | 删除旧分支 |
| 时序写/删/保留期 | **无**（外部服务，未查到） | 意图+重放，持久删除意图，保留期 | develop 已有 | 语义差分记录 |
| 时序 TDengine | **无直连**，仅 gRPC | REST sink + backend（~490 行） | develop 已有，**这是唯一可用的 TDengine 适配** | 去实验开关、整栈验证 |
| devicemetadata | 无（老代码里没有该服务） | `dbx.DB` + PG SQL，303 条/77 双写 | d1 的 **场景**、`mysqlschema/001–003`（develop 已有）+ `004,005`（d1） | 逐文件换 GORM；77 条双写；MySQL 迁移文件 |
| thingmodel / northbound / fileassets / notification / connectorcatalog / automation | 无 | 仅 PG（automation 经平台 `sql/mysql/010,011`） | d1 场景 + schema 草稿 | 各自 repository 改写 |
| marketinstall | 无 | 已有手写 MySQL store + 7 个 MySQL 专用测试 | 几乎全部 | 把 `database/sql` 换 GORM、迁移器换 `relational.Migrate` |
| authcore | 无 | `sql.Open` 自带方言分支 | `accounts_dialect_test.go` 等 | 换 `relationaldb` 的 platform 资源 |
| 差分测试 | 无 | `pkg/relational/reltest`（A0）；d1 另有 `dbxtest` | 见 §3.1 | 移植场景 |

### 3.1 d1 的差分测试能不能复用——能，成本低于预期

我读了 d1 的 `encore-api/thingmodel/mysql_contract_test.go`（170 行）和 `pkg/dbxtest/dbxtest.go`：
- **场景本身驱动的是业务服务 API**（`service.Create/Update/UpdateResources…`、`note("step", …)` 记录每一步的返回），**不依赖翻译器**。
- 对 `dbx` 的依赖只有三处：(1) 入口 `dbxtest.Compare(t, db, use, scenario)` 的 `dbx.DB` 句柄和 `use` 注入函数（thingmodel 这个文件里只出现 2 次，devicemetadata `store_*`/`group_*` 各只有 1 次引用）；(2) 清理用的 `d.Exec(ctx, "DELETE … WHERE tenant_id=$1", …)`；(3) `dbxtest.NewTenantAudit` 通过驱动观察 SQL 检查“每条语句都有 tenant_id”（这个要换成 GORM logger/callback，或直接放弃，改由 repository 单测保证）。
- 因此 d1 的 27 个 `*mysql*`/`*contract_test.go` 文件（`git diff --name-only develop..d1`）里，**场景与期望值可整体搬走**，改动是：`dbx.DB` → `*gorm.DB`、`$1` → `?`、`Compare` → `reltest` 的等价入口。`T1-CARDS.md` §0 第 5 条已按这个思路写；我的核对结论是**这条路可行**，可靠性高于“重写场景”。
- d1 列出的缺陷清单（`docs/MYSQL-PROFILE-STATE-2026-11-04.md` “Defects”）里，**与 MySQL 语义有关而非翻译器造成的**那些（`SKIP LOCKED` 在无索引时锁全部候选行、`UPDATE…FROM…RETURNING` 领取、`DISTINCT ON`、`COLLATE "C"`、`before` 保留字、唯一冲突 1062 分类、GORM 丢失条件 upsert 的 WHERE 等）会**原样再遇到一遍**；这些是 d1 最值钱的经验，应写进各卡的“双实现清单”模板。
- d1 缺口也会原样带过来：命令“提交到运行时下发”、OTA 推送、casbin 完整策略未覆盖（`MYSQL-PROFILE-STATE` “Known gaps”）。

### 3.2 老代码的 GORM 多库 / TDengine 适配能不能参考

- **GORM 多库**：不能。老代码没有。要参考 GORM 多方言，现成的是 develop 里的 `internal/dal/json_dialect.go`、`notification_source_outbox.go`、`history_intent_mysql.go`、`upsertCurrentMySQL`（这几个已经在 develop 上以“repository 内双实现”写好）。
- **TDengine 适配**：不能。老代码是 gRPC 读代理。现成的是 `internal/tdenginehistory` + `internal/storage/history_tdengine.go`。

---

## 4. 问题四：对 26 张卡的影响

当前状态：A0、A1 已完成（4 人天）。剩余 24 张卡约 51.5 人天。

### 4.1 必需，不变

- **B1–B6（devicemetadata，18 天）**：占 77/153 的双写，唯一没捷径的部分。场景可搬（§3.1），但 GORM 改写量、MySQL 触发器/CHECK 替代仍要手写。
- **C1（thingmodel）、C3（northbound）、C4（fileassets）、C9/C10（automation）、C11（authcore）、C2（platformconfig）**：缺一个，对应服务在 MySQL 上起不来。
- **D1（删翻译器）、D2（防回归检查）**：规则要求，且很便宜。

### 4.2 可缩小 / 合并 / 前移

| 卡 | 建议 | 理由 | 省 |
| --- | --- | --- | --- |
| **T3** | **提到最前**，不等 A2/A3 | PG+TDengine 不依赖关系库改造；只是目录冲突才被排后。先做则冲突消失（A2/A3 之后再改 `dal` 时 T3 已合入） | 0（但提前交付） |
| A2 + A3 | 合并为一张“平台根 SQL 清扫”，估 5 天 | 双写只有 15 条；A3 的 5 条里 `upsertCurrentMySQL`、`history_intent_mysql` 已存在。拆成两张是为了守 3 天上限，不是因为工作量 | 1 |
| A4（平台验收） | 并入 D3 | 1 天的“把前面跑一遍”，D3 本来就要跑冒烟 | 1 |
| D3（四组合冒烟） | 缩成“MySQL ×2 脚本”；PG+TDengine、MySQL+TDengine 两格放进 T3 / 最后一步 | 避免两个任务各跑一遍同样的栈 | 1 |
| C5 + C6（notification，6 天） | 合计下调到 4–4.5 天 | 140 条里只有 10 条双写（8 条是 `SKIP LOCKED`），`::` 仅 2 处，d1 的 31 步场景现成 | 1.5–2 |
| C7（connectorcatalog，1 天） | 并入 C4（同样的 JSON/咨询锁模式） | 4 条语句 | 0.5 |
| C8（marketinstall） | 保持 2 天，但降优先级 | 已有 MySQL store，可晚做 | 0 |
| **合计** | | | **约 5–6 人天** → 剩余约 45–47 天 |

### 4.3 需要新增（卡里没有）

1. **tptodb 旧分支处置**（0.5–1 天，并入 T3）：删掉 `internal/dal/telemetry_datas.go`、`telemetry_current_datas.go` 里的 `grpc.tptodb_type` 分支和 `internal/app/grpc_service.go`，配置里的 `grpc.tptodb_*` 一并移除，或明确保留为一个可选 adapter 并补契约测试（任务书 S04 的要求）。
2. **平台 PG-only 尾巴**（并入合并后的 A2/A3，约 1 天）：`device_latest_telemetry.go:26-28`、`VACUUM FULL`、`EXPLAIN` 估数（§2.1）。
3. **MySQL 档的历史存储定位**（决定 D1，见 §5）。

### 4.4 有没有更省事、又不违规的路线

**没有数量级上的捷径**：Encore 服务那 153 条双写和 `devicemetadata` 的 GORM 改写是实打实的工作，且禁止“运行时改写”恰好排除了最省事的做法（也就是 d1 的做法）。下面这些**可行且合规**的省法：

| 做法 | 说明 | 节省 | 风险 |
| --- | --- | --- | --- |
| **分档交付** | 先交付 PG+TDengine（3–4 天），MySQL 作为第二阶段；对外立刻有“可选时序库” | 对“第一个可用交付”的等待从 ~30 天降到 ~4 天 | 无 |
| **机械改写前置、双写后置** | 每个包先做占位符/`::`/`ANY` 的机械改写（可批量、可由脚本辅助——是**一次性改源码**，不是运行时翻译），再逐条处理双写 | 降低每卡中间态不可编译的概率 | 机械改写脚本不入库，要人工复核 |
| **MySQL 档先不含 devicemetadata 的“边角”能力** | 如读数异步导出、模拟器、调试会话（B5a/B5b 共 5 天）在 MySQL 档返回明确 `unsupported`（任务书 §3 允许“启动拒绝或明确 unsupported”），推迟到发布后补 | 最多 5 天 | 功能缺口需要写进发布说明；用户期望“完整”的话不可接受，**需总监决定** |
| 用 marketinstall 的现成 MySQL store 作为模板 | 它已经是 `database/sql`+手写 MySQL，7 个测试 | 对 C8 省 ~1 天 | 无 |

其余想法（例如“统一 MySQL 兼容层/ORM 再包一层”）都会落回翻译器或新 ORM，**违反规则，不建议**。

---

## 5. 问题五：建议

### 5.1 需要先拍板的决定

| 编号 | 问题 | 我的建议 |
| --- | --- | --- |
| **D1** | MySQL 档的历史存哪里？现状是 `history_backend=postgres` 就写 `global.DB`，MySQL 档会落进 MySQL 普通表（无 hypertable、无压缩、保留期靠 DELETE） | **MySQL 档推荐 TDengine；MySQL+TimescaleDB 第一期不做**（需要第二条 PG 连接 + 独立 Timescale adapter，任务书 S02，约 +5 天）。MySQL 档下“历史写 MySQL 表”保留为“小规模/开发用”，文档写明 |
| D2 | B5a/B5b（导出、命令执行、模拟器）在 MySQL 档是否允许首发 `unsupported` | 不建议；除非工期紧。默认完整做 |
| D3 | tptodb：删除还是保留为可选 adapter | **待定，见 §7.3**：企业版 `main` 默认 `tptodb_type: TSDB`，在用。原建议“删除”已撤回；改为先定企业版是否迁到 `HistoryBackend` |

### 5.2 分几步

**步骤 1：PG + TDengine 转正（3–4 天，先做，不动关系库）**
- 内容：T3 全部（整栈验证、§2.4 语义差分、去 `experimental`、`DATABASE-SWITCH.md` 时序一节）+ tptodb 旧分支清理。
- 验收：
  1. 安装器 tdengine profile 整栈：前端走历史页/导出/统计，冒烟 `smoke_history_chain.sh` 通过（记录写入 `/Volumes/80G/archive/evidence/...`，仓库一行索引）；
  2. 语义差分表（毫秒精度、UTC、同 key 同时间、空桶、diff、区间端点）在 TimescaleDB 与 TDengine 上逐项对照，差异写明；
  3. `history_backend=tdengine` 不设 `experimental` 即可启动；用“主库放哨兵、TDengine 放不同数据”的方法证明历史读只访问选中的库；
  4. tptodb 旧分支：按 §7.3 的决定处理（删除，或保留为命名明确、有契约测试的可选 adapter）；`TP_TDENGINE_TEST_URL` 下相关测试全绿，**无该变量时的 Skip 要在完成说明里写明**。

**步骤 2：PG → MySQL 关系库（约 45–47 人天，两条线约 25–28 个工作日）**
- 2a 平台根 SQL 清扫（合并 A2+A3，5 天）→ 同时 B1 起步（devicemetadata 单线，不拆）。
- 2b 小模块与 B 线并行，先易后难：C2、C11 → C3、C4+C7 → C5/C6 → C1 → C8 → C9/C10。
- 2c 最后 D1（删翻译器）→ D2（防回归）→ D3（MySQL 两个脚本 + A4 汇总）。
- 验收：
  1. 每个包的 `reltest` 场景在 PG 与 MySQL 逐步一致（场景取自 d1，步数列在各卡）；跳过的写原因；
  2. `grep -rn 'pkg/sqldialect\|pkg/dbx\|pkg/dbdialect' --include=*.go` 为空；D2 的防回归检查有正反例；
  3. `smoke_channels.sh`、`smoke_history_chain.sh` 在 MySQL（历史写 MySQL 表）上通过；
  4. 任一卡超过 3 天按 `CLAUDE.md` 停下拆小；`devicemetadata` 与 `automation` 的估时最不确定（见 §6）。

**步骤 3：MySQL + TDengine（2–3 天）**
- 内容：步骤 1 + 步骤 2 之后，跑 MySQL+TDengine 无 PG 整链（上报→历史→统计→导出→删设备清理），补尾巴。
- 验收：**没有任何 PG 实例可达**时，平台与 Encore 服务启动并完成闭环（任务书 §3 第四行）；broker 控制面仍 PG-only，需在文档里写明这一格“平台数据路径无 PG，broker 控制路径需 PG”（`MYSQL-PROFILE-STATE` 已承认这一点）。

**不做（本次范围外）**：MySQL+TimescaleDB（等 D1）、broker 控制面 MySQL、InfluxDB、Timescale↔TDengine 历史搬迁。

### 5.3 先做哪个组合：PG+TDengine

理由：(1) 零关系库改动，风险最低；(2) 代码已经存在且有真实库差分测试；(3) 对用户可见：安装器已有 tdengine profile；(4) 它顺带清掉 tptodb 这个第二套机制；(5) 它排在 A2/A3 之前，反而**消除**了原计划里 T3 与 A2/A3 的目录冲突。

---

## 6. 不确定项与我没验证的东西

- 盘点文档的语句数是下限；`devicemetadata` 与 `automation` 最可能超估。我只抽查了平台根的 `Raw/Exec` 调用点与几个双写原因，没有重数 Encore 的 833 条。
- 没有重跑任何 TDengine 或 MySQL 测试（环境变量未配置，且规则要求不动运行中的 docker 栈）；“真实 TDengine 3.4 上测过”引自 `PROJECT-AUDIT-2026-11-04.md:82`。
- tptodb 服务端：初稿时未查到，已在 §7 补上（企业版仓库）。它对 Kingbase/PolarDB 的支持：**未查到**，代码里只有 TDengine。
- `main-1.3.0:test/multidb/docker-compose.yml` 的用途未细读。
- 外部任务书原路径 `/Volumes/500G/docs/...` 本机未挂载，我读的是仓库内副本 `docs/closeout-20261102/inputs/...`（日期同为 2026-11-01，**未逐字比对是否与原文件一致**）。R01–R09、S01–S05 读了 R01、R02、S01–S05；R03–R09 只看了标题。
- 安装器仓库 `thingspanel-installer` 的 tdengine profile 本次未读（只引用审计文档的描述）。
- 工作量估算基于卡片文档和 d1 经验，不含评审、返工和合并冲突时间；“≈25–28 个工作日”假定两条线持续满载。

---

## 7. 企业版（`ThingsPanel/thingspanel-backend-enterprise`、`ThingsPanel/thingspanel-TDengine`）

> 追加于同日。来源：`gh repo clone`（只读，克隆在会话临时目录，未入库）。企业版后端 `main@a4a2a77`（2026-11-04）、`dm-20260520@76f6cbf`；TDengine 服务 `thingspanel-TDengine@fbb3ba6`（2026-09-24，34 个提交，首个提交 `3ce9046` 2024-06-09）。**§0–§6 里“tptodb 服务端未查到”的说法以本节为准。**

### 7.1 企业版实际有什么

| 能力 | 企业版现状 | 证据 |
| --- | --- | --- |
| **TDengine** | **有完整的独立服务 `thingspanel-TDengine`（tptodb 服务端）**：订阅 MQTT（`devices/telemetry/processed`）或 Kafka，经管道（缓冲、批量、背压）写 TDengine 超级表+按设备/指标建子表；同时提供 gRPC 查询（端口 50052）。**写入是它自己消费消息，与平台写 PG 并行，不经过平台事务** | `thingspanel-TDengine:docs/项目的全局认知.md`；`conf/config.yaml`（`data_source`、`mqtt.attribute_topic`）；`db/db.go:77-145`（`taosRestful`+zorm，`createTdStable`）；`pipeline/*.go` |
| 平台怎么用它 | 企业版后端 `main` 的读取分支与社区版 v1.1.11 **同一份代码**：`grpc.tptodb_type: TSDB`（`conf.yml:65`），读走 gRPC；写仍是 `query.TelemetryData.Create`（PG） | `enterprise:main:internal/dal/telemetry_datas.go:24-25,51-52,88-89`；`configs/conf.yml:65`、`conf-dev.yml:66`；同样的 5 个 RPC 调用 |
| 保留期 | `db.keep_days`（默认 365，配置里为 7） | `thingspanel-TDengine:db/db.go:150-153`；`conf/config.yaml:68` |
| gRPC 接口 | 8 个 RPC（与社区客户端 proto 一致），实现 1115 行，带聚合测试（`data_statistic_test.go`：窗口真实时间、空结果、缺窗口时间报错） | `grpc_tptodb/tp_to_db.proto:29-53`；`grpc_tptodb/server/data_*.go` |
| **删除设备历史** | **没有**对应 RPC，也没有按设备删除的实现（`DROP` 只用于重建超级表） | `grpc_tptodb/tp_to_db.proto`（无删除 RPC）；`db/db.go:172` |
| **达梦（DM）** | 只在分支 `dm`/`dm-20260520` 上，**4 个提交（2025-08-18 至 2026-05-20），未合入企业版 `main`**。用 `github.com/godoes/gorm-dameng v0.7.1`，配置 `db.type: postgres|dm`；`sql/dm-1…dm-10.sql`（`dm-1.sql` 1798 行）由 `sql/sql_add_quotes.py` 给表名/列名加引号生成 | `dm-20260520:go.mod:17`；`initialize/db_init.go`（332 行，`PgConnect`/`DBConnect` 二选一）；`initialize/db_exe_dmsql.go`；`如何使用不同数据库.md` |
| **MySQL / Kingbase / PolarDB** | **企业版也没有**：`go.mod` 里 MySQL 仍是 `// indirect`（`main:go.mod:103`）；`KINGBASE/POLARDB` 仍只是和社区版相同的字符串分支；全组织代码搜索 `kingbase`/`polardb` 只命中社区版 dal、README 和文档站 | `gh search code --owner ThingsPanel kingbase/polardb`；`thingspanel.github.io:docs/installation/environmental_requirement.md`（文档写“支持 PolarDB、KingBase 等国产数据库”，代码里没有对应驱动） |

### 7.2 企业版的达梦多库做法——是反例，不是模板

- **做法**：在业务代码里直接写 `if viper.GetString("db.type") == "dm" {…} else if … == "postgres" {…}`。`dm-20260520` 里 `db.type` 分支散落在 8 个文件：`initialize/db_init.go`(3)、`internal/dal/alarm.go`(6)、`telemetry_datas.go`(6)、`telemetry_current_datas.go`(2)、`ui_elements.go`(8)、`users.go`(2)、`mqtt/subscribe/{mqtt_client,telemetry_message}.go`(1+2)。
- **质量问题（已在代码里看到）**：`internal/dal/users.go` 的 `GroupByMonthCount` 在 `dm` 分支里嵌套了一个永远不可达的 `else if … == "postgres"`；`alarm.go` 的设备过滤在 `dm` 下用 `DBMS_LOB.INSTR` 做字符串包含代替 JSON key 判断（语义变弱，设备号是另一个设备号的子串时会误命中——这是我从代码读出的推断，未实测）；两个新增分支在 `db.type` 非 `dm`/`postgres` 时静默不加过滤条件。
- **分支处于不可合并状态**：与 `main` 的基线相差 583 个文件（分支基于很旧的 `fcf00c5`，只有 4 个提交，改动面 35 个文件 +3923/−351）。
- **规模比 MySQL 小得多**：只改了 3 个 dal 文件加启动，没碰 Encore，因为企业版**没有 Encore 服务**（`go.mod`、目录中无 `encore-api`；这点基于目录结构，未逐项核对）。
- **与本项目规则的关系**：`CLAUDE.md` 要求“GORM 表达不了的语义，在所属 repository 里分别写 PG 和 MySQL 两个显式实现”。达梦做法是**反过来**——在调用点用全局配置分支，且迁移靠脚本给 PG 的 SQL 机械加引号。**可借鉴的只有一件事**：达梦的 `sql/dm-*.sql` 手写迁移 + 版本号表的思路和我们 `sql/mysql/*.sql` 一样，没有新东西。**不可借鉴**：分支写法。

### 7.3 企业版对前面结论的修正

1. **“老代码没有直连 TDengine”依旧成立，但“TDengine 写入/删除/保留期在平台之外、未查到”要改**：写入是 `thingspanel-TDengine` 服务自己订阅消息写的；保留期靠 `keep_days`；**删除设备历史没有实现**（对应任务书 §2.5 的跨存储删除问题，企业版从来没解决）。
2. **tptodb 不能直接删（修正 §5.1 的 D3）**：企业版 `main` 的默认配置就是 `tptodb_type: TSDB`，说明企业版客户在用。社区版新的 `HistoryBackend`+意图重放（平台写入 TDengine REST，带幂等和持久删除）是**另一套架构**。若社区代码里删掉 `third_party/grpc/tptodb_client`，企业版与社区版的 dal 差异会进一步扩大。**这是产品策略问题，需要负责人定**：(a) 企业版也迁到新的 `HistoryBackend`（tptodb 服务退役，其 8 个 RPC 的语义改由 `tdenginehistory` 承接）；(b) 两边并存，社区版保留 tptodb 客户端为一个明确命名的可选 adapter（任务书 §2.4 “保留 tptodb 的条件是封装成普通 adapter 并验证同样的契约”）。
3. **有一个现成的语义参照**：`thingspanel-TDengine` 的 `grpc_tptodb/server/data_statistic.go` 与测试是线上跑过的 TDengine 聚合/分页语义。T3 的“语义差分”（毫秒精度、空桶、diff、区间端点）可以把这份实现作为第三方对照，而不是只对 TimescaleDB 单边对比。**我没有逐条对比过两套 TDengine SQL，这是建议，不是结论。**
4. **“企业版有很多数据库兼容”**：按代码证据，企业版实际有的是 **PG(+Timescale)、TDengine（外置服务）、达梦（未合入分支）**；MySQL、Kingbase、PolarDB 在代码里**未查到**，只在文档站和 README 里宣称。若企业版另有未在 `ThingsPanel` 组织下、或在我没看的分支（`irrigation*`、`water`、`sanze`、`customer-xwzl` 等，我只对 `main`、`dm`、`dm-20260520`、`cloud`、`nfdw`、`tp-community-nfdw` 的 `go.mod`/配置做了检索）里的实现，请告诉我仓库或分支名，我再看。

### 7.4 对建议的影响

| 项 | 原建议 | 修订 |
| --- | --- | --- |
| 先做 PG+TDengine | 3–4 天 | **不变**；T3 增加一项：把 `thingspanel-TDengine` 的聚合语义作为差分对照（+0.5–1 天） |
| tptodb 处置（D3） | 删除 | **改为待定**：先定企业版去向（7.3 第 2 点）。若选 (b)，社区版只需把 `grpc_service.go` 的无条件初始化改成“`tptodb_type` 非 NONE 才初始化”，并对 `internal/dal` 里的分支加契约测试，约 1 天 |
| 达梦 | 未涉及 | **不纳入本次范围**。若要把达梦也做成可选关系库，应走 `pkg/relational` 新增一个 dialect，而不是搬 `db.type` 分支；工作量估算：未估（需要先确认达梦对 GORM-gen、触发器、`SKIP LOCKED` 的支持，我没验证）。 |
| MySQL 档 | 先 TDengine | 不变。企业版没有 MySQL 实现可借鉴，也就没有“可复用 MySQL 代码”的捷径 |
