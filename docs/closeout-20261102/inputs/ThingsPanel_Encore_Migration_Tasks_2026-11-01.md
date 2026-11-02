# ThingsPanel 社区版 Encore 改造任务书

> 审计日期：2026-11-01（北京时间）。基线：`ThingsPanel/thingspanel-backend-community` 的远端 `main`，提交 `7f694779c979702740b15125164ebec2545604a4`，提交时间 2026-10-29 23:42:19 +08:00。
>
> 用途：技术负责人确定公共边界后，按任务编号分派给开发者或编码助手。本文包含现状证据、通用设计、35 个任务包、100 张接口小卡、每包提示语、验收门槛和逐接口台账。所有任务初始状态均为 **待实施**；“已有实现”表示本次看到了代码，不表示本次重新运行验收。

## 0. 怎么使用这份任务书

**阅读导航：** 第1节看现状与优先问题；第2～3节看公共设计与数据库矩阵；第4节排依赖；第5～6节复制任务提示语；第7节验收；附录A/B追接口和代码；附录D直接选择API小卡。

1. 先读第 1～4 节，明确现状、唯一职责所有者和数据库语义。不要只复制一段提示语就改架构。
2. 先做 `T00、T02.a` 冻结身份和资源授权，再做 `T01` 样例；并行修复 `T03、T04、T05`。`T02.b` 形成统一协议适配后，再批量迁移接口。
3. 给开发者发送第 5 节的“总提示语”，再加上第 6 节 **一个任务包中的一个限定子任务**。一个 PR 只完成一个表族、一个驱动或一组同类端点；不要同时切 HTTP 框架和数据库引擎。
4. 标为“负责人先定”的事务、锁、跨库和真实设备动作任务，需要负责人先提供不变量与反例。经验较少的开发者负责已明确边界的实现，不能自行决定重试、去重或切流语义。
5. 用附录 A 的每一行路由核对迁移去向。删除旧入口前，必须有新入口、鉴权、契约和入口分流证据。已经退役的入口做清理，不重新实现旧写入能力。
6. 本文件给出的是建议实施方案。首批数据库品牌组合是为了形成可执行最小范围；具体版本在 `T00` 冻结。其他引擎按同一契约增加适配器和认证记录。

## 1. 审计结论与范围

### 1.1 当前不是“Encore 里还嵌着一批 Gin”

当前是 **独立的 Community Gin 应用与已经原生实现的 Encore 服务并存**。根模块名为 `project`，`encore-api` 是独立的 `encore.app` Go 模块。`RouterInit` 仍使用 `gin.Default()`；完整非测试 Encore Go 源码中没有发现 Gin import。[C01][C02]

| 检查项 | 本次结果 | 应怎样理解 |
| --- | ---: | --- |
| 主启动路由图中的 Gin 常规 HTTP method+path | 343 | 包括仍承担业务、为 Encore 提供来源适配、按开关拒绝、已经返回 410 的入口；不是 343 个都要重写 |
| Gin 静态资源方法 | 2 | `StaticFile` 的 GET/HEAD 单列 |
| 仅 `AUTOMATION_COMPAT_MODE=shadow` 挂载的 Gin 路由 | 12 | 属于条件兼容面，单独安排退出 |
| 独立 `cmd/automation-compat` 演练程序 | 13 | 不计入主程序 343；退出演练资产时处理 |
| Encore `//encore:api` 声明 | 183 | 声明数量；多 method、通配路径不能等同业务接口数量 |
| Encore 声明展开后的 method+path 条目 | 194 | 包含通配条目；不可用来计算迁移完成百分比 |
| 当前镜像选择的 Encore 服务 | 4 | `authn,thingmodel,automation,notification` |
| 已有源码但未列入该镜像选择的服务 | 2 | `devicemetadata,northbound`；源码存在不等于已发布 |

Gin 台账按真实注册调用图展开前缀和循环，剔除注释与未调用的 `DeviceAuth.Init`，不是对 `.GET(` 的简单计数。附录 A 给出 357 个主/条件条目，附录 B 给出原生服务现状。发布服务选择证据为 `.github/workflows/encore-image-build.yml`。[C03]

### 1.2 已有成果应原位复用

| 能力 | 已有实现/唯一职责所有者 | 本次真正要做的事 |
| --- | --- | --- |
| 原生物模型 | `encore-api/thingmodel/core`、`app`、`postgres.go`；定义、修订、快照、幂等 | 扩展存储适配；处理旧模型写入口与消费者收尾；不造第二个模型核心 |
| 认证 | `encore-api/l1-access/authn`、`internal/authcore` | 收口身份库、会话与旧入口；raw cookie/重定向接口可以保留 |
| 自动化 | `encore-api/automation` 的规则、run、outbox、lease、恢复、tick | 统一所有触发入口的 owner；补原生嵌套场景；保留已有调度/恢复 |
| 通知 | `encore-api/l3-core/notification` 的 Core/Store/Worker/插件 | 收口 Community 来源与会话桥；保留回执、unknown 和供应商重试边界 |
| 设备接入与控制 | Community 的 `broker*`、`adapter`、`uplink`、`downlink`、dispatcher | 迁移 HTTP 适配与数据访问；保持唯一接入、受理和控制执行者 |
| 北向 | `encore-api/northbound` 的 inbox、worker、lease、限额 | 核对来源接线、补发布和数据库适配；不重建转发平台 |
| 市场安装 | `internal/service/market_native_install.go` 等 | 迁移已有导入/快照读回/绑定状态机；Horizon 仍是外部市场 owner |
| 生命周期 | `internal/app/ServiceManager` 和 MQTT StartContext/Stop | 补遗漏资源的关闭和就绪判断，不重建容器或生命周期框架 |

依据见 [C04]～[C12]。历史文档中的“未完成”条目可能已经被后续代码补齐；本任务书以固定提交的调用链为准。

### 1.3 优先问题

1. **设备删除顺序有租户隔离风险。** `DeleteDevice` 先按设备 ID 删除关联数据，最后才对设备表加租户条件；DAL 未检查零行删除，服务还忽略 `Commit` 错误。应在任何副作用之前验证可信 tenant+device，并在提交成功后再清缓存/断连。属于静态调用链确认的风险，本次没有在运行库执行越权复现。[C13]
2. **遥测批量失败后的 fallback 存在数组错配。** 历史点按 `(device,key,ts)` 去重，最新值按 `(device,key)` 生成 map；fallback 却按相同索引访问两个数组。批量失败且同 key 多时间点时，可能越界；不同 key 也没有顺序对应保证。禁止用简单越界判断掩盖漏写。[C14]
3. **自动化模式还存在两套处理方式。** 严格 ingress 路径已有持久 effect、身份和 receipt 校验；旧 `nativeForwarder` 缺 URL 会回退 legacy、队列满丢弃、只看 2xx。旧 cron 还无条件注册旧自动化任务。应复用可靠路径并统一全部触发入口；本次未观察实际设备重复动作。[C07][C15]
4. **关系库兼容范围远超 `PgConnect`。** 还包括 Encore 的 `device_metadata/automation/notification/northbound`、authcore 的独立 `pgx` 连接、Broker 锁/账本、通知来源 outbox 和所有迁移执行器。[C05][C16]
5. **WebSocket的认证与设备归属不完整。** 批量状态与当前遥测订阅路径存在身份验证后未逐设备核验租户归属的调用链，优先在T02.a修补；本轮未做运行利用验证。[C23]
6. **时序读写配置分裂。** 部分读走 `tptodb`，写入、导出、统计、删除和清理仍可能直接访问主库。已有 `Storage` 仅是生命周期接口，并不是完整的时序读写端口。[C17]

### 1.4 审计边界

- 完整 Git tree 为 1,777 条；取得并校验了 979 个源码/SQL/发布相关文件的 Git blob SHA，覆盖全部非测试 Go 文件。进行了全量路由登记、全树静态搜索与关键调用链审查，并非逐行形式化证明。
- 本轮没有启动后端、连接业务数据库、运行设备控制、发送通知或触发部署。没有把编译、mock、历史验收记录当作本次真实集成测试。
- 你的本地 Community 工作区当时落后远端并含未提交修改，因此没有用它覆盖、切换或代表最新 `main`。审计采用固定远端提交。
- 当前生产 ingress/环境变量/客户存量数据、前端实际版本与 installer 当前源码未在本轮完整核验；对应验证已落入任务 `T00、Q02、Q03`，不能据本文件直接宣布可发布。

## 2. 简单、可靠的通用设计

### 2.1 四个清晰边界

| 边界 | 做什么 | 不承担什么 |
| --- | --- | --- |
| HTTP/协议适配 | Encore typed 或 raw 解析请求、身份、校验、响应；MQTT/定时入口转换输入 | 不写业务 SQL，不另实现业务规则 |
| 现有业务模块 | 用例、授权、状态机、领域不变量；接受 `context.Context` 和可信身份 | 不依赖 `gin.Context/ResponseWriter`，不按数据库品牌分支 |
| 所属模块的数据访问 | 业务 repository/store/DAL、该 owner 的事务与并发语义 | 不跨服务任意改表，不提供万能 CRUD/SQL 解释器 |
| 基础设施适配与装配 | 连接池、方言实现、历史存储驱动、生命周期、配置与迁移 | 不变成新的网络“数据库服务”，不复制物模型/通知/自动化核心 |

**每种业务只有一个写入负责人。** 过渡期可以保留单向、显式的协议适配，但新 owner 不可用时返回明确错误，不能偷偷切回旧写路径。旧记录、旧只读页面和旧执行器是三件不同的事：保留历史记录不代表继续运行旧引擎。

普通 JSON 接口逐组采用 typed API；WebSocket、SSE、文件上传下载、签名回调、cookie/重定向可以使用 Encore raw + `net/http`。是否使用 raw 不作为“未完成”标准；真正的门槛是生产调用链是否仍依赖 Gin、鉴权和业务契约是否完整。Encore 官方允许这类 raw 场景。[O03]

沿用已有 API 错误码、空值、分页、时间与权限语义。不要试图用一层全局 middleware 把所有 Encore response 强行改成另一种类型；对外 DTO 明确定义旧 envelope，raw writer 与 typed adapter 共享错误语义。

### 2.2 源码所有权与两个 Go module

`internal/service`、`internal/dal`、`internal/repo` 中未发现直接 Gin import，因此不安排“重写整个 service 层”。真正的框架耦合集中在 API/middleware、`pkg/global` 的响应/SSE对象及传递依赖；另有大量全局 DB/Redis 和 `context.Background()` 需要按调用链收口。[C01][C18]

根模块和 Encore 模块的 `internal` 可见性不能靠错误 import 或复制目录解决。采用 **按领域转移唯一实现**：先冻结纯 Go 端口与构造依赖，再移动所属用例/存储实现，旧调用者改为显式适配或入口分流；完成切换即移除旧实现。已有原生模块留在原位。本轮默认不增加第三个共享Go module；只有必要的Encore薄host目录，没有重复业务实现。具体最小跨模块探针在 `T01` 验证后固定，不让每个任务自行创建共享框架或同名核心。

`devicemetadata` 不能整包启用来替代现有设备运行层：它有另一套 MQTT bridge，而且正式模型 owner 已是 `thingmodel`。也不能直接整包删除：`thingmodel` 通过 `sqldb.Named("device_metadata")` 复用了它声明的逻辑资源与迁移历史。先拆 host 与必要资源，再退出重复路由/订阅。[C05][C11]

### 2.3 关系数据库设计

**推荐目标：生产多引擎模式使用应用自管的 `database/sql`/现有 GORM，以及明确的迁移步骤；Encore 负责 API 和服务承载。** 官方 `sqldb` 原生支持 PostgreSQL，换 DSN 不能把它变成 MySQL 驱动。自管外部驱动可以接入，但连接、迁移、观测和测试责任需要自己承担。[O01][O02]

实施规则：

- 保留每个 owner 的逻辑库和表所有权；可共用服务器实例，不靠共享表绕过模块边界。
- 每进程、每逻辑资源只创建一个连接池，谁创建谁关闭；应用用例不直接创建连接或读环境变量。
- 普通 CRUD 继续用已有 GORM/DAL；特殊 JSON 查询、条件 upsert、批量返回、队列领取在所属 repository 里保留小范围 PostgreSQL/MySQL 实现。
- 公共技术能力只处理连接、方言标识、错误分类和迁移协调。禁止做 SQL 字符串翻译器，禁止新增第二套 ORM 或 `Database.Execute(any)` 大接口。
- 各 owner 已有 Repository/Store 优先扩展。物模型事务必须继续覆盖模型修改、回执、审计与事件；通知/自动化同样保留各自不变量。
- 生产切换到自管数据库时，同步迁出/移除必需的 `sqldb.NewDatabase/Named` 静态资源声明，保留历史资源身份与迁移映射。不能让“选择 MySQL”的产物仍要求一台隐藏 PG。临时 PG 专用构建若保留，必须显式命名，不能成为最终默认隐藏依赖。
- 数据库锁替换按语义实现：优先锁已有稳定实体行；无实体时在 owner 内用锁键记录与事务行锁。不得把 MySQL 会话锁当 PG 事务锁；统一加锁顺序，重试只能包住完整、无外部副作用的数据库事务。
- 已执行的 PostgreSQL migration/checksum 不得改写。每个 owner 的同一 schema 只能有一个迁移执行者；新引擎用自己的 baseline/增量，并校验与旧数据语义一致。

### 2.4 时序数据库设计

第一期只把 **遥测历史** 定义为可替换时序后端；最新遥测快照、当前属性、期望值、指令/任务回执和业务状态继续归关系库 owner。事件当前也是关系表，先通过所属 repository 做关系库兼容；若以后迁事件历史到时序，单独扩展端口与迁移，不把这件事藏在遥测适配器里。

在现有存储职责上收口三种能力即可，均为普通 Go 接口，不新增服务进程：

| 能力 | 必需操作 | 说明 |
| --- | --- | --- |
| 遥测历史 | 批量写、范围/游标读、聚合、删除、健康、关闭 | 所有历史查询、导出、统计走同一个已选择驱动 |
| 当前状态 | 读取快照、仅在更晚时间/允许的相同时间规则下更新 | 关系库内保持事务；不让各历史驱动各藏一份不同语义的最新值 |
| 保留期执行 | 应用发策略，adapter 执行 chunk/TTL/DELETE，返回进度 | 业务代码不直接执行 `VACUUM FULL` 或供应商专属管理 SQL |

接口的输入应是已校验的 tenant、device、key、时间和类型；输出必须含明确错误、部分成功结果或能力不支持。接口不要暴露 `*gin.Context`、`*gorm.DB`、`*sqldb.Tx`、供应商 ResultSet。

必须在 `S01` 冻结以下契约：毫秒时间精度、UTC存储/展示时区、数值/布尔/文本/null、同 key 同时间的冲突规则、乱序、排序稳定性、分页与总数、空桶、插值、聚合名称、diff 定义、区间端点、批量部分失败、数据清理完成状态。**现有不同端点的语义可能本来就不一致，先按端点保留；改变语义需要显式版本变化。** 例如现有 diff 同时出现 MAX-MIN 和末值减首值，不能让驱动作者随意统一。[C17]

保留 `tptodb` 的条件是把它封装成一个普通 adapter，并验证同样的读写/删除契约。配置字符串出现 `TSDB/KINGBASE/POLARDB` 仅证明存在分支；本仓库没有足够证据证明外部服务所有能力可用，不能据此标注“已支持”。

### 2.5 跨存储一致性与删除

现有可信遥测会在一个 PG 事务中处理路由 fence、归属、历史、最新值、receipt 与 automation effect。历史移到外部时序后，这些操作不再共享数据库事务。[C19]

推荐扩展现有接入 owner 的 ledger/effects：关系事务持久化可信受理、必要当前状态与历史写入意图，现有 worker 结构增加对应 effect，按稳定 receipt/key 投影到时序。禁止新增万能总线，禁止网络调用放在关系库长事务中，禁止把“入队”写成“历史已可查”。

需要显式区分：已受理、历史已持久化、自动化已受理、设备动作已确认。若旧契约要求历史提交后再触发自动化，effect 依赖关系必须保留这一顺序；若需要调整，先变更契约并验证消费者。普通 best effort 遥测和可靠属性/事件分别处理，不能未经设计把所有高频遥测都变成可靠队列承诺。

路由 epoch 切换与设备删除必须有屏障：不能在还有未知/未完成旧世代写入时，把旧凭据停用或关系删除误当成外部写已被取消。先停止旧世代受理，记录待处理写的决议，排空或对账，再完成切换；设备删除使用可信 tenant+device、墓碑/世代号与可重跑删除意图，旧消息重放不得复活历史。`S05` 先由负责人冻结该协议；不能用“加了 outbox”代替原有 fence 语义证明。发送前检查不能取消已在途未知写；接收端需拒绝失效世代，或把设备删除/重建世代纳入存储身份并强制读侧隔离，确认旧写静止后再完成物理清理。否则保持pending，不能因超时或队列为空宣告完成。接入route epoch与删除世代分开建模，合法旧route历史不应被一并过滤。

跨引擎数据搬迁第一期采用维护窗口：停相应写入、导出/checkpoint、转换导入、对账、切配置、恢复；中断可重跑。在线双写、自动冲突仲裁和跨库分布式事务不在第一期引入。

## 3. 首批支持矩阵与验收定义

以下为推荐实施范围，具体版本/授权版别由 `T00` 锁定后填入发布清单。它们是目标，不是当前已经兼容的声明。

| 关系库 | 时序历史库 | 必须证明 |
| --- | --- | --- |
| PostgreSQL | TimescaleDB | 现有主要功能保持；关系库不被强制安装 Timescale 扩展；可同实例或分实例 |
| PostgreSQL | TDengine | 所有历史读写/导出/聚合/删除确实访问 TDengine，当前状态仍一致 |
| MySQL | TimescaleDB | 所有业务逻辑库使用 MySQL；PG 只因 Timescale 时序后端存在 |
| MySQL | TDengine | 没有任何 PG 实例可达，Community/发布清单中的 Encore 服务仍可启动并完成业务闭环 |

PostgreSQL 兼容品牌、SQL Server、Oracle、InfluxDB 等作为后续认证条目。不能因 wire protocol 或品牌说明兼容就复用“通过”结论。对必需能力不支持的驱动，启动拒绝或返回明确 unsupported；不允许默默回退旧数据库、返回空数据或固定成功值。

每个组合都要覆盖：新装、适用的版本增量升级或跨引擎数据搬迁、重复执行、迁移中断恢复、跨租户、幂等冲突、并发领取、乱序最新值、相同时间点、批量部分失败、查询分页/聚合/导出、保留期、删除与重放、断连重连、停止排空。具体数据量和可接受延迟由现有基线测量，不编造性能承诺。

## 4. 任务总表与依赖

难度：**基础**＝边界确定后可独立实现；**中等**＝需熟悉现有调用链；**负责人先定**＝先冻结事务/权限/副作用语义并评审。表中依赖是开始实现的最小前置，不表示最后发布可以跳过数据库矩阵。

| ID | 任务包 | 优先级 | 前置 | 分派方式 |
| --- | --- | --- | --- | --- |
| T00 | 固定基线、路由契约、owner 与支持版本 | P0 | 无 | 负责人+测试 |
| T01 | 两个 Go module 的单一源码迁移样例 | P0 | T00、T02.a | 负责人先定 |
| T02 | 身份、租户、授权与 HTTP 契约适配 | P0 | a：T00；b：T01 | 负责人先定 |
| T03 | 修复删除设备的归属与事务边界 | P0 | T00 | 中等，重点评审 |
| T04 | 修复遥测 fallback 与乱序最新值 | P0 | T00 | 中等，定向数据测试 |
| T05 | 自动化 owner、模式与触发入口统一 | P0 | T00 | 负责人先定 |
| R01 | 连接、配置、错误分类与自管资源装配 | P1 | T01 | 中等 |
| R02 | 多引擎 schema、单一迁移执行者及关系数据搬迁 | P1 | a/b：R01；c：相关R03～R09目标适配完成 | 负责人先定 |
| R03 | Community 普通表族与查询方言 | P1 | R01、R02.a/b | 每人一个表族 |
| R04 | 物模型 Repository 的多关系库实现 | P1 | R01、R02.a/b | 中等+事务评审 |
| R05 | authcore 身份存储适配 | P1 | T02、R01、R02.a/b | 中等+权限评审 |
| R06 | automation store 的锁、序列与领取 | P1 | R01、R02.a/b、T05 | 负责人先定 |
| R07 | notification Store 与来源 outbox | P1 | R01、R02.a/b | 负责人先定 |
| R08 | northbound 存储适配 | P2 | R01、R02.a/b | 中等 |
| R09 | Broker ledger/control/commands/state | P1 | R01、R02.a/b、T03 | 负责人先定 |
| S01 | 时序契约与统一历史存储入口 | P1 | T00、T04、S05.a协议 | 负责人先定 |
| S02 | PostgreSQL/Timescale 历史适配器 | P1 | S01、R02.a/b、S05.a协议 | 中等 |
| S03 | TDengine 第二实现 | P1 | S01、S05.a协议 | 单驱动，负责人提供样例 |
| S04 | 查询、导出、统计、清理全部接入端口 | P1 | S01、S02 | 每人一类查询 |
| S05 | 跨库存储意图、切换/删除与数据搬迁 | P1 | 设计：T00/T04；实现：R09/S01/S02 | 负责人先定 |
| A01 | 用户/租户/RBAC/APIKey/菜单/字典 | P1 | T01、T02 | 按端点族拆分 |
| A02 | 设备/产品/分组/配置/主题映射 | P1 | T01、T02、T03 | 按用例拆分 |
| A03 | 旧物模型入口与 native consumer 收尾 | P1 | T02、A02 | 中等，保持原生契约 |
| A04 | 遥测/属性/事件/实时读取 API | P1 | T02、S01、S04 | 按传输和查询拆分 |
| A05 | 下行命令/期望数据/OTA API | P1 | T02、A02、T05 | 负责人定副作用语义 |
| A06 | 自动化/告警收尾及原生嵌套场景 | P1 | T02、T05 | 嵌套场景单独评审 |
| A07 | 通知旧入口与来源适配收尾 | P1 | T02、T05 | 中等，复用原生通知 |
| A08 | 模板市场/安装/仪表盘模板 | P1 | T02、A03；文件接线A11-FILE | 按状态机步骤拆分 |
| A09 | 北向数据流接线与发布 | P2 | T02、R08 | 中等，先核对来源 |
| A10 | 协议/服务插件与数据脚本 API | P1 | T02、A02 | 各类插件分别分派 |
| A11 | 看板/系统/文件/SSE/诊断入口 | P1 | T02 | 基础与流式任务分开 |
| Q01 | 生命周期、配置校验、就绪和诊断 | P1 | T00；与 R01 协作 | 中等 |
| Q02 | 发布服务清单、安装器和调度迁移 | P1 | R02.a/b、T05、Q01；搬迁验收加R02.c | 发布负责人 |
| Q03 | 契约、真实驱动与整栈验收 | P1 | 从 T00 开始逐项累积 | 测试+模块作者 |
| Q04 | 最终切流、Gin 退役与文档清理 | P2 | 对应 A/R/S/Q 全部过门 | 负责人执行发布 |

### 推荐推进顺序

- **第一批：** T00/T02.a冻结身份与WS归属保护，T01建样例，T02.b完成协议适配；T03/T04/T05修已知风险，Q01配套。不要让多个开发者同时改公共鉴权或连接初始化。
- **第二批：** R01/R02.a/b、S05.a协议与S01冻结后，按 owner 分派 R03～R09、S02/S03。A 系列可先在当前 PG 基线逐组迁移，但每个 PR 只改变一个维度。
- **第三批：** R02.c关系数据搬迁、S04/S05、各 A 包的业务闭环、Q02 安装发布接线。北向按独立能力开关验收；没有启用时明确不可用。
- **最后：** Q03 汇总四组合证据，Q04 按模块切流并移除旧实现。已完成的物模型/认证/自动化/通知核心只补明确缺口。

## 5. 可以直接复制的总提示语

### 5.1 派单小卡

API任务已在附录D按精确路由拆成小卡，可直接指定小卡编号。R/S等事务与驱动任务由派单负责人填写下表，不能只说“把这个模块改造一下”。卡片是实施单位；是否能独立切流仍按完整owner和表族验证，不能用局部接口完成替代全体写者收口。

```text
子任务编号：
本次唯一交付物：
允许修改的模块/文件范围：
指定端点、表族、驱动操作或状态转移：
复用的现有owner/接口：
必须保持的输入输出与业务不变量：
依赖的已合并提交：
验收命令及所需服务/夹具：
相邻任务的明确边界：
```

T03与T04任务卡已限定到具体调用链和失败样例，可作为最先分派的两项修复。复杂任务的协议未冻结时，不把架构选择交给执行者猜。

### 5.2 总提示语

```text
你要完成 ThingsPanel_Encore_Migration_Tasks_2026-11-01.md 中我指定的一项子任务。
基线为 ThingsPanel/thingspanel-backend-community 的 main；本任务书审计提交是 7f694779c979702740b15125164ebec2545604a4。
先查询 SkillHub 共享记忆（优先 memory_query，缺失时 mq），再读仓库 AGENTS.md、.ai/_项目开发规范.md 和该任务列出的源码。
若 main 已变化，先报告与任务相关的差异，保留已完成成果，不直接照旧报告改代码。

开工前输出：任务ID、唯一业务owner、需要修改的文件、现有可复用接口、前置任务是否完成、两个成功用例和三个失败用例。
边界已经明确的工作直接推进；只对实质架构/契约缺口提出具体阻塞，不能自行另造模块或偷偷扩大范围。
沿用现有 api/service/dal/repo 与 Encore core/app/Store。不要创建第二套物模型、认证、通知、自动化、Broker或北向核心；不要复制业务实现。
不跨 Go module 非法导入 internal；不把 Gin 挂进 Encore fallback 当作任务完成。必要 raw API 直接用 net/http。
身份和 tenant 来自可信认证上下文，不接受请求体自报身份作为授权。先验证资源归属，再做副作用。
不改变已确认的原生契约和业务语义；不增加新旧 owner 自动 fallback；不把新接口200当成动作或数据已经完成。
本次只改一种维度：HTTP适配、存储适配或一个明确业务缺口。禁止同一PR顺便重写规则、升级所有依赖、换ORM或改完整项目结构。
数据库兼容必须有真实目标引擎验证；必须覆盖适用的事务、幂等、并发、乱序、部分失败与中断恢复。mock只能证明逻辑隔离。
不要改已执行migration，不连接生产数据，不发送真实通知或控制真实设备，除非任务另有明确授权。

完成时提交：改动说明、接口/配置变化、源码唯一owner证明、实际测试命令与结果、未执行项及原因、回滚/恢复办法、仍需人工评审的边界。
先完成必要验证，不堆砌只复述实现的测试。涉及真实状态的失败测试不能省略；不得声称未运行的测试已通过。
结束前做记忆收尾：原始长期意图按 learn_evidence/当前受治理 learn 入口提交；已验证可迁移结论走当前候选治理。只有 stored 且有 memory_id 才称已正式入库。
```

后面每个任务的提示语须与上面的总提示语一起使用。

## 6. 逐项任务卡

### T00｜固定基线、接口契约和支持版本

**范围与产物：** 沿用现有 `docs/`、发布清单和测试目录，登记本文附录的路由、实际 ingress、四个被当前镜像构建选用的 Encore 服务及可选能力。产出一份 owner/配置/版本表和可执行契约样例；不新建项目管理系统。

**工作：** 固定 backend、frontend、installer 的实际提交/制品；核对 `TP_AUTH_OWNER`、`GOTP_AUTOMATION_MODE`、MQTT profile、shadow开关；登记每个路由的最终 owner、现有状态、调用者与切换办法。锁定两种关系库/时序库的精确版本。记录 Encore SDK `v1.30.0` 与现有 CI CLI `1.58.6`，不能拿最新文档功能直接当项目现成能力。[C02][C03]

**验收：** 357 行 Gin 台账都有任务和最终处置；条件/退役入口单列；样例含 status/header/body/空值/错误码/鉴权。新增 main 差异重新登记。旧 `.ai` 文档与代码冲突有说明，不能用“main已合入”替代部署证据。

**难点与分派：** 负责人负责 owner 和数据库版本，测试人员捕获脱敏契约。真实客户旧数据与仓库那批明确不迁移的自动化测试配置分别处理；不自动迁移后者。

```text
执行 T00。以指定提交重建当前路由/服务/资源清单，对照实际部署的版本和开关；只登记脱敏事实。给出每行接口的唯一owner与去向、数据库精确版本、客户端兼容样例。找不到的部署事实标成未核实，不猜测。不要写业务代码或启动真实设备动作。
```

### T01｜完成一个跨 Go module 的单一源码迁移样例

**范围：** 平台字典先行切片：`POST /api/v1/dict/column`、`GET /api/v1/dict/enum`、`DELETE /api/v1/dict/column/:id`；来源为 `router/apps/sys_dict.go` 及现有 API/service/DAL。目标可固定为 **一个** `encore-api/platformconfig` 服务目录，职责限于平台字典/功能配置，仍在 `encore.app`，不新增第三个 `go.mod`。

**工作：** 先消费T02.a冻结的身份/权限方案，登记完整8条字典路由及所有写者。画选定用例调用闭包，移动唯一业务实现和必要数据访问；通过构造参数提供资源，移除对旧 `global` 的传递依赖。三个端点先在隔离环境证明技术样例；测试切流只影响试点范围，正式表族切流必须等完整8条及所有写者完成A01后统一执行。仅当尚有旧内部调用者时，提供一个显式、单向、native-only client；无调用者不建 client，失败不回旧实现。数据库先沿当前 PG 基线，不顺便做 MySQL。

**验收：** 真实 Encore HTTP+隔离 PG 完成创建/查询/删除；验证权限、旧响应、取消、唯一写入与直接访问旧进程不能绕过切换。迁移用例的依赖闭包无 Gin/旧响应/SSE；同一业务/SQL实现只有一份。记录后续任务照用的目录、装配和测试模板；三接口通过不能宣告字典表族完成。新增platformconfig必须登记service/resource/migration/ingress/health/CI并接Q02服务清单，单独encore run成功不算发布完成。

**难点与分派：** 此包由较强开发者完成。禁止让 Encore 非法 import `project/internal`，禁止复制整套 service/dal/global，禁止为了共用少量代码先造第三套共享框架。

```text
执行 T01 的字典三接口试点。按领域物理迁移唯一实现到既定 Encore owner，不增加共享Go module，不复制旧业务。保持原URL和响应；只有真实未迁调用者才做单向native-only client。用真实Encore和隔离PG证明闭环、权限、cancel、旧直连禁写及无Gin依赖，形成后续唯一模板。
```

### T02｜统一可信身份、资源授权和协议适配规则

**分两阶段：** T02.a在T00后先冻结现有凭证/资源授权契约并修复WS归属缺口，作为T01的前置；T02.b在T01样例完成后实施统一协议adapter。

**范围：** `l1-access/authn`、`internal/authcore`、现有 authz、Community JWT/Casbin、response middleware；不是新写认证系统。[C06][C18]

**工作：** 明确浏览器用户、APIKey、服务来源、委托身份的验证方式；构造业务层可用的可信身份，保留 actor/delegation/auth_version/tenant/role等必要事实。资源授权在用例入口验证，身份认证不能替代租户归属检查。优先修补批量状态WS与当前遥测WS对每个device_id的归属校验，未授权设备不得查询或进入Redis订阅；这是静态确认缺口，尚未做运行利用验证。逐类固定 typed DTO 和 raw writer 的状态/错误码，不盲目统一所有 response。凭证/成员资料端点继续排除完整请求日志。

**验收：** 无凭证、冲突凭证、过期、吊销、权限不足、跨租户、委托范围外、服务 token 冒充用户均拒绝；注销/用户状态改变及时生效。原始 `x-token`、Bearer、APIKey 与 WS 首消息认证的必要契约都有对应样例；敏感值不进入日志/trace。现有HTTP与WS的token失败后APIKey回退行为不同，拒绝冲突/失败后换身份属于显式安全收敛，须记录行为变更和客户端迁移样例，不能声称全部照搬旧契约。

**难点与分派：** 负责人先定身份模型；开发者分别做一种凭证adapter或一组response样例。不要求已有 raw 登录/cookie/重定向全部改 typed。

```text
执行 T02，复用现有authn/authcore/authz，不新建IAM。先给出各凭证到可信身份的映射和每种资源授权规则，再迁移一个adapter。保持接口状态与错误格式，补跨租户、吊销、冲突凭证、委托范围用例；证明日志/trace不含凭证和完整联系方式。
```

### T03｜修复设备删除的租户与事务边界

**范围：** `internal/api/device.go`、`internal/service/device.go::DeleteDevice`、`internal/dal/devices.go` 及关联删除方法。[C13]

**工作：** 在任何删除前，通过可信 tenant+device 查询/锁定目标；所有关联数据操作使用同一范围。检查 Begin、查询、删除和 Commit；区分不存在、被并发修改、无权限与DB失败。仅在提交成功后清缓存/通知断连；失败的后处理有明确重试或诊断，不重复删除。

**验收：** 租户A删B设备时设备、历史、当前值、属性、事件、缓存与连接均不变；不存在/重复删除/有子设备/有引用/并发删除行为明确。注入提交失败，不返回成功、不清缓存、不下发断连；Commit结果未知时按原操作身份查证后续状态，不能宣称已经回滚或重新执行副作用。不得只在最终设备DELETE处补tenant条件。

**难点与分派：** 当前是单库修复，先单独PR。跨时序库删除由S05继续完成；不把“修好主表删除”当成跨库问题已解决。

```text
执行 T03。先复核DeleteDevice调用链，在任何副作用前以可信tenant和device验证归属并固定事务范围。检查RowsAffected的真实语义与Commit错误，只在成功后做缓存/断连。写双租户、并发删除和提交失败的定向回归，禁止顺便改设备模型或引入跨库事务。
```

### T04｜修复遥测 fallback 和最新值乱序

**范围：** `internal/storage/telemetry_writer.go` 的转换、批量、fallback；与可信 ingress 的新旧时间比较保持一致。[C14][C19]

**工作：** 消除历史数组与 current map 输出的索引配对假设。按业务键建立关联，或为每条历史构造相应候选快照；写最新值时校验时间及冻结的同时间冲突规则。每条真实持久结果独立统计，部分成功不能记成整批全成功/全失败。

**验收：** 强制批量事务失败；同device/key在1000与2000两个时间点的数据进入fallback时不panic；两个不同key不串值；后到旧点不覆盖新快照；相同时间不同payload按T00契约处理；失败重试和指标与实际数据相符。不能用 `i < len(currentData)` 跳过数据解决问题。

**难点与分派：** 静态证据已明确，但本轮未运行复现；开发者必须先构造失败测试再修复。保持trusted路径已有fence，不为了合并代码降低保证。

```text
执行 T04。复现批量写失败后history和current数组长度/顺序不同的问题，改为按业务键关联和条件更新最新值。覆盖同key多时间、不同key、跨批乱序、部分成功和重试，校验真实写入及指标。禁止越界时简单continue、禁止去掉trusted route检查。
```

### T05｜统一自动化 owner 与所有触发入口

**范围：** `internal/uplink/native_automation.go`、`automationbridge`、`internal/app/broker_ingress.go`、`native_automation_effect.go`、旧 cron/手工/属性/事件/状态触发。[C07][C15]

**工作：** 只保留一个有效模式决策；native缺必要配置即启动失败或明确不可用，不能回legacy。复用现有持久effect/严格receipt链，处置旧内存转发器，不新增第三套relay。逐项隔离旧定时器和旧执行器；已有历史数据不自动恢复执行。兼容演练入口独立开关，禁止与正式写路径同时当owner。

**验收：** 触发入口×legacy/native/错误配置矩阵；native模式存在旧到期任务仍不触发旧执行器；网络丢响应/重复effect只有一个受理身份；坏receipt/空2xx不算成功；原生不可用不重复从旧路径控制设备。队列容量、失败与重放状态可见。

**难点与分派：** 涉及真实动作、代际fence与可靠性等级，负责人先定保证。不要不经容量测量就把所有高频best-effort点一律变成可靠队列。

```text
执行 T05。列齐MQTT、属性、事件、状态、手工、旧cron、原生tick的owner，复用automationbridge与现有effect/receipt。去掉native配置错误回legacy和互相矛盾的执行路径；不另建relay。提供逐入口唯一执行、重复受理、坏receipt与旧定时隔离测试，区分受理与设备实际完成。
```

### R01｜关系库连接、配置与资源装配

**范围：** `initialize/pg_init.go`、`internal/app` 装配；Encore各owner初始化、authcore独立连接。配置解析可以同一规则、按进程实现装配，不为此新增网络服务或第三业务核心。

**工作：** 明确 `relational.driver` 与 `timeseries.driver` 的独立配置和每个逻辑资源映射；旧字段只在配置边缘解析一次并检查冲突。提供现有GORM/SQL Store需要的池；统一可识别的唯一冲突/不存在/超时/死锁等错误。冻结自管数据库资源替换顺序，配合R02退出隐藏sqldb依赖。

**验收：** 未知driver、缺配置、连错引擎、两份配置冲突、必需连接失败不能报ready；旧PG配置有明确兼容入口；连接数受控，关闭一次且有界；日志不含密码/DSN。静态扫描无新增散落Open连接。

**切分：** 先连接/config，再每个host装配；不在本包翻译全部业务SQL。

```text
执行 R01，先登记全部gorm/sqldb/pgx连接及唯一owner，再按既定配置装配。关系库和时序库必须独立选择，每资源一个池。保留现有DAL/Store，拒绝错误配置并关闭自建连接；不把改DSN或go.mod已有driver声明为兼容完成。
```

### R02｜多引擎 schema 与迁移执行者

**范围：** `sql/`、`migrations/`、`initialize/*_schema.go`、Encore四个逻辑库的migration、发布runner。[C05][C16]

**子任务与顺序：** R02.a清点schema、账本与执行者；R02.b实现各引擎baseline、增量和唯一runner。R03～R09、S02依赖的是R02.a/b。R02.c负责跨关系引擎的数据搬迁，在相关owner完成目标引擎适配后联调，不反过来阻塞其适配实现。

**R02.a/b工作与验收：** 逐owner列出现存账本、checksum和执行时点；冻结归属表。保留已执行PG历史，新增MySQL的语义等价baseline/增量。移除关系库新装对`create_hypertable`的强制要求，将时序schema归到时序adapter。迁移只在显式发布/安装步骤发生，不能多个实例启动时争相改表。验证空库、适用的同引擎版本升级、重复执行、checksum变更拒绝、并发runner和中断重跑；非事务DDL有checkpoint/前后检查。失败停止发布，`device_metadata`资源与历史账本可追踪。

**R02.c工作与验收：** 先完成一个owner的脱敏搬迁演练，再做全部相关逻辑库的一致切换。维护窗口内停止相关API写者、接入和worker，确定一致导出边界；源导出/checkpoint、类型转换、目标幂等导入及对账分别记录。保留稳定ID、tenant归属、关联关系、版本/固定快照、幂等键与回执、未完成/unknown状态、密码哈希和密文字节及对应密钥版本引用；调整目标序列/自增起点，验证时间精度、null/JSON与大小写规则。总量、分租户关联、关键值及目标业务读回必须一致；中断重跑不复制记录，不把已受理或unknown外部动作重置为待执行后再发送。源PG账本与目标MySQL baseline/checkpoint显式映射，不把PG执行记录伪装成MySQL已经执行。恢复写入前保留可验证的回退点；恢复写入后须核对新增记录与外部效果再决定回退，不能直接切回旧库丢弃新受理。此子任务迁关系业务数据；遥测历史搬迁归S05。

**难点与分派：** PG JSONB/数组/partial index/sequence/extension/时间类型不能只改名字；MySQL DDL不能假定可整段rollback。恢复可用备份+兼容代码，不能承诺所有down migration可逆。a/b按一个owner schema拆PR；c由负责人先冻结一致导出、状态保留及恢复协议，执行者只做一个owner的导出/导入/对账步骤。

```text
执行指定的R02.a/b或R02.c，不混做。a/b先提交账本/执行者/历史checksum对照，再做一个owner的目标DDL与唯一runner，验证新装、同引擎升级、重复、中断、并发和checksum拒绝。c仅在目标owner适配完成后，按已冻结维护窗口做一致导出、保留原ID/状态的幂等导入、对账与恢复；不得重发已受理或unknown动作，不把关系数据搬迁称作原地DDL升级。禁止改已执行PG文件、双runner或另造安装器。
```

### R03｜Community 表族与特殊查询方言

**范围：** `internal/dal`、`internal/repo`、`internal/query`、Casbin持久化及看板统计。按账户权限、设备产品、配置模板、业务状态、报表拆子任务，不整体重建仓储。

**工作：** 复用GORM处理普通CRUD；只将PG特定JSON、大小写比较、时区、generate_series/date_trunc/FILTER、upsert和错误识别收口。生成代码走现有生成方式，避免手改一批生成文件形成漂移。

**验收：** PG/MySQL同一组CRUD/筛选/唯一约束/分页/空值/中文和大小写/时间边界样例输出一致；比较数据库0行、未变化行和不存在行，不能混为一谈。报表遇查询错误不能返回“正常0”。

**切分：** 每个PR一个表族或一个复杂查询；只有相关数据访问变动，不改API和业务规则。

```text
执行 R03，只改分给你的表族/查询。沿现有DAL和GORM，为特殊SQL写小范围方言实现，不造通用SQL翻译器。给出PG与MySQL同输入结果对照，覆盖分页稳定性、JSON/null、时间和错误分类；检查生成代码来源和索引，别把失败伪装成空集合。
```

### R04｜物模型多关系库实现

**范围：** `thingmodel/app.Repository`、`app.Transaction`、`postgres.go`、逻辑资源`device_metadata`。[C04][C05]

**工作：** 保持core/app原样，扩展唯一repository装配与MySQL实现；锁、乐观版本、幂等、审计、事件在同一owner事务。迁移资源声明按R02保留身份和历史；发布快照及固定绑定格式不变。

**验收：** 同幂等键同内容返回同结果，异内容冲突；并发revision冲突；发布快照不可变；审计/事件/回执与模型提交一致。提交前可控失败使全部修改回滚；Commit结果未知时不能假定回滚或换新幂等键重做，须查证原回执或用原键恢复。已有资源文档完整往返。

**难点：** advisory lock替代、JSON语义、错误映射。不得把模型业务改写进MySQL adapter，也不能另建“mysql thingmodel core”。

```text
执行 R04。只扩展现有thingmodel Repository/Transaction，不改core/app契约。保持模型、审计、事件、幂等回执的同库事务和固定快照。为PG/MySQL跑相同并发revision、重复键冲突及回滚测试，保留device_metadata账本，不复制业务核心。
```

### R05｜authcore 身份存储兼容

**范围：** `encore-api/internal/authcore/runtime.go`、SQLAccounts、provider/会话数据、Community身份schema；原生认证继续归authn/authcore。[C06]

**工作：** 将直接`sql.Open("pgx",...)`改为R01装配，补身份存储方言。明确当前身份表owner及兼容读取者；不能因HTTP入口迁走就重新建一套users。会话/吊销/委托与provider密文持久化语义不变。

**验收：** 登录、刷新、退出、密码变更、禁用账户、auth_version、代登录和provider配置在两引擎成立；提交失败不发有效会话；唯一账户约束一致；Redis/SQL连接退出有owner；MySQL环境无隐藏PG连接。

```text
执行 R05，复用authcore身份存储和authn协议层，替换数据库装配与必要方言。先明确身份表唯一owner，禁止另建重复users或绕过吊销检查。验证两引擎登录/刷新/禁用/委托及失败事务，确保不依赖pgx专用连接且不泄漏凭证。
```

### R06｜automation store 的并发与持久化兼容

**范围：** `encore-api/automation/store`、迁移与运行装配；现有runtime/规则引擎保留。[C07]

**工作：** 逐类实现规则/引用/运行、调度领取、outbox、幂等和sequence替代。保留版本与generation比较、固定锁顺序和租约过期。所有外部动作仍在事务外执行；数据库死锁重试不重复触发动作。

**验收：** 同一有效租约内只有一个worker成为当前owner；租约过期允许恢复领取，但旧owner的完成提交被lease token/generation/CAS拒绝。覆盖超时回收、重启恢复、同key重复、payload冲突、不同generation；暂停/取消/restart/queued/parallel所依赖状态一致；可控事务失败不出现已完成但无回执的半状态，Commit未知按原身份查证。

**切分：** 一个PR一个Store表族/领取操作；负责人先给并发时序和不变量，不交给新人自行替换SQL锁。

```text
执行 R06 的指定Store切片。保留现有runtime和run/lease/outbox设计，只实现目标引擎的持久语义。先列锁顺序、领取条件、租约和generation不变量，给双worker、崩溃恢复、死锁重试测试。外部动作不能进DB重试事务，也不能增加第二执行器。
```

### R07｜通知 Store 与来源 outbox 兼容

**范围：** notification现有Store接口、worker、错误分类；Community `notification_source_outbox`、默认策略与native-publish的数据库操作。[C08][C16]

**工作：** 保持告警/来源意图同事务，通知owner维护自己的task/delivery；兼容锁、领取、RETURNING/JSON/partial index及密文存储。源意图幂等可以重试；供应商结果unknown不能盲目重新发送。默认/显式策略快照不能在重放时被重新解释。

**验收：** 同source intent只建一次任务；网络受理回包丢失后重试仍同receipt；双worker、租约过期、取消、unknown、失败重试分类、密钥轮换读取；客户敏感资料不进明文日志。纯fixture不宣称真实供应商验收。

```text
执行 R07 的一个Store或source-outbox切片。沿现有接口实现两引擎事务、领取和错误分类，保留快照、密文与稳定来源键。区别来源重试与供应商重发，unknown不得自动当失败重投。用受控fixture证明重复、丢回包、双worker和恢复；不要发送真实通知。
```

### R08｜北向已有存储的兼容

**范围：** `encore-api/northbound` 内直接sqldb访问、inbox、worker、delivery、overview。[C09]

**工作：** 在本模块收口现有SQL，不创建第二北向服务；接R01/R02。兼容租约、并发领取、批次回执、容量约束、终态清理与JSON查询。Overview查询失败返回明确不可用/降级状态，不能伪装零积压。

**验收：** 两引擎相同inbox重复/冲突、限额、双worker、插件unknown和终态保留；积压和失败指标与真实账本相符。此任务通过不代表服务已进入发布，后续必须做A09/Q02。

```text
执行 R08，扩展现有northbound持久层，不建新转发框架。逐类收口sqldb调用并接入目标引擎，保持inbox去重、租约、配额和状态。覆盖并发领取、插件unknown、清理及overview数据库错误；单独记录发布尚未接入的部分。
```

### R09｜Broker 与指令账本的数据库语义

**范围：** `internal/brokerledger`、`brokercontrol`、`brokercommands`、`brokerstate`、`automationguard` 与相关schema。[C16][C19]

**工作：** 改造advisory lock、凭据JSON等值、路由epoch、command generation、effect领取与状态CAS；复用现有Repository/Store。凭据、route、receipt、下行计划的相关修改保持同一owner事务；不引入另一套接入/控制账本。

**验收：** stale epoch/generation拒绝；重复receipt同内容只产生一次业务效果、不同内容冲突；两个接入者/两个worker争抢、事务死锁、命令响应丢失与未知状态均保持边界；租户/设备不会串账。真实设备执行结果与MQTT ACK分开记录。

**难点与分派：** 负责人冻结锁顺序和状态机。先在隔离驱动测试并发不变量，再受控设备联调；不得以SQL能运行判定兼容。

```text
执行 R09 的指定账本操作。复用现有Broker仓储和状态机，先描述tenant/device/epoch/generation/receipt的事务不变量，再实现PG/MySQL方言。证明并发领取、陈旧写拒绝和同key冲突；不新增Broker、不在事务重试中重复控制设备。
```

### S01｜冻结时序契约并建立统一历史入口

**范围：** 现有`internal/storage`及数据访问调用者；按T01的唯一owner路径移动/接入，不另建时序网络服务。[C17]

**工作：** 对历史写、范围/分页/聚合/导出/删除制作端口及契约表。最新值/当前属性仍是关系状态端口。所有请求携带可信tenant/device；配置只在装配解析一次。能力表只描述真实能力，不为每个查询造一个抽象层。

**验收：** 一套测试夹具覆盖数值/布尔/文本/null、相同时间冲突、乱序、区间端点、时区、空桶、两种已有diff语义、分页/总数、部分成功和unsupported；各端点与旧语义有对照，不发生静默业务改动。

**难点：** 旧实现自身有语义差异；先保留端点契约，优化/统一算法另立变更。interface要求能覆盖业务，而不是照着某家SQL拼参数。

```text
执行 S01。先用当前端点样例冻结时序语义，再在现有存储职责内定义最小历史端口及能力。当前快照归关系库，不让历史驱动另藏一套状态。提供覆盖边界值、乱序、diff、分页、删除和部分失败的公共夹具；不得静默统一旧行为或返回假成功。
```

### S02｜实现 PostgreSQL/Timescale 历史适配器

**范围：** 当前遥测历史表、批量写和PG/Timescale特定查询/清理。

**工作：** 把已有历史实现移到S01端口后面，保留表/时间/值映射；Timescale专属建表、chunk/retention操作归adapter。普通PG模式与Timescale扩展模式能力明确区分，避免关系库本身被迫带扩展。

**验收：** 旧数据无需破坏性重建即可读取；同样夹具全部通过；重复点、旧点、批量部分失败、聚合、分页、保留期均正确。验证历史adapter在独立实例能完成数据契约，不依赖调用者传入关系库事务。本卡只完成adapter验收；应用历史写链路外置必须等S05意图、屏障与恢复协议通过后才可切流。

```text
执行 S02。将现有遥测历史PG/Timescale逻辑接到S01接口，保留数据格式和端点语义，把扩展专属SQL留在adapter。验证旧数据、分实例、批量失败、聚合和清理；不能为了通过而要求所有关系库安装Timescale。
```

### S03｜实现 TDengine 第二时序驱动

**范围：** 一种精确版本的TDengine及一个历史adapter；第一期不同时做InfluxDB等更多引擎。

**工作：** 按S01映射租户/设备/key/时间/值类型和稳定幂等身份；冻结子表/标签与索引策略，控制基数与表数量。核实真实版本的重复时间、排序、聚合、删除、保留期和类型限制。能在adapter内安全补齐的语义明确实现，必需能力无法实现则拒绝该profile。

**验收：** 同一契约夹具在真实TDengine上通过；读写路径不会偷读PG；重试无重复副作用；分页稳定；大租户与其他租户隔离；关闭/超时/批量失败结果准确。容量测试与当前基线可比，不虚构性能提升。

**难点：** 同时间冲突、类型变化、标签基数、删除完成语义是设计点；负责人先确认存储映射，再让开发者逐操作实现。

```text
执行 S03，仅实现锁定版本TDengine的历史adapter。先提交数据映射、重复时间和类型冲突语义，再实现S01所有必需能力。用真实引擎跑共享夹具与容量基线，证明不偷读PG；不支持项明确报错，禁止静默降级或同时扩展其他品牌。
```

### S04｜所有历史读写、导出、统计、清理走统一入口

**范围：** `dal/telemetry_datas.go`、`telemetry_current_datas.go`、聚合/统计、service导出、设备删除、datapolicy；`tptodb_client`作为可选单adapter。

**工作：** 删除业务层散落的`grpc.tptodb_type`分支；历史访问统一S01，当前快照明确走关系端口。导出按稳定游标流式读取，限制范围/内存；聚合时间和分页总数不因driver改变。保留期由现有策略调度调用adapter，停止在通用业务路径执行VACUUM或把EXPLAIN估算当精确总数。

**验收：** 在主库放“哨兵数据”、时序库放不同数据，历史查询/导出/统计只返回被选中的时序源；当前值返回其指定owner结果。无跨租户、无超限导出、取消能中断查询；清理与删除有可核验结果。保留tptodb时补timeout/health/Close及同一套契约。

```text
执行 S04 的指定查询族。追踪从API到真正读写位置，将历史访问统一到S01，当前快照明确走关系owner。用两库不同哨兵证明选库正确，覆盖分页/导出/聚合/清理与取消；去掉业务层品牌分支，不得以返回len(rows)冒充总数。
```

### S05｜跨库意图、切换、删除与历史搬迁

**分两阶段：** S05.a先在T00/T04之后冻结协议、数据身份和崩溃反例，作为S01/S02/S03的输入；S05.b在R09/S01/S02完成后实现和联调。不能先完成第二驱动映射再补删除世代设计。

**范围：** `telemetry_broker.go`、已有broker ledger/effects、历史adapter、设备/保留期删除与一次性数据搬迁。[C19]

**工作：** 扩展现有effect种类保存历史写意图和投影结果；关系事务不直接跨外部库假装原子提交。定义受理/可查水位、自动化触发顺序、幂等键、容量/背压和重放。路由切换按旧世代排空/unknown对账建立屏障；删除先禁止旧世代新写，再执行可重试外部删除并记录完成。维护窗口搬迁有checkpoint、校验和与恢复计划。

**验收：** 在关系提交前后、时序写前后、回包丢失、标记完成前注入故障。对已持久受理的可靠数据，证明意图不静默丢失、可恢复重放、同一逻辑点去重、派生效果按冻结顺序执行；允许至少一次物理写重试。普通best effort数据遵守明确的丢弃和统计规则，无法判定结果保持可见状态。删除后重放旧消息不能复活；切换与写竞争保持既有fence不变量。中断搬迁重跑幂等、总量/采样值/时间范围对账一致。

**难点与分派：** 此包必须负责人先完成协议/反例审查，开发者只领一个状态转移或adapter操作。第二引擎缺可靠去重时不能宣布可靠profile已支持。

```text
执行 S05 指定子步骤。复用现有ledger/effects，先画关系提交、时序落库、receipt与自动化的事件顺序和崩溃点。不得承诺跨库事务或只凭入队返回历史已完成。证明重放幂等、切换屏障、删除墓碑不复活及中断搬迁恢复；未经批准不引入在线双写或新队列框架。
```

### A01｜用户、租户、权限与平台配置接口

**范围：** 附录identity/role/casbin/api_keys/menu/dictionary/settings，以及挂在`/api/v1/board/user/*`下的个人资料查询/修改和改密（G041～G043）；`sys_user`、角色、Casbin、APIKey、UI/menu、字典、功能设置。按业务职责归属，不按URL前缀把账户迁到看板。登录/会话继续归已有authn，不能另做认证。

**子任务：** 账户资料/租户；角色/Casbin/APIKey；菜单/UI/看板菜单绑定；字典/功能配置。各自一组PR，沿T01已有样例。

**验收：** 原路由status/body与权限契约；超管、租户管理员、普通用户和委托身份矩阵；菜单可见不替代后端授权；改密、禁用、撤销凭证后旧会话不能继续越权。特殊登录/重置/首次初始化逐条核对是否已迁入native；不能只看6条旧writer拒绝列表。

**难点：** 身份表与会话owner已有实现，需精确切换；保持原始用户ID、租户ID和授权关系，不重新生成ID。

```text
执行 A01 中指定端点族。复用authn和现有账户/权限逻辑，按T01迁移唯一实现，保持ID、响应与授权。列出每条路径的原处理者和新owner，完成角色/租户/委托/吊销反例；不新建IAM、不仅凭前端菜单判断权限、不恢复已退休旧写入口。
```

### A02｜设备、产品、分组、配置与接入身份

**范围：** 附录device/device_group/device_config；`device.go`中非模型/市场部分、产品、网关/子设备注册、一型一密、topic mapping、debug/diagnostics。

**子任务：** 设备CRUD/selector；产品/注册/凭据；分组及成员；设备配置与native binding CAS；主题映射/调试。原有Broker控制/运行逻辑继续复用，不启用旧devicemetadata的另一套MQTT。

**验收：** 双租户、父子关系、批量部分失败、配置revision冲突、原生快照缺失/权限、凭据轮换/缓存、设备禁用/删除、注册重复。新装与旧设备ID稳定；设备动作入口继续唯一owner。

**难点：** 一些设备菜单/配置已经调用原生模型，迁移时保留固定快照校验和binding revision；不要把这些再当旧模型能力重写。

```text
执行 A02 的指定设备用例。复用现有设备/Broker运行逻辑与native binding，移动唯一业务实现，不整包启用devicemetadata或复制MQTT。保持设备/租户ID、注册幂等和绑定CAS，验证父子、批量失败、凭据及跨租户场景，旧直连不能形成第二写owner。
```

### A03｜旧物模型/模板写入口退役及消费者收尾

**范围：** 附录device_model、旧device/template相关API；`device_model.go`、`device_template.go`、native consumer；已有thingmodel core/app保持不变。[C04][C20]

**工作：** 逐条确认旧模型CRUD/枚举/模板与当前消费者关系；明确迁移、只读保留、显式410或删除。尚有真实客户数据的迁移用受控作业，保留兼容ID、资源与绑定，完成后禁止旧写。设备运行配置、展示模板、模型定义职责分开，不能把所有template概念塞入thingmodel。

**验收：** 原生模型完整资源、修订、发布快照与固定设备绑定不变；旧路由无法修改原生资源或形成双写；迁移重复/中断/引用缺失可恢复；旧测试配置按已记录的“不迁移”决定处理。

**难点：** 旧template包含模型和展示混合语义；市场/看板部分交A08，不造第二模型核心。devicemetadata资源与旧host分离配合Q02/Q04。

```text
执行 A03。逐接口登记旧模型/模板的真实消费者和去向，保留现有thingmodel唯一核心。对旧写入口做受控迁移后退役，不恢复或复制旧CRUD；展示模板交其原owner。证明完整资源、固定绑定、ID与revision保持，旧路由不能旁路双写，迁移可重跑。
```

### A04｜遥测、属性、事件与实时读取接口

**范围：** 附录telemetry/attribute/event、4个WebSocket、模拟上报、查询/统计/导出；控制下发端点转A05，保留期策略与S04联合。

**工作：** 普通读取用typed；WS保留raw与首消息认证。用S01/S04端口取得历史/当前结果，不在API中拼SQL或切品牌；复用已有native模型指标映射。为查询/导出传递请求context、限定范围和分页；模拟上报必须沿既有授权与接入语义。

**验收：** 既有响应/单位/时间/空值，跨租户、空设备、未知指标、查询取消、并发订阅、慢客户端/断开重连；历史与当前来源可验证。WS在认证前不订阅数据，超出范围device_ids拒绝。

**难点：** 历史已落库与当前快照可能异步，不得由HTTP层假装同步。WS与普通request timeout不同，需要单独连接生命周期。

```text
执行 A04 指定查询或WS族。复用历史/当前端口和native指标映射，不改聚合语义、不在API写SQL。普通请求传递context，WS保留首消息鉴权并隔离租户。验证响应兼容、取消、慢客户端、重连及选库正确；控制操作交A05处理。
```

### A05｜命令、期望数据和 OTA

**范围：** 附录command_dispatch/expected/ota、telemetry/attribute中pub/control端点、automation动作owner、BrokerAI六条授权/只读/预览工具整族。当前工具没有submit/execute，不得新增委托下发能力；设备/遥测读取复用A02/A04提供的owner端口。

**子任务：** 命令受理/计划/状态；遥测与属性控制；期望值；OTA包/任务/进度/下载。复用既有持久command、dispatcher和generation fence。

**验收：** 同幂等键同内容同receipt、异内容冲突；超时未知不盲重发；取消/替代旧generation拒绝；权限与设备归属；OTA重复进度/离线恢复/失败状态、固件下载路径和内容校验。受理、broker接收、设备回执分别展示。

**难点：** 真实副作用不能用接口200证明。回滚只改变未来意图，已受理/unknown动作保持原owner和身份，不由另一套执行器重做。

```text
执行 A05 的一个动作族。复用现有command ledger/dispatcher/fence，仅迁移协议和必要用例依赖。冻结幂等、unknown、取消、代际替换与OTA状态，不新建执行队列。用受控模拟证明重复请求和丢回包不重复动作；真实设备验证另列授权和证据。
```

### A06｜自动化、告警与原生嵌套场景

**范围：** 附录automation_legacy/bridge、alarm/scene相关入口；已有automation规则、schedule、run、告警策略、owner ports。

**子任务：** 旧scene/scene_automations/alarm API处置及前端切换；所有旧触发器退出；原生嵌套场景接线。`HTTPCommandOwner.ActivateScene`当前返回unknown/ErrNotConfigured，嵌套场景由原生automation owner内部复用run完成，不回旧场景执行器。[C07]

**验收：** T05入口矩阵；条件/动作/告警/通知闭环；父子run租户一致、循环/深度/扇出限制、父重试不重复子run、取消/等待/恢复与执行模式一致。保留已存在tick/recovery/systemd调度，不重新开发调度引擎。

**难点：** 父子运行状态传播和已有single/restart/queued/parallel语义必须由负责人先定。客户旧规则若需迁移走单独计划，不能自动迁那批已明确保留的测试配置。

```text
执行 A06 的指定子任务，复用现有automation runtime/Store，不再造规则或调度引擎。若做嵌套场景，先定义父子run、幂等、循环限制、取消及恢复，并在原生owner内接线。证明旧执行器不会双触发，区分run受理、运行结果与设备确认。
```

### A07｜通知来源、旧入口和应用推送边界

**范围：** 附录notification_source/bridge/legacy、message_push的App设备/会话维护；现有notification native服务、Community source relay/default-policy。

**工作：** 保持notification Core/Worker为唯一通知发送者；逐个迁移session/source-directory/default-policy/native-publish的适配归属。3个旧组写API已经410，保留退役语义直到消费者退出，禁止重新实现。App设备注册/会话属账户设备管理，通知发送属notification，不能因为message_push名字就全部归北向。

**验收：** 来源与用户身份不能混用；目录/策略跨租户拒绝；源幂等/快照/unknown/回包丢失；同步验证码邮件保持已冻结同步语义；App设备撤销后不再推送；已退役接口不复活。沿用已有隐私与凭据续期控制。

```text
执行 A07 指定通知adapter。复用native Core/Store/Worker与source意图，不恢复已410的旧组写接口。区分App会话注册和通知投递owner；保留策略快照、来源幂等、unknown与同步验证码语义。提交吊销、跨租户、丢回包和重复来源用例，不发送真实通知。
```

### A08｜市场、模板安装与仪表盘模板

**范围：** 附录market/dashboard_template、device/template中展示部分；文件流接线依赖A11-FILE的窄文件端口（可先复用现有本地实现）；`market_native_install`、模板发布/安装、bundle、固定版本资源、dashboard实例化。[C10][C20]

**工作：** 迁移现有跨服务编排；Horizon仍是外部owner，thingmodel仍是模型owner。保留market导入状态映射、远端幂等、完整快照读回及最后本地绑定事务；网络调用不放长事务。封面/文件用A11的窄存储端口。

**验收：** 同版本/同内容重复安装稳定、异内容冲突、远端成功本地失败后续跑、绑定冲突、补偿和重试不复制模型；导入事件/命令参数和资源完整；跨租户与固定快照不漂移；安装成功需完整本地绑定而非只有远端导入成功。

**切分：** 发布、预检查、安装状态机、展示模板实例化分别PR，不改市场服务自身或另建模板核心。

```text
执行 A08 指定步骤。沿用已有market import/bundle状态机、Horizon边界和thingmodel快照，迁移唯一编排实现。验证同版本重试、内容冲突、远端已成功本地失败、绑定CAS与完整资源。不要把网络请求包进DB事务，不以远端导入成功提前宣布安装完成。
```

### A09｜北向数据流接线与发布

**范围：** 现有northbound inbox/worker/插件；Community事件来源handoff、管理入口/权限、发布服务清单。旧App推送不归北向，归A07。

**工作：** 明确源事件唯一产生者以及持久化点；核对是否有仓库外relay，在没证据前不假定已有。缺少时扩展现有source/ledger结构，向既有inbox投递稳定来源键，不另造转发引擎。接入现有连接器catalog/health/配置/密钥，补R08/Q02的资源、路由和CI；不可用时前端明确disabled。

**验收：** 来源提交前后崩溃、重复handoff、插件超时/unknown、队列满、租户配额、重启和终态保留；管理页面实际能通过后端菜单/权限进入。当前源码有22声明但现有正式构建未选择northbound，完成必须有构建/安装/运行证据。[C03][C09]

```text
执行 A09。先定位并证明Community源事件到现有northbound inbox的唯一handoff；没有代码/部署证据就明确缺口。复用已有worker/lease/catalog，补来源、权限、发布资源和CI，禁止另造北向平台。证明重复、容量、unknown与恢复，未启用部署不能显示功能已可用。
```

### A10｜协议/服务插件与数据脚本

**范围：** 附录plugin/script、plugin心跳/设备配置/服务接入回调、协议与服务插件配置、Lua脚本试运行。

**工作：** 复用现有adapter和executor，把Gin参数/响应与插件调用分开。协议接入、通知插件、北向sink、展示插件保留各自业务契约；只共享确实相同的HTTP超时/重试/脱敏小工具，不建万能PluginManager。脚本限制资源/时间，参数和输出显式验证。

**验收：** 未授权回调、错误tenant/device、插件超时、取消、畸形载荷、脚本超时/错误不影响全进程；凭据/Voucher不写完整日志；重新注册/心跳幂等；实际下行继续走既有dispatcher。删除`ProtocolPlugin.GetDeviceConfig`等输出完整配置的敏感日志。[C21]

```text
执行 A10 的一种插件或脚本端点。复用现有adapter/executor，仅分离HTTP协议与业务，保留各插件类型的职责。验证权限、超时、取消、畸形数据和脚本资源边界；删除完整Voucher/配置日志。不能为几类插件建一个包含所有操作的万能管理器。
```

### A11｜看板、系统、文件、SSE与诊断

**范围：** 附录dashboard/observability/file/stream中SSE、health/systime/version等；logo/sys_function及`/board/user/*`个人资料/改密归A01，设备诊断归A02，本包只管平台运行诊断；上传与OTA/市场共用的文件操作；`pkg/global/SSEManager.go`。[C12][C18]

**子任务：** A11-BOARD看板及租户统计；A11-OBS监控/审计日志/health/metrics；A11-FILE文件上传下载；A11-SSE长连接。统计中涉及账户数据时复用A01唯一账户owner的读能力，不再迁出一套账户实现。普通API按T01迁移；文件和SSE用raw+net/http，先支持现有本地文件后端，需要对象存储时再加adapter。

**验收：** 看板统计不跨租户；DB失败不伪装0；上传大小/类型/路径范围、取消及残留清理；下载授权/旧URL契约；SSE首认证、租户隔离、30秒心跳、单连接单写循环、慢连接有界队列/关闭、断开释放、代理不缓冲、长期连接不被普通deadline误杀。补既有diagnostics而非新上监控平台。

**难点：** SSE当前保存Gin writer且存在全局持锁写与并发心跳，应复用职责但改连接模型；不要从业务模块持有HTTP writer。

```text
执行 A11 指定子任务。看板/监控沿现有业务；流式文件和SSE用net/http raw，不持有Gin writer。SSE每连接单写、有界缓冲、租户隔离与取消；文件先复用本地后端并保留URL/权限。提供慢客户端、路径/大小限制、故障与退出的定向验证，不新增整套观测或文件平台。
```

### Q01｜资源生命周期、配置和可观测性

**范围：** 现有ServiceManager、HTTP/Cron/GRPC/Storage/Diagnostics/Telemetry、Encore Shutdown及authcore自建资源。[C12]

**工作：** 补GORM底层sql.DB关闭、cron Stop、gRPC句柄保存与Close、Collector.Stop、遥测排空和幂等Stop；authcore SQL/Redis归唯一owner。HTTP先同步监听成功再报ready。启动失败逆序清理，退出先停生产者、排空消费者、最后关DB。统一业务关联ID及受理/持久/投递/确认状态，保持凭据脱敏。

**验收：** 端口冲突、任一组件启动失败、启动中取消、重复Stop、任务执行中退出、MQTT重连中退出均有界；无资源在DB关闭后继续写；health区分进程活与依赖就绪；排空超时/积压/unknown可观测。

**难点：** 现有框架已支持很多取消/回滚，补遗漏即可。共享连接不能多个owner关闭；后台context不复用已结束的请求context。另拆cron子卡：逐项登记统计/清理/脚本/消息推送任务的owner、时区、启用条件、重入和多副本策略；删除RunScript空调度，检查每个AddFunc错误，复用现有锁/lease，不新建调度器。

```text
执行 Q01 的一个资源收尾。沿用ServiceManager和Encore Shutdown，谁创建谁关闭且只关一次；复用已有取消/逆序回滚。补同步监听和明确ready、停止生产/排空/关池顺序。验证启动失败、重复停止、有界退出，日志只保留必要关联ID，不造新生命周期框架。
```

### Q02｜发布服务、安装器、迁移与定时任务

**范围：** 现有backend workflows、测试部署脚本、既有`thingspanel-installer`正式编排；本轮未核验installer当前源码，实施须先锁定其版本。[C03][C22]

**工作：** 用一份服务/resource/migration/config/health/CI对照清单约束构建；保留已实现的commit固定、checksum校验、tick与通知续期。把PG特定psql执行通用化到R02，补空库安装与多库升级。未选devicemetadata host不启动其MQTT；保留必要模型资源映射。northbound按能力启用时补全资源，不能只加一个build参数。

**验收：** 四组合干净安装；已有PG安装到新PG版本做增量升级；PG→MySQL是R02.c的关系数据搬迁而非原地DDL升级，新支持引擎用新baseline→后续增量夹具验证；更换时序后端另走S05。覆盖并发/中断migration、schema/代码版本不匹配、失败停止发布、备份恢复和重跑。调度/续期实际运行、重复唤醒安全。Encore自托管镜像本身不执行migration/cron，需由已有发布/调度owner负责。[O04]

**难点：** 当前测试部署已有有效流程，但它绑定timescaledb/psql和已有账本；不能抄成通用新装流程。继续使用既有installer，不在后端再创建第二套正式Compose。

```text
执行 Q02 指定发布接线。先锁定installer/后端制品和服务资源清单，复用现有workflows、迁移校验、tick及续期。补多引擎新装/升级/恢复；禁止另造正式Compose。证明镜像、服务、配置、migration、健康和CI逐项对应，失败停发布，不能假设镜像会自行迁库或调度cron。
```

### Q03｜真实驱动、契约和整栈验收

**范围：** 复用现有测试/CI，随每个任务添加最少但有效的契约、存储和状态测试，最后汇总四组合。

**工作：** 纯Go业务用局部测试，Encore集成按固定版本运行`encore check`/`encore test`；外部DB由测试环境提供，不假定Encore自动创建MySQL/TDengine。抓取HTTP原契约、租户与角色矩阵、重要失败注入。MySQL+TDengine环境阻止PG连接，证明无隐藏依赖。[O05]

**验收：** 每项测试记录实际命令、版本、数据集和结果；未运行/环境缺失与代码失败分开。测试含“设备上报→历史/当前→规则→受理→设备回执→通知/北向”的受控闭环；各步骤区分状态。性能只与相同数据/配置基线比较，给延迟、吞吐、积压、连接/内存而非口头“性能更好”。

**难点：** 普通编译和mock不足以证明方言/并发兼容；真实供应商/设备验收须单独授权，不为了测试误发消息或误控设备。

```text
执行 Q03 指定测试切片。沿现有测试资产验证真实HTTP契约和真实目标驱动，按风险选择测试，别写只复述实现的断言。记录运行版本/命令/结果与未验证项；MySQL+TDengine必须无PG可达。区分受理、持久化、设备确认及通知投递，不用200或总通过率替代失败边界。
```

### Q04｜切流、Gin退役和遗留代码清理

**范围：** 附录A每条路由、旧listener/middleware、条件compat演练、devicemetadata旧host、文档与依赖；按模块逐批完成，不一次删除全树。

**工作：** 每条入口给出“迁至哪个Encore端点/合理raw/静态托管/明确退役”处置；切换前停止旧写受理、排空、冻结版本与回退点，再切现有ingress。已受理/unknown副作用不换owner重做。引用和流量归零后删除临时client/旧路由与无用Gin依赖；保留必要迁移历史与客户资产。

**验收：** 普通模式/已支持能力/错误配置下不存在隐藏旧写路径；生产业务Go依赖闭包无Gin；旧端口不可旁路，旧410保持期限内契约；路由台账全部有最终证据。发布服务列表不启动旧MQTT/重复引擎，安装升级/客户端契约仍通过。

**难点与分派：** 负责人执行实际切流；开发者先交可审查清理PR和证据。若仍保留只读兼容期，应如实标为部分完成，不能宣布全量去Gin。

```text
执行 Q04 的指定模块退役准备。逐条对照附录A确认新owner、旧调用者和退出证据，保留历史migration与客户资产。生成可审查清理PR及切流/恢复步骤，禁止新旧双写和未知副作用重发。只有生产依赖、路由与部署都无Gin且矩阵通过，才标全量完成；本任务未授权实际发布时不要部署。
```


## 7. 验收与交付统一格式

### 7.1 单个子任务的完成标准

- 本次范围内的原接口/存储操作都有新实现或明确退役处置；相邻能力没有被顺手改写。
- 业务实现只有一个所有者；没有复制core/service/SQL，没有新旧自动fallback，没有隐藏的直接旧库写入。
- 正常、权限/租户、参数边界及本任务相关故障用例通过；测试真实执行，且匹配到目标测试，不能把“0 tests to run”当成功。
- 迁移API有status/header/body、空值、分页和错误码对照；必要raw保留流式/回调协议。
- 存储适配有真实目标引擎证据，包含适用的并发/幂等/回滚/Commit未知/租约失效。普通mock不能替代。
- 新schema和配置有明确版本与owner；已执行migration未被改写；安装/升级/恢复路径有对应交付。
- 所有“尚未运行”“外部依赖缺失”“需要真实设备/供应商验收”的项目被单列，不算作通过。
- API小卡可以先完成实现和隔离验证；正式切流仍检查该表族的全部写者、后台消费者和引用。禁止三条字典样例通过就宣布整个字典完成迁移。

### 7.2 必须交回的开发报告

```text
任务/子任务ID：
基线提交与最终提交：
唯一owner、复用接口及删除的旧实现：
本次改变的行为：
保持的契约与显式安全行为变更：
改动文件/迁移/配置：
实际验证命令、目标版本、夹具与结果：
关键失败用例的结果：
未验证项及具体原因：
剩余旧调用者、临时adapter和退出条件：
发布前置、切换顺序、失败恢复：
记忆治理返回状态（不冒充正式落库）：
```

### 7.3 四种数据库组合的交付证据表

“通过”必须链接实际结果；本轮只产出任务书，下表全部待执行。

| 验证项 | PG + Timescale | PG + TDengine | MySQL + Timescale | MySQL + TDengine |
| --- | --- | --- | --- | --- |
| 空库安装、重复迁移 | 待执行 | 待执行 | 待执行 | 待执行 |
| 已支持起始版本的同引擎升级 | T00锁定后执行 | 按实际支持路径 | 新引擎baseline后续增量 | 新引擎baseline后续增量 |
| 跨引擎数据搬迁 | 如不换引擎则不适用 | 遥测历史搬迁 | 关系数据搬迁 | 关系与历史分别搬迁 |
| 原接口契约、角色和租户隔离 | 待执行 | 待执行 | 待执行 | 待执行 |
| 模型/认证/自动化/通知/Broker状态 | 待执行 | 待执行 | 待执行 | 待执行 |
| 历史读写/聚合/导出/保留/删除 | 待执行 | 待执行 | 待执行 | 待执行 |
| 失败注入、unknown、幂等和恢复 | 待执行 | 待执行 | 待执行 | 待执行 |
| 接入到可查、规则到动作的受控闭环 | 待执行 | 待执行 | 待执行 | 待执行 |
| 资源关闭、发布、调度和续期 | 待执行 | 待执行 | 待执行 | 待执行 |
| 隐藏PG检查 | 区分业务/时序资源 | 业务PG明确可见 | PG仅为时序 | 禁止PG可达仍完整通过 |

北向未启用的部署明确标为disabled，不能用跳过测试却显示可用的方式交付。启用北向则增加其源事件、插件、配额和恢复闭环。

### 7.4 对容易出错的实现做法的统一约束

| 主题 | 容易写错的做法 | 本方案要求 |
| --- | --- | --- |
| Encore迁移 | 给旧业务加一层路由/代理便宣布完成 | 传输、调用闭包、唯一写owner、部署选择分别验证 |
| 多数据库 | 只改DSN或引入driver | 覆盖所有逻辑库、schema、方言、锁、连接、迁移、发布 |
| ORM | 认为GORM可以自动抹平方言 | CRUD复用，特殊语义在所属repository显式实现 |
| 事务结果 | Commit报错就假定没有写入 | 结果未知先查证，原幂等键恢复，不换身份重做 |
| 锁与租约 | 所有数据库锁视为相同；租约只看过期时间 | 明确事务/会话范围、固定锁序、lease token与失效提交拒绝 |
| 最新值 | 最后到达的点覆盖当前值 | 按已冻结时间/同时间冲突规则更新，保留乱序保护 |
| 时序查询 | 使用不同后端时悄悄改变diff、区间或总数 | 按端点冻结旧语义，变更显式版本化 |
| 跨库写入 | 关系事务中调外部库便称原子 | 持久意图、幂等投影、可查水位、unknown与恢复协议 |
| 删除 | 队列空/写前查墓碑就称完成 | 处理在途未知写、世代隔离与物理清理完成条件 |
| 切流 | 更新前端URL就算旧写已退出 | 同时验证旧端口、后台消费者、定时器与绕过入口 |
| 权限 | token有效就订阅任意device_id | 每个资源先查可信租户归属/允许范围 |
| 重试 | 网络超时即换新key重发动作 | 保留稳定受理身份，unknown可见且不跨owner盲重发 |
| 验收 | HTTP200、编译成功或总通过率代表业务完成 | 追踪受理→持久→执行→目标确认，各阶段有单独证据 |

### 7.5 开始开发时的审计复现方法

在锁定提交的隔离checkout内，先读取 `AGENTS.md` 和 `.ai/_项目开发规范.md`。可用以下只读搜索辅助复核，但不能用搜索命中数替代注册调用图：

```bash
git rev-parse HEAD
rg -n 'gin.Default|gin.New|RouterInit|Register.*Routes' main.go internal/app router internal/api
rg -n '\.(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD|Any|StaticFile)\(' router internal/automationcompat
rg -n '^//encore:api' encore-api
rg -n 'sqldb.NewDatabase|sqldb.Named|sql.Open|pgxpool|gorm.Open' initialize internal encore-api
rg -n 'pg_advisory|SKIP LOCKED|jsonb|RETURNING|time_bucket|create_hypertable|VACUUM' internal initialize sql migrations encore-api
```

随后按函数调用补外部注册、循环、前缀、条件和未调用代码；与附录逐行比较。API完整契约需从真实编译metadata/OpenAPI和隔离HTTP测试生成，本文静态台账不是OpenAPI替代物。实际开发测试在对应Go module执行；Encore集成使用项目固定CLI的`encore check`/`encore test`，普通Go测试只用于其适用范围。


## 附录 A：Gin 逐接口任务映射

本表每行是一个静态注册的method+path，主路径与条件路径合计357行。`Gin业务/应用适配`不等于全部重写：部分handler已调用原生能力，按对应任务复用。每行只指定一个主任务，T02权限、R/S存储和Q发布要求仍适用。

| 路由ID | Method / Path | Handler | 分组 | 主任务 | 当前状态 | 源码 |
| --- | --- | --- | --- | --- | --- | --- |
| G001 | `GET /internal/automation/devices` | `inline handler` | 自动化设备/动作owner | **A02** | Encore依赖的设备/动作端口 | [internal/api/automation_owner.go:26](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/automation_owner.go#L26) |
| G002 | `POST /internal/automation/api/v1/:kind/datas/pub` | `inline handler` | 自动化设备/动作owner | **A05** | Encore依赖的设备/动作端口 | [internal/api/automation_owner.go:60](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/automation_owner.go#L60) |
| G003 | `POST /api/v1/broker-ai/delegations` | `issueBrokerAIToolDelegation` | BrokerAI授权/只读/预览 | **A05** | Gin业务/应用适配 | [internal/api/broker_ai_tools.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/broker_ai_tools.go#L31) |
| G004 | `POST /api/v1/broker-ai/delegations/:id/revoke` | `revokeBrokerAIToolDelegation` | BrokerAI授权/只读/预览 | **A05** | Gin业务/应用适配 | [internal/api/broker_ai_tools.go:32](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/broker_ai_tools.go#L32) |
| G005 | `POST /api/v1/broker-ai/tools/devices.describe` | `executeBrokerAITool` | BrokerAI授权/只读/预览 | **A05** | Gin业务/应用适配 | [internal/api/broker_ai_tools.go:33](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/broker_ai_tools.go#L33) |
| G006 | `POST /api/v1/broker-ai/tools/telemetry.query` | `executeBrokerAITool` | BrokerAI授权/只读/预览 | **A05** | Gin业务/应用适配 | [internal/api/broker_ai_tools.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/broker_ai_tools.go#L34) |
| G007 | `POST /api/v1/broker-ai/tools/commands.preview` | `executeBrokerAITool` | BrokerAI授权/只读/预览 | **A05** | Gin业务/应用适配 | [internal/api/broker_ai_tools.go:35](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/broker_ai_tools.go#L35) |
| G008 | `POST /api/v1/broker-ai/tools/commands.getResult` | `executeBrokerAITool` | BrokerAI授权/只读/预览 | **A05** | Gin业务/应用适配 | [internal/api/broker_ai_tools.go:36](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/broker_ai_tools.go#L36) |
| G009 | `GET /api/v1/alarm/device/counts` | `api.Controllers.AlarmApi.GetAlarmDeviceCountsByTenant` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L17) |
| G010 | `POST /api/v1/alarm/config` | `api.Controllers.AlarmApi.CreateAlarmConfig` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:24](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L24) |
| G011 | `DELETE /api/v1/alarm/config/:id` | `api.Controllers.AlarmApi.DeleteAlarmConfig` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:27](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L27) |
| G012 | `PUT /api/v1/alarm/config` | `api.Controllers.AlarmApi.UpdateAlarmConfig` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:30](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L30) |
| G013 | `GET /api/v1/alarm/config` | `api.Controllers.AlarmApi.ServeAlarmConfigListByPage` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:33](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L33) |
| G014 | `PUT /api/v1/alarm/info` | `api.Controllers.AlarmApi.UpdateAlarmInfo` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:41](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L41) |
| G015 | `PUT /api/v1/alarm/info/batch` | `api.Controllers.AlarmApi.BatchUpdateAlarmInfo` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:44](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L44) |
| G016 | `GET /api/v1/alarm/info` | `api.Controllers.AlarmApi.HandleAlarmInfoListByPage` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:47](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L47) |
| G017 | `GET /api/v1/alarm/info/history` | `api.Controllers.AlarmApi.HandleAlarmHisttoryListByPage` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:49](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L49) |
| G018 | `PUT /api/v1/alarm/info/history` | `api.Controllers.AlarmApi.AlarmHistoryDescUpdate` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:51](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L51) |
| G019 | `GET /api/v1/alarm/info/history/device` | `api.Controllers.AlarmApi.HandleDeviceAlarmStatus` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:53](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L53) |
| G020 | `GET /api/v1/alarm/info/config/device` | `api.Controllers.AlarmApi.HandleConfigByDevice` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:55](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L55) |
| G021 | `GET /api/v1/alarm/info/history/:id` | `api.Controllers.AlarmApi.HandleAlarmInfoHistory` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:57](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L57) |
| G022 | `DELETE /api/v1/alarm/info/history/:id` | `api.Controllers.AlarmApi.DeleteAlarmHistory` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/alarm.go:60](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/alarm.go#L60) |
| G023 | `GET /api/v1/attribute/datas/:id` | `api.Controllers.AttributeDataApi.HandleDataList` | 属性读取 | **A04** | Gin业务/应用适配 | [router/apps/attribute_data.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/attribute_data.go#L15) |
| G024 | `GET /api/v1/attribute/datas/set/logs` | `api.Controllers.AttributeDataApi.HandleAttributeSetLogsDataListByPage` | 属性读取 | **A04** | Gin业务/应用适配 | [router/apps/attribute_data.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/attribute_data.go#L18) |
| G025 | `DELETE /api/v1/attribute/datas/:id` | `api.Controllers.AttributeDataApi.DeleteData` | 属性读取 | **A04** | Gin业务/应用适配 | [router/apps/attribute_data.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/attribute_data.go#L21) |
| G026 | `POST /api/v1/attribute/datas/pub` | `api.Controllers.AttributeDataApi.AttributePutMessage` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/attribute_data.go:24](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/attribute_data.go#L24) |
| G027 | `GET /api/v1/attribute/datas/get` | `api.Controllers.AttributeDataApi.AttributeGetMessage` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/attribute_data.go:27](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/attribute_data.go#L27) |
| G028 | `GET /api/v1/attribute/datas/key` | `api.Controllers.AttributeDataApi.HandleAttributeDataByKey` | 属性读取 | **A04** | Gin业务/应用适配 | [router/apps/attribute_data.go:30](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/attribute_data.go#L30) |
| G029 | `POST /api/v1/board` | `api.Controllers.BoardApi.CreateBoard` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L16) |
| G030 | `DELETE /api/v1/board/:id` | `api.Controllers.BoardApi.DeleteBoard` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L19) |
| G031 | `PUT /api/v1/board` | `api.Controllers.BoardApi.UpdateBoard` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L22) |
| G032 | `GET /api/v1/board` | `api.Controllers.BoardApi.HandleBoardListByPage` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L25) |
| G033 | `GET /api/v1/board/:id` | `api.Controllers.BoardApi.HandleBoard` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L28) |
| G034 | `GET /api/v1/board/home` | `api.Controllers.BoardApi.HandleBoardListByTenantId` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L31) |
| G035 | `GET /api/v1/board/trend` | `api.Controllers.BoardApi.GetDeviceTrend` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L34) |
| G036 | `GET /api/v1/board/device/total` | `api.Controllers.BoardApi.HandleDeviceTotal` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:48](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L48) |
| G037 | `GET /api/v1/board/device` | `api.Controllers.BoardApi.HandleDevice` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:50](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L50) |
| G038 | `GET /api/v1/board/tenant` | `api.Controllers.BoardApi.HandleTenant` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:56](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L56) |
| G039 | `GET /api/v1/board/tenant/user/info` | `api.Controllers.BoardApi.HandleTenantUserInfo` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:58](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L58) |
| G040 | `GET /api/v1/board/tenant/device/info` | `api.Controllers.BoardApi.HandleTenantDeviceInfo` | 看板统计 | **A11** | Gin业务/应用适配 | [router/apps/board.go:60](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L60) |
| G041 | `GET /api/v1/board/user/info` | `api.Controllers.BoardApi.HandleUserInfo` | 个人资料/改密 | **A01** | Gin业务/应用适配 | [router/apps/board.go:66](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L66) |
| G042 | `POST /api/v1/board/user/update` | `api.Controllers.BoardApi.UpdateUserInfo` | 个人资料/改密 | **A01** | Gin业务/应用适配 | [router/apps/board.go:68](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L68) |
| G043 | `POST /api/v1/board/user/update/password` | `api.Controllers.BoardApi.UpdateUserInfoPassword` | 个人资料/改密 | **A01** | Gin业务/应用适配 | [router/apps/board.go:70](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/board.go#L70) |
| G044 | `POST /api/v1/casbin/function` | `api.Controllers.CasbinApi.AddFunctionToRole` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L15) |
| G045 | `DELETE /api/v1/casbin/function/:id` | `api.Controllers.CasbinApi.DeleteFunctionFromRole` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L16) |
| G046 | `PUT /api/v1/casbin/function` | `api.Controllers.CasbinApi.UpdateFunctionFromRole` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L17) |
| G047 | `GET /api/v1/casbin/function` | `api.Controllers.CasbinApi.HandleFunctionFromRole` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L18) |
| G048 | `POST /api/v1/casbin/user` | `api.Controllers.CasbinApi.AddRoleToUser` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L21) |
| G049 | `DELETE /api/v1/casbin/user/:id` | `api.Controllers.CasbinApi.DeleteRolesFromUser` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L22) |
| G050 | `PUT /api/v1/casbin/user` | `api.Controllers.CasbinApi.UpdateRolesFromUser` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:23](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L23) |
| G051 | `GET /api/v1/casbin/user` | `api.Controllers.CasbinApi.HandleRolesFromUser` | 授权 | **A01** | Gin业务/应用适配 | [router/apps/casbin.go:24](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/casbin.go#L24) |
| G052 | `POST /api/v1/command/datas/plans/preview` | `api.Controllers.CommandPlanApi.Preview` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/command_data.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/command_data.go#L15) |
| G053 | `POST /api/v1/command/datas/plans/:plan_id/authorize` | `api.Controllers.CommandPlanApi.Authorize` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/command_data.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/command_data.go#L16) |
| G054 | `POST /api/v1/command/datas/plans/:plan_id/submit` | `api.Controllers.CommandPlanApi.Submit` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/command_data.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/command_data.go#L17) |
| G055 | `GET /api/v1/command/datas/set/logs` | `api.Controllers.CommandSetLogApi.ServeSetLogsDataListByPage` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/command_data.go:20](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/command_data.go#L20) |
| G056 | `POST /api/v1/command/datas/pub` | `api.Controllers.CommandSetLogApi.CommandPutMessage` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/command_data.go:23](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/command_data.go#L23) |
| G057 | `GET /api/v1/command/datas/status/:message_id` | `api.Controllers.CommandSetLogApi.GetCommandStatus` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/command_data.go:26](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/command_data.go#L26) |
| G058 | `GET /api/v1/command/datas/:id` | `api.Controllers.CommandSetLogApi.HandleCommandList` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/command_data.go:29](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/command_data.go#L29) |
| G059 | `GET /api/v1/dashboard-menu/:dashboardId` | `api.Controllers.DashboardMenuApi.GetDashboardMenu` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/dashboard_menu.go:14](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/dashboard_menu.go#L14) |
| G060 | `PUT /api/v1/dashboard-menu/:dashboardId` | `api.Controllers.DashboardMenuApi.SaveDashboardMenu` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/dashboard_menu.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/dashboard_menu.go#L15) |
| G061 | `DELETE /api/v1/dashboard-menu/:dashboardId` | `api.Controllers.DashboardMenuApi.DeleteDashboardMenu` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/dashboard_menu.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/dashboard_menu.go#L16) |
| G062 | `POST /api/v1/data_script` | `api.Controllers.DataScriptApi.CreateDataScript` | 数据脚本 | **A10** | Gin业务/应用适配 | [router/apps/data_script.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/data_script.go#L16) |
| G063 | `DELETE /api/v1/data_script/:id` | `api.Controllers.DataScriptApi.DeleteDataScript` | 数据脚本 | **A10** | Gin业务/应用适配 | [router/apps/data_script.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/data_script.go#L19) |
| G064 | `PUT /api/v1/data_script` | `api.Controllers.DataScriptApi.UpdateDataScript` | 数据脚本 | **A10** | Gin业务/应用适配 | [router/apps/data_script.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/data_script.go#L22) |
| G065 | `GET /api/v1/data_script` | `api.Controllers.DataScriptApi.HandleDataScriptListByPage` | 数据脚本 | **A10** | Gin业务/应用适配 | [router/apps/data_script.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/data_script.go#L25) |
| G066 | `POST /api/v1/data_script/quiz` | `api.Controllers.DataScriptApi.QuizDataScript` | 数据脚本 | **A10** | Gin业务/应用适配 | [router/apps/data_script.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/data_script.go#L28) |
| G067 | `PUT /api/v1/data_script/enable` | `api.Controllers.DataScriptApi.EnableDataScript` | 数据脚本 | **A10** | Gin业务/应用适配 | [router/apps/data_script.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/data_script.go#L31) |
| G068 | `PUT /api/v1/datapolicy` | `api.Controllers.DataPolicyApi.UpdateDataPolicy` | 保留期 | **S04** | Gin业务/应用适配 | [router/apps/datapolicy.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/datapolicy.go#L16) |
| G069 | `GET /api/v1/datapolicy` | `api.Controllers.DataPolicyApi.HandleDataPolicyListByPage` | 保留期 | **S04** | Gin业务/应用适配 | [router/apps/datapolicy.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/datapolicy.go#L19) |
| G070 | `POST /api/v1/device` | `api.Controllers.DeviceApi.CreateDevice` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L16) |
| G071 | `DELETE /api/v1/device/:id` | `api.Controllers.DeviceApi.DeleteDevice` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L19) |
| G072 | `PUT /api/v1/device` | `api.Controllers.DeviceApi.UpdateDevice` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L22) |
| G073 | `PUT /api/v1/device/active` | `api.Controllers.DeviceApi.ActiveDevice` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L25) |
| G074 | `GET /api/v1/device/detail/:id` | `api.Controllers.DeviceApi.HandleDeviceByID` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L28) |
| G075 | `GET /api/v1/device/telemetry/evidence/:id` | `api.Controllers.DeviceApi.BrokerTelemetryEvidence` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:29](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L29) |
| G076 | `GET /api/v1/device` | `api.Controllers.DeviceApi.HandleDeviceListByPage` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L31) |
| G077 | `GET /api/v1/device/check/:deviceNumber` | `api.Controllers.DeviceApi.CheckDeviceNumber` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L34) |
| G078 | `GET /api/v1/device/tenant/list` | `api.Controllers.DeviceApi.HandleTenantDeviceList` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:37](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L37) |
| G079 | `GET /api/v1/device/list` | `api.Controllers.DeviceApi.HandleDeviceList` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:40](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L40) |
| G080 | `POST /api/v1/device/son/add` | `api.Controllers.DeviceApi.CreateSonDevice` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:43](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L43) |
| G081 | `GET /api/v1/device/connect/form` | `api.Controllers.DeviceApi.DeviceConnectForm` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:46](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L46) |
| G082 | `GET /api/v1/device/connect/info` | `api.Controllers.DeviceApi.DeviceConnect` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:49](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L49) |
| G083 | `POST /api/v1/device/update/voucher` | `api.Controllers.DeviceApi.UpdateDeviceVoucher` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:52](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L52) |
| G084 | `GET /api/v1/device/sub-list/:id` | `api.Controllers.DeviceApi.HandleSubList` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:55](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L55) |
| G085 | `PUT /api/v1/device/sub-remove` | `api.Controllers.DeviceApi.RemoveSubDevice` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:58](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L58) |
| G086 | `GET /api/v1/device/metrics/:id` | `api.Controllers.DeviceApi.HandleMetrics` | 设备/产品 | **A02** | Gin consumer of native immutable model snapshot | [router/apps/device.go:61](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L61) |
| G087 | `GET /api/v1/device/metrics/menu` | `api.Controllers.DeviceApi.HandleActionByDeviceID` | 设备/产品 | **A02** | Gin consumer of native immutable model snapshot | [router/apps/device.go:64](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L64) |
| G088 | `GET /api/v1/device/metrics/condition/menu` | `api.Controllers.DeviceApi.HandleConditionByDeviceID` | 设备/产品 | **A02** | Gin consumer of native immutable model snapshot | [router/apps/device.go:67](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L67) |
| G089 | `GET /api/v1/device/map/telemetry/:id` | `api.Controllers.DeviceApi.HandleMapTelemetry` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:70](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L70) |
| G090 | `PUT /api/v1/device/update/config` | `api.Controllers.DeviceApi.UpdateDeviceConfig` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:73](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L73) |
| G091 | `GET /api/v1/device/online/status/:id` | `api.Controllers.DeviceApi.HandleDeviceOnlineStatus` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:76](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L76) |
| G092 | `GET /api/v1/device/status/evidence/:id` | `api.Controllers.DeviceApi.BrokerStatusEvidence` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:77](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L77) |
| G093 | `GET /api/v1/device/semantic/evidence/:id` | `api.Controllers.DeviceApi.BrokerSemanticEvidence` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:78](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L78) |
| G094 | `POST /api/v1/device/:device_id/debug` | `api.Controllers.DeviceDebugApi.SetDeviceDebug` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:81](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L81) |
| G095 | `GET /api/v1/device/:device_id/debug/status` | `api.Controllers.DeviceDebugApi.GetDeviceDebugStatus` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:82](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L82) |
| G096 | `GET /api/v1/device/:device_id/debug/logs` | `api.Controllers.DeviceDebugApi.GetDeviceDebugLogs` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:83](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L83) |
| G097 | `POST /api/v1/device/service/access/batch` | `api.Controllers.DeviceApi.CreateDeviceBatch` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:86](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L86) |
| G098 | `GET /api/v1/device/metrics/chart` | `api.Controllers.DeviceApi.HandleDeviceMetricsChart` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:89](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L89) |
| G099 | `GET /api/v1/device/selector` | `api.Controllers.DeviceApi.HandleDeviceSelector` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:92](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L92) |
| G100 | `GET /api/v1/device/telemetry/latest` | `api.Controllers.DeviceApi.HandleTenantTelemetryData` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:95](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L95) |
| G101 | `GET /api/v1/device/status/history` | `api.Controllers.DeviceApi.GetDeviceStatusHistory` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:98](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L98) |
| G102 | `POST /api/v1/device/topic-mappings` | `api.Controllers.DeviceTopicMappingApi.CreateDeviceTopicMapping` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:103](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L103) |
| G103 | `GET /api/v1/device/topic-mappings` | `api.Controllers.DeviceTopicMappingApi.GetDeviceTopicMappings` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:104](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L104) |
| G104 | `PUT /api/v1/device/topic-mappings/:id` | `api.Controllers.DeviceTopicMappingApi.UpdateDeviceTopicMapping` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:105](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L105) |
| G105 | `DELETE /api/v1/device/topic-mappings/:id` | `api.Controllers.DeviceTopicMappingApi.DeleteDeviceTopicMapping` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/apps/device.go:106](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L106) |
| G106 | `POST /api/v1/device/template` | `api.Controllers.DeviceApi.CreateDeviceTemplate` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:114](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L114) |
| G107 | `DELETE /api/v1/device/template/:id` | `api.Controllers.DeviceApi.DeleteDeviceTemplate` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:117](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L117) |
| G108 | `PUT /api/v1/device/template` | `api.Controllers.DeviceApi.UpdateDeviceTemplate` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:120](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L120) |
| G109 | `GET /api/v1/device/template/detail/:id` | `api.Controllers.DeviceApi.HandleDeviceTemplateById` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:123](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L123) |
| G110 | `GET /api/v1/device/template` | `api.Controllers.DeviceApi.HandleDeviceTemplateListByPage` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:126](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L126) |
| G111 | `GET /api/v1/device/template/menu` | `api.Controllers.DeviceApi.HandleDeviceTemplateMenu` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:129](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L129) |
| G112 | `GET /api/v1/device/template/stats` | `api.Controllers.DeviceApi.HandleDeviceTemplateStats` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:132](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L132) |
| G113 | `GET /api/v1/device/template/selector` | `api.Controllers.DeviceApi.HandleDeviceTemplateSelector` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:135](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L135) |
| G114 | `GET /api/v1/device/template/chart` | `api.Controllers.DeviceApi.HandleDeviceTemplateByDeviceId` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:138](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L138) |
| G115 | `GET /api/v1/device/template/chart/select` | `api.Controllers.DeviceApi.HandleDeviceTemplateChartSelect` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:141](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L141) |
| G116 | `POST /api/v1/device/template/market/login` | `api.Controllers.DeviceApi.MarketLogin` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:147](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L147) |
| G117 | `POST /api/v1/device/template/market/refresh` | `api.Controllers.DeviceApi.MarketRefresh` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:148](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L148) |
| G118 | `POST /api/v1/device/template/market/publish` | `api.Controllers.DeviceApi.PublishToMarket` | 市场 | **A08** | Gin market orchestration + native model branch | [router/apps/device.go:150](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L150) |
| G119 | `GET /api/v1/device/template/market/list` | `api.Controllers.DeviceApi.ListMarketTemplates` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:152](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L152) |
| G120 | `GET /api/v1/device/template/market/detail/:market_id` | `api.Controllers.DeviceApi.GetMarketTemplateDetail` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:154](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L154) |
| G121 | `POST /api/v1/device/template/market/install` | `api.Controllers.DeviceApi.InstallFromMarket` | 市场 | **A08** | Gin market orchestration + native model branch | [router/apps/device.go:156](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L156) |
| G122 | `POST /api/v1/device/market/dashboard-bundles/analyze` | `api.Controllers.DeviceApi.AnalyzeDashboardBundle` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:164](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L164) |
| G123 | `POST /api/v1/device/market/dashboard-bundles` | `api.Controllers.DeviceApi.PublishDashboardBundle` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:166](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L166) |
| G124 | `POST /api/v1/device/market/bundles/publish-draft` | `api.Controllers.DeviceApi.PublishBundleDraft` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:168](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L168) |
| G125 | `GET /api/v1/device/market/bundles` | `api.Controllers.DeviceApi.ListMarketBundles` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:170](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L170) |
| G126 | `GET /api/v1/device/market/bundles/:bundleKey` | `api.Controllers.DeviceApi.GetMarketBundleDetail` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:171](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L171) |
| G127 | `GET /api/v1/device/market/bundles/:bundleKey/precheck` | `api.Controllers.DeviceApi.GetMarketBundlePrecheck` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:172](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L172) |
| G128 | `POST /api/v1/device/market/bundles/download` | `api.Controllers.DeviceApi.DownloadDashboardTemplateFromMarket` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:174](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L174) |
| G129 | `POST /api/v1/device/market/bundles/install` | `api.Controllers.DeviceApi.InstallBundleFromMarket` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:176](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L176) |
| G130 | `GET /api/v1/device/market/bundles/install/:id` | `api.Controllers.DeviceApi.GetBundleInstallStatus` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:178](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L178) |
| G131 | `PUT /api/v1/device/market/bundles/install/:id/bindings` | `api.Controllers.DeviceApi.UpdateBundleBinding` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:180](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L180) |
| G132 | `POST /api/v1/device/market/bundles/install/:id/retry` | `api.Controllers.DeviceApi.RetryBundleInstall` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:182](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L182) |
| G133 | `POST /api/v1/device/market/bundles/install/:id/compensate` | `api.Controllers.DeviceApi.CompensateBundleInstall` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:184](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L184) |
| G134 | `GET /api/v1/device/market/bundles/installations` | `api.Controllers.DeviceApi.ListBundleInstallations` | 市场 | **A08** | Gin业务/应用适配 | [router/apps/device.go:186](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L186) |
| G135 | `GET /api/v1/device/dashboard-templates` | `api.Controllers.DeviceApi.ListLocalDashboardTemplates` | 看板模板 | **A08** | Gin业务/应用适配 | [router/apps/device.go:191](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L191) |
| G136 | `GET /api/v1/device/dashboard-templates/:id/compatible-devices` | `api.Controllers.DeviceApi.ListDashboardTemplateCompatibleDevices` | 看板模板 | **A08** | Gin业务/应用适配 | [router/apps/device.go:192](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L192) |
| G137 | `POST /api/v1/device/dashboard-templates/:id/instances` | `api.Controllers.DeviceApi.CreateDashboardFromLocalTemplate` | 看板模板 | **A08** | Gin业务/应用适配 | [router/apps/device.go:193](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L193) |
| G138 | `POST /api/v1/device/group` | `api.Controllers.DeviceApi.CreateDeviceGroup` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:200](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L200) |
| G139 | `DELETE /api/v1/device/group/:id` | `api.Controllers.DeviceApi.DeleteDeviceGroup` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:203](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L203) |
| G140 | `PUT /api/v1/device/group` | `api.Controllers.DeviceApi.UpdateDeviceGroup` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:206](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L206) |
| G141 | `GET /api/v1/device/group` | `api.Controllers.DeviceApi.HandleDeviceGroupByPage` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:209](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L209) |
| G142 | `GET /api/v1/device/group/tree` | `api.Controllers.DeviceApi.HandleDeviceGroupByTree` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:212](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L212) |
| G143 | `GET /api/v1/device/group/counts` | `api.Controllers.DeviceApi.HandleDeviceGroupCounts` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:215](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L215) |
| G144 | `GET /api/v1/device/group/detail/:id` | `api.Controllers.DeviceApi.HandleDeviceGroupByDetail` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:218](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L218) |
| G145 | `POST /api/v1/device/group/relation` | `api.Controllers.DeviceApi.CreateDeviceGroupRelation` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:225](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L225) |
| G146 | `DELETE /api/v1/device/group/relation` | `api.Controllers.DeviceApi.DeleteDeviceGroupRelation` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:227](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L227) |
| G147 | `GET /api/v1/device/group/relation/list` | `api.Controllers.DeviceApi.HandleDeviceGroupRelation` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:229](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L229) |
| G148 | `GET /api/v1/device/group/relation` | `api.Controllers.DeviceApi.HandleDeviceGroupListByDeviceId` | 设备分组 | **A02** | Gin业务/应用适配 | [router/apps/device.go:231](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L231) |
| G149 | `GET /api/v1/device/model/source/at/list` | `api.Controllers.DeviceModelApi.HandleModelSourceAT` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:238](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L238) |
| G150 | `POST /api/v1/device/model/telemetry` | `api.Controllers.DeviceModelApi.CreateDeviceModelTelemetry` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:241](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L241) |
| G151 | `DELETE /api/v1/device/model/telemetry/:id` | `api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:242](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L242) |
| G152 | `PUT /api/v1/device/model/telemetry` | `api.Controllers.DeviceModelApi.UpdateDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:243](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L243) |
| G153 | `GET /api/v1/device/model/telemetry` | `api.Controllers.DeviceModelApi.HandleDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:244](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L244) |
| G154 | `POST /api/v1/device/model/attributes` | `api.Controllers.DeviceModelApi.CreateDeviceModelAttributes` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:249](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L249) |
| G155 | `DELETE /api/v1/device/model/attributes/:id` | `api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:250](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L250) |
| G156 | `PUT /api/v1/device/model/attributes` | `api.Controllers.DeviceModelApi.UpdateDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:251](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L251) |
| G157 | `GET /api/v1/device/model/attributes` | `api.Controllers.DeviceModelApi.HandleDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:252](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L252) |
| G158 | `POST /api/v1/device/model/events` | `api.Controllers.DeviceModelApi.CreateDeviceModelEvents` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:257](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L257) |
| G159 | `DELETE /api/v1/device/model/events/:id` | `api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:258](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L258) |
| G160 | `PUT /api/v1/device/model/events` | `api.Controllers.DeviceModelApi.UpdateDeviceModelGeneralV2` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:259](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L259) |
| G161 | `GET /api/v1/device/model/events` | `api.Controllers.DeviceModelApi.HandleDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:260](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L260) |
| G162 | `POST /api/v1/device/model/commands` | `api.Controllers.DeviceModelApi.CreateDeviceModelCommands` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:265](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L265) |
| G163 | `DELETE /api/v1/device/model/commands/:id` | `api.Controllers.DeviceModelApi.DeleteDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:266](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L266) |
| G164 | `PUT /api/v1/device/model/commands` | `api.Controllers.DeviceModelApi.UpdateDeviceModelGeneralV2` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:267](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L267) |
| G165 | `GET /api/v1/device/model/commands` | `api.Controllers.DeviceModelApi.HandleDeviceModelGeneral` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:268](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L268) |
| G166 | `POST /api/v1/device/model/custom/commands` | `api.Controllers.DeviceModelApi.CreateDeviceModelCustomCommands` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:274](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L274) |
| G167 | `DELETE /api/v1/device/model/custom/commands/:id` | `api.Controllers.DeviceModelApi.DeleteDeviceModelCustomCommands` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:275](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L275) |
| G168 | `PUT /api/v1/device/model/custom/commands` | `api.Controllers.DeviceModelApi.UpdateDeviceModelCustomCommands` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:276](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L276) |
| G169 | `GET /api/v1/device/model/custom/commands` | `api.Controllers.DeviceModelApi.HandleDeviceModelCustomCommandsByPage` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:277](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L277) |
| G170 | `GET /api/v1/device/model/custom/commands/:deviceId` | `api.Controllers.DeviceModelApi.HandleDeviceModelCustomCommandsByDeviceId` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:278](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L278) |
| G171 | `POST /api/v1/device/model/custom/control` | `api.Controllers.DeviceModelApi.CreateDeviceModelCustomControl` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:284](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L284) |
| G172 | `DELETE /api/v1/device/model/custom/control/:id` | `api.Controllers.DeviceModelApi.DeleteDeviceModelCustomControl` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:285](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L285) |
| G173 | `PUT /api/v1/device/model/custom/control` | `api.Controllers.DeviceModelApi.UpdateDeviceModelCustomControl` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:286](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L286) |
| G174 | `GET /api/v1/device/model/custom/control` | `api.Controllers.DeviceModelApi.HandleDeviceModelCustomControl` | 旧模型/模板 | **A03** | Gin legacy model/template tables; assess retirement | [router/apps/device.go:287](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device.go#L287) |
| G175 | `POST /api/v1/device_config` | `api.Controllers.DeviceConfigApi.CreateDeviceConfig` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L16) |
| G176 | `DELETE /api/v1/device_config/:id` | `api.Controllers.DeviceConfigApi.DeleteDeviceConfig` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L19) |
| G177 | `PUT /api/v1/device_config` | `api.Controllers.DeviceConfigApi.UpdateDeviceConfig` | 设备配置 | **A02** | Gin config owner + native snapshot validation/CAS | [router/apps/device_config.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L22) |
| G178 | `GET /api/v1/device_config` | `api.Controllers.DeviceConfigApi.HandleDeviceConfigListByPage` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L25) |
| G179 | `GET /api/v1/device_config/menu` | `api.Controllers.DeviceConfigApi.HandleDeviceConfigListMenu` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L28) |
| G180 | `GET /api/v1/device_config/:id` | `api.Controllers.DeviceConfigApi.HandleDeviceConfigById` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L31) |
| G181 | `PUT /api/v1/device_config/batch` | `api.Controllers.DeviceConfigApi.BatchUpdateDeviceConfig` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L34) |
| G182 | `GET /api/v1/device_config/connect` | `api.Controllers.DeviceConfigApi.HandleDeviceConfigConnect` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:37](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L37) |
| G183 | `GET /api/v1/device_config/voucher_type` | `api.Controllers.DeviceConfigApi.HandleVoucherType` | 设备配置 | **A02** | Gin业务/应用适配 | [router/apps/device_config.go:40](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L40) |
| G184 | `GET /api/v1/device_config/metrics/menu` | `api.Controllers.DeviceConfigApi.HandleActionByDeviceConfigID` | 设备配置 | **A02** | Gin consumer of native immutable model snapshot | [router/apps/device_config.go:43](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L43) |
| G185 | `GET /api/v1/device_config/metrics/condition/menu` | `api.Controllers.DeviceConfigApi.HandleConditionByDeviceConfigID` | 设备配置 | **A02** | Gin consumer of native immutable model snapshot | [router/apps/device_config.go:46](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/device_config.go#L46) |
| G186 | `GET /api/v1/event/datas` | `api.Controllers.EventDataApi.HandleEventDatasListByPage` | 事件 | **A04** | Gin业务/应用适配 | [router/apps/event_data.go:14](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/event_data.go#L14) |
| G187 | `GET /api/v1/expected/data/list` | `api.Controllers.ExpectedDataApi.HandleExpectedDataList` | 期望值 | **A05** | Gin业务/应用适配 | [router/apps/expected_data.go:14](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/expected_data.go#L14) |
| G188 | `POST /api/v1/expected/data` | `api.Controllers.ExpectedDataApi.CreateExpectedData` | 期望值 | **A05** | Gin业务/应用适配 | [router/apps/expected_data.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/expected_data.go#L15) |
| G189 | `DELETE /api/v1/expected/data/:id` | `api.Controllers.ExpectedDataApi.DeleteExpectedData` | 期望值 | **A05** | Gin业务/应用适配 | [router/apps/expected_data.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/expected_data.go#L16) |
| G190 | `PUT /api/v1/logo` | `api.Controllers.LogoApi.UpdateLogo` | 系统配置 | **A01** | Gin业务/应用适配 | [router/apps/logo.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/logo.go#L16) |
| G191 | `POST /api/v1/message_push` | `api.Controllers.MessagePushApi.CreateMessagePush` | App推送会话 | **A07** | Gin业务/应用适配 | [router/apps/message_push.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/message_push.go#L15) |
| G192 | `POST /api/v1/message_push/logout` | `api.Controllers.MessagePushApi.MessagePushMangeLogout` | App推送会话 | **A07** | Gin业务/应用适配 | [router/apps/message_push.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/message_push.go#L17) |
| G193 | `GET /api/v1/message_push/config` | `api.Controllers.MessagePushApi.GetMessagePushConfig` | App推送会话 | **A07** | Gin业务/应用适配 | [router/apps/message_push.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/message_push.go#L19) |
| G194 | `POST /api/v1/message_push/config` | `api.Controllers.MessagePushApi.SetMessagePushConfig` | App推送会话 | **A07** | Gin业务/应用适配 | [router/apps/message_push.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/message_push.go#L21) |
| G195 | `GET /api/v1/notification-default-policy` | `api.Controllers.NotificationGroupApi.GetTenantDefaultPolicy` | 通知发布/策略桥 | **A07** | Gin policy adapter for native notification | [router/apps/notification_groups.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L16) |
| G196 | `PUT /api/v1/notification-default-policy` | `api.Controllers.NotificationGroupApi.PutTenantDefaultPolicy` | 通知发布/策略桥 | **A07** | Gin policy adapter for native notification | [router/apps/notification_groups.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L17) |
| G197 | `GET /api/v1/notification_group/native-publish` | `api.Controllers.NotificationGroupApi.GetNativePublishStatus` | 通知发布/策略桥 | **A07** | 调用原生通知的发布桥 | [router/apps/notification_groups.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L21) |
| G198 | `POST /api/v1/notification_group/native-publish` | `api.Controllers.NotificationGroupApi.ApplyNativePublish` | 通知发布/策略桥 | **A07** | 调用原生通知的发布桥 | [router/apps/notification_groups.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L22) |
| G199 | `POST /api/v1/notification_group` | `api.Controllers.NotificationGroupApi.CreateNotificationGroup` | 通知旧入口 | **A07** | 已退役410；清理消费者 | [router/apps/notification_groups.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L25) |
| G200 | `DELETE /api/v1/notification_group/:id` | `api.Controllers.NotificationGroupApi.DeleteNotificationGroup` | 通知旧入口 | **A07** | 已退役410；清理消费者 | [router/apps/notification_groups.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L28) |
| G201 | `PUT /api/v1/notification_group/:id` | `api.Controllers.NotificationGroupApi.UpdateNotificationGroup` | 通知旧入口 | **A07** | 已退役410；清理消费者 | [router/apps/notification_groups.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L31) |
| G202 | `GET /api/v1/notification_group/list` | `api.Controllers.NotificationGroupApi.HandleNotificationGroupListByPage` | 通知旧入口 | **A07** | Gin业务/应用适配 | [router/apps/notification_groups.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L34) |
| G203 | `GET /api/v1/notification_group/:id` | `api.Controllers.NotificationGroupApi.HandleNotificationGroupById` | 通知旧入口 | **A07** | Gin业务/应用适配 | [router/apps/notification_groups.go:37](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_groups.go#L37) |
| G204 | `GET /api/v1/notification_history/list` | `api.Controllers.NotificationHistoryApi.HandleNotificationHistoryListByPage` | 通知旧入口 | **A07** | Gin业务/应用适配 | [router/apps/notification_history.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_history.go#L17) |
| G205 | `POST /api/v1/notification/services/config` | `api.Controllers.NotificationServicesConfigApi.SaveNotificationServicesConfig` | 通知旧入口 | **A07** | Gin业务/应用适配 | [router/apps/notification_services_config.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_services_config.go#L15) |
| G206 | `GET /api/v1/notification/services/config/:type` | `api.Controllers.NotificationServicesConfigApi.HandleNotificationServicesConfig` | 通知旧入口 | **A07** | Gin业务/应用适配 | [router/apps/notification_services_config.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_services_config.go#L18) |
| G207 | `POST /api/v1/notification/services/config/e-mail/test` | `api.Controllers.NotificationServicesConfigApi.SendTestEmail` | 通知旧入口 | **A07** | Gin业务/应用适配 | [router/apps/notification_services_config.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/notification_services_config.go#L21) |
| G208 | `POST /api/v1/open/keys` | `api.Controllers.OpenAPIKeyApi.CreateOpenAPIKey` | APIKey | **A01** | Gin业务/应用适配 | [router/apps/open_api_keys.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/open_api_keys.go#L16) |
| G209 | `GET /api/v1/open/keys` | `api.Controllers.OpenAPIKeyApi.GetOpenAPIKeyList` | APIKey | **A01** | Gin业务/应用适配 | [router/apps/open_api_keys.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/open_api_keys.go#L17) |
| G210 | `PUT /api/v1/open/keys` | `api.Controllers.OpenAPIKeyApi.UpdateOpenAPIKey` | APIKey | **A01** | Gin业务/应用适配 | [router/apps/open_api_keys.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/open_api_keys.go#L18) |
| G211 | `DELETE /api/v1/open/keys/:id` | `api.Controllers.OpenAPIKeyApi.DeleteOpenAPIKey` | APIKey | **A01** | Gin业务/应用适配 | [router/apps/open_api_keys.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/open_api_keys.go#L19) |
| G212 | `GET /api/v1/operation_logs` | `api.Controllers.OperationLogsApi.HandleListByPage` | 平台监控 | **A11** | Gin业务/应用适配 | [router/apps/operation_logs.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/operation_logs.go#L15) |
| G213 | `POST /api/v1/ota/package` | `api.Controllers.OTAApi.CreateOTAUpgradePackage` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L16) |
| G214 | `DELETE /api/v1/ota/package/:id` | `api.Controllers.OTAApi.DeleteOTAUpgradePackage` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L17) |
| G215 | `PUT /api/v1/ota/package` | `api.Controllers.OTAApi.UpdateOTAUpgradePackage` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L18) |
| G216 | `GET /api/v1/ota/package` | `api.Controllers.OTAApi.HandleOTAUpgradePackageByPage` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L19) |
| G217 | `POST /api/v1/ota/task` | `api.Controllers.OTAApi.CreateOTAUpgradeTask` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:24](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L24) |
| G218 | `DELETE /api/v1/ota/task/:id` | `api.Controllers.OTAApi.DeleteOTAUpgradeTask` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:26](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L26) |
| G219 | `GET /api/v1/ota/task` | `api.Controllers.OTAApi.HandleOTAUpgradeTaskByPage` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L28) |
| G220 | `GET /api/v1/ota/task/detail` | `api.Controllers.OTAApi.HandleOTAUpgradeTaskDetailByPage` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:30](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L30) |
| G221 | `PUT /api/v1/ota/task/detail` | `api.Controllers.OTAApi.UpdateOTAUpgradeTaskStatus` | OTA | **A05** | Gin业务/应用适配 | [router/apps/ota.go:32](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/ota.go#L32) |
| G222 | `GET /api/v1/protocol_plugin/config_form` | `api.Controllers.ProtocolPluginApi.HandleProtocolPluginFormByProtocolType` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/protocol_plugin.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/protocol_plugin.go#L15) |
| G223 | `POST /api/v1/role` | `api.Controllers.RoleApi.CreateRole` | 角色 | **A01** | Gin业务/应用适配 | [router/apps/role.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/role.go#L16) |
| G224 | `DELETE /api/v1/role/:id` | `api.Controllers.RoleApi.DeleteRole` | 角色 | **A01** | Gin业务/应用适配 | [router/apps/role.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/role.go#L19) |
| G225 | `PUT /api/v1/role` | `api.Controllers.RoleApi.UpdateRole` | 角色 | **A01** | Gin业务/应用适配 | [router/apps/role.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/role.go#L22) |
| G226 | `GET /api/v1/role` | `api.Controllers.RoleApi.HandleRoleListByPage` | 角色 | **A01** | Gin业务/应用适配 | [router/apps/role.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/role.go#L25) |
| G227 | `POST /api/v1/scene` | `api.Controllers.SceneApi.CreateScene` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene.go#L15) |
| G228 | `DELETE /api/v1/scene/:id` | `api.Controllers.SceneApi.DeleteScene` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene.go#L18) |
| G229 | `GET /api/v1/scene` | `api.Controllers.SceneApi.HandleSceneByPage` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene.go#L21) |
| G230 | `GET /api/v1/scene/detail/:id` | `api.Controllers.SceneApi.HandleScene` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene.go:24](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene.go#L24) |
| G231 | `PUT /api/v1/scene` | `api.Controllers.SceneApi.UpdateScene` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene.go:27](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene.go#L27) |
| G232 | `POST /api/v1/scene/active/:id` | `api.Controllers.SceneApi.ActiveScene` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene.go:30](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene.go#L30) |
| G233 | `GET /api/v1/scene/log` | `api.Controllers.SceneApi.HandleSceneLog` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene.go:33](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene.go#L33) |
| G234 | `POST /api/v1/scene_automations` | `api.Controllers.SceneAutomationsApi.CreateSceneAutomations` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L15) |
| G235 | `DELETE /api/v1/scene_automations/:id` | `api.Controllers.SceneAutomationsApi.DeleteSceneAutomations` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L18) |
| G236 | `PUT /api/v1/scene_automations` | `api.Controllers.SceneAutomationsApi.UpdateSceneAutomations` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L21) |
| G237 | `POST /api/v1/scene_automations/switch/:id` | `api.Controllers.SceneAutomationsApi.SwitchSceneAutomations` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:24](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L24) |
| G238 | `GET /api/v1/scene_automations/list` | `api.Controllers.SceneAutomationsApi.HandleSceneAutomationsByPage` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:27](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L27) |
| G239 | `GET /api/v1/scene_automations/detail/:id` | `api.Controllers.SceneAutomationsApi.HandleSceneAutomations` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:30](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L30) |
| G240 | `GET /api/v1/scene_automations/log` | `api.Controllers.SceneAutomationsApi.HandleSceneAutomationsLog` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:33](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L33) |
| G241 | `GET /api/v1/scene_automations/alarm` | `api.Controllers.SceneAutomationsApi.HandleSceneAutomationsWithAlarmByPage` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/apps/scene_automations.go:36](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/scene_automations.go#L36) |
| G242 | `POST /api/v1/service` | `api.Controllers.ServicePluginApi.Create` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L15) |
| G243 | `GET /api/v1/service/list` | `api.Controllers.ServicePluginApi.HandleList` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L17) |
| G244 | `GET /api/v1/service/detail/:id` | `api.Controllers.ServicePluginApi.Handle` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L19) |
| G245 | `PUT /api/v1/service` | `api.Controllers.ServicePluginApi.Update` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L21) |
| G246 | `DELETE /api/v1/service/:id` | `api.Controllers.ServicePluginApi.Delete` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:23](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L23) |
| G247 | `GET /api/v1/service/plugin/select` | `api.Controllers.ServicePluginApi.HandleServiceSelect` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L25) |
| G248 | `GET /api/v1/service/plugin/info` | `api.Controllers.ServicePluginApi.HandleServicePluginByServiceIdentifier` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:27](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L27) |
| G249 | `POST /api/v1/service/access` | `api.Controllers.ServiceAccessApi.Create` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:30](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L30) |
| G250 | `GET /api/v1/service/access/list` | `api.Controllers.ServiceAccessApi.HandleList` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:32](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L32) |
| G251 | `PUT /api/v1/service/access` | `api.Controllers.ServiceAccessApi.Update` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L34) |
| G252 | `DELETE /api/v1/service/access/:id` | `api.Controllers.ServiceAccessApi.Delete` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:36](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L36) |
| G253 | `GET /api/v1/service/access/voucher/form` | `api.Controllers.ServiceAccessApi.HandleVoucherForm` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:38](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L38) |
| G254 | `GET /api/v1/service/access/device/list` | `api.Controllers.ServiceAccessApi.HandleDeviceList` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/apps/service_plugin.go:40](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/service_plugin.go#L40) |
| G255 | `POST /api/v1/dict/column` | `api.Controllers.DictApi.CreateDictColumn` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L16) |
| G256 | `POST /api/v1/dict/language` | `api.Controllers.CreateDictLanguage` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L19) |
| G257 | `GET /api/v1/dict/enum` | `api.Controllers.DictApi.HandleDict` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L22) |
| G258 | `GET /api/v1/dict` | `api.Controllers.DictApi.HandleDictLisyByPage` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L25) |
| G259 | `GET /api/v1/dict/language/:id` | `api.Controllers.HandleDictLanguage` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L28) |
| G260 | `DELETE /api/v1/dict/column/:id` | `api.Controllers.DictApi.DeleteDictColumn` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L31) |
| G261 | `DELETE /api/v1/dict/language/:id` | `api.Controllers.DictApi.DeleteDictLanguage` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L34) |
| G262 | `GET /api/v1/dict/protocol/service` | `api.Controllers.DictApi.HandleProtocolAndService` | 字典 | **A01** | Gin业务/应用适配 | [router/apps/sys_dict.go:37](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_dict.go#L37) |
| G263 | `PUT /api/v1/sys_function/:id` | `api.Controllers.SysFunctionApi.UpdateSysFcuntion` | 系统配置 | **A01** | Gin业务/应用适配 | [router/apps/sys_function.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_function.go#L15) |
| G264 | `POST /api/v1/ui_elements` | `api.Controllers.UiElementsApi.CreateUiElements` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/sys_ui_elements.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_ui_elements.go#L16) |
| G265 | `DELETE /api/v1/ui_elements/:id` | `api.Controllers.UiElementsApi.DeleteUiElements` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/sys_ui_elements.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_ui_elements.go#L19) |
| G266 | `PUT /api/v1/ui_elements` | `api.Controllers.UiElementsApi.UpdateUiElements` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/sys_ui_elements.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_ui_elements.go#L22) |
| G267 | `GET /api/v1/ui_elements` | `api.Controllers.UiElementsApi.ServeUiElementsListByPage` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/sys_ui_elements.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_ui_elements.go#L25) |
| G268 | `GET /api/v1/ui_elements/menu` | `api.Controllers.UiElementsApi.ServeUiElementsListByAuthority` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/sys_ui_elements.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_ui_elements.go#L28) |
| G269 | `GET /api/v1/ui_elements/select/form` | `api.Controllers.UiElementsApi.ServeUiElementsListByTenant` | 菜单/UI | **A01** | Gin业务/应用适配 | [router/apps/sys_ui_elements.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_ui_elements.go#L31) |
| G270 | `GET /api/v1/user/detail` | `api.Controllers.UserApi.HandleUserDetail` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L16) |
| G271 | `PUT /api/v1/user/update` | `api.Controllers.UserApi.UpdateUsers` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L17) |
| G272 | `GET /api/v1/user/logout` | `api.Controllers.UserApi.Logout` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:18](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L18) |
| G273 | `GET /api/v1/user/refresh` | `api.Controllers.UserApi.RefreshToken` | 账户/身份 | **A01** | 旧路径；native auth时503 | [router/apps/sys_user.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L19) |
| G274 | `GET /api/v1/user` | `api.Controllers.UserApi.HandleUserListByPage` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L22) |
| G275 | `GET /api/v1/user/tenant/statistics` | `api.Controllers.UserApi.HandleTenantStatistics` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:23](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L23) |
| G276 | `POST /api/v1/user` | `api.Controllers.UserApi.CreateUser` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:24](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L24) |
| G277 | `PUT /api/v1/user` | `api.Controllers.UserApi.UpdateUser` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L25) |
| G278 | `DELETE /api/v1/user/:id` | `api.Controllers.UserApi.DeleteUser` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:26](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L26) |
| G279 | `GET /api/v1/user/:id` | `api.Controllers.UserApi.HandleUser` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:27](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L27) |
| G280 | `POST /api/v1/user/transform` | `api.Controllers.UserApi.TransformUser` | 账户/身份 | **A01** | 旧路径；native auth时503 | [router/apps/sys_user.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L28) |
| G281 | `PUT /api/v1/user/address/:id` | `api.Controllers.UserApi.UpdateUserAddress` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:31](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L31) |
| G282 | `GET /api/v1/user/tenant/id` | `api.Controllers.UserApi.GetTenantID` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L34) |
| G283 | `GET /api/v1/user/selector` | `api.Controllers.UserApi.GetUserSelector` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/apps/sys_user.go:37](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/sys_user.go#L37) |
| G284 | `GET /api/v1/system/metrics/current` | `api.Controllers.SystemMonitorApi.GetCurrentSystemMetrics` | 平台监控 | **A11** | Gin业务/应用适配 | [router/apps/system_monitor.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/system_monitor.go#L16) |
| G285 | `GET /api/v1/system/metrics/history` | `api.Controllers.SystemMonitorApi.GetHistorySystemMetrics` | 平台监控 | **A11** | Gin业务/应用适配 | [router/apps/system_monitor.go:17](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/system_monitor.go#L17) |
| G286 | `GET /api/v1/telemetry/datas/current/:id` | `api.Controllers.TelemetryDataApi.HandleCurrentData` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:16](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L16) |
| G287 | `GET /api/v1/telemetry/datas/current/keys` | `api.Controllers.TelemetryDataApi.HandleCurrentDataKeys` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:19](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L19) |
| G288 | `GET /api/v1/telemetry/datas/current/detail/:id` | `api.Controllers.TelemetryDataApi.ServeCurrentDetailData` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L22) |
| G289 | `GET /api/v1/telemetry/datas/history` | `api.Controllers.TelemetryDataApi.ServeHistoryData` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L25) |
| G290 | `GET /api/v1/telemetry/datas/history/pagination` | `api.Controllers.TelemetryDataApi.ServeHistoryDataByPage` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L28) |
| G291 | `GET /api/v1/telemetry/datas/history/page` | `api.Controllers.TelemetryDataApi.ServeHistoryDataByPage` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:30](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L30) |
| G292 | `DELETE /api/v1/telemetry/datas` | `api.Controllers.TelemetryDataApi.DeleteData` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:33](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L33) |
| G293 | `GET /api/v1/telemetry/datas/statistic` | `api.Controllers.TelemetryDataApi.ServeStatisticData` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:36](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L36) |
| G294 | `GET /api/v1/telemetry/datas/statistic/batch` | `api.Controllers.TelemetryDataApi.ServeStatisticDataByDeviceId` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:39](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L39) |
| G295 | `GET /api/v1/telemetry/datas/set/logs` | `api.Controllers.TelemetryDataApi.ServeSetLogsDataListByPage` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:42](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L42) |
| G296 | `POST /api/v1/telemetry/datas/pub` | `api.Controllers.TelemetryDataApi.TelemetryPutMessage` | 下行/计划 | **A05** | Gin业务/应用适配 | [router/apps/telemetry_data.go:47](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L47) |
| G297 | `GET /api/v1/telemetry/datas/simulation` | `api.Controllers.TelemetryDataApi.ServeEchoData` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:50](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L50) |
| G298 | `POST /api/v1/telemetry/datas/simulation` | `api.Controllers.TelemetryDataApi.SimulationTelemetryData` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:53](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L53) |
| G299 | `GET /api/v1/telemetry/datas/simulation/init` | `api.Controllers.TelemetryDataApi.GetSimulationInit` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:56](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L56) |
| G300 | `POST /api/v1/telemetry/datas/simulation/send` | `api.Controllers.TelemetryDataApi.SimulationSend` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:59](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L59) |
| G301 | `GET /api/v1/telemetry/datas/msg/count` | `api.Controllers.TelemetryDataApi.ServeMsgCountByTenant` | 遥测读取 | **A04** | Gin业务/应用适配 | [router/apps/telemetry_data.go:62](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/telemetry_data.go#L62) |
| G302 | `POST /api/v1/file/up` | `api.Controllers.UpLoadApi.UpFile` | 文件 | **A11** | Gin业务/应用适配 | [router/apps/upload.go:15](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/apps/upload.go#L15) |
| G303 | `GET /swagger/*any` | `ginSwagger.WrapHandler` | 平台监控 | **A11** | Gin业务/应用适配 | [router/router_init.go:38](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L38) |
| G304 | `GET /metrics` | `gin.WrapH` | 平台监控 | **A11** | Gin业务/应用适配 | [router/router_init.go:51](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L51) |
| G305 | `GET /metrics-viewer` | `StaticFile` | 平台监控 | **A11** | Gin静态资源GET/HEAD | [router/router_init.go:57](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L57) |
| G306 | `HEAD /metrics-viewer` | `StaticFile` | 平台监控 | **A11** | Gin静态资源GET/HEAD | [router/router_init.go:57](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L57) |
| G307 | `GET /files/*filepath` | `inline handler` | 文件 | **A11** | Gin业务/应用适配 | [router/router_init.go:60](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L60) |
| G308 | `GET /api/v1/notification/session-context` | `controllers.NotificationIdentityApi.SessionContext` | 通知来源读取 | **A07** | Encore依赖的来源适配 | [router/router_init.go:80](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L80) |
| G309 | `GET /api/v1/auth/login-options` | `inline capability handler` | 账户/身份 | **A01** | 旧能力页；native auth时503 | [router/router_init.go:87](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L87) |
| G310 | `GET /api/v1/auth/external/providers` | `inline capability handler` | 账户/身份 | **A01** | 旧能力页；native auth时503 | [router/router_init.go:87](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L87) |
| G311 | `GET /api/v1/auth/admin/providers` | `inline handler` | 账户/身份 | **A01** | 旧能力页；native auth时503 | [router/router_init.go:100](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L100) |
| G312 | `GET /api/v1/notification/source-members/:id` | `reader.GetMemberContact` | 通知来源读取 | **A07** | Encore依赖的来源适配 | [router/router_init.go:114](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L114) |
| G313 | `GET /api/v1/notification/source-app-members/:id` | `reader.GetAppMemberDevices` | 通知来源读取 | **A07** | Encore依赖的来源适配 | [router/router_init.go:115](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L115) |
| G314 | `GET /api/v1/notification/source-app-devices/:id` | `reader.GetAppDevice` | 通知来源读取 | **A07** | Encore依赖的来源适配 | [router/router_init.go:116](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L116) |
| G315 | `GET /api/v1/notification/source-tenants/:id` | `reader.GetTenantPresence` | 通知来源读取 | **A07** | Encore依赖的来源适配 | [router/router_init.go:117](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L117) |
| G316 | `GET /api/v1/notification/source-policies/:id` | `reader.GetAutomationPolicy` | 通知来源读取 | **A07** | Encore依赖的来源适配 | [router/router_init.go:118](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L118) |
| G317 | `GET /health` | `controllers.SystemApi.HealthCheck` | 平台监控 | **A11** | Gin业务/应用适配 | [router/router_init.go:138](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L138) |
| G318 | `POST /api/v1/plugin/heartbeat` | `controllers.Heartbeat` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/router_init.go:146](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L146) |
| G319 | `POST /api/v1/plugin/device/config` | `controllers.HandleDeviceConfigForProtocolPlugin` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/router_init.go:147](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L147) |
| G320 | `POST /api/v1/plugin/devices` | `controllers.HandleDeviceConfigForProtocolPluginByProtocolType` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/router_init.go:148](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L148) |
| G321 | `POST /api/v1/plugin/service/access/list` | `controllers.HandlePluginServiceAccessList` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/router_init.go:149](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L149) |
| G322 | `POST /api/v1/plugin/service/access` | `controllers.HandlePluginServiceAccess` | 协议/服务插件 | **A10** | Gin业务/应用适配 | [router/router_init.go:150](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L150) |
| G323 | `POST /api/v1/login` | `controllers.Login` | 账户/身份 | **A01** | 旧路径；native auth时503 | [router/router_init.go:151](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L151) |
| G324 | `GET /api/v1/verification/code` | `controllers.HandleVerificationCode` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/router_init.go:152](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L152) |
| G325 | `POST /api/v1/reset/password` | `controllers.ResetPassword` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/router_init.go:153](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L153) |
| G326 | `GET /api/v1/logo` | `controllers.HandleLogoList` | 系统配置 | **A01** | Gin业务/应用适配 | [router/router_init.go:154](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L154) |
| G327 | `GET /api/v1/telemetry/datas/current/ws` | `controllers.TelemetryDataApi.ServeCurrentDataByWS` | 实时连接 | **A04** | Gin业务/应用适配 | [router/router_init.go:156](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L156) |
| G328 | `GET /api/v1/device/online/status/ws` | `controllers.TelemetryDataApi.ServeDeviceStatusByWS` | 实时连接 | **A04** | Gin业务/应用适配 | [router/router_init.go:158](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L158) |
| G329 | `GET /api/v1/device/online/status/ws/batch` | `controllers.TelemetryDataApi.ServeDeviceOnlineStatusWS` | 实时连接 | **A04** | Gin业务/应用适配 | [router/router_init.go:160](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L160) |
| G330 | `GET /api/v1/telemetry/datas/current/keys/ws` | `controllers.TelemetryDataApi.ServeCurrentDataByKey` | 实时连接 | **A04** | Gin业务/应用适配 | [router/router_init.go:162](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L162) |
| G331 | `GET /api/v1/ota/download/files/upgradePackage/:path/:file` | `controllers.OTAApi.DownloadOTAUpgradePackage` | OTA | **A05** | Gin业务/应用适配 | [router/router_init.go:163](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L163) |
| G332 | `GET /api/v1/systime` | `controllers.SystemApi.HandleSystime` | 系统配置 | **A11** | Gin业务/应用适配 | [router/router_init.go:165](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L165) |
| G333 | `GET /api/v1/sys_function` | `controllers.SysFunctionApi.HandleSysFcuntion` | 系统配置 | **A01** | Gin业务/应用适配 | [router/router_init.go:167](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L167) |
| G334 | `POST /api/v1/tenant/email/register` | `controllers.UserApi.EmailRegister` | 账户/身份 | **A01** | 旧路径；native auth时503 | [router/router_init.go:169](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L169) |
| G335 | `GET /api/v1/tenant/has-admin` | `controllers.UserApi.HasAdmin` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/router_init.go:171](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L171) |
| G336 | `GET /api/v1/tenant/setup-state` | `controllers.UserApi.SetupState` | 账户/身份 | **A01** | Gin业务/应用适配 | [router/router_init.go:173](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L173) |
| G337 | `POST /api/v1/tenant/super-admin/init` | `controllers.UserApi.InitSuperAdmin` | 账户/身份 | **A01** | 旧路径；native auth时503 | [router/router_init.go:175](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L175) |
| G338 | `POST /api/v1/tenant/market-register` | `controllers.UserApi.MarketRegister` | 账户/身份 | **A01** | 旧路径；native auth时503 | [router/router_init.go:177](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L177) |
| G339 | `POST /api/v1/device/gateway-register` | `controllers.DeviceApi.GatewayRegister` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/router_init.go:179](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L179) |
| G340 | `POST /api/v1/device/gateway-sub-register` | `controllers.DeviceApi.GatewaySubRegister` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/router_init.go:181](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L181) |
| G341 | `GET /api/v1/sys_version` | `controllers.SystemApi.HandleSysVersion` | 系统配置 | **A11** | Gin业务/应用适配 | [router/router_init.go:183](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L183) |
| G342 | `POST /api/v1/device/auth` | `controllers.DeviceAuthApi.DeviceAuth` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/router_init.go:185](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L185) |
| G343 | `GET /api/v1/devices/:device_id/diagnostics` | `controllers.DeviceApi.GetDeviceDiagnostics` | 设备/产品 | **A02** | Gin业务/应用适配 | [router/router_init.go:187](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L187) |
| G344 | `GET /api/v1/automation/runtime` | `inline handler` | 旧自动化/告警 | **A06** | Gin业务/应用适配 | [router/router_init.go:195](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go#L195) |
| G345 | `GET /api/v1/events` | `sseApi.HandleSystemEvents` | 实时连接 | **A11** | Gin业务/应用适配 | [router/sse.go:13](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/sse.go#L13) |
| G346 | `GET /api/v1/compat/scene` | `automationcompat.Handlers.SceneList` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:20](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L20) |
| G347 | `GET /api/v1/compat/scene/detail/:id` | `automationcompat.Handlers.SceneDetail` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:21](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L21) |
| G348 | `POST /api/v1/compat/scene/active/:id` | `automationcompat.Handlers.SceneActive` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:22](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L22) |
| G349 | `GET /api/v1/compat/scene_automations/list` | `automationcompat.Handlers.AutomationList` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:25](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L25) |
| G350 | `GET /api/v1/compat/scene_automations/detail/:id` | `automationcompat.Handlers.AutomationDetail` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:26](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L26) |
| G351 | `POST /api/v1/compat/scene_automations/switch/:id` | `automationcompat.Handlers.AutomationSwitch` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:27](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L27) |
| G352 | `GET /api/v1/compat/scene_automations/log` | `automationcompat.Handlers.AutomationLog` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:28](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L28) |
| G353 | `GET /api/v1/compat/alarm/info` | `automationcompat.Handlers.AlarmInfo` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:32](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L32) |
| G354 | `GET /api/v1/compat/alarm/info/history` | `automationcompat.Handlers.AlarmHistory` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:33](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L33) |
| G355 | `GET /api/v1/compat/alarm/info/history/:id` | `automationcompat.Handlers.AlarmDetail` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:34](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L34) |
| G356 | `GET /api/v1/compat/stats` | `automationcompat.Handlers.Stats` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:41](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L41) |
| G357 | `GET /api/v1/compat/metrics` | `automationcompat.Handlers.Metrics` | 自动化兼容桥 | **A06** | 仅shadow条件的原生适配 | [internal/automationcompat/routes.go:42](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationcompat/routes.go#L42) |

未计入上表的独立演练程序 `cmd/automation-compat` 共13条（healthz＋12条适配），由A06/Q04决定保留演练还是删除；不能与正式shadow挂载重复计算。


## 附录 B：Encore 现有实现与发布范围

以下是183个API声明台账。一个声明可能有多个method或通配路径；源码有声明不等于当前镜像发布该服务。raw为合理协议形式，不表示Gin。

| 服务 | 声明数 | 当前镜像选择 | 后续主任务 |
| --- | ---: | --- | --- |
| authn | 11 | 已选择 | T02/R05/A01/A07 |
| thingmodel | 9 | 已选择 | R04/A03；保持现有core/app |
| automation | 64 | 已选择 | T05/R06/A06 |
| notification | 17 | 已选择 | R07/A07 |
| northbound | 22 | 未选择 | R08/A09/Q02 |
| devicemetadata | 60 | 未选择 | A02/A03/Q04处置旧host，R02/R04保留资源映射 |

镜像选择仅描述固定main中的`encore-image-build.yml`。将来新增platformconfig或其他必要薄host必须同步该清单、resource、migration、ingress、health和CI，不能只在本地启动成功。

| 声明ID | 服务 | Method / Path | 形式 / access | 当前镜像 | 源码函数 |
| --- | --- | --- | --- | --- | --- |
| E001 | `automation` | `POST /v1/rules/:id/bindings` | typed / auth | 选择 | [AttachBinding](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/binding_service.go#L126) |
| E002 | `automation` | `GET /v1/rules/:id/bindings` | typed / auth | 选择 | [ListRuleBindings](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/binding_service.go#L206) |
| E003 | `automation` | `DELETE /v1/bindings/:id` | typed / auth | 选择 | [DetachBinding](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/binding_service.go#L245) |
| E004 | `automation` | `GET /v1/device-automation/:deviceId` | typed / auth | 选择 | [GetDeviceAutomation](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/binding_service.go#L282) |
| E005 | `automation` | `GET /v1/device-configs/:id/bindings` | typed / auth | 选择 | [GetTemplateBindings](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/binding_service.go#L367) |
| E006 | `automation` | `GET /v1/blueprints` | typed / auth | 选择 | [ListBlueprints](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/blueprint_service.go#L26) |
| E007 | `automation` | `POST /v1/blueprint-preview` | typed / auth | 选择 | [PreviewBlueprint](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/blueprint_service.go#L47) |
| E008 | `automation` | `POST /v1/blueprints/:key/instantiate` | typed / auth | 选择 | [InstantiateBlueprint](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/blueprint_service.go#L85) |
| E009 | `automation` | `GET /v1/rules/:id/blueprint` | typed / auth | 选择 | [GetRuleBlueprint](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/blueprint_service.go#L142) |
| E010 | `automation` | `POST /v1/rules/:id/blueprint/upgrade` | typed / auth | 选择 | [UpgradeRuleBlueprint](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/blueprint_service.go#L173) |
| E011 | `automation` | `POST /v1/bundles/export` | typed / auth | 选择 | [ExportBundle](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/bundle_service.go#L127) |
| E012 | `automation` | `POST /v1/bundles/preview` | typed / auth | 选择 | [PreviewBundleImport](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/bundle_service.go#L398) |
| E013 | `automation` | `POST /v1/bundles/import` | typed / auth | 选择 | [ImportBundle](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/bundle_service.go#L439) |
| E014 | `automation` | `GET /v1/bundles/imports` | typed / auth | 选择 | [ListBundleImports](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/bundle_service.go#L655) |
| E015 | `automation` | `GET /v1/bundles/imports/:id` | typed / auth | 选择 | [GetBundleImport](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/bundle_service.go#L683) |
| E016 | `automation` | `POST /v1/rules/:id/change-preview` | typed / auth | 选择 | [PreviewRuleChange](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/change_preview.go#L107) |
| E017 | `automation` | `POST /v1/executors` | typed / auth | 选择 | [RegisterExecutor](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/executor_service.go#L227) |
| E018 | `automation` | `GET /v1/executors` | typed / auth | 选择 | [ListExecutors](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/executor_service.go#L269) |
| E019 | `automation` | `POST /v1/executors/:id/heartbeat` | typed / auth | 选择 | [HeartbeatExecutor](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/executor_service.go#L294) |
| E020 | `automation` | `GET /v1/executors/:id/messages` | typed / auth | 选择 | [ListExecutorMessages](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/executor_service.go#L320) |
| E021 | `automation` | `POST /v1/executors/:id/intents` | typed / auth | 选择 | [SubmitExecutorIntent](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/executor_service.go#L356) |
| E022 | `automation` | `POST /v1/executors/:id/verdicts` | typed / auth | 选择 | [SubmitExecutorVerdict](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/executor_service.go#L454) |
| E023 | `automation` | `POST /v1/migrations` | typed / auth | 选择 | [CreateMigration](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/migration_service.go#L54) |
| E024 | `automation` | `POST /v1/migrations/:id/apply` | typed / auth | 选择 | [ApplyMigration](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/migration_service.go#L114) |
| E025 | `automation` | `GET /v1/migrations/:id` | typed / auth | 选择 | [GetMigration](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/migration_service.go#L145) |
| E026 | `automation` | `POST /v1/migrations/:id/verify` | typed / auth | 选择 | [VerifyMigration](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/migration_service.go#L167) |
| E027 | `automation` | `POST /v1/migrations/:id/rollback` | typed / auth | 选择 | [RollbackMigration](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/migration_service.go#L314) |
| E028 | `automation` | `POST /internal/automation/tick` | raw / public | 选择 | [OwnerTick](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/owner_tick.go#L10) |
| E029 | `automation` | `POST /internal/automation/uplink` | raw / public | 选择 | [OwnerUplink](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/owner_uplink.go#L16) |
| E030 | `automation` | `POST /v1/binding-reconcile` | typed / auth | 选择 | [ReconcileBindingRefs](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/reference_service.go#L83) |
| E031 | `automation` | `DELETE /v1/rules/:id` | typed / auth | 选择 | [DeleteRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/reference_service.go#L265) |
| E032 | `automation` | `POST /v1/rules/:id/revision-check` | typed / auth | 选择 | [CheckRuleRevision](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/reference_service.go#L338) |
| E033 | `automation` | `GET /v1/health` | typed / public | 选择 | [Health](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L110) |
| E034 | `automation` | `POST /v1/rules` | typed / auth | 选择 | [CreateRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L129) |
| E035 | `automation` | `PUT /v1/rules/:id` | typed / auth | 选择 | [UpdateRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L172) |
| E036 | `automation` | `GET /v1/rules/:id` | typed / auth | 选择 | [GetRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L274) |
| E037 | `automation` | `GET /v1/rules` | typed / auth | 选择 | [ListRules](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L294) |
| E038 | `automation` | `GET /v1/legacy-refs/:legacyId` | typed / auth | 选择 | [ResolveLegacyRef](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L347) |
| E039 | `automation` | `POST /v1/rules/:id/enabled` | typed / auth | 选择 | [SetRuleEnabled](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L469) |
| E040 | `automation` | `POST /v1/rule-validation` | typed / auth | 选择 | [ValidateRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L497) |
| E041 | `automation` | `POST /v1/rules/:id/simulate` | typed / auth | 选择 | [SimulateRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L556) |
| E042 | `automation` | `POST /v1/ingress/events` | typed / auth | 选择 | [IngestEvent](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L593) |
| E043 | `automation` | `POST /v1/ingress/uplink` | typed / auth | 选择 | [IngestUplink](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L638) |
| E044 | `automation` | `GET /v1/rules/:id/runs` | typed / auth | 选择 | [ListRuleRuns](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L706) |
| E045 | `automation` | `POST /v1/rules/:id/run` | typed / auth | 选择 | [RunRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L749) |
| E046 | `automation` | `GET /v1/runs/:runId` | typed / auth | 选择 | [GetRun](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L791) |
| E047 | `automation` | `GET /v1/alarms` | typed / auth | 选择 | [ListAlarms](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L847) |
| E048 | `automation` | `POST /v1/alarms/:id/ack` | typed / auth | 选择 | [AckAlarm](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L871) |
| E049 | `automation` | `GET /v1/alarms/:id` | typed / auth | 选择 | [GetAlarm](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L897) |
| E050 | `automation` | `POST /v1/alarm-policies/:id/events` | typed / auth | 选择 | [SubmitAlarmEvent](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L926) |
| E051 | `automation` | `GET /v1/alarm-reconcile` | typed / auth | 选择 | [ReconcileAlarms](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L959) |
| E052 | `automation` | `GET /v1/alarms/:id/transitions` | typed / auth | 选择 | [ListAlarmTransitions](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L996) |
| E053 | `automation` | `POST /v1/legacy/scene-plan` | typed / auth | 选择 | [PlanLegacyScene](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1078) |
| E054 | `automation` | `GET /v1/backlog` | typed / auth | 选择 | [Backlog](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1211) |
| E055 | `automation` | `GET /v1/events` | typed / auth | 选择 | [Events](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1230) |
| E056 | `automation` | `POST /v1/schedules` | typed / auth | 选择 | [CreateSchedule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1321) |
| E057 | `automation` | `PUT /v1/schedules/:id` | typed / auth | 选择 | [UpdateSchedule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1368) |
| E058 | `automation` | `GET /v1/schedules/:id` | typed / auth | 选择 | [GetSchedule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1408) |
| E059 | `automation` | `GET /v1/schedules` | typed / auth | 选择 | [ListSchedules](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1428) |
| E060 | `automation` | `POST /v1/schedules/:id/enabled` | typed / auth | 选择 | [SetScheduleEnabled](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1453) |
| E061 | `automation` | `DELETE /v1/schedules/:id` | typed / auth | 选择 | [DeleteSchedule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1493) |
| E062 | `automation` | `GET /v1/schedules/:id/fires` | typed / auth | 选择 | [ListFires](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1509) |
| E063 | `automation` | `POST /v1/schedule-preview` | typed / auth | 选择 | [PreviewSchedule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1536) |
| E064 | `automation` | `GET /v1/rules/:id/explain` | typed / auth | 选择 | [ExplainRule](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/service.go#L1584) |
| E065 | `devicemetadata` | `GET /api/custom-command-controls` | typed / auth | 未选择 | [ListCustomCommandControls](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/custom_control_service.go#L29) |
| E066 | `devicemetadata` | `POST /api/custom-command-controls` | typed / auth | 未选择 | [CreateCustomCommandControl](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/custom_control_service.go#L74) |
| E067 | `devicemetadata` | `PUT /api/custom-command-controls` | typed / auth | 未选择 | [UpdateCustomCommandControl](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/custom_control_service.go#L113) |
| E068 | `devicemetadata` | `DELETE /api/custom-command-controls/:id` | typed / auth | 未选择 | [DeleteCustomCommandControl](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/custom_control_service.go#L140) |
| E069 | `devicemetadata` | `POST /api/migrate/from-legacy-template/:legacyTemplateID` | typed / auth | 未选择 | [MigrateFromLegacyTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/migration_service.go#L105) |
| E070 | `devicemetadata` | `POST /api/migrate/device-topic/:deviceID` | typed / auth | 未选择 | [MigrateDeviceTopic](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/migration_service.go#L220) |
| E071 | `devicemetadata` | `POST /api/migrate/rollback/:migrationID` | typed / auth | 未选择 | [RollbackMigration](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/migration_service.go#L275) |
| E072 | `devicemetadata` | `GET /api/migrate/history` | typed / auth | 未选择 | [ListMigrationHistory](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/migration_service.go#L344) |
| E073 | `devicemetadata` | `POST /api/migrate/all-legacy-templates` | typed / auth | 未选择 | [MigrateAllLegacyTemplates](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/migration_service.go#L454) |
| E074 | `devicemetadata` | `GET /api/device-metadata/healthz` | typed / public | 未选择 | [Healthz](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L31) |
| E075 | `devicemetadata` | `POST /api/thing-models` | typed / auth | 未选择 | [CreateThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L38) |
| E076 | `devicemetadata` | `GET /api/thing-models` | typed / auth | 未选择 | [ListThingModels](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L47) |
| E077 | `devicemetadata` | `GET /api/thing-models/:id` | typed / auth | 未选择 | [GetThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L60) |
| E078 | `devicemetadata` | `PUT /api/thing-models/:id` | typed / auth | 未选择 | [UpdateThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L69) |
| E079 | `devicemetadata` | `DELETE /api/thing-models/:id` | typed / auth | 未选择 | [DeleteThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L78) |
| E080 | `devicemetadata` | `POST /api/thing-models/:id/publish` | typed / auth | 未选择 | [PublishThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L87) |
| E081 | `devicemetadata` | `POST /api/thing-models/:id/derive-draft` | typed / auth | 未选择 | [DeriveThingModelDraft](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L96) |
| E082 | `devicemetadata` | `POST /api/thing-models/:id/rollback` | typed / auth | 未选择 | [RollbackThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L105) |
| E083 | `devicemetadata` | `POST /api/thing-models/:id/archive` | typed / auth | 未选择 | [ArchiveThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L114) |
| E084 | `devicemetadata` | `GET /api/thing-models/:id/versions` | typed / auth | 未选择 | [ListVersions](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L127) |
| E085 | `devicemetadata` | `GET /api/thing-models/:id/versions/:version` | typed / auth | 未选择 | [GetVersion](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L140) |
| E086 | `devicemetadata` | `GET /api/thing-models/:id/diff` | typed / auth | 未选择 | [DiffVersions](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L154) |
| E087 | `devicemetadata` | `POST /api/thing-models/:tmId/items` | typed / auth | 未选择 | [CreateItem](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L169) |
| E088 | `devicemetadata` | `POST /api/thing-models/:tmId/items/batch` | typed / auth | 未选择 | [BatchCreateItems](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L186) |
| E089 | `devicemetadata` | `GET /api/thing-models/:tmId/items` | typed / auth | 未选择 | [ListItems](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L203) |
| E090 | `devicemetadata` | `GET /api/thing-models/:tmId/items/:id` | typed / auth | 未选择 | [GetItem](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L216) |
| E091 | `devicemetadata` | `PUT /api/thing-models/:tmId/items/:id` | typed / auth | 未选择 | [UpdateItem](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L225) |
| E092 | `devicemetadata` | `DELETE /api/thing-models/:tmId/items/:id` | typed / auth | 未选择 | [DeleteItem](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L234) |
| E093 | `devicemetadata` | `POST /api/device-templates` | typed / auth | 未选择 | [CreateTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L243) |
| E094 | `devicemetadata` | `GET /api/device-templates` | typed / auth | 未选择 | [ListTemplates](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L252) |
| E095 | `devicemetadata` | `GET /api/device-templates/:id` | typed / auth | 未选择 | [GetTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L265) |
| E096 | `devicemetadata` | `PUT /api/device-templates/:id` | typed / auth | 未选择 | [UpdateTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L274) |
| E097 | `devicemetadata` | `DELETE /api/device-templates/:id` | typed / auth | 未选择 | [DeleteTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L283) |
| E098 | `devicemetadata` | `POST /api/device-templates/:id/publish` | typed / auth | 未选择 | [PublishTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L292) |
| E099 | `devicemetadata` | `POST /api/device-templates/:id/archive` | typed / auth | 未选择 | [ArchiveTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L301) |
| E100 | `devicemetadata` | `POST /api/device-templates/:id/derive-draft` | typed / auth | 未选择 | [DeriveTemplateDraft](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L310) |
| E101 | `devicemetadata` | `POST /api/device-templates/:id/bind-thing-model` | typed / auth | 未选择 | [BindThingModelVersion](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L319) |
| E102 | `devicemetadata` | `GET /api/thing-models/:thingModelID/device-templates` | typed / auth | 未选择 | [ListTemplatesByThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L332) |
| E103 | `devicemetadata` | `POST /api/products` | typed / auth | 未选择 | [CreateProduct](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L345) |
| E104 | `devicemetadata` | `GET /api/products` | typed / auth | 未选择 | [ListProducts](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L354) |
| E105 | `devicemetadata` | `GET /api/products/:id` | typed / auth | 未选择 | [GetProduct](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L367) |
| E106 | `devicemetadata` | `PUT /api/products/:id` | typed / auth | 未选择 | [UpdateProduct](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L376) |
| E107 | `devicemetadata` | `DELETE /api/products/:id` | typed / auth | 未选择 | [DeleteProduct](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L385) |
| E108 | `devicemetadata` | `GET /api/product-keys/:productKey` | typed / auth | 未选择 | [GetProductByKey](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L394) |
| E109 | `devicemetadata` | `GET /api/templates/:templateID/products` | typed / auth | 未选择 | [ListProductsByTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L407) |
| E110 | `devicemetadata` | `POST /api/devices` | typed / auth | 未选择 | [CreateDevice](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L420) |
| E111 | `devicemetadata` | `GET /api/devices` | typed / auth | 未选择 | [ListDevices](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L429) |
| E112 | `devicemetadata` | `GET /api/devices/:id` | typed / auth | 未选择 | [GetDevice](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L442) |
| E113 | `devicemetadata` | `PUT /api/devices/:id` | typed / auth | 未选择 | [UpdateDevice](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L451) |
| E114 | `devicemetadata` | `DELETE /api/devices/:id` | typed / auth | 未选择 | [DeleteDevice](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L460) |
| E115 | `devicemetadata` | `POST /api/devices/:id/actuate` | typed / auth | 未选择 | [Actuate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L469) |
| E116 | `devicemetadata` | `GET /api/devices/:id/latest` | typed / auth | 未选择 | [GetDeviceLatest](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L525) |
| E117 | `devicemetadata` | `GET /api/meta-item-keys` | typed / auth | 未选择 | [ListMetaItemKeys](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L550) |
| E118 | `devicemetadata` | `POST /api/validate/thing-model-item` | typed / auth | 未选择 | [ValidateItem](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L563) |
| E119 | `devicemetadata` | `POST /api/validate/thing-model` | typed / auth | 未选择 | [ValidateThingModel](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L593) |
| E120 | `devicemetadata` | `POST /api/validate/device-template` | typed / auth | 未选择 | [ValidateTemplate](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L623) |
| E121 | `devicemetadata` | `DELETE /api/thing-model-items/:id` | typed / auth | 未选择 | [DeleteThingModelItemByID](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L872) |
| E122 | `devicemetadata` | `PUT /api/thing-model-items/:id` | typed / auth | 未选择 | [UpdateThingModelItemByID](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L883) |
| E123 | `devicemetadata` | `GET /api/public/meta-item-keys` | typed / public | 未选择 | [GetPublicMetaItemKeys](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L894) |
| E124 | `devicemetadata` | `GET /api/dictionaries` | typed / public | 未选择 | [GetDictionaries](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go#L902) |
| E125 | `l1-access/authn` | `<raw default/all; inspect HTTP dispatch> /api/v1/auth/*path`；通配模板 | raw / public | 选择 | [Authentication](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L33) |
| E126 | `l1-access/authn` | `POST /api/v1/login` | raw / public | 选择 | [PasswordLogin](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L36) |
| E127 | `l1-access/authn` | `GET /api/v1/user/detail` | raw / public | 选择 | [UserDetail](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L39) |
| E128 | `l1-access/authn` | `GET /api/v1/user/logout` | raw / public | 选择 | [LegacyLogout](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L42) |
| E129 | `l1-access/authn` | `GET /api/v1/user/refresh` | raw / public | 选择 | [LegacyRefresh](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L45) |
| E130 | `l1-access/authn` | `GET /api/v1/tenant/setup-state` | raw / public | 选择 | [SetupState](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L48) |
| E131 | `l1-access/authn` | `GET /api/v1/tenant/has-admin` | raw / public | 选择 | [HasAdmin](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L51) |
| E132 | `l1-access/authn` | `POST /api/v1/tenant/super-admin/init` | raw / public | 选择 | [InitializeAdmin](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L54) |
| E133 | `l1-access/authn` | `POST /api/v1/tenant/market-register` | raw / public | 选择 | [LegacyInitializeAdmin](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L57) |
| E134 | `l1-access/authn` | `POST /api/v1/tenant/email/register` | raw / public | 选择 | [EmailSignup](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L60) |
| E135 | `l1-access/authn` | `POST /api/v1/user/transform` | raw / public | 选择 | [DelegateUser](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go#L63) |
| E136 | `l3-core/notification` | `GET,POST /api/v2/notification-plugins` | raw / public | 选择 | [PluginsV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L8) |
| E137 | `l3-core/notification` | `GET,PUT /api/v2/notification-plugins/:id` | raw / public | 选择 | [PluginV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L11) |
| E138 | `l3-core/notification` | `GET,POST /api/v2/notification-instances` | raw / public | 选择 | [InstancesV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L14) |
| E139 | `l3-core/notification` | `GET,POST,PUT /api/v2/notification-instances/*path`；通配模板 | raw / public | 选择 | [InstanceRoutesV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L21) |
| E140 | `l3-core/notification` | `GET,POST,PUT /api/v2/platform/notification-tenants/*path`；通配模板 | raw / public | 选择 | [PlatformTenantInstanceRoutesV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L26) |
| E141 | `l3-core/notification` | `GET,POST /api/v2/notifications` | raw / public | 选择 | [SubmitV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L31) |
| E142 | `l3-core/notification` | `GET /api/v2/notifications/:id` | raw / public | 选择 | [NotificationV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L34) |
| E143 | `l3-core/notification` | `GET /api/v2/notification-deliveries` | raw / public | 选择 | [DeliveriesV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L37) |
| E144 | `l3-core/notification` | `GET /api/v2/notification-deliveries/:id` | raw / public | 选择 | [DeliveryV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L40) |
| E145 | `l3-core/notification` | `POST /api/v2/notification-ingress/events` | raw / public | 选择 | [SourceEventV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L43) |
| E146 | `l3-core/notification` | `POST /api/v1/notification-instances/:instance_id/receipts` | raw / public | 选择 | [ReceiptV1](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L46) |
| E147 | `l3-core/notification` | `GET,POST /api/v2/notification-groups` | raw / public | 选择 | [GroupsV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L49) |
| E148 | `l3-core/notification` | `GET,PUT /api/v2/notification-groups/:id` | raw / public | 选择 | [GroupV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L52) |
| E149 | `l3-core/notification` | `POST /api/v2/notification-group-projections` | raw / public | 选择 | [GroupProjectionsV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L55) |
| E150 | `l3-core/notification` | `GET,PUT /api/v2/notification-plugin-grants` | raw / public | 选择 | [PluginGrantsV2](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L58) |
| E151 | `l3-core/notification` | `GET /api/v1/notification-source/group-snapshots/:id` | raw / public | 选择 | [SourceGroupSnapshotV1](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/api_v2.go#L63) |
| E152 | `l3-core/notification` | `GET /v1/notifications/history` | raw / public | 选择 | [History](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/service.go#L49) |
| E153 | `northbound` | `GET /v1/connectors` | typed / auth | 未选择 | [ListConnectors](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/catalog.go#L149) |
| E154 | `northbound` | `GET /v1/connections` | typed / auth | 未选择 | [ListConnections](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/connections.go#L18) |
| E155 | `northbound` | `GET /v1/connections/:id` | typed / auth | 未选择 | [GetConnection](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/connections.go#L68) |
| E156 | `northbound` | `POST /v1/connections` | typed / auth | 未选择 | [CreateConnection](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/connections.go#L101) |
| E157 | `northbound` | `PUT /v1/connections/:id` | typed / auth | 未选择 | [UpdateConnection](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/connections.go#L142) |
| E158 | `northbound` | `DELETE /v1/connections/:id` | typed / auth | 未选择 | [DeleteConnection](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/connections.go#L205) |
| E159 | `northbound` | `POST /v1/connections/:id/test` | typed / auth | 未选择 | [TestConnection](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/connections.go#L248) |
| E160 | `northbound` | `GET /v1/deliveries` | typed / auth | 未选择 | [ListDeliveries](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/deliveries.go#L15) |
| E161 | `northbound` | `POST /v1/deliveries/:id/replay` | typed / auth | 未选择 | [ReplayDelivery](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/deliveries.go#L93) |
| E162 | `northbound` | `POST /v1/inbox/events` | typed / public | 未选择 | [IngestEvents](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/inbox.go#L17) |
| E163 | `northbound` | `GET /v1/overview` | typed / auth | 未选择 | [Overview](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/overview.go#L14) |
| E164 | `northbound` | `GET /v1/pipelines` | typed / auth | 未选择 | [ListPipelines](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L17) |
| E165 | `northbound` | `GET /v1/pipelines/:id` | typed / auth | 未选择 | [GetPipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L68) |
| E166 | `northbound` | `POST /v1/pipelines` | typed / auth | 未选择 | [CreatePipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L138) |
| E167 | `northbound` | `PUT /v1/pipelines/:id` | typed / auth | 未选择 | [UpdatePipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L181) |
| E168 | `northbound` | `POST /v1/pipelines/:id/preview` | typed / auth | 未选择 | [PreviewPipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L251) |
| E169 | `northbound` | `POST /v1/pipelines/:id/activate` | typed / auth | 未选择 | [ActivatePipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L298) |
| E170 | `northbound` | `POST /v1/pipelines/:id/pause` | typed / auth | 未选择 | [PausePipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L305) |
| E171 | `northbound` | `POST /v1/pipelines/:id/disable` | typed / auth | 未选择 | [DisablePipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L313) |
| E172 | `northbound` | `DELETE /v1/pipelines/:id` | typed / auth | 未选择 | [DeletePipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L323) |
| E173 | `northbound` | `POST /v1/pipelines/:id/test-send` | typed / auth | 未选择 | [TestSendPipeline](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/pipelines.go#L400) |
| E174 | `northbound` | `GET /v1/subscriptions` | typed / public | 未选择 | [Subscriptions](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/subscriptions.go#L30) |
| E175 | `thingmodel` | `POST /api/v2/thing-model-commands/create` | typed / auth | 选择 | [Create](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L44) |
| E176 | `thingmodel` | `POST /api/v2/thing-model-commands/update` | typed / auth | 选择 | [Update](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L61) |
| E177 | `thingmodel` | `POST /api/v2/thing-model-commands/publish` | typed / auth | 选择 | [Publish](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L81) |
| E178 | `thingmodel` | `GET /api/v2/thing-models` | typed / auth | 选择 | [List](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L121) |
| E179 | `thingmodel` | `GET /api/v2/thing-models/:modelID` | typed / auth | 选择 | [Get](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L142) |
| E180 | `thingmodel` | `GET /api/v2/thing-models/:modelID/versions` | typed / auth | 选择 | [Versions](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L166) |
| E181 | `thingmodel` | `GET /api/v2/thing-models/:modelID/versions/:publishedRevision` | typed / auth | 选择 | [Version](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L202) |
| E182 | `thingmodel` | `GET /api/v2/thing-models/:modelID/resources` | typed / auth | 选择 | [Resources](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L215) |
| E183 | `thingmodel` | `POST /api/v2/thing-model-commands/update-resources` | typed / auth | 选择 | [UpdateResources](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/service.go#L228) |

## 附录 C：证据索引与官方约束

源码链接均固定到本次审计提交；以后main变化时应重新核对。官方文档用于解释框架边界，不替代项目固定版本的真实编译/运行验证。

| 编号 | 已读取的源码证据 |
| --- | --- |
| C01 | [Gin主路由与注册图](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go)；[HTTP启动](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/app/http_service.go) |
| C02 | [Community Go module](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/go.mod)；[Encore Go module](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/go.mod) |
| C03 | [正式镜像服务选择与固定CLI](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/.github/workflows/encore-image-build.yml)；[测试Encore部署workflow](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/.github/workflows/deploy-encore.yml) |
| C04 | [物模型用例](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/app/application.go)；[Repository/Transaction](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/app/types.go)；[PG adapter](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/postgres.go) |
| C05 | [物模型引用逻辑资源](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/db.go)；[资源唯一声明和迁移目录](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/db.go) |
| C06 | [原生身份资源初始化](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/internal/authcore/runtime.go)；[原生认证HTTP入口](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/endpoints.go)；[auth handler](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l1-access/authn/auth.go) |
| C07 | [自动化动作端口与未接通的ActivateScene](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/ports/ports.go)；[原生tick/recovery](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/owner_tick.go)；[自动化store](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/store/store.go) |
| C08 | [通知Core/Worker装配](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/service_runtime.go)；[通知Store接口](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/store_interfaces.go)；[旧通知组退役](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/notification_group.go) |
| C09 | [北向服务装配](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/service.go)；[北向inbox](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/inbox.go)；[北向worker](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/worker.go) |
| C10 | [原生市场导入和绑定](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/market_native_install.go) |
| C11 | [旧metadata host](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go)；[旧metadata MQTT bridge](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/mqtt.go) |
| C12 | [应用Shutdown](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/app/application.go)；[ServiceManager](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/app/service.go)；[Cron Stop](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/app/cron_service.go)；[gRPC Stop](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/app/grpc_service.go) |
| C13 | [设备删除服务](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/device.go)；[设备归属与删除DAL](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/dal/devices.go)；[设备API](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/device.go) |
| C14 | [批量转换/fallback/最新值](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/storage/telemetry_writer.go) |
| C15 | [旧native内存转发](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/uplink/native_automation.go)；[旧cron注册](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/initialize/croninit/cron.go)；[严格bridge](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/automationbridge/bridge.go)；[持久effect消费](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/native_automation_effect.go) |
| C16 | [PG连接及版本检查](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/initialize/pg_init.go)；[Broker持久接入](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/brokerledger/store.go)；[Broker控制仓储](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/brokercontrol/gorm_repository.go)；[通知来源outbox](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/dal/notification_source_outbox.go)；[初始SQL](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/sql/1.sql) |
| C17 | [历史查询/导出/清理](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/dal/telemetry_datas.go)；[当前值读取](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/dal/telemetry_current_datas.go)；[Storage生命周期接口](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/storage/storage.go)；[tptodb客户端](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/third_party/grpc/tptodb_client/init.go) |
| C18 | [全局依赖](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/pkg/global/global.go)；[Gin SSE writer](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/pkg/global/SSEManager.go)；[旧认证writer guard](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/middleware/auth_owner.go)；[JWT/APIKey](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/middleware/jwt_auth.go) |
| C19 | [可信遥测事务及fence](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/storage/telemetry_broker.go)；[现有effect schema](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/sql/broker/005_ingress_effects.sql) |
| C20 | [旧模型真实DAL调用](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/device_model.go)；[旧模板服务](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/device_template.go)；[native消费者与绑定CAS](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/device_config.go) |
| C21 | [协议插件配置与日志](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/protocol_plugin.go) |
| C22 | [测试发布与PG迁移执行](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/deploy/community-test/deploy_encore.py)；[迁移checksum辅助](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/deploy/community-test/encore_migrations.py)；[正式发布owner说明](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/.ai/00-项目核心/正式版本发布.md) |
| C23 | [批量状态WS鉴权后订阅](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/device_status_ws.go)；[遥测WS认证与订阅](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/telemetry_data.go) |
| O01 | [Encore SQL原生PostgreSQL](https://encore.dev/docs/go/primitives/databases) |
| O02 | [连接外部数据库](https://encore.dev/docs/go/primitives/connect-existing-db) |
| O03 | [Raw Endpoints](https://encore.dev/docs/go/primitives/raw-endpoints) |
| O04 | [自托管镜像：migration与cron由部署方执行](https://encore.dev/docs/self-host/build) |
| O05 | [Encore测试运行方式](https://encore.dev/docs/go/develop/testing) |

使用边界：本次没有连接真实业务库/设备/供应商或操作部署。源码中的缺口与静态风险不能自动表述成线上事故；任务书中的建议不能自动表述成已经获验收的系统能力。

[C01]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/router/router_init.go
[C02]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/go.mod
[C03]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/.github/workflows/encore-image-build.yml
[C04]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/app/application.go
[C05]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/thingmodel/db.go
[C06]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/internal/authcore/runtime.go
[C07]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/automation/ports/ports.go
[C08]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/l3-core/notification/service_runtime.go
[C09]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/northbound/service.go
[C10]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/market_native_install.go
[C11]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/encore-api/devicemetadata/service.go
[C12]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/app/application.go
[C13]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/device.go
[C14]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/storage/telemetry_writer.go
[C15]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/uplink/native_automation.go
[C16]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/initialize/pg_init.go
[C17]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/dal/telemetry_datas.go
[C18]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/pkg/global/global.go
[C19]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/storage/telemetry_broker.go
[C20]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/device_model.go
[C21]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/protocol_plugin.go
[C22]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/deploy/community-test/deploy_encore.py
[C23]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/api/device_status_ws.go
[O01]: https://encore.dev/docs/go/primitives/databases
[O02]: https://encore.dev/docs/go/primitives/connect-existing-db
[O03]: https://encore.dev/docs/go/primitives/raw-endpoints
[O04]: https://encore.dev/docs/self-host/build
[O05]: https://encore.dev/docs/go/develop/testing

## 附录 D：可以直接指定编号的 API 小卡

共 **100 张**小卡，覆盖附录A全部357行，每行恰好出现一次。每张最多4个入口；BrokerAI六条为一组，避免拆出重复授权实现。它们是35个任务包内的实施切片，不是新增业务模块，也不是可以各自独立上线的批准。

发给执行者的提示语可以直接使用下面这一段，只替换小卡编号；其准确范围已填在后表，不需要执行者自行挑接口。

```text
执行本任务书中的【小卡编号】，严格只处理该行列出的精确路由与必要调用链。
先读第5节总提示语和对应父任务卡；确认T00/T02.a/T01/T02.b及父任务前置已经完成。
以本行列出的源码入口为起点，复用父任务指定的唯一owner；只移动所需业务实现和协议适配，不变更驱动或复制业务层。
旧入口如果已410/503或仅shadow，按父任务做退役/切换验证，不能恢复旧写能力。
输出逐端点旧/新契约、权限与失败用例，提供实际验证结果和旧写者/消费者清单。
该小卡通过仅表示实施范围通过；完整表族与所有写者未收口前，不宣布单一owner切流完成，也不自行发布。
```

| 小卡编号 | 父任务/族 | 精确范围（路由ID与method/path） | 允许起点文件 |
| --- | --- | --- | --- |
| **A02-E01** | A02 / automation_owner | G001 `GET /internal/automation/devices` | `internal/api/automation_owner.go` |
| **A05-E01** | A05 / automation_owner | G002 `POST /internal/automation/api/v1/:kind/datas/pub` | `internal/api/automation_owner.go` |
| **A05-E02** | A05 / broker_ai | G003 `POST /api/v1/broker-ai/delegations`；G004 `POST /api/v1/broker-ai/delegations/:id/revoke`；G005 `POST /api/v1/broker-ai/tools/devices.describe`；G006 `POST /api/v1/broker-ai/tools/telemetry.query`；G007 `POST /api/v1/broker-ai/tools/commands.preview`；G008 `POST /api/v1/broker-ai/tools/commands.getResult` | `internal/api/broker_ai_tools.go` |
| **A06-E01** | A06 / automation_legacy | G009 `GET /api/v1/alarm/device/counts`；G010 `POST /api/v1/alarm/config`；G011 `DELETE /api/v1/alarm/config/:id`；G012 `PUT /api/v1/alarm/config` | `router/apps/alarm.go` |
| **A06-E02** | A06 / automation_legacy | G013 `GET /api/v1/alarm/config`；G014 `PUT /api/v1/alarm/info`；G015 `PUT /api/v1/alarm/info/batch`；G016 `GET /api/v1/alarm/info` | `router/apps/alarm.go` |
| **A06-E03** | A06 / automation_legacy | G017 `GET /api/v1/alarm/info/history`；G018 `PUT /api/v1/alarm/info/history`；G019 `GET /api/v1/alarm/info/history/device`；G020 `GET /api/v1/alarm/info/config/device` | `router/apps/alarm.go` |
| **A06-E04** | A06 / automation_legacy | G021 `GET /api/v1/alarm/info/history/:id`；G022 `DELETE /api/v1/alarm/info/history/:id`；G227 `POST /api/v1/scene`；G228 `DELETE /api/v1/scene/:id` | `router/apps/alarm.go`、`router/apps/scene.go` |
| **A06-E05** | A06 / automation_legacy | G229 `GET /api/v1/scene`；G230 `GET /api/v1/scene/detail/:id`；G231 `PUT /api/v1/scene`；G232 `POST /api/v1/scene/active/:id` | `router/apps/scene.go` |
| **A06-E06** | A06 / automation_legacy | G233 `GET /api/v1/scene/log`；G234 `POST /api/v1/scene_automations`；G235 `DELETE /api/v1/scene_automations/:id`；G236 `PUT /api/v1/scene_automations` | `router/apps/scene.go`、`router/apps/scene_automations.go` |
| **A06-E07** | A06 / automation_legacy | G237 `POST /api/v1/scene_automations/switch/:id`；G238 `GET /api/v1/scene_automations/list`；G239 `GET /api/v1/scene_automations/detail/:id`；G240 `GET /api/v1/scene_automations/log` | `router/apps/scene_automations.go` |
| **A06-E08** | A06 / automation_legacy | G241 `GET /api/v1/scene_automations/alarm`；G344 `GET /api/v1/automation/runtime` | `router/apps/scene_automations.go`、`router/router_init.go` |
| **A04-E01** | A04 / attribute | G023 `GET /api/v1/attribute/datas/:id`；G024 `GET /api/v1/attribute/datas/set/logs`；G025 `DELETE /api/v1/attribute/datas/:id`；G028 `GET /api/v1/attribute/datas/key` | `router/apps/attribute_data.go` |
| **A05-E03** | A05 / command_dispatch | G026 `POST /api/v1/attribute/datas/pub`；G027 `GET /api/v1/attribute/datas/get`；G052 `POST /api/v1/command/datas/plans/preview`；G053 `POST /api/v1/command/datas/plans/:plan_id/authorize` | `router/apps/attribute_data.go`、`router/apps/command_data.go` |
| **A05-E04** | A05 / command_dispatch | G054 `POST /api/v1/command/datas/plans/:plan_id/submit`；G055 `GET /api/v1/command/datas/set/logs`；G056 `POST /api/v1/command/datas/pub`；G057 `GET /api/v1/command/datas/status/:message_id` | `router/apps/command_data.go` |
| **A05-E05** | A05 / command_dispatch | G058 `GET /api/v1/command/datas/:id`；G296 `POST /api/v1/telemetry/datas/pub` | `router/apps/command_data.go`、`router/apps/telemetry_data.go` |
| **A11-E01** | A11 / dashboard | G029 `POST /api/v1/board`；G030 `DELETE /api/v1/board/:id`；G031 `PUT /api/v1/board`；G032 `GET /api/v1/board` | `router/apps/board.go` |
| **A11-E02** | A11 / dashboard | G033 `GET /api/v1/board/:id`；G034 `GET /api/v1/board/home`；G035 `GET /api/v1/board/trend`；G036 `GET /api/v1/board/device/total` | `router/apps/board.go` |
| **A11-E03** | A11 / dashboard | G037 `GET /api/v1/board/device`；G038 `GET /api/v1/board/tenant`；G039 `GET /api/v1/board/tenant/user/info`；G040 `GET /api/v1/board/tenant/device/info` | `router/apps/board.go` |
| **A01-E01** | A01 / account_profile | G041 `GET /api/v1/board/user/info`；G042 `POST /api/v1/board/user/update`；G043 `POST /api/v1/board/user/update/password` | `router/apps/board.go` |
| **A01-E02** | A01 / casbin | G044 `POST /api/v1/casbin/function`；G045 `DELETE /api/v1/casbin/function/:id`；G046 `PUT /api/v1/casbin/function`；G047 `GET /api/v1/casbin/function` | `router/apps/casbin.go` |
| **A01-E03** | A01 / casbin | G048 `POST /api/v1/casbin/user`；G049 `DELETE /api/v1/casbin/user/:id`；G050 `PUT /api/v1/casbin/user`；G051 `GET /api/v1/casbin/user` | `router/apps/casbin.go` |
| **A01-E04** | A01 / menu | G059 `GET /api/v1/dashboard-menu/:dashboardId`；G060 `PUT /api/v1/dashboard-menu/:dashboardId`；G061 `DELETE /api/v1/dashboard-menu/:dashboardId`；G264 `POST /api/v1/ui_elements` | `router/apps/dashboard_menu.go`、`router/apps/sys_ui_elements.go` |
| **A01-E05** | A01 / menu | G265 `DELETE /api/v1/ui_elements/:id`；G266 `PUT /api/v1/ui_elements`；G267 `GET /api/v1/ui_elements`；G268 `GET /api/v1/ui_elements/menu` | `router/apps/sys_ui_elements.go` |
| **A01-E06** | A01 / menu | G269 `GET /api/v1/ui_elements/select/form` | `router/apps/sys_ui_elements.go` |
| **A10-E01** | A10 / script | G062 `POST /api/v1/data_script`；G063 `DELETE /api/v1/data_script/:id`；G064 `PUT /api/v1/data_script`；G065 `GET /api/v1/data_script` | `router/apps/data_script.go` |
| **A10-E02** | A10 / script | G066 `POST /api/v1/data_script/quiz`；G067 `PUT /api/v1/data_script/enable` | `router/apps/data_script.go` |
| **S04-E01** | S04 / data_policy | G068 `PUT /api/v1/datapolicy`；G069 `GET /api/v1/datapolicy` | `router/apps/datapolicy.go` |
| **A02-E02** | A02 / device | G070 `POST /api/v1/device`；G071 `DELETE /api/v1/device/:id`；G072 `PUT /api/v1/device`；G073 `PUT /api/v1/device/active` | `router/apps/device.go` |
| **A02-E03** | A02 / device | G074 `GET /api/v1/device/detail/:id`；G075 `GET /api/v1/device/telemetry/evidence/:id`；G076 `GET /api/v1/device`；G077 `GET /api/v1/device/check/:deviceNumber` | `router/apps/device.go` |
| **A02-E04** | A02 / device | G078 `GET /api/v1/device/tenant/list`；G079 `GET /api/v1/device/list`；G080 `POST /api/v1/device/son/add`；G081 `GET /api/v1/device/connect/form` | `router/apps/device.go` |
| **A02-E05** | A02 / device | G082 `GET /api/v1/device/connect/info`；G083 `POST /api/v1/device/update/voucher`；G084 `GET /api/v1/device/sub-list/:id`；G085 `PUT /api/v1/device/sub-remove` | `router/apps/device.go` |
| **A02-E06** | A02 / device | G086 `GET /api/v1/device/metrics/:id`；G087 `GET /api/v1/device/metrics/menu`；G088 `GET /api/v1/device/metrics/condition/menu`；G089 `GET /api/v1/device/map/telemetry/:id` | `router/apps/device.go` |
| **A02-E07** | A02 / device | G090 `PUT /api/v1/device/update/config`；G091 `GET /api/v1/device/online/status/:id`；G092 `GET /api/v1/device/status/evidence/:id`；G093 `GET /api/v1/device/semantic/evidence/:id` | `router/apps/device.go` |
| **A02-E08** | A02 / device | G094 `POST /api/v1/device/:device_id/debug`；G095 `GET /api/v1/device/:device_id/debug/status`；G096 `GET /api/v1/device/:device_id/debug/logs`；G097 `POST /api/v1/device/service/access/batch` | `router/apps/device.go` |
| **A02-E09** | A02 / device | G098 `GET /api/v1/device/metrics/chart`；G099 `GET /api/v1/device/selector`；G100 `GET /api/v1/device/telemetry/latest`；G101 `GET /api/v1/device/status/history` | `router/apps/device.go` |
| **A02-E10** | A02 / device | G102 `POST /api/v1/device/topic-mappings`；G103 `GET /api/v1/device/topic-mappings`；G104 `PUT /api/v1/device/topic-mappings/:id`；G105 `DELETE /api/v1/device/topic-mappings/:id` | `router/apps/device.go` |
| **A02-E11** | A02 / device | G339 `POST /api/v1/device/gateway-register`；G340 `POST /api/v1/device/gateway-sub-register`；G342 `POST /api/v1/device/auth`；G343 `GET /api/v1/devices/:device_id/diagnostics` | `router/router_init.go` |
| **A03-E01** | A03 / device_model | G106 `POST /api/v1/device/template`；G107 `DELETE /api/v1/device/template/:id`；G108 `PUT /api/v1/device/template`；G109 `GET /api/v1/device/template/detail/:id` | `router/apps/device.go` |
| **A03-E02** | A03 / device_model | G110 `GET /api/v1/device/template`；G111 `GET /api/v1/device/template/menu`；G112 `GET /api/v1/device/template/stats`；G113 `GET /api/v1/device/template/selector` | `router/apps/device.go` |
| **A03-E03** | A03 / device_model | G114 `GET /api/v1/device/template/chart`；G115 `GET /api/v1/device/template/chart/select`；G149 `GET /api/v1/device/model/source/at/list`；G150 `POST /api/v1/device/model/telemetry` | `router/apps/device.go` |
| **A03-E04** | A03 / device_model | G151 `DELETE /api/v1/device/model/telemetry/:id`；G152 `PUT /api/v1/device/model/telemetry`；G153 `GET /api/v1/device/model/telemetry`；G154 `POST /api/v1/device/model/attributes` | `router/apps/device.go` |
| **A03-E05** | A03 / device_model | G155 `DELETE /api/v1/device/model/attributes/:id`；G156 `PUT /api/v1/device/model/attributes`；G157 `GET /api/v1/device/model/attributes`；G158 `POST /api/v1/device/model/events` | `router/apps/device.go` |
| **A03-E06** | A03 / device_model | G159 `DELETE /api/v1/device/model/events/:id`；G160 `PUT /api/v1/device/model/events`；G161 `GET /api/v1/device/model/events`；G162 `POST /api/v1/device/model/commands` | `router/apps/device.go` |
| **A03-E07** | A03 / device_model | G163 `DELETE /api/v1/device/model/commands/:id`；G164 `PUT /api/v1/device/model/commands`；G165 `GET /api/v1/device/model/commands`；G166 `POST /api/v1/device/model/custom/commands` | `router/apps/device.go` |
| **A03-E08** | A03 / device_model | G167 `DELETE /api/v1/device/model/custom/commands/:id`；G168 `PUT /api/v1/device/model/custom/commands`；G169 `GET /api/v1/device/model/custom/commands`；G170 `GET /api/v1/device/model/custom/commands/:deviceId` | `router/apps/device.go` |
| **A03-E09** | A03 / device_model | G171 `POST /api/v1/device/model/custom/control`；G172 `DELETE /api/v1/device/model/custom/control/:id`；G173 `PUT /api/v1/device/model/custom/control`；G174 `GET /api/v1/device/model/custom/control` | `router/apps/device.go` |
| **A08-E01** | A08 / market | G116 `POST /api/v1/device/template/market/login`；G117 `POST /api/v1/device/template/market/refresh`；G118 `POST /api/v1/device/template/market/publish`；G119 `GET /api/v1/device/template/market/list` | `router/apps/device.go` |
| **A08-E02** | A08 / market | G120 `GET /api/v1/device/template/market/detail/:market_id`；G121 `POST /api/v1/device/template/market/install`；G122 `POST /api/v1/device/market/dashboard-bundles/analyze`；G123 `POST /api/v1/device/market/dashboard-bundles` | `router/apps/device.go` |
| **A08-E03** | A08 / market | G124 `POST /api/v1/device/market/bundles/publish-draft`；G125 `GET /api/v1/device/market/bundles`；G126 `GET /api/v1/device/market/bundles/:bundleKey`；G127 `GET /api/v1/device/market/bundles/:bundleKey/precheck` | `router/apps/device.go` |
| **A08-E04** | A08 / market | G128 `POST /api/v1/device/market/bundles/download`；G129 `POST /api/v1/device/market/bundles/install`；G130 `GET /api/v1/device/market/bundles/install/:id`；G131 `PUT /api/v1/device/market/bundles/install/:id/bindings` | `router/apps/device.go` |
| **A08-E05** | A08 / market | G132 `POST /api/v1/device/market/bundles/install/:id/retry`；G133 `POST /api/v1/device/market/bundles/install/:id/compensate`；G134 `GET /api/v1/device/market/bundles/installations` | `router/apps/device.go` |
| **A08-E06** | A08 / dashboard_template | G135 `GET /api/v1/device/dashboard-templates`；G136 `GET /api/v1/device/dashboard-templates/:id/compatible-devices`；G137 `POST /api/v1/device/dashboard-templates/:id/instances` | `router/apps/device.go` |
| **A02-E12** | A02 / device_group | G138 `POST /api/v1/device/group`；G139 `DELETE /api/v1/device/group/:id`；G140 `PUT /api/v1/device/group`；G141 `GET /api/v1/device/group` | `router/apps/device.go` |
| **A02-E13** | A02 / device_group | G142 `GET /api/v1/device/group/tree`；G143 `GET /api/v1/device/group/counts`；G144 `GET /api/v1/device/group/detail/:id`；G145 `POST /api/v1/device/group/relation` | `router/apps/device.go` |
| **A02-E14** | A02 / device_group | G146 `DELETE /api/v1/device/group/relation`；G147 `GET /api/v1/device/group/relation/list`；G148 `GET /api/v1/device/group/relation` | `router/apps/device.go` |
| **A02-E15** | A02 / device_config | G175 `POST /api/v1/device_config`；G176 `DELETE /api/v1/device_config/:id`；G177 `PUT /api/v1/device_config`；G178 `GET /api/v1/device_config` | `router/apps/device_config.go` |
| **A02-E16** | A02 / device_config | G179 `GET /api/v1/device_config/menu`；G180 `GET /api/v1/device_config/:id`；G181 `PUT /api/v1/device_config/batch`；G182 `GET /api/v1/device_config/connect` | `router/apps/device_config.go` |
| **A02-E17** | A02 / device_config | G183 `GET /api/v1/device_config/voucher_type`；G184 `GET /api/v1/device_config/metrics/menu`；G185 `GET /api/v1/device_config/metrics/condition/menu` | `router/apps/device_config.go` |
| **A04-E02** | A04 / event | G186 `GET /api/v1/event/datas` | `router/apps/event_data.go` |
| **A05-E06** | A05 / expected | G187 `GET /api/v1/expected/data/list`；G188 `POST /api/v1/expected/data`；G189 `DELETE /api/v1/expected/data/:id` | `router/apps/expected_data.go` |
| **A01-E07** | A01 / settings | G190 `PUT /api/v1/logo`；G263 `PUT /api/v1/sys_function/:id`；G326 `GET /api/v1/logo`；G333 `GET /api/v1/sys_function` | `router/apps/logo.go`、`router/apps/sys_function.go`、`router/router_init.go` |
| **A07-E01** | A07 / push | G191 `POST /api/v1/message_push`；G192 `POST /api/v1/message_push/logout`；G193 `GET /api/v1/message_push/config`；G194 `POST /api/v1/message_push/config` | `router/apps/message_push.go` |
| **A07-E02** | A07 / notification_bridge | G195 `GET /api/v1/notification-default-policy`；G196 `PUT /api/v1/notification-default-policy`；G197 `GET /api/v1/notification_group/native-publish`；G198 `POST /api/v1/notification_group/native-publish` | `router/apps/notification_groups.go` |
| **A07-E03** | A07 / notification_legacy | G199 `POST /api/v1/notification_group`；G200 `DELETE /api/v1/notification_group/:id`；G201 `PUT /api/v1/notification_group/:id`；G202 `GET /api/v1/notification_group/list` | `router/apps/notification_groups.go` |
| **A07-E04** | A07 / notification_legacy | G203 `GET /api/v1/notification_group/:id`；G204 `GET /api/v1/notification_history/list`；G205 `POST /api/v1/notification/services/config`；G206 `GET /api/v1/notification/services/config/:type` | `router/apps/notification_groups.go`、`router/apps/notification_history.go`、`router/apps/notification_services_config.go` |
| **A07-E05** | A07 / notification_legacy | G207 `POST /api/v1/notification/services/config/e-mail/test` | `router/apps/notification_services_config.go` |
| **A01-E08** | A01 / api_keys | G208 `POST /api/v1/open/keys`；G209 `GET /api/v1/open/keys`；G210 `PUT /api/v1/open/keys`；G211 `DELETE /api/v1/open/keys/:id` | `router/apps/open_api_keys.go` |
| **A11-E04** | A11 / observability | G212 `GET /api/v1/operation_logs`；G284 `GET /api/v1/system/metrics/current`；G285 `GET /api/v1/system/metrics/history`；G303 `GET /swagger/*any` | `router/apps/operation_logs.go`、`router/apps/system_monitor.go`、`router/router_init.go` |
| **A11-E05** | A11 / observability | G304 `GET /metrics`；G305 `GET /metrics-viewer`；G306 `HEAD /metrics-viewer`；G317 `GET /health` | `router/router_init.go` |
| **A05-E07** | A05 / ota | G213 `POST /api/v1/ota/package`；G214 `DELETE /api/v1/ota/package/:id`；G215 `PUT /api/v1/ota/package`；G216 `GET /api/v1/ota/package` | `router/apps/ota.go` |
| **A05-E08** | A05 / ota | G217 `POST /api/v1/ota/task`；G218 `DELETE /api/v1/ota/task/:id`；G219 `GET /api/v1/ota/task`；G220 `GET /api/v1/ota/task/detail` | `router/apps/ota.go` |
| **A05-E09** | A05 / ota | G221 `PUT /api/v1/ota/task/detail`；G331 `GET /api/v1/ota/download/files/upgradePackage/:path/:file` | `router/apps/ota.go`、`router/router_init.go` |
| **A10-E03** | A10 / plugin | G222 `GET /api/v1/protocol_plugin/config_form`；G242 `POST /api/v1/service`；G243 `GET /api/v1/service/list`；G244 `GET /api/v1/service/detail/:id` | `router/apps/protocol_plugin.go`、`router/apps/service_plugin.go` |
| **A10-E04** | A10 / plugin | G245 `PUT /api/v1/service`；G246 `DELETE /api/v1/service/:id`；G247 `GET /api/v1/service/plugin/select`；G248 `GET /api/v1/service/plugin/info` | `router/apps/service_plugin.go` |
| **A10-E05** | A10 / plugin | G249 `POST /api/v1/service/access`；G250 `GET /api/v1/service/access/list`；G251 `PUT /api/v1/service/access`；G252 `DELETE /api/v1/service/access/:id` | `router/apps/service_plugin.go` |
| **A10-E06** | A10 / plugin | G253 `GET /api/v1/service/access/voucher/form`；G254 `GET /api/v1/service/access/device/list`；G318 `POST /api/v1/plugin/heartbeat`；G319 `POST /api/v1/plugin/device/config` | `router/apps/service_plugin.go`、`router/router_init.go` |
| **A10-E07** | A10 / plugin | G320 `POST /api/v1/plugin/devices`；G321 `POST /api/v1/plugin/service/access/list`；G322 `POST /api/v1/plugin/service/access` | `router/router_init.go` |
| **A01-E09** | A01 / role | G223 `POST /api/v1/role`；G224 `DELETE /api/v1/role/:id`；G225 `PUT /api/v1/role`；G226 `GET /api/v1/role` | `router/apps/role.go` |
| **A01-E10** | A01 / dictionary | G255 `POST /api/v1/dict/column`；G256 `POST /api/v1/dict/language`；G257 `GET /api/v1/dict/enum`；G258 `GET /api/v1/dict` | `router/apps/sys_dict.go` |
| **A01-E11** | A01 / dictionary | G259 `GET /api/v1/dict/language/:id`；G260 `DELETE /api/v1/dict/column/:id`；G261 `DELETE /api/v1/dict/language/:id`；G262 `GET /api/v1/dict/protocol/service` | `router/apps/sys_dict.go` |
| **A01-E12** | A01 / identity | G270 `GET /api/v1/user/detail`；G271 `PUT /api/v1/user/update`；G272 `GET /api/v1/user/logout`；G273 `GET /api/v1/user/refresh` | `router/apps/sys_user.go` |
| **A01-E13** | A01 / identity | G274 `GET /api/v1/user`；G275 `GET /api/v1/user/tenant/statistics`；G276 `POST /api/v1/user`；G277 `PUT /api/v1/user` | `router/apps/sys_user.go` |
| **A01-E14** | A01 / identity | G278 `DELETE /api/v1/user/:id`；G279 `GET /api/v1/user/:id`；G280 `POST /api/v1/user/transform`；G281 `PUT /api/v1/user/address/:id` | `router/apps/sys_user.go` |
| **A01-E15** | A01 / identity | G282 `GET /api/v1/user/tenant/id`；G283 `GET /api/v1/user/selector`；G309 `GET /api/v1/auth/login-options`；G310 `GET /api/v1/auth/external/providers` | `router/apps/sys_user.go`、`router/router_init.go` |
| **A01-E16** | A01 / identity | G311 `GET /api/v1/auth/admin/providers`；G323 `POST /api/v1/login`；G324 `GET /api/v1/verification/code`；G325 `POST /api/v1/reset/password` | `router/router_init.go` |
| **A01-E17** | A01 / identity | G334 `POST /api/v1/tenant/email/register`；G335 `GET /api/v1/tenant/has-admin`；G336 `GET /api/v1/tenant/setup-state`；G337 `POST /api/v1/tenant/super-admin/init` | `router/router_init.go` |
| **A01-E18** | A01 / identity | G338 `POST /api/v1/tenant/market-register` | `router/router_init.go` |
| **A04-E03** | A04 / telemetry | G286 `GET /api/v1/telemetry/datas/current/:id`；G287 `GET /api/v1/telemetry/datas/current/keys`；G288 `GET /api/v1/telemetry/datas/current/detail/:id`；G289 `GET /api/v1/telemetry/datas/history` | `router/apps/telemetry_data.go` |
| **A04-E04** | A04 / telemetry | G290 `GET /api/v1/telemetry/datas/history/pagination`；G291 `GET /api/v1/telemetry/datas/history/page`；G292 `DELETE /api/v1/telemetry/datas`；G293 `GET /api/v1/telemetry/datas/statistic` | `router/apps/telemetry_data.go` |
| **A04-E05** | A04 / telemetry | G294 `GET /api/v1/telemetry/datas/statistic/batch`；G295 `GET /api/v1/telemetry/datas/set/logs`；G297 `GET /api/v1/telemetry/datas/simulation`；G298 `POST /api/v1/telemetry/datas/simulation` | `router/apps/telemetry_data.go` |
| **A04-E06** | A04 / telemetry | G299 `GET /api/v1/telemetry/datas/simulation/init`；G300 `POST /api/v1/telemetry/datas/simulation/send`；G301 `GET /api/v1/telemetry/datas/msg/count` | `router/apps/telemetry_data.go` |
| **A11-E06** | A11 / file | G302 `POST /api/v1/file/up`；G307 `GET /files/*filepath` | `router/apps/upload.go`、`router/router_init.go` |
| **A07-E06** | A07 / notification_source | G308 `GET /api/v1/notification/session-context`；G312 `GET /api/v1/notification/source-members/:id`；G313 `GET /api/v1/notification/source-app-members/:id`；G314 `GET /api/v1/notification/source-app-devices/:id` | `router/router_init.go` |
| **A07-E07** | A07 / notification_source | G315 `GET /api/v1/notification/source-tenants/:id`；G316 `GET /api/v1/notification/source-policies/:id` | `router/router_init.go` |
| **A04-E07** | A04 / ws | G327 `GET /api/v1/telemetry/datas/current/ws`；G328 `GET /api/v1/device/online/status/ws`；G329 `GET /api/v1/device/online/status/ws/batch`；G330 `GET /api/v1/telemetry/datas/current/keys/ws` | `router/router_init.go` |
| **A11-E07** | A11 / settings | G332 `GET /api/v1/systime`；G341 `GET /api/v1/sys_version` | `router/router_init.go` |
| **A11-E08** | A11 / sse | G345 `GET /api/v1/events` | `router/sse.go` |
| **A06-E09** | A06 / automation_bridge | G346 `GET /api/v1/compat/scene`；G347 `GET /api/v1/compat/scene/detail/:id`；G348 `POST /api/v1/compat/scene/active/:id`；G349 `GET /api/v1/compat/scene_automations/list` | `internal/automationcompat/routes.go` |
| **A06-E10** | A06 / automation_bridge | G350 `GET /api/v1/compat/scene_automations/detail/:id`；G351 `POST /api/v1/compat/scene_automations/switch/:id`；G352 `GET /api/v1/compat/scene_automations/log`；G353 `GET /api/v1/compat/alarm/info` | `internal/automationcompat/routes.go` |
| **A06-E11** | A06 / automation_bridge | G354 `GET /api/v1/compat/alarm/info/history`；G355 `GET /api/v1/compat/alarm/info/history/:id`；G356 `GET /api/v1/compat/stats`；G357 `GET /api/v1/compat/metrics` | `internal/automationcompat/routes.go` |

“允许起点文件”不是要求只改router；必要调用链文件由父任务及T01模板约束，在开工文件清单中列明。没有完成前置的卡片先处理前置，不得自行发明模块/事务/权限契约。
