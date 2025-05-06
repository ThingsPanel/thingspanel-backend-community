# AI辅助开发说明

## 项目结构说明

### 核心目录
- `cmd/` - 应用程序的入口点
- `internal/` - 私有应用程序和库代码
  - `api/` - API 接口层
    - 示例代码：`./internal/apisys_user.go`
  - `model/` - 数据模型定义
    - 示例代码：`./internal/model/user.go`
  - `service/` - 业务服务层
    - 示例代码：`./internal/service/user_service.go`
  - `dal/` - 数据访问层
    - 示例代码：`./internal/dal/user_dal.go`
  - `middleware/` - 中间件
    - 示例代码：`./internal/middleware/auth.go`
  - `query/` - 查询构建器（gen生成）
    - 示例代码：`./internal/query/user_query.go`
  - `app/` - 应用程序特定代码

### 路由层
- `router/` - HTTP 路由配置

### 工具和通用代码
- `pkg/` - 可以被外部应用程序使用的库代码
  - `utils/` - 通用工具函数
  - `metrics/` - 指标收集
  - `global/` - 全局变量和配置
  - `errcode/` - 错误码定义
  - `constant/` - 常量定义
  - `common/` - 通用代码

### 配置和资源
- `configs/` - 配置文件目录
- `static/` - 静态资源文件
- `sql/` - SQL 脚本和迁移文件
- `files/` - 文件存储目录

### 第三方集成
- `third_party/` - 第三方服务集成
- `mqtt/` - MQTT 相关代码

### 测试和文档
- `test/` - 测试文件
- `docs/` - 项目文档

### 初始化
- `initialize/` - 应用程序初始化代码

## 开发指南

### 接口开发流程
1. 在 `router/` 中定义路由
2. 在 `internal/api/` 中实现 API 处理函数
3. 在 `internal/service/` 中实现业务逻辑
4. 在 `internal/model/` 中定义数据模型
5. 在 `internal/dal/` 中实现数据访问

### 代码规范
- 遵循 Go 标准项目布局
- 使用清晰的目录结构组织代码
- 保持代码模块化和可测试性
- 遵循 RESTful API 设计原则