# 变更摘要：北向服务（数据转发）

给代码审查与合入用。**只列这次实际改了什么**，不重复讲功能背景（那是
`CONTRACT-v1.md` 和 `USAGE.md` 的事）。

分支：后端 `codex/northbound-community-20261021`（基线 `1a1ef0a0`），
前端 `codex/northbound-frontend-20261021`（基线 `c6968c77`）。

---

## 1. 一句话

在社区版加了一个「数据服务 → 数据转发」入口：源端在既有事务里登记，Encore 侧做
配置与投递账本，两个真实运行的插件（HTTP / MQTT）把数据送到客户系统。**没有新账号
体系、没有新队列、没有重写物模型或 Broker。**

---

## 2. 合入前置条件（重要）

1. **必须先合 broker 改造。** 本分支基线是 `codex/gmqtt-community-20261019@1a1ef0a0`，
   不是 `main`。`main@0e3ba828` 里没有 `internal/brokerledger/`、
   `internal/uplink/reliable.go`、`internal/storage/telemetry_broker.go`，而这些正是
   北向服务挂载持久边界的地方。先合 `main` 会编译不过。
2. **`encore-api/` 整个目录是新增的**，但其中大部分不是本任务写的（见 §4）。
   合入时需要把物模型那批包一起带上，否则 `encore-api` 编不过、北向服务也起不来。

---

## 3. 变更清单

### 3.1 后端新增

| 路径 | 作用 |
|---|---|
| `encore-api/northbound/*.go`（16 个文件） | Encore 服务：连接、任务、版本、事件、投递账本、调度 worker、插件客户端、秘密加解密、订阅刷新、总览 |
| `encore-api/northbound/migrations/0001_init.up.sql` | 北向库建表（9 张表 + 索引） |
| `encore-api/northbound/northbound_test.go` | 纯逻辑单测（幂等键、投递 ID、匹配、变换、单位换算、退避、错误分类） |
| `internal/northbound/{config,contract,enroll,relay,stats}.go` | 社区侧源端：配置、事件信封、事务内登记、relay 交接、状态统计 |
| `internal/api/northbound_source.go` | 状态接口 + 纯传输代理（`x-token` → `Bearer`） |
| `internal/app/northbound.go` | 接线：读配置、注册 relay 服务（**没有这个文件功能等于没装**） |
| `router/apps/northbound.go` | `/api/v1/northbound/*` 与 `/api/v1/northbound-source/status` |
| `sql/northbound/001_source_outbox.sql` | 社区库：源端 Outbox + 拒绝账 + 订阅缓存 + relay 状态 |
| `plugins/northbound-sdk-go/` | 插件 SDK（`contract.go` + `server.go`） |
| `plugins/northbound-http/` | HTTP 出口插件（真实运行，已送达验证） |
| `plugins/northbound-mqtt/` | MQTT 出口插件（真实 Broker 验证） |
| `plugins/.gitignore` | 不提交插件二进制与构建临时文件 |
| `scripts/qa/northbound/{lib,receiver,journey,recovery,release}.py` | 三个验收入口 + 本地接收端 + 共用库 |
| `scripts/qa/northbound/conf/community-qa.yml` | 验收用社区配置（`ingress_v1` + `northbound.*`） |
| `deploy/northbound/`（5 个文件） | 单机编排、插件 Dockerfile、迁移脚本、env 模板、README |
| `docs/northbound/`（7 个文档 + 3 份报告） | 移交、合同、台账、部署、使用、维护、变更摘要，以及脚本生成的 journey/recovery/release 报告 |

### 3.2 后端修改（6 个文件，逐个说明为什么）

| 文件 | 改动 | 为什么 |
|---|---|---|
| `main.go` | 加 `app.WithNorthboundSource()` | 不接线的话 `northbound.Configure` 和 `relay.Start` 全仓没有调用点，功能装了但不跑 |
| `internal/api/enter.go` | 注册 `NorthboundSourceApi` | 同上，路由要能拿到 controller |
| `router/apps/enter.go` | 挂 `Northbound` 到 apps 组 | 同上 |
| `router/router_init.go` | 初始化北向路由 | 同上 |
| `internal/storage/telemetry_broker.go` | 在可信遥测批次提交的同一事务里登记北向事件 | 这是「已登记」的唯一保证点：事务回滚则事件不登记，事务提交则事件一定可见 |
| `internal/uplink/reliable.go` | 在可靠属性/事件写入的同一事务里登记 | 同上，覆盖非遥测的两类数据 |
| `.gitignore` | 忽略 `*.go.[0-9]*`、`*-go-tmp-umask` | `go build` 与编辑器会留零字节/编号副本，不挡住就会误提交 |

### 3.3 前端新增

| 路径 | 作用 |
|---|---|
| `src/views/data-service/northbound/index.vue` | 总览页（四个数字 + 连接器 + 本机源端） |
| `src/views/data-service/northbound/modules/connection-panel.vue` | 目标连接（JSON Schema 驱动表单） |
| `src/views/data-service/northbound/modules/pipeline-panel.vue` | 转发任务（草稿 / 预览 / 测试发送 / 启停） |
| `src/views/data-service/northbound/modules/delivery-panel.vue` | 投递记录（筛选 / 重放） |
| `src/service/request/northbound.ts` | 专用 flat request（北向服务返回裸 JSON，不是社区 `{code,data}` 信封） |
| `src/service/api/northbound.ts` | 管理 API 封装 |
| `src/typings/northbound.d.ts` | 类型定义 |

### 3.4 前端修改

| 文件 | 改动 |
|---|---|
| `src/router/elegant/{routes,imports,transform}.ts` | 注册 `data-forwarding_northbound` → `/data-forwarding/northbound` |
| `src/locales/langs/{zh-cn,en-us}/{route,page,common}.json` | 1 个路由键 + 1 个公共键 + 103 个页面键，中英文都补齐 |

---

## 4. 依赖但**不是本次新增**的内容

`encore-api/` 目录整体在 git 里是新增的，但其中下面这些包来自已验收的物模型成果
（`codex/thingmodel-dev-integration-20261018@37fbb5e`），**不是这次写的**：

```
encore-api/devicemetadata/     （设备元数据、物模型、迁移）
encore-api/thingmodel/         （物模型核心、应用层）
encore-api/l1-access/authn/    （社区会话鉴权适配）
encore-api/internal/authz/     （Casbin 授权引擎）
encore-api/encore.app, go.mod, go.sum, .gitignore
```

北向服务用到其中两样：`authn`（令牌校验，租户身份只从这里取）和 `authz`（角色→动作
判定）。**没有重写它们，也没有绕过它们。**

合入时这批包必须一起进，否则 `encore-api` 编不过。

---

## 5. 数据库变更

### 5.1 社区主库（新增 4 张表，不改任何既有表）

`sql/northbound/001_source_outbox.sql`：

| 表 | 作用 |
|---|---|
| `northbound_source_outbox` | 已登记、等待交接的事件 |
| `northbound_source_rejects` | 因容量/格式被拒、**从未登记**的事件计数（按分钟分桶） |
| `northbound_source_tenants` | 「哪些租户有活动任务」的缓存 |
| `northbound_source_relay_state` | 最近成功/最近错误，供页面显示 relay 是否在跑 |

**注意**：这个文件在 `sql/northbound/` 子目录，**不在** `initialize/pg_init.go` 的
自动执行范围（那个只扫 `sql/<数字>.sql`）。必须手工执行或用
`deploy/northbound/migrate.sh`。漏执行的后果是静默不转发，不是报错。

### 5.2 北向库（Encore 托管，自动迁移）

`encore-api/northbound/migrations/0001_init.up.sql`：9 张表，见文件头注释。
`encore run` / 生产部署时自动执行。

---

## 6. 配置变更

### 6.1 社区后端新增 `northbound` 段

默认 `enabled: false`，**不开不影响任何既有功能**。字段见
`docs/northbound/DEPLOYMENT.md` §2.3。

### 6.2 社区后端 `mqtt` 段要求（不是新增，但北向依赖）

```yaml
input_profile: ingress_v1
broker_cell: <必填>
broker_instance: <必填>
retained_profile: live-ingress   # 合法值 live-ingress / platform / wire
```

不是 `ingress_v1` 时页面会显示「无持久边界」警告，且不产生投递。**这是如实告知，
不是故障。**

### 6.3 Encore 环境变量

`NB_SERVICE_TOKEN`、`NB_SECRET_KEY`、`NB_HTTP/MQTT_PLUGIN_ENDPOINT`、
`NB_HTTP/MQTT_PLUGIN_TOKEN`、容量与保留期若干。全表见 `DEPLOYMENT.md` §2.1。

### 6.4 插件参数

`--port`（默认 18081/18082）、`--host`（默认 127.0.0.1，容器内必须 `0.0.0.0`）、
`--token`（或 `NB_PLUGIN_TOKEN`）。

---

## 7. 不合入的内容

| 路径 | 原因 |
|---|---|
| `tools/legacy-migration/` | 物模型迁移脚手架，属另一条线，**与本变更无关**。它在工作区里是未跟踪状态，不要顺手带进提交 |
| `plugins/*/northbound-plugin-*`、`*-go-tmp-umask`、`*.go.[0-9]*` | 构建产物与临时文件，已由 `.gitignore` 挡住 |

---

## 8. 已知限制

按影响排序，都是**如实记录、没有掩饰**的：

1. **设备 → Broker → 厂商 Mosquitto 插件 → ingress_v1 这一跳没实测。** `.so` 不在本
   仓库（`internal/brokerplugin/` 只有 `route.go`）。这一段属于已验收的 broker 改造，
   不是北向新增，但它是「端到端」缺的最后一块。
2. **容器编排整栈没跑过。** 插件镜像构建 + 容器内运行已实测；`compose up` 联调没有。
3. **Encore 生产镜像没构建出来。** `encore build docker` 本机跑了 17 分钟未完成。
4. **前端没跑 `pnpm build` / `vue-tsc`。** 替代验证：脚本扫描 4 个页面里全部
   `$t()` 引用（109 个键），确认中英文都存在、路由键两边都注册。
5. **保留期清理没等到真实窗口。** 只做了代码审查。
6. **多副本核心没实测。** 租约与 `SKIP LOCKED` 是按多副本写的，只单副本跑过。
7. **任务只有草稿能删。** 跑过的任务不能删，只能停用——这是为了不毁掉投递审计记录。

---

## 9. 验收怎么复核

```bash
cd thingspanel-backend-community

# 三个入口，需要 §6 的进程都在跑
python3 scripts/qa/northbound/journey.py     # 端到端主流程
python3 scripts/qa/northbound/recovery.py    # 8 个故障场景
python3 scripts/qa/northbound/release.py     # 代码质量 + 插件一致性 + 可交付性
```

报告写到 `docs/northbound/reports/{journey,recovery,release}.md`。

**跳过的断言在报告里单独计数，不算通过。** 三份报告是脚本生成的真实输出，不是手写
的。

最终一轮结果：journey 27 条、recovery 22 条、release 25 条，合计 **74 条断言，
0 失败 0 跳过**（2026-10-21T14:09Z）。这一轮是在 Encore 数据库被重建、**库是空的**
情况下跑的，说明两个主流程入口自带夹具、不依赖历史数据。

> `release.py` 的第一步就是探活核心（`核心在线（inbox 可达）`）。核心没起来时这条会先红，
> 别去查数据逻辑——先按 `HANDOVER.md` §2.2 把核心拉起来。
