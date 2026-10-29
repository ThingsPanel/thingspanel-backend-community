# 代码、Git 与验收证据索引

> 2026-10-25已按当前三仓源码重新复核；本轮增量、Git状态与证据边界见[REVIEW-2026-10-25.md](REVIEW-2026-10-25.md)。原2026-10-23证据保留其历史日期。唯一目标合同已修订为2.0.0-plan.4；内部引用协议见API-CONTRACT §10.1。

## 2026-10-26：1.3隔离基线实施记录

本记录更新上文针对10月23/25日候选的历史描述，不覆盖原始仓库。原工作目录存在并行未提交改动，本轮未在其中写入。实施候选分别位于 `~/.codex/worktrees/device-metadata-native-cutover-1.3/{backend,frontend,kernel}`；当前 Kernel 基线为 `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`，Community backend 为 `d638af3e84221b2bb29b1295deef9e72521b8aa3`，frontend 为 `7d331d15329af0d636265a8a97b9534fff435a21`。三者均为 `main-1.3.0` 的远端精确提交，工作区分别隔离；自动化告警任务工作树未读写。

本轮首个独立工作块是分组数据库基础和读侧：Kernel 新增 `devicemetadata/migrations/0018_device_groups.up.sql`、`group_read.go`、`group_read_test.go`。迁移建立 `device_groups`、`device_group_members`，使用租户复合外键、根/同父名称唯一约束和稳定排序索引；分组列表支持父级/root/search/page/sort，树支持rootId/maxDepth≤32、稳定同级顺序及显式截断。读路由只接受SYS_ADMIN/TENANT_ADMIN；成员关系只作组织用途，不授予设备权限。告警统计返回null，因为自动化/告警owner尚未提供正式接口。

代码仍为未提交候选，HEAD保持 `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`。文件SHA-256：`group_read.go` `95bdc07e979faae7bb0114d6e3f920e8dad2db4423225dcd92d9a335c6234856`；`group_read_test.go` `e792804a522a5470e58ba9c3a55cbee5f18da86a31f92f5c2757632e8f509f2e`；`device_crud_test.go` `c30993430a10f9ded3a97b662c8ca8a80bec26c8ea1d735ae44784fd41c59c98`；`0018_device_groups.up.sql` `fc3c00ca5f2f5eb4945239ebd348203448cae21cce8b8ec87ae663f33b2d795d`。前端与CLI本轮未改。

**接口阻断：**合同同时定义 `GET /api/v2/device-groups/tree` 与 `GET /api/v2/device-groups/{groupId}`。Encore拒绝这两个路由同时注册，错误为静态段 `tree` 与参数段冲突。当前只注册列表和树；详情查询实现保留为未注册的内部方法。已向用户征求是否将详情改为 `/api/v2/device-groups/by-id/{groupId}`；收到决定前不改OpenAPI，也不把详情计为完成。

**本轮实测：**`encore test ./devicemetadata -count=1` 通过；包含分组树稳定排序/子树/深度裁剪3项用例、PostgreSQL分组路径与去重计数用例、迁移约束/跨租户成员用例，以及模型发布→模板固定旧快照→创建设备→模型再发布新快照后绑定仍不漂移的回归用例。该绑定回归使用devicemetadata现有数据库辅助函数，不等于37f原生thingmodel正式handler与前端完整链路验收。命令使用 `encore test`，不是直接 `go test`。`encore check` 完成应用图/源码编译、运行迁移并显示126个注册路由，但应用启动时独立的notification服务因缺少privacy配置退出，因此不能记为全应用检查通过。没有真实HTTP、前端浏览器/HAR、CLI、迁移旧业务数据或设备运行时验收。

**未完成/风险：**分组创建/更新/移动/删除/成员操作与并发环保护未做；数据迁移旧组及成员未做；resourceVersion写入/CAS、ETag返回、资源受限用户授权、组路径完整详情、10k节点压力未验；列表跨页为空时total准确性需补测试；模板删除与告警规则外部引用保护尚未接正式服务协议，危险删除不能放行。相关任务仍保持“部分实现/待做”，本测试只证明隔离数据库迁移及上述具体测试通过。

基线日期：2026-10-23。本文记录本轮**实际读到的工作区和候选源码**，不把历史进度文档、测试文件存在或远端提交当作当前页面切换、数据迁移或版本发布证明。总体任务和五条完成度证据轴见 [TASKS.md](TASKS.md)。

## 1. 检查范围与证据口径

本轮先读取三个实际目录的 Git 状态、HEAD、分支、upstream 和远端引用，再检查后端路由/API/service/DAL/model/SQL/初始化、前端路由/页面/表单/权限/真实请求源码，以及 Encore 元数据、物模型候选、迁移和 CLI。对分支差异使用只读 `git show`，没有切换工作树、fetch/merge、reset、clean、stash、提交或启动应用。

源码行号以本轮采样内容为准；`B/...`、`K/...`、`F/...` 表示**当时工作区文件**，未另注明时不保证等于 HEAD。`K@37fbb5e/...`、`F@ee5aab55/...`、`B@0853da3/...` 明确表示提交中的只读对象。附件已经将这些证据区分。

本轮没有查询业务数据库、执行应用测试/迁移、登录业务页面生成 HAR、连接真实设备或部署。因此，“测试源码存在”“历史报告说通过”“本轮静态文档校验通过”是三种不同证据，均不能替代本版业务验收。

## 2. 三仓实际基线

首次文件指纹基线时间为 **2026-10-23T20:02:37.340600+08:00**（UTC 12:02:37）。完整 HEAD 如下，不能仅用分支名标记交付。

|简称|实际目录|分支 / 本地 HEAD|已跟踪变更 / 未跟踪|upstream 缓存 ahead / behind|只读 `ls-remote HEAD`|
|---|---|---|---|---|---|
|B|`/Users/junhong/Downloads/code/thingspanel-backend-community`|`main` / `0e3ba8281f09c815dabcd03c634d17af845e950b`|43 / 527|`origin/main`：7 / 12|`be3d5eba822515d16f7556c7c9fc369dd5ccc363`|
|K|`/Users/junhong/Downloads/code/iot-api-encore-kernel`|`main` / `c9817436b7f0f41588542a97236517545b5f1d01`|12 / 14|`origin/main`：6 / 0|`f932178f0db4fd04d4c20a00bc6e5f6cc08e9b5c`|
|F|`/Users/junhong/Downloads/code/thingspanel-frontend-community`|`master` / `c6968c77662f41a642fba822ddc92bc308de9895`|16 / 21|`origin/master`：1 / 12|`e49f49acca69d318cf9ed32976e98bae8f97539c`|

ahead/behind 来自现有本地 upstream 缓存，本轮没有 fetch，因此不声称它与实时远端同步。远端 HEAD 只用于证明远端引用与本地不同，不代表远端内容已审计或已部署。B/F 的 origin 为 ThingsPanel 对应 Community 仓库，K 的 origin 为 `zjhong/iot-api-encore-kernel`。

### 2.1 与本任务直接有关的脏文件

下表列关键证据的真实工作区状态；全仓变更不是本任务成果。

|仓库/路径|状态|解释|
|---|---|---|
|B `docs/ENCORE-THINGMODEL-FIRST-RELEASE-PLAN-2026-10-16.md`|未跟踪|现有决策输入，不是本轮新增，也不是 HEAD 中已发布文档。|
|B `docs/ENCORE-THINGMODEL-DIRECT-CUTOVER-2026-10-18.md`|未跟踪|核对原生直切、候选和禁止旧依赖的范围。|
|B `docs/ENCORE-KERNEL-MIGRATION-DISPOSITION-2026-10-20.md`|未跟踪|历史处置建议；与当前用户禁止桥接的指令冲突处以本次要求为准。|
|K `encore-api/devicemetadata/{models,store,service,onboarding}.go`、`device_crud_test.go`、README|已修改|当前基础 CRUD 与测试源含未提交改动，不能只报 HEAD 后声称是该提交已验收。|
|K `encore-api/devicemetadata/{group_service,group_store,group_test}.go`|未跟踪|分组代码实际存在；须列入待筛选候选和缺陷复验。|
|K `encore-api/devicemetadata/migrations/0009_device_groups.up.sql`、`0010_device_runtime_bindings.up.sql`、`0011_device_name.up.sql`|未跟踪|不能把文件存在或编号较大当作运行数据库已应用。|
|K `cli/tpctl-go/main.go`、`cli/tpctl/README.md`|已修改|本轮审计的是当前 Go 入口和 shell launcher 说明。|
|K `docs/progress/METADATA_API_PROGRESS.md`|已修改|78/78 登记数来自这个历史进度口径，不是新菜单验收。|
|K `docs/progress/2026-09-17-device-metadata-strangler-handoff.md`|未跟踪|历史 handoff 包含旧 connector/投影运行方式，不能作为直切完成证据。|
|F `src/service/api/device.ts`、`src/views/device/manage/modules/device-readings-compact.vue`|已修改|当前真实请求与部分 UI 改动；本轮没有覆盖其内容。|

候选与已核验提交引用：

|引用|完整提交|用途与限制|
|---|---|---|
|K 物模型候选|`37fbb5e8e83c47fa98a1eea59fefa38ceff97737`|core/app、native transport、资源快照、0014–0016 和测试源；不同于当前 main 工作区。|
|F 原生模型候选|`ee5aab55ba604165d99a07734d92b2705e97e0c1`|核对新模型 helper 与仍在调用旧设备/模板入口的接线，不能凭候选名宣称三个菜单已切。|
|F 历史相关引用|`c428cc23522c7efba19c63153cbb2b4e8e8784f9`|只读核实引用；不自动合入或继承历史通过结论。|
|B 升级工具候选|`0853da31046d01d07a00af5690ec2300abf936cb`|`scripts/thingmodel-upgrade-v1/` 的 QA 副本 runner/converter/ledger/checkpoint/recovery；不是生产安装升级包。|
|B 历史相关引用|`cd5b5841b597d1c3293c1a9ba8a3de7ea11a860b`|只读核实引用；仍需按 DM-BAS-001 逐文件选择实施基线。|

## 3. 关键事实与源码链

详细页面/按钮/字段证据见 [FRONTEND-INVENTORY.md](FRONTEND-INVENTORY.md)，服务/表/路由见 [BACKEND-INVENTORY.md](BACKEND-INVENTORY.md)、[LEGACY-ROUTES.md](LEGACY-ROUTES.md)，Encore 和测试源见 [ENCORE-INVENTORY.md](ENCORE-INVENTORY.md)。下表提供规划结论的最短复核链。

|证据 ID|核实结论|主要源码/资料|对应任务|
|---|---|---|---|
|EV-001|旧“设备模板”菜单是接入配置，不是旧同名模型表|F `src/router/device-route-mapping.ts`：`device_config → /device/template`，`device_template → /device/thingsmodel`；`src/views/device/config`；`src/service/api/device.ts`；B `internal/model/device_configs.gen.go:14–32`|DM-DOM-001、DM-TPL、DM-MIG-006|
|EV-002|旧 `device_templates` 包含物模型描述/展示资源，模型字段和命令另有表|B `internal/model/device_templates.gen.go:14–31`、对应 model/service/DAL 与 sql；FRONTEND-INVENTORY 的模型消费者|DM-API-007、DM-DB-011、DM-MIG-005|
|EV-003|Encore `device_templates` 是绑定模型快照的接入模板；产品另引用模板|K `models.go:103/138/165`、`store.go:createDeviceTemplate`、`0001_init.up.sql`|DM-DOM-001、DM-DB-004/005、DM-TPL-005|
|EV-004|三个当前菜单仍调用旧 Community API|F API wrapper 与 request 默认 `/api/v1`/`code===200`；三个菜单页面实际 import/call；F@ee5aab55 `saveNativeThingModelBinding` 仍 PUT `device_config`|DM-FE-001—039、DM-QA-004|
|EV-005|现有 Encore 设备 facade 和默认投影含旧系统依赖|K `encore-api/l3-core/devices/service.go`、`internal/legacy.NewFromEnv`；`devicemetadata/legacy_projection_port.go`、`runtime_projection.go`、store 中 project/refresh 调用|DM-RT-012、DM-MIG-014|
|EV-006|现有模板 publish 只改状态，配置及 snapshot 指针仍可更新|K `store.go` 的 `publishDeviceTemplate`、`updateDeviceTemplate`、`bindThingModelVersionAdvanced`|DM-DB-005、DM-TPL-005、DM-DEV-016/017|
|EV-007|原生物模型核心与固定发布快照已有独立实现|K@37fbb5e `encore-api/thingmodel/{core,app,transport,postgres}`、`service.go`、0014–0016；九个 native HTTP 操作|DM-API-007；只补 transport/身份/消费者所需差异|
|EV-008|语义和图表资源摘要不同，模型取值须固定版本|K@37fbb5e `core/snapshot.go:BuildSnapshot`、`app.ChecksumResources`、snapshot `resources_checksum`、`SnapshotDetail`|DM-API-007、DM-TPL-010/011、DM-FE-012|
|EV-009|现 tenant 正规化可碰撞，且与候选完整身份映射不同|K `models.go:315 normalizePrincipalID`；候选 `canonicalIdentity`；不能仅换算法后忽略旧 FK|DM-API-002、DM-DB-002、DM-MIG-004|
|EV-010|现分组防环和树构造有需复验的具体缺陷|K `group_store.go:validateDeviceGroupParent`、`buildDeviceGroupTree`、`group_test.go`；旧 B `router/apps/device.go:196–233`|DM-GRP-002/005、DM-QA-002/003|
|EV-011|设备细功能包含改编号、服务实例创建、批量关系、多级网关和资格检查|B service/device.go、device_config.go、DAL；F manage/details 页面；旧 checkDevice 判断未激活存在性|DM-DEV-021—027、DM-FE-029—032|
|EV-012|模板包含 Topic、六阶段 Lua、资料字段、注册、超时和图片|B device_config/处理脚本/Topic API-service；F config 详情；资料字段不属于遥测模型|DM-TPL-017—021、DM-FE-023—027|
|EV-013|运行详情包含延期队列、诊断、在线历史、最新键删除、实时订阅、导出、模拟器保存/连接/发送|F details/manage 各子页、`expect-message.vue`、模拟器及旧 WS；B `router/router_init.go` 和相关服务|DM-RT-013—019、DM-FE-033—039、DM-CLI-018|
|EV-014|当前 CLI 设备多走 `/v1/devices`，模板只见投影命令，分组走旧版 metadata API|K `cli/tpctl-go/main.go:34/157/193/307`、README；shell 实际入口 `cli/tpctl/tpctl`|DM-CLI-001—018|
|EV-015|旧后端实际使用 Gin/GORM/PostgreSQL/Casbin；没有 GNQ 命名依据|B `go.mod`（Gin 1.10.0、GORM 1.25.7、Casbin 2.82.0 等）、router、DAL、middleware|DM-DOM-001；仅历史实现信息，不影响新业务命名|
|EV-016|旧启动流程可执行数据库初始化/升级脚本，故不能为盘点随意启动|B 初始化 PgInit/CheckVersion 与 sql 引用|DM-MIG-001/002；本轮未启动应用|
|EV-017|自动化/告警/市场/ThingsVis/看板/移动端/OTA/其他服务共享接口|FRONTEND-INVENTORY 消费者行、BACKEND-INVENTORY §消费者、LEGACY-ROUTES 调用反查|DM-RT-008—011、DM-FE-014、DM-QA-008|

其中 `K models.go` 等省略前缀均指 `K/encore-api/devicemetadata/`，不是仓库根的同名文件。上述代码事实和缺陷判断属于源码级证据；具体运行结果以 TEST-PLAN 要求的复验报告为准。

## 4. 五份指定历史文档的处理

|文档|已读取且用于本规划的内容|不能据此推定的事项|
|---|---|---|
|K `docs/progress/METADATA_API_PROGRESS.md`|历史 78/78 API/MVP 登记；2026-09-17 部分显式采用 compat connector/旧投影，旧前端入口保留|78/78 ≠ 三菜单已调用新 API ≠ 原生发布完成。|
|K `docs/progress/2026-09-17-device-metadata-strangler-handoff.md`|历史旧 connector、旧前端和 QA 环境关系；部分旧配置 fallback、模板向 device_config 投影；历史 QA 结果|环境/候选不同，不能把历史 E11/E12 或百分比带入本轮；本版禁止沿用该运行架构。|
|B `docs/ENCORE-THINGMODEL-FIRST-RELEASE-PLAN-2026-10-16.md`|物模型首版、核心语义和曾发现的消费者/资源缺口|不能仅按计划日期判断功能今天已完成或已迁移。|
|B `docs/ENCORE-THINGMODEL-DIRECT-CUTOVER-2026-10-18.md`|原生物模型唯一 owner、候选与直切原则、合法无模型场景|不能把模型候选等同设备/模板/分组全部切换。|
|B `docs/ENCORE-KERNEL-MIGRATION-DISPOSITION-2026-10-20.md`|已有 Kernel 成果的保留/迁移参考及历史数据处置|其中桥接/投影建议若与本次明确要求冲突，不继承；也不改写原文。|

`B@0853da3/scripts/thingmodel-upgrade-v1/` 的 README、blockers、converter/runner/ledger/checkpoint/recovery 和测试只作一次性迁移设计与可复用实现来源。旧图表数组、特殊控件和不支持数据必须显式报告，不用空值替代后称成功。全部原文保持原样。

## 5. 测试证据：存在与执行分别登记

|测试源/观察|本轮状态|后续验收归属|
|---|---|---|
|K 当前 `device_crud_test.go`、未跟踪 `group_test.go`|已读源码；未运行；树用例覆盖不足已具体列任务|DM-QA-002/003|
|K@37fbb5e `thingmodel/core/domain_test.go`|已读四类字段、typed 参数、枚举、snapshot、权限测试源；未运行|DM-API-007、DM-QA-001/002|
|候选 `transport_test.go`、`app/application_test.go`|已读 wire/缺失 vs 0、身份、幂等/CAS/资源事务等测试源；未运行|DM-API-007、DM-QA-001/003|
|候选 `thingmodel/postgres_test.go`|已读真实 PG 场景代码，包括不可变版本、tenant、receipt、outbox 失败、并发；没有本轮 PG 执行证据|DM-QA-002|
|K `cli/tpctl-go/main_test.go`|现有四个 onboarding payload 解析测试，不覆盖完整三菜单 HTTP/CLI；未运行|DM-QA-005|
|前端辅助函数/读取相关测试源|文件存在与请求链已核实；未运行浏览器业务测试，未采 HAR|DM-QA-004|
|历史 QA/升级报告|只记录其适用候选与环境，不转成当前通过状态|DM-QA-007/010|
|本轮文档静态核对|只检查 ID、依赖、schema 引用、文件链接、任务/API/CLI/测试规格一致性|见 [VALIDATION.md](VALIDATION.md)，不等于产品测试|

当前可复用成果的状态应写“已有代码，未完成本版验收”或“部分实现”，不写“已实现并上线”。未来每个完成标记必须附候选 SHA、测试命令/环境、可读报告和结果，页面切换另附 HAR，数据迁移另附账本和对账，发行另附制品/部署验收。

## 6. 本轮文件保护与交付范围

开始时目标目录 `B/docs/initiatives/device-metadata-native-cutover/` 不存在；写入前再次核实不存在。原有文件 SHA-256 基线覆盖 B 1,240、K 339、F 954 个条目，共 **2,533 个**；源审计采用明确的文件清单，不读取未授权目录或把混合工作区打包为交付。

为并行只读审阅而生成的源码副本包含 1,909 个选定文件及逐文件 manifest；源审计压缩包 SHA-256 为 `c9e914307bade1561553d64006a489aa42dbd83b5d05751f46ffe8fb02f2ed50`。源码副本和临时生成脚本不属于交付，不写回仓库。它们不能取代实施时重新采集 Git/候选基线。

本任务只新增本目录下规划 Markdown、唯一 OpenAPI 和机器追踪 JSON。不会修改业务代码、旧决策文档、迁移 SQL、部署配置或其他未提交内容。没有执行数据迁移、部署或 Git 提交。

最终写入后的逐文件指纹、Git 排除新增目录后的状态比较及远端/交付副本校验结果见 [VALIDATION.md](VALIDATION.md)。

## 7. 已核对的正式设计规范

检查日期：2026-10-23。版本取官方规范，现有旧 MVP style-guide 只是历史输入。

|规范|本次采用/用途|官方来源|
|---|---|---|
|OpenAPI 3.2.1|唯一 HTTP 合同；3.2.1 于 2026-09-10 发布；生成器实际支持需在 DM-API-001 固定验证|[OpenAPI 规范目录](https://spec.openapis.org/oas/)；[3.2.1](https://spec.openapis.org/oas/v3.2.1.html)|
|JSON Schema 2020-12 / OAS 对应方言|类型、必填、可空、枚举、约束与生成；未知字段策略明确|[2020-12](https://json-schema.org/draft/2020-12)|
|RFC 9110|方法语义、状态、条件请求、ETag/If-Match；不凭个人习惯发明 HTTP 规则|[RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html)|
|RFC 9457|Problem Details；本项目 code/requestId 等扩展明确写入 schema|[RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html)|
|RFC 3339 / RFC 9562|UTC 日期时间 / UUID；UUID具体生成策略由服务端固定|[RFC 3339](https://www.rfc-editor.org/rfc/rfc3339.html)；[RFC 9562](https://www.rfc-editor.org/rfc/rfc9562.html)|
|RFC 6585|428 缺并发前置条件、429 限流|[RFC 6585](https://www.rfc-editor.org/rfc/rfc6585.html)|
|RFC 7396（对照核查）|本合同**不宣称使用 JSON Merge Patch**；PATCH 为显式 application/json 局部字段协议，对象整体替换，null 含义按各字段定义|[RFC 7396](https://www.rfc-editor.org/rfc/rfc7396.html)|

分页默认值、业务状态、批量上限、幂等窗口、资源权限和删除冲突是本项目明示的设计决定，不冒称由这些规范规定。完整 OAS validator 和 Go/TS 生成编译仍是开工门禁，本轮静态检查不能代替该门禁。


## 2026-10-26：Encore分组详情路由与重复迁移错误复核

在隔离Kernel候选中注册`GET /api/v2/device-groups/:groupID`后，`encore check`明确报E1094：静态段`tree`与参数段冲突。按框架事实将树操作路径改为`GET /api/v2/device-group-tree`，保留`GET /api/v2/device-groups/{groupId}`；已同步OpenAPI、API操作索引、API契约、CLI和测试追踪路径。此路径合同修订保留operationId与原任务/用例ID。重新运行`encore check`后，Encore应用图及源码编译阶段完成，路由冲突消失；随后0018迁移失败，错误为关系`devices_tenant_id_id_unique`已存在。因此本次check不通过，未证明当前隔离DB的迁移状态。只读迁移源码检查显示0018是仓库中唯一创建该具名约束的SQL；来源仍待核实，暂不删除约束或用`IF NOT EXISTS`掩盖状态不一致。没有真实HTTP、浏览器或CLI验收。

## 2026-10-26：分组读侧合同与focused Encore验证

沿DM-API-004、DM-GRP-001/002补齐列表分页解析：缺省为1/20，显式0、负数、非整数、超过page=1,000,000和pageSize>100拒绝，不再静默钳制。Encore不支持`*int`作为query字段，因此实现内部以string接收并严格转整数；OpenAPI仍描述query wire值为整数。列表筛选total使用相同WHERE条件，并与页面项、祖先路径、成员去重计数在同一个只读Repeatable Read事务读取；超末页也返回筛选总数。详情读相同快照；分组详情与树ETag按返回JSON内容计算强校验值。

当前验证：在Kernel `encore-api/`执行`encore test ./devicemetadata -count=1`，整个devicemetadata包PASS（最近一次1.204s）；测试覆盖严格page/pageSize与maxDepth解析、UUID校验、构树、树ETag及真实PostgreSQL路径/成员计数。Kernel根目录`git diff --check` PASS。Encore测试应用图解析、编译并在其测试数据库应用0018迁移，但没有调用HTTP路由。单独的`encore check`已完成应用图与源码编译，随后在另一个本地检查数据库上因具名约束`devices_tenant_id_id_unique`已存在而失败；相同名字仅在0018迁移创建，来源和迁移账本状态仍待只读检查。不得把两条结果合并为“check通过”或“HTTP验收通过”。当前未提交文件hash：`group_read.go` `fda57a97e5aa2c132f5668654a8a86ccbca5f4125798dc0d7e4f9607784171a7`；`group_read_test.go` `5280f7e97a991cd4a85e4a9526a7df45b602c7820e957cd167750eaa5f171a05`；0018迁移仍为原候选hash `fc3c00ca5f2f5eb4945239ebd348203448cae21cce8b8ec87ae663f33b2d795d`。B/K/F HEAD仍为base SHA，未提交、未推送。

更新后的限制：未知query参数拒绝、非法UUID稳定映射、正式scope/租户授权、列表/树并发读快照专项测试、10k树压力、真实HTTP、受限用户、成员写操作/并发环保护、旧分组迁移仍未验收。自动化告警引用未知时高风险删除仍须拒绝。


## 2026-10-26：Encore检查库迁移账本只读诊断

Luna agent通过`encore db conn-uri device_metadata --env=local`与`--test`分别只读连接（查询包在只读事务中并回滚；未输出DSN、未写库）：local环境的`schema_migrations`记录version 18 `dirty=true`，且`devices_tenant_id_id_unique`已存在，定义为`UNIQUE(tenant_id,id)`；test环境记录version 18 `dirty=false`且该约束存在。`encore test`使用测试DB，`encore check`此前迁移错误发生在local DB，两者虽显示相同database name但连接上下文/账本不同。

当前候选`0018_device_groups.up.sql`先添加上述唯一约束，再创建`device_groups`、两个同级命名唯一索引、父子租户复合外键、成员表及租户设备复合外键和索引。`dirty=true`加上第一个约束已存在，足以解释再次执行时出现`already exists`；但不能证明后续DDL全部应用。没有执行drop、reset、重跑或手工改账本，也没有证明local DB可安全标记迁移完成。恢复前必须只读比对0018全部表、约束、索引的定义及迁移失败记录；在确认完整之前保持check为环境阻塞，并继续使用可通过的隔离test DB做包级验证。


## 2026-10-26：Encore typed endpoint 请求/错误边界核查

Luna对本地Encore v1.30 API与middleware源码及官方文档作只读审查：当前三个分组GET均为typed endpoint，只能看到声明过的query字段；`encore.Request`/现有middleware没有提供原始URL query，因此它们不能可靠枚举并拒绝未知query参数。若严格拒绝是硬要求，需使用raw endpoint读取`r.URL.Query()`并白名单校验，代价是失去自动typed schema，需要另行维护OpenAPI/路由验证；替代方案是经确认由前置网关执行校验，不能假称typed handler已拒绝。

成功响应目前各自生成`Meta.RequestID`；typed handlers返回Encore `errs.Error`时，标准序列化为`code/message/details`，不会满足合同中Problem顶层`requestId`。仓库的RequestID context helper未见注册到Encore middleware。可复用typed端点的中间方案是在端点middleware统一生成并传递requestId，成功meta复用它，错误在`details`携带该值；但这仍不符合当前Problem顶层字段合同。尚未改API边界或合同，故DM-API-004和分组GET验收继续为部分实现。参考：[Encore API定义](https://encore.dev/docs/go/primitives/defining-apis)、[Encore API errors](https://encore.dev/docs/go/primitives/api-errors)。


## 2026-10-26：分组只读raw HTTP边界实现

在isolated Kernel候选中将列表、详情、树三个GET改为Encore `auth raw`，保留path/method和`groupPrincipalForRead`；分组查询/SQL与Repeatable Read读侧逻辑保持独立内核函数。raw边界按各route白名单拒绝未知/重复/空query及错误编码；未知key使用固定detail且不回显输入。成功body复用同一handler requestId，ETag只写响应头。handler内错误采用计划OpenAPI严格要求的RFC9457七字段`type/title/status/detail/instance/code/requestId`、`application/problem+json`及匹配HTTP状态；5xx detail固定，不序列化内部错误详情。

Encore v1.30 `auth raw`临时探针通过应用源码编译；认证上下文由已标auth的endpoint继续交Encore auth handler提供，随后仍由原principal/tenant guard校验。本地候选验证：`encore test ./devicemetadata -run 'TestParseGroupReadQueryStrictly|TestGroupReadProblem|TestGroupReadSuccessKeepsRequestIDAndETagHeader' -count=1` PASS（0.528s）；覆盖allowlist/重复与空值/长未知键、Problem字段和状态、500信息隐藏、requestId及ETag。`git diff --check` PASS。未做真实token HTTP；在认证handler拒绝并未进入raw handler时，Encore生成的认证响应是否满足Problem/requestId仍未验收。raw route不再自动生成typed Encore schema；本项目唯一规划wire contract仍在`openapi.yaml`，其中列表/详情/树路由已定义，本次没有改合同。

当前候选哈希（不代表提交）：`group_read.go` `7a0466711c70bf08c68d499c1bc7cb536484a3fe6bc1165c34e73afca5917ea4`；`group_read_raw.go` `d5b11be2a5f36c443cb1733f7d39a7f4b816b1e6ca336a43bdeb8f26d6706e01`；`group_read_test.go` `77fb84b25d3e61834e0a1008ac3c028eac7fe2ec3ed6fe44cf34e20fe3036ddc`。既有`device_crud_test.go`修改和0018迁移候选均未碰。


## 2026-10-26：认证失败响应的Encore运行时边界

Luna按Encore v1.30 runtime/auth源码与官方auth文档只读核对：auth handler拒绝token时，runtime直接写错误并返回`proceed=false`，Gateway不会转发到service；缺少auth UID检查也早于service middleware。Raw endpoint middleware无法改写handler/Encore已写出的响应。因此端点内已实现的RFC9457只覆盖请求通过Encore认证后进入handler的校验与业务错误；无法覆盖认证handler拒绝响应。不能以public端点手动复制认证代替。若保留当前“所有4xx/5xx都必须Problem+requestId”合同，需要Encore外层ingress/reverse proxy增加纯响应转换并验证；若不增加外层包装，则必须在改合同前约定认证引导失败继续使用Encore格式。此处需用户决定，现未改合同。

认证源码证据：[Encore v1.30 auth.go](https://github.com/encoredev/encore/blob/v1.30.0/runtimes/go/appruntime/apisdk/api/auth.go#L122-L155)、[gateway.go](https://github.com/encoredev/encore/blob/v1.30.0/runtimes/go/appruntime/apisdk/api/gateway.go#L42-L64)、[handler.go](https://github.com/encoredev/encore/blob/v1.30.0/runtimes/go/appruntime/apisdk/api/handler.go#L143-L149)、[官方认证文档](https://encore.dev/docs/go/develop/auth)。


## 2026-10-26：0018 local/test目录对象逐项比对

Luna在Encore local与`--test`数据库各自的只读事务中比对0018全部预期catalog对象：两个数据库的`devices_tenant_id_id_unique`、`device_groups`列/PK/tenant-id唯一约束/name与sort约束、parent tenant复合FK、root/name与parent/name两个partial unique index、parent/order索引、`device_group_members`复合PK、group/device租户复合FK和device查询索引均存在且定义一致。两库唯一已知差异是migration ledger：local version18 dirty=true、test version18 dirty=false。0018只有DDL、无数据转换；catalog证据支持local schema已完整落地的判断，但不证明执行历史、当时事务边界或并发使用。未修改/重跑ledger，未drop/reset。若后续修复只应考虑确认无并发连接、备份、核实Encore dirty恢复流程后，将local ledger登记为完成；不得重跑DDL或删对象。


## 2026-10-26：认证引导错误合同边界定案（plan.3）

按“系统统一设计、职责清晰、方法标准”原则，保留Encore `auth`作为唯一认证入口；不把认证判断或错误body格式转交代理、不复制认证。合同区分：Encore auth handler在请求到业务服务前拒绝凭据时使用`EncoreAuthError`原生JSON `code/message/details`；业务handler开始执行后的认证/授权/参数/数据错误仍为RFC9457七字段Problem。外层ingress只补HTTP标准要求的Bearer challenge，不判断凭据、不改写body。OpenAPI 401响应同时描述两种content type；API-CONTRACT、TEST-PLAN、DECISIONS与当前合同引用升级为`2.0.0-plan.4`。这个边界尊重runtime职责并如实承认requestId边界，不再声称所有pre-handler错误有RFC9457。

验证：Python YAML parser成功解析OpenAPI 3.2.1；`info.version=2.0.0-plan.4`；401 content含`application/json`和`application/problem+json`；`EncoreAuthError`必需字段为code/message/details。`git diff --check` PASS。真实401 HTTP、WWW-Authenticate及requestId实测仍未做。

## 2026-10-26：真实Encore分组HTTP与RFC 9110 challenge验收

Luna在独立临时副本 `/tmp/codex-device-metadata-build-20261026` 用 `encore build docker --services devicemetadata,authn --os linux --arch arm64 --config /tmp/codex-device-metadata-smoke-20261026/infra.json` 构建镜像 `codex-device-metadata-check:20261026-arm64-5`（exit 0，linux/arm64）。仅创建独立Docker network、一次性PostgreSQL与本地session stub；数据库没有宿主端口，应用只绑定 `127.0.0.1:60759`。使用合成HS256 token和stub会话，无passthrough、无真实Community/session或正式数据库。候选worktree和原始checkout均未修改。

在临时数据库按序应用仓库中全部12个现存 `.up.sql`（0001–0007、0014–0018），均exit 0；数据库迁移至0018并确认设备组/成员与设备/物模型表存在。仓库没有0008–0013迁移文件，没有跳过任何现存migration。

真实HTTP结果：`GET /api/device-metadata/healthz` 返回200 application/json；携带合成JWT的 `GET /api/v2/device-groups` 返回200 application/json，`data.items=[]`, `page=1`, `pageSize=20`, `total=0`，带非空meta.requestId。后在同一临时tenant插入root/child两组：`GET /api/v2/device-group-tree?maxDepth=1` 返回200、ETag、`truncated=true`和`hasMoreChildren=true`；`GET /api/v2/device-groups/{rootID}` 返回200、ETag与root详情。已认证的未知query请求返回400 `application/problem+json`，含匹配HTTP状态、`invalid_argument`与非空requestId。以上验证Encore路由、auth handler、签名校验、同网session stub、PostgreSQL读取及业务raw handler Problem输出。无token列表请求返回401 `application/json`，body keys为`code/message/details`、code=`unauthenticated`；没有`WWW-Authenticate`，无token值回显。结果分别保存在 `/tmp/codex-device-metadata-smoke-20261026/http_results.json`、`followup_http_results.json`与`migration_versions.txt`。

继续使用同一合成root/child验证筛选：`rootOnly=true&search=smoke-root`只返回root且total=1；`parentId={rootID}&search=smoke-child`只返回child且total=1，均为200 application/json。筛选证据见 `/tmp/codex-device-metadata-smoke-20261026/list_filters_results.json`。一次本地响应解析脚本错误假设items嵌套结构而报错，未改DB；改用实际schema复验后两条请求均通过。

DM-GRP-002真实规模追加：在同一唯一合成tenant的临时DB把活跃分组补到10,000（1 root + 9,999 children），先核验该tenant范围内共10,000行。真实HTTP请求 `GET /api/v2/device-group-tree?rootId={rootID}&maxDepth=2` 与默认全树均返回200 application/json、ETag、10,000节点、`truncated=false`、0个`hasMoreChildren`节点，响应6,908,919 bytes。首次分别耗时0.188s/0.177s；脱敏重跑分别耗时0.196026s/0.112085s。此证据说明该本地候选在单次10k规模读取中返回完整树；不构成持续负载/并发容量测试，也不证明损坏源数据环、跨租户异常parent或受限scope安全。临时数据仍保留，未清理。可复核脱敏摘要及构造说明见 `/tmp/codex-device-metadata-smoke-20261026/scale_10k_results.json`（mode 0600）。

跨租户真实HTTP验收：在10,000组tenant A fixture上新增一条独立tenant B root组，确认两tenant共10,001行且B仅1行。仍只使用A的有效token：A以唯一私有marker搜索B组返回200、items=[]、total=0；用B组ID请求A的详情和树root均返回404 `application/problem+json`，detail及完整响应均不含B marker。证明分组列表筛选、详情和树root查找的跨租户不可见边界；不覆盖其他metadata API、写操作、受限scope或坏数据parent。临时B行保留，具体身份值不记录。

租户输入真实HTTP验收：A token附带指向B的`tenantId` query，返回400 `application/problem+json`，无列表数据且不泄漏B marker；A token附带`X-Tenant-ID`指向B并搜索A的`smoke-root`，仍只返回A的1行（200、total=1），无B组泄漏。该验收证明当前分组read路径拒绝未知租户query并忽略客户端租户header；不覆盖带body租户字段的写operation或所有65个auth routes。仅发送请求，未改DB/代码。 

## 2026-10-26：Encore 401 Bearer challenge ingress shim

因直接Encore runtime的401缺`WWW-Authenticate`，在Kernel隔离候选新增标准库模块 `deployments/device-metadata-ingress`。它只做反向代理响应装饰：上游401且没有非空challenge时添加`Bearer realm="device_metadata"`；保留上游已有challenge；不读取token、不复制认证、不改响应体、状态或Content-Type。代理传输显式不使用`HTTP_PROXY/HTTPS_PROXY/ALL_PROXY`环境值，避免把Authorization请求意外转发给ambient proxy。`main.go` SHA-256 `51681df7edd57ec27cfc438e7bc3eafb02c900d41acc205bf378d63a1c88a72e`；`main_test.go` SHA-256 `1266ccb53ca0e4830177e1395b73d0007eac7530cdc7122c4fa3a82cdff2d2d9`；README SHA-256 `28e4212ad9c53ca4c07ba92cc3d4e9e629f5b08f1fc01b7170b99194f1f35f7e`；`go.mod` SHA-256 `241b55dd39f8ece7426dbdde6a7af17065390314ed7d85bd91626cea711dcf23`。

定向验证：最终代码在模块目录 `go test ./...` PASS、`go vet ./...` PASS。测试覆盖401加challenge、已有/空challenge处理、非401不添加、HTTP_PROXY环境不生效，以及请求/响应body、状态、Content-Type和端到端普通头透传。最终真实隔离集成中，直接上游与代理的无token响应均为401 application/json、85 bytes且body SHA-256相同；代理仅补challenge。带token列表代理200/application/json（items=20,total=10000）；非法page代理400/application/problem+json且没有challenge。临时代理先后监听`127.0.0.1:65259`与最终复测端口`127.0.0.1:49991`，每次验收后均停止并确认端口关闭；Encore app/PostgreSQL/session stub保持运行。

限制：这是候选模块与临时直连应用的隔离验收，不证明真实产品部署的统一ingress已接入。本仓库仍无承载全部设备元数据Encore流量的共享ingress配置；该模块需要被实际统一ingress采用并在部署链路验收后，DM-API-004才可关闭。

标准判断：RFC 9110 §15.5.2/§11.6.1要求服务器生成的401包含适用的`WWW-Authenticate` challenge；RFC 9457定义错误表示格式，但不替代该challenge。保留D-API-004-001的职责边界：认证体保持Encore原生格式、业务handler错误保持Problem；修订发布合同，要求真正对外的统一ingress仅为前置401补`WWW-Authenticate: Bearer realm="device_metadata"`，不得复制认证或改写body。当前候选仓库没有适用于该Encore路径的共享ingress，直接服务的401因此仍不满足公开HTTP验收；不可据此次直连200宣称发布就绪。

构建与运行镜像ID、容器状态、完整HTTP响应结构和逐条迁移退出状态由临时证据文件记录；凭据位于权限受限临时文件，不纳入仓库。未记录/回显JWT、数据库密码或签名key。

## 2026-10-26：Community JWT签发契约核查与plan.4

Luna只读核对Community真实issuer：`pkg/utils/jwt.go`中的`UserClaims`含`id/email/create_time/authority/tenant_id`与`jwt.StandardClaims`；GenerateToken固定HS256并设30天exp；`internal/service/sys_user.go`登录路径不填iss/aud/sub/nbf。刷新/模拟登录仍复用该签名器，但Redis有效期更短；Community Gin、会话查询、WebSocket及多个服务消费者复用此共享用户token。Kernel `authn.AuthHandler`在验签后逐请求调用Community live-session接口，核对身份、租户、角色和有效状态。故不能在Encore端突然要求当前issuer不生成的iss/aud/sub，否则合法用户会被拒且破坏旧消费者。按D-API-002-001，plan.4固定HS256、必需正exp、可选nbf校验，且继续用session owner做撤销/状态/identity校验；iss/aud只在未来issuer版本升级后再引入。未修改Community共享checkout或签发器。

生产环境正式auth mode现只有运行进程`TP_AUTH_MODE`环境变量来源，隔离Kernel仓未发现权威部署注入及dev/passthrough启动拒绝证据；JWT key也必须由安全secret注入覆盖示例默认值。由此生产模式fail-closed仍是DM-API-002未闭合项。


## 2026-10-26：固定并执行OpenAPI 3.2.1全量校验器

增加独立工具模块`tools/openapi-validator/`，固定`libopenapi-validator v0.16.1`与`libopenapi v0.41.3`。普通CLI默认将`GroupTreeNode.children`递归引用判成无限循环；该结构是有限树的数组递归形状。工具仅启用`IgnoreArrayCircularReferences`，保留其他引用检查及完整OAS文档校验。

验证：在工具模块内执行`go run . ../../openapi.yaml`，输出`document passes all OpenAPI validations`。完整校验工具已固定；Go/TypeScript客户端生成、编译及响应媒体类型解码还没验收。普通CLI命令仍会报递归误判，应使用仓库固定模块入口。


## 2026-10-26：Community SYS_ADMIN身份与tenant委派边界

Luna只读核对Community源码：bootstrap SYS_ADMIN用户`TenantID`初始化为空（`internal/service/sys_user.go:1005-1016`）；正常登录在`UserLoginAfter`把user表tenant/authority直接复制到token（`:191-208`）；JWT签发器只设HS256及标准exp（`pkg/utils/jwt.go:13-35`）。所以正式SYS_ADMIN token可有空`tenant_id`。Community `/api/v1/user/transform`只接收目标`become_user_id`，检查目标和role后签发目标用户自己的普通token（`internal/model/users.http.go:117-119`、`internal/api/sys_user.go:267-283`、`internal/service/sys_user.go:735-805`）；新token不含原SYS_ADMIN actor、delegation ID、范围、期限或独立撤销状态，不能当作委派凭证。`NotificationIdentityApi.SessionContext`逐请求确认用户及Redis会话，但SYS_ADMIN空tenant只代表身份，不证明目标租户授权（`internal/api/notification_identity.go:22-67`）。

按认证/授权职责分离：Kernel parser现在仅接受Community正式用户role `SYS_ADMIN`/`TENANT_ADMIN`；空tenant只允许SYS_ADMIN通过身份解析与live-session检查，不注入隐式`*` scope。无已验证委派时，`devicemetadata.requiredTenantID`与ThingModel `principalForData`拒绝所有SYS_ADMIN租户访问，无论其token `tenant_id`为空还是非空；分组读取只授予TENANT_ADMIN。代码哈希：`l1-access/authn/auth.go` `342b876737b9059066f6bd2a20831780b7dd23054d06a59e706093c734dd631b`；`community_session.go` `800d272acf1d05549fd97eb93f7f500bf8c9169f520d43189ed2060f14bbb185`；`auth_test.go` `36d81905701d169c2316905413e1170e12f6b823015cfc6878b306ce292460e`；`thingmodel/service.go` `178282a702baa88343c276e4a8093e204030554316c7dc5e5ee6b3f8bba8ed63`；`thingmodel/transport_test.go` `853910f843f14e27b394394ce452c1aa367a3a503ea42f9ccc8db0b82fb2a2e9`；`devicemetadata/models.go` `307c4458e24d792fd08e912358ee983be879cfe6fb57a3c499ef006855ee504c`；`devicemetadata/group_read.go` `43ca6a86505a7690787dd7c2b7ac16800e741411d1395db36112b57496173458`；`devicemetadata/group_read_test.go` `e85799d5c76f977298eb2c11f59882a7109491407150fddf7b06e03fc37e2ecc`。

验证：`encore test ./l1-access/authn/... -count=1` PASS（0.419s）；`encore test ./thingmodel -count=1` PASS（0.514s）；`encore test ./devicemetadata -count=1` PASS（1.124s）。覆盖空tenant SYS_ADMIN认证、session身份匹配、正式role白名单、无隐式scope、TENANT_ADMIN授权、无委派的SYS_ADMIN（tenant_id空或非空）被metadata与ThingModel拒绝。剩余：Community尚无正式tenant delegation机制；SYS_ADMIN目标租户访问继续fail closed，需待auth owner提供短时、可撤销、可审计且每请求验证的delegation后实现。

该权限实现是DM-API-003的保守垂直切片：`devicemetadata.requiredTenantID`作为租户菜单共用入口先拒绝SYS_ADMIN（没有delegation对象可验证）；分组read仅保留TENANT_ADMIN；ThingModel principal adapter不给SYS_ADMIN和历史TENANT_USER/TENANT_VIEWER自动授权。它尚未实现API-CONTRACT完整的operation/action/resource matrix、受限service scope、列表和统计按device ACL过滤、UI按钮与CLI矩阵；DM-API-003仍为P0阻断。


## 2026-10-26：HS256与JWT时间claims严格验证

Kernel只改`l1-access/authn/auth.go`并新增`auth_test.go`：JWT header算法必须精确`HS256`；`exp`缺失、零、负数或当前时刻已到期（now>=exp）均拒绝；当前issuer如带可选`nbf`，则必须为非负整数且已生效。既有Community签名claims（id/tenant_id/authority/exp且无OIDC claims）仍可通过解析。测试用独立本地HMAC key构造真实格式token，不调用Community或数据库。

验证：`encore test ./l1-access/authn/... -count=1` PASS（0.270s）；`git diff --check` PASS。候选hash：`auth.go` `ae8868b10deb48819ec95314215ac7dac84eb5b356d37942dc664e8819568353`；`auth_test.go` `6fa9dcdfcc61d65cc5f366523128f7f7bd40239737275867654c67e293ecec3a`。尚未覆盖真实`AuthHandler`+Community session HTTP、dev/passthrough生产启动拒绝、tenant委派、UUID碰撞/数据迁移；不应把parser单测宣称为API-002完成。


## 2026-10-26：AuthHandler会话验证测试与不安全模式环境限制

Kernel `AuthHandler`级测试现以test JWT走真实解析和`httptest` Community session HTTP，验证active用户认证通过、身份/tenant不匹配拒绝、冻结状态拒绝、HTTP401撤销拒绝，以及bearer token不进入错误/日志。另用Encore runtime metadata限制`dev`与`passthrough`只允许`EnvLocal`/`EnvTest`；环境metadata不可用或其他环境都在AuthHandler请求阶段fail closed，`legacy-jwt`不受限。

当前验证：`encore test ./l1-access/authn/... -count=1` PASS（0.258s）；`git diff --check` PASS。候选哈希：`auth.go` `e59269615902f9c4791cfcdfa531e31b1a149a97b6ff16f54c7896d8c430501e`；`auth_test.go` `594e2bb2d521cc60ce7565b0def421ccd302fb00093fc0387d27bbb4daaf1ad8`。限制：不安全mode当前是收到认证请求时拒绝，不是进程启动时拒绝；测试用的Community HTTP是隔离httptest服务，尚非部署中的正式Community服务；issuer/audience字段按D-API-002-001不要求；身份UUID碰撞、tenant委派及生产模式运行验收仍未闭合。


## 2026-10-26：combined device-metadata package regression after auth hardening

After the JWT parser and AuthHandler test changes, reran `encore test ./devicemetadata -count=1` in the isolated Kernel Encore app: PASS (1.253s). This confirms the existing group read package still compiles and passes against the hardened authn package. It does not exercise real protected HTTP routing or production Community connectivity. `git diff --check` also passes.


## 2026-10-26：devicemetadata 租户边界覆盖盘点

Luna 对隔离 Kernel 候选的全部 devicemetadata 路由做了只读盘点：65 个 `auth` API 均经过 `mustTenantID`、`mustRawTenantID` 或分组读取共用校验入口；其中包括 5 个遗留迁移接口。3 个 `public` API 为健康检查、内置 meta-item key、全局字典读取，不读取租户私有数据。代码入口哈希：`encore-api/devicemetadata/models.go` `ed25d4dce2ad266a8118b36471707e933f52fe69aaa15fc65f4ccc6ba805ee81`；`encore-api/devicemetadata/group_read.go` `43ca6a86505a7690787dd7c2b7ac16800e741411d1395db36112b57496173458`；`encore-api/l1-access/authn/auth.go` `77760d871a42b777af24bf10839ff5e2d0d48c30f11d5dfb85cfdfff38c7c327`。

这证明每个受保护路由都进入租户上下文校验，不代表完整授权已经完成：现有覆盖没有替代每个 operation 的 action/resource 策略、受限服务 scope、设备 ACL 对列表/统计/导出的过滤，也没有覆盖 UI/CLI 权限矩阵。迁移写接口虽受 tenant guard 保护，仍需 DM-API-003 的逐操作策略与审计验收；DM-API-003继续为 P0 阻断。此前相关包测试 PASS 仅作单元/包级证据，不等于65个路由都已通过真实HTTP授权验收。


## 2026-10-26：分组 raw handler 错误响应测试

在隔离 Kernel 候选 `encore-api/devicemetadata/group_read_test.go` 增加 handler 层 `httptest`：未知列表参数、非法 `rootOnly` 布尔值、`rootOnly` 与 `parentId` 冲突、非整数或低于下界的 `maxDepth` 均返回 HTTP 400 与 `application/problem+json`；断言Problem状态与HTTP状态一致，含非空requestId、正确path，且未知tenant参数不会回显。该测试直接调用三个 raw handler，不启动Encore router/AuthHandler，也不访问PG；因此只证明handler解析/响应边界，不证明token/session、真实路由、DB读写或资源授权。

验证：`cd kernel/encore-api && encore test ./devicemetadata -run '^TestGroupReadRawRejectsMalformedQueryWithProblemResponse$' -count=1` PASS（0.678s）；完整 `encore test ./devicemetadata -count=1` PASS（9.512s）；`git diff --check` PASS。测试文件SHA-256：`encore-api/devicemetadata/group_read_test.go` `408cb69e1a771198c1d1b360c124c26485902e13bc829ed6accf3d0e79383348`。

当前本机 `tp-copilot-encore-remote` 是 `thingspanel/device-metadata-remote:local` 镜像，容器创建时间为2026-10-17；只读GET `/api/device-metadata/healthz` 返回200，但 `/api/v2/device-groups` 返回404 endpoint not found。该容器早于当前分组候选代码，不能用于本候选真实API验收；未重启或替换容器。


## 2026-10-26：分组 raw handler + Encore 测试 PostgreSQL 读回

扩展 `kernel/encore-api/devicemetadata/group_read_test.go`：通过 `et.OverrideAuthInfo` 为当前 Encore 测试注入 TENANT_ADMIN 身份和测试tenant，在 `encore test` 提供的 PostgreSQL 中插入临时数据并直接调用已注册的 raw handler方法。列表 handler 按唯一search marker从PG读回新建组，校验items/total/page/pageSize与requestId；树 handler对含一子组的租户子树请求`rootId`与`maxDepth=1`，校验HTTP200、ETag、`truncated=true`、`hasMoreChildren=true`且children为空。测试清理插入记录。

验证：`encore test ./devicemetadata -run '^TestGroupReadRaw(ListHandlerReadsPostgres|TreeHandlerReturnsDatabaseTruncation)$' -count=1` PASS（0.721s）；完整 `encore test ./devicemetadata -count=1` PASS（0.942s）；`git diff --check` PASS。测试文件SHA-256：`encore-api/devicemetadata/group_read_test.go` `97e9bba9fcef338e9415b76613d5fbb9357d92017407949ad851e41171f31629`。

边界：测试走真实PG与handler序列化，但直接调用Go handler并通过Encore测试身份注入；未通过HTTP listener/Encore router、真实签名Community JWT或Community live-session；也未测试32层完整树/10k节点规模，不能关闭TC-DM-GRP-001/002。


## 2026-10-26：分组树深度与输入顺序契约用例

补充纯树构建函数的原任务边界用例：32层链在`maxDepth=32`完整返回且不标截断；同一链在`maxDepth=2`于第二层显式设置`hasMoreChildren`并返回`truncated=true`；父先/子先/打乱顺序传入同一四节点三级树，JSON结构保持完全相同。该单测覆盖DM-GRP-002/TC-DM-GRP-002-01与TC-DM-GRP-002-04中可由树构造函数证明的部分，不验证数据库异常源数据、租户过滤、真实Encore路由或10k节点性能。

验证：`encore test ./devicemetadata -run '^TestBuildGroupReadTree(HonorsDepth32AndDepth2Truncation|IsIndependentOfInputOrder)$' -count=1` PASS（1.089s）；完整 `encore test ./devicemetadata -count=1` PASS（0.893s）；`git diff --check` PASS。测试文件SHA-256更新为`encore-api/devicemetadata/group_read_test.go` `41c89ab5f72fa9414fdd2c8463a6a50cdc715c677bf258bee11b93283460d296`。


## 2026-10-26：模板/设备真实Encore HTTP基础切片与空列表修复

Luna在目标Kernel候选 `/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel` 的隔离Encore app、session stub和disposable PostgreSQL上，经临时loopback ingress完成13个HTTP步骤，13/13返回200。初始tenant A的物模型、模板、设备均为空；之后依次通过正式业务API创建物模型、一个model item、发布snapshot、创建DRAFT模板、按query筛选模板（1条）、读取模板详情、发布模板、读取空设备列表、创建一台绑定该模板的合成设备、按模板ID读取1台设备。测试token、ID、模板名及voucher值未写入证据文件；记录仅包含URL形状、状态码、Content-Type和脱敏字段断言，证据文件`/tmp/codex-device-metadata-smoke-20261026/encore_http_template_device_slice.json`权限为0600。

该切片只证明最小读取/创建与模板发布状态路径。未验42/120条分页、排序、全部筛选、跨tenant模板/设备、受限授权、设备ACL、产品/网关/子设备、幂等、UI/CLI及真实Community环境。旧Encore `PublishTemplate`返回`PUBLISHED`不证明不可变模板revision合同；设备创建响应包含`voucher`字段（值未保存），之后的设备列表未返回voucher值。该创建响应需按API-CONTRACT凭据专用授权/交付边界另行处理，不能据此判为凭据安全完成。

真实HTTP初始空结果发现物模型、模板、设备三种`listResponse`把nil切片编码为`items:null`，违反`API-CONTRACT.md`约定空列表必须是`[]`。Kernel候选现给通用`listResponse[T]`加`MarshalJSON`，仅将nil `Items`编码为`[]`，分页字段保持；新增`list_response_test.go`聚焦断言通过。验证：`encore test ./devicemetadata -run '^TestListResponseEncodesNilItemsAsEmptyArray$'` PASS；`go vet ./devicemetadata` PASS；`git diff --check` PASS。`models.go` SHA-256 `852771687bc1360eacae146acf1d212efbe1558427526e984d360271cb64d148`（该文件仍包含此前已存在的tenant-role dirty diff，本次只新增列表序列化方法）；`list_response_test.go` SHA-256 `7d06dc744c5d8a3872f8cef8d7baf8475b0753ba1db5211735a9263186fdf730`。

修复后的真实HTTP复验未完成：尝试构建目标ARM64 Docker镜像约10分12秒无输出、无错误且无image tag；按主任务指示仅中断本次build（退出码130），没有清理磁盘、重启或改动既有app/PG/session容器。可复现命令：`encore build docker codex-device-metadata-list-array:20261026-arm64-1 --services devicemetadata,authn --os linux --arch arm64 --config /tmp/codex-device-metadata-smoke-20261026/infra.json`。因此单测证明Go JSON序列化规则；candidate当前真实Encore HTTP仍未证明修复后返回`items:[]`。

同轮Luna在上述准确Kernel checkout只读复核权限：`auth.go:178-190`仅接受Community authority `SYS_ADMIN`/`TENANT_ADMIN`；`models.go:265-284`的tenant guard只放行TENANT_ADMIN，SYS_ADMIN无经审计委派时拒绝；本checkout没有`native_device_contract.go`。虽然`authn.Data`定义`Scopes`/`AuthorizedDeviceIDs`，当前`devicemetadata`没有消费这些值的授权/设备ACL实现。group/template现有租户/角色门禁不等于operation/resource权限矩阵，DM-API-003继续P0阻断。此前引用其他checkout的`TENANT_USER`/`TENANT_OPERATOR`映射结论不适用于本候选，已排除。


## 2026-10-26：空列表修复改为查询层初始化；修复后运行态复验受阻

更正本文件前文关于通用 `listResponse[T].MarshalJSON` 的状态：该实现已移除，避免将序列化定制放到Encore泛型API响应类型。当前候选在数据查询层创建非nil空切片，覆盖物模型、物模型项、模板、产品、设备、迁移历史；custom-control查询原本已返回非nil切片。新增Encore测试库回归实际调用七个列表服务，断言 `Items != nil`、长度为0、`Total == 0`。

代码证据（Kernel候选）：`store.go` SHA-256 `37da65a90aaf57ea2a51999eafbf47ebefebfc035a4a937a524846e03bea7a5a`；`migration_service.go` SHA-256 `a987c4a47675bfd8be1be2c408d919b4c350221e31b4a0d131d92589d962ef95`；`list_response_test.go` SHA-256 `37f44c6912c765b8d7ef3695ec123e144225d43ce68932d7bc783034ac84febe`。`models.go` SHA-256 `ed25d4dce2ad266a8118b36471707e933f52fe69aaa15fc65f4ccc6ba805ee81`，其中保留先前tenant-role dirty diff，本次未覆盖。Luna已报告 `encore test ./devicemetadata`、`go vet ./devicemetadata`、`git diff --check` 均通过。

修复后真实HTTP复验仍未取得：Linux/arm64 Docker build 在约25分钟持续高CPU、无输出、无trace、无镜像后按本轮指令中断，exit 130，证据 `/tmp/codex-device-metadata-smoke-20261026/empty-list-build-final.json`；没有清理磁盘或触碰既有app/PG/session容器。另一次本机 `encore run` 在监听前被notification服务的Darwin硬门禁终止（`l3-core/notification/secret.go` 检查 `runtime.GOOS != linux`），因此没有向该实例发HTTP请求。临时namespace `codex-dm-list-array-20261026` 保留。故当前只能确认测试数据库服务层的空切片契约，不能声称修复后真实Encore HTTP已返回 `items:[]`。

此前模板/设备切片运行态记录仍是修复前基线；它观察到空响应 `items:null`，并不能验证当前修复。DM-API-004、DM-DEV-003、DM-TPL-001/003维持未完成。


## 2026-10-26：分组树对数据库环数据返回明确失败

Luna在Kernel候选 `encore-api/devicemetadata/group_read_test.go` 增加 `TestGroupTreeRejectsDatabaseCycle`：通过Encore测试PostgreSQL插入同tenant root A与child B，再将A.parent_id更新为B形成环；调用 `getDeviceGroupTree` 断言错误码为 `FailedPrecondition`。fixture清理在单个事务中先断开A的parent，再删除B和A，避免自引用FK阻塞；不清空或重置其他数据。

验证命令：`cd kernel/encore-api && encore test ./devicemetadata -run '^TestGroupTreeRejectsDatabaseCycle$' -count=1`，Luna报告PASS（0.548s）。测试文件SHA-256 `1e638a493963dc1e7867ba70452794f98a5ddeab289181922a01619af86d98bc`；候选HEAD `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`。这证明分组树读服务遇到实际数据库环会快速明确失败，不证明迁移工具预检已实现，也未覆盖孤儿/越tenant坏数据、HTTP路由、并发树修改、受限scope或生产迁移。DM-GRP-002继续部分实现。


## 2026-10-26：DM-BAS-001 三仓隔离基线采集

只读采集 backend、frontend、kernel 三个1.3 worktree，生成 [BASELINE-MANIFEST-2026-10-26.json](BASELINE-MANIFEST-2026-10-26.json)。manifest记录绝对路径、Git根目录、detached HEAD、tracked diff与untracked路径；每个变更文件仅记录工作树内容 SHA-256、状态分类和大小，不复制文件正文，也不读取或记录 secret、DSN、token。backend与frontend起始无tracked diff；backend有既存规划目录untracked文件，frontend无untracked文件；kernel有8个tracked修改及10个untracked文件（6个直接文件、4个在deployments/device-metadata-ingress/下）。三仓均处于detached HEAD。

采集前后逐项比较三个HEAD、分支/detached状态、tracked diff路径、untracked路径和变更文件SHA-256；除本计划目录内新增manifest及本记录/任务状态更新外，复扫一致。manifest自身不对自身计算哈希；其余变更文件的hash与复扫一致。未触碰三个源码仓文件，未暂存、提交、清理或重置任何改动。

**当前状态仍为部分完成，不能结案：**后续补充已逐项归属18个Kernel候选变更并明确排除验收shim进入生产包；工具链机器版本与仓库声明不一致且前端Node未锁定，数据库已应用迁移账本/checksum仍未核验，可发布实现包及历史SHA对待测制品的最终冻结/对照仍未完成。

计划交叉引用验证：运行 `python3 validate-plan.py`，第二次通过（188 tasks、580 test cases、108 OpenAPI operations、165 legacy routes；errors=0）。保留warning：`baseline_unavailable`，历史 `plan-original` 目录不存在；该warning不影响当前规划结构校验，但历史基线比对仍缺。首次运行发现DM-BAS-001 state前缀不符合校验器允许词汇，修为“部分实现：”后复跑通过。


## 2026-10-26：DM-BAS-001 候选文件归属、工具链与迁移指纹补充

[BASELINE-MANIFEST-2026-10-26.json](BASELINE-MANIFEST-2026-10-26.json) 已增加 Kernel 18 项变更文件逐项矩阵、三仓工具链版本和 Kernel 全部 `.up.sql` 文件 SHA-256。18项逐项映射到DM-API-002/003/004/007、DM-DB-006、DM-DEV-001/003、DM-GRP-001/002、DM-MIG-004/008、DM-TPL-001/016、DM-QA-002/003等原任务；没有 automation/告警/Broker/notification 变更文件进入这18项。Group读模型仅有 `alarmsObservedAt` DTO字段，没有发现报警查询或自动化实现变更。`deployments/device-metadata-ingress/`明确为隔离验收shim，矩阵将其排除在生产实现包之外，仅标注DM-QA-003/DM-API-004验收辅助。

工具链复核：backend `go.mod` 锁定Go 1.23.8，机器观察Go 1.26.2；Kernel Encore SDK为 `encore.dev v1.30.0`、Go directive 1.23，机器Encore CLI v1.58.6、Go 1.26.2；前端 `package.json` 锁定pnpm 10.8.0，但机器pnpm 10.26.2，Node v25.9.0且本仓未发现Node版本pin。Python 3.12.5为本机validator执行环境。以上锁定/观测值分列，不声称工具组合已冻结。

迁移历史仅依据当前源码文件、Git跟踪状态、文件hash和本地提交元数据：`0001–0007`为已提交的旧device-metadata migration源码；`0014–0017`为当前Kernel已提交的native ThingModel迁移系列（0014–0016来自本地提交ea42ed39，0017来自59a8bdca）。精确提交 `37fbb5e8e83c47fa98a1eea59fefa38ceff97737` 只改ThingModel application及其测试，没有 `.up.sql` 变更，因此不能把0014–0017说成由37f本身新增。`0018_device_groups.up.sql` 是未跟踪分组候选。另列3个已提交notification migration作完整性核对并排除。未连接数据库，所有文件的“已应用”状态均未核实。

**DM-BAS-001仍为部分实现/阻断：**当前18项变更文件已逐项归属并排除验收shim进入生产包，且文件与迁移源指纹已列出；但三仓仍是detached HEAD，Node精确版本缺锁定、实际Go/pnpm/Encore CLI与部分声明版本不一致，数据库应用账本/迁移checksum未核、37f提交的迁移事实与源分支迁移集成关系未冻结，且尚未由owner签定最终可发布实现包和范围。不得据此标记整版验收完成。


## 2026-10-26：DM-DOM-001术语映射核验与未满足用例

Luna只读按原任务三条用例复核：`DOMAIN-MODEL.md`已区分旧`device_configs`（接入/协议/凭据策略）、旧`device_templates`（物模型与展示资源）和Encore`device_templates`（固定模型快照的接入模板），并给出目标实体、API、表与唯一owner；`API-CONTRACT.md`禁止旧字段别名/旧响应兼容，迁移映射只进入离线账本；合法无模板设备在目标合同中允许空模型；`INVENTORY.md`与165条旧路由账本给出范围处置。因此静态术语/合同规划已明确。

仍不能关闭原任务：`TC-DM-DOM-001-01/02/03`在`test-cases.json`均为`planned_not_executed`，没有来自三类源对象的脱敏样本与迁移映射fixture；没有运行API对旧ID复用的拒绝验证；也没有合法无模型设备的创建后PG/readback证明。静态OpenAPI例子和字段可空声明不是运行态证据。下一步应使用受控fixture完成样本映射、旧ID拒绝、无模型创建与读回，并保持无模型对象不被补造产品/模型。DM-DOM-001保持部分实现和阻断。


## 2026-10-26：DM-GRP-003分组创建与同步持久幂等切片

实现只在目标Kernel候选 `/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel`，HEAD仍为 `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`；保留原有dirty/untracked文件，不暂存或提交。OpenAPI已有 `createDeviceGroup`、`CreateDeviceGroup`与必需`Idempotency-Key`，因此没有改动冻结API合同。候选最高迁移为0018；Encore隔离test DB先由编译准备命令应用了0019收据迁移后，遵守迁移不可变规则，另追加0020建共享审计表，没有改写/重放0019、reset/force 0018或访问local default DB。

`POST /api/v2/device-groups`现在由raw RFC9457 handler处理，body只接受name/description/parentId/sortOrder，`Location`、`X-Request-Id`和`data/meta/requestId`按201返回。鉴权先复用`requiredTenantID`，只根据可信`TENANT_ADMIN` role放行；客户端scope字符串不能授权，SYS_ADMIN没有委派时沿现有guard拒绝。父级必须是同tenant且未删除；父链最大深度32；数据库根名/同父名partial unique冲突映射为409。请求hash针对POST、正式规范路径和规范化body。

同一PG事务内按tenant+可信subject+operationId名+canonical path+key取得advisory事务锁，读/写`metadata_idempotency`，再插分组、补全201 JSON响应收据并插入一条`metadata_audit_events`成功事件。相同请求返回持久响应和同一资源ID；异请求体、过期key返回409；过期响应清空而保留key/hash tombstone；重放先重新走Encore auth和group write guard。审计事件使用`operation_id=NULL`、`resource_kind=device_group`、`before_version=NULL`、`after_version=1`、`outcome=succeeded`，`redacted_changes`仅记`{"created":true}`，不记录名称/描述。该写操作不发runtime事件。

实际验证（均在Encore test数据库）：

- 工作目录`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel/encore-api`，命令`encore test ./devicemetadata -run '^$' -count=1`：PASS，`ok encore.app/devicemetadata 0.508s [no tests to run]`；此步触发0019在隔离test DB应用。新增审计schema后追加0020，未回写已应用0019。
- `encore test ./devicemetadata -run '^TestCreateDeviceGroup' -count=1 -v`：PASS，7个测试；覆盖root/child与PG读回、保存响应重放、异参409、tenant/subject隔离、过期409 tombstone、根/同父重名409、缺失/跨tenant/软删除parent 404、32层接受/33层拒绝、12路同key并发唯一资源、审计失败时group/receipt/audit全回滚、raw HTTP handler role guard/Location/Problem 422。
- `encore test ./devicemetadata -count=1`：PASS，`ok encore.app/devicemetadata 1.187s`。
- `git diff --check`：PASS，无输出。事务回滚用例故意让审计`request_id`超过冻结varchar上限，预期内部SQL日志出现`SQLSTATE 22001`；断言分组、收据和审计均无半行。

本切片只经`encore test`隔离test DB和注入身份直接调用raw handler，没有启动HTTP listener或执行真实签名JWT/Community session端到端；没有进行真实运行态HTTP验收。并发用例为12请求，不等同原合同50路门禁。仍缺DM-API-003完整operation/resource/UI/CLI权限矩阵、DM-DB-002 tenant上下文/RLS和直接SQL验证、完整`metadata_operations/items/artifacts`及恢复worker/TTL治理、其他所有写operation、正式运行HTTP；DM-GRP-003、DM-API-005、DM-DB-010均保持部分实现。`metadata_idempotency`当前基础还缺operation FK、密文响应/密钥ID和lease恢复列；`metadata_audit_events`尚未做应用角色/RLS收敛，DM-DB-009不能关闭。

候选文件SHA-256：`group_write.go` `31bf2e79b80a4e4f9738d517a9378dd182433ba069fbced3e5e9ce6e59756a38`；`group_read_raw.go` `468ee7c88241a129ff97e655e04f0989766f6c47961e4d82c2c76e0d08c9ddf2`；`group_write_test.go` `68ac4d07c374b856c315db28e54187ac9904815cac21b276956cc882f38c93ef`；`0019_metadata_idempotency.up.sql` `2f14c6c97f0b1c54517a63f971899dc410d1074d85e013e5e974a7aaf08779c6`；`0020_metadata_audit_events.up.sql` `71d3071efdc7e92e6d50cb72038935dd5c394e37375f1da5bd7b6ed88fe952ec`。相应任务状态仅更新为部分实现；实际序号和剩余范围同步记录于`DATABASE.md` §5.2/§5.4、`TASKS.md`及`tasks.json`。

## 2026-10-26：Kernel候选清单刷新

刷新 [BASELINE-MANIFEST-2026-10-26.json](BASELINE-MANIFEST-2026-10-26.json) 中的 Kernel 矩阵：当前目标 worktree 共22个文件（8个tracked修改、14个untracked文件，其中 ingress shim 展开为4个文件），逐项记录状态、工作树 SHA-256 与字节数；review矩阵也逐项覆盖22个文件。迁移源清单共17项，新增0019幂等收据与0020审计事件候选；0019仅标注为已用于隔离Encore test DB，不代表local/default或生产数据库已应用。此次只读采集源码工作树状态，未访问数据库。Kernel文件矩阵规范JSON的SHA-256为 `598033007481c7ea408c9936eb750cb68e49b796435ab566b543a12f727f3b7d`，范围与逐文件hash以manifest为准。由于manifest本身也记录本文件的SHA-256，未在本文件中自引用整份manifest摘要；整份manifest SHA可从本次交付报告核对。


## 2026-10-26：DM-GRP-004分组资料与排序更新切片

按冻结任务和API合同实现 `PATCH /api/v2/device-groups/{groupId}`；未改OpenAPI、0019或0020。请求只接受 `name`、`description`、`sortOrder`，严格拒绝 `parentId`/未知字段；省略字段保持原值，`description:null` 清空。名称与同父live唯一冲突返回409；越tenant对象隐藏为404。通过现有trusted tenant-admin guard；缺`If-Match`返回428，拒绝弱/通配或格式错误的ETag，强ETag不匹配返回412；缺失/重复/异参Idempotency-Key按问题响应拒绝/冲突。

更新使用资源行锁和resource_version条件更新；同租户更新事务锁保证父分组名称参与的强ETag读回与提交顺序一致。真实变化时只递增一次resource_version并更新updated_by/updated_at；无实际变化不递增版本。成功响应返回200、更新后ETag、X-Request-Id与DeviceGroupResponse。相同幂等键重放返回持久原响应/ETag；异参冲突；旧key过期保留409 tombstone。组资料更新、metadata_idempotency收据与metadata_audit_events事件在同一事务；审计只存变化字段名，不写名称/描述值；审计插入失败回滚资源和收据。分组成员和parent关系不变。列表/树的同级相同sortOrder现在以ID稳定排序，避免名称顺序与合同冲突。

验证均使用Encore隔离test DB：`encore test ./devicemetadata -run 'Test(DecodeUpdateDeviceGroupBodyStrictly|UpdateDeviceGroup)' -count=1 -v` PASS（解析/边界、更新详情与树读回、排序并列、null清空、ETag/幂等重放、异参409、同父重名409、越tenant 404、428/400/422 Problem、审计失败全事务回滚）；`encore test ./devicemetadata -count=1` PASS（3.317s）；`go vet ./devicemetadata` PASS；`git diff --check` PASS。回滚测试故意触发request_id长度约束，测试日志中的SQLSTATE 22001为预期注入故障，断言无半提交。未运行Docker build或真实HTTP验收，未访问local/default DB。

候选文件SHA-256：`group_read.go` `4e9ac164ef58b778a83156504e4efef2da461594ead3571609a07d591da2da27`；`group_read_raw.go` `63e81ba29aa63963304164814e123a75b10dc8e408dd3089038037c32f28ffff`；`group_update.go` `c56e722bcafc0ead031aa9b626828ac48962968266150803260f1614437f12c0`；`group_update_test.go` `0186a5997e3d89ff1713eef6fa307dc349ac123bf0e17105f8dc88b6774b3809`。DM-GRP-004仍为部分实现；真实运行HTTP与完整DM-API-003权限矩阵/受限scope、DB-002非owner角色/RLS验收仍缺。

本切片新增 `group_update.go`/`group_update_test.go` 并更新分组读取排序后，基线manifest的Kernel矩阵同步刷新至24个变更文件（8 tracked修改、16 untracked）；逐项状态/路径/大小/SHA-256按manifest记录。该矩阵规范JSON SHA-256为 `4eb7c967aa18811c6ad0c4b88a7ef7325c7e8d4f39c759bfb4016eede706428d`。完整manifest SHA不在本文件自引用：manifest同时fingerprint本EVIDENCE文件；以交付报告公布原始manifest SHA。

## 2026-10-26：DM-GRP-005分组移动与并发环检测切片

在目标Kernel候选 `kernel/encore-api` 实现冻结的 `POST /api/v2/device-groups/{groupId}/moves`，未修改OpenAPI或0019/0020。raw handler要求可信TENANT_ADMIN、一个有效Idempotency-Key及严格的MoveDeviceGroup JSON；支持parentId为UUID/null，required expectedResourceVersion为正安全整数。缺字段/值非法按422，JSON形状/未知字段按400；资源版本冲突、树环和同父重名按409，foreign/missing/deleted组及parent统一404。成功返回200 DeviceGroupResponse与X-Request-Id。

为避免create、update、move相互竞态，三条树写路径统一使用“请求幂等key advisory transaction lock → tenant `device-group-tree` advisory transaction lock → receipt/资源访问”的固定顺序。create插入新节点、update改名会影响后代路径/ETag、move改变后代层级，全部在树锁下完成。move在锁内加载tenant组树并校验移动节点子树环与最大相对深度，校验新parent tenant/live、父链不含目标节点、最大目标深度32、目标parent下无同名活组；只更新被移动行parent_id/updated_by/updated_at/resource_version，因此group ID、成员关系和组权限不变。expectedResourceVersion执行CAS；同parent请求作为无版本递增的no-op保存成功幂等响应与审计。资源更新、24h持久响应receipt与审计事件在同一事务；审计失败时全部回滚。

实际Encore隔离PG测试命令：

- `encore test ./devicemetadata -run 'Test(DecodeMoveDeviceGroupBodyStrictly|GroupPrincipalForWriteRequiresTenantAdmin|MoveDeviceGroup)' -count=1 -v`：PASS；覆盖严格body解码、TENANT_ADMIN guard、含两层子树移动与稳定ID/子关系、合法祖先移动、same-key replay/异参409、同parent no-op、self/descendant/cross-tenant/missing-parent/stale-version/duplicate-sibling/深度>32拒绝、真实PG A→B/B→A并发恰好一方成功且递归校验无环、审计失败事务回滚无资源/receipt/audit半行。回滚测试故意触发request_id varchar(128)约束，日志中SQLSTATE 22001为预期注入错误。
- `encore test ./devicemetadata -count=1`：PASS，`ok encore.app/devicemetadata 1.495s`。
- `go vet ./devicemetadata`：PASS。
- `git diff --check`：PASS。

未实际创建非空device_group_members fixture，成员集合目前由实现的单行parent字段更新保证不受影响，但该项仍需专门读回测试；未启动HTTP listener做真实签名JWT/运行态验收。仍需完整真实HTTP、DM-API-003 scope/role矩阵、DM-DB-002 RLS/direct SQL限制、正式部署/发布验收。

Incident：曾误运行 `encore check`，命令目标为未明确指定的默认环境。它启动了migration 0018；首句因此前已知的 `devices_tenant_id_id_unique` 已存在而以SQLSTATE 42P07失败。local/default migration ledger当前状态未知，未查询复核，也未继续触碰、修复或清理。该失败不计为成功验证。本切片的隔离 Encore 测试均在误运行 `encore check` 前完成并通过；该 incident 后没有再运行任何 Encore/DB 命令。

候选文件SHA-256：`group_move.go` `d1a11a2b083bf235c5af499c2d5ce9056d40b379ecc26e586c54c4c8e71a93c9`；`group_move_test.go` `9edd2328d22523d544115d1b6475ced3101404a26d9de83d04a16ad9af8e5fda`；`group_read_raw.go` `3fdf8c145dbb9b8e0ce4b650c92fa1fe5167e13ecf9d3b97175d96fd8f4f4a99`；`group_write.go` `be2c14c7f57055cb2d66e24935e872cfc822bb2b1935cbf8edb33fdb78aaf847`；`group_update.go` `acce014dd581bfd65e058bfc03dbd8d16fa89e4c408a517a803135698f9cabc6`。

本次manifest收尾：`BASELINE-MANIFEST-2026-10-26.json` 已刷新为Kernel 26项变更文件逐项路径/状态/大小/SHA及对应review行；规范矩阵SHA-256为 `3b3dbf91c97bc4aeb84689d18a19f45e78290b540dcb1ede679a363437fe522c`。manifest含本EVIDENCE摘要，因此不自引用整份manifest SHA；交付报告公布最终manifest SHA。

## 2026-10-26：DM-GRP-006只读阻塞审计

本次只读核对目标Kernel候选 `/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel`；没有改动源码、访问数据库或运行Encore命令，也未触碰automation/alert其他工作树。DM-GRP-006冻结合同要求有子组、直接成员或有效外部引用时409；空组才soft-delete；必须带Idempotency-Key和If-Match，不能级联删除设备，并需与成员新增竞态互斥。

外部引用边界有冻结设计，但候选未实现：API-CONTRACT.md §10.1定义reserve/get/confirm/renew/release五个service-only reference操作；DATABASE.md §5.1定义`metadata_resource_fences`和`metadata_reference_edges`，要求删除先将fence从open CAS到deleting，再在同一fence→edge锁序下检查reserved/active边，引用服务不可用时fail closed。候选`devicemetadata/migrations/`当前仅至0020，未发现这两张表、引用fence API handler或引用查询/登记实现；当前上限的审计与幂等schema不能证明不存在外部引用。因此不能实现“无引用即可成功删除”的安全判断，也不能把查询未命中或owner暂时不可用解释为无引用。

owner边界：Kernel `l3-core/automation/service.go`的现有规则以target device为目标，规则在`closedloop`进程内创建/删除；没有device-group引用登记或查询。通知服务的`notification_groups`/`notification_group_revisions`是通知域自身的群组与引用，不是`devicemetadata.device_groups`；本次未检查或修改其他owner工作树。这些观察不能替代正式跨owner reference registry，也不能证明未来或外部规则没有引用设备分组。

本库并发门也未闭合：0018的`device_group_members`有同tenant FK RESTRICT，但DM-GRP-008/009的成员变更HTTP写路径尚未实现；候选现有tenant tree lock目前由分组create/update/move共用，尚无成员新增操作可与delete采用同一锁。FK仍然允许把一个live group soft-delete而保留成员行，且不能阻止“删除先查空、成员随后插入”的竞态。因此仅加delete事务+precheck仍不满足合同的互斥条件。DM-GRP-006保持部分实现；没有产品测试命令执行。

推荐先完成DM-API-011/DM-DB-011正式reference registry/fence，并使相关来源owner依协议登记；同时由DM-GRP-008/009成员写入与删除共同采用统一tenant tree锁。然后再实现DM-GRP-006：资源行/tenant tree锁后fence→edge锁，检查子组、成员、reserved/active引用，只有全部证明为空才soft-delete；成功时资源状态、receipt和audit同事务提交。若registry或owner读取失败，返回服务不可用并保持组为live。此次审计未改变OpenAPI、迁移、任务状态；DM-GRP-006仍为部分实现。

## 2026-10-26：DM-GRP-007只读实现前置审计

本轮只读核对冻结OpenAPI、设备分组合同、目标Kernel候选与授权实现；没有修改源文件/其他工作树，没有运行Encore或数据库命令。冻结操作为`GET /api/v2/device-groups/{groupId}/members`，参数`page/pageSize/includeDescendants/search`；分页默认/上限遵循1与1..100，返回`GroupMemberPageResponse`。子树设备须按device ID去重，携带`directMembership`和`matchedGroupIds`，items/total来自同一授权过滤和读快照；`matchedGroupIds`只包含当前主体可见组，空结果为`items: []`。任务用例要求一设备同时属于根与子组时只出现一次、部分device scope下total与items都仅包括授权设备。

当前候选没有可按冻结schema返回的native Device资料。OpenAPI `GroupMember.device`引用完整`Device`对象，要求deviceNumber/deviceKind/resourceVersion/labels/location/templateRevisionId/modelBinding/administrativeState/activationState/gatewayId/runtimeRef/tags/customFields等必填字段。当前`devicemetadata/migrations/0001_init.up.sql`中的`devices`仅有`id/tenant_id/device_template_id/product_id/sn/voucher/is_online/activate_flag/parent_device_id/created_at/activated_at`等旧列；`models.go`的`Device`结构也使用snake_case字段并包含`voucher`。直接返回旧`Device`既不符合响应合同，且存在把凭据字段串入成员列表的风险；伪造null/空值填native必填字段也会伪造资源状态。因此本轮不新增handler/service或临时投影。

授权前置也未闭合：冻结OpenAPI权限为`group:read`，API-CONTRACT §4要求受限主体的组可见范围及设备授权范围均来自正式授权owner；候选`groupPrincipalForRead`当前只接受TENANT_ADMIN。仓库有`internal/authz.CanDevice`和`authn.Data.AuthorizedDeviceIDs`，供automation/verifier等调用，但该helper只依据本地role policy及device ID allowlist，不解析可信`Scopes`，也没有`group:read`映射或组可见集合计算。不能把device:view自行等同group:read，不能把请求参数当scope，也不能因测试身份可构造授权字段而认定生产授权实现完成。即使只返回授权device，matchedGroupIds如何过滤到当前主体可见组也没有正式实现。

DM-GRP-007保持部分实现且无本轮测试证据。建议先完成DM-DEV-001等native设备schema/读模型字段与敏感字段投影，再由DM-API-003正式权限owner提供trusted `group:read`、device scope及可见group计算；随后落此endpoint，并在Encore隔离PG测试中覆盖普通/子树去重、search/page/100上限、空`[]`及部分授权后的total/items一致。真实HTTP仍需单独验收。本审计未改OpenAPI、迁移、生产或local/default数据库。

## 2026-10-26：DM-API-009生成链兼容性审计

本轮完成两步只读审计：核对冻结规范/validator、Kernel与Frontend依赖和CI；再按任务范围盘点未来由该任务管理的客户端调用方。没有改Kernel/Frontend源码、安装依赖、运行Encore/DB命令、修改OpenAPI或扩大旧业务调用审计范围。

冻结唯一源是本目录`openapi.yaml`，声明`openapi: 3.2.1`、JSON Schema 2020-12 dialect，含108个HTTP操作（103 user/public、5 internal-service）和权限/owner/task扩展。`tools/openapi-validator`已固定`libopenapi-validator v0.16.1`/`libopenapi v0.41.3`并对文档验证通过；它是validator，不是客户端生成器。Kernel旧`specs/openapi/device-metadata-v1.yaml`是OAS 3.1.0旧合同，不能作为新客户端源。

当前没有可靠的、可据官方资料锁定并承诺完整处理OAS 3.2.1的Go+TypeScript生成组合：OpenAPI Generator官方仓库目前声明规范兼容范围为1.x至3.1，且3.1仍是beta；`openapi-typescript`官方包说明列出的输入支持为OAS 3.0/3.1。冻结合同发布于2026-09-10，相关生成器公开兼容表没有给出3.2支持承诺。108操作本身不是已证实的限制；阻塞是规范版本与两套生成器的公开支持范围不匹配，以及尚无实际完整生成/编译/扩展保留验证。故本轮不引入“能解析”但可能静默忽略字段、操作或`x-*`扩展的生成器，不伪称TC-DM-API-009已满足。来源：[OpenAPI 3.2.1](https://spec.openapis.org/oas/v3.2.1.html)、[OpenAPI Generator compatibility](https://github.com/OpenAPITools/openapi-generator#compatibility)、[openapi-typescript README](https://www.npmjs.com/package/openapi-typescript)。

工具链/CI盘点：Kernel无DM客户端生成脚本、生成目录或`check-contract-freeze.sh`文件；`.github/workflows/go-only-gates.yml`不校验spec、生成客户端或零差异，`scripts/codex/run-gates.sh`的contract任务只是提示文件路径。Frontend已有pnpm 10.8.0锁定，但`package.json`/`pnpm-lock.yaml`没有OpenAPI generator/client依赖和生成脚本；没有从冻结spec生成的客户端。validator依赖锁定不等于codegen锁定。

调用方范围只盘点计划明确管理的将来Go CLI与Frontend native transport：Frontend `src/service/api/thingmodel-native-transport.ts`和相邻测试是手写`/api/v2` transport；Kernel `cli/tpctl-go`当前未发现设备元数据客户端生成产物/代码生成调用。旧Frontend `/api/v1`大量业务流、Kernel `api-gateway-v2/mvp-facade-go`中的legacy路由/Facade不是本任务管理调用方，因此本轮不把它们当作新client违规门禁，也不运行全仓命中即失败的grep。后续应由显式managed path清单限定门禁（生成client/CLI device commands/native transport），只检查这些受管目录的`/api/v1`与Facade依赖。

DM-API-009更新为部分实现，原因和证据已同步`TASKS.md`/`tasks.json`。推荐下一步先冻结“Go types+transport”和“TS types+fetch transport”各自支持OAS 3.2.1的具体工具版本，使用干净临时目录针对全108-operation spec做生成，断言operations/schemas/permission-owner-task扩展保留且生成编译通过；若仅一个语言工具支持，则先选择可信的双端替代工具/修复上游支持，或由产品明确收窄规范版本，不能未经决策把spec回写到3.1。只有该前置实证后，再提交版本锁、生成器/spec digest manifest、handwritten transport extension分层、regen zero-diff CI和受管调用方审计。此轮没有生成制品或运行测试；仅运行文件级计划校验（见交付报告），无数据库状态变化。

## 2026-10-26：DM-API-009临时生成器探针

按任务要求，在全新scratch `/tmp/codex-dm-api009-generator-probe-20261026` 对冻结原文件直接生成；未降级spec、未使用skip-validate、未改任何源码/包清单，也未运行Encore/DB命令。冻结文件SHA-256为`f174629a6f39eb72e0833eb46042bf9e693dad050897b85e8e0e9f2d2081b459`。

Go试验使用`oapi-codegen-exp v0.1.0`（官方仓库将其标为experimental prototype，并说明可解析OAS 3.1/3.2；其CLI依赖Go 1.25，本机`go1.26.2 darwin/arm64`满足）。精确命令：`go run github.com/oapi-codegen/oapi-codegen-exp/cmd/oapi-codegen@v0.1.0 -config /tmp/codex-dm-api009-generator-probe-20261026/go/config.yaml /Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/backend/docs/initiatives/device-metadata-native-cutover/openapi.yaml`。退出码1，首因`error generating code: building v3 model: infinite circular reference detected: GroupTreeNode: GroupTreeResponse -> GroupTree -> GroupTreeNode -> GroupTreeNode [8204:11]`，未产出Go客户端，未编译。该工具虽可读3.2结构，实际被冻结的递归树schema使生成失败；不能作为Go端候选直接锁定。

TypeScript试验使用`openapi-typescript 7.13.0`，NPM cache单独位于`/tmp/codex-device-metadata-generator-npm-cache`，命令：`npm_config_cache=/tmp/codex-device-metadata-generator-npm-cache npm exec --yes --package=openapi-typescript@7.13.0 -- openapi-typescript /Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/backend/docs/initiatives/device-metadata-native-cutover/openapi.yaml --output /tmp/codex-dm-api009-generator-probe-20261026/ts/openapi.d.ts`。退出码0，生成334,234字节TypeScript声明。临时旁路核对结果：108/108 operationId、108/108 path+method绑定、1,524个操作级组件引用（请求/响应/参数）均在各自生成operation类型中出现；36个Page命名组件均生成；唯一nullable union `CustomFieldValues`仍为`string | number | boolean | null`。用Frontend锁定版本TypeScript 5.8.3运行`npm_config_cache=/tmp/codex-device-metadata-generator-npm-cache npm exec --yes --package=typescript@5.8.3 -- tsc --noEmit --skipLibCheck --lib es2020 /tmp/codex-dm-api009-generator-probe-20261026/ts/openapi.d.ts`，退出码0，无编译输出。

但这不是合格的最终生成方案：输入中570处/23种`x-*`扩展在生成文件中全部不可见，仓库也没有保护这些值的sidecar manifest/校验；11处`date-time`中，组件`Timestamp`仅保留格式注释，date-time query参数生成成普通`string`，不携带格式约束。类型生成能覆盖operation/schema名不代表permission/owner/release-task语义被保留。故不能以“TS命令成功+可编译”关闭门禁或让generator静默丢弃扩展。

可复核的精确版本、命令、日志和核对输出保存在`/tmp/codex-dm-api009-generator-probe-20261026/probe-report.txt`、`go/generator.log`、`ts/generator.log`、`ts/audit.json`；TS产物为`ts/openapi.d.ts`。官方资料：[oapi-codegen-exp experimental OAS 3.2 parser](https://github.com/oapi-codegen/oapi-codegen-exp#openapi-v31-feature-support)、[openapi-typescript releases](https://github.com/openapi-ts/openapi-typescript/releases)。结论仍是部分实现：Go生成受实际递归schema阻塞；TS需要显式保留扩展与时间格式的sidecar、且要有稳定锁定/干净重建门禁后才能继续。没有进行其他生成器下载/试跑，也没有添加依赖。

## 2026-10-26：DM-DEV-001稳定分页查询切片

先评估DM-DEV-003/DM-DB-003的无模板创建。冻结`CreateDevice`明确允许模板revision为空、要求原子创建、Idempotency-Key、可信租户/权限和新设备事实字段；现有`devices.device_template_id`在`0001_init.up.sql`为`NOT NULL REFERENCES device_templates`，现有`Device`仍是旧模型（含voucher/is_online/activate_flag），`createDevice`强制已发布模板并返回voucher，且不通过持久幂等收据/审计。仅将模板列放宽会让正式操作进入不完整的半合同，故未改该路径或迁移；DM-DEV-003仍部分实现。

改选依赖已存在表/函数、可由隔离Encore PG测试直接证明的DM-DEV-001稳定分页顺序：设备列表旧查询只按`created_at DESC`排序，相同时间戳的行跨页顺序未定义。`store.go:listDevices`现在增加`id DESC`作为次级排序；没有更改API path/参数、权限、过滤字段、schema、迁移或旧路由。新增`TestListDevicesStablePaginationForEqualCreatedAt`：在Encore test数据库建三个合成设备并将created_at设置成同一瞬间，验证pageSize=2的前后两页按ID降序拼接为预期完整序列、无重复/遗漏且两页total均为3。

实际命令和结果（工作目录`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel/encore-api`）：

- `encore test ./devicemetadata -run '^TestListDevicesStablePaginationForEqualCreatedAt$' -count=1 -v`：PASS，`ok encore.app/devicemetadata 0.708s`。这是明确的Encore isolated test DB；没有运行`encore check`或无目标的Encore/DB命令。
- `go vet ./devicemetadata`：PASS。
- `git diff --check`：PASS。

随后在本次获准的隔离测试范围内检查全包结果。首次`encore test ./devicemetadata -count=1`确实失败：`TestMoveDeviceGroupAtoBAndBtoAConcurrentNeverCreatesCycle`并发后一个move必胜，但获胜方向不固定；旧cleanup固定reverse输入顺序，可能先删仍被另一组引用的父节点，因此触发`device_groups_parent_tenant_fk` RESTRICT。已改成cleanup首先将A/B两个fixture的`parent_id`均置空，再删除两行，不依赖race方向。`TestThingModelNameUniquePerTenant`并没有独立失败；首次日志中的`thing_models_active_tenant_name` unique violation是该用例有意触发的同tenant重复名称断言，不是跨tenant创建失败。修复后分别运行`encore test ./devicemetadata -run '^TestMoveDeviceGroupAtoBAndBtoAConcurrentNeverCreatesCycle$' -count=1 -v`与`encore test ./devicemetadata -run '^TestThingModelNameUniquePerTenant$' -count=1 -v`，两者均PASS；unique用例预期的SQLSTATE 23505仍出现在日志中。之后`encore test ./devicemetadata -count=1`全包PASS（`ok encore.app/devicemetadata 1.518s`）；再运行`go vet ./devicemetadata`和`git diff --check`均PASS。以上Encore命令均明确使用Encore isolated test DB；没有查询、修复或运行local/default数据库命令。

DM-DEV-001只关闭“同一created_at下列表分页稳定排序”这一小项，不能代表冻结native `GET /api/v2/devices`已实现。尚缺native v2 handler/字段响应、完整筛选、tenant/resource ACL、在线/runtime一致快照、120设备/15授权分页合同、所有字段索引/查询计划，以及真实HTTP/UI/CLI验收。此切片未添加迁移，未访问local/default或生产数据库；相关错误incident状态没有复核。

同一验证过程中修复了`TestMoveDeviceGroupAtoBAndBtoAConcurrentNeverCreatesCycle`的fixture清理次序；该修改仅限测试清理代码，不改变分组产品逻辑。原先包级失败及根因、修复后的定向/全包通过分别记录，没有把最初失败覆盖掉。

候选源码SHA-256：`encore-api/devicemetadata/store.go` `6018778faec6130665b844435b672037a31acb65f081773648c6c171c8280f22`；`encore-api/devicemetadata/device_crud_test.go` `67f034f7cf575c56cea2abb75b378db78eb222b3297858b5c24eaa3e94e24fe2`；`encore-api/devicemetadata/group_move_test.go` `09094b22767f9633ab2fac3180284fcb72468982dac49318ff3d4b6970f5ea4f`。Manifest随之刷新，逐项路径、状态和文件hash以manifest为准；本次只新增一个列表排序断言并改测试fixture清理，没有覆盖其他dirty改动。


## 2026-10-26：DM-GRP-008/009原生成员批量增删切片

在隔离Kernel候选中新增Encore注册端点 `POST /api/v2/device-groups/{groupId}/member-additions` 与 `/member-removals`。两者复用现有 `groupPrincipalForWrite`，只接受TENANT_ADMIN；未增加角色、scope映射或权限owner。请求拒绝未知字段/尾随JSON、空或超过100条ID、重复/无效UUID和缺失/非法版本。请求路径、规范化body、tenant、subject、operation和Idempotency-Key参与请求收据作用域；相同集合的ID顺序规范化后得到同一摘要。

写事务固定按幂等key advisory lock → tenant device-group-tree advisory lock → receipt/组/设备访问。先锁定并读取live分组，要求 `expectedGroupVersion` 相同；然后按ID顺序逐一 `FOR UPDATE` 验证同租户且 `devices.deleted_at IS NULL`，任何未知、外租户或已软删设备整批返回404并回滚。成员关系用 `ON CONFLICT DO NOTHING` 或定向DELETE；只有实际关系变化时组 `resource_version` 增加一次，缺席关系的移除保持版本不变。每个首次成功请求（包括无变化操作）在同事务提交响应receipt与脱敏audit，重复同key只回放原响应。成员API只改关系，不删除设备或赋予device scope。

验证命令（均在 `kernel/encore-api`，使用Encore隔离test DB）：

- `encore test ./devicemetadata -run '^Test(DecodeSetGroupMembersBodyStrictly|SetDeviceGroupMembersRawRejectsForgedScopeWithoutTenantAdmin|SetDeviceGroupMembersPersistsCASReplayAndNoOp|SetDeviceGroupMembersRejectsWholeBatchOnInvalidDeviceAndStaleVersion|SetDeviceGroupMembersConcurrentCASSerializesSameGroup)$' -count=1 -v`：PASS；覆盖严格body、仅TENANT_ADMIN、成员增删/重放/异参冲突/无变化版本、同租户live设备、未知/外tenant/已软删ID拒绝、stale版本、审计次数及两请求CAS竞态一胜一冲突。
- `encore test ./devicemetadata -count=1`：PASS，`ok encore.app/devicemetadata 1.589s`。
- `go vet ./devicemetadata`、`git diff --check`：PASS。

初次切片记录（后续已由下方补正覆盖）：事务用例直接调用use case函数；一项权限用例直接调用raw handler；均未启动真实Encore listener。当时尚未验证完整100成员成功批次和非空跨组保留关系；下方新增的隔离PG测试已覆盖这两项本地验收。当前授权owner尚未提供正式受限 `group:write`/逐设备ACL上下文，因此未伪造该身份，非TENANT_ADMIN一律拒绝。幂等重放目前只重跑TENANT_ADMIN身份校验，不具备正式逐设备scope供重放时复验；DM-DB-002的应用角色/RLS和直接SQL负向门禁仍缺；移除成员时尚无外部规则owner reference revalidation协议。DM-GRP-008/009仍是部分实现，blocking保持true。未改迁移0018/0019/0020；未运行 `encore check`，未访问local/default或生产DB，也未改其他团队工作树。


复核补正（DM-GRP-009 fail-closed/reference owner）：生产Encore handler调用未注入checker的入口；当请求中存在真实关系变更时因正式reference owner/registry尚未提供而返回503 Unavailable，事务回滚，不留下成员变化、版本变化、receipt或audit。纯no-op移除集合为空，不需要外部引用复核。测试专用注入点只用于明确返回“无引用”的可信checker模拟，不代表生产owner已实现；另测checker不可用仍零变化。TASK TC-DM-GRP-009-03因此仍未在正式owner环境通过，DM-GRP-009保持blocking/部分实现。

移除先按同租户验证目标device并锁定关系；已有关系即使设备 `deleted_at` 非空也可解除，未知/外租户ID仍404。该操作只删除 `device_group_members` 关系，不改设备实体、其他组或scope。审计 `redacted_changes` 使用固定白名单键 `addedDeviceIds` / `removedDeviceIds`，值来自实际成功插入/删除集合，而非请求集合；含非成员no-op输入时只记录实际变化子集。审计写入失败时整笔事务回滚。

补正后验证（均在Kernel候选 `encore-api`，Encore隔离test DB）：补充定向命令 `encore test ./devicemetadata -run 'TestSetDeviceGroupMembers(AddsExactly100LiveDevicesAndDuplicateSetIsNoOp|RemovalPreservesOtherGroupAndDevice)$' -count=1 -v`：PASS，两个测试分别0.32s和0.03s（包0.869s）；`encore test ./devicemetadata -count=1`：PASS，1.854s；`go vet ./devicemetadata`：PASS；`git diff --check`：PASS。覆盖缺失checker/不可用checker503无变化、测试checker明确无引用后移除、tombstone关系清理、审计真实ID子集、审计失败全回滚。 新增 `TestSetDeviceGroupMembersAddsExactly100LiveDevicesAndDuplicateSetIsNoOp`：隔离PG真实插入100个同租户live设备，首次写入精确100条关系并使版本从1到2；同集新key重试保持100条且版本仍为2。新增 `TestSetDeviceGroupMembersRemovalPreservesOtherGroupAndDevice`：同一live设备属于A/B组，注入checker确认无引用后仅移除A关系，B关系和设备行仍存在。未运行 `encore check`，未访问默认/local/生产DB，未改0018/0019/0020。当前未覆盖生产引用owner集成、受限scope、真实Encore listener HTTP、RLS/direct SQL。100个live设备批量成功与重复集no-op、A/B组关系保留均已由上方隔离PG测试覆盖。

最终Kernel候选SHA-256（与 `BASELINE-MANIFEST-2026-10-26.json` 和实际文件一致）：`group_members.go` `146f40ed57a4ba07030338612a2951bbc23f2982fa770e8f619a21e76e00d893`；`group_members_raw.go` `3d7d8f06ec3d8c4c6a2c142c955883852b179aa8ddedcc6ffabbc56b5908b6e9`；`group_members_test.go` `bd1edbd733bd37da5cfea74e57ce2a307be691549baa1a0010bf1302daee810a`。Kernel候选矩阵仍为29项，逐文件状态、大小与hash以manifest为准。

## 2026-10-26：shared authz 分组权限 owner 切片（DM-API-003，仍部分实现）

在隔离 Kernel 1.3 候选中，native device-group 的读写 principal 改为调用 `internal/authz.Can`。共享 Casbin owner 只增加两条精确策略：正式 `TENANT_ADMIN` 对 `group/read` 与 `group/write`；Community `SYS_ADMIN`（无审计委派对象）、`TENANT_USER`、`TENANT_VIEWER`、历史小写角色以及只携带 `*`、`group:read`、`group:write` 的身份均不获授权。分组 guard 先验证 tenant，因此 tenant-scoped SYS_ADMIN 同样 fail closed。当前 authn issuer 不提供正式受限 scope/device-scope owner，故不解析或伪造这些scope；未变更 automation/control/verifier 服务语义，也未改 OpenAPI/API-CONTRACT。

验证：`encore test ./internal/authz ./devicemetadata -run 'Test(Can(MapsNativeGroupAccessOnlyToCommunityTenantAdmin|PreservesExistingDeviceAndPluginPolicies)|GroupPrincipalFor(ReadUsesSharedPolicyAndRejectsUndelegatedRoles|WriteRequiresTenantAdmin))$' -count=1 -v` PASS；`encore test ./internal/authz ./devicemetadata -count=1` PASS（authz 0.207s、devicemetadata 1.925s）；`go vet ./internal/authz ./devicemetadata` PASS；`git diff --check` PASS。数据库相关行为仅通过 Encore 隔离 test DB 的 `encore test` 验证；未运行 `encore check`，未访问默认/local/生产DB。分组既有HTTP/PG验收保持为此前证据，本切片增加的是 principal/policy 层回归，不声称完成真实HTTP矩阵。

本切片文件 SHA-256：`internal/authz/engine.go` `4da25acd62ef5015c9d2527f6a860de77948c16d7c45f5392705020ec58697c4`；`internal/authz/engine_test.go` `3d0cbcb7d5f0ac50fed703649944cb44777e5960684a14eae362ceba9470cfcc`；`devicemetadata/group_read.go` `115d19449e6c8b8f52aa3dbb1e155b85ec884ce1104a71e37b30339ecf0e3355`；`devicemetadata/group_write.go` `9086db96ab3e9dedbb8c2ee13cca72f9af704af35f11c2627003e2567c375860`；`devicemetadata/group_read_test.go` `55aec04ab13fa508c2d313cfcd3dae650e38229ff2923bd10a9333902cdac0cc`；`devicemetadata/group_move_test.go` `99e89185b7edd75a76109e1c87246af95323c89d18303dd38117fd43f3a9c10e`。

剩余：DM-API-003仍P0 blocking/partial。缺每operation action/resource matrix、正式delegation和受限服务身份scope、设备ACL对列表/统计/分组计数/导出的过滤、凭据权限、UI/CLI矩阵，以及覆盖所有端点的真实HTTP验收。shared authz现有device/plugin策略用专门回归断言保持原结果。

## 2026-10-26：DM-GRP-010权限前置与DM-GRP-008/009真实HTTP验收边界

DM-GRP-010冻结operation为`GET /api/v2/devices/{deviceId}/groups`，权限`device:read`，响应`DeviceGroupPageResponse`。只读核对发现shared authz当前没有`device/read`角色策略；`CanDevice`只使用现有role/action策略及`AuthorizedDeviceIDs`，而API-003正式device:read角色映射、受限scope和group/device可见性owner仍未完成。不能以`group:read`或仅同租户验证代替设备授权。因此本轮不实现该endpoint；DM-API-003、DM-API-004、DM-DB-006仍为部分实现。此轮无代码或测试改动。

DM-GRP-008/009已有隔离Encore PG业务用例，但成员写用例分别直接调用raw handler或use case，不经过Encore router/listener、签名JWT及Community live-session。EVIDENCE前文记录：本机`encore run`在listener启动前被notification服务Darwin门禁终止（`l3-core/notification/secret.go`的Linux-only检查）；现无已核实可同时提供当前候选listener、隔离PG和认证/session fixture的现成测试入口。因此本轮没有新增或运行listener测试，也没有触碰数据库。不能把既有test DB通过记录当作真实HTTP证据：DM-GRP-008/009仍保持partial/blocking；还缺可启动的候选runtime、明确隔离的listener PG及签名JWT/session fixture，之后才能通过HTTP验证路由、认证、Problem响应、幂等/CAS和owner fail-closed行为。

## 2026-10-26：DM-DB-002 tenant_id不可变切片

新增候选迁移`encore-api/devicemetadata/migrations/0021_tenant_id_immutable.up.sql`（SHA-256 `4db4bc4dc3a57ba464d22781e70ef367eac7db32953334c7e577e9e6cdc4ea89`），不修改或重放0018–0020。迁移创建共享触发函数，并按迁移时`current_schema()`中所有含`tenant_id`的普通表/分区表动态安装trigger。`OLD.tenant_id IS DISTINCT FROM NEW.tenant_id`时稳定报SQLSTATE 23514；将tenant_id赋为原值不报错。

新增`encore-api/devicemetadata/tenant_immutable_test.go`（SHA-256 `7359cb6e6e6d1e461f623fe8c26fd60982ebff7f27b8d16878d9bdb0a4737cf1`）。测试在Encore隔离PG catalog枚举当前schema全部tenant-bearing表并断言各自安装trigger；另创建native`device_groups`及legacy owner `thing_models`行，验证same-value UPDATE成功、改为另一tenant以23514失败。命令`encore test ./devicemetadata -run '^TestTenantIDImmutableTriggerCoversPackageTablesAndRejectsReassignment$' -count=1`：PASS（0.669s）；`go vet ./devicemetadata`：PASS。根代理随后在同一隔离Encore test DB独立运行`encore test ./devicemetadata -count=1`：PASS（2.006s）。未访问local/default或生产数据库。

本切片仅覆盖迁移时已存在的表和tenant_id更新不变式；未完成RLS/FORCE RLS、应用角色权限/受信tenant上下文、全部业务表NOT NULL与复合FK收敛、所有跨租户关系直接SQL负向测试或同租户竞争写验收。新表加入时需再次核验trigger覆盖。DM-DB-002仍为部分实现且blocking。

## 2026-10-26：DM-DB-002同库tenant复合FK切片

迁移前先在隔离Encore test DB只读检查11类既有关系，`TestTenantCompositeFKHistoricalRowsHaveNoCrossTenantMismatches`对`thing_model_items → thing_models`、`thing_model_snapshots → thing_models`、模板→ThingModel、模板→同模型snapshot、product→template、device→template/product、custom command→ThingModel、outbox→ThingModel、resources→ThingModel及`thing_models.current_snapshot_id → 同tenant同model snapshot`均检查0条missing/cross-tenant/cross-model目标行。查询在迁移0022应用前和之后均通过，未发现需要转换或清理的历史行。

新增候选迁移`encore-api/devicemetadata/migrations/0022_tenant_composite_foreign_keys.up.sql`（SHA-256 `d29f0fa162e2fdc56d44d5a506c6095085cce89c2b103f5e88b185256a93c47a`），只追加0022，未改/重放之前迁移。为`thing_models`、`thing_model_snapshots(tenant_id,thing_model_id,id)`、`device_templates`、`products`补唯一目标键（`devices(tenant_id,id)`已由0018提供）；将已有单列FK升级为复合tenant FK，并保留删除动作：`thing_model_items`、`custom_command_controls`、`thing_model_resources`为CASCADE；ThingModel snapshot、模板及其同模型snapshot、product/template、device/template/product、current snapshot pointer为NO ACTION；`thing_model_outbox`为RESTRICT。`devices.product_id`和`thing_models.current_snapshot_id`仍可NULL，MATCH SIMPLE保持未绑定/未发布状态。模板snapshot FK增加modelId到target的匹配，因此不能绑其他模型的snapshot。发布/回滚顺序已核对为先插入不可变snapshot、再更新ThingModel指针，满足即时约束。`thing_model_command_receipts`不含model_id，没有可升级的ThingModel FK；未修改。legacy `device_templates.bound_thing_model*`投影、`devices.parent_device_id`与runtime跨owner关系也未纳入。

新增`encore-api/devicemetadata/tenant_composite_fk_test.go`（SHA-256 `35ff09d4e844b5d75780ffd8574385e1d6576a81c03ebf1b6eb440663ef99968`）。catalog用例逐条核对11类关系的tenant列顺序、引用目标列、已验证状态与`ON DELETE`代码；直接SQL用例覆盖同tenant插入/当前snapshot更新成功，以及跨租户或跨model的item、snapshot、template、product、device、control、outbox、resource和current-pointer关系以23503拒绝，并确认可空`product_id`仍可无绑定插入。

验证仅针对Encore隔离test DB：迁移前历史数据预检`encore test ./devicemetadata -run '^TestTenantCompositeFKHistoricalRowsHaveNoCrossTenantMismatches$' -count=1 -v` PASS（0.745s）；迁移应用后`encore test ./devicemetadata -run '^TestTenantCompositeFK' -count=1 -v` PASS（0.671s）；完整`encore test ./devicemetadata -count=1` PASS（2.019s）；`go vet ./devicemetadata` PASS。未运行`encore check`，未访问local/default或生产数据库。

仍未完成RLS/FORCE RLS、应用角色最小权限、SET LOCAL tenant上下文/连接复用隔离、全库tenant NOT NULL与其他遗漏复合键收敛、租户引用关系直接SQL全覆盖和同租户竞争写、跨owner正式引用owner。此切片不覆盖迁移时尚不存在的新表；后续表仍须按同一模式补FK及catalog门禁。DM-DB-002保持部分实现且blocking。


## 2026-10-26：DM-DB-002 RLS与tenant事务上下文就绪度审计（只读）

按`DATABASE.md` §2.2（tenant事务上下文、RLS `USING`+`WITH CHECK`、运行角色不得拥有DDL/BYPASSRLS/表所有权）只读检查当前候选。`devicemetadata/db.go:5-7`只声明Encore逻辑库及migration目录；代码中没有`SET LOCAL app.current_tenant_id`或统一tenant-scoped事务helper。现有多数读写仍直接使用连接池：`devicemetadata/store.go`有51处`db.*`访问（如`:28-40`, `:43-83`, `:1880-1910`），`custom_control_service.go`有直接count/list/insert/update/delete（如`:42-58`, `:102-110`），`migration_service.go`存在事务外tenant查询（`:474-485`, `:517-538`），另有`service.go`、`thingmodel_helpers.go`及已加事务但未设置tenant GUC的分组读写。ThingModel owner通过`thingmodel/db.go:5-7`访问同一`device_metadata`；其`postgres.go:40-56`已有事务但未设tenant上下文，`:129-169`的读取直接走池化查询。旧库迁移源`legacydb.go:15-41`使用单独`TP_LEGACY_DATABASE_URL`池，不属于目标RLS事务；读取源库与写入目标库需要分开处理。

特殊数据语义：`meta_item_keys.tenant_id`允许NULL作为全局字典；`store.go:1880-1910`明确同时读取NULL全局键和当前租户键。后续RLS必须保留全局SELECT，同时以`WITH CHECK`禁止租户写入全局行。现有`migrations/0005_create_audit_log_table.up.sql:24-27`仅对旧`audit_log`执行`ENABLE ROW LEVEL SECURITY`，没有`FORCE ROW LEVEL SECURITY`；policy使用`current_setting('app.current_tenant_id')::uuid`但未提供missing-ok处理。它不能证明其余tenant表已受RLS保护。

当前隔离候选仓库未找到`device_metadata`的非owner运行角色/独立迁移角色配置。Encore数据库在代码中只是逻辑资源；self-hosted部署由infra配置提供数据库凭据，构建镜像不执行migration（官方说明：[infra配置](https://encore.dev/docs/self-host/configure-infra)，[self-hosted构建与迁移](https://encore.dev/docs/self-host/build)）。因此实际运行身份及role provisioning需由部署环境提供并验证；不得假定运行账号不是owner，也不得把角色创建塞入普通schema迁移而忽略平台角色边界。

结论：现在对所有表启用`FORCE RLS`会使尚未设置tenant-local GUC的池化读写报错或无行可见，并会影响ThingModel同库访问；在事务上下文helper和非owner运行身份落实前不可安全切换。按以下顺序实施：①确认运行role不拥有表且无DDL、BYPASSRLS或提权能力，迁移role独立；②从已验签/验证session并归一化的principal tenant建立统一transaction helper，使用事务本地`set_config('app.current_tenant_id', tenant, true)`；③迁移`devicemetadata`及`thingmodel`所有DB读写和旧数据迁移目标写入，使每条路径都经该helper，legacy source pool保持隔离；④为所有tenant表增加安全缺省的`USING`与`WITH CHECK`并验证RLS/FORCE，单独处理`meta_item_keys`全局只读；⑤用非owner、非BYPASSRLS运行role覆盖同租户CRUD、跨租户隐藏/拒写、missing/invalid GUC拒绝、全局metadata只读和连接池租户上下文不串用。此审计未改代码/迁移/合同，未运行测试或连接数据库；DM-DB-002仍partial/blocking。

## 2026-10-26：DM-API-009生成器最后兼容性探针（保持partial/blocking）

本节核对的冻结源为`openapi.yaml`，SHA-256 `f174629a6f39eb72e0833eb46042bf9e693dad050897b85e8e0e9f2d2081b459`。没有修改规范、产品代码或依赖。最终探针报告：`/tmp/codex-dm-api009-generator-probe-20261026/probe-report.txt`、`/tmp/codex-dm-api009-ogen-probe-20261026/ogen-v1.24.0.log`、`/tmp/codex-dm-api009-openapi-generator-7.26.0/strict-validation-report.txt`、`/tmp/codex-dm-api009-openapi-generator-7.26.0/validate-json.log`、`/tmp/codex-dm-api009-final-probe/probe-report.txt`。此前Go/TS原始输出和TS审计仍见本节上方记录及`/tmp/codex-dm-api009-generator-probe-20261026/{go/generator.log,ts/audit.json,ts/audit.log,ts/generator.log}`。

- `ogen v1.24.0`直接读取OAS 3.2.1时，在合法的`type: [string, number, boolean]`处失败（`multiple types are not supported`）；仅在scratch中把类型数组和exclusiveMinimum改写后，继续失败于重叠的`NativeThingModelValueType` oneOf分支，无法推断discriminator。规范不能为生成器改写。
- 官方OpenAPI Generator CLI `openapitools/openapi-generator-cli:v7.26.0`对原YAML严格`validate`因SnakeYAML非标量别名上限50退出1。为区分解析器问题，另由canonical YAML解析出alias-free JSON，保持`openapi: 3.2.1`和3.2 dialect，严格validate仍报540项错误（包括大量`responses.*.content`/`requestBody` unexpected及参数schema/type不匹配）；未跳过校验、未生成客户端。改成3.1.0的scratch输入也不可作为兼容办法，因为只改版本会把`jsonSchemaDialect: https://spec.openapis.org/oas/3.2/dialect/2026-02-26`留在3.1文档中，违反版本/方言匹配。结构扫描未发现querystring、additionalOperations、itemSchema、XML nodeType等其他3.2新增结构。
- `oapi-codegen-exp v0.1.0`原输入失败于递归`GroupTreeNode`。scratch中移除递归children、排除该schema并加手写递归类型后，其余客户端生成成功；增加通用Nullable no-op defaults扩展和`Id = ID`别名后`go test ./generated`通过，覆盖树JSON、chart配置null/object往返与UUID JSON/URL路径。但这仍不保真：源规范含252个`additionalProperties: false`节点，生成物有217个`AdditionalProperties` map；`DeviceGroup`会收集并序列化规范禁止的未知字段。文档化`struct-tags`模板配置只改字段标签，重生成后仍有217个map。没有受支持配置可保留封闭schema语义；不接受ad hoc生成后全局重写。
- TypeScript `openapi-typescript 7.13.0`生成物可由锁定的TypeScript 5.8.3编译，108个operation/path-method和组件引用核对通过；生成器丢失`x-*`治理元数据，date-time query参数退化为普通string。此前文字扫描统计的570项/23键把媒体类型`application/x-ndjson`也当成扩展；按实际YAML扩展键解析为569处/22键。仅在`/tmp/codex-dm-api009-generator-probe-20261026`生成的扩展sidecar探针是可行性说明，尚无受版本控制的sidecar及canonical-source校验门禁，不能视作语义已保留。

结论：目前没有同时满足OAS 3.2.1兼容、递归/union/时间/nullable类型、封闭对象语义、x-*保真、干净重建及两端编译门槛的Go+TypeScript生成组合。DM-API-009继续部分实现且`blocking: true`，DM-FE-001继续受其阻塞。下一步须采用明确支持OAS 3.2.1且通过全契约语义核对的生成器，或先由合同owner决策调整规范版本/方言；本轮不改冻结规范、不锁入失败工具，也不改产品依赖。

## 2026-10-26：DM-DB-002 native DeviceGroup事务本地tenant上下文切片

新增共享`beginTenantGroupTx`，按UUID验证已认证tenant并在开始事务后执行参数化`set_config('app.current_tenant_id',$1,true)`；设置失败即回滚。仅替换已注册native DeviceGroup路径：create、list、detail、tree、update、move、member-add、member-remove。写路径仍按原请求key advisory lock→tenant tree lock顺序，并在同一事务保留业务写、幂等receipt和audit；列表count/items/metadata和树多查询仍使用同一个只读Repeatable Read事务。详情同样经helper建立只读Repeatable Read事务。没有改legacy handlers、自动化/通知、迁移、RLS policy、跨owner引用语义、API/OpenAPI或auth matrix。

新增`group_tenant_tx_test.go`验证Encore隔离PG事务内`current_setting('app.current_tenant_id')`等于principal tenant，并验证空值、非UUID、nil UUID及非canonical值被拒绝。已有DeviceGroup service/use case Encore PG测试覆盖注册的读写路径，且其开始事务调用统一helper。focused命令`encore test ./devicemetadata -run 'TestBeginTenantGroupTx|Test.*DeviceGroup' -count=1`：PASS（1.865s）；全包`encore test ./devicemetadata -count=1`：PASS（2.126s）；`go vet ./devicemetadata`：PASS；`git diff --check`：PASS。均在Encore隔离test DB；未运行`encore check`，未访问local/default或生产DB。

本切片代码所在候选HEAD为`fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`（工作树未提交）。SHA-256：`group_tenant_tx.go` `0848b0546c9ee4ce7e8f04868d3ca9ced180b3e89636399320920086d7b1857f`；`group_tenant_tx_test.go` `ab9c1132c6673fdfd99851c3b9916e64a8d43c07651d2a23a3af1551732b02de`；`group_write.go` `47eb3c05d21715b3036dae97133fd59cf51e3955b8368c2ded373df0bbaf1182`；`group_update.go` `90984f5730db3309e34efaf79c823e6827d14e7048bbb69e02f946cef8c9f6f6`；`group_move.go` `619933a9a663dab245fff0c611c5cfbfb95971503df87d3693954cfd62e9046e`；`group_members.go` `4ed9e932eb61db6215c47ad0c1c2126573cbe6ef0211148fc14d11fd70fb8e4d`；`group_read.go` `a3fcd32aba1e81ce3e136ee57248417bb16f4090e9e306823bcf2fd9e627539b`。

此证据只证明helper在事务中设定trusted tenant，以及列出的native DeviceGroup代码路径使用该事务helper。没有启用/验证RLS或FORCE RLS，没有用非owner运行role验证策略拒绝，也没有证明事务结束后的连接池状态复位或同连接后续请求不串租户。ThingModel及其他devicemetadata表的路径仍未迁移，故DM-DB-002保持partial/blocking；DM-GRP-003/004/005/008/009仍partial/blocking，真实listener HTTP、完整授权矩阵、外部reference owner及各任务原验收范围仍缺。

## 2026-10-26：DM-DB-002 ThingModel持久写事务tenant-local上下文切片

在Kernel隔离候选 `device-metadata-native-cutover-1.3/kernel` 的 `encore-api/thingmodel/postgres.go` 中，`postgresRepository.Execute` 先校验完整 `ReceiptScope`，开始 `sqldb.Tx` 后、advisory lock及receipt查询前，以参数化 `SELECT set_config('app.current_tenant_id', $1, true)` 设置事务本地tenant。helper值由解析后的UUID `String()`规范为小写标准格式；nil UUID和非法UUID在开始事务/执行查询/调用业务回调前拒绝。receipt检查/重放、业务callback、命令receipt、业务写、审计及outbox仍由原同一sqldb.Tx承载。任何tenant GUC设置、业务helper、审计/outbox或receipt持久化失败都会走原defer rollback；callback失败的隔离PG测试实际先插入ThingModel记录再验证记录未提交。

新增 `encore-api/thingmodel/postgres_test.go` 用例证明：uppercase tenant输入写入GUC时被规范化；callback通过同一`postgresTransaction.tx`读取到tenant与事务ID，再通过同一Transaction插入记录；callback失败后业务记录不存在。非法与nil tenant均返回`app.ErrInvalid`且callback未运行。原有`TestPostgresRepositoryRollbackLeavesNoPartialWrites`仍覆盖model/audit/outbox/receipt失败时整体回滚。

文件SHA-256：`postgres.go` `02f27736fc9cce27a42c33ebc55daf4ee7860ff45dac5b7f224820d53ba8b311`；`postgres_test.go` `50f75605ce1f3cffcfd8e2b0e00169f4691fc15ff89dd3d72fc3a8680dee6a2a`。命令结果：`encore test ./thingmodel -run 'TestPostgresExecuteSetsTenantLocallyBeforeCallbackAndSharesTransaction|TestPostgresExecuteRejectsInvalidTenantBeforeCallback' -count=1` PASS（0.292s）；`encore test ./thingmodel -count=1` PASS（0.731s）；`go vet ./thingmodel` PASS；`gofmt`与`git diff --check` PASS。均为Encore隔离PostgreSQL test DB；未运行`encore check`，未接触local/default/prod DB。

本证据仅覆盖ThingModel `Execute` 持久写事务tenant-local上下文、scope nil/非法拒绝、callback/helper失败回滚及事务内读写边界；不证明RLS/FORCE RLS、非owner/BYPASSRLS角色策略、连接池transaction-local状态/后续连接复用隔离、ThingModel read paths、其他ThingModel DB路径或其余devicemetadata路径。DM-DB-002继续partial/blocking。

## 2026-10-26：DM-DB-002 ThingModel原生读路径tenant-local只读事务切片

在上述写事务切片基础上，为`postgresRepository.Get/List/Versions/Version/Resources`统一接入`beginTenantReadTx`。helper先解析并拒绝nil/非法tenant UUID，再开始sqldb事务；首条SQL为`SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`，随后以参数化`set_config('app.current_tenant_id',$1,true)`设置规范化小写UUID。初始化任一步失败立即rollback；查询/解码失败由defer rollback；成功路径明确Commit。List count和items复用一个只读Repeatable Read tx；Versions内模型存在性检查和snapshots查询复用一个tx；Resources及单查询Get/Version也走相同tenant-local只读事务。业务查询字段、过滤、排序和错误语义保持不变。源码静态检索`rg -n 'db\.(Query|QueryRow)' encore-api/thingmodel/postgres.go`无命中，五个生产读路径没有直接连接池Query/QueryRow调用。

新增Encore隔离PG测试验证helper事务内GUC规范化、`transaction_isolation=repeatable read`、`transaction_read_only=on`，并确认nil UUID拒绝；另新增端到端repository/service用例，正常读取Get/List/Versions/Version/Resources并检查返回内容。focused命令`encore test ./thingmodel -run 'TestPostgresTenantReadTxSetsTenantAndReadOnlyIsolation|TestPostgresRepositoryReadMethodsUseTenantTransactions' -count=1` PASS（0.577s）；`encore test ./thingmodel -count=1` PASS（0.599s）；`go vet ./thingmodel`、`gofmt`、`git diff --check` PASS。静态连接池查询检索PASS（零命中）。Encore测试使用隔离test DB；未运行`encore check`，未访问local/default/prod DB。

包含前述Execute写切片的文件SHA-256：`encore-api/thingmodel/postgres.go` `545417026bd844a65d17157cdd5c17d8eeae82f64cbb28f77c15382dc823fa5e`；`encore-api/thingmodel/postgres_test.go` `76cb0344efb98cd2d9919a680c4b0fe334d4b653865e94d6fe33552eac38bc19`。本切片只证明这五个原生读API使用tenant-local只读事务、List/Versions语句组共享事务及常规成功读取；不证明RLS/FORCE RLS策略、非owner/BYPASSRLS权限隔离、transaction-local GUC在连接池事务结束/复用后的复位隔离、其他ThingModel DB路径或devicemetadata路径。DM-DB-002继续partial/blocking。

## 2026-10-26：DM-DB-002 当前残余路径审计（只读）

本审计按当前Kernel隔离候选源码复核，而非沿用旧证据。覆盖 `encore-api/devicemetadata` 生产Go数据库访问及其调用到的 `encore-api/thingmodel` repository；排除测试代码和纯内存验证。只读代码与计划文件，未执行数据库、Encore、迁移或HTTP命令，未启用RLS。

### 数据库路径、事务边界与tenant来源

| 分类 | 当前路径 | 连接池/事务边界 | Trusted tenant source 与上下文 |
|---|---|---|---|
| 已注册native DeviceGroup | `group_read_raw.go` list/detail/tree；`group_write.go` create；`group_update.go` update；`group_move.go` move；`group_members_raw.go` member-add/remove；内部SQL在`group_read.go`和`group_*.go` | `db.Stdlib().BeginTx`同一`*sql.Tx`。读使用只读Repeatable Read；写将业务、receipt、audit留在同一事务。helper在事务中首步参数化设置transaction-local GUC | tenant由Encore认证数据构建的principal提供，校验canonical非nil UUID；`beginTenantGroupTx`设置`app.current_tenant_id`。覆盖上述已注册8类操作，不自动覆盖旧endpoint或内部非principal调用 |
| 已注册native ThingModel写 | `encore-api/thingmodel/service.go` commands create/update/publish/delete/update-resources，经`postgresRepository.Execute` | `sqldb.Tx`跨锁、receipt、callback业务变化、audit/outbox与receipt提交；callback读写均由同一`postgresTransaction`执行 | `ReceiptScope.TenantID`从认证principal生成并经scope UUID校验；Execute设置规范化tenant-local GUC |
| 已注册native ThingModel读 | repository `Get/List/Versions/Version/Resources`，供native list/detail/versions/version/resources使用 | 只读Repeatable Read事务；List count/items、Versions模型检查和版本读取共用事务 | service从认证principal构建tenant参数；repository校验tenant/model UUID，GUC来自规范化tenant |
| 其余legacy ThingModel/device-metadata API | `service.go`旧ThingModel/items、模板、产品、设备及相关路径；底层大量SQL在`store.go`，辅助访问在`thingmodel_helpers.go` | 多数直接调用Encore全局`sqldb` pool的`db.Query/QueryRow/Exec`；部分多语句流程用pool.Begin，但无GUC；单语句路径无显式事务边界 | API入口一般`mustTenantID()`从`authn.CurrentData()`取JWT/Encore tenant；SQL多显式传tenant。内部helper接收调用者tenant。参数过滤不等于RLS数据库上下文 |
| legacy custom command controls | `custom_control_service.go` list/create/update/delete/get | pool级Query/Exec；create的ThingModel归属检查、insert、readback使用独立pool操作，不同连接/事务，无GUC | `mustTenantID()`取认证tenant并作为SQL条件/写入字段；检查与写入不是原子事务 |
| migration HTTP service、批量导入 | `migration_service.go` legacy模板转换、topic迁移、rollback、history、批量导入；`legacydb.go`另建legacy pool | 目标库经全局pool；部分写用`db.Begin`但未设GUC，检查/history有直连池查询。batch另从`TP_LEGACY_DATABASE_URL`对应pgxpool读数据，跨库不共事务 | 单租户HTTP请求起点是认证principal。batch枚举的tenant来自legacy DB，不可自动当作请求授权tenant；目标库写事务没有GUC。legacy读取与目标写入不是同一事务 |
| migration scripts / schema migrations | `devicemetadata/migrations/scripts`及SQL迁移runner | 独立迁移/工具连接，按全库迁移身份执行，不是tenant-scoped API事务 | tenant枚举/命令输入必须由受控迁移身份授权；不能把普通请求tenant上下文直接用于跨租户迁移。本次未确认迁移role与运行role配置 |

### 未设置GUC的剩余风险与覆盖阻塞

1. `store.go`是最大残余：旧ThingModel CRUD、items/versions辅助、DeviceTemplate、Product、Device生产SQL仍大量直接连pool。显式`tenant_id`谓词可降低正常误查，但无法由数据库强制；遗漏谓词、查询改写或RLS表访问会绕过tenant过滤保障，多语句事务也没有可靠RLS上下文。
2. `custom_control_service.go`各endpoint无GUC。create的tenant模型检查、insert和readback分属pool操作，除RLS缺口外也有语句间一致性/并发边界问题。
3. `migration_service.go`为双库特殊边界。batch tenant由legacy库枚举，需要先确定受控迁移actor对目标tenant集合的授权；在目标事务设置GUC也不能令两库原子化。
4. 旧`/api/...`端点仍和`/api/v2/...` native端点并存。新DeviceGroup和ThingModel覆盖不是旧ThingModel、模板、产品、设备、custom-control或migration路径的替代。提前强制RLS可能导致这些路径拒绝、漏数据或打断回滚。
5. 既有证据仅证明指定事务可以设置GUC，不证明RLS/FORCE RLS、非owner且非BYPASSRLS运行角色、事务结束后pool连接复用隔离、session残留清除或跨owner约束。全局NULL `meta_item_keys`及全局维护/读取接口也需明确policy例外/独立role。

### 推荐的下一最小推进切片

建议先完整迁移`custom_control_service.go`四个已注册入口到tenant事务helper：读使用只读Repeatable Read；create将ThingModel归属检查、insert、readback放进单一事务；update/delete明确同事务写及readback边界。tenant仍只取认证principal并校验canonical UUID，SQL继续显式带tenant。切片测试应验证事务内GUC、helper失败时不执行业务SQL、create/readback同tx、非法tenant fail closed及写失败回滚。该工作量局限在一个在线资源模块，却能覆盖真实CRUD并消除当前跨连接检查/写入路径；之后可按相同模式推进legacy ThingModel或Template CRUD。migration/batch另立切片，先冻结迁移身份及legacy tenant枚举授权规则。

此建议不启用RLS。只有全库路径和全局例外、运行role权限、跨owner策略、事务池复用隔离都完成验证后，才评估RLS/FORCE RLS。本次DM-DB-002维持partial/blocking。

### 文件指纹与未验证边界

审计源码SHA-256：`group_tenant_tx.go` `0848b0546c9ee4ce7e8f04868d3ca9ced180b3e89636399320920086d7b1857f`；`group_read.go` `a3fcd32aba1e81ce3e136ee57248417bb16f4090e9e306823bcf2fd9e627539b`；`group_write.go` `47eb3c05d21715b3036dae97133fd59cf51e3955b8368c2ded373df0bbaf1182`；`group_update.go` `90984f5730db3309e34efaf79c823e6827d14e7048bbb69e02f946cef8c9f6f6`；`group_move.go` `619933a9a663dab245fff0c611c5cfbfb95971503df87d3693954cfd62e9046e`；`group_members.go` `4ed9e932eb61db6215c47ad0c1c2126573cbe6ef0211148fc14d11fd70fb8e4d`；`store.go` `6018778faec6130665b844435b672037a31acb65f081773648c6c171c8280f22`；`custom_control_service.go` `17eff9a80ff131130abda96b17720a333afea8ff5a954caceb788ee345c03d15`；`migration_service.go` `a987c4a47675bfd8be1be2c408d919b4c350221e31b4a0d131d92589d962ef95`；`thingmodel_helpers.go` `3869df1205de5ce7e9f01ecbc89022385052cbf3f4e8fc9b042273670d2e5ada`；`encore-api/thingmodel/postgres.go` `545417026bd844a65d17157cdd5c17d8eeae82f64cbb28f77c15382dc823fa5e`；`encore-api/thingmodel/service.go` `178282a702baa88343c276e4a8093e204030554316c7dc5e5ee6b3f8bba8ed63`。本文为静态代码/计划审查，未审查动态运行时流量、独立外部调用者或所有数据库role部署实例；未执行Encore、数据库、migration、真实listener HTTP、非owner角色或pool复用试验。验证范围限计划JSON解析与`python3 validate-plan.py`。

## 2026-10-26：DM-DB-002 custom-command-control tenant事务切片

在Kernel隔离候选 `device-metadata-native-cutover-1.3/kernel` 将已注册 `ListCustomCommandControls`、`CreateCustomCommandControl`、`UpdateCustomCommandControl`、`DeleteCustomCommandControl` 从pool级SQL改为Encore `sqldb.Tx`。新增 `beginTenantScopedSQLTx` 的职责限定为tenant-scoped Encore SQL事务初始化：仅接受canonical lowercase非nil UUID；开始事务后（只读时先设置`REPEATABLE READ, READ ONLY`）以参数化 `set_config('app.current_tenant_id',$1,true)` 写入transaction-local上下文；任一初始化步骤失败立即rollback。tenant仍由每个API入口唯一调用 `mustTenantID()` 获得，业务SQL继续显式带tenant条件。

List的count与items使用同一只读Repeatable Read事务。Create将 `getThingModelByIDTx` tenant归属检查、custom-control INSERT和readback放在同一事务；readback改用tx专属查询。Update将tenant过滤UPDATE与readback放在同一事务；Delete在tenant过滤DELETE成功后commit。原HTTP路由、参数、返回结构/字段和业务错误语义保持不变；数据库写失败或readback失败时defer rollback，不提交半成品。未改其他store/group路径、API/OpenAPI、migration/RLS、CLI/FE、automation/notification或批量migration。

新增 `custom_control_service_test.go`：验证事务helper在读/写事务内的tenant GUC，读事务为Read Only + Repeatable Read，拒绝空/nil/非canonical tenant；在认证服务上下文中覆盖create/list/update/delete的正常返回与删除后列表为空；覆盖nil tenant fail-closed、跨tenant ThingModel归属拒绝且无control写入、数据库拒绝的create无残留、数据库拒绝的update保留原值。源码将归属检查、插入和readback使用同一个 `*sqldb.Tx`；测试侧验证服务结果及helper上下文，但不声称测试了RLS策略执行。

验证均在Kernel `encore-api` 使用Encore隔离test DB：`encore test ./devicemetadata -run 'Test(BeginTenantScopedSQLTx|CustomCommandControl)' -count=1` PASS（0.622s）；`encore test ./devicemetadata -count=1` PASS（2.049s）；`go vet ./devicemetadata` PASS；`gofmt`及`git diff --check` PASS。计划JSON解析PASS，`python3 validate-plan.py` PASS（188 tasks、580 test cases、errors=[]；`baseline_unavailable` warning）。未运行`encore check`、local/default/prod DB、迁移或真实listener HTTP。

切片文件SHA-256：`encore-api/devicemetadata/custom_control_service.go` `974d89813c13c0ffe35175b23c4301460db777a549331fcd5f1425bde6d231ca`；`encore-api/devicemetadata/custom_control_service_test.go` `a64f25d22329f82f81e4fc4f69c56811edc957776be4c0dd8956e12baf14550d`。仍未证明RLS/FORCE RLS、非owner非BYPASSRLS运行role、所有pool连接的transaction-local状态复用隔离、其他legacy store/migration路径、跨owner策略或生产runtime HTTP；因此DM-DB-002保持partial/blocking，不启用RLS。

## 2026-10-26：DM-DB-002 DeviceTemplate读取tenant事务切片

将此前custom-control事务切片的 `beginTenantScopedSQLTx` 与 `wrapCustomControlTxError` 从 `custom_control_service.go` 移到 `tenant_sql_tx.go`，函数名和语义保持；前者只接受canonical lowercase非nil tenant UUID，在Encore `sqldb.Tx`中设置Read Only + Repeatable Read（读路径）并以参数化 `set_config('app.current_tenant_id',$1,true)`设置transaction-local GUC，初始化失败rollback；后者保留Unauthenticated，其他初始化错误映射Internal。custom-control调用行为不变。

本切片仅为 `store.go` 的 `listDeviceTemplates`、`getDeviceTemplate`、`listDeviceTemplatesByThingModel` 加tenant只读事务。三个查询仍使用显式tenant条件；list分页查询保留原 `COUNT(*) OVER()`、过滤、排序、LIMIT/OFFSET和响应行为，count语义/排序未改；成功读取后commit，查询/扫描/提交错误返回原有类别并由defer rollback。`getDeviceTemplate`也被publish/archive/derive/bind等旧写流程内部调用：本切片只让该读取SQL使用自己的只读tenant事务，不表示这些复合写操作已获得共同tenant transaction；写/发布/删除操作未变更。

新增 `device_template_tenant_tx_test.go`，使用Encore authenticated service context覆盖List/Get/ListByThingModel正常结果，断言分页总数、页大小、更新时间排序和返回tenant；另直接通过通用helper读取当前事务GUC、`transaction_read_only=on`及`transaction_isolation=repeatable read`。测试证明被测API读路径成功，配合源码可见三者均调用helper；没有启用RLS或声称策略执行测试。

验证均限Kernel `encore-api` 的Encore隔离test DB：`encore test ./devicemetadata -run '^TestDeviceTemplateReadPathsUseTenantScopedTransactions$' -count=1` PASS（0.785s）；`encore test ./devicemetadata -count=1` PASS（2.211s）；`go vet ./devicemetadata`、gofmt、`git diff --check` PASS。计划JSON解析及 `python3 validate-plan.py` PASS。未运行`encore check`，未访问local/default/prod DB、迁移或真实listener HTTP。

文件SHA-256：`encore-api/devicemetadata/store.go` `ec8f8649531cfc0d80cdb5175e6f30b5b32e31efb853810c3950f5b60460490b`；`encore-api/devicemetadata/custom_control_service.go` `ad33e060726ef21c6ab6a9a7325af6716a5ec731f214d25d4d91d7f220c8c380`；`encore-api/devicemetadata/tenant_sql_tx.go` `1dbff90104327e65dba22b6c25ee7aadd36089582e85b64824e405b5e22764a7`；`encore-api/devicemetadata/device_template_tenant_tx_test.go` `ebcc36c9fdd29a6bac13adc757a7cd25fe2657bc06087748e03f22867f2b2fbf`。仍未证明RLS/FORCE RLS、非owner/BYPASSRLS运行角色、pool复用隔离、被Get调用的复合写操作事务边界、其他legacy store/migration路径或真实runtime HTTP；DM-DB-002保持partial/blocking，不启用RLS。

## 2026-10-26：DM-DB-002 DeviceTemplate创建/更新tenant事务切片

仅重构 `store.go` 的 `createDeviceTemplate` 与 `updateDeviceTemplate`。两个API入口仍由既有service调用 `mustTenantID()` 获得并规范化tenant，再开启tenant-local `sqldb.Tx`；helper在事务中设置GUC后才执行业务SQL。Create使用同一事务锁定/校验tenant内ThingModel、查询并校验snapshot属于该模型、执行`INSERT ... RETURNING`，最后commit。Update在同一事务内锁定当前tenant模板，保留ARCHIVED不可变检查，再锁定/验证目标ThingModel、检查snapshot归属、执行`UPDATE ... RETURNING`并commit。创建/更新相关业务SQL继续显式带tenant条件。

为清楚验证同事务边界，抽出 `createDeviceTemplateTx` 和 `updateDeviceTemplateTx`，只接受调用者已打开并配置GUC的事务；外层API方法负责验证必填字段、启动helper事务、调用tx函数、commit，错误由defer rollback。保持原字段映射、状态默认、唯一冲突AlreadyExists、目标模型缺失NotFound、错误snapshot InvalidArgument和ARCHIVED FailedPrecondition语义。没有改publish/archive/delete/derive/bind，其他store方法，API/OpenAPI，迁移/RLS或authz/前端/CLI。

新增 `device_template_write_tenant_tx_test.go`，通过Encore authenticated service context覆盖CreateTemplate、GetTemplate回读和UpdateTemplate成功；跨tenant ThingModel返回NotFound，其他同tenant模型的snapshot错配返回InvalidArgument，数据库长度约束失败的Create无残留、失败Update保留原值，ARCHIVED模板拒绝更新且状态不变。测试另直接走tx函数：比较操作前后的`txid_current()`并检查tenant GUC，随后rollback并确认Create记录未持久化，证明校验/写函数接受并复用同一事务。正常Create/Update API的隔离PG数据结果同时由service调用验证。

验证均在Kernel `encore-api` 使用Encore隔离test DB：focused `encore test ./devicemetadata -run 'Test(DeviceTemplateCreateUpdateUseTenantTransactions|DeviceTemplateReadPathsUseTenantScopedTransactions|CrossTenantDeviceTemplateIsolation|DeviceTemplateStateTransitionDraftToPublished)' -count=1` PASS（0.471s）；全包 `encore test ./devicemetadata -count=1` PASS（2.290s）；`go vet ./devicemetadata`、gofmt、`git diff --check` PASS。计划JSON解析与 `python3 validate-plan.py` PASS；未运行`encore check`，未访问local/default/prod DB、迁移或真实listener HTTP。

切片SHA-256：`encore-api/devicemetadata/store.go` `1616d58feea4e3a926419dd9bf09322f72496fd43ed70506350794b485246e79`；`encore-api/devicemetadata/device_template_write_tenant_tx_test.go` `0587679c227801842232fc75da80e424a4a41913f1ba40860f00746842453d1a`。本证据不表示DeviceTemplate的publish/archive/delete/derive/bind已获得统一tenant事务，也不证明RLS/FORCE RLS、非owner/BYPASSRLS角色、pool复用隔离、其他legacy store/migration路径或真实listener HTTP；DM-DB-002继续partial/blocking，不启用RLS。


## 2026-10-26：当前候选Linux镜像构建通过，listener验收受Docker daemon中断阻塞

目标源码位于隔离kernel worktree，HEAD=`fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`；包含12个tracked Encore源码改动以及本轮分组/custom-control/DeviceTemplate测试和0018–0022迁移等untracked文件。Luna在独立挂载的`/Volumes/DevScratch/codex-dm-http-acceptance-20261026-131718-b6b04c`创建普通仓库副本并保留仓库根目录；293个Encore相关文件逐项SHA-256与候选匹配，manifest摘要=`2cb0497bc953d0ac42ac0e74f6157a85baa72bb87b9f6c30a9e6ba5ae286e727`。以`TMPDIR`指向该卷执行`encore build docker --services devicemetadata,authn --os linux --arch arm64`，构建exit 0，镜像`codex-device-metadata-current:codex-dm-accept-131718-b6b04c`。未使用passthrough或push。

listener阶段创建本轮独立PostgreSQL容器`codex-dm-accept-131718-b6b04c-pg`及network`codex-dm-accept-131718-b6b04c-net`。readiness命令在60秒内未返回`postgres_ready=yes`；等待中OrbStack Docker API socket变为不可达（`unix:///Users/junhong/.orbstack/run/docker.sock`不存在），shell随后以Ctrl-C中断并返回130。未开始migration loop、未启动app、未调用HTTP，故分组/custom-control/DeviceTemplate HTTP、认证challenge和Problem响应均无本轮结果。第一次错误地把旧隔离harness `infra.json`传给build CLI，构建失败且未启动/连接DB；后续构建使用本轮合成配置。未重启daemon，未触碰既有容器、镜像或卷；因Docker API不可达，本轮PG/network是否仍在daemon中存在及清理状态无法确认。DevScratch上的候选副本与构建临时目录保留，供daemon恢复后核对。DM-API-004和DM-DB-002不因此关闭；本记录只证明当前dirty候选可构建，不证明运行态或HTTP验收。

## 2026-10-26：DM-TPL-005 publishDeviceTemplate单一tenant事务切片

Kernel候选HEAD=`fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`（dirty worktree；本切片只改`encore-api/devicemetadata/store.go`中的publish实现并新增独立测试文件）。`publishDeviceTemplate`现在通过`beginTenantScopedSQLTx(ctx, tenantID, false)`开启唯一写事务；`publishDeviceTemplateTx`先以tenant/id/deleted过滤并`SELECT ... FOR UPDATE`锁模板，再在同一事务检查DRAFT、校验protocol type/config、通过`validateDeviceTemplateSnapshotTx`检查同tenant snapshot及model归属、转换状态并`UPDATE ... RETURNING`。外层只在所有步骤成功后commit；其余返回由defer rollback。对已PUBLISHED模板仍返回`FailedPrecondition`，message为`cannot transition from PUBLISHED to PUBLISHED`。并发请求在同一行锁串行化，第二个请求读取已提交的PUBLISHED状态并沿用此业务返回合同。没有改archive/delete/derive/bind、schema/migration、runtime/HTTP/API契约。

新增`device_template_publish_tx_test.go`覆盖：成功发布且读回PUBLISHED；协议校验在tenant GUC及同一tx中运行、无效config不改DRAFT；同tenant snapshot错model返回InvalidArgument、外tenant snapshot在tenant作用域内不可见并返回NotFound且模板状态不变；状态更新成功后注入后续SQL错误并rollback，状态仍为DRAFT；并发测试先由隔离事务持锁，确认两个发布请求均在PostgreSQL row lock上等待后释放，结果恰好一次成功、一次以PUBLISHED状态FailedPrecondition返回。

验证DB边界：所有Encore PG用例通过`encore.dev/storage/sqldb`声明的`device_metadata`资源访问；使用`encore test`。Encore文档说明运行测试时为测试单独提供SQL database cluster（优化临时测试性能）。没有读取连接配置、直接连接local/default/prod数据库或手工执行migration。未运行`encore check`。

实际结果（工作目录`kernel/encore-api`）：首次focused测试exit 1，原因是错误展示字符串含code前缀而断言误按裸message匹配；同一次还尝试安装测试约束，但Encore测试role不是表owner，DDL被拒。测试改为检查错误包含既有message，并在隔离事务中先完成状态写、再注入SQL错误后rollback，不修改schema。最终focused `encore test ./devicemetadata -run '^TestPublishDeviceTemplate' -count=1 -v` exit 0，4项测试及cross-tenant/wrong-model子用例PASS（29.223s）；全包`encore test ./devicemetadata -count=1` exit 0（2.530s）；`go vet ./devicemetadata` exit 0；gofmt与`git diff --check`通过。

文件SHA-256：`encore-api/devicemetadata/store.go` `ef7794910d7887bf550719b608edb00ea15b2f66812c8066d07ce19035afe923`；`encore-api/devicemetadata/device_template_publish_tx_test.go` `3ec3f07a6a54cf5ce671ee8e47b6a57b5d41be512f667eee76744c485845c451`。这些是整个dirty文件的SHA，不表示该文件其他existing changes属于本切片。

范围边界：复合FK会阻止把跨tenant/跨model snapshot写入真实模板，因此跨tenant snapshot用例直接测试publish所调用的事务校验helper并验证模板未变，未构造破坏性脏外键行；写入回滚用例验证发布状态写入后的事务错误回滚，不是status UPDATE语句自身失败注入。模板仍没有不可变接入配置revision/digest/审计指针，因此DM-TPL-005仅部分实现、保持blocking。DM-DB-002仍缺RLS/FORCE RLS、完整非owner运行身份与连接池复用隔离以及其余DB路径验收，继续partial/blocking。未验证真实listener HTTP、公开错误媒体类型或部署运行角色。

## 2026-10-26：DM-DB-002 / DM-TPL-005 DeviceTemplate derive tenant事务切片

仅将`deriveDeviceTemplateDraftAdvanced`改为tenant-local单一写事务。事务内显式按tenant/id/deleted过滤并`SELECT ... FOR UPDATE`锁源模板，验证源状态仍为`PUBLISHED`，再按现有字段映射和`(draft)`命名构造新DRAFT；调用`createDeviceTemplateTx`在同一事务内验证ThingModel/snapshot归属并插入，外层仅成功后commit，错误由defer rollback。源锁与archive写路径相同，因此并发时derive要么先完成并产生DRAFT、archive随后完成，要么archive先提交而derive读到ARCHIVED并沿用`FailedPrecondition`，不会从已归档源创建草稿。既有HTTP/JSON字段、租户来源与SQL显式tenant谓词不变；没有改bind/delete或其他store路径。

新增`device_template_derive_tx_test.go`，经authenticated service context覆盖成功derive和读回、非PUBLISHED拒绝及跨tenant隐藏；在隔离Encore PG事务内断言derive helper前后txid一致、GUC仍为该tenant，并在插入后注入SQL错误验证rollback不留草稿；另以持有源行锁的并发用例覆盖derive/archive串行化的两种合法顺序。测试未改schema，不直接连接local/default/prod DB。

验证（Kernel `encore-api`）：`encore test ./devicemetadata -run 'TestDeriveDeviceTemplateDraft' -count=1` PASS（0.725s）；`encore test ./devicemetadata -count=1` PASS（2.593s）；`go vet ./devicemetadata` PASS；gofmt与`git diff --check` PASS。计划JSON解析及`python3 validate-plan.py`通过；未运行`encore check`、migration或真实listener HTTP。

文件SHA-256：`encore-api/devicemetadata/store.go` `6c157d58ff5b70d529188b62d77636ccea41822d134414e4f9b13764519b925f`；`encore-api/devicemetadata/device_template_derive_tx_test.go` `8eec6a6067c31cf47c3af83489c0a2379a65516ae8afd6f4b388401ff3e04e71`。这些是dirty文件整体SHA，不能将同文件其他原有改动归因于本切片。

本切片未证明DeviceTemplate bind/delete及其他legacy store、migration/batch路径的tenant-local上下文；未证明RLS/FORCE RLS、非owner非BYPASSRLS运行角色、连接池事务结束/复用隔离或真实listener HTTP。不可据此启用RLS；DM-DB-002与DM-TPL-005仍partial/blocking，模板不可变revision/digest/审计指针合同仍未完成。


## 2026-10-26：OrbStack恢复后的隔离迁移演练与镜像tag复核

用户已自行重启OrbStack。Docker socket恢复；本轮创建的隔离network `codex-dm-accept-131718-b6b04c-net`仍存在，PG容器`codex-dm-accept-131718-b6b04c-pg`已重新启动，`pg_isready -U postgres -d postgres`返回accepting connections。只读核对Encore临时配置的数据库host指向该容器，目标`device_metadata`数据库public schema此前为0表；未连接任何local/default/prod数据库。按当前隔离kernel候选migration目录顺序将16个`.up.sql`应用到此一次性数据库，均返回exit 0（0001–0007、0014–0022）。迁移后只读catalog查询得到18张public基础表、14个已验证外键、2张device-group表、18个tenant_id不可变触发器。此证据证明当前迁移文件可落入新鲜PostgreSQL并形成所查catalog对象，不等价于完整migration reconciliation/rollback演练或应用验收。

本轮原候选副本的293个文件manifest仍与当前kernel工作树逐文件SHA匹配（commit `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`）；之后publish事务切片新增`device_template_publish_tx_test.go`并更新`store.go`，因此副本需要重建后才能用于最新候选。恢复后，Encore内置Go1.27链接器在app入口链接panic；使用系统Go1.26.2通过`ENCORE_GOROOT=/usr/local/go`执行构建命令，Encore输出编译/导出exit 0，但Docker daemon没有该新tag，`docker image inspect codex-device-metadata-current:codex-dm-accept-rebuild-20261026-4`返回No such image。旧候选镜像tag仍因缺失overlay layer路径而不能inspect。故本轮仍没有可运行的最新候选镜像，没有启动listener或调用HTTP；401 challenge、RFC9457、Group/custom-control/DeviceTemplate HTTP均未验收。未删除/修改其他容器、镜像或卷。DM-API-004、DM-DB-002保持blocking。


## 2026-10-26：当前候选 Linux listener 最小权限 HTTP 验收

本节记录当前dirty候选的隔离运行证据，不替代完整路由/授权/生产ingress验收。源候选为Kernel worktree HEAD `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`；以独立普通Git clone保留根目录构建，源清单754项逐文件内容SHA、模式/符号链接曾核验一致。清单路径 `/Volumes/DevScratch/dm-http-standard-20261026T061459Z-5a223f82/source-manifest.json`，SHA-256 `a21f204796668612fa8dd034d0a84f5e6749c126b98839008c26eced0723305d`；clone位于同目录 `kernel`。构建工作目录为 `kernel/encore-api`，命令：

`ENCORE_GOROOT=/usr/local/go TMPDIR=/Volumes/DevScratch/dm-http-standard-20261026T061459Z-5a223f82/tmp-build-20261026-5a223f82 encore build docker --services devicemetadata,authn --config /Volumes/DevScratch/dm-http-standard-20261026T061459Z-5a223f82/http-infra-20261026-5a223f82.json --os linux --arch arm64 codex-dm-http-accept-20261026-5a223f82:latest`

构建exit 0。精确镜像tag为 `codex-dm-http-accept-20261026-5a223f82:latest`，image ID `sha256:7091943c790898851e1aa0eed4676e41a3a945714d811d640be615cee73c50bf`，平台 `linux/arm64`。infra JSON SHA-256 `dd00e6d1163f9f21bfd7116dd43340694104a1c4f0aa12eb873f0104e4cc8ee1`，只含metadata和一个SQL server/database，host仅指向任务专用 `codex-dm-accept-131718-b6b04c-pg:5432/device_metadata`；数据库password为 `{"$env":"DB_PASSWORD"}` 引用。镜像inspect的Config.Env key仅 `ENCORE_INFRA_CONFIG_PATH`，生成的数据库密码值不在Config.Env。构建日志保留于 `http-build-20261026-5a223f82.log`，SHA-256 `11c27dae09ae7bd59c3298493a95962f7a3dd326b562d41c9237dd1451bbeffe`。

数据库只使用任务专用Postgres `codex-dm-accept-131718-b6b04c-pg` 与network `codex-dm-accept-131718-b6b04c-net`。经容器本地socket认证后创建runtime角色 `codex_dm_http_rt_20261026_5a223f82`；确认NOSUPERUSER/NOCREATEDB/NOCREATEROLE/NOBYPASSRLS，仅CONNECT `device_metadata`、USAGE `public`，仅SELECT `device_groups`、`device_group_members`、`devices`；`metadata_audit_events` SELECT与`device_groups` INSERT均为false。新生成的密码仅保存在DevScratch下0600 runtime env文件并运行时注入；没有读取/记录密码、JWT key或合成token值。两份合成JWT为HS256签名并经task-network session stub逐请求验证，auth mode为`legacy-jwt`，未启用dev/passthrough或外部认证。

仅在该临时数据库插入两条标记组：Tenant A `11111111-1111-4111-8111-111111111111` / `codex-http-a-5a223f82` / ID `33333333-3333-4333-8333-333333333333`；Tenant B `22222222-2222-4222-8222-222222222222` / `codex-http-b-5a223f82` / ID `44444444-4444-4444-8444-444444444444`。使用最小权限和该两条数据启动容器 `codex-dm-http-runtime-v3-20261026-5a223f82`，只发布loopback `127.0.0.1:32771 -> 8080`，HTTP矩阵脚本用Python标准库 `http.client`，exit 0：

|请求|结果|响应摘要|
|---|---|---|
|无Authorization读取 `/api/v2/device-groups`|401|`application/json`，keys=`code/details/message`，无`WWW-Authenticate`|
|伪造Bearer读取列表|401|`application/json`，keys=`code/details/message`，无`WWW-Authenticate`|
|Tenant A合成session读取列表|200|`application/json`，仅A marker，`tenantId=A`，`total=1`|
|Tenant B合成session读取列表|200|`application/json`，仅B marker，`tenantId=B`，`total=1`|
|Tenant A读取Tenant B group detail|404|`application/problem+json`，Problem七字段，`status=404`、`code=not_found`|
|Tenant A请求 `?rootOnly=invalid`|400|`application/problem+json`，Problem七字段，`status=400`、`code=invalid_argument`|

本轮listener只验证了真实JWT签名、Community live-session、group读路径与两tenant数据隔离；直接listener的401没有Bearer challenge。按`API-CONTRACT.md`认证引导合同，Encore原生401 body保留，但对外ingress必须补`WWW-Authenticate: Bearer`；当前直连缺challenge仍不通过合同，统一ingress生产接入未闭合，DM-API-004保持partial/blocking。业务handler参数错误的RFC9457边界通过。没有覆盖custom-control CRUD、Group POST、DeviceTemplate GET/POST/PUT、受限scope/完整operation矩阵，也未运行`encore check`。

验收后停止本轮三个runtime容器（`codex-dm-http-runtime-20261026-5a223f82`、`codex-dm-http-runtime-v2-20261026-5a223f82`、`codex-dm-http-runtime-v3-20261026-5a223f82`），均为exited以关闭loopback端口；未删除任何容器、network、volume、image或文件。任务PG与session stub容器保留运行。首个挂载脚本stub `codex-dm-http-session-20261026-5a223f82`未通过listener连接探针；随后无挂载脚本的stub `codex-dm-http-session-v2-20261026-5a223f82`返回合成session预期identity并用于验收；本轮启动的诊断子进程已精确结束。

临时证据目录为 `/Volumes/DevScratch/dm-http-standard-20261026T061459Z-5a223f82`。infra JSON、构建日志、source manifest、初版stub脚本SHA如上；无secret值的初版stub脚本SHA-256为 `3d4d90538ea5c52b1d548f3c2f2f6b9ee2260d9fc1784b2f55e017954c39ab6c`。`runtime.env`、`runtime-http-v2.env`、`http-fixture.env`、`session-stub.env`以0600保留但不记录其内容或hash。计划文档修改前副本保留在 `plan-doc-baseline-20261026-5a223f82/`，用于证明本次只更新指定文件。

## 2026-10-26：DM-TPL-005 / DM-DB-002 DeviceTemplate archive tenant事务切片

只读核实原 `archiveDeviceTemplate` 先经独立 `getDeviceTemplate` 查询，再单独UPDATE；函数合同允许DRAFT/PUBLISHED→ARCHIVED，已ARCHIVED返回FailedPrecondition `ARCHIVED templates cannot be modified`，非法状态沿用`CanTransition`错误；无匹配UPDATE则FailedPrecondition `device template not found or is already archived`。本切片保持这些状态/错误语义，不改service/API/OpenAPI或其他生命周期方法。

现在 `archiveDeviceTemplate` 使用既有`beginTenantScopedSQLTx(ctx, tenantID, false)`建立唯一写事务，GUC初始化后调用`archiveDeviceTemplateTx`。tx函数以tenant/id/deleted过滤查询完整模板并`FOR UPDATE`锁行，在锁内检查ARCHIVED和`CanTransition`，然后在同一事务执行 tenant-scoped `UPDATE ... RETURNING`。外层仅成功后commit，业务/DB错误走defer rollback。并发重复归档被行锁串行化：一请求提交，另一请求锁后观察ARCHIVED并按FailedPrecondition拒绝。服务入口仍由`mustTenantID()`提供tenant。

新增 `device_template_archive_tx_test.go`，通过authenticated service context验收DRAFT归档成功、重复归档FailedPrecondition、跨tenant及不存在ID返回NotFound。并发用例先持有模板行锁、等待两个archive请求在`FOR UPDATE`阻塞后释放锁，断言恰好一个成功、一个FailedPrecondition并最终状态ARCHIVED。回滚用例在同一tenant事务内调用`archiveDeviceTemplateTx`，确认事务ID前后一致且GUC tenant匹配，再注入后续SQL除零错误并rollback，断言数据库状态仍为DRAFT。

验证均在Kernel `encore-api` 使用Encore隔离test DB：focused `encore test ./devicemetadata -run 'Test(ArchiveDeviceTemplateTenantTransactionAndContract|PublishDeviceTemplateCommitsValidatedDraft|PublishDeviceTemplateSerializesConcurrentDuplicateRequests|DeviceTemplateCreateUpdateUseTenantTransactions)' -count=1` PASS（0.546s）；全包 `encore test ./devicemetadata -count=1` PASS（2.700s）；`go vet ./devicemetadata`、gofmt、`git diff --check` PASS。未运行`encore check`，未访问local/default/prod DB、migration或真实listener HTTP。

切片SHA-256：`encore-api/devicemetadata/store.go` `5f13fe2a5d2603739be7a0b7862cc90c16806a2aa85bcf1fb478c0f558f29628`；`encore-api/devicemetadata/device_template_archive_tx_test.go` `1a92a23d51d161ecc72a240a307850d4153794c388251d884cb94519e7bc7fb7`。仅archive写路径获证；不表示publish、delete、derive、bind或其他legacy路径完成，不证明RLS/FORCE RLS、非owner/BYPASSRLS角色、pool复用隔离或真实runtime HTTP。DM-TPL-005与DM-DB-002仍partial/blocking，不启用RLS。

## 2026-10-26：DM-DB-002 / DM-TPL-005 DeviceTemplate bind tenant事务切片

仅将`bindThingModelVersionAdvanced`改为单一tenant-local写事务。事务初始化后按tenant/id/deleted条件锁定模板行（`FOR UPDATE`），锁内保持原ARCHIVED拒绝语义；随后在相同事务内检查snapshot属于当前tenant且ThingModel ID一致，再执行显式tenant过滤的`UPDATE ... RETURNING`并commit。缺失/外tenant snapshot继续返回`NotFound`和原消息`thing model snapshot not found`，同tenant但属于另一ThingModel继续返回`InvalidArgument`，ARCHIVED继续返回原`FailedPrecondition`。service仍只从`mustTenantID()`取得tenant；未改delete、API/OpenAPI、migration/RLS或其他store路径。

新增`device_template_bind_tx_test.go`：authenticated service context覆盖绑定到同模型新snapshot和读回、ARCHIVED拒绝、外tenant/缺失snapshot隐藏、错ThingModel拒绝；事务helper级断言bind前后txid一致、GUC匹配tenant，并在更新后注入SQL错误再rollback，确认已提交snapshot指针不变。并发测试由第三个事务持有模板行锁，再并行bind/archive并等待两者阻塞；结果只允许bind先行成功后archive，或archive先行成功后bind按ARCHIVED拒绝，最终状态均为ARCHIVED。未对未改动的delete流程做串行性主张。

验证（Kernel `encore-api`）：`encore test ./devicemetadata -run 'TestBindDeviceTemplateVersion' -count=1` PASS；`encore test ./devicemetadata -count=1` PASS；`go vet ./devicemetadata`、gofmt、`git diff --check` PASS。未运行`encore check`、migration或local/default/prod DB。

文件SHA-256：`encore-api/devicemetadata/store.go` `8c5f276339504c257a13f988ae6d585d920282ab3e571a82c5bb5b8d0bcb395c`；`encore-api/devicemetadata/device_template_bind_tx_test.go` `f118b326dc65216aabc94526deecfbb8de056d5bba6380aeab8d2bb5200e25b0`。SHA针对dirty文件整体，不将同文件既有改动归因于bind切片。

本切片不证明delete或其他legacy/migration DB路径的tenant上下文，不证明RLS/FORCE RLS、非owner/BYPASSRLS运行角色、连接池事务结束/复用隔离或真实listener HTTP。DeviceTemplate不可变revision/digest/audit pointer合同仍未完成；DM-DB-002与DM-TPL-005继续partial/blocking，不启用RLS。

## 2026-10-26：DM-DB-002 DeviceTemplate本地引用与删除竞态切片

仅覆盖本地`products.device_template_id`、`devices.device_template_id`引用与`device_templates`软删除。`deleteDeviceTemplate`现在开启tenant-local事务，`deleteDeviceTemplateTx`先以tenant/id/未删除条件`SELECT ... FOR UPDATE`锁模板，再在该事务检查未软删除的本地Device引用、Product引用，保持原冲突顺序/消息（设备后产品），没有引用时同tx写`deleted_at`并commit。缺失/外租户模板继续隐藏为NotFound。没有更改DeviceAccessTemplate、其他外部owner或其他引用生命周期。

`createProduct`和`createDevice`也改为tenant-local事务：均对目标模板执行`FOR SHARE`，在共享锁内按既有合同检查模板可见性/状态，并在同一事务INSERT引用。PostgreSQL的FOR SHARE彼此兼容，允许同模板下多个创建并行；它与delete/archive等状态变更持有的FOR UPDATE冲突，仍保证创建检查、插入和软删除串行。Product仍允许非ARCHIVED模板、ARCHIVED返回既有TEMPLATE_ARCHIVED；Device仍只允许PUBLISHED模板。Device的product/parent关联读取及SN检查现在复用同tx；voucher/SN仍由本地代码生成并只写DB，没有新增MQTT、credential或其他外部调用。DB unique约束继续负责并发SN冲突最终裁决。锁模式不依赖FK自动取得且与UPDATE锁相容的KEY SHARE；多个创建可共享FOR SHARE，而delete/archive的FOR UPDATE与其冲突。

新增`device_template_reference_tx_test.go`：authenticated service context覆盖本地Product/Device引用阻止删除、无引用删除后Get不可见、跨tenant删除NotFound；在delete tx helper内验证txid/GUC，注入后续SQL失败并回滚，确认模板仍可读。另对Product和Device分别运行真实并发create-vs-delete锁竞争：断言只允许create先提交且delete按既有合同返回FailedPrecondition，或delete先提交且create因模板消失返回NotFound；不接受两者均成功/软删除留下新增活引用。测试仅检查本地数据库引用，不将结果外推为DM-TPL-009完成。

验证（Kernel `encore-api`）：`encore test ./devicemetadata -run 'TestDeviceTemplate(DeleteSerializesLocalReferences|DeleteRacesReferenceCreation)' -count=1` PASS（0.696s）；`encore test ./devicemetadata -count=1` PASS（2.639s）；`go vet ./devicemetadata`、gofmt、`git diff --check` PASS。计划JSON解析与`python3 validate-plan.py` PASS（188 tasks、580 cases、errors=[]；warning为`baseline_unavailable`）。未运行`encore check`或migration，未连接local/default/prod DB。

文件SHA-256：`encore-api/devicemetadata/store.go` `9a66f9784ca13d5ce8a5ab0133e99dce6af7d242513c1705fd72e9341937e7ba`；`encore-api/devicemetadata/device_template_reference_tx_test.go` `fbe605afd940b4a2ca9826164e1e26714cf77a5622ecc4f0147ae606ec0c44d8`。均为dirty文件整体SHA，不能把其中其他既有改动归因于本切片。

边界：只证明本地Product与Device引用在模板软删除和create入口之间的事务锁顺序/测试DB表现；未覆盖Product/Device后续变更/删除、其他store路径、外部引用owner和DeviceAccessTemplate，未证明RLS/FORCE RLS、非owner/BYPASSRLS策略、连接池事务结束/复用隔离或真实listener。不能据此关闭DM-DB-002或宣称DM-TPL-009完成；DM-DB-002继续partial/blocking，不启用RLS。

## 2026-10-26 07:01 UTC：OrbStack恢复后隔离listener HTTP复验（DM-API-004 / TC-DM-API-004-02；DM-API-002 / TC-DM-API-002-02部分覆盖）

本次复验使用Kernel候选普通独立clone `/Volumes/DevScratch/dm-http-standard-20261026T061459Z-5a223f82/kernel`，HEAD `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`，候选源码逐文件manifest SHA-256 `a21f204796668612fa8dd034d0a84f5e6749c126b98839008c26eced0723305d`。应用镜像为 `codex-dm-http-accept-20261026-5a223f82:latest`，精确image ID `sha256:7091943c790898851e1aa0eed4676e41a3a945714d811d640be615cee73c50bf`（linux/arm64）。运行环境仅使用本轮既有隔离资源：PostgreSQL `codex-dm-accept-131718-b6b04c-pg`、network `codex-dm-accept-131718-b6b04c-net`、synthetic session stub `codex-dm-http-session-v2-20261026-5a223f82`；应用只发布loopback临时端口，runtime `codex-dm-http-runtime-v4-20261026-5a223f82`。配置目标为该隔离PostgreSQL的`device_metadata`库并以运行时环境引用注入DB密码；未读取或记录密码/token。验收完成后仅停止本轮runtime，inspect为exited/exit 0；没有删除或停止PG、stub、network、volume、image或文件。

真实JWT签名及live-session请求（A/B为合成tenant身份）结果：

| 请求 | 结果 | 关键响应摘要 |
|---|---:|---|
| A、B分别GET `/api/v2/device-groups` | 200 | `application/json`；各自只返回本tenant合成group marker，`total=1` |
| A读取B的group detail | 404 | `application/problem+json`；七字段Problem，`code=not_found` |
| A请求 `?rootOnly=invalid` | 400 | `application/problem+json`；七字段Problem，`code=invalid_argument` |
| 无Bearer，且显式`Accept: application/problem+json`，直连listener | 401 | `application/json`，EncoreAuthError形态`code=unauthenticated`；无`WWW-Authenticate` |
| 非法Bearer，且显式`Accept: application/problem+json, application/json`，直连listener | 401 | `application/json`，EncoreAuthError形态`code=unauthenticated`；无`WWW-Authenticate` |
| 上述两种401经本地loopback shim重放 | 401 | body与直连一致；shim补`Www-Authenticate: Bearer realm="device_metadata"` |
| 非法业务参数经shim | 400 | `application/problem+json`，Problem状态/错误码保持；无认证challenge |

401结果依据显式Accept头的复测原始文件，避免把前一次未带Accept的curl结果误作Accept协商证据。缺失/非法Bearer在直连Encore listener上均未满足Bearer challenge合同；shim行为仅证明隔离验证中challenge补充可行，不证明统一公开ingress已部署或验收。RFC9457业务handler错误通过；RFC9457不要求把框架预处理认证错误改写为Problem。按`API-CONTRACT.md`预处理认证错误保留EncoreAuthError、ingress补Bearer challenge的职责划分，DM-API-004与TC-DM-API-004-02仍partial/blocking。DM-API-002/TC-DM-API-002-02只部分覆盖了真实签名JWT/live-session成功路径；未覆盖完整token拒绝矩阵（含exp/nbf/revocation/role等）。

原始脱敏响应均保留于 `/Volumes/DevScratch/dm-http-standard-20261026T061459Z-5a223f82/`，响应文件权限为0600；代表性显式Accept直连/经shim响应及SHA-256：

- `curl-v5-missing-bearer-accept-problem.direct.http.txt`：`d9ee24bc5450128d1c07fc0ad72bd88df7c6f6c80986f1d232f1a07a67f635d0`
- `curl-v5-missing-bearer-accept-problem.shim.http.txt`：`83e60cf6e20aab7d42a0e854e49f4af0a36d73d8b9972596e7da4df4b1bb262c`
- `curl-v5-invalid-bearer-accept-problem.direct.http.txt`：`44412b740c0ab35300e193b4b4908d3c8506afd1d8fda61b4a6f004b10241c83`
- `curl-v5-invalid-bearer-accept-problem.shim.http.txt`：`aacfa89806b70ce6ad2ab5268bf5adeadfe0fbac5e0ea51a44cb7673a1ec68a5`

本轮未覆盖Group POST、custom-control CRUD、DeviceTemplate GET/POST/PUT、完整ingress部署或全operation矩阵；未运行`encore check`。这些隔离listener结果不关闭DM-API-002/004，也不证明生产入口行为。

## 2026-10-26 15:29北京时间：八小时执行状态审计与接续纠正

本轮只读复核会话日志、三仓差异、代码、历史测试记录与隔离运行角色；未重新运行产品测试、未改业务代码、未执行数据库写入。结论、成果可用性与过程事件追加至REVIEW-2026-10-26.md执行审计，GOAL-HANDOFF §6和TASKS顶部已更新当前接续位置。tasks.json与TASKS同步纠正DM-API-005/DM-DB-010“只有create接入幂等”和DM-GRP-008/009“没有可用Linux listener”的过时描述，明确DM-TPL-005旧状态事务不等于目标immutable revisions。所有原scope/tests/accept/deps/priority/blocking未改。

当前代码、差异状态和OpenAPI指纹见execution-audit-snapshot-2026-10-26.json。当前store.go与HTTP镜像源副本不同，group raw/auth文件相同；既有GET HTTP只支持其已验范围，不证明最新模板代码通过。task运行角色只读权限仍为device_groups/device_group_members/devices SELECT，查询未见运行中的task runtime；待写HTTP harness未执行fixture/GRANT/写请求。没有将准备脚本算作验收通过。

本轮文档核验：validate-plan.py PASS（188 tasks / 580 cases / 108 operations）；独立比较确认原任务scope/tests/accept/deps/priority/blocking逐项未改，5条状态/证据与TASKS表同步，文档whitespace检查通过。这是计划一致性检查，不是新增产品通过证据。

## 2026-10-26：6.1审查后模板配置、TS生成与分组页面候选

用户再次确认自动化/告警由另一任务负责；其余原任务继续并行，不增加新的排除项。6.1统筹设计/复核，Luna执行限定文件：

- DM-DB-004、DM-TPL-004/007：新增`accessconfig`目标配置值对象与严格解码，支持显式model=null、Topic/Lua/资料字段本地规则；RawMessage保持数字精度。6.1复核发现并要求修复number默认值接受带引号数字、nullable字符串按字节计数、直接Validate允许nil必填数组三类边界，修复后该纯包test/vet通过。ExternalValidator只是正式owner的接入口，尚未实现provider能力/tenant/schema/secret资源校验，不存在“已有接口即已接通”的完成结论。
- DM-API-007及模板后续接线：新增`resolveAccessTemplateModelBindingTx`候选，复用调用方tenant-local事务，按固定model/revision读取canonical snapshot，锁model FOR SHARE，分别读取语义/资源摘要；不读latest，不重写物模型core，不新增kind适配规则。Luna报告隔离Encore PG聚焦测试覆盖null、固定版本不漂移、跨tenant、归档/非canonical/不完整拒绝、txid/GUC及调用方rollback；go vet通过。根代理已复核源码；service尚未调用，不代表模板API发布已实现。
- DM-API-009：从原完整OAS3.2.1固定生成TS types、完整components/约束/x-* metadata及manifest。源SHA `f174629a6f39eb72e0833eb46042bf9e693dad050897b85e8e0e9f2d2081b459`。openapi-typescript 7.13.0生成递归JSON时出现TS2502；6.1要求机械提升完整类型表达式为顶层递归alias，仅重定向自引用、不删改union；脚本结构漂移即失败。独立TS5.9.3 strict编译、递归合法/非法值类型样例、重复完整生成哈希一致性通过，metadata保持不变。Go客户端和全部CI/消费者仍缺。
- DM-FE-001/019/020：分组列表/共用弹窗源码切native v2+generated类型；Bearer/语言/data-meta/Problem/EncoreAuthError/ETag/幂等由新transport处理。6.1发现并修正SYS_ADMIN无委派写按钮、添加子组丢父ID、409/412后隐藏换新ETag导致覆盖三处风险；仅TENANT_ADMIN写，冲突保留原输入/ETag并阻止直接提交，用户明确确认重载后才更新版本。Luna报告transport/editor-state纯测试9/9、generated/transport/client/helper独立严格TS通过、两Vue script语法transpile及diff-check通过。未跑完整vue-tsc/生产构建或浏览器/HAR；grouping-details和删除reference fence未完成，不能关闭菜单任务。

新增实际HTTP phase1证据：`/Volumes/DevScratch/dm-group-http-20261026T0718Z-lTSSIl/`。主代理独立读取响应索引：create/replay 201，同key异参409，A/B同key隔离，A读B404，PATCH200/旧ETag412/缺If-Match428，move/replay200及异参409，非法名称422。索引SHA `b2c6861e1648c79089273b72e21f77d8f7c5d4cccd1356b8451d06e418780a5d`。第04请求虽名child但实际parentId=null，不计非空parent创建成功。首次member-add500为devices行锁所需UPDATE权限缺失（42501），事务member行数0；不能算业务成功。后续角色行锁权限修正与新签合成JWT的成员验收仍在单独执行，最终结果另附，不在此预先计通过。

候选源码指纹（均为未提交文件，不是交付commit）：
- `encore-api/devicemetadata/accessconfig/draft.go`：`519846d4277d8746bf977efe691e645e5c15c0126a1ddd0e4c99bcc314553eff`
- `encore-api/devicemetadata/accessconfig/draft_test.go`：`b690f8248f38425fc2865c9802aed391089c72c804e6667c11a5beeaea4c3bb4`
- `encore-api/devicemetadata/access_template_model.go`：`3d7e561f1764ed7ea16f5013864251663812b3de3e61db7bd61623cb024523c9`
- `encore-api/devicemetadata/access_template_model_test.go`：`0d99d8c32c5d2d57f98c7607000481671e321c4425dd208edeb0bb4a81e9ade5`
- `src/service/api/generated/device-metadata/types.ts`：`46e76af64d89f9c2faec0171b5389ebac5a38ccf6d764ef5d4b4521544cb856a`
- `src/service/api/generated/device-metadata/contract-metadata.json`：`7512769f2101b5405121f20100da19b51b0e598b5e09793ae5873cb547e7e92f`
- `src/views/device/grouping/index.vue`：`9e221580b5b3d05b9edb4da542703ff401ddc902f073598fc0e2a9a228c15806`
- `src/views/device/grouping/components/add-or-edit-devices/index.vue`：`c768d21a4da5e19552496245a592263a30b7651ee3badf2d15b92b5c87b05d60`

测试证据来自上述各agent实际命令回报及根代理源码/文件复核；未重复运行无变化全包测试。本批未修改权威OpenAPI、任务scope/tests/accept/deps/priority/blocking，未涉及生产迁移或发布。

## 2026-10-26：分组真实HTTP成员/子组验收补充与role回收

本批新增真实隔离HTTP证据覆盖DM-GRP-003/004/005/008/009的部分用例；未修改这些后端业务实现。phase1根/编辑/移动等响应目录为`/Volumes/DevScratch/dm-group-http-20261026T0718Z-lTSSIl/`。最终成员与真正非空parentId子组证据位于`/Volumes/DevScratch/dm-group-http-authfresh-20261026T-cifwZt/`：`acceptance-summary.md` SHA-256 `4b2b7fede2224879957c0f8e051a640406f787d5b39a77c097de4750be719778`；`accepted-evidence-sha256.txt` SHA-256 `8a55c67a986e113f68df4fc04d53392448310fa9234323e8c4c502fb9b8e9b05`。根代理已独立读汇总、核对hash，并从task PG查询确认临时表/列权限恢复。

- 成员add、同key重放、不同key重复加入均200，版本保持2；仅实测1设备，不代替100台HTTP容量。
- A/B混合设备批次404 Problem，GET/SQL版本仍2，保留原A成员，B关系未写入。
- 正式引用owner缺失时remove为503 Problem/code=unavailable；GET200/v2、SQL group2/member1不变；不代表正式成功移除通过。
- 真正带parentId=A-root的子组POST201；响应与SQL确认parent准确，子组version1；之前只叫child但parentId=null的请求不算嵌套创建。

中间失败全部保留而未计成功：旧合成JWT已过期（后改全新纯测试key/30分钟JWT及独立stub）；devices与members行锁缺列级UPDATE导致42501/500；一次子组fixture授权漏INSERT；一次临时HTTP文本解析错误。最终行锁仅临时授予devices.UPDATE(id)与device_group_members.UPDATE(device_id)，未扩为整表UPDATE。每次finally停本轮runtime/stub并撤权，主代理复核当前UPDATE列grant计数0，device_groups/device_group_members/devices仅SELECT；task PG与原stubs保留，无运行中的本轮app。没有访问默认或生产DB。此证据仅适用既有镜像的未变group/auth路径，不能证明后来新增模板解析器/校验服务已通过HTTP。

原任务状态只更新为partial，不关闭完整权限、RLS、100台HTTP、并发/重启、浏览器、CLI或正式引用owner验收。新CLI和迁移工具已按原任务ID排程；自动化/告警继续由其他任务负责。

## 2026-10-26：6.1总控接续，CLI真实闭环、模板/设备schema与三Tab候选

本批沿原188任务/580用例/108操作继续，未改scope、tests、accept、deps、priority或blocking。自动化/告警只读跟踪，不改对方代码；外部Workbuddy只读最新为`7c389ac0`（A27提交，读取当时clean）。对方文档自报343后端/21CLI/18MCP通过，但A22–24页面、A28–34和正式联调未完成，未整树合并；CLI main.go与绑定名称存在集成冲突，待正式接口/交付后选择性整合。

**分组CLI实际验收。** `/Volumes/DevScratch/dm-group-cli-accept-20261026/`包含16次实际二进制→loopback Encore→task PG调用；二进制SHA `f0f9345944d37f6a50a8caf7780da183f91ff7872d4ecad4c7f02d87ead1e9f9`。A/B list/tree隔离、create201、同tenant同key异参409/exit5、跨tenant get404/exit4、强ETag update/readback及旧ETag412/exit5通过；member add通过，缺引用owner时remove实测503/unavailable/exit8（不是成功移除）；停runtime后exit7且无legacy回退。根独立读取results/cleanup并核SHA。`results.json` SHA `d06be42f82798385e06fe2d7dc84a1256277650e6b4b5cbbd63477b06efad9a5`；`cli-transcripts.json` `2d121295b02fc939783df9632749dcfa9d349e5cfd3ed6dbe4554bbb93366ea5`；`privileges-after-revoke.json` `cf0d8a9f2d9ec25a2047298e1ec163b3566e81aeb99fdb2547820e111258dfed`、matches_baseline=true；`final-status.json` `853ac7a708568bbe9d28eff5368a70f786b1c58559b8a52b8e7ce92e652fcd40` PASS。runtime/stub停、合成凭证文件删除、所有临时表/列grant回收，测试group/device/template为0；按快照不可变保留3条synthetic model/snapshot。使用旧task镜像，不代表0023–26或RLS已验。

**模板与设备schema。** 0023把唯一Encore模板owner重命名到device_access_templates；根只机械改53个SQL表token，旧迁移不改，legacydb.go仍读Community旧源表。追加目标shell字段及immutable revisions（同tenant模型快照/双摘要一致、归档模型拒绝、FOR SHARE、语义内容非空、资源object、版本非负）；S3不为旧数据伪造draft/actors，S6待转换/收紧。新root `access_template_store.go`在调用方tx内insert/load/save/CAS；拒绝未转换旧行，不伪造公开deviceCount/imageUrl；外部资源授权/验证、幂等/审计和HTTP仍须服务接线。

真实命令 `GOMODCACHE=/Volumes/DevScratch/dm-go-modcache-20261026 encore test ./devicemetadata -run '^TestAccessTemplate(RevisionMigrationSchema|DraftStore.*)$' -count=1 -v` 四项PASS，日志`/Volumes/DevScratch/dm-go-modcache-20261026/focused-test-4.log`根已读取；旧模板archive/bind/derive/publish/read/write事务11项PASS，日志同目录`template-tx-regression.log`根已读取。原共享module cache依赖源文件缺失，通过新独立GOMODCACHE按原go.mod/go.sum下载解决，未清理共享cache/升级版本。只显式`encore db reset device_metadata --test`重置本app隔离test cluster，未触默认/业务/task HTTP PG。首次RESTRICT错误码预期、fixture nil bytea、test role不能drop旧约束等失败已纠正；负resources_revision由旧0016 CHECK先拒绝，0023guard只读源码断言，未冒充直接分支覆盖。

0024设备身份schema已过39个子例：目标行可不写旧sn/模板、同tenant编号含tombstone保留唯一、跨tenant可同号、kind/activation来源组合/JSON/租户不变约束。第一次结果见agent实际工具回报，未单独保存log；后续新增0025/26的一轮联合回归将保存日志。旧列保留仅S3离线转换，不建双写；目标必填字段暂NULL、activation来源与actors回填、绑定/网关/旧列退役和RLS仍待后续。

**前端。** 同实际HEAD7d331d基线与候选完整vue-tsc各1213条诊断，按相对文件+错误码新增0/消除0，本轮scope0；普通pnpm build退出2于既有typecheck，单独Vite bundle退出0。这不等于全项目构建/浏览器通过；日志`/tmp/device-metadata-fe-typecheck-evidence/`。补group/tree到现有统一Encore开发代理；不新增环境变量契约。详情基本资料/子组/设置及设备Tab/选择器已切generated native types，清除此两Vue的旧成员API，详情/member/device分页、强版本、最多100选择、同key/body重试、409/412显式确认刷新、分页/路由/弹窗迟到响应隔离经根审。移除全组成员预取，重复add交服务端；typed transport/state定向14项通过，定向格式与diff检查通过。未做浏览器/HAR。准确后端缺口是native设备GET和成员GET；POST additions/removals已存在并有真实证据，不能因误搜Community仓库声称不存在。

**仍在验收。** 0025operation/items/artifact、0026desired binding及root `metadata_write_tx.go/_test.go`正在一轮隔离PG联合验证。root helper沿同一receipt/audit owner事务提交、JSONB规范化后计算实际响应hash；仅非秘密同步写，服务必须先鉴权，包括重放。静态review发现expiry用app时钟而创建用DB时钟，根正在改用数据库时间后定向验。worker、异步API、秘密响应、完整配置采用/ACK、网关/逐设备binding完整性仍未交付。

本批候选指纹（base SHA + dirty files，不是commit交付）：

- `kernel/encore-api/devicemetadata/migrations/0023_access_template_revisions.up.sql`：`6f878508093e3eedd783320cca76b26b8cc3232c69b377d7c38558fba64de67d`
- `kernel/encore-api/devicemetadata/access_template_revision_schema_test.go`：`4c764da4fb1a5c584690cc8450de97521d4b85c081f7f27596b8b6f4a52e9935`
- `kernel/encore-api/devicemetadata/migrations/0024_device_identity.up.sql`：`65b28e05c737627f733743a6c7683076c4becad07f6ac5a3cb1bf9959ab2f25b`
- `kernel/encore-api/devicemetadata/device_identity_schema_test.go`：`c4f9e5fb012afd827af9a2dc853679e243f453ebed274a256757b79fa781ea3f`
- `kernel/encore-api/devicemetadata/access_template_store.go`：`cb6fa1c1f121cf41c7c017d03bea6258f0db9cc166b694f03b000765e9edbe67`
- `kernel/encore-api/devicemetadata/access_template_store_test.go`：`7a6a3bfcc63a1afa85c63442d704724bad8986f08bb45b295a6635e074cb6d73`
- `frontend/src/views/device/grouping-details/index.vue`：`4b36d345d69028b37c56ab38be8e3e311e7d665494078ad9b4846d68fbfebc0d`
- `frontend/src/views/device/grouping-details/modules/device-select-list.vue`：`0c85bfc3602ae878a66e32f11b16144ff57c8a620330d8b954d0231bbf977018`
- `frontend/src/service/api/device-group-membership-state.ts`：`96894568f486e6e8925ba57e23273e2e76c53f9d97b651a38a198370e91237c8`
- `frontend/build/config/proxy.ts`：`cd636841f8cabcecb5fada19538111f846470eaee126b758de792db621aaa31c`

### 同批联合验收最终结果

命令 `GOMODCACHE=/Volumes/DevScratch/dm-go-modcache-20261026 encore test ./devicemetadata -run '^Test(MetadataOperationsSchema.*|MetadataWriteTx.*|DeviceIdentity.*|DeviceAccessBindings.*)$' -count=1 -v` exit0、9项PASS（Bindings2、Identity2、Operations1、WriteTx4）；完整日志`/Volumes/DevScratch/dm-schema-integration-20261026.log`，SHA `99008e9d514028e51db54c51fd79b70c03f7bdebd031af584241958fd075b426`，根已读取末尾PASS并核SHA。root写事务expiry修为数据库`clock_timestamp()`判断；并发同key只执行一次、JSONB含指数和高精度数字重放、失败全回滚、过期tombstone通过。0025根审修复Problem数组类型guard与artifact原始内容不可变/过期删除不能复活；0026根审补配置变化必须递增generation、禁止倒退与同tenant operation FK负例。没有生产调用点/worker/API即不计业务功能完成。

所有测试只在Encore test DB；未reset默认业务DB、未迁移task HTTP DB，未启RLS。每设备恰一binding/activation跨表约束、网关、S3旧行转换/最终NOT NULL、正式owner验证/权限、真实服务和浏览器仍未覆盖。后续已继续派发设备Create/Update严格输入，6.1接正式API与native成员读取主链路，不重新审计未变化的旧CRUD。

- `kernel/encore-api/devicemetadata/migrations/0025_metadata_operations.up.sql` SHA：`01a7297c964eadc6b445a2b345fa35d6f09bbb8e4a78267edde53d9c4b812542`
- `kernel/encore-api/devicemetadata/metadata_operations_schema_test.go` SHA：`c6b21d3fcc7ed77ecabb9f4c592b5c1ac014bf4b596baa8cf1e6a89ef4464963`
- `kernel/encore-api/devicemetadata/migrations/0026_device_access_bindings.up.sql` SHA：`20228fae031eddce2be9654174ec8413b6a753acbbab7753e140d8fac1fbb117`
- `kernel/encore-api/devicemetadata/device_access_bindings_schema_test.go` SHA：`ff13d88b353ece15d6c404e6cb9113d340d3e981a4d74924073e6fdcbb47f678`
- `kernel/encore-api/devicemetadata/metadata_write_tx.go` SHA：`0e453384f3bfe3e5e8a5f916c938beaf790115029b39ac3f12c621caa16587cb`
- `kernel/encore-api/devicemetadata/metadata_write_tx_test.go` SHA：`5630f18bfbbc42b0bfcc7a34e848c851c165c42e99d7e86ca36b4191fef4e8dc`

本批计划交叉引用验证：validate-plan.py PASS，188/580/108不变、errors0；仅保留历史baseline_unavailable警告。独立核对本批变更的state/evidence JSON与Markdown均一致，未声称历史所有行文字完全相同。原DM-DEV-003“当前schema无法推进”已改为新schema前置已通过、输入切片正在做，避免后继沿过时阻塞判断停止。

## 2026-10-26：目标设备/组读取接通及当前源码真实HTTP验收

本批在同一1.3候选接通四条原operation：listDevices、getDevice、listDeviceGroupMembers、listGroupsForDevice。`native_device_read.go`、`native_group_members_read.go`、`native_device_groups_read.go`复用`native_device_record.go`固定33列/24字段投影，唯一来源为devices + desired binding + immutable revision + gateway_memberships；不读sn/parent/template旧列、voucher/online或latest模型。列表/成员先tenant和授权deviceIDs，再metadata条件、count/page；items/total同RepeatableRead快照，成员子树多组去重与matchedGroupIds/directMembership。尚未连接runtime/OTA/alarm的观察筛选明确503，不筛一页、不伪装0。设备反向组读取先授权设备，统一group hydration计数也新增受限deviceIDs过滤。当前Community issuer未发delegated scope，trusted受限context只在PG测试证明，不冒充真实受限JWT验收。

原生Create/Update输入严格校验候选已加入；区分省略/null/整体替换与initialGroupIds省略/[]，保留JSON数字精度。根审修复description的min=max2000误用、Unicode whitespace/C1、quoted坐标误收及float64把90.000000000000000001舍入90的边界（整数边界用exact Rat复核）；POST/PATCH正式服务尚未调用，不算写接口通过。时间读回统一UTC。

0027按DATABASE §3.7追加网关关系，双复合FK、地址C collation与Unicode/C1、parent live gateway/child live gateway或subdevice、tenant树锁、祖先/整个移动后代子树环和32层校验；devices最终状态的deferred trigger允许同tx先解绑再软删，拒绝留边改非法kind/软删。旧parent未回填/运行时回退。

### 联合PG/权限与CLI检查

`GOMODCACHE=/Volumes/DevScratch/dm-go-modcache-20261026 encore test ./devicemetadata ./internal/authz -run '^Test(DecodeNative|ScanNativeDevice|NativeDevice|GetNativeDeviceRecord|ReadNativeDevice|CurrentNativeDeviceRead|GatewayMemberships|GroupRead|GroupPrincipal|ParseGroupRead)' -count=1 -v` 最终exit0，26顶层PASS、0 FAIL/0 SKIP（metadata25/authz1）；三次attempt完整日志`/Volumes/DevScratch/dm-native-read-integration-20261026.log` SHA `4ed2a5111b4ea2813d555ad6121f57084f7f40cc143b22b5ec622b8efe90d866`。失败保留：首轮PK误要求ON DELETE子句及fake scanner索引偏移panic；第二轮PG JSONB空白字节比较错误；均纠正测试而不扭曲正确生产投影。第三轮含坐标真实校验修复及raw JSON大整数9007199254740993读回。旧group读与新增X-Request-Id成功/Problem一致性亦覆盖。

CLI仅扩现有薄客户端支持`device groups list D`，保持group路径；本地go test/vet通过，404无fallback和小驼峰分页有请求测试；实际binary SHA `967ae39ca35ea38019674abfb20125157da49b69768f6af58999a1f933ae468d`。

### 当前源码隔离镜像与新HTTP环境

目录`/Volumes/DevScratch/dm-native-read-http-20261026-7jzu1spv/`。新普通clone HEAD fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05，同步71个encore-api修改/非忽略新增文件；source-manifest SHA `a40e1fcc10df2eb8b04c570705c2afa81ec678ebe2c5313e62ac0482ed12ac64`。只在clone以`ENCORE_GOROOT=/usr/local/go GOMODCACHE=/Volumes/DevScratch/dm-go-modcache-20261026 TMPDIR=<该目录>/tmp-build encore build docker codex-dm-native-read-20261026-57fc52d5:latest --services devicemetadata,authn --config <该目录>/infra.json --os linux --arch arm64`构建；镜像ID `sha256:07c442d7357cc8f490d4bb6279f7b238524c71cd59334d18bf89fb3da4a3c076`。首次没指定services导致缺notification DB配置，未扩infra而沿原成功命令限定服务；首次日志保留。第二次Encore导出成功，包装脚本对zsh readonly status赋值导致wrapper exit1，未假报整个wrapper为0，实际镜像inspect与后续运行已证实制品可用。

新network`codex-dm-native-read-57fc52d5-net`、PG`codex-dm-native-read-57fc52d5-pg`（postgres:16-alpine）、新volume同prefix；只在新PG应用现存21个up迁移（号段0001–0007/0014–0027，未捏造不存在0008–0013）。A/B各12完整目标设备+binding，统一createdAt，旧sn/template/voucher未写；A root direct1/child2/重叠1/去重2，B独立root2。owner迁移，runtime role `codex_dm_native_read_57fc52d5` 为non-owner、NOINHERIT/NOSUPERUSER/NOCREATEDB/NOCREATEROLE/NOBYPASSRLS，仅10相关表SELECT，database/schema CREATE=false、写grant空；RLS相关表=0。证明最小只读角色能执行此路径，不证明RLS。fixture-result SHA `1d5a9d07dae2b1c1e7b8821d87a31347dc840109f3729c1e8d899884ea5d3120`，migration log/seed/readback指纹在其中。

### 真实HTTP结果（合成HS256 + live-session stub）

22次HTTP + 2次真实CLI PASS：A pageSize7两页total12且ID稳定；B只见自己的12台；改变deviceNumber实际切换返回ID；ungrouped total10、hasTemplate=false total12；完整24字段且强ETag；A读B设备404；root direct1、子树去重2/matchedGroupIds来源正确；反向组total2；跨tenant组/反向组404；groupId子树条件在page前count2而返回1；body/query tenant选择/重复page400；缺Bearer401；无委派SYS_ADMIN403；connectivity/firmware/active alarm依赖缺失503/unavailable。两个真实CLI分别members list子树total2、device groups list total2，均同native服务，不走旧路径。业务JSON/Problem的X-Request-Id与body一致；预处理401仍Encore默认，公开入口Bearer challenge未验收。

公开证据：`http-native-read.json` SHA `3c3ea2b71586de7a374f06c196af280f769c19693bfa5410f93c231ea06fdb0a`，`cli-native-read.json` `1a106402c5725592250849926b30b70017a327f9c8e96514f19c7c62e1cfb3b9`，`http-result.json` `331489cd70957715c7d3d4cf2f20e56bf546d77c3a39259d3b681235c6d5a53c`。根独立检查当前image、运行结果及公开JSON/log/sql/py未包含runtime DB密码。测试app/stub已stop并remove，合成JWT记录/环境文件删除；PG/image/证据保留。未触旧task PG/net、默认/生产DB或原共享脏checkout。

### 仍未关闭的原范围

本轮不是三个菜单完整交付：设备POST/PATCH、template正式服务/provider校验/固定配置采用、凭据与激活、runtime/OTA/alarm过滤、RLS/完整角色/运行态ACK、S3离线对账/S6收紧和旧列退役、浏览器/HAR仍缺。页面设备详情还待native切换；新成员/设备GET和开发代理devices路径已补，不以HTTP通过替代浏览器。Workbuddy只读HEAD仍7c389ac08e69c92d9d26a4375218a99aa611b414，尚未到正式联合交付，不抢改/不整树合并。

本批候选代码指纹（非commit）：

- `kernel/encore-api/devicemetadata/native_device_input.go`：`086a8d2536a8d8a610f7cd2523bc19c98d843109b3908f0b95e0091d39e747f5`
- `kernel/encore-api/devicemetadata/native_device_record.go`：`58309381d8e4897ad9d24efa464dd83429a7118584f1a320750ce50856839a30`
- `kernel/encore-api/devicemetadata/native_device_read.go`：`8fce64f759b34cc25027d779285be6e3a08fe2ea7e4b8b3483c41f199c381b25`
- `kernel/encore-api/devicemetadata/native_group_members_read.go`：`7c085bf32ad1f7feadeaa957231b491de98c61fc7832e7361148731ea68c96f9`
- `kernel/encore-api/devicemetadata/native_device_groups_read.go`：`35caf2f854b7186155459fd466c7d904e1dde09bb2f78371feb9339ee8f5e861`
- `kernel/encore-api/devicemetadata/migrations/0027_gateway_memberships.up.sql`：`be5b983ad7d67437548660d4575818729dd16138bb88fcc3bce0850fd3c7dccf`
- `kernel/cli/tpctl-go/device_groups.go`：`4cfae7f55bd686c5e641317dfcc143cd1e8e9c872bf58c2186c7ef41ecad3146`
- `frontend/build/config/proxy.ts`：`c6e1015b745d9a295a0a6134aa8d5d34e826f603f69a685c9eb342e5b38a525b`

最新前端candidate（含三Tab/selector与devices开发代理）一次Vite bundle命令exit0，`/Volumes/DevScratch/dm-native-read-http-20261026-7jzu1spv/frontend-bundle.log` SHA `0b38c2a4fb06a4a23428668855497299ecb7ef457e9f24efac7ff292b7c337a8`；dist/index.html SHA `fd490772cb6726ea597047c65b8f6a247cbbc6ca1a7b202d5dcb82c320f67d7e`。保留原GridV2/UnoCSS提示，不扩修无关内容；这不是typecheck/浏览器通过。


## 2026-10-26 原生设备创建编辑真实验收

根6.1 sol负责服务实现、工程决策与签收；Luna负责初始组辅助函数、输入Problem、PG/HTTP脚本测试和CLI/前端transport。沿原DM-DEV-003/004、DM-API-003—006、DM-CLI-002—004/015、DM-FE-004增量推进。未修改自动化/告警，原188任务/580用例/108 operation范围不变；没有关闭整菜单、配置完整创建或发布任务。

### 实现与关键决策

- `POST /api/v2/devices`普通无接入配置登记201，返回完整24字段Device、Location和强ETag；inactive/not_activated不假造运行时激活。设备、唯一binding、初始组成员、组resource_version及逐组审计、设备审计和幂等receipt同一事务。省略initialGroupIds仅有组写权且唯一root才默认加入，[]明确不归组；显式混入其他tenant组全部rollback。
- `PATCH /api/v2/devices/:deviceID`仅展示资料白名单；先锁live device再取完整投影。GET/Create/PATCH共用一个强ETag算法，要求If-Match及key；省略/null/空串分清，JSON对象/数组整体替换。PG JSONB语义比较决定是否增版，{}与相同产品引用保持版本/ETag，不把无变化写当新版本。产品独立same-tenant校验，产品建议不隐式绑定模板。
- 身份仍来自验签/有效session的可信tenant与subject；共享authz仅增加Community TENANT_ADMIN的native device write映射，创建要求tenant-wide写权，编辑在SQL前做deviceIDs授权，SQL持续tenant围栏。完整受限service issuer与RLS仍未完成。
- 配置创建必须经过固定provider版本/schema与参数域校验；当前正式HTTP调用缺owner时明确503且设备/receipt零写。现有connector模块没有正式版本化schema事实源，不能拿legacy插件ID、弱validator或假ACK顶替；下一步在既有connector owner内补正式实现。runtime外部身份保留与配置采用仍属于原任务，不能因nil port存在当交付。
- 模板绑定创建路径补当前canonical ThingModel shell非archive复查和锁；只认固定publishedRevision，未复制造最新模型。自定义字段按固定定义检查，禁用字段已有历史值必须保持（整体替换请求若遗漏/改写禁用历史值则拒绝，避免删除历史）；完整字段定义/默认值/切版工作未关闭。
- 字段输入错误保留typed field/reason，结构400；字段422返回JSON pointer/code/message；未知字段不回显其输入名/值，未知数据库错误500只记录稳定code和requestId，Problem隐藏内部原因。pre-handler401仍EncoreAuthError，不声称全部入口均Problem。

### 实际测试与失败修复记录

1. 35项top-level定向测试PASS/0 FAIL/0 SKIP，含真实隔离Encore PG与权限策略；命令和SHA见 `native-write-focused-result.json`。日志 `/Volumes/DevScratch/dm-native-write-integration-20261026-attempt4.log` SHA `c5b2a2b2ca6aa906e18581c2ad6d9c996aa7bc39f2a17e4bf69cceb0ca931f63`。覆盖metadata receipt并发一次/失败回滚/expiry、高精度、设备读写、默认/显式组、逐组audit与replay一次、编号tenant唯一、gateway重复地址409、cross-tenant product拒绝、真实product清空/恢复、description三态/JSON整体替换/no-op、stale ETag和受限deviceIDs。
2. 前三次未通过分别是Luna交稿的Rows.Close API/测试identity类型、重复:=编译错、产品同值PATCH错误预期加版。根审还修正了不合OAS的configured fixture、typed输入错误断言、JSONB空白精确比较。最终运行完整指定范围通过；保留attempt1—3日志，不能把静态交稿当已测。当前可达普通登记/编辑没有发现未修生产逻辑错误。
3. 普通clone同base `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`，逐项copy/校验79份candidate Encore文件，`source-manifest-write.json` SHA `8aa7c72ad2eb6feb31571936a25696973e17e48eb5ea01a1437b0e558ef70585`。`encore build docker ... --services devicemetadata,authn --config .../infra.json --os linux --arch arm64` exit0；image `codex-dm-native-write-20261026-57fc52d5:latest` ID `sha256:eb94ceefc1864fb7d4729d4d85df29ab208a6d2d3ba8d1c5b57d059b372e627a`，build log SHA `2151063239eb6b8a46dd4a89038e602303b21f107270e66355a0ed408a3a0702`。没有更新/执行新DDL或生产迁移。
4. 同已有isolated PG，non-owner角色临时仅获9项指定DML及receipt SELECT；首次HTTP create503定位为测试角色缺metadata_idempotency SELECT（PG permission denied），修正精确最小grant后重跑，未扩大生产权限。`http-native-write-955bb51d8c56.json` 25次HTTP全部通过，SHA `12adc4a9aa2c69ac02a65c740bb900edb975b4c28ba7644efe68701ef58eb699`。覆盖201+GET一致、samekey重放原response/requestId、异参409、编号同tenant冲突/跨tenant可用、结构400/字段422、PATCH428/200/412、同key回放、description/no-op/最终读回、显式[]/组加入并audit+version、mixedtenant404全部rollback、配置nil owner503零写。所有业务响应检查content-type、required envelope/Problem与requestId一致。
5. 原生CLI `device list/create/get/update/get`同API共5次通过，最后GET核对description。binary SHA `ab8cecb4f4bbe8cfa5e4eca2b0e5bc923d4370bd00f0bd66fe77f9f8949136c8`；Go test/vet通过。CLI共用原group请求执行器，不实现的singular操作明确拒绝；保留旧plural既有命令待统一切换，未假称完整CLI发行。
6. Frontend typed get/create/update transport已接生成OAS，精确200/201、Location/strong ETag、调用方固定key、no retry与typed412/422；设备+既有group transport/state共21 PASS/0 FAIL，日志 `/Volumes/DevScratch/dm-native-device-frontend-transport-20261026.log` SHA `463ca5227bf9c4620838fbe2946b384cc47a5ff76bc124d3981e8a5a7e9648af`；定向strict tsc exit0（空日志）。这不是Vue表单或完整项目typecheck/build通过。

所有本轮app/stub均stopped-and-removed；临时DML及新增receipt SELECT已撤回，原角色恢复只读、DML=0；临时JWT/env文件删除。PG/image保留。本轮fixture是新签名合成JWT+synthetic live-session owner，未验证真实Community登录，RLS未启用。已对公开证据扫描真实DB口令泄漏为0，不写token/凭据/请求体到报告。

统一证据索引：`/Volumes/DevScratch/dm-native-read-http-20261026-7jzu1spv/native-write-evidence-index.json`；HTTP脚本根审后SHA `94b15a4ed05c93a189056557c3a6d0b6530392e3040d501be66358934ce3cd57`。

### 模板与浏览器的下一个接续点

- 模板19字段单对象投影helper是79文件镜像之后的独立候选，另有rollback PG测试；不包含于上述镜像，不据此宣称list/get HTTP完成。最新源码/log/hash在后续补充。模板正式list需要批量聚合deviceCount，不能循环单对象loader造N+1；共享authz template权限及图片资产URL必须由正式owner接入。
- 只读定位UI：5002 Vite PID38178静态页面200；动态登录/用户与菜单依赖9999/api/v1，当前匿名请求ConnectionReset、Vite代理500空响应；默认native14210有监听但目标v2均404。未杀原进程、未改现有环境。下一步根建立独立loopback Vite/native listener的合成bootstrap浏览器验收，明确与真实登录/动态菜单验收分列，再处理真实入口接线；不再让用户回答仓库/HTTPS入口或401实现格式。
- Workbuddy只读HEAD仍 `7c389ac08e69c92d9d26a4375218a99aa611b414`，自动化/告警继续另一任务开发，不改不整树合并。本轮测试/CLI可用仅限已测范围；模板正式CRUD/provider、凭据与激活/runtime ACK/全部运行页、RLS、迁移/发行等原任务继续active。

本批源码指纹（非commit）：

- `kernel/encore-api/devicemetadata/native_device_write.go`：`e2ed46c4c1dcd36c46d2b2f3853f2782e91f32c1ab17d639595d0ba93009db43`
- `kernel/encore-api/devicemetadata/native_device_write_raw.go`：`a67127df678082c8c8b7f0e51c86734d79a62de1c489be64593006793532a333`
- `kernel/encore-api/devicemetadata/native_device_write_test.go`：`3a0a936046bb5f374950682f4215f23b2b81673bad4657b1f097ed0b414bd73f`
- `kernel/encore-api/devicemetadata/native_device_write_raw_test.go`：`b239eecc7c5e4c5c585846f42971e00856ed1055d3fd6c2d37a5bfc1dc5612b7`
- `kernel/encore-api/devicemetadata/native_device_initial_groups.go`：`13821c0fe027b584f4470e11d5e7ef3159454518ee77a897a48c46b785efcf8f`
- `kernel/encore-api/devicemetadata/native_device_input.go`：`c486c4400d2a287cd5fb3dfe98276da83c182c6fea6c50bb449a3ef4d426213e`
- `kernel/encore-api/devicemetadata/native_device_input_problem.go`：`990f12bd6359d1329bf1df367d8243044dcdebc9f46b41ec003d5c476d58a763`
- `kernel/encore-api/devicemetadata/native_device_read.go`：`c9894bd0d9507ac78d62bbb9e285e0f341c95f2793434ab12edc951d48e0927a`
- `kernel/encore-api/internal/authz/engine.go`：`86449abc8cab3cacad9eccc578ea372d6dac2ee62f1ee63daf5955cec18d3bfe`
- `kernel/cli/tpctl-go/devices_native.go`：`37603430b651b37b78e743143faf5a165aed36113fd130cfe0ed445f8dd08c35`
- `kernel/cli/tpctl-go/devices_native_test.go`：`589366555d4410947c9aadb05569ed8f7800da0a565fa23760228c04e729fb18`
- `kernel/cli/tpctl-go/device_groups.go`：`50c516d9817ef13c1594723f6d445f2f2ac2f2ad00bf2eac74640c66ea28d168`
- `kernel/cli/tpctl-go/main.go`：`d38b8f27fbdf875ba2ccc15e8fb15fd2d7d2b9c99f0b008b65b14e209d5ee784`
- `frontend/src/service/api/device-native-transport.ts`：`586a539a3dadfd432923fd6da9b5419ad37b771359dd98108a05e8ce20e2f986`
- `frontend/src/service/api/device-native-transport.test.ts`：`543427acd93d666ee323beea155a8f751749ac0a3b56f2f44780ce26666c6143`


### 同日接续增量：模板读取候选与真实UI根因

- 新模板单对象投影 `native_template_record.go` 根审后SHA `46824ccdeef3b6ea9f0bf6d4df505211ebb23b6bd7d3ca6013d3d9feb7d407b1`，test SHA `ab7f199fe9c9dc0988a5809e68a676cbd929f13baa265857362a246d87032ee9`。READ边界将S3 incomplete/invalid draft与safe-range异常转503/Unavailable，保留真实NotFound；资产URL按OAS只约束UTF8/1024 codepoints，不加合同没有的URI格式。focused Encore测试2项主测试/3设备scope/4 URL子测通过；全部fixture同事务rollback并验证无残留。日志 `/Volumes/DevScratch/native-template-record-projection-20261026.log` SHA `37c968c7378dcf5bfb5f773a31bf4ceada81d27fa96b304ca60e3d53a78877cf`。该helper后于write image snapshot，尚无正式HTTP/auth/list签收；列表batch aggregate与filters已另派Luna，root接正式服务。
- `template list/get` CLI候选新增，正式OAS filters、分页/--all、真实模板table字段与强ETag通过mock HTTP tests/Go test/vet；日志 `cli-template-go-test.log`/`cli-template-go-vet.log`。尚无正式API真实调用，不覆盖此前已验的tpctl-native-write。source SHA templates `a71bcfa6f684a9edd8fb7fb0141795fb19b86a20270c974816f176cdb89fa43e`，test `5c223d7f00c300751ab77c14a338db96364d429c5d6d9fd71dfe87cf622e60b6`；共用executor/main的新SHA分别 `df70395c95056a9b70f2799547007ad7e23f3bf5193d69702c8103afff98e520` / `8030519a31588f97b5b1e340637694f54e4ec515bdb40e4f509a4250aad63d89`。
- 真实UI bootstrap最小根因已进一步核实：`thingspanel-install-backend-1`（Compose thingspanel-install/backend）9999→9999映射正确，但RestartCount观察817→819；最近日志只做脱敏分类，MQTT timeout/init/fatal导致重启循环。未输出env/凭据/完整日志，未改/重启容器或5002。Luna继续只读查预期broker/service/network，根决定最小恢复动作；不拿合成bootstrap宣称真实Community登录通过。当前仍有非依赖该登录的模板/API/CLI原任务可推进，Goal不阻塞/不完成。


## 2026-10-26 模板列表详情及分组只读浏览器验收

本轮实质进度：根接通GET `/api/v2/device-access-templates` 与 `/:templateID`，Luna交付列表批量聚合、测试、CLI与frontend transport。原188任务/580cases/108operations保持；不把只读通过称模板整菜单或整版完成。

### 模板服务与真实验收

- 原生目标DTO实际是19个字段；上轮描述中的20是计数错误，已纠正文档，OAS/DTO均未增加或删减字段。读取完整draft/actor、UTC时间、真实MAX published revision、按已授权deviceIDs去重的live deviceCount；S3 incomplete/无效draft及asset owner缺失503，跨tenant404，不用旧模板/路径补值。
- 两条正式路由使用共享authz template:read（仅已验证Community TENANT_ADMIN）、可信tenant与repeatable-read/read-only tx；COUNT、page与单次页内revision/device聚合同快照，无逐项DB查询。GET详情返回强ETag；源码 `native_template_read.go`，结合 `native_template_record.go` 与 `native_template_list.go`。
- 根审修复SQL默认sort为created_at DESC,id DESC（-name同DESC/DESC）；加ProtocolSet/BrandSet/TagSet保留合法空筛选，避免空protocol/tag错误变全量，空brand只匹配真实空串。
- 根实际运行：8项template record/list/decoder/service/authz定向测试，加1项raw invalid-query测试，共9 top-level PASS/0 FAIL/0 SKIP。日志 `/Volumes/DevScratch/dm-native-template-read-integration-20261026.log` 与 `...-raw-20261026.log`。它们含模型/图片resolver单独测试和按已授权deviceIDs范围聚合，不能替代真实RLS或所有服务issuer。
- 将86个候选Encore源文件同步到同base普通clone，`source-manifest-template-read.json` SHA `48f774502e1d99f9b08296e79cfb2af09e46a9047cf7a19ccefca5a570777ac7`。build `--services devicemetadata,authn` exit0，image `codex-dm-native-template-read-20261026-57fc52d5:latest` ID `sha256:28ab00b61fab89926692da975ca7b823497aec661c8b22a66cc3d1d2b4cdaca1`。
- 在原owned PG新增随机A/B/C synthetic SQL fixture：A12/B3完整shell，A1两个revision与两个live设备（另一个tombstone排除）、A2一个revision/设备、B1一个revision/设备，C一条有unresolved imageAssetId模板。fixture只用于读取，不是API创建/配置发布证据。root修正seed中错误的JOIN USING device_id与psql制表符比较，单事务实际apply成功，恢复记录 `template-fixture-result.json`。
- `http-native-template-read.json` 最终PASS：58次真实HTTP检查默认两页7+5、A/B隔离、19字段、revision/count与稳定强ETag、published true2/false10、全部formal filter、排序、空筛选、400/401 Encore格式/403/404/503。root CLI新binary实际运行3commands：list --all两页12、get保存ETag与API一致、brand=Acme total6。binary SHA `26d01efc05ad4de71d34b4e48a8886e5b8907d11d7279753b395a346c9f249f5`，HTTP artifact SHA `c8111cb1ae05db5ce882a347b849c719eb62f9371f7bf12eb22574864ff4f30b`。
- 保留4个失败attempt artifacts：启动期synthetic session尚未ready导致401被harness立即判失败（改bounded readiness retry）；原read role未获template shell SELECT导致503（仅在owned test DB新增该表SELECT，无DML）；harness仍按ASC期望与OAS/已修server DESC不符；harness变量名与fixture key引用错误。均修正后同scope重跑PASS，非掩盖服务错误。
- runtime role仍non-owner/NOSUPERUSER/NOCREATEDB/NOCREATEROLE/NOBYPASSRLS、无DML，增加template shell只读权限供新服务；RLS尚未启用。根索引 `native-template-read-evidence-index.json` 包含source/image/CLI/log hashes与泄漏扫描，真实DB口令在该公开证据中命中0。

### 分组列表/三个Tab/selector真实浏览器只读证据

现有5002原样保留；根另开5003 loopback Vite，独立cache与scratch配置，真实native app与signed JWT/session fixture。static route/local user bootstrap仅供隔离组件验收，不是真实Community登录、动态菜单或角色可见性验收。metadata数据全部来自真实新表API；seed是owned synthetic UI group fixture，不是CreateGroup用例。

Cua实际观察：

1. 组列表显示“原生验收组”，root总数1。
2. 子分组Tab显示“原生验收子组/用于子分组页签”。
3. 设备Tab显示一个direct member（91a0066a-A1-old-0、standalone、enabled）。
4. 设置Tab显示组名/description；编辑弹窗回填同值并取得真实树数据；取消，未保存。
5. 设备selector显示3个live候选；按编号91a0066a-A2-0搜索后收敛1项；取消，未确认加入。

服务代理日志 `ui-native-request-log.jsonl` 记录11次native GET均200，受禁旧metadata调用0。仅这段流程的server proxy log，不是HAR，也未覆盖任何提交写入/删除/跨tenant浏览器矩阵。global logo/sys_version未mock仍404，测试退出登录请求未mock亦404，不把这些当真实登录功能通过。两张临时标签页已关闭，5003/app/stub已停删，JWT/env/nonce私有文件已删；5002与原业务容器未重启。读取UI证据 `ui-group-read-result.json` SHA `8e8207758be4f7fa7343c68c791977afc1aee6a146e368728853857ad671ca7f`，对应frontend源码manifest SHA `af543eff2529aa37ee1090e33679836d12238a5ad0e6b56d82ba6e972552ca25`。截图/AX在会话中观察，未产生独立PNG或完整HAR。

### 后续frontend transport增量（不混同旧UI snapshot）

模板list/get typed client使用唯一OAS生成类型，新增metadata-native-http共用requester供device/template调用；group transport保留。最新strong ETag要求完整quoted opaque tag，拒绝weak/*/unquoted/单边quote/control等；不强制私有sha256前缀。29项device/group/template transport-state回归PASS，定向strict tsc exit0，日志 `frontend-template-etag-tests.log` / `frontend-template-etag-tsc.log`。这次提取在上述UI snapshot之后完成，未重跑最新helper浏览器；不把旧UI证明写到新source上。Vue模板页仍未切换，普通全项目typecheck旧1213诊断未修，不重复无关全build。

### 根确认的两个正式owner缺口及下一执行点

- 原TASKS的共享file-storage owner在两个候选repo内未找到实现，只有已冻结的createFileAsset/getFileAsset两操作；Community旧file/up只返本地路径，无asset UUID/tenant ACL。不得将metadata_artifacts或旧路径当asset事实。下一步以既定file-storage职责实现/接正式upload/get及对象存储，再注入resolver，不重建对象存储内核、不长期用nil当交付。
- 原pluginmgr目录有canonical connector UUID/runtime/instance，但发布器忽略manifest metadata.version/image等，现存数据无immutable schema/digest。找到相邻真实源码：clean Xiaomi4348f6f、dirty HA4d8d45b（只读）均pin官方SDK v0.1.0；官方tag types提供FormConfig{Schema,UISchema}与普通string FormType，非空form_type还会转legacy element数组。Xiaomi统一FormConfig混有接入点账户/password字段，HA区分CFG空schema、SVCR base_url/token与VCRT raw；动态实体commands.parameter_schema不属于静态provider参数。根下一集中补pluginmgr发布端显式版本/digest/双参数schema目录；实例秘密、公开公共参数、per-device CFG和动态实体能力必须分域，不能盲目把旧表单/secret抄进parameters。没有配置schema owner时当前create503只是未完状态，不得将其当配置创建完成。
- 本机旧Community bootstrap准确根因：install broker/owner容器和正式image缺失、原/tmp env/token丢失、现broker-services与backend服务凭据不匹配。只准备恢复草案，未启动/改policy/重建Broker core；停止继续找失效临时文件，保留真实登录验收缺口。临时合成UI证据不能关闭这一门禁。
- Workbuddy只读HEAD仍7c389ac08e69c92d9d26a4375218a99aa611b414，但新增automation executor/alarm/store/migration dirty，说明仍在开发；未改、未合并、未发消息。原范围及Goal继续active。



## 2026-10-26 文件owner与连接器固定版本增量

- 保持原188任务/580cases/108公开操作。以下是实际源码与分层测试增量，不是整菜单/真实登录/生产验收完成；自动化告警未修改。
- fileassets唯一文件owner使用独立file_storage DB、标准Encore私有BucketRef，POST/GET /api/v2/file-assets已接。公开FileAsset仅6字段，模板仍只持久imageAssetId；正式读取通过private owner调用取得短URL。PNG/JPEG/WebP签名与10MiB限制、严格multipart file/purpose、template:read/write、verified tenant、规范Problem和强表示ETag均已实现。
- URL短租约与成功幂等响应用AES-256-GCM保存，AAD绑定用途/tenant/asset/expiry或receipt scope/requestHash；24h过期键继续冲突。原始成功response/requestID/location重放；URL过期需GET刷新，不重传图片。keyring由部署注入，无自动生成的生产fallback。短租约在owner内按asset行锁跨副本重用，强ETag覆盖实际downloadUrl而非删字段伪称强验证器。
- 上传先独立提交可恢复intent，再持行锁执行对象I/O与资产、lease、成功receipt、脱敏audit、intent提交；失败恢复先查已提交asset，保留有效对象，无asset才清理。private定时恢复有数量上限/旧intent阈值/SKIP LOCKED，不声称跨对象存储与PG为分布式原子事务。全局cron调度未做生产验证。
- file_storage迁移0001冻结SHA def26edc8736e5b61fb924348bc218d89a99c8b9cc229b0c8dfd9d1c76f5c8ad；追加0002 SHA 73251fcace5a021bc304890b1b5f85e4de04afb47afa42d5f61878ad7ac6860c。未改旧已应用migration。
- pluginmgr唯一connector owner新增connector_runtime不可变provider_releases与publication_events。发布冻结(provider UUID,opaque version)、artifact SHA256、两个独立Draft2020-12 schema；仅本地refs、保留json.Number、无默认值注入，验证错误不回显参数或schema。manifest严格拒绝未知/重复/非法UTF8/NUL，长度按Unicode codepoint。schemaDigest采用明确域+uint64大端长度+两个原始UTF8 JSON字节序列；它是精确发布内容身份，明确不是JCS，TemplatePackage的既定规范化合同未修改。schema原文保存BYTEA，不能经JSONB改变身份字节。
- 同版本不同内容冲突、完全相同重放原publication；新发布和平台actor审计同事务。private发布必须verified SYS_ADMIN+provider:publish，TENANT_ADMIN不能改全局目录。该权限不赋予SYS tenant读取。公开GET connection-providers与private固定version/schemaDigest解析均接同read-only repeatable-read快照和shared tenant规则；空目录/空instances只表示未登记的原生发布/部署，不拿legacy/noop补成功。真实connector实例/正式包发布仍待接。
- connector_runtime 0001 SHA 1bf6e6668753ec0f0cab55f66d1063c06ed90152d1eba804589af8a07202bddb；追加0002 SHA 82642b886e66b977b6c851cbab01fdacf2a113b73bfe101967fc3c928ab7f16d。已应用后冻结。TRUNCATE在测试runtime role先以42501拒绝；测试核实无TRUNCATE权限且immutable trigger启用，没有提权制造23514假证据。UPDATE/DELETE实际trigger23514。
- Root实际串行Encore定向验证71主测试PASS/0FAIL/0SKIP（含既有必要回归），日志fileassets-provider-integration-attempt9.log SHA65d13bb10639e421422c7e91e984292ac799cde0722c79077b6e26eb34b735b9；另provider-catalog-owner-test.log实际1项PASS（auth override调用真实owner方法+PG；非HTTP/private-RPC签名验收）。真实Encore local object provider SDK已执行上传→签名URL HTTP下载/校验→删除→notfound；这不是fake workflow store，也不替代自托管S3签名强制验证。
- 早期失败日志保留：cron endpoint必须package-level proxy、实际CLI资源名要求snake_case（旧module注释kebab-case不适用）、未完成稿/unused import、缺省imageAssetId误编码空串、嵌套重复键及TRUNCATE权限测试期望均已修正。attempt9通过不抹除历史失败。
- 新镜像codex-dm-fileassets-provider-20261026-57fc52d5:latest，linux/arm64，sha256:4e72753bb0182c4d8dae2ca546d7e1d88fa05c61f930e5da4eff91ce1889870a；125文件source-manifest-fileassets-provider.json SHA25a99a8e814bb46ee47a51b289b4bd5bac245161e326abd266f727f2dc4df1ea，base fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05，Go module/sum未改。--skip-config编译，实际运行需显式只读挂载ENCORE_INFRA_CONFIG_PATH，不将临时DB/S3凭据嵌镜像。
- 前端新增FileAsset typed client，复用generated types/现token-lang和single-attempt requester；multipart不手写boundary，精确201/Location/response metadata、GET强ETag与错误按合同校验。38项定向transport回归/scoped tsc通过，尚未接Vue设置页；原group浏览器读取证据不能替代此新增source的页面验收。
- 下一根动作：完成S3实际POST/GET/租户/幂等/下载及模板cover正向HTTP，再接模板create/PATCH的owner验证/同事务audit/receipt（响应含签名URL时不得把capability明文当metadata永久事实）；原模型、provider实例、runtime ACK、RLS、迁移/发布/完整页面等门禁继续保留。

本节日志和manifest均位于 /Volumes/DevScratch/dm-native-read-http-20261026-7jzu1spv/。当前MinIO官方镜像/二进制获取失败，根已安排按已核实官方commit自建仅测试镜像；并行工作继续，不把环境问题交还用户提供仓库/镜像。最新真实HTTP和分组写UI结果待补。


### 2026-10-27 00时前后 Workbuddy A29只读复核

Workbuddy /Users/junhong/Downloads/code/tp-automation-rewrite 已由旧dirty A27推进到干净HEAD41ba8e1e9b1d625f2e44393ac6df5ffb35eaa818；父85e8cc12f6e521a147a6850de80d941044e5d5c4是Node-RED/Flink样例与smoke的A29提交。393tests/58endpoints/37smoke是其进度文档声明，本次没有复跑也未找到对应运行结果文件；Node-RED真实导入点击和A30调用方适配/告警联测仍开放。样例使用device_id和rule/policy revision，未证明native template/group/fixedmodel references。当前可进入限定范围的隔离集成候选，但DM-RT-008/009及DM-API-011五操作/fence不能算接通；没有合并或修改对方工作区。其边界不阻断本轮file/provider/普通元数据任务。


## 2026-10-27 用户纠正：模板不做版本和草稿

本节优先于此前相反的“冻结版本/原188任务”指令。用户明确模板无草稿/版本表/发布，只有物模型需要版本；根查明D-DATA-004属于此前新增工程取舍而非必要业务需求。详见DESIGN-CORRECTION-2026-10-27.md。22个受影响任务已标记设计纠偏，原ID/测试/日志保留为历史，不当作当前正确设计的完成证据；原188/580/108数量只作历史记录，后续目标随正确功能重建。

已停止Luna所有template draft/revision依赖新增，仍继续独立分组写UI、FileAsset HTTP和asset CLI。文件owner/普通group能力可复用；模板候选0023、0026绑定、DTO草稿与latestRevision、发布历史接口、复制导出和切版预览要协同收简。物模型1.3核心、已发布模型版本引用继续保留。尚未清理完全部候选代码/生成合同；未改生产或已应用migration checksum，不宣布整改完成。


### 2026-10-27 独立功能继续：分组写UI与asset CLI

分组浏览器实际创建子组201、编辑name/description200、加入fixture设备200；刷新读回及PG检查resource_version=3，三次写各1审计/receipt。ui-group-write-result.json SHA8102f3cd6648dfd3aba642e57187632133ae45ab37c976242a0d171fc544808b。两次先前500由测试role缺metadata_idempotency写/读权限引起，改用独立最小列权限role后通过；设备tenant列拒绝跨tenant23514 probe成立，role已撤权删除。原readrole不变；synthetic session不代表真实登录/生产角色/RLS。未实测重复点击重放，未验删除/成功移除。自建CUA tabs/Vite/app/stub和私钥nonce已清理，ownedPG保留合成fixture。

新请求日志ui-native-request-log.jsonl SHA841d9054f96f532d0b7aa696ebaab9f0e4830ca112a29356e6a028a513f275a8；该同名路径已覆盖旧只读阶段日志，旧fe6fcba74b...指纹不再对应当前文件。旧只读result仍在；后续用这次写验收结果和新指纹，不声称旧原始日志仍可重现。

asset CLI新增upload/get，typed multipart/size/MIME/checksum/Location/strongETag/idempotency-key及不跟redirect已有Go tests/vet/build通过。binary /Volumes/DevScratch/tpctl-native-assets-20261027-8zdHaA/tpctl SHA941039665bd5ab34f4b1dd9863922dd5d2df15e822f30b619ae50e7c6d616ad4；尚待真实FileAsset HTTP签收，不把mock测试称实际上传成功。文件API现POST503问题仍定位；MinIO mc实际I/O成功不代替Encore Bucket HTTP路径通过。


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


### 进度清单复核与绑定读取增量

当前设备模板绑定GET实现、前端薄客户端和CLI已进入候选；root实跑current-device-template-binding-read-tests-final.log，4个主测试PASS/0FAIL/0SKIP。覆盖读取投影与租户范围、数值精度与秘密字段、已存连接器不依赖写owner、拒绝未知查询。尚未实际HTTP/CLI验收，不算完成整个关联设备功能块。

前文“模板改模型7→8后旧设备仍7”的测试只证明候选行为，不证明它符合1.3既有继承规则。设备模板引用固定的物模型发布版本，与每台设备是否独立保留旧版本，是两个不同问题；后者仍须核对原实现再决定，不能作为既定需求扩展绑定写入。

用户进度汇报继续采用27页面功能块、23当前必做+4外部后续必需、完整验收0块、11局部流程已验证。FUNCTION-STATUS已同步移除旧49测试/A29过时结论；不从188个历史工程任务状态推导功能完成率。

Luna本次只读交叉核对确认27块计数一致，并查到DM-FE-017仍错误列模板发布/历史为阻断。Root已在tasks.json与TASKS.md撤回该残余项并去除其它任务对它的依赖；旧用例只留追溯，不作为现行需求。


## 2026-10-27 模板共用模型选择与移除成员纠偏通过实际HTTP

1.3源码确认device_configs就是设备模板菜单；模型固定版本选择属于模板，设备引用模板。追加0029复用模板现有thing_model_id/thing_model_snapshot_id并由当前configuration精确解析所选tenant/model/revision；移除设备bindings重复模型列。旧0028checksum不改，未操作生产。普通模板选7→设备7；模型仅发布8不会自动切模板；模板显式选8后关联设备都读8；清空后modelBinding=null且模型snapshots不变。旧“设备独立固定7”测试和历史结论撤回。

Root实际Encore回归首轮编译发现两处测试草稿错误已修；随后26PASS/3FAIL，失败是新模型引用约束下旧list随机model fixture无snapshot、invalid配置fixture缺model字段，以及group幂等重放直接返回JSONB而首响为canonical字节。前两项修fixture并保留生产约束；第三项修生产重放返回已校验canonicalResponse。相关失败/补跑日志保留，补跑17PASS、额外模型tenant/精确version约束1PASS。此17中有既有legacy template回归，不将其当模板发布功能要求复活。

Luna实际HTTP 8项通过：template-model-inheritance-http-result.json SHA f483ce01776c20ccca7d314d028ecb0802bb6e4d3f33efc46f3be7b238e0939d。包括模型选择继承/明确改选/清空/ETag、正式成员移除200与字节相同重放、跨tenant404、其它组及设备保留。模型发布8由owned copy DB中synthetic current-pointer SQL模拟，不是物模型publish endpoint验收；使用signed synthetic TENANT_ADMIN/session stub，不代表真实登录。独有PG/network/env/镜像均清理，原fixture只读未改。镜像构建指纹bbf6b01b…84f013仅作历史制品证据，镜像已删除。

## 2026-10-27 自动化与告警进入隔离集成

用户提供Workbuddy功能对照表并明确要求把自动化/告警接到设备和模板。本轮从其固定HEAD fbdd653d3aded175644293c4b87ca280dc7104c3按git archive引入99个tracked owner文件；没有复制dirty A31 api_types/store/迁移草稿，没有修改对方工作区。新owner与既有l3-core/automation避免同名，包集成为automationowner，公开/v1路径不变。原native metadata owner仍独立拥有自己的数据库，不跨owner写表。

发现原HTTPDirectory仍查Community旧设备表，改由metadata私有目录/对象存在性RPC提供当前原生UUID、templateId、labels、kind；scope按可信tenant查询，unknown online=-1、不造offline，结果超10000明确拒绝而非截断假完整。公共owner统一现TENANT_ADMIN/拒绝SYS无委托及设备受限账号的tenant-wide管理；private对象校验跨tenant/missing/deleted返回404。该最小角色集合不代表完整受限用户产品授权已经实现。

scope写入补request_id：在owner现auto_requests上追加request_hash（迁移15；编号13/14不存在，14预留A31但未复制空迁移），锁定key后同事务绑定写入+收据；同key异意图409，重试返回原实际binding ID，不再返回upsert未落库的随机ID。解绑保持历史，旧重试不覆盖新的重新绑定。ACK操作者只取可信身份，确认字段与ack transition同事务；重复确认不改actor/time/seq、不产生第二条transition且state仍active。新客户端始终传key，旧typed调用无key仍保留自然幂等边界，不能声称旧A27所有操作已被本轮全面重写。

Root完整owner回归405主测试PASS/0FAIL/0SKIP；随后追加绑定kind返回及告警offset分页（total/limit/offset，opened_at/id稳定降序），6项定向主测试PASS，覆盖251条跨页历史与租户隔离、ACK/绑定必要回归。完整405日志在追加分页之前，不能声称最新源完整406已全部重跑。目录RPC有3项查询测试及1项PG身份/跨租户/删除校验通过（早期test fixture未设置auth tenant和短tenant误期望padding已修，未放松生产权限）。索引 /Volumes/DevScratch/automation-native-integration-20261027/current-evidence-index.json。

前端复用独立tp-automation-frontend固定HEAD bdaf3b85a42a5ca0753b9fc34d6e9d830f77b98e的12个规则页/helper/client；API facade统一委托新raw requester，不保留两个auth/transport实现或automation-core第二基址。四个设备/模板Tab wrapper已接native共享面板，告警可分页/确认后读回、按绑定kind区分自动化/策略、保留来源及effective/overridden/issues/未知写入状态。定向transport5测试、TS和SFC检查通过；规则页依赖全仓旧类型错误，未宣称全仓typecheck通过。两父页native身份与菜单入口正在收尾，真实整合HTTP/CUA仍进行中；不记4块完整签收，不把通知真联测/A31/生产发布当完成。


## 2026-10-27 自动化实际HTTP和四Tab浏览器验证

冻结后端V2 manifest SHA7a1cb12d5656305309c0ef9b38e22819e9877f2e5ad7329af83e406d100ff66d；在V1后修复effective参数必须按模板参数+设备覆盖合并的错误，新定向测试通过。实际10项HTTP通过：native scope读取、绑定ID与重放、异意图409、tenant B404/SYS无委托403、设备覆盖仅改threshold仍继承metric、解绑回退模板、告警50+1分页、确认重放恰好一条transition且仍active。结果automation-scope-http-result.json SHA187e0d4325a85c0f0e6131775f052b6cfcaee3d539056717cd27e8d4afe752db。

真实CUA已进入模板自动化/策略、设备自动化/告警四Tab，UI设置threshold60→刷新仍来自device且只标threshold覆盖→UI解绑→刷新来自template且threshold40/metric恢复。安全detector-only策略经真实POST创建后由模板UI绑定，设备策略页显示继承。owned DB合成51条该device告警，UI首屏50/51、加载更多51/51、active1/1、cleared50/50；UI确认并刷新后仍active，SQL可信actor/ackedAt均匹配且ack transition=1。未用真实设备触发，不代表通知投递或真实IDP/动态菜单通过。业务请求走真实owner，无业务mock；外壳legacy logo/sys_version/telemetry在隔离proxy404另记边界。

浏览器初版结果ui-acceptance-owned/browser-acceptance-result.json SHA148f8b1546d45c3e7b4ed098aa420bd73f858b23157200d207ebb9b698165ad5；对应当时request log SHAecd1e87cdf1a213a476e056f746c808ce4dcc2d153bba738b82eadc4c85f5cca。之后浏览器继续修复复测会产生新的日志指纹，勿把初版指纹当最终冻结。

实际发现重要缺口：新建路由URL正确但main为空，memory-router单测通过不能替代页面可用；告警新建缺少kind及alarm spec且原steps携带设备动作；scope新建后仅保存targets不等于已绑定。已安排Luna修复后再次CUA。本阶段仍未签收整个四功能块。另对最后scope解绑是否会退回condition匹配开展最小行为测试，不凭默认假设修改运行语义。

Workbuddy源已更新到干净HEAD8f52e53e（A31，含迁移14/15及恢复对账）。本候选仍固定A30；本地已有native迁移15，后续引入A31必须追加兼容编号并保留已应用迁移checksum，不能直接覆盖15。当前没有生产迁移/发布。

规则编辑器空白复核：同一URL硬刷新后完整表单显示且query保留，无需额外路由生产改动；旧HMR实例不能当当前冷启动结果。保留ThingModel旧冷启动/未登录测试并断言所有目标的实际currentRoute.name及layout/page matched链，定向3项通过。告警表单及scope创建后关联仍在实施，下一轮必须真实UI保存验证。


### 模板触发范围缺陷与最小纠正（V3候选）

原A19测试虽名为“detach stops”却断言最后模板解绑后继续触发，和对照表及Community1.3 type11行为冲突。已只读核对internal/service/automate_telemetry.go:1085-1099及dal/device_trigger_condition.go:40-44：模板条件按实际设备device_config_id及trigger_source筛选，限制触发主体，不是只限制动作目标。新增用例复现模板cfg-1规则被cfg-2相同key事件触发、最后绑定detach仍触发；初次失败原CLI未持久化，仅有明确标注的执行摘要，不能冒充原始失败日志。

根据原功能决定：有任何binding历史的规则，其后续自动触发必须匹配当前device/template有效绑定；从未绑定的旧self条件保持原行为。设备override解绑但模板仍绑定时恢复模板值；最后绑定解绑停止后续新触发，run/episode/pending历史不删除。只改store CandidateRules和RulesMatchingDevice的tenant-scoped history/active过滤及A19测试，没有新增产品模式/模板版本/草稿。动作targets未改。

Luna定向6项选择用例包级PASS、顶层./automationowner整包PASS；没有最新./automationowner/...全部子包回归的新主测试计数。非verbose结果/初次失败摘要分别存binding-trigger-scope-{targeted,full-package,initial-two-failures}.log，索引binding-trigger-scope-evidence.json。后端V3 manifest 464files，SHA1fde40860c3f22c5e1cdfff75726f6c157ac2404ce51d11578a9a6484a6e7d7c；相对V2仅上述store两文件及a19_binding_test变化。新镜像实际HTTP验证仍进行中。

前端补回每行“管理规则”，复用已有rule-manage的编辑/启停/运行记录/删除，用rule_id精确读取一条而非重造CRUD。明确共享规则全局影响，与仅解绑区分。SFC compile与限定eslint已过，真实浏览器操作待验。scope新建与告警表单正在收尾：新告警策略默认detector、无设备动作，保留原event策略未编辑字段；create未知保持同请求，create成功attach失败只重试attach。


### 四Tab新建与实际运行路径增量

V3实际镜像dd7cdf80ea04feb33a0a115d9e560ce7b59705d1c5240a46ffc309b0613c51e1已在owned runtime替换V2，PG/stub/端口保持。模板告警表单真实validation→create→attach→返回模板alarm Tab→设备继承显示均通过；经/v1/ingress/uplink提交唯一key合成遥测后产生1个新episode，设备告警页刷新可见。不是SQL制造该新告警，也不是物理设备/MQTT或真实通知投递。原51条SQL seed只用于分页/筛选/ACK证据，不混为运行触发证据。

V3作用域真实HTTP验证：另一模板fresh设备送相同key，episode0；最后模板绑定detach后，同模板fresh设备首次送key，episode0；从未绑定的legacy self rule（wait-only无外发）收到合成事件后run1，旧condition行为保留。原active告警仍保留，不能用“已有active同subject重复事件没有新增episode”冒充不触发证据，本轮使用fresh subject避免该误判。clone fixture最初携带未引用params被validation拒绝，移除不必要参数后通过，未放松后端校验。v3-http-scope-summary.json SHA4488a4e0d4df25ca6e24c8bcd94cd2745c672d8bbe6c86a0443c245b43ce7b91。

真实表单也发现规则facade与后端的DTO不一致：Validation issues:null；ListRules只有rules/total；日志nested slices为空时null，Outbox是state不是status；ChangePreview真实camelCase scope结构；Simulate是trace及path/outcome，不是前端idx/decision结构；Explain完整性是evidence.note；DeleteRule返回rule_id等处置数据而非deleted布尔。正由唯一前端owner一次对齐真实wire并补实际形状测试，不用放宽为any或新增后端假字段。根已通过V3真实HTTP取list/runs/preview/simulate/explain五条200响应做fixture核对；未发送真实动作。原typed facade单测通过不能代替这些真实响应接入验证。

后续原范围下一点已只读备查：device/manage仍调旧deviceList/group tree/counts，compact也全量读旧deviceList。既有deviceNative.list/create/groupNative.tree可复用；runtime connectivity/alarm筛选目前503，整树计数需正式聚合，不能用假offline或删原功能掩盖。普通新增旧流程deviceNumber未填时服务端默认device.ID，模板已决定设备类型；后续接线应保留自动默认行为，不能因新DTO必填而强迫用户额外填写技术字段。市场安装、连接/凭据步骤是独立owner动作，不能native metadata创建成功就宣布已接入。此段为后续准备，未实现或计完成。


### 本轮本地集成最终验收与交接

前端最终69源文件manifest：frontend-source-manifest-safe-create-final.json SHA20ad0f735ee0ee6378f6096635974ebd790c8a5c5ada3735c874cb4098a7580f；后端V3 464文件SHA1fde40860c3f22c5e1cdfff75726f6c157ac2404ce51d11578a9a6484a6e7d7c。根再次逐文件重算，两份manifest与当前source mismatch=0。未提交、未合并、未推送、未部署生产。

QA真实UI补齐单条管理、编辑validation/preview/PUT、启用/停用读回、运行记录、解释no_data/no_history完整性提示，以及带真实格式合成样本的模拟would_run1/side_effects0。只删除本轮owned wait-only测试规则：force=false DELETE200、GET404、设备规则列表移除；owned DB tombstone存在、enabled=false、delete_reason=deleted_by_user。适用命名non-production workflow/test-only数据例外，不碰用户业务数据。

根发现scope新建enabled规则再attach的跨请求空窗会使never-bound self全租户生效，因此改为create enabled=false→attach→GET确认当前active binding→按原选择CAS enable；新建禁用选择不发enable。关联失败不启用；启用未知先读回，已启用即确认，原version仍disabled才重试，版本被别人改变则不覆盖。刷新恢复保留原expected version，不用GET的新version覆盖基线；也不因绑定missing擅自停用别人后续启用的规则。最后一次真实CUA记录严格false→attach→GETactive→true，最终rule enabled=true与active binding读回。final-ui-create-enabled-summary.json SHA82f67335764e14c80728eb1f4249862112f677c604053215de932b0e8a59cf2e。

最终总UI摘要SHA7e4bad63d390f9330557516bec473a3894fcc7975e91fbf7655e0ae2f444dff7；对应browser request log SHA5c66c0eadedcbbea514a2360aa038370be4a989efdc80c07dce088643c613fae。V3 scope摘要修正了REST惯例造成的201误记：UI create实际200；direct legacy create未持久化精确status则明确未保留，不推断201。修正摘要SHA4488a4e0d4df25ca6e24c8bcd94cd2745c672d8bbe6c86a0443c245b43ce7b91。

Root最新定向API12/12、scope safe-flow4/4通过，另有安全顺序变更前route/transport/form/summary等15项通过；这些包含重复测试，不能相加为新总数。限定eslint0errors，不声称全仓类型检查通过。current-evidence-index.json含分层日志、来源指纹、历史计数边界及失败摘要限制。

四Tab核心链路本地验收完成；不等于三个菜单整体交付或Workbuddy40个功能全部正式验收。真实身份/受限授权、通知投递、物理设备/MQTT、完整runtime owner和旧自动化/type11迁移继续待办。下一原范围执行点为设备列表/普通新增的原生接线，按已有默认编号/模板设备类型保留原用户流程，不新增模板生命周期。owned验收资源正在按精确资源名清理，源码/冻结manifest/脱敏证据保留。


本轮owned清理已完成并按资源名核验：app/stub/PG/network移除，5103停止监听，owned浏览器页关闭，其它9页保留；V2/V3镜像删除，未发现独立V1；14个私有凭据/session/env/stub文件、两份含密码infra配置与Vite缓存清除。原用户服务未碰，source clone与脱敏manifest/log/summary保留。总UI摘要追加cleanup后的最终SHA为f7dc2a39582a4c5078f2da9fd9f318decdd5db0e74996862ff02b43d6a80afca（此前7e4bad…仅功能结果、未含cleanup）。最终plan交叉校验0errors/1历史baseline缺失warning；初次因不支持“部分交付”词汇失败，改回既定“部分实现”后通过，不影响产品代码。

后续设备列表任务已派Luna继续，避免与本轮源指纹混淆，69份前端源已封存frontend-source-safe-create-final.tar.gz SHA f77af4147afa982f8ee01537de8d42d229e7ec1fc0b8b894cb9104918bcf31a3；冻结manifest仍20ad0f…，后续manage/compact源码修改属于下一阶段，不能拿新工作区覆盖本轮浏览器验收制品。后端冻结源码仍保留在本轮普通clone，Goal保持active，三菜单未整体签收。

## 2026-10-27 设备列表与默认编号

- 后端冻结466文件SHA256 `4c1cfb26a70009ee5f3b5350936fece13cd6f13bab0391596942769d3a709857`；前端71文件最终冻结 `29ec8a747a9e4022c5865fe862f2a42c7bc6fac0ce70ada58d88a32130ccaac8`。源码归档frontend-list-source-final.tar.gz SHA256 `51e94604f55c6deff11fb728b9fbd63ea5bc48ff32fb4645fbff79bbcf7b3ef4`。之后普通登记/tags和凭据增量不包含在该冻结中。
- 定向后端最终7个主测试PASS、0FAIL/0SKIP（初次有真实INSERT缺location实参缺陷及fixture lifecycle错误，已修复，初始失败保留）；CLI默认编号测试通过。前端transport13及group/state12通过；scoped tsc/eslint通过，不宣称全仓类型检查通过。
- 自有镜像ID `sha256:804a1692cf0f58407bc97211a7fecf4e6b641549229fe6187328a8a85a696fdd`，后端实际HTTP10/10。模板绑定POST最初503原因为runtime最小权限缺UPDATE(id)以取得FOR SHARE锁；补列权限后原请求重试成功，初次失败保留。
- 真实UI搜索、25台20+5翻页、模板2台、父子组并集2台、未分组23台；模板名称与协议列真实显示。runtime筛选503有持续上次成功提示，未伪造在线/告警。summary SHA256 `256e15a2a4d22d572b8118291bc1322619cb89a7f828b7b40c82c201f7064337`，脱敏请求日志 `fa753f6acb0e9aa45d26a9a83d1e37731292da682079fd83d3ee9c64aa9fa843`。
- 边界：synthetic IDP；compact遥测旧owner404、provider目录旧shell404、无真实物理MQTT/通知。资源仍由本轮QA持有，以复用后续验收；未发布。

## 2026-10-27 凭据存储基础

0030及crypto原语7个不同主测试分别通过；最终schema/replayAAD日志credential-foundation-repaired-tests-v2.log SHA256 `f1cdef6b09bbe41076ce0afdc96a76bed4c3bf1958362dd1a39b4b18c06df58b`为2主测试，schema内另含子测试，不混计31为主测试。首次fixture类型与生命周期失败保留，未修改已应用0030。

GET credential summary新增2主测试PASS/0FAIL/0SKIP，日志SHA256 `3c1a1af0f6ad8cb5bdfdcee51ded815968abc63db020d2c7d908470d19e76088`；只查询摘要列，同tenant live device与授权范围，稳定分页、空[]、严格query、无敏感材料。尚无authn HTTP和create/runtime证据。

## 2026-10-27 凭据创建与加密receipt

固定当前backend477文件manifest SHA256 `abcfe2d5ca2b6dc32ac75aa0a8d1799b20da287888c09b8858a6c22901c3c666`。0031仅扩展既有metadata_idempotency，不新增owner或秘密服务；secret row+HMAC请求摘要+AEAD成功响应+redacted audit同事务，重放对当前主体/动作/资源重新授权并检查credential未撤销/未过期。秘密响应首次10分钟，过期重放清密文留409墓碑；后台定期擦除未访问过期密文仍未实施。

11个主测试最终PASS、0FAIL/0SKIP：4普通receipt、2摘要read、2秘密receipt、3decoder/create-replay/keyring。最终native-credential-read-create-retry2.log SHA256 `2f30543e17d667b3d53f5353b5c41dd6840f195409f934c95ff9dc3e4b0ce996`。首轮unused import编译失败未执行测试，次轮10PASS/1FAIL是cleanup UUID/text fixture比较失败，修后同集合11PASS；初败日志保留。当前不代表整个owner/全仓回归、authn真实HTTP或runtime接入通过。

GET仅SELECT摘要；POST token及username_password生成/导入、X509只接受严格单张公开PEM、secret不进入DB/audit/log。公开DER SHA256与秘密HMAC区分；显式部署keyring，缺失503无默认密钥。接口均pending，不隐式接入；没有reveal/轮换/撤销/activation交付声明。已交QA按固定477源构建、复用owned环境继续实际HTTP。

## 2026-10-27 重启后凭据与分组联调

- 不重跑暂停前11/12/25测试或列表/登记/标签/自动化四Tab。后端479归档与clone一致，Encore1.58.6构建linux/arm64镜像 `sha256:ac61bf0cc4659afdae59df8c466a5436e5b3b902649c636b3c989a3801e22ca2`。owned PG按原SQL追加0030/31并检查完整结构；未使用外层单事务，两个ON_ERROR_STOP命令各0退出，事后逐对象完整核实。无框架ledger，业务migration_log未充当迁移账本；不是生产升级证明。
- 补全的私有infra只引用owned PG四个独立空DB与不可用本地bucket，未访问该bucket/无关DB；不能外推文件或通知owner通过。修复runtime最小列授权：credential FOR SHARE需UPDATE(id)，member FOR UPDATE需UPDATE(group_id)，groups需精确INSERT列与成员关系DELETE；不授devices DELETE/表owner/ALL。初次配置/权限失败均保留。
- 凭据15条HTTP记录证明4创建模式、no-store、严格未知字段422、跨tenant404无写、同key/body原secret内存相等重放及异body409、摘要无secret；全部pending。分组空删200→404→原key重放200、子组/成员409、move/member add/remove与设备保留已验。remove权限修复后用了新key，不能称本轮那次为同key重试。
- CLI补空组删除与冲突：409/exit5、子组200及原key/ETag200重放、父组200、最后GET404。binary SHA `6a0f8ff5d7cf5219fad23fc00b7b1a1481aefa412225d7506321470a5e54bc53`，source manifest fingerprint `8fd7d842f8af81b5b957ba2c24335d339ad41c15164674b77c13c973b8a86102`。临时token/ETag已清理。backend/CLI安全证据在 `evidence/2026-10-27-pause/resume-20261027/`，20文件index SHA `d1342dbb9ad2853ea71cdee422f047b42950ca12533dbc5ac8477fffeaf5bd77`，root核验0 mismatch。
- UI凭据三类kind创建、20+1分页、secret手动清除；POST真实201后丢浏览器响应，换Tab不丢请求/离开路由受阻，原key/body重试仅1条新增。凭据UI摘要 SHA `7e492f3980afea435466ece71fbc42553d1a0146f224ae4180853f42129669ab`，脱敏drop证据 `f9a7a04bf21a4b662bc8f1de36f0f520de0a6a459cc1456ff43dfe794d536a51`。
- UI group DELETE真实200后丢响应，保留原key/body/ETag，retry前无GET已404组，重试成功后刷新目标消失。移动后reload路径保持；成员首次stale冲突后刷新版本移除成功，设备列表仍1台并可加回组。device详情另有404提示，设备列表是保留证明，不假称全详情通过。group UI摘要 SHA `d97f65ac4d860c04dc23e8e9292d4ba5998ae1d87495d12b372d6ed774eb246f`，drop日志 `d4ce2686e3f702464abeab5ba7c9d99b84f3e5e6d751cd2f83010f47a2444f35`。
- 源码修复：两处删除UI共用冻结intent，不将删除成功后读取失败当失败；按logged-in/subject/tenant隔离并保护晚到回包，同账号token续期不丢key。删除state6定向测试通过；凭据session/空别名4定向测试通过；SFC编译/局部helper类型检查通过，不宣称全仓vue-tsc通过。frontend82-v3 manifest SHA `27734a525d8864a6c31bcd2371e21acbc5e7c5ec6147524a0e5d77a4c66bcccf`，tar `c2e984a71b816369bfa93c3521ef062be50efdd4cc781d98a93b27b727de85bb`。业务UI证据基于v2未变路径继承，v3只对身份别名小修局部验证。
- 工具诊断曾回显一个owned合成token，已清屏并单条撤销，API与UI确认revoked；安全证据不含该原secret，但不能声称所有tool trace零秘密。实际logout请求被synthetic fixture的旧logout404阻挡，不搭新登录系统，不作为auth真实注销已验。全部为本地stub身份，无真实IDP/物理MQTT/启用/轮换/reveal/旧数据迁移交付声明。
- 单次drop代理55068已停，gates=false，API55067/Vite5114/PG保留复用；私有session/keyring不入Git。后续从剩余真实运行链路与模板原功能继续，不重新搭本轮已验证环境。


## 模板扩展信息 current configuration（2026-10-27，本轮增量）

范围：DM-TPL-019和DM-FE-025中的模板字段定义切片；列表/新增/编辑/启停/删除，未增加版本、草稿、发布、新表或迁移。设备资料表单不在本切片完成。

- root合同：原PATCH新增可选customFieldChanges，严格配置校验、旧/新定义兼容保护覆盖完整替换与字段增量；模板FOR UPDATE与设备写值FOR SHARE串行，0/false/null皆算已保存值；冲突409与ETag412分开。
- 后端冻结480文件：manifest `8e8ef99be664e57456b8199f701a9d23e45347ba82a33bdc52237f0abbeb38ea`，archive `5ef78af75db18c930afd8dfea60bb36894b0c8cc7de7e90ba5861152a15f3d3c`。root逐文件及归档核对均0差异。制品在evidence/2026-10-27-pause/resume-20261027/backend-source-*-template-custom-field-changes-480.*。
- 前端冻结85文件，继承82-v3，实际新增/改变7文件（3生成合同、helper/test/component、Tab入口）。manifest `4b28559b5539b939b86e5e81ff1113c256bce7a5d49be009ba8cbcc5b5baea91`，archive `74bdc2df448532128c1da6d4c8a5e113b4af5bca5159876a29d15803f75633b4`；evidence/resume-20261027/frontend-source-*-template-extend-v1.*。
- 前端8项helper通过；helper tsc、SFC compiler、target eslint通过。局部vue-tsc受既有依赖图错误影响，目标文件0 diagnostics，不能宣称全仓类型通过。日志：evidence/resume-20261027/template-extend/。
- 仅运行改动相关验证；之前凭据/分组/列表、25/60历史套件不重复。HTTP/UI本轮验收结果待追加，不将源码冻结当运行完成。


### 本轮后端真实运行结果与前端v2修正

最终480普通clone与冻结逐文件0差异；运行镜像`sha256:cc422dadc7014c26e4af8c8880b6afb149f19cc8504795baf85e17d4bea3e3ad`，owned API55067。真实HTTP 14请求/17项检查通过（含复用fixture检查），不表述为17次HTTP请求。覆盖字段增量与完整配置的已有值409、0/false/null、枚举收窄、成功差量保存、原body重放/异body409、严格shape422、ETag412及未改配置/高精度数值保持。root已核对7份共享test/build/HTTP/permissions/session证据hash一致；详情在evidence/2026-10-27-pause/resume-20261027/template-custom-field-*及synthetic-session-rotation-containment.json。

成功保存首轮503定位为owned runtime缺少现有aggregate save的12列UPDATE权限：has_column_privilege与EXPLAIN无写入复现SQLSTATE42501，补精确SET列后成功；原id锁权限保留，table UPDATE仍false，未授tenant_id/deleted_at/ALL，无schema迁移。复用原环境，不重建PG。

前端v1实际UI遇到unknown后retry入口未及时渲染；仅template HMR保留原intent后显示Modal内原请求重试，成功创建一次。最终v2将unknown状态改为新对象替换，避免原地raw对象改动不触发ref；保存结束时显示重试入口，不丢原payload/ETag/key。8项本轮helper及SFC/target eslint/helper tsc再次通过（因本次v2修正，非旧套件回归）。85-v2 manifest `e9ce86fdf3e4960dff880a07ba49a1eda577bf23e02803ff0c3424fb093478fa`，archive `280cdabbb0b17ca3cfe2d9a3ed11b3054079825b9d5a8b9a850d0d7cc17e8169`，替代v1，原制品保留。

本轮诊断误输出4条owned synthetic session token；已替换active stub会话映射，新会话GET200，签名有效但未登记会话401，源码核对为token精确命中且无fallback。原4值不再保留，因此未取得逐token401，不声称4/4。没有生产令牌操作。相关诊断禁止打印未知JSON key集合。


### 本轮UI最终验收（frontend85-v2，root已核对）

实际浏览器通过：unknown原请求在Modal内恢复且仅创建一次；Number默认null编辑为0，启用读回；无值字段删除读回消失；含设备值legacyMode删除提示409且行保留；单独默认值Safe→Manual允许且已有值不被清理；新增Enum默认null并读回。旧详情URL刷新按预期跳canonical，id保留，模板GET200、详情DOM与最终字段读回正常。早先把logo/sys_version独立404 toast误判为详情404，已通过实际网络与DOM纠正，不修改路由、不重跑既有冷启动测试。

最终证据evidence/resume-20261027/template-extend/template-ui-acceptance-v2.json SHA `dc3cad2ead761d142cccfe6608c4ce3b6af6db4e0f9f009e5e24f39f419c418f`；events JSONL SHA `064b2f36b4c2dce4a76fcbb7ed0607555f5a9e9ff69c2fda23489354906ce328`（历史误判保留并标superseded）；frontend-v2-target-checks.log SHA `3ba23ea76dc8c53034f73149b37e05dc5ebfb2e02770c4f1d2d088e1c6615efa`。root核对上述3文件hash及85源码无漂移，源码v2后未改。55068无监听，API55067/Vite5114继续保留，仅本地合成身份验收，不代表真实身份/升级/三菜单整体签收。


## 设备“信息”Tab收尾（2026-10-27，本轮增量）

范围：DM-DEV-025/DM-FE-025中的原位置/地图和模板扩展资料值表单。模板current definitions和设备GET/PATCH、OAS、backend480/image均复用，不增加版本/草稿/新表/新HTTP操作。旧message.vue保留，details message占位换新组件并show:lazy。

- 新专用device-info-native client沿共享metadata requester使用原GET/PATCH，保留资料/位置JSON scalar原token；整体替换customFields时不舍入未改数字或丢停用历史值，不自动写入模板默认值。
- 前端编辑仅合并dirty字段/位置；已知初始412刷新最新数据，保留dirty输入并重新确认，未改字段取服务器最新值。未知结果保留原body/ETag/key，shallowRef+immutable attempt防Vue代理身份比较失败，session/target及重挂载保留未解决原请求。
- 地图仍按钮按需弹窗，不改TencentMap；失败读取/模板关系不一致禁写并可刷新；Boolean selector按实际NaiveUI string值实现，不用truthy转换false。
- 5项新client tests、9项新state tests、scoped helper/client tsc、目标lint和SFC compile通过；客户端tsc使用临时localStg shim，只证明本范围，既有全仓ambient/import诊断仍未清，不宣称全仓构建通过。
- 前端90-v1冻结：manifest `557c89cea2e25228f1574b36e9ff04317bcda5c7c6d927a23c6ad728c15ed347`，archive `9a42804368c9b5cf66b99959b1138a71cb61df9c7db86200dd0a3bcf507f5ea2`，root逐文件/归档0差异；继承85-v2，5新文件与details入口共6处改变。
- client checks/setup共享evidence/resume-20261027/device-info/；真实API设置两台INFO fixture只是验收前提，不重跑模板/凭据/组/列表旧套件，不部署或改业务数据。
- 实际UI验收正在进行。一次受控并发GET200/PATCH200仅改bound fixture的count=7和地址，Decimal序列化保留其余原值供新页面验证412；并发安全summary `28ef3176b740d8b8ccdb0efc9575e5828609d316d162ff415287f20034d8e6f7`。最终UI/raw读回待追加，不提前把信息Tab计为完成。


### 设备信息最终90-v2验收

- v2仅撤掉新组件对旧共享map-validator的依赖，按既有native Location合同允许(0,0)，以null表示未设置；原共享validator不改。新增1项位置定向case覆盖(0,0)、边界、单边、越界通过，受影响helper tsc/SFC/target lint通过；v1 client5/state9已过且未受影响的结果继承，不重跑旧整套。
- 最终90-v2 manifest `00e15f2e287a25d8cc8809ef8ce26dfb7cb0371df47a6e2daec39e180e797a18`，archive `8ab37f479c36b5b7c6ae07c1bb468e7588df82c95030007e78b9acccd30247a2`，root核对90文件无漂移。
- 实际UI：组合字段/中文位置保存读回；无模板(0,121.5)及v2(0,0)保存、清位置为null；一次受控并发后412只保留dirty Note，count7及并发地址保留；真实上游200后丢响应，原key/body/If-Match重试，retry前无GET，恢复后最终resourceVersion5符合4次逻辑更新+创建，未重复提交。
- 最终只读GET200：精度1.000000000000000001、停用历史9007199254740993、false、optional缺省及并发地址/空坐标保持。初读摘要曾拿早期Note/null-mode目标比较出false；UI后来已改Auto和新的Note。最终定向布尔读回NoteMatchesUnknownTarget=true且version仍5，旧摘要保留，root-final-acceptance.json明确旧断言过期，非产品失败。
- UI结果SHA `07a27e7babb13dc44c6f88563ed3b9cd9ba98f53c1e5b2bf947d95d35e84c512`；v2checks log `ccbf794102445c7be78c7f0fa72b2fe841b63edfc7ba60d3ef12435e80191411`；脱敏drop events `4c524dcfcd0086298a9e2008687143ef259be062cb078815e96793065bf5be5c`；最终Note safe `c6c829697499b484069e68b6e4d798c4378b096c51fbaa9f7649e7483e5c62ce`。root已核对共享文件hash。
- 环境还原：API55067/Vite5114运行，apiBase55067；55068关闭，gate active/armed=false。management与metadata目录名一度混淆，原scratch始终完整，未做清理/重建会话。仅owned synthetic fixture，本轮不宣称真实身份、运行owner或三菜单发布完成。


## 模板详情三个 Tab（协议配置 / 数据处理 / 设置）源码切片（2026-10-28，本轮增量）

范围：设备模板详情的协议配置（Topic 映射增删改启停）、数据处理（六阶段 Lua 脚本增删改启停）、设备设置（封面、自动注册、在线判定策略、启用/停用）。三块均复用现有 GET/PATCH 合同，未加模板版本/草稿/发布、未加后端端点、未做迁移。**本轮只到源码与单元级验证，运行验收未签收。**

- 合同复用：`UpdateDeviceAccessTemplate` 已含 `configuration`、`imageAssetId`、`administrativeState`；`configuration` 内 `topicMappings`/`processingScripts` 与 `access.credentialPolicy.autoRegister`、`access.onlinePolicy` 都在同一 PATCH 内，因此三 Tab 不需要新端点。校验规则逐条对齐 `accessconfig.Configuration.Validate`：脚本 `language=lua`、`entrypoint=encodeInp`、`content` 1..262144、同 phase 至多一条启用、最多 100 条；Topic 映射 name≤128、source/target≤1024、priority 0..10000 整数、dataKind/dataIdentifier 同空或同非空、`(direction,sourceTopic,targetTopic)` 唯一。
- 字节保留：`template-configuration-raw.ts` 用容错扫描器按字节定位 `configuration` 及其成员，只拼接被改成员；本轮新增 `replaceConfigurationPath` 支持嵌套路径与标量，因此改 `autoRegister`/`onlinePolicy` 不会重编码 `access.parameters`。测试用 `9007199254740993` 与 `1.000000000000000001` 作探针，替换后原样保留。
- 冻结请求：新增共享 `template-configuration-attempt.ts`，按 `(templateId, scope)` 冻结 payload+ETag+`Idempotency-Key`，身份变化清空，unknown 只允许原请求重试；`template-topic-mappings-state.ts` 改为委托同一存储，对外签名不变（11 项旧测试原样通过）。封面与启用状态作为独立 scope 单独 PATCH，不携带 `configuration`。
- 校验结果：`template-*.test.ts` 7 文件 **55/55 通过**（本批新增 24：共享 attempt 6、数据处理 9、设置 6、路径拼接 3；回归 31：Topic 映射 11、raw 成员拼接 6、transport 6、自定义字段 8）。目标 15 文件 eslint **0 error**（18 条 `no-unused-vars` 均为 interface 参数名，与未改动 transport 基线同型）。四个改动 SFC 的 script+template 编译通过。全仓 `vue-tsc` **1224 条既有诊断，本批文件 0 条**。不宣称全仓类型/构建通过。
- 源码冻结 105 文件（继承 device-info-v2 的 90 文件，**0 漂移**）：相对上一版改 3 个（`metadata-native-http.ts`、`template-native-transport.ts`、`config-detail/index.vue`）、新增 15 个。manifest `04321feb48a9833f0af343892c600023654ba23908b036e97542691459c8bde5`，archive `8ae71285baf7c4edb6ed88446180d7432e7701e2dd15c01c99a0cfa9b3860ed1`（105 条目，与清单一致）。helper log `64ac79703e020d7399468c0d65ce00ccd62878b293d86548bfcfdc350e9884cf`，target checks `fa0a25691ea5542982eb1c8fcf16fb980164ed6ae25faf24fdd32d23a1b17429`，状态说明 `e4b3dd7535c882c7cdffb6201917bfc68b2ba18d5f51f7eb66291caeaeafd9b5`，均在 `evidence/resume-20261027/template-abatch/`。
- **运行验收未签收**：OrbStack 重启后自有 API 镜像与 stub 容器消失，`/tmp/ev/` 的协议配置 HTTP/UI 证据被系统清理，因此早前那次「PATCH 200/旧 ETag 412/无物模型 422/刷新读回精度」只能记为已执行、制品不可复核，不作为证据引用。三个 Tab 一律不计完成。
- 环境阻塞（已核实，非猜测）：macOS 上 `encore run` 无法起全应用，`notification` 的隐私守卫硬性要求 `GOOS=linux`，init 失败即致命退出；`encore run` 无 `--services`；`encore build docker --services … --skip-config` 在 Encore CLI 1.58.6 上挂起 18 分钟零输出（仓库 README 已记录该 CLI 在此路径的循环），已终止。owned PG 卷 `codex-dm-native-read-57fc52d5-pgdata` 仍在。下一步改走仓库自带 Linux 容器通道重建 harness，再一次性补 A1/A2/A3 的真实 HTTP 与浏览器验收。
- 页面明示能力边界：数据处理「调试运行」依赖 `POST /api/v2/processing-script-tests`（无 handler），页面上直接写明不可用，不做假入口；设备设置停用按合同语义提示「只阻止新绑定/新激活，不自动断开现有设备」。
