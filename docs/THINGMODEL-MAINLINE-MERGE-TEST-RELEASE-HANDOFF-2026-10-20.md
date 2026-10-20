# ThingsPanel 物模型进入产品主线：合并、测试与发布移交

> 面向：负责代码合并、测试和产品发布的开发/测试同事。  
> 核对时间：2026-10-20（Asia/Shanghai）。  
> 当前状态：**66 隔离 QA 的核心原生链路及本次同租户市场安装冲突修复已验证；尚未达到主线合并/正式发布放行条件。**  
> 本文是可执行的移交入口，不代表已经合并、发布或升级线上数据。

## AI 快速摘要（结构化状态）

```yaml
product: ThingsPanel Community
module: native-encore-thing-model
release_direction: scheme-B-direct-cutover
owner_goal: merge-test-release
scope:
  include:
    - Encore 原生物模型管理接口与页面
    - 固定版本/快照和设备模板、设备配置绑定
    - Web/App/ThingsVis 对模型快照的消费
    - 市场设备模板携带原生物模型依赖的发布、下载、安装
    - 旧物模型数据升级脚本及回滚/恢复演练
  exclude:
    - Jitsi/会议系统
    - 全部 Community 后端替换
    - MQTT broker 替换
environment:
  qa_host: 100.125.207.66
  qa_ui_local_tunnel: http://127.0.0.1:18082
  qa_ui_remote_loopback_port: 18080
  qa_install_path: /opt/tp-thingmodel-dev-66-20261019/app
  qa_stack: tp-thingmodel-dev-66-20261019
  production_changed: false
latest_qa:
  backend_version: dev-market-reuse-20261020
  backend_binary_sha256: bd963f6c836e28ddb5effb5b4f72a325669015a54fa5e755ddaacaedb871b40f
  backend_image: tp-native-consumer-backend:qa-market-reuse-20261020
  backend_image_id: sha256:57c3c38b775c85a77d72eb6e276cab7fcc15f1b501dda5b176631e061616251f
  frontend_commit: ee5aab55ba604165d99a07734d92b2705e97e0c1
  encore_kernel_revision: 37fbb5e8e83c
  release_ready: false
blocking_gates:
  - 需从已提交代码重建 QA 后端并固定源 commit、构建参数和镜像 digest；现有 QA 二进制不能只凭标签认定可发布
  - 后台服务身份和规则触发的真实自动命令执行未闭环
  - 旧快捷控制/图表/关联数据异常需盘点、转换并清零阻断项
  - 完整市场发布-下载-安装-再使用往返未完成
  - 全量旧库升级、新装、切换后恢复与正式安装器未完成发行级演练
  - 前端全仓 vue-tsc 当前有 1221 条诊断，需基线归因并达到发布门槛
  - 全部活跃消费者、设备类型/网关、角色权限和旧入口退役未完成全覆盖
```

## 1. 工作目标和发布边界

本次目标是把**物模型模块**合并到产品主线，完成针对该模块的测试并发布。方案采用 B：产品日常接口和页面直接使用 Encore；存量用户通过版本化升级脚本一次性转换数据与引用。不要为物模型保留常驻 Legacy API 兼容层或双写投影。

这不是把整个 Community 后端或 MQTT 数据面搬到 Encore。设备连接、协议、broker、遥测原始数据和现有设备运行服务仍由 Community 承担；物模型定义、发布快照及依赖物模型的消费者按新契约读取 Encore 固定版本。控制统一走命令定义和命令通道，不能继续经遥测入口下发。

**发布判定：** 66 QA 上做通的路径是重要证据，但它不是正式 RC。正式合并前要固定各仓代码提交；正式发布前要让同一 RC 提交/镜像通过下文阻断清单和数据升级演练。任何未解决的数据丢失、跨租户、版本漂移、市场包内容不一致或中断误切换问题都阻断发布。

## 2. 当前环境和安全边界

| 项目 | 当前信息 |
|---|---|
| 隔离 QA 服务器 | `100.125.207.66`，SSH 使用团队已授权的个人密钥/账号 |
| QA 页面 | 本地现有转发 `http://127.0.0.1:18082`；远端容器回环端口 `127.0.0.1:18080` |
| 需要新建本机转发时 | `ssh -N -L 18082:127.0.0.1:18080 <已授权账号>@100.125.207.66` |
| 页面入口 | `http://127.0.0.1:18082/login`，市场设备模板页 `/resource-hub/device-template` |
| QA 部署目录 | `/opt/tp-thingmodel-dev-66-20261019/app` |
| QA stack | `tp-thingmodel-dev-66-20261019`，8 个服务；市场同租户修复只重启了 backend |
| 正式环境/生产 | 未改动；没有主线合并，没有正式发布，没有线上旧库升级 |
| 会议/Jitsi | 排除在本次物模型工作之外，没有改动 |
| 测试账号 | 不写在文档、PR、日志或制品里；向环境负责人通过现有安全渠道索取或使用分配给自己的测试账号 |

账号密码、邮箱验证码、JWT、市场 token、设备密钥及配置文件中含秘密的字段不可复制到移交文档、PR、截图或测试产物。发布记录只留账号角色、租户 ID、脱敏结果和证据路径。

## 3. 代码来源、版本和合并注意事项

### 3.1 当前 QA 实际运行的候选

| 部件 | 当前有证据的版本/摘要 | 说明 |
|---|---|---|
| Encore 原生 API | `37fbb5e8e83c`；容器镜像 `tp-thingmodel-native:dev-37fbb5e8e83c` | 当前 66 QA 原生服务；主原生 List 数据库密码问题已修复并通过真实 API/页面读回 |
| 前端 | `ee5aab55ba604165d99a07734d92b2705e97e0c1` | Vite production bundle 生成成功；QA 页面消费原生快照；全仓类型检查仍未通过 |
| Community backend 上一候选 | `fdb69c0c8a45e8bf1afa2e4ed24081fdb38b14d6`，版本 `dev-fdb69c0c8a45` | 曾部署到 QA；不能把它当成 10 月 20 日最新后端二进制的精确源码标识 |
| Community backend 最新 QA 修复 | 版本 `dev-market-reuse-20261020`；binary SHA256 `bd963f6c836e28ddb5effb5b4f72a325669015a54fa5e755ddaacaedb871b40f`；image `sha256:57c3c38b775c85a77d72eb6e276cab7fcc15f1b501dda5b176631e061616251f`；对应代码已提交至 `eb165d40db69c51067c98b0a03e728a84b0dc23f` | QA 二进制只部署在隔离环境。修复代码已可追溯，但 QA 镜像仍须从该提交重建并比对摘要/行为后，才可作为 RC 制品。部署/回滚信息在 66 `/opt/tp-thingmodel-dev-66-20261019/upgrade/market-source-reuse-20261020.*` |
| 升级安装器/runner | 多租户副本迁移工具和 QA 专用部署工具有实测；正式安装器编排未通过 | QA 工具不是生产升级器，不可直接用于客户库 |

源码/制品证据：

- 66 总执行报告：`/Users/junhong/Downloads/code/thingmodel-workspaces/t00-t01-20261018/task-reports/SCHEME-B-66-EXECUTION-20261019.md`，包含第 10–14 节测试、部署和未完成项（本机工作区证据文件）。
- 总体验收与 DEV 安排：`/Users/junhong/Downloads/code/thingmodel-workspaces/t00-t01-20261018/task-reports/THINGMODEL-END-TO-END-ACCEPTANCE-AND-DEV-20261018.md`（本机工作区证据文件）。其中早期部署状态可能过期，按 66 执行报告和本文件为准。
- 直接切换设计：`/Users/junhong/Downloads/code/thingspanel-backend-community/docs/ENCORE-THINGMODEL-DIRECT-CUTOVER-2026-10-18.md`。
- 首发计划：`/Users/junhong/Downloads/code/thingspanel-backend-community/docs/ENCORE-THINGMODEL-FIRST-RELEASE-PLAN-2026-10-16.md`。
- 原始 T00/T01 源候选清单 `release-manifest/candidate-sources.json` 的状态是 `CANDIDATE_SOURCE_SELECTION_NOT_RC`，它属于本机任务工作区，不能照抄当成当前完整 RC。

### 3.2 已提交且同事可拉取的代码

以下分支已推送到各自仓库的 `origin`，可以作为人工合并/审查的代码入口。它们是独立候选分支，尚未合并主线，也没有 PR 或产品发布：

| 仓库/部件 | 远端分支 | 已推送 commit | 内容与边界 |
|---|---|---|---|
| Community backend 集成候选 | `codex/thingmodel-dev-integration-20261018` | `cd5b5841b597d1c3293c1a9ba8a3de7ea11a860b` | 已提交的 Encore 原生物模型集成、市场发布/安装和相关迁移代码候选；合并前仍需逐仓核对主线差异和测试 |
| Community backend 市场修复 | `codex/thingmodel-market-source-reuse-20261020` | 修复 commit `eb165d40db69c51067c98b0a03e728a84b0dc23f`；当前分支 HEAD `03e25837be873835256cdbc8de7f08d2825465eb` | 同租户安装时仅复用完全相同的固定源快照；专项 Go 回归已通过。分支随后加入本移交文档，基于上面的集成候选 |
| Community frontend | `codex/native-device-metadata` | `ee5aab55ba604165d99a07734d92b2705e97e0c1` | 66 QA 的原生物模型/设备消费前端候选；生产构建成功，全仓类型检查尚未通过 |
| Encore kernel | `codex/thingmodel-dev-integration-20261018` | `37fbb5e8e83c47fa98a1eea59fefa38ceff97737` | 66 QA 使用的原生 API 候选，已在远端；不含本次额外升级安装器交付 |
| Test automation | `codex/thingmodel-native-qa-automation-20261020` | `80a9f644c946b8b3f76b6c870eadc3fa28bbb391` | 原生模型、模板、市场、设备绑定/消费原子动作及校验器；8 个注册表、52 个动作校验通过，不包含账号密码 |

获取分支示例（在对应仓库执行；只取分支，不会改动主线）：

```bash
git fetch origin codex/thingmodel-dev-integration-20261018
git fetch origin codex/thingmodel-market-source-reuse-20261020
git fetch origin codex/native-device-metadata
git fetch origin codex/thingmodel-native-qa-automation-20261020
```

Encore kernel 分支与 backend 集成分支名字相同，但分属不同 Git 仓库，commit SHA 也不同。请按仓库和完整 SHA 识别，不能只看分支名。

**尚未提交为正式产品代码的内容：** installer 仓库没有完整的物模型升级/回滚编排实现；其中 `upgrades/README.md` 只是目录占位说明。副本演练脚本和 66 部署脚本是 QA 工具，不是可供客户执行的升级包。因此不能把“相关代码都提交”理解成升级器已完成。需先补产品级脚本及测试，再单独提交到 installer 仓。

### 3.3 合并前仍需核对的工作区

当前后端隔离工作树 `/Users/junhong/.codex/worktrees/thingmodel-t00-t01/thingspanel-backend-community` 位于 `codex/thingmodel-template-binding-20261018`，基点 `e20707fb916c940947dfa0e9a318d8ca9ebb0a09`。仍有未提交修改和新文件；它不是可直接合并的分支。已整理并提交的核心集成候选和市场冲突修复见上表，不能把该脏工作树其余改动自动视作已提交或纳入发布。

Community 主工作区 `/Users/junhong/Downloads/code/thingspanel-backend-community` 是 `main...origin/main [ahead 7, behind 12]`，同时混有大量 MQTT/通知等与本次无关的未提交改动。**不得把主工作区整体提交、push 或合并到产品主线。**

合并负责人先做以下动作：

1. 从 `origin` 拉取每个产品仓的当前主线，另建干净、互相隔离的物模型发布分支/工作树。
2. 以本节已推送分支为候选起点，和 QA 对应源码快照及实际工作树逐文件核对；不能依赖文件夹名、版本字符串或镜像标签推断源 commit。
3. 为 backend、frontend、Encore kernel、installer 分别记录：base commit、最终 commit、变更目录、生成代码/SQL migration、测试结果和 PR URL。若某仓确实没有改动，明确写 `no change`。
4. 对 backend 的 `dev-market-reuse-20261020` 二进制做可重复重建，并核对 SHA256；要求源 commit、构建命令、依赖/工具版本和最终镜像 digest 都能追溯。无法对应时重建，不直接晋升 QA 镜像。
5. 更新一份随 release tag 固定的跨仓 manifest，并把它与 RC 镜像 digest、数据库 migration checksum 一起归档。

早期 `candidate-sources.json` 中列出的 v1.2.12.1 tag 与 kernel SHA 是 T00/T01 设计起点，不是本次部署全部源码的提交映射。提交/合并前要复核各仓 remote、主分支和候选提交当前仍存在且相互兼容。

## 4. 已完成的功能和测试证据

### 4.1 原生模型及设备消费主链路

在 66 隔离 QA 已验证的主要路径包括：

- 原生物模型列表、创建、编辑/保存、发布、版本读取和资源操作；模型管理接口直接使用 Encore `/api/v2/thing-models` 相关原生 API。
- 设备模板/配置绑定不可变物模型发布快照；改发新版本不会悄悄改变既有绑定，按明确动作升级绑定。
- 两台虚拟设备读取同一固定模型版本，但 telemetry、attribute、event 数据相互隔离；Web/App 设备页使用绑定快照显示不同设备自己的数据。
- 命令按原生 COMMAND 定义校验；受控虚拟订阅者收到实际 payload，ACK 后平台读回成功状态。不能把“HTTP 接收成功”说成设备动作成功。
- 无 token/假 token、跨租户读取、CAS 冲突、未知命令和错误参数等安全负向场景有专项通过记录。
- 设备属性/事件定义来自 Encore 固定版本；telemetry 与属性/事件显示保留；遥测页面中的旧控制入口被移除。控制保留在命令。
- 副本迁移对子集执行过转换、社区绑定 CAS、中断续跑、重复运行及整库副本恢复对账；这不代表全量生产库已升级。

可复核的 QA 版本和细项见总执行报告第 10–12 节及 `upgrade/evidence/`。其中主要结果：metadata R3 10 项、security R3 14 项、command R3-v3 7 步；前端契约/异步竞态 7/7，Vite production bundle 成功；后端相关 service/API/DAL 测试通过。

### 4.2 66 同租户市场安装冲突已修复并验证

模板源模型与发布者同租户时，旧实现重复创建同名模型，撞到 Encore `tenant_id + name` 唯一约束。最新 backend 修复仅在同租户原生模型的固定源快照 ID、完整模型内容和资源内容全部一致时复用；内容不一致不会覆盖或改名。

2026-10-20 在原失败租户重新从页面安装：`POST /api/v1/device/template/market/install` 返回 HTTP 200/业务码 200；Community 原导入记录从 `IMPORTING` 变成 `COMPLETE`。设备配置绑定 source model `1101f97a-5202-438d-ba8d-3a8d468b9326`、snapshot `d18593e5-2f4a-4a34-ad91-879904b9a0be`、revision 3。Encore 中同租户同名模型仍为 1 条，v1/v2/v3 快照仍为 3 条。详情见总执行报告第 14 节。

### 4.3 当前明确不通过/未验证的项

| 项目 | 状态 | 对发布的影响 |
|---|---|---|
| 全仓前端 `vue-tsc --noEmit --skipLibCheck` | 失败，exit 2，1221 条诊断；本次修改文件无路径匹配诊断，但未对干净基线复跑 | 需对干净基线和 RC 做归因，确认新增诊断为 0 或按正式 CI 门禁处理，不能隐藏失败 |
| Vite 前端生产 bundle | 成功 | 仅证明 bundler 成功，不等于类型检查或整版发行通过 |
| 后台服务身份下自动化规则触发原生命令 | 未完成；无有效用户会话时按安全策略拒绝 | 阻断“自动化控制闭环”声明和相关用例 |
| 旧 cardId、快捷命令、图表/控制映射和异常源数据 | 有部分盘点/局部验证，仍有未确认项 | 任何有效控制不能静默丢弃、猜测转换或继续走遥测下发；未解决项列明并阻断相关租户迁移 |
| 市场完整发布→另一账号下载→安装→设备使用往返 | **未完成** | 已验证一个市场模板的安装和同租户修复，不等于上传/审批/下载全链路、全部依赖和资源一致性通过 |
| 所有旧模板/导出包版本兼容与客户端最低版本门槛 | 未全验 | 老包不能丢模型定义或静默退化 |
| 全租户旧库转换、全量对账 | 未完成 | 禁止对 C 在线或生产库执行硬切；当前只有代表性副本子集演练 |
| 完整安装器新装/升级/失败中断/切换后恢复 | 未完成发行级验收 | QA runner 不是产品安装器；不得直接以 QA 部署工具替代 |
| 全部调用方、网关/设备类型、角色矩阵和所有旧模型入口退役 | 未全覆盖 | 合并前按消费者清单覆盖；未经验证的边界按 blocked 记录 |

## 5. 同事的手动验收步骤

每条记录测试人、时间、环境、前后端/内核 commit、镜像 digest、租户和对象 ID、步骤、预期与实际、HTTP/页面/数据库/设备证据链接。使用虚拟设备可以验证业务逻辑；不能据此声称真实硬件、所有网关或全部客户协议都已验收。

### A. 原生模型

- [ ] 新建模型，编辑基础信息和封面，保存并刷新读回。
- [ ] 分别创建/编辑/删除 telemetry、attribute、event、command；字段类型、单位、读写属性、枚举值、参数必填/范围/嵌套结构保存后保持类型和值。
- [ ] 发布 v1，再编辑草稿发布 v2；v1 固定快照和资源内容不改变。版本历史按 Encore 的真实模型 ID 返回正确版本。
- [ ] Web/App 图表编辑、保存、关闭、重开；Web 保存不能误删 App 独立配置。资源引用可读取；资源丢失应明确报错。
- [ ] 两人并发编辑时 CAS 冲突不静默覆盖；失败保留输入；重复请求不生成重复版本。
- [ ] 检查前端网络记录仅访问原生 Encore 模型契约；模型列表、编辑、发布、版本不得回落到 Legacy API。

### B. 模板、设备、数据和命令

- [ ] 从模板绑定指定已发布模型版本；编辑/发布模型新版本不会自动漂移旧绑定；显式升级、解绑、重绑读回准确。
- [ ] 用模板创建两台设备，检查配置绑定的 model ID、snapshot ID、revision；无需手工补旧模型表行。
- [ ] 通过 MQTT/受控虚拟设备发送两组不同 telemetry、attribute、event；分别在 Web/App 读取实时值与历史值，切换设备不得串值。
- [ ] 遥测、属性、事件页没有旧下发/自定义控制入口；图表控制不得使用 telemetry 下行 API。
- [ ] 从 command 定义和页面发送有效命令；检查目标设备实际收到 payload、ACK、平台结果状态。非法参数、未知命令、离线和超时需清楚失败，不显示假成功。
- [ ] 至少覆盖自动化/告警消费者和一个不同用户角色；无用户会话后台执行按单独服务身份验证，不能借用用户 token。

### C. 市场模板完整往返

- [ ] 发布端选一个绑定固定 Encore snapshot 的设备模板；发布/审核包包含模型基本信息、四类字段、参数、图表资源、模板配置和可用图片，不包含 tenant/device ID、实际凭证或密钥。
- [ ] 审批账号完成审批；使用第二个测试账号/租户下载并安装。另重复测试“发布源租户自装”路径，确认此次同名冲突修复生效。
- [ ] 比对发布前与安装后的模型快照、字段类型/枚举/命令参数、图表、图片及模板配置；依赖 ID 映射到本地可用对象，版本固定。
- [ ] 用下载后模板创建设备，复跑 B 组遥测、页面和命令检查。
- [ ] 重复安装、依赖不存在、内容/hash 不匹配、版本冲突和安装中断均有确定结果；失败不会留下 COMPLETE 假记录、模型重复项或不能用的半份配置。
- [ ] 记录审批、market upload/download 的 HTTP 请求与返回码以及市场包摘要。当前“安装 API 200”不能替代此组验收。

### D. 权限、数据升级和恢复

- [ ] 无 token、假 token、不同 tenant、只读/管理员/审批角色分别验证；跨租户 model/resource/version 操作不能泄漏存在性或允许修改。
- [ ] 对脱敏旧库副本跑全量 dry-run：统计每租户旧模型、字段四类、命令/自定义控制、图表与资源、设备模板/配置绑定、设备引用、孤儿/重复/异常数据，并给出 source ID→target ID/version 对照和无法转换清单。
- [ ] 先处理旧控制映射：可证据映射的转换至 COMMAND；不能确定的记录原 ID 和原值并阻断切换，不要猜测或静默删除。
- [ ] 固定备份社区库、Encore `device_metadata` 库和相关文件资产；验证可读、可恢复、校验和匹配。明确切换窗口和模型写冻结边界；遥测持续写入不可被整库旧备份覆盖。
- [ ] 在代表性副本执行升级、对账、重复运行、中断故障注入和恢复。核对数量、ID 映射、固定版本、模板/设备引用、图表/命令内容、tenant 隔离和活跃消费者。
- [ ] 只有 dry-run 0 阻断且对账为 0 未解释差异时才允许切换。失败必须停在旧链路，不得解冻半迁移状态。
- [ ] 切换后写入新模型再演练恢复：记录采用前向修复还是受控回退；不能只切回旧镜像或恢复整库而丢失新写入/遥测。

## 6. 合并、CI、RC 与发布顺序

### 阶段 1：整理并审查代码

1. 以各仓**当前**主线 SHA 为 base，建立干净的 `codex/` 或团队命名发布分支。
2. 将本次物模型功能拆成按仓 PR：Community backend、frontend、Encore kernel、installer/migration。无改动仓也要写明兼容性审查结论。
3. 先审 API 契约、租户/角色授权、固定快照不可变性、唯一索引/冲突策略、SQL migration 兼容/幂等和回滚边界。所有生成代码与 SQL 一并审查。
4. 检查改动范围无误：不带入无关 MQTT/通知/Jitsi/本地环境文件；不含 token、密码或测试数据。
5. 合并前各 PR 必须基于已确认的跨仓依赖顺序，记录并校验真实 commit SHA。不得对产品主线做本地 squash/merge 后无法追溯的操作。

### 阶段 2：自动化测试和构建

合并前在干净 checkout 上运行相关仓现有 CI 全套单测、静态检查、数据库迁移检查和生产构建，并保留日志。最低包含：

- Encore：core、app/service、API、DAL、权限/跨租户、CAS/幂等、迁移包测试；执行数据库 migration up/down 或受支持的恢复校验。
- Community backend：`go test ./...`（或仓库 CI 中等价全套）；市场导入/发布、设备模板绑定/安装、API authorization、迁移及失败补偿专项测试。
- 市场修复回归可单独复跑：`go test ./internal/service -run 'TestNativeMarket(ImportReusesExactSourceSnapshot|SourceSnapshotReuseRejectsChangedContent|ImportPreservesLargeEnumAndNestedChartNumbers|ClientDoesNotExposeErrorBody)' -count=1`。
- Frontend：安装锁定依赖后运行 `npm run typecheck`、仓库质量检查和 production build；如存量诊断已存在，先在相同依赖/配置的干净基线复跑，生成可审查的新增/既有诊断比较，新增诊断必须为零。
- Installer/runner：shell lint/语法、Compose config、dry-run fixture、幂等、中断续跑、恢复和 hash 对账测试。
- 固定 RC：产物构建后记录 SHA256、OCI image digest、migration checksum、SBOM/依赖清单（如 CI 支持）。测试和发布必须消费同一 digest，不允许测试后重新 build 一个“相同版本号”包发布。

已知发布阻断：10 月 19 日全量 `vue-tsc --noEmit --skipLibCheck` 为 exit 2、1221 条诊断，虽然被改文件没有路径匹配诊断，但没有干净基线证明均是旧问题。必须由前端负责人对照基线处理后再签字。

### 阶段 3：66 QA RC 人工验收

1. 先将本次修复以已提交 commit 重新构建/部署到隔离 66 QA。保存旧 image、Compose override 和切换前容器指纹；只更新目标服务。
2. 使用本文件第 5 节 A–D 清单；任一阻断项失败就记录缺陷，修复后从相关用例起重跑，不能只看 health。
3. 市场上传/审批/下载、旧库全量转换和升级器在 QA 使用脱敏数据；C 在线、正式用户库和会议容器不作为测试目标。
4. 归档测试人签名/日期、页面证据、HTTP 状态/响应摘要、数据库 count/hash 对账、设备 MQTT/命令 ACK、错误日志与最终 RC manifest。

### 阶段 4：主线合并与正式发布

1. PR 全部通过评审、CI 和 66 RC 验收，Release Owner 对下文阻断门槛逐项签字后，按依赖顺序合并；固定 release tag 与跨仓 manifest。
2. 发布新装包和旧用户升级包，升级器先备份、预检、dry-run；使用客户脱敏副本演练通过后再进入正式变更窗口。
3. 发布时执行维护窗口/模型写冻结，迁移后做全量账目核对、原生链路 smoke、代表性设备数据和命令检查；所有检查通过才宣告完成。
4. 发布后监控 API 5xx、迁移账本、绑定缺失/跨租户拒绝、版本读取失败、市场安装失败和命令错误；保留回退/前修预案与责任人。

## 7. Release Owner 放行表

以下全部勾选后，才可将物模型标记为产品主线发布完成：

- [ ] backend/frontend/kernel/installer commit 与 RC manifest 完全对应，QA 制品可重建；所有相关 PR 通过 review。
- [ ] Encore/API 与前端主路径全程原生直连；旧物模型 API、旧表 fallback、遥测控制通路均无活跃调用。
- [ ] 模型创建/编辑/发布/版本/资源、模板固定绑定、双设备隔离 telemetry/属性/事件、命令实际 ACK 全通过。
- [ ] 设备模板市场完整上传、审批、下载、安装和安装后使用通过；源租户自装同名冲突和异租户安装都通过。
- [ ] 全部调用方、重点网关/设备类型和租户角色覆盖通过；自动化服务身份及无用户会话执行边界通过。
- [ ] 前端 typecheck、后端/Encore 全仓测试、production build、数据库迁移检查通过；已知 1221 条诊断已有基线归因且无新增阻断。
- [ ] 代表性脱敏旧库副本全量迁移、差异清零、幂等重跑、中断恢复、切换后写入恢复通过；无未解释数据损失/孤儿引用。
- [ ] 安装器新装和旧版升级完整跑通；备份可恢复，回退方案与迁移写入边界经演练。
- [ ] Release Owner、QA、数据库/迁移负责人和模块负责人在 release checklist 留名、日期及证据链接。

## 8. 当前推荐决策

**先移交并启动“冻结源码 → 补齐 blocker → 建 RC → QA 验收 → PR 合并 → 产品发布”流程。**不要因为 66 的核心链路与单项市场安装通过，就直接把现有 QA 镜像或脏工作区合并到主线。

第一优先级是从本节已推送分支建立可审查 PR，并重建可追溯的 QA RC；并行补齐自动化服务身份、旧控制映射与市场完整往返。其后完成全量副本迁移/恢复、前端类型门禁和正式安装器实现与演练。全部 Release Owner 放行项通过后，再执行主线合并和产品发布。

## 9. 本文维护记录

- 2026-10-20：基于 66 QA 实际部署和数据库回读，补入市场源租户同名安装修复；明确隔离 QA 已通过项和仍阻断产品发布的项目。
- 2026-10-20：补入 backend、frontend、Encore kernel 和 Test Automation 已推送分支及完整 commit；明确正式 installer 升级/回滚实现仍未完成，QA 工具不能代替产品升级器。
- 权威的实时状态更新：在本文件“当前状态”和第 4、7 节同步，并附带变更日期、证据路径；不要用更早的设计草稿覆盖最新执行证据。
