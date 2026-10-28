# ThingsPanel 社区后端仓库导航

本仓库是社区版业务后端，使用 Go、Gin，Go 模块名为 `project`。主要处理设备管理、业务 API、MQTT 消息与数据存储，并对接 Encore 原生物模型及 Horizon 市场服务。

## 目录地图

| 路径 | 用途 |
| --- | --- |
| `main.go`、`internal/app/` | 程序入口、配置加载、服务装配与生命周期 |
| `router/` | HTTP 路由注册，主入口为 `router_init.go` |
| `internal/api/` | 接口参数处理与响应 |
| `internal/service/`、`internal/logic/` | 业务服务与业务逻辑 |
| `internal/dal/`、`internal/repo/`、`internal/query/`、`internal/model/` | 数据访问、仓储、查询与模型；沿已有调用链确定使用哪层 |
| `internal/middleware/` | 鉴权、响应等中间件 |
| `mqtt/`、`internal/adapter/`、`internal/uplink/`、`internal/processor/` | MQTT 接入、适配与上行处理 |
| `internal/downlink/`、`internal/storage/`、`internal/diagnostics/` | 下行控制、存储及诊断能力 |
| `initialize/` | 数据库、Redis、缓存、定时任务等初始化 |
| `configs/` | 运行配置、消息文本等配置资源 |
| `sql/`、`migrations/` | 初始化及增量 SQL，执行入口需结合启动代码确认 |
| `pkg/`、`third_party/` | 公共工具、常量、全局对象、指标及第三方集成 |
| `cmd/`、`test/` | 辅助命令与自动测试工具；单元测试还分布在业务包的 Go 测试文件中 |
| `files/`、`static/` | 文件资源与静态页面；运行时文件可能包含业务数据 |
| `docs/`、`.ai/`、`.github/workflows/` | 接口/架构文档、项目规范与任务记录、CI 与发布流程 |

## 从哪里开始查

- HTTP 问题沿 `router/` → `internal/api/` → `internal/service/` → 数据访问层追踪。
- 启动与配置从 `main.go` → `internal/app/` 查起；支持 `-config` 指定配置，先确认实际加载文件。
- 设备消息从 `mqtt/` 和 `internal/app/` 的服务装配追踪上行/下行，核对适配器、载荷和存储边界。
- 原生物模型消费者与设备绑定在本仓库，原生模型服务在相邻 `iot-api-encore-kernel/`；跨服务问题需检查双方接口。
- 开发前读 `.ai/_项目开发规范.md`，背景见 `.ai/_项目全局信息.md`、`system_architecture.md`。

## 开发与验证

- 命令在本仓库根目录执行，使用 Windows/PowerShell。
- 按修改范围运行相关包测试；现有候选 CI 包含 `go test ./internal/service ./internal/api ./internal/repo`。
- 全树编译检查可用 `go test ./... -run '^$'`；数据库、Redis、MQTT 等集成验证需单独准备环境，编译通过不代表集成验证通过。
- 沿用现有分层、错误返回与接口契约；不要直接修改其他服务数据库。
- 临时输出按上级工作区 `AGENTS.md` 存放；保留本地配置、凭据及业务文件。

## 测试范围与效率

- 根据本次改动和风险选择最少且有效的验证，优先相关包、受影响路径和关键业务边界。
- 文档、注释、轻微样式等低风险改动以必要检查为主，不新增只复述实现的测试。
- 不运行无关测试，不为凑覆盖率增加测试，不反复运行已通过且未受后续改动影响的检查。
- 涉及数据、权限、接口契约、并发或状态流转时，保留能证明正确性的必要回归；不能以节省 token 为由跳过关键验证。
- 检查通过后继续交付；只有出现新改动、失败或未解决风险时才扩大测试。输出只报告结论与必要错误摘要，减少时间和 token 消耗。
