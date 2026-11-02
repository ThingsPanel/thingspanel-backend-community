# Encore / CLI / 物模型实际源码盘点

初次盘点：2026-10-23；本轮源码复核：2026-10-25。当前事实已用 `source-current/K`（main工作区）与 `source-current/K@37fbb5e8`（独立候选ref）重新阅读；关键文件SHA256与本轮source manifest一致，Git/工作区快照见 [REVIEW-2026-10-25.md](REVIEW-2026-10-25.md)。没有运行产品测试、数据库迁移或部署。测试名称只证明测试源码存在，不证明当前制品已经通过测试或上线；候选物模型也不等于主checkout已切换。

## 1. 对规划影响最大的结论

1. 当前 `devicemetadata` 有设备、模板、产品、分组基础 CRUD；但这些 API 仍位于 `/api/...`，不是本次 `/api/v2/...` 合同，响应为裸对象或 `items/page/page_size/total`。仅文件存在不代表前端已经切换。
2. 当前 `l3-core/devices/service.go` 是明确的旧系统代理，`/v1/devices/*` 为 `encore:api public`，依赖 `internal/legacy.NewFromEnv()`；`command-identifiers` 先读 formal metadata，出错后退回 legacy。它不是第二个应该保留的设备 owner。
3. 当前模板、设备创建/修改以及模板发布/换模型都调用 `project*Runtime` / `refresh*RuntimeProjection`；`legacy_projection_port.go` 的默认实现为 `legacyMetadataProjectionAdapter`。这些属于本次禁止延续的旧事实投影链，不能因为封装成接口就称原生直切。
4. 候选 `37fbb5e` 的 `thingmodel/core` 与 `thingmodel/app` 已有独立语义、CAS、发布、资源快照、事务审计/Outbox/幂等收据和测试源码，应复用；新工作主要是选择候选、统一原生 transport 合同、消除旧 owner 及闭合消费者，不能复制核心。
5. 当前 `device_templates` 的 publish 只是模板状态变为 PUBLISHED；配置和模型 snapshot 指针仍可被 update/bind 改写。它没有本次需要的不可变“接入模板版本”事实。不可将现有物模型 snapshot 当成已有接入配置 snapshot。
6. CLI 只有分组子命令直接调用现有 `/api/device-groups`；常用设备命令仍调用 `/v1/devices` 代理，模板仅有 `project-runtime`。正式 CLI 是必做项。`cli/tpctl/README.md` 声明当前是 shell launcher + Go 单一实现，不能凭猜测说它仍是 Python。

## 2. 命名、字段与数据库

| 对象 | 源码事实 | 规划处置 |
|---|---|---|
| `DeviceTemplate` | `K/encore-api/devicemetadata/models.go:91`：id、tenant_id、name、brand、product_name、model_number、thing_model_id、thing_model_snapshot_id、protocol_type/config、voucher_type、connection_type、auto_register、online_config、tags、extras、description_i18n、image_url、status、timestamps | 演进为唯一 DeviceAccessTemplate owner；接入配置是版本化值对象，不新增平行模板 CRUD |
| 固定模型引用 | `store.go:createDeviceTemplate` 要求 model ID 和 snapshot ID 非空，且校验 snapshot 属于该租户和指定模型 | 现有校验可复用；合法未建模配置所需 nullable 是明确新增行为，不能声称当前已支持 |
| 模板变更 | `store.go:updateDeviceTemplate` 仅阻止 ARCHIVED，允许 PUBLISHED 修改；`bindThingModelVersionAdvanced` 同样仅挡 ARCHIVED；`publishDeviceTemplate` 检查 protocol config 后更新状态 | 必须新增不可变接入版本、显式设备切版与 runtime generation，保留原协议规则的可用部分 |
| 产品 | `models.go:124 Product` 含 device_template_id、product_key、sku、image_url、ota_package_id、description_i18n、deleted_at | 产品反向引用模板；模板中的 product_name 只是描述，不能直接当 Product ID 或倒置关系 |
| 设备 | `models.go:148 Device` 含模板必填、可选product、sn/name、明文voucher、is_online、activate_flag、parent_device_id、创建/激活/删除时间 | 新元数据表移除online和明文凭据事实；模板/模型可空需要有意演进；运行观测与凭据owner必须明示 |
| 表空间/数据库 | `devicemetadata/db.go` 声明 Encore 数据库名 `device_metadata`；SQL 是未限定 schema 的表名；`thingmodel/db.go` 使用 `sqldb.Named("device_metadata")` 复用同资源 | 不能据目录名猜独立 schema；迁移账本阶段核对实际 search_path/schema；核心与元数据表共库不意味着复制 owner |
| 当前迁移 | 2026-10-25当前K复制中有0001–0013，包括0009 group、0010 runtime binding、0011 name、0012 runtime projection outbox、0013 idempotency | 需引用复制时文件状态；不要只沿用初始0001–0011清单 |
| 候选迁移 | K@37fbb5e：0001–0007、0014_typed_thing_model_items、0015_native_model_commands、0016_thing_model_resources | 先对账已应用checksum和缺号，不改历史编号、不重新创建已存在表 |
| FK | 当前0001多数关系是单列UUID FK，0009 parent_id/group_id/device_id也不是租户复合FK | API租户检查不能替代目标复合唯一/FK；新约束和源数据预检是发布门禁 |

## 3. 当前入口与复用/退役范围

| 功能 | 当前实际入口 | 状态与处置 | 证据 |
|---|---|---|---|
| 设备CRUD | POST/GET `/api/devices`；GET/PUT/DELETE `/api/devices/:id` | 基础代码可复用，合同/权限/运行依赖待改 | K `service.go:451` 起、`store.go:1432` 起 |
| 命令校验投递 | POST `/api/devices/:id/actuate` | 当前metadata直接调用MQTT，不能保留为新并行control owner | K `service.go:518`；`Actuate`、`mqtt.go` |
| 最新值 | GET `/api/devices/:id/latest` | 当前来自进程MQTT缓存，不等于正式历史/最新遥测owner | K `service.go:587` |
| 接入模板CRUD | POST/GET `/api/device-templates`；GET/PUT/DELETE `/:id` | 复用字段和规则；配置版本/幂等/权限未满足新合同 | K `service.go:242` 起 |
| 模板生命周期 | POST `/:id/publish`、`archive`、`derive-draft`、`bind-thing-model` | publish无配置不可变版本；换snapshot会改模板共用指针 | K `store.go:942/1025/1075/1145` |
| 模型使用模板查询 | GET `/api/thing-models/:thingModelID/device-templates` | 可参考引用查询，不能视作全部消费者引用保护 | K `service.go:363` |
| 产品 | `/api/products` CRUD；`/api/product-keys/:productKey`；`/api/templates/:templateID/products` | 复用唯一产品owner，三菜单只新增必要selector合同 | K `service.go:376` 起 |
| 分组CRUD | POST/GET `/api/device-groups`；GET/PUT/DELETE `/:id` | 候选代码存在；缺角色/资源级授权证据、CAS和正确树行为 | K `group_service.go:11` 起 |
| 树/成员 | GET `/api/device-group-tree`；GET `/api/device-groups/:groupId/devices`；GET `/api/devices/:deviceId/device-groups`；POST/DELETE `/api/device-groups/:groupId/devices/:deviceId` | 单关系幂等可参考；批量、子树成员、移动并发约束须补 | K `group_service.go:70` 起 |
| onboarding | POST `/api/device-onboarding` | 同时直接新建模型、snapshot、模板、设备及provider/legacy映射并投影旧runtime；不得原样当目标单设备创建 | K `onboarding.go:139–257` |
| runtime projection | POST `/api/device-templates/:id/runtime-projection`；GET/POST `/api/devices/:id/runtime-projection` | 本次明确退役，不改名留下桥接层 | K `runtime_projection.go:40/58/77`、`legacy_projection_port.go` |
| 旧系统迁移入口 | `/api/migrate/from-legacy-template/:legacyTemplateID`、`device-topic/:deviceID`、`rollback/:migrationID`、`history`、`all-legacy-templates` | 仅审计旧转换行为；目标应是受控离线runner/正式native写入，不长期暴露兼容接口 | K `migration_service.go` |
| 快捷控件 | `/api/custom-command-controls` GET/POST/PUT、DELETE `/:id` | 当前独立旧控件schema需核对产品保留/转换，不能伪装为新物模型核心已覆盖 | K `custom_control_service.go` |
| l3代理 | `/v1/devices/detail/list/list-simple/create/update/delete/activate/...` | 全部被影响消费者需直切正式API；不保留代理服务 | K `l3-core/devices/service.go` |

## 4. 物模型候选的九个原生HTTP操作

以下为 `K@37fbb5e/encore-api/thingmodel/service.go` 原样路径，均标记 `encore:api auth`。目标规划可统一 transport 字段与envelope，路由不必重造。

| 方法 | 路径 | 请求/响应 | 关键行为 |
|---|---|---|---|
| POST | `/api/v2/thing-model-commands/create` | CreateRequest → app.Result | `Idempotency-Key` header；model完整草稿，可带resources |
| POST | `/api/v2/thing-model-commands/update` | UpdateRequest → app.Result | modelID、expectedDraftRevision、完整model；拒绝fieldOperations；同时改resources须expectedResourcesRevision |
| POST | `/api/v2/thing-model-commands/publish` | PublishRequest → app.Result | modelID、expectedDraftRevision、changelog、可选expectedResourcesRevision；冻结语义与资源快照，consumerState=PENDING |
| GET | `/api/v2/thing-models` | ListRequest → ModelList | query筛名称；kind/lifecycle虽有query字段但非空会拒绝；page默认1/pageSize20，上限200且静默截断 |
| GET | `/api/v2/thing-models/:modelID` | → ModelDetail | 返回草稿、draftRevision、lifecycle、nullable publishedRevision、presentationRevision；当前Get未赋presentationRevision，默认0，不能当作已正确接线 |
| GET | `/api/v2/thing-models/:modelID/versions` | VersionsRequest → VersionList | 版本summary含publishedRevision、snapshotID、checksum、createdAt、changelog |
| GET | `/api/v2/thing-models/:modelID/versions/:publishedRevision` | → SnapshotDetail | 固定模型和固定资源，同一Publication；不能退回最新资源 |
| GET | `/api/v2/thing-models/:modelID/resources` | → ResourcesResponse | 当前资源草稿，不是某历史发布版本 |
| POST | `/api/v2/thing-model-commands/update-resources` | UpdateResourcesRequest → app.Result | modelID、expectedRevision（0合法且不得与缺失混同）、resources；独立资源CAS |

本目录未发现原生 export/import HTTP 端点。发布版本响应可以用于原生导出包组装，但不能将拟新增包schema写成当前已实现API。

### 4.1 精确字段及语义

- `ModelDraft`：name；可选descriptionI18n（default/locales）、category；显式fields数组（可为空，不可省略）。
- `ModelField`：kind = TELEMETRY/ATTRIBUTE/EVENT/COMMAND；identifier保留原字节，不做大小写/字符“清理”；nameI18n；可选descriptionI18n；access含read/write/observe三个必填bool；unit；sortOrder。TELEMETRY/ATTRIBUTE必须有valueType且无parameters；EVENT/COMMAND必须有parameters且无valueType。
- `Parameters`：schemaVersion固定1，items为扁平Parameter数组；Parameter含identifier、nameI18n、可选descriptionI18n、valueType。类型没有 required/default 等不存在的核心字段，不应自行添加。
- `ValueType.kind`：INT/FLOAT/NUMBER/BOOL/STRING/ENUM。ENUM的transport `baseKind`使用NUMBER/BOOLEAN/STRING；BOOLEAN映射核心BOOL。enumValues[].code必须是真实number/bool/string，与baseKind一致，不能全部转字符串。
- `ModelDetail`原wire：modelID、draft、draftRevision、lifecycle、publishedRevision、presentationRevision。`ModelSummary`原wire：modelID、name、lifecycle、draftRevision、publishedRevision、updatedAt。
- `SnapshotDetail`原wire：modelID、publishedRevision、snapshotID、checksum、createdAt、changelog、model、resources。resources含modelID、revision、profile、webChartConfig、appChartConfig。
- `ResourceProfile`：author、tags、cover、version、brand、modelNumber。资源属于presentation/catalog层，不进入语义core的Field；当前chart payload只被验证为object/null且最大256KiB，未知图表字段不能凭想象编造。
- 语义snapshot checksum由`core/snapshot.go:BuildSnapshot`生成，hash包含model_id、tenant_id、name、description、category、revision、排序后fields；**不包含图表资源**。资源有独立`app.ChecksumResources`及DB `resources_checksum`。当前SnapshotDetail没有暴露resourcesChecksum，需要仅扩展transport读取已存在值。
- `app.Result`原wire：status、lifecycle、modelID、draftRevision、可省略publishedRevision、consumerState、可省略checksum、replayed、可省略resourcesRevision。它不是运行时设备命令receipt；不能把thing-model-commands路径理解成实际设备命令投递。

### 4.2 认证与公共合同差异

`service.go:245 principalForData`从`authn.CurrentData()`取user/tenant和角色。ADMIN角色映射read/write/publish/delete；TENANT_USER/TENANT_VIEWER只read，未知角色即使Scopes=["*"]也不自动写。`canonicalIdentity`保留完整原始身份用namespace SHA1 UUID映射。core只接收可信Principal，检查租户与capability，不能从客户端body直接取tenant。

现有响应无data/meta envelope，错误使用Encore `errs`（Unauthenticated、PermissionDenied、NotFound、Aborted、InvalidArgument、Internal）；与目标Problem和统一分页有差异。这些可列transport任务，无须改写core/app。

### 4.3 可复用测试源码（本轮未运行）

| 测试文件 | 已见测试场景 |
|---|---|
| `thingmodel/core/domain_test.go` | 四类字段、类型化命令参数、identifier原样、同kind唯一、typed枚举、空模型snapshot、snapshot深拷贝、授权与semantic checksum |
| `thingmodel/transport_test.go` | typed fields/COMMAND参数往返、拒绝fieldID/metaItems、缺access bool、空数组合法、资源revision缺失vs0、身份不碰撞、不信任未知角色scope、分页溢出 |
| `thingmodel/app/application_test.go` | 创建更新发布版本、租户/权限、幂等同参重放异参冲突、CAS、失败回滚审计事件收据、资源空tags保持 |
| `thingmodel/postgres_test.go` | 真实PG repository测试源码：create/publish/replay/tenant、资源CAS及发布snapshot、资源+模型事务、outbox失败回滚、过期receipt tombstone、并发仅一次/CAS单写者、旧row不可见、snapshot不能UPDATE |

## 5. 已发现的现有实现风险

| 风险 | 代码证据 | 应转化的验收 |
|---|---|---|
| 当前tenant正规化会碰撞，且与候选核心不一致 | K `models.go:314 normalizePrincipalID`删除非hex字符后pad/truncate；候选`canonicalIdentity`全原串hash | 取不同原tenant但相同hex投影的对照数据，必须不同目标tenant；旧已写tenant键有持久映射与预检，不能直接换算法丢访问 |
| 当前API缺角色/资源级授权接线证据 | devices/templates/groups handlers只调用mustTenantID；独立middleware目录不证明被接到这些请求 | 对viewer、受限service、跨tenant、只读scope执行各写API，逐一断言拒绝 |
| 独立ParseJWT不验签 | K `middleware/auth.go:ParseJWT`仅base64解payload；未在本轮看到该函数被上述handler使用 | 不得直接复用为正式auth；正式边界必须证明验签/issuer/audience/exp/撤销。不要误报它必然是当前HTTP认证链 |
| 分组环检查方向反了 | K `group_store.go:validateDeviceGroupParent`从被移动组沿祖先向上查询，判断目标parent是否出现 | A为B祖先时A移到B应拒绝；B移到A应允许合法结果；并发A↔B不能成环 |
| 分组构树依赖输入顺序 | `buildDeviceGroupTree`先将node值复制进roots/children，后续追加子节点不回写已复制值 | 父先子后、子先父后、三级任意排列输出相同完整树；现group_test只测child先parent，另一个测试把孤儿提升为root，不能作为新严格规则证据 |
| 非原子删除引用检查 | 模板delete先查devices/products数量再soft-delete，分组delete先查children/members再更新 | 删除与新增引用并发必须只得到串行合法结果；跨owner引用纳入同一受控协议 |
| current onboarding与command fallback越界 | `GetFormalDeviceCommands`按identifier跨多个身份列查询，没有tenant参数；l3 public caller失败回legacy | 正式control只接受可信租户+规范deviceId，范围限定；无fallback，模型为空应明确错误/能力结果 |
| 当前snapshot结构与新核心不同 | 当前devicemetadata有早期model/items/snapshot实现；候选thingmodel有native入口 | 不并列保留两个核心写入口；引用使用同一候选native core的快照，旧表/row仅迁移源 |

## 6. CLI现状

| 范围 | 实际行为 | 新任务缺口 |
|---|---|---|
| 入口/运行时 | `cli/tpctl/README.md`声明Go-only；shell launcher执行`cli/tpctl-go` | 核验并保留薄shell启动器；不需要凭空规划Python迁移 |
| base/认证 | `main.go:17`默认TP_ENCORE_BASE=`http://127.0.0.1:4000`；`authHeaderValue:956`依次TP_ENCORE_TOKEN/TP_AUTH_TOKEN/TP_TOKEN，添加Bearer | profile、可信tenant会话、令牌过期/撤销、版本/digest检查需实现 |
| 设备 | main.go:34常用get/list/create/update/delete/activate/check-number/selector/online-status/update-voucher/control/command-logs/command-identifiers走`/v1/devices/*` | 全部直切同一正式v2 API；不可因base为Encore地址称已原生 |
| 分组 | main.go:157调用`/api/device-groups`、`/api/device-group-tree`及成员路径 | 有代码可复用客户端结构，字段/路径/错误/分页/CAS/批量仍需新合同验收 |
| 模板 | main.go:193仅device-templates project-runtime | 全CRUD、版本、导入导出、模板设备绑定均未见正式命令 |
| HTTP输出 | call:307使用http.DefaultClient，无timeout；非2xx统一error，main用exit2和JSON code500；printResp解包raw字符串 | 定义稳定退出码，stdout/stderr、超时/取消、Problem、幂等键、ETag、JSON null/类型等 |
| 测试 | main_test.go只有4个onboarding payload解析用例 | 不能当三菜单CLI HTTP/权限/端到端证据；需真实API+PG+runtime E2E |



## 7. 2026-10-25 当前源码复核与增量

### 7.1 当前身份与授权链，不能混淆独立示例和正式入口

|已确认源码事实|精确证据|实施/验收影响|
|---|---|---|
|实际Encore认证入口是AuthHandler；默认legacy-jwt核验HMAC-SHA256签名和HS256算法|`K/encore-api/l1-access/authn/auth.go:36–78,97–133`|不能把另一个middleware包的仅decode函数说成当前主链“不验签”。复用现有正式身份owner，补合同缺口。|
|当前claims仅id/tenant_id/authority/exp；exp≤0没有被拒，未见issuer/audience检查；注释明确不查Redis注销/踢出记录|`auth.go:81–97,141–146`|DM-API-002须覆盖缺exp、过期、错误issuer/audience、注销/冻结；由可信会话决定tenant，不由body/header指定。|
|dev与passthrough仅记录warning，passthrough授予tenant_admin和`*`|`auth.go:61–75`|正式发布配置必须拒绝这些模式；不能拿本地dev token测试结果作为生产认证通过。|
|metadata设备/模板/分组handlers只有mustTenantID调用，没有这些handler中的角色/资源授权检查|`devicemetadata/service.go:242–363,451–516`、`group_service.go:11–138`、`models.go:290–295`|DM-API-003应逐操作验证action+resource scope；导入/批量/父子关系也相同。独立RBAC示例不能代替实际接线。|
|当前tenant规范化删除非hex后截断/补零；已有单元测试甚至断言这种映射|`models.go:314–348`、`identity_normalization_test.go:14–30`|例如`tenant-admin`与`tenant/admin`均只剩`eaad`，静态可证碰撞；迁移先建立旧键映射与异常报告，不能直接改算法使既有资源失联。|
|候选模型从正式authn Data生成Principal，仅映射明确角色；原始身份完整namespace hash|`K@37fbb5e8/encore-api/thingmodel/service.go:244–270`、`transport_test.go:122–142`|复用模型core的可信Principal边界；不能为了菜单再复制核心授权。候选内TENANT_USER/VIEWER兼容分支不表示Community今日有这些产品角色。|

### 7.2 分组的两个真实算法缺口和一个CLI字段缺口

1. `group_store.go:17–51` 的递归从被移动group沿parent向上，检测新parent是否在旧祖先中。这会拒绝合法的“B移动到已有祖先A”，却不能阻止“A移动到自己的后代B”。DM-GRP-005需验证子孙方向及并发A↔B；仅自环检查不足。
2. `group_store.go:162–179` 把节点值先复制到roots/children，之后向map中的父对象追加子节点不会回写先前复制的值。父先于子出现时root可能没有children，三级树也会受输入顺序影响。`group_test.go:5–23`只覆盖child先parent，`:25–32`还明确把孤儿提升为root。DM-GRP-002需全排列、三级树、孤儿异常、稳定排序测试；当前两测试源码不能算完整验收。
3. CLI `main.go:157–164,179–183`发送`pageSize/name/parentId`，而服务`models.go:20–23,177–181`读取`page_size/query/parent_id`。现有分组命令虽直连Encore，也有参数错位证据。DM-CLI-010/002/015须让分页、名称与父组过滤真实影响HTTP请求及服务结果，不能只测试命令解析或静态help。新合同统一lowerCamel字段，不保留这组旧query拼写。

### 7.3 幂等、数据库与运行时引用的复用边界

- 当前`idempotency.go:27–100`仅给metadata actuate可选body key；空key直接放行，过期或FAILED会把同key重置IN_PROGRESS再执行（`:74–86`）。这不同于目标“所有有副作用写必带header key、过期键不重新执行”规则，也不同于候选模型30天tombstone。DM-API-005可复用持久账本思路，不能直接将现状泛称已达全模块幂等。
- `store.go:854–883`模板删除先查设备/产品，再另行soft-delete；`group_store.go:219–240`分组删除同样是先查children/members后更新。源码没有把并发新增引用与删除串行化；新增引用协议及同库写锁/约束属于必要发布任务。
- 当前`0009_device_groups.up.sql:4,21–23`虽有tenant列和(tenant,id)唯一键，parent/group/device FK仍仅单列ID；`0010_device_runtime_bindings.up.sql:5–9`明确存legacy_template/config/device ID。新数据库不能原样保留该projection事实模型。
- `legacy_projection_port.go:39–66`默认实例仍为`legacyMetadataProjectionAdapter`，最终调用`internal/legacy.ProjectMetadataTemplate/Device`。`service.go:256,294,319,353,465,503`的创建/变更链仍触发projection；这不是可被改名保留的原生执行链。
- `onboarding.go:319–337`的`GetFormalDeviceCommands`只有identifier参数，按device UUID/SN/provider_entity_id/legacy_device_id的OR查询，没有可信tenant输入；`l3-core/devices/service.go:146–158`是public入口，formal失败再legacy回退。DM-RT-006/012应改为正式受信租户+规范deviceId+固定模型版本，禁止残留fallback。
- 对本次复制的当前devicemetadata与候选thingmodel Go/SQL全文检索并阅读正式handlers，未见`metadata_reference_edges/fences`或reserve/confirm/renew/release原生生命周期实现。已有模板引用查询/单列FK与模型receipt不是这套跨owner协议。DM-API-011应补在已有metadata owner内，不能因已有查询误标已验收；此结论仅限本次核查源码范围，不声称检索了所有未提供仓库。

### 7.4 物模型复用候选仍成立

`K@37fbb5e8`九个auth路由已逐条重新核对，见§4；核心/应用/存储不是空目录。`core/snapshot.go:15–27,40–83`维护tenant/capability与独立语义快照；`app/application.go:270–296,486`冻结独立资源内容/摘要；`postgres.go:41–121`在事务中锁定同scope幂等收据，30天后保留tombstone，不重复执行；`:466`同时保存语义与资源snapshot；`:543`同事务写模型outbox。`db.go:6–7`仍是`device_metadata`单一现有数据库资源，避免新建另一模型数据库/owner。

可复验的具名测试源码包括：

|已有源码测试|覆盖目的|不证明的内容|
|---|---|---|
|`core/domain_test.go:63 TestValidateFieldPreservesIdentifierVerbatim`；`:175 TestBuildSnapshotNeedsNoTemplateAndCreatesNextRevision`|标识符保真、模型不依赖模板|不证明当前三菜单已使用此core|
|`app/application_test.go:167 TestIdempotencyReplayPayloadConflictAndCAS`；`:300 TestFailureRollsBackModelAuditEventAndReceipt`|CAS/幂等及业务+审计+事件+收据事务|本轮未执行，不计通过率|
|`transport_test.go:122 TestIdentityDoesNotCollapseDistinctLegacyIDsOrTrustScopes`|身份不碰撞、不因未知role+scope放权|不代替生产JWT验签/注销/issuer测试|
|`postgres_test.go:65 TestPostgresResourcesCASIdempotencyTenantAndPublishedSnapshot`；`:336 TestPostgresConcurrentRequestsCreateOnceAndCASSelectsOneWriter`；`:413 TestPostgresPublishedSnapshotCannotBeModified`|真实PG测试源覆盖固定资源版本、并发单写者、不可变snapshot|不把测试源存在当当前候选数据库/HTTP/页面E2E通过|

现有Get仍未给`ModelDetail.PresentationRevision`赋值（`service.go:142–153`），版本响应仍未暴露资源独立摘要；这些是transport接线任务。语义checksum不含图表，不能把它宣称为整个发布包digest。该复用候选不能与main当前早期`devicemetadata`模型CRUD拼成两个并行写owner。

### 7.5 CLI及历史进度的结论

`cli/tpctl/README.md:1–17`与shell launcher确认Go-only入口；`main.go:34–91`设备仍走`/v1/devices/*`，模板仅`:193–197`的project-runtime，`:307–340`使用无显式timeout的DefaultClient并将非2xx原始body转错误。`main_test.go:5–62`仅4个onboarding参数解析测试；没有本次三菜单正式客户端端到端通过证据。

`METADATA_API_PROGRESS.md:5–20`虽写78/78，同时明确这只是历史接口登记，并仍提出兼容连接器切换；`2026-09-17-device-metadata-strangler-handoff.md:53–80`仍以兼容连接器/投影为目标。二者作为历史行为参考，受本轮禁止桥接与旧运行fallback的明确要求取代；不可据其状态标为今日原生完成。
