# 北向服务（数据转发）执行台账

日期：2026-10-21（北京时间）
任务书：`docs/NORTHBOUND-SERVICE-ENCORE-IMPLEMENTATION-PLAN-2026-10-21.md`
本文件是**施工台账**，逐条记录 NB-01 至 NB-10 的实际动作、实际命令与证据位置。
凡是未执行的验证，本文件不写“通过”。

---

## 0. 隔离工作区（先做，避免影响其它会话）

| 仓库 | 主工作树 | 主工作树状态（核查时刻） | 本次隔离工作树 | 本次分支 | 基线提交 |
|---|---|---|---|---|---|
| 后端 | `/Users/junhong/Downloads/code/thingspanel-backend-community` | `main` = `0e3ba828`，脏项 247（43 已跟踪修改 + 204 未跟踪） | `/Users/junhong/Downloads/code/nb-workspaces/backend` | `codex/northbound-community-20261021` | `1a1ef0a0` |
| 前端 | `/Users/junhong/Downloads/code/thingspanel-frontend-community` | `master` = `c6968c77`，脏项 37 | `/Users/junhong/Downloads/code/nb-workspaces/frontend` | `codex/northbound-frontend-20261021` | `c6968c77` |
| Kernel（只读来源） | `/Users/junhong/Downloads/code/iot-api-encore-kernel` | `main` = `c981743`，脏项 21 | 不新建 | — | 只读，不写 |

命令：

```bash
git worktree add /Users/junhong/Downloads/code/nb-workspaces/backend \
  -b codex/northbound-community-20261021 1a1ef0a0
git worktree add /Users/junhong/Downloads/code/nb-workspaces/frontend \
  -b codex/northbound-frontend-20261021 c6968c77
```

主工作树未被 reset、clean、stash 或覆盖；Kernel 未被修改、未删除、未新增 worktree。

### 0.1 为什么后端基线取 `1a1ef0a0` 而不是 `main`

核查结论（`git ls-tree` 对比）：

- `main@0e3ba828` 只有 713 个受跟踪文件，`internal/` 下**没有** `brokerledger/`、`brokercontrol/`、`brokerplugin/`、`mosquittodriver/` 等目录，也没有 `internal/uplink/reliable.go`、`internal/storage/telemetry_broker.go`。
- `codex/gmqtt-community-20261019@1a1ef0a0` 有 1154 个受跟踪文件，上述目录与文件**均已提交**。
- 主工作树里那 204 项未跟踪文件正是这份 broker 改造的续作，尚未提交。

北向服务的交接点（可信遥测批次事务、可靠属性/事件事务）只存在于后者。若以 `main` 为基线，NB-04 将没有可挂载的持久边界，只能退化成内存总线转发，与本任务书第 5 节矛盾。因此：

- **后端基线 = `codex/gmqtt-community-20261019@1a1ef0a0`（已提交、可审查）**。
- 依赖关系：合入顺序必须是 broker 改造先入主干，北向随后。这一点写入 §9 阻塞与依赖。
- 未采用“把主工作树 204 项未跟踪文件一起提交”的做法，因为那会把其它会话未完成、未验收的成果混进本分支，无法逐项审查。

### 0.2 已验收物模型来源

| 项 | 值 |
|---|---|
| 分支 | `codex/thingmodel-dev-integration-20261018` |
| 提交 | `37fbb5e`（`fix: preserve empty resource tags in published version reads`） |
| 工作树 | `/Users/junhong/Downloads/code/thingmodel-workspaces/dev-release-20261018/kernel-worktree` |
| 工作树状态 | 干净（`git status --porcelain` 无输出） |
| 相对 `origin/main` | 4 个提交 |
| 相对 Kernel `main` | 落后（Kernel main 已到 `c981743`），因此只取该分支的物模型相关包，不整仓复制 |

---

## 1. 目录职责冻结

| 目录 | 职责 | 数据/事实源归属 |
|---|---|---|
| `encore-api/` | 社区版 Encore 应用（独立 Go module `encore.app`）：物模型、设备元数据、认证适配、授权引擎、**northbound 服务** | Encore 数据库 |
| `encore-api/northbound/` | 北向配置、授权、投递协调、状态查询、插件调用 | `northbound` 库 |
| `encore-api/thingmodel/` | 已验收物模型（只搬不改语义） | `device_metadata` 库 |
| `encore-api/devicemetadata/` | 设备元数据（同上） | `device_metadata` 库 |
| `internal/northbound/` | 社区数据面的**北向源端适配**：事务内登记 Outbox、桥接进程、管理 API 传输代理 | 社区主库 |
| `sql/northbound/` | 社区主库的北向源端 DDL（与 `sql/broker/` 同级、同风格、独立编号） | 社区主库 |
| `plugins/northbound-http/` | 官方 HTTP 示例插件（独立进程、独立 module） | 无 |
| `plugins/northbound-mqtt/` | 官方 MQTT 示例插件（独立进程、独立 module） | 无 |
| `plugins/northbound-sdk-go/` | 可选轻量 Go SDK（只含 wire 类型与 HTTP 客户端，不含队列/权限/重试） | 无 |
| `deploy/northbound/` | 单机编排：Encore 服务、两个插件、Mosquitto、本地接收端 | 无 |
| `scripts/qa/northbound/` | 三个验收入口与夹具 | 无 |
| `docs/northbound/` | 合同、部署、使用、维护、证据 | 无 |

**不新建**：第二套账号、第二套角色权限表、第二套队列、第二套消息账本、第二套物模型、第二个 Broker。

---

## 2. 接口与资源命名冻结

| 项 | 冻结值 |
|---|---|
| Encore 服务名 | `northbound` |
| 管理 API 前缀 | `/northbound/v1/...`（Encore 生成路径） |
| 浏览器入口 | `/api/v1/northbound/*` → Gin 纯传输代理 → Encore（不改写身份、不做本地授权判定） |
| 源端接入 API | `POST /northbound/v1/inbox/events`（仅服务身份） |
| 插件 wire 契约 | `northbound-connector/v1` |
| 插件资源类型 | `northbound-connector/v1`、`northbound-pipeline-template/v1` |
| 插件默认端口 | HTTP 插件 `18081`，MQTT 插件 `18082` |
| 配置键前缀 | `northbound.*`（Gin 侧），Encore 侧用 secrets/env |

---

## 3. 支持输入表（只承诺这些）

只有**已提交**的接入路径才列入。`mqtt.input_profile=ingress_v1` 才有持久接收边界。

| 接入方式 | 数据类型 | 是否有持久边界 | 北向是否支持 | 依据 |
|---|---|---|---|---|
| MQTT（`input_profile=ingress_v1`） | 遥测 `telemetry_sample` | 是（`broker_ingress_receipts` + 同事务批次提交） | 支持 | `internal/storage/telemetry_broker.go:commitTrustedTelemetry` |
| MQTT（`ingress_v1`） | 属性 `attribute` / `gateway_attribute` | 是（receipt 事务内写业务行） | 支持 | `internal/uplink/reliable.go:write` |
| MQTT（`ingress_v1`） | 事件 `event` / `gateway_event` | 是 | 支持 | 同上 |
| MQTT（`ingress_v1`） | 设备响应 `*_response` | 是 | **不支持**（属命令回执，不是北向业务数据） | `reliable.go:response` 分支 |
| MQTT（`input_profile=legacy`，默认） | 任意 | **否**（内存 Bus，ACK 早于持久化） | **不支持**，页面明示 | `internal/uplink/bus.go` |
| HTTP / TCP / 三方云 | 任意 | 否 | **不支持** | 无持久接收实现 |
| 设备上下线、告警、生命周期 | — | 否 | **不支持** | 未证明 source 链路 |

---

## 4. 参数表（首版锁定值）

| 参数 | 值 | 说明 |
|---|---|---|
| 投递语义 | 至少一次 + 明确失败 + 受控重放 | 不承诺端到端 exactly-once |
| 保证起点 | 事件进入源端 Outbox 之后 | 页面与文档必须这样写 |
| 默认保留期 | 源端 Outbox 7 天；Encore 事件 7 天；投递账 30 天 | 可配置 |
| 单租户源端积压上限 | 50,000 行 / 64 MiB | 超限**拒绝登记**并计入拒绝账 |
| 单租户 Encore 待投递上限 | 20,000 行 | 超限拒绝受理 |
| 单批最大事件数 | 200 | 插件 wire |
| 单事件最大载荷 | 256 KiB | 超过直接隔离 |
| 认领租约 | 30 秒 | 过期可被重新认领 |
| 最大尝试次数 | 12 | 之后进死信 |
| 退避 | 1s 起，指数 ×2，上限 5 分钟，±20% 抖动 | 尊重目标 `retry_after` |
| 并发 | 每租户每管道 1 个顺序执行槽；全局 8 个 | 首版保守 |
| 顺序键 | `tenant + pipeline + device` | 同键串行 |
| 时区/时间 | 全部 UTC 存储 | 展示层本地化 |

---

## 5. 台账

### NB-01 基线与施工范围冻结 — 已完成

- [x] 读取任务书与两份原始文档
- [x] 查询项目共享记忆（`.workbuddy-ai/memory/2026-10-20.md`，GMQTT 验收记录）
- [x] 重新核实三个仓库 Git 状态与脏项
- [x] 确认已验收物模型来源提交 `37fbb5e` 及其干净工作树
- [x] 建立隔离工作树，保护其它会话未提交成果
- [x] 冻结目录职责、接口前缀、支持表、参数表
- [x] 验证基线可构建：`go build ./...` 退出码 0
- [x] 验证 Encore 本地运行时可用（探针应用 `/tmp/encore-probe`，`GET /ping` 返回 `{"OK":true}`，含自动建库与迁移）

### NB-02 Encore 核心纳入社区版 — 已完成

**做了什么**：把本期必需的 Encore 成果搬进社区版仓库，形成一个**自包含的 Go module**（`encore.app`），不依赖 Kernel 路径。

**为什么**：任务书禁止"整仓复制 Kernel"，也禁止把 Kernel 作为长期构建/运行依赖。所以只取北向与管理必需的四个包，其余不取。

**实际改了哪些文件**（新增，共 72 个文件，716K）：

| 路径 | 来源 | 说明 |
|---|---|---|
| `encore-api/go.mod`、`go.sum`、`encore.app` | Kernel `encore-api/` | module 名 `encore.app`，`encore.app` 内容为 `{}` |
| `encore-api/thingmodel/`（含 `core/`、`app/`） | 已验收物模型 `37fbb5e` | 只搬不改语义 |
| `encore-api/devicemetadata/`（含 `migrations/0001…0016`） | 同上 | 库名 `sqldb.Named("device_metadata")` |
| `encore-api/l1-access/authn/` | 同上 | `auth.go`、`community_session.go` |
| `encore-api/internal/authz/engine.go` | 同上，**本任务有修改** | 见下 |
| `tools/legacy-migration/` | 从 `devicemetadata/migrations/scripts/` **移出** | 见"踩过的坑"第 2 条 |

**`internal/authz/engine.go` 的改动**（唯一一处对已验收代码的修改）：

```go
const (
    ActionView   = "view"
    ActionManage = "manage"
    ActionReplay = "replay"
)
// getEnforcer 内追加：
_, _ = e.AddPolicy("tenant_admin",    "northbound", ActionView)
_, _ = e.AddPolicy("tenant_admin",    "northbound", ActionManage)
_, _ = e.AddPolicy("tenant_admin",    "northbound", ActionReplay)
_, _ = e.AddPolicy("tenant_operator", "northbound", ActionView)
_, _ = e.AddPolicy("tenant_viewer",   "northbound", ActionView)
```

不新增角色、不新增权限表，只往既有 Casbin 策略里加资源 `northbound`。

**实际执行的验证**：

```
$ cd encore-api && encore test ./...
thingmodel        PASS
thingmodel/app    PASS
thingmodel/core   PASS
devicemetadata    PASS
errcode           PASS
middleware        PASS
l1-access/authn   PASS
```

**踩过的坑（两条，都是真实报错）**：

1. `encore run` 报 `E0003 parse error: cannot use channel types in Encore schema definitions`。
   原因：`//encore:service` 的 `Service` 结构体里放了 `stop chan struct{}`。Encore 会把 service 结构体当 schema 解析。
   修法：`Service` 改成空结构体，把 `stop`/`stopOnce`/`wg` 移到包级 `runtimeState`，worker 方法接收者改为 `*runtimeState`。
2. `encore test ./...` 在 `devicemetadata/migrations/scripts` 上 `build failed`（`relocation target runtime.exitEncoreG not defined`）。
   原因：migration 目录里放了 `package main` 的可执行脚本，Encore 会把它当包编译。
   修法：移到 `tools/legacy-migration/` 并给独立 `go.mod`（`thingspanel.local/tools/legacy-migration`）。

**限制**：`encore-api/` 是一个**新的独立 module**，社区版根 module 不会 `go build ./...` 到它，需要单独进入目录构建。这是 Encore 的固有形态，已在部署文档写明。

---

### NB-03 接口合同冻结 — 已完成

**做了什么**：写 `docs/northbound/CONTRACT-v1.md`（11 章，冻结），并把合同常量写进代码：

- `encore-api/northbound/types.go`：`ContractVersion = "northbound-connector/v1"`、`SourceContractVersion = "northbound-source/v1"`
- `internal/northbound/contract.go`：社区侧同名常量 + `SubscriptionResponse`

合同覆盖：数据所有权、源端交接信封、幂等与冲突、响应码、插件 wire 6 端点、管理 API 全表 + 权限矩阵、pipeline/delivery 状态机、未知结果与重放、容量与背压、错误码（跨租户一律 404）、兼容演进、一致性测试清单 8 条。

**一个必须写进合同的实现细节**：Encore 的 API 路径**就是声明里的 path**，不会自动加服务名前缀。所以是 `/v1/connectors` 而不是 `/northbound/v1/connectors`。这一点曾导致 404，已修正代理映射与 relay URL。

---

### NB-04 数据交接（源端事务内登记） — 代码完成；交接链路已真实跑通，设备接入跳未跑

**做了什么**：在社区数据面的**两个既有事务边界**里各插一个登记调用，不新建队列、不新建账本。

**改了哪些文件**：

| 文件 | 改动 |
|---|---|
| `sql/northbound/001_source_outbox.sql` | 新增 4 张表：`northbound_source_outbox`（`UNIQUE(tenant_id,event_id)`）、`northbound_source_rejects`、`northbound_source_tenants`、`northbound_source_relay_state` |
| `internal/northbound/config.go` | 配置结构 + `atomic.Pointer` 热更新，**默认 `Enabled=false`** |
| `internal/northbound/enroll.go` | `EnrollTelemetry` / `EnrollBusinessEvent` / `EventIDFor` |
| `internal/northbound/relay.go` | 独立桥接进程：认领 → POST `/v1/inbox/events` → 按逐条 rejected 处理 |
| `internal/northbound/stats.go` | 状态查询 + 过期清理 |
| `internal/northbound/contract.go` | 合同常量 + 订阅响应类型 |
| `internal/app/northbound.go` | **新增**：读配置 + 注册 `northbound-source` 服务（启停 relay） |
| `main.go` | 在 `WithDatabase()`/`WithRedis()` 之后加 `WithNorthboundSource()` |
| `internal/api/northbound_source.go` | 状态接口 + 传输代理 |
| `router/apps/northbound.go`、`router/apps/enter.go`、`router/router_init.go`、`internal/api/enter.go` | 路由与控制器注册 |
| `internal/storage/telemetry_broker.go` | `recordTrustedTelemetry` 加 `ctx`，在 `broker_state_projection_queue` 之前调 `EnrollTelemetry` |
| `internal/uplink/reliable.go` | 在 automation effect 之后、`CheckEffectCapacity` 之前调 `EnrollBusinessEvent` |

**关键设计点（为什么这样做）**：

1. **登记与业务行同事务**：要么业务行和 Outbox 一起提交，要么一起回滚。不存在"业务写成功但转发丢了"的窗口。
2. **事件 ID 由回执派生**，不含时间戳/尝试次数：
   ```go
   EventIDFor(receiptID, deviceID, kind) = uuid.NewSHA1(ns, "northbound-event\x00"+receiptID+"\x00"+deviceID+"\x00"+kind)
   ```
   relay 重发同一个 ID，Encore 侧 `ON CONFLICT DO NOTHING` 天然去重。
3. **容量拒绝不返回错误**：超限时 `recordReject(capacity_exceeded)` 后正常返回，绝不因为北向背压把主接入链路弄挂。这就是"已登记等待交接"与"因容量不足未登记"必须分开的落点——前者在 `northbound_source_outbox` 的 `pending`，后者只在 `northbound_source_rejects` 的计数里。
4. **无订阅租户不写行**：先查 `northbound_source_tenants`，没配转发任务的租户一行都不写，避免为全体租户付存储成本。

**实际执行的真实链路验证**（社区后端 `:9998` + 真实 PostgreSQL `:15432` + 真实 Redis `:16379` + 真实 Encore `:4020`）：

准备：把 `sql/northbound/001_source_outbox.sql`、`sql/broker/015_expected_context.sql`、`sql/broker/016_plugin_context.sql` 应用到 `thingspanel_manual` 库；用 `scripts/qa/northbound/conf/community-qa.yml`（`mqtt.input_profile=ingress_v1`、`northbound.enabled=true`）启动社区后端。

```
# 1) 源端状态接口（真实读库 + 真实拉订阅）
GET http://127.0.0.1:9998/api/v1/northbound-source/status  (x-token: <社区令牌>)
→ {"code":200,"data":{
     "enabled":true,"sourceProfile":"ingress_v1","encoreConfigured":true,"schemaReady":true,
     "outbox":{"pending":0,"leased":0,"handedOff":0,"failed":0},
     "rejects":{"capacity":0,"invalid":0},
     "subscribedTenants":1}}

# 2) relay 从 Encore 拉到的订阅缓存（真实写入社区库）
SELECT tenant_id, active_pipelines FROM northbound_source_tenants;
→ t1 | 2

# 3) 写入一条 Outbox 记录（等价于事务内登记提交后的状态），等 relay 认领
INSERT INTO northbound_source_outbox (...) VALUES (..., 'relay-e2e-001', ...);

# 4) 6 秒后
SELECT event_id, state, attempts, handed_off_at FROM northbound_source_outbox WHERE event_id='relay-e2e-001';
→ relay-e2e-001 | handed_off | 1 | 2026-10-21 09:09:40.153959+00
SELECT last_success_at, last_error FROM northbound_source_relay_state;
→ 2026-10-21 09:09:40.156558+00 | (空)

# 5) Encore 侧确实收到并逐任务分发（真实库）
SELECT event_id, tenant_id, device_id, payload FROM nb_events WHERE event_id='relay-e2e-001';
→ relay-e2e-001 | t1 | dev-relay-01 | {"humidity": 0.55, "temperature": 26.5}
SELECT p.name, d.state, d.attempts FROM nb_deliveries d JOIN nb_pipelines p ON p.id=d.pipeline_id
 WHERE d.event_id='relay-e2e-001';
→ 部分成功验收任务 | delivered | 1
→ 温度转发到 MQTT   | delivered | 1
```

一个事件命中两个任务，两条投递各自独立结算、各自确认——这正是"不能一个目标成功就清掉其他目标的待办"。

**本轮在这一段发现并修掉的真实缺陷（3 个）**：

1. **relay 认领语句把整数塞进 text 参数。**
   `now() + (? || ' seconds')::interval`，gorm/pgx 把 `30` 当 text 编码，运行期报
   `unable to encode 30 into text format for text (OID 25): cannot find encode plan`。
   relay 因此**一条都交接不出去**，而且只在真库上才暴露。
   修法：改成 `now() + make_interval(secs => ?)`；`retry` 里的
   `(? || ' milliseconds')::interval` 同样改成 `make_interval(secs => ?)`。
2. **`claim` 把 `failed` 也当成可认领状态。**
   `state IN ('pending','failed')` 会让"尝试次数耗尽 → failed"的行被无限重新认领，
   `MaxAttempts` 形同虚设。修法：只认领 `pending` 和租约过期的 `leased`。
3. **北向源端没接到应用启动流程。** `northbound.Configure` / `relay.Start` 全仓没有任何调用点，
   功能等于没装。修法：新增 `internal/app/northbound.go` 与 `WithNorthboundSource()`，
   在 `WithDatabase()` 之后注册，`main.go` 同步接线；未配置地址时打 warn 而不是静默空转。

**还有一处接线缺陷（代理头不匹配）**：前端用 `x-token`，Encore 的认证处理器读 `Authorization`。
原代理"原样转发 Authorization"等于把浏览器令牌丢了，代理必然 401。
修法：代理把调用方的 `x-token`（没有则用 `Authorization`）翻译成 `Authorization: Bearer <调用方令牌>`，
再转发；不签发、不提权，转发完删掉 `x-token` 与 `Cookie`。

**尚未执行（不写成通过）**：**设备 → Broker → 厂商 Mosquitto 插件 → ingress_v1** 这一跳没有跑。
原因：该插件产物（`.so`）不在本仓库，`internal/brokerplugin/` 只有 `route.go`，重建它超出本任务范围。
因此本次验证覆盖的是"登记之后"的完整链路（relay → Encore → 投递），
**不覆盖**"设备报文如何变成可信回执"这一段；后者属于已验收的 broker 改造，不是本期新增代码。

---

### NB-05 配置与调度（Encore 侧） — 已完成，真实调用验证通过

**做了什么**：11 张表 + 管理 API + 投递 worker。

**`encore-api/northbound/migrations/0001_init.up.sql`**（11 张表）：

`nb_connector_installations`、`nb_connections`、`nb_connection_secrets`、`nb_pipelines`、`nb_pipeline_versions`、`nb_events`、`nb_deliveries`、`nb_delivery_attempts`、`nb_tenant_quota`、`nb_pipeline_quota`、`nb_runtime_facts`。

其中两条约束是全部正确性的地基：

```sql
CONSTRAINT nb_delivery_branch UNIQUE (pipeline_id, event_id, branch)
CREATE INDEX nb_deliveries_claim ON nb_deliveries (next_attempt_at)
    WHERE state IN ('pending','retry_wait');
```

**真实调用证据**（Encore `:4020` + HTTP 插件 `:18081` + 本地接收端 `:18090`）：

```
POST /v1/connections
→ 201 {"id":"63833895-...","connectorName":"HTTP 数据出口","secretFieldsSet":["authorization"],"revision":1}

POST /v1/connections/63833895-.../test
→ {"ok":true,"state":"active","checks":[
     {"name":"connector","ok":true,"message":"HTTP 数据出口 1.0.0"},
     {"name":"config","ok":true,"message":"配置可用"},
     {"name":"destination","ok":true,"message":"connection opened"}]}

POST /v1/pipelines
→ 201 {"state":"draft","revision":1,"activeVersion":null}

POST /v1/pipelines/94775221-.../preview
→ {"matched":true,
   "output":{"humidity":62,"site":"acceptance-lab","temp_c":23.5},
   "idempotencyKey":"nb1:91b44e71...",
   "warnings":[]}

POST /v1/pipelines/94775221-.../activate
→ 200 {"state":"active","activeVersion":1}

POST /v1/pipelines/94775221-.../test-send
→ {"accepted":true,"deliveryId":"62b21363-...","state":"pending"}
```

预览证明三件事同时生效：`include` 把 `battery`/`rssi` 滤掉、`rename` 把 `temperature` 变 `temp_c`、`unitConvert` 把 `0.62` 的 ratio 换成 `62`。

**真实送达证据**（本地接收端 `/tmp/nb-qa/received.jsonl` 原文）：

```json
{"method":"POST","path":"/ingest",
 "headers":{"idempotency-key":"nb1:8dde6150..."},
 "body":"{\"deliveryId\":\"62b21363-...\",\"eventId\":\"test-02d3f706-...\",
          \"idempotencyKey\":\"nb1:8dde6150...\",\"eventType\":\"telemetry\",
          \"subject\":\"devices/dev-thermo-01\",\"occurredAt\":\"2026-10-22T00:20:00+08:00\",
          \"deviceId\":\"dev-thermo-01\",
          \"data\":{\"humidity\":62,\"site\":\"acceptance-lab\",\"temp_c\":23.5},
          \"serialization\":{\"format\":\"json\"}}"}
```

账本侧：`state=delivered`、`ack_level=destination_acked`、`attempts=1`、`target_receipt={"body":"{\"ok\": true}","status":200}`。

**这里发现并修掉了两个真实缺陷（只在真库/真插件下才暴露）**：

1. **`order_key` 含 NUL 字节 → 插入必失败。**
   `orderKey := tenantID + "\x00" + pipelineID + "\x00" + env.DeviceID`，而 PostgreSQL 的 `text` 列不允许 `0x00`。表现是 `test-send` 返回 `unavailable: delivery could not be recorded`，且错误被吞掉，看不出原因。
   修法：分隔符改成 `\x1f`（`inbox.go`），并**同时**给事件插入与投递插入加上 `rlog.Error`，把真实 DB 错误和 tenant/pipeline/event 标识写进服务日志。只修前者不修后者，线上还是查不出来。
2. **投递时没套用任务的 transform。**
   `loadDeliveryContext` 直接取了 `nb_events.payload` 原样发送，`transform` 只在 fanout 时算了一遍就丢掉。结果：预览显示 `{"humidity":62,"site":...,"temp_c":23.5}`，真实发出去却是 `{"rssi":-67,"battery":88,"humidity":0.62,"temperature":23.5}`。这会让"配置生效版本"完全失效。
   修法：`loadDeliveryContext` 联查 `nb_pipeline_versions`，取**投递行钉住的那个 version** 的 `transform` 并在发送前套用。语义因此变成：编辑任务只影响之后新建的记录，已入队的记录保持它被匹配时的形状。

---

### NB-06 调度、重试、未知结果 — 六个场景已真实验证

全部在真实组件上跑：Encore `:4020`、HTTP 插件 `:18081`、MQTT 插件 `:18082`、本地接收端 `:18090`、Mosquitto `:18830`。

| 场景 | 怎么造 | 真实结果 |
|---|---|---|
| 重试与指数退避 | MQTT 插件修好前，发布持续失败 | 同一条投递 `attempts` 涨到 9 次仍在 `retry_wait`，`failure_code=publish_failed`；`nb_delivery_attempts` 逐次留痕 |
| 目标确认分级 | HTTP 200 / MQTT QoS1 | HTTP `target_receipt={"status":200,"body":"{\"ok\": true}"}`；MQTT `{"qos":1,"note":"Broker 已确认接收","topic":"..."}`，`ack_level=destination_acked` |
| **未知结果** | 接收端 `/control` 切 `slow`，延迟 5s > 插件 3s 超时 | `state=unknown`、`attempts=1`、`duration_ms=3003`；**没有盲重试** |
| **未知结果的重放门槛** | 对上面那条做重放 | 只给 `reason` → `409 aborted`「confirm the risk or use a connector with strong idempotency」；加 `acknowledgeRisk:true` → `200 pending` → 随后 `delivered` |
| **暂停后不再新增** | 暂停 HTTP 任务，再走真实 inbox 入口投事件 | 事件仍被受理 `accepted=1`，但该任务待办数 `4 → 4` 不变；只有未暂停的 MQTT 任务 `fanout=1`。**暂停不丢事件、不补历史** |
| **租约过期自动恢复** | 直接造一条 `state=leased` 且 `lease_until` 已过期 5 分钟的记录 | 4 秒内被重新认领、真实发出，`delivered`、`attempts=2`。无需人工介入 |
| **容量拒绝** | 为租户 `t-cap` 真实造满 20000 条 pending | 新事件被拒 `capacity_exceeded`（`detail: tenant backlog reached 20000 records`）；`nb_events` 中该事件**一行都没有**（不留半写）；`nb_tenant_quota.rejected_total=1` |
| **部分成功** | 接收端加 `failContains:"dev-bad"`，连接 `batch:false`，一批投两个设备 | `partial-ok-001 → delivered`；`partial-bad-001 → retry_wait`（`http_500`，`failure_detail` 是目标真实响应体）。互不牵连 |
| **一事件多任务分别结算** | 同一事件命中两个任务 | 两条投递各自 `delivered`；暂停其中一个不影响另一个 |
| **失败后人工重放** | 目标修好后重放 `retry_wait` 记录 | `200 pending` → `delivered`；`nb_delivery_attempts` 保留 6 次 `retryable_failure` + 1 条 `replayed` + 1 次成功，完整还原经过 |

**本轮因此修掉的缺陷（2 个）**：

1. **`unknown` 结果丢失插件给的原因。** `applyResult` 的 `default` 分支把 `failure_code` 硬编码成 `unknown_status`、`detail` 写成 `redact(r.Status)`，插件给的 `ErrorCode`/`ErrorDetail` 全被丢掉，运维只看到 `unknown_status / unknown`。
   修法：显式加 `case OutcomeUnknown`，并让 `default` 也优先采用插件的 `ErrorCode`/`ErrorDetail`。
2. **冲突返回 400 而不是 409。** 见 NB-03 的合同修订记录。

**尚未跑**：保留期清理（`pruneExpired`）的定时触发只做了代码审查，没有等到 7/30 天窗口；一致性测试的 5 条（结果条数不匹配、超时分类、QoS0、幂等键稳定、插件重启）留到 NB-08 脚本化。

---

### NB-07 插件真实运行 — 已完成（两个）

**做了什么**：两个**独立进程、独立 module** 的示例插件，都真实跑起来并被 Encore 探活。

| 插件 | 目录 | 端口 | 说明 |
|---|---|---|---|
| HTTP 数据出口 | `plugins/northbound-http/` | 18081 | 用 `plugins/northbound-sdk-go` |
| MQTT 数据出口 | `plugins/northbound-mqtt/` | 18082 | **不用 SDK**，独立实现 wire 合同 |

MQTT 插件故意不复用 SDK，就是为了证明"合同可以被第三方独立实现"——如果只有用我们 SDK 才能接，那这个合同就是假的。

**真实探活证据**（`GET /v1/connectors`）：

```json
{"contractVersion":"northbound-connector/v1","connectors":[
  {"id":"io.thingspanel.sink.http","version":"1.0.0",
   "displayName":"HTTP 数据出口",
   "description":"把选中的数据以 JSON 推送到客户指定的 HTTP 接口，按目标响应判定结果。",
   "capabilities":{"eventTypes":["telemetry","attribute","event"],"batch":true,
                   "ackLevel":"destination_acked","idempotency":"conditional"}},
  {"id":"io.thingspanel.sink.mqtt","version":"1.0.0","displayName":"MQTT 数据出口", ...}]}
```

**插件内部鉴权**（真实验证）：无 `Authorization` → 401；错 token → 401；未配 `NB_PLUGIN_TOKEN` → 进程拒绝启动（fail closed）。

**真实送达**：

- HTTP：接收端 JSONL 原文见 NB-05。
- MQTT：`mosquitto_sub` 在 `:18830` 真实收到
  `acceptance/dev-thermo-01/telemetry {"values":{"humidity":"***","temperature":23.5}}`
  —— `wrapAs` 和 `mask` 都生效了。

**这里也修掉一个真实缺陷**：MQTT 插件把 `json.RawMessage` 直接交给 paho 的 `Publish`，paho 用类型断言只认 `[]byte`/`string`，`json.RawMessage` 是命名类型匹配不上，于是每次发布都失败并报 `unknown payload type`。修法：`payload := []byte(it.Data)`，空载荷兜底 `{}`。**这个缺陷只有接真 Broker 才会暴露，单测和 mock 都发现不了。**

---

### NB-08 一致性测试 — 已完成

**做了什么**：把合同第 11 章的每一条都写成脚本断言，在真实插件进程、真实数据库上跑。
入口是 `scripts/qa/northbound/release.py`，报告在 `docs/northbound/reports/release.md`。

**实际结果**（2026-10-21T10:30:35Z，`release.md` 原文）：

```
结论：通过    断言：24 通过 / 0 失败 / 0 跳过
```

24 条里，A 组代码质量 5 条、B 组插件一致性 15 条、C 组可交付性 4 条。

B 组逐条（报告里的「断言」列，可直接对照）：

| 合同条目 | 断言 | 实际值 |
|---|---|---|
| §11-1 describe 与登记一致 | `HTTP 插件 describe 与合同一致` | `id=io.thingspanel.sink.http contract=northbound-connector/v1` |
| §11-1 | `MQTT 插件 describe 与合同一致` | `id=io.thingspanel.sink.mqtt contract=northbound-connector/v1` |
| §11-2 无令牌必须拒 | `HTTP 插件无令牌时全部拒绝` | 6/6 个端点拒绝 |
| §11-2 | `MQTT 插件无令牌时全部拒绝` | 6/6 个端点拒绝 |
| §11-2 错误令牌必须拒 | `插件拒绝错误令牌` | HTTP 401 |
| §11-3 坏配置 validate 必须拒 | `错误配置被 validate 拒绝` | `ok=False failure=config_invalid config_ok=False` |
| §11-3 且不得发数据 | `校验失败不向目标发送数据` | 命中探针路径 0 次 |
| §11-9 QoS0 不得冒充确认 | `QoS0 记为 none（不冒充已确认）` | `ack_level=none` |
| §11-7 幂等键稳定 | `重复提交后幂等键不变` | 9 条投递的键前后完全一致 |
| §11-7 不重复建账 | `重复提交不新增投递` | 首次 9 条 → 重复提交后 9 条 |
| §11-7 键格式 | `幂等键格式统一为 nb1:` | 全部带 `nb1:` 前缀 |
| §11-7 键不含实例/尝试次数 | `幂等键不含实例或尝试次数` | `nb1:` + 64 位十六进制 |
| §11-6 unknown 不算成功 | `契约声明 unknown 不算成功` | 合同含 unknown 约定 |
| 合同 v1.2 删除规则 | `草稿任务可以删除` | HTTP 200 |
| 合同 v1.2 删除规则 | `跑过的任务拒绝删除` | HTTP 409 `aborted` |

合同 §11-4、§11-5、§11-6、§11-8 由 `recovery.py` 覆盖（部分成功、状态码映射、超时、
租约回收），对应断言名已写进合同 §11 的映射表。

**这一轮抓出两个真实缺陷，都已修掉并留下回归手段：**

1. **投递 worker 空指针崩溃（严重）。**
   代码里是 `var pe *pluginError; if isPluginFailure(err) { kind = pe.kind }`——`pe` 声明
   后从未赋值，只要插件在 `open` 阶段真的失败，就是解引用空指针。
   触发条件极普通：**重启一次插件**。日志证据（`encore run` 崩溃栈）：
   ```
   panic: runtime error: invalid memory address or nil pointer dereference
   encore.app/northbound.deliverBatch(...)  worker.go:167
   ```
   后果是整个北向服务进程退出，不只是这一条投递失败。
   修法：把分类逻辑收成一个 `classifyPluginError(err)`（`catalog.go`），两处调用点都改用它；
   加了 `TestClassifyPluginError`（7 个用例，含 nil、普通 error、包装后的 error）。
   这个测试用旧代码会直接 panic，属于有效回归。

2. **插件在容器里监听 `127.0.0.1`，端口映射打不进去。**
   两个插件的 `http.Server.Addr` 写死 `127.0.0.1:%d`。裸进程验收时完全没问题，
   一进容器就废：Docker 端口发布转发到容器 eth0，监听容器自己的回环从宿主机永远连不上。
   表现很有欺骗性——`docker ps` 显示 Up，日志也说 `listening on 127.0.0.1:18081`，但
   `curl` 返回 000。
   修法：加 `--host` 参数，默认仍是 `127.0.0.1`（裸进程安装更安全），容器里传 `0.0.0.0`。
   实测对比：加参数前 `curl` 000，加参数后无令牌 401、带令牌正确返回 `describe`，
   容器以非 root（65532:65532）运行。

**为什么之前没发现第 1 个**：之前的验收里插件一直在跑，`open` 从不失败；401 场景是插件
在 `write` 阶段返回逐条结果，走不到那条崩溃路径。这次是我为了重启插件换二进制，才把
`open` 失败这条路径走通了。**这类缺陷只有真的把依赖弄挂才会暴露。**

**同时修掉的一处测试自身缺陷**：`重复提交后幂等键不变` 原来取
`SELECT idempotency_key ... LIMIT 1`，一个事件会 fanout 到多个任务、每行键都不同，
`LIMIT 1` 无 `ORDER BY` 取到哪行是随机的，所以这条断言本身会随机失败。改成比较
「每个投递 id 自己的键」前后是否一致，失败时还会指出是哪几条变了。

**限制**：
- 第 8 条用租约过期模拟进程被杀，不是真的 `kill -9`（信号处理与优雅退出没测）。
- 第 7 条只验证平台不重复建账、幂等键稳定送达；接收端只记录不去重，所以「目标侧确实
  没写第二次」依赖接收端实现。
- 插件重启后 `instanceId` 变化不影响幂等，是从键格式上保证的，没做端到端演练。

---

### NB-09 前端页面 — 代码完成；构建未运行

**做了什么**：一个入口页 + 三个面板，注册路由，补齐中英文文案。

**改了哪些文件**（前端工作区 `/Users/junhong/Downloads/code/nb-workspaces/frontend`）：

| 文件 | 内容 |
|---|---|
| `src/views/data-service/northbound/index.vue` | 总览：四个数字卡 + 连接器表 + 本机源端表 |
| `src/views/data-service/northbound/modules/connection-panel.vue` | 目标连接：JSON Schema 驱动表单 + 测试连接 |
| `src/views/data-service/northbound/modules/pipeline-panel.vue` | 转发任务：草稿 / 预览 / 测试发送 / 启停 |
| `src/views/data-service/northbound/modules/delivery-panel.vue` | 投递记录：筛选 / 重放（unknown 需勾风险） |
| `src/service/request/northbound.ts` | 专用 flat request |
| `src/service/api/northbound.ts` | 管理 API 封装 |
| `src/typings/northbound.d.ts` | `declare namespace Northbound` |
| `src/router/elegant/{routes,imports,transform}.ts` | 注册 `data-service_northbound` |
| `src/locales/langs/{zh-cn,en-us}/{route,page,common}.json` | 1 路由 + 1 公共 + 103 页面键 |

**为什么单独写一个 request 实例**：社区前端的 `createFlatRequest` 默认按 `{code,data}`
信封判断成功。北向服务返回的是裸 JSON 加 Encore 标准错误体
（`{"code":"aborted","message":"..."}`），两者语义冲突。复用社区实例会把 409 当成
业务错误吞掉，所以单独建一个只做「HTTP 2xx 即成功」的实例，并在 `onRequest` 里补
`x-token`。这不是绕过统一请求层，是两套后端契约本来就不一样。

**实际执行了哪些验证**：

1. 扫 4 个 `.vue` 里全部 `$t('...')` 引用 → **109 个文案键**，与 `zh-cn` / `en-us`
   合并后的键集比对：**两边都不缺**。
2. `route.data-service_northbound` 中英文都已注册（zh=`数据转发`，en=`Data Forwarding`）。
3. 6 个语言 JSON 全部 `json.load` 通过（不是「看着像 JSON」）。
4. `git diff --stat src/router/` 确认只动了 3 个注册文件，没有误改其它路由。
5. 解析 7 个新增文件里的 **30 条 import**：相对路径与 `@/` 别名 **0 条解析失败**；
   只剩 3 个包引用（`vue`、`naive-ui`、`@sa/axios`），它们需要 `node_modules`
   才能验证，属于第 5 点做不到的部分。

**没做到的，如实写**：`pnpm build` / `vue-tsc --noEmit` **没有跑成**。
`pnpm install` 在本机跑了 **54 分钟**，`node_modules` 停在 92 MB 且无进展（卡在网络
拉取），为避免继续占用资源已中止。所以：

- **类型检查没有执行**，TS 类型错误目前只经过人工核对，没有编译器背书。
- **构建产物没有产出**，没有真实 bundle 可看。

上面的 5 条静态检查能证明「文案齐全、路由注册、JSON 合法、引用都能解析」，**不能**
替代类型检查与构建。这一条必须在有依赖的环境（CI 或联网机器）里补跑，命令就是社区前端
标准的 `pnpm install && pnpm build`。

---

### NB-10 部署、文档与收尾 — 已完成

**做了什么**：把「怎么装、怎么升、怎么退、怎么用、怎么维护」写成能照着做的文档，
并把单机编排、迁移脚本、变更摘要补齐。

**改了哪些文件**：

| 文件 | 内容 |
|---|---|
| `docs/northbound/DEPLOYMENT.md` | 组件与端口（实测值）、环境变量全表、单机部署五步、容器部署、升级（含旧积压行为）、三档回退、上线检查清单、未验证清单 |
| `docs/northbound/USAGE.md` | 入口与权限、五分钟跑通、四个页面逐项说明、三个易混概念、unknown 处置、多任务独立确认、API 速查、常见问题 |
| `docs/northbound/MAINTENANCE.md` | 每天看哪四个数字、日志位置、四类故障定位、保留与清理、调参、重放纪律、备份、巡检 SQL 清单、不要做的操作 |
| `docs/northbound/CHANGES.md` | 变更摘要：合入前置条件、文件清单、依赖但非本次新增的内容、数据库/配置变更、不合入内容、已知限制 |
| `docs/northbound/HANDOVER.md` | 移交说明：环境怎么拉起来、复现验收的命令、没做完的按优先级排、踩过的坑、关键决策及原因 |
| `deploy/northbound/compose.yaml` | 三服务编排（核心 + 两插件），只绑回环 |
| `deploy/northbound/Dockerfile.plugin` | 两插件共用镜像，`PLUGIN` 参数选目录 |
| `deploy/northbound/migrate.sh` | 社区库迁移 + 逐表验证，缺表非零退出 |
| `deploy/northbound/northbound.env.example` | 环境变量模板，逐项注明对应哪条配置 |
| `deploy/northbound/README.md` | 目录说明 + 两个易踩的坑 + 实测边界 |

**为什么 `migrate.sh` 是必需的**：`sql/northbound/001_source_outbox.sql` 在子目录，
而社区后端的自动迁移（`initialize/pg_init.go:231`）只按顺序扫 `sql/<数字>.sql`，
**扫不到子目录**。漏执行的后果不是报错，而是静默不转发——数据照常入库，一条都发不出去，
页面上「等待投递」永远是 0。这种错只能靠脚本堵住。

**实际执行了哪些验证**：

1. `docker compose --env-file <模板> config` → **通过**，渲染结果确认三个服务的镜像、
   端口、环境变量、`depends_on`、外部网络都对。
2. `docker build -f deploy/northbound/Dockerfile.plugin --build-arg PLUGIN=northbound-http`
   → **成功**，镜像 8.07 MB。
3. 容器实跑：`--host 0.0.0.0` 启动后无令牌 → **401**，带令牌 → 正确 `describe`，
   运行用户 `65532:65532`（distroless nonroot）。
4. 文档里所有端口、命令、环境变量都对着实际运行的进程与代码核对过（不是凭记忆写）。
5. 修掉一处前后端不一致：合同 v1.1 写「首版不提供删除任务」，但前端有删除按钮。
   核实后按「草稿可删、跑过的不可删」实现 `DELETE /v1/pipelines/:id`，合同升到 v1.2，
   前端按钮只在草稿行出现，并加了 2 条断言（`草稿任务可以删除`、`跑过的任务拒绝删除`）。

**限制**（完整清单见 `DEPLOYMENT.md` §8）：

- 容器编排整栈 `up` 联调**没跑**。插件镜像与容器内运行已验证，`compose up` 没有。
- Encore 生产镜像**没构建出来**：`encore build docker` 跑了 17 分钟未完成，已中止。
- 前端 `pnpm build` **没跑成**（见 NB-09）。
- 保留期清理**没等真实窗口**，只做代码审查。
- 设备 → Broker → 厂商插件 → `ingress_v1` 这一跳**没测**（`.so` 不在本仓库）。

---

## 6. 三个验收入口（最终状态）

| 入口 | 覆盖 | 最终结果（2026-10-21） |
|---|---|---|
| `scripts/qa/northbound/journey.py` | 端到端主流程 8 段 | **通过**，27 通过 / 0 失败 / 0 跳过 |
| `scripts/qa/northbound/recovery.py` | 8 个故障注入场景 | **通过**，22 通过 / 0 失败 / 0 跳过 |
| `scripts/qa/northbound/release.py` | 代码质量 + 插件一致性 + 可交付性 | **通过**，24 通过 / 0 失败 / 0 跳过 |

三份报告由脚本生成，在 `docs/northbound/reports/`。跳过的断言在报告里单独计数，
**不算通过**。

跑之前需要这些进程在跑（端口见 `DEPLOYMENT.md` §2）：Encore 核心 `:4020`、
HTTP 插件 `:18081`、MQTT 插件 `:18082`、本地接收端 `:18090`、Mosquitto `:18830`、
社区后端 `:9998`、社区库 `:15432`、Redis `:16379`、Broker `:11883`。

> 运维提醒：这三个进程必须用受管后台方式启动。用 `nohup ... &` 起的进程在 shell
> 退出后会被回收，脚本会以「插件全部 502」的形式失败——本次就踩过一次，误以为是
> 代码问题。

## 菜单层级收尾修正

最终交接版本将北向页挂在独立的「数据转发 → 北向数据转发」菜单下，路径为
`/data-forwarding/northbound`；既有「数据服务 → 规则引擎」保留原位。
`sql/northbound/002_data_forwarding_menu.sql` 为动态菜单注册两个角色权限，
由 `deploy/northbound/migrate.sh` 应用并检查。
