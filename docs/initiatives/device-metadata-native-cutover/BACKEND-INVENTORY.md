# Community 后端真实源码盘点（证据附件）

初始审计：2026-10-23；本轮源码复核与任务追溯修订：2026-10-25。远端根为 `/Users/junhong/Downloads/code/thingspanel-backend-community`，本轮分支 main、HEAD `0e3ba8281f09c815dabcd03c634d17af845e950b`。本轮依据从指定工作区只读复制并经 SHA256 校验的 `source-current/B` 核查下列关键源码；实际变更和文件清单见 [EVIDENCE.md](EVIDENCE.md)。本文路径相对远端仓库根。初始审计结论与本轮复核范围分开记录；没有运行应用、业务测试、迁移或查询生产数据库。

**状态口径：以下都是旧 Community 功能/行为证据，不表示 Encore 新接口完成，不表示页面已切换。** `internal/service/device.go`、`router/apps/device.go`、`router/router_init.go`、`internal/middleware/jwt_auth.go`、`internal/middleware/casbin_middle.go` 在初始 Git 基线中为已跟踪修改；`pkg/authz/community_roles.go`、多份 broker evidence 及测试为未跟踪。其余精确逐文件 Git 状态以本轮 Git 基线为准；不可把副本当作 HEAD 原样内容。

## 1. 已核实的领域与实际框架

|对象|实际代码与字段|结论/目标边界|
|---|---|---|
|后端框架|`go.mod:8-33`：Gin v1.10.0，GORM v1.25.7、gorm/gen v0.3.25、Postgres driver v1.5.6，Casbin v2.82.0；`router/router_init.go:28` 返回 `*gin.Engine`|历史实现为 Go/Gin + GORM/Postgres + Casbin；不能写 GNQ。Broker 实际代码/运行部署需另述，不能由框架或目录名推断部署。|
|旧菜单接入模板|`internal/model/device_configs.gen.go:14-32`；`router/apps/device_config.go:13-49`；`sql/1.sql:909-926`|`device_configs` 含名称、可空 `device_template_id`、设备类别、协议、凭据类型、协议 JSON、连接方向、附加/其他配置、租户、模板密钥/自动注册、图片；目标是 DeviceAccessTemplate，不是物模型。|
|旧物模型容器|`internal/model/device_templates.gen.go:14-31`；`router/apps/device.go:110-141`；`internal/service/device_template.go:17-190`|`device_templates` 含 author/version/description/flag/label、Web/App chart JSON、图片 path、type_key/brand/model_number。字段/命令存于下列子表。不能迁成新接入模板而丢失语义。|
|旧物模型字段|`internal/model/device_model_telemetry.gen.go:14-27`、`device_model_attributes.gen.go:14-27`、`device_model_commands.gen.go:14-25`、`device_model_events.gen.go:14-25`|均以 `device_template_id` 引用旧物模型，遥测/属性含identifier/readWrite/type/unit/additional_info；命令/事件含 params JSON。复用已完成 Encore ThingModel owner 的迁移与发布快照，不新增第二核心。|
|旧定制控制|`internal/model/device_model_custom_control.gen.go:14-25`、`device_model_custom_commands.gen.go:10-19`；`router/apps/device.go:271-287`|custom commands保存按钮名、标识符、instruct；custom control保存control_type/content/enable_status；应明确转换到已有COMMAND/展示资源快照或逐项报告不支持项，不能无声丢弃。|
|产品|`internal/model/products.gen.go:14-27`、`sql/1.sql:1203-1217`；`internal/service/device_auth.go:54-66`，`internal/dal/device_auth.go:27-36`|确有 products：name/type/key/model/image/description/nullable tenant/device_config_id；用于可选 product_key 动态注册关联。它既不是物模型，也不是接入模板，不能无证据只称纯商业目录；是否迁 product_key 引导能力需按注册身份方案处置。|
|设备身份/元数据|`internal/model/devices.gen.go:14-52`、`sql/1.sql:1389-1420`|device_number旧全局唯一；id允许用户指定8–36字符；name/label/location/additional_info/product/parent/config/reference。新UUID与编号规则须有持久ID映射；运行身份不能因重编ID失联。|
|接入方式与连接方向|`devices.gen.go:49`、`device_configs.gen.go:18-22`、`device_configs.http.go:5-16`|三个正交维度：设备类型1直连/2网关/3子设备；access_way A=协议/B=服务；device_conn_type A=设备连接平台/B=平台连接设备。不得混成一个 A/B 枚举。|
|凭据和注册|`device_configs.gen.go:30-31`、`internal/service/device_auth.go:19-162`、`internal/service/device.go:1112-1190`|模板引导密钥/auto_register 与设备 voucher 是不同生命周期；凭据材料归秘密存储，元数据保存引用/状态；注册必须有原生正式用例而非只有开关字段。|
|运行观察|`devices.gen.go:30,48,52`；`internal/service/device.go:1961-1990,2284-2295`|is_online/last_offline_time/current_version/状态历史和telemetry timestamps不是设备元数据事实。由runtime/OTA/telemetry owner提供正式读合同。|
|设备分组|`groups.gen.go:14-23`、`r_group_device.gen.go:10-13`；`sql/1.sql:1515-1522`|树组织、多对多成员；旧root有NULL与字符串0；tier不可信（创建写-1）；迁移按父关系重算。分组不自动授予设备权限。|

## 2. 设备菜单、关系与注册行为清单

统一迁移分类取用户规定五类之一；“新系统计划完整实现”指新的业务功能，不保留下列旧路径。

|ID|旧功能/入口|实际实现与边界证据|处置|任务映射与必须验证的差异|
|---|---|---|---|---|
|B-D01|GET `/api/v1/device` 列表|`router/apps/device.go:31`；`service/device.go:609-650`；`dal/devices.go:295-466`，默认只active，SYS_ADMIN可跨租户读|新系统计划完整实现|DM-DEV-001、DM-API-003；授权先于列表/total，管理员跨租户行为须明确可信委派。|
|B-D02|列表所有过滤|`model/devices.http.go:67-93`：编号、名称/模糊search、启用、产品、协议、label、固件、分组及后代/ungrouped、接入模板/物模型、online、告警、接入方式、批次、deviceType、serviceIdentifier/serviceAccessID|新系统计划完整实现|DM-DEV-001；新合同逐项有字段或明确owner；状态/告警过滤必须分页前求交，不能删去原字段却标完成。|
|B-D03|GET `/device/detail/:id`|`api/device.go:99-110`；`service/device.go:542-607`，明细通过claims检查tenant/sysadmin并组合接入信息|新系统计划完整实现|DM-DEV-002；无模板/模型设备可读；runtime不可用不伪造offline。|
|B-D04|POST `/device`|`service/device.go:37-135`：可指定ID；未给编号默认设备ID；无模板也可创建；自动生成voucher；默认active/enabled|新系统计划完整实现|DM-DEV-003；新默认inactive是行为变化，应明确用户激活流程；迁移保留旧激活状态；不把默认创建与设备首次连接混为一谈。|
|B-D05|PUT `/device` 展示/接入字段更新|`service/device.go:266-382`、`model/devices.http.go:40-63`；校验设备tenant，广泛更新product/parent/config/protocolConfig/当前固件/is_online等|新系统计划完整实现|DM-DEV-004、关系专用用例；runtime字段禁止客户端伪写；product更换必须有明确入口，不能只在create支持而丢失旧修改能力。|
|B-D06|修改设备编号|`service/device.go:307-347`：校验系统唯一；非MQTT时通知断连|新系统计划完整实现|主任务 DM-DEV-021，附加 DM-DEV-004/DM-DB-003；同租户唯一、旧运行标识切换、并发冲突、服务外部编号不可误改。不能无确认删功能。|
|B-D07|DELETE `/device/:id`|`service/device.go:424-540`：有子设备/自动化条件拒删，随后同事务删当前遥测/历史/下发日志/属性/事件再删设备|新系统计划完整实现|DM-DEV-005；新生命周期必须与历史保留分离；跨租户失败绝不能在拒绝前删除历史。旧清理不能照搬进metadata owner。|
|B-D08|启用/停用|设备有is_enabled且broker/control检查；本轮未找到独立菜单设备启停handler，UpdateDeviceReq也没有is_enabled|新系统计划完整实现|DM-DEV-006；属于本次正式要求的新用例，不写“旧完整CRUD已存在”。启停与online/activation分开，必须确认runtime拒接/恢复。|
|B-D09|PUT `/device/active`|`api/device.go:82-96` 未传claims；`service/device.go:384-422`以编号查设备，非active变active/enabled，写activate_at|新系统计划完整实现|DM-DEV-007；租户来自可信上下文，重复激活应幂等而不重置凭据；不要继承仅编号可操作范围。|
|B-D10|GET `/device/check/:deviceNumber`|`service/device.go:656-675`：不存在false、已激活false、存在未激活true|新系统计划完整实现|主任务 DM-DEV-022；DM-DEV-008 单独实现“新增编号可用性”，与“预登记激活资格”分开；同名旧接口不是唯一性检查。|
|B-D11|GET `/device/tenant/list`|`service/device.go:762-807`：groupId、deviceName、bindConfig；无组按租户选择，有组按关联选择|新系统计划完整实现|DM-DEV-001/019；自动化、看板选择器仍需要绑定/未绑定过滤和明确可用类别。|
|B-D12|GET `/device/list` 可绑定候选|`service/device.go:809-818`，`dal/devices.go:562-594`：网关未关联设备列表，tenant/search/type|新系统计划完整实现|DM-DEV-019；不得返回跨租户、已绑定或协议不兼容候选。|
|B-D13|POST `/device/son/add` 批绑定|`service/device.go:820-870`：逗号分割sonIDs；接受device_type 2或3，支持网关下网关；要求原parent为空及有config；设parent/address，通知插件|新系统计划完整实现|主任务 DM-DEV-027，附加 DM-DEV-018/019；必须支持多级网关，防环、深度、地址唯一及parent/child权限；整批预检、按项持久结果，不声称跨runtime全批原子。不能只允许child=subdevice。|
|B-D14|PUT `/device/sub-remove`|`service/device.go:693-717`；`dal/devices.go:284-293`清parent/address，通知旧父断连|新系统计划完整实现|DM-DEV-018；解绑后的网关/子设备是合法未绑定管理状态；控制不可向不确定路由发送。|
|B-D15|GET `/device/sub-list/:id`|`service/device.go:1193-1203`，`dal/devices.go:670-687`，分页/tenant/parent|新系统计划完整实现|DM-DEV-019；parent未授权与跨租户请求404；成员去重/统计一致。|
|B-D16|GET `/device/connect/form`、`/connect/info`、`/device_config/connect`、`/voucher_type`|`service/device.go:873-1050`；`service/device_config.go:266-334`，MQTT ACCESSTOKEN/BASIC/其他协议动态表单/连接说明|新系统计划完整实现|DM-DEV-009、DM-RT-003、DM-TPL-015；元数据声明凭据策略和provider schema；runtime返回endpoint/脱敏连接信息；secret独立授权揭示。|
|B-D17|POST `/device/update/voucher`|`service/device.go:1112-1190`校验voucher全局唯一、更新明文JSON、删除redis鉴权缓存、通知非MQTT断连|新系统计划完整实现|DM-DEV-009/010/011；secret引用存储，轮换/撤销可恢复、runtime确认、不得以只改数据库视为完成。|
|B-D18|PUT `/device/update/config`|`service/device.go:1051-1110`：网关有子拒切；已绑定子设备拒切；空字符串解绑；仅改device_config_id/缓存|新系统计划完整实现|DM-DEV-016/017；预览必须涵盖模型快照、协议、脚本、凭据、网关、规则/看板；目标revision固定、CAS+runtime确认。|
|B-D19|POST `/device/service/access/batch`|`service/device.go:139-255`验证serviceAccess归属、可无config、外部号按tenant/access做svc-hash、重复跳过、DB后通知插件|新系统计划完整实现|主任务 DM-DEV-012，附加 DM-DEV-024、DM-RT-001、DM-DEV-013/014；API必须可传正式serviceAccess/provider实例引用、外部设备标识与每设备参数；“仅CreateDevice数组”不足以覆盖。|
|B-D20|预登记列表/凭据导出服务函数|`service/device.go:678-691,719-759`，`model/devices.http.go:173-214`；导出batch/voucher/number到本地xlsx，但本轮路由扫描未见绑定handler|需要业务方确认|DM-DEV-011/015；普通导出明确排除secret。历史含凭据批量交付是否仍需专门受审计交付流程，不能仅因service存在宣称页面功能可用。|
|B-D21|GET `/device/selector`|`service/device.go:2253-2261`；`dal/devices.go:831-875`|新系统计划完整实现|DM-DEV-001；筛选、分页、授权与主列表共享同一用例，不另建平行选择器事实源。|
|B-D22|GET `/device/metrics/:id`、`metrics/menu`、`metrics/condition/menu`|`service/device.go:1206-1431,1433-1660,1662-1856`|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007，附加 DM-RT-008/011；字段/属性/事件/COMMAND来自同一已发布固定快照；空模型不能SQL回退。ThingModel候选代码证据见 ENCORE-INVENTORY，必须再验新组合。|
|B-D23|地图telemetry、指标图表、最近三设备遥测|`service/device.go:1858-1947,2110-2251,2263-2282`|属于其他明确负责的运行时服务|DM-RT-005；telemetry owner保留历史/聚合能力、有效时间区间与模型快照语义；列表metadata不复制历史。|
|B-D24|online/status、status/evidence、telemetry/evidence、semantic/evidence|`router/apps/device.go:76-83`；`service/device.go:1961-1990`；`internal/api/device_broker_evidence.go`、`device_telemetry_evidence.go`、`device_broker_semantic.go`|属于其他明确负责的运行时服务|DM-RT-002/005/006；证据页需区分观察时间/unknown与缺数据；已有未跟踪tests存在≠本轮跑过。|
|B-D25|状态历史与实时订阅|`router/apps/device.go:98`；`service/device.go:2284-2295`；`router/router_init.go:108-114`有旧单路/batch WS|属于其他明确负责的运行时服务|主任务 DM-RT-015（历史）、DM-RT-017（实时），附加 DM-RT-002/005；目标正式API/SSE需认证、订阅范围、重连/撤权测试；不能保留旧WS作为“只剩实时没迁”。|
|B-D26|设备调试开关/状态/日志|`router/apps/device.go:81-83`；`internal/service/device_debug.go:35-229` tenant校验、限时开关、Redis日志|属于其他明确负责的运行时服务|DM-RT-004；日志限量/TTL/脱敏/越权、会话关闭、runtime不可用错误；不建metadata日志表。|
|B-D27|GET `/devices/:device_id/diagnostics`|`router/router_init.go:138-139` 位于JWT前；`api/device.go:919-945`|属于其他明确负责的运行时服务|主任务 DM-RT-015，附加 DM-API-003；新接口必须受资源级诊断权限保护，不继承匿名诊断。|
|B-D28|POST `/device/auth` 动态注册|`router/router_init.go:137`；`service/device_auth.go:19-162`；`dal/device_auth.go:15-36`|新系统计划完整实现|主任务 DM-RT-013，附加 DM-TPL-020、DM-DEV-003、DM-API-002；正式enrollment用例：凭可信注册凭证识别tenant+模板revision；autoRegister/配额/设备号/父关系/产品关联；不让客户端指定tenant。|
|B-D29|POST `/device/gateway-register`、`/gateway-sub-register`|`router/router_init.go:131-133`；`service/device.go:1992-2108`，直接注册读取req.TenantId；子注册继承父tenant|新系统计划完整实现|主任务 DM-RT-013，附加 DM-DEV-018；合并到正式enrollment用例及gateway关系；旧URL停止；同引导凭证重复提交只得一个设备，不回显既有其他设备secret；禁止租户输入信任。|

## 3. 接入模板菜单清单

|ID|旧功能/入口|真实证据|处置|任务/验收要点|
|---|---|---|---|---|
|B-T01|GET `/device_config` 分页/筛选及设备数|`dal/device_config.go:96-145`：tenant/name/deviceType/protocol/thingmodel；按模板统计active设备，createdAt排序|新系统计划完整实现|DM-TPL-001；设备数按授权范围、避免N+1；空数组和排序稳定。|
|B-T02|GET `/device_config/menu`|`service/device_config.go:221-229`；`dal/device_config.go:147-168`|新系统计划完整实现|DM-TPL-001；与主列表同事实源/授权，停用与无发布版能见但不可新绑定。|
|B-T03|GET `/device_config/:id`|`api/device_config.go:61-70`；`service/device_config.go:192-202`按ID读，无claims参数；返回完整model含secret|新系统计划完整实现|DM-TPL-002、DM-API-003；新详情强制tenant/资源授权，凭据引导secret不得普通读。|
|B-T04|POST `/device_config`|`service/device_config.go:26-72`：默认MQTT、ACCESSTOKEN、随机template_secret；JSON只检查语法；claims tenant|新系统计划完整实现|DM-TPL-003；配置按冻结provider schema验证，model可空；创建草稿与发布分开。|
|B-T05|PUT `/device_config`|`service/device_config.go:74-162`、`model/device_configs.http.go:19-36`|新系统计划完整实现|DM-TPL-004/005/007；旧直接修改影响运行；新草稿不改变设备固定revision，发布后显式选择切换。|
|B-T06|other_config 在线判断|`device_configs.http.go:91-94` online_timeout以分钟、heartbeat以秒；`service/device_config.go:99-113`两者同时非0拒绝|新系统计划完整实现|DM-TPL-007、DM-RT-002；迁移单位需60倍转换，零/空值含义明确，心跳/活动/transport互斥策略由runtime执行。|
|B-T07|协议/凭据类型更换|`service/device_config.go:128-159`：非MQTT配置变化断连；改MQTT设ACCESSTOKEN，否则清voucher_type|新系统计划完整实现|DM-TPL-004/007、DM-DEV-016/017；新切换不得自动重置凭据，先明确兼容影响和每设备应用结果。|
|B-T08|DELETE `/device_config/:id`|`service/device_config.go:164-190`先检查设备引用；DB还被products/OTA/scripts引用|新系统计划完整实现|DM-TPL-009/010；应用删除冲突列明设备、产品、OTA、规则、市场、脚本引用，不回裸SQL错误；并发新增引用需fence。|
|B-T09|PUT `/device_config/batch` 批量绑定|`service/device_config.go:231-264`检查原未绑定后逐个写；失败可能已有部分提交|新系统计划完整实现|主任务 DM-DEV-023，附加 DM-DEV-016/017、DM-API-008；复用同binding用例，整批预检、逐项持久状态与稳定itemKey，重试不重复绑定。|
|B-T10|auto_register与模板secret编辑|`device_configs.http.go:33-34`；`service/device_auth.go:19-35`|新系统计划完整实现|主任务 DM-TPL-020，附加 DM-RT-013、DM-DEV-010；注册凭证轮换/撤销、审计、注册配额/重放；单一owner管理，普通模板PUT不得任意注入密钥。|
|B-T11|模板自动化动作/条件选择器|`router/apps/device_config.go:43-46`；`service/device_config.go:336-456,457+`|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007，附加 DM-RT-008；绑定已发布模型快照，选择模板下设备动作扩散仍由规则owner，metadata仅查授权对象。|
|B-T12|数据转换脚本CRUD/启停/调试|`router/apps/data_script.go:16-31`；`service/data_script.go:38-124`；`sql/1.sql:1350-1364`|新系统计划完整实现|主任务 DM-TPL-018，附加 DM-TPL-004/005、DM-RT-007；脚本内容与版本可重现，执行/隔离/调试归connector runtime；不要第二脚本引擎。|
|B-T13|Topic映射CRUD|`router/apps/device.go:101-106`；`service/device_topic_mapping.go:25-296`；`sql/12.sql:27-43`|新系统计划完整实现|主任务 DM-TPL-017，附加 DM-TPL-004/005、DM-RT-007；direction/source/target/priority/enabled/dataIdentifier，引用固定发布版本；匹配执行归broker/connector runtime，防跨租户更换config。|
|B-T14|物模型字段/命令/图表选择|`service/device_template.go:54-121`、`device_model.go:30-197,414-461`、`router/apps/device.go:235-287`|已有 Encore 代码可复用，但未完成验收|DM-API-007；旧编辑器所有字段和Web/App图表映到现有ThingModel snapshot，不能在接入模板里再建fields/charts表。|
|B-T15|物模型列表统计/选择器|`router/apps/device.go:129-135`；`service/device_template.go:192-243`|已有 Encore 代码可复用，但未完成验收|DM-API-007、模板前端；引用选择器调用正式模型API，物模型迁移完成与接入模板迁移分开计算。|
|B-T16|GET `/device/template/chart`、`chart/select`|`service/device_template.go:145-154`、`service/device.go:1949-1959`；旧设备→config→template chain|已有 Encore 代码可复用，但未完成验收|主任务 DM-API-007，附加 DM-TPL-016、DM-RT-011；固定snapshot的Web/App资源；新设备模型解析不经旧表JOIN或latest回退。|

## 4. 分组菜单清单

|ID|旧功能/入口|真实证据|处置|任务/验收要点|
|---|---|---|---|---|
|B-G01|POST `/device/group`|`service/device_group.go:26-85`：校验父同tenant、全租户组名唯一、tier=-1|新系统计划完整实现|DM-GRP-003；新同父唯一若采用必须记录为明确放宽决策；root统一NULL，父级存在/租户/并发重名。|
|B-G02|PUT `/device/group`|`service/device_group.go:91-132`仅拒自父，校验id/parent同tenant；未见后代防环|新系统计划完整实现|DM-GRP-004/005；移动独立用例、锁树/CAS，拒后代成环，深度边界可测。|
|B-G03|DELETE `/device/group/:id`|`service/device_group.go:87-89`；`dal/device_groups.go:21-27`；成员FK ON DELETE CASCADE|新系统计划完整实现|DM-GRP-006；本次新策略非空/外部引用409而不隐式丢成员，明确先移/删关系步骤。|
|B-G04|GET `/device/group` 分页与parent选择|`dal/device_groups.go:40-72`按tenant、name/parent、创建时间倒序|新系统计划完整实现|DM-GRP-001；排序/分页/total/权限一致。|
|B-G05|GET `/device/group/tree`|`service/device_group.go:152-192`；`dal/device_groups.go:91-132`对后代active设备去重计数|新系统计划完整实现|DM-GRP-002；保留同一设备多组时祖先去重，避免无限递归，截断要明示。|
|B-G06|GET `/device/group/counts`|`dal/device_groups.go:134-153`：active设备总量、未分组总量|新系统计划完整实现|主任务 DM-DEV-020，附加 DM-DEV-026、DM-GRP-002；对应字段和ungrouped筛选按现OpenAPI实现；空租户都是0，同范围重算一致。|
|B-G07|GET `/device/group/detail/:id`|`service/device_group.go:205-238`返回detail/tier路径/statistics；`dal/device_groups.go:192-242`包含total/online/offline/alarm|新系统计划完整实现|DM-GRP-001、DM-RT-002、alarm消费；元数据与runtime/alarm统计标owner及观察时间，不能只保留directCount。|
|B-G08|POST `/device/group/relation` 批量加成员|`service/device_group.go:240-260`逐一验证组/设备同tenant；`dal/r_group_device.go:64`；unique(group,device)|新系统计划完整实现|DM-GRP-008；同批越权全拒，重复加入幂等；数据库tenant复合FK补足。|
|B-G09|DELETE `/device/group/relation`|`service/device_group.go:262-273`同租户组/设备验证|新系统计划完整实现|DM-GRP-009；不存在仍幂等无成员，CAS/重复请求行为一致。|
|B-G10|GET `/device/group/relation/list`|`service/device_group.go:275-289`、`dal/r_group_device.go:77-111`|新系统计划完整实现|DM-GRP-007；按授权分页、device模板标签、total一致。|
|B-G11|GET `/device/group/relation` 设备所属组|`service/device_group.go:291-316`返回完整group_path|新系统计划完整实现|DM-GRP-010；移动后路径更新、不能缓存旧路径；设备/组双向授权。|
|B-G12|创建自动加入唯一根组|`dal/devices.go:36-73`同事务创建关系；`dal/device_groups.go:156-172`恰好1个parent='0'根组才自动加入|需要业务方确认|主任务 DM-DEC-004，附加 DM-DEV-003、DM-GRP-008；新计划默认显式分组时须指出该旧自动行为；若保留，制定0/1/2根组、并发建根与创建设备、跨tenant测试；不能悄悄删功能。|

## 5. 必须同步切换的其他消费者

|消费者/功能|实际路径|当前耦合|处置/任务|
|---|---|---|---|
|自动化与场景|`router/apps/scene_automations.go`、`internal/service/scene_automations.go`；`internal/service/broker_automation.go:139-150,273,361-370`；`sql/1.sql:1116-1134`|condition type10指设备、11指device_config；运行时按config选择设备、查父链启停|新系统计划完整实现：规则owner改调用正式元数据/模型合同、既有执行逻辑不迁metadata；显式模板revision与动态设备集合语义；主任务 DM-RT-008，附加 DM-API-007/011。|
|告警状态与告警详情|`internal/dal/latest_device_alarm.go`、`internal/service/alarm.go:402`；`dal/devices.go:413-443`；`dal/device_groups.go:214-226`|列表/组统计JOIN latest_device_alarms、报警消息含product_id|属于其他明确负责的运行时服务：alarm owner读写，元数据调用授权统计；新ID映射需改关联，不把alarm表搬metadata。 主任务 DM-RT-009，附加 DM-DEV-026、DM-API-011。|
|OTA|`internal/service/ota.go:30`、`sql/1.sql:401-422`|升级包以device_config_id筛设备，devices.current_version显示固件|属于其他明确负责的运行时服务：OTA owner保留包/任务/固件观察；适配accessTemplate/revision和device ID、删除引用保护；无额外OTA重做。 主任务 DM-RT-012，附加 DM-RT-001、DM-API-011、DM-DB-011；保持既有OTA owner。|
|协议插件取配置|`router/router_init.go:99-100`、`internal/service/protocol_plugin.go:30-56,144-149`|旧API直接返回设备与父子config/voucher；目前位于JWT前|新系统计划完整实现：正式service identity与限域read合同；runtime从新metadata获得期望配置，不调用Gin/旧DB；secret使用受控引用，配置版本校验。 主任务 DM-RT-001，附加 DM-RT-007/012。|
|服务插件发现/导入|`router/router_init.go:101-102`、`internal/service/service_access.go:187-188`、`device.go:139-255`|external identity/服务接入点/模板可选|新系统计划完整实现：同正式API、稳定provider namespace+external ID、幂等发现/导入，服务插件不得再直接写旧devices。 主任务 DM-RT-001，附加 DM-TPL-015、DM-DEV-024/012。|
|市场单物模型模板发布/安装|`router/apps/device.go:144-156`、`internal/service/device_template_publish.go`、`device_template_install.go`|旧模板和fields/charts导出/安装；不是普通新设备接入模板副本|新系统计划完整实现：市场owner适配现有ThingModel package和新接入模板revision引用；模板/模型导入顺序、内容校验、引用映射；不迁市场账号业务到metadata。 主任务 DM-RT-010，附加 DM-API-007、DM-TPL-012/013。|
|市场Bundle发布|`market_bundle_publish.go:235-249,287,536-562,676-688`|读旧device_templates与关联device_configs，拼model/插件依赖|新系统计划完整实现：固定快照/接入revision依赖，不从旧库组包；幂等与租户归属有端到端测试。 主任务 DM-RT-010，附加 DM-TPL-012、DM-API-011。|
|市场Bundle安装/重绑/补偿|`market_bundle_install.go:319-515,585-596,837-841,934-936`；`router/apps/device.go:160-186`|安装旧model字段；通过device→config→template判兼容，补偿删除旧template|新系统计划完整实现：同正式API，新ID映射引用，重试/补偿不可删既有共享资源；不造兼容resolver。 主任务 DM-RT-010，附加 DM-TPL-013、DM-API-011。|
|本地看板模板、ThingsVis实例|`dashboard_template.go:478-545,691`；`router/apps/device.go:189-193`|按旧model ID筛兼容设备、查询旧config；保存Web/App资源|新系统计划完整实现：固定ThingModel资源快照、正式兼容查询、新device引用；一个owner渲染资源，别在metadata另建chart模块。 主任务 DM-RT-011，附加 DM-API-007/011。|
|遥测/属性/事件/模拟数据|`router/apps/telemetry_data.go:16-62`、`attribute_data.go:16-31`、`event_data.go:14`|当前值、历史、聚合、删除、下发日志、模拟初始化/发送|属于其他明确负责的运行时服务：telemetry/attribute/control owner；新页面使用正式边界，仿真有资源权限/限流，历史不迁metadata。 主任务 DM-RT-005，附加 DM-RT-016/018/019。|
|命令执行/计划/回执|`router/apps/command_data.go:13-29`、`service/command_durable.go:238-243,434`|计划preview/authorize/submit，命令Pub、status、日志；查询设备启停及父链|属于其他明确负责的运行时服务：control owner复用耐久幂等与版本校验，API accepted/delivered/ACK不能等同物理执行成功。 主任务 DM-RT-006，附加 DM-RT-014、DM-DEC-001。|
|移动端|本复制范围无独立mobile服务目录；已有 `device_templates.app_chart_config` 是明确App消费数据，单独App源码未提供|不能仅因Web切换便认定移动端完成|需要业务方确认：独立移动客户端范围/版本支持；现有App图表数据引用迁移为发布阻断依赖，不凭空声称已查过App请求。 主任务 DM-RT-011，附加 DM-DEC-003。|

## 6. 权限、表约束和迁移风险（代码证据，不作漏洞已复现声明）

1. **可信租户上下文必须补到所有用例。** `api/device_config.go:32-70,108-122` 更新/删除/详情/批绑定均未向service传claims，DAL按ID写；`api/device.go:82-96,644-720,815-827` 中若干关系/激活/凭据处理也未显式传claims。JWT存在不等于资源授权存在。新增跨tenant E2E必须覆盖每个operation，不只主列表。
2. **默认拒绝而非仅登记URL才校验。** `internal/middleware/casbin_middle.go:12-38` 明示“资源在表才校验”，匹配基于URL，拒绝返回400。目标每个operation绑定permission和resource scope，HTTP403/404一致。`router/router_init.go:143-146` 在设备常规路由前挂JWT/Casbin；不能对前置注册/诊断路由假定有此保护。
3. **Community用户角色实际只有SYS_ADMIN/TENANT_ADMIN。** `pkg/authz/community_roles.go:3-6`；`jwt_auth.go:97-106`拒其他Authority；APIKey校验后派生TENANT_ADMIN在`:136-143`。`device_scope_test.go:10`还有旧TENANT_USER测试名/用例，它不证明Community产品仍支持该角色。
4. **菜单种子历史称谓不代表今日前端。** `sql/1.sql:1689-1710`分出`device_template`功能模板（定义物模型图表）、`device_config`配置模板（协议参数）、设备/分组；`sql/19.sql:114`的资源市场“设备模板”又是另一入口。主文档须用前端真实路由判断菜单，避免仅从SQL名称做结论。
5. **设备编号、voucher原库全局唯一，租户外键不完整。** `sql/1.sql:1416-1420`；新可改为tenant内业务编号唯一，但runtime公共标识必须provider命名空间唯一，持久映射保留旧编号/ID/外部标识。旧`id`可非UUID（`service/device.go:40-62`），迁移不能CAST后报错才处理。
6. **凭据资料不能混入普通导出或元数据日志。** 旧template_secret/设备voucher普通模型可序列化；旧导出函数写voucher列（`device.go:719-759`）。迁移报告仅记录ref/fingerprint，不输出secret；新接口单独审计reveals。
7. **旧分组树无完整防环保护。** 创建验证父存在；更新只拒自身（`:91-118`）；递归SQL `dal/device_groups.go:267-285`无环保护/tenant条件，tier创建-1。迁移需检测环/深度/孤儿/跨tenant父，并隔离不可修复数据；正式新树写加事务锁和复合tenant关系。
8. **旧关系FK只按ID。** `sql/1.sql:1515-1522`成员unique(group,device)、ON DELETE CASCADE，但tenant_id不参与FK。目标复合tenant约束、防删后悬空；旧`products.tenant_id`可空必须预检，不猜租户。
9. **旧副作用不是原子。** 批绑定逐项写；模板更新后再断连；服务批创后再通知；凭据写后cache删除。目标正式Outbox/operation状态和runtime generation ACK，受控故障注入验证不丢动作；不是复制旧cache逻辑。
10. **旧删除会删历史。** `device.go:456-510`明确删除多类历史；本轮metadata删除不得连带删除其他owner历史。历史保留和设备tombstone关联需规定访问控制及运行身份连续性。
11. **新数据字段不能静默丢弃。** legacy `label`是逗号字符串、新labels是map，需要确定转换；location字符串/经纬度需合法性预检；remark1/2/3/additional_info/batch_number/当前固件不能无声忽略。迁移每字段应标迁入元数据、交给相应owner、归档或需业务确认。
12. **模板与设备都有protocol_config。** `device_configs.gen.go:21`、`devices.gen.go:32`；不能只迁模板配置。必须区分共享配置与设备实例参数/外部ID，冻结覆盖合并规则，按provider schema验证；避免复制两个可编辑相同事实源。
13. **新架构不自动复制旧所有选择。** 编号可改、多级网关、唯一根组自动入组、全租户组名唯一、默认active创建均是已核实旧行为；若改变，应以明确产品/技术决定记录和可执行测试解释，不误称旧功能不存在。

## 7. 已有测试证据的边界

- `internal/service/device_scope_test.go:10` 只覆盖`canReadTenantDevice`函数（同租户、SYS_ADMIN跨租户、nil），不是整套资源授权测试。
- `internal/api/device_broker_evidence_test.go:23,62`、`device_broker_semantic_integration_test.go:27`、`device_telemetry_evidence_integration_test.go:27,311` 包含运行证据/租户/重复查询参数等测试源；本轮未执行，不可写测试通过。
- `internal/service/device_template_install_test.go:18,58,87,99,109,126,144` 是市场图片下载/内容/来源/重定向限制的测试源，不能用作模板CRUD、物模型迁移或页面切换验收。
- 其他目前没有本轮取得的独立成功日志/构建SHA对照：设备/接入模板/分组新功能状态应“待做”或“已有Encore可复用未验收”，不能从旧路由数量计算完成度。

## 8. 对初始草稿路径/规则的具体纠正

- 不存在 `router/apps/device_model.go`：物模型路由实际在 `router/apps/device.go:235-287`。
- 不存在 `internal/dal/product.go`：产品key查询在 `internal/dal/device_auth.go:27-36`，实体在`internal/model/products.gen.go`。
- 不存在 `internal/service/automation.go`：使用`scene_automations.go`及`broker_automation.go`。
- 不是“旧编号校验=新增可用性”；不是“子设备只有leaf可绑定”；不是“设备配置协议参数只在模板存在”；不是“产品没有实际代码用途”。
- 新PATCH若声明JSON Merge Patch，不能同时把labels/draft对象说成原子替换；单独PUT或明确partial JSON更一致。
- metadata operation授权不能固定`device:read`，模板作业要复用来源operation授权；下载的操作目录应与实际binary content一致。
- Product API若无默认模板字段，不应写CreateDevice隐式继承产品默认模板；应先查明Encore既有product语义并明确唯一约束/所有者。


## 9. 初始化与菜单实际执行路径补充

- `initialize/pg_init.go:35-77` 在启动 `PgInit` 时连接Postgres、初始化Casbin，并调用`CheckVersion`；`:184-271` 会创建/更新`sys_version`并按序执行`sql/{N}.sql`。本轮未启动应用，避免以“只查看”触发初始化写库。迁移计划要把现库实际版本、SQL checksum、菜单种子执行状态纳入源数据预检，不能只看到文件就声称数据库已应用。
- `initialize/casbin_init.go:14-31` 使用GORM DB adapter和`./configs/casbin.conf`加载policy；`internal/service/casbin.go:65-83`按g2 URL登记及`Enforce(user,url,allow)`。这证明老策略按URL登记而非按HTTP操作/资源实例统一授权；新OpenAPI权限矩阵应覆盖方法与动作，不能简单复制字符串表。
- `internal/dal/ui_elements.go:73-99` 的当前菜单读取按SYS_ADMIN/TENANT_ADMIN authority JSON过滤、递归子菜单，并对TENANT_ADMIN移除社区不支持用户菜单。剩余else分支还含TENANT_USER历史代码，不应据其存在恢复该产品角色。
- `sql/1.sql:220-230` 的groups仅主键，无parent外键。新树FK/防环/软删除必须重新定义。
- `pkg/constant/constant.go:13-16,53-56` 再次证实1=直连、2=网关、3=网关子设备；`model/devices.http.go`个别响应注释与该定义不一致，迁移应按已验证值和实际逻辑而非注释猜测。

## 10. 2026-10-25 当前源码复核与追溯修订

本节是在本轮取得并校验的当前工作区快照上独立复核的静态结果，不把旧报告的测试结论升级为本轮通过。未访问生产数据库或启动应用。

|核查项|当前源码证据|本轮结论 / 任务影响|
|---|---|---|
|框架与模型边界|`go.mod:8-32`；`internal/model/device_configs.gen.go:14-32`、`device_templates.gen.go:14-31`、`devices.gen.go:14-52`|Gin 1.10.0 / GORM 1.25.7 / Casbin 2.82.0；接入模板与物模型混名结论仍成立。接入模板保留协议、凭据与注册策略；旧模型保留字段/图表来源。DM-DOM-001/002。|
|编号资格、批量网关、删除历史|`internal/service/device.go:656-675,820-869,424-540`|编号check是已存在且未激活资格；网关支持网关子节点且旧批处理逐项写；删除确会删历史。已把旧路由分别对应到DM-DEV-022、DM-DEV-027、DM-DEV-005，而不是泛泛CRUD。|
|模板操作资源授权|`internal/api/device_config.go:32-68,108-120`；`internal/service/device_config.go:74-202,231-264`|详情/更新/删除/批绑定没有把claims传入相应用例；新用例须逐资源校验。不把JWT存在等同资源授权；本轮未做漏洞利用验证。DM-API-003。|
|注册与状态字面值|`internal/service/device_auth.go:19-162`，尤其`:83`；`device.go:126,207,408`；`broker_automation.go:139,150,273,361,370`|动态注册写`enable`，普通创建/激活写`enabled`，自动化按`enabled`筛选。证明源码存在不一致，尚未证明运行数据中有多少异常。DM-MIG-002/007必须预检状态分布，未知/异常值报告并冻结转换规则；DM-RT-013只能产生正式状态枚举。|
|注册关联租户|`internal/service/device_auth.go:54-66,87-110`；`internal/dal/device_auth.go:27-36`|旧productKey/父编号查询不含可信tenant谓词；新注册应由引导凭据确定tenant，产品/父网关均同tenant验证。DM-RT-013、DM-API-003。|
|分组树与单位|`internal/service/device_group.go:26-132`；`internal/dal/device_groups.go:267-285`；`internal/model/device_configs.http.go:90-92`|旧组名全租户唯一、tier=-1、更新只拒自父，递归SQL无tenant/环条件；online_timeout分钟、heartbeat秒。现有新树约束和显式单位转换必要，不能照搬旧表。DM-DB-006、DM-GRP-005、DM-MIG-006/008。|
|特殊入口与真实角色|`router/router_init.go:99-139,143-146`；`pkg/authz/community_roles.go:3-6`|配置插件、注册、诊断和WS在常规JWT挂载前；不能假定它们全部由后置中间件保护。当前Community人员角色仅SYS_ADMIN/TENANT_ADMIN，不创建第三用户角色。|
|服务发现候选与已登记设备不同|`internal/api/service_access.go:84-96`；`internal/service/service_access.go:159-193`；F `src/service/api/plugin.ts:52-56`|真实服务候选由插件分页返回并标注已绑定状态；不是listDevices的别名，也不是provider/instance目录本身。DM-RT-001、DM-TPL-015、DM-DEV-024、DM-CLI-017负责正式`listServiceDiscoveredDevices`发现和导入合同，不另造设备事实源。|
|既有物模型决策|三份 `docs/ENCORE-THINGMODEL-*.md`、`docs/ENCORE-KERNEL-MIGRATION-DISPOSITION-2026-10-20.md` 的当前内容|10/16与10/18已要求方案B、固定快照和一次受控升级；文档中的隔离QA成功不能当当前checkout的发行证据。10/20残留的“过渡投影/兼容”不符合本轮用户明确边界，不纳入目标。|

`legacy-routes.json` 的40个无稳定ID占位已闭合；164条原登记加1条本轮确认的服务发现，共165条均包含`primary_task`与`supporting_tasks`，仍保留一个处置分类。并校正激活资格、批网关、状态历史、诊断、实时流、缓存删除、模拟与控制等旧任务归属。custom/control路由虽沿用已有核心可复用分类，其主任务明确为DM-DEC-001：无法无损表达的对象未经处置不得通过发布；“核心已有”不代表定制控制已获验收。
