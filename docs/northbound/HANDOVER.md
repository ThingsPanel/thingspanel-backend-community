# 移交说明：北向服务（数据转发）

写这份文档的原因很直接：**任务还没 100% 收尾，需要换人接着做。** 所以这份文档不讲
功能背景（那是 `CONTRACT-v1.md` 的事），只讲三件事：

1. 现在到底做到哪一步了
2. 别人怎么在 10 分钟内把环境跑起来、复现验收
3. 还剩什么没做、按什么顺序做

---

## 1. 一句话现状

**代码写完了，三个验收入口在本机真实环境下全部通过（27 + 22 + 25 = 74 条断言，
0 失败 0 跳过），并且在空库冷启动后复跑过一遍。没做完的是三件"需要有环境才能做"的验证：前端构建、Encore 生产镜像、
容器编排联调。**

---

## 2. 五分钟接手：环境怎么拉起来

### 2.1 工作区（不要动主工作树）

| 仓库 | 隔离工作区 | 分支 | 基线 |
|---|---|---|---|
| 后端 | `/Users/junhong/Downloads/code/nb-workspaces/backend` | `codex/northbound-community-20261021` | `1a1ef0a0` |
| 前端 | `/Users/junhong/Downloads/code/nb-workspaces/frontend` | `codex/northbound-frontend-20261021` | `e49f49ac`（`origin/master`） |
| Kernel | `/Users/junhong/Downloads/code/iot-api-encore-kernel` | **只读，不要改** | — |

两个分支都已提交并推送，工作区是干净的：

| 仓库 | 分支 | 关键提交 |
|---|---|---|
| 后端 | `codex/northbound-community-20261021` | `b764b99e` 独立「数据转发」菜单、`aeaa72cc` 验收工具三处修复，以及最后一次 `docs(northbound)` 数字更正 |
| 前端 | `codex/northbound-frontend-20261021` | `47608c3f` 新增页面、`613966d7` 拆出独立「数据转发」菜单 |

> 具体 HEAD 以 `git log -1` 为准，别照抄文档里的哈希——文档自己也会再产生提交。

> 前端基线不要用本地 `master`（`c6968c77`，那是物模型 UI 那条线的分支，不是本分支的
> 祖先）。要算差异就用 `git diff $(git merge-base HEAD origin/master)`，结果是 7 新增 + 9 修改。

主工作树 `/Users/junhong/Downloads/code/thingspanel-backend-community` 里有别的会话
**未提交**的成果，不要 reset / clean / stash 它。

### 2.2 九个进程，缺一不可

全部只绑 `127.0.0.1`。**必须用受管后台方式启动**——用 `nohup ... &` 起的进程在 shell
退出后会被回收，脚本会以「插件全部 502」的形式失败（我踩过一次，白查半小时）。

| # | 进程 | 端口 | 启动命令 |
|---|---|---|---|
| 1 | 社区 PostgreSQL | 15432 | 已由 OrbStack 容器提供（库 `thingspanel_manual`，用户 `tp_manual`） |
| 2 | Redis | 16379 | 同上（db 1 / db 10） |
| 3 | 社区 MQTT Broker（gmqtt） | 11883 | 同上 |
| 4 | Encore 核心 | 4020 | 见下 |
| 5 | HTTP 插件 | 18081 | `/tmp/nb-qa/sink-http --port 18081 --token dev-plugin-token` |
| 6 | MQTT 插件 | 18082 | `/tmp/nb-qa/sink-mqtt --port 18082 --token dev-plugin-token` |
| 7 | 本地接收端 | 18090 | `python3 scripts/qa/northbound/receiver.py --port 18090` |
| 8 | 验收用 Mosquitto | 18830 | `mosquitto -c /tmp/nb-qa/mosquitto.conf` |
| 9 | 社区后端 | 9998 | `/tmp/nb-qa/thingspanel-backend -config ./scripts/qa/northbound/conf/community-qa.yml` |

Encore 核心（第 4 个）：

```bash
cd /Users/junhong/Downloads/code/nb-workspaces/backend/encore-api
GOPROXY=https://goproxy.cn,direct \
TP_AUTH_MODE=dev \
NB_SERVICE_TOKEN=dev-service-token \
NB_SECRET_KEY=dev-secret-key \
NB_HTTP_PLUGIN_TOKEN=dev-plugin-token \
NB_MQTT_PLUGIN_TOKEN=dev-plugin-token \
encore run --port 4020
```

插件二进制如果丢了（`/tmp` 被清过），重新编译：

```bash
cd /Users/junhong/Downloads/code/nb-workspaces/backend
(cd plugins/northbound-http && go build -o /tmp/nb-qa/sink-http .)
(cd plugins/northbound-mqtt && go build -o /tmp/nb-qa/sink-mqtt .)
```

社区后端二进制如果丢了：

```bash
cd /Users/junhong/Downloads/code/nb-workspaces/backend
go build -p 2 -o /tmp/nb-qa/thingspanel-backend .
```

`-p 2` 不是随手加的：默认并发编译在本机内存下会被系统 SIGTERM。

### 2.3 数据库

社区库要三批 SQL（**`sql/northbound/` 在子目录，不会被自动迁移扫到**）：

```bash
psql "$COMMUNITY_DSN" -f sql/broker/015_expected_context.sql
psql "$COMMUNITY_DSN" -f sql/broker/016_plugin_context.sql
psql "$COMMUNITY_DSN" -f sql/northbound/001_source_outbox.sql
# 或者一把梭（带逐表验证）：
COMMUNITY_DSN='postgres://tp_manual:...@127.0.0.1:15432/thingspanel_manual?sslmode=disable' \
  ./deploy/northbound/migrate.sh
```

Encore 库不用管，`encore run` 自动建库跑迁移。

### 2.4 自检：确认真的起来了

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18081/v1/describe   # 期望 401
curl -s -H 'Authorization: Bearer dev-plugin-token' \
     http://127.0.0.1:18081/v1/describe | head -c 80                            # 期望 id=...sink.http
curl -s -H 'Authorization: Bearer dev-plugin-token' \
     http://127.0.0.1:18082/v1/describe | head -c 80                            # 期望 id=...sink.mqtt
```

### 2.5 复现验收（这一步能证明我说的都是真的）

```bash
cd /Users/junhong/Downloads/code/nb-workspaces/backend
python3 scripts/qa/northbound/journey.py     # 约 1 分钟，27 条
python3 scripts/qa/northbound/recovery.py    # 约 1 分钟，22 条
python3 scripts/qa/northbound/release.py     # 约 1.5 分钟，25 条（含 encore test，放在最后）
```

报告写到 `docs/northbound/reports/`。**跳过的断言在报告里单独计数，不算通过。**

> `release.py` 会跑 `go vet` + `encore test` + `go build`，在低配机器上可能超过
> 2 分钟。用后台方式跑，不要用默认 120 秒超时的前台命令（我踩过一次，误以为是 OOM）。

### 2.6 环境会自己挂：Docker 重启 = Encore 核心死

**这是本次交付最后阶段真实发生的一次故障，务必知道。**

`encore run` 的数据库不是一个固定服务，而是 Encore CLI 自己拉起的容器
（`sqldb-bw4dm-default-*`，映射到随机高位端口，这次是 `32768`）。这些容器**没有
restart policy**。所以只要 OrbStack / Docker Desktop 重启，它们会停在 `Exited (255)`，
而其它带 restart policy 的容器会自己起来——**看起来一切正常，只有北向核心死了**。

表现是：

```
northbound: probe query failed: unavailable: FATAL: database not running:
  dial tcp 127.0.0.1:32768: connect: connection refused (SQLSTATE 08006)
```

随后每个请求都是 `unavailable`，`encore run` 进程退出。本次它连续跑了 **3 小时 9 分**
才因为宿主 Docker 重启而倒。

判断方法（30 秒）：

```bash
docker ps -a --filter name=sqldb --format '{{.Names}}\t{{.Status}}'
# 看到 Exited (255) → 就是这个问题
lsof -nP -iTCP:4020 -sTCP:LISTEN   # 端口还在但 curl 返回 000，也指向这里
```

恢复：重新起 `encore run`（见 §2.2 第 4 个进程）。它会重建集群、自动重跑迁移。

**但要注意代价**：重建 = **Encore 侧数据全丢**（连接、任务、版本、事件、投递账本全在
那个容器里）。社区侧的 `northbound_source_outbox` 在 15432 那个持久容器里，不会丢，
但里面的 `handed_off` 记录对应的北向事件已经不存在了。所以：

- 开发/验收环境：直接重跑 `journey.py`，它会自己建连接和任务。
- **不要**把 `encore run` 的本地库当账本用。生产必须给 Encore 配外部托管 PostgreSQL
  （见 `DEPLOYMENT.md`），那才是持久的。

顺带一提：冷启动后 `encore run` 要重新编译整个应用，本机大约 **3～5 分钟**，
期间端口已经 listen 但 HTTP 不响应（`curl` 返回 000）。这不是挂了，别急着杀。

### 2.7 跑完验收先探活，别假设核心还活着

**观察到的现象**：有一次跑完 `release.py` 之后，`:4020` 无监听、`encore run` 进程消失，
但 `sqldb-*` 容器仍然 Up。紧接着再跑 `release.py`，幂等那两条就变成了
`首次 0 条 -> 重复提交后 0 条`——**环境故障被报成了产品缺陷**。

**做了对照实验，不要冤枉 `encore test`**：

```bash
# 测试前 200，跑一次 encore test，测试后仍然 200，PID 没变
$ encore test ./northbound/ -count=1 ; echo $?
0
```

所以 `encore test` **不是**元凶。那次消失最可能的原因是机器当时负载 66，进程被系统
回收；但我没有拿到 OOM 日志，**不写成结论**。

**因此做了两件事**：

1. `release.py` 开头加了一条前置断言 `核心在线（inbox 可达）`。核心没起来时，红在第一行，
   而不是在下游产生一堆看起来像数据 bug 的红。
2. 把 `encore test` 整组挪到脚本最后。它不是元凶，但它是唯一会改动应用运行形态的检查，
   放在最后不影响其它断言。

**接手时请这样用**：

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://127.0.0.1:4020/v1/inbox/events \
  -H 'Content-Type: application/json' -H 'X-Northbound-Token: dev-service-token' \
  -d '{"contractVersion":"northbound-source/v1","sourceProfile":"ingress_v1","events":[]}'
# 200 = 核心正常；000/502 = 需要按 §2.2 第 4 个进程重启
```

---

## 3. 十个任务的实际状态

| 任务 | 状态 | 说明 |
|---|---|---|
| NB-01 基线与范围冻结 | ✅ 完成 | 基线 `1a1ef0a0`（不是 `main`，`main` 缺 broker 改造，编译不过） |
| NB-02 Encore 核心纳入社区版 | ✅ 完成 | `encore-api/` 独立 module，北向服务在里面 |
| NB-03 接口合同冻结 | ✅ 完成 | `CONTRACT-v1.md`，已到 v1.2（两次更正都有修订记录） |
| NB-04 数据交接（事务内登记） | ✅ 完成 | 链路真实跑通；设备接入那一跳未测（见 §4） |
| NB-05 配置与调度 | ✅ 完成 | 连接/任务/版本/预览/启停，真实调用验证过 |
| NB-06 调度、重试、未知结果 | ✅ 完成 | `recovery.py` 8 个场景全过 |
| NB-07 两个插件真实运行 | ✅ 完成 | HTTP 与 MQTT 都真实送达，MQTT 过真实 Broker |
| NB-08 一致性测试 | ✅ 完成 | 合同 §11 每条都有脚本断言，见台账 NB-08 |
| NB-09 前端页面 | ⚠️ **代码完成，构建未跑** | 见 §4 第 1 条 |
| NB-10 部署与文档 | ✅ 完成 | 7 份文档齐备（合同/台账/部署/使用/维护/变更/移交）+ 单机编排 + 迁移脚本 |

详细施工记录（每步做了什么、为什么、改了哪些文件、证据在哪）在
`docs/northbound/EXECUTION-LEDGER.md`。**那份台账是本项目最重要的文档**，接手先读它。

---

## 4. 没做完的（按优先级，这是接手要做的事）

### 第 1 优先：前端构建与类型检查

**为什么没做**：`pnpm install` 在本机跑了 54 分钟，`node_modules` 停在 92 MB 无进展
（卡在网络拉取），为避免继续占用资源已中止。

**要做什么**：

```bash
cd /Users/junhong/Downloads/code/nb-workspaces/frontend
pnpm install
npx vue-tsc --noEmit --skipLibCheck    # 注意：package.json 的 typecheck 脚本用了
                                       # Windows 的 `set NODE_OPTIONS=` 语法，macOS 上会失败
pnpm build
```

**已知的替代验证**（不能替代构建，只能说明「不像明显坏的」）：
- 4 个 `.vue` 里 109 个 `$t()` 键在中英文里都存在
- 7 个新增文件里 30 条 import，相对路径与 `@/` 别名 0 条解析失败
- 6 个语言 JSON 都能 `json.load`
- 路由键 `data-forwarding_northbound` 两边都注册；父菜单为独立「数据转发」组

**风险**：TS 类型错误目前只经过人工核对，没有编译器背书。

### 第 2 优先：Encore 生产镜像

**为什么没做**：`encore build docker tp/northbound-core:dev --arch arm64 --skip-config`
跑了 17 分钟未完成（首次构建要拉基础镜像并交叉编译整个 app），已中止。

**要做什么**：

```bash
cd encore-api
encore build docker <registry>/northbound-core:<tag> --arch amd64 --config <infra 配置>
```

注意：生产**不要**用 `--skip-config`，否则镜像里没有数据库连接信息。

### 第 3 优先：容器编排联调

`deploy/northbound/compose.yaml` 已写好并通过 `docker compose config` 校验，插件镜像
也已真实构建并验证过容器内运行，但**整栈 `up` 没跑过**。

### 第 4 优先：端到端的最后一跳

设备 → Broker → 厂商 Mosquitto 插件 → `ingress_v1`。**`.so` 不在本仓库**
（`internal/brokerplugin/` 只有 `route.go`），本机拉不起来。这一段属于已验收的 broker
改造，不是北向新增，但没有它就不算完整端到端。

### 第 5 优先：保留期清理

事件 7 天、投递 30 天，只做了代码审查，没等真实窗口。上线后第一周按
`MAINTENANCE.md` §4 的 SQL 核对一次。

---

## 5. 这一轮修掉的两个真实缺陷（别再踩）

### 5.1 投递 worker 空指针，重启插件就能打挂整个服务

**症状**：`encore run` 直接 panic 退出。

```
panic: runtime error: invalid memory address or nil pointer dereference
encore.app/northbound.deliverBatch(...)  worker.go:167
```

**根因**：`var pe *pluginError; if isPluginFailure(err) { kind = pe.kind }` —— `pe`
声明后从未赋值，`isPluginFailure` 返回真时就是解引用空指针。触发条件极普通：**重启
一次插件**，`open` 失败就走这条路径。

**已修**：收成 `classifyPluginError(err)`（`catalog.go`），两处调用点都改用它；加了
`TestClassifyPluginError`（7 个用例），旧代码在这个测试下会直接 panic。

### 5.2 插件在容器里监听 127.0.0.1，端口映射打不进去

**症状**：`docker ps` 显示 Up，日志说 `listening on 127.0.0.1:18081`，但 `curl`
返回 000。

**根因**：`http.Server.Addr` 写死 `127.0.0.1:%d`。Docker 端口发布转发到容器 eth0，
监听容器自己的回环从宿主机永远连不上。

**已修**：加 `--host` 参数，默认仍是 `127.0.0.1`（裸进程更安全），容器里传
`0.0.0.0`。compose 里已写好。

### 5.3 还有几个坑，都记在文档里了

| 坑 | 表现 | 记在哪 |
|---|---|---|
| `sql/northbound/` 在子目录，自动迁移扫不到 | 静默不转发，页面「等待投递」永远 0 | `deploy/northbound/README.md` |
| `go test` 在 Encore 应用里会 panic | 必须用 `encore test` | `release.py` 注释 |
| Encore 把 `failed_precondition` 映射成 400 | 冲突码必须是 `errs.Aborted` | 合同 §9 |
| PG text 列不接受 NUL 字节 | 拼接 `order_key` 用 `\x1f` | `inbox.go` 注释 |
| `(? || ' seconds')::interval` 把整数塞进 text 参数 | relay 一条都交接不出去 | `relay.go` |
| paho `Publish` 不认 `json.RawMessage` | 报 `unknown payload type` | MQTT 插件注释 |
| `retained_profile` 合法值是 `live-ingress`，不是 `retained_live_ingress` | 后端起不来 | QA 配置 |

### 5.4 退避测试原来是概率红的（已修）

`TestBackoffGrowsAndIsCapped` 里断言 `nextBackoff(6, 10) >= 30s`。但 attempt 6 的基数
是 32s 且 `backoffFor` 带 ±20% 抖动，真实取值范围是 **25.6s～38.4s** —— 也就是说这个
断言有约三分之一概率随机失败。

本次是在 release 复跑时撞上的（同一份代码前几次都是绿的）。已改成按抖动区间判断：

```go
if d := nextBackoff(6, 10); d < 25*time.Second || d > 39*time.Second { ... }
```

并用 `encore test -run TestBackoffGrowsAndIsCapped -count=20` 连跑 20 次确认稳定。

**教训**：任何带抖动的函数，断言必须写成区间。随机的红比不测更糟——它和真实回归
长得一模一样。

---

## 6. 几个关键决策，以及为什么

| 决策 | 原因 |
|---|---|
| 基线用 `1a1ef0a0` 而不是 `main` | `main` 没有 `internal/brokerledger/`、`uplink/reliable.go`、`storage/telemetry_broker.go`，而这些正是北向挂载持久边界的地方 |
| 源端表放社区主库，投递账本放 Encore 库 | 源端登记必须和遥测批次在**同一事务**；投递账本是北向自己的 |
| 不新建账号体系 | 复用社区令牌，代理只做 `x-token` → `Bearer` 转换，不提权不签发 |
| 任务只有草稿能删 | 跑过的任务拥有必须留存的投递记录，删除会毁审计证据 |
| `unknown` 不自动重试 | 目标可能已写入，平台无法替用户判断重复写入能否接受 |
| 「等待投递」与「因容量未登记」分开显示 | 前者会继续发，后者永远不补发，加在一起就是骗人 |
| 幂等键不含版本/尝试次数/实例 | 否则重试或升级会在目标侧产生第二次业务写入 |

---

## 7. 文件地图

```
docs/northbound/
├── HANDOVER.md          ← 本文件，接手先读这个
├── EXECUTION-LEDGER.md  ← 施工台账，最详细，按 NB-01..NB-10 逐条记
├── CONTRACT-v1.md       ← 唯一接口合同（v1.2）
├── DEPLOYMENT.md        ← 装/升/退，含实测端口与未验证清单
├── USAGE.md             ← 怎么用（页面 + API + 常见问题）
├── MAINTENANCE.md       ← 值班看什么、故障怎么查、巡检 SQL
├── CHANGES.md           ← 变更摘要，给代码审查与合入用
└── reports/             ← 三份脚本生成的真实报告
    ├── journey.md       (27 通过 / 0 失败 / 0 跳过)
    ├── recovery.md      (22 通过 / 0 失败 / 0 跳过)
    └── release.md       (25 通过 / 0 失败 / 0 跳过)

encore-api/northbound/       ← Encore 服务（17 个 go 文件 + 迁移 + 单测）
internal/northbound/         ← 社区侧源端（登记 + relay）
plugins/northbound-http/     ← HTTP 出口插件
plugins/northbound-mqtt/     ← MQTT 出口插件
plugins/northbound-sdk-go/   ← 插件 SDK
scripts/qa/northbound/       ← 三个验收入口 + 接收端 + 共用库
deploy/northbound/           ← 单机编排 + 镜像 + 迁移脚本
sql/northbound/              ← 社区库迁移
```

变更规模（相对各自基线，已排除 `tools/`）：

| 仓库 | 新增 | 修改 | 合计 |
|---|---|---|---|
| 后端 | 128 | 8 | 136 文件 / +27164 −3 行 |
| 前端 | 7 | 9 | 16 文件 |

后端那 128 个新增里，**67 个不是这次写的**：`encore-api/devicemetadata`（48）、
`encore-api/thingmodel`（15）、`encore-api/l1-access`（3）、`encore-api/internal/authz`（1）
来自已验收的物模型成果（见 §8 第 2 条）。本次北向自己新增的是 **61 个文件**。

---

## 8. 合入主分支要注意

1. **broker 改造必须先合。** 顺序错了编译不过。
2. **`encore-api/` 整个目录是新增的**，但其中 `devicemetadata/`、`thingmodel/`、
   `l1-access/`、`internal/authz/` 来自已验收的物模型成果
   （`codex/thingmodel-dev-integration-20261018@37fbb5e`），**不是这次写的**，
   必须一起带上否则编不过。
3. **`tools/legacy-migration/` 不要带进提交。** 它是另一条线的物模型迁移脚手架，
   在工作区里是未跟踪状态，与本变更无关。
4. 插件二进制、`*-go-tmp-umask`、`*.go.[0-9]*` 已被 `.gitignore` 挡住。

---

## 9. 如果只有 10 分钟

按这个顺序做，能最快判断"这东西到底能不能用"：

1. 读 `docs/northbound/reports/release.md` 最后三行 → 看结论
2. 按 §2.2 起进程，按 §2.4 自检 → 看链路通不通
3. 跑 `journey.py` → 看端到端成不成
4. 读 `EXECUTION-LEDGER.md` 的 NB-08 段 → 看有哪些坑已经被踩过

---

## 10. 遗留风险，一句话一条

- 前端没过编译器，可能有类型错误。
- Encore 生产镜像没构建过，容器化部署路径未验证。
- 端到端缺设备接入那一跳（`.so` 不在本仓库）。
- 保留期清理没等真实窗口，只有代码审查。
- 多副本核心没测（租约与 `SKIP LOCKED` 是按多副本写的）。
- 验收环境全部单机、全部 `127.0.0.1`，跨网络、跨机器的情况没测。
