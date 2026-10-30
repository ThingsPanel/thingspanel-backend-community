# 设备与模板详情Tab开发实态审计（2026-10-27）

## 结论

21个详情Tab并非只做了两三个。按当前证据，6个主要本地流程已验、15个未收尾；“还剩11个”是设备信息Tab完成前的历史截点。这里的“已验”指本地合成身份与owned测试环境内的页面/API主流程，不代表真实owner、物理设备、通知或生产发布已验。27块整版签收仍是0，不等于没有局部开发。

剩余15项按当前页面实态拆分为：4个当前Tab占位（设备子设备/设置、模板数据处理/设置）；7个运行类页面仍走旧运行链（设备图表/遥测/属性/事件/命令/期望消息/诊断）；4个部分接入（设备连接凭据、模板关联设备/物模型/协议配置）。这是源码接线盘点，不替代功能状态表原有的“7部分+8待接”阶段统计。标注“新”指本轮新接线或新实现，“复用”指继续调用旧代码/既有基础能力，“占位”指当前入口不可操作。文件与函数用于说明实态，不以文件存在本身判完成。

## 设备详情 13 Tab

| Tab | 状态 | 实际源码与调用 | 新增/复用/占位及真正缺口 |
|---|---|---|---|
| 图表 `chart` | **运行仍走旧链** | `frontend/src/views/device/details/index.vue:62-68` → `modules/telemetry-chart.vue:15,211-225`；读取 `telemetryDataCurrent`、`getAttributeDataSet`，通过 `resolveDeviceNativeModel` 装饰字段。 | 复用：原 ThingsVis 图表与实时推送；新增：原生模型字段解析。缺口：数据读取和实时/历史推送仍旧runtime API；kernel 当前没有对应 `/api/v2/devices/:id/readings` handler，不能把模型元数据接入算成原生运行时完成。 |
| 遥测 `telemetry` | **运行仍走旧链** | `index.vue:69-74` → `modules/telemetry/telemetry.vue:10-23,274-305`；仍调用 `getTelemetryLogList`、`telemetryDataCurrent`、模拟数据接口并建旧WebSocket。 | 新增/复用：增加原生ThingModel字段映射/显示值辅助，界面、历史、导出、模拟器及WS沿用旧链路。缺口：原生实时读数、历史分页/删除/导出、模拟器与正式设备流未接通。 |
| 连接 `join` | **凭据子功能已验，整连接未完** | `index.vue:75-80` → `modules/native-device-credentials.vue`；kernel `devicemetadata/native_credential_read.go` 与 `native_credential_write.go` 提供原生摘要与写入。 | 新：凭据创建、摘要分页、敏感值一次性显示/恢复已有本地HTTP与UI证据。缺口：合成IDP身份、真实设备接入、启用/轮换和真实连接验证；pending凭据不代表设备已接入。 |
| 子设备 `device-analysis` | **Tab占位** | `index.vue:83-88` 明确挂 `NativeDetailPending`；提示组件显示“正在接入”。OpenAPI有 `/api/v2/devices/{deviceId}/subdevices`，但kernel没有匹配的Encore route/handler。 | 缺口：原生网关关系查询/增删/迁移与UI工作流；OAS条目仅为合同，不是已运行实现。 |
| 信息 `message` | **本地主要流程已验** | `index.vue:90-95` → `modules/native-device-info.vue`，使用 `deviceInfoNative` 对现有device GET/PATCH编辑位置和customFields；安全精度解析见 `service/api/device-info-native.ts`。 | 新：原生信息Tab、局部更新与raw-token保留。复用：既有设备GET/PATCH、模板定义读取。90-v2页面和API本地读写、412保留dirty值、原请求恢复、(0,0)/清位置等已验。缺口：真实身份与真实设备/owner条件，不是缺少本地页面实现。 |
| 属性 `stats` | **运行仍走旧链** | `index.vue:97-101` → `modules/stats.vue:7-18,155-170`；绑定时可用原生ThingModel元数据装饰行，但数据/下发/日志仍调用 `getAttributeDataSet*`、`attributeDataPub`、`expectMessageAdd`。 | 新增：模型定义/展示字段的接入。复用旧：读值、发布、历史与期望消息仍由旧接口提供。缺口：原生读数及属性写入/回执/删除HTTP链路和UI实际联验。 |
| 事件上报 `event-report` | **运行仍走旧链** | `index.vue:103-107` → `modules/event-report.vue:6-18,45-50`，使用旧 `getEventDataSet`；只用原生事件定义给返回行补充名称/显示格式。 | 复用旧：事件历史列表接口。新增仅有元数据装饰；没有native readings/events endpoint handler。缺口：事件查询/分页/载荷解码与设备实际上报联验。 |
| 指令下发 `command-delivery` | **命令选项已复用，原生执行未开发** | `index.vue:109-113` → `modules/command-delivery.vue:7-28,83-101`；命令选项来自原生模型快照，但发送、回执日志及创建期望消息调用旧 `commandDataPub`、`getCommandDataSetLogs`、`expectMessageAdd`。 | 新增：原生命令定义显示。复用旧：实际下发和日志。OAS存在command-executions合同，但kernel未见匹配handler。缺口：原生命令执行/状态/取消API和UI联验。 |
| 期望消息 `expect-message` | **运行仍走旧链** | `index.vue:115-119` → `modules/expect-message.vue`，调用旧 `expectMessageList`、`expectMessageDelete`。 | 复用旧：列表/筛选/删除UI和API。没有原生期望消息endpoint或页面接线。缺口：原生队列创建、读取、取消/过期与状态联验。 |
| 自动化 `automate` | **本地主要流程已验** | `index.vue:121-125` → `modules/automate.vue` → `shared/native-automation-panel.vue` → kernel `automationowner/service.go`、`binding_service.go`。 | 新：native owner面板/规则与绑定链路；真实HTTP/UI创建、覆盖、解绑、启停、日志等已有证据。缺口：真实owner/权限、实际设备运行器升级与端到端触发条件，整块签收未做。 |
| 告警 `give-an-alarm` | **本地主要流程已验** | `index.vue:127-131` → `modules/give-an-alarm.vue` → `shared/native-device-alarms.vue`、`native-automation-panel.vue`；kernel `automationowner/service.go` 告警/策略routes。 | 新：告警和策略原生owner接线；本地列表、筛选、确认、合成遥测触发读回已有UI/HTTP证据。缺口：真实设备/MQTT、通知owner与真实权限/运行链路。 |
| 诊断 `device-diagnosis` | **运行仍走旧链** | `index.vue:133-137` → `modules/device-diagnosis.vue:9,151-165`，调用旧 `deviceDiagnostics`、调试状态/日志接口。OpenAPI虽列 `/api/v2/devices/{deviceId}/diagnostics`，kernel无对应handler。 | 复用旧诊断和调试路径；本轮新原生Tab没有接线。缺口：原生诊断/模拟器配置/读数健康信息API与页面联验。 |
| 设置 `settings` | **Tab占位** | `index.vue:139-144` 挂 `NativeDetailPending`。详情页头部另有设备name/description/tags的原生GET/PATCH，不属于该Tab。 | Tab不能操作。复用：详情头编辑已接原生设备PATCH。缺口：先按旧功能边界确定Tab设置项，再接原生合同；不能把头部信息编辑算成设置Tab。 |

## 设备模板详情 8 Tab

| Tab | 状态 | 实际源码与调用 | 新增/复用/占位及真正缺口 |
|---|---|---|---|
| 关联设备 `associatedDevices` | **部分接入：原生分页读取已连** | `frontend/src/views/device/config-detail/index.vue:64` 调 `templateNative.get`；`getAssociatedDevices()` 使用 `deviceNative.list({templateId})`；Tab `:185-198` 展示原生分页结果。旧独立组件 `modules/associated-devices.vue:19,81-101` 仍调用旧 `deviceConfigBatch/deviceList`，但不由当前detail index挂载。 | 新：当前详情只读列表接原生。复用：旧关联弹窗留存但非当前入口。缺口：当前页创建/绑定/解绑完整原生操作及浏览器验收；不能将旧组件存在视作接线。 |
| 物模型 `thingModel` | **部分接入：详情只读，编辑复用既有原生物模型流程** | `index.vue:200-209` 读当前模板配置显示modelId/publishedRevision；提示去模板编辑页修改。`config-edit/native-template-editor.vue` 和 `thingmodel-native`负责模型列表、版本选择。kernel `thingmodel/service.go` 有模型/版本/资源合同。 | 当前详情没有直接编辑入口；模板原有编辑页已具模型选择能力，审计不把“详情页不能改”另算新缺口。剩余是该既有编辑路径与绑定影响的完整联验。**模板本身没有草稿/版本/发布模型；物模型仍保留版本能力。** |
| 协议配置 `protocolConfig` | **部分接入：当前配置只读** | `index.vue:210-218` 仅显示native template的 `configuration.access` 字段；旧 `modules/connection-info.vue` / `form.vue` 有旧编辑实现但不在当前详情入口。 | 新：当前配置读取显示。复用：旧协议编辑代码未迁入当前详情。缺口：确认原有协议配置编辑工作流在模板编辑页是否完整接入原生PATCH并完成实测；不新增provider发布产品。 |
| 数据处理 `dataProces` | **Tab占位** | `index.vue:219-221` 挂 `NativeDetailPending`；旧 `modules/data-handle.vue` 有数据处理UI源码但当前入口不调用。 | 六阶段Lua配置/启停/调试原生路径未开发。旧代码仅作行为参考，缺口是原生配置读写、安全校验、运行owner及页面联验。 |
| 自动化 `automate` | **本地主要流程已验** | `index.vue:222-224` → `modules/automate.vue` → `NativeAutomationPanel(scope="template")`；kernel `automationowner` rules/bindings APIs。 | 新：模板scope原生规则与绑定。已有本地HTTP/UI创建/覆盖/解绑/启停/日志证据。缺口：真实owner/权限、设备实际执行和整版签收。 |
| 告警 `alarm` | **本地主要流程已验** | `index.vue:225-227` → `modules/alarm-info.vue` → `NativeAutomationPanel(scope="template", kind="alarm_policy")`。 | 新：策略与告警owner原生接线；模板策略、设备继承及合成触发已有实际UI/HTTP验证。缺口：真实物理上报、通知owner和生产权限条件。 |
| 扩展信息 `extensionInfo` | **本地主要流程已验** | `index.vue:228-230` → `modules/native-template-extend-info.vue` 调 `templateNative.get/update`；kernel `native_template_read.go`、`native_template_write_raw.go`。 | 新：保留既有customFieldDefinitions功能的原生增改删/启停和设备值冲突保护；HTTP/UI与失败恢复已验。缺口：真实身份/迁移/整版owner边界；未增加版本、草稿或发布模型。 |
| 设备设置 `devicesSetting` | **Tab占位；资产底层能力已有** | `index.vue:231-233` 挂 `NativeDetailPending`。旧 `modules/setting-info.vue` 调旧 `deviceConfigDel/deviceConfigEdit`。模板图片有独立原生file-assets HTTP/CLI证据，但当前Tab没有封面编辑入口。 | 封面asset上传/下载底层已存在；Tab与模板注册/在线策略未接。缺口：把属于原有设置项的封面等行为接入当前native模板页并实测。不能把孤立文件API当此Tab完成。 |

## 如何理解“11个”和“未收尾”

- 用户提到的“设备11+模板5”是设备信息Tab本轮完成前的阶段计数；当前状态表与最后一轮90-v2证据都更新为设备10、模板5，共15个未收尾详情Tab。
- 当前15个按实际代码接线组成：4个Tab占位（设备子设备/设置、模板数据处理/设置）；7个运行类页面仍走旧链（设备图表/遥测/属性/事件/命令/期望消息/诊断）；4个部分接入（连接凭据、模板关联设备/物模型/协议配置）。连接凭据子功能已有本地验收，但真实接入未完，因此连接Tab整体仍未收尾。
- 6个本地主要流程已验：设备信息/自动化/告警，模板自动化/告警/扩展信息。它们是本地阶段完成，不代表整版交付；整版27块目前签收0。
- `openapi.yaml`中出现的readings、subdevices、command-executions、diagnostics、simulator-config等操作是合同声明；在当前kernel `encore-api`源码中没有对应 `/api/v2/devices/...` handler。Tab对接不能仅凭OAS声明或旧组件继续可用就算新原生链路已开发。

## 核验来源与边界

- 页面入口：`frontend/src/views/device/details/index.vue`、`frontend/src/views/device/config-detail/index.vue`；占位提示：`frontend/src/views/device/native-detail-pending.vue`。
- 运行时调用：`frontend/src/views/device/details/modules/telemetry-chart.vue`、`telemetry/telemetry.vue`、`stats.vue`、`event-report.vue`、`command-delivery.vue`、`expect-message.vue`、`device-diagnosis.vue`；确认旧API仍被实际Tab引用。
- 原生实现核对：`kernel/encore-api/devicemetadata`中当前仅有设备/模板、凭据、分组、统计/绑定读写等相应v2 handlers；`kernel/encore-api/automationowner`实现自动化与告警owner；`kernel/encore-api/thingmodel/service.go`实现模型相关操作。对运行时OAS操作核对过全kernel `encore:api` routes，没有把未实现合同误判为handler。
- 证据索引：`backend/docs/initiatives/device-metadata-native-cutover/FUNCTION-STATUS-2026-10-27.md`、`TASKS.md`、`EVIDENCE.md`（最新“设备信息最终90-v2验收”及模板扩展信息收尾）。只读源码/证据审计；未运行测试、未构建、未操作运行环境，未改业务代码。
