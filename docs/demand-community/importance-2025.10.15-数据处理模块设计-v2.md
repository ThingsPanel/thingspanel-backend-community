# 数据处理模块设计 v2

## 一、模块概述

数据处理模块（Processor）是一个独立的、解耦的数据转换层，负责在设备原始数据和平台标准化数据之间进行转换。通过 Lua 脚本引擎实现灵活的协议适配。

**核心职责**：
- 上行数据解码：设备协议数据（字节流）→ 平台标准化数据（JSON）
- 下行数据编码：平台标准化数据（JSON）→ 设备协议数据（字节流）

**设计原则**：
- ✅ 单一职责：只负责数据格式转换
- ✅ 高内聚低耦合：不依赖设备信息、业务规则、存储逻辑
- ✅ 独立可测：可单独测试，不依赖外部服务
- ✅ 性能优先：脚本缓存、超时控制、资源限制

---

## 二、模块位置

```
internal/processor/
├── processor.go          # 核心接口定义
├── script_processor.go   # Lua 脚本处理器实现
├── models.go            # 输入输出结构体定义
├── cache.go             # 脚本缓存管理（Redis）
├── executor.go          # Lua 执行引擎（沙箱 + 超时控制）
├── constants.go         # scriptType 映射常量
└── errors.go            # 错误类型定义
```

---

## 三、核心接口设计

```go
type DataProcessor interface {
    // Decode 上行数据解码：设备协议数据 -> 标准化数据
    // 用于：telemetry、attribute、event
    Decode(ctx context.Context, input *DecodeInput) (*DecodeOutput, error)

    // Encode 下行数据编码：标准化数据 -> 设备协议数据
    // 用于：telemetry_control、attribute_set、command
    Encode(ctx context.Context, input *EncodeInput) (*EncodeOutput, error)
}
```

---

## 四、数据结构定义

### 4.1 输入结构

#### DecodeInput（上行解码输入）
```go
type DecodeInput struct {
    DeviceConfigID string          // 设备配置ID（用于查找脚本）*必填
    Type           DataType         // 数据类型：telemetry/attribute/event *必填
    RawData        []byte           // 原始字节数据 *必填
    Timestamp      int64            // 时间戳（毫秒） 可选
}
```

#### EncodeInput（下行编码输入）
```go
type EncodeInput struct {
    DeviceConfigID string          // 设备配置ID（用于查找脚本）*必填
    Type           DataType         // 数据类型：telemetry_control/attribute_set/command *必填
    Data           json.RawMessage  // 标准化数据（JSON格式） *必填
    Timestamp      int64            // 时间戳（毫秒） 可选
}
```

### 4.2 输出结构

#### DecodeOutput（上行解码输出）
```go
type DecodeOutput struct {
    Success   bool            // 执行是否成功
    Data      json.RawMessage // 标准化后的数据（JSON格式）
    Timestamp int64           // 处理时间戳
    Error     error           // 错误信息（Success=false 时有值）
}
```

#### EncodeOutput（下行编码输出）
```go
type EncodeOutput struct {
    Success     bool   // 执行是否成功
    EncodedData []byte // 编码后的设备协议数据（字节流）
    Error       error  // 错误信息（Success=false 时有值）
}
```

### 4.3 数据类型枚举

```go
type DataType string

const (
    // 上行数据类型
    DataTypeTelemetry  DataType = "telemetry"   // 遥测数据上报
    DataTypeAttribute  DataType = "attribute"   // 属性数据上报
    DataTypeEvent      DataType = "event"       // 事件数据上报

    // 下行数据类型
    DataTypeTelemetryControl DataType = "telemetry_control" // 遥测数据下发
    DataTypeAttributeSet     DataType = "attribute_set"     // 属性设置
    DataTypeCommand          DataType = "command"           // 命令下发
)
```

### 4.4 ScriptType 映射关系

```go
// ScriptType 与 DataType 的映射关系（硬编码）
const (
    ScriptTypeTelemetryUplink      = "A" // 遥测上报预处理 -> telemetry
    ScriptTypeTelemetryDownlink    = "B" // 遥测下发预处理 -> telemetry_control
    ScriptTypeAttributeUplink      = "C" // 属性上报预处理 -> attribute
    ScriptTypeAttributeDownlink    = "D" // 属性下发预处理 -> attribute_set
    ScriptTypeCommand              = "E" // 命令 -> command
    ScriptTypeEvent                = "F" // 事件 -> event
)

// DataType -> ScriptType 映射表
var dataTypeToScriptType = map[DataType]string{
    DataTypeTelemetry:        ScriptTypeTelemetryUplink,
    DataTypeAttribute:        ScriptTypeAttributeUplink,
    DataTypeEvent:            ScriptTypeEvent,
    DataTypeTelemetryControl: ScriptTypeTelemetryDownlink,
    DataTypeAttributeSet:     ScriptTypeAttributeDownlink,
    DataTypeCommand:          ScriptTypeCommand,
}
```

---

## 五、脚本管理

### 5.1 脚本加载流程

```
1. 根据 deviceConfigID + DataType 计算 scriptType
2. 生成缓存 key: {deviceConfigID}_{scriptType}_script
3. 尝试从 Redis 缓存读取
   ├─ 命中 -> 使用缓存脚本
   └─ 未命中 -> 从数据库加载 -> 写入缓存（永久有效）
4. 检查脚本启用状态（enable_flag = 'Y'）
5. 返回脚本内容
```

### 5.2 缓存策略

**缓存 Key 格式**：
```
{deviceConfigID}_{scriptType}_script
```

**缓存内容**：
```go
type CachedScript struct {
    ID          string `json:"id"`
    Content     string `json:"content"`      // 脚本内容
    EnableFlag  string `json:"enable_flag"`  // 启用标识 Y/N
    ScriptType  string `json:"script_type"`  // 脚本类型
}
```

**缓存策略**：
- TTL：永久有效（不设置过期时间）
- 失效时机：脚本更新/删除/禁用时手动清理
- 缓存内容：脚本完整信息（ID、Content、EnableFlag、ScriptType）

### 5.3 脚本执行方式

**说明**：gopher-lua 库不支持真正的预编译和 FunctionProto 缓存，因此采用以下方案：

```go
// 执行流程
1. 从 Redis 缓存加载脚本内容（字符串）
2. 每次执行时创建新的 LState
3. 使用 L.DoString() 加载并执行脚本（gopher-lua 内部会做解析优化）
4. 调用脚本中定义的 encodeInp 函数
5. 返回执行结果

// 性能说明
- DoString 的解析开销可接受（微秒级别）
- 主要开销在脚本逻辑执行，而非解析
- Redis 缓存已经避免了数据库查询开销
```

---

## 六、Lua 执行引擎

### 6.1 沙箱安全限制

**禁用的危险函数**：
```lua
-- 禁用的标准库
os.*          -- 操作系统操作
io.*          -- 文件 IO
package.*     -- 模块加载
dofile()      -- 执行外部文件
loadfile()    -- 加载外部文件
require()     -- 模块引入
```

**允许的函数**：
```lua
-- 基础函数
print, tostring, tonumber, type, pairs, ipairs, next
table.*, string.*, math.*, json.*
-- 自定义函数（如需要）
hex.encode, hex.decode, base64.encode, base64.decode
```

### 6.2 资源限制

```go
const (
    ScriptTimeout     = 3 * time.Second  // 执行超时：3秒
    ScriptMaxMemory   = 50 * 1024 * 1024 // 最大内存：50MB
    ScriptMaxOpsCount = 100000           // 最大操作数：10万次
)
```

### 6.3 执行流程

```
1. 创建新的 LState（独立执行环境）
2. 设置沙箱环境（禁用危险函数）
3. 加载 JSON 库（L.PreloadModule）
4. 设置超时控制（context.WithTimeout）
5. 在协程中执行脚本：
   a. 使用 L.DoString() 加载脚本
   b. 调用 encodeInp(msg, topic) 函数（topic 传空字符串兼容旧脚本）
   c. 获取返回值
6. 等待执行完成或超时
7. 关闭 LState
8. 记录执行日志（耗时、成功/失败）
```

**脚本函数签名（兼容性说明）**：
```lua
-- 旧版脚本（保持兼容）
function encodeInp(msg, topic)
    -- topic 参数会收到空字符串 ""
    -- 可以忽略 topic 参数
    return processedData
end

-- 新版脚本（推荐）
function encodeInp(msg)
    -- 也可以只定义一个参数
    -- Lua 会自动忽略多余的传参
    return processedData
end
```

---

## 七、错误处理

### 7.1 错误类型定义

```go
const (
    ErrCodeScriptNotFound      = "SCRIPT_NOT_FOUND"       // 脚本不存在
    ErrCodeScriptDisabled      = "SCRIPT_DISABLED"        // 脚本未启用
    ErrCodeScriptExecuteFailed = "SCRIPT_EXEC_FAILED"     // 脚本执行失败
    ErrCodeScriptTimeout       = "SCRIPT_TIMEOUT"         // 脚本执行超时
    ErrCodeInvalidInput        = "INVALID_INPUT"          // 输入参数无效
    ErrCodeCacheError          = "CACHE_ERROR"            // 缓存操作失败
    ErrCodeDatabaseError       = "DATABASE_ERROR"         // 数据库查询失败
)
```

### 7.2 错误处理策略

```go
// 脚本不存在或未启用
-> 返回 Success=false, Error=ErrScriptNotFound/ErrScriptDisabled
-> 调用方决定是否透传原始数据

// 脚本执行失败
-> 返回 Success=false, Error=ErrScriptExecuteFailed（包含详细错误信息）
-> 记录错误日志
-> 不透传数据（避免错误数据入库）

// 脚本执行超时
-> 中断执行，返回 Success=false, Error=ErrScriptTimeout
-> 记录告警日志
```

---

## 八、日志记录

### 8.1 日志级别

```go
// INFO 级别
- 脚本加载成功
- 脚本执行成功（记录耗时）

// WARN 级别
- 脚本不存在（deviceConfigID + scriptType）
- 脚本未启用

// ERROR 级别
- 脚本执行失败（记录错误详情）
- 脚本执行超时
- 缓存/数据库操作失败
```

### 8.2 日志格式

```go
logrus.WithFields(logrus.Fields{
    "module":          "processor",
    "device_config_id": input.DeviceConfigID,
    "data_type":       input.Type,
    "script_type":     scriptType,
    "duration_ms":     duration.Milliseconds(),
    "success":         output.Success,
}).Info("script executed")
```

---

## 九、模块不关心的内容

数据处理模块**不需要知道**：
- ❌ 数据来自哪个协议（MQTT/CoAP/TCP/Kafka）
- ❌ 设备的详细信息（device_id、product_id、名称等）
- ❌ 业务规则（告警阈值、规则引擎、场景联动）
- ❌ 存储方式（时序数据库/关系数据库/文件）
- ❌ 下游处理流程（数据入库、消息推送、日志记录）

数据处理模块**只需要知道**：
- ✅ 设备配置ID（deviceConfigID）
- ✅ 数据类型和方向（DataType）
- ✅ 原始数据/标准化数据（rawData/jsonData）

---

## 十、调用示例

### 10.1 上行数据解码（Decode）

```go
// 场景：MQTT 接收到设备遥测数据
processor := NewScriptProcessor()

input := &DecodeInput{
    DeviceConfigID: "config_123",
    Type:           DataTypeTelemetry,
    RawData:        []byte{0x01, 0x02, 0x03, 0x04}, // 设备原始数据
    Timestamp:      time.Now().UnixMilli(),
}

output, err := processor.Decode(ctx, input)
if err != nil {
    log.Error("decode failed:", err)
    return
}

if !output.Success {
    log.Error("script execution failed:", output.Error)
    return
}

// output.Data = {"temperature": 25.5, "humidity": 60}
// 后续：数据入库、触发规则引擎
```

### 10.2 下行数据编码（Encode）

```go
// 场景：平台下发属性设置指令
processor := NewScriptProcessor()

input := &EncodeInput{
    DeviceConfigID: "config_123",
    Type:           DataTypeAttributeSet,
    Data:           json.RawMessage(`{"led_status": "on", "brightness": 80}`),
    Timestamp:      time.Now().UnixMilli(),
}

output, err := processor.Encode(ctx, input)
if err != nil {
    log.Error("encode failed:", err)
    return
}

if !output.Success {
    log.Error("script execution failed:", output.Error)
    return
}

// output.EncodedData = []byte{0x05, 0x01, 0x50, ...}
// 后续：通过 MQTT 发送给设备
```

---

## 十一、性能优化

### 11.1 缓存策略

- **脚本内容缓存**：Redis 永久缓存，手动失效
- **缓存预热**：启动时加载常用脚本（可选，通过 PreloadScripts 方法）
- **缓存命中率**：避免每次数据库查询，大幅提升性能

### 11.2 并发控制

- **无状态设计**：每次执行创建独立的 LState，天然支持并发
- **协程安全**：LState 独立，无需锁机制
- **连接池**：复用 Redis 连接（由 global.REDIS 管理）

### 11.3 资源隔离

- **超时控制**：context.WithTimeout（3秒），防止脚本卡死
- **沙箱隔离**：禁用危险函数，防止恶意脚本
- **独立虚拟机**：每次执行独立的 LState，脚本间完全隔离

### 11.4 性能说明

**脚本执行开销分析**：
```
总耗时 = 缓存查询(ms) + 脚本解析(μs) + 脚本执行(ms)

- 缓存查询：Redis 本地访问 < 1ms
- 脚本解析：DoString 解析 < 100μs（微秒级）
- 脚本执行：取决于脚本复杂度，一般 < 10ms

预期单次处理耗时：< 20ms（P99）
```

**为什么不做预编译**：
- gopher-lua 不支持导出 FunctionProto
- DoString 解析开销极小（微秒级）
- 脚本执行才是主要开销，预编译收益有限
- Redis 缓存已经避免了数据库查询开销

---

## 十二、后续扩展方向（暂不实现）

- ⏳ **降级策略**：脚本失败时透传原始数据
- ⏳ **批量处理**：同一设备多条数据批量执行
- ⏳ **性能指标**：集成到 pkg/metrics 模块（执行耗时、成功率等）
- ⏳ **脚本热更新**：监听数据库变更，自动刷新缓存
- ⏳ **多语言支持**：除 Lua 外支持 JavaScript/Python
- ⏳ **脚本调试工具**：在线调试、断点、变量查看
- ⏳ **脚本版本管理**：支持脚本回滚、灰度发布

---

## 十三、测试计划

### 13.1 单元测试

- ✅ 脚本加载逻辑（缓存命中/未命中）
- ✅ Decode/Encode 正常流程
- ✅ 错误处理（脚本不存在、执行失败、超时）
- ✅ 沙箱安全（禁用函数调用测试）

### 13.2 性能测试

- ✅ 单次执行耗时（目标：<10ms）
- ✅ 并发执行（1000 并发，无死锁）
- ✅ 内存占用（长时间运行无泄漏）

### 13.3 集成测试

- ⏳ 与 MQTT 模块集成（后续实现）
- ⏳ 与 Kafka 模块集成（后续实现）

---

## 十四、开发计划

### Phase 1：核心功能（优先级：P0）✅ 已完成
- [x] 定义核心接口和数据结构（processor.go, models.go）
- [x] 实现 Lua 执行引擎（executor.go）
- [x] 实现脚本缓存管理（cache.go）
- [x] 实现 ScriptProcessor（script_processor.go）
- [x] 错误类型定义（errors.go）
- [x] 常量和类型映射（constants.go）

### Phase 2：安全和优化（优先级：P1）✅ 已完成
- [x] 沙箱安全限制
- [x] 超时控制（3秒）
- [x] 日志记录（INFO/WARN/ERROR）
- [x] 参数验证
- [x] 脚本兼容性处理（topic 参数传空字符串）

### Phase 3：测试（优先级：P1）⏳ 暂不实现
- [ ] 单元测试
- [ ] 性能测试

### Phase 4：集成（优先级：P2）⏳ 待实现
- [ ] 与 MQTT 模块集成
- [ ] 与现有 service 层对接
- [ ] 替换旧的脚本执行逻辑

---

## 附录：参考资料

- 现有实现：`internal/service/data_script.go`
- 脚本工具：`pkg/utils/script.go`
- 数据库表：`data_scripts`
- 缓存命名：`initialize/GetScriptByDeviceAndScriptType()`
