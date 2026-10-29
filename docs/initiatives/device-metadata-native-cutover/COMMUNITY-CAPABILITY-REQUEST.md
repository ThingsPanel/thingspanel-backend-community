> **2026-10-29 更新：本文大部分已作废。** 产品确认社区版不做设备启停和换模板；子设备解绑和凭据轮换/撤销改为直接使用 Community 现有接口（`PUT /device/sub-remove`、`POST /device/update/voucher`）实现，不再需要 Community 新增能力。现状见 `HANDOFF-ENTRY.md` §6。下面保留原文只作历史记录。

# 需要 Community / MQTT 鉴权层补的能力（2026-10-29）

本文给 Community 和 MQTT 接入层的维护者：新版设备元数据已经把四类操作记入「操作 + outbox」，但因为运行时没有对应能力，它们一直停在“待处理”。这里说明每一类需要什么、我们这边已经准备好什么、做到什么程度算完成。依据是代码，不是推测：`devicemetadata/native_runtime_dispatch.go`（投递器）、`native_runtime_receipt.go`（回执）、`native_device_operation.go`（操作种类）。

## 现状

投递器（`TP_RUNTIME_OUTBOX_DISPATCH=on`，每分钟一次）只下发 Community 真正有对应接口的三类：

| 操作种类 | Community 调用 | 状态 |
|---|---|---|
| `device_renumber` | `PUT /api/v1/device {device_number}` | 已下发；测试替身下验证过 |
| `device_deletion` | `DELETE /api/v1/device/{id}` | 已下发；测试替身下验证过 |
| `gateway_binding`（仅绑定） | `PUT /api/v1/device {parent_id, sub_device_addr}` | 已下发 |

其余四类被投递器跳过，操作保持待处理（不会被误报成成功）：

| 操作种类 | 用户在页面做什么 |
|---|---|
| `administrative_state` | 设备“停用 / 启用” |
| `template_binding` | 设备换绑模板 |
| `credential_change` | 凭据轮换、撤销 |
| `gateway_binding`（解绑，`gatewayId` 为空） | 子设备解绑 |

## 需要的能力

### 1. 设备启停必须影响接入

- **缺什么**：Community 的设备更新请求没有启用字段；`is_enabled` 不参与 MQTT 鉴权（`mqtt/`、`internal/` 下没有检查）。所以“停用”写进去也不会阻止设备连接和上报。
- **需要**：更新接口接受启用状态；MQTT 鉴权（连接和发布两处）在设备停用时拒绝，已在线的连接被断开或至少不再接收其上行数据。
- **完成标准**：停用后设备无法新建连接、已有连接的上行不入库；启用后恢复；该接口对同一目标状态重复调用结果相同（幂等）。

### 2. 凭据按代（generation）生效

- **缺什么**：凭据没有“当前有效版本”的账本。轮换后旧凭据是否失效、撤销是否立即生效，运行时无法表达也无法回执。
- **需要**：每台设备有单调递增的凭据 generation；鉴权只接受当前 generation 对应的凭据；轮换产生新 generation，撤销使其不再被接受。
- **完成标准**：轮换后旧凭据无法再连接；撤销后对应凭据无法连接且不可恢复；回执里能报告已应用的 generation。

### 3. 换模板

- **缺什么**：Community 的设备更新没有“绑定的模板/物模型版本”字段，主题映射和数据处理脚本无法随模板切换。
- **需要**：设备更新可指定模板标识（或等价的主题映射/脚本集合）；切换对之后的上行立即生效。
- **完成标准**：换模板后新上行按新模板的 Topic 映射和处理脚本解析。

### 4. 子设备解绑

- **缺什么**：只有绑定（设置 `parent_id`、`sub_device_addr`），没有清除。
- **需要**：更新接口允许把 `parent_id` 清空（或提供专门的解绑接口）。
- **完成标准**：解绑后该设备不再属于网关，网关的子设备列表不再包含它。

## 我们这边已经准备好的

- **操作与 outbox**：提交时写入操作和 outbox 行，带 `generation` 和期望状态（`desired` JSON）；页面显示“已受理，待运行时应用”。
- **回执入库**：`runtimeReceipt{eventId, tenantId, deviceId, generation, contentDigest, state, failureCode, retryable}`，`state` 为 `received / applied / failed / stale`。只有 `applied` 才会把操作标为成功，`stale` 表示更新的 generation 已取代它。已有数据库测试，且处理过迟到和重复回执。
- **投递器扩展点**：`communityRuntimeApplier.Apply` 按操作种类分支；新增一类只需在这里加一个分支，并把 `Skip` 改为真实调用。成功才回执 `applied`，Community 明确拒绝回执 `failed`，网络错误或 5xx 保持待处理并重试。
- **页面与提示**：设备设置页已经提示“运行时应用回执尚未接入，状态可能长期停留在待应用”，能力补齐后无需改页面语义。

## 联调时我们这边的验收步骤

1. 开启 `TP_RUNTIME_OUTBOX_DISPATCH=on`，Community 补齐上述接口。
2. 在设备设置里停用设备 → 操作变成功；用该设备的凭据尝试连接应被拒绝。
3. 轮换凭据 → 旧凭据连接被拒，新凭据可用；撤销 → 凭据不可用。
4. 换模板 → 发一条上行，按新模板解析。
5. 网关下解绑子设备 → 子设备列表刷新后不再包含它。

每一步都应在 `E2E-CHECKLIST-2026-10-29.md` 的 D4、D5、D14 里留下证据后再勾选。

## 不在本仓库范围内

MQTT 接入强制层（Broker 插件或鉴权服务）不在这个仓库，上面第 1、2 项的“强制”部分必须由那边实现；本仓库只能下发期望状态并接收回执。
