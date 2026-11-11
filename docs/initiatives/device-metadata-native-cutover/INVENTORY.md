# 三菜单完整功能库存与处置入口

> 2026-10-25按各附件注明的当前源码范围重新复核；本轮增量、Git状态与证据边界见[REVIEW-2026-10-25.md](REVIEW-2026-10-25.md)。原2026-10-23证据保留其历史日期。唯一目标合同已修订为2.0.0-plan.4；内部引用协议见API-CONTRACT §10.1。

初次库存：2026-10-23；增量复核：2026-10-25。这里按真实页面/操作/字段/请求、服务行为、消费者与候选源码分别列证据；同一业务可能有多条前后端证据，**不可相加当“功能数”或完成率**。

|附件|覆盖内容|状态口径|
|---|---|---|
|[FRONTEND-INVENTORY.md](FRONTEND-INVENTORY.md)|139条页面、操作、字段、请求、消费者及候选证据行；包括表单/筛选/弹窗/批量/详情/按钮权限|当前请求链与候选分支分列；139 ID保留并纠正路径/可达性，补齐已有服务实例、子设备详情、看板模板及市场组合包子场景；本轮未重取的候选views单列历史日期；没有浏览器/测试通过证据|
|[BACKEND-INVENTORY.md](BACKEND-INVENTORY.md)|设备29项、接入模板16项、分组12项，以及框架/表/权限/消费者/初始化行为|旧行为证据，不是新接口验收；每项给处置和验证差异|
|[ENCORE-INVENTORY.md](ENCORE-INVENTORY.md)|当前devicemetadata/l3旧代理、9个候选native模型操作、类型/资源/快照、CLI和已知实现风险|已有代码与测试源；没有当前全栈通过或发布证据|
|[LEGACY-ROUTES.md](LEGACY-ROUTES.md)、[legacy-routes.json](legacy-routes.json)|从源码提取165个相关旧路由操作，逐条路径/handler/行号/归属|退役与消费者反查账本；不是迁移完成度|

## 处置只能使用以下五类

1. 新系统计划完整实现：新正式API/数据/CLI/页面完整闭合。
2. 已有Encore代码可复用，但未完成验收：必须给候选/文件/测试源与本轮限制。
3. 属于其他明确负责的运行时服务：仍是本次正式合同和端到端验收依赖，不等于“以后再做”。
4. 经产品边界确认后不迁移：本轮没有擅自取消业务功能；被用户明确禁止的旧代理/投影/兼容入口属于已确定技术退役，业务功能由新用例承接。
5. 需要业务方确认：仅限DECISIONS中确实不能由源码决定的业务范围/政策，未决仍阻断发布。

前端附件用完整/复用/运行时/不迁/待确认缩写，开头给出一一映射。后端附件逐行使用全称。Encore附件的当前入口处置按下表补充，保证没有借“参考”逃避决策。

|Encore入口/对象|唯一处置|目标任务|
|---|---|---|
|当前设备、接入模板、产品、分组基本CRUD/store/schema|已有Encore代码可复用，但未完成验收|DM-DB / DEV / TPL / GRP；正式合同及认证/并发/引用需对齐|
|37fbb5e原生模型core/app/资源/发布/测试|已有Encore代码可复用，但未完成验收|DM-API-007；只做必要transport和消费者接线|
|当前actuate/MQTT cache/latest|属于其他明确负责的运行时服务|DM-RT-002/005/006；control/telemetry，移除第二执行owner|
|当前onboarding自动造模型/legacy映射|新系统计划完整实现（重用合法创建用例，排除旧副作用）|DM-DEV-003/024、DM-RT-013；合法无模型/无模板保留|
|legacy_projection_port/runtime-projection/l3旧代理/Facade|经产品边界确认后不迁移（用户明确禁止该技术架构）|DM-RT-012、DM-MIG-014；业务由正式API承接|
|公开from-legacy/rollback/旧迁移HTTP入口|经产品边界确认后不迁移（不作为运行API保留）|DM-MIG系列离线受控runner；只用正式native写入口|
|旧custom-command-controls/custom_controls|需要业务方确认（可表达内容优先无损转换）|DM-DEC-001；COMMAND与展示资源承接，不新增物模型核心|
|CLI现有设备代理、template project-runtime|新系统计划完整实现（正式命令替换旧路由）|DM-CLI-001—018；复用Go/shell基础，不保留旧HTTP|

## 源码反查补入的独立任务

|功能缺口|最终任务|主要合同|
|---|---|---|
|改编号；预登记/扫码激活明确排除|DM-DEV-021/022、DM-FE-030|changeDeviceNumber；无企业版资格接口|
|批量模板绑定与解绑|DM-DEV-023、DM-FE-031|batchSetDeviceTemplateBindings|
|无模板服务接入、外部号、逐设备参数|DM-DEV-024、DM-FE-029|CreateDevice.directAccess / deviceParameters|
|扩展资料与位置、标签|DM-DEV-025、DM-TPL-019、DM-FE-025|customFieldDefinitions / customFields|
|高级联合过滤与未分组统计|DM-DEV-026、DM-FE-003|DeviceFilters / DeviceStatistics|
|多级网关与批量关系|DM-DEV-018/027、DM-FE-032|gateway-membership-batches|
|Topic Mapping|DM-TPL-017、DM-FE-023|TemplateDraft.topicMappings；kind+identifier|
|六阶段Lua脚本与沙箱调试|DM-TPL-018、DM-FE-024|processingScripts / testProcessingScript|
|动态注册、一型一密|DM-TPL-020、DM-RT-013、DM-FE-027|enrollment-credentials / enrollDevice / scoped status|
|图片资产|DM-TPL-021、DM-FE-027|file-assets，模板持久assetId|
|延期命令队列/取消|DM-RT-014、DM-FE-033|command-executions / cancellations|
|状态历史/诊断/模拟器完整操作|DM-RT-015/019、DM-FE-034/035/036|status-history / diagnostics / simulation runs|
|latest键删除、实时订阅、历史导出|DM-RT-016/017/018、DM-FE-037/038/039|cacheVersion删除 / SSE / reading-exports|
|上述完整CLI|DM-CLI-016/017/018|同operationId，无CLI领域副本|

每行库存必须在发布时链接一个通过的TC用例或批准的不迁决定；不能仅用“改了共享wrapper”销项。尤其要逐一核验自动化、告警、市场、ThingsVis、Web/App看板、移动端、OTA及后台服务身份。只有wrapper无可达页面的产品/OTA入口先做调用方核实，不虚构当前页面，也不自动删功能。


## 2026-10-25 前端增量边界

- FEI-D016“按编号添加”原本禁用；用户2026-10-26确认社区版不支持企业版扫码/按编号领取激活，继续禁用，不再新增资格接口。
- FEI-D026覆盖独立子设备详情；FEI-X009覆盖本地看板模板兼容设备/实例；FEI-X012覆盖已有服务实例的跨页发现与配置；FEI-X015覆盖组合包绑定/重绑/重试/补偿。均映射现有owner和任务，不另建第二套业务逻辑。
- 服务发现读取唯一新增正式操作 `listServiceDiscoveredDevices`：`GET /api/v2/connection-providers/{providerId}/instances/{instanceId}/discovered-devices`，`device:discover`，owner为connector-runtime；其发现数据不迁为metadata事实表。
- 对Topic/debug分页、子设备换父可达性、告警路径的修正见前端附件§11；不能把页面wrapper的可选参数或未挂载业务分支当作用户已可用能力。
- 139条库存是审计条目数，不是139个已完成迁移功能。运行验收、数据迁移与发布必须独立登记；候选分支历史源码不等于当前checkout已切换。
