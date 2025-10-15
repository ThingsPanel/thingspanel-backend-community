# Flow 层设计

## 1. 概述

### 当前问题

- MQTT 订阅处理代码耦合（单文件 `mqtt/subscribe/telemetry_message.go`）
- 数据处理流程分散在各个订阅处理函数中
- 难以扩展其他数据源（MQTT/Kafka）
- 业务逻辑与协议层耦合

### 设计目标（关注数据流处理核心流程）

```
MQTT 消息
  ↓
telemetry_message.go (200+ 行代码)
  ├─ 设备验证
  ├─ 数据脚本处理 (service.GroupApp.DataScript.Exec)
  ├─ 数据转换（map → TelemetryData）
  ├─ 消息发送 (TelemetryMessagesChan)
  ├─ 心跳处理
  ├─ 数据转发 (publish.ForwardTelemetryMessage)
  └─ 场景联动 (service.GroupApp.Execute)
```

## 2. 设计方案

### 核心思路

1. **协议适配层（Adapter）**：解耦数据来源（MQTT/Kafka/HTTP）
2. **消息总线（Bus）**：统一数据流转通道
3. **流处理层（Flow）**：负责业务处理流程编排
4. **执行模块**：复用现有模块（Processor、Storage、Automation）

### 优势

- 解耦协议层和业务层
- 支持多种数据源
- 降低模块间耦合（Processor、Storage、Automation）

## 3. 架构设计

### 3.1 层次架构

```
┌──────────────────────────────────────────────┐
│         适配层 (Adapter)                     │
│  - MQTT Adapter                              │
│  - Kafka Adapter (未来)                      │
│  - HTTP Adapter (未来)                       │
└──────────────┬───────────────────────────────┘
               ↓ 统一消息格式 (DeviceMessage)
┌──────────────────────────────────────────────┐
│         消息总线 (Bus)                       │
│  - 基于 buffered channel                     │
│  - 按消息类型分发                            │
└──────────────┬───────────────────────────────┘
               ↓
┌──────────────────────────────────────────────┐
│      消息流处理层 (Flow)                     │
│  - TelemetryFlow   (遥测)                    │
│  - AttributeFlow   (属性数据)                │
│  - EventFlow       (事件数据)                │
│  - CommandFlow     (指令数据)                │
└─────┬──────┬──────┬──────┬────────────────────┘
      ↓      ↓      ↓      ↓
  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐  外部模块（复用现有模块）
  │设备  │ │数据  │ │存储  │ │场景  │
  │验证  │ │脚本  │ │层    │ │联动  │
  └──────┘ └──────┘ └──────┘ └──────┘
```

### 3.2 目录结构

```
internal/
├── adapter/              # 协议适配层模块
│   ├── message.go        # 统一消息格式定义
│   ├── mqtt_adapter.go   # MQTT 适配器实现
│   └── kafka_adapter.go  # Kafka 适配器（未来）
│
├── flow/                 # 消息流处理层
│   ├── bus.go            # 消息总线（channel 管理）
│   ├── telemetry.go      # 遥测数据流程
│   ├── attribute.go      # 属性流程（待实现）
│   ├── event.go          # 事件流程（待实现）
│   └── flow_manager.go   # 流程管理器
│
├── processor/            # 现有数据脚本处理（数据脚本层）
├── storage/              # 现有存储层
└── service/              # 现有业务服务（心跳、场景联动）

internal/app/
└── flow.go               # Flow 层集成启动
```

## 4. 核心模块设计

### 4.1 统一消息格式 (`adapter/message.go`)

**目标**：定义跨协议的统一消息结构

**核心字段**：

- `MessageType`：消息类型（telemetry/attribute/event/command）
- `DeviceID`：设备 ID
- `TenantID`：租户 ID
- `Timestamp`：时间戳（毫秒）
- `Payload`：原始数据（[]byte 格式）
- `Metadata`：附加信息（例如 Topic、QoS）

**设计优势**：

- 协议类型无关性
- Payload 保持原始字节流
- 支持扩展字段

### 4.2 适配器 (`adapter/mqtt_adapter.go`)

**目标**：

1. 订阅 MQTT Topic
2. 解析消息并识别消息类型
3. 转换为 `DeviceMessage`
4. 发送到消息总线

**处理流程**：

```
MQTT 消息订阅
  ↓
根据 Topic 解析消息类型
  ↓
提取 DeviceID、TenantID
  ↓
设备验证（调用设备缓存）
  ↓
构造 DeviceMessage
  ↓
发送到 Bus (非阻塞，channel 缓冲机制)
```

**待实现功能**：

- 使用 `verifyPayload()` 验证
- 使用 `initialize.GetDeviceCacheById()` 获取设备
- 待实现：脚本处理验证

### 4.3 消息总线 (`flow/bus.go`)

**目标**：消息分发中心

**实现方式**：

- 基于 buffered channel
- 按消息类型分发（telemetryChan、attributeChan...）
- 支持可配置 buffer size

**核心接口**：

- `PublishTelemetry(msg *DeviceMessage)`
- `SubscribeTelemetry() <-chan *DeviceMessage`
- `Close()`：优雅关闭

**后续支持 Kafka**：

- 当前 QPS 不高（预估 1 万），缓存足够
- Channel 特点：高性能
- 待扩展升级：Kafka 消息队列（兼容、解耦）

### 4.4 遥测数据流程 (`flow/telemetry.go`)

**目标**：处理遥测数据的完整处理流程

**处理步骤**：

1. **解析数据**：Payload ([]byte) → map[string]interface{}
2. **数据脚本处理**（可选）：
   - 检查设备是否配置脚本（device.DeviceConfigID）
   - 调用 `service.GroupApp.DataScript.Exec()`
   - 根据结果决定是否继续后续流程
3. **数据转换**：map → TelemetryData 结构
4. **存储发送**：发送到 Storage 的 inputChan
5. **心跳处理**：异步调用 `HeartbeatDeal()`
6. **数据转发**：调用 `publish.ForwardTelemetryMessage()`
7. **场景联动**：异步调用 `service.GroupApp.Execute()`

**关键考虑**：

- 保持顺序性：依次执行各步骤（device cache、script service、storage、automation）
- 错误降级流程：脚本失败继续后续流程
- 日志记录（心跳、场景联动）：异步执行
- 使用现有模块解耦代码

### 4.5 流程管理器 (`flow/flow_manager.go`)

**目标**：统一管理所有 Flow 的生命周期

**职责**：

- 启动所有 Flow 独立 goroutine
- 连接 Bus 的 channel 到对应 Flow
- 监听 Flow 的生命周期
- 优雅关闭所有消息处理协程

## 5. 集成启动

### 5.1 启动流程

```
main.go
  ↓
app.NewApplication(
    ...
    app.WithFlowService(),  // 新增
)
  ↓
WithFlowService()
  ├─ 读取配置 (viper)
  ├─ 创建 Bus (buffer size 可配置)
  ├─ 创建 TelemetryFlow (依赖注入)
  ├─ 创建 FlowManager
  ├─ 包装为 FlowServiceWrapper (实现 Service 接口)
  └─ 注册到 ServiceManager
  ↓
application.Start()
  ↓
ServiceManager.StartAll()
  ↓
FlowServiceWrapper.Start()
  ├─ Bus 启动
  ├─ 各 Flow 启动独立 goroutine
  └─ MQTT Adapter 启动
```

### 5.2 依赖注入

**从 Application 获取**：

- `*gorm.DB`：数据库连接
- `*logrus.Logger`：日志
- `*redis.Client`：Redis 连接（设备验证）
- `Storage`：存储服务接口
- `Processor`：数据脚本处理器

**配置项（conf.yml）**：

```yaml
flow:
  enable: true                    # 是否启用流程
  bus_buffer_size: 10000          # Bus channel 缓冲区大小
  telemetry_worker_count: 1       # 遥测处理 worker 数量
```

### 5.3 优雅关闭

```
application.Shutdown()
  ↓
ServiceManager.StopAll()
  ↓
FlowServiceWrapper.Stop()
  ├─ 停止 MQTT Adapter (停止订阅消息)
  ├─ 关闭 Bus (close channels)
  ├─ 等待 Flow 处理完现有消息 (最多 30s)
  └─ 输出日志
```

## 6. 数据流示例

### 遥测数据的完整流程

```
1. MQTT 消息订阅
   Topic: devices/telemetry
   Payload: {"device_id": "dev001", "values": {...}}

2. MQTT Adapter 处理
   - 根据 Topic 解析 MessageType = Telemetry
   - 设备验证，获取设备信息
   - 构造 DeviceMessage
   - 发送到 Bus.telemetryChan

3. Bus 分发
   - 从 telemetryChan 读取
   - 分发到 TelemetryFlow

4. TelemetryFlow 处理
   - 解析数据 Payload
   - 检查 device.DeviceConfigID
     ├─ 有配置 → 调用 DataScript.Exec()
     └─ 无配置 → 跳过
   - 数据转换（map → []TelemetryDataPoint）
   - 发送到 Storage.inputChan
   - 异步：HeartbeatDeal(device)
   - 可选：ForwardTelemetryMessage()
   - 异步：Automation.Execute()

5. Storage 处理
   - 批量合并（使用现有逻辑）

6. Automation 处理
   - 规则匹配
   - 触发动作执行
```

## 7. 待实现模块关联

### 7.1 Processor (数据脚本)

**调用方式**：

- Flow 层根据 `device.DeviceConfigID` 是否存在
- 若存在则调用 `service.GroupApp.DataScript.Exec()`
- 输入：device、消息类型、原始数据、topic
- 输出：处理后数据（[]byte）

**错误处理**：

- 数据脚本处理根据结果决定后续流程
- （暂定）数据经过脚本后继续后续流程

### 7.2 Storage (存储层)

**调用方式**：

- Flow 层数据转换为 `storage.Message` 格式
- 发送到 Storage 的 inputChan
- Storage 内部批量合并处理（现有逻辑）

**格式转换**：

- 不依赖独立 Storage 接口
- 使用现有逻辑

### 7.3 Automation (场景联动)

**调用方式**：

- 异步调用 `service.GroupApp.Execute()`
- 输入：device、触发参数（AutomateFromExt）
- 触发参数包含：
  - `TriggerParamType`：触发类型（遥测/属性）
  - `TriggerParam`：参数名称
  - `TriggerValues`：参数值 map

**异步处理**：

- 不等待 goroutine 执行完成
- 不影响处理流程
- 根据结果更新

### 7.4 设备验证

**（配置）**：

- Adapter 层：设备缓存验证
- Flow 层：识别设备配置消息

**调用方式**：

- `initialize.GetDeviceCacheById(deviceID)`

## 8. 性能考量

### 8.1 吞吐量限制

- **Adapter → Bus**：非阻塞、可配置缓冲机制
- **Bus → Flow**：可配置 worker 数量（预估 1 万）
- **Flow → Storage**：Storage 内部批量合并处理（高性能）

### 8.2 延迟优化

- **可选步骤**：数据转换、存储发送、数据转发
- **异步步骤**：心跳、场景联动（不阻塞主流程）
- **设备缓存**：< 50ms (MQTT → Storage channel)

### 8.3 缓冲机制

- Bus channel 满了会导致Adapter 适配器阻塞
-

通过 MQTT 反压机制阻塞上游

- 注意存储层阻塞

### 8.4 资源占用

- **内存**：Bus buffer (10000 条消息 × 消息大小)
- **Goroutine**：适配器数量 + Worker + Storage
- **数据库连接**：使用 GORM 连接池

## 9. 配置管理

### 9.1 功能开关

```yaml
flow:
  enable: true  # false 则回退到旧逻辑（兼容老代码）
```

### 9.2 性能调优

```yaml
flow:
  bus_buffer_size: 10000          # 缓冲区大小
  telemetry_worker_count: 1       # 处理协程数
  enable_metrics: true             # 是否启用监控
```

### 9.3 兼容降级

- Flow 启动根据配置决定是否启用
- 数据脚本错误降级流程

## 10. 监控指标

### 10.1 关键指标

**Bus 指标**：

- `bus_channel_size`：当前 channel 缓存数量
- `bus_publish_total`：累计消息总数
- `bus_publish_blocked`：阻塞次数

**Flow 指标**：

- `flow_received_total`：收到消息总数
- `flow_processed_total`：处理完成总数
- `flow_failed_total`：处理失败总数
- `flow_duration_ms`：处理耗时（P50/P99）

**依赖指标**：

- `script_exec_total`：数据脚本次数
- `script_exec_failed`：数据脚本失败次数
- `automation_trigger_total`：场景联动触发次数

### 10.2 指标输出

- 使用 `pkg/metrics` 现有框架
- 通过 HTTP `/metrics` 端点输出

## 11. 开发计划

### Phase 1：框架搭建（Day 1）

- [ ] 创建 `adapter/message.go`（统一消息格式）
- [ ] 创建 `flow/bus.go`（消息总线）
- [ ] 创建 `flow/telemetry.go`（遥测流程框架）
- [ ] 创建 `internal/app/flow.go`（集成启动）
- [ ] 配置文件定义

### Phase 2：遥测数据集成（Day 2-3）

- [ ] 实现 `adapter/mqtt_adapter.go`
  - 迁移 `telemetry_message.go` 的验证逻辑
  - 接入 Bus
- [ ] 实现 `flow/telemetry.go`
  - 对接数据脚本处理、存储、场景联动
  - 错误处理
- [ ] 集成测试
  - 使用 virtual_sensor 发送数据
  - 验证数据正确处理
  - 验证场景联动触发

### Phase 3：功能完善（Day 4）

- [ ] 功能配置开关
- [ ] 缓冲机制（协程、流程控制）
- [ ] 性能测试
- [ ] 监控指标集成

### Phase 4：上线（Day 5）

- [ ] 代码审查
- [ ] 文档补充
- [ ] 代码 Review

### Phase 5：扩展其他消息类型（待实现）

- [ ] AttributeFlow（属性）
- [ ] EventFlow（事件）
- [ ] CommandFlow（指令响应）

## 12. 风险和问题缓解

### 12.1 主要风险

**风险 1：数据脚本处理顺序性**

- 现有数据脚本是否有时序依赖
- 缓解：保持顺序，复用所有数据脚本类型

**风险 2：场景联动性能问题**

- 异步处理不影响性能
- 缓解：监控指标、通知机制，兼容可选

**风险 3：线程安全性和时序性**
-

数据不一致问题

- 缓解：Feature Flag + 功能完善

### 12.2 问题缓解

1. **设备验证依赖**：缓存验证性能优化，避免重复消息影响订阅
2. **转发功能**：`publish.ForwardTelemetryMessage()` 调用失败，不阻塞主流程
3. **心跳处理**：`HeartbeatDeal()` 调用验证，避免缓存不一致影响心跳管理器
4. **批量合并处理器**：从 `MessagesChanHandler` 迁移调用流程

## 13. 未来扩展

### 13.1 Kafka 支持

**触发条件**：

- 单机器 QPS 超过 5000
- 分布式消息队列、持久化
- 分布式消息队列缓存

**改造步骤**：

- 实现 `KafkaBus`
- 独立配置管理
- 不依赖独立 Flow 层接口

### 13.2 多租户隔离

- Bus 支持按 TenantID 分组
- Flow Worker 按租户分组

### 13.3 其他 Flow

- Flow 的独立机制
- 支持不同优先级处理流程

---

**文档版本**：v1.0
**创建时间**：2025-10-15
**作者**：架构组
