# CLI 交付合同：设备、设备接入模板、设备分组

> **当前设计优先约束（2026-10-27用户纠正）：设备模板不做草稿、版本表、发布或模板切版；只有物模型保留版本。** 此前相反的候选设计已撤回。原188/580/108是历史规划规模，不能再作为拒绝收简的约束；接口/DB/CLI/用例正按新边界重写。详见[设计纠偏与影响检查](DESIGN-CORRECTION-2026-10-27.md)。相关旧测试仅保留历史证据，不证明新需求完成。


> 这是本次原生直切的交付规划，不表示命令已经实现。HTTP 路径、方法、字段、权限和响应以同目录 [openapi.yaml](openapi.yaml) 为唯一权威；本文规定命令行界面、输出、退出码及可执行验收场景。与 [TASKS.md](TASKS.md)、[API-CONTRACT.md](API-CONTRACT.md)、[DOMAIN-MODEL.md](DOMAIN-MODEL.md)、[TEST-PLAN.md](TEST-PLAN.md)、[DECISIONS.md](DECISIONS.md) 交叉使用。

## 1. 实施结论及当前证据

CLI 与 Web 同期交付。采用一个正式 `tpctl` Go 可执行程序，保留 `cli/tpctl/tpctl` 的 shell 启动方式。命令通过从 OpenAPI 生成的 Go 类型和客户端调用正式 Encore API。CLI 不连接数据库、不调用 Community Gin API、不调用旧 Encore 代理、不使用 runtime-projection 同步旧 `device_config_id`，也不承载领域规则。

当前源码只能证明已有启动框架和部分命令，不能标记本合同完成：

| 当前证据 | 已有内容 | 本次处置 |
|---|---|---|
| Kernel `cli/tpctl/README.md:13-17` | shell launcher 调用 `cli/tpctl-go`，复用构建产物 | 复用启动和构建方式；记录发布二进制版本与规范版本 |
| Kernel `cli/tpctl-go/main.go:34-91` | `devices` 命令主要调用 `/v1/devices/*`，把请求包成字符串 `payload` | 替换为本文正式操作；不能以访问Encore主机为由认定它是原生业务 |
| Kernel `cli/tpctl-go/main.go:157-189` | group 命令调用 `/api/device-groups`，已有CRUD/单个成员操作 | 可复用通用HTTP处理经验；路由、DTO、CAS、批量授权按新合同实现并验收 |
| Kernel `cli/tpctl-go/main.go:193-197`；`cli/tpctl/README.md:44-46` | template 只有 runtime-projection；说明中仍要求维持旧device_config_id | 在受影响发布范围退役；不能保留成桥接或回退选项 |
| Kernel `cli/tpctl-go/main.go:307-340,343-351,908-945` | 通用HTTP调用、raw解包、旧错误退出行为 | 改成正式响应/Problem及本文退出码；错误不得打印含secret的完整原始报文 |
| Kernel `cli/tpctl-go/main_test.go:5-54` | onboarding 参数解析的测试源 | 只计测试源存在；未覆盖三领域HTTP、权限、幂等、数据库及CLI端到端 |
| Kernel `cli/tpctl-go/README.md:3-28` | onboarding 的说明、身份变量和Encore base | 可复用授权环境接入；新设备登记/凭证/激活必须显式调用正式用例，CLI不能拼出第二套onboarding领域流程 |

当前提交/工作区证据见 [EVIDENCE.md](EVIDENCE.md)。上述混合工作区文件不整体复制、不重置；实施前应固定候选SHA并选择性合入。正式命令改为单数 `device`、`template`、`group`；`template` 唯一表示 **设备接入模板 DeviceAccessTemplate**。`thing-model` 继续指既有物模型服务。旧的同域命令在新版本报告替代命令并退出2，不发送任何旧路径请求。

## 2. 通用命令合同

### 2.1 层级、参数和身份

```text
tpctl device ...
tpctl template ...
tpctl group ...
tpctl product list|get ...
tpctl provider list ...
tpctl thing-model ...
tpctl operation get|items|wait ...
tpctl artifact download ...
tpctl asset upload|get ...
```

`product` 和 `provider` 只提供本次选择器所需只读能力，不借此重做产品或协议执行服务。物模型命令复用既有核心与正式transport，不能另建物模型业务。

| 通用参数 | 规则 |
|---|---|
| `--context NAME`、`--api-url URL` | 从本机配置选择已授权的正式Encore环境；URL必须是批准的origin/base，路径来自生成客户端。配置为旧`/api/v1`、旧`/v1/devices`或其他受禁入口时拒绝。生产使用TLS；仅明确本机开发origin允许HTTP。不能因503自动换base或旧API |
| `--token-file FILE` | 从受限文件读取正式access token；也可复用现有 `TP_ENCORE_TOKEN` 等已授权输入，但不得回显。凭据来源冲突时退出2。token不放query，不新增调试身份头 |
| `--file FILE` / `--file -` | 读取一个正式request schema的JSON；`-`为stdin。拒绝重复JSON键、未知字段和错误类型。与同一body的便利参数互斥，防止无声覆盖。文中`--file`不表示绕过生成类型 |
| `--output table\|json\|jsonl` | 默认交互终端table；非交互默认json。输出格式不改变API调用、字段含义、鉴权或业务语义 |
| `--page N --page-size N` | 映射`page/pageSize`；page从1开始，pageSize为1–100，默认20；不能默认取前99条后宣称完整 |
| `--all` | 仅分页列表命令可用；从第一页按正式分页规则取完；与显式`--page`互斥。每页相同过滤、排序、身份。跨页实时列表不是冻结快照，审计/一致导出使用export作业 |
| `--idempotency-key KEY` | 写操作按OpenAPI要求发送；8–128字符，字符范围与合同一致。非交互写入必须显式给出；交互可生成一次并在stderr记录非敏感key，重试同一请求继续用同一key |
| `--if-match ETAG` / `--if-match-file FILE` | 必须使用对应资源GET返回的原样强ETag；保留引号。两个输入互斥。拒绝弱ETag、`*`；绝不隐式GET最新ETag再覆盖用户意图 |
| `--save-etag FILE` | GET命令将响应ETag单独保存；不存在ETag时明确报合同错误，不能从resourceVersion自行拼字符串 |
| `--expected-version N` | 映射某些POST操作的`expectedResourceVersion`、`expectedDeviceVersion`、`expectedGroupVersion`或模板资源CAS的`expectedTemplateVersion`，具体见操作表；不是所有操作都附加If-Match |
| `--wait --timeout 300s --poll-interval 2s` | 明确等待已受理作业或命令回执；默认不等待。等待只轮询原作业，遵守Retry-After并有总时限，不重新提交业务写入 |
| `--yes` | 删除、撤销、解除关系、取消待发命令等交互确认的自动化显式选择；只关闭本地确认，不替代服务鉴权、引用检查、CAS或幂等 |
| `--debug` | 只写stderr：operationId、路径模板、requestId、耗时、HTTP状态、脱敏header；不记录token、secret、reauthToken、完整请求体或设备报文 |

**没有 `--tenant-id`、`X-Tenant-ID` 或payload中的tenantId选择功能。** 租户和资源范围由正式服务从可信身份解析。切租户使用身份系统正式签发的另一个授权context；CLI不能把JWT未验证payload或本地配置当作服务端租户证明。响应中的tenantId只读。注册引导凭证具有独立用途，只能用于 `device enroll`，不能替代普通管理token。

CLI可做生成schema规定的语法/类型/范围校验；设备编号唯一性、角色、跨租户、网关成环、模板兼容、命令参数业务意义、删除引用和运行时状态均由正式API判定。所有便利参数只序列化为该API的字段，不根据旧表名或经验生成额外业务状态。

### 2.2 输出与退出码

普通单次 `--output json` 将正式响应envelope原样作为一个JSON对象输出，保留lowerCamel字段、null、数值、枚举、`meta.requestId`；不会重建旧`code/message/data`、解包`raw`、把202伪装成功完成，或因输出方便把ID改成数字。

分页列表 `--output jsonl` 每行是`data.items`中的一个原始typed item；其语义是明确的列表投影，分页/requestId摘要写stderr。`--all --output json` 输出每个原始页envelope组成的JSON数组；不伪造一个服务端从未返回的总页envelope。无数据jsonl为0行、退出0。SSE的jsonl每行是一个原始`ObservationEvent`，规则见运行时命令。

table仅渲染展示列；大字段可截断但ID不截断，空值显示`-`，unknown保留为unknown。设备默认列为ID、deviceNumber、name、deviceKind、administrativeState、activationState、templateId；运行连接状态单独由state/watch命令读取，不能在普通列表隐式逐设备请求runtime。模板列含ID/name/管理状态/当前配置摘要；分组列含ID/name/parentId/sortOrder。

stdout只含成功数据或显式流数据；帮助/提示/进度/可重试信息/错误写stderr。HTTP错误时stderr按json/jsonl模式输出原始Problem，stdout为空；客户端输入/网络错误用单独`cliError`对象，带`code/message/requestId`（没有requestId则null），不伪造服务器HTTP Problem。`--wait`最终失败可以输出最后一次合法作业响应到stdout，再以非0退出，供自动化定位已发生的部分变化。

| 退出码 | 唯一含义与例子 |
|---|---|
| 0 | 请求成功；查询负结果如`available=false`或`eligible=false`仍是成功查询。未带--wait的202只表示已受理，输出真实state和ID |
| 2 | 命令/本地文件/请求输入不合法；缺必须key/ETag/确认；HTTP400/413/415/422/428；import校验发现无效行；脚本沙箱返回可解释的语法/约束失败 |
| 3 | 缺失、过期或失效身份，HTTP401；不能刷新成别的租户后重发 |
| 4 | 禁止或资源不可见，HTTP403/404；不通过不同输出区分“别的租户存在”和“根本不存在” |
| 5 | 业务状态/重复键内容/CAS/引用冲突，HTTP409/412；受控结果过期HTTP410；已投递命令不能取消 |
| 6 | 已受理作业最终failed/cancelled，包含批量部分成功；等待命令最终failed/timed_out/expired/cancelled。显式cancel命令成功得到cancelled仍退出0 |
| 7 | DNS/TLS/连接失败、网络或本地等待超时、达到重连/重试预算；429等待预算耗尽。保留operationId/executionId和原key，未知结果不得再造key重提 |
| 8 | 正式服务端失败，HTTP5xx；runtime依赖不可用503不能伪报离线/空列表 |
| 9 | API版本、响应schema、Content-Type、分页或流合同不兼容；受禁旧envelope；resync缺口需重新取得基线。不能静默删掉未知必需语义继续执行 |
| 130 | 用户SIGINT；停止本地轮询/流并关闭连接，不代表服务器作业被取消 |

进程对可解析的新可选字段可以保留；遇到未识别枚举/必需结构缺失不得自行猜业务意义。客户端支持版本与OpenAPI代码生成产物一起发布，破坏性字段/状态变更触发新合同版本；本次不保留旧HTTP接口兼容。

### 2.3 幂等、并发和等待

- 自动化写入必须显式提供固定key；同key重试保持路径、body和前置条件不变。同key不同body服务409、CLI退出5。只能在明确相同请求、仍在服务去重窗口且身份不变时重试；响应丢失且去重窗口不明时停止并人工/作业核对，不擅自生成新key。
- HTTP客户端不因为POST/DELETE失败自动换路径、不重新解析某个“latest”版本、不自动重发请求，也不在412后自动GET新ETag继续写。GET重试/429等待有界，遵守服务Retry-After。任何重放前服务仍重检授权。
- 创建设备优先复用1.3已正常的“模板选择已发布物模型→基于模板创建设备”；CLI没有扫码、企业版产品管理激活和按编号领取资格命令。已有Community设备状态按真实行为核对，不凭状态字段新增扫码流程。
- 模板绑定/解绑直接提交templateId或directAccess，并使用当前绑定ETag；不调用模板切换preview，也不需要previewToken。runtime应用状态按configurationGeneration确认。
- `operation wait`与所有`--wait`保留真实pending/running状态。`succeeded`才是全部要求完成；批次failed需同时输出items，不能把成功的90项掩盖在一句失败中。设备/模板/网关batch最多100，CLI不自动切成多批改变原子或部分成功语义。
- 命令回执等待使用`getDeviceCommandExecution`，不是metadata operation。`device command submit --wait --until acknowledged`默认等待协议ACK；可明确选`accepted`或`delivered`。任何ACK都不声称物理效果；无ACK能力的协议不得伪造acknowledged。

### 2.4 秘密、下载和文件

设备凭据创建/reveal、模板注册凭据创建和设备enroll会返回秘密。这些命令必须在发请求前要求 `--secret-output FILE`；完整原始响应写入当前用户独占、0600、新建且非符号链接的文件。默认stdout为空，stderr只报非敏感凭据ID和保存路径。显式 `--secret-output - --allow-secret-stdout --output json` 才允许将该受授权响应写stdout；禁止自动复制剪贴板和写普通trace。已有文件默认不覆盖，输出路径不安全时不先创建设备凭据。

秘密输入、reauthToken使用受限文件或stdin，不能使用`--secret value`、URL参数或含秘密的shell内联JSON。普通list/get/export、表格、JSONL、审计摘要不包含秘密。证书只提交公开证书，元数据服务不接收私钥。设备reveal仅在正式`device:credentials:reveal`权限及reauth通过后执行；不可恢复摘要返回409，不能另查数据库取回。

模板引导凭据当前合同只有list/create/revoke，没有reveal/自动rotate；轮换要显式create新凭据、通过runtime验证，再显式revoke旧凭据。CLI不发明隐藏多步骤事务。

导入/导出下载和图片上传采用明确 `--file/--out` 路径。文件下载先写临时文件，校验正式checksum后原子命名；不跟随携带授权header的跨origin重定向。410/撤权时删除未完成的临时文件并报错误。下载只读正式受控artifact API或file-storage批准的授权URL，不能下载旧后台路径。导出不包含秘密；CSV防公式规则由服务完成，CLI不重新实现领域导出。`template export --out package.json`将响应的data按严格TemplatePackage写入文件，使该文件可直接作为import请求；若同时要求`--output json`，stdout仍是完整正式TemplatePackageResponse，不把两个结构混淆。导出文件的包formatVersion、contentDigest、modelPackage.checksum和resourcesChecksum含义各自保持。

## 3. 功能命令与字段映射

以下为规范命令名；完整逐operation映射见第7节。`D/T/G/E/O`分别表示设备、模板、组、命令执行、异步作业的不透明ID；全部值必须来自正式响应/受控导入映射。

### 3.1 设备与关系

| 命令 | 参数及精确业务范围 | 前置条件 / 结果 |
|---|---|---|
| `device list` / `device stats` | 支持本节全部设备filter；stats没有page | runtime/告警/OTA过滤由服务在分页前求交，CLI不本地删减当前页 |
| `device get D` | 设备元数据、templateId及固定model引用及desired配置，字段以Device响应为准 | `--save-etag`供update/delete；applied配置由`device state D`读取，不在metadata响应中制造第二事实源，也不隐式串接runtime查询 |
| `device create --file create.json` | 完整CreateDevice；便利参数可用`--name --device-number --kind --product-id --template-id --initial-state` | 无模板合法；模板与directAccess二选一；返回登记结果，不能宣称可连接 |
| `device create --direct-access-file direct.json ...` | 显式序列化DirectAccess到CreateDevice.directAccess；externalDeviceId与access来自正式provider；access.parameters是公共配置，access.serviceInstanceId是服务实例；逐设备deviceParameters放CreateDevice顶层 | 用于服务接入、无模板协议设备；不另造service-device CRUD；与templateId互斥 |
| `device update D --file patch.json --if-match-file device.etag` | name/description/labels/tags/location/customFields/productId；JSON部分更新，给出的对象整体替换 | 省略不改，nullable的null才清空；不能通过此命令改number、template、parent或管理状态 |
| `device renumber D --device-number N --reason R --expected-version N` | changeDeviceNumber；保留deviceID和历史runtime身份 | 202及runtime应用回执；重号或不允许改号报冲突，不建别名旧API |
| `device delete D --if-match-file device.etag --yes [--wait]` | 删除用例与运行时撤销 | 引用冲突5；202未完成不打印deleted |
| `device administrative-state get D` | 取得状态与该子资源ETag | enabled/disabled与online/offline不同 |
| `device enable D` / `device disable D` | 同setDeviceAdministrativeState，body只有state | 使用该状态资源ETag+幂等；等待真实runtime确认 |
| `device number-check --number N [--exclude-device-id D]` | 业务编号格式/唯一性查询，按schema允许字段 | 不枚举全平台编号；negative result退出0；不是激活资格 |
| `device activate D --expected-version N [--wait]` | 调用同正式activateDevice | 不隐式重置凭据、不重复生成身份 |
| `device template get D` | 查看当前templateId/directAccess绑定及精确ETag | templateId指向模板当前配置；null合法 |
| `device template bind D --file binding.json --if-match-file binding.etag` | SetTemplateBinding完整输入：templateId或directAccess二选一，并可带deviceParameters | 直接保存绑定；使用当前配置，不需要preview token |
| `device template unbind D --if-match-file binding.etag` | 同setDeviceTemplateBinding，templateId=null且directAccess=null | 不删设备；按配置generation确认runtime应用 |
| `device template batch-bind --file batch.json [--wait]` | BatchTemplateBinding.items 1–100，逐项deviceId/expectedResourceVersion/templateId（可为null） | 整批格式与授权预检，逐项持久结果；部分失败exit6；不需要preview token |
| `device gateway get D` | 读取本设备父关系及ETag | D可为subdevice或具有父的gateway |
| `device gateway bind D --gateway-id G --address A --if-match-file relation.etag` | 单关系setGatewayMembership | gateway也可挂gateway；同tenant、无环、最大深度32、地址唯一由服务校验 |
| `device gateway unbind D --if-match-file relation.etag --yes` | gatewayId和subdeviceAddress同时null | 仅解除关系，合法detached设备仍存在 |
| `device gateway batch --file gateway-batch.json [--wait]` | 1–100项，deviceId/expectedResourceVersion/gatewayId/subdeviceAddress | 混合绑定/解绑以显式null表达；不接受逗号拼接ID；逐项runtime确认 |
| `device subdevices list D` | 当前网关直接子设备，分页/search | 不暗中递归全树；多级树可按明确父ID逐层取 |
| `device groups list D` | 当前设备反向成员关系，分页 | 多组关系无影子group_ids事实源 |
| `device enroll --file enrollment.json --enrollment-credential-file bootstrap --secret-output result.json` | 正式enrollDevice；凭据服务端确定tenant/template/版本 | 仅此命令使用引导凭据；body无tenantId；CLI不拼create/credential/activate |

设备filter逐一映射如下；`device list`、`device stats`和export的filters JSON使用相同正式字段：

| CLI flag | OpenAPI字段 | 值/说明 |
|---|---|---|
| `--search` / `--number` | search / deviceNumber | search按name+业务编号；number精确匹配；UTF-8不做未经合同允许的大小写折叠 |
| `--kind` | deviceKind | standalone / gateway / subdevice |
| `--template-id` / `--has-template` | templateId / hasTemplate | 可选模板或true/false；false用于未绑定候选；不推断为model=null |
| `--product-id` / `--model-id` | productId / modelId | 产品与物模型分别过滤 |
| `--group-id` / `--include-descendants` | groupId / includeDescendants | direct或子树集合按合同；不在CLI自行递归过滤 |
| `--group-scope` | groupScope | all / ungrouped；与具体groupId互斥由schema/服务规则统一验证 |
| `--gateway-id` / `--gateway-scope` | gatewayId / gatewayScope | all / bound / unbound；不能把网关关系当分组 |
| `--administrative-state` / `--activation-state` | administrativeState / activationState | enabled/disabled；inactive/active |
| `--connectivity` / `--alarm-state` | connectivity / alarmState | online/offline/unknown；any/active/none；都是相关owner的观察语义 |
| `--label` | label | 结构化labels筛选语法由API固定；不是把旧label逗号串复制为新kv标签 |
| `--access-way` / `--protocol` | accessWay / protocol | protocol/service；协议字符串来自正式provider目录 |
| `--provider-id` / `--service-instance-id` | providerId / serviceInstanceId | provider定义与执行实例分别传值 |
| `--firmware-version` / `--batch-number` | firmwareVersion / batchNumber | 固件观察来自OTA；业务批次字段独立 |
| `--sort` | sort | createdAt/-createdAt/name/-name/deviceNumber/-deviceNumber；服务追加稳定ID排序 |

### 3.2 凭据、导入导出与作业

| 命令 | 输入 / 调用范围 | 必须验证的行为 |
|---|---|---|
| `device credential list D` | 分页摘要；`device:credentials:read` | 无secret；普通device:read不隐式获取凭据权限 |
| `device credential create D --file credential.json --secret-output secret.json` | token/username_password/x509；创建pending凭据 | 同key响应丢失可授权重放；不自动启用、不自动轮换 |
| `device credential reveal D C --file reveal.json --secret-output secret.json` | reason、reauthToken；`device:credentials:reveal` | 无独立权限403；不可恢复摘要409；不从DB旁路 |
| `device credential rotate D C --replacement-id NEW --overlap-seconds 60 --expected-version N` | 只引用先前显式创建、同设备pending的新凭据 | 同key不重复消费新凭据或延长重叠期；等待runtime确认 |
| `device credential revoke D C --reason R --expected-version N --yes` | 正式撤销；device:credentials:write | 不删除历史摘要；新旧device API不并存，重叠只是明确凭据轮换窗口 |
| `device batch-create --file devices.json` | items[{itemKey,device}]，atomic=true，1–100 | 元数据全有或全无；不自动激活；CLI不将101拆成两个事务 |
| `device import format --format csv --out devices.csv` | 正式导入模板下载 | 列名和版本从服务取得，不维护另一份手写模板 |
| `device import validate --file devices.csv --format csv [--wait]` | multipart，mode=validate | 只生成校验作业，不产生设备；错误行、sourceDigest、validationDigest可读 |
| `device import get IMPORT_ID` | 正式校验报告 | importId取正式ImportValidation.importId字段，不能猜测operationId=importId |
| `device import commit IMPORT_ID --source-digest ... --validation-digest ... [--wait]` | 仅可提交无错误且未改变的验证结果 | 已过期/失权/摘要不同拒绝；断线后查询原作业，不重新上传产生新事实 |
| `device export --file export.json [--wait]` | deviceIds或filters二选一；format/fields遵循schema | 冻结授权后的选择集；125条筛选导出125条；无secret列 |
| `operation get O` / `operation items O --all` | 正式Operation/OperationItem响应 | 依原始operation权限授权，不硬编码device:read；itemKey/row/resourceId/error完整 |
| `operation wait O --timeout 300s` | 只GET轮询已有作业 | 超时exit7保留ID；失败/部分失败exit6；不自动重提或取消 |
| `artifact download DOWNLOAD_ID --out result.xlsx` | 正式下载与checksum | 撤权/410不下载，hash不符exit9；完整文件原子命名 |

### 3.3 接入模板、Topic、脚本、资料定义和注册凭据

本节中模板copy/import/export作为保留能力继续列于目标接口；它们不属于本轮核心验收必做项。既有市场包发布/安装由市场owner继续负责。

| 命令 | 范围 | 前置条件 / 结果 |
|---|---|---|
| `template list` / `template get T` | search/deviceKind/protocol/brand/tag/modelId/administrativeState/sort，分页；详情及ETag | 展示模板唯一一份当前配置，没有published筛选或模板revision历史 |
| `template create --file template.json` | name、展示信息、imageAssetId、完整TemplateConfiguration | model可以null；access/schemaDigest/providerVersion明确；不自动创建空模型 |
| `template update T --file patch.json --if-match-file template.etag` | 名称、展示信息、管理状态或完整configuration；JSON部分更新 | 省略不改，nullable的null清除；configuration/数组/对象整体替换并直接保存当前值 |
| `template enable T` / `template disable T` | 同update，仅administrativeState | disabled阻止新绑定/新激活；现有绑定不由CLI隐式解除 |
| `template validate T --file configuration.json` | TemplateConfiguration，服务端校验当前配置的模型/provider/Topic/script/资料规则 | 只返回校验报告，不写入、不发布；不是保存前必须取得的token |
| `template references T` | 可见的设备/产品/规则引用 | 不可见引用仅由删除冲突返回不可识别摘要；不泄漏隐藏ID |
| `template devices T` | 明确同`listDevices`传templateId，不是另一业务API | 可结合--has-template、其他设备filter；只显示授权设备 |
| `template copy T --name NAME` | 从T当前配置创建一份新模板 | 不复制secret/runtime身份/成员；不指定历史版本 |
| `template delete T --if-match-file template.etag --yes` | 删除/归档语义按API | 被引用409；不级联设备或模型 |
| `template export T --out package.json` | 当前TemplateConfiguration及TemplatePackage中的可选ThingModel固定快照 | 不含tenant/secret/运行态；ThingModel快照的版本身份仅用于依赖导入映射 |
| `template import --file package.json [--wait]` | TemplatePackage请求数据，schema版本明确 | 服务端建立本地模型/snapshot及包内映射；CLI不拼物模型创建/发布流程 |
| `template topic list T` | 投影当前configuration.topicMappings | 输出规则原字段；明确投影模式，不伪称独立分页API |
| `template script list T` | 投影当前configuration.processingScripts | 保留六阶段、enabled、language、entrypoint及脚本自己的engineVersion |
| `template field list T` | 投影当前configuration.customFieldDefinitions | 明确这是设备资料字段，不是ThingModel字段 |
| `template topic apply T` / `template script apply T` / `template field apply T` | 对当前configuration的清晰命令别名，提交完整配置及原ETag | 不是单条rule API，不只传片段或清空其他数组；业务校验交给服务；Topic/脚本局部ID是模板内稳定key |
| `template script test --file test.json` | testProcessingScript；script/input/topic/可选templateId上下文 | 同正式connector-runtime沙箱，无真实下发；错误稳定，CLI不嵌入Lua引擎 |
| `template enrollment-credential list T` | 引导凭据摘要分页 | 独立template:credentials:read |
| `template enrollment-credential create T --expected-version N --secret-output secret.json` | 为当前模板配置生成引导凭据 | expected-version是模板resourceVersion的CAS，不是模板业务版本；一次受控输出，不写模板导出包 |
| `template enrollment-credential revoke T C --yes` | 正式撤销 | 独立write权限、审计；新注册被拒绝，既有设备凭据不被混为同一用途 |
| `asset upload --file cover.png --purpose template-image` / `asset get A` | multipart文件存储owner；返回assetId/checksum/授权URL | 模板显式update imageAssetId；不隐式上传后改模板；不支持任意URL抓取 |
| `provider list` / `product list` / `product get P` | 分页正式目录 | 产品默认模板只是建议，创建设备仍需显式选择；不由CLI生成产品或猜provider |

`template update`的安全更新步骤是显式的：先用`template get T --output json --save-etag template.etag`获取并检查当前值；人工或外部构建程序准备符合`TemplateConfiguration`的完整configuration；最后提交一次带原ETag的PATCH。CLI不计算新的业务事实、不代替校验报告；任何CAS冲突都停止。Topic priority及成对dataKind/dataIdentifier、同phase至多一个enabled脚本、资料字段type/defaultValue及已有实例值兼容性，全部由正式API检查。模板当前配置改变不创建模板历史版本；设备运行配置沿用configurationGeneration报告期望和应用状态。

### 3.4 设备分组

| 命令 | 正式字段 / 范围 | 验证边界 |
|---|---|---|
| `group list` | --parent-id/--root-only/--search/--sort及分页 | parentId与rootOnly冲突按合同拒绝；不拿旧0当新UUID |
| `group tree [--root-id G] [--max-depth 32]` | GroupTree.nodes/truncated/hasMoreChildren | 最大32层；被裁剪明确显示；不能默默认为树已完整 |
| `group get G` | 详情、parent/path、ETag | 失败/不可见不返回别的租户path |
| `group create --file group.json` | name/description/parentId/sortOrder；根parentId=null | 同父名唯一、权限由服务验证 |
| `group update G --file patch.json --if-match-file group.etag` | name/description/sortOrder | 不接受parentId；不能绕move |
| `group move G --parent-id P --expected-version N` / `--root` | MoveDeviceGroup，根parentId=null | --parent-id和--root二选一；CAS/环/深度/同名由服务处理 |
| `group delete G --if-match-file group.etag --yes` | 删除空组或返回引用冲突 | 不级联设备、子组、规则；重复与不存在按正式合同 |
| `group members list G [--include-descendants]` | GroupMemberPage；device/directMembership/matchedGroupIds | 多组设备去重由服务；来源标记不泄漏不可见组 |
| `group members add G --file members.json` | deviceIds + expectedGroupVersion，1–100 | 所有成员授权原子检查，批内一项失败全部拒绝 |
| `group members remove G --file members.json --yes` | 同SetGroupMembers | 重复移除保持不存在，不误删device；CLI不拆批 |

### 3.5 运行时、控制、历史、诊断与观察流

| 命令 | API能力 / 参数 | 权限与结果边界 |
|---|---|---|
| `device state D` / `device states --file ids.json` | getRuntimeState / batch query 1–100 IDs；包含runtime报告的applied配置与应用状态 | device:read；unknown/observedAt/lastSeenAt保留；不按请求失败改成offline；与metadata的desired配置分别标注来源 |
| `device connection D` | 正式ConnectionInfo | device:diagnose；无secret；服务/网关/直连字段按实际能力 |
| `device status-history D --from T --to T [--connectivity online]` | 状态事件分页，UTC；`--connectivity`映射connectivity，可取online/offline/unknown | device:read；由服务在分页和total前应用筛选；无记录不等于offline |
| `device diagnose D [--from T --to T] [--recent-failure-limit 20]` | from/to必须一起给；省略两者时默认最近24h，最大31d；recentFailureLimit为1–100、默认20 | device:diagnose；输出windowFrom/windowTo、三方向统计、阶段检查与recentFailures；不把缺数据补0或宣称诊断成功 |
| `device simulator-config D --out simulator.json` | 正式SimulatorConfig含endpoint/topic/samplePayload/fixed model | device:diagnose；不含secret；只读样例合同；发送另用正式session/run命令，不把下载配置声称已发报文 |
| `device debug start D --duration-seconds 300 --level debug` | createDeviceDebugSession，10–900秒 | device:diagnose；返回会话ID/expiresAt；不永久开启 |
| `device debug stop D SESSION_ID --yes` | stopDeviceDebugSession | 同权限；只停目标会话，不依赖本地缓存 |
| `device debug logs D --from T --to T --direction uplink` | 分页，uplink/downlink/internal | 服务端脱敏；CLI不保存完整认证报文 |
| `device readings D --kind telemetry --identifier temp --from T --to T [--aggregation avg --window 30d]` | 正式readings；kind为telemetry/attribute/event；aggregation和window逐字段映射，原始读取及latestOnly按唯一schema | device:read；历史仍由telemetry owner；非raw必须给identifier和window；不在CLI自行聚合、补点或改单位 |
| `device readings delete-latest D --kind telemetry --identifier temp --expected-cache-version V --reason R --yes` | 正式DeleteLatestReading | **device:data:delete**；`--expected-cache-version`映射expectedCacheVersion，只删Reading.cacheVersion精确匹配的latest缓存，历史不删；同observedAt的新采样也有新version并导致409 |
| `device command submit D --file command.json [--wait --until acknowledged]` | identifier/snapshotId/parameters/timeoutSeconds及label；label为nullable string、最长100字符；无--file的参数模式用`--label TEXT`映射label | **device:control**；--file模式在JSON中提供label，不叠加同body便利参数；使用当前已应用固定模型COMMAND；不允许任意telemetry/attribute写 |
| `device command submit D ... --when-online --expires-at T` | 显式deliveryMode=when_online，最大24h | 与body重复赋值冲突退出2；仅一请求，CLI不本地排队；过期不投递 |
| `device command get D E` / `device command list D --state pending [--label-contains TEXT]` | 正式receipt/分页；state/identifier/from/to；labelContains最长100字符；响应保留label、parameters及redactedParameterPaths | 同control权限；pending/accepted/delivered/acknowledged分别显示；服务已脱敏参数不可由CLI补全，过滤在分页前执行 |
| `device command cancel D E --reason R --yes` | CancelCommand，只能取消pending | **device:control**，已投递409；不删除回执掩盖操作；幂等取消成功exit0 |
| `device watch --device-id D [--device-id D2] --kind state --kind reading --output jsonl` | streamDeviceObservations，deviceIds最多100、kinds最多3 | 正式SSE，只读授权；不请求旧WS，不落本地第二运行事实库 |

watch用标准SSE解析，发送API声明的query序列化和可选`Last-Event-ID`（CLI为`--last-event-id`），逐行原样输出ObservationEvent，包括heartbeat和resync_required。断网按明确预算用最后事件ID重连，去重只按eventId，不按设备值猜测。重连仍用当前受信身份并接受服务重新授权；401/403退出3/4。游标过期收到resync_required时将事件输出后退出9，要求操作者显式取得当前state/readings基线再开始新订阅，不能无提示跳过缺口。SIGINT关闭流退出130；没有写入、取消或删除副作用。

历史查询与导出共用正式聚合语义：`aggregation=raw/min/max/avg/sum/diff/last`；`window`为null或`30s/1m/2m/5m/10m/30m/1h/3h/6h/1d/7d/30d`。raw使用null窗口，CLI省略`--window`；非raw必须明确窗口，读数查询还必须有单个identifier。历史查询必须同时提供from/to；`--latest-only`只能使用raw，且不得带from/to/window。时间范围为`[from,to)`，最大366天，raw单次最多24h；固定长度桶按UTC epoch对齐，diff明确定义为桶内max-min，last按observedAt与sampleId稳定取末条，空桶不填0。数值聚合仅用于数值字段，事件只能raw；不同snapshotId的字段不跨版本混算。分页在聚合之后，CLI保留aggregation/windowStart/windowEnd/sampleCount和正式total，所有计算与空桶行为由telemetry owner实现。

旧UI的`1mo`在Community后端实际映射为固定30天（`internal/dal/telemetry_datas.go:414-426`），新命令和UI统一称`30d`；它不是日历月。CLI拒绝`--window 1mo`或未声明的日历窗口，不通过本地换算创造另一套时间语义。查询与导出都不再使用历史导出的intervalSeconds；模拟器profile自己的intervalSeconds仍是独立发送间隔字段。

诊断响应按服务提供的windowFrom/windowTo解释统计：stats.uplink、stats.downlink、stats.storage分别保留success、total、successRate、complete。successRate的机器值为0..1；total为0时successRate为null；complete=false时success/total/successRate均为null，不冒充完整成功统计。recentFailures逐条保留id、timestamp、direction、stage、code、message、requestId，响应同时保留recentFailuresTruncated；CLI不从checks自行推算计数、比率或补造失败记录。模拟器保存、初始化、发送及关闭分别使用下面的正式runtime操作；历史文件导出使用telemetry owner作业，CLI不私加旧URL、直接调用数据库或自行MQTT发报文。


#### 历史导出与受控模拟器（正式操作）

| 命令 | request / 权限 | 行为与等待 |
|---|---|---|
| `device readings export D --file history-export.json [--wait]` | ReadingExportRequest；**device:data:export** | kind、identifiers、from/to、format csv/jsonl、aggregation raw/min/max/avg/sum/diff/last、window；raw的window=null，非raw须明确window；最大366天、raw单次24h、最多100万行；与查询共用UTC固定桶及空桶规则，不在CLI本地聚合 |
| `device readings export-get O` | getReadingExport，同导出权限 | 查询telemetry owner的作业；不能用metadata operation查询这个O |
| `device readings export-download O --out history.csv` | downloadReadingExport，同导出权限 | 重新授权、过期410、checksum、完整下载；当前未succeeded不能伪造空文件成功 |
| `device simulator profile get D --save-etag simulator.etag` | getDeviceSimulatorProfile；**device:simulate** | runtime保存kind/payload/topicPurpose/intervalSeconds/messageLimit，独立resourceVersion |
| `device simulator profile update D --file profile.json --if-match-file simulator.etag` | UpdateSimulatorProfile；同simulate权限+幂等 | 一次完整PUT；payload按固定模型验证；不接受用户任意server/port/socket，target由正式runtime解析 |
| `device simulator session create D --profile-version N [--ttl-seconds 300]` | SimulationSessionRequest；同simulate权限+幂等 | TTL 60–3600秒；初始化受控连接，返回ready/closed/expired与session ID；CLI不自行连broker |
| `device simulator session close D SESSION_ID --yes` | closeDeviceSimulationSession；同simulate权限+幂等 | 显式关闭，不等于删除设备或配置；不能关闭他人的session |
| `device simulator run D --session-id S [--message-count 1] [--wait]` | SimulationRunRequest；同simulate权限+幂等 | 1–1000条且受profile.messageLimit限制；202仅受理；实际发送在runtime；不自动创建session或修订profile |
| `device simulator run-get D RUN_ID` | getDeviceSimulationRun；同simulate权限 | pending/running/succeeded/failed/cancelled、sent、failed、error全部保留；--wait轮询此操作，不轮询metadata作业 |
| `device enroll-status O --status-token-file status-token.txt [--wait]` | getDeviceEnrollmentStatus；独立**enrollment:status**能力 | 仅用enroll结果的短期statusToken，绑定job/device/tenant，10分钟；不能用模板引导凭据或普通管理token冒充；不能查询metadata-operations或reveal秘密 |

历史导出`--wait`轮询`getReadingExport`；模拟运行`--wait`轮询`getDeviceSimulationRun`；注册状态`--wait`轮询`getDeviceEnrollmentStatus`。这三种owner各自保存事实，CLI只按operation映射选择读取端点。注册响应完整保存到secret-output文件，包括statusToken；statusToken过期退出3，操作者可显式以原注册幂等key合法重放领取新短期token，CLI不会自动发起新登记。注册status到configurationState=applied且activationState=active才达到就绪；failed退出6，超时7，缺token2。`device access bind/unbind`只是device template bind/unbind的命名别名，使用同一SetTemplateBinding和If-Match；不提供另一接入配置owner。

新增Topic/脚本的稳定局部key可由显式本地工具 `tpctl util uuid` 生成；它只生成随机UUID，不发HTTP、不判断模板授权、不生成资源ID。外部JSON构建工具亦可生成符合ConfigurationItemId的值。deviceParameters是逐设备配置，只可在CreateDevice或显式设备template binding中提交；公共access.parameters与实例serviceInstanceId分域，不通过普通device update旁路改变运行配置。

### 3.6 既有物模型的复用命令

`thing-model list/get/versions list/version get/resources get/create/update/publish/resources update`对应第7节已存在的9个正式operation。创建、修改、发布和资源更新复用既有ThingModel app事务和校验；CLI只生成transport。正式新wire使用OpenAPI lowerCamel `modelId/snapshotId`，历史Go/候选前端的`modelID/snapshotID`不能作为额外别名保留。`expectedDraftRevision`、`expectedResourcesRevision`、`expectedRevision=0`的语义按现有核心合同，不统一误改为模板resourceVersion。

`thing-model list`只提供现有合同的`--query`和分页；不加入未实现的lifecycle/kind/sort筛选。模板配置引用ThingModel时读取真实versions/snapshot；不能把前端本地PUBLISHED过滤或最大100条列表误当完整服务契约。

## 4. 可复现命令流程（全部是未来验收脚本示例）

以下假定fixture准备好的JSON文件已按OpenAPI生成。变量只代表测试环境返回的ID；本轮不执行这些写操作。

```bash
# 1. 登记与激活是两个明确用例
tpctl device create --file create-device.json --idempotency-key qa-create-0001 --output json
tpctl device credential create "$TP_DEVICE_ID" --file credential-policy.json --secret-output credential-result.json --idempotency-key qa-credential-0001
tpctl device activate "$TP_DEVICE_ID" --expected-version 2 --idempotency-key qa-activate-0001 --wait --output json

# 2. 读取精确绑定ETag后直接提交模板ID或解绑
tpctl device template get "$TP_DEVICE_ID" --save-etag binding.etag --output json
tpctl device template bind "$TP_DEVICE_ID" --file approved-binding.json --if-match-file binding.etag --idempotency-key qa-bind-0001 --wait --output json

# 3. 模板当前配置通过完整schema直接保存，不在CLI内偷偷合并局部字段
tpctl template get "$TP_TEMPLATE_ID" --save-etag template.etag --output json
tpctl template update "$TP_TEMPLATE_ID" --file edited-template.json --if-match-file template.etag --idempotency-key qa-template-save-0001 --output json
tpctl template validate "$TP_TEMPLATE_ID" --file edited-configuration.json --output json

# 4. 导入必须先校验，后提交匹配摘要，最后核对items
tpctl device import validate --file devices.csv --format csv --idempotency-key qa-import-0001 --wait --output json
tpctl device import commit "$TP_IMPORT_ID" --source-digest "$TP_SOURCE_DIGEST" --validation-digest "$TP_VALIDATION_DIGEST" --idempotency-key qa-commit-0001 --wait --output json
tpctl operation items "$TP_OPERATION_ID" --all --output jsonl

# 5. 多组、未分组及服务接入使用同一个设备查询合同
tpctl device list --group-scope ungrouped --access-way service --service-instance-id "$TP_SERVICE_INSTANCE_ID" --page-size 100 --all --output jsonl
tpctl group members add "$TP_GROUP_ID" --file group-members.json --idempotency-key qa-members-0001 --output json
tpctl device gateway batch --file gateway-batch.json --idempotency-key qa-gateway-0001 --wait --output json

# 6. 实时流与正式命令分开；响应里保留真实回执阶段
tpctl device watch --device-id "$TP_DEVICE_ID" --kind state --kind reading --output jsonl
tpctl device command submit "$TP_DEVICE_ID" --file deferred-command.json --idempotency-key qa-command-0001 --output json
tpctl device command list "$TP_DEVICE_ID" --state pending --label-contains '验收' --output json

# 7. 状态、诊断及历史查询使用正式字段，30d是固定30天
tpctl device status-history "$TP_DEVICE_ID" --connectivity offline --from '2026-10-01T00:00:00Z' --to '2026-10-02T00:00:00Z' --all --output jsonl
tpctl device diagnose "$TP_DEVICE_ID" --from '2026-10-01T00:00:00Z' --to '2026-10-02T00:00:00Z' --recent-failure-limit 20 --output json
tpctl device readings "$TP_DEVICE_ID" --kind telemetry --identifier temperature --from '2026-09-01T00:00:00Z' --to '2026-10-01T00:00:00Z' --aggregation diff --window 30d --output json
```

示例中的version必须由fixture真实响应获得，不能在生产脚本硬编码2/4；这里固定数字仅作为测试夹具前置条件。业务否决不能通过修改本地fixture或自动取新version绕过。

## 5. CLI 原子任务、依赖和完成判据

| 任务 | 实现范围 / 用户价值 | 前置与发布阻断 | 验收证据 |
|---|---|---|---|
| DM-CLI-001 | 认证、授权context与正式合同版本检查 | DM-API-002/003；P1 / 发布阻断 | 不信任tenant输入、错误版本9、401/403矩阵、token脱敏 |
| DM-CLI-002 | 生成类型/认证/Problem/分页/ETag/幂等/文件输出基础 | OpenAPI冻结和DM-API-003/005/009；P1 / 发布阻断 | 生成无diff、真实HTTP请求断言、跨tenant、secret捕获检查 |
| DM-CLI-003 | device list/get/stats及完整filter | DM-DEV-001/002/020/026；P1 / 发布阻断 | TC-CLI-003/004/027，125条和受限账号总数 |
| DM-CLI-004 | device create/update/delete | DM-DEV-003/004/005；P1 / 发布阻断 | TC-CLI-005/006/007，DB与API读回 |
| DM-CLI-005 | 设备凭据list/create/reveal/rotate/revoke | DM-DEV-009/010/011，runtime；P1 / 发布阻断 | TC-CLI-008/009/010，实际凭据轮换与日志脱敏 |
| DM-CLI-006 | 激活/启停/命令及等待 | DM-DEV-006/007、DM-RT-006；P1 / 发布阻断 | TC-CLI-011/012/031，受理与应用证据分开 |
| DM-CLI-007 | template CRUD/复制/启停/引用保护 | DM-TPL-001/002/003/004/008/009/011；P1 / 发布阻断 | TC-CLI-013/014，使用当前配置与引用冲突 |
| DM-CLI-008 | validate/references | DM-TPL-007/010；P1 / 发布阻断 | TC-CLI-015，保存当前configuration后按generation确认应用 |
| DM-CLI-009 | 单设备模板bind/unbind和网关关系 | DM-DEV-016/017/018/019；P1 / 发布阻断 | TC-CLI-016/017，If-Match/CAS/跨tenant |
| DM-CLI-010 | group CRUD/tree/list | DM-GRP-001–006；P1 / 发布阻断 | TC-CLI-018/019，根/32层/非法父 |
| DM-CLI-011 | move、members批量增删和反向归组 | DM-GRP-005/007–010；P1 / 发布阻断 | TC-CLI-020/021，原子授权/多组去重 |
| DM-CLI-012 | 批量创建/导入校验提交/设备及模板导出导入 | DM-DEV-012–015、DM-TPL-012/013；P1 / 发布阻断 | TC-CLI-022–026，dry-run零设备写入/检查点/数量摘要 |
| DM-CLI-013 | 作业查询、逐项结果与恢复等待 | DM-API-008、DM-CLI-002；P1 / 发布阻断 | operation/get/items/wait、超时不重提、部分失败6、来源权限重检 |
| DM-CLI-014 | 保留Go唯一实现与shell启动器，形成发行包、文档和样例 | DM-CLI-003/004/007/010/012；P1 / 发布阻断 | 二进制与规范版本、帮助/样例、Go构建、shell一致出口、旧路由不随新包发布 |
| DM-CLI-015 | CLI契约与端到端验收，包含旧入口退役证明 | DM-CLI-005/006/008/009/011/013/014；P1 / 发布阻断 | 同候选所有命令、stdout/stderr/exit code、API/DB/runtime回读；受禁路径/DB直连/旧projection均0 |
| DM-CLI-016 | Topic/scripts/fields当前configuration、脚本沙箱、引导凭据、资产 | DM-TPL-017–021；P1 / 发布阻断 | TC-CLI-028–030/035/036；完整配置读回与秘密隔离 |
| DM-CLI-017 | renumber/directAccess/高级筛选/批量模板及多级网关 | DM-DEV-021/023–027；P1 / 发布阻断 | TC-CLI-027/037/038/039，逐项report与runtime确认 |
| DM-CLI-018 | 延期命令/状态历史/diagnose/simulator-config/latest-delete/watch | DM-RT-014–019；P1 / 发布阻断 | TC-CLI-031–034/040/045–047；SIGINT130、授权、历史导出及真实模拟发送 |

具体任务状态以TASKS为准：已有Go代码/局部测试源可复用，**本次CLI验收未完成**。全部受本次范围影响的命令验收后才能共同直切，不先发布一个菜单作为长期并存试点。

## 6. 从单元到真实端到端的验收矩阵

通用fixture：租户A/B；A管理员、A只读、A只可见D1的受限主体、A具备control但无凭据权限主体；125个设备、至少2份独立模板当前配置、32层分组、多组设备、无模板服务设备、网关G1→G2→D1；可控模拟runtime；所有测试记录同一候选CLI/API/DB迁移SHA。不得使用示例数据替代真实回读。

| 测试 ID / 任务 | 命令与前置 | 预期HTTP/输出/退出码及副作用 |
|---|---|---|
| TC-CLI-001 / 001-002 | 缺命令、未知flag、`--page-size 101`、未知JSON字段、重复键、缺key、`--if-match '*'` | 发出请求0次；stderr定位参数；stdout空；exit2 |
| TC-CLI-002 / 002 | 同get命令分别缺token、过期token、B的ID、已知不存在ID | 401→3；403/404→4；B和不存在输出无可枚举差异；无token/secret日志 |
| TC-CLI-003 / 003 | `device list --page-size 100 --all --output jsonl`，fixture稳定125台 | 两页正式GET、125个唯一typed对象；换json保留2页envelope；exit0 |
| TC-CLI-004 / 003 | list/stats/export相同授权及联合filter | 选择集合/total/导出行数一致；受限主体计数不泄漏；runtime503→8而非0条或offline |
| TC-CLI-005 / 004 | 同key同create JSON发送两次；模拟首响应丢失 | 一个device、一条有效业务创建；返回同ID；两次成功0；同key改body→409/5 |
| TC-CLI-006 / 004 | get保存ETag；另一主体更新；用旧ETag update | 412→5；设备保持他人版本；客户端不GET再写、不重试新版本 |
| TC-CLI-007 / 004 | 删除被规则/子设备引用的设备；再测允许删除 | 冲突409/5且无删；合法202只受理0，--wait成功后runtime确认，设备/凭据按合同失效 |
| TC-CLI-008 / 005 | credential create未给secret-output；再给安全新文件 | 前者请求0/exit2；后者201、0600完整响应、stdout空、stderr仅ID；现存或symlink目标拒绝 |
| TC-CLI-009 / 005 | readonly调用reveal；合法reveal reason+reauth；不可恢复摘要 | 403/4；合法200安全文件；摘要409/5；stdout/stderr/trace均无意外secret |
| TC-CLI-010 / 005 | 创建pending替代凭据，显式rotate，同key重试，runtime暂不可用 | replacement只消费一次、overlap不延长；timeout7保留operation；恢复后同job成功，旧凭据按窗口撤销 |
| TC-CLI-011 / 006 | `device create`后分别get元数据和读state；再显式activate/disable | create不自动发送activate；get只给元数据和desired，不携带第二份applied或隐式查runtime；state保留runtime应用状态；inactive清晰；正式activate满足前提后active；管理disabled不假写offline |
| TC-CLI-012 / 006 | number-check测试available=false；不提供企业版activation-check | 仅按社区版正式编号检查响应；不新增扫码资格领取 |
| TC-CLI-013 / 007 | template create/update/copy/disable | model=null可保存；copy使用当前配置且不复制secret；disable只走正式update、CAS冲突5 |
| TC-CLI-014 / 007 | template有可见/不可见引用时delete/references | 可见引用可分页；不可见引用无ID；delete409/5，不自动解绑全部设备 |
| TC-CLI-015 / 008 | 创建并直接保存模板configuration，validate后读回当前值 | 省略字段不变、整体configuration替换；validate不写数据、不产生token；模型版本仍固定引用 |
| TC-CLI-016 / 009 | template bind/unbind；检查旧ETag与目标租户 | 412→5或404→4；失败不覆盖设备；合法作业按configurationGeneration确认应用 |
| TC-CLI-017 / 009 | 绑定非gateway/跨tenant/重复地址/自身/祖先；合法多级 | 错误422/409/404按2/5/4；无残留关系；合法G1→G2→D1后两端列表一致 |
| TC-CLI-018 / 010 | root group create；`group list --root-only`；32层tree | root parentId=null；truncated/hasMoreChildren保留；maxDepth不足不能输出完整树假象 |
| TC-CLI-019 / 010 | group update带parentId、删除非空组、同父重名 | schema或服务拒绝2；引用/重名409→5；设备与子组不被删 |
| TC-CLI-020 / 011 | 并发A移B、B移A；stale expected version | 一方冲突且无环；exit5；无客户端自动改父或重试覆盖 |
| TC-CLI-021 / 011 | members add含1个不可见device；重复add；多组子树list；remove | 整批失败无部分关系；重复无重复行；返回GroupMember direct/来源；只删指定关系 |
| TC-CLI-022 / 012 | batch-create 1/100/101项；100项含一个重复编号 | 1/100按合同；101请求0/exit2；错误批次没有元数据半成功；不隐式分批 |
| TC-CLI-023 / 012 | import validate文件含3错误行 | 领域设备新增0；报告含row/pointer/code、digest；invalidRows=3；--wait完成exit2 |
| TC-CLI-024 / 012 | 修复文件validate；commit错误摘要、旧权限；合法commit后断网 | 错误摘要409/5或422/2；失权4；合法只同一个job，恢复查询checkpoint不重新造设备 |
| TC-CLI-025 / 012 | export filters选125条与IDs选3条；下载过期/撤权 | 分别125/3行；不含secret；410→5、403→4；无遗留部分文件；checksum一致 |
| TC-CLI-026 / 012 | template包跨tenant受权导出/导入、重复同key安装 | 新本地ID映射可审计，模型字段/资源语义等价；来源摘要先验证，目标语义checksum按新身份重算，不能要求等于来源；重复不多建；旧包不走运行兼容 |
| TC-CLI-027 / 017 | 无模板directAccess服务设备create；provider/instance/ungrouped/hasTemplate/firmware联合filter | 使用同create/list API；不自动建模板/产品；错误schema/secret参数422/2；集合与Web一致 |
| TC-CLI-028 / 016 | full-configuration apply含Topic up/down、dataIdentifier、priority；旧ETag | 校验和读回当前configuration；已绑定设备是否随模板保存自动更新仍需runtime owner冻结；非法model引用/越tenant topic服务拒绝；旧ETag412/5 |
| TC-CLI-029 / 016 | 六阶段Lua脚本各一次；双enabled同phase；死循环test | 阶段完整；双enabled校验2；沙箱超时有稳定结果且无真实投递；CLI未执行Lua |
| TC-CLI-030 / 016 | customFieldDefinitions三类型defaults，设备customFields含0/false/null及非法type | 合法读回保留0/false；类型不符422/2；停用保留值；删除影响冲突5 |
| TC-CLI-031 / 006/018 | 命令正确/错snapshot/未声明identifier；离线when_online；到期；分别提交label省略/null、100/101字符；重新get/list并按labelContains过滤，fixture包含敏感参数 | 错误422/409→2/5；合法202只accepted/pending；上线只投递一次；到期不投递；--wait失败exit6；省略label按null，null及100字符可读回，101字符exit2；get/list保留同一label、服务脱敏parameters和redactedParameterPaths，遮蔽位置为null且JSON Pointer对应正确；过滤后total/分页一致；101字符labelContains拒绝；stdout/stderr不泄漏已脱敏值，不把脱敏读回当无损重放body |
| TC-CLI-032 / 018 | cancel pending；重复cancel；cancel delivered | pending取消0、幂等；delivered409/5，回执仍在；cross-tenant404/4 |
| TC-CLI-033 / 018 | delete-latest携带cacheVersion V1，同observedAt的新采样V2恰好已到；再用V2删除 | V1冲突409/5不删V2；V2合法只删最新缓存，历史同窗口点数不变；无data:delete权限4 |
| TC-CLI-034 / 018 | watch 2租户ID混入、>100、正常断网恢复、撤权、SIGINT | 越权4/超限2；同eventId去重不改payload；撤权停止；SIGINT130且无写入；resync事件明确9 |
| TC-CLI-035 / 016 | enrollment credential create/list/revoke，禁autoRegister后enroll | summary无secret；create安全文件；撤销/关闭后拒绝注册；body tenantId本地/服务拒绝，不改tenant |
| TC-CLI-036 / 016 | asset upload合法/伪MIME/超限/他tenant asset；显式template image引用 | 文件owner校验；错误2/4；不自动改模板；checksum和asset读取一致 |
| TC-CLI-037 / 017 | device renumber重号/旧version/合法后runtime失败恢复 | 409/5不占号；合法保持deviceID和历史身份；不能半占新号；operation失败6可审计 |
| TC-CLI-038 / 017 | batch模板绑定100项，其中1项runtime应用失败 | 每项结果可读，99成功1失败；整体failed/exit6；重试不重复已完成副作用 |
| TC-CLI-039 / 017 | batch网关存在同批环或重复地址；合法多级后中断 | 整批预检拒绝；合法逐项runtime回执；--wait超时7不重提；可从items恢复 |
| TC-CLI-040 / 018 | 同device分别查询online/offline/unknown状态历史并翻页；diagnose测试默认24h、显式31d、单独from或to、超31d、recentFailureLimit=1/100/0/101；fixture含total=0、未知、不完整统计及真实失败；simulator-config和runtime503 | connectivity过滤在total/分页前生效，空历史不是offline；诊断windowFrom/To与请求或默认24h一致；非法范围/单边时间/limit越界exit2；三方向success/total/successRate/complete原样输出，total=0时successRate=null，complete=false时success/total/successRate均null，JSON比率保持0..1；recentFailures按限制保留id/timestamp/direction/stage/code/message/requestId，响应保留recentFailuresTruncated且可定位fixture失败；配置固定模型且无secret；503→8，无demo兜底；下载配置不声称已发模拟消息 |
| TC-CLI-041 / 001/014/015 | 同候选运行所有命令；记录HTTP/DB连接与旧metadata读权限禁用 | 所有请求来自第7节正式operation；受禁Gin/代理/旧WS/旧表连接0；不能用mock通过替代真实E2E |
| TC-CLI-042 / 002/014 | `thing-model resources update` expectedRevision=0；model publish绑定固定版本 | 正式lowerCamel输入、既有核心语义；资源与模型revision分别校验；不改核心算法/另造snapshot |
| TC-CLI-043 / 002 | 模拟合法HTTP200返回旧code/data envelope、未知必需状态或错误分页 | exit9，stdout不输出伪正常业务数据；不读取raw/旧接口补救 |
| TC-CLI-044 / 015 | 所有--output模式、空列表、HTTP错误、作业部分失败、secret命令抓stdout/stderr | JSON不混提示，jsonl逐行可解析，错误退出码一致；受权secret仅到指定出口；无失误写console |
| TC-CLI-045 / 018 | readings与export逐一测试raw/min/max/avg/sum/diff/last及全部window；同桶数值1/4/7、有空桶、边界from/to、跨月固定30d、跨模型版本；raw的24h/超24h、聚合366d/超366d、100万行/超限；非raw缺window或查询缺identifier；latest-only带历史参数、单边时间、非数值聚合/事件聚合、未声明1mo、历史intervalSeconds；授权下载 | raw的window=null；同桶min=1/max=7/avg=4/sum=12/diff=6，last按observedAt+sampleId取末条；固定UTC epoch桶与[from,to)准确，空桶不填0，30d不按日历月变长；各snapshot分别聚合且返回windowStart/End/sampleCount，分页total为聚合结果数；查询和导出同过滤同桶值一致；缺必需参数、非法组合或越界exit2，未知1mo/intervalSeconds不静默转换；合法文件数量/值与telemetry一致；等待只查reading-exports；撤权4/过期5；无metadata历史副本 |
| TC-CLI-046 / 018 | profile PUT旧ETag、session初始化、run3条、重放同key、关闭/到期后run | 旧ETag412/5；真实目标收到恰3条且两设备隔离；同key不重发；session失效冲突5；恶意server/port字段2；run异步失败6 |
| TC-CLI-047 / 017/018 | enroll返回inactive+statusToken；查同job、别job、token过期、token用于管理GET | 同job受限状态可读；别job404/4；过期401/3；不能访问管理或秘密；就绪只在applied+active后0；重复登记无新增设备 |

单元测试覆盖命令到operation/headers/query/body、schema输入、输出、退出码、SSE解析；HTTP合同测试用生成契约server fixture验证请求；真实集成与端到端必须验证正式Encore handler、真实持久化和runtime应用。测试使用Go与shell现有体系；本轮仅规划，未运行上述业务命令。

## 7. 全部103个社区版用户操作的 operationId → CLI 映射

以下表由规划OpenAPI操作登记逐项核对；包含三个菜单所需的正式运行时和物模型调用。本节操作映射逐项对应当前OpenAPI；命令别名只能路由到同一operation，不增加业务用例。其request schema和权限是服务端权威，CLI不能弱化。


共同前缀为 `tpctl`；HTTP路径保留完整值便于静态/网络验收。`I`表示必须显式Idempotency-Key；`C`表示必须对应资源强If-Match；POST body内的expected版本仍按request schema另行提供。纯查询POST没有I。

| operationId / 责任owner | 正式HTTP | CLI命令 | request schema / Header | 权限 / 实现任务 |
|---|---|---|---|---|
| `listDevices` / devicemetadata | `GET /api/v2/devices` | `device list [filters]` | `无body` / — | `device:read` / DM-DEV-001 |
| `getDevice` / devicemetadata | `GET /api/v2/devices/{deviceId}` | `device get D` | `无body` / — | `device:read` / DM-DEV-002 |
| `createDevice` / devicemetadata | `POST /api/v2/devices` | `device create --file create.json` | `CreateDevice` / I | `device:write` / DM-DEV-003 |
| `updateDevice` / devicemetadata | `PATCH /api/v2/devices/{deviceId}` | `device update D --file patch.json` | `UpdateDevice` / I C | `device:write` / DM-DEV-004 |
| `deleteDevice` / devicemetadata | `DELETE /api/v2/devices/{deviceId}` | `device delete D --yes` | `无body` / I C | `device:delete` / DM-DEV-005 |
| `getDeviceAdministrativeState` / devicemetadata | `GET /api/v2/devices/{deviceId}/administrative-state` | `device administrative-state get D` | `无body` / — | `device:read` / DM-DEV-006 |
| `setDeviceAdministrativeState` / devicemetadata | `PUT /api/v2/devices/{deviceId}/administrative-state` | `device enable D / device disable D` | `SetAdministrativeState` / I C | `device:write` / DM-DEV-006 |
| `activateDevice` / devicemetadata | `POST /api/v2/devices/{deviceId}/activations` | `device activate D --expected-version N` | `ActivateDevice` / I | `device:activate` / DM-DEV-007 |
| `checkDeviceNumber` / devicemetadata | `POST /api/v2/device-number-validations` | `device number-check --number N` | `DeviceNumberCheck` / — | `device:read` / DM-DEV-008 |
| `listDeviceCredentials` / devicemetadata | `GET /api/v2/devices/{deviceId}/credentials` | `device credential list D` | `无body` / — | `device:credentials:read` / DM-DEV-009 |
| `createDeviceCredential` / devicemetadata | `POST /api/v2/devices/{deviceId}/credentials` | `device credential create D --file input.json --secret-output result.json` | `CreateCredential` / I | `device:credentials:write` / DM-DEV-009 |
| `rotateDeviceCredential` / devicemetadata | `POST /api/v2/devices/{deviceId}/credentials/{credentialId}/rotations` | `device credential rotate D C --replacement-id NEW --expected-version N` | `RotateCredential` / I | `device:credentials:write` / DM-DEV-010 |
| `revokeDeviceCredential` / devicemetadata | `POST /api/v2/devices/{deviceId}/credentials/{credentialId}/revocations` | `device credential revoke D C --reason R --expected-version N --yes` | `RevokeCredential` / I | `device:credentials:write` / DM-DEV-010 |
| `revealDeviceCredential` / devicemetadata | `POST /api/v2/devices/{deviceId}/credentials/{credentialId}/reveals` | `device credential reveal D C --file reveal.json --secret-output result.json` | `CredentialRevealRequest` / I | `device:credentials:reveal` / DM-DEV-011 |
| `batchCreateDevices` / devicemetadata | `POST /api/v2/device-batches` | `device batch-create --file batch.json` | `BatchDeviceCreate` / I | `device:write` / DM-DEV-012 |
| `validateDeviceImport` / devicemetadata | `POST /api/v2/device-imports` | `device import validate --file data.csv --format csv` | `ImportUpload` / I | `device:import` / DM-DEV-013 |
| `getDeviceImport` / devicemetadata | `GET /api/v2/device-imports/{importId}` | `device import get IMPORT_ID` | `无body` / — | `device:import` / DM-DEV-013 |
| `commitDeviceImport` / devicemetadata | `POST /api/v2/device-imports/{importId}/commits` | `device import commit IMPORT_ID --source-digest HEX --validation-digest HEX` | `ImportCommit` / I | `device:import` / DM-DEV-014 |
| `downloadDeviceImportFormat` / devicemetadata | `GET /api/v2/device-import-format` | `device import format --format csv --out format.csv` | `无body` / — | `device:import` / DM-DEV-013 |
| `exportDevices` / devicemetadata | `POST /api/v2/device-exports` | `device export --file export.json` | `DeviceExport` / I | `device:export` / DM-DEV-015 |
| `getDeviceTemplateBinding` / devicemetadata | `GET /api/v2/devices/{deviceId}/template-binding` | `device template get D` | `无body` / — | `device:read` / DM-DEV-016 |
| `setDeviceTemplateBinding` / devicemetadata | `PUT /api/v2/devices/{deviceId}/template-binding` | `device template bind D --file binding.json / device template unbind D / device access bind D --file binding.json / device access unbind D` | `SetTemplateBinding` / I C | `device:write` / DM-DEV-017 |
| `getGatewayMembership` / devicemetadata | `GET /api/v2/devices/{deviceId}/gateway-membership` | `device gateway get D` | `无body` / — | `device:read` / DM-DEV-018 |
| `setGatewayMembership` / devicemetadata | `PUT /api/v2/devices/{deviceId}/gateway-membership` | `device gateway bind D --gateway-id G --address A / device gateway unbind D` | `SetGatewayMembership` / I C | `device:write` / DM-DEV-018 |
| `listSubdevices` / devicemetadata | `GET /api/v2/devices/{deviceId}/subdevices` | `device subdevices list D` | `无body` / — | `device:read` / DM-DEV-019 |
| `getDeviceStatistics` / devicemetadata | `GET /api/v2/device-statistics` | `device stats [filters]` | `无body` / — | `device:read` / DM-DEV-020 |
| `listDeviceAccessTemplates` / devicemetadata | `GET /api/v2/device-access-templates` | `template list [filters]` | `无body` / — | `template:read` / DM-TPL-001 |
| `getDeviceAccessTemplate` / devicemetadata | `GET /api/v2/device-access-templates/{templateId}` | `template get T / template topic list T / template script list T / template field list T` | `无body` / — | `template:read` / DM-TPL-002 |
| `createDeviceAccessTemplate` / devicemetadata | `POST /api/v2/device-access-templates` | `template create --file template.json` | `CreateDeviceAccessTemplate` / I | `template:write` / DM-TPL-003 |
| `updateDeviceAccessTemplate` / devicemetadata | `PATCH /api/v2/device-access-templates/{templateId}` | `template update T --file patch.json / template enable T / template disable T` | `UpdateDeviceAccessTemplate` / I C | `template:write` / DM-TPL-004 |
| `validateDeviceAccessTemplate` / devicemetadata | `POST /api/v2/device-access-templates/{templateId}/validations` | `template validate T --file configuration.json` | `TemplateConfiguration` / — | `template:write` / DM-TPL-007 |
| `deleteDeviceAccessTemplate` / devicemetadata | `DELETE /api/v2/device-access-templates/{templateId}` | `template delete T --yes` | `无body` / I C | `template:delete` / DM-TPL-009 |
| `listDeviceAccessTemplateReferences` / devicemetadata | `GET /api/v2/device-access-templates/{templateId}/references` | `template references T` | `无body` / — | `template:read` / DM-TPL-010 |
| `copyDeviceAccessTemplate` / devicemetadata | `POST /api/v2/device-access-templates/{templateId}/copies` | `template copy T --name NAME` | `CloneTemplate` / I | `template:write` / DM-TPL-011 |
| `exportDeviceAccessTemplate` / devicemetadata | `GET /api/v2/device-access-templates/{templateId}/export` | `template export T --out package.json` | `无body` / — | `template:export` / DM-TPL-012 |
| `importDeviceAccessTemplate` / devicemetadata | `POST /api/v2/device-access-template-imports` | `template import --file package.json` | `TemplatePackage` / I | `template:import` / DM-TPL-013 |
| `listProducts` / devicemetadata | `GET /api/v2/products` | `product list [--search S] [--template-id T]` | `无body` / — | `product:read` / DM-TPL-014 |
| `getProduct` / devicemetadata | `GET /api/v2/products/{productId}` | `product get P` | `无body` / — | `product:read` / DM-TPL-014 |
| `listServiceDiscoveredDevices` / connector-runtime | `GET /api/v2/connection-providers/{providerId}/instances/{instanceId}/discovered-devices` | `provider devices list --provider-id P --service-instance-id I [--search TEXT] [--page N --page-size N --snapshot-id S / --all]` | `无body` / — | `device:discover` / DM-RT-001 |
| `listConnectionProviders` / connector-runtime | `GET /api/v2/connection-providers` | `provider list` | `无body` / — | `template:read` / DM-TPL-015 |
| `listDeviceGroups` / devicemetadata | `GET /api/v2/device-groups` | `group list [--parent-id G / --root-only]` | `无body` / — | `group:read` / DM-GRP-001 |
| `getDeviceGroup` / devicemetadata | `GET /api/v2/device-groups/{groupId}` | `group get G` | `无body` / — | `group:read` / DM-GRP-001 |
| `getDeviceGroupTree` / devicemetadata | `GET /api/v2/device-group-tree` | `group tree [--root-id G] [--max-depth N]` | `无body` / — | `group:read` / DM-GRP-002 |
| `createDeviceGroup` / devicemetadata | `POST /api/v2/device-groups` | `group create --file group.json` | `CreateDeviceGroup` / I | `group:write` / DM-GRP-003 |
| `updateDeviceGroup` / devicemetadata | `PATCH /api/v2/device-groups/{groupId}` | `group update G --file patch.json` | `UpdateDeviceGroup` / I C | `group:write` / DM-GRP-004 |
| `moveDeviceGroup` / devicemetadata | `POST /api/v2/device-groups/{groupId}/moves` | `group move G --parent-id P --expected-version N / group move G --root --expected-version N` | `MoveDeviceGroup` / I | `group:write` / DM-GRP-005 |
| `deleteDeviceGroup` / devicemetadata | `DELETE /api/v2/device-groups/{groupId}` | `group delete G --yes` | `无body` / I C | `group:delete` / DM-GRP-006 |
| `listDeviceGroupMembers` / devicemetadata | `GET /api/v2/device-groups/{groupId}/members` | `group members list G [--include-descendants]` | `无body` / — | `group:read` / DM-GRP-007 |
| `addDeviceGroupMembers` / devicemetadata | `POST /api/v2/device-groups/{groupId}/member-additions` | `group members add G --file members.json` | `SetGroupMembers` / I | `group:write` / DM-GRP-008 |
| `removeDeviceGroupMembers` / devicemetadata | `POST /api/v2/device-groups/{groupId}/member-removals` | `group members remove G --file members.json --yes` | `SetGroupMembers` / I | `group:write` / DM-GRP-009 |
| `listGroupsForDevice` / devicemetadata | `GET /api/v2/devices/{deviceId}/groups` | `device groups list D` | `无body` / — | `device:read` / DM-GRP-010 |
| `getMetadataOperation` / devicemetadata | `GET /api/v2/metadata-operations/{operationId}` | `operation get O / operation wait O` | `无body` / — | `originating-operation:read` / DM-API-008 |
| `listMetadataOperationItems` / devicemetadata | `GET /api/v2/metadata-operations/{operationId}/items` | `operation items O [--all]` | `无body` / — | `originating-operation:read` / DM-API-008 |
| `downloadMetadataArtifact` / devicemetadata | `GET /api/v2/metadata-downloads/{downloadId}` | `artifact download DOWNLOAD_ID --out FILE` | `无body` / — | `originating-operation:read` / DM-API-008 |
| `getDeviceRuntimeState` / device-runtime | `GET /api/v2/devices/{deviceId}/runtime-state` | `device state D` | `无body` / — | `device:read` / DM-RT-002 |
| `getDeviceConnectionInfo` / device-runtime | `GET /api/v2/devices/{deviceId}/connection-info` | `device connection D` | `无body` / — | `device:diagnose` / DM-RT-003 |
| `createDeviceDebugSession` / device-runtime | `POST /api/v2/devices/{deviceId}/debug-sessions` | `device debug start D --duration-seconds N --level debug` | `DebugSessionRequest` / I | `device:diagnose` / DM-RT-004 |
| `stopDeviceDebugSession` / device-runtime | `DELETE /api/v2/devices/{deviceId}/debug-sessions/{sessionId}` | `device debug stop D SESSION_ID --yes` | `无body` / I | `device:diagnose` / DM-RT-004 |
| `listDeviceDebugLogs` / device-runtime | `GET /api/v2/devices/{deviceId}/debug-logs` | `device debug logs D [--from T --to T --direction uplink]` | `无body` / — | `device:diagnose` / DM-RT-004 |
| `listDeviceReadings` / telemetry | `GET /api/v2/devices/{deviceId}/readings` | `device readings D --kind telemetry --identifier FIELD --from T --to T --aggregation avg --window 30d`；或`... --latest-only`原始当前值模式 | `无body` / — | `device:read` / DM-RT-005 |
| `submitDeviceCommand` / control | `POST /api/v2/devices/{deviceId}/command-executions` | `device command submit D --file command.json`（label在文件内；无--file参数模式用`--label TEXT`） | `CommandSubmission` / I | `device:control` / DM-RT-006 |
| `getDeviceCommandExecution` / control | `GET /api/v2/devices/{deviceId}/command-executions/{executionId}` | `device command get D E / device command submit D --wait` | `无body` / — | `device:control` / DM-RT-006 |
| `queryDeviceRuntimeStates` / device-runtime | `POST /api/v2/device-runtime-state-queries` | `device states --file ids.json` | `RuntimeStateQuery` / — | `device:read` / DM-RT-002 |
| `changeDeviceNumber` / devicemetadata | `POST /api/v2/devices/{deviceId}/number-changes` | `device renumber D --device-number NEW --reason R --expected-version N` | `RenumberDevice` / I | `device:write` / DM-DEV-021 |
| `batchSetDeviceTemplateBindings` / devicemetadata | `POST /api/v2/device-template-binding-batches` | `device template batch-bind --file bindings.json` | `BatchTemplateBinding` / I | `device:write` / DM-DEV-023 |
| `testProcessingScript` / connector-runtime | `POST /api/v2/processing-script-tests` | `template script test --file test.json` | `ProcessingScriptTest` / I | `template:diagnose` / DM-TPL-018 |
| `listTemplateEnrollmentCredentials` / devicemetadata | `GET /api/v2/device-access-templates/{templateId}/enrollment-credentials` | `template enrollment-credential list T` | `无body` / — | `template:credentials:read` / DM-TPL-020 |
| `createTemplateEnrollmentCredential` / devicemetadata | `POST /api/v2/device-access-templates/{templateId}/enrollment-credentials` | `template enrollment-credential create T --expected-version N --secret-output secret.json` | `CreateTemplateEnrollmentCredential` / I | `template:credentials:write` / DM-TPL-020 |
| `revokeTemplateEnrollmentCredential` / devicemetadata | `DELETE /api/v2/device-access-templates/{templateId}/enrollment-credentials/{credentialId}` | `template enrollment-credential revoke T C --yes` | `无body` / I | `template:credentials:write` / DM-TPL-020 |
| `enrollDevice` / device-runtime | `POST /api/v2/device-enrollments` | `device enroll --file input.json --enrollment-credential-file bootstrap --secret-output result.json` | `DeviceEnrollment` / I | `enrollment:register` / DM-RT-013 |
| `batchSetGatewayMemberships` / devicemetadata | `POST /api/v2/gateway-membership-batches` | `device gateway batch --file gateway-batch.json` | `GatewayMembershipBatch` / I | `device:write` / DM-DEV-027 |
| `listDeviceCommandExecutions` / control | `GET /api/v2/devices/{deviceId}/command-executions` | `device command list D [--state pending --identifier FIELD --from T --to T --label-contains TEXT]` | `无body` / — | `device:control` / DM-RT-014 |
| `cancelDeviceCommandExecution` / control | `POST /api/v2/devices/{deviceId}/command-executions/{executionId}/cancellations` | `device command cancel D E --reason R --yes` | `CancelCommand` / I | `device:control` / DM-RT-014 |
| `listDeviceStatusHistory` / device-runtime | `GET /api/v2/devices/{deviceId}/status-history` | `device status-history D [--from T --to T --connectivity offline]` | `无body` / — | `device:read` / DM-RT-015 |
| `getDeviceDiagnostics` / device-runtime | `GET /api/v2/devices/{deviceId}/diagnostics` | `device diagnose D [--from T --to T --recent-failure-limit 20]` | `无body` / — | `device:diagnose` / DM-RT-015 |
| `getDeviceSimulatorConfig` / device-runtime | `GET /api/v2/devices/{deviceId}/simulator-config` | `device simulator-config D --out simulator.json` | `无body` / — | `device:diagnose` / DM-RT-015 |
| `deleteDeviceLatestReading` / telemetry | `POST /api/v2/devices/{deviceId}/latest-reading-deletions` | `device readings delete-latest D --kind telemetry --identifier FIELD --expected-cache-version V --reason R --yes` | `DeleteLatestReading` / I | `device:data:delete` / DM-RT-016 |
| `streamDeviceObservations` / device-runtime | `GET /api/v2/device-observation-stream` | `device watch --device-id D --kind state --output jsonl` | `无body` / — | `device:read` / DM-RT-017 |
| `createFileAsset` / file-storage | `POST /api/v2/file-assets` | `asset upload --file image.png --purpose template-image` | `FileAssetUpload` / I | `template:write` / DM-TPL-021 |
| `getFileAsset` / file-storage | `GET /api/v2/file-assets/{assetId}` | `asset get A` | `无body` / — | `template:read` / DM-TPL-021 |
| `exportDeviceReadings` / telemetry | `POST /api/v2/devices/{deviceId}/reading-exports` | `device readings export D --file export.json` | `ReadingExportRequest` / I | `device:data:export` / DM-RT-018 |
| `getReadingExport` / telemetry | `GET /api/v2/reading-exports/{operationId}` | `device readings export-get O / device readings export D --wait` | `无body` / — | `device:data:export` / DM-RT-018 |
| `downloadReadingExport` / telemetry | `GET /api/v2/reading-exports/{operationId}/download` | `device readings export-download O --out history.csv` | `无body` / — | `device:data:export` / DM-RT-018 |
| `getDeviceSimulatorProfile` / device-runtime | `GET /api/v2/devices/{deviceId}/simulator-profile` | `device simulator profile get D` | `无body` / — | `device:simulate` / DM-RT-019 |
| `updateDeviceSimulatorProfile` / device-runtime | `PUT /api/v2/devices/{deviceId}/simulator-profile` | `device simulator profile update D --file profile.json` | `UpdateSimulatorProfile` / I C | `device:simulate` / DM-RT-019 |
| `createDeviceSimulationSession` / device-runtime | `POST /api/v2/devices/{deviceId}/simulator-sessions` | `device simulator session create D --profile-version N` | `SimulationSessionRequest` / I | `device:simulate` / DM-RT-019 |
| `closeDeviceSimulationSession` / device-runtime | `DELETE /api/v2/devices/{deviceId}/simulator-sessions/{sessionId}` | `device simulator session close D SESSION_ID --yes` | `无body` / I | `device:simulate` / DM-RT-019 |
| `runDeviceSimulation` / device-runtime | `POST /api/v2/devices/{deviceId}/simulation-runs` | `device simulator run D --session-id S --message-count N` | `SimulationRunRequest` / I | `device:simulate` / DM-RT-019 |
| `getDeviceSimulationRun` / device-runtime | `GET /api/v2/devices/{deviceId}/simulation-runs/{runId}` | `device simulator run-get D RUN_ID / device simulator run D --wait` | `无body` / — | `device:simulate` / DM-RT-019 |
| `getDeviceEnrollmentStatus` / device-runtime | `GET /api/v2/device-enrollment-operations/{operationId}` | `device enroll-status O --status-token-file token.txt [--wait]` | `无body` / — | `enrollment:status` / DM-RT-013 |
| `createThingModel` / thingmodel | `POST /api/v2/thing-model-commands/create` | `thing-model create --file model.json` | `NativeThingModelCreate` / I | `thingmodel.write` / DM-API-007 |
| `updateThingModel` / thingmodel | `POST /api/v2/thing-model-commands/update` | `thing-model update --file update.json` | `NativeThingModelUpdate` / I | `thingmodel.write` / DM-API-007 |
| `publishThingModel` / thingmodel | `POST /api/v2/thing-model-commands/publish` | `thing-model publish --file publication.json` | `NativeThingModelPublish` / I | `thingmodel.publish` / DM-API-007 |
| `listThingModels` / thingmodel | `GET /api/v2/thing-models` | `thing-model list [--query QUERY]` | `无body` / — | `thingmodel.read` / DM-API-007 |
| `getThingModel` / thingmodel | `GET /api/v2/thing-models/{modelId}` | `thing-model get MODEL_ID` | `无body` / — | `thingmodel.read` / DM-API-007 |
| `listThingModelVersions` / thingmodel | `GET /api/v2/thing-models/{modelId}/versions` | `thing-model versions list MODEL_ID` | `无body` / — | `thingmodel.read` / DM-API-007 |
| `getThingModelVersion` / thingmodel | `GET /api/v2/thing-models/{modelId}/versions/{publishedRevision}` | `thing-model version get MODEL_ID PUBLISHED_REVISION` | `无body` / — | `thingmodel.read` / DM-API-007 |
| `getThingModelResources` / thingmodel | `GET /api/v2/thing-models/{modelId}/resources` | `thing-model resources get MODEL_ID` | `无body` / — | `thingmodel.read` / DM-API-007 |
| `updateThingModelResources` / thingmodel | `POST /api/v2/thing-model-commands/update-resources` | `thing-model resources update --file resources.json` | `NativeThingModelUpdateResources` / I | `thingmodel.write` / DM-API-007 |

## 8. 生成一致性、发布门禁和未来 MCP

本版逐项登记 **103 个社区版用户可调用的正式operationId**，不把操作登记数量当成实现完成率。CI必须比较OpenAPI中排除internal-service后的operation集合与本表/命令注册集合；新增或删除operation而未更新映射与用例时失败。状态从“有代码”到“合同测试通过”“真实CLI/API/runtime验收通过”“发行包发布”分别记录，不以单元stub替代运行证据。

类型生成和合同测试必须覆盖每个命令实际请求的method/path/query/header/body，所有命令字段来自同一schema。新业务字段先改OpenAPI，再生成Go/TypeScript并更新命令，不在CLI用自由map绕过规范。输出和退出码的黄金文件要与真实响应结合，特别验证null、0、false、ID、UTC、未知枚举、Problem、202和分页。

未来MCP直接复用相同operationId、认证/委派身份、tenant/resource授权、幂等和CAS。MCP不通过CLI解析table，不复制CLI的字段推断，不读取服务数据库，不使用另一个接入模板/物模型业务实现。显示工具参数只是对同一schema的映射；权限撤销、preview过期和幂等重放在同一正式owner执行。

发布前必须同时通过三菜单浏览器、CLI、数据库迁移对账和实际runtime闭环；原生API或数据库不可用时CLI明确失败。没有旧API回退、双写、双读、兼容facade、旧runtime-projection或匿名诊断通道。

## 9. 服务内部协议的明确排除

OpenAPI计划2共有108项：本文件103项用户操作，以及5项 `x-audience=internal-service` 的引用账本操作（reserve/get/confirm/renew/releaseMetadataReference）。内部操作由正式服务客户端调用，**不生成tpctl命令，也不进入未来MCP工具目录**。CLI覆盖率分母为103；服务合同覆盖率单独为5/5。生成器错误暴露内部操作、普通用户token可以调用、或把tenant/sourceOwner做成CLI参数，均阻断DM-CLI-002/015、DM-API-010/011和发布验收。正式API/认证仍只有一套，不能为此新增旁路。

## 10. 服务发现及分组参数回归

`tpctl provider devices list --provider-id P --service-instance-id I --page-size 7 --search TEXT --output json`调用listServiceDiscoveredDevices。instanceId是opaque字符串，只作路径编码，不解释为tenant。`--all`持有首次snapshotId，固定过滤/pageSize；10分钟过期以410/exit5终止，不自动重新开始混接两份结果。无权实例404/exit4、缺action scope403/exit4、provider不可用503/exit8。table展示externalId/name/importState，JSON保留snapshotId/observedAt/expiresAt和分页；不得输出不可见registeredDeviceId。该场景纳入TC-DM-CLI-017-01，普通候选查看无副作用，不要求幂等key。

当前分组CLI发送pageSize/name/parentId，当前Encore接收page_size/query/parent_id，源码已证明存在字段不一致；目标仍以OpenAPI唯一lowerCamel命名为准。DM-CLI-010-03录制真实pageSize=7、search和parentId请求，并断言每项筛选实际生效；不得靠增加旧字段别名掩盖错误。

内部resolveRuntimeCredential是未注册公共HTTP的凭据owner用例，ProductMigrationRecord是离线升级输入；二者仅复用同一OpenAPI schema，不属于103项CLI operation。CLI/MCP生成器也必须排除x-runtime-private-contracts与x-offline-migration-contracts，不能把schema存在当作可调命令。

### 当前配置最小职责补充（2026-10-27）

DeviceAccessTemplate只保存/返回imageAssetId，不含imageUrl/downloadUrl；封面展示通过FileAsset GET获取带授权的短期地址，不把临时capability写入metadata或普通幂等收据。configuration.access的providerVersion/schemaDigest允许省略；显式提供仍校验格式，不将新provider发布目录作为内建MQTT普通创建/保存的前置条件。ThingModel的publishedRevision与固定snapshot引用继续保留。

### 已实现的普通模板保存命令（2026-10-27）

`tpctl template create --file template.json --idempotency-key create-unique-key`

`tpctl template get TEMPLATE_ID --save-etag template.etag`

`tpctl template update TEMPLATE_ID --file changes.json --if-match-file template.etag --idempotency-key update-unique-key --save-etag template.etag`

body使用当前configuration，图片只用imageAssetId；MQTT不自动补providerVersion/schemaDigest。创建成功后直接可用，无模板publish命令。批量绑定接口按同步200元数据结果设计，不要求轮询新作业；批量CLI尚待实现。
