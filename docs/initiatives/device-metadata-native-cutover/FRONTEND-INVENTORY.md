# 前端真实功能、请求与验收库存

> 初次审计：2026-10-23；当前源码增量复核：2026-10-25。只读源码审计；没有启动浏览器、发送业务请求、执行前端测试、部署或迁移数据。本文件是 `INVENTORY.md`、`TASKS.md` 和 `TEST-PLAN.md` 的证据附件，不能把下列“源码存在”解释为线上验收通过。

## 1. 结论与证据口径

1. **当前工作区三个菜单仍通过旧 `request` 调用 Community 管理 API。** 设备列表 `src/views/device/manage/index.vue:667-681` 调用 `deviceList`；设备模板列表 `src/views/device/config/index.vue:6,45` 调用 `deviceConfig`；分组列表 `src/views/device/grouping/index.vue:37-47` 调用 `getDeviceGroup`。分别映射 `GET /device`、`GET /device_config`、`GET /device/group`，共用客户端优先使用配置的 demo baseURL 或开发代理，仅缺省时使用 `${window.location.origin}/api/v1`（`src/service/request/request.ts:7-12`），成功条件仍是旧响应 `code === 200`（同文件 `38-54`）。未找到可证明这三个菜单已经接入正式 Encore 原生 API 的当前请求链。
2. **“设备模板”页面对应旧 `device_configs`，不是旧 `device_templates`。** `src/router/device-route-mapping.ts:4-19` 明确写出历史路由键 `device_config → Device Template page`、`device_template → Thing Model page`；实际 URL 为 `/device/template` 和 `/device/thingsmodel`。原生规划必须用 `DeviceAccessTemplate` 等明确领域名称，不能按目录 `template/` 误认本次“设备模板”菜单。
3. **模板菜单远不止 CRUD。** 存在关联设备批量绑定/解绑、模型关联、协议/凭证/动态接入表单、Topic Mapping、6 类处理脚本及调试、扩展字段定义、自动注册、模板密钥、在线超时/心跳、封面、市场发布、自动化及告警入口。设备详情还包含状态历史、数据/属性/事件/命令/延期消息、诊断/调试、模拟器、分组关系和网关子设备。库存不能用“设备详情接入新 API”一行盖过这些独立用例。
4. **按钮权限需新增明确合同并验收。** 三菜单相关 `src/views/device/**` 未检出 `v-auth`、`v-permission`、`hasAuth`、`hasPermission` 等动作授权；详情编辑按钮明确 `v-show="true"`（`details/index.vue:510`）。当前角色类型只有 `SYS_ADMIN` / `TENANT_ADMIN`（`src/typings/api.d.ts:64-67`），登录后 `roles=[authority]`（`src/store/modules/auth/index.ts:157`）；现有路由守卫仍有登录及 role 规则，空 route.roles 放行、SYS_ADMIN 旁路（`router/guard/permission.ts:19-36`），动态菜单来自 `GET /ui_elements/menu`（`service/api/route.ts:7-10`）。这只证明页面层动作权限没有显式覆盖，**不能据此宣称后端没有授权**。
5. **候选分支不能冒充当前切换结果。** `F@ee5aab55` 有固定物模型快照、字段转换、请求竞态保护及测试源可复用；但 `native-template-binding.ts:1,28-38` 仍调用 `deviceConfigEdit → PUT /device_config`，候选详情仍读 `deviceDetail`。必须按文件/符号选择性复用，不把候选整支或整个混合工作区视为本次交付。

### 来源与状态

- `F`：远端 `/Users/junhong/Downloads/code/thingspanel-frontend-community` 当前工作区的 SHA256 校验只读副本，本轮只读复核副本 `source-current/F`（根代理采集并校验 SHA256；临时副本不作为永久交付，持续定位采用远端仓库根、HEAD、Git状态和相对源码路径）。主代理读取的 Git 基线：`master`，HEAD `c6968c77662f41a642fba822ddc92bc308de9895`，16 modified / 21 untracked。下表未加前缀的路径均相对 `F`。
- 当前与本范围有关的 modified 包括 `src/service/api/device.ts`、`src/views/device/manage/modules/device-readings-compact.vue`、`src/utils/device-readings.ts`、`src/__tests__/unit/device-readings.test.ts`、`src/components/data-table-page/index.vue`、`build/config/proxy.ts` 和 locales。其余文件是否已跟踪以主代理 Git 文件清单为权威；本地复制不能自行重建远端 Git 状态。
- `F@ee5aab55`：只读候选 ref `codex/native-device-metadata`，不是当前 checkout；本轮采集的 API/helper 与其测试源位于 `source-current/F@ee5aab55`，已复核绑定写入仍走旧 config。第8节 N004、N005、N008 的候选 views 文件本轮未重新采集，明确保留为2026-10-23历史源码证据，不能当作2026-10-25重新核实或已集成。
- 所有“当前旧代码”行的新系统状态统一为“待做 / 部分可复用，未完成原生验收”；旧函数存在仅证明功能意图和调用链，不证明服务器实现、权限、测试或页面运行结果。
- `build/config/proxy.ts:27-48` 当前原生代理范围含 notification；没有看到三菜单的设备管理代理映射。仍需发布时以 HAR 验证真实部署 URL；静态分析不能替代网络记录。

### 处置缩写（每行只选一类）

| 标记 | 用户要求的处置类别 | 含义 |
|---|---|---|
| 完整 | 新系统计划完整实现 | 由新元数据合同实现；旧布局/通用组件可复用，不沿用旧 API |
| 复用 | 已有 Encore 代码可复用，但未完成验收 | 在候选分支找到原生相关实现/测试源；明确尚未集成/执行 |
| 运行时 | 属于其他明确负责的运行时服务 | 状态、遥测、控制、诊断、接入执行、自动化、告警等责任方；前端仍须接正式合同 |
| 不迁 | 经产品边界确认后不迁移 | 本审计没有独立作出新的“不迁”批准；既有决策中的旧控制语义由主规划引用原决策 |
| 待确认 | 需要业务方确认 | 仅限无法从源码/已有决策确定的产品边界；不能把技术可核实项推给业务 |

下表旧请求路径均省略统一 `/api/v1` 前缀；新任务 ID 指向主规划已分配的正式 `DM-*` 任务。每行处置、独立实现与验收范围按对应任务及唯一OpenAPI追溯，第9节汇总前端任务映射。

## 2. 导航、基础客户端与权限

| 库存 ID | 真实功能 / 当前情况 | 旧请求或字段 | 处置 / 新任务 | 精确源码证据 | 可直接转测试的通过条件 |
|---|---|---|---|---|---|
| FEI-C001 | 设备菜单入口 | `/device/manage` | 完整；DM-FE-002 | `src/router/elegant/routes.ts:353-361` | 角色有权限时菜单打开设备页；无权限时菜单隐藏、深链拒绝；请求只到正式 API |
| FEI-C002 | “设备模板”菜单入口及详情/编辑 | `/device/template`；历史键 `device_config` | 完整；DM-FE-002、015 | `src/router/elegant/routes.ts:279-307`；`src/router/device-route-mapping.ts:4-19` | 点击模板进入接入模板，不误入物模型；刷新/深链/返回保持正确对象 |
| FEI-C003 | 设备分组菜单、详情 | `/device/grouping`、`/device/grouping-details` | 完整；DM-FE-002、019 | `src/router/elegant/routes.ts:326-343` | 根组、子组详情能直接打开；未授权组深链无内容泄漏 |
| FEI-C004 | 原有物模型菜单独立 | `/device/thingsmodel`；键 `device_template` | 复用；DM-API-007、DM-TPL-016 | `src/router/elegant/routes.ts:391-399`；`src/router/device-route-mapping.ts:4-19` | 模板模型选择只绑定已发布 snapshot；跳转复用既有物模型页，不另造一个模型编辑器 |
| FEI-C005 | 旧请求封装、token、错误 envelope | Authorization、x-token；`code===200` | 完整；DM-FE-001、014、DM-API-003 | `src/service/request/request.ts:7-23,38-54,58-118` | 新生成客户端解析统一 Problem；401/403/404/409/412/422/503 显示可操作状态；不尝试旧 URL |
| FEI-C006 | 动态菜单与静态 role 守卫 | `GET /ui_elements/menu`；route.roles | 完整；DM-FE-002、DM-API-003 | `src/service/api/route.ts:7-10`；`src/store/modules/route/index.ts:147-180`；`src/router/guard/permission.ts:19-36` | SYS_ADMIN、TENANT_ADMIN及令牌只读/资源scope测试主体的菜单/深链/API一致（不新增TENANT_USER或第三角色）；不把空 roles 自动当所有动作有权 |
| FEI-C007 | 动作按钮目前无显式权限检查 | 编辑 `v-show=true`；其他禁用主要是 demo、加载或功能条件 | 完整；DM-FE-002 | `src/views/device/details/index.vue:510`；`src/views/device/config/index.vue:166-175`；对 `src/views/device` 的权限符号搜索 | 每个新动作按 capability 决定可见/可操作；绕过 UI 直接请求仍被服务拒绝；不能把“model为空禁发布”当权限 |
| FEI-C008 | 缓存与快速切换 | `deviceManageGroupSelection`、page cache、模板缓存 | 完整；DM-FE-022、DM-API-003 | `src/views/device/manage/index.vue:86-98,676-680,692-701`；`src/views/device/details/index.vue:141,386-410` | 切账号/tenant/设备时清理或按主体隔离缓存；A迟到响应不能覆盖B；无旧对象闪现 |
| FEI-C009 | UI 样例数据夹在真实空结果中 | 固定 demo device ID | 完整；DM-FE-022、DM-QA-004 | `src/utils/device-detail-demo-data.ts:7-9`；`src/views/device/details/modules/expect-message.vue:78-79`；`device-diagnosis.vue:156-157,244-247` | 正式验收使用非 demo 设备并核对真实后端记录；空数据不能显示样例为成功证据；demo明确隔离标注 |

## 3. 设备列表、创建、身份和关系

| 库存 ID | 真实功能 / 表单字段 | 当前旧 API / 状态 | 处置 / 任务映射 | 精确源码证据 | 验收用例要点 |
|---|---|---|---|---|---|
| FEI-D001 | 列表分页、详情入口、名称、在线、告警、接入类型、模板、协议/服务、最近数据时间 | `GET /device`；page/page_size | 完整；DM-DEV-001、DM-FE-003 | `src/views/device/manage/index.vue:215-293,667-681`；`src/service/api/device.ts:73-75` | 125条数据跨页无重漏；total按授权范围；字段语义匹配正式 schema，未知状态不能显示离线 |
| FEI-D002 | 关键词按设备名称/编号搜索 | `search` | 完整；DM-FE-003、DM-DEV-001 | `src/views/device/manage/index.vue:370-375` | 名称/编号匹配一致；空白、中文、特殊字符、快速输入以最后查询为准 |
| FEI-D003 | 标签筛选 | `label`（旧逗号字符串） | 完整；DM-FE-003、DM-DEV-001 | `src/views/device/manage/index.vue:376-381` | 新标签数组按合同匹配；包含逗号的标签不拆坏；跨tenant无标签泄漏 |
| FEI-D004 | 指定模板筛选和模板选择器 | `device_config_id`；旧选择器只取前99 | 完整；DM-FE-003、DM-TPL-001 | `src/views/device/manage/index.vue:200-212,315-325` | 100个以上模板可搜索/翻页选择；按固定模板ID筛选，不按显示名 join |
| FEI-D005 | 直连/网关/子设备筛选 | `device_type=1/2/3` | 完整；DM-FE-003、DM-DEV-001 | `src/views/device/manage/index.vue:348-359` | 新枚举与三种设备语义一致；空/未知值422，不默认为直连 |
| FEI-D006 | 在线/离线筛选 | `is_online=1/0` | 运行时；DM-RT-002、DM-FE-003 | `src/views/device/manage/index.vue:326-335` | 过滤在授权/分页前生效；unknown与offline区分；状态服务不可用显示503，不读旧is_online |
| FEI-D007 | 告警状态筛选 | `warn_status=Y/N` | 运行时；DM-RT-009、DM-FE-003 | `src/views/device/manage/index.vue:337-346` | 有告警/无告警口径明确并与告警列表一致；过滤不能把当前页本地删减后冒充总数 |
| FEI-D008 | 协议/服务提供方和服务实例筛选 | `service_identifier`、动态 `service_access_id`；`/service/plugin/select`、`/service/access/list` | 完整；DM-TPL-015、DM-FE-003、DM-RT-001 | `src/views/device/manage/index.vue:362-368,405-451,483-541` | provider与instance明确不同ID；切provider清除不相容instance；选择器分页完整 |
| FEI-D009 | 左侧分组树选择 | `GET /device/group/tree`；`group_id` | 完整；DM-GRP-002、DM-FE-003 | `src/views/device/manage/index.vue:170-197,667-674,704-723` | 组树与设备结果一致；子树是否包含后代按合同明确；无权节点不出现 |
| FEI-D010 | 全部/未分组、数量统计 | `group_scope=ungrouped`；`GET /device/group/counts`，device_total/ungrouped_total | 完整；DM-DEV-020、DM-FE-003 | `src/views/device/manage/index.vue:465-480,667-674`；`src/service/api/device.ts:42-44` | 多组设备只计一次；移出最后一个组才进未分组；计数与可见集合一致，不能沿用404吞错 |
| FEI-D011 | 列表/卡片/地图/密集读数视图 | 共用列表与位置/读数；无证据证明服务端排序已完成 | 完整；DM-FE-003、022 | `src/views/device/manage/index.vue:798-835`；`src/components/data-table-page/index.vue:209,510-513,807-821` | 同一筛选切视图集合一致；无坐标设备有明确缺省；新服务端排序需独立契约，不把UI图标算完成 |
| FEI-D012 | 列表实时在线状态 | WS `/device/online/status/ws/batch`；发送device_ids与token | 运行时；DM-RT-002、DM-FE-003 | `src/views/device/manage/index.vue:122-156,683-687`；`src/utils/deviceStatusWebSocket.ts:25-32,96-105` | 翻页仅订阅当前授权设备；断线/过期显示unknown；撤权断订阅，不把缓存在线永久保留 |
| FEI-D013 | 手工创建设备，三步抽屉 | name必填、label数组、可空device_config_id；`POST /device`，access_way A | 完整；DM-DEV-003、DM-FE-004 | `src/views/device/manage/index.vue:586-594,846-891`；`manage/modules/add-devices-step1.vue:28-38,41-89,228` | 无模板/选模板都可按合同创建；重复点击只1台；旧第一步已落库的行为要改为明确创建结果/后续凭证失败状态 |
| FEI-D014 | 创建时从市场安装模板 | 市场token、template ID、version，结果device_config_id | 运行时；DM-RT-010、DM-FE-004、018 | `src/views/device/manage/modules/add-devices-step1.vue:94-98,127-162` | 安装成功后才能选择有效本地revision；缺provider或资源显示阻断；重复安装不造重复模板 |
| FEI-D015 | 创建第二步凭证及接入信息 | 动态input/table/select；`GET /device/connect/info`、`POST /device/update/voucher` | 完整；DM-DEV-009、DM-FE-006、DM-RT-003 | `src/views/device/manage/modules/add-devices-step2.vue:29-38,52,69,79-96` | 凭证按policy校验；普通详情不回明文secret；清空/取消/失败不残留凭证；实际设备可按新凭据连接 |
| FEI-D016 | 按编号检查、激活函数存在，但入口禁用 | `GET /device/check/{number}`；`PUT /device/active` | 待确认；DM-DEC-004；依赖DM-DEV-007、008、DM-FE-005 | `src/views/device/manage/index.vue:393-396,621-628,645-665`；`src/service/api/device.ts:58-60,223-225` | 是否启用页面入口由DM-DEC-004确认；若启用，非法/重复/已归属/跨tenant编号不能激活且有真实E2E；正式编号资格/激活用例仍按DM-DEV任务规划，不能把旧disabled代码计作可用功能 |
| FEI-D017 | 从服务添加设备入口 | 当前点击跳转集成页，不走旧server抽屉 | 运行时；DM-RT-001、DM-FE-004、DM-DEV-013 | `src/views/device/manage/index.vue:398-402,606-608`；`manage/modules/add-devices-server1.vue` | 集成发现候选→正式导入合同→设备详情闭环；旧server抽屉分支仍写在template（manage/index.vue:924-948），但当前菜单handler直接路由跳转、不令其可达；不得计作可用页面功能 |
| FEI-D018 | 服务发现设备批量导入 | device_name/number、device_config_id、protocol_config、additional_info；`POST /device/service/access/batch` | 完整；DM-DEV-013、014、DM-FE-007；接入发现由runtime | `src/views/device/service-details/components/serviceModal.vue:116-138`；`src/utils/service-device-import.ts`；`src/service/api/plugin.ts:52-66` | 每行source identity映射可审计；部分失败与重跑有checkpoint；不在浏览器逐行调用旧接口 |
| FEI-D019 | 设备详情基本信息读取和模板/网关/状态/告警导航 | `GET /device/detail/{id}` | 完整；DM-DEV-002、DM-FE-010 | `src/views/device/details/index.vue:313-316,555-589`；`src/service/api/device.ts:90-92` | 新字段/固定revision、desired/applied版本清晰；找不到/无权不留上次设备数据 |
| FEI-D020 | 编辑名称、设备编号、标签、描述 | `PUT /device` | 完整；DM-DEV-004、DM-FE-004 | `src/views/device/details/index.vue:463-483,526-536` | If-Match防覆盖；重号409保留输入；不可变编号策略若变更需明确新合同，不无意继承旧任改 |
| FEI-D021 | 经纬度/地图及扩展资料值编辑 | `location="longitude,latitude"`；additional_info旧JSON字符串 | 完整；DM-DEV-025、DM-TPL-019、DM-FE-025 | `src/views/device/details/modules/message.vue:22-29,83-109,138-160,176-179,213-250` | 坐标边界合法；String/Number/Boolean/Enum类型、默认值正确；字段变更沿既有值兼容保护返回409，不新增预览流程、不默默丢值 |
| FEI-D022 | 模板切换/解绑 | `PUT /device/update/config`；旧无预览与CAS | 完整；DM-DEV-016、017、DM-FE-012 | `src/views/device/details/modules/settings.vue:42-53,174-179`；`src/service/api/device.ts:475-477` | preview展示模型/运行配置/凭证/规则影响；未发布或冲突不能确认；stale ETag拒绝；新版本不静默改变所有设备 |
| FEI-D023 | 设备反向分组列表与单个加/删 | group tree+`GET/POST/DELETE /device/group/relation` | 完整；DM-GRP-008、009、010、DM-FE-021 | `src/views/device/details/modules/settings.vue:78-89,127-155,238` | 反向关系与组成员页一致；重复点击幂等；同次取消不会因watch再删错误对象 |
| FEI-D024 | 删除设备 | `DELETE /device/{id}`；确认弹窗 | 完整；DM-DEV-005、DM-FE-005 | `src/views/device/details/modules/settings.vue:182-200` | 引用冲突指出依赖；提交后运行凭据撤销有回执；操作失败不从UI假删除；跨tenant404 |
| FEI-D025 | 管理启停并无可见已接UI证据 | settings有更新is_online函数，但模板未绑定 | 完整；DM-DEV-006、DM-FE-005 | `src/views/device/details/modules/settings.vue:108-114`；同组件template | 新管理状态与连接观察状态独立；禁用不会伪写online；发布验收需真实可见入口和runtime拒连证据 |
| FEI-D026 | 网关下子设备分页列表/详情跳转 | `GET /device/sub-list/{id}` | 完整；DM-DEV-019、DM-FE-013 | `src/views/device/details/modules/device-analysis.vue:69,94,107-112`；`src/service/api/device.ts:492-494`；`src/views/device/details-child/index.vue:18,26-43,94,113,231-239` | 只列当前网关直接子设备；翻页total准确；子设备独立详情的GET/PUT及共用tabs也必须切换，模型/权限与普通详情一致；user tab仅NEmpty占位（details/modules/user.vue:9-12），不视为设备用户管理已实现 |
| FEI-D027 | 选择现有设备绑定网关，多选 | `GET /device/list`；`POST /device/son/add {id,son_id}`逗号字符串 | 完整；DM-DEV-018、DM-FE-013 | `src/views/device/details/modules/device-analysis.vue:179,195`；`src/service/api/device.ts:498-504` | 新请求使用typed IDs；不允许跨tenant、非gateway父、嵌套环、不合法device kind；重复关系不新增 |
| FEI-D028 | 子设备地址编辑（当前UI固定现父；换父属于新关系合同） | `PUT /device {id,parent_id:props.id,sub_device_addr}` | 完整；DM-DEV-018、DM-FE-013 | `src/views/device/details/modules/device-analysis.vue:162` | 新关系专用API原子迁移；同网关重复地址409；并发/失败保持旧父关系完整 |
| FEI-D029 | 从网关移除子设备 | `PUT /device/sub-remove {sub_device_id}` | 完整；DM-DEV-018、DM-FE-013 | `src/views/device/details/modules/device-analysis.vue:82`；`src/service/api/device.ts:506-508` | 仅解绑不删除设备；runtime路由撤销回执可见；重复解绑幂等 |
| FEI-D030 | 文件批量创建/导入/导出目前未发现菜单可用动作 | 主manage的actions=[]，topActions只有添加 | 完整；DM-DEV-012-015、DM-FE-007-009 | `src/views/device/manage/index.vue:303,586-594` | 按用户要求作为新交付独立实现；CSV/XLSX校验、错误行、digest、批次重跑、全筛选导出；不能标“旧功能已完成” |
| FEI-D031 | 当前固件版本展示 | 设备设置中的只读version | 运行时；DM-RT-011、DM-FE-010 | `src/views/device/details/modules/settings.vue:225-227` | 固件观测信息从OTA/runtime明确owner读取；不把版本字符串复制到元数据事实源 |

## 4. 设备详情中的运行时功能（保留体验，独立责任合同）

| 库存 ID | 真实功能与字段 | 旧调用 / 状态 | 处置 / 任务映射 | 精确源码证据 | 原生验收条件 |
|---|---|---|---|---|---|
| FEI-R001 | 连接信息、凭证动态表单、复制接入参数 | `/device/connect/form`、`/device/connect/info`、`POST /device/update/voucher` | 运行时；DM-RT-003；凭证写入DM-DEV-009/DM-FE-006 | `src/views/device/details/modules/join.vue:54-63,87-111,142-182` | 协议/端点来自有效模板revision与runtime；缺配置不可默认MQTT；secret独立授权/脱敏/不进日志 |
| FEI-R002 | 服务接入连接详情跳转 | access_way B隐藏手工凭证卡片 | 运行时；DM-RT-001、003 | `src/views/device/details/modules/join.vue:123-127,142,178,187` | 服务接入显示connector instance；无手工凭证能力的provider不显示可写表单 |
| FEI-R003 | 当前遥测键值、单位、时间、刷新 | `GET /telemetry/datas/current/{id}` | 运行时；DM-RT-005、DM-FE-011 | `src/views/device/details/modules/telemetry/telemetry.vue:303`；`src/service/api/device.ts:262-265` | 按固定snapshot解释类型/单位；无模型设备显式unmodeled；无数据与加载失败分别显示 |
| FEI-R004 | 当前遥测字段删除 | `DELETE telemetry/datas`（delete2包装器） | 运行时；DM-RT-016、DM-FE-037 | `src/views/device/details/modules/telemetry/telemetry.vue:333`；`src/service/api/device.ts:386-388` | 删除当前缓存/历史的范围必须明确；独立权限/审计；不删物模型字段；没有授权不得提供按钮 |
| FEI-R005 | 历史遥测曲线、日期范围、聚合 | statistic，window30s至1mo，avg/max/min/sum/diff | 运行时；DM-RT-005、DM-RT-018、DM-FE-011、DM-FE-039 | `src/views/device/details/modules/telemetry/modules/time-series-data.vue:189-205,228-248,333`；`src/service/api/device.ts:344-382` | UTC边界和聚合准确；历史点模型解释不漂移；新API错误不触发旧history fallback |
| FEI-R006 | 历史表格、翻页、Excel导出 | history页请求export_excel | 运行时；DM-RT-018、DM-FE-039 | `src/views/device/details/modules/telemetry/modules/history-data.vue:23,45,101,187`；`src/service/api/device.ts:469` | 导出当前筛选全部历史点；权限/时间窗/大小明确；导出是遥测owner，不搬数据到metadata |
| FEI-R007 | 曲线全屏、图表数据导出 | 客户端表格/图像导出 | 运行时；DM-RT-005、DM-FE-011 | `src/views/device/details/modules/telemetry/modules/time-series-data.vue:469,549,577` | 全屏保持筛选；下载列/时间/单位与展示一致；不把仅当前加载点称全量历史 |
| FEI-R008 | 遥测下发日志与原有直接/延期下发 | telemetryDataPub、expectMessageAdd | 运行时；DM-RT-006；既有物模型COMMAND决策 | `src/views/device/details/modules/telemetry/telemetry.vue:287,361,369,472`；`src/service/api/device.ts:391-402` | 旧可控telemetry须映射明确COMMAND或给阻断报告；不能重新开放“任意字段写”；受理≠投递成功 |
| FEI-R009 | 属性当前值/单位/更新时间 | `getAttributeDataSet` | 运行时；DM-RT-005 | `src/views/device/details/modules/stats.vue:19-40,132-138`；`src/service/api/device.ts:412-414` | ATTRIBUTE只作属性数据读取；snapshot类型/枚举一致；跨tenant拒绝 |
| FEI-R010 | 属性当前值删除 | `deleteAttributeDataSet(row.id)` | 运行时；DM-RT-016、DM-FE-037 | `src/views/device/details/modules/stats.vue:42-60`；`src/service/api/device.ts:416-418` | 不误删模型定义；明确当前缓存生命周期和审计；失败保留行 |
| FEI-R011 | 属性下发及操作日志 | 日志created_at/message_id/data/operation_type/status/error_message；直接/延期下发 | 运行时；DM-RT-006；COMMAND转换规则 | `src/views/device/details/modules/stats.vue:93-125,142-150`；`src/service/api/device.ts:421-428` | 属性不能被隐式当COMMAND；合法转换后的参数/回执一致；发送和设备返回结果分别显示 |
| FEI-R012 | 事件上报列表 | `GET /event/datas`相关wrapper | 运行时；DM-RT-005 | `src/views/device/details/modules/event-report.vue`；`src/service/api/device.ts:439-441` | 使用固定EVENT定义；结构化payload不混为属性；排序/分页/时间范围合同完整 |
| FEI-R013 | 命令选择、参数、投递、回执、历史日志 | command identifiers/pub/status/logs | 运行时；DM-RT-006、DM-FE-011 | `src/views/device/details/modules/command-delivery.vue`；`src/views/device/details/modules/public/distribution-and-table.vue`；`src/service/api/device.ts:444-459` | 只能提交固定snapshot声明的COMMAND；验证类型/枚举/必填；同key无重复投递；ACK不声称物理效果 |
| FEI-R014 | 延期消息队列列表/筛选 | status pending/sent/expired；send_type telemetry/attribute/command；label、expiry_time、payload、send_time | 运行时；DM-RT-014、DM-FE-033 | `src/views/device/details/modules/expect-message.vue:34-75,91-149`；`src/service/api/device.ts:400-406` | 明确迁后允许的COMMAND类型、到期规则和状态机；筛选分页准确；旧未声明控制必须转换/阻断 |
| FEI-R015 | 延期消息删除/取消 | `DELETE /expected/data/{id}` | 运行时；DM-RT-014、DM-FE-033 | `src/views/device/details/modules/expect-message.vue:83-89`；`src/service/api/device.ts:408-411` | 已投递不可假称已撤回；pending取消与投递并发有唯一终态；跨设备/tenant不可取消 |
| FEI-R016 | 设备图表与物模型图表资源 | templateId→chart config；ThingVis | 运行时；DM-RT-005、DM-TPL-016、DM-API-007 | `src/views/device/details/index.vue:386-410`；`src/views/device/details/modules/telemetry-chart.vue` | 读取固定发布snapshot的资源；旧版本重开保持图表；资源缺失明确报错，不能显示空面板成功 |
| FEI-R017 | 密集读数视图、1秒刷新、布尔快捷控制 | deviceList→current telemetry→template字段；command→receipt | 运行时；DM-RT-005、006、DM-FE-003 | `src/views/device/manage/modules/device-readings-compact.vue:65,97,121,146,183-217,342,360-392,531` | 分页/批量读数防N+1；只给明确Boolean COMMAND显示开关；快速切设备不串值；不复制物模型规则 |
| FEI-R018 | 状态历史弹窗 | online/offline、start/end time、status、page | 运行时；DM-RT-015、DM-FE-034 | `src/views/device/details/modules/device-status.vue:76-89,163,241`；`src/service/api/device.ts:541-550` | 时间排序与UTC准确；无记录≠离线；查看历史需要相同设备scope |
| FEI-R019 | 诊断汇总与失败记录 | uplink/downlink/storage success/total/rate；timestamp/direction/stage/error | 运行时；DM-RT-015、DM-FE-035 | `src/views/device/details/modules/device-diagnosis.vue:151-193`；`src/service/api/device.ts:553-555` | 统计窗口明确；真实失败可定位stage；端点失败不能被catch空状态或demo替代为正常 |
| FEI-R020 | 调试开关读取/开启/关闭 | `/device/{id}/debug/status`、POST debug {enabled} | 运行时；DM-RT-004、DM-FE-011 | `src/views/device/details/modules/device-diagnosis.vue:208-229`；`src/service/api/device.ts:585-593` | 新会话有TTL/权限/到期；响应失败恢复UI；刷新恢复真实运行态而不是仅本地开关 |
| FEI-R021 | 调试日志轮询/刷新/滚动 | debug/logs页面仅传limit100、未传offset；wrapper虽定义offset不代表UI可翻页 | 运行时；DM-RT-004、DM-FE-011 | `src/views/device/details/modules/device-diagnosis.vue:232-255`；`src/service/api/device.ts:595-597` | 时间窗/方向/游标可分页；secret服务端脱敏；无数据清空旧列表；切设备停止旧轮询 |
| FEI-R022 | 模拟器保存/读取/初始化/发送 | JSON消息、server、port、topic | 运行时；DM-RT-015、DM-RT-019、DM-FE-036 | `src/views/device/details/modules/telemetry/telemetry.vue:205,245,692,723-730`；`src/service/api/device.ts:510-529` | 受控测试设备目标、协议校验、权限和审计；实际消息到指定设备；不能允许任意目标替代metadata合同 |
| FEI-R023 | 设备自动化规则入口 | 组件传device_id | 运行时；自动化owner，DM-RT-008 | `src/views/device/details/modules/automate.vue`；`src/views/device/details/index.vue:115-119` | 规则读新device/snapshot；禁用或删除引用可阻断；不重新实现自动化引擎 |
| FEI-R024 | 设备告警历史筛选/详情/备注修改/删除 | `GET /alarm/info/history/device`、`PUT /alarm/info/history`、`DELETE /alarm/info/history/{id}` | 运行时；告警owner，DM-RT-009 | `src/views/device/details/modules/give-an-alarm.vue:27-58,84,130,148,249`；`src/service/api/device.ts:105-127` | 备注/删除各自授权；设备scope与告警scope一致；告警历史留在原owner |
| FEI-R025 | 从设备创建告警规则/看规则 | `/scene_automations/alarm` | 运行时；告警/自动化owner，DM-RT-009 | `src/views/device/details/modules/give-an-alarm.vue:216`；`src/service/api/device.ts:114-117` | 选择的标识符/单位来自固定snapshot；模板升级预览列出不兼容规则 |

## 5. 设备模板（接入模板）菜单

| 库存 ID | 真实功能 / 关键字段 | 旧请求 / 当前UI边界 | 处置 / 任务映射 | 精确源码证据 | 验收条件 |
|---|---|---|---|---|---|
| FEI-T001 | 模板列表、名称搜索、分页、重置 | `GET /device_config`；name/page/page_size10 | 完整；DM-TPL-001、DM-FE-015 | `src/views/device/config/index.vue:30-45,62`；`src/service/api/device.ts:148-152` | 搜索分页按授权范围；>100条可访问；空/失败区分 |
| FEI-T002 | 卡片/列表切换、显示接入类型和关联设备数 | name、device_type、device_count | 完整；DM-TPL-001、DM-FE-015 | `src/views/device/config/index.vue:112-144,221,342` | device_count由同一授权统计口径生成；视图切换不改结果；未证实排序stub不能计完成 |
| FEI-T003 | 列表排序仅有stub | handleSorterChange未实现 | 完整；DM-TPL-001、DM-FE-015 | `src/views/device/config/index.vue:207-210` | 新排序字段白名单和稳定次排序有契约及测试；未知sort422 |
| FEI-T004 | 创建接入模板 | name、device_type、可空device_template_id、protocol_type、voucher_type、protocol_config；表单类型还含description/remark等 | 完整；DM-TPL-003、DM-FE-015/016 | `src/views/device/config/index.vue:69`；`config-edit/index.vue:33-45,96-107,151-189,351-435` | 模型可空；合法provider/config schema通过；name必填；JSON对象不二次编码成字符串 |
| FEI-T005 | 修改接入模板基本/协议配置 | `PUT /device_config`；编辑时device_type禁改，provider可重新选择 | 完整；DM-TPL-004、DM-FE-016 | `src/views/device/config-edit/index.vue:151-189,194-208,329-342,369-435` | 新编辑创建draft，既有设备revision不变；CAS冲突保留输入；切provider先报告丢失/不适用字段 |
| FEI-T006 | 可选物模型选择、滚动分页 | `GET /device/template`；空绑定选项 | 复用；DM-API-007、DM-TPL-016、DM-FE-016 | `src/views/device/config-edit/index.vue:114-138,359-367`；候选原生绑定见第8节 | 仅选有效发布snapshot；分页全量可达；modelId/snapshotId/revision遵循唯一OpenAPI，候选旧wire大小写不作为兼容别名 |
| FEI-T007 | 模板详情与8类tab | 关联设备/物模型/协议/脚本/自动化/告警/扩展/设置 | 完整；DM-TPL-002、DM-FE-015 | `src/views/device/config-detail/index.vue:38-49,68-73,100-123` | 每tab有真实原生请求和独立加载/权限；无模型只隐藏依赖能力，不阻断基础模板 |
| FEI-T008 | 详情物模型绑定/解绑/创建入口 | `/device/template/menu`；PUT config.device_template_id | 复用；DM-TPL-016、DM-FE-016 | `src/views/device/config-detail/modules/attribute-info.vue:26,35,40` | 绑定固定已发布快照；“创建模型”进入既有模型服务；不能重写ThingModel核心 |
| FEI-T009 | provider分组列表和动态schema加载 | protocol/service；`/service/plugin/select`、`/protocol_plugin/config_form` | 完整；DM-TPL-007、015、DM-FE-016 | `src/views/device/config-edit/index.vue:224-257`；`config-detail/modules/connection-info.vue:280-310` | provider identifier、schemaVersion与执行实例分开；所有input/select/table字段按JSON schema服务校验 |
| FEI-T010 | 凭证类型选项 | `/device_config/voucher_type`，device_type/protocol_type | 完整；DM-TPL-015、DM-FE-016 | `src/views/device/config-edit/index.vue:260-277,420-432`；`connection-info.vue:312-322` | child device不显示不适用凭证；policy类型有明确枚举和约束；policy不携带实际secret |
| FEI-T011 | 详情协议/凭证只读、动态表单只读，但仍有Save函数 | select disabled；form edit=true；PUT/device_config | 完整；DM-TPL-004、DM-FE-016 | `src/views/device/config-detail/modules/connection-info.vue:261-267,363-387,416-418`；`form.vue:53,60,80,116,135-169` | 原生明确查看revision与编辑draft两种模式；只读页不提供假保存；不可变revision无法写 |
| FEI-T012 | Topic Mapping列表 | 页面仅传device_config_id、不传分页参数；provider meta `__topic_mapping__`控制显示 | 完整；DM-TPL-017、DM-FE-023 | `src/views/device/config-detail/modules/connection-info.vue:214-228,297-310,389-413`；`src/service/api/device.ts:569-571` | 列表绑定具体template revision/provider能力；禁能力时不吞存量规则；全量规则可追溯 |
| FEI-T013 | Topic Mapping创建 | UI mapping_name/direction/description/original_topic/target_topic/data_identifier/priority/enabled；wire name/source_topic | 完整；DM-TPL-017、DM-FE-023 | `src/views/device/config-detail/modules/connection-info.vue:71-80,233-255`；`src/service/api/device.ts:558-575` | topic pattern、方向、优先级、模型标识符校验；服务端禁止冲突；data_identifier必须出现在schema，旧TS接口漏该字段 |
| FEI-T014 | Topic Mapping编辑、启停、删除 | PUT/DELETE `/device/topic-mappings/{id}`；enabled在编辑表单 | 完整；DM-TPL-017、DM-FE-023 | `src/views/device/config-detail/modules/connection-info.vue:188-191,233-255`；`src/service/api/device.ts:577-582` | 改动形成新revision/明确发布流程；CAS和幂等；删除后runtime按generation应用，不双写旧表 |
| FEI-T015 | 脚本列表/搜索/类型筛选/分页 | A遥测上行/B遥测下行/C属性上行/D属性下行/E命令下行/F事件上行 | 完整；DM-TPL-018、DM-FE-024 | `src/views/device/config-detail/modules/data-handle.vue:36-64,228-248`；`src/service/api/device.ts:236-238` | 六阶段有唯一新枚举映射；按类型过滤准确；不把下行脚本当成放开任意ATTRIBUTE写入的理由 |
| FEI-T016 | 创建/编辑Lua脚本 | name/content/description/device_config_id/enable_flag/script_type/remark；encodeInp(msg,topic) | 完整；DM-TPL-018、DM-FE-024 | `src/views/device/config-detail/modules/data-handle.vue:67-94,274-300`；`src/service/api/device.ts:243-250` | 语言/入口/输入输出/大小/资源限制明确；代码版本固定在模板revision；未发布不能影响在线设备 |
| FEI-T017 | 脚本启用/停用 | PUT `/data_script/enable`，enable_flag Y/N | 完整；DM-TPL-018、DM-FE-024 | `src/views/device/config-detail/modules/data-handle.vue:266`；`src/service/api/device.ts:231-233` | 管理配置与runtime applied状态分开；重复启用幂等；停用后明确生效generation |
| FEI-T018 | 脚本删除 | DELETE `/data_script/{id}` | 完整；DM-TPL-018、DM-FE-024 | `src/views/device/config-detail/modules/data-handle.vue:274-300`；`src/service/api/device.ts:257-259` | 被发布版本使用不得破坏快照；仅删除draft或归档；并发和引用冲突可观察 |
| FEI-T019 | 脚本调试/上次输入和结果 | last_analog_input/analog_input/resolt_analog_input；POST `/data_script/quiz` | 运行时；DM-TPL-018、DM-FE-024 | `src/views/device/config-detail/modules/data-handle.vue:88-94,306-310,577`；`src/service/api/device.ts:253-255` | 沙箱执行有超时/无副作用/脱敏；输入、输出、错误可复现；超时脚本不能拖垮服务 |
| FEI-T020 | 扩展字段定义列表 | additional_info JSON数组；name/type/default_value/desc/enable | 完整；DM-TPL-019、DM-FE-025 | `src/views/device/config-detail/modules/extend-info.vue:30-37,73-85,99-102` | 与ThingModel字段明确分离；新结构化schema唯一key，不使用可改name作为唯一标识 |
| FEI-T021 | 扩展字段新增/编辑 | String/Number/Boolean；设备消费还接受Enum | 完整；DM-TPL-019、DM-FE-025 | `src/views/device/config-detail/modules/extend-info.vue:110-118,151-163`；`details/modules/message.vue:22-29` | 默认值按类型校验；旧Enum存量预检报告并保留可解释语义；变类型不兼容时由现有字段保护拒绝，不展示或泄漏设备值 |
| FEI-T022 | 扩展字段启停 | enable布尔；整个additional_info覆盖写 | 完整；DM-TPL-019、DM-FE-025 | `src/views/device/config-detail/modules/extend-info.vue:121-133` | 禁用仅停止新表单展示/输入或按已定合同处理；不静默删除历史设备值；CAS避免覆盖并发新增字段 |
| FEI-T023 | 扩展字段删除 | 从数组splice后PUT整个config | 完整；DM-TPL-019、DM-FE-025 | `src/views/device/config-detail/modules/extend-info.vue:136-149` | 有设备值时返回影响/冲突；受控清理策略有审计；旧快照仍可读取 |
| FEI-T024 | 模板关联设备列表/分页/详情 | GET `/device?device_config_id=...` | 完整；DM-DEV-001、DM-TPL-010、DM-FE-017、DM-FE-031 | `src/views/device/config-detail/modules/associated-devices.vue:67-70,173-178,196-210` | 按模板ID与revision可筛；显示desired/applied差异；权限过滤后的total一致 |
| FEI-T025 | 选择未绑定设备，滚动加载 | GET `/device/selector?has_device_config=false&page_size=30` | 完整；DM-DEV-001、DM-DEV-026、DM-FE-031 | `src/views/device/config-detail/modules/associated-devices.vue:126-144,162-168`；`src/service/api/device-template-model.ts:48-54` | 130条候选全可选；已绑定、不可见设备不会进入可绑集合；搜索分页不丢选择 |
| FEI-T026 | 模板批量关联现有设备 | PUT `/device_config/batch {device_ids,device_config_id}` | 完整；DM-DEV-016、DM-DEV-023、DM-FE-031 | `src/views/device/config-detail/modules/associated-devices.vue:88-108`；`src/service/api/device.ts:182-184` | 每设备影响与并发版本明确；批次原子/逐项语义合同固定；重复请求无重复变更；部分冲突不能假全成功 |
| FEI-T027 | 从模板解除设备关联 | 名叫deviceDelete但实际PUT `/device/update/config`，config为空 | 完整；DM-DEV-016、DM-DEV-017、DM-DEV-023、DM-FE-012、DM-FE-031 | `src/views/device/config-detail/modules/associated-devices.vue:185-193`；`src/service/api/device.ts:79-81` | 仅解绑模板不删设备；无模板后的协议/模型/凭证影响先预览；保留设备和历史身份 |
| FEI-T028 | 从模板详情创建设备 | route携带deviceConfigId | 完整；DM-DEV-003、DM-FE-004 | `src/views/device/config-detail/modules/associated-devices.vue:366` | 预选具体已发布revision；旧draft不可绑定；创建后回关联页正确计数 |
| FEI-T029 | 自动注册开关 | auto_register 0/1，一型一密文案 | 完整；DM-TPL-020、DM-RT-013、DM-FE-027 | `src/views/device/config-detail/modules/setting-info.vue:47,187-192,210-214,256-265` | policy显式包含允许注册条件/范围；模板禁用或政策关闭后不能产生设备；重复接入身份只注册一次 |
| FEI-T030 | 模板密钥复制 | template_secret从普通config响应读取 | 完整；DM-TPL-020、DM-FE-027 | `src/views/device/config-detail/modules/setting-info.vue:101-164,262-265` | 秘密与policy分开；读取/轮换/撤销需权限和审计；普通get/list/export均不泄漏；旧值迁移受控 |
| FEI-T031 | 在线超时与心跳策略 | other_config JSON；online_timeout分钟/heartbeat秒，UI互斥 | 完整；DM-TPL-004/007、DM-RT-002 | `src/views/device/config-detail/modules/setting-info.vue:48-55,96,178-184,268-293` | 新schema显式单位/互斥/范围；0含义明确；状态判断由runtime执行；改策略需revision与apply回执 |
| FEI-T032 | 模板封面上传与保存 | POST `/file/up` type=image，x-token；image_url写config | 完整；DM-TPL-021、DM-FE-027 | `src/views/device/config-detail/modules/setting-info.vue:62-80,222-229` | MIME/大小/所有权校验；asset ID明确生命周期；上传失败不写无效引用；不能绕tenant读他人图片 |
| FEI-T033 | 删除模板 | DELETE `/device_config/{id}`，确认 | 完整；DM-TPL-009、DM-FE-015 | `src/views/device/config-detail/modules/setting-info.vue:26-38,244-246`；`src/service/api/device.ts:170-172` | 有设备/产品/规则/快照引用按合同409；删除不级联设备/模型；过时ETag拒绝 |
| FEI-T034 | 发布模板到市场，登录/刷新token/确认 | market发布同时包含config协议/凭证政策+旧model/面板 | 运行时；market owner，DM-RT-010、DM-FE-018 | `src/views/device/config/index.vue:79-100,166-175,342`；`src/service/api/market.ts:11-32` | 仅导出允许字段/固定版本；不包含secret；服务不可用真实阻断；重复发布幂等 |
| FEI-T035 | 市场列表/筛选/详情/安装 | keyword/category/sort_by/page/page_size；template ID/version | 运行时；DM-RT-010、DM-FE-018 | `src/service/api/market.ts:35-57`；`manage/modules/add-devices-step1.vue:94-162` | 跨账号真实安装后模型/命令/图表完整且本地ID有效；旧包转换离线受控，不引入运行兼容 |
| FEI-T036 | 模板范围自动化入口 | config_id传scene linkage | 运行时；自动化owner，DM-RT-008 | `src/views/device/config-detail/modules/automate.vue` | 作用域使用新templateID与固定模型引用；设备切模板影响可预览 |
| FEI-T037 | 模板范围告警入口 | config_id传告警组件 | 运行时；告警owner，DM-RT-009 | `src/views/device/config-detail/modules/alarm-info.vue` | 新模板与设备命中集合一致；模型字段变化不能造成静默失效 |
| FEI-T038 | 新增复制/管理启停/发布历史/原生包文件导入导出 | 当前模板菜单未看到对应完备操作 | 完整；DM-TPL-005/006/008/011/012/013、DM-FE-017/018 | `src/views/device/config/index.vue`；`src/views/device/config-detail/index.vue`（与已有旧功能分开） | 作为用户目标的新功能验收；复制排除秘密；发布不可变；保存draft不升级设备；包往返digest一致 |

## 6. 设备分组菜单

| 库存 ID | 真实功能与字段 | 旧API | 处置 / 任务映射 | 精确源码证据 | 验收条件 |
|---|---|---|---|---|---|
| FEI-G001 | 根级分组列表、名称搜索、分页 | GET `/device/group`，parent_id=0/page_size10 | 完整；DM-GRP-001、DM-FE-019 | `src/views/device/grouping/index.vue:37-69` | root语义为null或合同值；搜索范围明确；输入loading禁用不造成旧响应覆盖 |
| FEI-G002 | 创建根/子分组 | name、description、parent_id | 完整；DM-GRP-003、DM-FE-019 | `src/views/device/grouping/index.vue:116`；`grouping-details/index.vue:153`；`grouping/components/add-or-edit-devices/index.vue:44-49,104-116` | 同父重名409；空名422；指定父不存在/越tenant404；失败保留表单 |
| FEI-G003 | 分组父级树选择 | GET `/device/group/tree`，前端添加合成根id0 | 完整；DM-GRP-002、DM-FE-019 | `src/views/device/grouping/components/add-or-edit-devices/index.vue:89-95,175` | 自身/后代不可选择且后端仍检测；树截断/懒加载明确；不信客户端去环 |
| FEI-G004 | 编辑名称/描述 | PUT `/device/group` | 完整；DM-GRP-004、DM-FE-019 | `src/views/device/grouping/components/add-or-edit-devices/index.vue:104-116,183` | 修正rules.Name与rules.name不匹配；CAS冲突可重试；不能先隐藏弹窗再把失败当成功 |
| FEI-G005 | 修改父级/层级调整 | 旧通用update可提交parent_id；无拖拽证据 | 完整；DM-GRP-005、DM-FE-020 | `src/views/device/grouping/components/add-or-edit-devices/index.vue:44-49,104-116,175` | 新move专用合同；A→B与B→A并发不成环；深度超限拒绝；失败恢复原树 |
| FEI-G006 | 删除根组/子组 | DELETE `/device/group/{id}` | 完整；DM-GRP-006、DM-FE-019 | `src/views/device/grouping/index.vue:84`；`grouping-details/index.vue:138`；`src/service/api/device.ts:63-65` | 非空/被规则引用返回明确冲突；不会删设备；重复请求和越tenant语义一致 |
| FEI-G007 | 详情名称、层级path、返回父组/根组 | GET group detail；tier.group_path | 完整；DM-GRP-001、DM-FE-019 | `src/views/device/grouping-details/index.vue:59-73,95,220-231` | 移动后完整path正确；根组/深层组导航稳定；路径不泄漏不可见祖先信息 |
| FEI-G008 | 子组列表和子组分页 | GET group?parent_id | 完整；DM-GRP-001、DM-FE-019 | `src/views/device/grouping-details/index.vue:105-130` | direct children和整子树区分；删除最后一项后页码正确，不残留旧组 |
| FEI-G009 | 分组成员列表、分页 | GET `/device/group/relation/list`，page_size5 | 完整；DM-GRP-007、DM-FE-021 | `src/views/device/grouping-details/index.vue:170`；`src/service/api/device.ts:83-85` | 正式合同区分直接/间接来源；多组设备去重；空列表把旧total与旧行清空 |
| FEI-G010 | 添加成员候选搜索/翻页/多选 | GET `/device`；旧在当前页客户端排除group_id | 完整；DM-DEV-001、DM-FE-021 | `src/views/device/grouping-details/modules/device-select-list.vue:22-30,67-75` | 应在服务器过滤已入组且total一致；跨页选择范围明确；不能用设备单group_id判断多组关系 |
| FEI-G011 | 批量加入成员 | POST relation {device_id_list,group_id} | 完整；DM-GRP-008、DM-FE-021 | `src/views/device/grouping-details/modules/device-select-list.vue:100-105`；`src/service/api/device.ts:96-98` | 1/100/101边界、跨tenant混入整批拒绝、重复添加无重复、失权设备不能加入 |
| FEI-G012 | 移除成员 | DELETE relation | 完整；DM-GRP-009、DM-FE-021 | `src/views/device/grouping-details/index.vue:198`；`src/service/api/device.ts:187-189` | 仅删关系，设备和其他组成员关系保留；详情反向关系同步 |
| FEI-G013 | 分组设置页 | 详情settings复用编辑组件 | 完整；DM-GRP-004/005、DM-FE-019/020 | `src/views/device/grouping-details/index.vue:340` | 与主列表编辑同一字段/权限/CAS，不形成另一路直接修改父节点的逻辑 |

## 7. 其他消费者（不能只切三个菜单留下旧依赖）

| 库存 ID | 消费入口 | 真实旧请求 / 数据关系 | 处置 / 任务映射 | 精确源码证据 | 原生回归要求 |
|---|---|---|---|---|---|
| FEI-X001 | 设备共享store | singleton deviceDetail | 完整；DM-FE-014/022 | `src/store/modules/device/index.ts:10-16` | 改生成客户端；按device+主体作用域缓存；子组件不再任取上次详情 |
| FEI-X002 | 移动端设备详情 | deviceDetail、deviceTemplateDetail；图表/遥测/属性 | 运行时；DM-RT-011、DM-FE-014 | `src/views/device-details-app/index.vue:7,105,127` | App与Web同deviceID、snapshot、单位、枚举；无旧detail/model API |
| FEI-X003 | 自动化场景条件编辑 | group tree、tenant设备、config菜单、device/config metrics | 运行时；DM-RT-008 | `src/views/automation/scene-edit/index.vue:165,184,202,221-223`；`src/service/api/automation.ts:4-29` | 规则选择器分页完整且同授权；条件字段固定模型语义；不写旧device_config_id |
| FEI-X004 | 联动动作编辑 | 同设备/模板/组选择；动作metrics | 运行时；DM-RT-008/006 | `src/views/automation/linkage-edit/modules/edit-action.vue:204,223,241,261-263` | 动作只选COMMAND；服务器校验snapshot，不复制前端推断 |
| FEI-X005 | 联动前提编辑 | 组/设备/模板条件 | 运行时；DM-RT-008 | `src/views/automation/linkage-edit/modules/edit-premise.vue:199,233,295` | 模型改版影响能追到具体规则前提；跨tenant组不得建立条件 |
| FEI-X006 | ThingsVis配置与运行时设备选择 | config→old model mapping、group tree、device lists、field cache | 运行时；DM-RT-011、005 | `src/components/thingsvis/ThingsVisAppFrame.vue:1512-1538,1565,1616,1854-1890,1927,2250,2317,2436-2437` | 配置与运行两条链都换新API；固定snapshot与资源digest；清缓存后依旧不回旧API |
| FEI-X007 | 通用指标选择器 | 设备/模板列表及字段 | 完整；DM-RT-011、DM-FE-014 | `src/components/device-selectors/DeviceMetricsSelector.vue:255,319` | 可读字段只来自正式模型schema；跨页面选择器共用生成client |
| FEI-X008 | 通用下发选择器 | 设备及command候选 | 运行时；DM-RT-006/011 | `src/components/device-selectors/DeviceDispatchSelector.vue:261,324` | 仅声明COMMAND；不能通过旧telemetry可写标记扩展控制能力 |
| FEI-X009 | 看板卡片创建/编辑/选择；本地看板模板兼容设备与实例创建 | deviceListForPanel→`/device/tenant/list`；model source/metrics；GET `/device/dashboard-templates/{id}/compatible-devices`；POST `/device/dashboard-templates/{id}/instances` | 运行时；DM-RT-011、005 | `src/components/panel/ui/card-form.vue:138,180`；`add-card.vue:139`；`card-selector.vue:142`；`src/service/api/panel.ts:24-32`；`src/service/api/dashboard-template.ts:133,148`；`src/views/visualization/thingsvis-template/index.vue:124,176` | 现有卡片source映射受控转换；选择器新ID；兼容设备绑定校验tenant、模型snapshot及资源digest，创建实例引用可追踪；历史数据留原owner |
| FEI-X010 | 系统看板设备统计/最新遥测/趋势/告警数 | `/board/device`、`/board/tenant/device/info`、`/device/telemetry/latest`、`/board/trend`、`/alarm/device/counts` | 运行时；DM-RT-011/009 | `src/service/api/system-data.ts:52,58,105,251,257` | 数量同正式元数据授权口径；最新/趋势归runtime；不能从旧table重算元数据 |
| FEI-X011 | 服务接入详情跳回设备列表 | route带service_identifier/service_access_id | 完整；DM-FE-003、DM-RT-001 | `src/views/device/service-details/index.vue:66` | 深链筛选可恢复；provider与instance均有权限验证 |
| FEI-X012 | 服务接入发现与模板菜单；已有服务实例的跨页选择、名称/模板/通信参数配置 | access/device/list、device_config/menu、batch导入；serviceConfigModal会回退取200个configs，不能误取model ID | 运行时；发现owner；DM-DEV-013/014 | `src/service/api/plugin.ts:52-66`；`src/utils/service-device-import.ts`；`src/views/device/service-details/index.vue:147-153,197`；`components/serviceConfigModal.vue:95-116,252-272` | 正式发现复用listServiceDiscoveredDevices（GET /api/v2/connection-providers/{providerId}/instances/{instanceId}/discovered-devices，device:discover，connector-runtime owner）；跨页选项不漏、无模板合法、名称及逐设备通信参数保留，非法JSON不提交；元数据创建走唯一正式API，重跑不双建 |
| FEI-X013 | 产品/预注册旧wrapper | `/product`、`/device/preRegister`、导出、`/device_config/{id}` | 待确认；DM-TPL-014、DM-RT-011 | `src/service/product/list.ts:3-15`；当前views/product仅locale文件 | wrapper存在但当前可达页面/调用方未发现；先查引用/后端真实消费者再决定保留产品/预注册流程，不能虚构当前产品菜单 |
| FEI-X014 | OTA设备选择旧wrapper | GET `/device`；OTA task/package接口 | 运行时；OTA owner，DM-RT-011 | `src/service/product/update-ota.ts:3-12`；`src/service/product/update-package.ts:3-7` | wrapper是否活跃需调用图/后端核实；若使用需新设备selector，OTA事实/执行不搬入metadata |
| FEI-X015 | 市场发布/安装依赖；组合包设备绑定/重绑、失败重试及补偿 | config协议+credentials policy+model+面板组合包；PUT install/{id}/bindings、POST retry/compensate；绑定向导仍用旧deviceList/deviceTemplate | 运行时；DM-RT-010 | `src/service/api/market.ts:19-32,51-57`；`src/views/device/config/composables/use-market-auth.ts`；`src/views/device/market/composables/use-device-binding.ts:154-204`；`src/service/api/market-bundle.ts:540-567`；`src/views/device/market/InstallationDetail.vue:235,254,265` | 市场owner调用同一正式metadata/model API；1000+候选完整分页、模型ID与接入模板ID不得混用；重绑部分失败明确、retry幂等、补偿不得删除已被引用模板/设备；旧包转换仅离线导入。confirmPublish/cancelDraft为NOT_IMPLEMENTED stub（market-bundle.ts:399-410），不作为已实现入口 |

## 8. 候选分支可复用证据（与当前页面严格分开）

| 库存 ID | 候选文件/符号证据（均为 F@ee5aab55） | 已有内容 | 限制与可执行验收 | 处置 / 映射 |
|---|---|---|---|---|
| FEI-N001 | `src/service/api/thingmodel-native-transport.ts:39-57,102` | ModelDraft、NativeProfile、资源、SnapshotDetail typed DTO；snapshotID/modelID/publishedRevision | 与唯一正式ThingModel OpenAPI逐字段对齐；候选modelID/snapshotID切换为modelId/snapshotId，不保留别名或另建模型owner；需真实HTTP | 复用；DM-API-007/009 |
| FEI-N002 | `src/service/api/native-template-binding.ts:15-25,28-38` | 从native读取模型/版本/快照；saveNativeThingModelBinding仍走旧config | 复用读/helper；写入改正式DeviceAccessTemplate draft/发布合同；旧PUT/device_config必须为零 | 复用；DM-FE-016、DM-TPL-016 |
| FEI-N003 | `src/service/api/native-template-binding-transport.ts:5-19` | 模型列表多页读取后过滤PUBLISHED | 125条含草稿/发布模型完整分页；版本列表不能硬限100；取消请求/失权不漏对象 | 复用；DM-FE-016 |
| FEI-N004 | `src/views/device/use-native-device-metadata.ts:24-45` | requestId竞态保护，snapshot字段/事件装饰 | A慢B快最后必须保留B；加载失败清理旧schema；依赖正式新device详情，不靠旧deviceDetail | 复用；DM-FE-010/022 |
| FEI-N005 | `src/views/device/native-device-model-contract.test.ts:136,191,204,241,279,298,340` | 固定snapshot/revision、commands-only writable、参数/枚举、属性/事件、重复标识、迟到响应测试源 | 测试源存在未运行；记录同候选SHA的结果；加真实API/UI网络场景才可标页面已切 | 复用；DM-API-007、DM-QA-001/004 |
| FEI-N006 | `src/service/api/thingmodel-native.test.ts:34,72,85,98,107` | auth/lang/idempotency、GET、401/409脱敏、不自动重试、UUID fallback测试源 | 统一正式错误合同和headers，避免客户端业务规则；记录运行结果与依赖版本 | 复用；DM-FE-001、DM-QA-001 |
| FEI-N007 | `src/service/api/native-template-binding-transport.test.ts:10,26` | 所有模型分页与stale total停止条件 | 不是全菜单E2E；应基于正式分页schema复用算法测试，不机械沿旧响应结构 | 复用；DM-FE-016、DM-QA-001 |
| FEI-N008 | `src/views/device/template/native-field-mapping.test.ts:9,23,36,61,88,100,111,139,149`；`native-model-resources.test.ts:6,15,23` | typed字段往返、flat event/command、枚举、stable key、顺序、拒绝非法字段、资源digest/空值/四类资源 | 复用既有物模型编辑器与资源映射；新增接入模板只引用发布snapshot，不重复开发模型核心 | 复用；DM-API-007、DM-TPL-016 |

### 当前工作区测试源

- `src/__tests__/unit/device-route-mapping.test.ts:4-5` 检查模板/物模型路由映射；不证明菜单已切新API。
- `src/__tests__/unit/device-readings.test.ts:15` 起覆盖布尔读数/命令匹配、隐藏transport metadata、枚举、HA映射等工具函数；该文件本身处于modified基线。不能推导整个密集读数页可运行或后端控制成功。
- `src/utils/service-device-import.test.ts` 是服务导入draft构造测试；不证明批量API、持久幂等和数据迁移已验收。
- 未在现有抓取范围找到三个菜单完整真实HTTP/浏览器E2E执行报告。所有测试只登记“测试源存在”，不登记“有通过证据”。

## 9. 已分配的前端任务（独立实现与验收边界）

下表已映射正式DM ID；不应只塞入“完成模板页面”一项。依赖均先完成OpenAPI schema/操作冻结与owner确认；发布阻断属性以TASKS.md为准。

| 前端任务键 / 正式DM任务 | 做什么 / 为什么 | 具体范围与依赖 | 可执行测试 / 验收 | 对应库存 |
|---|---|---|---|---|
| FE-TOPIC-LIST → DM-FE-023 | 接Topic规则分页和provider能力；保留可见配置 | 生成DTO、template revision、provider能力；只读列表 | 0/1/100+规则；无能力provider；越tenant404；数据字段全映射 | T012 |
| FE-TOPIC-EDIT → DM-FE-023 | Topic新增/编辑/启停/删除按draft+CAS | 单个rule表单、方向/topic/dataIdentifier/priority/enabled；服务校验反馈 | 重复priority/pattern、非法topic、stale ETag、重复提交；未发布设备不受影响 | T013-T014 |
| FE-SCRIPT-LIST → DM-FE-024 | 脚本六类列表/分页/筛选 | 新stage枚举、状态、revision | 六阶段逐一过滤；空页/失败/无权；不漏命令和事件阶段 | T015 |
| FE-SCRIPT-EDIT → DM-FE-024 | 代码编辑器与脚本draft增改删/启停 | 代码语言/入口/限制、CAS；引用旧发布版只读 | 语法/超长/引用冲突、同时编辑、失败保留内容；发布前runtime不变 | T016-T018 |
| FE-SCRIPT-EVAL → DM-FE-024 | 调试输入/运行结果界面 | 正式沙箱evaluation、requestId、超时、无副作用标志 | 正常转换、异常、死循环超时、脱敏、快速换脚本迟到结果隔离 | T019 |
| FE-PROFILE-DEF → DM-FE-025 | 扩展字段定义CRUD及启停 | 独立profile schema、stable key、3类+存量Enum、defaults、revision | typed默认值、同key冲突、改类型影响、删除被用字段冲突 | T020-T023 |
| FE-PROFILE-VALUES → DM-DEV-025 / DM-FE-025 | 设备位置和扩展值表单 | typed coordinates、profile values与snapshot读取；CAS | 坐标±90/180边界、空/默认值、Number/Bool/Enum、模型字段不会混入 | D021 |
| FE-TEMPLATE-MEMBERS → DM-FE-031 | 关联设备及批量绑定/解绑 | templateID/revision筛选、无模板selector、批量preview/token/ETag/operation | 跨页选100、批内1冲突、重试、解绑保留设备；只显示真实apply状态 | T024-T028 |
| FE-REGISTRATION → DM-FE-027 | 自动注册政策和注册凭证维护 | policy开关与独立引导凭据list/create/revoke权限；轮换显式create、受控交付、revoke；revision | 只读用户看不到secret；复制可审计；禁用policy后runtime不注册 | T029-T030 |
| FE-ONLINE-POLICY → DM-FE-016 / DM-FE-027 | 心跳/超时策略表单 | typed单位/互斥范围、draft/desired/applied | 0/极值/同时填两者、并发冲突；runtime结果与政策一致 | T031 |
| FE-TEMPLATE-ASSET → DM-FE-027 | 封面上传与引用 | 正式asset owner上传/download授权，metadata仅asset ID | 错MIME/超限/跨tenant/上传中取消/保存失败孤儿清理 | T032 |
| FE-STATE-HISTORY → DM-FE-034 | 在线状态历史弹窗 | runtime时间范围、状态、分页、新错误结构 | 无数据/unknown、DST/UTC边界、翻页撤权、503不同于离线 | R018 |
| FE-DIAGNOSTICS → DM-FE-035 | 诊断统计与故障记录 | window/方向/stage、可信runtime指标、错误显式 | 真实模拟故障、成功率分母0、500不显示demo成功 | R019 |
| FE-DEFERRED → DM-FE-033 | 延期COMMAND创建/队列/取消 | 复用control owner，旧telemetry/attribute按已决策转换；状态机 | pending→sent/expired/cancelled；取消与投递竞态唯一终态；跨tenant隔离 | R008/R011/R014/R015 |
| FE-RUNTIME-DELETE → DM-FE-037 | 当前遥测/属性值删除界面 | 仅经正式runtime delete合同，独立授权/确认/审计 | 删除当前值不删模型/历史；失败不假删除；引用id跨设备不可用 | R004/R010 |
| FE-SIMULATOR → DM-FE-036 | 模拟器配置/初始化/发送 | 确定保留边界后，受控runtime target、payload schema、权限 | 只能向授权测试目标发送；合法消息在读取页出现；超时/非法目标不发送 | R022 |
| FE-HISTORY-EXPORT → DM-FE-039 | 历史曲线/表格导出 | 遥测owner正式范围/聚合/download合同 | 当前筛选125点导125点；时间/单位正确；撤权/过期下载拒绝 | R005-R007 |
| FE-CONSUMER-* → DM-RT-008—011 / DM-FE-014 | 每个消费入口独立切换而非统一改wrapper即算完 | X001-X015逐组件、route、owner、测试记录 | 浏览器/自动化真实请求无受禁旧API；mock成功不作最终证据 | X001-X015 |

## 10. 发布验收需要补充的证据与待核项

1. 当前报告是静态代码证据。须在最终候选SHA上采集完整操作HAR/截图/断言、正式API结果与数据库读回，同一候选执行CLI和runtime闭环。
2. `F@ee5aab55` 的原生物模型测试路径有2026-10-23历史核对记录；本轮只重新采集API/helper及其测试源，N004/N005/N008的views文件尚未重取，全部仍需在最终候选实际执行；本报告没有声称通过。新计划不能把候选旧PUT/device_config保留下来作迁移过渡。
3. 产品/预注册和OTA目前只看到旧wrapper、product locale，尚未找到当前可达产品/OTA页面。主规划须结合后端消费者和真实菜单配置判断活跃范围，不能把dead wrapper当已用菜单或自行删除。
4. 脚本、Topic、扩展schema、批量模板绑定、注册密钥、在线政策、封面、延期队列、状态历史、诊断、模拟器和当前值删除必须各有正式operation或明确owner合同，不得靠泛化的“runtime-owned”一行漏掉验收。
5. 没有新增业务“不迁移”批准。本审计不擅自省略旧功能；若保留某项的产品边界仍未定，正式DECISIONS须注明影响、建议、负责人和发布阻断条件。



## 11. 2026-10-25 当前源码复核与增量修正

本轮对三菜单路由、API wrapper、真实页面调用、动态菜单/角色守卫、表单和批量关系、网关/子设备、分组树/成员、模板协议/脚本/Topic/扩展字段、诊断/状态/告警以及跨菜单消费者进行了静态复核。对全部 `src` 文本做设备/模板/分组调用方检索，并实读上表相关关键路径；这证明源码调用链，**不等于逐页面执行139项验收，也不证明服务端已运行**。

| 复核项 | 当日结论与改动 | 追溯 |
|---|---|---|
| 请求和菜单 | 三菜单仍走Community旧请求；baseURL说明修正为配置/代理优先、缺省同源，不臆定当前部署环境 | FEI-C001—007、D001、T001、G001；request.ts:7-12 |
| 角色与按编号入口 | 角色类型为SYS_ADMIN/TENANT_ADMIN；只读/资源限制属于能力维度；按编号入口仍disabled，处置改待确认以与DM-DEC-004一致 | FEI-C006、D016 |
| 不可达或占位 | server抽屉代码仍在，但当前handler只跳集成页；子设备详情user tab仅空占位；不把函数或组件存在算可用功能 | FEI-D017、D026；details/modules/user.vue:9-12 |
| 已有页面范围纠正 | 子设备当前表单改地址、不提供换父选择；Topic列表当前没传page；debug日志当前没传offset；告警路径补齐info段 | FEI-D028、T012、R021、R024 |
| 原库存消费者细节补全 | 子设备独立详情、已有服务实例跨页配置、看板模板兼容设备/实例创建、组合包绑定/重试/补偿合入已有行，不新增平行模块 | FEI-D026、X009、X012、X015；对应DM-FE/RT任务 |
| 命名混淆再次出现 | 市场绑定向导用deviceTemplate获取旧物模型并叫localTemplates，设备映射读template_id/device_template_id；新消费者必须使用明确定义的设备、接入模板、模型快照ID | FEI-X015；use-device-binding.ts:154-204 |
| 候选边界 | 重取的native-template-binding.ts:1,28-38仍调用deviceConfigEdit；候选views N004/N005/N008本轮未重取，保留历史证据日期 | FEI-N002、N004、N005、N008 |

本轮保留139个稳定库存ID。修订是已有库存的精度纠正和消费者子场景补全，不增加虚假的已完成项。角色、页面请求与模块关系可以由源码确认；线上真实菜单配置、服务端授权、运行时状态、候选测试通过、数据迁移与发布仍必须按TEST-PLAN收集证据。
