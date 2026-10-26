# 自动化 / 场景 / 告警重写 — 第一轮执行记录

更新时间：2026-10-24
执行人：接手开发（WorkBuddy）
依据：《开发交接提示语 2026-10-23》《开发任务书 v1.1》A01–A34

---

## 1. 实际分支与提交版本

| 项 | 值 |
| --- | --- |
| 工作树 | `/Users/junhong/Downloads/code/tp-automation-rewrite`（独立 `git worktree`，不动同事的 43 个未提交修改） |
| 分支 | `feat/automation-rewrite-20261023` |
| 基线提交 | `486160092b1579965573814c8dd11a2a689c9246`（研究基线） |
| 本轮提交 | `a0b4bfd2`（A16）→ `e60ccb77`（A13）→ `9049561e`（A15）→ `63e030ec`（A17）→ `e2b76c95`（A14）→ `2ecf77cf`（A12）→ `fa5c599c`（A11）→ `09bc1a7e`（A10）→ `805b1d5e`（A02 阻塞修复）→ `256f83cb`（A09）→ `99776e10`（A08）→ `9e964641`（A07）→ `dd0ed6f9`（A06）→ `105526ec`（A05）→ `cdb0ea26` → `c72fdab6` |
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
| G11 | `auto_state` 主键是 `(device_id, signal, key)`，**不含物模型版本** | 物模型改标识符/单位后，旧值仍会命中新 key | 物模型 owner + A10 |
| G12 | 恢复条件读的是**最新存储值**，不是触发时刻的快照 | 对迟滞语义无影响，但解释链上会看到比触发更新的证据 | A08 遗留，待 A14 执行记录统一 |
| G13 | `budget.max_attempts` **未在步骤上强制** | 目前靠运行级 `deadline` 兜底；步骤 `attempt` 只计数不拒绝 | A11/A12 运行模式补强 |

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

## 9. A08 持续条件、迟滞与缺测（已完成到测试）

A07 解决“什么时候启动一次运行”，A08 解决“这次运行该不该启动、能不能算数”。旧系统的三处硬伤：持续窗口可以被一条更晚的“正常”读数从背后推翻（V31 已修）、触发与恢复用同一个阈值导致临界值抖动反复告警、设备静默后告警永远停在“触发”且没有任何提示。

### 9.1 持续条件与输入进度屏障（保留 + 加固）

`for = 5m` 的语义原样保留：第一条满足条件的样本只创建 `auto_pending`，不执行动作；到期由 `DuePending` 扫描，判定仍为 true 才启动运行。屏障（V31）继续生效：若 deadline 之前收到的样本还有未处理的，定时器**延后 2 秒**并发布 `pending.deferred`，不允许 5:00 的定时越过 4:59 的“正常”读数。

### 9.2 迟滞（回差）：中性区不产生迁移

触发与恢复用不同阈值，两者之间是**中性区**：

| 读数 | 判定 | 结果 |
| --- | --- | --- |
| > 30℃ | true | 触发（`qualified`） |
| < 28℃ | false | 恢复（`clear`） |
| 28–30℃（如 29℃） | 中性区 | 保持现状，记录 `no_transition` |

29℃ 既不触发也不解除，且会**留下一条 `no_transition` 迁移记录**——不是“什么都没发生”，而是“评估过、决定不动”。回差只在已触发方向生效，首次判定仍用原阈值，旧规则的阈值语义不会整体偏移。

### 9.3 恢复必须整段保持

恢复窗口与触发窗口对称：4 分 59 秒不清告警，第 5 分钟才清。第 4 分钟重启后，`auto_pending` 里的 `started_at`/`deadline` 不变，原窗口照常走完，**不重新计时**。反过来，一次高温读数之后设备静默，pending 到期时判定为 unknown，产生 `pending.expired` 并清掉 pending——静默不能当作“恢复正常”。

### 9.4 `freshness.mode`：周期采样 vs 仅变更上报

两种设备的“新鲜”含义完全不同，不能用同一条规则：

| 模式 | 依据 | 适用 |
| --- | --- | --- |
| `periodic`（默认，旧语义） | 值本身的年龄（`max_age`） | 周期性上报的传感器 |
| `on_change` | **值年龄不作依据**，靠 `heartbeat` 内的 status 心跳判活 | 只在变化时上报的开关/门磁 |

`on_change` 未声明 `heartbeat` 时**发布即拒绝**（`heartbeat_required`）：没有心跳窗口，“值没变”和“设备没了”长得一模一样，猜任何一个都是错的。判活读 status 信号的 `online`，`bool`/`"1"`/`"true"`/`"online"`/数字都能识别；读不到心跳判 `no_heartbeat`（unknown），不在线判 `device_offline`（unknown）。

### 9.5 `freshness.no_data`：静默是事实，不是恢复

设备静默超过 `no_data` 窗口时：

1. 对活动告警追加一条 `kind=unknown`、`evidence.code=no_data` 的迁移，带 `last_seen`、`silent_for`；
2. 发布 `alarm.no_data` 事件；
3. **绝不清告警**——设备可能只是安静，把静默当恢复是这类系统最危险的谎；
4. 每段静默只记一次（`alreadyNoted` 幂等），且不阻碍后续真实恢复：静默后再报 20℃，告警仍能正常 clear。

未声明 `no_data` 的策略完全不受影响（opt-in）。

### 9.6 单一 `evalContext()` 统一四条求值路径

此前“分支条件”只读 `max_age`，其余路径走完整新鲜度判定——同一条规则在不同位置对“数据可信”给出不同答案。本轮把上下文构造收敛到 `evalContext(ctx, def, deviceID, trigger, now)`，实时求值、恢复求值、分支条件、定时器续跑四条路径全部经它构造，规则的新鲜度契约只有一种含义。

### 9.7 证据

```bash
encore test ./automation/... -run 'Hysteresis|Recovery|Silence|NoData|OnChange|Periodic|Heartbeat|Status|Freshness|EmptyMode'
# ok  encore.app/automation / encore.app/automation/domain

encore_e2e.py --community http://127.0.0.1:19999 --automation http://127.0.0.1:14000
# == 13. duration, hysteresis and no-data (A08) ==  8 项全 PASS
```

数据库集成用例直接覆盖任务书的验收夹具：`TestHysteresisNeutralZoneRecordsNoTransition`（>30 触发 / 29 中性 / 27 清）、`TestRecoveryMustHoldForTheFullWindow`（4m59 不清、5m 清）、`TestRecoveryPendingSurvivesRestart`（重启续原窗口）、`TestSilenceAfterHighReadingDoesNotQualify`、`TestNoDataMarksActiveAlarmUnconfirmed`（静默 → `NoDataNoted=1`、告警仍 active、迁移为 unknown/`no_data`、再 tick 不重复、随后 20℃ 仍能恢复）。

### 9.8 未完成（如实登记）

| 项 | 说明 |
| --- | --- |
| 恢复条件仍读存储值（G12） | 恢复求值用 `Lookup` 读最新存储样本，而不是“触发时刻的快照”；对迟滞语义无影响，但解释链上会看到比触发更新的证据 |
| 前端缺测/迟滞配置 | 依赖 A02 页面核对；接口字段已就绪 |
| 输入键不含模型版本（G11） | `auto_state` 主键是 `(device_id, signal, key)`，物模型改标识符后旧值仍会命中 |

---

## 10. A09 有限运行、步骤记录与稳定身份（已完成到测试）

旧实现里“运行”只活在进程内：除了 `wait` 有定时器，其它位置一崩就没人知道走到哪一步；步骤行的 id 每次重新生成，重试时状态写不进去；目标行的 `status` 既表示“被选中”又表示“下发结果”，重放时把选择覆盖掉。这一章把运行变成**有检查点、有租约、有稳定身份**的有限程序。

### 10.1 运行契约：检查点 + 租约 + 恢复计数

迁移 `4_run_progress.up.sql` 给 `auto_runs` 加三列：

| 列 | 作用 |
| --- | --- |
| `cursor_pc` | **已经开始的**程序计数器。写在步骤执行**之前**，所以崩溃窗口是“这步可能做了”，由步骤/动作身份吸收；写在之后会留下“做了但指针还指着它”的窗口，重放时无法区分 |
| `lease_owner` / `lease_until` | 谁在走这个程序。租约 2 分钟，崩溃的 worker 留下**过期租约**而不是隐形锁，恢复才是可判定的 |
| `resume_count` | 这个运行被接管过几次。是“撑过了一次崩溃”的证据，不是健康指标 |

领取条件只有一条：**未结束 且 没有活着的租约**。两个副本不会走同一程序；重复唤醒也不会因为“定时器响了两次”而重跑。

### 10.2 步骤身份：id 由数据库返回

`ensureStep` → `UpsertStep`，`ON CONFLICT (run_id, idx) DO UPDATE ... RETURNING id, attempt`。

这里有个必须用 RETURNING 的理由：重启后行已存在，若调用方继续用自己新生成的 id，之后每一次 `UpdateStepStatus` 都更新 0 行——步骤永远停在 `pending`，也就是任务书禁止的“永久卡死步骤”。现在 `UpdateStepStatus` 把 0 行直接当错误返回，这类问题不会再静默。

`attempt` 在每次重入时 +1：重试同一个程序计数器是**同一个逻辑步骤的第二次尝试**，不是新步骤。`stepKey = runID:pc`，**不含 attempt**——传输重试必须复用同一身份，否则对端无法去重。

### 10.3 幂等受理与结果关联

`EnqueueOutbox` 返回 `inserted bool`。重复插入意味着“这个逻辑动作已经有行了”，按已有行的状态分流：

| 已有状态 | 处理 |
| --- | --- |
| `accepted` | 直接复用结果，**不再调用端口**——重试不会被算成第二次受理 |
| `pending` / `dispatched` | 用**同一个 id** 重试传输，对端仍可去重 |
| `cancelled` | 报 cancelled，绝不悄悄重发 |

“合法第二次执行”是另一个运行、另一个 `run_id`、另一个 key，因此照常受理——`TestALegitimateSecondRunIsNotDeduplicated` 守住这条，避免把去重做成“第二次触发丢失”。

### 10.4 目标选择与结果必须分开（本轮修掉的真实缺陷）

`auto_targets.status` 原本兼作两职。第一遍执行把 `selected` 覆写成 `accepted`，于是**重放时找不到任何已选目标**，`performAction` 走进 `empty_targets` 分支，把步骤标成 `skipped`——一个真实动作静默变成空操作。这类问题不会报错，只会“什么也没发生”。

修法是把选择冻结到独立列 `selected`（迁移里回填 `status='selected'` 的历史行），`performAction` 只看 `selected`，`status` 专职承载下发结果。接口同时暴露两个字段，运维能分清“这个设备是否属于这次运行”和“这次下发返回了什么”。

### 10.5 声明支持的步骤 = 有实现的步骤

`domain.StepImplementations` 成为唯一清单，校验与运行期都读它：

- 发布时未知步骤被拒，错误信息**列出本版本可执行的六种**（`action/wait/branch/alarm/notify/activate_scene`），作者不用猜；
- 运行期万一遇到（历史数据或手写数据）不再伪装成普通失败，而是以 `unsupported_step` 结束并说明“发布时应当被拒绝”；
- `TestEveryDeclaredStepKindHasARealImplementation` 对清单里**每一种**都造一条可发布规则并真跑，断言不出现 `unsupported_step`/`step_rejected`、步骤行存在且不停在 `pending`。清单加一种而没加实现，这条测试会直接失败。

### 10.6 过期与恢复扫描（Tick 顺序有意义）

```
DuePending → ExpireOverdueRuns → DueRunTimers → ResumableRuns → FireDue → SweepNoData → DispatchOutbox
```

过期必须排在续跑**之前**：等待与 deadline 同时到点时，若先跑定时器，运行会在“已经超预算”的情况下继续执行，而过期永远不会发生。过期时同语句清租约，并取消该运行的待触发定时与未派发意图。

恢复扫描只挑“未结束 + 租约过期或为空 + deadline 未到 + 没有待触发的 wait 定时器”的运行——**有待触发的 wait 不能被恢复**，否则会跳过等待。`POST /v1/tick` 现在返回 `runs_expired` / `runs_resumed` 两个计数。

### 10.7 证据

```bash
encore test ./automation/... -run 'Resume|Replay|Legitimate|WaitResumes|Overdue|TwoWorkers|UnsupportedStep|EveryDeclared|RunStatusIsNever'
# ok  encore.app/automation

encore_e2e.py --community http://127.0.0.1:19999 --automation http://127.0.0.1:14000
# == 14. run progress, step identity and declared steps (A09) ==  13 项全 PASS
```

最关键的三条：

- `TestResumeAfterCrashContinuesFromTheCheckpoint`：把运行改回“死在第 0 步之后、第 1 步尚未记录”的状态（`status=running`、过期租约、`cursor_pc=1`，并删掉第 1 步的行与意图），一次 tick 后运行 `succeeded`、`resume_count=1`、设备指令从 2 条变成 **3 条**（只补第 1 步，没有重放第 0 步）、两步都是终态。
- `TestReplayOfAnAcceptedActionIsNotASecondAction`：同一步骤被重新进入，端口调用数**仍是 1**、意图行数仍是 1、key 未变。
- `TestResumedStepRowKeepsItsIdentity`：重试后步骤行 id 不变、`attempt=1`、状态从 `pending` 走到终态。

### 10.8 未完成（如实登记）

| 项 | 说明 |
| --- | --- |
| 真实 `kill -9` 演练（G8） | 单测用“崩溃后留下的行状态”驱动，是同一份契约；真正的进程级演练仍待 A30/A31 的故障测试一起做 |
| 排队 TTL | `mode=queued` 的 `ttl` 目前只校验不生效，属 A11/A12 四种运行模式补强 |
| 步骤级重试上限 | `budget.max_attempts` 未在步骤上强制；目前靠运行级 deadline 兜底 |
| 常驻调度循环 | 与 A07 同一处阻塞：A33 切换时接 |

---

## 11. A10 设备控制贯通与代际校验（已完成到测试，提交 `09bc1a7e`）

### 11.1 问题：裁决点站错了位置

A09 之后，"被新触发替换掉的旧运行不该再动设备"这件事只在**自动化进程内**成立：`dispatchOne` 在调用前看一眼运行是否已取消。但取消是并发发生的——检查通过之后、HTTP 请求到达之前，新触发完全可以完成受理。此时旧命令仍然打到设备上，而运行记录写着"已取消"。G4 登记的就是这个窗口。

A10 的结论是：**裁决点必须搬到最后一道能拒绝的门上**，也就是设备命令的受理边界。搬过去之后，即使旧命令比新命令**晚到**，也必须被拒——这是 A09 那套"先到先得"逻辑做不到的。

### 11.2 三样东西：代际、幂等键、精确响应

`sql/28.sql` 新建两张表（走 `initialize/command_guard_schema.go`，复用既有的 `native_component_migration_ledger` + 咨询锁 + sha256 漂移检测，不新造一套迁移机制）：

```sql
CREATE TABLE IF NOT EXISTS automation_scope_generation (
    scope_key text PRIMARY KEY, generation bigint NOT NULL DEFAULT 0, ...);
CREATE TABLE IF NOT EXISTS automation_command_guard (
    idempotency_key text PRIMARY KEY, tenant_id text NOT NULL DEFAULT '',
    scope_key text NOT NULL DEFAULT '', generation bigint NOT NULL DEFAULT 0,
    action_kind text NOT NULL DEFAULT '', device_id text NOT NULL DEFAULT '',
    payload_hash text NOT NULL DEFAULT '', state text NOT NULL DEFAULT 'in_flight',
    http_status integer NOT NULL DEFAULT 0, response_body text NOT NULL DEFAULT '',
    accepted_ref text NOT NULL DEFAULT '', lease_until timestamptz, ...);
```

- **代际**用 `GREATEST` 单调抬升，不依赖到达顺序：`INSERT ... ON CONFLICT DO UPDATE SET generation = GREATEST(generation, excluded.generation)`。带 `X-Automation-Generation` 小于已见最高值的请求一律 409。
- **幂等键**用 `ON CONFLICT DO NOTHING` 抢占：抢到即执行，抢不到就按已有行的 `state` 分流，而不是重放副作用。
- **精确响应**存原始 status + body，回放时**原样吐出**。这一点很关键：调用方拿到的是"第一次的真实答复"，不是"我又做了一次，结果碰巧一样"。

### 11.3 四种拒绝码与自动化侧的四种后果

| 受理结果 | 拒绝码 | 自动化侧 | 为什么不是别的 |
| --- | --- | --- | --- |
| 同键同内容 | — | 回放首次响应，`accepted` | 重试不等于重做 |
| 同键异内容 | 4092 | `failed`，不重试 | 同键换内容说明调用方有 bug，重试只会更乱 |
| 代际落后 | 4091 + `X-Automation-Superseded: 1` | 运行 `cancelled`，**停止后续程序**，原因 `superseded` | 不是 failed（没出错），也不是成功（设备没动） |
| 仍在处理中 | 4093 | `dispatched`，+5s 再问 | 不能当成功，也不能当失败 |
| 租约过期仍无结论 | 4090 | `unresolved`，**永不重发** | 不知道设备收没收到，重发可能双开 |

`ports.Dispatch` 现在**先读 `X-Automation-Superseded` 头与 4090–4093**，再走原来的通用 HTTP 映射——顺序不能反，否则 4091 会被通用映射折叠成普通冲突。

### 11.4 旧调用方零改动

`Guard(db)` 在没有 `X-Idempotency-Key` 时直接 `c.Next()`，一个字节的额外逻辑都不做。控制台、App、脚本、Postman 全部走旧路径。只有自动化调用方带键，才进入这套状态机——所以"fail closed"（`db == nil` 时 503）也只针对自动化调用方，不会让别人的接口在数据库抖动时变脆。

### 11.5 本轮修掉的两个真实缺陷

1. **拒绝的命令仍然到达设备**。`TestAnOlderGenerationNeverReachesTheDevice` 报 "an older generation must be refused, got 200"。根因：gin 中间件只 `return` 不 `c.Abort()`，引擎继续往下走 handler，响应体看起来还正常——**肉眼完全看不出来**。已改为 `writeAborted`（`Abort` + 写 body + `WriteHeaderNow`），superseded 与 replay 两条分支统一走它。
2. **回放 body 为空**。`TestSameKeyAndSameContentReplaysTheFirstAnswer` 第二次拿到空串。根因：`guardRow.Response` 没有 `gorm:"column:response_body"`，gorm 按 `response` 找列找不到 → 空。这类错不会报错，只会让"精确响应回放"退化成"回放一个空 200"。

### 11.6 证据

`internal/automationguard` 11 个集成用例（`TEST_AUTOMATION_GUARD_DSN` 门控，只允许 `qa_automation_guard_test` 库，防误连）：旧调用方不受影响 / 同键同内容回放 / 同键异内容冲突 / 旧代际永不触达设备 / 同代际多个动作都放行 / 在飞不重发 / 中途死亡保持 unknown（连试 3 次仍是 unknown）/ 业务错误记 failed 且可回放 / superseded 可回放 / 无库只拒自动化调用方 / 记录带路由种类与 scope。

`automation/a10_dispatch_test.go` 7 例：superseded → run `cancelled` + step `0:action:cancelled` + outbox `cancelled`；superseded 停止后续程序；冲突记 failed 不重试；unresolved 不无限重试；在飞先问后确认；意图携带 scope/generation/key；第二次运行拿到不同的键。

---

## 12. A11 四种运行模式的边界（已完成到测试，提交 `fa5c599c`）

### 12.1 排队的顺序不能靠时间戳

`NextQueuedRun` 原本 `ORDER BY started_at`。同毫秒触发的多个运行共享时间戳，tie-break 落到随机 uuid 上——**排队顺序退化成随机**。`TestQueuedModeRunsInArrivalOrder` 直接抓到（`next was "ev-a11-queue-3"`）。改为 `ORDER BY generation, id`：generation 随每次受理单调递增，它就是到达顺序，不依赖时钟精度。

### 12.2 TTL 要在"推进时"校验，不是"入队时"

走廊的灯不该在十分钟后才亮。排队 TTL 如果只在入队时算一次，排到队首时早就超时了却还是会执行。现在 `promoteQueued` 在推进前重新校验，超时即 `queue_ttl_exceeded`，不执行、不堵后面。

`StartQueuedRun` 同时**重置运行时钟**：排队等待的时间不该吃掉执行预算，否则"排了两分钟、预算三分钟"的运行只剩一分钟干活。

### 12.3 过期原因要写明是哪只钟

`ExpireOverdueRuns` 的 `reason_code` 改读 `snapshot->>'deadline_reason'`，`reason` 用 CASE 分 `parallel_ttl_exceeded` / `queue_ttl_exceeded` / 默认。运维看到"超时"能立刻知道是并行上限还是排队上限，不用去猜。

`acceptRun` 里：parallel 取 `min(stepBudget, parallel.TTL)`；queued 取 `queue.TTL + stepBudget`。

### 12.4 证据

`automation/a11_modes_test.go` 11 例：single 记录被忽略的触发、restart 取消旧运行且新代际更高、queued 按到达顺序（直接断言 `NextQueuedRun` 而非列表序）、排队超 TTL 不迟到、排队期间停用不执行、溢出拒绝、parallel 上限、parallel TTL 过期、取消不改写已受理动作（靠 wait 步骤让运行保持 running）、device scope 各自运行、rule scope 互相竞争。

---

## 13. A12 批量目标：冻结、复核、部分失败（已完成到测试，提交 `2ecf77cf`）

### 13.1 三种"没做成"必须分开

| 情形 | 结果 | 原因码 |
| --- | --- | --- |
| 规则本来就没有目标 | `skipped` | — |
| 有目标但全部被排除 | `failed` | `all_targets_excluded` |
| 有目标但全部不可用 | `failed` | `all_targets_unavailable` |
| 部分成功、部分排除 | `partial`（新状态） | `partial_targets` |

把前三种混成一个"失败"或一个"跳过"，运维就无法区分"规则配错了"和"设备掉线了"。`partial` 是新加的运行状态，因为"20 台成功、1 台被排除"既不是成功也不是失败。

### 13.2 入场冻结 + 执行前批量复核

入场时把目标写进快照（可追溯：事后能回答"当时选了谁"）；真正执行前用**一次** `dir.ByIDs` 批量复核归属与能力。两次之间设备可能被删、被移出租户、能力被摘——复核把这类情况变成明确的 `excluded`，而不是一个设备侧的超时。

### 13.3 标签必须精确分词

`strings.Contains(d.Label, "floor-1")` 会让 `floor-1` 选中 `floor-10`——**静默驱动错误的设备**，这是最危险的一类错。新增 `ports.LabelTokens` 按 `,，;；|/` 与空格分词，HTTP 目录与测试 fake 共用同一份实现，保证"测过的语义就是跑着的语义"。

### 13.4 区域仍然显式不支持

社区版目录没有区域层级。`area` 目标类型返回明确的 `area_not_supported`，而不是空列表——另造一套资产目录是任务书明令禁止的（不新建第二套）。

### 13.5 证据

`automation/a12_targets_test.go` 7 例：入场冻结、无目标 skipped vs 全排除 failed、执行前设备消失（用 wait 开窗后复核）、20+1 台 → `partial`、标签 AND/OR、device_config 实例展开、跨租户不泄漏（报 `device_not_found`，不透露存在性）。

---

## 14. A14 有限分支与前置检查（已完成到测试，提交 `e2b76c95`）

### 14.1 互锁不该写成"分支 + 忽略失败"

旧做法要表达"只有门开着才开门"，只能写一个 `if` 分支再给动作加 `continue_on_failure`。问题在于 `continue_on_failure` 是给"失败可以容忍"用的，它不区分"条件是 false"和"条件是 unknown"——于是传感器掉线时，门照样开。

A14 把这件事独立出来：`Step.Guard *Group`。语义只有一条：**只在 `VerdictTrue` 放行**。`unknown` / `error` 一律阻断，`continue_on_failure` 不能放行 guard（这是刻意的——放行就等于把互锁变成建议）。

### 14.2 阻断是可解释的

guard 不通过时步骤记 `skipped` + verdict + evidence，运行以 `guard_blocked` 结束。运维看到的是"第 2 步的前置检查未通过，因为 CO 浓度 unknown"，而不是"运行失败"。

### 14.3 发布期就拒绝错的结构

- 分支嵌套 `maxBranchDepth = 3`，超深报 `nesting_too_deep`；
- `Guard` 挂在 `branch` / `wait` 上报 `guard_not_allowed`（这两个步骤没有副作用，挂 guard 只会让人误解语义）；
- `Guard` 非空但条件为空 → 报错，不允许"空 guard 恒真"这种默认。

### 14.4 证据

`automation/a14_branch_test.go` 6 例：四值路径各自去向、未确定**不进 else**（`branch_undetermined`）、guard 阻断（`guard_blocked` + step skipped + **0 条命令**）、guard 放行、超深嵌套发布期拒绝、wait 上挂 guard 拒绝。

---

## 15. A17 解释查询：回答"为什么没跑"（已完成到测试，提交 `63e030ec`）

### 15.1 最贵的错答

用户问"为什么没触发"，最贵的错误答案是**"条件不满足"**——而传感器其实从来没上报过。这两个结论在旧实现里长得一模一样。

`GET /v1/rules/:id/explain` 给出六值：

| 值 | 含义 |
| --- | --- |
| `fired` | 窗口内确有运行 |
| `waiting_for_duration` | 条件已成立，还在等持续时长（附剩余毫秒） |
| `condition_not_met` | 有新鲜数据，条件确实不成立 |
| `undetermined` | 有数据但证据过期/不完整 |
| `no_data` | 压根没有样本 |
| `not_fired` | 其他未触发情形 |

`no_data` 与 `undetermined` 是两个独立值，就是为了挡住上面那个错答。`evidence.complete=false` 时响应里明说证据不足。

### 15.2 解释与决策共用同一个评估器

`Explain` 复用 `domain.Evaluate`，不另写一份判定逻辑。否则解释会慢慢和真实决策漂移——那种 bug 最难查，因为两边看起来都对。

### 15.3 证据

`automation/a17_explain_test.go` 6 例：fired / no_data / condition_not_met / waiting_for_duration / 过期 → undetermined / 窗口边界。

`encore check` 端点从 28 增至 **30**（`explain` 与既有 `pending`/`inputs` 一并登记）。

---

## 16. A15 告警策略与生命周期（已完成到测试，提交 `9049561e`）

### 16.1 三件必须拆开的事

旧实现把"异常发生了""告警还在""通知发了"挤在同一行状态里，于是三件事互相污染：

1. **同一异常持续上报**会不断新建告警行（每来一条样本一条），运维看到 5 条告警，其实是 1 件事；
2. **确认（ack）**顺手把状态改成"已处理"，而异常其实还在，恢复信号永远等不到那个已经不在 active 的 episode；
3. **发布新版策略**被当成"旧告警作废"，于是改一个阈值就把正在响的告警静默清掉。

A15 的答案是：episode 的身份是 `(tenant, policy_key, subject_id, dimension)`，`policy_key` 是策略的**业务键**而不是行 id，所以发布新版（`revision` 变）不换身份；`ack` 只写确认字段，不碰 `state`；恢复只能由 `clear` 转换完成。

### 16.2 两条受理通道，一条 HTTP 摸不到

`Definition.Alarm *AlarmSpec` 显式声明 `sources`：

| 通道 | 谁能进来 | 为什么这样切 |
| --- | --- | --- |
| `detector` | 只从执行器内部的判定结果进来（`submitDetectorVerdict`） | 普通业务请求不能冒充"评估器说条件成立"——这条路径**不经 HTTP**，从结构上不可达 |
| `event` | `POST /v1/alarm-policies/:id/events`，且 `source` 必须在 `allowed_sources` 白名单内 | 外部系统要能报事件，但必须点名是谁 |

`Normalize()` 对 `Kind==KindAlarm && Alarm==nil` 给出**最严格**的默认（detector-only），而不是"全开"。`validate.go` 里：告警类型缺 `alarm` 报 `missing_alarm_spec`；非告警类型带 `alarm` 报 `alarm_spec_not_allowed`；`event` 通道没写白名单直接拒绝。

### 16.3 去重边界是 `action_id`，不是内容哈希

`alarm_event_intake` 主键 `(tenant_id, policy_key, action_id)`：

- 同 `action_id` 同内容 → 回放原结果（`Replayed=true`），不重复转换；
- 同 `action_id` 不同内容 → `action_id_content_mismatch` 显式冲突，不"以最后一条为准"；
- 受理记录先 `ReserveAlarmIntake`（`ON CONFLICT DO NOTHING` + 回读），转换完 `FinishAlarmIntake`，中途崩了下次重判而不是重放半成品。

乱序与旧执行者：`RefreshEpisode` 带 `WHERE last_seq < $2`，`AdvanceEpisodeRevision` 带 `WHERE revision < $2`，`ClearEpisode` 返回 `ClearOutOfOrder` / `ClearStaleRev` / `ClearNotActive` 而不是默默写成功。

### 16.4 一次转换 = 一条事实 + 一个事件 + 一个通知意图，同一事务

`decideInTx` 里 `publishAlarmEvent` 写 outbox 事件、`enqueueAlarmNotification` 写 outbox 通知（`IdempotencyKey = "alarm:"+transitionID+":notify"`）。三者同事务，所以不存在"告警响了但事件丢了"或"事件发了但告警没建"。

`alarm.*` 事件经 `RelayAlarmEvents` 变成 `SignalAlarm` 输入（`EventID = "alarm:"+transitionID`，`CausalHop+1`），中继游标是 `auto_events.relayed_at`。告警↔联动环靠 `MaxCausalHop=8` 截断并留 `causal.hop_exceeded` 事实。

### 16.5 缺测与等待都复用既有机制

- `Trigger.For > 0` 且无 active → `startPending` 并返回 `waiting_for_duration`，**没有为告警另建第二套等待表**；
- 缺测写 `alarm.no_data` 事件与缺测记录同事务，payload 带 `transition_id`/`policy_key`/`subject_id`；
- `unknown` / `error` 既不当作恢复也不当作确认，只记录事实（`ReasonHysteresisNeutral`）。

### 16.6 证据

`automation/a15_alarm_test.go` **13 例**：5 次上报仍是 1 条 active（`sample_count=5`，1 条 qualified 事实，1 条 `alarm.qualified`）、ack 不清告警、发新版不重复不误清、detector 策略拒绝 event 通道、事件源必须白名单、同 action 只应用一次、同 id 异内容冲突、乱序/旧执行者改不动生命周期、转换经事件出口公告、一次转换触发联动规则、联动环在跳数上限截断、unknown/error 不算恢复、不支持的 kind 被拒。

迁移 `automation/migrations/5_alarm_policy.up.sql`：补 `policy_key/level/source/first_seq/sample_count/last_evidence`，活动唯一索引改为 `(tenant_id, policy_key, subject_id, dimension) WHERE state='active'`，建 `alarm_event_intake`，`auto_events` 加 `relayed_at`。

---

## 17. A13 旧场景行为（已完成到测试，提交 `e60ccb77`）

### 17.1 旧场景不是一种东西，是两种

旧 `scenes` 表里混着两类东西，行为完全不同：

| 形态 | 是什么 | 迁移后 |
| --- | --- | --- |
| 状态场景（`state_scene`） | 一个设备的一条状态写入，按下即生效 | `KindScene`，单步，**一次性、不隐含持续监督** |
| 动作流程（`sequence`） | 多动作按顺序执行，含命令、场景激活、告警、服务调用 | `KindScene`，多步保序 |

关键点：两种形态**入口都还是那个按钮**（`KindScene`），用户的使用习惯不变；变的是内部结构可被正确执行。

### 17.2 旧告警动作必须落到现在的策略上

旧场景里的"触发告警"动作，迁移时走 `Resolver.AlarmPolicyFor(legacyID)` → `store.GetRuleByLegacyID`（`WHERE tenant_id AND legacy_id AND deleted_at IS NULL`）。查不到就报 `action_unresolved` **让迁移失败**，而不是丢动作、也不是另建一份策略——后者会造成两套告警定义同时存在。

### 17.3 环与超深链在迁移期就拒绝

`CheckCycles(root, l)` 返回**具体路径**，`MaxSceneDepth = 8`。自环、三节点环、超深链在预演阶段就报 `scene_cycle` / `scene_depth_exceeded`，不留到运行期变成递归爆栈。

### 17.4 预演是无副作用的

`POST /v1/legacy/scene-plan` 返回：将要发布的确切定义 + 逐动作映射（`StepMapping`）+ 全部 issue。`PlanLegacyScene` 不写任何表，可以先看清楚再迁移。

`ContinueOnFailure` 保留，并记入 `ContinuedSteps` 让人看见"这一步失败会被忽略"。`TypeService` 显式失败（服务动作社区版没有执行者，不假装成功）。

### 17.5 证据

`automation/legacy/scene_test.go` **13 例**：单设备状态写是 state_scene、多动作保序是 sequence、命令不算状态、config 扇出、告警必须解析到已有策略（缺失即失败）、service 动作显式失败、`continue_on_failure` 保留并报告、缺失设备报 issue 而非空目标、自环/三节点环/超深链、空场景、`Summarize`。

`automation/a13_scene_test.go` **5 例**（DB 集成）：迁移后场景保序且真的抬起告警（步骤 `0:action:accepted`、`1:alarm:succeeded`，episode `Source="event:automation"`）、同一场景跑两次只抬一次告警、状态场景一次性且无 timer/pending、告警动作解析不到则迁移失败、环在迁移前被拒。

---

## 18. A16 通知插座：交接不等于送达（已完成到测试，提交 `a0b4bfd2`）

### 18.1 一个 "ok" 撑不起四种现实

旧端口只有一个成功/失败。现实有四种：

| 值 | 含义 | 该怎么做 |
| --- | --- | --- |
| `NotificationAccepted` | 202 + body 明确 accepted | `handed_off`，**不等于终端已收到** |
| `NotificationAcceptanceUnknown` | 传输错误、body 读不出、5xx、202 但没有 accepted | 用**同一冻结意图、同一幂等键**重放 |
| `NotificationProviderUnknown` | 通知核自己也不知道（`provider_unknown` / `delivery_unknown` / 带 unknown 且无 delivery id） | **不绕过通知核盲重发**，保留 `unresolved` |
| `NotificationFailed` | 明确失败 | 记 step 失败 |

`HTTPNotificationOwner.Submit` 按这张表映射，200 不再等于"受理"。

### 18.2 正文在入队时冻结

`performNotify` 把 `occurred_at` / `expires_at` / `group_revision` 写进 outbox payload。之后改通知组、改默认策略，都**不改已经排队的这条消息**——否则"昨天那条告警为什么发给了新加的人"永远查不清。

`dispatchNotification` 只读冻结 payload，不看当前策略。

### 18.3 告警和它的通知同事务

`enqueueAlarmNotification` 在转换事务内写 outbox，键 `alarm:<transitionID>:notify`。通知所有者缺失时（`notif == nil`）→ `unresolved` 且**不失败**，告警本身保持 active——消息可以丢，告警状态不能丢。

### 18.4 证据

`automation/a16_notify_test.go` **6 例**：202+accepted 是 `handed_off` 不是 delivered（outbox `handed_off`、`accepted_ref=n-42`、step `0:notify:handed_off`）、受理响应丢失后同键同正文重放、provider unknown 三轮 tick 仍只调用 1 次且 step 记 `provider_unknown`、改通知组不重写已排队消息（仍发"原文本"、`group_revision=5`、`np-1`）、告警与通知同事务提交（`alarm:<transitionID>:notify`，tick 后 `handed_off`，告警仍 active）、通知所有者缺失时告警仍在。

---

## 19. A18 最小规则模板与版本化参数（已完成到测试，提交 `1ba7d31f`）

### 19.1 模板不是第二套实现

模板只做一件事：把一组参数**声明**成一个普通的 `domain.Definition`，然后走和手写规则完全相同的 `domain.Validate` 门。所以"从模板建的规则"和"手写的规则"在运行期是同一个东西——一个评估器、一个校验器、一个编译器。让模板成为"实例"的，是一条独立的来源记录，不是一种特殊的规则。

四个模板（就是任务书点名的那四个）：

| 模板 | 形态 | 为什么是这个形态 |
| --- | --- | --- |
| `sustained_high_temperature` 持续高温与恢复 | 告警策略（v1 / v2） | 要能被确认、能进告警列表、能追溯 |
| `door_left_open` 门未关超时 | 告警策略（v1 / v2） | 同上 |
| `device_offline` 设备离线 / 无数据 | 告警策略 | 在线状态是状态信号，不是数值 |
| `motion_then_light_off` 有人后延时关灯 | 动作流程（自动化） | 纯动作序列，没有告警生命周期 |

把前三个做成告警策略、第四个做成自动化，是刻意的：任务书明确要求"不要把所有执行结果都伪装成告警"。

### 19.2 参数契约

`Param` 有六种类型：`device` / `number` / `duration` / `enum` / `text` / `policy`。校验只有一处（`blueprint.Instantiate`），Web、CLI、MCP 都从这里拿结果，所以"同一个值在三处被同样接受或拒绝"是结构保证而不是约定。

- 默认值只放阈值和时长，**任何模板都不带设备 id、不带通知组 id**（有测试守着）；
- `duration` 必须能 `time.ParseDuration`，且 `0s` 也算非法（"等 0 秒"不是等待）；
- 未知参数直接报 `unknown_param`——CLI 打错一个字母不能变成"静默忽略你填的值"；
- 版本 revision 由声明面（key + version + params + requires）算出，不是手写常量，因此不会和表单显示的内容漂移。

### 19.3 升级只能显式发生

发布新版模板**不碰任何在用实例**。升级走 `PlanUpgrade`，规则是：

1. 只能向前；
2. 客户覆盖过的参数必须还存在——否则 `override_param_removed`，并把客户填的原值引出来；
3. 结果必须仍能通过校验——新版收窄范围时，`out_of_range` 会指名是哪个参数、允许范围从多少变成多少；
4. 客户没动过的值跟随新默认值，这正是"版本"的意义，且 diff 里明写。

`Diff` 单列 `RangesChanged`（例如 `-50..200 → -50..120`），因为范围变化才是"客户的值突然不合法"的真正原因，说"模板变了"等于没说。

### 19.4 性能与存储

实例行只存模板身份 + 参数值 + 覆盖值，**不复制 AST**；定义只存在于 `auto_rule_revisions`。一个模板被一千条规则用，就是一份 body 加一千行小记录。`auto_blueprint_instances (tenant_id, blueprint_key, blueprint_version)` 有索引，回答"这个版本影响谁"不用扫规则表。

### 19.5 能力校验：能证明才拒绝

`Requires` / `Param.Capability` 声明模板需要的字段（`telemetry:temperature` 之类），实例化时用**既有设备目录端口**查一次：

- 设备目录没接入、查询失败、或设备的能力清单为空 → `capability_unverified` **警告**，规则照建；
- 设备能力清单明确不含该字段 → `capability_missing` **拒绝**。

社区设备列表根本不暴露能力清单，所以"空"绝不能读成"没有"——那样每个模板在每台真实设备上都会被拒。

### 19.6 顺带修掉的两个真实缺陷

1. **带持续时长的告警永远不触发**。"等 `for`"这道门被写了两遍（`submitDetectorVerdict` 和 `decideQualified`），定时器在等待结束后产生的合格判定又走回第二道门，第二道门说"还没等够"，于是重新起一个 pending——每个 tick 重起一次，永远不开告警，而日志里一切正常。现在这道门只存在于入口一处，定时器走 `confirmDetectorVerdict` 直接判定。回归用例 `TestASustainedAlarmEventuallyOpens`。
2. **告警策略被迫声明一个永远不会执行的步骤**。`Validate` 对所有类型都要求至少一步，而告警策略根本没有动作可跑（`RunNow` 明确拒绝、ingest 走判定而不是开 run），作者只能加一个装饰性步骤，表单上看还像是"这一步会发消息"。现在告警类型允许零步骤。

### 19.7 证据

`automation/blueprint`（纯单元，18 例）：四个模板只要一个设备 id 就能产出可运行定义、模板不带真实设备/通知组、告警模板零步骤、动作流程是 restart 模式且三步、能力跟随所选指标、未知参数、必填缺失、数字越界、时长非法、新鲜度窗口长于等待、发新版不动在用实例、diff 如实反映默认值与范围变化、越界覆盖阻止升级、干净升级保留客户值并取新默认、不能降级、被移除的覆盖参数阻止升级、未覆盖的被移除参数不阻止、revision 由声明面推导。

`automation`（DB 集成，12 例）：四个模板带参数列出、模板产出普通规则 + 来源记录、**规则真的会跑**（一条样本不告警 → 等够时长开告警 → 降到恢复阈值自动恢复）、发新版不改在用规则、升级后重发修订并重新固定来源、越界覆盖阻止升级且不发布、dry run 不发布、手写规则没有来源也不能升级、未知设备被拒且不留规则、能力缺失才拒绝（空清单只警告）、预演不产生任何副作用、预演一次报出全部字段问题。

---

## 20. A19 设备模板关联与继承（已完成到测试，提交 `751798da`）

**问题。** 旧控制台允许把一条场景联动挂到设备模板上：条件的 `trigger_source` 填的是 `device_config_id`（条件类型 11），模板下所有设备都响应。`auto_bindings` 表在 A04 就建好了，字段齐全，但**没有任何一行代码写过它**——也就是说这项现有功能在重写里是断的。A18 的 `auto_blueprint_instances` 回答的是"这条规则来自哪个模板"，与"这条规则挂到哪些设备"是两件事，不能合并成一张表。

**设计。** 绑定 =（规则，作用域，参数值）。作用域两种：单台设备、一个模板。参数不复制进规则：正文保留 `{{name}}` 占位符，绑定提供数值。

- **参数槽**：`domain.Param` 声明 key/type/default/min/max，类型 `string|number|duration|bool`。替换发生在**序列化后的正文**上，且只改写 JSON 字符串字面量内部，所以一个参数永远不能改变文档形状（不能注入字段、不能破坏引号）。类型校验发生在写入绑定时，不是设备上报时。
- **发布期校验**：引用了未声明的占位符 → `undeclared_param`；声明了但正文没人引用 → `unused_param`（表单上有个改了没用的字段，比没有这个字段更糟）。
- **绑定键是推导出来的**：`tenant|rule|dev:<id>` 或 `tenant|rule|cfg:<id>`。所以"把规则挂到模板 T"做两次是同一个绑定，不是两条都会触发的行——这是迁移可以安全重跑的前提。
- **合并顺序就是覆盖语义**：`BindingsForDevice` 先模板行后设备行，后写覆盖前写。没有第二张"覆盖表"。
- **脱离保留证据**：`DetachBinding` 写 `enabled=false` + `detached_at`，不删行。"为什么它不跑了"和"旧模板关联数量对账"都需要这行。
- **参数冻结**：解析出的值同时写进运行快照（`binding_params`/`binding_source`）和 pending 行。等待 5 分钟的过程中有人把阈值从 30 改成 100，不能移动客户正在等的终点线。
- **候选规则索引**：`CandidateRules` 增加 `device_config_id` 参数，模板绑定也能命中；租户没有任何模板绑定时完全不查设备→模板（`HasTemplateBindings` 带缓存），上行路径对这类租户零变化。
- **两条评估路径合并**：`Ingest` 与 `RecoverAfterRestart` 原本是两个循环，崩溃恢复的那条会用**还带占位符的模板正文**去评估。现在共用 `evaluateForDevice`。

**端点（5 个，总数 39 → 44）。**

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/v1/rules/:id/bindings` | 挂到设备或模板，可带参数覆盖 |
| GET | `/v1/rules/:id/bindings` | 这条规则挂在哪，含已脱离的行 |
| DELETE | `/v1/bindings/:id` | 脱离（幂等，重复调用不报错） |
| GET | `/v1/device-automation/:deviceId` | 这台设备上真正生效的规则：模板来源、设备覆盖、按条件匹配的旧规则 |
| GET | `/v1/device-configs/:id/bindings` | 模板视角：已挂/已脱离数量 + 设备数，用于对账 |

**证据。** 新增 9 个集成用例 + 9 个 domain 用例：

- `TestATemplateRuleRunsOnEveryDeviceOfTheTemplate`——同一模板两台设备各跑一次，`scope_key` 各自独立（模板规则若共享作用域，第二台会取消第一台），快照记录 `binding_source=template` 与 `binding_params.limit=30`。
- `TestOneDevicesOverrideDoesNotChangeTheOther`——模板 30、设备二 60；40 度只触发设备一；65 度才触发设备二。
- `TestADetachedBindingStopsFutureTriggersButKeepsItsRecord`——脱离后规则本身还在（回到默认值），绑定行保留且带 `detached_at`。
- `TestBindingTheSameScopeTwiceDoesNotCreateASecondRule`——同作用域挂两次只有一行，一次上报只跑一次。
- `TestAPendingWindowKeepsTheValuesItOpenedWith`——等待期间把阈值改到 100，到点仍按 30 判定，快照里是 30。
- `TestTheDevicePageListsTemplateDeviceAndConditionRules`——同一份配置在设备页只有一种答案，且按条件生效的旧规则不会被藏起来。
- `TestTheTemplatePageCountsAttachedAndDetached`——1 已挂 / 1 已脱离 / 2 台设备。
- `TestABadParameterValueIsRefusedBeforeItIsStored`、`TestAnInvalidValueIsRefusedAtTheBindingEndpoint`——`"很热"`、`300`、拼错的参数名都在写入前被拒，且不留残行。
- domain 9 条：参数替换不改变文档形状、引号转义往返、时长参数、未声明/未使用占位符、越界值、未闭合占位符、缺值。

**没有做。** A19 只解决"绑定与继承"；换模板、删设备、模板升级后的引用失效属于 A20，本轮不做。设备目录不可用时 `deviceConfigOf` 返回空并**跳过**模板绑定（设备级绑定仍有效），这一点在 A20 的引用检查里要变成可观测的告警，而不是静默降级。

---

## 21. 本轮验证汇总

```bash
cd encore-api
encore check 'curl /v1/health'   # check: 1/1 commands succeeded（52 endpoints registered）
encore test ./...                # domain ok / ingress ok / automation ok
```

| 包 | 用例数 | 覆盖 |
| --- | --- | --- |
| `automation/domain` | 77 | 9 比较符、四值判定、单位不匹配、事件参数、跨设备、校验门、A06 对照 30 条、cron 解析与夏令时 12 条、漏跑策略 8 条、A08 新鲜度与迟滞 9 条 |
| `automation/ingress` | 12 | 六类输入 + 网关三类、稳定 ID、重投、瞬时事件、状态归一、因果跳数 |
| `automation/legacy` | 13 | A13 场景形态分类、动作保序、告警动作解析、环与超深链、`Summarize` |
| `automation/blueprint` | 18 | 四个模板、参数契约、版本 diff、显式升级、覆盖冲突、能力跟随指标 |
| `automation/bundle` | 15 | A21 资源包：哈希稳定性、元数据不入哈希、角色/插槽声明、引用闭合、凭据扫描、预览 create/update/skip、能力未证实不拒绝 |
| `automation`（DB 集成） | 193 | 幂等、持续条件、V31 屏障、证据过期、四种运行模式、等待续跑、分支三态、通知端口、未知不当成功、告警四条、模拟三条、目标解析三条、A05 七条、A06 三条、A07 十四条、A08 七条、A09 十条、A10 七条、A11 十一条、A12 七条、A13 五条、A14 六条、A15 十三条、A16 六条、A17 六条、A19 九条、A20 十二条、A21 十条、A02 五条、A25 九条 |
| `automationguard`（独立集成） | 11 | 代际拒绝、幂等回放、冲突、在飞、租约过期、旧调用方隔离 |
| **合计（顶层用例）** | **328** | 全部通过（`encore test -count=1 ./automation/...` 强制重跑无缓存） |

端点数：**52** 个（启动日志 `registered 52 API endpoints`，`encore check` 1/1 成功）。

端到端：`encore_e2e.py` **74 项检查全通过**（第 10、11 节为 A05 的真实 HTTP 适配器路径，第 12 节为 A07 定时，第 13 节为 A08 迟滞与新鲜度，第 14 节为 A09 运行进度与步骤身份）。A10–A20 的章节待补（见第 22 章）。


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
14. **目标行 `status` 兼作“是否被选中”和“下发结果”**：第一遍执行把 `selected` 覆写成 `accepted`，重放时找不到已选目标，真实动作静默变成 `skipped`。已拆出 `selected` 列。
15. **步骤行 id 每次重新生成**，`ON CONFLICT DO UPDATE` 不返回真实 id，后续状态更新命中 0 行 → 步骤永远停在 `pending`（永久卡死步骤）。已改为 `RETURNING id, attempt` 并由数据库给 id。
16. `CreateStep` 的错误被丢弃，可能在**没有步骤记录**的情况下派发设备动作（无来源动作）。现在失败即结束运行并写明原因。
17. 重放时 `EnqueueOutbox` 撞唯一索引后仍用新 id 调 `MarkOutbox`，更新 0 行，意图状态与实际脱节。现在重复插入时回读已有行并按状态分流。
18. 运行**没有执行租约**，两个 worker 可以同时走同一程序，重复副作用只能靠 outbox 唯一键兜住（表现为莫名的 `step_failed`）。现在领取是排他的，拿不到租约就什么都不做。
19. 运行预算耗尽但无人推进时永远停在 `running`，读起来像“还在跑”。现在由 `ExpireOverdueRuns` 收成 `expired` 并带 `finished_at` 与原因。
20. 未实现的步骤在运行期被当作普通失败，原因码是 `step_rejected` / “步骤执行失败”，看不出是规则问题。现在发布期就拒绝，运行期以 `unsupported_step` 明确说明。
21. **gin 中间件只 `return` 不 `c.Abort()`**：被代际校验拒绝的旧命令照样走到设备 handler 并下发，响应体还是 200 —— 这是 A10 最危险的一个，因为它**在日志里看起来完全正常**。已统一走 `writeAborted`。
22. `guardRow.Response` 未映射 `response_body` 列，gorm 按字段名找列找不到 → 幂等回放返回空 body，"精确响应回放"静默退化为"回放一个空 200"。
23. 排队序用 `ORDER BY started_at`，同毫秒触发共享时间戳，tie-break 落到随机 uuid → **排队顺序变随机**。改为 `ORDER BY generation, id`。
24. 排队 TTL 只在入队时算一次，排到队首时早已超时却仍执行（走廊灯十分钟后才亮）。改为推进时校验。
25. `StartQueuedRun` 不重置运行时钟，排队时间吃掉执行预算，运行还没干活就 `expired`。
26. 标签用 `strings.Contains` 匹配，`floor-1` 会命中 `floor-10` —— **静默驱动错误设备**，无任何报错。改为 `ports.LabelTokens` 精确分词。
27. 目标复核只在入场做一次，两次之间被删除/移出租户的设备会变成设备侧超时，原因不可读。改为执行前一次批量复核。
28. "部分目标被排除但其余成功"被记为成功，运维看不出少了一台。新增 `partial` 运行状态。
29. 互锁只能靠"分支 + `continue_on_failure`"表达，于是条件为 `unknown` 时动作照样执行 —— 传感器掉线时门照开。改为 `Step.Guard`，只在 `VerdictTrue` 放行。
30. 解释查询把"没有样本"和"条件不满足"答成同一句话，用户据此去改阈值，越改越错。拆出 `no_data` / `undetermined` 两个独立值。
31. 集成测试之间通过 worker 扫全库互相串扰：前序用例遗留的 waiting run 被后续用例的 `advance`+`tick` 唤醒，经错误的 fake owner 派发。现在每个用例 `t.Cleanup` 关掉自己的未完成工作（`closeUnfinishedWork`）。
32. **告警身份用了策略行 id**：发布新版策略会换行 id，于是每改一次阈值就把正在响的告警清掉一次、再建一条新的。改为业务键 `policy_key`，活动唯一索引随之改为 `(tenant_id, policy_key, subject_id, dimension)`。
33. **`ack` 与 `clear` 抢同一个状态字段**：确认被写成"处理完了"，异常还在但 episode 已不在 active，恢复信号无处落地。拆成确认字段与状态字段，`ack` 不再碰 `state`。
34. **告警事件入口没有通道概念**：任何带策略 id 的请求都能冒充"评估器说条件成立"。现在 `AlarmSpec.Sources` 显式声明，detector 通道不经 HTTP 可达，event 通道必须 `allowed_sources` 白名单；缺 `alarm` 声明的告警规则直接发布期拒绝。
35. **`VerdictFalse` 被路由到 `no_transition`**：恢复路径整条不走（A15 首轮 6 个用例同时挂）。"条件不成立"与"没有转换"必须分开，`false` 改为走 `TransitionClear`。
36. **通知端口只有一个成功/失败**：受理响应丢失被当成失败重发、provider unknown 被当成失败盲重发、200 被当成"已送达"。现在四态分离，202+accepted 才是 `handed_off` 且明确不等于终端已收到。
37. **通知正文在派发时才读当前策略**：改一次通知组，所有已排队的消息都改成新收件人。现在 `occurred_at`/`expires_at`/`group_revision` 在入队时冻结进 outbox payload。
39. **带持续时长的告警永远不触发**："等 `for`"这道门被写了两遍，定时器等待结束后的合格判定又走回第二道门，于是每次 tick 都重起一个 pending，告警永不打开而日志一切正常。现在这道门只在入口一处，定时器走 `confirmDetectorVerdict`。回归用例 `TestASustainedAlarmEventuallyOpens`。
40. **告警策略被迫声明一个永不执行的步骤**：`Validate` 对所有类型都要求至少一步，而告警策略没有动作可跑，作者只能加装饰性步骤，表单上还像是"会发消息"。现在告警类型允许零步骤。
41. **服务层测试共用一个租户，Tick 会扫到别的用例的遗留 pending**：一个用例留下的恢复等待窗被另一个用例的 tick 唤醒，把无关断言打挂。`closeUnfinishedWork` 现在同时清理该租户。
43. **模板绑定这条现有功能在重写里断了**：`auto_bindings` 表建好但无人写入，模板下的新设备根本不会继承任何规则，而设备页也看不出差别。现在绑定、继承、覆盖、脱离都有落点。
44. **崩溃恢复走的是另一条评估循环**：`RecoverAfterRestart` 直接拿模板正文评估，占位符原样进判定，与崩溃前的行为不一致。现在与实时路径共用 `evaluateForDevice`。
45. **等待窗不冻结参数**：`for` 窗口开到一半改了覆盖值，到点用的是新值，客户等的终点线被移动。现在参数随 pending 行与运行快照冻结。
47. **旧「激活场景」动作把场景 ID 放错字段**：迁移把场景 id 写进 `device_ref`，而校验要求 `value`、执行器读的也是 `value`。结果是每一个迁移过来的场景激活都过不了校验（`missing_scene`），即使强行入库也只会派发 `scene_id: ""` —— 一个**看起来成功、实际什么都没激活**的步骤。现在写进 `value`，并有一条用例把字段钉死。
48. **引用检查会误伤**：第一版 `ReferencingRules` 把 `identifier` 也算引用，而 `identifier` 是动作要写的遥测 key，客户用一个 uuid 形状的 key 就会让自己的规则删不掉。改为只认 `alarm_policy_id` 与 `activate_scene` 步骤的 `value`。
49. **幽灵等待窗按设备清理会误删**：一条规则失去绑定，如果按设备清 pending，同一台设备上其他仍然有效的等待窗会被一起丢掉。改为按 (设备, 规则) 精确清理。
50. **目录不可用与设备已删除不可区分**：若把目录报错当成"设备都不存在"，一次目录抖动就会把全租户的设备级绑定全部脱离。现在返回 `directory_unavailable` 且**一个字节都不改**。

46. **旧场景按表名批量改名**：状态写入和动作流程被当成同一种东西，动作顺序丢失、旧告警动作要么被丢要么另建一份策略。现在两种形态显式分类、告警动作解析到 `legacy_id` 对应策略（查不到即迁移失败）、环与超深链在预演期带路径拒绝。

---

## 22. 下一项可以开始的任务

A01–A21、A02 剩余、A25 已到"测试完成"。下一批按依赖是 **A26（`tpctl`）** 与 **A22/A23/A24（前端）**：

1. **A26 `tpctl` CLI**：把已经就绪的 52 个端点包成命令行（`rules/bindings/bundles/runs/legacy` 五组子命令），要求"命令能重放 HTTP 请求、能输出 JSON、失败退出码非零"。它依赖 A21 的导入状态机（`imports/:id` 可断点续传）与 A02 的分页参数，这两块现在都有真实实现可对。
2. **A22/A23/A24 前端**：六类页面、设备/模板/告警上下文页、执行详情与排障入口。需要先开 `thingspanel-frontend-community` 工作树；后端侧缺的是"页面要的一次性聚合读"——目前设备上下文页要打 4 个请求，是否合并成一个 `GET /v1/devices/:id/automation-context` 要在动前端之前定。
3. **A27–A34**：MCP、外部执行器合同、Node-RED/Flink 样例（A29 只做最小）、旧调用方适配、迁移回退工具、性能容量配额、验收（U01–U09、V01–V32）、切换。
4. **G7 通知联测**：`alarm.qualified/cleared` 契约已对齐 `notification_source.go`，真实联测仍未做，保持"待联测"。
5. **`encore_e2e.py` 补 A10–A25 章节**并重跑（现在停在 A09）。

不建议现在动 A33 的切换接线（5.2 节 G10 / 6.6 节），因为旧引擎还在跑，接上就是双写。

---

## 23. 六句话汇报（本轮 = A15 / A13 / A16 / A18 / A19 / A20）

1. **做了什么**：四件事，一条主线——**别把两件不同的事说成同一件**。A15 把告警从"一行状态"拆成身份 + 生命周期 + 受理通道（episode 身份是 `(tenant, policy_key, subject, dimension)`，`policy_key` 是业务键所以发新版不换身份；`ack` 只写确认字段不碰 `state`；detector 通道不经 HTTP、event 通道必须白名单；`action_id` 是去重边界）。A13 把旧场景拆成状态场景与动作流程（顺序保留、旧告警动作解析到现在的策略、环与超深链在预演期带路径拒绝、`/v1/legacy/scene-plan` 无副作用）。A16 把通知端口的"ok"拆成四态（202+accepted 才算 `handed_off` 且不等于终端已收到、受理响应丢失用同一冻结意图同键重放、provider unknown 不盲重发、正文入队即冻结）。A18 把"模板"和"规则"拆开（模板是声明、走同一个校验门；实例只存身份+参数；升级只能显式发生；四个模板里三个是告警策略、一个是动作流程）。
2. **原来什么还保留**：旧场景入口不变（两种形态都还是 `KindScene`，用户按的还是那个按钮）；`continue_on_failure` 保留并被记录；旧告警动作不另建策略，走 `legacy_id → policy_key`；通知仍交给既有通知核，没有第二套发送通道；A08 的 `for` 等待被告警复用而不是另建等待表；`ack` 不改变"异常还在"这个事实；模板实例就是普通规则，`/v1/rules` 的读、改、启停、模拟、解释、运行记录全部照旧可用；`auto_bindings` 表没被改动（留给 A19）。
3. **怎么证明做对**：`encore test -count=1 ./automation/...` **269 个顶层用例全绿**（`automation` 147、`blueprint` 18、`domain` 68、`legacy` 13、`ingress` 12、`automationguard` 11），`encore check` 1/1，**39 个端点**。最能说明问题的六条：`TestRepeatedAbnormalSamplesKeepOneActiveIdentity`（5 次上报仍是 1 条 active）、`TestAProviderUnknownOutcomeIsNotResent`（三轮 tick 仍只调用 1 次）、`TestAMigratedSceneKeepsItsOrderAndRaisesTheRealAlarm`（迁移后 `0:action:accepted` → `1:alarm:succeeded`）、`TestTheRuleFromATemplateActuallyRuns`（一条样本不告警 → 等够时长开告警 → 降到恢复阈值自动恢复）、`TestPublishingANewTemplateVersionDoesNotChangeARunningRule`（发 v2 后在用规则仍是 5m、revision 仍是 1）、`TestAnUpgradeIsBlockedWhenTheCustomersValueNoLongerFits`（150 °C 越界，升级被拒且一个字节都没发布）。
4. **难点和性能结果**：最难的三处。第一，**身份选择**——episode 用策略行 id 会让"每改一次阈值就清一次告警"，换成业务键后问题从结构上消失。第二，**同一道门写了两遍**——"等 `for`"既在入口又在判定里，于是带持续时长的告警永远不触发，而日志完全正常；这类 bug 只能靠"模板真的跑一次"才暴露，写死一个 `for=0` 的用例永远发现不了。第三，**模板升级的边界**——客户覆盖过的值不能丢、没覆盖的应跟随新默认，这两条一开始写反了（把 `cur.Values` 整体带过去），表现是"升级后默认值没变"，看起来像 diff 撒谎。性能上没有压测，只有结构保证：实例行不复制 AST、定义只在 `auto_rule_revisions`、`(tenant_id, blueprint_key, blueprint_version)` 有索引、规则与来源同事务写（1 次 begin/commit）、能力校验复用既有目录端口且只查一次。
5. **还有什么没完成**：A19–A34 全部未开始（设备模板继承、换模板失效、资源包、前端六类页面、执行详情、模拟预演、`tpctl`、MCP、外部执行器、Node-RED/Flink 样例、旧调用方适配、迁移回退、性能容量、验收、切换）；A02 的 `ListRuns` 分页与前端硬编码未改；`encore_e2e.py` 还没补 A10–A18 章节；**A16 只在端口层验证，真通知核的 202 响应体与 provider unknown 实测未做，保持"待联测"（G7）**；真实 `kill -9` 演练（G8）待 A30/A31；常驻调度循环仍阻塞在 A33；性能预算表、迁移回退演练、三个纵向业务样例仍未做；G1–G3、G5、G7、G11–G13 按原登记保持未完成。
6. **下一项怎样衔接**：A19 直接接 A18 的"来源"与 A04 的 `auto_bindings`。A18 回答的是"这条规则来自哪个模板版本"（一规则一来源），A19 回答的是"这条规则绑到哪些设备/模板，每台的覆盖值是多少"（一规则多绑定）；两张表分开正是因为一条模板规则要能服务一批设备，而每台设备的覆盖值必须独立、且改一台不能影响另一台。A18 已经把"客户覆盖值"这个概念立住了（`Overrides` + 升级时保留），A19 只是把它从"规则级"下沉到"设备级"。A19 之后必须接 A20，因为"换模板/删设备后旧规则不会偷偷继续跑"只有在绑定关系存在之后才有"引用"可查。

### A19（设备模板关联、继承与参数覆盖，提交 `751798da`）

1. **做了什么**：把"模板里配一次、新设备自动继承、单台设备只改参数"这件事真正接上。绑定是一行小记录（规则 + 作用域 + 参数值），规则正文只有一份并保留 `{{name}}` 占位符；模板作用域让新设备**不需要任何点击**就继承，设备作用域则按合并顺序覆盖模板值。脱离不删行、留日期。解析出的参数值冻结进运行快照和 pending 行。5 个端点：挂/查/脱离、设备页"真正生效的规则"、模板页对账。
2. **原来什么还保留**：旧控制台的"模板关联"语义保留——原来条件里的 `trigger_source` 填模板 id 表示"这一类设备都响应"，现在由模板作用域绑定承载，入口和按钮不变。按条件匹配任意设备的旧规则仍然生效，并且在设备页显式标成 `condition`，不会被藏起来。规则本体、修订历史、启停、模拟、解释、运行记录全部照旧；设备级绑定仍然只按 id 匹配，不依赖目录。租户没有任何模板绑定时，上行路径一次多余的目录调用都没有。
3. **怎么证明做对**：`encore test -count=1 ./automation/...` **288 个顶层用例全绿**（automation 157 / domain 77 / blueprint 18 / legacy 13 / ingress 12 / automationguard 11），`encore check` 1/1，**44 个端点**。最能说明问题的四条：`TestATemplateRuleRunsOnEveryDeviceOfTheTemplate`（同模板两台各跑一次、作用域互不干扰、快照记录来源与参数）、`TestOneDevicesOverrideDoesNotChangeTheOther`（40 度只触发设备一，65 度才触发设备二）、`TestAPendingWindowKeepsTheValuesItOpenedWith`（等待中改阈值到 100，到点仍按 30 判定）、`TestBindingTheSameScopeTwiceDoesNotCreateASecondRule`（同作用域挂两次只有一行、只跑一次）。
4. **难点和性能结果**：最难的是**参数该在什么时候填**。放在评估器里要改每一个字段的语义；放在存储里就变成每台设备一份规则副本，改一次规则要级联重写。最后选在"序列化后的正文"上替换，且只改 JSON 字符串内部——既不碰评估器，也不会让一个参数改变文档形状。第二个坑是**两条评估路径**：实时路径和崩溃恢复路径原本是两个循环，恢复的那条会拿还带占位符的正文去评估，而单测永远发现不了，因为单测不会中途杀进程。性能上没有压测，只有结构保证：每个事件**一次**候选查询 + **一次**绑定查询（不是每条规则一次）；`HasTemplateBindings` 与设备→模板都有短 TTL 缓存，写入时显式失效；绑定行很小且按 `(tenant_id, device_config_id)` 建了部分索引；规则正文不复制。
5. **还有什么没完成**：A20–A34 全部未开始（换模板/删设备/升级后的引用失效是下一个）；A02 的 `ListRuns` 分页与前端硬编码未改；设备目录不可用时模板绑定会静默跳过（设备级绑定不受影响），这条要在 A20 里变成可观测告警；`encore_e2e.py` 还没补 A10–A19 章节；A16 的真实通知核联测仍"待联测"（G7）；性能预算表、迁移回退演练、三个纵向业务样例仍未做。
6. **下一项怎样衔接**：A20 直接站在 A19 的绑定行上——"引用"第一次变成可查的数据。A19 回答"这条规则挂到哪些设备、每台用什么值"，A20 要回答"挂的东西没了怎么办"：设备被删、模板被换、模板升级后 `Binding.Revision` 指向旧版本、场景被删但还被引用。A19 已经为这些留好了位置：绑定行的 `revision` 是固定值（升级必须显式处理）、脱离保留 `detached_at`（可对账）、`DeviceConfigID` 独立成列（换模板可比较新旧）。A20 之后才能做 A21 的资源包，因为导出时必须知道"这个模板带走了哪些绑定"。

### A20（引用失效、墓碑与对账，提交 `64673598`）

1. **做了什么**：把"删了配置"和"撤回动作"彻底分开。三件事：`DELETE /v1/rules/:id` 删之前先用递归 jsonpath 查谁还引用它（`alarm_policy_id`，以及 `activate_scene` 步骤的 `value`），被引用就 412 并**点名**是谁，除非显式 `?force=true`（此时墓碑里写 `forced_with_N_references`）；`POST /v1/binding-reconcile` 报告指向已删设备的绑定（孤儿）和永远不会有结论的等待窗（幽灵 timer），默认只报告不动作；`POST /v1/rules/:id/revision-check` 在保存前回答"这次改动会不会让设备正在用的值失效"。删除本身会脱离绑定、清等待窗、取消定时、写 `delete_reason`。
2. **原来什么还保留**：删除还是软删（行在、`deleted_at` 在、`enabled=false`、`version+1`），已发生运行记录一条不动、状态不变、快照里的参数还在；脱离仍然只写 `detached_at` 不删行，所以设备页还能说"它以前由一条已删规则驱动"；没有引用时删除行为和以前完全一样，老调用方不改一行代码；告警策略与通知策略的引用不在本次范围内，行为不变。
3. **怎么证明做对**：`encore test -count=1 ./automation/...` **300 个顶层用例全绿**（automation 169 / domain 77 / blueprint 18 / legacy 13 / ingress 12 / automationguard 11），`encore check` 1/1，**46 个端点**。最能说明问题的五条：`TestDeletingARuleLeavesNoGhostTimer`（等待窗开着时删规则，再把时钟推过两个截止点，一次都没跑）、`TestDeletingAReferencedRuleIsRefusedAndNamesTheReferencer`（412 且消息里出现引用者名字，规则本体没被动过）、`TestAnotherTenantCannotDeleteThisTenantsRule`（返回 404 而不是"删掉了"）、`TestTheReconciliationRefusesToGuessWhenTheDirectoryFails`（目录报错时 `directory_unavailable`，绑定一条没被脱离）、`TestAGhostWindowIsClearedOnlyForTheRuleThatLostItsBinding`（同设备两条等待窗，只清失去绑定的那条，另一条留着）。
4. **难点和性能结果**：最难的是**"不存在"和"问不到"必须分开**。目录超时如果当成"设备都没了"，一次抖动就能把全租户的设备级绑定全部脱离，而且事后无法复原——所以目录不可用时直接返回 `directory_unavailable` 并且一个字节都不改。第二个坑是**清理范围**：按设备清 pending 会把同一台设备上其他仍然有效的等待窗一起丢掉，改成按 (设备, 规则)。第三个是**引用匹配的精度**：第一版把 `identifier` 也算引用，而它是动作要写的遥测 key，客户用一个 uuid 形状的 key 就会让自己的规则删不掉。性能上：对账是**一次**目录调用（不是每行一次），绑定查询按设备做了一次本地缓存，`auto_bindings (tenant_id, device_id) WHERE enabled` 有部分索引，引用检查是一条 `jsonb_path_exists`（GIN 索引可加在 `definition` 上，本轮未建，因为规则数量在社区版是百级）。
5. **还有什么没完成**：A21–A34 未开始；**"无法映射的新模型不自动发布"只做到一半**——`revision-check` 能拦住"新正文套不上已有绑定"，但"物模型换了、映射不出来"要靠 A21 的资源包预演和 A31 的迁移回退才闭环；换模板后的"旧关联数量对账"目前只在模板页给 attached/detached 两个数，没有逐台设备的差异视图；A02 的 `ListRuns` 分页与前端硬编码未改；`encore_e2e.py` 还没补 A10–A20 章节；G7 通知真实联测仍"待联测"。
6. **下一项怎样衔接**：A20 依赖 A19 才有"引用"可查（绑定行指向 `rule_id`），A21 依赖 A20 才有"导入前先问能不能装"的抓手——资源包导入时必然带着一堆规则之间、规则与策略之间的引用，`ReferencingRules` 与 `revision-check` 正好是它的预演器。A20 的 `delete_reason` 与 `detached_at` 也是 A21"导入不覆盖本地已改过的值"的判据：一行被本地改过或被本地删过，导入就不能悄悄改回去。

### A21（模板资源包与一键导入，提交 `881d6d7a`）

1. **做了什么**：把"一组规则"打成能装进另一套环境的包。五个端点：导出、预演、导入、导入列表、导入详情。包里规则用**业务键**（`rule:door-left-open`）而不是行 id；设备以**角色**出现（`device` / `template`），导入时映射成目标环境的真实设备或模板；通知策略以**插槽**出现（`slot:on-call`），导入方选本地策略或明确拒绝——拒绝时删掉 notify 步骤而不是留一个之后校验不过的步骤。导入是一个状态机（`staged → activating → activated|failed`），`created_rules` 是断点续传的恢复点，同一 content hash 第二次导入直接返回第一次的结果。
2. **原来什么还保留**：规则本体、修订历史、绑定、启停、模拟、解释、运行记录全部照旧；导入出来的就是普通规则，`/v1/rules` 下所有既有读改路径都能用。A18 的 `blueprint` 包不动（它是"内置模板 + 参数契约"，A21 是"用户自己的包"），A19 的绑定与覆盖不动，A20 的删除预检不动。旧的、没有角色和插槽的包仍然能导入（`TestAnOldPackageWithoutRolesOrSlotsStillImports`）。
3. **怎么证明做对**：`encore test -count=1 ./automation/...` **319 个顶层用例全绿**（automation 184 / bundle 15 / domain 77 / blueprint 18 / legacy 13 / ingress 12），`encore check` 1/1，**51 个端点**。最能说明问题的五条：`TestImportingTheSamePackageTwiceCreatesItOnce`（同包导两次只有一条规则）、`TestAFailedImportIsNotReportedAsDoneAndResumes`（中途失败状态是 `failed` 不是 `activated`，resume 续跑且不重复建）、`TestAnOptionalSlotLeftUnansweredMeansNoNotification`（拒绝插槽后步骤被删掉，不是留一个坏的）、`TestAPackageWithACredentialInAWebhookIsRefused`（正文里有 webhook 凭据直接拒）、`TestAnUnmappedRoleIsRefusedAndCreatesNothing`（角色没映射，一行都不写）。
4. **难点和性能结果**：最难的是**两种哈希不能混**。`definition_hash` 是本地形态（含本地 id 与已解析的通知目标），`bundle_hash` 是包形态；一开始拿前者当"这个包装过没"的判据，于是每次重导都像"变了"，永远拿不到 skip。现在两列分开，`updateRuleTx` 用 `COALESCE(NULLIF($8,''), bundle_hash)` 保证本地编辑不会把包形态冲掉。第二个坑是**导出被自己的校验拒绝**：模板规则的目标列表本来为空（靠绑定展开），导出时置空就过不了"至少一个目标"。做法是保留列表但内容换成角色键，导入时再回填。性能上：导出是**一次**全量规则读 + 一次绑定读（不是每条规则一次），导入的 existing 查询走 `RulesByResourceKeys` 批量，`auto_rules (tenant_id, resource_key) WHERE resource_key IS NOT NULL AND deleted_at IS NULL` 是唯一索引，所以"重复导入"是一次索引查找而不是扫描。
5. **还有什么没完成**：A22–A34 未开始；包格式只覆盖规则 + 绑定 + 通知插槽，**不含告警策略本体与设备物模型**（这两样在社区版属于别的模块，跨环境需要各自导出）；导入的并发互斥靠唯一索引 + 状态机，两个进程同时 resume 同一个 import 目前是"后到的看到 `activating` 就等"，没有分布式锁；`encore_e2e.py` 还没补 A21 章节；G7 通知真实联测仍"待联测"。
6. **下一项怎样衔接**：A21 把"跨环境搬运"做完了，A22/A23/A24 才有页面可画——导入页要的预演报告就是 `POST /v1/bundles/preview` 的响应。A25 直接复用 A21 的 `bundle.Hash` 与 `DefinitionRefs`：变更预览的"前后哈希"和"依赖存在性"与资源包是同一套口径，否则同一份正文在两个页面上会算出两个结论。

### A02 剩余（运行日志分页与过滤，提交 `fe845508`）

1. **做了什么**：把运行日志从"样本"变成"清单"。`ListRuns` 补 `limit/offset/before/after/status` 与 `total`，响应回显真实窗口；`status` 是逗号集合，拼错直接 400 并回显那个值；时间窗口限定 `started_at`，必须 RFC3339；排序改成 `(started_at DESC, id DESC)` 防止跨页重复。
2. **原来什么还保留**：不带任何查询串的旧调用仍然返回原来的形状（默认 50 条），前端不改一行代码也能用；运行详情、步骤明细、解释查询都不动。
3. **怎么证明做对**：5 条用例（`TestTheRunLogReportsATotalNotJustAPage`、`TestTheRunLogCanBeBoundedByTime`、`TestTheRunLogFiltersByStatusSet`、`TestAMisspelledStatusIsRefused`、`TestTheOldCallWithoutFiltersStillWorks`）。其中"拼错状态是 400 而不是空列表"是刻意的：空日志会被读成"没有失败"。
4. **难点和性能结果**：`count(*)` 与分页查询是两次往返，但都走 `(tenant_id, rule_id, started_at DESC)`；`runSelect` 与 `scanRuns` 抽出来共用，直接消掉了"加列漏改"这类风险——A21 加 `resource_key`/`bundle_hash` 时就因为 `GetRuleRevision` 的 SELECT 列表没跟着改而全挂过一次（`revision_missing`），同样的坑不想再踩第二次。
5. **还有什么没完成**：前端 `pop-up.vue:332` 硬编码 `enabled:'Y'` 仍在（属 A22/A23 前端范围）；日志没有"按设备过滤"的入口（`RunQuery` 已有 `DeviceID` 字段，接口没暴露）。
6. **下一项怎样衔接**：分页参数是 A26 `tpctl` 的直接消费者（`tpctl runs list --rule --status --limit`），也是 A24 执行详情页的数据源。

### A25（无副作用模拟与变更预览，提交 `f8867fd0`）

1. **做了什么**：两件事。第一，模拟真的走**编译后的程序**：同一个 PC、同一组分支目标、同一条守卫规则、同一份步骤与时长预算，`StepWait` 推进虚拟时钟，所以 `ends_at` 是"程序跑完的时刻"而不是"最后一个样本"；每个拟动作带 `would_execute` / `blocked` 与可读说明；参数按设备绑定值 `Materialize`（与执行器同一个 `bindRule`）。第二，`POST /v1/rules/:id/change-preview` 回答"这次改动会碰到什么"：作用域从定义 + 绑定展开（模板规则目标列表为空但影响 40 台也报 40 台，列表截到 50 但计数精确）、run policy、依赖存在性、绑定兼容问题、是否需要确认，并给出 `pv1:<sha256[:16]>` 令牌绑定 (rule, version, before/after hash, scope, deps, expiry)。保存时重算比对：版本变了或正文变了是 `preview_stale`，过期是 `preview_expired`。
2. **原来什么还保留**：`/v1/rules/:id/simulate` 的入参与响应字段没删（`steps`/`notes` 照旧），新增的 `actions`/`ends_at`/`side_effects` 是追加；不带 `previewToken` 的旧编辑路径完全不变（`checkPreviewToken` 见到空令牌直接放行）；`force=true` 的语义沿用 A20 的"我确认，但我不改设备上的值"。
3. **怎么证明做对**：`encore test -count=1 ./automation/...` **328 个顶层用例全绿**（automation 193 / bundle 15 / domain 77 / blueprint 18 / legacy 13 / ingress 12），`encore check` 1/1，**52 个端点**。最能说明问题的五条：`TestTheSimulationNeverCallsTheWriteExits`（命令/通知端口是"一碰就 fail"的，模拟跑完一次都没碰）、`TestTheSimulationAgreesWithTheRealEvaluation`（同一条样本，模拟说跑 1 次、真实路径也确实 1 条运行；降到阈值以下两边都是 0）、`TestTheSimulationStopsAtAGuard`（守卫不成立时第一个动作就 `blocked`，后面的泵根本不出现）、`TestASaveWithAStalePreviewIsRefused`（别人先存了一版，旧令牌被拒）、`TestASaveThatWouldOrphanDeviceValuesIsRefused`（不预览也拦得住，`force` 才放行且设备上的 55 一个字节没变）。
4. **难点和性能结果**：最难的是**"模拟"不能是第二个解释器**。第一版把步骤平铺列出，等于告诉客户"两条分支都会走"——比没有模拟更糟，因为它自信地错。改成走同一个编译产物后，又暴露出第二个问题：模拟用的是模板正文，而真实执行用的是 `bindRule` 填过参数的那份，于是 `{{limit}}` 原样进判定、`40 > "{{limit}}"` 判不出真，表现为"模拟 0 次、现实 1 次"。这条是 A19 埋下的、单测发现不了的坑，被"模拟必须和现实一致"这个用例钉住了。性能上：模拟是纯内存（一次规则读 + 每设备一次绑定读，设备数在模拟输入里是个位数量级），不写任何表；预览是 2–3 次读（规则、绑定、目录），目录故障时降级为空作用域而不是报错——编辑器不该因为目录抖动打不开。
5. **还有什么没完成**：A22–A34 未开始；模拟**不模拟时间流逝带来的外部状态变化**（比如等待期间设备离线，只按样本判定新鲜度，不模拟心跳缺失的完整语义）；预览的 `requiresConfirmation` 阈值是"设备数 > 1 或有绑定问题"，没有按租户配置；`encore_e2e.py` 还没补 A25 章节；G7 通知真实联测仍"待联测"。
6. **下一项怎样衔接**：A25 是 A22/A23/A24 三个页面的共同底座——编辑器要的"保存前先看一眼"就是 `change-preview`，排障页要的"这条规则本来会做什么"就是 `simulate`。A26 `tpctl` 把两者都包成命令（`tpctl rules preview --file`、`tpctl rules simulate --samples`），这样"页面能做的命令行也能做"，联调和自动化脚本不必依赖浏览器。A25 之后才能做 A32 的验收：U05/U06 要的"模拟无副作用"和"预览不能套用过期计划"现在有可执行的证据了。
