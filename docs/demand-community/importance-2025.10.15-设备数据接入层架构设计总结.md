# 设备数据接入层架构设计总结（2025.10 重构版）

## 一、背景与目标

### 1.1 重构背景
- **旧版问题**：MQTT订阅函数体量过大（>200行），业务逻辑耦合严重
- **扩展困难**：难以支持新协议（Kafka、HTTP等），代码复用性差
- **维护成本高**：功能交叉、职责不清，难以定位问题

### 1.2 设计目标
- **协议无关**：支持MQTT、Kafka等多种接入协议，易于扩展
- **职责分离**：清晰的层次划分，每层专注单一职责
- **高性能**：基于Channel的异步处理，支持高并发场景
- **可观测性**：完善的日志、指标监控体系

---

## 二、整体架构

### 2.1 架构分层

系统采用**五层架构**，数据流向为：

```
设备 → Adapter层 → Uplink层 → Processor层 → Storage层 → 数据库
                      ↓
                  Forwarder（数据转发）
```

**下行链路**：
```
API/场景 → Downlink层 → Adapter层 → 设备
```

### 2.2 核心设计原则

1. **协议适配隔离**：Adapter层负责协议差异屏蔽，上层统一处理
2. **消息驱动**：层与层之间通过Bus（消息总线）解耦
3. **异步处理**：基于Channel的生产者-消费者模式
4. **接口抽象**：依赖接口而非具体实现，便于测试和扩展

---

## 三、各层详细设计

### 3.1 Adapter层（协议适配层）

**职责**：
- 接收不同协议的原始数据（MQTT Topic订阅、Kafka消费、HTTP请求等）
- 协议格式验证和初步解析
- 统一转换为标准`DeviceMessage`格式
- 发布到Uplink Bus消息总线

**核心组件**：
- `mqttadapter.Adapter`：MQTT协议适配器
- `kafkaadapter.Adapter`：Kafka协议适配器
- `adapter.DeviceMessage`：统一消息格式

**设计要点**：
- **消息类型识别**：根据Topic/主题自动识别消息类型（遥测/属性/事件/状态）
- **设备类型判断**：区分直连设备和网关设备（`gateway_telemetry` vs `telemetry`）
- **格式检测**：自动识别实时数据和历史数据格式（数组vs对象）
- **协议响应**：Adapter层负责协议层ACK响应（如MQTT的立即回复）

**消息类型**：
```
直连设备：telemetry, attribute, event, status
网关设备：gateway_telemetry, gateway_attribute, gateway_event
历史数据：telemetry_history, event_history（通过Metadata标记）
```

---

### 3.2 Uplink层（上行数据处理层）

**职责**：
- 从Bus接收统一格式的设备消息
- 调用Processor进行数据解码（脚本处理）
- 分发处理结果到Storage存储和Forwarder转发
- 更新设备在线状态和心跳

**核心组件**：
- `uplink.Bus`：消息总线（按类型分发到不同Channel）
- `uplink.TelemetryUplink`：遥测数据处理器
- `uplink.AttributeUplink`：属性数据处理器
- `uplink.EventUplink`：事件数据处理器
- `uplink.StatusUplink`：设备状态处理器
- `uplink.ResponseUplink`：下行指令响应处理器
- `uplink.UplinkManager`：统一管理各处理器的生命周期

**设计要点**：
- **消息总线**：根据消息类型路由到对应的Channel（背压机制防止内存溢出）
- **网关数据拆分**：`gateway_telemetry`等消息会被拆分为多个单设备消息
- **历史数据支持**：识别并批量处理历史数据数组格式
- **心跳管理**：遥测数据上报时触发心跳更新
- **数据转发**：处理后的数据通过Forwarder接口转发

**处理流程**：
```
1. Bus接收消息 → 按类型分发到对应Channel
2. XxxUplink从Channel消费消息
3. 调用Processor进行脚本解码
4. 发送到Storage Channel进行存储
5. 触发Forwarder进行数据转发
6. 更新心跳和在线状态
```

---

### 3.3 Processor层（数据处理层）

**职责**：
- 执行Lua脚本进行数据编解码
- 上行：设备原始数据 → JSON标准格式
- 下行：JSON标准数据 → 设备协议格式
- 脚本执行超时控制和错误处理

**核心组件**：
- `processor.DataProcessor`：处理器核心接口
- `processor.LuaExecutor`：Lua脚本执行器
- `processor.ScriptProcessor`：脚本处理器（含缓存）

**设计要点**：
- **脚本缓存**：根据DeviceConfigID缓存脚本内容，减少数据库查询
- **沙箱隔离**：每次执行创建独立Lua虚拟机，确保安全性
- **超时控制**：脚本执行超时自动中断（默认5秒）
- **协议类型**：支持telemetry/attribute/event/command等多种数据类型

**接口定义**：
```
Decode(上行解码): DeviceConfigID + RawData → JSON Data
Encode(下行编码): DeviceConfigID + JSON Data → RawData
```

---

### 3.4 Storage层（存储层）

**职责**：
- 接收已解码的标准格式数据
- 写入数据库（遥测历史表、最新值表、属性表、事件表）
- 批量写入优化（遥测数据）
- 存储性能监控

**核心组件**：
- `storage.Storage`：存储服务主接口
- `storage.telemetryWriter`：遥测数据批量写入器
- `storage.directWriter`：属性/事件直接写入器

**设计要点**：
- **批量写入**：遥测数据按批次+时间窗口批量插入（提升性能）
- **分表写入**：遥测同时写入历史表和最新值表
- **异步存储**：通过Channel异步消费，不阻塞上游处理
- **监控指标**：记录接收数量、写入成功/失败数量、延迟等

**数据类型**：
```
Telemetry → telemetry_datas + telemetry_current_datas
Attribute → attribute_datas
Event → event_datas
```

---

### 3.5 Downlink层（下行指令层）

**职责**：
- 接收平台下发的指令请求（命令/属性设置/遥测下发）
- 调用Processor进行数据编码
- 通过Adapter发送到设备
- 更新指令日志状态

**核心组件**：
- `downlink.Bus`：下行消息总线
- `downlink.Handler`：下行消息处理器
- `downlink.MessagePublisher`：消息发布接口（Adapter实现）

**设计要点**：
- **消息类型**：Command（命令）、AttributeSet（属性设置）、AttributeGet（属性获取）、Telemetry（遥测下发）
- **脚本编码**：根据DeviceConfigID调用Encode脚本
- **日志管理**：自动更新command_set_logs/attribute_set_logs/telemetry_set_logs表
- **协议抽象**：Handler不依赖具体协议，通过MessagePublisher接口发送

**处理流程**：
```
1. API调用 → 创建日志记录（status=0）
2. 发布到Downlink Bus
3. Handler消费消息 → 脚本编码
4. 调用Adapter发布消息 → 更新日志（status=1成功/2失败）
```

---

## 四、关键技术设计

### 4.1 消息总线（Bus）

**Uplink Bus**：
- 按消息类型分Channel（telemetry/attribute/event/status/response）
- 缓冲队列（默认10000）+ 背压机制
- 支持优雅关闭

**Downlink Bus**：
- 按指令类型分Channel（command/attribute_set/attribute_get/telemetry）
- 启动时绑定Handler自动消费

### 4.2 网关数据处理

**格式识别**：
```json
{
  "gateway_datas": {...},        // 网关自身数据
  "sub_device_datas": {          // 子设备数据
    "sub_device_id_1": {...},
    "sub_device_id_2": {...}
  }
}
```

**处理策略**：
- Adapter层：识别为`gateway_telemetry`等类型
- Uplink层：拆分为多个单设备消息分别处理
- 支持网关和子设备同时上报

### 4.3 历史数据支持

**格式检测**：
- 实时格式：`{key: value}` 对象
- 历史格式：`[{key: value, ts: 123}, ...]` 数组

**处理流程**：
- Adapter层：检测格式，在Metadata中标记`is_historical=true`
- Uplink层：识别历史标记，批量插入多条记录

### 4.4 设备状态管理

**状态来源**：
- `status_message`：设备主动上报（MQTT Will消息）
- `heartbeat_expired`：心跳超时检测
- `timeout_expired`：连接超时检测

**处理链路**：
```
来源 → StatusUplink → 更新设备状态缓存 + 数据库 → WebSocket推送
```

### 4.5 数据转发

**Forwarder接口**：
- Uplink层处理完成后调用`ForwardData()`
- 支持多目标转发（HTTP/MQTT/Kafka等）
- 异步转发，不阻塞主流程

---

## 五、扩展性设计

### 5.1 新增协议接入

只需实现新的Adapter：
```
1. 实现Adapter结构体
2. 解析协议数据 → 转换为DeviceMessage
3. 发布到Uplink Bus
4. 实现MessagePublisher接口（下行）
```

### 5.2 新增数据类型

在现有层次中添加对应处理器：
```
1. Uplink层：新增XxxUplink处理器
2. Storage层：新增对应表的Writer
3. Bus层：新增对应Channel
```

### 5.3 脚本语言扩展

Processor层采用接口设计，可扩展支持：
- JavaScript（Goja引擎）
- Python（嵌入式）
- WASM（沙箱执行）

---

## 六、优势总结

### 6.1 对比旧版改进

| 维度 | 旧版 | 新版 |
|------|------|------|
| 协议支持 | 仅MQTT | MQTT + Kafka + 易扩展 |
| 代码行数 | 单函数>200行 | 分层<100行/模块 |
| 可测试性 | 难以单元测试 | 接口抽象，易测试 |
| 扩展性 | 需改动核心代码 | 新增Adapter即可 |
| 可维护性 | 职责混乱 | 职责清晰 |

### 6.2 架构优势

1. **高内聚低耦合**：各层职责明确，接口依赖
2. **水平扩展**：支持多实例部署（Kafka消费者组）
3. **性能优化**：批量写入、异步处理、脚本缓存
4. **可观测性**：完善的日志和监控指标
5. **向后兼容**：保留MQTT原有功能，平滑迁移

---

## 七、后续优化方向

1. **性能优化**：引入协程池减少goroutine创建开销
2. **监控增强**：增加Prometheus指标暴露端点
3. **容错能力**：增加重试机制和死信队列
4. **插件化**：支持热加载协议插件
5. **流控优化**：支持动态调整Channel缓冲区大小
