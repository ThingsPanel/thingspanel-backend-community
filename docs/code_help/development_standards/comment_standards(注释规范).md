# 注释规范

根据情况注释代码，尽可能通过清晰的代码结构和命名减少注释的需要。

以下三种情况尽可能的标注好。
用于标记待办事项（TODO）、需要修复的缺陷（FIXME）或其他临时解决方案（HACK）。

## 描述

### 1. 概述

- **清晰性**：注释应清晰、简洁、直接。
- **准确性**：注释内容必须准确反映代码的意图和行为。
- **及时更新**：代码修改时，相关注释也应相应更新。

### 2. 代码文档注释

- **包注释**：每个包应有一个包注释，位于包声明之前的一个或多个连续注释中。
- **函数注释**：每个公开（exported）函数都应有一个注释，用来说明函数的功能和用法。
- **类型和变量注释**：公开的结构体、接口、类型别名、全局变量都应有注释。
- **Godoc**：注释应兼容 Godoc 工具，使用完整的句子和适当的标点。

### 3. 特殊注释标签

- **TODO**：标记未来要完成的工作或待优化的代码。
- **FIXME**：指出需要修复或重构的代码部分。
- **HACK**：标识临时解决方案或待改进的代码。
- **DEPRECATED**：用于标记不推荐使用的旧代码或功能。

### 4. 避免的行为

- **冗余注释**：不要对明显的代码添加注释。
- **过时的注释**：确保注释与代码同步更新。
- **注释掉的代码**：避免保留被注释掉的旧代码，除非有特别说明。

### 5. 其他考虑

- **代码自解释原则**：尽可能通过清晰的代码结构和命名减少注释的需要。
- **国际化**：考虑到团队成员的语言习惯，注释可以使用英语或团队内通用的其他语言。
- **特定场景注释**：根据项目或团队的特定需求，可能需要额外的注释规则，如性能说明、安全性说明等。

## godoc使用

- go get golang.org/x/tools/cmd/godoc
- 代码当前路径执行godoc -http=:6060
- 127.0.0.1:6060本地查看

`Godoc` 是 Go 语言的文档生成工具，它从源代码中提取注释来生成文档。为了确保你的 Go 代码能够生成清晰、有用的文档，需要遵循一定的注释规范。

## 注释示例

```go
package services

// 引入必要的包
// 例如: "github.com/some/dependency"

// DeviceService 提供物联网设备相关的服务逻辑。
// 它封装了设备状态获取和配置更新等核心功能。
type DeviceService struct {
    // 这里可以声明服务所需的状态或依赖
    // 例如：数据库连接
}

// NewDeviceService 创建并返回一个新的 DeviceService 实例。
// 此函数初始化必要的依赖和状态。
// TODO: 完成与数据库的集成。
func NewDeviceService() *DeviceService {
    // 返回一个新的 DeviceService 实例
    return &DeviceService{
        // 初始化逻辑，例如设定默认值
    }
}

// GetDeviceStatus 根据设备ID获取其状态。
// 此方法可能会访问数据库或外部API来获取最新的设备信息。
// FIXME: 需要优化设备状态查询的性能。
func (s *DeviceService) GetDeviceStatus(deviceID string) string {
    // 获取指定设备的状态
    // 示例返回值
    return "设备状态: 在线"
}

// UpdateDeviceConfig 根据设备ID和配置信息更新设备配置。
// 这包括配置参数的验证和应用更改。
// HACK: 目前使用一种非标准的配置更新方法，未来可能更改。
func (s *DeviceService) UpdateDeviceConfig(deviceID string, config map[string]interface{}) {
    // 更新设备配置的逻辑
    // 例如：调用API或更新数据库记录
}

// DeprecatedMethod 是一个已被弃用的方法。
// 它包含了旧的逻辑或算法。
// DEPRECATED: 请使用 NewMethod 作为替代。
func (s *DeviceService) DeprecatedMethod() {
    // 旧的处理逻辑
}

// NewMethod 是一个新方法，用于替换 DeprecatedMethod。
// 它提供了改进的逻辑或算法。
func (s *DeviceService) NewMethod() {
    // 新的处理逻辑
}
```
