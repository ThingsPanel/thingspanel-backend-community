# ThingsPanel Backend 目录结构说明

## 项目概述

ThingsPanel Backend 是一个基于 Go 的物联网后端，采用清晰的模块化与分层设计。下列内容帮助快速定位业务代码与支撑模块。

## 根目录结构

```text
thingspanel-backend-community/
├── cmd/                  # 命令行工具与模拟程序
├── configs/              # 配置与密钥
├── docs/                 # 项目文档与开发指南
├── files/                # 静态资源（例：logo）
├── initialize/           # 启动初始化逻辑
├── internal/             # 核心业务实现
├── mqtt/                 # MQTT 客户端与消息流
├── pkg/                  # 公共库与常量
├── router/               # HTTP 路由初始化
├── sql/                  # 数据库建表与迁移脚本
├── static/               # 对外静态页面
├── test/                 # 测试代码
├── third_party/          # 第三方服务封装
├── main.go               # 主程序入口
├── go.mod / go.sum       # Go 模块定义
├── README.md / README_ZH.md
├── LICENSE
└── Dockerfile
```

## 核心目录说明

### cmd/
- `gen/`：GORM 代码生成工具（含 `_REANME.md` 说明）
- `virtual_sensor/`：虚拟传感器示例，包含 MQTT 客户端与脚本

### configs/
- `conf.yml`、`conf-dev.yml`：运行时配置
- `messages*.yaml`：国际化文案
- `rsa_key/`：RSA 公私钥

### docs/
- `API标准化/`：API 规范
- `code_help/`：开发帮助文档（当前文件所在目录）
- `demand-community/`：需求与规划
- `设计/`：设计文档
- `docs.go`、`swagger.json|yaml`：Swagger 相关资产

### initialize/
- `automatecache/`、`croninit/`：自动化与定时任务初始化
- `alarm_cache.go`、`automate_cache.go`：缓存初始化
- `casbin_init.go`、`cron_init.go`、`limiter.go`、`log_init.go`、`pg_init.go`、`redis_init.go`、`rsa_private_key.go`、`viper_init.go`：核心初始化入口
- `test/`：初始化逻辑测试
- `定时任务说明.md`、`缓存说明.md`：模块说明

### internal/
- `adapter/`：MQTT 适配与消息桥接
- `api/`：HTTP 接口处理
- `app/`：应用启动与服务编排
- `command/`：命令处理预留模块
- `dal/`：数据访问层
- `downlink/`：下行消息分发总线
- `flow/`：设备流程与事件处理
- `logic/`：轻量逻辑封装
- `middleware/`：HTTP 中间件
- `model/`：数据模型（含 `.gen.go` 自动生成文件）
- `processor/`：规则与脚本执行器
- `query/`：GORM Gen 查询代码
- `service/`：业务服务层
- `storage/`：遥测与状态存储抽象

### mqtt/
- `device/`、`publish/`、`subscribe/` 等：MQTT 上行、下行与 WebSocket 管理
- `simulation_publish/`：模拟数据发送
- `init_config.go`、`mqtt_config_struct.go`：MQTT 配置

### pkg/
- `common/`：通用工具
- `constant/`、`errcode/`：常量与错误码
- `global/`：全局配置
- `metrics/`：监控指标
- `utils/`：辅助方法

### router/
- `apps/`：各模块路由挂载
- `router_init.go`、`sse.go`：HTTP 与 SSE 路由初始化

### sql/
- 序号化的 SQL 脚本，用于数据库初始化与迁移

### static/
- `metrics-viewer*.html`：内置监控视图

### test/
- 集成与单元测试样例

### third_party/
- `grpc/`：gRPC 客户端（如 `tptodb_client`）
- `others/`：HTTP 等第三方集成

### 其他
- `.github/`：CI/CD 工作流配置
- `files/`：对外提供的静态资源
- `.cursor/`、`.deepsource.toml` 等开发环境辅助文件

## 架构提示
- 分层结构：API → Service → DAL/Query → Model，保持模块依赖自上而下
- 代码生成：模型与查询由 GORM Gen 生成（`.gen.go` 后缀），勿手工修改
- 配置管理：通过 Viper 加载 `configs/`，支持多环境切换

## 相关文档
- [开发规范](./development_standards/)
- [Go 语言规范](./golang_standards.md)
- [依赖管理](./dependency_management.md)
- [AI 辅助开发](./AI_code.md)
