# R2G 交付说明：`/internal/broker-scope` 改写为 Encore

分支 `wp/R2G-scope-encore`（基于 `develop@789a83e1`）。未推送、未合并。

## 结论：数据谁拥有、端点放哪

- Broker 范围查询用的 `devices` / `device_configs` 是**旧平台库（`platform` 逻辑资源）**的表，列：`is_enabled`、`activate_flag`、`parent_id`、`service_access_id`、`access_way`、`protocol_type`（见 `sql/1.sql`、`sql/2.sql`）。
- `encore-api/devicemetadata` 的 `devices` 是**另一张表**（库 `device_metadata`，列是 `administrative_state`、`activation_state`、`parent_device_id`，没有 `device_configs`、协议类型、接入方式、服务接入）。迁移尚未完成，两者不是同一份事实，不能在 devicemetadata 里诚实地回答。
- 但 `encore-api` 本来就依赖共享包 `pkg/relational`（`go.mod` 的 `replace`），其中 `platform` 是正式登记的逻辑资源，`relational.ResolveTarget(EnvSource, "platform")` + `relational.Open` 直接给出 **GORM** 句柄。所以 Encore 可以只读旧平台库，**不经过老 Gin，不是桥**；没有碰 `pkg/sqldialect`、`encore-api/pkg/dbx`。（没用 `relationaldb.Open`：它在 PG 模式下只接受 Encore 自己声明的库。）
- 因此没有触发“停下来写方案”的条件，按任务书实现。

## 做了什么

- 新增 Encore 服务 `encore-api/brokerscope/service.go`：`//encore:api public raw method=GET path=/internal/broker-scope`。平台库句柄在第一次请求时懒打开（平台库没配时不影响 Encore 启动），`Shutdown` 关闭连接池。`public` 仅表示 Encore 网关可达；前端 nginx 只转发明确的 API 前缀，没有转发 `/internal/*`，所以仍不对外（本次未改任何 nginx/compose 文件）。
- 新增无 Encore 依赖的 `encore-api/internal/brokerscope/brokerscope.go`：`Query`（与原 Gin 版逐字相同的 GORM 条件）和 `Handler`（鉴权 + 输出）。
- 行为保持：JSON 形状 `{"targets":[{id,tenant_id}],"plugins":[...]}`；`Authorization: Bearer $GOTP_BROKER_SCOPE_TOKEN`；变量未设或短于 32 → 404；错误/缺失/大小写不符的 token → 401（常量时间比较，鉴权通过前不打开数据库）；库不可用 503；查询失败 500（不泄露错误文本）；`Cache-Control: no-store`；每个列表最多 1000 条；空列表输出 `[]`。
- 删除 `internal/api/broker_scope.go`、`internal/api/broker_scope_test.go` 及 `router/router_init.go` 里的 `api.RegisterBrokerScopeRoutes`。Gin `/health` 未动。
- `.github/workflows/encore-image-build.yml` 的 `--services` 清单加入 `brokerscope`（否则镜像里没有这个端点）。
- 登记 `docs/WORKSPACES.md`；范围外发现写入 `docs/TODO.md`；`docs/tasks/R2-DONE.md` 里“沿用 Gin 路线”的说明标注为已被取代。

## 部署需要的改动（我没有改安装器，按下面改）

1. **URL**：Encore 容器不是 `backend:9999`。把安装器本地配置里 `scope_sync.url` 改为 Encore 服务的地址，路径不变，例如 `http://thingmodel-api:4000/internal/broker-scope`（主机名/端口按实际 Encore 容器；Encore 默认监听 4000，请核对）。`agent/system-config.example.json` 在 `wp-r1-system-role`、`wp-r2-restart` 两个 worktree 里都**没有** `scope_sync` 段，示例文件无需改；`scope_sync.go` 只要求 `http://` 前缀，不用改代码。
2. **Encore 容器环境变量**：
   - `GOTP_BROKER_SCOPE_TOKEN=<与 Agent 同一个 ≥32 字符的 token>`（原来配在 Gin 容器上，现在要配在 Encore 容器上，Gin 容器上可删）；
   - 平台库连接：`TP_RELATIONAL_POSTGRES_PLATFORM_DSN=postgres://…/ThingsPanel?sslmode=disable`（或 `…_PLATFORM_HOST/PORT/DATABASE/USERNAME/PASSWORD`）。MySQL 发行选项用 `TP_RELATIONAL_DRIVER=mysql` 与 `TP_RELATIONAL_MYSQL_PLATFORM_*`。安装器 compose 的 `thingmodel-api` 已有 `TP_LEGACY_DATABASE_URL`，同一个连接串可以复用。建议用只读账号。
3. 镜像需用包含 `brokerscope` 的服务清单重新构建（已改 `encore-image-build.yml`；`deploy/community-test/deploy_encore.py` 的 `native_services` 另见 TODO）。

## 怎么验证的

| 内容 | 命令 | 结果 |
|---|---|---|
| 认证拒绝、404、空数组等，**不连数据库** | `cd encore-api && go test ./internal/brokerscope/` | 通过（涉及数据库的用例因未设 `TEST_PG_SCHEMA_DSN` 而**跳过**，原因写在输出里） |
| 设备组合与 200 响应，**真实 PostgreSQL 16**（一次性 docker 容器，已删除） | `TEST_PG_SCHEMA_DSN=postgres://postgres:…@127.0.0.1:55432/postgres?sslmode=disable go test -count=1 -v ./internal/brokerscope/` | 全部通过：原 Gin 测试的 10 种组合（MQTT、无配置默认 MQTT、Modbus 插件、服务接入、子设备、access_way B、停用、未激活）+ 另一租户配置不会被联接 + 1000 条上限 + 路由 200 头/体 + 空库 `[]` + 连接池关闭时 500 且不泄露文本 + 库不可用 503 |
| 编译 | `cd encore-api && encore check` | **编译成功**（`Compiling application source code done`），运行步骤因本机数据库集群起不来失败（`database did not come up`，本地环境问题，符合预期）。注意：这只证明编译和静态分析通过，没有证明 Encore 进程里端点能响应 |
| Gin 侧 | `go build ./router/ ./internal/api/` | 通过 |

证据：`/Volumes/KIOXIA/archive/evidence/2026-11-12-R2G/`（`go-test-postgres16.txt`、`encore-check.txt`）。

测试夹具用的是最小的两张表（只含查询用到的列，列定义对照 `sql/1.sql`），放在 PG 的临时表里，**不是**完整旧平台 schema。

## 没做 / 没验证

- 没有在运行中的 Encore 进程里请求过 `/internal/broker-scope`（`encore run` 本机起不来）；没有对真实安装器栈跑 Agent 的 `scope_sync` 端到端；raw 端点的路由注册只由 `encore check` 的编译保证。
- MySQL 没有实跑（查询只用 `COALESCE`/`IN`/`LEFT JOIN`，应可移植，但未验证）。
- 没改安装器、没改 nginx/compose；上面三项部署改动需要负责人/安装器会话执行。
- 没有给 Encore 新增去重/限速；Agent 每 20 秒轮询一次的负载与原来相同。
