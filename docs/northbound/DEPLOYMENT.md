# 北向服务（数据转发）部署、升级与回退

本文只写**能照着做、做完能自检**的步骤。凡是本文标「实测」的，都在本机
（macOS / arm64 / OrbStack）真实跑过，命令与端口就是实际用的那一套；标
「未实测」的，本文会直说没跑过以及原因，不会含糊成「应该可以」。

- 组件构成、数据所有权、接口语义见 `CONTRACT-v1.md`
- 逐任务的施工记录与证据位置见 `EXECUTION-LEDGER.md`
- 日常操作（建连接、配任务、看投递）见 `USAGE.md`
- 值班要看什么、出问题怎么定位见 `MAINTENANCE.md`

---

## 1. 组件清单

北向服务不是一个新平台，是**在既有社区部署上加三个进程**：

| # | 组件 | 是什么 | 数据落在哪 |
|---|---|---|---|
| 1 | Encore 核心（`encore-api/`） | 管理面 + 投递底座：连接、任务、版本、事件、投递账本、调度器 | 自己的 PostgreSQL 库 `northbound`（由 Encore 托管，自动迁移） |
| 2 | HTTP 出口插件（`plugins/northbound-http`） | 把投递写进客户 HTTP 接口 | 无状态，不落库 |
| 3 | MQTT 出口插件（`plugins/northbound-mqtt`） | 把投递发布到客户 MQTT Broker | 无状态，不落库 |
| — | 社区后端（既有） | 在既有事务里登记源端事件，并把管理请求代理给 Encore | 社区主库 `northbound_source_outbox` 等 |

**为什么不复用社区账号体系**：北向服务用社区签发的同一枚 `x-token` 做身份，
经社区后端代理转成 `Authorization: Bearer` 后交给 Encore 的 authhandler 校验；
租户身份只从校验过的令牌里取，不从请求体取。没有第二套账号、第二套权限表。

**为什么不复用社区队列**：投递账本是 Encore 库里的 `nb_deliveries`，源端登记是
社区主库里的 `northbound_source_outbox`。两段之间靠 `POST /v1/inbox/events`
交接，不共享表、不共享内存总线。原因见 `CONTRACT-v1.md` §2。

---

## 2. 端口与配置项（实测值）

本机验收环境实际监听如下。生产环境换端口只改配置，不改代码。

| 组件 | 实测监听 | 由谁决定 |
|---|---|---|
| Encore 核心 | `127.0.0.1:4020` | `encore run` 的本地端口 |
| HTTP 出口插件 | `127.0.0.1:18081` | `--port` 或默认值 |
| MQTT 出口插件 | `127.0.0.1:18082` | `--port` 或默认值 |
| 社区后端 | `127.0.0.1:9998` | `service.http.port` |
| 社区 PostgreSQL | `127.0.0.1:15432` | 容器端口映射，库名 `thingspanel_manual` |
| Redis | `127.0.0.1:16379` | 容器端口映射，用 db 1 与 db 10 |
| 社区 MQTT Broker（gmqtt） | `127.0.0.1:11883` | `mqtt.broker` |
| 验收用 Mosquitto | `127.0.0.1:18830` | `mosquitto.conf` 的 `listener` |
| 验收用本地接收端 | `127.0.0.1:18090` | `receiver.py --port` |

### 2.1 Encore 核心的环境变量

秘密只从环境读（`encore secret` 注入的环境变量与本地 `export` 走同一条路径），
不读库、不打日志。

| 变量 | 默认 | 作用 |
|---|---|---|
| `NB_SERVICE_TOKEN` | 空 | 社区运行侧调用 `/v1/inbox/events` 的令牌。**空 = 内部接口关闭（fail closed）**，不是放开 |
| `NB_SECRET_KEY` | 空 | 租户凭证的 AES-256 密钥材料。为空时保存凭证会失败 |
| `NB_HTTP_PLUGIN_ENDPOINT` | `http://127.0.0.1:18081` | HTTP 插件地址 |
| `NB_HTTP_PLUGIN_TOKEN` | 空 | 调 HTTP 插件的令牌 |
| `NB_MQTT_PLUGIN_ENDPOINT` | `http://127.0.0.1:18082` | MQTT 插件地址 |
| `NB_MQTT_PLUGIN_TOKEN` | 空 | 调 MQTT 插件的令牌 |
| `NB_CONNECTORS` | 空 | 整份连接器清单的 JSON。设置后覆盖上面四项。**这不是插件市场**：只接受发布方写死的清单，不接受租户填 URL |
| `NB_EVENT_RETENTION` | `168h` | 事件保留期 |
| `NB_DELIVERY_RETENTION` | `720h` | 投递记录保留期 |
| `NB_MAX_BACKLOG_EVENTS` | `20000` | 积压上限，超过即按容量拒绝 |
| `NB_MAX_BATCH_EVENTS` | `200` | 单次交接最大事件数 |
| `NB_MAX_EVENT_BYTES` | `262144` | 单事件最大字节 |
| `NB_LEASE` | `30s` | 投递租约。进程被杀后，租约到期自动被回收 |
| `NB_MAX_ATTEMPTS` | `12` | 最大尝试次数，超过进死信 |
| `NB_WORKERS` | `8` | 投递并发 |

### 2.2 插件的环境变量 / 参数

两个插件参数一致：

```bash
--port  <端口>            # 默认 18081（HTTP）/ 18082（MQTT）
--host  <监听地址>        # 默认 127.0.0.1；容器里必须改成 0.0.0.0
--token <平台令牌>        # 也可用环境变量 NB_PLUGIN_TOKEN
```

**`--host` 不是可有可无的**：默认只监听回环，裸进程安装时这是对的（不会意外暴露
到网络）。但容器里必须传 `--host 0.0.0.0`——Docker 的端口发布转发到容器的 eth0，
监听容器自己的回环从宿主机永远连不上。这个坑的表现很有欺骗性：`docker ps` 显示
Up，日志也说 `listening on 127.0.0.1:18081`，但从外面就是连不上。本机实测确认过
两种写法的区别（见 §8）。

**令牌为空时插件拒绝启动或拒绝全部请求**，不存在「没配就放开」的分支。

### 2.3 社区后端的配置项

`mqtt` 段（**不改这几项，源端没有持久边界，北向服务不会产生任何投递**）：

```yaml
mqtt:
  input_profile: ingress_v1      # 只有 ingress_v1 被承诺至少一次转发
  broker_cell: community         # ingress_v1 必填
  broker_instance: <实例名>       # ingress_v1 必填，同一 Broker 集群内唯一
  retained_profile: live-ingress # 合法值：live-ingress / platform / wire
```

`northbound` 段（新增）：

```yaml
northbound:
  enabled: true                          # 默认 false。关着的时候登记代码不执行
  encore_base_url: http://127.0.0.1:4020
  service_token: <与 NB_SERVICE_TOKEN 一致>
  source_profile: ingress_v1
  max_backlog_events: 50000
  max_backlog_bytes: 67108864
  max_event_bytes: 262144
  relay_batch: 100
  relay_interval: 1s
  subscribe_interval: 15s
  max_attempts: 20
  retention: 168h
  lease: 30s
```

> `enabled: true` 但 `encore_base_url` 或 `service_token` 为空时，后端只打一条
> 警告并继续启动：数据会留在本地 Outbox 里**等着**，不会被悄悄丢掉。

---

## 3. 单机部署（本机实测的形态）

### 3.1 顺序有依赖，不能跳

```
① 数据库迁移  →  ② Encore 核心  →  ③ 两个插件  →  ④ 社区后端  →  ⑤ 前端
```

②③ 不依赖 ④，但 ④ 启动后会立刻开始向 ② 交接，所以 ② 必须先就绪。

### 3.2 ① 数据库迁移

**社区主库**要两批：

```bash
# a) broker 上下文表。ingress_v1 启动时会检查，缺了会直接起不来：
#    "broker expected context schema is not installed"
psql "$COMMUNITY_DSN" -f sql/broker/015_expected_context.sql
psql "$COMMUNITY_DSN" -f sql/broker/016_plugin_context.sql

# b) 源端 Outbox。这是北向服务唯一落在社区库里的表。
#    注意：它在 sql/northbound/ 子目录，不在 initialize/pg_init.go 的
#    自动执行范围（那个只扫 sql/<数字>.sql），所以必须手工执行或走
#    deploy/northbound/migrate.sh。
psql "$COMMUNITY_DSN" -f sql/northbound/001_source_outbox.sql
```

脚本是 `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`，重复执行安全。

**Encore 库不用手工建**：`sqldb.NewDatabase("northbound", {Migrations: "./migrations"})`
在 `encore run` 启动时自动建库并跑 `encore-api/northbound/migrations/0001_init.up.sql`。

自检：

```bash
psql "$COMMUNITY_DSN" -c "\d northbound_source_outbox"
psql "$COMMUNITY_DSN" -c "\d northbound_source_rejects"
```

两张表都要在。**它们不能合并成一张**：`northbound_source_outbox` 里的是「已登记、
等着交接」，`northbound_source_rejects` 里的是「因容量不足根本没登记」。前者会继续
发送，后者不会补发。把两者混成一个「待发送」数字，就是在骗人。

### 3.3 ② Encore 核心

```bash
cd encore-api

export NB_SERVICE_TOKEN=...            # 与社区后端 northbound.service_token 一致
export NB_SECRET_KEY=...               # 32 字节，openssl rand -hex 32
export NB_HTTP_PLUGIN_ENDPOINT=http://127.0.0.1:18081
export NB_HTTP_PLUGIN_TOKEN=...        # 与插件 --token 一致
export NB_MQTT_PLUGIN_ENDPOINT=http://127.0.0.1:18082
export NB_MQTT_PLUGIN_TOKEN=...

encore run --port 4020
```

启动时会做两件事：注册两个固定官方插件（幂等，不联网探测）、拉起投递 worker。

自检：

```bash
curl -s -H "Authorization: Bearer <任意有效社区令牌>" \
     http://127.0.0.1:4020/v1/overview | head -c 300
```

返回 401 也说明进程活着（说明 authhandler 在工作）。返回连接拒绝才是没起来。

### 3.4 ③ 两个插件

```bash
# 构建（模块缓存已热时约 10 秒）
go build -o /tmp/nb-qa/sink-http ./plugins/northbound-http
go build -o /tmp/nb-qa/sink-mqtt ./plugins/northbound-mqtt

# 启动
/tmp/nb-qa/sink-http --port 18081 --token "$NB_PLUGIN_TOKEN" &
/tmp/nb-qa/sink-mqtt --port 18082 --token "$NB_PLUGIN_TOKEN" &
```

自检（令牌必须与 `--token` 一致）：

```bash
curl -s -H "Authorization: Bearer $NB_PLUGIN_TOKEN" http://127.0.0.1:18081/v1/describe
# {"id":"io.thingspanel.sink.http","contractVersion":"northbound-connector/v1",...}

curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18081/v1/describe
# 401   ← 无令牌必须被拒，这条不通说明鉴权没生效
```

### 3.5 ④ 社区后端

```bash
go build -p 2 -o /tmp/nb-qa/thingspanel-backend .
/tmp/nb-qa/thingspanel-backend -config ./scripts/qa/northbound/conf/community-qa.yml
```

`-p 2` 不是随手加的：默认并发编译在本机 8 GB 内存下会被系统 SIGTERM。

自检：

```bash
curl -s http://127.0.0.1:9998/api/v1/northbound-source/status -H "x-token: $TOKEN"
```

期望能看到 `subscribedTenants`、`waiting`、`handedOff` 三个字段。`waiting` 有数
但一直不下降，说明 ② 没起来或令牌不一致——看后端日志里的
`northbound relay` 相关行。

### 3.6 ⑤ 前端

前端是既有社区前端的增量，不是独立站点。构建与社区前端一致：

```bash
cd thingspanel-frontend-community
pnpm install
pnpm build          # 内部先跑 vue-tsc --noEmit
```

产物与社区前端一样交给现有 Web 服务器。北向页面挂在 `/data-forwarding/northbound`，
菜单名「数据转发」，位于「数据服务」分组下。

前端把请求发到 `${origin}/api/v1/northbound/*`，由社区后端代理到 Encore。**前端
不需要知道 Encore 的地址**，也不需要直连 Encore。

---

## 4. 容器部署

`deploy/northbound/` 提供单机编排。它与社区既有部署**共用一张外部网络**，
不重复创建 PostgreSQL / Redis / Broker —— 那些是社区部署的资产，北向服务只是
往里加表、加进程。

```bash
cd deploy/northbound
cp northbound.env.example northbound.env
$EDITOR northbound.env          # 至少填三个令牌与密钥

docker compose --env-file northbound.env up -d
docker compose --env-file northbound.env ps
```

镜像来源二选一：

```bash
# A) 插件镜像（本目录的 Dockerfile.plugin，按参数构建）
docker compose --env-file northbound.env build northbound-sink-http northbound-sink-mqtt

# B) Encore 核心镜像（Encore 官方方式，不是普通 Dockerfile）
cd ../../encore-api
encore build docker <registry>/northbound-core:<tag> --arch amd64
```

`northbound.env.example` 里每一项都注明了对应第 2 节的哪个变量。

---

## 5. 升级

升级北向服务不动社区业务表，只动自己的账本和进程。

### 5.1 升级前

1. 记录当前状态：`curl .../v1/overview` 存一份，重点看 `pending` / `retry_wait` / `unknown`。
2. 确认没有正在跑的重放：`SELECT count(*) FROM nb_deliveries WHERE state IN ('pending','retry_wait');`
3. 备份 Encore 库（`pg_dump` 库 `northbound`）与社区库的 `northbound_source_outbox`。

### 5.2 升级顺序

```
① 停社区后端的 relay（把 northbound.enabled 改 false 并重启，或直接停后端）
② 升级 Encore 核心 → 自动跑新迁移
③ 升级两个插件
④ 起社区后端（northbound.enabled 改回 true）
```

先停源端再动核心，是为了让「交接」这件事在升级窗口里是静止的：不会有半条事件
卡在 HTTP 请求中间。

### 5.3 升级对旧积压的影响（这是重点）

- **旧积压按原任务版本执行**。投递行上钉着 `pipeline_version`，投递时读的是**那一版**
  的 transform 和序列化配置，不是任务当前设置。升级改了任务配置，已经排队的数据
  仍按当初的规则发出去。
- **幂等键不变**。键是 `nb1:` + 事件/任务/版本/目标/设备的哈希，不含实例 ID、
  不含尝试次数。升级后重试同一个事件，目标侧看到的还是同一个键。
- 插件升级只影响新投递；旧积压重新投递时会用新插件进程，但载荷与幂等键不变。

### 5.4 升级后自检

```bash
python3 scripts/qa/northbound/journey.py     # 端到端
python3 scripts/qa/northbound/release.py     # 代码质量 + 插件一致性 + 可交付性
```

两个脚本都要求对应进程在跑（端口见第 2 节）。跳过项会在报告里明确写成「跳过」，
不会算通过。

---

## 6. 回退

回退分三档，按影响面从小到大。

### 6.1 只停转发（最轻，推荐先做这个）

```yaml
northbound:
  enabled: false
```

重启社区后端。效果：

- 新数据**不再登记**，北向服务收不到新事件。
- 已登记的积压留在 `northbound_source_outbox`，不会丢，重新打开后继续交接。
- Encore 侧已有配置、投递记录全部保留。
- 设备接入、遥测入库、属性下发**完全不受影响**。

这是首选，因为它不动数据、不动表结构、不影响任何既有功能。

### 6.2 回退插件版本

插件是无状态的。把二进制换回上一版、重启进程即可。

需要注意的只有一点：**回退插件不会撤销已经写进第三方的数据**。如果新版本已经
把某些记录发到了客户系统，回退只影响后续投递。

### 6.3 回退 Encore 核心（最重）

1. 停社区后端 relay（同上）。
2. 把 Encore 核心换回上一版镜像/提交。
3. 起核心。**新迁移不回滚**：Encore 的迁移是单向的，回退代码但保留表结构。
   本版本的 `0001_init.up.sql` 只新增表与索引，旧代码在新表结构上可以运行。
4. 起社区后端。

如果新版本引入过破坏性迁移（本版本没有），回退前必须先从备份恢复 Encore 库。

### 6.4 完全移除

```sql
-- 只删北向自己的东西，不碰任何社区业务表
DROP TABLE IF EXISTS northbound_source_rejects;
DROP TABLE IF EXISTS northbound_source_outbox;
```

Encore 库整个 `DROP DATABASE northbound;` 即可——它只装北向服务的账本。

前端删掉路由与页面文件，或保留页面（页面在核心不可达时会显示错误，不影响其它页面）。

---

## 7. 部署检查清单

上生产前逐条打勾，每条都能用命令验证：

- [ ] `sql/broker/015`、`016` 已应用（否则 ingress_v1 起不来）
- [ ] `sql/northbound/001` 已应用，`northbound_source_outbox` 与 `northbound_source_rejects` 两张表都在
- [ ] `NB_SERVICE_TOKEN` 与社区后端 `northbound.service_token` **一致**
- [ ] `NB_SECRET_KEY` 已设置且长度正确（为空则保存凭证会失败）
- [ ] 两个插件的 `--token` 与 `NB_HTTP_PLUGIN_TOKEN` / `NB_MQTT_PLUGIN_TOKEN` **一致**
- [ ] 无令牌访问插件端点返回 401（fail closed）
- [ ] `mqtt.input_profile = ingress_v1` 且 `broker_cell`、`broker_instance`、`retained_profile` 都填了
- [ ] `/api/v1/northbound-source/status` 能看到 `subscribedTenants` 且不为 0
- [ ] `/v1/overview` 里「等待投递」与「因容量未登记」是两个独立数字
- [ ] `journey.py` 与 `release.py` 在目标环境跑过，报告里没有失败项

---

## 8. 本文没验证的部分

如实列出，避免被当成已验证：

| 项 | 状态 | 原因 |
|---|---|---|
| **插件镜像构建与容器内运行** | **已实测** | `docker build -f deploy/northbound/Dockerfile.plugin` 构建成功（8.07 MB，distroless/static:nonroot），容器内 `--host 0.0.0.0` 启动后：无令牌 → 401，带令牌 → 正确 `describe`，运行用户 `65532:65532`。**这一步就是发现 `--host` 缺陷的地方** |
| 容器编排整栈 `docker compose up` | **未实测** | 本机验收用的是裸进程。编排文件按实际端口与变量编写，`docker compose config` 语法校验通过，但没有真的 `up` 起来联调 |
| Encore 核心镜像 | **未实测** | `encore build docker` 在本机跑了 17 分钟未完成（首次构建要拉基础镜像并交叉编译整个 app），为避免占用过多资源已中止。生产镜像请按 §4 的命令在 CI 里构建 |
| 生产形态的 Encore 镜像 + 外部托管库 | **未实测** | 本机用 `encore run` 的本地托管库；生产需要在构建时传 infra 配置指定 `northbound` 库连接 |
| 设备 → Broker → 厂商 Mosquitto 插件 → ingress_v1 这一跳 | **未实测** | `.so` 不在本仓库（`internal/brokerplugin/` 只有 `route.go`），无法在本机拉起；这一段属于已验收的 broker 改造，不是北向服务新增 |
| 保留期清理的定时触发 | **只做了代码审查** | 事件保留 7 天、投递保留 30 天，没有等真实窗口到期 |
| 多副本 Encore 核心 | **未实测** | 投递租约与 `FOR UPDATE SKIP LOCKED` 是按多副本写的，但只在单副本下跑过 |
