# 唯一原生 API 契约与实现任务

> **当前设计优先约束（2026-10-27用户纠正）：设备模板不做草稿、版本表、发布或模板切版；只有物模型保留版本。** 此前相反的候选设计已撤回。原188/580/108是历史规划规模，不能再作为拒绝收简的约束；接口/DB/CLI/用例正按新边界重写。详见[设计纠偏与影响检查](DESIGN-CORRECTION-2026-10-27.md)。相关旧测试仅保留历史证据，不证明新需求完成。


规划日期：2026-10-23；复核修订：2026-10-26。**[openapi.yaml](openapi.yaml) 是本轮唯一目标 wire contract；本文解释规则与依据。2026-10-26移除社区版不具备的扫码/预登记激活资格API。** [api-operations.json](api-operations.json)是从同一模型生成的操作索引，不是第二份契约。规范处于 `2.0.0-plan.4` 规划版；本轮没有声称服务已实现、生成器已兼容或合同已完成发布冻结。DM-API-001/009必须产生validator、客户端编译、合同测试和digest证据后才能冻结。

关联：[DOMAIN-MODEL.md](DOMAIN-MODEL.md)、[DATABASE.md](DATABASE.md)、[CLI.md](CLI.md)、[TASKS.md](TASKS.md)、[TEST-PLAN.md](TEST-PLAN.md)。

## 1. 正式规范与项目选择

本轮直接核对官方来源，初查2026-10-23、重新核查2026-10-25：

|范围|采用版本/规则|官方来源|适用方式|
|---|---|---|---|
|API描述|OpenAPI **3.2.1**（官方目录列为2026-09-10发布）|[OAS目录](https://spec.openapis.org/oas/)、[3.2.1规范](https://spec.openapis.org/oas/v3.2.1.html)|固定spec版本与工具链；不沿用旧Facade的3.1.0文档当新权威|
|JSON数据约束|JSON Schema Draft 2020-12；OAS 3.2 base dialect|[JSON Schema 2020-12](https://json-schema.org/draft/2020-12)、[OAS dialect](https://spec.openapis.org/oas/3.2/dialect/2026-02-26)|required与null分开；严谨枚举、数值类型、对象及引用|
|HTTP语义|RFC9110|[HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110.html)|方法、状态码、强ETag/If-Match；GET无业务写副作用|
|业务handler错误|RFC9457 Problem Details|[RFC9457](https://www.rfc-editor.org/rfc/rfc9457.html)|进入Encore业务handler后的参数、权限、数据错误用application/problem+json；HTTP状态与Problem.status一致|
|时间|RFC3339|[RFC3339](https://www.rfc-editor.org/rfc/rfc3339.html)|响应UTC `Z`，毫秒；内部timestamptz；客户端禁止本地时区歧义|
|UUID|RFC9562|[RFC9562](https://www.rfc-editor.org/info/rfc9562/)|目标实体UUID不透明；外部身份单独字段，旧ID迁移映射|
|前置条件/限流|RFC6585的428、429|[RFC6585](https://www.rfc-editor.org/rfc/rfc6585.html)|缺必要If-Match→428；限流429+Retry-After|
|部分更新|本项目显式JSON部分更新，**不宣称采用JSON Merge Patch**|[RFC7396作语义对照](https://www.rfc-editor.org/rfc/rfc7396.html)|PATCH application/json；对象/数组整体替换；省略不改；避免与RFC7396递归合并冲突|

大小写、pageSize=20/100、幂等窗口、深度32、批次100和导出大小是本项目工程政策，不是上述RFC强制值。全量OAS校验器已固定并通过当前3.2.1合同；Go/TypeScript生成器兼容及编译仍需实际验证。不允许一份3.2.1主文件加另一份手写3.1“客户端权威”。必要生成适配必须机械派生、保留同一语义并通过nullable/union/multipart样例，不静默降级。

## 2. 命名、版本与传输

- 公共路径统一 `/api/v2`。资源复数；特定业务动作使用显式集合（activations、number-changes、moves等；市场包发布等既有市场动作仍按各自owner保留）。按功能语义命名，不沿用旧device_config/device_template的歧义。
- JSON为lowerCamel；新设备编号为deviceNumber，物模型正式transport为modelId/snapshotId。既有native候选的modelID/snapshotID只在源码证据出现，切换时直接换消费端，不提供别名或旧响应兼容。
- 已有9个ThingModel native路由保留；`thing-model-commands`是模型编辑/发布命令，不是设备下行COMMAND。其core/app和显式revision CAS复用。
- 正式版本冻结后，新增可选响应字段可以兼容演进；删除/改类型/收紧输入枚举等须明确版本变化和一次性消费端切换。不得用版本策略为本次保留旧Gin、Facade或双读写。
- `x-owner`是职责标识，**不要求新建一套同名微服务**。优先复用现有Encore模块与runtime能力；运行时的正式入口直接调用其业务用例，不转发旧Community管理HTTP接口，不以旧表投影实现“原生”。
- 请求/响应schema、每个operationId、owner、permission、任务、参数、状态码都在OpenAPI；不能以本文的一行摘要替换其类型定义。

## 3. 成功、错误与字段语义

成功JSON使用 `{ "data": ..., "meta": { "requestId": "..." } }`；文件下载和SSE按对应媒体类型，不强行套JSON。列表data为 `items/page/pageSize/total`。空列表是`[]`，未知观察值是`null/unknown`，不能用0或空字符串冒充事实。

请求默认拒绝未知属性和未知query。required表示必须出现；nullable表示允许JSON null，二者独立。创建的省略默认值只能来自schema/明文规则；修改省略=不改，允许空的字段null=清除，非空字段null=422；对象/数组整体替换。`false`、`0`、`[]`和`{}`不当作“未填写”。配置动态parameters只允许在固定providerVersion/schemaDigest定义范围内；不是任意JSON漏洞。COMMAND参数按设备已应用的固定物模型schema验证。

实体ID由服务端产生；旧8–36位ID不能直填新UUID。Topic/脚本使用专用ConfigurationItemId作为模板内稳定局部key，客户端可生成UUID，服务端校验同模板唯一；它不是资源权限凭证。provider外部标识与runtime externalId是独立语义，不从旧ID字符串裁切推导。

|HTTP|稳定含义/典型code|通过条件|
|---|---|---|
|400|INVALID_QUERY / MALFORMED_REQUEST|无法解析、未知query等；不静默忽略|
|401|UNAUTHENTICATED / TOKEN_EXPIRED|缺失、失效、撤销或验证失败；带Bearer challenge|
|403|PERMISSION_DENIED|已认证但动作不允许，不泄漏对象敏感细节|
|404|NOT_FOUND|资源不存在、跨租户或资源scope不可见使用一致结果|
|409|REFERENCE_CONFLICT / IDEMPOTENCY_CONFLICT / REVISION_CONFLICT / OPERATION_IN_PROGRESS|业务引用、同键异参、body revision或运行状态冲突|
|410|ARTIFACT_EXPIRED|文件/临时结果过期；不以空文件成功代替|
|412|PRECONDITION_FAILED|强ETag不匹配；不能自动覆盖|
|422|VALIDATION_FAILED / CONFIGURATION_INVALID|schema、关系、provider能力、固定模型参数等错误|
|428|PRECONDITION_REQUIRED|应有If-Match而缺失|
|429|RATE_LIMITED|Retry-After，有界重试|
|500/503|INTERNAL / DEPENDENCY_UNAVAILABLE|失败可追requestId；不返回SQL/secret，不回旧API/表|

认证引导边界：Encore auth handler在业务handler之前拒绝缺失/无效凭据时，返回OpenAPI `EncoreAuthError`（Encore原生`code/message/details`），不承诺RFC9457或requestId；handler内生成的401及其他业务错误仍返回Problem。对外ingress必须在前置认证401上提供适用的`WWW-Authenticate: Bearer` challenge；不复制认证、不检查token、不改写Encore body。直接暴露Encore服务且未提供challenge不符合HTTP验收合同。

Problem至少包含type/title/status/detail/instance/code/requestId；字段错误含JSON Pointer、code、message。删除冲突的不可见引用只给类别/阻断码，visibleResourceId=null；普通引用列表仅返回有权看的对象，不能用必填引用ID泄漏隐藏资源。

## 4. 身份、租户、角色与资源授权

所有管理API由已有正式authn校验签名、固定HS256算法、必需且未过期的exp，并逐请求向Community会话owner校验撤销/冻结状态及id、tenant、authority一致性，生成可信tenant、subject、角色和资源scope。当前正式Community issuer签发共享HS256用户会话token，claims为id/tenant_id/authority/exp，不签发iss/aud/sub；本版不要求这些未发行字段，也不改变跨服务共享会话语义；若未来要引入issuer/audience，须先规划token版本/存量会话过渡并审计所有消费者。可选nbf若存在则必须有效。客户端body/query/任意tenant header不能选择租户。Community SYS_ADMIN token只证明管理员身份；无已验证的委派记录时，无论`tenant_id`为空还是非空，租户级API都拒绝访问，角色和`*`不得替代委派。平台管理员要先获得有审计的目标租户会话/委派上下文；服务身份携带最小scope而不是伪装管理员。

当前Community实际支持的用户Authority只有SYS_ADMIN、TENANT_ADMIN：B pkg/authz/community_roles.go:3–6、internal/middleware/jwt_auth.go:92及internal/service/sys_user.go:153已重新核实。TENANT_USER/TENANT_VIEWER仅见旧分支、残留条件或通用ThingModel候选，不能据此恢复这些Community产品角色。与Encore历史platform_admin/tenant_admin的映射由正式身份owner固定；本次不新增用户角色或重建账号/权限模块。下表只读和资源受限是授权能力场景，可由正式身份owner签发的受限测试上下文/服务身份验证，不声称已有对应Community用户菜单。现有ThingModel只按可信role映射read/write/publish，未知角色即使带`*` scope也不得扩大权限。当前metadata的去非hex/截断tenant归一化有碰撞风险，改算法前必须迁移已落库键并验证一对一。

|主体能力集|设备/模板/组读取|普通管理写|发布/控制/模拟|凭据/数据导出/数据删除|资源范围|
|---|---|---|---|---|---|
|平台管理且已明确租户委派|按委派授予|按授予|分别授予|分别授予并审计|目标租户内，不是任意跨租户参数|
|租户管理员|本租户授权范围|管理scope|发布、控制、模拟分别检查|凭据写/揭示、导出、删除各自scope|仍检查关联对象属于本租户|
|受限授权上下文（不新增用户角色）|读取scope|明确授予才写|不从“能编辑设备”推出能控制/模拟|不从read推出secret或数据删除|授权device IDs及模板/组可见范围|
|只读能力上下文（不新增用户角色）|read范围|403|403|默认无secret写/揭示/删除；导出需单独scope|total、统计、选择器、导出同一范围|
|服务主体|声明scope|声明scope|声明scope|声明scope|受同一tenant/resource规则约束|
|设备引导凭据|仅enrollDevice|不具管理权限|仅注册用途|只能获得本次注册允许的设备凭据|服务端从凭据推导tenant/template；body无tenantId|

metadata权限命名如device:read/write/delete/activate、device:credentials:read/write/reveal、device:diagnose/control/simulate、device:data:export/delete、template:read/write/import/export/diagnose/credentials:*、group:read/write/delete。ThingModel继续使用现有thingmodel.read/write/publish action映射；只是作用域登记语法不同，**同一授权owner**解析，不创建第二套权限引擎。

资源级校验作用于父、子、目标模板、产品、provider实例、group及每一批项目。加入分组不授予设备权限。受限用户的列表total、分组计数、告警统计和导出都必须先过滤权限。菜单/按钮能力只影响交互，最终拒绝由服务端执行。

## 5. 分页、筛选、排序与批量

所有常规列表page≥1、pageSize 1..100（默认1/20）；非法值不静默夹到200。items与total来自同一授权过滤和数据库读快照；默认稳定排序含ID tie-breaker。跨多页实时浏览不承诺全局快照，导出固定过滤/身份集合与read watermark，报告明确数据窗口。

DeviceFilters覆盖真实旧界面：名称/编号、类别、模板/物模型/产品、组/后代/未分组、网关/已绑未绑、管理/激活/在线状态、标签、协议/接入方式/provider/服务实例、固件、批次、告警。状态/告警/OTA条件由各owner提供授权ID或可重复观察范围，**在计数和分页前求交**；不能取第一页后在浏览器筛，不能把依赖失败解释为零设备。无template候选通过hasTemplate，网关候选通过gatewayScope，不另建平行选择器。

metadata批量创建最多100，原子落metadata，不自动代表runtime激活。模板批量绑定和网关批量绑定涉及runtime，采用完整预检+逐项持久提交/结果，成功项可保留、失败项可恢复；整批不能只回一个success。组成员批加/移除在同租户事务内全体校验，重复关系幂等，最多100。

用户设备导入先上传并做全文件校验，ImportValidation明确importId与operation；commit校验sourceDigest、schema版本和权限后分批提交。20,000行/20MiB为默认验收上限，CSV/XLSX保留编号字符串、0/false/null语义，拒绝公式执行/超限压缩包/重复表头/未知列。需要业务秘密的专门导入不能借普通表格字段偷渡。设备导出字段白名单明确包含标签、自定义资料、批次、位置等，不含凭据。

## 6. 幂等、并发与异步状态

**所有有副作用的操作必须提供Idempotency-Key。** POST校验、预览、批量观察是只读操作，不因HTTP POST就误记业务写。记录范围为tenant + subject + operationId + 规范路径 + key；请求摘要包含规范化body与影响表示的并发前置条件。相同key不同payload为409；相同key同payload只提交一次，网络丢响应/重启仍可重放；重放前重新鉴权。秘密响应加密、短期保留，撤权者不可重放。

元数据默认重放窗口24h；既有ThingModel核心保留30天收据窗口与过期键tombstone。用同一机制按operation登记窗口，避免为了表面一致无理由重写核心。过期键不能当新请求再次执行；明确返回KEY_EXPIRED/冲突，客户端以新请求意图决定是否换key。永久migration ID映射/审计不随该TTL清理。未终结内部引用保留恢复定位索引，服务端同owner自然键查回路径见§10.1；这不授予过期业务请求再次执行的权限。

资源GET给强ETag；PATCH/DELETE/关系PUT使用If-Match；缺428、不匹配412、弱ETag或`*`拒绝。动作集合使用request里的expectedResourceVersion或已存在ThingModel expectedDraftRevision/expectedRevision；body缺字段422，版本不符409。不要在集合资源上拿一个无关ETag代替目标资源CAS。自动重试不能静默重新GET再覆盖用户编辑。

202返回Operation或专属runtime run/receipt，并有可追踪Location；只有业务要求完成才succeeded。metadata operation/items/download按**来源operation的权限**重检，不固定device:read，从而模板导入者能合法读取自己的作业。telemetry历史导出任务由telemetry owner存储，使用同一状态/错误schema但独立reading-exports入口，不塞入元数据数据库。

凭据轮换先create得到pending凭据并完成受控交付，再用replacementCredentialId提交轮换；runtime确认后按允许窗口撤销旧凭据。不得异步生成秘密却没有可领取结果，也不依赖长期reveal补救。

凭据创建响应的`expiresAt`是本次秘密响应及同key重放窗口（首次成功写入起10分钟，不随重试延期），不是凭据本身的到期时间；新建设备凭据仍为pending。窗口到期返回稳定409，不以同key再次生成凭据。每次重放重验当前主体、动作和设备权限；凭据已撤销或自身已过期则拒绝重放。请求秘密最大8192 UTF-8字节，公开证书最大32768 UTF-8字节且只允许一张合法公开X509证书；私钥不进入请求或存储。创建及其错误响应均no-store，审计仅记录非秘密操作元数据。

## 7. 删除、引用及生命周期

设备删除：建立删除fence→查组/网关/产品/自动化/告警/市场等引用→阻止新引用→撤销接入与凭据→确认runtime隔离→tombstone。子设备、有效规则等冲突返回409；依赖不可确认则不完成删除。旧DeleteDevice连带清理历史的实现不复用。模板无有效设备/产品/OTA/规则/市场等引用才软删除；已选物模型快照仍由ThingModel owner保护。组必须无子组、无直接成员、无外部引用，禁止隐式递归删除设备。

跨独立owner用受控引用登记/删除fence和失败恢复，不能假定“先查一次再删除”能防止并发新引用。详情/列表默认不返回tombstone；审计按独立权限读取；具体保留时长由D-PRODUCT-003批准。

## 8. 模型、模板、运行配置与资源包
设备模板只有一份当前配置；create/update直接保存，validate仅校验输入配置，不建立草稿、发布版本或验证令牌。本次从当前合同删除`previewDeviceTemplateChange`、`publishDeviceAccessTemplate`、`listDeviceAccessTemplateRevisions`、`getDeviceAccessTemplateRevision`；模板export改为直接读取当前配置。模板复制、导入/导出仍作为保留能力列在合同中，但不纳入本轮核心验收必做；既有菜单范围内的Topic/Lua/扩展资料、动态注册和关联设备继续保留。既有市场包发布属于市场owner的发布，不受模板版本功能撤回影响。


旧设备模板菜单的CRUD来自Community `/api/v1/device_config`（`router/apps/device_config.go`）：`GET/POST/PUT /api/v1/device_config`、`GET/DELETE /api/v1/device_config/{id}`、`GET /api/v1/device_config/menu`、`PUT /api/v1/device_config/batch`；`internal/api/device_config.go`实现分页、下拉、详情与写入。旧 `device_configs` 保存协议/接入配置；`PUT /batch`把一组设备的`device_config_id`设为指定配置，对应新的批量模板绑定用例。`GET /api/v1/device_config/connect`和`GET /api/v1/device_config/voucher_type`提供连接/凭证表单选择；`GET /api/v1/device_config/metrics/menu`返回telemetry/attribute/command选项，`GET /api/v1/device_config/metrics/condition/menu`返回telemetry/attribute/event选项，供自动化动作/条件选择。它们需继续接正式模型与自动化owner，不能因模板去版本化而删除。Community 同名 `device_templates` 保存物模型及图表相关数据，不能当作设备模板菜单的同一来源。新合同把接入配置直接保存为模板唯一当前配置；模板绑定引用templateId，设备期望配置generation可携带当次配置快照，不形成模板历史版本。配置引用的ThingModel仍选择已发布revision，并固定校验tenant/model/snapshot/revision；Topic规则、Lua脚本、扩展资料定义和连接配置都在当前TemplateConfiguration中。

设备绑定/解绑直接提交当前templateId或directAccess并使用If-Match；不再调用切版预览，不需要previewToken。写入后按configurationGeneration等待runtime应用。设备desired配置携带该generation使用的当前TemplateConfiguration值快照；配置generation是传输/并发状态，不是模板历史版本。当前合同尚未确定：保存模板configuration是否自动向全部已绑定设备递增generation并推送，还是只在显式设备绑定/更新时生效；实现owner需先冻结扇出规则及其原子性。ThingModel绑定仍固定modelId/snapshotId/revision，模型发布不自动覆盖设备已固定的版本。

TemplatePackage包含模板当前配置和至多一个modelPackage，后者引用完整NativeThingModelSnapshotDetail。旧核心无独立模型导入/导出API；模板导出组合所选固定ThingModel Version，导入调用现有模型创建/发布用例并持久保存包内ID→新ID/版本映射。包formatVersion=1；图像assets内嵌内容、MIME和SHA256；总包128MiB上限，单图10MiB、最多10图；原字段imageAssetId由包映射为目标资产ID。Topic/脚本局部ID范围不等同模型资源ID。

包contentDigest对去除contentDigest后的JSON作固定规范化SHA256（DM-TPL-012/013以跨Go/TS黄金样例冻结，不使用普通map随机序列化）。resourcesChecksum验证既有资源规范化；源语义checksum仅作为受包hash保护的来源证据，因为既有HTTP响应没有计算源语义hash所需tenant输入。目标通过同一个core重新发布并记录新checksum。来自不同身份/版本的两个语义checksum不要求相等；可以比较规范化业务字段和各资源内容。

导入不存在“信任包里的tenantId/目标ID”入口；不直接写物模型子表。旧ICardView[]、custom_commands、custom_control和widget presets经已有转换器扩展处理，不能用自由object/空节点掩盖无法迁移的内容。

## 9. 运行时正式合同

|能力|正式操作族|所有者及必须验证的边界|
|---|---|---|
|状态/状态历史/批量观察|runtime-state、device-runtime-state-queries、status-history|runtime；observedAt/unknown与desired/applied分开；不能N+1查50设备|
|连接与诊断|connection-info、diagnostics|runtime；资源级diagnose权限，端点/信息脱敏，原匿名诊断停止|
|调试|debug-sessions、debug-logs|runtime；TTL、日志范围/限量/脱敏，session关闭后不继续采集|
|读数/缓存删除/导出|readings、latest-reading-deletions、reading-exports|telemetry；历史不进metadata；latest删除绑定不可混淆cacheVersion，连同时间戳相同的新采样也不会误删；历史导出按明确窗口/聚合|
|设备控制|command-executions、cancellations|control；只执行固定已应用模型COMMAND，ACK与物理效果不混同；延期任务仅pending可取消，已投递409|
|实时订阅|device-observation-stream|runtime组合正式观察事件；SSE fetch bearer，20秒heartbeat、100设备、游标重放/resync、撤权关闭；无旧WS|
|自动注册|device-enrollments|runtime验证用途受限引导凭据，调用同一metadata创建/凭据/激活用例；不信任请求tenant|
|模拟器|simulator-config/profile/sessions、simulation-runs|runtime保存仅模拟配置；正式授权broker/provider目标，无任意server/port代理；发送有限、可追踪、可重复安全；无第二Broker|
|脚本调试|processing-script-tests|connector-runtime复用Lua引擎沙箱；无真实下行副作用；配置元数据只是被执行的版本化输入|
|图片|file-assets|文件owner；元数据持久assetId，短期downloadUrl只作响应；不得存签名URL当长期身份|

上述能力是本次完整验收依赖，不能仅写“归运行时”而省略任务。运行时服务实现可以复用已经存在的Broker/历史/控制能力；正式API直接接该owner用例，**不调用旧Gin管理API，不读取旧metadata表维持运行**。

### 9.1 旧详情能力的精确字段与边界

模型字段和参数标识符统一复用 `NativeThingModelIdentifier`，保留既有 core 的非空/原字节规则；消费者不另设128字符上限。否则“可发布的模型无法执行”会成为隐蔽的合同错误。Topic引用仍为 `(dataKind,dataIdentifier)`，不能只用字符串猜字段类型。

读数查询与历史导出使用同一 `ReadingAggregation` / `ReadingWindow`：raw、min、max、avg、sum、diff、last；固定时长 30s、1m、2m、5m、10m、30m、1h、3h、6h、1d、7d、30d。旧 DAL `telemetry_data_aggregate.go:GetQueryString2` 的 diff 是 **max−min**；旧 `telemetry_datas.go:414–426` 的 1mo 实际是 **固定30天**，新界面和 CLI 明确叫30d，不称日历月。目标桶按 UTC epoch 对齐，输入窗口 `[from,to)`；空桶省略，首尾桶仅计算查询范围内样本；不同snapshot分别聚合。原始单请求≤24小时，聚合≤366天；输出按桶分页，导出最多100万行，超限明确422，不隐式改变采样模式。

`Reading` 明确返回 aggregation、windowStart/windowEnd、sampleCount；原始样本这些窗口字段为null，聚合 observedAt 为windowStart，cacheVersion=null。聚合不把无样本填0；数值[9,2,7]的sum=18、diff=7、avg=6是查询和导出的共同黄金用例。事件只能raw；min/max/avg/sum/diff只接受数值字段。

`listDeviceStatusHistory` 的 connectivity=online/offline/unknown 与响应同枚举，过滤先于分页及total计算。`CommandSubmission` 的可选label≤100；回执持久返回label、已提交parameters和redactedParameterPaths，页面刷新后仍可展示原请求的可见内容。`listDeviceCommandExecutions.labelContains` 为大小写敏感字面子串，不解释`%`/`_`；遮蔽参数按JSON Pointer列出，不能拿脱敏结果假装可无损重放。

`DeviceDiagnostic` 同时包含阶段检查、windowFrom/windowTo、uplink/downlink/storage统计与recentFailures。窗口默认24小时，上限31天；from/to一起给。successRate是0..1，total=0时为null；统计覆盖不足时complete=false且计数/比率均null。最近失败按timestamp/id倒序、默认20条/最多100条，并标truncated。各owner按已去重event/attempt固定分母；下行尝试成功不冒称物理效果成功。服务不可用返回503或明确部分unknown，不能用0/100%填补。

### 9.2 服务发现与配置交付（2026-10-25补齐）

`listServiceDiscoveredDevices`属于既有connector-runtime，参数providerId、instanceId、page/pageSize、search、snapshotId。它返回外部候选及授权后可见的已登记状态，不能用`listDevices`或provider目录代替。首个page=1取10分钟快照；后续页必须同snapshotId及相同过滤/pageSize，按externalId稳定排序，total先授权后计数。过期410、修改范围409、失权403/404、上游不可用503；CLI --all在同快照翻页。快照是runtime的临时观察材料，不建metadata事实表。选中后createDevice再次验证provider/instance归属、namespace/externalId和唯一性，不把候选当作授权凭据。

配置交付沿用既有metadata outbox和runtime账本，机器schema位于同一OpenAPI的`DeviceConfigurationEvent`、`DeviceDesiredConfiguration`、`RuntimeConfigurationReceipt`及`x-runtime-event-contracts`，不增加HTTP入口或消息服务。事件固定tenant/device/generation/eventId、可空持久operationId、完整期望配置和contentDigest；引用固定接入revision/物模型snapshot，禁止读latest。SHA256覆盖RFC8785 JCS(configuration)；[RFC8785](https://www.rfc-editor.org/rfc/rfc8785)是本项目选用的规范化方案（Informational RFC），不会替换既有物模型checksum算法。跨Go/TS黄金字节向量必须通过后冻结。

tenant字段来自可信生产事务；消费者同时验证传输服务身份、tenant及device scope。事件只携带凭据ID/指纹/期望状态，材料由既有凭据owner的受限合同解析，不新增公开秘密端点。按tenant/device持久顺序处理：同generation同digest重放receipt、不同digest冲突；小于applied返回stale；缺号先持久缓冲并重送原outbox事件，禁止跳过撤销等副作用。received只代表持久接收，applied需真实运行应用与账本可恢复，失败不推进applied。metadata仅据可信applied回执完成异步作业/激活/删除条件；应用后回执丢失重试仍不重复生效。对应DM-RT-007与TC-DM-RT-007-01—03。

## 10. 客户端、CLI、MCP和合同门禁

Go/TypeScript类型从冻结OpenAPI生成，CLI只做参数/输出适配，浏览器只做展示与交互；所有业务决定在正式owner用例。CLI不连接数据库、不调用/v1代理或project-runtime。MCP后续直接映射operationId和委派身份，不复制CLI、core或权限规则，也不能通过传tenantId扩大范围。

CI至少检查：OAS验证、schema示例、operation/权限/owner/task覆盖、客户端再生成零差异、实际handler路由与request/response合同、错误媒体类型、enum/null/整数边界、multipart、二进制/SSE、旧调用禁入。已有check-contract-freeze.sh只做旧Facade字符串检查，不能继续当新API验收证据。真实产品测试/构建留给实现任务，本轮只做文档静态一致性检查。

完整操作索引见下表（从OpenAPI同源生成）；请求/响应字段以YAML对应schema为准。

## 10.1 受限服务引用协议（2026-10-25补齐）

同一份OpenAPI现在包含**108项操作**：103项社区版用户/CLI操作，另5项 `x-audience=internal-service` 的引用协议操作。内部协议在既有devicemetadata中实现，不新建服务、兼容桥或第二套授权；它补全DATABASE §5.1的删除保护依赖。CLI/MCP工具生成器必须显式排除此audience；服务客户端仍从同一OpenAPI生成。

|operationId|路径/方法|必要输入|状态/失败条件|
|---|---|---|---|
|reserveMetadataReference|POST `/api/v2/internal/metadata-references`|target.kind/id，source.kind/id/revision/relationKind，Idempotency-Key|201 reserved；目标不open、同来源版本重复自然键409；不可见404|
|getMetadataReference|GET `.../{referenceId}`|referenceId|200 + 强ETag/resourceVersion；跨tenant或其他source owner404|
|confirmMetadataReference|POST `.../{referenceId}/confirmations`|expectedReferenceVersion/expectedSourceRevision/expectedTargetEpoch及key|reserved→active；过期预留、版本/epoch变化或fence不open为409|
|renewMetadataReference|POST `.../{referenceId}/renewals`|同上，expectedTargetEpoch指引用当前保存值|在重新校验目标live/open和scope后更新到当前epoch，预留600秒并递增version；只允许reserved|
|releaseMetadataReference|POST `.../{referenceId}/releases`|expectedReferenceVersion/expectedSourceRevision及key|reserved/active→released；可在目标deleting时释放；不按sourceId删除其他版本|

tenant和sourceOwner只从正式服务身份/可信委派上下文获取；请求schema拒绝这两个客户端字段。普通用户即使拿到同名scope字符串也403。source.kind/relationKind按服务owner注册白名单，源版本的真实存在、业务提交或撤销由来源owner证明并负责；元数据服务不替来源写业务表。

预留与目标fence加锁在同一事务：只有open可reserve；reserved和active都阻断删除，600秒到期仅提醒恢复，不自动release。来源先reserve→持久提交源业务→confirm，失败时显式release；崩溃后GET原引用、核对来源，再confirm/renew/release。目标拒绝删除后重开会增加epoch，旧reserved必须显式renew并复验才能confirm。source r2更新时可以先持有独立r2边，确认后再释放r1；迟到r1释放不能作用于r2。实现与故障测试由DM-API-011负责，数据库约束见§5.1，任务测试TC-DM-API-011-01—06。

**响应丢失与持久恢复**：来源owner在reserve之前先保存intent、不可变sourceRevision和稳定幂等键；源业务变更与confirm/release待发记录在来源自己的同一事务提交。reserve提交但响应丢失时以原键找回；未终结reserved/active的恢复索引不随通用24h清除。相同owner/tenant的自然键重复409在`Problem.conflicts[].visibleResourceId`返回既有referenceId（kind=metadata_reference、reasonCode=REFERENCE_EXISTS），服务端仍先鉴权；其他owner/租户不得看到这个ID。无论找回原收据还是得到冲突ID，都必须GET当前引用，不能把旧reserved响应当作当前状态。源commit结果不确定时先回读/对账，只有证明确未提交或已撤销使用才release。**全部reserve/confirm/renew/release/delete事务固定fence→edge锁顺序**；来源r2保护建立后再释放r1。


<!-- GENERATED OPERATIONS -->

|operationId|方法与路径|owner / 权限|请求 → 响应|实现任务|
|---|---|---|---|---|
|`listDevices`|`GET /api/v2/devices`|devicemetadata / `device:read`|参数见schema → DevicePageResponse|DM-DEV-001|
|`getDevice`|`GET /api/v2/devices/{deviceId}`|devicemetadata / `device:read`|参数见schema → DeviceResponse|DM-DEV-002|
|`createDevice`|`POST /api/v2/devices`|devicemetadata / `device:write`|CreateDevice → DeviceResponse|DM-DEV-003|
|`updateDevice`|`PATCH /api/v2/devices/{deviceId}`|devicemetadata / `device:write`|UpdateDevice → DeviceResponse|DM-DEV-004|
|`deleteDevice`|`DELETE /api/v2/devices/{deviceId}`|devicemetadata / `device:delete`|参数见schema → OperationResponse|DM-DEV-005|
|`getDeviceAdministrativeState`|`GET /api/v2/devices/{deviceId}/administrative-state`|devicemetadata / `device:read`|参数见schema → AdministrativeStateResponse|DM-DEV-006|
|`setDeviceAdministrativeState`|`PUT /api/v2/devices/{deviceId}/administrative-state`|devicemetadata / `device:write`|SetAdministrativeState → OperationResponse|DM-DEV-006|
|`activateDevice`|`POST /api/v2/devices/{deviceId}/activations`|devicemetadata / `device:activate`|ActivateDevice → OperationResponse|DM-DEV-007|
|`checkDeviceNumber`|`POST /api/v2/device-number-validations`|devicemetadata / `device:read`|DeviceNumberCheck → DeviceNumberAvailabilityResponse|DM-DEV-008|
|`listDeviceCredentials`|`GET /api/v2/devices/{deviceId}/credentials`|devicemetadata / `device:credentials:read`|参数见schema → CredentialSummaryPageResponse|DM-DEV-009|
|`createDeviceCredential`|`POST /api/v2/devices/{deviceId}/credentials`|devicemetadata / `device:credentials:write`|CreateCredential → CredentialSecretResponse|DM-DEV-009|
|`rotateDeviceCredential`|`POST /api/v2/devices/{deviceId}/credentials/{credentialId}/rotations`|devicemetadata / `device:credentials:write`|RotateCredential → OperationResponse|DM-DEV-010|
|`revokeDeviceCredential`|`POST /api/v2/devices/{deviceId}/credentials/{credentialId}/revocations`|devicemetadata / `device:credentials:write`|RevokeCredential → OperationResponse|DM-DEV-010|
|`revealDeviceCredential`（2026-10-29已作废：明文直接由`listDeviceCredentials`的`secret`返回，不实现本操作）|`POST /api/v2/devices/{deviceId}/credentials/{credentialId}/reveals`|devicemetadata / `device:credentials:reveal`|CredentialRevealRequest → CredentialSecretResponse|DM-DEV-011|
|`batchCreateDevices`|`POST /api/v2/device-batches`|devicemetadata / `device:write`|BatchDeviceCreate → OperationResponse|DM-DEV-012|
|`validateDeviceImport`|`POST /api/v2/device-imports`|devicemetadata / `device:import`|ImportUpload → ImportValidationResponse|DM-DEV-013|
|`getDeviceImport`|`GET /api/v2/device-imports/{importId}`|devicemetadata / `device:import`|参数见schema → ImportValidationResponse|DM-DEV-013|
|`commitDeviceImport`|`POST /api/v2/device-imports/{importId}/commits`|devicemetadata / `device:import`|ImportCommit → OperationResponse|DM-DEV-014|
|`downloadDeviceImportFormat`|`GET /api/v2/device-import-format`|devicemetadata / `device:import`|参数见schema → binary (see response content types)|DM-DEV-013|
|`exportDevices`|`POST /api/v2/device-exports`|devicemetadata / `device:export`|DeviceExport → OperationResponse|DM-DEV-015|
|`getDeviceTemplateBinding`|`GET /api/v2/devices/{deviceId}/template-binding`|devicemetadata / `device:read`|参数见schema → DeviceTemplateBindingResponse|DM-DEV-016|
|`setDeviceTemplateBinding`|`PUT /api/v2/devices/{deviceId}/template-binding`|devicemetadata / `device:write`|SetTemplateBinding → OperationResponse|DM-DEV-017|
|`getGatewayMembership`|`GET /api/v2/devices/{deviceId}/gateway-membership`|devicemetadata / `device:read`|参数见schema → GatewayMembershipResponse|DM-DEV-018|
|`setGatewayMembership`|`PUT /api/v2/devices/{deviceId}/gateway-membership`|devicemetadata / `device:write`|SetGatewayMembership → OperationResponse|DM-DEV-018|
|`listSubdevices`|`GET /api/v2/devices/{deviceId}/subdevices`|devicemetadata / `device:read`|参数见schema → DevicePageResponse|DM-DEV-019|
|`getDeviceStatistics`|`GET /api/v2/device-statistics`|devicemetadata / `device:read`|参数见schema → DeviceStatisticsResponse|DM-DEV-020|
|`listDeviceAccessTemplates`|`GET /api/v2/device-access-templates`|devicemetadata / `template:read`|参数见schema → DeviceAccessTemplatePageResponse|DM-TPL-001|
|`getDeviceAccessTemplate`|`GET /api/v2/device-access-templates/{templateId}`|devicemetadata / `template:read`|参数见schema → DeviceAccessTemplateResponse|DM-TPL-002|
|`createDeviceAccessTemplate`|`POST /api/v2/device-access-templates`|devicemetadata / `template:write`|CreateDeviceAccessTemplate → DeviceAccessTemplateResponse|DM-TPL-003|
|`updateDeviceAccessTemplate`|`PATCH /api/v2/device-access-templates/{templateId}`|devicemetadata / `template:write`|UpdateDeviceAccessTemplate → DeviceAccessTemplateResponse|DM-TPL-004|
|`validateDeviceAccessTemplate`|`POST /api/v2/device-access-templates/{templateId}/validations`|devicemetadata / `template:write`|TemplateConfiguration → ValidationReportResponse|DM-TPL-007|
|`deleteDeviceAccessTemplate`|`DELETE /api/v2/device-access-templates/{templateId}`|devicemetadata / `template:delete`|参数见schema → DeviceAccessTemplateResponse|DM-TPL-009|
|`listDeviceAccessTemplateReferences`|`GET /api/v2/device-access-templates/{templateId}/references`|devicemetadata / `template:read`|参数见schema → TemplateReferencePageResponse|DM-TPL-010|
|`copyDeviceAccessTemplate`|`POST /api/v2/device-access-templates/{templateId}/copies`|devicemetadata / `template:write`|CloneTemplate → DeviceAccessTemplateResponse|DM-TPL-011|
|`exportDeviceAccessTemplate`|`GET /api/v2/device-access-templates/{templateId}/export`|devicemetadata / `template:export`|参数见schema → TemplatePackageResponse|DM-TPL-012|
|`importDeviceAccessTemplate`|`POST /api/v2/device-access-template-imports`|devicemetadata / `template:import`|TemplatePackage → OperationResponse|DM-TPL-013|
|`listProducts`|`GET /api/v2/products`|devicemetadata / `product:read`|参数见schema → ProductPageResponse|DM-TPL-014|
|`getProduct`|`GET /api/v2/products/{productId}`|devicemetadata / `product:read`|参数见schema → ProductResponse|DM-TPL-014|
|`listConnectionProviders`|`GET /api/v2/connection-providers`|connector-runtime / `template:read`|参数见schema → ConnectionProviderPageResponse|DM-TPL-015|
|`listDeviceGroups`|`GET /api/v2/device-groups`|devicemetadata / `group:read`|参数见schema → DeviceGroupPageResponse|DM-GRP-001|
|`getDeviceGroup`|`GET /api/v2/device-groups/{groupId}`|devicemetadata / `group:read`|参数见schema → DeviceGroupResponse|DM-GRP-001|
|`getDeviceGroupTree`|`GET /api/v2/device-group-tree`|devicemetadata / `group:read`|参数见schema → GroupTreeResponse|DM-GRP-002|
|`createDeviceGroup`|`POST /api/v2/device-groups`|devicemetadata / `group:write`|CreateDeviceGroup → DeviceGroupResponse|DM-GRP-003|
|`updateDeviceGroup`|`PATCH /api/v2/device-groups/{groupId}`|devicemetadata / `group:write`|UpdateDeviceGroup → DeviceGroupResponse|DM-GRP-004|
|`moveDeviceGroup`|`POST /api/v2/device-groups/{groupId}/moves`|devicemetadata / `group:write`|MoveDeviceGroup → DeviceGroupResponse|DM-GRP-005|
|`deleteDeviceGroup`|`DELETE /api/v2/device-groups/{groupId}`|devicemetadata / `group:delete`|参数见schema → DeviceGroupResponse|DM-GRP-006|
|`listDeviceGroupMembers`|`GET /api/v2/device-groups/{groupId}/members`|devicemetadata / `group:read`|参数见schema → GroupMemberPageResponse|DM-GRP-007|
|`addDeviceGroupMembers`|`POST /api/v2/device-groups/{groupId}/member-additions`|devicemetadata / `group:write`|SetGroupMembers → DeviceGroupResponse|DM-GRP-008|
|`removeDeviceGroupMembers`|`POST /api/v2/device-groups/{groupId}/member-removals`|devicemetadata / `group:write`|SetGroupMembers → DeviceGroupResponse|DM-GRP-009|
|`listGroupsForDevice`|`GET /api/v2/devices/{deviceId}/groups`|devicemetadata / `device:read`|参数见schema → DeviceGroupPageResponse|DM-GRP-010|
|`getMetadataOperation`|`GET /api/v2/metadata-operations/{operationId}`|devicemetadata / `originating-operation:read`|参数见schema → OperationResponse|DM-API-008|
|`listMetadataOperationItems`|`GET /api/v2/metadata-operations/{operationId}/items`|devicemetadata / `originating-operation:read`|参数见schema → OperationItemPageResponse|DM-API-008|
|`downloadMetadataArtifact`|`GET /api/v2/metadata-downloads/{downloadId}`|devicemetadata / `originating-operation:read`|参数见schema → binary (see response content types)|DM-API-008|
|`getDeviceRuntimeState`|`GET /api/v2/devices/{deviceId}/runtime-state`|device-runtime / `device:read`|参数见schema → RuntimeStateResponse|DM-RT-002|
|`getDeviceConnectionInfo`|`GET /api/v2/devices/{deviceId}/connection-info`|device-runtime / `device:diagnose`|参数见schema → ConnectionInfoResponse|DM-RT-003|
|`createDeviceDebugSession`|`POST /api/v2/devices/{deviceId}/debug-sessions`|device-runtime / `device:diagnose`|DebugSessionRequest → DebugSessionResponse|DM-RT-004|
|`stopDeviceDebugSession`|`DELETE /api/v2/devices/{deviceId}/debug-sessions/{sessionId}`|device-runtime / `device:diagnose`|参数见schema → DebugSessionResponse|DM-RT-004|
|`listDeviceDebugLogs`|`GET /api/v2/devices/{deviceId}/debug-logs`|device-runtime / `device:diagnose`|参数见schema → DebugLogPageResponse|DM-RT-004|
|`listDeviceReadings`|`GET /api/v2/devices/{deviceId}/readings`|telemetry / `device:read`|参数见schema → ReadingPageResponse|DM-RT-005|
|`submitDeviceCommand`|`POST /api/v2/devices/{deviceId}/command-executions`|control / `device:control`|CommandSubmission → CommandReceiptResponse|DM-RT-006|
|`getDeviceCommandExecution`|`GET /api/v2/devices/{deviceId}/command-executions/{executionId}`|control / `device:control`|参数见schema → CommandReceiptResponse|DM-RT-006|
|`queryDeviceRuntimeStates`|`POST /api/v2/device-runtime-state-queries`|device-runtime / `device:read`|RuntimeStateQuery → RuntimeStateBatchResponse|DM-RT-002|
|`changeDeviceNumber`|`POST /api/v2/devices/{deviceId}/number-changes`|devicemetadata / `device:write`|RenumberDevice → OperationResponse|DM-DEV-021|
|`batchSetDeviceTemplateBindings`|`POST /api/v2/device-template-binding-batches`|devicemetadata / `device:write`|BatchTemplateBinding → BatchTemplateBindingResponse|DM-DEV-023|
|`testProcessingScript`|`POST /api/v2/processing-script-tests`|connector-runtime / `template:diagnose`|ProcessingScriptTest → ProcessingScriptTestResultResponse|DM-TPL-018|
|`listTemplateEnrollmentCredentials`|`GET /api/v2/device-access-templates/{templateId}/enrollment-credentials`|devicemetadata / `template:credentials:read`|参数见schema → TemplateEnrollmentCredentialPageResponse|DM-TPL-020|
|`createTemplateEnrollmentCredential`|`POST /api/v2/device-access-templates/{templateId}/enrollment-credentials`|devicemetadata / `template:credentials:write`|CreateTemplateEnrollmentCredential → TemplateEnrollmentSecretResponse|DM-TPL-020|
|`revokeTemplateEnrollmentCredential`|`DELETE /api/v2/device-access-templates/{templateId}/enrollment-credentials/{credentialId}`|devicemetadata / `template:credentials:write`|参数见schema → TemplateEnrollmentCredentialResponse|DM-TPL-020|
|`enrollDevice`|`POST /api/v2/device-enrollments`|device-runtime / `enrollment:register`|DeviceEnrollment → DeviceEnrollmentResultResponse|DM-RT-013|
|`batchSetGatewayMemberships`|`POST /api/v2/gateway-membership-batches`|devicemetadata / `device:write`|GatewayMembershipBatch → OperationResponse|DM-DEV-027|
|`listDeviceCommandExecutions`|`GET /api/v2/devices/{deviceId}/command-executions`|control / `device:control`|参数见schema → CommandReceiptPageResponse|DM-RT-014|
|`cancelDeviceCommandExecution`|`POST /api/v2/devices/{deviceId}/command-executions/{executionId}/cancellations`|control / `device:control`|CancelCommand → CommandReceiptResponse|DM-RT-014|
|`listDeviceStatusHistory`|`GET /api/v2/devices/{deviceId}/status-history`|device-runtime / `device:read`|参数见schema → DeviceStatusEventPageResponse|DM-RT-015|
|`getDeviceDiagnostics`|`GET /api/v2/devices/{deviceId}/diagnostics`|device-runtime / `device:diagnose`|参数见schema → DeviceDiagnosticResponse|DM-RT-015|
|`getDeviceSimulatorConfig`|`GET /api/v2/devices/{deviceId}/simulator-config`|device-runtime / `device:diagnose`|参数见schema → SimulatorConfigResponse|DM-RT-015|
|`deleteDeviceLatestReading`|`POST /api/v2/devices/{deviceId}/latest-reading-deletions`|telemetry / `device:data:delete`|DeleteLatestReading → LatestReadingDeletionResponse|DM-RT-016|
|`streamDeviceObservations`|`GET /api/v2/device-observation-stream`|device-runtime / `device:read`|参数见schema → SSE ObservationEvent|DM-RT-017|
|`createFileAsset`|`POST /api/v2/file-assets`|file-storage / `template:write`|FileAssetUpload → FileAssetResponse|DM-TPL-021|
|`getFileAsset`|`GET /api/v2/file-assets/{assetId}`|file-storage / `template:read`|参数见schema → FileAssetResponse|DM-TPL-021|
|`exportDeviceReadings`|`POST /api/v2/devices/{deviceId}/reading-exports`|telemetry / `device:data:export`|ReadingExportRequest → OperationResponse|DM-RT-018|
|`getReadingExport`|`GET /api/v2/reading-exports/{operationId}`|telemetry / `device:data:export`|参数见schema → OperationResponse|DM-RT-018|
|`downloadReadingExport`|`GET /api/v2/reading-exports/{operationId}/download`|telemetry / `device:data:export`|参数见schema → CSV or NDJSON binary/text download|DM-RT-018|
|`getDeviceSimulatorProfile`|`GET /api/v2/devices/{deviceId}/simulator-profile`|device-runtime / `device:simulate`|参数见schema → SimulatorProfileResponse|DM-RT-019|
|`updateDeviceSimulatorProfile`|`PUT /api/v2/devices/{deviceId}/simulator-profile`|device-runtime / `device:simulate`|UpdateSimulatorProfile → SimulatorProfileResponse|DM-RT-019|
|`createDeviceSimulationSession`|`POST /api/v2/devices/{deviceId}/simulator-sessions`|device-runtime / `device:simulate`|SimulationSessionRequest → SimulationSessionResponse|DM-RT-019|
|`closeDeviceSimulationSession`|`DELETE /api/v2/devices/{deviceId}/simulator-sessions/{sessionId}`|device-runtime / `device:simulate`|参数见schema → SimulationSessionResponse|DM-RT-019|
|`runDeviceSimulation`|`POST /api/v2/devices/{deviceId}/simulation-runs`|device-runtime / `device:simulate`|SimulationRunRequest → SimulationRunResponse|DM-RT-019|
|`getDeviceSimulationRun`|`GET /api/v2/devices/{deviceId}/simulation-runs/{runId}`|device-runtime / `device:simulate`|参数见schema → SimulationRunResponse|DM-RT-019|
|`getDeviceEnrollmentStatus`|`GET /api/v2/device-enrollment-operations/{operationId}`|device-runtime / `enrollment:status`|参数见schema → DeviceEnrollmentStatusResponse|DM-RT-013|
|`createThingModel`|`POST /api/v2/thing-model-commands/create`|thingmodel / `thingmodel.write`|NativeThingModelCreate → NativeThingModelMutationResultResponse|DM-API-007|
|`updateThingModel`|`POST /api/v2/thing-model-commands/update`|thingmodel / `thingmodel.write`|NativeThingModelUpdate → NativeThingModelMutationResultResponse|DM-API-007|
|`publishThingModel`|`POST /api/v2/thing-model-commands/publish`|thingmodel / `thingmodel.publish`|NativeThingModelPublish → NativeThingModelMutationResultResponse|DM-API-007|
|`listThingModels`|`GET /api/v2/thing-models`|thingmodel / `thingmodel.read`|参数见schema → NativeThingModelSummaryPageResponse|DM-API-007|
|`getThingModel`|`GET /api/v2/thing-models/{modelId}`|thingmodel / `thingmodel.read`|参数见schema → NativeThingModelDetailResponse|DM-API-007|
|`listThingModelVersions`|`GET /api/v2/thing-models/{modelId}/versions`|thingmodel / `thingmodel.read`|参数见schema → NativeThingModelVersionSummaryPageResponse|DM-API-007|
|`getThingModelVersion`|`GET /api/v2/thing-models/{modelId}/versions/{publishedRevision}`|thingmodel / `thingmodel.read`|参数见schema → NativeThingModelSnapshotDetailResponse|DM-API-007|
|`getThingModelResources`|`GET /api/v2/thing-models/{modelId}/resources`|thingmodel / `thingmodel.read`|参数见schema → NativeThingModelResourcesResponse|DM-API-007|
|`updateThingModelResources`|`POST /api/v2/thing-model-commands/update-resources`|thingmodel / `thingmodel.write`|NativeThingModelUpdateResources → NativeThingModelMutationResultResponse|DM-API-007|
|`reserveMetadataReference`|`POST /api/v2/internal/metadata-references`|devicemetadata / `metadata.reference:reserve`（仅service）|ReserveMetadataReference → MetadataReferenceResponse|DM-API-011|
|`getMetadataReference`|`GET /api/v2/internal/metadata-references/{referenceId}`|devicemetadata / `metadata.reference:read`（仅service）|参数见schema → MetadataReferenceResponse|DM-API-011|
|`confirmMetadataReference`|`POST /api/v2/internal/metadata-references/{referenceId}/confirmations`|devicemetadata / `metadata.reference:confirm`（仅service）|MetadataReferenceCommand → MetadataReferenceResponse|DM-API-011|
|`renewMetadataReference`|`POST /api/v2/internal/metadata-references/{referenceId}/renewals`|devicemetadata / `metadata.reference:renew`（仅service）|MetadataReferenceCommand → MetadataReferenceResponse|DM-API-011|
|`releaseMetadataReference`|`POST /api/v2/internal/metadata-references/{referenceId}/releases`|devicemetadata / `metadata.reference:release`（仅service）|ReleaseMetadataReference → MetadataReferenceResponse|DM-API-011|
|`listServiceDiscoveredDevices`|`GET /api/v2/connection-providers/{providerId}/instances/{instanceId}/discovered-devices`|connector-runtime / `device:discover`|参数见schema → ServiceDiscoveredDevicePageResponse|DM-RT-001|

### 创建与迁移的两个明确分支

产品默认模板只是建议，不产生未在schema定义的兼容限制。激活有模板设备验证模板当前配置；无模板验证directAccess，模型允许为空。凭据只按provider policy需要；无配置设备可登记inactive，激活422 CONFIGURATION_INCOMPLETE；runtime确认后才active。initialGroupIds显式数组优先，[]不归组，省略保留本tenant唯一根且有成员写权限的默认归组，否则无组，不扩大资源授权。

ProductMigrationRecord仅是同owner受控离线升级输入schema，非新增公共HTTP/CLI写操作；目标tenant由批准run与持久映射核实，不信sourceTenantId字段自行授权。一次性来源映射和正式运行目录分离，细节见DATABASE末节和DM-MIG-006。


**显式激活与凭据内部合同**：DeviceDesiredConfiguration.required包含desiredActivationState；inactive+enabled即使凭据已配置仍不可连接，激活用例发active期望，只有applied后元数据activationState才确认。`x-runtime-private-contracts.resolveRuntimeCredential`是同一devicemetadata凭据owner的Encore private调用（不注册公共HTTP，不新增服务）；输入ResolveRuntimeCredential，输出敏感RuntimeCredentialMaterial，错误复用Problem。runtime/provider机器主体需credential.runtime:resolve，可信tenant来自服务上下文，且credential必须由同device已提交generation明确引用，fingerprint匹配；pending只在正在应用的active意图中可解析，revoked拒绝，旧代冲突。响应<=60秒有效；verifier、owner解密材料或公开certificate三种模式互斥，经双向认证或等价的加密服务传输交付；无秘密outbox/日志/trace/普通HTTP，运行侧确需存储只用既有加密设施。CLI/MCP/普通用户无入口，不能以scope字符串伪装机器身份。该内部合同不计入109个HTTP operation，私有schema生成与服务合同另由DM-DEV-009/DB-007/RT-007验收。

### 当前配置最小职责补充（2026-10-27）

DeviceAccessTemplate只保存/返回imageAssetId，不含imageUrl/downloadUrl；封面展示通过FileAsset GET获取带授权的短期地址，不把临时capability写入metadata或普通幂等收据。configuration.access的providerVersion/schemaDigest允许省略；显式提供仍校验格式，不将新provider发布目录作为内建MQTT普通创建/保存的前置条件。ThingModel的publishedRevision与固定snapshot引用继续保留。

批量模板绑定/解绑按现有普通批量操作收简为有界同步200结果；整批元数据校验/事务提交，不新增异步作业。此200不表示运行时已应用。


### 2026-10-27 既有模型继承与成员移除规则澄清

Device.modelBinding和DeviceTemplateBinding.modelBinding表示关联模板当前选定的已发布物模型版本，不是设备独立保存的历史模板配置。模板显式选择其它发布版本会改变关联设备的modelBinding及表示ETag；单纯发布新的物模型版本不会自动切换模板。模板与设备的公开字段形状不变。

removeDeviceGroupMembers为普通组织关系变更：同租户和授权校验、expectedGroupVersion、原子事务、幂等审计继续满足；移出分组不删除对象，不以不存在的外部引用checker阻断所有移除。删除组的引用保护不受此澄清影响。


### 2026-10-27 设备列表与默认编号补齐

CreateDevice.deviceNumber允许省略，服务端在同一创建事务生成device ID并作为缺省编号；显式null/空值仍拒绝，自带合法编号保留。请求收据按原输入（保留省略与显式值区别）幂等重放首次分配结果。此为恢复Community原有默认行为，不新增用户输入。

DeviceGroup新增只读subtreeDeviceCount：同授权设备范围，本组与全部后代组成员按设备ID去重，和includeDescendants=true列表一致。directDeviceCount、descendantDeviceCount原定义不变，不能相加冒充子树数量。GET device-statistics补齐原有操作；metadata计数与list共用同快照过滤，未接runtime/告警观测为null与unknown，不能回0假成功。

### 2026-10-27 原生空分组删除的当前依赖范围

DELETE group的公开200/If-Match/key及有子组/成员/实际外部引用409合同不变。当前选定A30仅有device/device_config绑定，无native device_group引用owner；旧Community groups的场景引用不能未经迁移映射当作native device_groups引用。本切片由同tenant现有树锁串行create/move/member/delete，并检查live子组和全部成员关系；不为了当前不存在的owner新增registry/fence或总是503占位。若后续迁移或接入真正持native组引用的owner，必须在启用该引用写入前补真实一致性保护并验收；这不免除设备/模板已有真实自动化引用的保护，也不代表旧组引用迁移已完成。

### 模板扩展字段原功能收尾（2026-10-27）

现有PATCH device-access-templates增加向后兼容的customFieldChanges（upsert/remove必填数组，总操作1..100），与完整configuration互斥，不增加HTTP operation或模板版本/草稿/发布流程。upsert以key定位，仅替换显式字段；省略保留原始值，null显式写入；remove按key移除，不存在时无副作用。已有字段顺序保持、新增按请求顺序追加。新定义合成后统一校验完整配置，未改的其它配置及数值默认值不经JS浮点往返。

修改时复用模板行锁、原ETag、Idempotency-Key及原子audit/receipt；已被live设备保存的key不能直接删除（false/0/null同样算保存），不兼容类型/枚举/重新启用409并保留设备值。改名/描述/默认与停用可正常保存，无值字段可删除。字段保护同时适用于完整configuration替换与customFieldChanges增量保存，按旧/新定义实际差异判断，不允许旧编辑入口绕过。新增字段与枚举项沿用完整配置的必填/未知字段校验。已有设备写值读取定义时与模板修改共用行锁边界，防检查后并发新增不兼容数据；不新增通用预览作业或清理设备值的隐式行为。

页面保留原列表/新增/编辑/启停/删除确认；失败保留输入，结果未知保留原payload/ETag/key，成功后的读取失败不重新发写。只对本次受影响路径做定向验证，不重跑之前凭据/分组/列表整轮。
