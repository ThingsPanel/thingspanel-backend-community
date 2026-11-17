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
- 原生物模型与通知核心在本仓库 `encore-api/`，独立通知适配器在 `notification-adapters/`。日常开发与构建不依赖相邻 Kernel 工作区；历史移植来源与未收口内容见 `docs/KERNEL-SOURCE-CONVERGENCE.md`。
- 开发前读 `.ai/_项目开发规范.md`，背景见 `.ai/_项目全局信息.md`、`system_architecture.md`。

## 开发与验证

- 命令在本仓库根目录执行，使用 Windows/PowerShell。
- 按修改范围运行相关包测试；现有候选 CI 包含 `go test ./internal/service ./internal/api ./internal/repo`。
- 全树编译检查可用 `go test ./... -run '^$'`；数据库、Redis、MQTT 等集成验证需单独准备环境，编译通过不代表集成验证通过。
- 沿用现有分层、错误返回与接口契约；不要直接修改其他服务数据库。
- 临时输出按上级工作区 `AGENTS.md` 存放；保留本地配置、凭据及业务文件。

## 本地启动（Windows 社区版）

默认联调方式是 **本地前端 + 线上社区测试机的 Gin v1 和 Encore v2**。下面的端口和接口已在社区测试机 `47.92.253.145` 验证。后续先按本节启动和检查，只有对应检查失败才排查。

1. 在一个 PowerShell 窗口开启 SSH 转发并保持运行。提前通过本机环境变量 `TP_COMMUNITY_SSH_KEY` 指定已授权的测试机私钥文件。首次连接先核对服务器主机指纹。

   ```powershell
   if (-not $env:TP_COMMUNITY_SSH_KEY -or -not (Test-Path -LiteralPath $env:TP_COMMUNITY_SSH_KEY -PathType Leaf)) {
     throw '请将 TP_COMMUNITY_SSH_KEY 指向已授权的测试机私钥文件'
   }
   ssh -i $env:TP_COMMUNITY_SSH_KEY -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 -N `
     -L 127.0.0.1:9999:127.0.0.1:9999 `
     -L 127.0.0.1:14210:127.0.0.1:14000 `
     -L 127.0.0.1:15432:127.0.0.1:5432 `
     -L 127.0.0.1:16379:127.0.0.1:6379 root@47.92.253.145
   ```

   | 本机入口 | 测试机服务 |
   | --- | --- |
   | `127.0.0.1:9999` | Gin v1，测试机端口 `9999` |
   | `127.0.0.1:14210` | Encore v2，测试机端口 `14000` |
   | `127.0.0.1:15432` | PostgreSQL，测试机端口 `5432` |
   | `127.0.0.1:16379` | Redis，测试机端口 `6379` |
   | `47.92.253.145:1883` | Mosquitto MQTT 公网入口 |

2. 在另一个 PowerShell 窗口进入相邻的 `thingspanel-frontend-community` 或当前任务的前端 worktree，运行：

   ```powershell
   pnpm install --frozen-lockfile
   $env:VITE_METADATA_CONNECTOR_URL = 'http://127.0.0.1:9999/api/v1'
   $env:VITE_THINGMODEL_NATIVE_URL = 'http://127.0.0.1:14210'
   pnpm dev --host 127.0.0.1 --strictPort --open false
   ```

   若 Node 22 启动时在 `unplugin-vue-components` 报 `ERR_REQUIRE_CYCLE_MODULE`，在同一个前端目录用下列已验证的临时方式启动。临时 Vite 配置需与原配置相邻才能解析相对导入，不提交临时配置或加载器。

   ```powershell
   $vitePreload = Join-Path $env:TEMP 'tp-community-vite-preload.mjs'
   @'
   import { register } from 'node:module'
   register('data:text/javascript,' + encodeURIComponent(`
     export async function resolve(specifier, context, next) {
       if (specifier.includes('/unplugin-vue-components/dist/vite.js')) {
         const cjs = specifier.replace(/vite[.]js$/, 'vite.cjs')
         const source = 'import mod from ' + JSON.stringify(cjs) + '; export default (mod.default ?? mod)'
         return { url: 'data:text/javascript,' + encodeURIComponent(source), shortCircuit: true }
       }
       return next(specifier, context)
     }
   `), import.meta.url)
   '@ | Set-Content -Encoding utf8 -LiteralPath $vitePreload
   Copy-Item -LiteralPath vite.config.ts -Destination vite.local-run.config.mts
   $env:VITE_SERVICE_ENV = 'dev'
   node --import ([uri]$vitePreload).AbsoluteUri ./node_modules/vite/bin/vite.js --config vite.local-run.config.mts --host 127.0.0.1 --strictPort --open false
   ```

3. 打开 `http://127.0.0.1:5002/`。快速检查三个入口：

   ```powershell
   curl.exe -sS -o NUL -w '%{http_code}\n' http://127.0.0.1:5002/
   curl.exe -sS -o NUL -w '%{http_code}\n' http://127.0.0.1:5002/api/v1/auth/login-options
   curl.exe -sS -o NUL -w '%{http_code}\n' http://127.0.0.1:5002/api/v2/devices
   ```

   预期依次为 `200`、`200`、`401`（未登录时）。登录后对设备的操作写入线上社区测试环境。

验证边界：PostgreSQL 和 Redis 已用部署配置中的凭据通过 SSH 隧道做过只读查询和 `PING`。凭据从授权的部署配置获取，不写入仓库。服务端当前使用 **Mosquitto**，旧配置中的 `mqtt_server: gmqtt` 不能作为实际 broker 类型依据。MQTT `1883` 可达，但用独立 Client ID 的认证尚未通过。Gin 后端已编译，完整本地后端未跑通。需要运行第二套后端时，先准备隔离的 MQTT 身份、订阅和运行数据环境，避免与现有部署重复消费或执行定时任务。

## 测试范围与效率

- 根据本次改动和风险选择最少且有效的验证，优先相关包、受影响路径和关键业务边界。
- 文档、注释、轻微样式等低风险改动以必要检查为主，不新增只复述实现的测试。
- 不运行无关测试，不为凑覆盖率增加测试，不反复运行已通过且未受后续改动影响的检查。
- 涉及数据、权限、接口契约、并发或状态流转时，保留能证明正确性的必要回归；不能以节省 token 为由跳过关键验证。
- 检查通过后继续交付；只有出现新改动、失败或未解决风险时才扩大测试。输出只报告结论与必要错误摘要，减少时间和 token 消耗。

## 工作现场规则

所有会话（人或 Claude/Codex）必须遵守仓库根目录 `CLAUDE.md` 的“工作现场规则”：先读 `docs/PROJECT-AUDIT-2026-11-04.md` 与 `docs/WORKSPACES.md`，只做分配的一个任务；从 `develop` 切 `wp/<任务编号>-<短名>` 并使用 `.claude/worktrees/` 下自己的 worktree；新目录当天登记到 `docs/WORKSPACES.md`；不用 `git stash`；证据不进仓库；不推送、不合并，等负责人确认；删除分支、worktree、目录、Docker 卷前先列清单给用户确认。

## 过程资料与发布可移植性

- 新日志、截图、验收证据写入仓库外 `TP_ARTIFACTS_DIR`，未设置时用工作区同级 `artifacts/`；不新增根目录过程报告。需保留的历史过程说明归档 `docs/archive/` 并维护引用。
- 构建、测试和部署不得依赖个人绝对路径或未跟踪本机文件；外部fixture路径通过明确环境变量传入并校验，缺失时报错或明确跳过，不猜测私人目录。
- 运行凭据、实例身份、数据库、迁移脚本及回滚资料不是临时产物，不能随缓存清理。
