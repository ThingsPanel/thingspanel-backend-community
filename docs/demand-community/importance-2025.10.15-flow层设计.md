# Flow 层设计（2025.10 更新）

## 背景与目标
- 旧版 MQTT 订阅函数体量过大（>200 行）且直连业务逻辑，难以扩展其他协议或复用处理链路。
- 新 Flow 层以“适配器 → 总线 → 流程”拆分职责，做到协议无关、业务复用、易监控。

```
MQTT/其他入口
   ↓ Adapter（统一 DeviceMessage）
   ↓ Flow.Bus（按类型分发）
   ↓ Flow.*（业务编排：存储 / 心跳 / 自动化 / SSE）
   ↓ 现有模块（processor、storage、service 等）
```

## 当前架构

```
internal/
├─ adapter/
│   ├─ message.go         // DeviceMessage 定义
│   └─ mqtt_adapter.go    // MQTT 入口，后续可扩展 HTTP/CoAP
│
├─ flow/
│   ├─ bus.go             // 消息总线 + typed channel
│   ├─ telemetry.go       // 遥测管线（脚本 → 存储 → 心跳 → 自动化）
│   ├─ attribute.go       // 属性管线（逻辑与 telemetry 类似）
│   ├─ event.go           // 事件管线
│   ├─ status.go          // 状态管线（心跳/超时模式处理 + SSE）
│   ├─ response.go        // 下行指令/属性设置响应回传
│   └─ flow_manager.go    // 统一管理各 Flow 生命周期
│
├─ processor/             // 数据脚本执行
├─ storage/               // 写入遥测 / 属性 / 事件库
└─ service/               // HeartbeatService、HeartbeatMonitor、Automate 等
```

## 核心组件概览

- **Adapter**  
  - `mqtt_adapter.go` 负责订阅、验设备、封装 `FlowMessage`。Topic 自动识别直连/网关类型，metadata 标记来源。

- **Flow.Bus**  
  - 带缓冲的 channel 总线，按消息类型拆分 `telemetry|attribute|event|status|response`，publish/subscribe 皆非阻塞。
  - 提供 `PublishStatusOffline` 供 HeartbeatMonitor 注入离线事件。

- **Telemetry / Attribute / Event Flow**  
  1. 从缓存取设备信息并执行脚本解析（processor）。  
  2. 写入 storage input channel，复用批处理。  
  3. 刷新心跳：`HeartbeatService.RefreshHeartbeat`（优先 heartbeat，其次 timeout）。  
  4. 若设备原本离线，自动更新状态并通过 SSE、自动化、预期数据补发。  
  5. 透传所需元数据供转发或监控。

- **StatusFlow**  
  - 按配置决定是否接受设备上报或仅处理 `heartbeat_expired`/`timeout_expired`。  
  - 统一执行状态入库 → 缓存清理 → Redis Pub/Sub → SSE → 自动化。  
  - 超时模式上线时写入 TTL，离线保留 key 等待 HeartbeatMonitor 过期。

- **ResponseFlow**  
  - 汇聚 `command` / `attribute_set` 等响应消息，便于后续拓展到通知或审计。

- **FlowManager**  
  - 负责启动 goroutine、连接 bus channel、处理优雅关闭（30s 等待）。

- **HeartbeatMonitor**（service 层）  
  - 订阅 Redis 过期事件，调用 `bus.PublishStatusOffline`，让离线处理复用 StatusFlow。

## 应用集成

- `internal/app/flow.go` 提供 `WithFlowService()` 选项：
  1. 通过 viper 读取 `flow.enable`、`flow.bus_buffer_size`。  
  2. 构造 Bus、各 Flow、FlowManager，并注册为 `Service`。  
  3. 与存储服务、HeartbeatService、ScriptProcessor 解耦，通过依赖注入串联。
- `app.WithHeartbeatMonitor()` 在 FlowBus 启动后挂载 Redis 过期监控，保证离线事件闭环。
- `main.go` 中按顺序注入服务即可完成初始化。

### 配置示例
```yaml
flow:
  enable: true
  bus_buffer_size: 10000
```

## 监控与扩展

- 指标建议：Bus 缓冲占用、Flow 处理耗时/失败、脚本执行次数、自动化触发计数。可复用 `pkg/metrics`。  
- 扩展协议：新增 `adapter/<proto>_adapter.go`，输出 `FlowMessage` 即可。  
- 扩展流程：在 `FlowManagerConfig` 中注入新的 Flow，并在 Bus 中增加对应 typed channel。

## 后续计划
1. 增加 HTTP/WebSocket 适配器，实现多协议入口共用 Flow。  
2. 将 ResponseFlow 输出到审计/通知中心，实现链路闭环。  
3. 引入限流与熔断策略，保护 storage/automation 在极端场景下的稳定性。
