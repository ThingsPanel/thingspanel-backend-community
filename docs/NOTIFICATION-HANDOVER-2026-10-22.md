# ThingsPanel 通知系统交接入口（2026-10-22）

> 本文是接手通知主线合并、发布和 API 联调的入口。先看本文的状态和边界，再按“必读入口”进入代码。本文记录的是本地仓库/远端分支的核对快照，不代表已经合并、部署或通过真实微信联调。

## 1. 目标和架构约束

本期目标是把 Community 告警与统一通知核心、插件调用链整理成可评审、可发布、可联调的实现，并为微信小程序/微信公众号预留同一插件契约接入路径。

**只保留一个 Encore 通知业务核心：**复用现有 `iot-api-encore-kernel` 仓库内的 `encore-api/l3-core/notification/`。不要在 Community Gin 后端、另一个 IoT API 服务或新仓库中再建第二套通知账号、任务队列、投递状态或回执内核。

职责划分：

- **Encore 内核**：通知插件注册与授权、租户渠道实例、通知组版本、通知请求/投递/尝试/回执事实和插件调用。
- **Community 过渡适配层**：继续承担告警判定和本地事务 outbox；可靠转交稳定事件；兼容旧通知组的投影、路由 CAS 与必要的旧接口。它不成为新通知的业务事实源。
- **渠道插件**：按冻结的语言无关 HTTP v1 契约实现 provider 行为。小程序与公众号须分别核实身份、用户授权和消息模板要求，再决定是否拆成两个插件；不得在告警业务代码里直接调用微信 API。

本设计与 [通知核心 ADR](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/docs/decisions/notification-encore.md) 一致。Encore 开发应遵守该仓库的 Go-only、代码分层和既有模块放置规范；业务核心留在 L3 notification，跨旧系统兼容放在明确的 adapter/legacy 边界，插件渠道按契约扩展。

## 2. 当前仓库状态（已核对）

| 代码线 | 已推送远端的分支和 HEAD | 尚未完成的事项 |
|---|---|---|
| Community bridge | [Community 分支](https://github.com/ThingsPanel/thingspanel-backend-community/tree/codex/notification-preview-20261010)，HEAD `ba35997e`；该分支比当时 `origin/main` 多 40 个提交、少 12 个提交 | 尚未合入 Community `main`；没有找到对应开放 PR。该 checkout 还有一个未提交的 `notification_e2e02_fault_matrix_test.go` 修改，未推送。必须先与最新 Community main 做干净基线审查，不能把当前脏主工作树整体合入。 |
| Encore 通知核心 | [Encore kernel 分支](https://github.com/zjhong/iot-api-encore-kernel/tree/codex/notification-preview-20261010)，HEAD `448c674e`；远端分支与该提交一致，比 `origin/main` 多 81 个提交 | 尚未合入 kernel `main`。分支 checkout 有 3 个部署文件未提交修改（`.env.example`、部署 README、Compose），未推送。81 个提交不能整体视作“通知改动”；合并前需按提交/目录审查，避免把其它内核工作一并带入。 |
| 插件 SDK/适配器 | 已有独立 SDK、SMTP、钉钉、阿里云短信、legacy webhook 仓库 | 仓库存在不等于已由 Encore Core 调用。先完成 Core `plugin_client`/部署注册接线和生产运行验证。小程序、公众号插件当前均未找到。 |

当前 Community 主工作树与以上 feature checkout 分离，含大量 broker、northbound、物模型及其他未提交变更，不能用其目录中“看得到通知文件”作为合并证据。合并时从干净目标分支建候选工作树，逐项挑选并审查本功能提交。

## 3. 运行链与关键接口

预期链路：

```text
Community 告警事务 + source outbox
  -> Community source relay（稳定事件、幂等转交）
  -> 同一 Encore kernel 的 L3 notification core
  -> 租户通知组/渠道实例/持久化 worker
  -> 固定插件 HTTP v1 契约
  -> SMTP / 钉钉 / 短信 / webhook / 后续微信渠道
```

Community 兼容接口和切换规则见 [native notification group publishing](https://github.com/ThingsPanel/thingspanel-backend-community/blob/codex/notification-preview-20261010/docs/notification-native-publish.md)。其发布操作只证明租户路由/组修订已配置，**不证明 provider 接受，更不证明终端已收到**。操作通过 Community 的租户鉴权入口管理路由；API 联调时重点核对 `GET/POST /api/v1/notification_group/native-publish`、对应 Encore group revision、source relay 稳定事件重放，以及回退只影响未来事件。

Encore 业务契约与插件契约入口：

- [Encore notification v2 OpenAPI](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/specs/openapi/notification-v2.yaml)
- [Notification plugin v1 OpenAPI](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/specs/notification/plugin-v1.openapi.yaml)
- [Encore 通知核心源码](https://github.com/zjhong/iot-api-encore-kernel/tree/codex/notification-preview-20261010/encore-api/l3-core/notification)
- [现有 adapter 说明及未完成边界](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/notification-adapters/README.md)

发送状态必须区分：请求已持久接收、插件/provider 已受理、可信回执确认送达。超时可能是结果未知，不能自动盲重发。当前阿里云短信的真实回执 capability 仍关闭；旧 APP 多目标推送仍走 legacy 兼容路径。

## 4. 交接执行顺序

### Gate A：基线与范围

1. 分别从最新 Community `main` 和 `iot-api-encore-kernel` `main` 建干净候选 checkout。
2. 先保存并列出原工作树的所有 dirty changes；不要 stash、reset 或覆盖用户修改。
3. 对 Community `codex/notification-preview-20261010` 与 Encore 同名分支做提交级差异审查。仅纳入本通知范围；未提交的部署文件和测试修改单独审查、单独提交。
4. 产出两份清单：准备合入的 commit/file 清单，以及保留/延期的文件清单。先让自动化 CI/代码评审针对该干净候选运行。

### Gate B：统一 Encore 调用链

1. 在既有 Encore kernel 的 L3 notification 内完成/确认插件 client、插件授权注册、部署健康检查与实例路由；禁止新增平行 Core。
2. 证明 Community relay 到 Encore ingress 的端到端稳定幂等：成功、丢响应重放、跨租户拒绝、旧组路由停止/回退。
3. 检查迁移、部署顺序、密钥保管和回退说明；发布只使用选定版本镜像及固定迁移，不从另一个 checkout 手工复制未审查源码。

### Gate C：API 联调

用隔离的测试租户和已有代表渠道做一条闭环：管理员配置插件/渠道实例 → 发布通知组修订 → 触发一个测试告警 → 确认 relay/Encore task/delivery/attempt → 校验 provider 受理或失败状态 → 检查幂等重放、超时 unknown、租户隔离和回退。每个结果要标明是 mock、真实 provider 受理还是终端送达；没有真实回执就不要报 delivered。

### Gate D：微信渠道立项与联调

先分别确认小程序与公众号的官方账号权限、消息类型/模板、用户身份绑定和用户授权流程、发送限制、失败码及回执能力。确认后复用同一个 plugin v1 契约增加渠道适配器，秘密留在受控实例配置中，模板字段在插件 manifest/validation 中表达。先测试账号/有限目标联调，再评估发布；微信插件不要求改通知 Core 的业务主链。

### Gate E：发布门禁

完成 Community 与 kernel 的目标分支合并、CI、迁移/备份恢复、部署/健康检查、单租户切换和回滚演练后，才签发版本/发布。当前本地候选验收记录为 73 项中 65 通过、3 部分完成、3 阻塞、2 延期，正式发布状态为 HOLD；其中真实代表渠道和合并联调仍是阻塞项。该报告在本机 `notification-workspaces/evidence/FINAL-SUPERVISOR-REPORT.md`，不属于本仓库已发布证据。

## 5. 接手人必读入口

1. 本文：当前分支、合并边界和执行门禁。
2. [通知核心 ADR](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/docs/decisions/notification-encore.md)：统一内核的职责裁决。
3. [Community bridge API/回退契约](https://github.com/ThingsPanel/thingspanel-backend-community/blob/codex/notification-preview-20261010/docs/notification-native-publish.md)。
4. [Encore notification v2 契约](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/specs/openapi/notification-v2.yaml) 与 [plugin v1 契约](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/specs/notification/plugin-v1.openapi.yaml)。
5. [适配器状态、能力边界与本地验证说明](https://github.com/zjhong/iot-api-encore-kernel/blob/codex/notification-preview-20261010/notification-adapters/README.md)。

## 6. 本快照未声称完成的工作

- 没有合并 Community 或 Encore kernel 的 `main`；没有发布版本/镜像；没有对生产 API 或真实微信账号联调。
- 本机候选报告的 mock/本地集成证据不等于部署证明。社区网关、生产数据库迁移/恢复、真实 provider 代表渠道、用户终端送达证据仍须按上述 gates 单独验收。
- 微信小程序/公众号适配器尚不存在；账号、模板、用户授权和发送状态语义须先核验，不要把 TODO 写成已支持能力。
