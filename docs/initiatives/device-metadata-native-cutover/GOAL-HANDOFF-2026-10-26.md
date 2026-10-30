# 三菜单原生改造：新对话持续目标交接

> **当前设计优先约束（2026-10-27用户纠正）：设备模板不做草稿、版本表、发布或模板切版；只有物模型保留版本。** 此前相反的候选设计已撤回。原188/580/108是历史规划规模，不能再作为拒绝收简的约束；接口/DB/CLI/用例正按新边界重写。详见[设计纠偏与影响检查](DESIGN-CORRECTION-2026-10-27.md)。相关旧测试仅保留历史证据，不证明新需求完成。


日期：2026-10-26。用户要求把剩余开发交给一个**新的对话，以 Goal 模式持续执行**，核心原因是避免每完成一步就重新拟定下一步，逐渐偏离原任务书。旧 Goal 已由用户清除。

本文件是执行交接与状态记录，不新增产品范围，不替代 TASKS、OpenAPI 或测试计划。新对话接手后成为下列三个隔离工作区的唯一写入者；旧对话停止业务开发。

## 1. 第一动作：在新对话建立 Goal

先调用 `get_goal`；没有目标时调用 `create_goal`，不要只在文字里说“目标已建立”。用户没有指定 token 预算，不设预算。创建后用 `get_goal` 核验 active 状态，再直接开始实施。

建议传给 `create_goal` 的完整 objective：

> 按 ThingsPanel Community Edition 的 docs/initiatives/device-metadata-native-cutover 原任务书及用户已确认边界，持续完成设备、设备模板、设备分组三个菜单本阶段的 Encore 原生 API、数据库、权限、前端、CLI、迁移工具与真实验收。以本交接列出的 1.3 隔离工作区接续已有未提交候选，复用已合并的物模型和模板创建设备能力。原 TASKS.md/tasks.json、openapi.yaml、DATABASE.md、TEST-PLAN.md、DECISIONS.md 和最新 REVIEW 是任务与验收依据；沿原任务 ID 和用例 ID 逐块端到端完成，每块同步代码 SHA/差异、实际测试、任务状态和剩余问题。不得另造范围或把部分测试当整项完成。保护其他开发者和原脏工作区；自动化/告警核心及其工作树由另一任务负责，DM-RT-008/009在正式合同和环境就绪后接入，不阻塞三个菜单主体；引用无法确认时危险删除/切版必须拒绝。排除企业版扫码/领取激活资格，保留社区版普通编号校验及现有状态语义。持续选取依赖已满足的原任务实施；局部阻塞时推进无关的可执行工作。完成条件是本阶段 blocking=true 的任务均有符合原验收条件的可复现证据，三菜单真实 API/PG/前端请求/CLI/迁移演练通过，未决事项已由用户明确处置且写回同一计划。未完成不得标 Goal complete；后置自动化联合验收、未获授权的生产迁移/发布不得伪称完成。

Goal 是对话状态，交接文档不是 Goal 的替代品。官方机制参考：[Using Goals in Codex](https://developers.openai.com/cookbook/examples/codex/using_goals_in_codex)。

## 2. 唯一任务来源和防漂移办法

原计划来源：[/Users/junhong/Downloads/code/thingspanel-backend-community/docs/initiatives/device-metadata-native-cutover/](/Users/junhong/Downloads/code/thingspanel-backend-community/docs/initiatives/device-metadata-native-cutover/)。

当前可写的实施副本就是**本文件所在目录**。开始接手时，原来源与实施副本的业务合同相同；实施副本多了最新任务状态/证据。本次仅同步了 `tasks.json` 中 DM-DB-006、DM-GRP-001、DM-GRP-002 的 state/evidence，使其与 TASKS.md一致；未改任务ID、scope、依赖、测试条件或OpenAPI。

必须先读：[TASKS.md](TASKS.md)、[PARALLEL-COORDINATION-2026-10-25.md](PARALLEL-COORDINATION-2026-10-25.md)、[DOMAIN-MODEL.md](DOMAIN-MODEL.md)、[openapi.yaml](openapi.yaml)、[DATABASE.md](DATABASE.md)、[TEST-PLAN.md](TEST-PLAN.md)、[DECISIONS.md](DECISIONS.md)、[REVIEW-2026-10-26.md](REVIEW-2026-10-26.md)。细节查 [API-CONTRACT.md](API-CONTRACT.md)、[CLI.md](CLI.md)、[EVIDENCE.md](EVIDENCE.md) 与 inventory，不要每轮重读全仓。

机器索引：[tasks.json](tasks.json)、[test-cases.json](test-cases.json)、[api-operations.json](api-operations.json)。当前为188任务、580用例规格、108操作（103公开/CLI、5内部服务）；184个blocking任务、4个后置/排除项。规格条数不是已通过用例数。

[goal-baseline-2026-10-26.json](goal-baseline-2026-10-26.json) 固定原始合同、任务范围摘要、三仓SHA及已存在改动的文件指纹。接手时核对；后续若原目录被其他审阅者更新，先比较并记录正式计划修订，不直接覆盖任何一方。

每个工作块必须遵循同一循环：

1. 在原TASKS中选依赖已满足的任务ID，写明对应operation、表、用例与完成条件。
2. 只做该块必要改动，沿API/存储/调用端把一个用户场景接通；不因某个外部依赖阻塞而停掉其他块。
3. 执行有意义的相关测试，按源码/真实PG/HTTP/浏览器/CLI/runtime/迁移区分证据。
4. 同步 `tasks.json` state/evidence、TASKS.md状态、EVIDENCE.md实测记录。只有原验收条件全部满足才能标完成。
5. 每轮恢复时先读本文件“当前执行位置”和相关任务行，再从同一主目标继续；不再生成一份新范围规划。若改合同，先说明理由与具体差异，并同步OpenAPI、操作索引、CLI和用例。

进度用大白话，报本块完成条件和整体剩余工作。不能把“3个部分实现/188”说成1.6%产品完成度；不能把“本轮没有新增关闭任务”说成1.3全部已有能力是0%。

## 3. 工作区、基线与写入边界

|仓库|接续开发的隔离目录|当前HEAD|
|---|---|---|
|Community backend / 计划记录|`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/backend`|`d638af3e84221b2bb29b1295deef9e72521b8aa3`|
|Community frontend|`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/frontend`|`7d331d15329af0d636265a8a97b9534fff435a21`|
|Encore Kernel / 原生API / CLI|`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel`|`fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`|

三个HEAD均为本轮已fetch并核对过的main-1.3.0精确提交，目前detached。接手重新确认Git状态；需要提交时只选本任务明确文件，保留其他人内容。不reset、clean、stash、强制覆盖、整树合并、push或发布。

`/Users/junhong/Downloads/code/{thingspanel-backend-community,thingspanel-frontend-community,iot-api-encore-kernel}` 是原共享脏工作区，不在那里实施业务代码。新对话虽然挂在backend项目，执行命令必须显式使用上表目录。

旧候选 `~/.codex/worktrees/device-metadata-native-cutover/{thingspanel-backend-community,thingspanel-frontend-community,iot-api-encore-kernel}` 基于旧main/master；旧Kernel HEAD不含37fbb5e8，不能整套搬入1.3。只在需要时逐文件只读参考，默认不使用。

自动化告警工作线在协作文件中登记为 `tp-automation-rewrite` / `feat/automation-rewrite-20261023`（历史HEAD edb3e0d1，不保证最新）。不进入或修改对方工作树，不重写规则、场景或告警核心。

## 4. 已确认事实与用户业务决定

- 1.3物模型已经合并：backend `99c17f62`、frontend `1156db2d`、Kernel包含 `37fbb5e8`。本轮已核对祖先关系；用户确认模板选模型并创建设备可用。复用和回归，不重新开发物模型core或正常的模板创建设备流程。
- 设备模板菜单对应旧device_configs；旧device_templates是物模型/展示资源，Encore模板是接入模板基础。不要因表同名复制错实体。
- 模板选择物模型时没有普通/网关/子设备适配检查规则，不新增该限制。
- 设备详情只会有系统内置控件和模型绑定的可视化。若实际发现第三种，提供具体源码/数据证据并报告设计缺陷；不要再次询问不存在的“其他特殊控件”假设。
- 凭据必须按现有可用行为支持查看明文与手工修改。没有新增独立生成/轮换流程的要求；旧版有bug则修，有闭环缺口可补。权限、加密和审计仍按正式合同实现，不在普通列表泄漏秘密。
- 设备导入/新建默认归根组，沿已有社区业务意图。批量创建的具体同步/异步机制由开发者依真实规模和当前合同判断，用户已授权工程取舍。
- 企业产品管理激活、二维码扫码激活、按编号领取资格排除；不存在 `checkDeviceActivationEligibility` / `activation-check` 新功能。普通编号校验、已有状态、真实社区 `ActiveDevice` 行为不可因词语“激活”一并删除。
- 用户早先说明本地测试数据无需备份/旧版兼容、无需绞杀者模式；最新交接又明确要求迁移开发与验收。因此迁移工具、数据完整性和隔离环境演练在范围内；不得凭旧回答执行真实生产无备份迁移，也不把生产支持矩阵重新变成本地主体开发的拦路问题。
- 自动化告警由独立任务完成；本任务接到正式API、事件schema、权限、SHA、可测环境后再做DM-RT-008/009。引用未知时危险删除/切版保守拒绝；普通无关CRUD继续推进。

这些明确答案优先于DECISIONS中的旧“待确认”文字。需要把旧待确认状态按任务ID校正，保留来由，不重复询问用户已经回答的问题。

## 5. 已有能力与实际缺口

可复用：合并的 `thingmodel/{core,app,postgres,service}`、现有模板固定物模型快照和设备创建基础、前端原生模型资源/展示消费组件、现有CLI命令框架和业务操作库存。

尚需按原任务开发：正式v2合同与鉴权/租户映射/CAS/幂等；接入模板不可变配置版本；设备完整资料/凭据/绑定/runtime采用闭环；分组树及成员写入；三个菜单真实请求直切；CLI从 `/v1/devices/*` 等旧代理转为同一正式API；迁移工具和对账；真实PG/HTTP/浏览器/CLI/runtime及联合验收。不要把旧端点存在当成新合同已完成。

独立工作块仍按原ID：公共权限与合同、分组、接入模板、设备、各菜单前端、CLI、迁移/安装验收。优先把一个块从数据库到页面/CLI接通，再进入下一块；共享前置缺口只做当前块必要部分并保证同一owner，不复制第二套业务。

## 6. 当前执行位置：2026-10-26 15:29审计更新

本节取代最初“首个分组读侧候选”的接续状态。完整回顾、可用性、过程事件和执行分工见 [REVIEW执行审计](REVIEW-2026-10-26.md#执行审计2026-10-26-1529北京时间)，当前源码指纹见 [execution-audit-snapshot](execution-audit-snapshot-2026-10-26.json)。该快照保留累计候选，不代表均由同一agent在过去8小时新增。

|工作块|已存在、可继续复用|下一实质缺口|
|---|---|---|
|分组|8条v2路由；PG级create/PATCH/move/members、CAS/幂等/审计；GET listener签名JWT+stub隔离验收|成员读取/反向组查询、删除正式引用fence、设备scope；写HTTP/前端/CLI完整链路|
|身份/DB公共能力|JWT/session/role修复；0018–0022、复合tenant FK、tenant-local helper|目标新表约束、RLS/运行角色/池复用、每operation权限、统一客户端|
|接入模板/设备|1.3既有模型→模板→设备基础及旧路径事务修复|目标template shell+immutable revisions+device bindings；凭据/完整v2 API及调用端|
|前端/CLI/迁移|现有布局、Go框架、既有QA工具供复用|三个菜单实际切换、同API命令、全量副本演练与对账|

当前不是等待生产入口才能开发。隔离Linux镜像/PG/session stub已可跑GET；写HTTP尚未执行，当前角色仅SELECT且缺本轮合法设备fixture。无运行中的task runtime，无新增写grant。已有镜像构建副本的store.go落后于当前候选，最新模板HTTP必须重建。默认环境曾有一次encore check失败事件，细节留在EVIDENCE，不继续默认环境迁移。

由6.1 sol负责总体设计、关键依赖、共用代码、集成审查和验收；Luna只执行边界清晰的小任务。当前优先解模板不可变版本/设备绑定/客户端这条共享关键路径，并行推进分组调用端；保留全部原任务ID/验收，不再逐个旧SQL路径反复加固而长期没有页面/CLI进展。

最新增量：模板配置纯包已交稿并修复6.1边界审查问题；固定模型snapshot解析器候选已交稿；TypeScript生成/metadata sidecar通过严格编译；分组列表/共用增改弹窗已切native候选，待全项目与浏览器验收。原自动化/告警后置边界不变。详细SHA和新增HTTP证据见EVIDENCE末尾，TASKS菜单/Tab表已同步。

### 后续执行增量：18时后至本轮收口

- 自动化/告警不抢改；外部Workbuddy只读复核到`7c389ac0`（A27已提交，工作树该次clean），A22/23/24前端和A28–34及正式联调仍未交付。其自报343后端/CLI21/MCP18通过不等于本任务已独立复验，不提前整树合并。
- 分组CLI二进制16次真实HTTP→PG调用已通过，成员remove只证明缺引用owner时503拒绝；权限和runtime已回收。
- 前端同HEAD基线与候选各1213条诊断且本轮scope0；普通build仍失败，独立Vite bundle通过。已补native group/tree同源开发代理。详情子组/设置已切源码，成员Tab与selector的新native实现刚交根审；未做浏览器，不宣称三Tab可用。
- 0023目标模板shell/revision、草稿Tx store/CAS/rollback、旧模板11项事务回归已通过Encore隔离PG；0024设备身份schema39子例通过。S3暂空字段和旧列不表示最终目标已放宽，S6必须离线对账/收紧/退役。
- 共享关键路径增量：0025公共operation/items/artifact、0026唯一desired绑定及root公共非秘密幂等/审计Tx联合9项已PASS，日志`/Volumes/DevScratch/dm-schema-integration-20261026.log`。下一块进入device/template v2服务与native成员读取；设备Create/Update严格输入切片已派发。禁止反复打磨无变化旧路径。具体状态/证据与原ID见TASKS及EVIDENCE。

### 当前接续：目标读取已进入真实服务

- native设备list/get、分组成员GET、设备反向组GET已接；26项联合PG/权限、22次当前镜像真实HTTP与2条CLI PASS。新完整目标fixture/只读non-owner实例，signed JWT + session stub；不是真实Community登录或RLS。证据`/Volumes/DevScratch/dm-native-read-http-20261026-7jzu1spv/`，image `sha256:07c442d7357cc8f490d4bb6279f7b238524c71cd59334d18bf89fb3da4a3c076`。app/stub已删、私密JWT记录已删，新PG/image保留，旧资源没动。
- 0027网关关系和32层整子树/环/最终设备状态检查通过；每设备恰一binding与activation跨表约束、旧数据转换/最终NOT NULL/RLS依旧缺。
- 下一块直进DM-DEV-003/004与模板v2服务，不停在读取验收：设备Create/Update严格输入已过测试，使用同一metadata_write_tx、目标device/binding及固定revision；省略initialGroupIds仅在恰好一个root且有权限时默认加入，[]明确不归组；普通登记可无配置inactive。配置/凭据/激活必须接正式owner，依赖未知危险操作继续拒绝，不能造provider/ACK。写操作同时持久receipt/audit/CAS，加入组必须更新组version并同tenant授权。
- 分组3Tab/selector源码已切native，最新Vue bundle已exit0（typecheck旧错误仍在、浏览器未验）；设备详情与其他菜单页面仍待native切换，实际浏览器不可由此HTTP代替。任务与范围保持原188/580/108。

## 7. 完成证据和报告方式

每块记录：原任务/用例ID、实际repo SHA及未提交差异hash、环境、命令、结果、证据等级、失败与下一可执行任务。代码未提交就明确写“base SHA + working diff”，不能假造交付SHA；小范围本地提交可按任务授权执行，push/部署/生产迁移另需授权。

本阶段完成：blocking=true任务按原accept逐项通过或用户明确修订/排除并同步合同；前端抓真实请求；CLI同API；迁移演练账本与读回对账；权限、租户、并发、引用完整性有真实测试。缺真实环境的项保持未完成。后置DM-RT-008/009不阻塞本阶段，但自动化联合验收状态保持独立。

只在关键节点汇报，说明总工作块与当前块、完成证据和剩余风险。避免把临时发现演变为新大规划，避免重读全仓和反复跑无改动的已通过测试。正常Goal回合应继续实质工作；只有确实需要用户输入/外部状态且没有其他可推进项时才按Goal规则报告阻塞。


### 2026-10-26 原生设备创建编辑验收与下一执行点

- DM-DEV-003创建/DM-DEV-004编辑（005是删除，已纠正文中编号），普通登记与资料编辑正式路由已接；35项定向PG/权限、25次HTTP及5次CLI通过。新镜像79文件manifest固定，完整指纹/失败修复过程/限制在EVIDENCE最新小节。不是配置完整创建、激活或整菜单交付。
- 创建同事务写设备+唯一binding+初始组成员+组版本+逐组审计+device审计+receipt；PATCH同一Device强ETag，描述三态/整体替换/语义no-op不增版。配置provider与runtime身份owner未实现时503零写，继续在既有connector owner内补正式固定版本/schema，不新造第二设备事实源。
- CLI singular device四命令、frontend typed transport已就绪；页面仍未接。模板19字段单对象投影在79文件镜像后交稿，只有独立PG证据，不能算入该HTTP验收镜像或list接口。
- 浏览器接线最小根因已找到：5002静态页面正常；9999 Community用户/菜单bootstrap连接被reset，默认native14210也不是本轮API（404）。保留现有5002。下一轮根做隔离UI环境接真实native API，合成bootstrap必须明确标注且不替代真实Community登录/菜单验收；不再向用户追问技术入口。
- 原任务继续：先接模板正式list/get及draft CRUD/provider固定schema，再串设备列表/表单/设置页面与真实浏览器；运行页/凭据/作业/迁移等原阻断任务继续保留，不因普通CRUD成功取消。Workbuddy只读HEAD仍7c389ac08e69c92d9d26a4375218a99aa611b414，未达正式集成，不改对方自动化告警。

补充接续：template list/get CLI mock候选已交，未含于已验device binary；模板projection root修正后PG通过，列表批量聚合helper执行中。真实UI登录最小根因为install Community backend启动MQTT timeout/fatal导致重启循环（RestartCount817→819），9999端口映射正确；只读查broker下一步根定最小恢复，不能盲改前端proxy掩盖backend故障。


### 本轮接续点：模板真实读取与分组只读UI

模板GET list/detail已正式接，9项定向tests、58 HTTP和3 CLI PASS；86文件镜像sha256:28ab00b61fab89926692da975ca7b823497aec661c8b22a66cc3d1d2b4cdaca1，证据索引native-template-read-evidence-index.json。DTO实际19字段，已纠正20的描述错误。模板带封面仍503（file owner未实现），template create/publish/provider仍缺，不关闭菜单。

分组列表/3Tab/编辑回填/selector3→1搜索通过Cua只读组件验收，signed JWT+synthetic bootstrap，11个v2 GET200无旧metadata调用；无业务写入、无HAR/真实登录/完整角色验收。临时Vite5003、app/stub、私有JWT文件与Cua标签已清理，原5002保留。frontend共用requester及template typed transport随后新增，29回归/strict tsc PASS，旧UI snapshot不能代表这版helper已做浏览器。

下一6.1根行动聚焦两项，不再反复只读搜repo或叠nil接口：以既有108操作和owner分工实现file-storage两个资产操作/正向resolver；在pluginmgr正式发布链路补immutable provider version/artifact+schema digest和public参数/instance secret/per-device CFG分域，再完成模板写入与配置创建。实际SDK v0.1.0及clean Xiaomi/dirty HA源码已定位，见最新EVIDENCE。不要复制SVCR password/token到public parameters或把动态HA实体commands schema当provider静态schema。

真实Community登录缺的是旧install broker正式image、临时secret与匹配服务policy；已止于只读证据/未执行恢复脚本，不重建Broker core。其缺口不停止其他原任务。Workbuddy7c389ac有新的automation executor/alarm dirty，未完成不集成，不碰对方文件。188/580/108与184 blocking/4后置排除保持，Goal不complete不blocked。


### 接续：文件owner、provider固定目录与写UI

EVIDENCE新节“2026-10-26 文件owner与连接器固定版本增量”记录本轮实际71项定向回归+1项private owner/PG测试与125文件镜像；文件POST/GET/恢复与模板正向resolver、provider正式list/fixed resolver/private平台发布审计已集成编译，未当整菜单完成。新镜像4e72753b；source manifest25a99a8e。测试keyring必须部署注入；生产无fallback。两个owner独立DB最新migration已冻结，后续只追加。

当前Luna继续两条真实验收：1) 分组UI保存最初因只读role返回500/42501，建立独立最小写role，不改原只读role；devices/relation rowlock仅在tenant immutable trigger有效时授UPDATE(tenant_id)，收尾REVOKE/DROP，保留失败及恢复证据；2) 官方MinIO镜像不可拉，已核实源码commit后独立cache自建测试镜像，准备私有S3和两个新测试DB/runtime角色，不重试无效镜像、不问用户提供技术位置。Root继续模板写入/正式owner校验及整体验收，不扩自动化告警核心。

模板create/PATCH parser已过测试，完整草稿/Unicode/presence/null/高精度/嵌套重复键齐；正式事务尚未接。下一开发须保留现模型固定binding，校验provider发布版本和两参数域；含封面URL的幂等响应需要加密保护或owner受控cache，不能沿用明文metadata receipt冒称只存assetID。provider directory实例仍为真实空[]，实际部署及service_connector CFG发现/身份验证未接不能宣称可执行。Goal继续active，188/580/108保持。


### 2026-10-27 00时前后 Workbuddy A29只读复核

Workbuddy /Users/junhong/Downloads/code/tp-automation-rewrite 已由旧dirty A27推进到干净HEAD41ba8e1e9b1d625f2e44393ac6df5ffb35eaa818；父85e8cc12f6e521a147a6850de80d941044e5d5c4是Node-RED/Flink样例与smoke的A29提交。393tests/58endpoints/37smoke是其进度文档声明，本次没有复跑也未找到对应运行结果文件；Node-RED真实导入点击和A30调用方适配/告警联测仍开放。样例使用device_id和rule/policy revision，未证明native template/group/fixedmodel references。当前可进入限定范围的隔离集成候选，但DM-RT-008/009及DM-API-011五操作/fence不能算接通；没有合并或修改对方工作区。其边界不阻断本轮file/provider/普通元数据任务。


## 2026-10-27 06时前后接续：纠偏已进入代码

当前三步：1 核实原页面功能（已核实）；2 模板DB/接口/读取/绑定/CLI同步收简（执行中）；3 普通CRUD和页面真实联验（未完成）。不以旧188/580/108计算完成率。

- 菜单语义已二次核验：`frontend/src/router/device-route-mapping.ts`明确“设备模板”=/device/template=旧device_configs；旧device_templates对应物模型。Topic/Lua/auto_register/additional_info确属模板菜单已有Tab，不能因同名表误判而删除。撤回的是模板草稿/版本生命周期。
- 根新增候选0028迁移：draft列改configuration，设备关联template_id，独立model_id/model_snapshot_id保留原固定ThingModel版本，移除模板revision表。**不从MAX revision猜测当前配置**；原值NULL继续待明确离线转换。旧0023/0026 checksum不改，未应用生产。
- 在owned隔离PG用事务试跑0028并ROLLBACK通过；额外插入了真实ThingModel版本7的合成快照和配置设备关联，以防“保留模型数据”只是空表检查。检查模板表无draft/revision、已有当前配置不变、设备数量及模型snapshot引用不变、ThingModel snapshots整行EXCEPT前后无差异。此证据只证明迁移试跑，不证明业务API/前端完成。
- 日志`/Volumes/DevScratch/dm-native-read-http-20261026-7jzu1spv/template-correction-migration-check.log` SHA07dfb73311e474b32ae19c997e26337e3ee070e19ade6a95b625b1a68bcfde3c；结果`template-correction-migration-result.json`；迁移SHAd8af1040c3d88c1e971d54d8ba60ab78dbba5d4aa41900b9a425e2e7fe1773cd。原始试跑第一次断言JOIN写法错误已纠正，不冒充首跑通过。
- 根同步设备DTO/输入/读写与设备CLI为templateId；设备创建按当前configuration读取，模型选择解析为独立固定snapshot。CLI `go test ./...`实际通过（0.616s），Go/Encore新模型与schema回归尚待统一编译验证。配置创建正式owner callback仍未接，不记完成。
- Luna并行：模板store/parser/list/get及unit/PG tests去draft和latestPublishedRevision；OpenAPI/业务合同移除模板发布/历史和切版preview；文件HTTP/CLI在专用构建副本继续。三者未完成时不称整改完成。
- 构建阻塞根因已定位到本地Encore1.58.6 `cmdutil.WorkspaceRoot` 113-132：遇到worktree的.git普通文件，continue未推进父目录，CLI高CPU且空日志。采样确认卡在此处，非Go编译慢。停止本次自己的CLI进程后，Luna使用HOME普通clone（.git目录）构建，已进入实际go build。不改用户.git、全局daemon或CLI依赖版本。
- Workbuddy HEAD仍41ba8e1e（A29）；现在有未提交A30兼容适配/测试文件，本次只读未运行、不纳入集成完成。自动化/告警继续由原负责人开发。

### 2026-10-27 当前配置源码回归结果

根已实跑Encore定向回归：首轮因Volumes modcache读取权限未启动测试；改home modcache后48主测试通过、1失败、0skip。唯一失败是模板DTO移除latestPublishedRevision后旧字段数断言仍为19；改为18后单独补跑该失败测试通过，共49个不同主测试通过。不是整包或整菜单验收。覆盖0028实际测试DB schema、单份配置存储CAS/租户/回滚、模板list/get/设备计数范围、输入精度/重复键、设备输入/读写及新固定ThingModel绑定；新模型版本7/8可独立解析、快照仍不可变、模板选择改动不会移动既有设备模型快照，跨tenant模型引用被FK拒绝。

可复现日志：template-current-configuration-tests-attempt2.log SHA3ee5d88e21a3f0dee04e2894fd5f17b12649afa023d631348dfdba27cbf28b32；template-current-configuration-tests-repaired.log SHA08d37ed6848db8bf5ec84eefe5764ca75b7686805c0a869910e84a9f73f0c9dc；汇总template-current-configuration-test-result.json（均位于owned scratch目录）。当前图片真实HTTP已过create201/replay/409/GET/签名下载checksum/篡改与unsigned403/tenantB404，CLI与清理结果由Luna最后签收后补；不提前记整项完成。

## 2026-10-27 普通模板保存链路已取得真实接口、CLI和浏览器证据

根负责方案与审查，Luna分工执行，沿最新用户边界完成了一个实际切片：模板单份configuration直接创建/保存，设备按templateId创建；无模板草稿、发布或历史版本。内建MQTT不要求新provider发布目录，也不要求伪造version/schemaDigest。其它连接器owner、模板列表/详情全部Tab、删除/绑定等仍未整体验收。

### 实现与职责

- 新增原生POST/PATCH device-access-templates，复用可信租户/权限和同事务业务+幂等收据+脱敏审计；支持重放、同key异body409、旧ETag412、空PATCH不增resourceVersion、同tenant名称唯一。deviceKind沿原编辑规则不可改；普通领域前置条件409，与ETag412分开。
- 公开模板DTO为17字段，只保存/返回imageAssetId；FileAsset GET负责下载地址。私有ValidateTemplateImage仅做asset归属/purpose查询，不访问对象存储或写URL租约，已以objects/box为nil的真实PG方法验证。避免为普通模板保存另建加密capability缓存。
- 设备按当前模板配置创建，固定ThingModel snapshot独立写入binding；模板改模型7→8，旧设备仍7、新设备8。GET后deviceCount改变会使完整模板表示ETag改变，编辑前必须使用当前GET的ETag。
- 设备按templateId/modelId/protocol读取已改为bindings及current configuration，不再引用已删revision表/旧SQL别名；真实PG验证跨tenant过滤。
- CLI普通template create/get/update及device create/get已接；前端薄客户端与config-edit页面直接请求新API；表单标签用真实数组保留逗号，不用逗号切坏值；保留模型已发布版本选择、权限、提交防重和412输入保护。非MQTT配置的普通资料可编辑，协议部分当前只读；这是未完成协议接线的边界，不是移除旧功能。

### 本轮证据

- 聚合60个不同定向主测试经必要修正全部PASS、0skip；不是全仓或整菜单测试。索引：template-direct-save-evidence-index.json。初期模型测试使用了创建设备前的ETag，按真实表示变化改为GET后取新ETag；过滤测试两个问题是合成canonical模型缺fields及空slice/nil比较，均修测试而未放松生产校验。失败日志保留。
- HTTP12例+CLI5例实际通过：template-current-write-http-3911aaaf.json SHA20fde6bfd005be938863cecfb569a819c9a0e03421ee49f7efe3e2e38289a11e。包括201/17字段/Location、replay/409/PATCH/412/404/403/422、按模板创建设备、templateId+protocol查询与deviceCount读回。真实CLI五次exit0。
- 真浏览器CUA：POST201→GET200→PATCH200→GET200→刷新GET200；名称描述持久读回、单个标签mqtt,ui-check未被拆分，旧device_config写请求0。template-current-ui-cua-result.json SHAda3feecb986183fff82dda1c3da5ae424d7f57ab0d644591f1bbdcb19b255644；请求摘要SHA0e0d658c9a6f13508d94d76d7349c4e78163821c958e0eb5eb376d369752e2f6。使用合成TENANT_ADMIN，不代表真实登录或其它Tab。Vite公共壳logo/sys_version在隔离harness404，不影响本模板流程，不把其mock当业务证明。
- 仅测试镜像sha256:6a0ca863cdba82f7395629c0a03baebc37358239965b1921158a91984405673c。首次500不是业务SQL失败：Encore1.58.6 parse.go把databases对象KEY当物理DB名，entry.name当逻辑Encore资源名，原config倒置导致连接池无资源且PG无连接；只修外置完整runtime config后通过，无改业务源码或重编译。HTTP最终config SHA9e6713a010404ffe2607c9fa9242339aec9617796261570de607f7aeee92c0a9；独立UI轮config SHA7df25007aa10dcc15edcc9c989e5a4d09cde234cfd73986af3bd4bdcc903cf43。两轮copy DB/role/env/app/stub/Vite/owned浏览器页均已清理，原fixture DB/role/network不变。
- 新版OAS仍104操作；已移除模板发布/历史4操作和导出残留revision path参数。已有批量关联按1–100个设备的同步元数据事务设计，返回200 BatchTemplateBindingResponse，不再把新增async job作为业务必需；该批量写实现尚待完成。最终spec SHAc34adfc21f0ab76b179fd7e87af39f671d7d626cf308d075beed571a2f355fa8；TS已重生成。plan-current-configuration-audit-final.json为0error、1个历史baseline目录缺失warning；这里只证明计划交叉引用，不证明产品测试。原188/580为保留历史ID，不能充当完成率。

### 交接与剩余

TASKS/tasks.json/test-cases已将模板发布/历史撤回为非blocking，复制/原生包/新增管理启停标可选；已有Topic/Lua/注册/扩展/绑定与市场功能保留。Workbuddy新HEAD fbdd653d A30只是旧客户端兼容shadow适配，仍有A31迁移dirty文件，没有native metadata refs联测，不合并未完成代码。

下一执行点：设备当前模板binding读取API、前端薄client和CLI三线并行；之后普通绑定/解绑和关联设备页面。三菜单的其余列表/Tab、正式connector/runtime owner、真实登录、迁移对账与整体签收仍须继续；未标Goal完成。全部日志/结果位于 /Volumes/DevScratch/dm-native-read-http-20261026-7jzu1spv/。
