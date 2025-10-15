# 数据处理模块设计

## 模块输入输出设计

### 输入数据结构
```lua
{
  "direction": "uplink" | "downlink",  -- 数据方向
  "type": "telemetry" | "attribute" | "event" | "telemetry_control" | "attribute_set" | "command",  -- 数据类型
  "deviceId": "device_123",  -- 设备ID
  "deviceConfigId": "config_456",  -- 设备配置ID（用于查找对应脚本）
  "rawData": []byte,  -- 原始数据（字节数组）
  "timestamp": 1697123456789  -- 时间戳
}
```

### 输出数据结构

**上行数据输出（设备→平台）：**
```lua
{
  "success": true,
  "deviceId": "device_123",
  "type": "telemetry",
  "data": {  -- 标准化后的数据
    "temperature": 25.5,
    "humidity": 60,
    "pressure": 101.3
  },
  "timestamp": 1697123456789,
  "error": null  -- 处理失败时的错误信息
}
```

**下行数据输出（平台→设备）：**
```lua
{
  "success": true,
  "deviceId": "device_123",
  "type": "attribute_set",
  "encodedData": []byte,  -- 编码后的设备协议数据（字节数组）
  "error": null
}
```

## 模块核心接口设计

```
数据处理模块提供两个核心方法：

1. decode(input) -> output
   - 用于上行数据：rawData([]byte) → 标准化数据
   
2. encode(input) -> output
   - 用于下行数据：标准化数据 → encodedData([]byte)
```

## 解耦架构示意

```
上行流程：
设备原始数据 → 消息处理 → 数据解析 
  ↓
[数据处理模块]
  - 根据 deviceConfigId 加载对应 Lua 脚本
  - 调用 decode() 转换数据
  - 返回标准化数据
  ↓
业务处理（规则引擎、告警） → 数据存储


下行流程：
业务指令 
  ↓
[数据处理模块]
  - 根据 deviceConfigId 加载对应 Lua 脚本
  - 调用 encode() 编码数据
  - 返回设备协议数据([]byte)
  ↓
消息下发 → 设备
```

## 模块关键特性

**1. 脚本管理**
- 按 deviceConfigId 存储和索引脚本
- 支持脚本版本管理
- Redis缓存

**2. 执行隔离**
- Lua 沙箱环境（限制危险函数）
- 超时控制（例如：3秒超时）
- 内存限制（例如：50MB）

**3. 错误处理**
- 脚本执行失败时返回详细错误
- 支持降级策略（透传或使用默认解析）
- 执行日志记录

**4. 性能优化**
- 脚本预编译和缓存
- 批量处理支持
- 异步执行（非阻塞）

## 与其他模块的交互

```
数据处理模块不需要知道：
✗ 数据来自哪个协议（MQTT/CoAP/TCP）
✗ 业务规则（告警阈值、规则引擎）
✗ 存储方式（时序数据库/关系数据库）
✗ 设备的产品信息

数据处理模块只需要知道：
✓ 设备配置ID（deviceConfigId）
✓ 数据类型和方向
✓ 原始字节数据
```
