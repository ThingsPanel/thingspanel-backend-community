# 旧路由与原生任务追溯矩阵

共 165 个路由登记项（164项原登记 + 1项本轮新增服务发现），仅为盘点数量，绝非迁移完成度。路由事实继承 2026-10-23 的工作区审计；2026-10-25 已把全部登记项映射到现有稳定任务 ID，并校正新增用例后的任务归属。每行保留一个处置分类，主任务负责旧功能的目标闭环，附加任务提供合同、依赖或产品决定；它们不代表要保留该旧路径。

机器来源：[legacy-routes.json](legacy-routes.json)；实现和验收：[TASKS.md](TASKS.md)、[TEST-PLAN.md](TEST-PLAN.md)；行为/边界证据：[BACKEND-INVENTORY.md](BACKEND-INVENTORY.md)。当前代码重新核验范围以 [EVIDENCE.md](EVIDENCE.md) 为准，任务映射补齐不是运行验收通过。

|方法|旧路径|真实handler|证据|功能盘点ID|处置|主任务 / 附加任务|映射边界|
|---|---|---|---|---|---|---|---|
|POST|`/api/v1/device`|`DeviceApi.CreateDevice`|`router/apps/device.go:16`|B-D04|新系统计划完整实现|主任务 DM-DEV-003|—|
|DELETE|`/api/v1/device/:id`|`DeviceApi.DeleteDevice`|`router/apps/device.go:19`|B-D07|新系统计划完整实现|主任务 DM-DEV-005|—|
|PUT|`/api/v1/device`|`DeviceApi.UpdateDevice`|`router/apps/device.go:22`|B-D05/B-D06|新系统计划完整实现|主任务 DM-DEV-004；附加 DM-DEV-021、DM-DEV-025|—|
|PUT|`/api/v1/device/active`|`DeviceApi.ActiveDevice`|`router/apps/device.go:25`|B-D09|新系统计划完整实现|主任务 DM-DEV-007|—|
|GET|`/api/v1/device/detail/:id`|`DeviceApi.HandleDeviceByID`|`router/apps/device.go:28`|B-D03|新系统计划完整实现|主任务 DM-DEV-002|—|
|GET|`/api/v1/device/telemetry/evidence/:id`|`DeviceApi.BrokerTelemetryEvidence`|`router/apps/device.go:29`|B-D24|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/device`|`DeviceApi.HandleDeviceListByPage`|`router/apps/device.go:31`|B-D01/B-D02|新系统计划完整实现|主任务 DM-DEV-001|—|
|GET|`/api/v1/device/check/:deviceNumber`|`DeviceApi.CheckDeviceNumber`|`router/apps/device.go:34`|B-D10|新系统计划完整实现|主任务 DM-DEV-022；附加 DM-DEV-008|旧入口判断已存在且尚未激活；新增编号可用性是另一个正式用例，禁止合并语义。|
|GET|`/api/v1/device/tenant/list`|`DeviceApi.HandleTenantDeviceList`|`router/apps/device.go:37`|B-D11|新系统计划完整实现|主任务 DM-DEV-001|—|
|GET|`/api/v1/device/list`|`DeviceApi.HandleDeviceList`|`router/apps/device.go:40`|B-D12|新系统计划完整实现|主任务 DM-DEV-019|—|
|POST|`/api/v1/device/son/add`|`DeviceApi.CreateSonDevice`|`router/apps/device.go:43`|B-D13|新系统计划完整实现|主任务 DM-DEV-027；附加 DM-DEV-018、DM-DEV-019|—|
|GET|`/api/v1/device/connect/form`|`DeviceApi.DeviceConnectForm`|`router/apps/device.go:46`|B-D16|新系统计划完整实现|主任务 DM-TPL-015；附加 DM-DEV-009、DM-RT-003|—|
|GET|`/api/v1/device/connect/info`|`DeviceApi.DeviceConnect`|`router/apps/device.go:49`|B-D16|属于其他明确负责的运行时服务|主任务 DM-RT-003|—|
|POST|`/api/v1/device/update/voucher`|`DeviceApi.UpdateDeviceVoucher`|`router/apps/device.go:52`|B-D17|新系统计划完整实现|主任务 DM-DEV-010|—|
|GET|`/api/v1/device/sub-list/:id`|`DeviceApi.HandleSubList`|`router/apps/device.go:55`|B-D15|新系统计划完整实现|主任务 DM-DEV-019|—|
|PUT|`/api/v1/device/sub-remove`|`DeviceApi.RemoveSubDevice`|`router/apps/device.go:58`|B-D14|新系统计划完整实现|主任务 DM-DEV-018|—|
|GET|`/api/v1/device/metrics/:id`|`DeviceApi.HandleMetrics`|`router/apps/device.go:61`|B-D22|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/metrics/menu`|`DeviceApi.HandleActionByDeviceID`|`router/apps/device.go:64`|B-D22|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007；附加 DM-RT-008|—|
|GET|`/api/v1/device/metrics/condition/menu`|`DeviceApi.HandleConditionByDeviceID`|`router/apps/device.go:67`|B-D22|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007；附加 DM-RT-008|—|
|GET|`/api/v1/device/map/telemetry/:id`|`DeviceApi.HandleMapTelemetry`|`router/apps/device.go:70`|B-D23|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|PUT|`/api/v1/device/update/config`|`DeviceApi.UpdateDeviceConfig`|`router/apps/device.go:73`|B-D18|新系统计划完整实现|主任务 DM-DEV-017|—|
|GET|`/api/v1/device/online/status/:id`|`DeviceApi.HandleDeviceOnlineStatus`|`router/apps/device.go:76`|B-D24|属于其他明确负责的运行时服务|主任务 DM-RT-002|—|
|GET|`/api/v1/device/status/evidence/:id`|`DeviceApi.BrokerStatusEvidence`|`router/apps/device.go:77`|B-D24|属于其他明确负责的运行时服务|主任务 DM-RT-002|—|
|GET|`/api/v1/device/semantic/evidence/:id`|`DeviceApi.BrokerSemanticEvidence`|`router/apps/device.go:78`|B-D24|属于其他明确负责的运行时服务|主任务 DM-RT-006|—|
|POST|`/api/v1/device/:device_id/debug`|`DeviceDebugApi.SetDeviceDebug`|`router/apps/device.go:81`|B-D26|属于其他明确负责的运行时服务|主任务 DM-RT-004|—|
|GET|`/api/v1/device/:device_id/debug/status`|`DeviceDebugApi.GetDeviceDebugStatus`|`router/apps/device.go:82`|B-D26|属于其他明确负责的运行时服务|主任务 DM-RT-004|—|
|GET|`/api/v1/device/:device_id/debug/logs`|`DeviceDebugApi.GetDeviceDebugLogs`|`router/apps/device.go:83`|B-D26|属于其他明确负责的运行时服务|主任务 DM-RT-004|—|
|POST|`/api/v1/device/service/access/batch`|`DeviceApi.CreateDeviceBatch`|`router/apps/device.go:86`|B-D19|新系统计划完整实现|主任务 DM-DEV-012；附加 DM-DEV-024、DM-RT-001|—|
|GET|`/api/v1/device/metrics/chart`|`DeviceApi.HandleDeviceMetricsChart`|`router/apps/device.go:89`|B-D23|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/device/selector`|`DeviceApi.HandleDeviceSelector`|`router/apps/device.go:92`|B-D21|新系统计划完整实现|主任务 DM-DEV-001|—|
|GET|`/api/v1/device/telemetry/latest`|`DeviceApi.HandleTenantTelemetryData`|`router/apps/device.go:95`|B-D23|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/device/status/history`|`DeviceApi.GetDeviceStatusHistory`|`router/apps/device.go:98`|B-D25|属于其他明确负责的运行时服务|主任务 DM-RT-015；附加 DM-RT-002|—|
|POST|`/api/v1/device/topic-mappings`|`DeviceTopicMappingApi.CreateDeviceTopicMapping`|`router/apps/device.go:103`|B-T13|新系统计划完整实现|主任务 DM-TPL-017；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|GET|`/api/v1/device/topic-mappings`|`DeviceTopicMappingApi.GetDeviceTopicMappings`|`router/apps/device.go:104`|B-T13|新系统计划完整实现|主任务 DM-TPL-017；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|PUT|`/api/v1/device/topic-mappings/:id`|`DeviceTopicMappingApi.UpdateDeviceTopicMapping`|`router/apps/device.go:105`|B-T13|新系统计划完整实现|主任务 DM-TPL-017；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|DELETE|`/api/v1/device/topic-mappings/:id`|`DeviceTopicMappingApi.DeleteDeviceTopicMapping`|`router/apps/device.go:106`|B-T13|新系统计划完整实现|主任务 DM-TPL-017；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|POST|`/api/v1/device/template`|`DeviceApi.CreateDeviceTemplate`|`router/apps/device.go:114`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|DELETE|`/api/v1/device/template/:id`|`DeviceApi.DeleteDeviceTemplate`|`router/apps/device.go:117`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|PUT|`/api/v1/device/template`|`DeviceApi.UpdateDeviceTemplate`|`router/apps/device.go:120`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/template/detail/:id`|`DeviceApi.HandleDeviceTemplateById`|`router/apps/device.go:123`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/template`|`DeviceApi.HandleDeviceTemplateListByPage`|`router/apps/device.go:126`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/template/menu`|`DeviceApi.HandleDeviceTemplateMenu`|`router/apps/device.go:129`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/template/stats`|`DeviceApi.HandleDeviceTemplateStats`|`router/apps/device.go:132`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/template/selector`|`DeviceApi.HandleDeviceTemplateSelector`|`router/apps/device.go:135`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/template/chart`|`DeviceApi.HandleDeviceTemplateByDeviceId`|`router/apps/device.go:138`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007；附加 DM-TPL-016、DM-RT-011|—|
|GET|`/api/v1/device/template/chart/select`|`DeviceApi.HandleDeviceTemplateChartSelect`|`router/apps/device.go:141`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007；附加 DM-TPL-016、DM-RT-011|—|
|POST|`/api/v1/device/template/market/login`|`DeviceApi.MarketLogin`|`router/apps/device.go:147`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-002|保留市场owner的认证能力并调整正式消费关系；不在devicemetadata重建市场账号或认证模块。|
|POST|`/api/v1/device/template/market/refresh`|`DeviceApi.MarketRefresh`|`router/apps/device.go:148`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-002|保留市场owner的认证能力并调整正式消费关系；不在devicemetadata重建市场账号或认证模块。|
|POST|`/api/v1/device/template/market/publish`|`DeviceApi.PublishToMarket`|`router/apps/device.go:150`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/template/market/list`|`DeviceApi.ListMarketTemplates`|`router/apps/device.go:152`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/template/market/detail/:market_id`|`DeviceApi.GetMarketTemplateDetail`|`router/apps/device.go:154`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/template/market/install`|`DeviceApi.InstallFromMarket`|`router/apps/device.go:156`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/market/dashboard-bundles/analyze`|`DeviceApi.AnalyzeDashboardBundle`|`router/apps/device.go:164`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/market/dashboard-bundles`|`DeviceApi.PublishDashboardBundle`|`router/apps/device.go:166`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/market/bundles/publish-draft`|`DeviceApi.PublishBundleDraft`|`router/apps/device.go:168`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/market/bundles`|`DeviceApi.ListMarketBundles`|`router/apps/device.go:170`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/market/bundles/:bundleKey`|`DeviceApi.GetMarketBundleDetail`|`router/apps/device.go:171`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/market/bundles/:bundleKey/precheck`|`DeviceApi.GetMarketBundlePrecheck`|`router/apps/device.go:172`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/market/bundles/download`|`DeviceApi.DownloadDashboardTemplateFromMarket`|`router/apps/device.go:174`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/market/bundles/install`|`DeviceApi.InstallBundleFromMarket`|`router/apps/device.go:176`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/market/bundles/install/:id`|`DeviceApi.GetBundleInstallStatus`|`router/apps/device.go:178`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|PUT|`/api/v1/device/market/bundles/install/:id/bindings`|`DeviceApi.UpdateBundleBinding`|`router/apps/device.go:180`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/market/bundles/install/:id/retry`|`DeviceApi.RetryBundleInstall`|`router/apps/device.go:182`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|POST|`/api/v1/device/market/bundles/install/:id/compensate`|`DeviceApi.CompensateBundleInstall`|`router/apps/device.go:184`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/market/bundles/installations`|`DeviceApi.ListBundleInstallations`|`router/apps/device.go:186`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-010；附加 DM-API-007、DM-TPL-012、DM-TPL-013、DM-API-011|—|
|GET|`/api/v1/device/dashboard-templates`|`DeviceApi.ListLocalDashboardTemplates`|`router/apps/device.go:191`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-011；附加 DM-API-007、DM-API-011|—|
|GET|`/api/v1/device/dashboard-templates/:id/compatible-devices`|`DeviceApi.ListDashboardTemplateCompatibleDevices`|`router/apps/device.go:192`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-011；附加 DM-API-007、DM-API-011|—|
|POST|`/api/v1/device/dashboard-templates/:id/instances`|`DeviceApi.CreateDashboardFromLocalTemplate`|`router/apps/device.go:193`|§5 市场/看板消费者|新系统计划完整实现|主任务 DM-RT-011；附加 DM-API-007、DM-API-011|—|
|POST|`/api/v1/device/group`|`DeviceApi.CreateDeviceGroup`|`router/apps/device.go:200`|B-G01|新系统计划完整实现|主任务 DM-GRP-003|—|
|DELETE|`/api/v1/device/group/:id`|`DeviceApi.DeleteDeviceGroup`|`router/apps/device.go:203`|B-G03|新系统计划完整实现|主任务 DM-GRP-006|—|
|PUT|`/api/v1/device/group`|`DeviceApi.UpdateDeviceGroup`|`router/apps/device.go:206`|B-G02|新系统计划完整实现|主任务 DM-GRP-004；附加 DM-GRP-005|—|
|GET|`/api/v1/device/group`|`DeviceApi.HandleDeviceGroupByPage`|`router/apps/device.go:209`|B-G04|新系统计划完整实现|主任务 DM-GRP-001|—|
|GET|`/api/v1/device/group/tree`|`DeviceApi.HandleDeviceGroupByTree`|`router/apps/device.go:212`|B-G05|新系统计划完整实现|主任务 DM-GRP-002|—|
|GET|`/api/v1/device/group/counts`|`DeviceApi.HandleDeviceGroupCounts`|`router/apps/device.go:215`|B-G06|新系统计划完整实现|主任务 DM-DEV-020|—|
|GET|`/api/v1/device/group/detail/:id`|`DeviceApi.HandleDeviceGroupByDetail`|`router/apps/device.go:218`|B-G07|新系统计划完整实现|主任务 DM-GRP-001|—|
|POST|`/api/v1/device/group/relation`|`DeviceApi.CreateDeviceGroupRelation`|`router/apps/device.go:225`|B-G08|新系统计划完整实现|主任务 DM-GRP-008|—|
|DELETE|`/api/v1/device/group/relation`|`DeviceApi.DeleteDeviceGroupRelation`|`router/apps/device.go:227`|B-G09|新系统计划完整实现|主任务 DM-GRP-009|—|
|GET|`/api/v1/device/group/relation/list`|`DeviceApi.HandleDeviceGroupRelation`|`router/apps/device.go:229`|B-G10|新系统计划完整实现|主任务 DM-GRP-007|—|
|GET|`/api/v1/device/group/relation`|`DeviceApi.HandleDeviceGroupListByDeviceId`|`router/apps/device.go:231`|B-G11|新系统计划完整实现|主任务 DM-GRP-010|—|
|GET|`/api/v1/device/model/source/at/list`|`DeviceModelApi.HandleModelSourceAT`|`router/apps/device.go:238`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|POST|`/api/v1/device/model/telemetry`|`DeviceModelApi.CreateDeviceModelTelemetry`|`router/apps/device.go:241`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|DELETE|`/api/v1/device/model/telemetry/:id`|`DeviceModelApi.DeleteDeviceModelGeneral`|`router/apps/device.go:242`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|PUT|`/api/v1/device/model/telemetry`|`DeviceModelApi.UpdateDeviceModelGeneral`|`router/apps/device.go:243`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/model/telemetry`|`DeviceModelApi.HandleDeviceModelGeneral`|`router/apps/device.go:244`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|POST|`/api/v1/device/model/attributes`|`DeviceModelApi.CreateDeviceModelAttributes`|`router/apps/device.go:249`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|DELETE|`/api/v1/device/model/attributes/:id`|`DeviceModelApi.DeleteDeviceModelGeneral`|`router/apps/device.go:250`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|PUT|`/api/v1/device/model/attributes`|`DeviceModelApi.UpdateDeviceModelGeneral`|`router/apps/device.go:251`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/model/attributes`|`DeviceModelApi.HandleDeviceModelGeneral`|`router/apps/device.go:252`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|POST|`/api/v1/device/model/events`|`DeviceModelApi.CreateDeviceModelEvents`|`router/apps/device.go:257`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|DELETE|`/api/v1/device/model/events/:id`|`DeviceModelApi.DeleteDeviceModelGeneral`|`router/apps/device.go:258`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|PUT|`/api/v1/device/model/events`|`DeviceModelApi.UpdateDeviceModelGeneralV2`|`router/apps/device.go:259`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/model/events`|`DeviceModelApi.HandleDeviceModelGeneral`|`router/apps/device.go:260`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|POST|`/api/v1/device/model/commands`|`DeviceModelApi.CreateDeviceModelCommands`|`router/apps/device.go:265`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|DELETE|`/api/v1/device/model/commands/:id`|`DeviceModelApi.DeleteDeviceModelGeneral`|`router/apps/device.go:266`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|PUT|`/api/v1/device/model/commands`|`DeviceModelApi.UpdateDeviceModelGeneralV2`|`router/apps/device.go:267`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|GET|`/api/v1/device/model/commands`|`DeviceModelApi.HandleDeviceModelGeneral`|`router/apps/device.go:268`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007|—|
|POST|`/api/v1/device/model/custom/commands`|`DeviceModelApi.CreateDeviceModelCustomCommands`|`router/apps/device.go:274`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|DELETE|`/api/v1/device/model/custom/commands/:id`|`DeviceModelApi.DeleteDeviceModelCustomCommands`|`router/apps/device.go:275`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|PUT|`/api/v1/device/model/custom/commands`|`DeviceModelApi.UpdateDeviceModelCustomCommands`|`router/apps/device.go:276`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|GET|`/api/v1/device/model/custom/commands`|`DeviceModelApi.HandleDeviceModelCustomCommandsByPage`|`router/apps/device.go:277`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|GET|`/api/v1/device/model/custom/commands/:deviceId`|`DeviceModelApi.HandleDeviceModelCustomCommandsByDeviceId`|`router/apps/device.go:278`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|POST|`/api/v1/device/model/custom/control`|`DeviceModelApi.CreateDeviceModelCustomControl`|`router/apps/device.go:284`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|DELETE|`/api/v1/device/model/custom/control/:id`|`DeviceModelApi.DeleteDeviceModelCustomControl`|`router/apps/device.go:285`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|PUT|`/api/v1/device/model/custom/control`|`DeviceModelApi.UpdateDeviceModelCustomControl`|`router/apps/device.go:286`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|GET|`/api/v1/device/model/custom/control`|`DeviceModelApi.HandleDeviceModelCustomControl`|`router/apps/device.go:287`|B-T14/B-T15/B-T16|已有 Encore 代码可复用，但未完成验收|主任务 DM-DEC-001；附加 DM-API-007、DM-TPL-016、DM-MIG-005|该路由有旧实现，但custom控制/指令能否无损转换仍受DEC-001发布阻断；不得把普通ThingModel核心复用等同于本项已可迁。|
|POST|`/api/v1/device_config`|`DeviceConfigApi.CreateDeviceConfig`|`router/apps/device_config.go:16`|B-T04|新系统计划完整实现|主任务 DM-TPL-003|—|
|DELETE|`/api/v1/device_config/:id`|`DeviceConfigApi.DeleteDeviceConfig`|`router/apps/device_config.go:19`|B-T08|新系统计划完整实现|主任务 DM-TPL-009|—|
|PUT|`/api/v1/device_config`|`DeviceConfigApi.UpdateDeviceConfig`|`router/apps/device_config.go:22`|B-T05/B-T06/B-T07|新系统计划完整实现|主任务 DM-TPL-004；附加 DM-TPL-007|—|
|GET|`/api/v1/device_config`|`DeviceConfigApi.HandleDeviceConfigListByPage`|`router/apps/device_config.go:25`|B-T01|新系统计划完整实现|主任务 DM-TPL-001|—|
|GET|`/api/v1/device_config/menu`|`DeviceConfigApi.HandleDeviceConfigListMenu`|`router/apps/device_config.go:28`|B-T02|新系统计划完整实现|主任务 DM-TPL-001|—|
|GET|`/api/v1/device_config/:id`|`DeviceConfigApi.HandleDeviceConfigById`|`router/apps/device_config.go:31`|B-T03|新系统计划完整实现|主任务 DM-TPL-002|—|
|PUT|`/api/v1/device_config/batch`|`DeviceConfigApi.BatchUpdateDeviceConfig`|`router/apps/device_config.go:34`|B-T09|新系统计划完整实现|主任务 DM-DEV-023；附加 DM-DEV-016、DM-DEV-017、DM-API-008|—|
|GET|`/api/v1/device_config/connect`|`DeviceConfigApi.HandleDeviceConfigConnect`|`router/apps/device_config.go:37`|B-D16|新系统计划完整实现|主任务 DM-TPL-015|—|
|GET|`/api/v1/device_config/voucher_type`|`DeviceConfigApi.HandleVoucherType`|`router/apps/device_config.go:40`|B-D16|新系统计划完整实现|主任务 DM-TPL-015|—|
|GET|`/api/v1/device_config/metrics/menu`|`DeviceConfigApi.HandleActionByDeviceConfigID`|`router/apps/device_config.go:43`|B-T11|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007；附加 DM-RT-008|—|
|GET|`/api/v1/device_config/metrics/condition/menu`|`DeviceConfigApi.HandleConditionByDeviceConfigID`|`router/apps/device_config.go:46`|B-T11|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007；附加 DM-RT-008|—|
|POST|`/api/v1/data_script`|`DataScriptApi.CreateDataScript`|`router/apps/data_script.go:16`|B-T12|新系统计划完整实现|主任务 DM-TPL-018；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|DELETE|`/api/v1/data_script/:id`|`DataScriptApi.DeleteDataScript`|`router/apps/data_script.go:19`|B-T12|新系统计划完整实现|主任务 DM-TPL-018；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|PUT|`/api/v1/data_script`|`DataScriptApi.UpdateDataScript`|`router/apps/data_script.go:22`|B-T12|新系统计划完整实现|主任务 DM-TPL-018；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|GET|`/api/v1/data_script`|`DataScriptApi.HandleDataScriptListByPage`|`router/apps/data_script.go:25`|B-T12|新系统计划完整实现|主任务 DM-TPL-018；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|POST|`/api/v1/data_script/quiz`|`DataScriptApi.QuizDataScript`|`router/apps/data_script.go:28`|B-T12|属于其他明确负责的运行时服务|主任务 DM-TPL-018；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|测试执行归connector-runtime；配置仍由同模板草稿/版本保存，不新建脚本引擎。|
|PUT|`/api/v1/data_script/enable`|`DataScriptApi.EnableDataScript`|`router/apps/data_script.go:31`|B-T12|新系统计划完整实现|主任务 DM-TPL-018；附加 DM-TPL-004、DM-TPL-005、DM-RT-007|—|
|GET|`/api/v1/telemetry/datas/current/:id`|`TelemetryDataApi.HandleCurrentData`|`router/apps/telemetry_data.go:16`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/current/keys`|`TelemetryDataApi.HandleCurrentDataKeys`|`router/apps/telemetry_data.go:19`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/current/detail/:id`|`TelemetryDataApi.ServeCurrentDetailData`|`router/apps/telemetry_data.go:22`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/history`|`TelemetryDataApi.ServeHistoryData`|`router/apps/telemetry_data.go:25`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/history/pagination`|`TelemetryDataApi.ServeHistoryDataByPage`|`router/apps/telemetry_data.go:28`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/history/page`|`TelemetryDataApi.ServeHistoryDataByPage`|`router/apps/telemetry_data.go:30`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|DELETE|`/api/v1/telemetry/datas`|`TelemetryDataApi.DeleteData`|`router/apps/telemetry_data.go:33`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-016；附加 DM-RT-005、DM-API-005|—|
|GET|`/api/v1/telemetry/datas/statistic`|`TelemetryDataApi.ServeStatisticData`|`router/apps/telemetry_data.go:36`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/statistic/batch`|`TelemetryDataApi.ServeStatisticDataByDeviceId`|`router/apps/telemetry_data.go:39`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/set/logs`|`TelemetryDataApi.ServeSetLogsDataListByPage`|`router/apps/telemetry_data.go:42`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-014；附加 DM-RT-006|—|
|POST|`/api/v1/telemetry/datas/pub`|`TelemetryDataApi.TelemetryPutMessage`|`router/apps/telemetry_data.go:45`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-006；附加 DM-RT-014、DM-DEC-001|目标只能执行已声明COMMAND；无法无损表达的旧telemetry/attribute控制先由DEC-001处置，不保留通用旧下发入口。|
|GET|`/api/v1/telemetry/datas/simulation`|`TelemetryDataApi.ServeEchoData`|`router/apps/telemetry_data.go:48`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-019；附加 DM-RT-015、DM-RT-005|—|
|POST|`/api/v1/telemetry/datas/simulation`|`TelemetryDataApi.SimulationTelemetryData`|`router/apps/telemetry_data.go:51`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-019；附加 DM-RT-015、DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/simulation/init`|`TelemetryDataApi.GetSimulationInit`|`router/apps/telemetry_data.go:54`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-019；附加 DM-RT-015、DM-RT-005|—|
|POST|`/api/v1/telemetry/datas/simulation/send`|`TelemetryDataApi.SimulationSend`|`router/apps/telemetry_data.go:57`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-019；附加 DM-RT-015、DM-RT-005|—|
|GET|`/api/v1/telemetry/datas/msg/count`|`TelemetryDataApi.ServeMsgCountByTenant`|`router/apps/telemetry_data.go:60`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-015；附加 DM-RT-005|—|
|GET|`/api/v1/attribute/datas/:id`|`AttributeDataApi.HandleDataList`|`router/apps/attribute_data.go:15`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/attribute/datas/set/logs`|`AttributeDataApi.HandleAttributeSetLogsDataListByPage`|`router/apps/attribute_data.go:18`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-014；附加 DM-RT-006|—|
|DELETE|`/api/v1/attribute/datas/:id`|`AttributeDataApi.DeleteData`|`router/apps/attribute_data.go:21`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-016；附加 DM-RT-005、DM-API-005|—|
|POST|`/api/v1/attribute/datas/pub`|`AttributeDataApi.AttributePutMessage`|`router/apps/attribute_data.go:24`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-006；附加 DM-RT-014、DM-DEC-001|目标只能执行已声明COMMAND；无法无损表达的旧telemetry/attribute控制先由DEC-001处置，不保留通用旧下发入口。|
|GET|`/api/v1/attribute/datas/get`|`AttributeDataApi.AttributeGetMessage`|`router/apps/attribute_data.go:27`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|GET|`/api/v1/attribute/datas/key`|`AttributeDataApi.HandleAttributeDataByKey`|`router/apps/attribute_data.go:30`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|POST|`/api/v1/command/datas/plans/preview`|`CommandPlanApi.Preview`|`router/apps/command_data.go:15`|§5 命令执行/计划/回执|属于其他明确负责的运行时服务|主任务 DM-RT-006|—|
|POST|`/api/v1/command/datas/plans/:plan_id/authorize`|`CommandPlanApi.Authorize`|`router/apps/command_data.go:16`|§5 命令执行/计划/回执|属于其他明确负责的运行时服务|主任务 DM-RT-006|—|
|POST|`/api/v1/command/datas/plans/:plan_id/submit`|`CommandPlanApi.Submit`|`router/apps/command_data.go:17`|§5 命令执行/计划/回执|属于其他明确负责的运行时服务|主任务 DM-RT-006|—|
|GET|`/api/v1/command/datas/set/logs`|`CommandSetLogApi.ServeSetLogsDataListByPage`|`router/apps/command_data.go:20`|§5 命令执行/计划/回执|属于其他明确负责的运行时服务|主任务 DM-RT-014；附加 DM-RT-006|—|
|POST|`/api/v1/command/datas/pub`|`CommandSetLogApi.CommandPutMessage`|`router/apps/command_data.go:23`|§5 命令执行/计划/回执|属于其他明确负责的运行时服务|主任务 DM-RT-006|—|
|GET|`/api/v1/command/datas/status/:message_id`|`CommandSetLogApi.GetCommandStatus`|`router/apps/command_data.go:26`|§5 命令执行/计划/回执|属于其他明确负责的运行时服务|主任务 DM-RT-006|—|
|GET|`/api/v1/command/datas/:id`|`CommandSetLogApi.HandleCommandList`|`router/apps/command_data.go:29`|§5 命令执行/计划/回执|属于其他明确负责的运行时服务|主任务 DM-RT-006|—|
|GET|`/api/v1/event/datas`|`EventDataApi.HandleEventDatasListByPage`|`router/apps/event_data.go:14`|§5 遥测/属性/事件/模拟|属于其他明确负责的运行时服务|主任务 DM-RT-005|—|
|POST|`/api/v1/device/auth`|`DeviceAuthApi.DeviceAuth`|`router/router_init.go:137`|B-D28|新系统计划完整实现|主任务 DM-RT-013；附加 DM-TPL-020、DM-DEV-003、DM-API-002|—|
|POST|`/api/v1/device/gateway-register`|`DeviceApi.GatewayRegister`|`router/router_init.go:131`|B-D29|新系统计划完整实现|主任务 DM-RT-013；附加 DM-TPL-020、DM-DEV-003、DM-API-002|—|
|POST|`/api/v1/device/gateway-sub-register`|`DeviceApi.GatewaySubRegister`|`router/router_init.go:133`|B-D29|新系统计划完整实现|主任务 DM-RT-013；附加 DM-TPL-020、DM-DEV-003、DM-API-002|—|
|GET|`/api/v1/devices/:device_id/diagnostics`|`DeviceApi.GetDeviceDiagnostics`|`router/router_init.go:139`|B-D27|属于其他明确负责的运行时服务|主任务 DM-RT-015；附加 DM-API-003|—|
|GET|`/api/v1/telemetry/datas/current/ws`|`TelemetryDataApi.ServeCurrentDataByWS`|`router/router_init.go:108`|B-D25|属于其他明确负责的运行时服务|主任务 DM-RT-017；附加 DM-RT-002、DM-RT-005、DM-API-003|—|
|GET|`/api/v1/device/online/status/ws`|`TelemetryDataApi.ServeDeviceStatusByWS`|`router/router_init.go:110`|B-D25|属于其他明确负责的运行时服务|主任务 DM-RT-017；附加 DM-RT-002、DM-RT-005、DM-API-003|—|
|GET|`/api/v1/device/online/status/ws/batch`|`TelemetryDataApi.ServeDeviceOnlineStatusWS`|`router/router_init.go:112`|B-D25|属于其他明确负责的运行时服务|主任务 DM-RT-017；附加 DM-RT-002、DM-RT-005、DM-API-003|—|
|GET|`/api/v1/telemetry/datas/current/keys/ws`|`TelemetryDataApi.ServeCurrentDataByKey`|`router/router_init.go:114`|B-D25|属于其他明确负责的运行时服务|主任务 DM-RT-017；附加 DM-RT-002、DM-RT-005、DM-API-003|—|
|POST|`/api/v1/plugin/device/config`|`HandleDeviceConfigForProtocolPlugin`|`router/router_init.go:99`|§5 协议插件取配置|新系统计划完整实现|主任务 DM-RT-001；附加 DM-RT-007、DM-RT-012|—|
|POST|`/api/v1/plugin/devices`|`HandleDeviceConfigForProtocolPluginByProtocolType`|`router/router_init.go:100`|§5 协议插件取配置|新系统计划完整实现|主任务 DM-RT-001；附加 DM-RT-007、DM-RT-012|—|
|POST|`/api/v1/plugin/service/access/list`|`HandlePluginServiceAccessList`|`router/router_init.go:101`|§5 服务插件发现/导入|新系统计划完整实现|主任务 DM-RT-001；附加 DM-TPL-015、DM-DEV-024、DM-DEV-012|—|
|POST|`/api/v1/plugin/service/access`|`HandlePluginServiceAccess`|`router/router_init.go:102`|§5 服务插件发现/导入|新系统计划完整实现|主任务 DM-RT-001；附加 DM-TPL-015、DM-DEV-024、DM-DEV-012|—|
|GET|`/api/v1/service/access/device/list`|`ServiceAccessApi.HandleDeviceList`|`internal/api/service_access.go:84-96; internal/service/service_access.go:159-193`|B-X-DISCOVERY|属于其他明确负责的运行时服务|主任务 DM-RT-001；附加 DM-DEV-024、DM-TPL-015、DM-CLI-017|2026-10-25新增盘点；F plugin.ts:52-56确认真实GET。候选externalId与已绑定状态归connector-runtime，正式listServiceDiscoveredDevices；只读发现不自动登记设备、不返回voucher。|
