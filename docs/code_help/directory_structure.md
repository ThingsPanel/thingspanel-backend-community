# ThingsPanel Backend 目录结构说明

## 项目概述

ThingsPanel Backend 是一个基于 Go 语言开发的物联网后端系统，采用清晰的分层架构设计。项目遵循标准的 Go 项目布局，使用模块化的设计理念。

## 根目录结构

```
thingspanel-backend-community/
├── cmd/                    # 命令行工具和入口程序
├── configs/                # 配置文件
├── docs/                   # 项目文档
├── files/                  # 静态文件资源
├── initialize/             # 初始化模块
├── internal/               # 内部业务逻辑
├── mqtt/                   # MQTT 相关模块
├── pkg/                    # 公共包和工具库
├── router/                 # 路由配置
├── sql/                    # 数据库脚本
├── static/                 # 静态资源文件
├── test/                   # 测试文件
├── third_party/            # 第三方集成
├── main.go                 # 主程序入口
├── go.mod                  # Go 模块定义
├── go.sum                  # 依赖版本锁定
└── Dockerfile              # Docker 构建文件
```

## 详细目录说明

### `/cmd` - 命令行工具
```
cmd/
├── gen/                    # 代码生成工具
│   ├── main.go            # GORM 代码生成器
│   └── _REANME.md
└── virtual_sensor/         # 虚拟传感器模拟工具
    ├── main.go
    ├── mqtt_client.go
    ├── sensor.sh
    └── temp_hum_sensor.go
```

### `/configs` - 配置文件
```
configs/
├── casbin.conf             # Casbin 权限控制配置
├── conf-dev.yml           # 开发环境配置
├── conf.yml               # 生产环境配置
├── messages.yaml          # 国际化消息配置
├── messages_str.yaml      # 字符串消息配置
└── rsa_key/               # RSA 密钥文件
    ├── private_key.pem
    └── public.pem
```

### `/docs` - 项目文档
```
docs/
├── API标准化/              # API 规范文档
├── code_help/             # 开发帮助文档
│   ├── development_standards/  # 开发规范
│   ├── AI_code.md
│   ├── dependency_management.md
│   ├── directory_structure.md
│   └── golang_standards.md
├── damand/                # 需求文档
├── 设计/                  # 设计文档
├── README-DEV.md          # 开发者说明
├── TODO.md                # 待办事项
├── docs.go                # Swagger 文档生成
├── swagger.json
└── swagger.yaml
```

### `/initialize` - 初始化模块
```
initialize/
├── automatecache/         # 自动化缓存初始化
├── croninit/             # 定时任务初始化
├── test/                 # 初始化测试
├── alarm_cache.go        # 告警缓存初始化
├── automate_cache.go     # 自动化缓存初始化
├── casbin_init.go        # Casbin 权限初始化
├── cron_init.go          # 定时任务初始化
├── limiter.go            # 限流器初始化
├── log_init.go           # 日志初始化
├── pg_init.go            # PostgreSQL 初始化
├── redis_init.go         # Redis 初始化
├── rsa_private_key.go    # RSA 密钥初始化
├── viper_init.go         # 配置初始化
├── 定时任务说明.md
└── 缓存说明.md
```

### `/internal` - 内部业务逻辑（核心模块）
```
internal/
├── api/                   # HTTP API 处理器
├── app/                   # 应用层配置和服务管理
├── dal/                   # 数据访问层（Data Access Layer）
├── logic/                 # 业务逻辑层
├── middleware/            # 中间件
├── model/                 # 数据模型定义
├── query/                 # 数据库查询生成代码
└── service/               # 业务服务层
```

#### `/internal/api` - API 处理器
包含所有 HTTP API 的处理函数，按功能模块划分：
- 设备管理（device.go, device_*.go）
- 告警管理（alarm.go）
- 场景自动化（scene*.go）
- 系统管理（sys_*.go）
- 数据管理（*_data.go）
- 通知管理（notification_*.go）
- OTA 升级（ota.go）
- 等等...

#### `/internal/service` - 业务服务层
包含具体的业务逻辑实现，与 API 层对应。

#### `/internal/dal` - 数据访问层
包含数据库操作相关的代码，每个文件对应一个数据表的操作。

#### `/internal/model` - 数据模型
- `*.gen.go` - GORM 自动生成的模型文件
- `*.http.go` - HTTP 请求/响应结构体
- `*_vo.go` - 视图对象（View Object）

#### `/internal/query` - 查询生成器
GORM Gen 自动生成的查询代码。

### `/mqtt` - MQTT 消息处理
```
mqtt/
├── device/                # 设备状态管理
├── publish/               # 消息发布
├── subscribe/             # 消息订阅处理
├── simulation_publish/    # 模拟数据发布
├── ws_subscribe/          # WebSocket 订阅
├── init_config.go         # MQTT 配置初始化
└── mqtt_config_struct.go  # MQTT 配置结构体
```

### `/pkg` - 公共包
```
pkg/
├── common/                # 公共工具函数
├── constant/              # 常量定义
├── errcode/               # 错误码管理
├── global/                # 全局变量和配置
├── metrics/               # 性能指标收集
└── utils/                 # 工具函数库
```

### `/router` - 路由配置
```
router/
├── apps/                  # 各模块路由配置
├── router_init.go         # 路由初始化
└── sse.go                 # Server-Sent Events 路由
```

### `/sql` - 数据库脚本
包含数据库迁移和初始化脚本，按版本编号排列（1.sql - 11.sql）。

### `/test` - 测试文件
包含单元测试、集成测试和测试工具。

### `/third_party` - 第三方集成
```
third_party/
├── grpc/                  # gRPC 客户端
└── others/                # 其他第三方 HTTP 客户端
```

## 架构设计说明

### 分层架构
项目采用典型的分层架构设计：

1. **API 层** (`/internal/api`) - 处理 HTTP 请求，参数验证
2. **服务层** (`/internal/service`) - 业务逻辑处理
3. **数据访问层** (`/internal/dal`) - 数据库操作
4. **模型层** (`/internal/model`) - 数据结构定义

### 代码生成
- 使用 GORM Gen 自动生成数据库操作代码
- 模型文件和查询文件自动生成，以 `.gen.go` 结尾

### 模块化设计
- 按业务功能模块化组织代码
- 每个模块包含完整的 API、Service、DAL 实现
- 清晰的依赖关系，便于维护和扩展

### 配置管理
- 支持多环境配置（开发/生产）
- 使用 Viper 进行配置管理
- 支持配置热重载

## 开发建议

1. **新增功能**：按照现有的分层结构，在对应目录下添加文件
2. **数据库操作**：优先使用 DAL 层的方法，避免在 Service 层直接写 SQL
3. **错误处理**：使用 `/pkg/errcode` 统一的错误码体系
4. **日志记录**：使用统一的日志格式和级别
5. **测试**：为新功能编写对应的单元测试

## 相关文档

- [开发规范](./development_standards/)
- [Go 语言规范](./golang_standards.md)
- [依赖管理](./dependency_management.md)
- [AI 辅助开发](./AI_code.md)