# 测试与整版发布验收计划

> **当前设计优先约束（2026-10-27用户纠正）：设备模板不做草稿、版本表、发布或模板切版；只有物模型保留版本。** 此前相反的候选设计已撤回。原188/580/108是历史规划规模，不能再作为拒绝收简的约束；接口/DB/CLI/用例正按新边界重写。详见[设计纠偏与影响检查](DESIGN-CORRECTION-2026-10-27.md)。相关旧测试仅保留历史证据，不证明新需求完成。


日期：2026-10-23。**这里都是待执行规格，不是本轮测试通过报告。** 本轮只做源码与文档静态检查，没有运行应用、数据库迁移、浏览器业务操作或部署。

追踪链：`库存ID → DM任务 → operationId/schema/表 → TC场景 → 候选SHA/制品digest → 执行报告 → 门禁`。完整逐任务场景在 [TASKS.md](TASKS.md) 每行及 [test-cases.json](test-cases.json)（580条）；CLI另有 [CLI.md](CLI.md) 的47个具体命令场景。相似场景可能交叉覆盖，不能把数量相加当作完成率。

## 并行工作线联合门禁

[PARALLEL-COORDINATION-2026-10-25.md](PARALLEL-COORDINATION-2026-10-25.md) 已按2026-10-26用户确认纠正：物模型1.3已经合入且模板创建设备可用；自动化告警待独立开发完成后再接调用，P2后置不阻断本阶段；企业版扫码/产品管理激活、按编号领取资格排除。若有未知活动引用，危险删除/切版保守拒绝。

|边界|任务 / 原有测试ID|可执行断言|
|---|---|---|
|1.3已合并物模型|DM-API-007 / TC-DM-API-007-01；DM-DEV-016|固定合入SHA与模型schema摘要；v1规则及设备在发布v2后不漂移，切版预览列出受影响引用|
|自动化规则（后置）|DM-RT-008 / TC-DM-RT-008-01/02/03|规则owner独立验收；旧config ID只用于离线转换；运行按正式目录和control API；动态/固定成员、重复事件、权限可测|
|告警（后置）| / TC-DM-RT-009-01/02/03|告警episode仍由告警owner维护；模板字段删除、分组变动和设备删除的引用被正确定位/阻断|
|社区版范围排除|DM-DEV-022、DM-DEC-004|不开发企业版扫码/产品管理激活和按编号领取资格；仅验收普通状态与编号查重|
|跨任务合并|DM-QA-008 / TC-DM-QA-008-01/03|记录源SHA、目标SHA、正式OpenAPI digest、共享文件合并责任、真实HTTP/PG/HAR证据，旧控制入口不能通过|

## 1. 证据等级与执行环境

|等级|能证明什么|不能替代什么|
|---|---|---|
|E0 源码/文档|功能意图、当前调用链、候选结构与测试源存在|不能证明运行成功、线上调用或数据已迁|
|E1 单元测试|纯领域验证、类型转换、状态机、幂等摘要、输出格式|不能证明真实PG约束/跨服务一致性|
|E2 合同与真实PG集成|真实schema/FK/事务/CAS/不可变快照与升级路径|不能证明浏览器真实请求或runtime已应用|
|E3 正式HTTP/CLI|路由、authn/authz、schema、错误、持久化和CLI输出一致|不能证明UI按钮/竞态/旧请求清零|
|E4 浏览器|真实页面操作、HAR、角色、表单/弹窗/批量/详情|不能以demo填充、mock或旧投影假成功|
|E5 runtime/设备闭环|连接、注册、路由、遥测/脚本、控制投递/ACK、配置采用|ACK不等于物理效果；模拟器不替所有真实协议设备|
|E6 迁移/发行|源版本矩阵、全量对账、故障恢复、新装/升级和固定制品|不能从历史QA副本报告自动继承到当前发布|

所有报告记录B/K/F SHA、脏文件fingerprint（只允许已归属候选）、OpenAPI/client digest、数据库schema checksum、Go/TS/Encore/PG版本、runtime/provider版本、fixture版本、时间和requestId。未执行/blocked与失败分别记录；不得将skip、继续执行或旧版本通过记作本版成功。

## 2. 固定测试数据

两个独立租户A/B，同名设备编号和同名模板/组各一份；SYS_ADMIN（有/无租户委派）、TENANT_ADMIN，以及正式身份owner签发的只读/受限设备scope测试上下文和最小scope服务主体各一份。后两者是能力测试，不新建Community用户角色；TENANT_USER/TENANT_VIEWER残留不代表可登录角色。引导注册凭据与单次注册statusToken独立于管理token。测试token由真实测试authn签发，不用可伪造的tenant header。

A租户准备：普通设备D1、D2；无模板服务设备S1；无模型预登记设备P1；网关G1→G2→子设备C1；组R→R1→R11，D1同时属于R1与另一个根组；一条同名TELEMETRY与ATTRIBUTE标识；模型M发布v1/v2，模板T发布r1/r2；产品指向默认模板建议版本；两种不同provider实例拥有相同外部设备号。各关系通过正式API创建；真实PG专用约束测试另用直接SQL验证数据库防线。

历史源fixture同时包含：NULL与字符串0根组、孤儿/环/跨tenant引用、合法无模板/无模型、旧图表数组与不支持控件、模板secret/设备凭据、超深树、重复/超长编号、缺图片、服务外部号、旧online_timeout分钟/heartbeat秒、典型Lua六阶段、Topic同名异kind字段。**异常fixture须导致明确阻断，不自动修剪成“迁移成功”。**

性能代表集：50,000设备、1,000组、深度至32、125条跨页模型、20,000行用户导入；归档CPU/内存/磁盘、PG参数和数据分布。list p95≤500ms、detail≤300ms是拟定工程目标，需在G0环境冻结后测量，不是当前性能结论。

## 3. 每个API均要套用的公共矩阵

`CT-{operationId}-{case}` 是公共用例ID。不存在相应参数/副作用的端点标N/A并写理由，不能省略授权检查。完整适用关系由OpenAPI生成，见本文末操作矩阵。

|case|给定/动作|可观察断言|
|---|---|---|
|OK|合法最小请求、完整可选字段、空结果/无模型合法对象|成功状态/媒体类型/required/null/枚举精确通过同版schema；API/DB读回一致|
|AUTH|缺、伪造、过期、撤销、错误audience/issuer token|401；没有业务写/敏感数据；管理员未委派不能隐式跨租户|
|PERM|有读权限无写/secret/control/export/simulate权限|403；隐藏按钮构造同请求仍拒绝；scope不从组成员自动扩大|
|TENANT|A主体请求B的ID或把tenantId塞body/query/header|404或输入422/400；B无变化；total/计数/下载/日志不泄漏B|
|RESOURCE|A主体只可见D1，却请求D2或批内混D2|按合同拒绝；筛选/total/导出/相关组计数同一授权范围|
|SCHEMA|unknown字段、错误类型、缺required、nullable/null、0/false/空数组、超长|400/422稳定code和JSON Pointer；不静默转字符串/截断/丢字段|
|PAGE|page0、pageSize101、未知sort、125条跨页、同时间排序|非法拒绝；无重复/漏页；items/total同授权读快照；导出固定窗口|
|IDEM|同key同参并发50次；提交后丢响应并重启；同key异参|一份资源/效果；相同结果；异参409；过期key不作为新请求重执行|
|CAS|两个客户端同ETag写；缺If-Match；body revision缺失/陈旧|资源前置428/412；body revision422/409；只有一个合法更新|
|REF|新增引用与删除同时提交；不可见引用；依赖服务不可用|无悬挂活动引用；409脱敏类别；依赖不明不完成删除|
|ASYNC|202后断线/重启/轮询、部分失败、run/items变化|Location/ID可追；pending不显示完成；成功与实际runtime/数据一致|
|SECRET|列表/导出/错误/审计/trace与幂等重放|无secret；受控揭示单独授权；撤权后不能读旧秘密响应|
|NATIVE|关闭旧API并移除旧metadata表读写权限后重跑|仍通过；网络和依赖扫描无旧Facade/Gin/legacy resolver/fallback|
|FAIL|DB提交前后、outbox发送前后、runtime故障、限流|无半事务；可恢复且有界重试；503/unknown/pending真实，不假0/成功|

所有进入业务handler的错误均检查application/problem+json、HTTP=status、code/requestId；Encore auth handler在进入业务服务前拒绝凭据时，按`EncoreAuthError`校验原生application/json与401状态，并单独核对Bearer challenge不泄漏token。成功JSON/data/meta和二进制/SSE的边界都由实际handler合同测试覆盖。

## 4. 分层实现与验收范围

|层级|独立任务|核心正常/失败/边界场景|通过门槛|
|---|---|---|---|
|纯领域单元|DM-API-007、DEV/TPL/GRP业务任务|类型化字段/COMMAND、同名异kind、空模型、配置校验、树环、member去重、编号规则、null/false|复用核心测试并补真实缺口；不为低风险纯文案写镜像实现测试|
|真实PG|DM-QA-002、DM-DB-001—012|tenant复合FK、空库/升级库、不可变snapshot、发布竞争、成员批事务、改号预留、删除fence、凭据加密/密钥引用|直接SQL反例均被拒；中断前后无半状态；schema checksum一致|
|HTTP合同|DM-QA-001/003|全部108操作（用户103、内部服务5）的成功/错误schema、角色、参数、幂等/CAS/文件/SSE、物模型新transport|实际Encore handler及生成客户端通过；不只测试mock DTO|
|前端|DM-QA-004、DM-FE-001—039|逐139库存行；按钮/字段/筛选/弹窗/跨页选择/竞态/空态/错误/敏感操作/模型快照|每个可达功能有HAR+断言+API读回；无旧URL/WS；demo不得替真实结果|
|CLI|DM-QA-005、DM-CLI-001—018|104操作映射、47命令场景、stdout/stderr/退出码、token、--all/--wait、CAS、secret、SSE SIGINT|与同主体HTTP相同结果；无SQL/旧代理/CLI平行业务|
|runtime|DM-QA-006、DM-RT-001—019|注册/凭据、上报/脚本/Topic、多级网关、切版、状态/诊断/调试、命令/延期、模拟器/订阅|两设备与两租户隔离；desired/applied准确；真实投递和ACK分层记录|
|消费者|DM-QA-008|市场/ThingsVis/看板/移动端/OTA/后台服务身份（自动化告警后置）；字段/命令/图表固定版本|逐消费者销项；市场无真实上游则blocked，不用mock宣称完成|
|迁移/发行|DM-QA-007/010、DM-MIG-001—014|备份恢复、源预检、dry-run、分批kill/replay、永久映射、数量/内容/关系全量对账、新装/升级|每源对象有目标/批准处置；未决阻断0；失败恢复不丢遥测/原生新写|

## 5. 可直接实现的关键端到端场景

### E2E-01 三菜单和固定模型版本

1. A管理员通过createThingModel创建M、含四类字段及typed参数；publishThingModel发布v1；读取getThingModelVersion记snapshotId与两个摘要。
2. 创建接入模板T草稿，选择M/v1，配置协议、Topic、Lua、扩展资料；发布r1。通过产品选择器只给建议，不自动替换显式模板。
3. 创建设备D1/D2并完成凭据、激活，绑定T/r1；添加多级分组/成员；由UI和CLI分别读取相同设备/模板版本/分组。
4. 发布M/v2与T/r2；未切版的D1/D2仍读v1/r1的字段、COMMAND、图表和配置。预览D2切版并提交，只在runtime确认新generation后成功。
5. 租户B或仅D1 scope不得读/写D2；组统计不泄漏D2。执行时旧metadata表权限关闭。失败任何一步不标整版通过。

### E2E-02 凭据/注册/网关

模板autoRegister开启，生成引导凭据；设备注册只从该凭据得tenant/template，body任意tenant拒绝。异步注册返回单job statusToken，正确job可读，其他job/metadata secret拒绝。重复并发注册不多建/不泄漏既有设备凭据。

G1→G2→C1多级绑定与批量解绑保持ID和历史；A→后代成环、同父重复地址、跨租户绑定均拒绝。轮换先创建pending凭据再引用轮换；runtime确认后旧凭据在批准窗口失效；停用/删除不只改数据库标记。

### E2E-03 联合查询与真实页面

125条设备/模型，混合group/ungrouped、服务实例、固件、告警、online和scope。相同过滤下列表total、统计、导出同范围；分页前完成外部条件求交；runtime不可用明确错误/unknown。快速切D1→D2、A请求慢B快时，详情、图表、debug和SSE只显示最后选择对象。记录HAR，不能由代理配置或测试mock证明页面切换。

### E2E-04 模板高级配置与导入导出

分别操作Topic CRUD/启停（含同identifier不同kind）、六阶段Lua编辑/模拟/启停、资料字段string/number/boolean/受支持enum、online timeout/heartbeat、图片与注册凭据。草稿改动不影响已绑定版本；非法provider参数/越租户topic/无限循环脚本明确拒绝。

导出T/r1为包，包含modelPackage与资源/图片摘要，绝不含secret。向B导入时创建新本地model/template IDs、通过账本映射，不写A身份；包hash和资源hash可校验，新语义hash允许因身份/revision变化而不同。旧图表数组或未知控件不能置空后声称成功。

### E2E-05 运行详情与控制

上报两次完全相同observedAt但不同sampleId/cacheVersion；旧cacheVersion删除必须409，新值保留，历史不变。即时/when_online COMMAND按已应用snapshot验证；过期不投递；取消pending成功、投递后取消409；ACK与物理效果分别显示。

正式模拟器保存配置、初始化session、有限发送、读取run结果；目标必须来自授权runtime配置，拒任意server/port；消息只进入该设备观察链。SSE断线恢复event去重，cursor过期resync，撤权立即断流。历史导出125条跨页记录结果125条，窗口/聚合/UTC一致，撤权或过期下载拒绝。

补充旧详情等价验收：同桶[9,2,7]的sum18/diff7/avg6，7d/30d跨2月仍为固定秒数，查询/导出逐桶一致、空桶不补0；129字符合法模型identifier从发布到Topic/读数/COMMAND不截断。online/offline/unknown筛选后的total与各页同范围；带label和参数提交后重启刷新可读，label中%/_按字面过滤，敏感参数有JSON Pointer脱敏标记。诊断窗口成功2/总4返回0.5，零分母为null，统计覆盖不全为unknown，最近失败数量/倒序/truncated准确。

### E2E-06 一次性迁移与故障恢复

备份校验及实际restore→源preflight→dry-run清单→持久run/id_map/checkpoint→物模型/资源→接入模板/版本→产品/设备/凭据/runtime identity→网关/组/成员→外部引用。每批在API提交前后、checkpoint前后kill进程并重跑；与一次成功运行数量/内容/关系等价。

最终按tenant/entity执行 `source = migrated/reused + other_owner + approved_exclude + blocking_unresolved` 互斥分类对账；合法无模板/无模型是migrated/reused子集，不重复相加。关系单独核对映射/合法原始无边/批准移除/阻断；ready时blocking_unresolved=0。异常/悬挂/未知映射必须0或批准处置，不能只核总数。冻结所有metadata写入口后再取最终源一致快照/应用/对账；旧运行入口和表权限关闭仍通过E2E。切换前失败保持冻结并恢复；切换后优先前向修复，任何恢复不得抹掉新接受的原生写入或telemetry。

## 6. 发布门禁矩阵

|门禁|必须齐全的交付与证据|阻断判定|本轮状态|
|---|---|---|---|
|G0 基线/术语/合同|候选最小提交集；词汇/owner；OAS工具验证/client编译；3项待确认业务决策 + 已确认企业版排除|混合无关改动、未定边界、生成类型失真|规划完成；实现/签收待做|
|G1 公共/数据库|可信身份/tenant/scopes；schema/复合FK/CAS/幂等/删除fence；复用1.3已合并物模型|任何越权、碰撞、悬挂、不可恢复副作用|未验收|
|G2 三领域/运行时|社区版设备、模板、分组核心用例及相关runtime通过；DM-RT-008/009后置|任一关键用例或正式依赖未就绪|未验收|
|G3 UI/CLI/消费者|社区版本阶段前端与18CLI任务103op逐项验收；外部自动化告警留专项对接|旧请求、CLI代理、按钮/API不一致、demo假数据|未切换/未验收|
|G4 数据迁移|备份可恢复、preflight/dry-run、持久映射、全部故障点续跑、全量对账|源异常未处置、遗漏关系、身份丢失、数量/内容不符|未执行|
|G5 发行/容量/恢复|空库新装、所有支持源版本升级、同版制品、容量/故障、前向恢复|仅QA副本脚本、历史报告或版本不匹配|未执行|
|G6 正式切换批准条件|G0—G5全部通过；维护窗/冻结/最终对账ready；唯一新入口；旧依赖零|不能以95%通过放行关键越权/丢数；任一阻断不切|本轮不执行部署或迁移|

**完成不是百分比平均值。** 发布阻断用例必须全部通过；性能工程目标未达到也需具体决策与复测，不能藏在总通过率。实现者只更新有日志/制品证据的对应任务及五条证据轴，不把测试源码存在勾成“有测试通过证据”。

## 7. 本轮文档验证与后续执行边界

本轮可检查：188任务ID/依赖/发布闭包、108operationId/path/任务/权限/schema引用、103项CLI与5项受限服务映射、580场景追踪、文档相互链接、Git既存文件fingerprint与本目录内有锚点修订/新增文件边界。2026-10-25已使用官方OAS3.2 schema-base/2026-08-30与jsonschema执行文档结构/Schema Object校验，结果见VALIDATION-2026-10-25；Go与TS生成器实际编译、真实handler契约仍作为DM-API-001/009门禁保留。没有运行产品测试或数据库，因此本表不得出现虚构“passed”。

<!-- GENERATED ACCEPTANCE OPERATIONS -->

## 8. 全部正式操作的验收追踪矩阵

每行给出主责任任务及其精确TC ID。公共矩阵的AUTH/PERM/TENANT/RESOURCE/SCHEMA/NATIVE/FAIL适用于全部操作；下列附加族依据正式参数和响应列出，具体边界仍以对应TC和schema为准。所有状态为待执行；本表不表示HTTP调用或测试已经通过。

|operationId / owner|主责任任务 / 逐任务场景|附加公共用例族|目标合同|
|---|---|---|---|
|`listDevices` / devicemetadata|DM-DEV-001；TC-DM-DEV-001-01, TC-DM-DEV-001-02, TC-DM-DEV-001-03|OK/SECRET/PAGE|`GET /api/v2/devices`；参数 → DevicePageResponse|
|`getDevice` / devicemetadata|DM-DEV-002；TC-DM-DEV-002-01, TC-DM-DEV-002-02, TC-DM-DEV-002-03|OK/SECRET|`GET /api/v2/devices/{deviceId}`；参数 → DeviceResponse|
|`createDevice` / devicemetadata|DM-DEV-003；TC-DM-DEV-003-01, TC-DM-DEV-003-02, TC-DM-DEV-003-03|OK/SECRET/IDEM|`POST /api/v2/devices`；CreateDevice → DeviceResponse|
|`updateDevice` / devicemetadata|DM-DEV-004；TC-DM-DEV-004-01, TC-DM-DEV-004-02, TC-DM-DEV-004-03|OK/SECRET/IDEM/CAS|`PATCH /api/v2/devices/{deviceId}`；UpdateDevice → DeviceResponse|
|`deleteDevice` / devicemetadata|DM-DEV-005；TC-DM-DEV-005-01, TC-DM-DEV-005-02, TC-DM-DEV-005-03|OK/SECRET/IDEM/CAS/REF/ASYNC|`DELETE /api/v2/devices/{deviceId}`；参数 → OperationResponse|
|`getDeviceAdministrativeState` / devicemetadata|DM-DEV-006；TC-DM-DEV-006-01, TC-DM-DEV-006-02, TC-DM-DEV-006-03|OK/SECRET|`GET /api/v2/devices/{deviceId}/administrative-state`；参数 → AdministrativeStateResponse|
|`setDeviceAdministrativeState` / devicemetadata|DM-DEV-006；TC-DM-DEV-006-01, TC-DM-DEV-006-02, TC-DM-DEV-006-03|OK/SECRET/IDEM/CAS/ASYNC|`PUT /api/v2/devices/{deviceId}/administrative-state`；SetAdministrativeState → OperationResponse|
|`activateDevice` / devicemetadata|DM-DEV-007；TC-DM-DEV-007-01, TC-DM-DEV-007-02, TC-DM-DEV-007-03|OK/SECRET/IDEM/CAS/ASYNC|`POST /api/v2/devices/{deviceId}/activations`；ActivateDevice → OperationResponse|
|`checkDeviceNumber` / devicemetadata|DM-DEV-008；TC-DM-DEV-008-01, TC-DM-DEV-008-02, TC-DM-DEV-008-03|OK/SECRET|`POST /api/v2/device-number-validations`；DeviceNumberCheck → DeviceNumberAvailabilityResponse|
|`listDeviceCredentials` / devicemetadata|DM-DEV-009；TC-DM-DEV-009-01, TC-DM-DEV-009-02, TC-DM-DEV-009-03|OK/SECRET/PAGE|`GET /api/v2/devices/{deviceId}/credentials`；参数 → CredentialSummaryPageResponse|
|`createDeviceCredential` / devicemetadata|DM-DEV-009；TC-DM-DEV-009-01, TC-DM-DEV-009-02, TC-DM-DEV-009-03|OK/SECRET/IDEM|`POST /api/v2/devices/{deviceId}/credentials`；CreateCredential → CredentialSecretResponse|
|`rotateDeviceCredential` / devicemetadata|DM-DEV-010；TC-DM-DEV-010-01, TC-DM-DEV-010-02, TC-DM-DEV-010-03|OK/SECRET/IDEM/CAS/ASYNC|`POST /api/v2/devices/{deviceId}/credentials/{credentialId}/rotations`；RotateCredential → OperationResponse|
|`revokeDeviceCredential` / devicemetadata|DM-DEV-010；TC-DM-DEV-010-01, TC-DM-DEV-010-02, TC-DM-DEV-010-03|OK/SECRET/IDEM/CAS/ASYNC|`POST /api/v2/devices/{deviceId}/credentials/{credentialId}/revocations`；RevokeCredential → OperationResponse|
|`revealDeviceCredential` / devicemetadata|DM-DEV-011；TC-DM-DEV-011-01, TC-DM-DEV-011-02, TC-DM-DEV-011-03|OK/SECRET/IDEM|`POST /api/v2/devices/{deviceId}/credentials/{credentialId}/reveals`；CredentialRevealRequest → CredentialSecretResponse|
|`batchCreateDevices` / devicemetadata|DM-DEV-012；TC-DM-DEV-012-01, TC-DM-DEV-012-02, TC-DM-DEV-012-03|OK/SECRET/IDEM/ASYNC|`POST /api/v2/device-batches`；BatchDeviceCreate → OperationResponse|
|`validateDeviceImport` / devicemetadata|DM-DEV-013；TC-DM-DEV-013-01, TC-DM-DEV-013-02, TC-DM-DEV-013-03|OK/SECRET/IDEM/REF/ASYNC|`POST /api/v2/device-imports`；ImportUpload → ImportValidationResponse|
|`getDeviceImport` / devicemetadata|DM-DEV-013；TC-DM-DEV-013-01, TC-DM-DEV-013-02, TC-DM-DEV-013-03|OK/SECRET/REF|`GET /api/v2/device-imports/{importId}`；参数 → ImportValidationResponse|
|`commitDeviceImport` / devicemetadata|DM-DEV-014；TC-DM-DEV-014-01, TC-DM-DEV-014-02, TC-DM-DEV-014-03|OK/SECRET/IDEM/REF/ASYNC|`POST /api/v2/device-imports/{importId}/commits`；ImportCommit → OperationResponse|
|`downloadDeviceImportFormat` / devicemetadata|DM-DEV-013；TC-DM-DEV-013-01, TC-DM-DEV-013-02, TC-DM-DEV-013-03|OK/SECRET/REF|`GET /api/v2/device-import-format`；参数 → binary (see response content types)|
|`exportDevices` / devicemetadata|DM-DEV-015；TC-DM-DEV-015-01, TC-DM-DEV-015-02, TC-DM-DEV-015-03|OK/SECRET/IDEM/ASYNC|`POST /api/v2/device-exports`；DeviceExport → OperationResponse|
|`getDeviceTemplateBinding` / devicemetadata|DM-DEV-016；TC-DM-DEV-016-01, TC-DM-DEV-016-02, TC-DM-DEV-016-03|OK/SECRET/REF|`GET /api/v2/devices/{deviceId}/template-binding`；参数 → DeviceTemplateBindingResponse|
|`previewDeviceTemplateChange` / devicemetadata|DM-DEV-016；TC-DM-DEV-016-01, TC-DM-DEV-016-02, TC-DM-DEV-016-03|OK/SECRET|`POST /api/v2/devices/{deviceId}/template-change-previews`；TemplateChangeRequest → TemplateChangePreviewResponse|
|`setDeviceTemplateBinding` / devicemetadata|DM-DEV-017；TC-DM-DEV-017-01, TC-DM-DEV-017-02, TC-DM-DEV-017-03|OK/SECRET/IDEM/CAS/REF/ASYNC|`PUT /api/v2/devices/{deviceId}/template-binding`；SetTemplateBinding → OperationResponse|
|`getGatewayMembership` / devicemetadata|DM-DEV-018；TC-DM-DEV-018-01, TC-DM-DEV-018-02, TC-DM-DEV-018-03, TC-DM-DEV-018-04, TC-DM-DEV-018-05|OK/SECRET/REF|`GET /api/v2/devices/{deviceId}/gateway-membership`；参数 → GatewayMembershipResponse|
|`setGatewayMembership` / devicemetadata|DM-DEV-018；TC-DM-DEV-018-01, TC-DM-DEV-018-02, TC-DM-DEV-018-03, TC-DM-DEV-018-04, TC-DM-DEV-018-05|OK/SECRET/IDEM/CAS/REF/ASYNC|`PUT /api/v2/devices/{deviceId}/gateway-membership`；SetGatewayMembership → OperationResponse|
|`listSubdevices` / devicemetadata|DM-DEV-019；TC-DM-DEV-019-01, TC-DM-DEV-019-02, TC-DM-DEV-019-03|OK/SECRET/PAGE|`GET /api/v2/devices/{deviceId}/subdevices`；参数 → DevicePageResponse|
|`getDeviceStatistics` / devicemetadata|DM-DEV-020；TC-DM-DEV-020-01, TC-DM-DEV-020-02, TC-DM-DEV-020-03|OK/SECRET|`GET /api/v2/device-statistics`；参数 → DeviceStatisticsResponse|
|`listDeviceAccessTemplates` / devicemetadata|DM-TPL-001；TC-DM-TPL-001-01, TC-DM-TPL-001-02, TC-DM-TPL-001-03|OK/SECRET/PAGE|`GET /api/v2/device-access-templates`；参数 → DeviceAccessTemplatePageResponse|
|`getDeviceAccessTemplate` / devicemetadata|DM-TPL-002；TC-DM-TPL-002-01, TC-DM-TPL-002-02, TC-DM-TPL-002-03|OK/SECRET|`GET /api/v2/device-access-templates/{templateId}`；参数 → DeviceAccessTemplateResponse|
|`createDeviceAccessTemplate` / devicemetadata|DM-TPL-003；TC-DM-TPL-003-01, TC-DM-TPL-003-02, TC-DM-TPL-003-03|OK/SECRET/IDEM|`POST /api/v2/device-access-templates`；CreateDeviceAccessTemplate → DeviceAccessTemplateResponse|
|`updateDeviceAccessTemplate` / devicemetadata|DM-TPL-004；TC-DM-TPL-004-01, TC-DM-TPL-004-02, TC-DM-TPL-004-03|OK/SECRET/IDEM/CAS|`PATCH /api/v2/device-access-templates/{templateId}`；UpdateDeviceAccessTemplate → DeviceAccessTemplateResponse|
|`publishDeviceAccessTemplate` / devicemetadata|DM-TPL-005；TC-DM-TPL-005-01, TC-DM-TPL-005-02, TC-DM-TPL-005-03|OK/SECRET/IDEM/CAS/REF|`POST /api/v2/device-access-templates/{templateId}/publications`；TemplatePublication → DeviceAccessTemplateRevisionResponse|
|`listDeviceAccessTemplateRevisions` / devicemetadata|DM-TPL-006；TC-DM-TPL-006-01, TC-DM-TPL-006-02, TC-DM-TPL-006-03|OK/SECRET/PAGE|`GET /api/v2/device-access-templates/{templateId}/revisions`；参数 → DeviceAccessTemplateRevisionPageResponse|
|`getDeviceAccessTemplateRevision` / devicemetadata|DM-TPL-006；TC-DM-TPL-006-01, TC-DM-TPL-006-02, TC-DM-TPL-006-03|OK/SECRET|`GET /api/v2/device-access-templates/{templateId}/revisions/{revision}`；参数 → DeviceAccessTemplateRevisionResponse|
|`validateDeviceAccessTemplate` / devicemetadata|DM-TPL-007；TC-DM-TPL-007-01, TC-DM-TPL-007-02, TC-DM-TPL-007-03|OK/SECRET|`POST /api/v2/device-access-templates/{templateId}/validations`；TemplateDraft → ValidationReportResponse|
|`deleteDeviceAccessTemplate` / devicemetadata|DM-TPL-009；TC-DM-TPL-009-01, TC-DM-TPL-009-02, TC-DM-TPL-009-03|OK/SECRET/IDEM/CAS/REF|`DELETE /api/v2/device-access-templates/{templateId}`；参数 → DeviceAccessTemplateResponse|
|`listDeviceAccessTemplateReferences` / devicemetadata|DM-TPL-010；TC-DM-TPL-010-01, TC-DM-TPL-010-02, TC-DM-TPL-010-03|OK/SECRET/PAGE|`GET /api/v2/device-access-templates/{templateId}/references`；参数 → TemplateReferencePageResponse|
|`copyDeviceAccessTemplate` / devicemetadata|DM-TPL-011；TC-DM-TPL-011-01, TC-DM-TPL-011-02, TC-DM-TPL-011-03|OK/SECRET/IDEM|`POST /api/v2/device-access-templates/{templateId}/copies`；CloneTemplate → DeviceAccessTemplateResponse|
|`exportDeviceAccessTemplate` / devicemetadata|DM-TPL-012；TC-DM-TPL-012-01, TC-DM-TPL-012-02, TC-DM-TPL-012-03|OK/SECRET|`GET /api/v2/device-access-templates/{templateId}/revisions/{revision}/export`；参数 → TemplatePackageResponse|
|`importDeviceAccessTemplate` / devicemetadata|DM-TPL-013；TC-DM-TPL-013-01, TC-DM-TPL-013-02, TC-DM-TPL-013-03|OK/SECRET/IDEM/REF/ASYNC|`POST /api/v2/device-access-template-imports`；TemplatePackage → OperationResponse|
|`listProducts` / devicemetadata|DM-TPL-014；TC-DM-TPL-014-01, TC-DM-TPL-014-02, TC-DM-TPL-014-03|OK/SECRET/PAGE|`GET /api/v2/products`；参数 → ProductPageResponse|
|`getProduct` / devicemetadata|DM-TPL-014；TC-DM-TPL-014-01, TC-DM-TPL-014-02, TC-DM-TPL-014-03|OK/SECRET|`GET /api/v2/products/{productId}`；参数 → ProductResponse|
|`listConnectionProviders` / connector-runtime|DM-TPL-015；TC-DM-TPL-015-01, TC-DM-TPL-015-02, TC-DM-TPL-015-03|OK/SECRET/PAGE|`GET /api/v2/connection-providers`；参数 → ConnectionProviderPageResponse|
|`listDeviceGroups` / devicemetadata|DM-GRP-001；TC-DM-GRP-001-01, TC-DM-GRP-001-02, TC-DM-GRP-001-03|OK/SECRET/PAGE|`GET /api/v2/device-groups`；参数 → DeviceGroupPageResponse|
|`getDeviceGroup` / devicemetadata|DM-GRP-001；TC-DM-GRP-001-01, TC-DM-GRP-001-02, TC-DM-GRP-001-03|OK/SECRET|`GET /api/v2/device-groups/{groupId}`；参数 → DeviceGroupResponse|
|`getDeviceGroupTree` / devicemetadata|DM-GRP-002；TC-DM-GRP-002-01, TC-DM-GRP-002-02, TC-DM-GRP-002-03, TC-DM-GRP-002-04|OK/SECRET|`GET /api/v2/device-group-tree`；参数 → GroupTreeResponse|
|`createDeviceGroup` / devicemetadata|DM-GRP-003；TC-DM-GRP-003-01, TC-DM-GRP-003-02, TC-DM-GRP-003-03|OK/SECRET/IDEM|`POST /api/v2/device-groups`；CreateDeviceGroup → DeviceGroupResponse|
|`updateDeviceGroup` / devicemetadata|DM-GRP-004；TC-DM-GRP-004-01, TC-DM-GRP-004-02, TC-DM-GRP-004-03|OK/SECRET/IDEM/CAS|`PATCH /api/v2/device-groups/{groupId}`；UpdateDeviceGroup → DeviceGroupResponse|
|`moveDeviceGroup` / devicemetadata|DM-GRP-005；TC-DM-GRP-005-01, TC-DM-GRP-005-02, TC-DM-GRP-005-03, TC-DM-GRP-005-04|OK/SECRET/IDEM/CAS/REF|`POST /api/v2/device-groups/{groupId}/moves`；MoveDeviceGroup → DeviceGroupResponse|
|`deleteDeviceGroup` / devicemetadata|DM-GRP-006；TC-DM-GRP-006-01, TC-DM-GRP-006-02, TC-DM-GRP-006-03|OK/SECRET/IDEM/CAS/REF|`DELETE /api/v2/device-groups/{groupId}`；参数 → DeviceGroupResponse|
|`listDeviceGroupMembers` / devicemetadata|DM-GRP-007；TC-DM-GRP-007-01, TC-DM-GRP-007-02, TC-DM-GRP-007-03|OK/SECRET/PAGE|`GET /api/v2/device-groups/{groupId}/members`；参数 → GroupMemberPageResponse|
|`addDeviceGroupMembers` / devicemetadata|DM-GRP-008；TC-DM-GRP-008-01, TC-DM-GRP-008-02, TC-DM-GRP-008-03|OK/SECRET/IDEM/CAS|`POST /api/v2/device-groups/{groupId}/member-additions`；SetGroupMembers → DeviceGroupResponse|
|`removeDeviceGroupMembers` / devicemetadata|DM-GRP-009；TC-DM-GRP-009-01, TC-DM-GRP-009-02, TC-DM-GRP-009-03|OK/SECRET/IDEM/CAS/REF|`POST /api/v2/device-groups/{groupId}/member-removals`；SetGroupMembers → DeviceGroupResponse|
|`listGroupsForDevice` / devicemetadata|DM-GRP-010；TC-DM-GRP-010-01, TC-DM-GRP-010-02, TC-DM-GRP-010-03|OK/SECRET/PAGE|`GET /api/v2/devices/{deviceId}/groups`；参数 → DeviceGroupPageResponse|
|`getMetadataOperation` / devicemetadata|DM-API-008；TC-DM-API-008-01, TC-DM-API-008-02, TC-DM-API-008-03|OK/SECRET/ASYNC|`GET /api/v2/metadata-operations/{operationId}`；参数 → OperationResponse|
|`listMetadataOperationItems` / devicemetadata|DM-API-008；TC-DM-API-008-01, TC-DM-API-008-02, TC-DM-API-008-03|OK/SECRET/PAGE/ASYNC|`GET /api/v2/metadata-operations/{operationId}/items`；参数 → OperationItemPageResponse|
|`downloadMetadataArtifact` / devicemetadata|DM-API-008；TC-DM-API-008-01, TC-DM-API-008-02, TC-DM-API-008-03|OK/SECRET|`GET /api/v2/metadata-downloads/{downloadId}`；参数 → binary (see response content types)|
|`getDeviceRuntimeState` / device-runtime|DM-RT-002；TC-DM-RT-002-01, TC-DM-RT-002-02, TC-DM-RT-002-03|OK/SECRET|`GET /api/v2/devices/{deviceId}/runtime-state`；参数 → RuntimeStateResponse|
|`getDeviceConnectionInfo` / device-runtime|DM-RT-003；TC-DM-RT-003-01, TC-DM-RT-003-02, TC-DM-RT-003-03|OK/SECRET|`GET /api/v2/devices/{deviceId}/connection-info`；参数 → ConnectionInfoResponse|
|`createDeviceDebugSession` / device-runtime|DM-RT-004；TC-DM-RT-004-01, TC-DM-RT-004-02, TC-DM-RT-004-03|OK/SECRET/IDEM|`POST /api/v2/devices/{deviceId}/debug-sessions`；DebugSessionRequest → DebugSessionResponse|
|`stopDeviceDebugSession` / device-runtime|DM-RT-004；TC-DM-RT-004-01, TC-DM-RT-004-02, TC-DM-RT-004-03|OK/SECRET/IDEM/REF|`DELETE /api/v2/devices/{deviceId}/debug-sessions/{sessionId}`；参数 → DebugSessionResponse|
|`listDeviceDebugLogs` / device-runtime|DM-RT-004；TC-DM-RT-004-01, TC-DM-RT-004-02, TC-DM-RT-004-03|OK/SECRET/PAGE|`GET /api/v2/devices/{deviceId}/debug-logs`；参数 → DebugLogPageResponse|
|`listDeviceReadings` / telemetry|DM-RT-005；TC-DM-RT-005-01, TC-DM-RT-005-02, TC-DM-RT-005-03, TC-DM-RT-005-04|OK/SECRET/PAGE|`GET /api/v2/devices/{deviceId}/readings`；参数 → ReadingPageResponse|
|`submitDeviceCommand` / control|DM-RT-006；TC-DM-RT-006-01, TC-DM-RT-006-02, TC-DM-RT-006-03|OK/SECRET/IDEM/ASYNC|`POST /api/v2/devices/{deviceId}/command-executions`；CommandSubmission → CommandReceiptResponse|
|`getDeviceCommandExecution` / control|DM-RT-006；TC-DM-RT-006-01, TC-DM-RT-006-02, TC-DM-RT-006-03|OK/SECRET|`GET /api/v2/devices/{deviceId}/command-executions/{executionId}`；参数 → CommandReceiptResponse|
|`queryDeviceRuntimeStates` / device-runtime|DM-RT-002；TC-DM-RT-002-01, TC-DM-RT-002-02, TC-DM-RT-002-03|OK/SECRET|`POST /api/v2/device-runtime-state-queries`；RuntimeStateQuery → RuntimeStateBatchResponse|
|`changeDeviceNumber` / devicemetadata|DM-DEV-021；TC-DM-DEV-021-01, TC-DM-DEV-021-02, TC-DM-DEV-021-03|OK/SECRET/IDEM/CAS/ASYNC|`POST /api/v2/devices/{deviceId}/number-changes`；RenumberDevice → OperationResponse|
|`batchSetDeviceTemplateBindings` / devicemetadata|DM-DEV-023；TC-DM-DEV-023-01, TC-DM-DEV-023-02, TC-DM-DEV-023-03|OK/SECRET/IDEM/REF/ASYNC|`POST /api/v2/device-template-binding-batches`；BatchTemplateBinding → OperationResponse|
|`testProcessingScript` / connector-runtime|DM-TPL-018；TC-DM-TPL-018-01, TC-DM-TPL-018-02, TC-DM-TPL-018-03|OK/SECRET/IDEM|`POST /api/v2/processing-script-tests`；ProcessingScriptTest → ProcessingScriptTestResultResponse|
|`listTemplateEnrollmentCredentials` / devicemetadata|DM-TPL-020；TC-DM-TPL-020-01, TC-DM-TPL-020-02, TC-DM-TPL-020-03|OK/SECRET/PAGE|`GET /api/v2/device-access-templates/{templateId}/enrollment-credentials`；参数 → TemplateEnrollmentCredentialPageResponse|
|`createTemplateEnrollmentCredential` / devicemetadata|DM-TPL-020；TC-DM-TPL-020-01, TC-DM-TPL-020-02, TC-DM-TPL-020-03|OK/SECRET/IDEM/CAS|`POST /api/v2/device-access-templates/{templateId}/enrollment-credentials`；CreateTemplateEnrollmentCredential → TemplateEnrollmentSecretResponse|
|`revokeTemplateEnrollmentCredential` / devicemetadata|DM-TPL-020；TC-DM-TPL-020-01, TC-DM-TPL-020-02, TC-DM-TPL-020-03|OK/SECRET/IDEM/REF|`DELETE /api/v2/device-access-templates/{templateId}/enrollment-credentials/{credentialId}`；参数 → TemplateEnrollmentCredentialResponse|
|`enrollDevice` / device-runtime|DM-RT-013；TC-DM-RT-013-01, TC-DM-RT-013-02, TC-DM-RT-013-03|OK/SECRET/IDEM|`POST /api/v2/device-enrollments`；DeviceEnrollment → DeviceEnrollmentResultResponse|
|`batchSetGatewayMemberships` / devicemetadata|DM-DEV-027；TC-DM-DEV-027-01, TC-DM-DEV-027-02, TC-DM-DEV-027-03|OK/SECRET/IDEM/REF/ASYNC|`POST /api/v2/gateway-membership-batches`；GatewayMembershipBatch → OperationResponse|
|`listDeviceCommandExecutions` / control|DM-RT-014；TC-DM-RT-014-01, TC-DM-RT-014-02, TC-DM-RT-014-03, TC-DM-RT-014-04|OK/SECRET/PAGE|`GET /api/v2/devices/{deviceId}/command-executions`；参数 → CommandReceiptPageResponse|
|`cancelDeviceCommandExecution` / control|DM-RT-014；TC-DM-RT-014-01, TC-DM-RT-014-02, TC-DM-RT-014-03, TC-DM-RT-014-04|OK/SECRET/IDEM|`POST /api/v2/devices/{deviceId}/command-executions/{executionId}/cancellations`；CancelCommand → CommandReceiptResponse|
|`listDeviceStatusHistory` / device-runtime|DM-RT-015；TC-DM-RT-015-01, TC-DM-RT-015-02, TC-DM-RT-015-03, TC-DM-RT-015-04, TC-DM-RT-015-05|OK/SECRET/PAGE|`GET /api/v2/devices/{deviceId}/status-history`；参数 → DeviceStatusEventPageResponse|
|`getDeviceDiagnostics` / device-runtime|DM-RT-015；TC-DM-RT-015-01, TC-DM-RT-015-02, TC-DM-RT-015-03, TC-DM-RT-015-04, TC-DM-RT-015-05|OK/SECRET|`GET /api/v2/devices/{deviceId}/diagnostics`；参数 → DeviceDiagnosticResponse|
|`getDeviceSimulatorConfig` / device-runtime|DM-RT-015；TC-DM-RT-015-01, TC-DM-RT-015-02, TC-DM-RT-015-03, TC-DM-RT-015-04, TC-DM-RT-015-05|OK/SECRET|`GET /api/v2/devices/{deviceId}/simulator-config`；参数 → SimulatorConfigResponse|
|`deleteDeviceLatestReading` / telemetry|DM-RT-016；TC-DM-RT-016-01, TC-DM-RT-016-02, TC-DM-RT-016-03, TC-DM-RT-016-04|OK/SECRET/IDEM/CAS|`POST /api/v2/devices/{deviceId}/latest-reading-deletions`；DeleteLatestReading → LatestReadingDeletionResponse|
|`streamDeviceObservations` / device-runtime|DM-RT-017；TC-DM-RT-017-01, TC-DM-RT-017-02, TC-DM-RT-017-03|OK/SECRET|`GET /api/v2/device-observation-stream`；参数 → SSE ObservationEvent|
|`createFileAsset` / file-storage|DM-TPL-021；TC-DM-TPL-021-01, TC-DM-TPL-021-02, TC-DM-TPL-021-03|OK/SECRET/IDEM|`POST /api/v2/file-assets`；FileAssetUpload → FileAssetResponse|
|`getFileAsset` / file-storage|DM-TPL-021；TC-DM-TPL-021-01, TC-DM-TPL-021-02, TC-DM-TPL-021-03|OK/SECRET|`GET /api/v2/file-assets/{assetId}`；参数 → FileAssetResponse|
|`exportDeviceReadings` / telemetry|DM-RT-018；TC-DM-RT-018-01, TC-DM-RT-018-02, TC-DM-RT-018-03, TC-DM-RT-018-04|OK/SECRET/IDEM/ASYNC|`POST /api/v2/devices/{deviceId}/reading-exports`；ReadingExportRequest → OperationResponse|
|`getReadingExport` / telemetry|DM-RT-018；TC-DM-RT-018-01, TC-DM-RT-018-02, TC-DM-RT-018-03, TC-DM-RT-018-04|OK/SECRET/ASYNC|`GET /api/v2/reading-exports/{operationId}`；参数 → OperationResponse|
|`downloadReadingExport` / telemetry|DM-RT-018；TC-DM-RT-018-01, TC-DM-RT-018-02, TC-DM-RT-018-03, TC-DM-RT-018-04|OK/SECRET|`GET /api/v2/reading-exports/{operationId}/download`；参数 → CSV or NDJSON binary/text download|
|`getDeviceSimulatorProfile` / device-runtime|DM-RT-019；TC-DM-RT-019-01, TC-DM-RT-019-02, TC-DM-RT-019-03|OK/SECRET|`GET /api/v2/devices/{deviceId}/simulator-profile`；参数 → SimulatorProfileResponse|
|`updateDeviceSimulatorProfile` / device-runtime|DM-RT-019；TC-DM-RT-019-01, TC-DM-RT-019-02, TC-DM-RT-019-03|OK/SECRET/IDEM/CAS|`PUT /api/v2/devices/{deviceId}/simulator-profile`；UpdateSimulatorProfile → SimulatorProfileResponse|
|`createDeviceSimulationSession` / device-runtime|DM-RT-019；TC-DM-RT-019-01, TC-DM-RT-019-02, TC-DM-RT-019-03|OK/SECRET/IDEM|`POST /api/v2/devices/{deviceId}/simulator-sessions`；SimulationSessionRequest → SimulationSessionResponse|
|`closeDeviceSimulationSession` / device-runtime|DM-RT-019；TC-DM-RT-019-01, TC-DM-RT-019-02, TC-DM-RT-019-03|OK/SECRET/IDEM/REF|`DELETE /api/v2/devices/{deviceId}/simulator-sessions/{sessionId}`；参数 → SimulationSessionResponse|
|`runDeviceSimulation` / device-runtime|DM-RT-019；TC-DM-RT-019-01, TC-DM-RT-019-02, TC-DM-RT-019-03|OK/SECRET/IDEM/ASYNC|`POST /api/v2/devices/{deviceId}/simulation-runs`；SimulationRunRequest → SimulationRunResponse|
|`getDeviceSimulationRun` / device-runtime|DM-RT-019；TC-DM-RT-019-01, TC-DM-RT-019-02, TC-DM-RT-019-03|OK/SECRET/ASYNC|`GET /api/v2/devices/{deviceId}/simulation-runs/{runId}`；参数 → SimulationRunResponse|
|`getDeviceEnrollmentStatus` / device-runtime|DM-RT-013；TC-DM-RT-013-01, TC-DM-RT-013-02, TC-DM-RT-013-03|OK/SECRET/ASYNC|`GET /api/v2/device-enrollment-operations/{operationId}`；参数 → DeviceEnrollmentStatusResponse|
|`createThingModel` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET/IDEM|`POST /api/v2/thing-model-commands/create`；NativeThingModelCreate → NativeThingModelMutationResultResponse|
|`updateThingModel` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET/IDEM/CAS|`POST /api/v2/thing-model-commands/update`；NativeThingModelUpdate → NativeThingModelMutationResultResponse|
|`publishThingModel` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET/IDEM/CAS/REF|`POST /api/v2/thing-model-commands/publish`；NativeThingModelPublish → NativeThingModelMutationResultResponse|
|`listThingModels` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET/PAGE|`GET /api/v2/thing-models`；参数 → NativeThingModelSummaryPageResponse|
|`getThingModel` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET|`GET /api/v2/thing-models/{modelId}`；参数 → NativeThingModelDetailResponse|
|`listThingModelVersions` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET/PAGE|`GET /api/v2/thing-models/{modelId}/versions`；参数 → NativeThingModelVersionSummaryPageResponse|
|`getThingModelVersion` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET|`GET /api/v2/thing-models/{modelId}/versions/{publishedRevision}`；参数 → NativeThingModelSnapshotDetailResponse|
|`getThingModelResources` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET|`GET /api/v2/thing-models/{modelId}/resources`；参数 → NativeThingModelResourcesResponse|
|`updateThingModelResources` / thingmodel|DM-API-007；TC-DM-API-007-01, TC-DM-API-007-02, TC-DM-API-007-03, TC-DM-API-007-04|OK/SECRET/IDEM/CAS|`POST /api/v2/thing-model-commands/update-resources`；NativeThingModelUpdateResources → NativeThingModelMutationResultResponse|
|`reserveMetadataReference` / devicemetadata（仅service）|DM-API-011；TC-DM-API-011-01, TC-DM-API-011-02, TC-DM-API-011-03, TC-DM-API-011-04, TC-DM-API-011-05, TC-DM-API-011-06|OK/AUTH/PERM/TENANT/RESOURCE/IDEM/REF/FAIL|`POST /api/v2/internal/metadata-references`；ReserveMetadataReference → MetadataReferenceResponse|
|`getMetadataReference` / devicemetadata（仅service）|DM-API-011；TC-DM-API-011-01, TC-DM-API-011-02, TC-DM-API-011-03, TC-DM-API-011-04, TC-DM-API-011-05, TC-DM-API-011-06|OK/AUTH/PERM/TENANT/RESOURCE/IDEM/REF/FAIL|`GET /api/v2/internal/metadata-references/{referenceId}`；参数 → MetadataReferenceResponse|
|`confirmMetadataReference` / devicemetadata（仅service）|DM-API-011；TC-DM-API-011-01, TC-DM-API-011-02, TC-DM-API-011-03, TC-DM-API-011-04, TC-DM-API-011-05, TC-DM-API-011-06|OK/AUTH/PERM/TENANT/RESOURCE/IDEM/REF/FAIL|`POST /api/v2/internal/metadata-references/{referenceId}/confirmations`；MetadataReferenceCommand → MetadataReferenceResponse|
|`renewMetadataReference` / devicemetadata（仅service）|DM-API-011；TC-DM-API-011-01, TC-DM-API-011-02, TC-DM-API-011-03, TC-DM-API-011-04, TC-DM-API-011-05, TC-DM-API-011-06|OK/AUTH/PERM/TENANT/RESOURCE/IDEM/REF/FAIL|`POST /api/v2/internal/metadata-references/{referenceId}/renewals`；MetadataReferenceCommand → MetadataReferenceResponse|
|`releaseMetadataReference` / devicemetadata（仅service）|DM-API-011；TC-DM-API-011-01, TC-DM-API-011-02, TC-DM-API-011-03, TC-DM-API-011-04, TC-DM-API-011-05, TC-DM-API-011-06|OK/AUTH/PERM/TENANT/RESOURCE/IDEM/REF/FAIL|`POST /api/v2/internal/metadata-references/{referenceId}/releases`；ReleaseMetadataReference → MetadataReferenceResponse|

### 内部服务引用协议验收

5项internal-service操作使用与用户操作相同的身份、租户、幂等和错误矩阵，另执行TC-DM-API-011-01—06：来源owner隔离、租户伪造、不同源revision重叠保护、删除fence epoch变化、过期预留不自动放弃保护、跨owner提交前后崩溃恢复。CLI/MCP禁止暴露这5项；生成器覆盖检查分别断言用户104/104、服务5/5，不把后者当CLI遗漏。全部产品场景仍为planned_not_executed。

### 本轮新增发现的专门回归

|范围|任务/用例|可观察通过条件|
|---|---|---|
|外部服务候选发现|DM-RT-001-03、DM-CLI-017-01|pageSize=7同快照无重漏；正确绑定状态与total；过期410、越权403/404、上游503；并发登记不重复|
|实际认证链|TC-DM-API-002-02|保留已有签名校验，缺exp/错误iss/aud/撤销冻结拒绝，正式环境不能选择dev/passthrough|
|分组CLI字段|TC-DM-CLI-010-03|真实请求字段与OpenAPI逐项一致，筛选和pageSize真正改变结果|
|状态迁移|TC-DM-MIG-002-02、TC-DM-MIG-007-03|enable/enabled等原值分布有审计；未知不自动转换；四种状态不混合|
|配置事件/回执|TC-DM-RT-007-01—03|schema/JCS黄金字节、可信tenant、固定版本、缺号/冲突/重放、应用后回执丢失恢复；received不算applied|
|内部引用恢复|TC-DM-API-011-03—06|reserve响应丢失且跨24h仍能找到原边；未知源提交不误释放；fence→edge固定锁序|
|市场组合包和看板|TC-DM-RT-010-01—03、TC-DM-FE-018-03、TC-DM-QA-008-01|超过1000候选完整分页；重绑/重试/补偿可恢复，不删除共享/被引用对象，固定模型资源digest|

|operationId / owner|任务/用例|矩阵|请求/响应|
|---|---|---|---|
|`listServiceDiscoveredDevices` / connector-runtime|DM-RT-001；TC-DM-RT-001-01—03|OK/AUTH/PERM/TENANT/RESOURCE/PAGE/FAIL|`GET /api/v2/connection-providers/{providerId}/instances/{instanceId}/discovered-devices`；参数 → ServiceDiscoveredDevicePageResponse|

补充服务边界用例：TC-DM-RT-007-03执行inactive+enabled+有效凭据仍拒连接、active期望applied后才可接入；TC-DM-DEV-009-03执行private凭据解析的机器身份/tenant/device/generation/指纹限制及脱敏。此private用例和离线ProductMigrationRecord均不伪装成公开HTTP/CLI operation；分别以同OpenAPI组件做服务/升级输入合同验证。
