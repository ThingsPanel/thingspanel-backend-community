# 2026-10-25 规划文档验证与工作区保护记录

本文记录本轮实际执行的**规划契约、交叉引用和文件完整性检查**。它不替代产品测试、页面验收、数据库迁移或发布记录。当前源码事实与Git起始基线见[REVIEW-2026-10-25.md](REVIEW-2026-10-25.md)；2026-10-23旧检查保留在[VALIDATION.md](VALIDATION.md)，不能把其旧数量或结果当成本轮状态。

## 1. 最终规划规模与证据口径

|项目|本轮规模|含义|
|---|---:|---|
|稳定任务|188|保留原187个ID，新增DM-API-011；每项包含目的、范围、依赖、场景、验收与证据|
|优先级|P0 56 / P1 132|均为本版完整范围；P1不表示可漏交|
|状态|已有代码1 / 部分实现66 / 待做116 / 待核实5|唯一“已有代码”为候选物模型核心；没有据此宣布原生任务验收完成|
|正式HTTP操作|109|104个用户操作，5个受限服务引用操作|
|HTTP路径 / 组件schema|93 / 255|同一openapi.yaml，版本2.0.0-plan.2|
|CLI操作映射|104/104|5个内部服务操作明确排除，私有凭据与离线迁移输入也不是CLI命令|
|逐任务测试规格|580|全部planned_not_executed；测试规格数量不是已通过测试数|
|旧路由账本|165|修正40处旧任务占位并补外部设备候选发现，每项稳定任务追溯|
|前端库存|139|保留原FEI ID，当前源码与候选/历史证据分开|
|独立源码采集副本|1,133文件 / 8,205,699字节|全部size/SHA256与采集清单一致；不声称全仓每行均已审读|

非HTTP合同仍使用同一OpenAPI组件：配置事件/回执、同owner的private凭据材料解析、受控离线ProductMigrationRecord。它们各有owner、输入/输出、任务和测试追溯，不是新增公开API、兼容层或平行业务服务。

## 2. 已执行的官方规范验证

已从官方源重新核查[OpenAPI 3.2.1](https://spec.openapis.org/oas/v3.2.1.html)，采用其3.2系列官方schema-base，配合JSON Schema 2020-12校验。实际执行器为远端已有Python `jsonschema 4.26.0`，并使用`referencing`注册官方schema、dialect与meta资源；未安装或修改项目依赖。

|检查|实际结果|证据|
|---|---|---|
|OAS文档与Schema Object结构|0个验证错误|[contract-validation-2026-10-25.json](contract-validation-2026-10-25.json)|
|26个代表性正反schema样例|26/26符合预期；13个合法通过，13个非法被拒|[contract-examples.json](contract-examples.json)与验证结果|
|跨租户注入、未知属性、缺必填、重复成员、版本下界、引用类型、错误status、敏感候选字段、应用回执|对应负例按目标schema拒绝|样例ID SC-DEV/GRP/REF/ERR/DISC/RECEIPT|
|Go/TypeScript客户端生成并编译|未执行|仍为DM-API-001/009开工及冻结门禁|
|实际Encore handler响应合同|未执行|仍为DM-QA-001/003验收工作|

被验证OpenAPI的SHA256：

```text
2f2c7635f53a811f76897fb29d6815f7c230fe8de117d09e5ce9fb2bd96eecf0
```

官方schema来源及规范JSON内容SHA256保存在验证结果中。离线使用已取回的官方schema避免网络波动；没有用自造schema替代官方标准。schema通过不证明跨字段业务规则、幂等事务、加密、并发或运行时应用已经实现。

## 3. 规划交叉引用检查

使用本目录只读工具[validate-plan.py](validate-plan.py)检查以下内容，结果归档为[planning-validation-2026-10-25.json](planning-validation-2026-10-25.json)：

|检查|要求|
|---|---|
|任务完整性|188个唯一ID、11列任务表、全部必填字段、580个场景与JSON一致|
|依赖|前置ID存在，无环；DM-QA-010闭包包含全部188任务|
|OpenAPI|109个operationId唯一，路径参数齐全，所有本地引用可解析；owner/permission/task/request/response与索引一致|
|CLI|104个公共操作映射完整，路径/权限/任务一致；5个internal-service操作不进入CLI|
|非HTTP合同|运行事件、离线产品输入和private凭据schema可解析，并有任务/测试追溯|
|旧路由|165个方法/路径组合唯一，每项唯一处置、稳定主任务/附加任务存在，无占位任务|
|前端库存|139个唯一FEI、任务引用存在、旧库存ID保留|
|链接及采样稳定性|相对文档链接可达；检查期间读取文件未发生变化|
|原有规划保护|原任务、operation、测试、路由、FEI ID集合完整保留|

复核命令（仅文档检查，无网络、数据库或业务代码执行；需要PyYAML）：

```bash
python3 docs/initiatives/device-metadata-native-cutover/validate-plan.py --expected-operations 109 --expected-cases 580
```

若另有原规划只读副本，可用`--baseline`指定并重复旧ID保留检查；没有原副本时工具会明确标记该项未核对，而不是宣称完成。本轮实际运行有原19文件副本，已进行该比较。

## 4. 工作区保护与写回

写回目标仅为Community的`docs/initiatives/device-metadata-native-cutover/`。原目录19文件已先完整读取、保存原始hash并逐项修订，原任务ID不重编号。每个旧文件写回前必须仍匹配其采集hash，新文件必须不存在，发现并发改动则停止该文件写入。

写回前已再次验证：三仓HEAD、branch、缓存upstream、ahead/behind与起始一致；排除指定规划目录后的**2,535个原有文件**的集合、大小、SHA256/符号链接和Git状态摘要完全一致。对应B 1,242，K 339，F 954；原目录内19文件另逐文件保护。F第一次ls-remote超时后重试成功，三仓实时远端refs均与登记缓存一致；没有fetch/pull。

**写回后检查已通过。** 本轮仅更新原目录18份文件、新增6份规划/验证文件，原VALIDATION.md保持原字节，共25份交付文件。全部本地/远端SHA256逐项一致，目录无额外临时文件。排除本目录后，三仓2,535个原有文件的集合、size/SHA256和Git状态摘要仍与起始完全一致；三仓HEAD/branch/upstream/ahead-behind也未变化。Community变更登记由591到597，增量恰为本目录6个新增文件；Encore仍26，前端仍37。验证记录收口更新后再次对照相同保护集合和最终交付hash，不重新执行业务测试。

未执行reset、clean、stash、checkout、commit、merge、全量暂存；没有把混合工作区作为本任务交付。业务源码、配置、迁移SQL和运行数据不在允许写回集合中。

## 5. 明确没有执行的事项

没有启动Community或Encore业务服务，没有执行Go/前端/CLI业务测试，没有浏览器HAR或菜单实际点击验收，没有读取运行数据库、迁移业务数据、调用实际设备、部署或发布。因此：

- “代码存在”和“测试源码存在”仅是证据等级，不能换算完成率。
- 三菜单当前源码仍使用旧请求链，不能认定已切换。
- 候选物模型与当前checkout分列；旧QA报告不能继承到本次制品。
- 数据迁移和版本发布状态未核实，发布门禁继续阻断。
- 580条测试是后续可实施的规格，26个schema样例和规划静态检查是本轮实际通过的文档验证。
