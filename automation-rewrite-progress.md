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

---

## 6. 本轮验证汇总

```bash
cd encore-api
encore check 'curl /v1/health'   # check: 1/1 commands succeeded（19 endpoints registered）
encore test ./...                # automation ok / automation/domain ok
```

- `automation/domain`：30 个表驱动用例（9 比较符、四值判定、单位不匹配、事件参数、跨设备、校验门）。
- `automation`：23 个 DB 集成用例（幂等、持续条件、V31 屏障、证据过期、四种运行模式、等待续跑、分支三态、通知端口、未知不当成功、告警四条、模拟两条、目标解析三条）。
- 端到端：25 项检查全通过。

修复的真实缺陷（不是测试凑数）：

1. 设备目录 `page_size=2000` 被后端拒绝（上限 1000），且端口把 `code!=200` 当空列表 → 目标解析静默变空。已改为分页 + 校验 `code`。
2. `is_online` 是 0/1 数字而非布尔，按 bool 解析会整体失败。
3. 标签字段缺失被当成“没有匹配设备”，改为显式 `label_not_exposed`。

---

## 7. 下一项可以开始的任务

按依赖顺序，下一步是 **A02 收尾 + A05/A06 起步**，两者可并行：

1. **A02（页面与操作对照）**：核对前端仓库版本，产出六类页面的旧→新操作映射与截图证据。
   - 入口：`/Users/junhong/Downloads/code/thingspanel-frontend-community`
   - 交付：页面清单 + 每页操作对照 + 缺失能力清单。
2. **A05（事件与消费者）**：把 `auto_events` 接到通知核心与外部消费者，补“告警事件 → 通知”的真实联测（当前 G7 待联测）。
3. **A06（批量目标）**：与目录 owner 约定 `area` 成员关系、`label`、能力清单三个字段，消掉 G1/G2/G3。
4. **A10（代际校验）**：在命令受理端接受 `X-Automation-Generation`，消掉 G4。

建议先做 A05，因为它能立刻把 G7 从“待联测”变成“已联测”，是本轮唯一还挂着的跨模块验收。

---

## 8. 六句话汇报

1. **做了什么**：在 `48616009` 上开独立工作树，交付 `encore-api/`（Encore 唯一宿主，19 个接口、13 张表、单一评估器、持久运行、告警 episode、无副作用模拟、三个窄端口），并提交 `c72fdab6`。
2. **原来什么还保留**：设备命令、通知、目录、物模型、接入、JWT 鉴权全部原样复用；旧场景的 9 个比较符、TEL/ATTR/CMD 动作、组间 OR 组内 AND 语义逐项对齐，没有新建第二套。
3. **怎么证明做对**：`encore test ./...` 53 个用例全绿；`encore_e2e.py` 25 项检查全通过，其中旧库 `telemetry_set_logs` 回读到 `{"switch":1}`、`status=1`，证明新链路真的走了旧命令模块。
4. **难点和性能结果**：难点是“未知不等于假”——旧代码在读不到值时直接恢复告警，新实现把 unknown/error 提为一等值并有专门用例守住；性能只到“有观测口”阶段（`/v1/backlog`），预算表数字尚未填。
5. **还有什么没完成**：A02 六类页面证据、A05 通知真实联测、A06 目录三字段（区域/标签/能力）、A10 代际校验、A11 kill -9 演练、性能与迁移回退。
6. **下一项怎样衔接**：先做 A05，把已实现的 `alarm.qualified/cleared` 事件接到通知核心并跑真实联测，把 G7 从“待联测”转为“已联测”；同时并行 A02 页面核对，为前端切换提供对照表。
