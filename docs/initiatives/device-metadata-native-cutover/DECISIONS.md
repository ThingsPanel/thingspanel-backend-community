# 已确认决策与待业务确认项

> **当前设计优先约束（2026-10-27用户纠正）：设备模板不做草稿、版本表、发布或模板切版；只有物模型保留版本。** 此前相反的候选设计已撤回。原188/580/108是历史规划规模，不能再作为拒绝收简的约束；接口/DB/CLI/用例正按新边界重写。详见[设计纠偏与影响检查](DESIGN-CORRECTION-2026-10-27.md)。相关旧测试仅保留历史证据，不证明新需求完成。


> 2026-10-25已按当前三仓源码重新复核；本轮增量、Git状态与证据边界见[REVIEW-2026-10-25.md](REVIEW-2026-10-25.md)。原2026-10-23证据保留其历史日期。唯一目标合同已修订为2.0.0-plan.4；内部引用协议见API-CONTRACT §10.1。

日期：2026-10-23。入口：[TASKS.md](TASKS.md)。证据：[EVIDENCE.md](EVIDENCE.md)、[INVENTORY.md](INVENTORY.md)。本轮不执行确认、迁移、发布或代码变更。

## 1. 已确定，不能再次推给业务方

|ID|决定|依据与后果|任务|
|---|---|---|---|
|D-ARCH-001|三个菜单整体规划、整版验收；不是先挑一个菜单试点|用户明确要求。按公共合同/数据/并行用例/全量切换推进，发布门禁同时覆盖三菜单|DM-BAS-001、DM-QA-010|
|D-ARCH-002|唯一Encore原生API和新数据事实；无桥接/旧API兼容/双读双写/运行回退|用户本轮优先于旧strangler文档及10/20保留投影建议。允许一次性受控迁移|DM-DOM-002、DM-RT-012、DM-MIG-014|
|D-ARCH-003|1.3物模型已合并且模板选模型创建设备正常；自动化/告警待其他人完成后再接；企业版扫码/产品激活排除|Community 1.3合并99c17f62，Frontend 1.3合并1156db2d，Kernel 1.3包含37fbb5e8；用户2026-10-26确认正常|DM-API-007、DM-RT-008/009、DM-DEV-022、DM-DEC-004|
|D-DOM-001|设备模板菜单=旧device_configs；旧device_templates=物模型与展示资源；Encore模板是新的接入模板基础|前端路由/真实请求与后端实体均已核实。统一为DeviceAccessTemplate，演进既有owner，不新造平行模板模块|DM-DOM-001、DM-DB-004|
|D-DOM-002|已合并的1.3原生物模型直接复用，禁止重写模型核心|1.3已有提交与用户功能确认；新v2 API只核差异和回归|DM-API-007|
|D-DOM-003|产品可选；无模型、无模板、服务设备是合法业务对象|实际创建、服务批创和现有决策均确认。不能以数据库NOT NULL迫使造空模型|DM-DEV-003/024、DM-DB-011|
|D-SEC-001|租户来自正式验证后的身份；不信任body/query/header租户或匿名注册tenant|当前旧gateway-register请求曾含tenant，当前normalizePrincipalID存在碰撞。修复属于授权边界，不是可选兼容|DM-API-002/003、DM-RT-013|
|D-RUN-001|在线、遥测历史、协议执行、诊断、命令投递分别归runtime/telemetry/control/connector|界面展示不改变数据owner。metadata只记录期望配置，不能为了ER完整迁入历史|DM-RT系列|
|D-REL-001|保留多级网关、编号变更、批量模板绑定、Topic/脚本/扩展资料等真实旧功能|均有前后端源码证据，不能静默删掉。完善事务/权限/版本而非照抄旧风险|DM-DEV-021/023/027、DM-TPL-017—020|
|D-CLI-001|CLI本次必交付，Go+shell单一实现；MCP未来只调用同API和授权|当前CLI设备还走/v1代理，模板只有project-runtime，分组直连老Encore路径；不能算原生已验收|DM-CLI-001—018、DM-API-010|
|D-DATA-001|迁移只在维护窗以账本受控一次性执行；旧ID映射长期审计，不做运行时lookup|无新旧写入并存；备份、源预检、dry-run、checkpoints、readback和故障恢复齐全才能切|DM-MIG系列|
|D-TEST-001|代码存在、测试存在/通过、页面切换、数据迁移、版本发布分列|旧登记数/历史QA/候选ref都不能自动计为当前发布完成|DM-QA-010|

## 2. 明确的工程取舍（已写入合同，实施时以测试冻结）

|ID|工程规则|理由及验证边界|
|---|---|---|
|D-API-001|目标OpenAPI 3.2.1，JSON Schema 2020-12；正式规范链接见API-CONTRACT|全量validator固定为`libopenapi-validator v0.16.1`/`libopenapi v0.41.3`，为有限树结构忽略数组递归误报后，整份spec校验通过；Go/TypeScript生成器和客户端编译仍待验证，不得假称已冻结发布合同|
|D-API-002|成功data/meta，错误RFC9457；page/pageSize统一1/20/100；未知字段/查询参数拒绝|这是项目工程政策，不是RFC规定的默认值。模型既有query筛选和排序明确保留，必须映射同一响应结构|
|D-API-003|PATCH application/json显式部分更新；对象/数组整体替换；nullable字段null清空|不冒充RFC7396 Merge Patch，避免标签/草稿对象递归合并歧义。缺字段不改，非空字段null为422|
|D-API-004|强ETag的资源修改使用If-Match；集合命令使用显式expectedRevision/version|HTTP前置条件缺失428/不匹配412；核心模型已有body revision冲突保留409，避免重复CAS输入|
|D-API-005|metadata24h、既有ThingModel30天重放窗口，撤权后不能重放秘密；长作业保留独立ID和永久迁移账本|窗口与任务保留是工程默认，不冒称标准。expired receipt不把旧命令静默重新执行；目标存储/清理见DATABASE|
|D-DATA-002|同租户设备编号大小写敏感、不静默trim；UTF-8无控制符/首尾空格、128上限；保留改号工作流|旧编号全局唯一；新租户唯一必须配runtime namespace规则，不可以不同租户相同ClientID误路由。超长/冲突源数据进入异常报告|
|D-DATA-003|分组同父名称唯一，最大深度32；网关同样防环/深度32|旧全租户组名唯一，放宽到同父是明确工程规则；显示完整路径消除歧义。超深存量树不自动截断，需处置/调参并重新冻结合同|
|D-DATA-004|已被2026-10-27用户纠正覆盖：设备模板普通直接保存，不做草稿/发布/版本历史；仅引用物模型版本|原“工程取舍”的模板不可变接入版本及设备切版撤回；按DESIGN-CORRECTION-2026-10-27重写接口/DB/CLI/用例，不改物模型core|
|D-DATA-005|元数据软删除，已发布物模型snapshot不改不删（不包含已撤回的模板版本表）；有引用409；依赖不明时阻断|旧DeleteDevice会删历史，新owner边界不允许。历史保留/清理由telemetry/control政策负责|
|D-RUN-002|实时订阅用正式授权SSE；替换旧WS消费端，20秒心跳、100设备上限、游标恢复|这是目标协议选择，非已有实现。长期连接与权限撤销/背压由DM-RT-017验收，不保留旧WS运行兼容|
|D-OPS-001|批量创建最多100且metadata事务全有或全无；跨runtime关系批次按项提交与报告|不能谎称跨服务全批原子。用户导入先全文件预检，分批执行可恢复；每行稳定key；未知字段不吞掉|

这些取舍可以在DM-API-001冻结前以具体证据修订；修订须同时更新OpenAPI、DB、CLI和用例，不能只改一侧。

## 3. 剩余三项业务发布确认（扫码/产品激活已明确排除）

未决项不以默认“不迁”处理，本轮也不为了这些问题中断其余规划。签收需在实现前完成；表中负责人是责任角色而非虚构个人。

|决定ID / 任务|需要确认的具体事项|已知证据与可选边界|推荐/默认安全处理|责任角色、通过条件|
|---|---|---|---|---|
|D-PRODUCT-001 / DM-DEC-001|旧custom_commands/custom_control、非标准chart card、widget presets及遥测/属性下发无法完整映射为新COMMAND/资源时，哪些必须保留、哪些可不迁|旧custom_controls有instruct/control_type/content；图表旧ICardView[]不能直接放入新object；旧延期消息有类型差异。正常类型化物模型核心已支持，不把这些说成核心未做|先按真实数据生成差异明细；能无损映射就复用；不能表达者阻断，禁止置空。经产品明确确认的不迁项才能进入“经产品边界确认后不迁移”|产品+模型/runtime owner；逐源对象有目标方案或批准不迁、数量和验收；未决=0|
|D-PRODUCT-002 / DM-DEC-002|是否必须保留历史凭据揭示/复制与批量秘密交付；现存设备凭据如何连续接入及允许多长轮换窗口|旧详情显示凭据、模板有一型一密；某凭据xlsx导出仅service存在未见路由。仅摘要存储无法恢复旧明文，不能承诺“以后随时查看”|普通列表/导出永不含secret；需要揭示的类型用受控加密与独立权限，其他类型只轮换。迁移连续性与强制轮换窗口必须明确|产品+运行运维；按credential kind列恢复性/展示权/保留期/轮换策略，实际设备验收通过|
|D-PRODUCT-003 / DM-DEC-003|支持哪些来源版本/部署形态、维护窗长度、元数据/审计/导出/历史保留期限和失败恢复目标|历史QA副本工具存在，不是生产安装器；本轮未读运行数据库、未测全量耗时；不能把旧计数当当前规模|指定源版本矩阵，先副本演练测窗口；切换前冻结元数据写，遥测独立保留；切换后以前向修复为默认，不无损验证就降级|产品+发行/运维；支持矩阵、RTO/RPO、保留政策及恢复演练报告签收|

## 4. 技术发布阻断（已有答案方向，不再向业务询问）

- 以物模型已合并的main-1.3.0为复用基线，逐文件选择最小实现集合；当前main/master工作区与1.3不同，不整树合并。
- 清除metadata runtime projection、l3 legacy代理与CLI旧路径；所有调用者直切正式合同。
- 修复可信身份正规化碰撞、角色/资源权限缺口、复合tenant FK、删除并发引用fence、分组防环/构树顺序问题。
- 完成不可变接入模板版本、provider配置schema、可靠generation采用、凭据/注册闭环及全部runtime依赖。
- 通过真实PG/HTTP、浏览器HAR、CLI、模拟设备、消费者、全量迁移和安装/升级矩阵；缺某owner真实环境则发布blocked，不用mock报告替代。

**已确认D-PRODUCT-004/DM-DEC-004**：社区版无企业版产品管理激活、扫码激活、按编号领取资格；旧禁用入口保留禁用，不新增正式资格操作或CLI。物模型已合并，自动化告警后置接入，不再向产品方询问这个排除事项。发布阻断按本阶段blocking=true任务评估。

## 5. 2026-10-25补充的工程决定

|ID|决定|范围/理由/验收|
|---|---|---|
|D-API-006|跨owner引用生命周期在同一OpenAPI正式定义，内部5操作仅受信服务身份可调|补齐原DATABASE §5.1未给schema的实现缺口；DM-API-011提供操作与六条故障/越权用例，不新建服务或兼容层。|
|D-DATA-006|引用唯一键纳入source_revision，并按referenceId+revision释放|来源r2先登记/确认，再释放r1；迟到r1请求不伤r2；预留超时仅触发核对，不能绕过删除保护。|
|D-EVIDENCE-001|保留原187任务ID，新增1个可独立实现的公共协议任务|188任务/109操作/580测试规格是规划规模；没有新增产品通过、页面切换、数据迁移或发布证据。|

**D-SEC-002（2026-10-25源码闭合）**：Community用户角色限SYS_ADMIN/TENANT_ADMIN；只读/资源scope是能力测试维度，不借本项目恢复TENANT_USER或新增角色管理模块。证据与映射见API-CONTRACT §4、REVIEW-2026-10-25。

## 6. 2026-10-26 Encore路由冲突的合同修订

**D-GRP-001：**保留`GET /api/v2/device-groups/{groupId}`作为分组详情路径；将分组树操作`getDeviceGroupTree`改为`GET /api/v2/device-group-tree`。Encore实际编译器拒绝同一父路径下静态`/tree`与参数`/{groupId}`并存，错误E1094。此次修订只调整树查询路径，保持operationId、请求/响应schema、权限、原DM-GRP-001/002任务ID和TC用例不变；已同步OpenAPI、API-CONTRACT、api-operations.json、CLI和TEST-PLAN。完成证据仍需真实HTTP检查。


## 7. 2026-10-26 Encore认证引导错误边界

**D-API-004-001：** Encore auth handler在请求进入service handler/middleware之前拒绝凭据时，401 body保留Encore原生`application/json`（`code/message/details`），不承诺RFC9457字段或`requestId`；进入业务handler后的参数、授权及业务错误仍统一使用RFC9457 Problem。保留Encore `auth`唯一认证入口，不改为public、不复制/重复认证，也不做401 body格式转换。真实HTTP验收发现Encore原生401缺少`WWW-Authenticate`；因此对外ingress必须为该401附加Bearer challenge后才能发布，ingress不读取token、不重复认证、不改body。此决定将目标wire contract升级至`2.0.0-plan.4`；OpenAPI 401响应同时描述Encore认证引导错误与handler生成Problem。

依据：Encore v1.30 runtime `runAuthHandler`在错误时直接写响应并停止转发；Gateway仅在`proceed=true`时转发，service middleware无法包装。RFC 9110要求服务器生成的401包含适用的`WWW-Authenticate` challenge；本地实际Encore 401无此头，因此标准challenge由外层ingress补齐。详见EVIDENCE 2026-10-26认证失败响应边界与真实HTTP验收。


## 8. 2026-10-26 Community共享JWT现有claims边界

**D-API-002-001：** 本版复用Community既有共享用户会话JWT：固定HS256，`id`、`tenant_id`、`authority`和正数`exp`为必需字段；若`nbf`出现必须校验；每个请求仍向Community session owner校验当前token未撤销/冻结且用户ID、tenant、authority一致。现发行器不设置`iss/aud/sub/nbf`，故不要求这些缺失字段，以免拒绝当前及其他旧服务共用的合法登录token。将来增加issuer/audience需先评估全部JWT消费者与存量会话过渡，再单独升级合同。此决定将合同升级至`2.0.0-plan.4`。

依据：Community `pkg/utils/jwt.go`定义唯一HS256签发器与30天exp；`internal/service/sys_user.go`填充实际claims；WebSocket/其他服务及Kernel均复用该token；Kernel session check每次向Community确认会话和身份。see EVIDENCE 2026-10-26 JWT issuer contract audit。

**D-API-002-002：** 认证身份与租户授权上下文分开。Community正式`SYS_ADMIN`可签发空`tenant_id`；Encore应允许该有效身份通过签名和live-session校验，但不给它隐含`*` scope。没有经过验证的可撤销、短时、可审计actor→target tenant委派上下文时，SYS_ADMIN不得访问任何tenant-scoped API，无论JWT的`tenant_id`为空还是非空。`/api/v1/user/transform`签发目标用户自己的token且不携带原actor/delegation，因此不视为管理员委派。

依据：Community bootstrap与登录/JWT/session/transform源码链见EVIDENCE 2026-10-26 SYS_ADMIN tenant context audit；Kernel `mustTenantID`与ThingModel `principalForData`均拒绝空租户。此次只调整Encore对有效空租户SYS_ADMIN的身份解析；不新增委派接口或改变Community token signer。
