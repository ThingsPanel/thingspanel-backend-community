# 自动化 / 场景 / 告警重写 — 第一轮执行记录

更新时间：2026-10-23
执行人：接手开发（WorkBuddy）
依据：《开发交接提示语 2026-10-23》《开发任务书 v1.1》A01–A34

---

## 1. 实际分支与提交版本

| 项 | 值 |
| --- | --- |
| 工作树 | `/Users/junhong/Downloads/code/tp-automation-rewrite`（独立 `git worktree`，不动同事的 43 个未提交修改） |
| 分支 | `feat/automation-rewrite-20261023` |
| 基线提交 | `486160092b1579965573814c8dd11a2a689c9246`（研究基线） |
| 本轮提交 | `c72fdab68cb312e57ce4dda36ef8cc625af8e7eb` |
| 主仓 HEAD | `0e3ba8281f09c815dabcd03c634d17af845e950b`（`main`，比基线旧 75 个提交，未改动） |

基线关系核对结论（`git rev-list --left-right --count`）：

- `0e3ba828`（main）↔ `48616009`（基线）= `0 75`：**基线是 main 的后代，main 上不含基线**；
  只有 `origin/main-1.3.0` 含该提交。因此在 `48616009` 上开工作树，而不是在 main 上开发。
- `32f89e55`（北向工作线）↔ 1.3 = `25 75`，merge-base 为 `0e3ba828`：北向线是分叉，不是子集。

---

## 2. 环境启动命令及结果

工具链实测：Go `go1.26.2 darwin/arm64`、encore `v1.58.6`、Docker `29.8.2`、Compose `v5.5.0`、Node `v22.22.2`、psql `18.4`。

`brew services start postgresql@18 / redis` 失败（`launchctl bootstrap … exited with 5`），改为**完全隔离的 Docker 栈**，端口与默认值全部错开，避免和本机已有实例冲突：

| 组件 | 端口 | 文件 |
| --- | --- | --- |
| PostgreSQL（timescaledb:14） | 55432 | `deploy/automation-rewrite/docker-compose.yml` |
| Redis 7 | 16381 | 同上 |
| Mosquitto 2.1.2 | 11885 | 同上 + `mosquitto.conf` |
| 旧后端 HTTP | 19999 | `configs/conf-automation-local.yml` |
| Encore 服务 HTTP | 14000 | `encore run --port 14000` |

```bash
# 1) 依赖
cd deploy/automation-rewrite && docker compose up -d
docker ps --format '{{.Names}}\t{{.Status}}'
# tp-auto-pg      Up (healthy)
# tp-auto-redis   Up (healthy)
# tp-auto-mqtt    Up

# 2) 旧后端（空库，由程序自己迁移；不要手工先跑 sql/1..N.sql）
export GOMODCACHE=/Users/junhong/Downloads/code/.gomodcache
export GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct
go build -o /tmp/tp-auto-backend .
/tmp/tp-auto-backend -config ./configs/conf-automation-local.yml     # 日志：启动成功
curl --noproxy '*' -X POST http://127.0.0.1:19999/api/v1/login \
  -d '{"email":"tenant@tenant.cn","password":"123456"}'              # code 200 + token

# 3) Encore 宿主
cd encore-api
encore check 'curl /v1/health'      # check: 1/1 commands succeeded
encore test ./...                   # 全部通过
TP_AUTH_MODE=dev \
TP_COMMUNITY_BASE_URL=http://127.0.0.1:19999 \
TP_COMMUNITY_TOKEN=<登录 token> \
NO_PROXY=127.0.0.1,localhost \
encore run --port 14000
```

踩到的坑（已解决，写进交接，避免下一个人重踩）：

1. `go build` 报 `mkdir /Users/junhong/go/pkg/mod/...: permission denied`（该目录属 root）→ 独立 `GOMODCACHE`。
2. 先手工跑 `sql/1..27.sql` 再启动，报 `relation "alarm_config" already exists`（程序按 `VERSION_NUMBER=25` 从 1 再跑一遍）→ **空库 + 程序自动迁移**。
3. `DROP SCHEMA public CASCADE` 会连 timescaledb 扩展一起删掉，再启报 `function create_hypertable(...) does not exist` → 重置脚本要补 `CREATE EXTENSION IF NOT EXISTS timescaledb`。
4. shell 里有 `HTTP_PROXY=127.0.0.1:62557`，所有本地请求必须 `curl --noproxy '*'`，脚本用 `ProxyHandler({})`。
5. `nohup … &` 在非交互 shell 会被回收（日志显示“启动成功”但端口不通）→ 用后台任务 + 非 9999/4000 端口（已被 OrbStack 占用）。
6. 种子账号是 `tenant@tenant.cn / 123456`（`tenant_id=d616bcbb`），不是 `tenant@thingspanel.cn`。
7. 设备配置路由是 `/api/v1/device_config`；`device_number` 唯一，脚本必须带随机后缀。
8. 上行报文 `{"device_id":..., "values":"<base64 of JSON>"}`（`publicPayload.Values []byte`）。
9. mosquitto 容器只能挂配置，**不能覆盖 command**（镜像 entrypoint 已含参数），否则 `Address already in use`。

---

## 3. A01 旧功能台账（初版）

状态标记：`[源码]` 已读实现、`[实测]` 已在隔离环境跑通、`[待补]` 尚未取到证据。

### 3.1 配置对象与存储

| 旧对象 | 证据 | 新去向 | 主任务 | 验收用例 |
| --- | --- | --- | --- | --- |
| `scene_automations`（规则头：name/enabled/trigger_condition_groups/actions） | [源码] `internal/model/scene_automations.gen.go`、`internal/service/scene_automations.go` | `auto_rules` + `auto_rule_revisions`（版本化不可变定义） | A04 | 建规则→revision=1；改规则→revision+1 且旧 revision 仍可被在飞运行读取 |
| `scene_automation_log`（执行结果 S/F、detail 文本） | [实测] 基线脚本读到 `execution_result=S` | `auto_runs` + `auto_steps` + `auto_targets` + `auto_outbox`（结构化，可解释） | A04/A09 | `/v1/runs/:id` 同时给出运行、步骤、目标快照、意图四级证据 |
| `alarm_history` / `alarm_cach`（告警去重缓存） | [源码] `internal/service/alarm.go`、`automate_decoration.go` | `alarm_episodes` + `alarm_transitions`（`(tenant,policy,subject,dimension)` 单活动实例，partial unique index） | A15 | 连续 5 条异常样本 → 只有 1 个活动 episode、1 条 qualified 迁移 |
| 条件组（组间 OR、组内 AND） | [源码] `AutomateConditionCheck` | `domain.Trigger.Groups` + `domain.Group.Conditions`（同一语义，四值化） | A04 | 组内 false 短路；组间任一 true 即 true |
| 触发参数类型 TEL / ATTR / EVT / STATUS | [源码] `device_trigger_condition.http.go`（10/11/22） | `domain.SignalKind` = telemetry/attribute/event/status | A04 | 每种信号各一条用例，事件条件非事件时刻判 unknown |
| 9 个比较符 `= != > < >= <= between in exists` | [源码] + [实测] 表驱动 18 例 | `domain.compareValues`（同一份允许列表 `ValidOperators`） | A04 | `domain/eval_test.go::TestOperators` 全绿 |
| 动作类型 10/11/20/30/40 | [源码] `automate_telemetry_action.go` | `domain.StepKind` = action/notify/alarm/activate_scene + branch/wait | A09/A21 | 每种步骤一条运行用例 |
| 动作参数类型 TEL/ATTR/CMD（含 `c_telemetry` 等别名） | [源码] `AUTOMATE_ACTION_PARAM_TYPE_*` | `ports.CommandKind` = telemetry/attribute/command | A10 | 下发到旧接口并回读旧日志 |
| `AutomateTelemetryActionService`（40）返回 `"服务", nil` 的假成功 | [源码] 同上（B21） | 不实现该分支；未配置 owner 时一律 `unknown` | B21 | `TestUnknownCommandResultIsNotSuccess` |

### 3.2 已确认的旧缺陷（本轮必须修掉，且已被新实现覆盖）

| 编号 | 旧行为 | 影响 | 新实现 | 证明 |
| --- | --- | --- | --- | --- |
| D1 | `ConditionAfterAlarm` 在 `!ok`（读不到值）时直接 `AlarmRecovery` | 设备离线/数据缺失会把活动告警“恢复”掉 | `applyAlarmVerdict`：unknown/error 只写 `unknown`/`error` 迁移，**不清告警** | `TestAlarmUnknownDoesNotClear` |
| D2 | `automateConditionCheckByOperator` 对未知比较符返回 false | 配置错误被当成“条件不成立”，静默不触发 | 未知比较符 → `VerdictError` + `invalid_compare` 原因 | `TestMalformedComparisonsAreErrors` |
| D3 | 无 `for` 语义时每次上行都触发，无 pending 状态 | 抖动即重复执行 | `auto_pending` + 输入进度屏障 + 证据新鲜度 | `TestContinuousConditionWaitsForFor`、`TestInputProgressBarrier`、`TestStaleEvidenceDoesNotFire` |
| D4 | `LimiterAllow` 每秒 1 次的进程内限流 | 重启失效、多副本不一致 | 持久运行模式 single/restart/queued/parallel + generation | 4 条模式用例 |
| D5 | 告警恢复无迟滞（异常消失即恢复） | 临界值抖动导致告警风暴 | 恢复必须是显式 `recovery` 条件；未配置则保持活动并写 `no_transition` | `TestAlarmRecoveryRequiresCondition` |
| D6 | 场景动作假成功（B21） | 用户以为设备收到了 | 未确认一律 `unknown`，意图保留可重试 | `TestUnknownCommandResultIsNotSuccess` |

### 3.3 复用边界（不新建第二套）

| 能力 | 归属 | 本轮做法 | 证据 |
| --- | --- | --- | --- |
| 设备命令下发 | 旧 `internal/api/*_data.go` | `ports.HTTPCommandOwner` 调 `/api/v1/telemetry|attribute|command/datas/pub` | [实测] 旧库 `telemetry_set_logs` 出现 `{"switch":1}`、`status=1` |
| 通知 | 旧通知核心 `/api/v2/notification-ingress/events` | `ports.HTTPNotificationOwner` 按既有契约提交，202+accepted 才算受理 | [源码] `notification_source.go` 对齐；[待补] 真实通知核心联测 |
| 设备目录 | 旧 `/api/v1/device` | `ports.HTTPDirectory` 分页读取 | [实测] 解析到真实设备 |
| 物模型 | 旧 `devicemetadata` | 本轮只读能力清单（目录未暴露则交命令 owner 拒绝） | [待补] 能力清单字段 |
| 鉴权 | 旧 JWT | 复用 `l1-access/authn`（HS256 + `TP_JWT_KEY` + 会话校验） | [源码] 原样复用 |

### 3.4 待补证据（下一轮补齐，不隐藏）

- 六类页面（全局场景联动 / 场景管理 / 设备自动化 / 设备告警 / 模板自动化 / 模板告警）的旧→新操作对照与截图：**未开始**（A02 剩余部分）。
- 前端仓库版本核对：**未开始**。
- 旧 `alarm_history` 字段级清单（`route=legacy/blocked`、`SaveAlarmHistoryWithSource`）：已读源码，未做逐字段表。
- 35 个跨模块对接点：已完成命令/通知/目录三类，其余待逐条登记。

---

## 4. 一条原业务的运行证据（基线复现）

脚本：`test/automation-rewrite/baseline_legacy_run.py`（跑通两次）

链路：`MQTT 上行 → 旧 automate 过滤 → 条件判定 → TEL 动作 → 落库 → API 回读`

```text
POST /api/v1/device_config        → 200
POST /api/v1/device               → 200
POST /api/v1/scene_automations    → 200（TEL temperature > 30 → action_type 10 TEL switch）
POST /api/v1/scene_automations/switch/{id} → 启用
mosquitto_pub devices/telemetry   {"device_id":"…","values":"<base64 of {\"temperature\":31.5}>"}
GET  /api/v1/scene_automations/log
```

回读结果：

```json
{"execution_result":"S","detail":"设备名称:AUTO-BASE-DEV-… 遥测指令:{\"switch\":1} 执行成功;"}
```

这条证据的作用：证明“旧业务真的能跑”，后面所有新实现的对齐都以它为参照，而不是以文档描述为参照。

---

## 5. A03 Encore 唯一落点

**唯一宿主**：`encore-api/`（模块 `encore.app`，`encore.dev v1.30.0`，CLI `v1.58.6`）

```
encore-api/
├── encore.app                     # {}
├── go.mod / go.sum
├── l1-access/authn/               # 复用社区 JWT 鉴权（legacy-jwt / dev / passthrough）
└── automation/                    # 三域唯一写入者
    ├── service.go                 # //encore:service + 19 个 //encore:api
    ├── api_types.go               # 对外读写模型（无 any / 无接口，满足 Encore schema 约束）
    ├── migrations/1_init.up.sql   # 自有库，13 张表
    ├── domain/                    # 单一评估器 + 单一校验门（无 I/O 依赖）
    ├── runtime/                   # 摄入 / 待定窗口 / 运行 / 分支 / 等待 / outbox / 告警 / 模拟
    ├── store/                     # 全部 SQL（其它包不写这些表）
    └── ports/                     # 命令 / 通知 / 目录三个窄端口
```

所有权声明：

- 本服务**独占** `auto_*` 与 `alarm_*` 共 13 张表，迁移随服务走，可独立部署与回退。
- 设备命令、通知、目录、物模型、接入**不在本服务内重新实现**，只经 `ports/` 调用既有 API。
- 告警策略不得直接下发设备动作（校验层硬拒 `alarm_side_effect`），保证告警 owner 只产生生命周期事实。

对外接口（19 个）：

```
GET  /v1/health                      public
POST /v1/rules                       GET /v1/rules        GET/PUT/DELETE /v1/rules/:id
POST /v1/rules/:id/enabled           POST /v1/rule-validation
POST /v1/rules/:id/simulate          GET  /v1/rules/:id/runs
POST /v1/ingress/events              GET  /v1/runs/:runId
GET  /v1/alarms                      POST /v1/alarms/:id/ack   GET /v1/alarms/:id/transitions
POST /v1/tick                        POST /v1/recover
GET  /v1/backlog                     GET  /v1/events
```

### 5.1 本轮新增的端到端证据

脚本：`test/automation-rewrite/encore_e2e.py` → **25 项检查全部通过**

关键几项（真实 HTTP、真实旧后端，无 mock）：

| 检查 | 结果 |
| --- | --- |
| 校验拒绝空触发 | `valid=false, code=empty_trigger` |
| 建规则 | `revision=1, definition_hash=22eb67be` |
| 模拟 | `would_run=1, side_effects=0`，且事后 0 条运行 |
| 摄入幂等 | 同 `event_id` 第二次 `duplicate=true` |
| 运行解释 | `succeeded`，目标 `accepted`，1 条意图，幂等键 `…:0:cmd:<device>` |
| 告警生命周期 | `qualified → ack → clear`，确认后仍 active |
| 旧库回读 | `telemetry_set_logs` 有 `{"switch":1}`，`status=1` |

### 5.2 待解决差异（如实登记）

| 编号 | 差异 | 现状 | 归属 |
| --- | --- | --- | --- |
| G1 | 社区设备目录**不返回区域层级** | 区域目标显式写 `area_not_supported` 排除行，不静默空集 | 目录 owner（A06） |
| G2 | 社区设备列表**不返回 `label`** | `label_not_exposed` 显式排除行，不假装没匹配 | 目录 owner（A06） |
| G3 | 社区设备列表**不返回能力清单** | `hasCapability` 视为未知并放行，最终由命令 owner 拒绝 | 目录/物模型 owner |
| G4 | 命令受理端**不认识 `X-Automation-Generation`** | 只做到“派发前检查已取消 + 如实记录受理”；权威代际校验待落地 | A10 跨模块 |
| G5 | 旧后端**没有“立即执行某场景”接口** | `ActivateScene` 返回 `unknown`，不伪造成功 | A21 |
| G6 | `page_size` 上限 1000，且业务错误用 HTTP 200 + `code!=200` 表达 | 端口改为分页读取并校验 `code`（本轮修掉的真实缺陷） | 已完成 |
| G7 | 通知核心未做真实联测 | 契约已按 `notification_source.go` 对齐，状态“待联测” | A15 |
| G8 | 未做重启恢复的进程级演练 | `RecoverAfterRestart` + 持久 pending 已实现并有单元用例，缺 kill -9 演练 | A11 |
| G9 | 性能预算表未填数字 | 只有 `Backlog` 观测口，缺压测数据 | A30/A31 |
| G10 | 旧上行**尚未转发**到新入口 | 新入口已可用且已验证，但社区 MQTT 链路没加钩子；现在接会与旧引擎双写 | A33 切换时接线，见 6.6 |

---

## 6. A05 事件入口与交接可靠性（已完成到测试）

### 6.1 输入分类（显式适配，不靠猜）

入口：`POST /v1/ingress/uplink`，`class` 决定走哪个适配器。代码在 `automation/ingress/`。

| class | 信号 | 语义 | 一条报文产出 |
| --- | --- | --- | --- |
| `telemetry` | `telemetry` | 持久样本，可被后续条件读取 | 每个 key 一条事件 |
| `attribute` | `attribute` | 持久状态值，可回读 | 每个 key 一条事件 |
| `event` | `event` | **瞬时**，只表示“这次发生了” | 一条，`value` 恒为 `true`，参数放 `params` |
| `status` | `status` | 上下线，是状态迁移 | 一条，`key=online`，值归一成布尔 |
| `alarm` | `alarm` | 告警生命周期迁移 | 一条，`key` 为迁移类型 |
| `gateway_telemetry` / `gateway_attribute` / `gateway_event` | 同上 | 网关代传的子设备流量 | 同上，另记 `gateway_id` |

两条硬规则：

1. **解码与转换在适配器之前**。进来的值已经是物模型转换、单位换算后的结果，适配器不重新解释原始报文，也不让管道对同一份数据转换两次（对应 B02“转换阶段固定；不重复评估”）。
2. **属性与瞬时事件不混用**。事件条件只在“本次样本”可证，绝不会被读成持续状态——这是旧实现把很久前一次事件当持续条件的问题。

### 6.2 稳定事件 ID（重投不变）

`ingress.StableID` 用 `sha256(各部分用 0x1f 连接)` 取前 16 字节。优先级：

| 输入 | ID 来源 | 为什么 |
| --- | --- | --- |
| attribute / event | 主题里的 `message_id` 优先 | 同一消息重发即使 JSON 重排也能去重 |
| telemetry | `(class, tenant, device, key, observed_at, value)` 内容哈希 | 同一读数重投去重，**不同时刻的同值仍算两条证据**（否则“持续 5 分钟”会断） |
| status | `(tenant, device, value, observed_at)` | 真实迁移保持独立，重复上报由状态比对识别 |
| alarm | 告警 owner 生成的 `transition_id` | 权威 ID，重投不重复启动同一联动动作 |

接收时间**不参与** ID 计算，所以 broker 重投、relay 重试、进程重启都不会改变 ID。

### 6.3 持久受理点、ACK、重放与保留窗口

| 项 | 落点 | 说明 |
| --- | --- | --- |
| 持久受理点 | `auto_inbox`，`event_id` 主键 | 先落库再评估；崩溃只留可重放的行，不会静默丢样本 |
| ACK 语义 | `processed_at` 置位 | 未置位 = 已受理未处理，`RecoverAfterRestart` 按 `seq` 顺序补处理 |
| 重放 | `RecoverAfterRestart(limit)` | 幂等：第二次调用返回 0，不再产生运行 |
| 保留窗口 | `auto_inbox` 按 `received_at` 建索引 | 清理策略属 A32，本轮只保证可查 |
| 跨库原子性 | **不声称** | inbox 在自动化库，原始遥测在 TSDB，明确是“至少一次 + 幂等”而不是本地事务 |

### 6.4 重复上报与因果链

- **重复上线被识别**：`status` 信号在 `StatusCoalesce`（60s）内值未变时，仍然落库（它是真实报文），但跳过评估，并发布 `ingress.duplicate_state` 记录判定原因。设备每次重连都报一次 online，不这样处理会重复触发。
- **因果链**：`auto_runs` / `auto_pending` / `auto_inbox` 都带 `causal_root` 与 `causal_hop`。告警迁移事件把发起运行的链信息带出去，联动规则继承并把跳数 +1。
- **循环停止**：`runtime.MaxCausalHop = 8`。超限不执行，发布 `causal.hop_exceeded`，带 `causal_root`、`causal_hop`、`limit` 和原因，可查询。
- 跳数在等待期间不丢：`for: 5m` 的规则把链信息写进 `auto_pending`，到点续跑仍算同一跳。

### 6.5 本轮 A05 证据

| 完成标准 | 证据（可复跑） |
| --- | --- |
| 每种输入都有样例 | `ingress_test.go` 18 个用例：telemetry/attribute/event/status/alarm/网关三类全覆盖 |
| 重投证据 | `TestRedeliveredUplinkProducesOneRun`（同报文两次 → 1 次运行、1 条命令）；e2e 第 10 节真实 HTTP 重投 |
| 重启证据 | `TestRestartRecoversUnprocessedReceiptsOnce`（先落库不处理 → 恢复补处理 → 二次恢复 0 条） |
| 重复上线被识别 | `TestRepeatedOnlineIsRecognised`（10s 内重复 online 不新增运行；真实断开重连仍会动作） |
| 属性与瞬时事件不混用 | `TestAttributeAndEventStayDistinct`（属性投影成值，事件投影成 `true`） |
| 告警转换可触发联动 | `TestAlarmTransitionTriggersLinkedRule`（`tr-100` → 运行 `causal_root=tr-100, hop=1`） |
| 同一转换重投不重复动作 | `TestAlarmTransitionRedeliveryIsIdempotent` |
| 因果跳数受限且原因可查 | `TestCausalHopLimitStopsTheLoop`（超限 0 运行 + 1 条 `causal.hop_exceeded`） |

### 6.6 尚未接线（如实登记，不假装完成）

**旧上行还没有转发到新服务。** 现在新入口只由显式调用驱动，社区后端的 MQTT 上行链路没有加转发钩子。

原因不是漏做，而是现在接上会**双写**：旧规则引擎仍在跑，同一个遥测会既走旧场景又走新规则，产生两次真实下发，正好违反 A05 的“旧、新路径不会同时产生真实副作用”。正确顺序是 A33 切换时：

1. 先关旧引擎的规则执行（保留采集与转发）；
2. 再把 uplink 的 `telemetry/attribute/event/status` 四类在转换之后转发到 `/v1/ingress/uplink`；
3. 用 A01 台账逐条比对旧场景结果与新运行结果，一致后才删旧执行路径。

这一步的接线点已定：`internal/uplink/*.go` 四个 handler 在写库之后、调旧 service 之前，是唯一需要改的位置。本轮不改它，避免把“未切换”变成“已双写”。

---

## 7. A06 条件组合与错误判定（已完成到测试）

### 7.1 旧条件面完整保留

| 旧能力 | 现状 | 证据 |
| --- | --- | --- |
| 9 个比较符（`=` `!=` `>` `<` `>=` `<=` `between` `in` `exists`） | 全部保留，语义逐条对齐 | `TestAllLegacyOperators` 16 行对照（含边界值） |
| 组间 OR / 组内 AND | 保留，并记录每组判定 | `TestGroupSemantics` |
| 指定设备 / 跨设备条件 | 保留，按 `device_id` 精确取数 | `TestCrossDeviceReadsTheNamedDevice`、`TestTwoDevicesSameFieldDifferentValues` |
| 事件参数匹配 | 保留，事件条件只在触发时刻可证 | `TestEventConditionIsOnlyCurrentAtTriggerTime` |

### 7.2 四值判定补强（本轮实际改动）

以前只有 `no_data` / `quality_unknown` / `unit_mismatch` / `invalid_compare` 四种原因，且**存储读取失败会被折叠成“没有数据”**。现在：

| 情况 | 判定 | 原因码 | 为什么重要 |
| --- | --- | --- | --- |
| 存储读取失败 | `error` | `read_error` | “数据库拒绝”和“设备从未上报”要分开，否则排障方向全错 |
| 数据早于 `max_age` | `unknown` | `stale_evidence` | 陈旧值既不能当 `true`（会误动作），也不能当 `false`（会误恢复） |
| 实际值不是数字 | `error` | `invalid_number` | 区分“数据坏了”与“条件不成立” |
| 阈值写错 | `error` | `invalid_threshold` | 配置错误要报出来，不能静默 `false` |
| `between` 格式错 | `error` | `invalid_range` | 同上 |
| 单位不一致 | `error` | `unit_mismatch` | 原来就有，补了原因码断言 |

关键语义：**触发样本本身永远算当前证据**。设备时钟落后 2 小时是现场常见问题，如果按存储时间判新鲜度，这类设备会永远“数据过期”。规则是：

- 触发样本与条件同设备同信号同 key，且不早于存储值 → 用触发样本，跳过 `max_age`；
- 存储值严格更新（乱序到达）→ 用存储值，避免迟到的旧样本覆盖当前判断。

这个取舍有专门用例：`TestTriggeringSampleIsExemptFromMaxAge`。

### 7.3 本轮 A06 证据

| 完成标准 | 证据 |
| --- | --- |
| 两设备同名字段不同数值判断准确 | `TestCrossDeviceReadsTheNamedDevice`（域层）+ `TestTwoDevicesSameFieldDifferentValues`（DB 层，dev1 热 dev2 冷 → 不动作；dev2 变热 → 动作） |
| 所有旧有效比较符与分组有对照用例 | `TestAllLegacyOperators` 16 行 + `TestGroupSemantics` + `TestErrorInOneGroupIsNotMaskedByTrueInAnother` |
| 缺测可解释 | `TestMissingDataIsUnknownNotFalse`（`no_data`） |
| 数据读取失败可解释 | `TestReadFailureIsErrorNotMissingData`（`read_error`，不是 `no_data`） |
| 非法数字可解释 | `TestNonNumericValueIsClassified`、`TestMalformedThresholdIsClassified` |
| 单位不兼容可解释 | `TestUnitMismatchReportsItsReasonCode` |
| 不落入正常恢复 | `TestStaleRecoveryEvidenceDoesNotClearAlarm`（DB 层：恢复条件读的是 1 小时前的值 → 告警保持 active，迁移表记 `unknown`） |
| 不落入默认 true | `TestPendingExpiresInsteadOfFiringOnStaleEvidence`（到点但证据过期 → 0 运行 0 命令 + 1 条 `pending.expired`） |

### 7.4 仍未解决

| 编号 | 差异 | 说明 |
| --- | --- | --- |
| G11 | 输入键**不含模型版本** | `auto_state` 主键是 `(device_id, signal, key)`。设备物模型换版后，旧值仍按同一 key 命中。要修需目录/物模型 owner 提供版本号，属跨模块。本轮不造假：宁可保持现状并登记，也不编一个本地版本号 |
| G12 | 恢复条件读的是**存储值**，可能是别的设备 | 这是设计使然（恢复条件可引用其他设备），但意味着“恢复证据的新鲜度”取决于那个设备自己的上报节奏。已在 `max_age` 里体现，不做额外假设 |

---

## 8. A07 单次与周期定时的可靠执行（已完成到测试）

旧系统把定时做成了“每条规则一份 `scene_timer` 记录 + 进程内 goroutine”。进程重启后定时丢失，删除场景后 goroutine 仍在跑，而“错过的时间点”既没有记录也没有策略。A07 把定时改成**业务数据 + 幂等 occurrence**。

### 8.1 契约

| 表 | 作用 | 关键约束 |
| --- | --- | --- |
| `auto_schedules` | 一条定时（一次性或 cron） | `next_fire_at` 上有 `WHERE enabled AND deleted_at IS NULL` 的部分索引，到期扫描走索引而不是全表 |
| `auto_schedule_fires` | 一次“应该执行的时刻” | `UNIQUE (schedule_id, scheduled_for)` —— 重复唤醒、重试领取、重启都只能撞上这一行 |

状态分离，崩溃不可能伪装成成功：

| 状态 | 含义 |
| --- | --- |
| `claimed` | 已记录该时刻，还没开始执行 |
| `running` | 有 worker 持有租约，正在执行 |
| `done` | 运行已创建（成功与否看运行自身状态） |
| `failed` | 重试到上限，原因已记录 |
| `skipped` | 策略决定不执行（规则停用、错过窗口） |

领取用 `FOR UPDATE ... SKIP LOCKED` + 时间租约（`ScheduleLease = 30s`）。多副本不会拿到同一行；worker 崩溃后租约到期自动可再领取，不需要人工清理。

### 8.2 时区与夏令时

cron 是五字段（分 时 日 月 周），支持 `*`、`a-b`、`*/n`、`a/n`、逗号；`7` 归并为周日；日/周同时受限时按**传统 OR** 语义。时区是 IANA 名称，未知时区**报错而不是回落到 UTC**（悄悄差 8 小时比拒绝保存更糟）。

夏令时两条都显式处理：

- 春季跳变里**不存在**的本地时刻直接跳过。`30 2 * * *` 在 `America/New_York` 的 2026-03-08 不执行，下一次是 03-09 02:30，而不是顺移到 03:30。
- 秋季重复的本地时刻**只执行第一次**。2026-11-01 01:30 出现两次，只跑一次，不会半夜跑两遍。

### 8.3 漏跑策略

`misfire_policy` 只有两个值，默认 `skip`：

- `skip`：错过的时刻**一条都不补**。半夜重启后不会把四小时前的阀门指令重新发一遍。
- `run_once`：无论错过多少次，**最多补一次**，补在当前时刻，不是补在历史时刻。

判定“是否错过”用 `MisfireGrace = 2 分钟`：扫描晚几秒是正常调度，不是漏跑。同一个常量同时决定“跑不跑”和“记不记 missed”，两者不会打架。错过窗口会发一条 `schedule.misfired` 事件，带上 `missed` 次数与 `resume_at`。

### 8.4 幂等三条路径

| 场景 | 结果 |
| --- | --- |
| 同一时刻被重复唤醒 | `InsertFire` 撞唯一索引 → 计 `duplicates`，不产生第二次运行 |
| 记录后、执行前崩溃 | 该行仍是 `claimed`，下一轮被领取并执行一次 |
| 执行中崩溃（运行已创建） | `FindRunByTrigger` 命中已有运行 → 直接标 `done`，不重复执行动作 |

一次性定时执行完 `next_fire_at` 置空并自动 `enabled=false`，不会留下“启用但永远不会再触发”的僵尸行。删除定时在同一条 UPDATE 里清 `next_fire_at` 与租约；删除规则时调用 `CancelSchedulesForRule`。停用规则**不**清定时（到期扫描的 JOIN 已经要求规则 enabled），这样重新启用时能按漏跑策略续上，而不是把任务弄丢。

### 8.5 证据

```bash
encore test ./automation -run 'Schedule|Misfire|Occurrence|Crash|Disabled|Deleted|OneOff|Preview|Late|Deleting'
# ok  encore.app/automation

encore_e2e.py --community http://127.0.0.1:19999 --automation http://127.0.0.1:14000
# == 12. timed triggers (A07) ==  13 项全 PASS
```

`encore_e2e.py` 第 12 节走真实 HTTP：预览 `0 9 * * 1-5` 得到三个工作日 09:00；拒绝过去时刻的一次性定时；创建 6 秒后到期的一次性定时 → `POST /v1/tick` 命中 → `schedule_executed=1`、occurrence `done` 且带 `run_id`、规则运行数 1→2；再 tick 不再执行且该定时已 `enabled=false`；周期定时停用清 `next_fire_at`、启用重算、删除后 `GET` 返回 `not_found`。

### 8.6 尚未接线

| 项 | 说明 |
| --- | --- |
| 常驻调度循环 | 目前 `Tick` 里同步跑一次 `FireDue`，由外部（cron/HTTP）驱动。Encore 的常驻循环（`//encore:service` 的 `Run`）本轮未加，因为会与现有 `POST /v1/tick` 的运维方式冲突；接线点在 A33 |
| 前端定时编辑 | 依赖 A02 的页面核对；接口已就绪（CRUD + 预览 + occurrence 历史） |
| 立即执行一次 | 属 A21（场景立即执行），不在 A07 范围 |

---

## 9. 本轮验证汇总

```bash
cd encore-api
encore check 'curl /v1/health'   # check: 1/1 commands succeeded（20 endpoints registered）
encore test ./...                # domain ok / ingress ok / automation ok
```

| 包 | 用例数 | 覆盖 |
| --- | --- | --- |
| `automation/domain` | 59 | 9 比较符、四值判定、单位不匹配、事件参数、跨设备、校验门、A06 对照 30 条、cron 解析与夏令时 12 条、漏跑策略 8 条 |
| `automation/ingress` | 12 | 六类输入 + 网关三类、稳定 ID、重投、瞬时事件、状态归一、因果跳数 |
| `automation`（DB 集成） | 47 | 幂等、持续条件、V31 屏障、证据过期、四种运行模式、等待续跑、分支三态、通知端口、未知不当成功、告警四条、模拟两条、目标解析三条、A05 七条、A06 三条、A07 十三条 |
| **合计（顶层用例）** | **118** | 全部通过；含子测试共 156 条断言级用例 |

端到端：`encore_e2e.py` **53 项检查全通过**（第 10、11 节为 A05 的真实 HTTP 适配器路径，第 12 节为 A07 的定时执行路径）。

修复的真实缺陷（不是测试凑数）：

1. 设备目录 `page_size=2000` 被后端拒绝（上限 1000），且端口把 `code!=200` 当空列表 → 目标解析静默变空。已改为分页 + 校验 `code`。
2. `is_online` 是 0/1 数字而非布尔，按 bool 解析会整体失败。
3. 标签字段缺失被当成“没有匹配设备”，改为显式 `label_not_exposed`。
4. `auto_inbox` 原本不存因果链，重启恢复会**静默重置跳数**，让循环上限失效。已在迁移 `2_ingress_causal.up.sql` 补列，`UnprocessedEvents` 一并回读。
5. 告警迁移事件原本不带 `transition_id`，下游只能靠内容哈希去重（弱）。现在带权威 ID + `causal_root`/`causal_hop`。
6. `lookup` 把存储错误当成“没有数据”返回，导致数据库故障与设备静默不可区分。现在错误上抛为 `error`，缺测为 `unknown`。
7. 陈旧值原本仍被当作当前证据：恢复条件可以拿 1 小时前的读数清掉活动告警。现在按 `max_age` 判为 `unknown`，并记录 `stale_evidence`。
8. 触发样本一度被误判为“读到的旧值”而判过期（设备时钟滞后场景）。现在触发样本在同信号同 key 且不早于存储值时优先，既有用例守住。
9. 处理完一个到期时刻后 `next_fire_at` 不前进（`ResolveMissed` 在“未过期”分支直接返回原时刻），导致同一时刻每轮都被重新物化，只靠唯一索引挡住 —— 表现为“重复”而不是“时钟卡住”。已改为**每次处理都返回严格晚于 now 的时刻**。
10. 一次性定时按时执行后不置空 `next_fire_at`，留下一条“启用但永不触发”的僵尸行。现在按时执行即 `enabled=false` 且清空到期时间。
11. 错过的时刻被当成“待执行”插入（`claimed`），于是 `skip` 策略下仍然补跑了一次。现在历史时刻一律写 `skipped` 并附原因，`run_once` 才额外在当前时刻补一行。
12. 停用后重新启用时 `next_fire_at` 为 NULL 且不重算，定时永远不再触发。现在启用时按 `now` 重算。
13. 一次性定时的时刻已过去时，`next_fire_at` 为 NULL 但记录 `enabled=true`，接口静默返回一个永远不会响的定时。现在直接拒绝并说明原因。

---

## 10. 下一项可以开始的任务

A07 已到“测试完成”。按依赖选 **A08（持续条件、迟滞与缺测处理）**，它只依赖 A06 的判定层，且是旧“持续 N 秒”语义的直接加固：

1. **A08（下一项）**：持续条件（`for`）+ 迟滞（回差）+ 缺测（no-data）三条判定语义。
   - 现状：`for` 与 `auto_pending` 已可用，V31 输入进度屏障已挡“定时器越过未处理样本”；但**迟滞**（触发/恢复用不同阈值）与**缺测超时**（设备静默 N 分钟主动产生 unknown 判定）还没有。
   - 要做：`Definition` 增加 `hysteresis`（恢复阈值/回差）与 `no_data`（静默超时）两段；`evaluateLeaf` 在恢复方向用回差阈值；新增 `no_data` 定时扫描（复用 `auto_pending` 的到期机制与 `MisfireGrace` 式的宽限判定）。
   - 关键约束：回差只在**已触发**方向生效，不能改变“首次判定”的语义，否则旧的阈值规则会全部偏移。
2. **A02（并行）**：核对前端仓库版本，产出六类页面（全局场景联动、场景管理、设备自动化、设备告警、模板自动化、模板告警）的旧→新操作对照。
   - 入口：`/Users/junhong/Downloads/code/thingspanel-frontend-community`
3. **A07 剩余接线（阻塞在别处）**：常驻调度循环（A33 切换时接）、前端定时编辑（依赖 A02）。
4. **G7 通知联测**：`alarm.qualified/cleared` 事件契约已对齐 `notification_source.go`，真实联测仍未做，保持“待联测”。
5. **A10（代际校验）**：命令受理端接受 `X-Automation-Generation`，消掉 G4。

不建议现在动 A33 的切换接线（6.6 节），因为旧引擎还在跑，接上就是双写。

---

## 11. 六句话汇报（本轮 = A07）

1. **做了什么**：把定时从“进程内 goroutine”改成**业务数据 + 幂等 occurrence**。新增 `auto_schedules`（一条定时）与 `auto_schedule_fires`（一次应执行的时刻，`UNIQUE (schedule_id, scheduled_for)`）；新增五字段 cron 解析、IANA 时区、夏令时两条边、`skip`/`run_once` 漏跑策略；新增 8 个接口（CRUD、启停、预览、occurrence 历史），并把定时接进现有的 `POST /v1/tick` 单一遍循环。
2. **原来什么还保留**：旧“场景定时”的语义都保留了——按固定时刻执行一次、按周期执行、到点后走同一套动作与目标解析、停用即不执行、删除即不再触发。没有新建第二套动作/通知/设备命令实现，定时只负责“什么时候启动一次运行”，之后完全复用 `acceptRun` 与既有 outbox。
3. **怎么证明做对**：`encore test ./...` **118 个顶层用例（含子测试 156 条）全绿**；`encore_e2e.py` **53 项检查全通过**，其中第 12 节是真实 HTTP 的定时路径（预览→创建 6 秒后的一次性定时→tick 命中→occurrence `done` 带 `run_id`→再 tick 不再执行→停用/启用/删除）。最能说明问题的一条：`TestCrashDuringExecutionAdoptsTheExistingRun`——把 occurrence 改回“已开始但崩溃”并保留已创建的运行，下一轮只把它标 `done`，运行数与设备指令数都仍是 1。
4. **难点和性能结果**：难点是“错过”的判定。按字面“到期时间早于现在就是错过”，正常晚几秒的扫描会被记成漏跑，一次性定时几乎每次都被标 `missed`（端到端第一次跑就抓到了这个：`schedule_misfired=1` 而运行明明成功）。最终引入 `MisfireGrace = 2 分钟`，并让**同一个常量**同时决定“跑不跑”和“记不记 missed”。性能上没有压测，只有结构上的保证：到期扫描走 `next_fire_at` 的部分索引 + `LIMIT 50` + `SKIP LOCKED`，单轮不随定时总数线性增长；occurrence 查询走 `(schedule_id, scheduled_for)` 唯一索引。
5. **还有什么没完成**：常驻调度循环未接（依赖 A33 切换，现在靠 `POST /v1/tick` 驱动）；前端定时编辑未做（依赖 A02）；“立即执行一次”属 A21 不在本轮；迟滞与缺测属 A08；G1–G4、G7、G11 仍按原登记保持未完成；性能预算、迁移回退演练、kill -9 演练仍未做。
6. **下一项怎样衔接**：A08 直接落在本轮的判定层与定时机制上——`for` 复用 `auto_pending`，缺测超时可以复用刚做好的 `next_fire_at` + 租约 + 宽限判定这套模式，不需要再引入第二种定时设施；A02 页面核对可并行，产出的六类页面对照会直接告诉 A07 的定时编辑该放在哪个页面。
