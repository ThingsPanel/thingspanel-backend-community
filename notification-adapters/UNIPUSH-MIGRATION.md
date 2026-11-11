> 历史迁移记录。当前来源、自动身份和验证边界以 docs/KERNEL-SOURCE-CONVERGENCE.md 为准；不要据此恢复旧手工凭据流程。

# UniPush 插件源码与 Encore 集成

`thingspanel.unipush` 是通知系统的 App 推送通道。Community Encore 通知核心负责策略、成员展开和投递记录；本目录的独立 HTTP v1 插件负责投递至 UniCloud，并在发送前经 Community 私有目录重新验证手机绑定。该插件不另建通知核心，也不是 Encore 应用内注册的服务。

源码从 `ThingsPanel/iot-api-encore-kernel` 的 `main-1.3.0` `fafe0f6f` 移植：`notification-adapters/{go.mod,sdk,mock,unipush,cmd/unipush,deployments/unipush}`。`mock` 仅用于 SDK 测试；新增 `unipush/protocol_integration_test.go` 验证插件 HTTP 协议与模拟的目录及云端交互。原始单 CID 实现提交 `fcd1b31c` 和多设备实现提交 `758a7360` 的 Git 作者均为 `jie <jianghuxiao2@gmail.com>`；GitHub 提交未关联可确认的账号，不能由此判断作者是否为某位同事。

构建与测试（在本目录运行）：

```sh
go test ./...
go vet ./...
go build -o /tmp/unipush-community ./cmd/unipush
```

部署步骤和变量见 `unipush/README.md` 与 `deployments/unipush/.env.example`。插件配置、社区端私有目录授权、App ID、部署 ID 和 UniCloud `push-message-v2` 云函数及其凭据必须匹配。云函数在独立 App 仓库，不包含在本次源码移植中；运行环境需单独核对并部署。不要把真实密钥写进仓库。

测试中本地 HTTP 目录和云端是替身；并未验证线上插件、实际 Encore 进程、App 真机、厂商离线通道或 iOS APNs。迁移源码不代表已经构建生产镜像或部署，已有服务不应因合并源码被中断。

## 通知 worker 联调（2026-11-01）

Community 源码基线为 `3c1220a5847f90b28c2bb6e62cdfe2756f44b5b4`；插件源码来自 Kernel `fafe0f6f`。`encore-api/l3-core/notification/unipush_runtime_integration_test.go` 启动真实的 UniPush 插件进程，调用 notification 的真实 PostgreSQL store、入队与 worker；测试通过插件 v1 的 `/v1/manifest` 和 `/v1/send`，验证一次发往云端的请求和数据库接受状态。成员目录、UniCloud 为回环 HTTP 替身，不发送手机消息。

使用隔离的 `notification_test` PostgreSQL，监听 `127.0.0.1:25543`，用户与数据库均为 `notification_test`。测试会在其中创建并删除独立 schema，勿指向现有业务库。测试从服务仓库根目录运行：

```sh
cd notification-adapters
go build -o /tmp/unipush-community ./cmd/unipush
cd ../encore-api
NOTIFICATION_TEST_DSN='postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test?sslmode=disable' \
UNIPUSH_FIXTURE_BIN=/tmp/unipush-community \
go test -tags notification_cli ./l3-core/notification -run '^TestRealUniPushWorker$' -count=1 -v
```

上面的密码只用于本次隔离 fixture。该命令在 2026-11-01 通过；`notification_cli` 标签让测试通过自身 SQL fixture 初始化 store，它验证真实业务 worker，但不是完整 Encore 应用运行器或 API 登录链路。完整运行器验证应改用同样环境变量执行 `encore test ./l3-core/notification -run '^TestRealUniPushWorker$' -count=1 -v`，以实际结果为准。

本次 `encore test` 先遇到 Go 官方模块代理 EOF，固定依赖使用可用代理下载后继续；随后 Encore 创建测试数据库集群时拉取 `encoredotdev/postgres:18` 失败（Docker Hub EOF）。镜像源下载超时后停止，因此完整 Encore 运行器测试未完成。测试结束后已停止此次单独启动的 PostgreSQL fixture，未操作已有容器。
