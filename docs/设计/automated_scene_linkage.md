# 自动化场景联动设计文档

## 告警模块流程

### 告警配置管理
1. **创建告警配置** - 配置告警名称、等级、通知组、租户等信息
2. **更新告警配置** - 修改告警配置参数，包括启用状态
3. **删除告警配置** - 删除指定告警配置
4. **查询告警配置** - 支持分页查询和条件过滤

### 告警触发流程
1. **告警检测** - 监控设备状态，检测告警触发条件
2. **告警触发** - 调用 `AddAlarmInfo` 或 `AlarmExecute` 方法
3. **配置验证** - 检查告警配置是否存在和启用
4. **通知发送** - 根据配置的通知组ID执行通知
5. **记录保存** - 创建告警信息记录到数据库

### 告警恢复流程
1. **恢复检测** - 监控告警恢复条件
2. **恢复处理** - 调用 `AlarmRecovery` 方法
3. **状态更新** - 将告警状态设置为"N"（正常）
4. **历史记录** - 保存告警恢复历史

## 通知组模块流程

### 通知组管理
1. **创建通知组** - 配置通知类型、状态、通知配置等
2. **更新通知组** - 修改通知组配置信息
3. **删除通知组** - 删除指定通知组
4. **查询通知组** - 支持分页查询和条件过滤

### 通知配置类型
- **EMAIL** - 邮件通知，配置邮件地址列表
- **MEMBER** - 成员通知（待实现）
- **WEBHOOK** - Webhook通知，配置回调URL和签名密钥

## 通知服务配置流程

### 通知服务配置管理
1. **保存配置** - 创建或更新通知服务配置（邮件/短信）
2. **获取配置** - 根据通知类型获取配置信息
3. **测试邮件** - 发送测试邮件验证配置有效性

### 通知执行流程
1. **获取通知组** - 根据通知组ID获取配置
2. **状态检查** - 验证通知组状态是否为"OPEN"
3. **类型分发** - 根据通知类型执行对应的通知方式
4. **邮件发送** - 解析邮件地址列表，逐个发送邮件
5. **Webhook调用** - 构造请求体，发送签名请求
6. **结果记录** - 保存通知历史记录

## 通知历史记录流程

### 记录管理
1. **保存记录** - 每次通知执行后创建历史记录
2. **查询记录** - 支持分页查询通知历史
3. **状态跟踪** - 记录发送时间、内容、目标、结果等

### 记录内容
- 发送时间、内容、目标地址
- 发送结果（成功/失败）
- 通知类型、租户信息
- 错误信息（失败时）

## 自动化联动集成

### 告警触发联动
1. **设备状态监控** - 实时监控设备运行状态
2. **条件匹配** - 设备状态变化触发预设条件
3. **告警执行** - 自动执行告警配置，生成告警信息
4. **通知分发** - 根据告警配置的通知组发送通知
5. **历史记录** - 保存完整的告警和通知执行记录

### 场景自动化流程
1. **场景条件** - 监控预设的场景触发条件
2. **告警集成** - 场景变化可触发相关告警配置
3. **批量处理** - 支持多设备、多告警配置的批量处理
4. **状态同步** - 实时同步告警状态和设备状态

## 核心接口说明

### 告警核心方法
- `AddAlarmInfo` - 基础告警信息添加
- `AlarmExecute` - 完整告警执行流程
- `AlarmRecovery` - 告警恢复处理

### 通知核心方法
- `ExecuteNotification` - 执行通知分发
- `sendEmailMessage` - 邮件通知具体实现
- `SendSignedRequest` - Webhook通知实现

### 数据流转
告警配置 → 触发检测 → 通知组查找 → 通知服务配置 → 消息发送 → 历史记录

## 通知相关模块文件目录

### API层
- `internal/api/alarm.go` - 告警管理接口
- `internal/api/notification_group.go` - 通知组管理接口
- `internal/api/notification_histories.go` - 通知历史记录接口
- `internal/api/notification_services_config.go` - 通知服务配置接口

### 服务层
- `internal/service/alarm.go` - 告警业务逻辑
- `internal/service/notification_groups.go` - 通知组业务逻辑
- `internal/service/notification_history.go` - 通知历史业务逻辑
- `internal/service/notification_services_config.go` - 通知服务配置业务逻辑
- `internal/service/notification_test.go` - 通知功能测试

### 数据访问层
- `internal/dal/alarm.go` - 告警数据访问
- `internal/dal/notification_groups.go` - 通知组数据访问
- `internal/dal/notification_history.go` - 通知历史数据访问
- `internal/dal/notification_services_config.go` - 通知服务配置数据访问
- `internal/dal/latest_device_alarm.go` - 最新设备告警数据访问

### 模型层
- `internal/model/alarm_*.go` - 告警相关数据模型
- `internal/model/notification_*.go` - 通知相关数据模型

### 查询层
- `internal/query/alarm_*.go` - 告警查询生成代码
- `internal/query/notification_*.go` - 通知查询生成代码

### HTTP客户端
- `third_party/others/http_client/request_method.go` - HTTP请求工具，包含webhook发送逻辑

### 路由层
- `router/apps/alarm.go` - 告警路由配置
- `router/apps/notification_*.go` - 通知路由配置

## 问题分析与修复

### 原问题
1. **缺少通知历史记录** - webhook通知执行后没有保存到notification_histories表
2. **错误处理不完整** - 发送失败时只记录日志，未保存失败记录
3. **与邮件通知处理不一致** - 邮件通知有完整的历史记录保存逻辑
4. **缺少超时和重试机制** - webhook发送可能无限等待

### 已实施的修复方案

#### 1. 统一通知历史记录接口
- 新增 `saveNotificationHistory` 统一方法
- 所有通知类型使用相同的历史记录保存逻辑
- 标准化状态值：PENDING、SUCCESS、FAILURE

#### 2. Webhook通知完整重构
- 新增 `sendWebhookMessage` 方法
- 实现先创建PENDING记录，后更新状态的流程
- 添加失败重试机制（重试1次）
- 新增 `SendSignedRequestWithTimeout` 带超时的HTTP请求方法

#### 3. 超时和重试机制
- HTTP请求超时：10秒
- 失败重试：立即重试1次
- 所有错误都进行重试
- 记录最终结果到历史记录

#### 4. 邮件通知优化
- 统一使用 `saveNotificationHistory` 方法
- 标准化日志输出级别为Info
- 保持原有功能不变

### 修复后的流程

#### Webhook通知流程
1. **创建PENDING记录** - 先保存通知历史，状态为PENDING
2. **发送HTTP请求** - 使用10秒超时的带签名请求
3. **处理响应** - 检查HTTP状态码，4xx/5xx视为失败
4. **重试机制** - 失败时立即重试1次
5. **更新最终状态** - 根据最终结果更新为SUCCESS或FAILURE
6. **记录错误信息** - 失败时保存详细错误信息到remark字段

#### 通知历史记录字段
- `send_target`: 存储webhook URL或邮件地址
- `send_content`: 存储完整JSON数据或邮件内容
- `send_result`: PENDING/SUCCESS/FAILURE状态
- `remark`: 错误信息（失败时）
- `notification_type`: WEBHOOK/EMAIL/MEMBER/VOICE

### 新增文件和方法
- `third_party/others/http_client/request_method.go` 新增 `SendSignedRequestWithTimeout`
- `internal/dal/notification_history.go` 新增 `UpdateNotificationHistory` 和 `UpdateNotificationHistoryWithContent`
- `internal/service/notification_services_config.go` 新增统一接口和webhook方法

## 最新重构方案（2025-09-11）

### 接口变更
**原来的调用方式：**
```go
ExecuteNotification(groupID, title, content)
```

**新的调用方式：**
```go
ExecuteNotification(groupID, alertJson)
```

### 告警JSON格式
告警模块现在需要组装完整的JSON数据：
```json
{
  "alert_title": "webhook测试[H]2025-09-11 21:37:42",
  "alert_details": "场景自动化触发告警;设备(webhook测试)遥测 [test_data1]: 2 > 1", 
  "alert_description": "这是告警配置中的描述信息"
}
```

### Unicode转义修复
- 使用 `encoder.SetEscapeHTML(false)` 避免 `>` 变成 `\u003e`
- 确保JSON数据在通知历史中正确显示

### 错误信息增强
失败时在原JSON后追加错误信息：
```json
{"alert_title":"...","alert_details":"...","alert_description":"..."}; Webhook发送失败: connection refused
```

### 需要手动完成的修改
由于文件被频繁修改，需要手动完成以下修改：

1. **添加imports到 `internal/service/alarm.go`：**
```go
import (
    "bytes"
    "strings" 
    // ... 其他imports
)
```

2. **更新 `AlarmExecute` 方法中的通知调用：**
将第294行的：
```go
GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, title, content)
```

改为：
```go
// 构建告警JSON
alertData := map[string]interface{}{
    "alert_title":       title,
    "alert_details":     content,
    "alert_description": alarmConfig.Description,
}

// 序列化JSON，不转义HTML字符
buffer := &bytes.Buffer{}
encoder := json.NewEncoder(buffer)
encoder.SetEscapeHTML(false)
err = encoder.Encode(alertData)
if err != nil {
    logrus.Error("构建告警JSON失败:", err)
} else {
    alertJson := strings.TrimSpace(buffer.String())
    GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, alertJson)
}
```