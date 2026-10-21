# 北向服务接口合同 v1（唯一合同）

冻结日期：2026-10-21
状态：**冻结**。实现、前端、插件、测试和文档都以本文件为准。任何修改必须升版本号并同时更新本文件、Schema 和一致性测试。

本文件取代此前任何分散描述。发现实现与本文件不一致时，以实现为缺陷，而不是以实现为准修改合同。

---

## 1. 版本与所有者

| 合同 | 版本字符串 | 所有者 | 变化含义 |
|---|---|---|---|
| 源端交接 | `northbound-source/v1` | 社区运行侧（Gin）+ 北向服务（Encore） | 事件信封字段变化 |
| 插件 wire | `northbound-connector/v1` | 北向服务 + 插件作者 | 插件必须同时改版本 |
| 管理 API | 路径前缀 `/v1` | 北向服务 | 破坏性变化必须新增 `/v2` |

- 管理 API 版本与插件合同版本**独立演进**。
- 插件 `describe` 返回的 `contractVersion` 与服务期望不一致时，服务拒绝调用该插件并把它标记为 `unreachable`，不尝试兼容猜测。

---

## 2. 数据所有权

| 事实 | 唯一所有者 | 不允许 |
|---|---|---|
| 设备、物模型、租户、角色 | 社区版既有实现 | 北向另建一套 |
| 源端持久接收回执（receipt） | 社区 Broker/ledger | 北向复制一份回执 |
| 源端北向 Outbox | 社区主库 `northbound_source_outbox` | 北向读社区主库 |
| 目标连接、转发任务、投递账 | 北向服务 `northbound` 库 | 社区主库复制投递账 |
| 第三方凭证 | 北向服务加密存储 | 出现在日志/响应/事件载荷 |
| 插件进程与目标连接 | 插件 | 插件自建持久队列 |

---

## 3. 源端交接合同（Community → Encore）

### 3.1 保证起点

**只有进入 `northbound_source_outbox` 的事件才被承诺至少一次投递。** 在此之前（MQTT 内存总线、ACK 早于持久化）的丢失不在北向保证范围内，页面必须显示这一点。

`mqtt.input_profile` 为 `legacy`（默认）时不存在持久接收边界，北向页面必须显示“当前接入方式不具备持久边界”，并且不产生任何投递记录。

### 3.2 事件信封

`POST /v1/inbox/events`，请求头 `X-Northbound-Token: <服务令牌>`，`Content-Type: application/json`。

```json
{
  "contractVersion": "northbound-source/v1",
  "sourceProfile": "ingress_v1",
  "events": [
    {
      "contractVersion": "northbound-source/v1",
      "eventId": "8f14e45f-…",
      "tenantId": "tenant-a",
      "deviceId": "device-1",
      "eventType": "telemetry",
      "occurredAt": "2026-10-21T08:00:00Z",
      "receivedAt": "2026-10-21T08:00:01Z",
      "source": "thingspanel.community",
      "subject": "devices/device-1",
      "data": {"temperature": 23.6}
    }
  ]
}
```

| 字段 | 规则 |
|---|---|
| `eventId` | 源端持久边界生成，**稳定不变**。重试、重放、桥接重复都用同一个值 |
| `tenantId` | 取自已验证的租户上下文，不接受载荷自报 |
| `deviceId` | 逻辑设备 ID（网关场景是子设备） |
| `eventType` | `telemetry` \| `attribute` \| `event`，其它值拒绝 |
| `occurredAt` | 事件时间（设备观测时间） |
| `receivedAt` | 平台接收时间 |
| `data` | JSON 对象，≤ 256 KiB |

`eventId` 生成规则（源端实现）：`uuidv5(namespace, "northbound-event\0" + receiptID + "\0" + deviceID + "\0" + kind)`。

### 3.3 幂等与冲突

- 相同 `eventId` + 相同 `data` 摘要 → 计为 `duplicate`，不重复投递。
- 相同 `eventId` + 不同 `data` 摘要 → 拒绝 `event_id_conflict`（这是源端缺陷，不能静默覆盖）。
- 投递行按 `(pipeline_id, event_id, branch)` 唯一，重复批次不会生成第二条待办。

### 3.4 响应

```json
{"accepted": 1, "duplicate": 0, "fanout": 2, "rejected": [{"eventId": "…", "code": "capacity_exceeded", "detail": "…"}]}
```

| 拒绝码 | 含义 | 源端处理 |
|---|---|---|
| `capacity_exceeded` | 北向侧积压达到上限，未受理 | 保留在 Outbox，稍后重试 |
| `payload_too_large` | 单事件超过上限 | 标记失败，需人工处理 |
| `invalid_event_type` / `missing_device` / `invalid_event_id` | 数据不合法 | 标记失败 |
| `event_id_conflict` | 同 ID 不同内容 | 标记失败并告警 |
| `missing_tenant` | 缺租户 | 标记失败 |

### 3.5 订阅刷新

`GET /v1/subscriptions`（同一服务令牌）返回有活跃任务的租户：

```json
{"tenants": [{"tenantId": "tenant-a", "activePipelines": 2}]}
```

源端把它缓存在 `northbound_source_tenants`，仅用于判断“是否需要写 Outbox”。刷新失败时保留旧值：宁可多写积压，也不静默停止登记。

---

## 4. 插件 wire 合同 `northbound-connector/v1`

### 4.1 传输与鉴权

- 传输：HTTP/1.1，JSON。
- 插件监听回环地址（默认 `127.0.0.1:18081` / `18082`），不对外暴露。
- 每个请求必须带 `Authorization: Bearer <插件令牌>`；插件令牌由部署注入，**不写数据库、不写日志**。
- 无令牌或令牌错误 → `401`，服务把该插件标记为 `unreachable`。
- 每个请求可带 `X-Northbound-Contract: northbound-connector/v1`。

### 4.2 端点

| 方法 | 路径 | 作用 | 超时 |
|---|---|---|---|
| GET | `/v1/describe` | 能力与配置 Schema | 5s |
| GET | `/v1/health` | 进程健康 | 5s |
| POST | `/v1/validate` | 校验配置与凭证（不发送业务数据） | 20s |
| POST | `/v1/open` | 建立目标连接，返回实例句柄 | 20s |
| POST | `/v1/write` | 发送一批数据 | 20s |
| POST | `/v1/close` | 释放实例 | 20s |

### 4.3 `describe`

```json
{
  "contractVersion": "northbound-connector/v1",
  "id": "io.thingspanel.sink.http",
  "version": "1.0.0",
  "displayName": "HTTP 数据出口",
  "description": "…",
  "capabilities": {
    "eventTypes": ["telemetry", "attribute", "event"],
    "batch": true,
    "ackLevel": "destination_acked",
    "idempotency": "conditional",
    "verify": false,
    "maxBatchEvents": 200
  },
  "configSchema": { "type": "object", "…": "…" },
  "secretFields": ["token"],
  "limits": {"maxBatchEvents": 200, "maxPayloadBytes": 4194304, "maxInFlight": 4}
}
```

- `id` 必须与部署登记的连接器 ID 完全一致，否则服务拒绝该插件。
- `ackLevel` 只能声明 `destination_acked` 或 `effect_confirmed`。只做协议确认的插件必须声明 `destination_acked`，页面会显示“目标已接收，业务效果未核验”。
- `idempotency`：`none` / `conditional` / `strong`。声明 `strong` 才允许对“未知结果”直接重放。

### 4.4 `write`

```json
{
  "contractVersion": "northbound-connector/v1",
  "connectionId": "…",
  "instanceId": "…",
  "pipelineId": "…",
  "attempt": 2,
  "items": [
    {
      "deliveryId": "…",
      "eventId": "…",
      "idempotencyKey": "nb1:…",
      "eventType": "telemetry",
      "subject": "devices/device-1",
      "occurredAt": "2026-10-21T08:00:00Z",
      "deviceId": "device-1",
      "data": {"temperature": 23.6},
      "serialization": {"topic": "northbound/{deviceId}"}
    }
  ]
}
```

响应：

```json
{
  "contractVersion": "northbound-connector/v1",
  "results": [
    {
      "deliveryId": "…",
      "status": "delivered",
      "ackLevel": "destination_acked",
      "receipt": {"status": 200},
      "errorCode": "",
      "errorDetail": "",
      "safeToRetry": true,
      "retryAfterMs": 0
    }
  ]
}
```

规则（强制）：

1. **每个输入项必须有一条结果。** 缺失的结果按 `unknown` 处理，绝不按成功处理。
2. `status` 只能是 `delivered` / `retryable_failure` / `permanent_failure` / `unknown`。
3. `safeToRetry=false` 且 `status=retryable_failure` 时，平台把结果记为 `unknown`，不自动重试。
4. `permanent_failure` 不重试；若 `errorCode` 为 `authentication_failed` 或 `config_invalid`，平台把连接标记为 `needs_repair`。
5. `retryAfterMs` 优先于平台退避，但不超过 5 分钟。
6. 插件不得自行持久化待发数据；它只负责单次批次。

### 4.5 结果分类约定

| 目标情况 | 插件应返回 |
|---|---|
| 2xx / PUBACK(QoS≥1) | `delivered` + `destination_acked` |
| 429 / 503 / 连接被拒 | `retryable_failure` + `retryAfterMs`（若有） |
| 401 / 403 / 400 / 422 | `permanent_failure` + `authentication_failed` 或 `http_4xx` |
| 请求已发出但未收到响应（超时） | `unknown` + `target_result_unknown`，`safeToRetry=false` |
| 请求未发出（DNS/连接失败） | `retryable_failure` + `safeToRetry=true` |

---

## 5. 管理 API

浏览器路径 `/api/v1/northbound/<x>` 由社区后端**纯传输代理**到北向服务 `/<x>`（`/v1/...`）。代理不改写身份、不做授权判定。

| 方法 | 路径 | 动作 | 权限 |
|---|---|---|---|
| GET | `/v1/overview` | 状态汇总 | `northbound:view` |
| GET | `/v1/connectors` | 连接器目录与真实健康 | `northbound:view` |
| GET | `/v1/connections` | 目标连接列表 | `northbound:view` |
| GET | `/v1/connections/:id` | 连接详情 | `northbound:view` |
| POST | `/v1/connections` | 新建连接 | `northbound:manage` |
| PUT | `/v1/connections/:id` | 修改（需 `expectedRevision`） | `northbound:manage` |
| DELETE | `/v1/connections/:id` | 删除（有任务或未完成记录时拒绝） | `northbound:manage` |
| POST | `/v1/connections/:id/test` | 校验并打开目标 | `northbound:manage` |
| GET | `/v1/pipelines` | 转发任务列表（含积压） | `northbound:view` |
| GET | `/v1/pipelines/:id` | 任务详情 | `northbound:view` |
| POST | `/v1/pipelines` | 新建草稿 | `northbound:manage` |
| PUT | `/v1/pipelines/:id` | 修改（需 `expectedRevision`） | `northbound:manage` |
| POST | `/v1/pipelines/:id/preview` | 用样例数据预览，不发送 | `northbound:view` |
| POST | `/v1/pipelines/:id/activate` | 启用 | `northbound:manage` |
| POST | `/v1/pipelines/:id/pause` | 暂停（保留积压） | `northbound:manage` |
| POST | `/v1/pipelines/:id/disable` | 停止新增与投递（保留积压） | `northbound:manage` |
| POST | `/v1/pipelines/:id/test-send` | 发送一条测试数据 | `northbound:manage` |
| DELETE | `/v1/pipelines/:id` | 删除草稿任务（仅 `draft` 且从未产生投递） | `northbound:manage` |
| GET | `/v1/deliveries` | 投递记录（分页/过滤） | `northbound:view` |
| POST | `/v1/deliveries/:id/replay` | 受控重放（需 `reason`） | `northbound:replay` |
| GET | `/v1/subscriptions` | 源端订阅刷新 | 服务令牌 |
| POST | `/v1/inbox/events` | 源端交接 | 服务令牌 |

权限矩阵（与社区既有角色一致，不新建身份）：

| 角色 | view | manage | replay |
|---|---|---|---|
| `tenant_admin` | ✓ | ✓ | ✓ |
| `tenant_operator` | ✓ | ✗ | ✗ |
| `tenant_viewer` | ✓ | ✗ | ✗ |
| `platform_admin` | ✓ | ✓ | ✓ |

---

## 6. 状态机

### 6.1 转发任务

```
draft ──activate──> active ──pause──> paused ──activate──> active
                      │                  │
                      └──disable──> disabled <──disable────┘
```

- `active`：新事件生成投递。
- `paused`：不生成新投递，**继续投递已有积压**。
- `disabled`：不生成新投递，**停止投递**，积压保留。
- 删除任务**只允许草稿**：`draft` 且从未产生过任何投递。一旦任务跑过，它就拥有了必须留存的投递记录，删除会毁掉审计证据，因此返回 409 `aborted`，让操作者改用 `pause`/`disable`。这条是 v1.2 的补充：v1.1 写的是"首版不提供删除"，但页面已经提供了删除入口，前后端不一致；与其留一个点了报错的按钮，不如把删除限定在唯一无风险的状态上。

### 6.2 投递

```
pending ──claim──> leased ──delivered──> delivered
   ▲                  │
   │                  ├──retryable──> retry_wait ──> pending
   │                  ├──permanent──> dead
   │                  └──no answer──> unknown
   └────replay────────┴────────────── dead / unknown（保留原幂等键）
```

- `leased` 租约 30 秒，过期可被重新认领（进程崩溃恢复）。
- `unknown` 不自动重试。
- `dead` 只能由人工重放。

---

## 7. 幂等、未知结果、重放

- 幂等键：`nb1:sha256(tenantId \0 pipelineId \0 eventId \0 branch)`，**不含**插件版本、尝试次数、任务版本。
- 重放保持原幂等键；需要故意再产生一次业务效果时，属于另一个显式操作，首版不提供。
- 对 `unknown` 重放必须满足其一：连接器声明 `idempotency=strong`，或请求带 `acknowledgeRisk=true`。
- `delivered` 不允许重放。

---

## 8. 容量、保留、背压

| 项 | 默认值 | 行为 |
|---|---|---|
| 源端单租户积压 | 50,000 条 / 64 MiB | 超限**拒绝登记**并计入 `northbound_source_rejects` |
| 北向单租户待投递 | 20,000 条 | 超限拒绝受理，源端保留重试 |
| 单事件载荷 | 256 KiB | 超限拒绝 |
| 源端保留 | 7 天 | 只清理 `handed_off`/`failed` |
| 北向事件保留 | 7 天 | 只清理无未完成投递的事件 |
| 投递账保留 | 30 天 | 只清理 `delivered`/`dead` |
| 认领租约 | 30 秒 | — |
| 最大尝试 | 12 | 之后 `dead` |
| 退避 | 1s×2ⁿ，上限 5 分钟，±20% 抖动 | 尊重 `retryAfterMs` |

**“已登记等待交接”与“因容量不足未登记”必须分开显示。** 前者是 Outbox 中 `pending`/`leased` 的行；后者只存在于拒绝计数里，不能被描述为“等待发送”。

---

## 9. 错误码

服务返回 Encore 标准错误体：

```json
{"code": "invalid_argument", "message": "…", "details": null}
```

| HTTP | code | 场景 |
|---|---|---|
| 400 | `invalid_argument` | 参数/JSON 不合法 |
| 401 | `unauthenticated` | 缺令牌、令牌无效、服务令牌错误 |
| 403 | `permission_denied` | 角色不允许该动作 |
| 404 | `not_found` | 资源不存在或不属于本租户 |
| 409 | `aborted` | 版本冲突、状态不允许、仍有积压、需要确认风险 |
| 503 | `unavailable` | 数据库或依赖不可用 |

**跨租户访问一律返回 404，不返回 403**，避免泄露资源是否存在。

**为什么冲突码是 `aborted` 而不是 `failed_precondition`**：Encore 按 gRPC 官方映射把 `failed_precondition` 转成 HTTP 400，语义会变成"你的请求格式有问题"。冲突的真实含义是"资源变了/状态不允许，刷新后重试"，必须是 409。因此实现用 `errs.Aborted`，本表随之以 `aborted` 为准。这条是 v1.1 的更正，v1.0 曾写成 `failed_precondition`+409，两者无法同时成立。

### 修订记录

| 版本 | 日期 | 变化 |
|---|---|---|
| v1.0 | 2026-10-21 | 首次冻结 |
| v1.1 | 2026-10-21 | 修正 §9 冲突码为 `aborted`（Encore 把 `failed_precondition` 映射成 400，与 409 冲突）；其余不变 |
| v1.2 | 2026-10-21 | 补 §5 与 §6.1：新增 `DELETE /v1/pipelines/:id`，仅允许删除 `draft` 且无投递的任务。原因见 §6.1 |

---

## 10. 兼容与演进

- 新增可选字段：不升版本，旧实现忽略。
- 删除/改名字段、改变语义：升 `contractVersion`，新旧并行一个版本，文档注明下线时间。
- 插件升级：旧积压保持原任务版本与幂等键；升级只影响新投递。
- 服务回滚：不撤销已写入第三方的结果；只恢复后续任务的执行版本。

---

## 11. 一致性测试清单

任何插件合并前必须通过。下表把每一条映射到**实际执行它的脚本与断言名**，断言名与
`docs/northbound/reports/` 里报告表格的「断言」列完全一致，可以直接对照。

| # | 合同要求 | 由谁验证 | 断言名 |
|---|---|---|---|
| 1 | `describe` 的 `id`/`contractVersion` 与服务登记一致 | `release.py` | `HTTP 插件 describe 与合同一致`、`MQTT 插件 describe 与合同一致` |
| 2 | 无令牌 → 401；错误令牌 → 401 | `release.py` | `HTTP 插件无令牌时全部拒绝`（6 个端点逐个试）、`MQTT 插件无令牌时全部拒绝`、`插件拒绝错误令牌` |
| 3 | `validate` 对错误配置返回 `ok=false` 且不发送数据 | `release.py` | `错误配置被 validate 拒绝`、`校验失败不向目标发送数据`（用独立探针路径，不受其它用例流量干扰） |
| 3b | 正确配置时 `validate` 三项检查全绿 | `journey.py` | `连接校验三项全绿` |
| 4 | `write` 对 N 个输入返回 N 条结果（缺失结果 = unknown） | `recovery.py` | `两个事件都被受理`、`成功的一条 delivered`、`失败的一条独立 retry_wait` |
| 5 | 2xx → `delivered`；429 → `retryable_failure` 且带 `retryAfterMs`；401 → `permanent_failure` + `authentication_failed` | `recovery.py` | `500 后进入 retry_wait`、`429 后进入 retry_wait`、`Retry-After 被记录`、`401 后记录进入 dead`、`连接被标记为 needs_repair` |
| 6 | 超时 → `unknown` + `safeToRetry=false` | `recovery.py` | `超时后进入 unknown`、`unknown 不会被自动重试`、`unknown 保留插件给出的原因` |
| 7 | 同一 `idempotencyKey` 重放不在目标产生第二次业务写入 | `release.py` + `journey.py` | `重复提交后幂等键不变`、`重复提交不新增投递`、`幂等键格式统一为 nb1:`、`幂等键不含实例或尝试次数`、`重复事件被识别为 duplicate`、`同一事件只有一条投递` |
| 8 | 进程被 kill 后重启，未确认的记录由平台重新投递，插件不残留自己的队列 | `recovery.py` | `过期租约被自动回收并送达` |
| 9 | QoS0 必须报 `ackLevel=none`，不得冒充已确认 | `release.py` | `QoS0 记为 none（不冒充已确认）` |

### 本清单没有覆盖的部分

如实列出，避免被读成"全都验过了"：

- **第 8 条用的是租约过期，不是真的 `kill -9`**。两者对平台的效果一样（进程消失 →
  租约到期 → 被重新认领），但真杀进程还会顺带验证信号处理与优雅退出，这一层没测。
- **第 7 条验证的是「平台不重复写账本、幂等键稳定送达」**，没有验证「接收端按幂等键
  去重」。验收用的本地接收端只记录不去重，所以"目标侧确实没写第二次"这件事依赖
  接收端实现，不由平台保证——这正是合同要求插件声明 `duplicates_possible` 的原因。
- **插件重启后 `instanceId` 变化不影响幂等**：由 `幂等键不含实例或尝试次数` 这条从
  格式上保证（键里根本没有 instance 段），没有做「重启插件再重发同一事件」的端到端
  演练。

对应实现见 `encore-api/northbound/northbound_test.go`（纯逻辑单测）与
`scripts/qa/northbound/`（真实数据库 + 真实插件 + 真实接收端 + 真实 Broker）。
