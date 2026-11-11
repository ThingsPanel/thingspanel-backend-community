# 原生设备元数据：数据库定义与一次性迁移

> **当前设计优先约束（2026-10-27用户纠正）：设备模板不做草稿、版本表、发布或模板切版；只有物模型保留版本。** 此前相反的候选设计已撤回。原188/580/108是历史规划规模，不能再作为拒绝收简的约束；接口/DB/CLI/用例正按新边界重写。详见[设计纠偏与影响检查](DESIGN-CORRECTION-2026-10-27.md)。相关旧测试仅保留历史证据，不证明新需求完成。


> 2026-10-25已按当前三仓源码重新复核；本轮增量、Git状态与证据边界见[REVIEW-2026-10-25.md](REVIEW-2026-10-25.md)。原2026-10-23证据保留其历史日期。唯一目标合同已修订为2.0.0-plan.4；内部引用协议见API-CONTRACT §10.1。

> 版本：2026-10-23 规划基线。本文定义**目标数据库合同**，不表示这些迁移已编写、已应用或已发布。本轮只读源码并编写文档，未连接业务数据库、未执行 SQL/迁移、未启动应用。正式 wire 合同以 [openapi.yaml](openapi.yaml) 为准；术语见 [DOMAIN-MODEL.md](DOMAIN-MODEL.md)，拆工见 [TASKS.md](TASKS.md)，验证见 [TEST-PLAN.md](TEST-PLAN.md)，未决产品事项见 [DECISIONS.md](DECISIONS.md)。

## 1. 结论、现状和实施边界

推荐沿现有 Encore `device_metadata` 数据库演进：复用 `devices`、`products`、分组基础和已完成的 ThingModel 核心；把现有 Encore `device_templates` 演进为唯一的 `device_access_templates`；维护单份当前模板配置、正式设备接入绑定、网关关系和必要持久作业能力。**不建立第二套模板/模型 owner，不建立旧 ID 日常解析器，不继续 runtime projection 到 Community 旧表。**

以下是不同含义的数据，必须分别保存：

|数据|唯一 owner / 保存位置|本轮处理|
|---|---|---|
|设备身份、显示资料、管理启停、激活事实、产品关系|`devicemetadata` / `devices`|原生目标表；管理启停不等于在线状态。|
|可复用当前接入配置、设备期望绑定|`devicemetadata` / 模板与绑定表|单份configuration包括协议、连接方向、Topic Mapping、脚本、资料字段定义；无模板草稿/版本/发布，实际执行不在此库。|
|语义模型、字段/命令参数、发布快照、Web/App 展示资源|既有 `thingmodel` / 现有模型表|复用已纳入Kernel main-1.3.0的`37fbb5e`核心和同一数据库迁移账本；只做合同、租户约束与必要读映射强化。|
|网关父子关系、分组树及成员|`devicemetadata` / 关系表|关系只保存一份；设备不另存 `parent_device_id`、`group_ids` 或重复模板字段。|
|凭据用途、校验材料、受保护可恢复秘密、有效期|`devicemetadata` / 统一 `device_credentials`|设备凭据与模板引导凭据共用存储能力；运行时验证通过正式合同访问。|
|已应用配置 generation、连接、在线/离线、最近活动、连接历史、调试日志|`device-runtime` / 自己的运行账本|不迁入元数据事实表。元数据作业可保存引用和确认凭证，不拥有当前运行状态。|
|遥测/属性当前值与历史、事件、实际命令及延期队列、回执、OTA 观察、告警|各既有 telemetry/control/OTA/alarm owner|不为“补全表”迁入元数据库；正式 API 负责同租户授权及结果组合。|
|模板图片、导入原文件、导出结果文件|既有文件存储 owner|元数据仅持有资产 ID/不透明对象引用、摘要与生命周期；短期签名 URL 不入永久事实。|

### 1.1 可复核的代码基线

源证据使用以下仓库简称；行号对应本轮核验源码，不把工作区内容冒充 HEAD 原样内容。

|简称|仓库与证据|本轮能证明什么|
|---|---|---|
|B|Community 后端 `main`，HEAD `0e3ba8281f09c815dabcd03c634d17af845e950b`；`internal/model/device_configs.gen.go:14–32`、`device_templates.gen.go:14–31`、`devices.gen.go:14–52`、`products.gen.go:14–27`；详细清单见 [INVENTORY.md](INVENTORY.md)|旧菜单“设备模板”由 `device_configs` 承载协议/凭据/接入配置；旧 `device_templates` 承载物模型与图表。只证明功能与数据来源。|
|K|Encore 当前 `main`，HEAD `c9817436b7f0f41588542a97236517545b5f1d01`；`encore-api/devicemetadata/db.go:5–7`、`migrations/0001_init.up.sql` 至 `0013_idempotency_records.up.sql`|现有数据库定义与迁移源码存在；不能据此认定现场数据库已应用。|
|K37|候选 `37fbb5e`；`encore-api/thingmodel/db.go:5–8`、`postgres.go:22–27`、`migrations/0014_typed_thing_model_items.up.sql`、`0015_native_model_commands.up.sql`、`0016_thing_model_resources.up.sql`|ThingModel 原生仓库复用 `sqldb.Named("device_metadata")`；模型核心、收据、独立资源摘要已有代码。候选分支不同于当前工作区；本轮未执行其测试。|

当前 K `0001` 的设备/产品仍强制模板，模板仍强制模型和快照；设备含 `is_online`/`voucher`，`0010` 含旧配置/设备 ID 的 projection 关系。它们与本目标不一致，必须以追加迁移和明确数据转换解决，不能保留作运行时回退。`0009` 分组已有根名唯一索引，但关系 FK 只按 ID，须补同租户复合约束。`0013` 幂等键没有完整 subject/path 作用域，不能直接宣称满足新合同。

**发布前置任务：** DM-DB-001 先读取真实数据库迁移账本和 checksum，区分空库、当前主线库、已接入候选核心的库。不得按“本地最大编号为 13”假定所有库均未应用 14–16；后续迁移号由该任务分配，本文不抢占实际编号。

## 2. 全库规则与公共列包

### 2.1 类型、默认值与身份

所有表使用 PostgreSQL；部署实际主版本与 Encore 支持范围在 DM-DB-001 固定，DDL 不依赖未核验的新版本特性。下列 `DEFAULT` 是数据库缺省，`—` 表示**无默认值，写用例必须明确提供**。`NULL` 仅表示未知/无关联，不用 `''`、`0`、全零 UUID、虚构默认产品或空模型替代。

|规则|目标合同|
|---|---|
|ID|业务 UUID 由服务端分配；迁移已有合法 Encore ID 时保持身份，其他来源通过永久映射取得目标 ID。客户端不能指定任意目标 ID。唯一例外是模板聚合内部的 ConfigurationItemId：Topic/脚本条目可由客户端生成 UUID，身份范围是 templateId+itemId，不能当跨资源授权 ID。|
|租户|业务表 `tenant_id UUID NOT NULL`，无默认值；来自验证后的认证上下文。后台作业由受托服务身份和冻结作用域携带，不能接受 body 中任意租户值。|
|用户/服务主体|新元数据 `created_by/updated_by` 为受信 principal 的不透明 `VARCHAR(128)`，不能使用旧 `normalizePrincipalID` 的截断/去字符算法构造新身份。现有 ThingModel UUID actor 的映射先核对，见 §8。|
|字符串|除明确的数值/单位转换，不自动小写、trim、改字符集。业务编号大小写敏感；拒绝首尾空白及控制字符。业务长度与 OpenAPI 一致；现有 ThingModel 字段遵循它已实现的 UTF-8 字节规则。|
|时间|`TIMESTAMPTZ`；`NOW()` 为同一事务时间。API 输出 RFC 3339 UTC、毫秒精度；数据库可保留更高精度，不用客户端时间覆盖服务端审计时间。|
|版本|资源 CAS 用 `BIGINT`，有效范围 `1..9007199254740991`；初始化 1；一次实际变更只递增一次，幂等重放或无变化不递增。ThingModel 草稿/发布/资源 revision 按 §6 既有语义，不能套用另一计数器。|
|JSONB|只接受对应 OpenAPI/provider schema 的对象或数组，拒绝未知字段、非有限数值、额外秘密和超限内容；空对象/空数组合法的字段明确给默认值。`JSONB` 不是任意字段垃圾箱。|
|租户 FK|单库关联一律 `(tenant_id, foreign_id) → (tenant_id,id)`；被引用表建立 `UNIQUE(tenant_id,id)`。复合 FK 防跨租户拼接，RLS/资源授权另行实施。|
|删除|资源默认软删除；业务 UUID 不复用。当前设备编号即使对应 tombstone 仍保留占用。关系显式解除可物理删行，但必须同事务审计；不可变版本禁止 UPDATE/DELETE。|
|跨 owner|不跨数据库伪造 FK；引用目标通过 owner 的受限正式合同验证与登记。删除必须先 fence，见 §5.1。相同数据库中的 ThingModel FK 仍不授权 metadata 直接写模型。|

#### 公共列包 E：可变业务实体

引用 `E` 的表包含下列**全部**列；表格只列该表额外字段。

|字段|SQL 类型|可空 / DEFAULT|约束与含义|
|---|---|---|---|
|`id`|UUID|否 / —|PK；服务生成。|
|`tenant_id`|UUID|否 / —|`UNIQUE(tenant_id,id)`；认证提供，不默认系统租户。|
|`created_at`|TIMESTAMPTZ|否 / `NOW()`|创建后不可改；迁移使用已核实源时间，缺失的来源必须记入迁移审计。|
|`updated_at`|TIMESTAMPTZ|否 / `NOW()`|受控更新触发器在实际变更时赋 `NOW()`。|
|`created_by`|VARCHAR(128)|否 / —|受信主体；迁移写真实迁移服务主体，不捏造源用户。|
|`updated_by`|VARCHAR(128)|否 / —|受信实际修改主体。|
|`resource_version`|BIGINT|否 / `1`|CHECK 正整数且不超过 JSON 安全上限；CAS 后递增。|

#### 公共列包 D：可软删除实体

|字段|SQL 类型|可空 / DEFAULT|约束与含义|
|---|---|---|---|
|`deleted_at`|TIMESTAMPTZ|是 / `NULL`|仅删除用例完成所有前置步骤后填写。|
|`deleted_by`|VARCHAR(128)|是 / `NULL`|与 `deleted_at` 同空或同非空；不在普通 PATCH 可写字段内。|

`E+D` 不等于可以直接把 `deleted_at` 设值：设备须 runtime 隔离/撤销确认，模板/产品/组须引用保护；具体规则见各表。无另述时 `ON DELETE RESTRICT`，不使用宽泛 CASCADE 清除其他 owner 的事实。

#### 公共列包 A：不可变记录

`id UUID PK`、`tenant_id UUID NOT NULL`、`created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`、`created_by VARCHAR(128) NOT NULL`、`UNIQUE(tenant_id,id)`。除定义中允许的派送状态/过期清理字段，不更新内容。记录 `tenant_id` 无默认值。

### 2.2 数据库权限与并发

运行角色不得拥有 DDL、关闭触发器、BYPASSRLS 或迁移表写权限。事务开始通过服务内部 `SET LOCAL` 写受信 tenant 上下文；连接复用后不得残留上次租户。RLS 的 `USING` 与 `WITH CHECK` 同时限定 tenant，缺失/无效上下文失败关闭；启用 `FORCE ROW LEVEL SECURITY` 或使用明确不拥有表的应用角色，不能把“建了 policy”当作生效证据。后台 worker 按一个租户领取与执行，不使用用户可控 `SET`。

FK 只保证存在，不能保证目标未删除或某个 deviceKind。写用例先按固定顺序锁定目标实体及删除 fence，检查 live/type/scope，再写关系。关键跨行不变量以约束触发器/共享事务锁补强；服务测试与直接 SQL 负向测试都要覆盖，避免两请求各自“先查后写”通过。

CAS 更新形式为同租户、未删除、`resource_version=expected` 的条件更新；0 行不能一律当“找不到”，由已授权读状态区分 404 与 412。HTTP/业务前置语义见 API-CONTRACT；ThingModel 已有 revision 命令冲突仍是 409。

## 3. 业务实体与关系字段字典

### 3.1 `devices` — 设备身份和管理事实

Owner：`devicemetadata`。列包：`E+D`。演进现有 `devices`；设备、产品、接入模板均可独立存在。任务：DM-DB-003、DM-DEV-003/004/005/006/007/021/025。

|字段|SQL 类型|可空 / DEFAULT|约束 / API 含义|
|---|---|---|---|
|`device_number`|VARCHAR(128) COLLATE "C"|否 / —|`UNIQUE(tenant_id,device_number)` 包括已删除行；非空、无控制符/首尾空白；`deviceNumber`。|
|`name`|VARCHAR(128)|否 / —|1..128；可重复，`name`。|
|`device_kind`|VARCHAR(16)|否 / —|`standalone/gateway/subdevice`；创建后不由普通 PATCH 更改。|
|`product_id`|UUID|是 / `NULL`|同租户 FK `products`；不隐式绑定产品默认模板。|
|`description`|VARCHAR(2000)|是 / `NULL`|空字符串与 NULL 保持 API 语义。|
|`labels`|JSONB|否 / `'{}'`|最多 32 项；key 1..64、`[A-Za-z0-9_.-]`；value 字符串≤256。|
|`tags`|JSONB|否 / `'[]'`|≤100 个唯一非空字符串，每项≤128；旧逗号标签不会自动变成键值 labels。|
|`custom_fields`|JSONB|否 / `'{}'`|≤100 项标量/NULL；按当前模板configuration的字段定义校验；无模板时仅 `{}`。|
|`location`|JSONB|是 / `NULL`|结构为 latitude/longitude/address；坐标成对非空或成对 NULL，范围±90/±180；仅地址合法。|
|`batch_number`|VARCHAR(128)|是 / `NULL`|批次标签，非批操作主键。|
|`administrative_state`|VARCHAR(16)|否 / `'enabled'`|`enabled/disabled`，期望管理状态；实际连接不落此列。|
|`activation_state`|VARCHAR(16)|否 / `'inactive'`|`inactive/active`；只有正式激活确认或经核验迁移才能为 active。|
|`activated_at`|TIMESTAMPTZ|是 / `NULL`|新激活确认必须填真实时间，inactive 必须空；来源已激活但时间确实不可考时保留NULL与下列来源标识，不填当前时间伪造历史。|
|`activation_time_source`|VARCHAR(24)|否 / `'not_activated'`|not_activated/runtime_confirmation/source_record/source_unknown；内部来源审计字段，不可由公共PATCH设置。active且时间NULL仅允许受控迁移确认的source_unknown。|

不再保留可写 `voucher`、`is_online`、`device_template_id`、`parent_device_id`、`topic_profile=legacy`。模板关系来自 `device_access_bindings`；网关来自 `gateway_memberships`；`modelBinding` 从模板当前选择的已发布物模型版本派生；`runtimeRef` 只是正式绑定中的稳定外部引用。

索引：`(tenant_id,created_at DESC,id DESC) WHERE deleted_at IS NULL`、`(tenant_id,device_kind,created_at DESC,id DESC)`、`(tenant_id,product_id)`、`(tenant_id,administrative_state,activation_state)`、`(tenant_id,batch_number)`；名称搜索依据 DM-DB-012 实测选择 B-tree 前缀或获准的 trigram，不能靠无上限 `%...%` 全表扫。标签按实际过滤用法配置 GIN，避免给所有 JSONB 无差别建索引。

删除：父网关仍有孩子、自动化/市场等有效引用、未完成配置作业均冲突；所有 credentials 撤销并由 runtime 确认隔离后才 tombstone。显式删除分组成员；保留身份、凭据撤销摘要、审计及历史所需稳定 runtime 引用。**不级联删除 telemetry、event、command、attribute 或连接历史**。B `internal/service/device.go:424–540` 的旧删除历史行为不复制。

### 3.2 `device_number_reservations` — 异步改号的待生效占用

Owner：`devicemetadata`。不是第二份当前编号；只保存改号目标，成功后移除，当前事实仍仅 `devices.device_number`。任务：DM-DEV-021、DM-DB-003/010。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`tenant_id`|UUID|否 / —|PK 第 1 部分。|
|`reserved_number`|VARCHAR(128) COLLATE "C"|否 / —|PK 第 2 部分；与设备编号相同校验。|
|`device_id`|UUID|否 / —|同租户 FK；`UNIQUE(tenant_id,device_id)`，一个设备至多一项未完成改号。|
|`operation_id`|UUID|否 / —|同租户 FK `metadata_operations`；唯一；原操作可重入。|
|`created_at`|TIMESTAMPTZ|否 / `NOW()`|首次预留时间。|
|`expected_device_version`|BIGINT|否 / —|受理时设备 CAS 版本。|

创建、改号、预留、完成和删除预留都取得同一 `(tenant,number)` 事务锁；涉及两个编号时按 UTF-8 字节顺序取锁，跨表约束触发器在锁内检查 `devices` 与 reservation 的互斥。仅两张表各有 UNIQUE 不能解决“创建抢到预留号”的竞态，必须测试。预留**不按墙钟自动释放**：runtime 结果未知时保持占用并恢复原 operation；只有 runtime 明确未应用/已补偿才释放。成功在一事务替换当前编号、递增设备版本、移除预留和记录审计，deviceId 与历史稳定身份不变。改号失败旧号仍有效，不能遗留两个当前号。

### 3.3 `products` — 复用可选产品目录

Owner：`devicemetadata` 现有产品目录；本次仅选择/详情与关系完整性，不新增整套产品管理项目。列包 `E+D`。任务：DM-DB-011、DM-TPL-014、DM-MIG-006。

|字段|SQL 类型|可空 / DEFAULT|约束 / API|
|---|---|---|---|
|`name`|VARCHAR(128)|否 / —|显示名称，不作为模型身份。|
|`product_key`|VARCHAR(128)|是 / `NULL`|非空值租户内唯一；引导注册在可信凭据租户内查找。旧 NULL tenant 不凭空归属。|
|`sku`|VARCHAR(128)|是 / `NULL`|目录编码。|
|`product_type`|VARCHAR(64)|是 / `NULL`|旧 type 分类，不等于 deviceKind。|
|`model_code`|VARCHAR(128)|是 / `NULL`|商业型号，不是 ThingModel ID。|
|`image_asset_id`|UUID|是 / `NULL`|文件 owner 引用；API `imageUrl` 读时解析，不持久化短期 URL。|
|`ota_package_id`|VARCHAR(256)|是 / `NULL`|OTA owner 的不透明引用；无跨库 FK，不复制升级包。|
|`description`|VARCHAR(2000)|是 / `NULL`|目标单值描述；旧 i18n 多语言不可无声丢失，转换规则须登记。|

索引：`UNIQUE(tenant_id,product_key) WHERE product_key IS NOT NULL`、live 名称/创建时间；产品默认模板如由既有owner提供，仅作为显式选择建议，不引入模板版本字段。目标 product_key 为NULL或非空字符串；旧空字符串显式转换为NULL并记录来源，不造可注册的空key。保留已删除 product_key 不复用，避免动态注册命中另一产品。删除时已有 device.product_id 或外部有效引用则阻断；不能因删目录而自动改设备或模板。

### 3.4 `device_access_templates` — 单份当前接入配置

Owner：`devicemetadata`，列包E+D。用户菜单设备模板对应旧Community device_configs；物模型对应旧device_templates，不能混用。当前实施以0028/0029追加迁移为准，已应用历史迁移不回写。

|字段|SQL类型|可空/默认|含义|
|---|---|---|---|
|`name`|VARCHAR(128)|非空|租户live名称唯一。|
|`description`|VARCHAR(2000)|可空|显示信息。|
|`brand`|VARCHAR(64)|可空|模板目录信息。|
|`product_name`|VARCHAR(128)|可空|显示文本，不生成产品。|
|`model_number`|VARCHAR(64)|可空|目录型号，不生成物模型。|
|`image_asset_id`|UUID|可空|正式文件资产引用，不存短期URL。|
|`tags`|JSONB|默认[]|最多32个唯一标签，每个1..64字符。|
|`administrative_state`|VARCHAR(16)|enabled|enabled/disabled，管理状态不等于运行在线。|
|`configuration`|JSONB|新建必填|完整当前TemplateConfiguration，直接保存；历史NULL必须显式离线转换，不能从MAX旧revision猜。|
|`thing_model_id`|UUID|可空|由configuration.model精确选定的物模型身份；与snapshot同空。|
|`thing_model_snapshot_id`|UUID|可空|由选定publishedRevision精确解析的不可变物模型snapshot，不是模板版本。|

CAS复用resource_version防覆盖，不是模板版本表。deviceCount按同tenant、授权范围、live设备及template_id绑定去重求值。模板显式改选物模型版本后，关联设备共享新选择；仅发布新物模型版本不会自动改选模板。索引按live名称/目录/绑定查询保留，不保存易漂移计数。

### 3.5 已撤回的模板版本表

`device_access_template_revisions`已由0028删除，`draft`已重命名为configuration。此处不再保留可供实施的草稿、发布、历史版本或切版设计。0029还删除了设备绑定上的独立model_id/model_snapshot_id副本，统一由模板当前选择读取。物模型自身的版本与snapshot完整保留。

### 3.6 `device_access_bindings` — 唯一设备期望接入绑定

Owner：`devicemetadata`。每台设备恰好一行，即使尚未配置接入；不另建“服务设备”表。任务：DM-DB-008、DM-DEV-016/017/023/024、DM-RT-001/007。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`tenant_id`|UUID|否 / —|复合 PK/FK。|
|`device_id`|UUID|否 / —|PK `(tenant_id,device_id)`；同租户 FK devices。|
|`template_id`|UUID|是 / `NULL`|同租户 FK 当前设备模板；无模板版本引用。|
|`direct_access`|JSONB|是 / `NULL`|完整 DirectAccess：access/externalDeviceId；无模板服务/协议设备合法。|
|`device_parameters`|JSONB|否 / `'{}'`|API deviceParameters；≤100 项、64KiB，按锁定 provider 的 deviceParameterSchema 验证；与共享 access.parameters 分域存储，不合并覆盖。|
|`configuration_generation`|BIGINT|否 / `1`|同设备单调增长，包含模板、直接配置、管理状态、凭据和网关等影响接入的变更。|
|`pending_operation_id`|UUID|是 / `NULL`|同租户 FK operation；同设备影响配置的变更串行仲裁。|
|`runtime_provider_id`|UUID|是 / `NULL`|正式 provider owner 的稳定 ID 引用。|
|`service_instance_id`|VARCHAR(256)|是 / `NULL`|runtimeRef.serviceInstanceId 的已验证稳定引用；service_connector 必需，其他模式按 provider 能力允许空。期望配置中的access.serviceInstanceId来自模板当前configuration或direct_access。|
|`runtime_namespace`|VARCHAR(128)|是 / `NULL`|provider 确认的身份空间，包含服务实例所需区分；不是客户端自由拼接 tenant。|
|`runtime_external_id`|VARCHAR(256)|是 / `NULL`|provider 确认的稳定身份；改 display number 不擅自重建此身份。|
|`created_at`|TIMESTAMPTZ|否 / `NOW()`|首次登记。|
|`updated_at`|TIMESTAMPTZ|否 / `NOW()`|配置关系变更。|
|`updated_by`|VARCHAR(128)|否 / —|受信主体。|

`template_id`与`direct_access`至多一个非空；两者皆空表示合法未配置状态，只可 inactive，激活时返回 configuration_incomplete。绑定响应 resourceVersion 取 devices.resource_version，不另造第三个 CAS 版本。`modelBinding`仅从模板当前选择的已发布物模型版本读出；directAccess 不偷偷引入另一套物模型绑定路径。

runtime 的 provider/namespace/external_id 三列全空或全非空；三列全空时 service_instance_id 也必须空；非空时 service instance 是否必需按固定模式校验。这是验证后的稳定身份**引用**，不是 online/applied generation 的缓存。`directAccess.externalDeviceId` 是请求的实例外部编号；确认后必须与 provider 解析出的身份关系一致。provider 实例和 schema 都须同租户可用。相同外部编号在不同实例可合法存在，同实例不能对应两台目标设备。正式身份用两个唯一条件索引：instance非空时 `UNIQUE(runtime_provider_id,service_instance_id,runtime_namespace,runtime_external_id)`；instance为空时 `UNIQUE(runtime_provider_id,runtime_namespace,runtime_external_id)`，均仅对 runtime_external_id 非空生效。这样既避免 SQL NULL 使唯一性失效，也防同一真实身份跨租户二次认领，冲突响应不泄露其他租户。未确认前由同一正式 provider 身份预留/登记提供跨设备排他保证，operation持久其receipt，不以无约束 JSON 重复导入。

索引：`(tenant_id,template_id,device_id)`、`(tenant_id,runtime_provider_id,service_instance_id,device_id)`；针对期望配置的 protocol/provider/serviceInstance 过滤使用合同固定 JSON 路径表达式索引，或由同一配置生成的只读查询投影；不得另建人工可写同义配置列。模板设备通过template_id JOIN当前configuration获取期望配置；runtimeRef 展示已确认身份引用。按实例查询必须使用正式 serviceInstanceId/namespace，不能再使用旧 `svc-...` hash 字符串解析规则。device_parameters 在预览和提交时都按**目标**固定 provider 的独立 schema 验证，模板切换不沿用无效旧参数；无接入配置时只允许空对象。

切换事务更新 desired 绑定和 generation、设备 resourceVersion、审计、operation/outbox；runtime 去重应用相同 generation，晚到旧 generation 拒绝。作业只有取得对应 generation+配置摘要的真实确认才能 succeeded。runtime 当前 applied 状态在 runtime；此库仅保存作业确认收据供核验，不能据 outbox delivered 推断已应用。

### 3.7 `gateway_memberships` — 支持多级网关的邻接关系

Owner：`devicemetadata`。无 E/D；当前关系单独保存，解除显式删行，审计保留。任务：DM-DB-008、DM-DEV-018/019/027、DM-MIG-008。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`tenant_id`|UUID|否 / —|复合 PK/FK。|
|`child_device_id`|UUID|否 / —|PK `(tenant_id,child_device_id)`；同租户 devices FK。|
|`gateway_device_id`|UUID|否 / —|同租户 devices FK；不能等于 child。|
|`subdevice_address`|VARCHAR(128) COLLATE "C"|否 / —|1..128；`UNIQUE(tenant_id,gateway_device_id,subdevice_address)`。|
|`created_at`|TIMESTAMPTZ|否 / `NOW()`|关系首次建立时间。|
|`updated_at`|TIMESTAMPTZ|否 / `NOW()`|父级/地址改变时间。|
|`updated_by`|VARCHAR(128)|否 / —|主体。|

父必须 live gateway；子可 gateway 或 subdevice，不能 standalone。同 tenant 树变更取得树级事务锁，递归检验祖先/后代、自环、双父和最大深度 32（根为第 1 层）；绑定已有子树须检查**移动后所有后代**的深度。provider 不支持多级时返回明确能力错误。B `service/device.go:820–870` 允许网关子节点，不能把目标建成仅叶子关系。

索引：`(tenant_id,gateway_device_id,child_device_id)` 及地址唯一键。解除后没有关系行，API gatewayId/address 都为 NULL；不能保留 parent=NULL、address 非空半关系。设备删除不隐式迁移孩子；必须先显式解绑/迁移。批关系变更整批先做授权/环/地址预检，再逐项持久执行，部分失败有 item 结果；不得声称跨 runtime 全批原子。

### 3.8 `device_groups` 与 `device_group_members`

Owner：`devicemetadata`。复用 `0009`，将 `device_group_devices` 演进为业务明确的 `device_group_members`；切换后唯一写入口。任务：DM-DB-006、DM-GRP-001..010。

`device_groups` 使用 `E+D`：

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`parent_id`|UUID|是 / `NULL`|同租户自身 FK RESTRICT；NULL 为根，不能用 `'0'`。|
|`name`|VARCHAR(128)|否 / —|非空；同父 live 名称唯一。|
|`description`|VARCHAR(2000)|是 / `NULL`|显示资料。|
|`sort_order`|INTEGER|否 / `0`|0..2147483647；同顺序以 id 稳定排序。|

根节点使用 `UNIQUE(tenant_id,name) WHERE parent_id IS NULL AND deleted_at IS NULL`；非根使用 `UNIQUE(tenant_id,parent_id,name) WHERE parent_id IS NOT NULL AND deleted_at IS NULL`，避免 NULL 使根唯一性失效。索引 live `(tenant_id,parent_id,sort_order,id)`、live `(tenant_id,created_at DESC,id DESC)`。不持久化可漂移的 tier/path/descendant_count；`path/pathComplete` 从同一授权树查询。directDeviceCount仅本组直接成员，descendantDeviceCount仅后代组（不含本组）按deviceId去重；includeDescendants成员查询另对本组与后代的并集去重。`activeAlarmCount/alarmsObservedAt` 从正式alarm owner读取并标观察时间，不入分组表，也不把不可用当0。

移动操作以同租户组树锁防并发互为父，检查最大深度 32、子树循环与父已删除状态。删除默认要求无 live 子组、无直接成员、无外部有效引用；不自动级联丢设备或整个子树。旧全租户组名唯一放宽为同父唯一，是明确新规则，迁移前仍报告历史重复/归并影响。

`device_group_members`：

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`tenant_id`|UUID|否 / —|复合 PK/FK。|
|`group_id`|UUID|否 / —|同 tenant groups FK RESTRICT。|
|`device_id`|UUID|否 / —|同 tenant devices FK RESTRICT。|
|`created_at`|TIMESTAMPTZ|否 / `NOW()`|加入时间。|
|`created_by`|VARCHAR(128)|否 / —|实际添加主体。|

PK `(tenant_id,group_id,device_id)`；反向索引 `(tenant_id,device_id,group_id)`。一设备可多组，重加同一关系 no-op，移除不存在关系 no-op，但均须先授权。一次成员请求最多 100，预检所有设备/组再原子提交；不能先添加合法前半再因越权失败。组成员变更递增组 resourceVersion，不因加入组修改设备接入。旧“只有一个根组时自动加入”的行为由 DM-DEC-004 冻结，不能用数据库 trigger 默默延续或默默取消。

### 3.9 `device_credentials` — 统一设备凭据和模板引导凭据

Owner：`devicemetadata`。列包 `E`，无 D；撤销是业务状态，历史摘要保留。任务：DM-DB-007、DM-DEV-009/010/011、DM-TPL-020、DM-RT-013、DM-DEC-002。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`device_id`|UUID|是 / `NULL`|同 tenant devices FK；与 template_id **XOR**。|
|`template_id`|UUID|是 / `NULL`|同 tenant templates FK；与 device_id XOR。|
|`purpose`|VARCHAR(32)|否 / —|`device_access/template_enrollment`；必须与 XOR owner 一致。|
|`kind`|VARCHAR(32)|否 / —|`token/username_password/x509`；模板 enrollment 当前只允许 token。|
|`status`|VARCHAR(16)|否 / pending|设备 `pending/active/revoked`；模板引导仅 `active/revoked`，无默认 active 绕过 runtime 应用。|
|`public_identifier`|VARCHAR(256)|是 / `NULL`|可公开 client/user 标识，不能写秘密；与 kind/provider 校验一致。|
|`fingerprint`|BYTEA|否 / —|32字节；API摘要编码为64位hex。秘密采用带域/owner作用域的HMAC，公开X509采用公开DER的SHA256。|
|`fingerprint_key_id`|VARCHAR(128)|秘密必填|HMAC密钥引用；公开证书指纹可空，不把指纹当口令校验器。|
|`protection_mode`|VARCHAR(24)|否 / —|`verifier/encrypted/public_certificate` 三选一。|
|`verifier`|TEXT|是 / `NULL`|不可逆校验材料，编码包含已选密码算法、salt、参数；不包含明文。|
|`ciphertext`|BYTEA|是 / `NULL`|需恢复秘密时的认证加密密文。|
|`key_id`|VARCHAR(128)|是 / `NULL`|加密密钥版本/现有密钥设施引用；encrypted 必需。|
|`cipher_suite`|VARCHAR(32)|是 / `NULL`|固定经评审 AEAD 格式/版本；encrypted 必需。|
|`nonce`|BYTEA|是 / `NULL`|encrypted 必需；长度与算法绑定，不重用。|
|`public_certificate`|TEXT|是 / `NULL`|仅公开 x509 证书，≤32768 字节；私钥不进入服务。|
|`activated_at`|TIMESTAMPTZ|是 / `NULL`|pending 空，active 有值；引导凭据为创建时间。|
|`expires_at`|TIMESTAMPTZ|是 / `NULL`|无预设到期；非空须晚于created_at及非空activated_at。|
|`revoked_at`|TIMESTAMPTZ|是 / `NULL`|仅 revoked 非空；撤销不可反转。|
|`revoked_by`|VARCHAR(128)|是 / `NULL`|与 revoked_at 同空/同非空。|
|`revocation_reason`|VARCHAR(512)|是 / `NULL`|撤销/轮换审计原因，不能记 raw secret。|
|`supersedes_credential_id`|UUID|是 / `NULL`|同 tenant、同 owner 凭据；非自身；一次替代关系。|
|`rotation_operation_id`|UUID|是 / `NULL`|同 tenant operation FK；pending replacement 只能被一个轮换消费。|
轮换窗口的持久确认仍待生命周期实现；0030基础迁移不含overlap_ends_at，不能将规划列当作已落地。
模板引导凭据仅关联template_id及其当前注册策略，不新增enrollment_revision_id或模板发布版本。

CHECK 按 protection_mode 要求恰好一组内容：verifier 模式仅 verifier；encrypted 模式 ciphertext/key_id/cipher_suite/nonce 完整；public_certificate 模式仅 certificatePem，kind=x509。X509 指纹来自公开证书；token/口令指纹用已固定安全机制，不能以“指纹”存明文。所有模式普通列表只返回摘要。

凭据秘密最大 8192 字节；密文、验签/校验材料不进入普通审计、outbox、导出、错误、trace。需要受控 reveal 的秘密采用 encrypted 模式；hash-only 或公钥证书无可恢复 secret 时正式 reveal 返回稳定 409，不临时改成明文存储。密钥来源复用已有部署设施；没有现有设施时补最小密钥注入/版本轮换，不强制新 Vault 或新服务。禁止把 key 与 ciphertext 保存在同一表/报告。

索引：`(tenant_id,device_id,state)`、`(tenant_id,template_id,state)`、`(tenant_id,rotation_operation_id)`、有效期清理索引；安全指纹按 purpose/kind/provider 身份作用域去重。同 public_identifier 的重叠轮换仅允许同一设备；跨设备认领由正式 runtime namespace 注册与事务仲裁阻断，不能用一个全表用户名 UNIQUE 错杀合法不同租户/实例。

创建 replacement 返回 pending 与一次受控 secret；轮换只引用该 credential ID。先让 runtime 应用 replacement，随后按固定窗口撤旧；异步过程中不得生成第二个用户无法领取的 secret。模板 enrollment credential 创建在当前模板resourceVersion CAS下保存同模板FK；token解析tenant/template并检查当前autoRegister策略与凭据有效状态，客户端body不提供tenant。当前配置改为autoRegister=false时在同一事务撤销该模板旧引导凭据并写审计，恢复autoRegister不会复活已撤销凭据。不新增模板版本、发布、引导凭据切版或历史快照。注册返回的 statusToken 是 runtime owner 的10分钟单job状态能力，不进入此设备/模板长期credential表；不授予管理或secret读取权限，不新增元数据运行态表。

## 4. 模板与设备配置的 JSON 字典

这些是聚合内值对象，不为每个小字段新增平行owner。模板直接保存当前configuration并完整校验；绑定、导入和CLI使用同一schema。物模型独立保留其发布版本和不可变snapshot。

### 4.1 接入配置

|JSON 字段|类型 / 必填 / 缺省|约束与边界|
|---|---|---|
|`deviceKind`|enum，必填，无默认|standalone/gateway/subdevice；与设备 kind 一致。|
|`model`|object 或 null，必填|非空 `{modelId,publishedRevision}`；服务端解析同租户固定模型快照。|
|`access.mode`|enum，必填|direct/protocol_connector/service_connector/gateway。|
|`access.protocol`|string1..64，必填|协议标识，不与接入方式或连接方向混为一列。|
|`access.providerId`|UUID 或 null，必填|由模式/正式 provider schema 决定是否允许 null。|
|`access.providerVersion`|string1..128 或 null，必填|与 providerId 一致；不能引用会漂移的 latest。|
|`access.serviceInstanceId`|string1..256 或 null，必填|service_connector 必须非空；由 connector 正式目录解析tenant/归属与版本，其他mode按provider能力。|
|`access.schemaDigest`|64 位小写十六进制，必填|固定 provider schema 摘要。|
|`access.parameters`|object，必填|≤64 KiB；按版本 schema 验证；秘密只能正式 credential 引用。|
|`access.connectionDirection`|enum，必填|device_to_platform/platform_to_device；独立于 mode 与 deviceKind。|
|`access.credentialPolicy.kind`|enum，必填|none/token/username_password/x509。|
|`access.credentialPolicy.autoRegister`|boolean，必填|动态注册策略；不把模板 secret 填入本对象。|
|`access.onlinePolicy.mode`|enum，必填|transport/heartbeat/activity；这是 runtime 计算在线的期望规则。|
|`access.onlinePolicy.timeoutSeconds`|integer1..86400，必填|统一秒；旧 online_timeout 分钟与 heartbeat 秒须显式换算。|
|`topicMappings`|array，必填，服务初始 []|≤1000；见下表。|
|`processingScripts`|array，必填，服务初始 []|≤100；见下表。|
|`customFieldDefinitions`|array，必填，服务初始 []|≤100；见下表。|

`gateway` 接入允许子设备和 provider 支持的多级 gateway；独立 gateway 也可走 direct/protocol/service 接入。provider 能力在发布与绑定时核验，不能仅检查 JSON 枚举。

DirectAccess 为 `{access,externalDeviceId}`，两字段均存在，externalDeviceId 可空。设备实例参数位于上层 CreateDevice/DeviceTemplateBinding/SetTemplateBinding 的 `deviceParameters`，对应独立 `device_parameters` 列，最多100项、64KiB；必须匹配 ConnectionProvider.deviceParameterSchema，缺省创建为 `{}`。共享 `access.parameters` 与逐设备参数有不同schema与命名空间，runtime接受两个明确输入，**不做隐式合并、深合并或覆盖**；未知字段报错。旧设备 `protocol_config` 按来源与provider定义转换到实例参数，不能只因模板也有配置就丢弃。更新绑定时省略 deviceParameters 的处理按正式绑定用例冻结，并在目标schema下重新验证，不通过普通资料PATCH暗改接入。

### 4.2 Topic、脚本与资料字段

|值对象|字段（全为必填；nullable 明列）|约束 / DEFAULT|
|---|---|---|
|TopicMapping|id ConfigurationItemId(UUID)；name string1..128；description nullable string≤2000；direction up/down；sourceTopic/targetTopic string1..1024；dataKind nullable TELEMETRY/ATTRIBUTE/EVENT/COMMAND；dataIdentifier nullable NativeThingModelIdentifier（非空、保留核心原字节规则，无另设128字符上限）；priority int0..10000；enabled boolean|同一当前配置内id唯一；同 direction/source/target 的重复映射拒绝；稳定执行顺序 priority,id；跨租户主题禁止；dataKind/dataIdentifier必须同时空或同时非空，引用同固定模型的 `(kind,identifier)`，同名异kind不混淆。各项无隐式默认。|
|ProcessingScript|id UUID；name string1..128；description nullable≤2000；phase 六值；language=lua；entrypoint=encodeInp；content string1..262144；enabled boolean；lastSampleInput nullable≤262144|phase 为 telemetry_up/down、attribute_up/down、command_down、event_up；同 phase 至多一个 enabled；代码/样例不含秘密；空数组 [] 合法。metadata 不解释/执行 Lua；执行和测试由 connector-runtime 限资源沙箱负责。|
|CustomFieldDefinition|key regex `[A-Za-z_][A-Za-z0-9_]*` 长度≤64；name string1..128；type string/number/boolean/enum；defaultValue nullable string/number/boolean；description nullable≤2000；enabled boolean；enumValues 必填数组，每项为 `{value:string1..256,label:string1..256}`，最多1000项|key 唯一；defaultValue 与 type 一致，null 表示无默认。enum 类型须有非空选项数组，value 唯一，非 null 的默认值必须属于选项；其他类型 enumValues 必须为 `[]`。旧 Enum 仅在可无损归一时迁入，否则报告异常。不能当 ThingModel 字段/遥测历史；停用保留已有值，删定义/改类型须做设备值影响检查及显式处理。|

`devices.custom_fields` 中未知 key、错误类型拒绝；不因绑定新模板静默删除资料值。preview 应指出不兼容 key 及处理前置条件；正式切换在旧版本仍未改变时再次校验。lastSampleInput 只是用户保存样例，不是运行调试日志；样例缺少/过大/敏感内容须在导入和发布前报告。

## 5. 公共持久能力与跨 owner 完整性

### 5.1 引用登记与删除 fence

任务：DM-API-006、DM-API-011、DM-DB-011、DM-RT-008..012、DM-QA-005。业务目录自身的 FK 已覆盖本库强关系；下面机制用于自动化、告警、市场、看板、OTA、文件等 owner 的引用与删除竞态，不是业务兼容桥。

`metadata_resource_fences`，owner 为被引用元数据资源的 `devicemetadata`：

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`tenant_id`|UUID|否 / —|复合 PK。|
|`resource_kind`|VARCHAR(48)|否 / —|device/access_template/access_revision/product/group；白名单。|
|`resource_id`|UUID|否 / —|PK `(tenant_id,resource_kind,resource_id)`；受控 trigger 校验本库对应资源。|
|`epoch`|BIGINT|否 / `1`|正整数；fence 变更递增，不复用旧租约。|
|`state`|VARCHAR(16)|否 / `'open'`|open/deleting/deleted。|
|`operation_id`|UUID|是 / `NULL`|deleting/deleted 对应删除 operation；同 tenant FK。|
|`updated_at`|TIMESTAMPTZ|否 / `NOW()`|fence 更新时间。|
|`updated_by`|VARCHAR(128)|否 / —|受信 actor。|

`metadata_reference_edges`：

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|公共列|`A`|见列包|记录当前引用边；状态变更审计，不直接删除历史。|
|`target_kind`|VARCHAR(48)|否 / —|同 fence kind。|
|`target_id`|UUID|否 / —|复合 FK `(tenant_id,target_kind,target_id)` → fence。|
|`target_epoch`|BIGINT|否 / —|登记时 fence epoch；只允许 state=open。|
|`source_owner`|VARCHAR(64)|否 / —|受认证 service 身份对应白名单，不能由普通用户冒充。|
|`source_kind`|VARCHAR(64)|否 / —|如 automation_rule/dashboard_instance/ota_package。|
|`source_id`|VARCHAR(256)|否 / —|owner 不透明资源 ID；跨库无 FK。|
|`relation_kind`|VARCHAR(64)|否 / —|明确引用用途，影响删除/切版的校验。|
|`source_revision`|VARCHAR(128)|否 / —|不可变来源版本；纳入reserved/active唯一键。源版本更新先登记并确认新边，再释放旧边；旧release只作用于旧referenceId+revision。|
|`resource_version`|BIGINT|否 / `1`|1..JSON安全上限；状态改变/续期CAS后递增，幂等重放或no-op不变；API resourceVersion。|
|`state`|VARCHAR(16)|否 / `'reserved'`|reserved/active/released。|
|`reservation_expires_at`|TIMESTAMPTZ|是 / `NULL`|reserved必填，创建/续期时事务时间+600秒；active/released为空。API reservedUntil。超时只触发核对，不自动当作无引用。|
|`confirmed_at`|TIMESTAMPTZ|是 / `NULL`|active 时必填。|
|`released_at`|TIMESTAMPTZ|是 / `NULL`|released 时必填。|
|`updated_at`|TIMESTAMPTZ|否 / `NOW()`|状态变化时间。|

活动边唯一键：tenant+target_kind+target_id+source_owner+source_kind+source_id+source_revision+relation_kind，`WHERE state IN ('reserved','active')`；索引 target/state 与 source_owner/source_id。目标 fence 不物理删除，确保 ID 再用或晚到引用不会绕过 tombstone。

登记流程：创建资源与初始open fence同事务完成；引用方以机器身份获取同 tenant 目标、在目标 owner 原子检查 open+epoch 并 reserve；自己的业务提交后确认 active；失败明确 release。删除先把 fence 从 open CAS 为 deleting，关闭新引用入口，然后核对 reserved/active 与本库 FK；存在引用则返回脱敏 409 并恢复 open（epoch 增加）；无引用后执行必要 runtime 隔离与 tombstone。epoch增加不撤销已有active引用的删除保护；旧epoch的reserved确认须重新核对并续登记，未解决前仍阻断删除。请求丢失或 owner 不可用时阻断删除并重试核对，不能“租约到了就当不存在”。引用侧只可在持有有效登记后向用户确认引用保存成功。reserve/get/confirm/renew/release已在同一openapi.yaml明确，主任务DM-API-011，删除fence接入DM-API-006，存储约束DM-DB-011。普通用户/CLI/MCP不得调用；同一正式authn、可信租户和Problem规则适用。确认与续期检查expectedReferenceVersion、expectedSourceRevision和引用保存的expectedTargetEpoch；confirm另检查当前fence epoch/open及预留有效期。renew在目标open且重新核验后更新epoch/期限，只有reserved可续期。release只针对指定边/来源版本，目标deleting期间仍允许。详情见API-CONTRACT §10.1及TC-DM-API-011-01—06。

**响应丢失与持久恢复**：来源owner在reserve之前先保存intent、不可变sourceRevision和稳定幂等键；源业务变更与confirm/release待发记录在来源自己的同一事务提交。reserve提交但响应丢失时以原键找回；未终结reserved/active的恢复索引不随通用24h清除。相同owner/tenant的自然键重复409在`Problem.conflicts[].visibleResourceId`返回既有referenceId（kind=metadata_reference、reasonCode=REFERENCE_EXISTS），服务端仍先鉴权；其他owner/租户不得看到这个ID。无论找回原收据还是得到冲突ID，都必须GET当前引用，不能把旧reserved响应当作当前状态。源commit结果不确定时先回读/对账，只有证明确未提交或已撤销使用才release。**全部reserve/confirm/renew/release/delete事务固定fence→edge锁顺序**；来源r2保护建立后再释放r1。


ThingModel 同库已有 FK 优先：元数据以正式模型读合同校验后保存引用，模型删除/归档保护由其 owner 实施；不让 devicemetadata 注册一套可写模型目录。若未来模型迁出同数据库，再由模型 owner 承接相同引用/fence 协议，不建立跨库 FK。

### 5.2 `metadata_audit_events` — 共用脱敏审计

Owner：`devicemetadata`。演进现有通用 `audit_log`，不是为三个菜单分别新建日志；ThingModel 既有 `thing_model_audit_log` 保持模型事务事实，不把同一个模型事件再写一份“新核心日志”。任务 DM-DB-009、DM-API-003/005。

Kernel候选实现记录：`0020_metadata_audit_events.up.sql`落地此处事件字段与索引的当前基础；分组创建已在业务事务内写入成功事件。该候选迁移不包含tenant RLS/应用角色权限收敛，其他metadata写操作也未接入，DM-DB-009仍为部分实现，不能据此关闭DM-DB-002/DM-API-003。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|公共列|`A`|见列包|append-only；只有批准保留期治理可清理。|
|`operation_id`|UUID|是 / `NULL`|同 tenant metadata operation FK；同步用例可空。|
|`request_id`|VARCHAR(128)|否 / —|trace 相关；**不全局 UNIQUE**，同请求可有多条资源事件。|
|`operation_name`|VARCHAR(128)|否 / —|正式 OpenAPI operationId 或受信内部步骤。|
|`resource_kind`|VARCHAR(64)|否 / —|明确实体类型。|
|`resource_id`|UUID|是 / `NULL`|未创建成功时可空；不以 FK 删除阻止历史审计保留。|
|`before_version`|BIGINT|是 / `NULL`|创建前空。|
|`after_version`|BIGINT|是 / `NULL`|无成功变更时空。|
|`outcome`|VARCHAR(16)|否 / —|succeeded/denied/failed；失败不假造业务变更。|
|`reason_code`|VARCHAR(128)|是 / `NULL`|稳定错误/审核码。|
|`redacted_changes`|JSONB|否 / `'{}'`|白名单差异，≤64KiB；无秘密/reauth token/签名URL。|

索引 `(tenant_id,created_at DESC,id DESC)`、`(tenant_id,resource_kind,resource_id,created_at)`、`(tenant_id,operation_id)`。业务成功变更与审计同事务；鉴权失败可经安全日志通道保留，不声称失败请求能与不存在的业务事务原子提交。保留期由运维配置冻结，默认不自动删除；清理不可影响永久迁移映射或仍需恢复的作业。

### 5.3 `metadata_outbox` — 原生期望变更事件

Owner：`devicemetadata`。演进/替换 `0012` 的投递基础设施；事件目的地只允许正式 runtime/consumer，不继续旧 projection。ThingModel 自己的 outbox 复用已有事务实现，工作器框架可共享，但同一业务事件不写两个事实账本。任务 DM-DB-009、DM-RT-007。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|公共列|`A`|见列包|事件身份/内容不可变；下方投递列可更新。|
|`operation_id`|UUID|是 / `NULL`|同 tenant 作业关联。|
|`resource_kind`|VARCHAR(48)|否 / —|device/template/group 等白名单。|
|`resource_id`|UUID|否 / —|稳定资源身份。|
|`generation`|BIGINT|否 / —|资源顺序；设备使用 configuration_generation。|
|`event_type`|VARCHAR(128)|否 / —|版本化正式事件类型。|
|`schema_version`|INTEGER|否 / `1`|正整数；消费者拒绝未知版本。|
|`payload`|JSONB|否 / —|固定内容/不可变引用，不能消费时重新读 latest 拼出另一事件；不含 secret。|
|`payload_digest`|CHAR(64)|否 / —|事件内容摘要。|
|`state`|VARCHAR(16)|否 / `'pending'`|pending/leased/delivered/dead_letter。|
|`attempts`|INTEGER|否 / `0`|≥0。|
|`available_at`|TIMESTAMPTZ|否 / `NOW()`|重试调度。|
|`lease_owner`|VARCHAR(128)|是 / `NULL`|worker 身份。|
|`lease_until`|TIMESTAMPTZ|是 / `NULL`|leased 时必需；与 lease_owner 同空。|
|`delivered_at`|TIMESTAMPTZ|是 / `NULL`|消费者可靠接收时间，不等于 runtime applied。|
|`last_error_code`|VARCHAR(128)|是 / `NULL`|脱敏稳定错误；无凭据。|

唯一 `(tenant_id,resource_kind,resource_id,generation,event_type)`；索引 `(state,available_at,id)` 的未完成条件索引及 `(tenant_id,resource_id,generation)`。同资源按 generation 有序，多 worker 用数据库领取租约；消息至少一次、消费者按 eventId/generation 去重。死信不跳过关键旧变更并把后续作业说成功；需受控恢复。旧 projection 未完成项隔离并生成新 desired reconciliation 计划，不能改目的地后盲重放旧 payload。

### 5.4 `metadata_idempotency` — 持久请求收据

Owner：`devicemetadata`；目标覆盖设备/模板/组/作业共用规则。规划早期曾引用 `0013`，但当前Kernel候选迁移目录缺少 `0008–0013` 文件；分组切片按实际候选顺序在 `0018_device_groups` 后追加 `0019_metadata_idempotency.up.sql`，未改写或重放已应用迁移。任务 DM-API-005、DM-DB-010。ThingModel 既有 receipts 见 §6.5，不复制其执行逻辑。

Kernel候选实现记录：`0019`目前提供同步写入所需的tenant/subject/operation/path/key复合主键、请求摘要、状态、HTTP状态、非秘密JSON响应/摘要和首次24小时窗口，过期重放清响应并保留409 tombstone。尚缺本节设计中的operation FK、密文响应列及租约恢复字段；`metadata_operations`、`metadata_operation_items`、`metadata_artifacts`尚未落地。此基础仅由createDeviceGroup使用，DM-API-005与DM-DB-010均保持部分实现。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`tenant_id`|UUID|否 / —|PK 作用域。|
|`subject_id`|VARCHAR(128)|否 / —|受信主体，不能省略导致跨用户重放。|
|`operation_name`|VARCHAR(128)|否 / —|OpenAPI operationId。|
|`canonical_path`|VARCHAR(1024)|否 / —|包含规范资源 ID 的ASCII正式路径；不存未经解码/规范化的任意URL，不可用路由模板混同不同资源。|
|`idempotency_key`|VARCHAR(128)|否 / —|8..128、合同允许字符。|
|`request_hash`|CHAR(64)|否 / —|方法+path+规范化请求及相关前置条件；秘密输入使用受保护摘要，不能存 raw body。|
|`state`|VARCHAR(16)|否 / `'in_progress'`|in_progress/completed/failed/expired。|
|`operation_id`|UUID|是 / `NULL`|同 tenant metadata operation FK；202 可复用同 ID。|
|`http_status`|SMALLINT|是 / `NULL`|完成时实际状态。|
|`response`|JSONB|是 / `NULL`|非秘密响应；与 response_ciphertext 至多一项非空。|
|`response_ciphertext`|BYTEA|是 / `NULL`|含一次展示 secret 的响应须加密。|
|`response_key_id`|VARCHAR(256)|是 / `NULL`|密文格式引用，含独立 nonce/AEAD 包；加密响应必需。|
|`response_hash`|CHAR(64)|是 / `NULL`|结果一致性摘要，不当作返回 secret 的授权。|
|`created_at`|TIMESTAMPTZ|否 / `NOW()`|首次受理。|
|`updated_at`|TIMESTAMPTZ|否 / `NOW()`|状态变化。|
|`replay_until`|TIMESTAMPTZ|否 / —|本轮元数据默认首次创建+24h；不因重试续期。|
|`lease_owner`|VARCHAR(128)|是 / `NULL`|同步未完成受理的恢复仲裁。|
|`lease_until`|TIMESTAMPTZ|是 / `NULL`|与 owner 同空；超时先核对事务/作业，不直接重做。|

复合 PK 为 tenant+subject+operation_name+canonical_path+key；索引 replay_until 和未完成租约。处理相同 key 时串行仲裁，同 hash 重放同结果/同 operation；异 hash 返回 409。业务提交、收据、审计/outbox 在同一数据库事务；“进程返回之前写日志”不算原子幂等。

窗口后擦除响应（包括密文），保留紧凑 key/hash/state tombstone 并返回稳定 expired 409；不把过期 key 当全新请求再执行。secret 重放每次重新认证、动作/资源授权及 reauth 要求，撤权不能通过缓存读到 secret。清理密文不影响 credential 自身保护材料或长期作业身份。

### 5.5 `metadata_operations` 与 `metadata_operation_items`

Owner：`devicemetadata`。共用作业表而非三个菜单各建队列。任务 DM-API-008、DM-DB-010、DM-DEV-012..015/023/027、DM-TPL-012/013。

`metadata_operations` 使用 `E`：

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`kind`|VARCHAR(40)|否 / —|仅允许 OpenAPI Operation.kind 中 owner=`devicemetadata` 的元数据作业子集；包含设备激活/删除/改号/启停、模板/网关关系、凭据及创建/导入/导出批作业。明确排除 telemetry owner 的 `reading_export`；其状态只落 telemetry 账本，可复用 Operation 响应 schema。|
|`state`|VARCHAR(16)|否 / `'pending'`|pending/running/succeeded/failed/cancelled。|
|`originating_operation_name`|VARCHAR(128)|否 / —|发起 API operationId，查作业/下载按它重检权限。|
|`originating_subject_id`|VARCHAR(128)|否 / —|受信主体；不是任意 tenant admin 即可读取所有秘密结果。|
|`resource_kind`|VARCHAR(48)|是 / `NULL`|单资源用例使用；批作业可空。|
|`resource_id`|UUID|是 / `NULL`|单资源身份；与 kind 同空。|
|`authorized_selection`|JSONB|否 / `'{}'`|冻结被授权选择集/范围和来源版本；不是永久授权 token，执行/读取仍重检。|
|`request_digest`|CHAR(64)|否 / —|规范请求摘要。|
|`input_artifact_id`|UUID|是 / `NULL`|同 tenant artifacts 引用；导入内容固定。|
|`validation_digest`|CHAR(64)|是 / `NULL`|经过校验的源+转换器+结果摘要；commit 必须匹配。|
|`total_items`|INTEGER|否 / `0`|≥0。|
|`completed_items`|INTEGER|否 / `0`|≥0、≤total；由已终结 items 计数事务更新。|
|`failed_items`|INTEGER|否 / `0`|≥0、≤completed；不能 failed>0 却宣称全成功。|
|`checkpoint`|JSONB|否 / `'{}'`|版本化步骤/游标；不存密码或无版本自由文本。|
|`result`|JSONB|是 / `NULL`|OpenAPI Operation.result；大结果只给 report artifact。|
|`failure`|JSONB|是 / `NULL`|脱敏 Problem；failed 必填。|
|`lease_owner`|VARCHAR(128)|是 / `NULL`|worker 标识。|
|`lease_until`|TIMESTAMPTZ|是 / `NULL`|与 lease_owner 同空。|
|`attempts`|INTEGER|否 / `0`|≥0；仅安全步骤可重试。|
|`started_at`|TIMESTAMPTZ|是 / `NULL`|开始执行。|
|`finished_at`|TIMESTAMPTZ|是 / `NULL`|终态必需。|
|`expires_at`|TIMESTAMPTZ|是 / `NULL`|可下载大结果过期；不代表永久作业身份被删除。|

本表支持清单是 OpenAPI 中正式元数据作业的冻结子集，不因共享 Operation 响应 schema 纳入其他 owner 的作业，也不额外加入无正式 API 的组导入导出。索引 tenant+createdAt、tenant+originating_subject、state+leaseUntil、tenant+resourceId；运行中同设备接入变更通过 binding.pending_operation_id 锁定顺序。

`metadata_operation_items`：

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`tenant_id`|UUID|否 / —|复合 PK/FK。|
|`operation_id`|UUID|否 / —|同 tenant operations FK RESTRICT。|
|`item_key`|VARCHAR(128)|否 / —|PK `(tenant_id,operation_id,item_key)`；稳定业务行键。|
|`source_row`|INTEGER|是 / `NULL`|文件行号≥1；非文件批操作可空。|
|`input_digest`|CHAR(64)|否 / —|同 itemKey 异内容冲突。|
|`state`|VARCHAR(16)|否 / `'pending'`|pending/succeeded/failed/skipped；与 API 一致。|
|`resource_id`|UUID|是 / `NULL`|创建/目标实体 ID；类型由 job 冻结。|
|`expected_resource_version`|BIGINT|是 / `NULL`|CAS 前置。|
|`target_generation`|BIGINT|是 / `NULL`|要求应用的设备 generation。|
|`desired_document`|JSONB|是 / `NULL`|必要的固定配置/不可变引用，不含 secret；用来恢复同一操作而非读取 latest。|
|`checkpoint`|JSONB|否 / `'{}'`|步骤版本与已提交回执引用。|
|`owner_receipt`|JSONB|是 / `NULL`|正式 runtime/owner 的确认 ID、generation、摘要、时间；不是当前运行态副本。|
|`error`|JSONB|是 / `NULL`|失败时脱敏 Problem。|
|`created_at`|TIMESTAMPTZ|否 / `NOW()`|首次受理。|
|`updated_at`|TIMESTAMPTZ|否 / `NOW()`|进度更新。|

索引 `(tenant_id,operation_id,state,item_key)`。同一个 item 的应用请求使用 jobId+itemKey 派生稳定幂等键，重启不可再建实体。批设备 `atomic=true` 是元数据全批一个事务；导入与批切版/批网关按项提交，可能部分失败，API 必须逐项准确显示。终态不回退；重试已终结失败需要新受审计操作引用原作业，或在原作业终结前执行内部恢复，不能静默把 failed 改回 running。

### 5.6 `metadata_artifacts` — 导入/导出与校验报告登记

Owner：`devicemetadata` 管理用途和授权；实际 bytes 由文件存储 owner。使用 `A`。任务 DM-DB-010、DM-DEV-013..015、DM-TPL-012/013。

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`operation_id`|UUID|否 / —|同 tenant operation FK；来源权限由其判定。|
|`purpose`|VARCHAR(32)|否 / —|import_source/validation_report/export/package/report。|
|`file_owner`|VARCHAR(64)|否 / —|固定受信文件 owner。|
|`file_object_id`|VARCHAR(256)|否 / —|不透明稳定引用；不能是短期 URL 或用户任意本地路径。|
|`file_name`|VARCHAR(256)|否 / —|清理路径分隔符，只有显示名。|
|`content_type`|VARCHAR(128)|否 / —|按正式允许格式与实际内容验证。|
|`size_bytes`|BIGINT|否 / —|≥0；符合入口最大文件限制。|
|`checksum`|CHAR(64)|否 / —|原始 bytes SHA-256；与模型/资源摘要无关。|
|`state`|VARCHAR(16)|否 / `'staged'`|staged/ready/expired/deleted；只有 ready 可下载。|
|`expires_at`|TIMESTAMPTZ|是 / `NULL`|入口明确配置的结果保留期，过期 410。|
|`deleted_at`|TIMESTAMPTZ|是 / `NULL`|实际 bytes 清理后登记，不删除历史行。|

索引 tenant+operation/purpose、state+expiresAt；去重 `(tenant_id,operation_id,purpose,checksum)`。先存对象并核验 digest，再同事务标 ready；孤立 staged 对象由协调清理器按 receipt 清理。上传对象无 ready 记录不能凭路径下载。返回地址按请求重鉴权后由 owner 签发，数据库不保存 signed URL。operation 与 artifact 的循环引用以插入 job→artifact→设置 input_artifact_id 顺序，在同 tenant FK 下完成；FK 可 DEFERRABLE，不用取消约束。

## 6. 已有 ThingModel 表：原位复用与必要强化

### 6.1 事实源和版本关系

K37 `thingmodel/postgres.go:22–27` 明确 `canonical_model IS NULL` 的旧行在显式转换前对原生 repository 不可见；`postgres.go:465–483` 在同一事务保存语义快照 bytes/checksum、资源 content/revision/checksum 并更新当前模型。元数据不得通过读取旧 `thing_model_items` 或 `content` 猜出“看起来相似”的新模型。

以下字典列**已有关键列和实际缺省**，不是本轮要再新建一套模型表。候选 `0014–0016` 只追加于同一迁移账本；已有 core/app 复用，强化项目限 DM-API-007、DM-DB-002/005/011、DM-MIG-005。

### 6.2 `thing_models` 与 `thing_model_items`

`thing_models`，owner `thingmodel`：

|已有字段|SQL 类型|可空 / 现有 DEFAULT|语义 / 必要强化|
|---|---|---|---|
|id / tenant_id|UUID / UUID|否 / —|id PK；增加 UNIQUE(tenant_id,id) 支持同租户 FK。|
|name|VARCHAR(128)|否 / —|既有 UNIQUE(tenant_id,name)。|
|description_i18n / category|JSONB / VARCHAR(64)|是 / NULL|旧列；原生完整语义取 canonical_model，不产生双写来源。|
|status|VARCHAR(16)|否 / `'DRAFT'`|DRAFT/PUBLISHED/ARCHIVED；生命周期枚举不表示已有删除/归档 API。|
|current_snapshot_id|UUID|是 / NULL|已发布当前指针，新增同 tenant+model 的 FK/一致性约束。|
|created_at / updated_at|TIMESTAMPTZ|否 / NOW()|现有创建/更新时间。|
|created_by / updated_by|UUID|是 / NULL|现有列；actor 映射已核验后可补写，不把非 UUID 字符串截断塞入。|
|deleted_at|TIMESTAMPTZ|是 / NULL|现有软删字段；当前九个原生入口无删除命令，不擅自新增。|
|web_chart_config / app_chart_config|JSONB|是 / NULL|`0006` 旧列；原生资源以 thing_model_resources 为唯一 owner，转换后不作运行时回退。|
|canonical_model|JSONB|是 / NULL|`0015` 加入；现有封装 schema_version + core.Model，是原生语义草稿事实。|
|draft_revision / published_revision|BIGINT|否 / 0|`0015` 非负 CHECK；未分类旧行从 0 开始；原生草稿从 1 起，发布前 published=0。API 输出限定安全整数。|

原有索引：`(tenant_id,updated_at DESC,id) WHERE canonical_model IS NOT NULL AND deleted_at IS NULL`。只有既有 app/core 可以改变 canonical aggregate；列表选择器读取同一原生 owner。

`thing_model_items` 是已有较早的字段表，**不是 K37 原生 aggregate 的第二份可编辑副本**：

|已有字段|SQL 类型|可空 / 现有 DEFAULT|处理|
|---|---|---|---|
|id / thing_model_id / tenant_id|UUID|否 / —|id PK，旧 FK 仅 model_id；核验数据后改同 tenant 复合 FK。|
|type / identifier|VARCHAR(16) / VARCHAR(64)|否 / —|旧 UNIQUE(model_id,identifier) 与原生 `(kind,identifier)` 范围不同；不能用它限制原生不同 kind 同名。|
|name_i18n / description_i18n|JSONB|前者否，后者是 / —|仅旧字段转换来源。|
|value_type|JSONB|`0014` 后可空 / NULL|TELEMETRY/ATTRIBUTE 标量。|
|parameters|JSONB|是 / NULL|`0014` 增加；COMMAND/EVENT 扁平参数。|
|access|JSONB|否 / —|read/write/observe 三布尔，转换不猜缺省。|
|web_chart_config / app_chart_config|JSONB|是 / NULL|旧字段图表转换来源。|
|meta_items|JSONB|否 / `[]`|旧可扩展字段；原生未支持的内容逐项 disposition，不能静默复制当核心已支持。|
|sort_order|INTEGER|否 / 0|字段顺序。|
|created_at / updated_at|TIMESTAMPTZ|否 / NOW()|旧来源时间。|

正式直切后不再让旧字段 CRUD 与新 canonical aggregate 并行写。若原生仓库不需要该表作为投影，完成经验证转换后保留只读历史或按批准保留策略归档；不为了“复用表名”强制新核心写两份字段事实。

### 6.3 `thing_model_snapshots`

Owner：`thingmodel`。已有 UPDATE/DELETE 拒绝 trigger（`0001`），本轮沿用并验收；无需另建 metadata model snapshot 表。

|已有字段|SQL 类型|可空 / 现有 DEFAULT|语义 / 强化|
|---|---|---|---|
|id / tenant_id / thing_model_id|UUID|否 / —|id PK；model FK 原仅 ID，强化同 tenant；增加固定组合唯一键供模板引用。|
|version|INTEGER|否 / —|既有 UNIQUE(model_id,version)；原生接口安全 revision 范围大于 INT，需要追加迁移扩为 BIGINT，验证已有数据无变换。|
|content|JSONB|否 / —|旧共享字段；canonical_snapshot=false 时不能作为原生 bytes 猜读。|
|changelog|TEXT|是 / NULL|发布说明。|
|published_at|TIMESTAMPTZ|否 / NOW()|发布时间。|
|published_by|UUID|否 / —|既有主体；可信映射与 auth 合同一致。|
|canonical_snapshot|BOOLEAN|否 / FALSE|`0015` 原生标识。|
|canonical_content|BYTEA|是 / NULL|精确语义 checksum 输入 bytes，不能改用 JSONB 的重新序列化 bytes。|
|canonical_checksum|CHAR(64)|是 / NULL|core.BuildSnapshot 语义 SHA-256，包含 model/tenant 身份与 revision，不含图表资源。|
|resources_content|JSONB|是 / NULL|`0016` 固定 profile/Web/App 资源文档。|
|resources_revision|BIGINT|是 / NULL|≥0；资源修订与语义修订独立。|
|resources_checksum|CHAR(64)|是 / NULL|app.ChecksumResources 的独立摘要，不等于 canonical_checksum。|

已有 CHECK：非原生行 canonical_content/checksum 都空；原生行二者都非空。资源三列全空或全非空且 revision≥0。原生新发布还必须包含完整资源三列（即使空资源 revision=0 也有独立摘要）。模板引用新增 UNIQUE `(tenant_id,thing_model_id,id,version)`，复合 FK 固定同 model/snapshot/revision；digest 值通过 owner 读与事务校验匹配，不能让客户端指定虚假摘要。模型当前快照指针使用同样一致性检查。

`0016` 之前 canonical_snapshot=true 但没有 resources 的记录必须显式盘点：不能补当前最新图表冒充历史资源，不能禁用不可变触发器直接改旧快照。由模型 owner 依据可证明来源发布**新**完整快照，并在迁移计划中明确设备/模板引用选择；不能证明的历史内容列阻断异常。非本次绑定目标的旧快照可按历史审计保留，不虚构其已适配。

索引保留 `(tenant_id,thing_model_id,version DESC) WHERE canonical_snapshot=TRUE`，新引用复合索引须同时验证 ORM/SQL 查询计划。

### 6.4 `thing_model_resources` 与旧扩展表

Owner：`thingmodel`。由 `0016` 创建，保留一份资源草稿：

|已有字段|SQL 类型|可空 / 现有 DEFAULT|语义 / 强化|
|---|---|---|---|
|tenant_id / thing_model_id|UUID|否 / —|复合 PK；原 FK model_id CASCADE，强化同租户、并确保未授权删除不能清资源。|
|revision|BIGINT|否 / 0|独立 CAS，0 是合法初始值，不等于没传 expectedRevision。|
|profile|JSONB|否 / `{"author":"","tags":[],"cover":"","version":"","brand":"","modelNumber":""}`|既有资源格式原样复用；profile.version 是展示字段，不是模型发布 revision。|
|web_chart_config / app_chart_config|JSONB|是 / NULL|各限 262144 bytes，既有 owner 接受 JSON object/null；具体图表语义验证仍待前端资源任务验收。|
|updated_by|UUID|是 / NULL|现有 actor 类型，不能随意改主键身份模型。|
|updated_at|TIMESTAMPTZ|否 / NOW()|资源保存时间。|

`custom_command_controls`（`0007`）已有 id UUID/default随机、model_id/tenant_id、name128、description/content、enable_status 默认 enable、control_type32、created/updated。`meta_item_keys`（`0001`）已有可空 tenant 的内置/租户元数据键与 value_schema。这些是转换来源/既有扩展，不据表存在宣布新原生 HTTP 能写它们；按 DM-DEC-001 确认映射到已声明 COMMAND/资源或明确不迁，未决阻断发布，不复制一套“兼容字段 API”。

### 6.5 既有模型收据、审计、Outbox

|已有表及 owner|关键字段、类型、默认与约束|本轮处理|
|---|---|---|
|`thing_model_command_receipts` / thingmodel|PK `(tenant_id UUID,subject_id TEXT,operation TEXT,idempotency_key TEXT)`；payload_hash CHAR64 非空；result JSONB 可空；created_at NOW 非空；expires_at 非空；key 字节8..128；expiry索引|K37 postgres.go:65–112 实现30天重放，过期清 result 留 key tombstone。继续复用；metadata 的24h为 operation 配置值，不能为表面统一无理由改模型核心。重放依旧先鉴权。|
|`thing_model_audit_log` / thingmodel|id UUID PK；tenant UUID 非空；actor UUID 可空；action VARCHAR64、target_type VARCHAR32、target_id UUID 非空；before/after JSONB 可空；request_id VARCHAR64 可空；occurred_at NOW 非空；tenant/time、target索引|K37 postgres.go:501–531 同事务写；保留模型事件唯一事实。transport 的资源摘要读取补齐不重写审计模块。|
|`thing_model_outbox` / thingmodel|id UUID PK；tenant UUID；model_id UUID FK RESTRICT；operation TEXT；draft_revision BIGINT>0；payload JSONB；state PENDING/PROCESSING/DELIVERED/FAILED 默认PENDING；created_at NOW；delivered_at可空；attempts默认0且≥0；last_error可空|沿用 K37 Emit。补同租户复合 FK、租户 worker 验证、幂等消费者与删除完整性；“PENDING_CONSUMER”不能伪报已应用。不建立镜像 ThingModel 发布消息流。|

**语义/资源/文件四类摘要必须区分：** ThingModel canonical_checksum、ThingModel resources_checksum、接入模板 content_digest、导出包/文件 checksum 各有不同输入。跨身份导入会因 model/tenant/revision 变化生成新的语义 checksum，不能拿源 checksum 直接写目标 identity，也不能因源/目标 checksum 不同就误判数据丢失。先验证源 owner 的摘要，再通过现有核心创建目标并逐字段/资源关系对账。

## 7. 一次性迁移持久账本

Owner：专用、受控 `upgrade` 工具/作业；不是运行时 resolver。表建议放私有 `upgrade` schema，运行 API/CLI 角色无 SELECT/WRITE 权限，长期模板包导入的工作器也不获得本节账本权限。模板包导入映射归 §5.5 的正式 metadata operation/items，不依赖 `upgrade.migration_runs` 或本节任一表。可复用已核验 ThingModel runner 的 batches/items/bindings 思路，**同一次升级只有一个权威账本**，不并行维持两个可写迁移状态机。旧历史迁移日志只导入来源引用或保留只读，不当日常 lookup。

迁移可涉及尚未解析 tenant 的 preflight。因此此节明确允许少数 operational 行 `tenant_id=NULL`，仅限 unresolved/bootstrap 阶段，绝不允许业务表缺 tenant。未知 tenant 不分配“默认租户”；`validated/applied/verified` 的业务 item 必须有真实目标 tenant。父 run 记录受信授权租户集合，worker 每次校验 item 在范围内。所有 mapping 内容均不含 secret。

任务：DM-MIG-001..014、DM-DB-001/010、DM-API-002。

### 7.1 `upgrade.migration_runs`

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`id`|UUID|否 / —|PK。|
|`parent_run_id`|UUID|是 / NULL|同账本 FK；分 tenant/恢复子任务。|
|`tenant_id`|UUID|是 / NULL|单租户 run 指定；bootstrap/全局预检可空。|
|`authorized_tenants`|JSONB|否 / —|固定可信租户 ID 清单；不是客户端任意租户请求。|
|`source_instance`|VARCHAR(128)|否 / —|源部署不透明标识，不含数据库凭据。|
|`source_snapshot_id`|VARCHAR(256)|否 / —|一致导出/备份快照标识。|
|`source_manifest_digest`|CHAR(64)|否 / —|源表/文件清单、版本与 hash 摘要。|
|`backup_artifact_ref`|VARCHAR(512)|否 / —|受限备份 ID，不保存签名URL或密码。|
|`target_release_digest`|VARCHAR(256)|否 / —|目标完整制品/迁移集摘要。|
|`contract_digest`|CHAR(64)|否 / —|本次正式 OpenAPI 摘要。|
|`converter_version`|VARCHAR(128)|否 / —|版本/提交；不可用工作树“当前脚本”含混代替。|
|`mode`|VARCHAR(16)|否 / —|dry_run/apply/reconcile/recover。|
|`state`|VARCHAR(24)|否 / `'prepared'`|prepared/preflight/validated/applying/verifying/ready/failed/aborted/closed。|
|`plan_digest`|CHAR(64)|是 / NULL|validated 之后必需；冻结转换/排除/ID计划。|
|`counts`|JSONB|否 / `'{}'`|按 tenant/entity/state 的来源/目标/异常计数。|
|`created_at`|TIMESTAMPTZ|否 / NOW()|创建。|
|`created_by`|VARCHAR(128)|否 / —|实际迁移操作主体。|
|`started_at` / `finished_at`|TIMESTAMPTZ|是 / NULL|结束状态要求 finished_at。|
|`lease_owner` / `lease_until`|VARCHAR(128) / TIMESTAMPTZ|是 / NULL|同空；独占相同 source snapshot→target 范围 apply。|
|`failure_code`|VARCHAR(128)|是 / NULL|脱敏错误。|

唯一防并发活跃索引：source_instance+source_snapshot_id+target_release_digest+mode，限 applying/verifying；若分租户子 run，以受控父 run 锁定范围避免重叠 apply。索引 source_instance/state、tenant/createdAt。

### 7.2 `upgrade.migration_items`

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`id`|UUID|否 / —|PK。|
|`run_id`|UUID|否 / —|FK run RESTRICT。|
|`tenant_id`|UUID|是 / NULL|未解析前可空；validated 及后续阶段须非空。|
|`source_tenant_key`|VARCHAR(256)|否 / —|原始 tenant 字节值，不能截断归一。|
|`source_entity`|VARCHAR(64)|否 / —|来源真实表/实体。|
|`source_id`|VARCHAR(1024)|否 / —|原 ID 原样，支持非 UUID。|
|`source_item_digest`|CHAR(64)|否 / —|规范化source_tenant_key/source_entity/source_id/target_kind元组摘要；索引使用摘要而非可能超大UTF-8文本键。|
|`source_digest`|CHAR(64)|否 / —|源行规范化摘要；秘密字段仅受保护指纹，不明文报告。|
|`target_kind`|VARCHAR(64)|否 / —|明确目标实体/owner，或批准 disposition 类型。|
|`disposition`|VARCHAR(24)|否 / —|migrate/reuse/other_owner/approved_exclude/needs_decision/quarantine。|
|`state`|VARCHAR(16)|否 / `'prepared'`|prepared/validated/applied/verified/skipped/failed。|
|`api_operation_name`|VARCHAR(128)|是 / NULL|apply 时的正式 owner operation。|
|`idempotency_key`|VARCHAR(128)|是 / NULL|稳定 run/source/entity/item 推导；retry 原 key。|
|`owner_receipt`|JSONB|是 / NULL|owner 返回 resource/operation/revision 等无秘密结果。|
|`target_digest`|CHAR(64)|是 / NULL|转换后同语义 canonical 对账摘要。|
|`attempts`|INTEGER|否 / 0|≥0。|
|`created_at` / `updated_at`|TIMESTAMPTZ|否 / NOW()|过程时间。|
|`last_error_code`|VARCHAR(128)|是 / NULL|脱敏错误。|

唯一 `(run_id,source_item_digest)`；命中时逐项比较原始身份元组，hash碰撞作为阻断异常而非合并。source_digest 改变则不是同一次 prepared item，必须新计划/快照，不能覆盖已验证结果。索引 run/state/id、tenant/source_entity。

### 7.3 `upgrade.migration_id_map` — 永久旧新身份映射

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`id`|UUID|否 / —|PK；映射记录稳定身份。|
|`source_key_digest`|CHAR(64)|否 / —|对下列五项源身份元组的版本化canonical编码做SHA-256，UNIQUE；避免超长UTF-8复合B-tree键。|
|`source_instance`|VARCHAR(128)|否 / —|源身份元组第1项。|
|`source_tenant_key`|VARCHAR(256)|否 / —|原始值。|
|`source_entity`|VARCHAR(64)|否 / —|明确区分 Community device_configs 与 device_templates。|
|`source_id`|VARCHAR(1024)|否 / —|原样 ID。|
|`target_role`|VARCHAR(64)|否 / —|template_shell/template_revision/model/snapshot/device/group/credential/tenant/subject 等；同一来源可产生多个明确角色。|
|`tenant_id`|UUID|否 / —|已核验目标 tenant，不能空。|
|`target_owner`|VARCHAR(64)|否 / —|正式实体 owner。|
|`target_id`|VARCHAR(256)|否 / —|owner 返回稳定 ID；元数据目标必须有效 UUID，其他 owner 保留不透明格式。|
|`target_revision`|BIGINT|是 / NULL|固定版本，如有。|
|`target_snapshot_id`|UUID|是 / NULL|模型固定快照，如有。|
|`first_source_digest`|CHAR(64)|否 / —|首次建立映射依据；后续源变更由新 item 追踪，不改目标身份。|
|`created_run_id`|UUID|否 / —|FK run RESTRICT。|
|`last_verified_run_id`|UUID|否 / —|最近核验。|
|`mapping_kind`|VARCHAR(16)|否 / —|preserved/created/reused/approved_merge。|
|`created_at` / `verified_at`|TIMESTAMPTZ|否 / NOW()|永久审计时间。|

逻辑唯一元组 `(source_instance,source_tenant_key,source_entity,source_id,target_role)` 以 source_key_digest 唯一索引查找；命中后必须逐项比较原文，hash相同但元组不同按阻断异常处理，绝不合并。目标反向索引 tenant+owner+target_id。默认一对一；若多个来源指向同一目标，必须 approved_merge、有审批决策与逐字段同一性证据，不能因名称相同自动合并。单个 source_digest 不做目标主键，避免源资料变更时新建重复设备。不存在 TTL；普通运行 API、CLI、runtime 不查询本表。

长期正式模板包导入**仅复用源身份→目标身份的映射结构，不使用 `upgrade.migration_id_map` 或其他 upgrade 表**。唯一 owner 为 `devicemetadata`：`metadata_operations.kind=template_import` 固定当前可信 tenant、包 contentDigest 与授权范围；其 `metadata_operation_items` 用包内 source kind/source ID/target role 的规范摘要作为稳定 item_key，在 desired_document 保存来源元组和包摘要，在 owner_receipt 保存正式 owner 返回的 target owner/ID/revision/snapshot ID/摘要。映射与同一导入 operation/items 持久关联，按既有幂等和检查点恢复，不需要创建迁移 run；映射不随下载文件或短期链接过期而丢失。包 UUID 只作来源引用，不能绕过服务端 ID 分配或覆盖另一 tenant 对象。模型导入由既有正式模型 commands 创建/发布，原生 core 决定新 checksum。撤销全部 upgrade schema 权限后，正式模板包导入、同作业重试和断点恢复仍须通过验收。

### 7.4 `upgrade.migration_checkpoints`

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`run_id`|UUID|否 / —|PK/FK。|
|`stage`|VARCHAR(64)|否 / —|tenant/model/template/product/device/relation/credential/consumer/reconcile 等固定阶段。|
|`partition_key`|VARCHAR(256)|否 / —|稳定租户/实体分片；PK `(run_id,stage,partition_key)`。|
|`tenant_id`|UUID|是 / NULL|bootstrap 才可空。|
|`sequence`|BIGINT|否 / 0|单调≥0；CAS 更新。|
|`last_source_key`|JSONB|否 / `'{}'`|稳定排序游标，不是页码 offset。|
|`last_item_id`|UUID|是 / NULL|对应已持久 owner receipt 和映射的 item。|
|`verified_count`|BIGINT|否 / 0|≥0。|
|`batch_digest`|CHAR(64)|是 / NULL|刚确认批次内容。|
|`updated_at`|TIMESTAMPTZ|否 / NOW()|检查点时间。|

检查点只能在该批 owner 结果可恢复、ID 映射/item 状态已持久化后前进；不能先推进 cursor 再调用 API。崩溃后重读最后一批并以相同 key 核对，不靠进程内 map 或文件名推断成功。

### 7.5 `upgrade.migration_anomalies`

|字段|SQL 类型|可空 / DEFAULT|约束|
|---|---|---|---|
|`id`|UUID|否 / —|PK。|
|`run_id`|UUID|否 / —|FK run。|
|`item_id`|UUID|是 / NULL|具体源行；全局异常可空。|
|`tenant_id`|UUID|是 / NULL|未能解析 tenant 时空，不编造值。|
|`code`|VARCHAR(128)|否 / —|如 TENANT_COLLISION/ORPHAN_MODEL/GROUP_CYCLE/UNSUPPORTED_SCRIPT。|
|`severity`|VARCHAR(16)|否 / —|blocking/warning。|
|`field_path`|VARCHAR(512)|是 / NULL|精确字段或关系。|
|`source_reference`|JSONB|否 / —|来源表/ID/列，无秘密。|
|`safe_detail`|JSONB|否 / `'{}'`|损失/无法转换原因、引用目标、数量；脱敏。|
|`state`|VARCHAR(16)|否 / `'open'`|open/resolved/approved_exclude。|
|`decision_ref`|VARCHAR(512)|是 / NULL|resolved/exclude 需要可审计决策或修复证据。|
|`resolved_by` / `resolved_at`|VARCHAR(128) / TIMESTAMPTZ|是 / NULL|同空。|
|`created_at`|TIMESTAMPTZ|否 / NOW()|发现时间。|

索引 run/state/severity、tenant/code。每一源业务行必须归入迁移/复用/其他 owner/明确排除/阻断之一；不能通过丢弃异常行让数量看起来一致。release requires blocking open=0；批准不迁必须有产品/数据所有者决策，不是工程自动把失败降级。

## 8. schema 演进顺序与数据迁移来源

### 8.1 DDL 与发布制品顺序

|顺序|工作 / 前置|观察性验收|任务|
|---|---|---|---|
|S0|核对真实 Encore/Community 版本账本、每份 SQL checksum、数据库主版本与已存在字段；读取全部脏工作区证据但不混入发布制品|同号异内容、未知 ledger、目标版本不支持即停；形成空库/受支持升级路径清单|DM-DB-001、DM-MIG-001/013|
|S1|固定可信 tenant/subject 映射；检测旧 normalizePrincipalID 碰撞；先增同 tenant 唯一键和必要父约束|所有来源身份一对一，任何截断碰撞有异常清单；没有默认零 UUID|DM-API-002、DM-DB-002|
|S2|整合已核验 `0014–0016` 原生核心，追加必要 BIGINT/FK/资源完整性迁移；不能改已应用旧 SQL|空库与升级库达到相同模型 schema；既有核心测试、原始快照 checksum 仍可验证|DM-DB-001/011、DM-API-007|
|S3|演进模板壳/产品/设备基础表与公共审计列；先允许过渡 NULL 仅供离线迁移窗口|不启动新旧并行写；字段完整回填/验证后才能加最终 NOT NULL/CHECK|DM-DB-003/004/011|
|S4|建立模板 revision、绑定、号码预留、网关、组/成员、credentials；建立引用 fence/登记、operation/idempotency/artifact/audit/outbox|同租户关系约束、不可变 trigger、唯一索引全部通过，循环 FK 以可延迟验证处理而非取消|DM-DB-005..010|
|S5|按 §8.2 做一次性转换；旧源只读；消费方显式建立新 ID/版本引用|无法转换/隐藏跨租户引用全部报告，未决不进入 ready|DM-MIG-003..010|
|S6|补最终 NOT NULL/复合 FK/索引并 validate；收紧运行角色/RLS；退役旧projection/旧endpoint读取路径|剥离旧 metadata DB 权限后新 API、前端、CLI、runtime与消费者仍通过|DM-DB-002/012、DM-MIG-014|

上述离线 schema 变更是新制品初始化/维护窗升级的阶段，不表示业务运行期双模型共存。不使用在线双写、旧视图 facade 或旧表 fallback。大表索引需要的受控在线 DDL/锁时长由部署实测安排，不能擅自把一次迁移拆成长期兼容架构。

### 8.2 来源 → 目标与必查转换

|旧来源|目标 / 责任|转换和异常条件|任务|
|---|---|---|---|
|Community tenant/user 与 K 旧归一 ID|认证 owner 身份 + 永久 ID map|B 原始 tenant 原样保留；K models.go:314–355 会删非hex/截断补零，须穷举逆向来源碰撞；无法唯一确定 owner 时阻断，不把两租户并入一 UUID|DM-API-002、DM-MIG-002/007|
|B device_templates + telemetry/attributes/commands/events + custom controls + 图表|既有 thingmodel aggregate/resources/snapshots|复用已核验 converter，四种字段/参数/单位/访问能力、Web/App配置与展示 profile逐项对应；未知控制/extra 字段按 DEC001，不造第二模型核心|DM-MIG-005、DM-API-007|
|B device_configs、device_topic_mappings、data_scripts、扩展资料定义|device_access_templates + 初始不可变 revision|type1/2/3→deviceKind；独立保留 protocol、accessWay、连接方向；模板 secret 分离credentials；Topic优先级、六阶段Lua、customFields不漏；旧 heartbeat秒/online_timeout分钟显式换算|DM-MIG-006、DM-TPL-017..020|
|K 已有 device_templates|同一模板 ID演进 + 初始 revision|解开强制有model约束；识别原/升级 bound_model列冲突，以唯一经确认快照引用为准；冲突不能先选非空值蒙混|DM-DB-004/005、DM-MIG-006|
|B/K products|products，固定可空默认 revision|确认产品tenant、key/type/model/图片/OTA引用；产品默认模板仅建议；名称相同不自动合并|DM-MIG-006、DM-DB-011|
|B devices / K devices|devices + bindings + identity refs|映射deviceId/number/name/kind/product/activation/enable；旧label→tags、已有结构化标签→labels；位置解析范围；batch保留；nullable template合法；设备级protocol_config/服务实例外部id按正式schema转|DM-MIG-007、DM-DEV-024/025|
|旧 voucher/template_secret|统一 device_credentials|按已批准算法验证/加密迁移；nonce/key_id、指纹、用途、owner/注册版本明确；报告无secret；若拒迁秘密则先有可执行重新发放/设备更新方案，不能默认丢凭据|DM-DEC-002、DM-MIG-007|
|旧 parent_id/sub_device_addr|gateway_memberships|先所有设备，再按根→子顺序；允许网关子节点；自环/循环/孤儿/跨tenant/地址重复/深度超限阻断|DM-MIG-008|
|旧 groups/r_group_device + K groups/devices关系|device_groups/device_group_members|旧根0→NULL；不信tier值；保留多组、去重已重复关系并报告；同tenant、无环、同父名、32层|DM-MIG-008|
|图片 path/image_url|文件owner asset + 元数据 image_asset_id|真实bytes迁移或经核验既有asset映射；MIME/size/hash对账；临时URL不能当永久ID；外链抓取按正式文件规则|DM-TPL-021、DM-MIG-006|
|旧自动化/告警/市场/看板/OTA、协议/服务插件引用|各 owner 正式新 ID / 模型快照 / 接入revision|迁移输入按源FK/JSON路径枚举；每条引用改写+登记+回读验证；无业务逻辑复制到metadata|DM-RT-008..012、DM-MIG-010/014|
|旧is_online、last_offline、current_version、运行日志和历史数据|原runtime/OTA/telemetry/control/alarm owner|不复制入元数据；稳定runtimeRef保证历史关联；新状态由运行服务正式观察并标observedAt|DM-RT-001..007/014..017|
|remark1/2/3、additional_info、旧自定义资源或无法表示的值|明确目标字段/其他owner/批准归档|逐字段清单与损失报告，不能“未用字段”默认删除；产品含义未核实者 needs_decision|DM-MIG-002/003/010、DM-DEC-001|

旧框架仅作为实现证据：B go.mod 的实际 Gin/GORM/Casbin 不能推导出某个运行时部署已经存在；迁移日志写真实制品与 owner，不写未经核验的框架名称。

## 9. 一次性迁移的可执行运行手册任务

本节规定未来实现与验收步骤，**本轮不执行**。业务数据是否保留由部署升级范围明确；一旦选择保留，就必须完成该租户全部来源分类和对账，不能仅迁可成功的部分。

### M0 — 备份、授权与可恢复性

固定源/目标实例、允许租户、数据库与应用版本、迁移脚本提交、OpenAPI/CLI/前端/runtime制品摘要；检查空间、锁与连接权限。产生一致备份的 TOC/hash、文件数量、对象存储清单及密钥版本引用；在隔离环境实际恢复并核对关键行数/FK/模型 bytes checksum。备份存在但无法恢复不得进入 apply。秘密备份加密访问，审计不包含密钥。目标若已有数据也须备份，区分迁移前既有共享对象与本次新建对象。对应 DM-MIG-001。

### M1 — 源预检与一致快照

使用一致数据库快照/导出，在 repeatable-read 范围冻结各表同一观察点；需要冻结业务写时覆盖 UI、CLI、API key、市场、自动化、插件与后台服务，不能只禁一个页面按钮。预检 tenant/subject归一碰撞、非UUID、重复编号、孤儿/跨tenantFK、模板/模型空值的合法性、JSON/脚本/图表格式、时间/单位、secret可恢复性、图片可获取性、多级网关/组环与深度。**合法无模板/无模型/无产品作为migrated/reused的正交子集计数，不计为坏数据、不在实体总数等式中重复相加。** 预检is_enabled每一种原值：动态注册源码写enable、普通创建/激活写enabled、自动化严格只认enabled；这是源码不一致，生产分布必须实际统计。显式转换规则须保留raw value、规则版本、来源行与确认依据；未知/NULL不能擅自启用或停用。不导出遥测/连接历史到目标metadata。对应 DM-MIG-002。

### M2 — dry-run 与冻结计划

转换器只读源快照；可以写专用迁移账本/脱敏报告或隔离验证库，生产业务表写入数必须 0。输出每tenant/entity目标数量、源→目标关系计划、内容digest、需要的owner调用序列、异常与潜在损失、已复用资源、批准排除清单。run 固定 converter/contract/source/plan 摘要；重复运行同输入必须产生相同计划（新 ID 可由持久分配/收据恢复，但不能随机导致重复对象）。目标版本/schema/provider能力变化须使旧 plan失效重校验。对应 DM-MIG-003/004。

### M3 — apply 的拓扑顺序

1. 核准 tenant/subject映射和目录/provider/asset owner资源；迁移身份不能取代用户租户授权。
2. 调用既有 ThingModel commands 转换/创建模型与资源，发布完整固定快照，持久源model→目标model/snapshot映射与两个摘要。
3. 迁入单份模板当前configuration，物模型选择可空；图片按asset映射。模板引导凭据只关联当前模板及当前注册策略，默认冻结注册直到正式切换条件满足；不创建草稿或模板版本。
4. 迁产品目录及可空默认revision，再建设备身份/资料与未配置binding；保留原激活/管理事实的经核实值，应用效果仍须runtime核对。
5. 填设备template/directAccess绑定与稳定provider身份，迁凭据校验材料；先校验外部namespace唯一，不重建历史运行身份。
6. 按网关拓扑建立父子；按组拓扑建立树，再成员。检查相同设备多组去重计数。
7. 各消费owner通过正式合同改写规则、看板、市场、OTA等引用并登记，重建正式配置generation/待应用事件；不重放旧projection payload。
8. 核验runtime配置/凭据应用、对象文件、外部引用，再全量内容与关系对账，run变为ready。

所有迁入对象通过同一 owner 业务用例/正式迁移受托入口验证；普通 CLI 不直连DB。需要可信升级写入的特殊字段（来源时间、既有ID/active事实）限专用、可审计迁移上下文，不能放宽公共Create/PATCH允许任意tenant/actor/ID。现有ThingModel converter负责纯转换，最终规范化/发布仍用既有core，不能直接拼写canonical checksum。对应 DM-MIG-005..008。

### M4 — 分批、检查点、幂等与恢复

批次按tenant/entity/source主键稳定排序，大小经实测配置，不以不断变化页码推进。每项在调用owner前持久prepared与稳定幂等键；成功后持久receipt与ID map；完成批后推进检查点。丢响应时先以原key/operation查询恢复，不能“重试新key”。同一source映射已存在时核对身份与digest，已有目标不自动覆盖。

测试在“source读后、prepared后、API提交前、API提交后响应丢失、map写前、checkpoint写前、验证前”逐一杀进程；重启后对象数、ID、revision、凭据发放次数均不增加。跨服务不假装一个数据库事务；ownerreceipt+持久map使可恢复。失败项阻断后续依赖，不能跳过并把run标ready。对应 DM-MIG-009。

### M5 — 全量对账

对账等式为 `来源总数 = migrated/reused + other_owner + approved_exclude + blocking_unresolved`，各项按tenant/entity/source快照计算，不能用“新库行数相同”替代。正式ready要求 blocking_unresolved=0，所有排除有决策。合法未绑定属于已迁入对象的正交标记，不是第五个加项；按字段另记合法NULL数量。关系另按来源边逐项分为已映射、批准移除、阻断，合法原始无边不伪造为一条关系；所有目标引用完整且同tenant。模型字段/命令参数和图表资源分别算数量/内容摘要；模板完整配置、设备身份/生命周期/产品/标签位置、外部namespace、凭据指纹与使用性、网关链、组树、成员集合、消费方引用均逐项核对。目标每条FK无孤儿且同tenant；源/目标身份改写后按规范化逻辑内容比较，不能直接比较含身份的语义hash要求相同。

运行验证只使用测试/受控设备与正式API：凭据可用、模板binding generation真正应用、UI/CLI读取同一版本、模型资源与图表可呈现、history仍由原owner正确关联。旧active-only统计和新全生命周期统计分别列示，解释差异。导出报告包含 run/plan/source/release digest，不含secret。对应 DM-MIG-010、DM-QA-006。

### M6 — 正式直切、失败恢复与旧依赖退役

冻结全部相关metadata写入口→最终一致快照/有界补量计划→apply→全量verify→确认三菜单、CLI、runtime和消费方同一正式制品→一次切换唯一入口→解除写冻结。任何G0–G6阻断项未通过不得解除冻结，不挑一个菜单先试点。

切换前失败：保留维护窗与审计，按账本核对哪些owner已提交；优先恢复/重试同一计划，必要时只撤销本次新建且无外部新引用的对象，不能删掉迁移前已共享资源。恢复目标备份必须与所有受影响owner/receipt对齐，恢复后重新全量校验，不只恢复单表。源系统在切换前保留为只读迁移来源，不构成长期开口。

切换后发现问题：先冻结新metadata写，保留新写入与所有operation/receipt，按同一新schema前向修复。**不得自动回读旧表、恢复旧API或双向补写。**任何另行降级都须独立无损逆转换与完整数据决定；本计划不声称已有该能力。演练至少包括恢复中断、密钥缺失、runtime应用失败、跨owner引用迟到和报告缺失。对应 DM-MIG-011/012。

最后通过网络/代码/数据库权限证据证明三个菜单及所有消费者没有旧Gin请求、旧metadata表运行读写、projection重放或旧IDresolver依赖；移除旧入口的制品注册，保留只读审计备份与永久map。新安装直接使用相同正式schema/API，不先建旧系统再过桥。对应 DM-MIG-013/014。

## 10. 索引、保留期与容量验收

|查询/过程|必须可观察的证据|任务|
|---|---|---|
|设备列表、多条件过滤、选择器与total|5万设备、至少两租户，tenant前导条件索引；items/total同授权范围同快照；runtime/alarm/OTA筛选先求交再分页，不分页后丢行|DM-DB-012、DM-DEV-001/026、DM-RT-002|
|分组1000节点/多组成员、32层树、网关树|父索引+成员反向索引；环检测终止；同设备祖先计数去重；只读查询不升级全库写锁|DM-DB-006/008/012|
|模板版本/模型快照|固定tenant/template/revision与model/snapshot走唯一键；不可变内容校验后再发布；不N+1读模型字段|DM-DB-005/011/012|
|作业/outbox/幂等恢复|pending/lease/expiry索引，worker领取不扫描全部历史；至少一次重复不产生第二副作用；长期tombstone有容量测算|DM-DB-009/010/012|
|来源/目标映射与全量核验|稳定source/target双向索引；关系检查分区执行但覆盖率100%；map/checkpoint不落临时进程内存|DM-MIG-004/009/010|

业务tombstone、不可变revision/snapshot和永久migration map默认不自动过期。普通audit/作业详情保留期、密文响应24h、模型receipts30天、artifact TTL分别由发布配置冻结，不能将一个通用清理任务删除所有数据。长作业的稳定ID及最小恢复收据必须覆盖最长重试期；任务尚未结束、outbox未应用、reference尚未核实的记录禁止清理。文件expired记录留下授权与审计元数据，下载返回410。长期容量达到阈值时采用owner负责的归档/分区方案，不能为省空间删除仍被revision/FK引用的模型快照。

`EXPLAIN (ANALYZE, BUFFERS)` 等实测只在脱敏代表性/隔离数据库执行；记录硬件、Postgres版本、数据分布、计划、p95和锁等待。性能数字不是本轮已完成事实；通过门槛以 TEST-PLAN 的冻结环境为准。

## 11. 数据库合同验收矩阵

这些用例 ID 可直接进入自动化测试名称与验收记录；每条同时关联任务及真实API/表，不只写“补测试”。本轮全部为**待执行**，不存在因SQL文件存在而宣称通过。

|用例 ID|输入 / 操作|可观察通过条件|任务 / API / 表|
|---|---|---|---|
|DB-C01|同租户/跨租户各造相同编号，两个并发请求造同租户同号|不同tenant可建；同tenant仅1行、另409；跨tenant详情404；SQL不可插跨tenant FK|DM-DB-002/003；createDevice；devices|
|DB-C02|无模板无模型无产品登记设备；再以directAccess接入服务实例|首次inactive合法；正式配置/凭据/激活后runtime可用；没有虚构Product/ThingModel|DM-DEV-024；createDevice/setDeviceTemplateBinding；bindings|
|DB-C03|直接保存模板当前配置；物模型发布新版；模板显式改选物模型版本|无模板草稿/版本/发布；模型仅发布不改模板，模板显式改选后关联设备共享该选择|0028/0029；DM-DB-004、DM-TPL-005|
|DB-C04|绑定模型A的snapshot，却提交模型B或异tenant版本/digest|公共写只收选择器，服务拒绝不匹配；复合FK阻止绕过API；模型空值五列全空合法|DM-API-007、DM-DB-011；revisions/snapshots|
|DB-C05|仅改图表资源；发布新模型版本；读取固定旧版|resourcesChecksum变化按其算法，旧版不变；不得把语义checksum等同资源hash；新版本输出两个摘要|DM-API-007；getThingModelVersion；0015/0016|
|DB-C06|deviceNumber改号与另一create并发抢同目标号；在runtime确认前杀worker|只有一个合法占用；未确认不释放预留；恢复同operation；成功后旧号不再当前、deviceId/runtimeRef不变|DM-DEV-021；changeDeviceNumber；reservations|
|DB-C07|gateway→gateway→subdevice多级绑定；并发A→B/B→A；移动含深子树|支持合法多级；自环/祖先环/33层/地址重复明确拒绝；没有双父或半关系|DM-DEV-018/027；gateway_memberships|
|DB-C08|同parent/根重复组名、不同parent同名、跨tenant父、并发循环move|同parent/根重复409；不同parent合法；跨tenant404；32层通过33层失败；树可终止遍历|DM-GRP-003/005；device_groups|
|DB-C09|一设备加入两个子组，祖先includeDescendants；同批混一越权设备|祖先结果只1设备并显示matchedGroupIds；越权批全不写；重复加入/移除幂等|DM-GRP-007/008/009；members|
|DB-C10|普通设备/模板导出、错误日志、trace、审计、幂等表检查|无plaintext token/password/privatekey/reauth；credential owner XOR可直接SQL校验；hash-only reveal409|DM-DB-007、DM-DEV-011、DM-TPL-020|
|DB-C11|先建pending replacement，轮换重复10次、确认后再次重试|仅一replacement被消费；overlap_ends_at不续期；旧凭据按窗撤销；runtime不可用则不伪成功|DM-DEV-010；credentials/operations|
|DB-C12|同key同body并发、同key不同body、24h后重放、secret缓存后撤权|同实体/operation响应；异body409；过期409不再执行；撤权后secret不可重放|DM-API-005；metadata_idempotency|
|DB-C13|业务提交后断网/杀进程，重复派送/乱序generation|状态+audit+outbox原子存在；runtime重复只1次、旧generation拒；outbox delivered不等于作业succeeded|DM-DB-009、DM-RT-007|
|DB-C14|规则owner reserve后延迟confirm，与delete并发；owner失联/租约过期|deleting fence拒新引用；未决reserve阻断delete；不能自动过期当无引用；不可见引用只返回类型/原因|DM-API-006、DM-DB-011；reference_edges/fences|
|DB-C15|导入同源同row重试、提交digest变更、批处理中第N项失败|稳定目标ID，无重复；变更拒绝；report准确已成功/失败；checkpoint不越过未持久结果|DM-DEV-013/014、DM-DB-010|
|DB-C16|导出后撤销发起动作权限、换tenant下载、文件过期|逐次重检originating operation；无权限403/不可见404；过期410；DB不存signed URL|DM-API-008、DM-DEV-015；artifacts|
|DB-C17|删除设备前后比较telemetry/history owner数据及外部引用|有引用/孩子先409；隔离撤销确认才tombstone；历史数量及stable identity关联不变|DM-DEV-005、DM-RT-007；devices|
|DB-C18|两原tenant经旧normalize产生同UUID；非法旧ID/缺父/空合法模板并存|碰撞/孤儿阻断；合法无模板不报损坏；原始tenant/ID完整留map；不默认零租户|DM-API-002、DM-MIG-002/007|
|DB-C19|同快照dry-run两次；在每提交断点杀迁移runner并重跑|计划digest一致；dry-run业务写0；最终数量/关系/凭据发放次数一致；永久map不依赖临时文件|DM-MIG-003/004/009|
|DB-C20|空库与每条支持升级路径，含K37前后快照|终态schema/约束等价；已应用SQL checksum不变；旧无resources快照不被伪造补写；必要新快照有映射|DM-DB-001、DM-MIG-005/013|
|DB-C21|切换后撤销运行角色对旧metadata表权限并阻断旧Gin入口|三菜单、CLI、runtime、消费方功能与E2E仍通过；无任何旧fallback/双写查询|DM-MIG-014、DM-QA-010|
|DB-C22|应用角色未SET tenant、复用连接切tenant、尝试直接SQL改变tenant|缺上下文拒绝；不能看到上一租户；WITH CHECK拒跨tenant写；后台作业范围不越权|DM-DB-002、DM-API-002/003|

## 12. 实施拆工映射与仍需冻结的事项

|数据库工作包|具体交付|任务|
|---|---|---|
|迁移账本与通用约束|受支持升级拓扑、checksum清单、E/D/RLS/复合FK与运行角色|DM-DB-001/002|
|设备与改号|devices演进、编号预留锁/约束、合法空关系、生命周期与自定义资料|DM-DB-003、DM-DEV-021/024/025|
|模板当前配置|唯一模板owner、完整JSON验证、CAS保存、共用物模型选择；无模板版本表|DM-DB-004、DM-TPL-005|
|组与网关|现有组表演进、成员反向索引、两类树防环/深度/并发、批关系作业|DM-DB-006/008、DM-DEV-027|
|凭据|统一owner XOR、保护材料、用途/版本绑定、轮换/撤销/可恢复性|DM-DB-007、DM-DEC-002|
|公共事务/作业|演进单套audit/outbox/idempotency、jobs/items/artifacts、origin授权与TTL|DM-DB-009/010、DM-API-005/008|
|产品/模型/外部引用|可选产品、同库固定模型FK、跨ownerreference+fence、删除保护|DM-DB-011、DM-API-006/007|
|性能与容量|实测查询计划、锁竞争、期限清理、永久map/快照容量|DM-DB-012|
|一次性数据升级|本节5张账本表、转换器、备份恢复、dryrun、分批、全量对账、直切退役|DM-MIG-001..014|

发布前仍需产品/数据owner决定而非工程猜测的内容：旧自定义控制/无法表达资源的明确处置（DM-DEC-001）；是否保留可恢复凭据及历史明文导出相关产品能力（DM-DEC-002）；支持的源版本、部署形态、维护窗、保留期及恢复目标（DM-DEC-003）；按编号入口是否开放、创建向导是否自动触发激活，以及若要求改变已核实的默认归组行为才需批准（DM-DEC-004）；active真值已固定为runtime确认，不待产品重定。移动端和已核实消费者仍包含在本次完整切换范围内。这些决定不会阻止本轮完成规划，但未关闭时属于对应功能发布阻断项。

可由代码确认的事项不推给业务方：模板与物模型命名分离、合法无模板设备、多级网关、可改设备编号、现有产品用途、协议/接入方式/连接方向三者独立、运行状态/历史不入metadata、SQL真实账本须核对、既有ThingModel核心复用，均已作为本设计约束。

## 11. 本轮补充的运行边界

服务发现快照由connector-runtime临时保存，不新增metadata表。配置交付事件/receipt以openapi.yaml的DeviceConfigurationEvent、DeviceDesiredConfiguration、RuntimeConfigurationReceipt为唯一结构；metadata_outbox保存固定期望配置，runtime应用账本和appliedGeneration仍在runtime owner。事件与metadata期望generation同事务提交；既有runtime采用可恢复的应用/回执机制，不靠本规划另造数据库模块。收到/应用/失败/过期分别可验，schema与行为见API-CONTRACT §9.2、DM-RT-007。

### 产品目录的受控离线写入

本次公共API只有产品list/get，产品菜单完整管理不在范围。DM-MIG-006使用同一devicemetadata产品owner内部升级用例（不注册HTTP/CLI旁路），输入遵从同一openapi.yaml组件ProductMigrationRecord，run固定允许tenant/版本/digest。sourceTenantId只作来源证据，可信tenant取批准run和永久身份映射；产品owner复用其名称/key/引用校验并在同一提交留下收据，迁移账本持久映射source ID。target ID不可由普通用户指定，重复source只核验既有结果，不能按名称合并。每日运行身份无该入口权限，撤销升级身份后运行API仍不依赖upgrade表。此为一次性迁移实现，不增加长期API或产品owner。


**显式激活与凭据内部合同**：DeviceDesiredConfiguration.required包含desiredActivationState；inactive+enabled即使凭据已配置仍不可连接，激活用例发active期望，只有applied后元数据activationState才确认。`x-runtime-private-contracts.resolveRuntimeCredential`是同一devicemetadata凭据owner的Encore private调用（不注册公共HTTP，不新增服务）；输入ResolveRuntimeCredential，输出敏感RuntimeCredentialMaterial，错误复用Problem。runtime/provider机器主体需credential.runtime:resolve，可信tenant来自服务上下文，且credential必须由同device已提交generation明确引用，fingerprint匹配；pending只在正在应用的active意图中可解析，revoked拒绝，旧代冲突。响应<=60秒有效；verifier、owner解密材料或公开certificate三种模式互斥，经双向认证或等价的加密服务传输交付；无秘密outbox/日志/trace/普通HTTP，运行侧确需存储只用既有加密设施。CLI/MCP/普通用户无入口，不能以scope字符串伪装机器身份。该内部合同不计入109个HTTP operation，私有schema生成与服务合同另由DM-DEV-009/DB-007/RT-007验收。

### 2026-10-27 凭据存储落地边界

`0030_device_credentials.up.sql`已在隔离Encore测试库应用，校验和固定；后续只能追加迁移。当前表为device/template XOR owner、generated owner kind/id及purpose，同租户/同owner替代关系FK；pending/active/revoked，撤销不可反转。AES-256-GCM nonce12字节，秘密1..8192字节，公开证书上限32768字节。存储基础不代表接入、轮换或runtime解析已经完成。

正在实施的0031仅扩展既有metadata_idempotency：秘密请求摘要为HMAC及key引用；完整响应AEAD加密并绑定tenant、主体、operation、path、key、credential。普通JSON收据流程继续独立使用原字段；不新建receipt服务或owner。秘密创建响应首次窗口10分钟，重试不续期；过期重放时清密文保留409墓碑，每次重放重新授权且已撤销凭据拒绝。当前只实现请求触发的擦除，定期清理未访问过期密文仍待补齐；不可将此基础实现当作完整保留策略已交付。此节为实施状态，待定向测试后补通过证据。
