# ThingsPanel 双团队成果复查与统一收尾提示语

复查日期：2026-11-02（Asia/Shanghai）。用途：两队暂停后保存成果、修复问题、继续剩余开发并共同验收。本次仅检查代码、提交和测试证据，未合并代码、部署、操作真实设备或修改现有业务数据库。

## 1. 结论

**已有一批真实修复和有价值的实验，应保留并集成；目前还不能认定两大批任务已经完成。剩余工作包含主体实现，不能全部按“只差合并、测试和文档的小尾巴”派发。**

最需要立即处理的是以下问题：

1. **安装器有本次可重复的故障。** 在临时目录和替身命令下，14 个下载失败位置中，13 个会改变旧 Compose 文件。当前单文件临时下载、单独备份 Compose，尚未形成整组配置的可靠更新与恢复。
2. **设备并发保护漏了一条实际入口。** 普通新建子设备已加入父行事务锁，删除也有真实改进；但绑定已有子设备的 `CreateSonDevice` 仍直接写 ParentID，没有采用同一租户与父行保护。
3. **安装恢复尚未实现完整闭环。** A08 的补偿、重试、后续绑定主要靠拒绝执行来保护；原同步安装链仍有请求摘要未核对、映射错误被忽略、状态落库失败却继续报告完成等问题。它也尚未在本次候选中迁入 Encore。
4. **Agent 和 SDK 尚未接成真实执行链。** Agent 是本地状态管理内核；SDK 是单进程执行控制组件。两者都有值得保留的测试，但尚不能证明插件实际安装、停用、旧实例隔离和升级切换。
5. **多数据库与正式安装仍有主体缺口。** ThingsVis 已做真实 PostgreSQL/MySQL 局部验证，但核心数据库接线、正式 ThingsVis 制品、已有库升级、安装记录和四种完整组合尚未共同验收。

建议由**一名收尾总负责人**维护统一集成版本、剩余任务账和共同门禁，甲乙两队继续按既定模块分工。先保全并收拢成果，先修明确缺陷，再补真实运行链，最后对同一组制品验收。无需推翻已经做出的正确修复。

## 2. 本次怎样判断“完成”

本次查看了社区远端 main、已有集成分支、甲队八个候选工作目录、乙队 backend 候选，以及 installer、ThingsVis、frontend、Horizon、Device SDK。对已定位的候选取得了变更源码、未提交文件、交接文档和选取的测试日志，并检查实际函数和调用顺序。

没有依据分支名、文件名或“测试目录存在”判断通过；也没有把未提交或未合 main 本身认定为代码错误。未提交会影响准确交接，必须保存，但不应抹掉成果。其他尚未提供的新目录或新制品如有有效成果，应加入统一清单后复用。

| 证据级别 | 本次例子 | 可以证明什么 |
| --- | --- | --- |
| 本轮独立重跑 | SDK 6 项 ExecutionGate 并发相关测试通过；installer 故障测试 13/14 失败 | 被测代码在明确测试条件下的实际结果 |
| 已有执行日志＋对应源码 | T04 的 22 项顶层 PASS；Agent 双架构本地状态测试；隔离 PG 补偿守卫；ThingsVis 真库与 HTTP 记录 | 对应候选和测试范围的阶段成果，不能自动外推到新集成版本 |
| 静态确认 | 子设备绑定未采用新屏障、A08 忽略部分持久化错误、Agent 无执行器、正式配置仍 PG | 当前源码存在的实现或缺口；未声称进行过线上故障复现 |
| 静态竞态推演 | Horizon 固定对象路径先写、DB 版本后提交 | 有充分代码依据的并发风险，需用定向并发测试复现和关闭 |
| 草案或 fixture | 原键查询、J3 原语、部分合同与页面替身 API | 规则或局部消费者行为；不能等同真实服务端功能 |

## 3. 已做出的成果：应保留什么

### 3.1 甲队

**设备删除与普通子设备创建有实际修复。** T03 候选在删除前按 tenant/device 加锁，检查依赖，检查删除行数，并将提交结果未知与已确认提交区分；普通子设备创建在同一事务里锁父设备、创建子设备及分组；场景引用也增加了设备锁。这些是有效实现。相关真实驱动测试源码已存在，但本次没有取得这些新增测试的完整执行日志，不能直接标全部通过。来源见 E03。

**T04 有清晰的实现和通过记录。** `313260442c844502e080db3867d8d23f745a3e76` 的写入实现使用结构化键，先确定数据库实际接受的历史值，再更新当前值，处理乱序与同时间戳冲突；Commit/Rollback 结果未知时不盲目 fallback。现有 `final-run.txt` 共 22 个顶层 PASS、无 SKIP，包含 PostgreSQL 集成用例和纯去重单测。提交回包丢失是在 GORM ConnPool 包装层、底层事务完成后注入错误，不能说成真实网络断线或 PostgreSQL 崩溃试验。本轮未重跑，日志尚缺完整命令、数据库版本与测试时源码摘要。来源见 E04。

**T05 不只是一个配置开关。** 有效配置快照冻结、无效 native 配置不回落 legacy、多个旧手动/定时/遥测执行入口的 mode guard 都有源码。仍需证明 native 侧实际接管手动、定时、事件等能力，并在集成版本上完成唯一执行者与重启恢复验收。来源见 E05。

**身份与租户修复、SDK 基础应保留。** ServiceAccess 的 List/Update/Delete、API key 有租户/有效状态改进；SDK 的 ExecutionGate 本轮 6 项 `-race` 测试通过。但普通创建接口尚无安装需要的稳定操作键语义，SDK 真实 Server 尚未调用 gate。来源见 E06、E07。

### 3.2 乙队

**ThingsVis 做过真实数据库实验。** 有 PG 14.5、MySQL 8.4.13 arm64、独立 Prisma client、八类合成模型与引用、同引擎 dump/restore、双 provider 空库账本、真实应用健康/注册/登录、应用重启及合成增量迁移记录。MySQL DDL 中途失败的实验还正确暴露了已建表但账本未完成、后续 P3009 阻止继续的状态。这些是可复用成果。来源见 E08。

它们主要采用在已编译 v1.0.23 镜像上替换客户端的候选制品，不包含当前全部源码的完整 Next.js 构建；不能代替正式升级、已有 PG 库迁移接管、跨引擎搬迁及回退。后续加强 guard 的 PG 镜像构建失败，与早期成功的 PG 镜像要分别记录。MySQL 的邮箱大小写、身份长度限制等语义仍须统一裁定和存量预检。

**Agent 本地状态内核有测试。** 两份架构日志各记录 13 项顶层 PASS，包含本地持久化、重新打开、重放与跨进程争用；当前新增的 consistency 等测试不能直接继承旧日志。README 明确说明没有 workload 执行、API、调用鉴权或平台联络，不能作为可部署 Agent。来源见 E09。

**安全守卫和合同实验有价值。** A08 隔离 PostgreSQL 的五个补偿守卫子项有 PASS 和包级 `ok`，fake HTTP 零调用；包装脚本没有可靠保留原 Go 命令退出码，新增未提交 Transition 测试也不能继承旧结果。34 项契约校验器全部通过。市场页面确实有真实组件及业务 API 调用，已有五个浏览器场景通过，但服务端是内存 fixture，且通过记录属于旧候选，当前提交与未提交页面未完成真实链路复验。它们应作为阶段证据保留。来源见 E10、E11。

## 4. 明确缺口与风险

### F01｜P0：安装器下载失败会破坏旧配置集合

**证据：** `/Volumes/500G/code/team-b-installer-20261102/install.bash` 的 `download_file` 先下载单个临时文件，随后立即覆盖目标；多个文件依次执行。`upgrade.sh` 则直接 `curl -o` 写正式路径，仅单独备份 Compose。

**本轮复现：** 2 个 test method 下共 14 个下载失败边界，安装第 2～7 个、升级第 1～7 个均未保住旧 Compose，exit 1。仅安装第一次下载失败保持原内容。测试运行在私有临时目录，Docker/curl/wget 均为替身，没有调用真实 daemon 或网络。Windows 只有静态相同写法的观察，本次未执行 Windows。

**收尾：** 全部文件先暂存、校验，再受控替换；同时处理模式、原不存在文件、并发升级和重启恢复。`trap` 抓不到 kill -9；一组文件不能靠一次单文件 rename 或一个 `.bak` 证明恢复完整。数据库/业务副作用还需要自己的恢复协议。详见 E12。

### F02｜P0：父子设备保护遗漏已有子设备绑定入口

**证据：** T03 当前 `internal/service/device.go` 的 `CreateSonDevice` 仍按 sonID 查询并直接设置 ParentID，未使用普通创建新加的父设备 tenant 校验和同事务父行锁。API 与 `/api/v1/device/son/add` 路由仍调用它。

**收尾：** 复用现有父行协议，把绑定已有子设备及所有实际 reparent 写入口一起纳入；验证跨租户拒绝、删除先持锁/绑定先持锁两个顺序、失败无副作用。此项为静态确认未采用同一保护，本轮未攻击现有服务，也未断言所有其他数据库约束都不存在。详见 E03。

### F03｜P0/P1：A08 持久化错误、幂等与恢复仍未闭合

**证据：** `InstallBundle` 命中 tenant+key 后直接返回旧结果，没有核对新请求摘要；默认 key 仅由 bundle/version/tenant 组成，无法清楚区分同租户同版本的不同安装实例。创建模板/看板后的部分 mapping、binding 错误未处理；最终状态写失败仍继续返回目标状态并通知 Horizon。原 `UpdateStatus` 没有统一的并发条件，新加一个 CAS 不能覆盖所有旧写者。

`CompensateInstallation`、`RetryInstallation`、`UpdateDeviceBinding` 当前是有意拒绝不安全操作。保护应该保留，但不能登记为恢复功能完成。候选仍是原 project service/repo 同步链，没有本轮 A08 的 Encore、路由或发布服务迁移。

**收尾：** 先收口稳定操作键、实例身份和请求摘要；实现持久 operation/step、受控状态转换、真实 worker 与原键查询；明确处理每次落库错误；最终状态未可靠保存不得报告成功。远端创建成功而本地记录失败时，按原键核对已有结果，不能再创建一份。补齐恢复后再开放相应操作。详见 E10。

### F04｜P1：租户最新遥测汇总当前是业务不可用

**证据：** `10e3f13…` 的 `GetTenantTelemetryData(_ string)` 直接返回 `CodeServiceUnavailable`，测试也以无数据、无 SQL/RPC 为目标。这里指业务错误；旧 HTTP 封装测试仍期望 HTTP 200，不应误写成该入口已改 HTTP 503。

**收尾：** 完成 tenant/device/删除代次一致的安全查询，再恢复本期承诺能力。不是恢复所有旧接口；已批准退役的入口按原安排退出。详见 E13。

### F05｜P1：J2/J6 还缺真实 provider 与执行接线

**证据：** `ServiceAccess.CreateAccess` 仍随机生成 UUID 并直接插入，没有稳定操作键及原键查询约定。SDK gate 只在自身文件和测试出现，未接真实 Server；Agent 本地 verifier 也不能自行证明远端旧实例已停止。

**收尾：** 在唯一业务 owner 补 prepare/query/activate/revoke 和资源归属；SDK 真实执行前校验可信身份、绑定及代次；乙实现有限 Agent executor、可信任务与持久回执。安装 generation、Broker route epoch、设备删除 generation、binding revision 分开定义，不能拿一个内存 epoch 混用。详见 E06、E07、E09。

### F06｜P1：多关系库与时序库仍缺全平台交付

**证据：** 当前核心候选 `initialize/pg_init.go` 仍 `gorm.Open(postgres.Open(...))`，devicemetadata 仍有 pgxpool，多个原生服务仍使用原 sqldb 资源；T04 仍用单个关系事务写历史/current/receipt，并含 PG 方言。本次候选没有提供核心 R/S 主干的统一多引擎制品。

**收尾：** 先核认其他有效候选是否存在，已有即复用；未完成的按原 R01～R09、S01～S05 逐资源补齐。统一调用迁移机制，各 owner/schema 保留原账本与历史 checksum，同一 schema 唯一执行者。历史进入统一时序端口；最新快照、当前属性、业务回执仍归既定关系库 owner。不能简单替换 DSN，也不能暗留核心或安装表依赖 PG。详见 E02、E04、E14。

### F07｜P1：ThingsVis 候选尚未成为正式双 provider 产品

**证据：** 正式 package/Dockerfile 仍按默认 PG schema 生成，正式入口仍 `db push`；新 source 查询适配不在改装候选镜像内。导入回执与 query-by-key 还在草案中，同请求重试生成两个看板已有实验记录。已有 PG 无正式账本时如何安全接管，尚无完整证据。

**收尾：** 复用已验证 client/DDL，确定身份/邮箱/搜索和长度规则，补存量碰撞预检，完成当前源码构建、正式 provider 配置、已有库 baseline/升级、迁移失败核对恢复、同库导入回执。PG/MySQL 同引擎备份不能替代 PG→MySQL 搬迁及目标写后回退。详见 E08、E15。

### F08｜P1：Horizon 发布回执与对象提交有风险

**证据：** `PublishBundle` 使用安装记录 repo 查发布幂等；先写 `bundles/<bundleKey>/<version>/bundle.json` 固定对象路径，再插 DB 版本行。按该调用顺序，两个同版本异内容请求可能造成“DB 获胜版本摘要与最后覆盖的对象不一致”。这是静态竞态推演，本轮没有运行 MinIO 并发复现。

**收尾：** 先用可控制时序的并发测试复现，建立正确发布 scope/请求摘要与回执、不可变对象引用和提交规则，确保失败请求不能覆盖成功版本的内容。已有敏感字段扫描应保留；shape 检查不代替验签、完整摘要、目录撤回和信任规则。详见 E16。

### F09｜P1：当前前端与完整业务链未验收

**证据：** 页面源码确实调用业务 API，但通过的浏览器证据来自旧版本加替身服务。unknown/inFlight 主要在内存；刷新、重新打开或跨设备不具有服务端持久恢复。旧全量 vue-tsc 记录 exit 2，完整日志不全；局部修正和 SFC 通过不能外推为当前整站构建通过。

**收尾：** 用同基线、同依赖树保存完整类型/构建日志，分开既有与新增诊断；在当前精确版本上连真实隔离 backend/Horizon/ThingsVis，验收回包丢失、刷新与原键恢复。Horizon 安装记录入口当前 HTTP 503 也要与 Community 本地安装职责分清。详见 E11、E16。

## 5. 统一收尾任务清单

这里是对已有任务的收尾拆分，不新增平行模块，也不删除原 35 包和插件阶段任务。

| 收尾卡 | 主责 / 先决条件 | 应交付的结果 | 最小验收与难点 |
| --- | --- | --- | --- |
| C00 保存成果、建立唯一集成基线 | 总负责人；立即开始 | 所有候选 commit/dirty 摘要、依赖与测试绑定；独立集成分支；原任务状态账 | 语义合并 device 等重叠文件；不 reset/clean 旧目录；每项区分缺实现、待集成、待独测、待联调 |
| C01 安装配置暂存与可恢复更新 | 乙；C00 后可与甲并行 | 正式 Linux/已承诺 Windows 入口使用同一更新流程；可信预检、持久恢复状态 | 现有 14 个下载边界全部通过，再覆盖替换失败、kill -9、原不存在文件、权限、并发和真实启动；不增加第二安装器 |
| C02 父子引用与删除屏障补全 | 甲；C00 | 合入 T03/父创建/场景保护，修 CreateSonDevice 及实际 reparent 入口 | 两种锁顺序、跨租户、Commit unknown、无误断开；测试缺 DSN/Redis 的 Skip 不算通过 |
| C03 遥测与自动化正常能力闭环 | 甲；C00，依赖相应读/执行 owner | 合入 T04、恢复受支持最新汇总；旧 owner 停止与 native 可用同时交付 | 保留 22 项候选证据并在相关集成变更后复验；事件/属性/定时/手动不能因 guard 全停而算成功 |
| C04 可执行 J1～J8 与业务 provider | 甲业务 owner＋乙 consumer | 版本固定的身份、稳定键、原键查询、归属、代次、待激活与迁移/恢复契约 | 已有 fixture 复用，落实 provider/consumer；先打通一个真实 PG 安装链，避免无限停在草案 |
| C05 A08 Encore 与持久恢复 | 乙；C04 | 唯一编排、真实 Encore API、operation/step、worker、受控状态转换和恢复 | 同键异内容冲突、同版本多实例、落库失败、双 worker、丢回包/重启、复用资源不得误删；当前安全守卫合格后再放开 |
| C06 核心 R/S 多引擎余项 | 甲；C04 的存储/迁移合同 | 实际逻辑资源的 PG/MySQL 适配、历史存储两实现与跨库删除/切换 | 各 owner 账本、错误/锁/约束、历史与当前一致性；保留已执行 PG migration，不把最新状态迁到外部 TSDB |
| C07 ThingsVis 正式双 provider 与导入回执 | 乙；C04，配合 C06 | 当前源码正式构建、双 client/schema、已有 PG 升级、看板与回执同库提交 | 身份/邮箱/长度差异、存量冲突、DDL 部分成功、丢回包不重复生成看板；不以合成迁移代替真实升级 |
| C08 Agent 与 SDK 真实执行接线 | 甲 SDK/业务、乙 Agent；C04 | 有限执行器、可信任务、注册/代次/回执、真正 inactive/启用/停用/升级 | 跨进程、旧主机失联、旧任务延迟执行、真实资源争用；保留本地 kernel，不能只增加状态单测 |
| C09 Horizon 发布与当前 UI 恢复 | 乙；C04/C05/C07 | 发布回执、不可变对象与信任链；当前 UI 真实服务端查询 | 同版本异内容并发、过期/撤回目录、验签、刷新/重开、全量构建与相关真实浏览器测试 |
| C10 原路由余项与最终共同验收 | 甲核心/发布、乙 A08/installer/UI，总负责人集成 | 原 35 包与 100 小卡闭合；唯一制品集；J-G0～J-G4 报告 | 逐路由允许 typed/raw、原生消费者、正式退役/静态托管等正确去向；四组合覆盖安装表和启用 ThingsVis；按 owner 切换前确认旧写者退出或接收方有效围栏；共同验收后最终移除旧路由/进程 |

每张卡由技术负责人先给出准确文件、输入输出、失败处理、依赖和验收命令。对已有可靠样例先复核复用；跨库事务、权限、未知结果、旧实例隔离由负责人评审，再分配具体适配和入口任务给成员。不要让成员各自重新设计共同协议。

### 5.1 原 35 包的总范围仍然保留

| 原任务范围 | 本轮可确认状态 / 后续处理 |
| --- | --- |
| T00～T02（3 包） | 身份与租户有改进；统一版本、两个 Go module 的单一源码样例、全部传输契约仍逐卡出证据 |
| T03（1 包） | 部分实际修复，已有子设备绑定入口待收口；新增测试执行结果待绑定 |
| T04（1 包） | 实现与 22 项候选通过记录可保留，等待最终集成版本相关复验 |
| T05（1 包） | 配置和旧入口 guard 已做；native 全部实际触发链仍须证明 |
| R01～R09（9 包） | 本次候选尚未交付全平台多关系库主干，先核认其他现有成果再逐 owner 补齐 |
| S01～S05（5 包） | T04 不代替外部时序端口、持久投影/删除世代和切换搬迁；按原卡补证据与实现 |
| A01～A07、A09～A11（10 包） | 用原 100 小卡逐项核对实际新入口/退役、共享业务与发布选入；不从声明数量计算完成率 |
| A08（1 包） | 乙主责；守卫、fixture 已有，Encore 与持久恢复仍需实现 |
| Q01～Q04（4 包） | 生命周期、唯一 installer、真实矩阵与最终切流共同验收；不提前把 Gin 退役标完成 |

原来的 357 行 Gin 登记和 183 个 Encore 声明是起始台账。本次没有取得覆盖全部余项的新版验收账，不能据此给总体完成百分比，也不能把未出示证据的项一律判为没有开发。

## 6. 两队如何继续而不互相影响

**总负责人唯一管理集成。** 甲队多个 worktree 已修改相同 device/DAL/自动化文件，尤其 T03 与父创建候选存在重叠；必须按语义合并，不能逐目录整文件覆盖。乙的 A08 消费甲的资源与授权协议，不自行另建一套设备/模型/插件业务。共同文件与构建配置指定一个修改者，同仓遵守原有短写入窗口。

**两个团队独立测试，另建共同验收环境。** 分别隔离数据库/schema、Redis 会话/PubSub/锁、MQTT 凭据与 topic、容器名、宿主端口、网络、卷、安装目录和模拟设备。现有 Compose 有固定容器名和已占用端口，不能认为换 `compose -p` 就完成隔离。

**先完成一条真实链，再扩矩阵。** 甲的真实 provider 和 SDK 边界、乙的真实 Agent/编排及 ThingsVis 一起组成固定 PG 首链。在各自模块里继续并行，只有接缝、共同版本和联合环境由总负责人集中管理。

| 门禁 | 共同通过标准 | 本次结论 |
| --- | --- | --- |
| J-G0 | 固定制品/owner、J1～J8 版本和 provider/consumer 可执行合同 | 有候选草案和实验；缺统一已落实的集成版本与合同，不能报总体通过 |
| J-G1 | 真实连接器→Agent 准备→模型导入→设备绑定→激活→上报可读→停用/依赖预览 | Agent/SDK/owner/A08 尚未接成完整真实链；不能用单点健康通过代替 |
| J-G2 | 丢回包、双方重启、双 worker、旧代次、资源抢占、失联旧实例、回退/unknown | 有局部反例及守卫测试；真实组合恢复仍待实现和验证 |
| J-G3 | PG+TimescaleDB、PG+TDengine、MySQL+TimescaleDB、MySQL+TDengine 的闭环与关键故障 | ThingsVis 局部两库成果不等于四种客户全栈；未共同验收 |
| J-G4 | 唯一正式 installer 的新装、已有版本升级、迁移中断恢复与数据切换 | 当前下载失败反例阻断可靠升级；历史升级与全部恢复仍待验收 |

MySQL+TDengine 完整 profile 应在客户栈没有必需 PostgreSQL、且 PostgreSQL 不可达时验证；MySQL+TimescaleDB 中 PostgreSQL 只作为明确声明的时序依赖。安装记录和启用的 ThingsVis 一并参加。公网 Horizon 自己的数据库不自动纳入客户 profile；私有一体化部署单列依赖。不能通过禁用约定的看板能力冒充完整矩阵通过。

## 7. 材料清单与证据索引

### 7.1 交给负责人阅读的材料

1. `/Volumes/500G/docs/ThingsPanel_Encore_Migration_Tasks_2026-11-01.md`：原 35 包、100 个 API 小卡与路由清单。
2. `/Volumes/500G/docs/ThingsPanel_插件发布与一键安装设计_2026-11-01.md`：原插件发布、一键安装、信任和运行设计。
3. `/Volumes/500G/docs/ThingsPanel_双团队并行统筹与负责人提示语_2026-11-01.md`：既定责任边界、J1～J8 与 J-G0～J-G4。
4. `/Volumes/500G/docs/ThingsPanel_双团队成果复查与统一收尾提示语_2026-11-02.md`：本文件，包含本次检查结果与第 9 节统一提示语。

### 7.2 精确证据入口

- **E01 主线：** [社区 backend main 查询](https://api.github.com/repos/ThingsPanel/thingspanel-backend-community/branches/main)，复查时为 `7f694779c979702740b15125164ebec2545604a4`（2026-10-29）。main 未合入本轮提交本身不是功能缺陷，但不能拿它当本轮集成成果。
- **E02 集成与快照：** `/Users/junhong/.codex/worktrees/kernel-results-integration-20261101/backend` 为 `b86bd68a4bf27a2b1525f7827201a6b4b0cdb3cd`，CLI worktree `/Users/junhong/.codex/worktrees/community-cli-completion-20261101/backend` 为 `684e91985dfc7d1959613ff849734454b7a2fd74`；均 clean 且不包含本轮七个候选提交头。祖先检查不能排除等价 cherry-pick。旧 `/Users/junhong/Downloads/code/thingspanel-backend-community` 的本地 main 为 `0e3ba828…` 且有历史未提交内容，应保留。
- **E03 T03：** `/Users/junhong/.codex/worktrees/team-a-t03-device-delete/thingspanel-backend-community/internal/service/device.go`（DeleteDevice、CreateDevice、CreateSonDevice）；同目录下 `internal/dal/devices.go`、`internal/dal/device_trigger_condition.go`、`internal/service/scene_automations.go` 及新增 Postgres 测试。API/路由与社区基线核对。
- **E04 T04：** `/Users/junhong/.codex/worktrees/team-a-t04/thingspanel-backend-community/internal/storage/telemetry_writer.go`、`telemetry_broker.go`、`telemetry_writer_p0_test.go`、`telemetry_winner_crosspath_test.go`、`telemetry_broker_postgres_test.go`；日志 `/Volumes/500G/team-a-t04-evidence-20261102/final-run.txt`。
- **E05 T05：** `/Users/junhong/.codex/worktrees/team-a-automation-mode-20261102/thingspanel-backend-community/internal/automationbridge/bridge.go`、`internal/uplink/native_automation.go`、`internal/service/automate_telemetry.go`、`internal/service/automate_task.go`、`initialize/croninit/cron.go`。
- **E06 租户/安装 provider：** `/Users/junhong/.codex/worktrees/team-a-access-auth/thingspanel-backend-community/internal/service/service_access.go`、`internal/middleware/apikey.go`。
- **E07 SDK：** `/Volumes/500G/code/device-connector-sdk-team-a/execution_gate.go`、`connectorsdk.go`；本轮命令 `go test -race -run '^TestExecutionGate' -count=1 -v .`，独立缓存、关闭模块网络；日志 `/Volumes/500G/thingspanel-closeout-sdk-20261102-t3vuoihe/test.log`，exit 0。
- **E08 ThingsVis 真库：** `/Volumes/500G/team-b-thingsvis-mysql-real-20261102/RESULTS.md`、`REVISED-CANDIDATE.md`；`/Volumes/500G/team-b-thingsvis-real-pg14-20261102/RESULTS.md`；`/Volumes/500G/team-b-thingsvis-dual-restart-20261102/RESULTS.md`、`SIGNOFF-MATRIX.md`、`INSTALLER-BOUNDARY.md`、`pg-build-v2.log`。
- **E09 Agent：** `/Volumes/500G/code/team-b-installer-20261102/agent/README.md`、`state.go`；`/Volumes/500G/team-b-agent-amd64-test-final.log`、`/Volumes/500G/team-b-agent-arm64-test-final.log`。
- **E10 A08：** `/Users/junhong/.codex/worktrees/team-b-compensation-guard/thingspanel-backend-community/internal/service/market_bundle_install.go`、`internal/repo/market_bundle_install_repo.go`；`/Volumes/500G/team-b-compensation-guard-evidence-20261102/go-test.log`；`/Volumes/500G/team-b-contract-fixtures/npm-test.log`；query-by-key、J3 资料分别在 `/Volumes/500G/team-b-a08-api-fixture-20261102` 与 `/Volumes/500G/team-b-j3-vectors-20261102`。
- **E11 前端：** `/Volumes/500G/code/team-b-frontend-publish-20261102/src/views/device/config/publish-attempt.mjs`、`PublishWizard.vue`；市场 `InstallWizard.vue`、`InstallationDetail.vue`；`/Volumes/500G/team-b-publish-browser-20261102/RESULTS.md`；前端仓库 `docs/audits/team-b-publish-safety-20261102.md`。
- **E12 安装故障：** `/Volumes/500G/code/team-b-installer-20261102/install.bash`、`upgrade.sh`；原 harness `/Volumes/500G/team-b-installer-stage-fixtures-20261102/test_download_failure.py` 和 README。本轮重跑日志副本 `/Volumes/500G/thingspanel-closeout-snapshot-20261102/installer-download-repro.log`。在候选目录重现命令：`TP_STAGE_INSTALLER_SOURCE=/Volumes/500G/code/team-b-installer-20261102 PYTHONDONTWRITEBYTECODE=1 python3 /Volumes/500G/team-b-installer-stage-fixtures-20261102/test_download_failure.py -v`。该 harness 只模拟下载故障，不执行真实安装。
- **E13 最新汇总守卫：** `/Users/junhong/.codex/worktrees/team-a-telemetry-read-guard-20261102/thingspanel-backend-community/internal/service/device.go`（GetTenantTelemetryData）与 `internal/api/telemetry_latest_unavailable_test.go`。
- **E14 核心存储：** `/Users/junhong/.codex/worktrees/team-a-access-auth/thingspanel-backend-community/initialize/pg_init.go`、`encore-api/devicemetadata/legacydb.go` 及 northbound/automation/devicemetadata/notification 原生 DB 声明。
- **E15 ThingsVis 正式接线：** `/Volumes/500G/code/team-b-thingsvis-mysql-20261102/apps/server/package.json`、`Dockerfile`、`docker-entrypoint.sh`、`prisma/schema.prisma`、两份 `Dockerfile.team-b-*-candidate`；`docs/dashboard-import-recovery-implementation-plan-20261102.md`。
- **E16 Horizon：** `/Volumes/500G/code/thingspanel-horizon/services/market-service/internal/service/bundle_service.go`（PublishBundle）、`internal/handler/bundle_handler.go`（InstallationUnavailable）、`portal/src/api/bundle-download-validation.ts`。

本轮只读源码与历史证据快照保存在 `/Volumes/500G/thingspanel-closeout-snapshot-20261102`。包含 core/plugin/integration 三组选择性归档；不应把这些快照当成可直接部署的新仓库。

## 8. 实际工作目录与复查版本

以下是本次复查快照，不是自动选定的最终发布集。开始收尾时重新检查新增变更，先保存再集成。

| 工作目录 | HEAD | 本次状态 |
| --- | --- | --- |
| `/Users/junhong/.codex/worktrees/team-a-access-auth/thingspanel-backend-community` | `0b19b8d2935ceb5deab79cc6883141df1338b86a` | 有未提交变更；HEAD 不含全部成果 |
| `/Users/junhong/.codex/worktrees/team-a-t03-device-delete/thingspanel-backend-community` | `0b19b8d2935ceb5deab79cc6883141df1338b86a` | 有未提交变更；HEAD 不含全部成果 |
| `/Users/junhong/.codex/worktrees/team-a-t04/thingspanel-backend-community` | `313260442c844502e080db3867d8d23f745a3e76` | clean |
| `/Users/junhong/.codex/worktrees/team-a-automation-mode-20261102/thingspanel-backend-community` | `8c8796065b08e1536bc0e53bd220d89d0e6cd200` | 有未提交变更；HEAD 不含全部成果 |
| `/Users/junhong/.codex/worktrees/team-a-parent-create-fence-20261102/thingspanel-backend-community` | `0b19b8d2935ceb5deab79cc6883141df1338b86a` | 有未提交变更；HEAD 不含全部成果 |
| `/Users/junhong/.codex/worktrees/team-a-telemetry-read-guard-20261102/thingspanel-backend-community` | `10e3f13f29757e14a415672d2ae8819f2a139109` | clean |
| `/Users/junhong/.codex/worktrees/team-a-template-owner-20261102/thingspanel-backend-community` | `79683d0d004f1338ebfbd46250fdae2720b814c0` | 有未提交变更；HEAD 不含全部成果 |
| `/Users/junhong/.codex/worktrees/team-a-ws-device-auth-20261102/thingspanel-backend-community` | `e2b63e7358aa29534b1b27f8387d71f12057405e` | 有未提交变更；HEAD 不含全部成果 |
| `/Users/junhong/.codex/worktrees/team-b-compensation-guard/thingspanel-backend-community` | `dc2bf9d899717b8b29b505a6cd16c500b6e4bf0c` | 有未提交变更；HEAD 不含全部成果 |
| `/Volumes/500G/code/team-b-installer-20261102` | `282eb7c38cb5200d21171094a262d5db6f371851` | 有未提交变更；HEAD 不含全部成果 |
| `/Volumes/500G/code/team-b-thingsvis-mysql-20261102` | `e22e42d7a0bcfa634d7b62f36dc901f177980913` | 有未提交变更；HEAD 不含全部成果 |
| `/Volumes/500G/code/team-b-frontend-publish-20261102` | `012f52cce92ca66c34d29fd4dbb8089e243300e3` | 有未提交变更；HEAD 不含全部成果 |
| `/Volumes/500G/code/thingspanel-horizon` | `7ce55781ef8a8dd3a074907c213c0c131b386ce6` | 有未提交变更；HEAD 不含全部成果 |
| `/Volumes/500G/code/device-connector-sdk-team-a` | `f183a19cbc51b6c4cb1bba4119a9105098c0fed7` | clean；本轮独立窄测通过 |

选择性源码归档的 tar.gz 校验值：

- `core`：`76e73446fcf2f12fe0b50135ad3bb3bcd4e19c9b3409ba1264e0a858968c86f2`。
- `plugin`：`f8ebc07b95bcfc29e7b0be465fc7fc430da15e68ebbb0114c30abf8584864f3a`。
- `integration`：`196afa7bd643f2e8e5940ff02c3d35256a335457e0b9a0a3c1e790b30ff3961c`。

原始 `.b64` 文件位于 `/Volumes/500G/thingspanel-closeout-snapshot-20261102`，内容为上述 tar.gz 的 Base64；原工作目录仍是开发入口。

## 9. 可直接派发给收尾总负责人的统一提示语

以下提示语整体转交给一名总负责人，由其统筹原甲乙技术负责人及队伍。

你是 ThingsPanel 本轮收尾总负责人，统筹原甲、乙两位技术负责人及其队伍。两队已暂停，请先接管现有成果，完成剩余实现、集成和各自测试，再完成共同验收。保留有价值的补丁和真实测试成果，集中解决尚未闭环的工作。

### 一、先读四份材料，固定实际版本

1. `/Volumes/500G/docs/ThingsPanel_Encore_Migration_Tasks_2026-11-01.md`
2. `/Volumes/500G/docs/ThingsPanel_插件发布与一键安装设计_2026-11-01.md`
3. `/Volumes/500G/docs/ThingsPanel_双团队并行统筹与负责人提示语_2026-11-01.md`
4. `/Volumes/500G/docs/ThingsPanel_双团队成果复查与统一收尾提示语_2026-11-02.md`

第 4 份材料给出此次实际工作目录、候选提交、已验证成果、剩余问题与证据。开始时重新核实 HEAD 和工作区差异，避免把复查之后的新改动覆盖掉。原 35 个任务包、100 个 API 小卡仍是完整范围；按本次复查确认的剩余工作推进。先查询已有共享记忆和仓库说明，结束时按既有治理流程做记忆收尾。

先交付一份统一基线清单：每个仓库的准确目录、基础 commit、候选 commit、未提交文件/补丁摘要、已有测试对应版本、保留/合入/补测/退回的处理结果。保全原目录里的历史改动，尤其旧 backend 根目录；建立独立集成分支，禁止用 reset、clean 或批量覆盖来整理别人的成果。总负责人唯一负责集成清单与共同门禁状态。

### 二、分工保持清楚

甲负责核心业务、身份与租户授权、Encore 入口迁移、关系/时序存储、业务资源的 prepare/query/activate/revoke 和 SDK 业务执行入口。乙负责原 A08 安装编排及其 Encore 接口、市场前端与 Horizon、Agent、唯一正式 installer，以及 ThingsVis 数据库适配。

乙通过甲的业务接口操作模型、设备、绑定、连接器登记等资源，并保存操作与资源引用；不另建同义业务模块，也不绕过业务接口直接改甲的表。甲提供镜像、配置、迁移 runner、就绪和生命周期契约，乙接入正式安装器；SQL/迁移内容由对应数据模块维护。

同一仓库遵守现有串行写入要求，提前排短写入窗口。甲、乙各用独立测试环境；分别隔离端口、容器名、网络、卷、数据库、Redis、MQTT 身份和安装目录。共同验收使用第三套固定版本环境。共享入口、依赖文件、构建发布配置由总负责人安排单一修改者。

### 三、先关闭本次明确发现的问题，再集成已做出的修复

乙优先修正式 installer 的整组配置更新。本次对当前源码的隔离重跑，14 个下载失败边界中 13 个改变了旧 Compose；`install.bash` 单文件临时下载后立即覆盖，`upgrade.sh` 仍直接写正式文件。先把全部配置及管理文件下载到隔离暂存区，验证来源/摘要/完整性及配置，再受控替换；失败保持或恢复原文件、权限和原来不存在的文件状态。保存可供重启恢复的进度，覆盖并发升级、信号中断和 kill -9；shell trap 或一个 `.bak` 文件不能证明整组恢复。复用现有下载故障 harness 先让 14 个边界全部通过，再扩展真实安装/升级、Linux 和已承诺的 Windows 路径。禁止直接运行新下载的 `prepare.py` 来充当它自身的可信预检。

甲先逐项核对并合入权限、设备/模板删除、WebSocket 设备授权、遥测一致性和自动化模式相关候选。普通设备创建已经补了父行事务锁，继续复用；把仍直接绑定已有子设备的 `CreateSonDevice` / `/api/v1/device/son/add` 接入相同的租户归属校验与父子并发屏障，覆盖跨租户、父设备删除竞争和更新失败。验证原生和仍保留的旧入口都执行相同的权限与业务规则。删除必须覆盖受影响行数、事务提交异常、删除后的重放；保留 T04 已有的同时间戳、乱序、局部失败、历史和当前值一致性测试；自动化必须证明唯一执行者和可靠触发。`GetTenantTelemetryData` 当前直接返回不可用，应在权限、设备归属、删除代次和安全查询完成后恢复本期承诺的最新遥测汇总能力。

对暂时拒绝操作的安全守卫，保留保护并登记对应未完成能力；本期承诺支持的能力补齐安全实现后才能恢复，不能只删除守卫。明确退役或正式不支持的旧入口保持批准后的处置，不为恢复旧入口重新开发一套业务。

继续完成多关系库与多时序库：覆盖主业务、身份、物模型、自动化、通知、北向、Broker/control/ledger、安装记录，以及启用的 ThingsVis。逐个替换仍然直接绑定 PostgreSQL 的实际连接与查询，落地目标数据库对应的迁移、约束、并发领取、锁、幂等和错误处理。统一迁移调用机制，每个 owner/schema 保留自身版本账本，同一 schema 只能有一个迁移执行者；不改已执行的 PostgreSQL 历史迁移，也不合并全部账本。ThingsVis 保留自身迁移工具的明确归属。统一历史时序入口，覆盖历史写入、查询、统计、导出、清理和跨库删除/切换；最新快照、当前属性和业务回执仍由既定关系库 owner 管理。验证历史与当前状态的正确衔接，不能只切换连接字符串。

更新 Encore 路由台账：每个原 Gin 路由都有明确去向，包括真实 Encore typed/raw 入口、已有原生消费者、批准退役、静态托管或关闭条件演练入口；保留能力必须指向相同业务用例与实际发布服务，并说明客户端兼容和测试。完成 A01～A11 剩余小卡；A08 由乙负责。按相应处置和回归结果退役旧路由，不能依靠新增接口数量推断完成率。

### 四、把插件安装、停用和恢复接入真实运行链路

甲乙共同冻结最小可执行的 J1～J8 契约，落实身份/租户/权限、稳定操作键、资源归属、查询与恢复、版本/摘要/数据库能力、各类代次、真实未启用状态、安装/升级和补偿。双方提供 provider/consumer 测试并锁定同一契约版本，随后按模块并行实现。每次业务 owner 切换前确认旧写者退出或接收方有有效围栏；共同验收后再最终清理旧路由和进程。

将 SDK 执行控制接入真实业务入口；由可信服务校验身份与当前绑定，不把本地内存 epoch 当作跨进程隔离。甲负责业务授权与旧实例失效规则，乙负责 Agent 的真实进程/容器执行及恢复。证明未激活实例不能产生设备业务副作用；暂停、升级、失联旧实例和重启后仍受约束。

乙完成持久化的安装操作/步骤状态机与真实 worker，接入 Encore API、平台业务接口、Agent 和 ThingsVis。当前 A08 的 `CompensateInstallation`、`RetryInstallation` 和 `UpdateDeviceBinding` 主要是拒绝不安全操作，必须接上真正的核对、恢复和绑定流程；保留安全守卫直到对应流程通过。甲的 `ServiceAccess.CreateAccess` 目前随机生成 UUID，需要补稳定操作键、请求摘要和按原键查询结果，不能要求乙靠重复创建猜测成功。每个步骤在丢回包或进程重启后能查询已有结果；重复请求复用同一操作，参数改变返回冲突。区分成功、明确失败、未知结果和需要人工处理；未知不能当成成功，也不能盲目重做副作用。卸载和补偿只能处理本操作确实拥有的资源，复用资源、后续修改、并发安装和删除都要有回归。

优先修 `InstallBundle` 的两处实际缺口：命中同 tenant/key 时校验请求摘要，不再无条件返回旧结果；同租户安装同版本的不同实例有明确的实例身份。资源映射、绑定记录和最终状态的写入失败必须被处理，所有转换使用约定的并发条件；禁止最终状态未持久化成功却返回完成、发送成功通知。以真实数据库错误、响应丢失和双 worker 测试验证，不能只在单独的 Python fixture 中实现正确语义。

乙将已有 ThingsVis PostgreSQL/MySQL 候选及测试整理为正式构建、客户端生成、迁移与安装接线；将 Agent 候选整理为真实受控执行服务。完成市场前端和 Horizon 的发布、浏览、下载、安装与恢复状态接线，任何状态都要以服务端实际结果为准。保留原插件设计要求的验签、可信目录有效期及撤回、依赖锁定、摘要校验和 Agent 有限操作范围；分别给出正反例测试，单项 hash 测试不能代替整条信任链。

ThingsVis 已有真实 PG14/MySQL8.4 局部证据，先复核复用；它们主要来自在既有已编译镜像上替换客户端的候选验证，不替代当前源码完整构建、已有 PG 库升级、看板导入回执和正式 installer 验收。看板导入恢复草案要落实为按原操作查询的实际接口，避免回包丢失后重复生成看板。Horizon 的发布幂等当前查询安装记录，发布对象又先写固定版本路径再落版本行；先做并发异内容发布与丢回包复现，落实独立发布回执、不可变对象引用和明确冲突，证明 DB 中的摘要与下载对象始终一致，再恢复自动重试。

### 五、按同一条产品链完成测试

先由两队分别运行各自模块测试，报告实际执行、通过、失败和跳过；测试必须关联源码版本与制品摘要。mock、fixture、文本匹配、交叉编译和候选镜像的局部通过都按实际范围记账。

然后在固定集成版本上依次完成：

- J-G0：统一版本、责任清单、可执行契约及双方合同测试。
- J-G1：真实 PostgreSQL 首链，成熟连接器制品 → Agent 准备 → 模型导入 → 设备绑定 → 激活 → 模拟设备上报可读 → 停用/依赖预览。
- J-G2：丢回包、平台/Agent/插件重启、双 worker、旧代次、资源抢占、失联旧实例、版本不符、回退和未知结果等组合故障。
- J-G3：PG+TimescaleDB、PG+TDengine、MySQL+TimescaleDB、MySQL+TDengine 四种 profile 的完整链路与关键故障。包含安装记录和启用的 ThingsVis；MySQL+TDengine 完整 profile 要在 PostgreSQL 不可用时通过。
- J-G4：通过唯一正式 installer 完成新装、升级、迁移中断恢复，以及平台、Agent、插件分别重启/升级。验证可兼容和不可兼容数据的恢复，按既定维护协议验证关系/时序数据搬迁。

### 六、任务卡与最终交付

开工就更新原 35 包、100 个 API 小卡和插件阶段任务的状态账，区分缺实现、待集成、待独立测试、待共同验收及已有有效证据。负责人先复核复用已有可运行的迁移/存储适配样例，缺少时补做一个，再把同类剩余工作分给成员。每张收尾卡写明：负责人、依赖、准确文件与入口、输入输出、正常/失败行为、验收命令、证据位置、受影响的共同门禁。跨库事务、未知结果、权限边界和旧实例隔离由负责人评审；成员按样例实现具体模块。

最终交付可审查的集成分支或 PR、构建制品摘要、更新后的原任务和路由台账、两队各自测试报告、J-G0～J-G4 共同报告、安装升级恢复说明，以及一份真实的剩余问题清单。任何未完成项都标明原因、影响、负责人和解除条件。先完成可审查的开发和测试交付，生产变更遵循已有发布授权。

现在从成果保全和统一基线开始，持续推进剩余实现与验证；阶段性汇报已完成证据和具体阻塞，最终状态以同一组集成制品的实际验收为准。
