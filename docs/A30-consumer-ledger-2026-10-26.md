# A30 旧客户端消费者台账与兼容适配（2026-10-26）

本文件是 A30 的交付物之一。它回答两个问题：

1. **谁还在调用旧接口**——逐个入口给出仓库、文件、行号、方法与路径，而不是"应该还有 App 在用"。
2. **这些调用在换引擎之后还能不能通**——每条入口标注"已适配 / 未适配 / 无法检索"，并给出联测证据。

代码：`internal/automationcompat/`（适配器）、`cmd/automation-compat/`（独立运行，供联测）、`test/automation-rewrite/compat_smoke.py`（对真服务 41 项断言）。

---

## 1. 检索方法

三个客户端仓库都在本机，用同一组命令检索，结果可复现：

```bash
# 场景 / 场景联动 / 告警三类路径的所有调用点
grep -rnE "api/v1/(scene|scene_automations|alarm)" \
     app/api app/pages app/features \
     thingspanel-frontend-community/src \
     | grep -v node_modules

# 只找封装层（判断"有没有封装"与"有没有人用"是两件事）
grep -rnE "scene[A-Z][a-zA-Z]*\(" thingspanel-frontend-community/src/views
```

**封装层存在不等于有人调用。** 这一轮因此出现了两个相反的例子：

- 控制台 `src/service/api/automation.ts` 里 9 个 `scene*` 封装全部有页面在用；
- App 的 `api/modules/automation.js` 里 `sceneActive` 封装**没有任何页面引用**——App 的场景激活是在 `pages/automation/index.vue` 里直接拼 URL 发的。只读封装层会得出"App 不激活场景"的错误结论。

---

## 2. 消费者清单

### 2.1 App（`/Users/junhong/Downloads/code/app`，uni-app）

| # | 入口 | 文件:行 | 方法 路径 | 请求 | 客户端读什么 | 适配 |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | 仪表盘"自动化"数量 | `pages/dashboard/index.vue:182` | GET `/api/v1/scene_automations/list` | `page=1&page_size=1` | `data.total` | ✅ |
| A2 | 场景管理列表 | `pages/automation/index.vue:202` | GET `/api/v1/scene` | `page&page_size` | `data.list[]` | ✅ |
| A3 | 场景激活 | `pages/automation/index.vue:258` | POST `/api/v1/scene/active/:id` | `{}` | `code==200` → 成功提示 | ✅ |
| A4 | 联动启停 | `pages/automation/index.vue:246` | POST `/api/v1/scene_automations/switch/:id` | `{}` | `code==200` → 改本地开关 | ✅ |
| A5 | 场景编辑回填 | `pages/automation/scene.vue:157` | GET `/api/v1/scene/detail/:id` | — | `data.actions[]`（`action_type`/`action_param_type`/`action_param`/`action_value`） | ✅（部分步骤不可表示，见 4.3） |
| A6 | 场景新建/修改 | `pages/automation/scene.vue:260-266` | POST/PUT `/api/v1/scene` | 旧 scene 对象 | `code==200` | ❌ 未适配 |
| A7 | 告警列表 | `pages/alarms/index.vue:172` | GET `/api/v1/alarm/info/history` | `page&page_size` | `data.list[]`：`id`/`name`/`alarm_level`/`content`/`create_at` | ✅ |
| A8 | 告警卡（活动） | `api/modules/alarm.js:25` | GET `/api/v1/alarm/info` | — | 同上 | ✅ |
| A9 | 告警处置 | `api/modules/alarm.js:35,40` | PUT `/api/v1/alarm/info`、`/batch` | 处置结果与说明 | `code==200` | ❌ 未适配 |
| A10 | 设备详情"含告警的联动" | `api/modules/device.js:105` | GET `/api/v1/scene_automations/alarm` | `device_id` | `data.list[]` | ❌ 未适配 |

### 2.2 控制台（`/Users/junhong/Downloads/code/thingspanel-frontend-community`）

| # | 入口 | 文件:行 | 方法 路径 | 适配 |
| --- | --- | --- | --- | --- |
| W1 | 场景管理列表 | `views/automation/scene-manage/index.vue:64` | GET `/api/v1/scene` | ✅ |
| W2 | 场景激活 | `views/automation/scene-manage/index.vue:48` | POST `/api/v1/scene/active/:id` | ✅ |
| W3 | 场景日志 | `views/automation/scene-manage/index.vue:91` | GET `/api/v1/scene/log` | ⚠️ 见 4.4 |
| W4 | 场景删除 | `views/automation/scene-manage/index.vue:109` | DELETE `/api/v1/scene/:id` | ❌ 未适配 |
| W5 | 场景编辑回填 | `views/automation/scene-edit/index.vue:502` | GET `/api/v1/scene/detail/:id` | ✅ |
| W6 | 场景新建/修改 | `views/automation/scene-edit/index.vue:485,491` | POST/PUT `/api/v1/scene` | ❌ 未适配 |
| W7 | 联动详情 | `views/automation/linkage-edit/index.vue:140` | GET `/api/v1/scene_automations/detail/:id` | ✅ |
| W8 | 联动新建/修改 | `views/automation/linkage-edit/index.vue:101,113` | POST/PUT `/api/v1/scene_automations` | ❌ 未适配 |
| W9 | 设备详情"含告警的联动" | `service/api/device.ts:116` | GET `/api/v1/scene_automations/alarm` | ❌ 未适配 |

### 2.3 ThingsVis

ThingsVis 以 iframe / SDK 形式嵌在控制台里（`src/utils/thingsvis/*`），**源码不在本仓库**，检索不到它自己发的请求。因此：

- 归为"**未找到调用**"，不是"已证明无调用"；
- 适配器**不删任何入口**，并允许它通过 `X-TP-Client: thingsvis` 头把自己单独计数；
- 没有这个头时它落在 `web` 桶里，会让"控制台还在用"这个结论偏保守——**这个方向的误差是安全的**：它只会推迟删除兼容入口，不会提前删。

### 2.4 客户脚本 / 未知客户端

同一类：可能是脚本、网关、监控探针。没有 UA 的记为 `unknown`，有 UA 但不像浏览器的记为 `script`。这两个桶**永远单独统计，不与 web/app 合并**——空桶是证据，清零是猜测。

---

## 3. 兼容层做了什么

代码在 `internal/automationcompat/`，挂载方式见 `routes.go`。

**它只翻译合同，不重跑业务。** 每个 handler 最终落到新服务的四个端点之一：`GET /v1/legacy-refs/:id`（旧 id 解析）、`GET /v1/rules/:id`、`POST /v1/rules/:id/run`、`POST /v1/rules/:id/enabled`。适配器里没有第二个评估器、第二个调度器、第二个命令下发器——两条路径能推动同一台设备，是 A28 存在的理由。

### 3.1 旧 id → 新规则

`auto_rules.legacy_id` 已经是迁移写入的映射。适配器通过 `GET /v1/legacy-refs/:legacyId` 解析，返回里带 `ambiguous` 与 `candidates`：

- 一个旧 id 对应多条新规则时**拒绝**（`100461`），而不是取排序第一条。静默取第一条会把"打开门厅的灯"变成"跑了另一条规则"。
- 解析不到时回 `100404` + `reason_code=legacy_ref_missing`，**不是**静默成功。

### 3.2 响应信封

旧合同是**失败也回 HTTP 200**，业务码在 `code` 里。App 只看 `res.code`，控制台的 axios 把非 2xx 当传输错误并重试。所以适配器一律 `200 + {code,message,data}`，并在响应头带：

```
Deprecation: true
Sunset: Sat, 01 May 2027 00:00:00 GMT
Warning: 299 - "automation compatibility adapter, ends 2027-04-30"
```

### 3.3 适配器自己的业务码

| code | 含义 | 调用方该做什么 |
| --- | --- | --- |
| `100404` | 旧 id 还没迁移（`legacy_ref_missing`） | 等迁移，不是重试 |
| `100460` | 对象已迁移，旧路径只读 | 去控制台改 |
| `100461` | 旧 id 对应多条新规则（`ambiguous_legacy_ref`） | 人工确认映射 |
| `100462` | 兼容期已过（`compat_sunset`） | 升级客户端 |
| `100463` | 新服务拒绝或不可达（`upstream_*`） | 稍后重试 |
| `100464` | 运行建了但**没执行**（`run_rejected` 等） | 修规则或设备 |

### 3.4 可观测调用量

`GET /api/v1/compat/stats`（JSON）与 `GET /api/v1/compat/metrics`（Prometheus 文本）：

```
automation_compat_calls_total{consumer="app",endpoint="scene.list",outcome="ok"} 12
automation_compat_last_call_timestamp_seconds{consumer="app",endpoint="scene.list",outcome="ok"} 1793...
automation_compat_sunset_timestamp_seconds 1814...
```

`outcome` 分 `ok` / `refused` / `not_found` / `error`。把 `refused` 和 `error` 分开是刻意的：前者是"客户撞上了限制"，后者是"我们的上游挂了"，处置方式完全不同。

---

## 4. 这一轮发现的真问题

### 4.1 App 仪表盘上的自动化数量永远是 1

`GET /v1/rules` 的 `total` 原来是 `len(本页)`。App 仪表盘用 `page_size=1` 只是为了拿个数字，于是每个租户都显示 1。已改成真实计数（`store.CountRules`，与列表共用同一段 WHERE，避免"页"和"数"对不上）。

### 4.2 激活被拒绝时回 200，App 弹"成功"

运行被拒（设备不在目录、规则停用、作用域被占）时，`RunNow` 返回的是一条 `status=rejected` 的运行记录，HTTP 仍是 200。适配器原样转发 → App 弹"成功"，而设备根本没动。这正是 A01 台账 D6（假成功）的翻版，只不过这次发生在兼容层。

现在只有 `succeeded/accepted/running/queued` 回 200，其余回 `100464` 并说明原因。联测里这一条是**真的**：环境没有设备目录，运行确实是 `rejected`，适配器也确实没让它变成成功。

### 4.3 旧编辑器无法表示等待与分支

新定义的 `wait`/`branch` 在旧动作列表里没有对应形状。适配器**不伪造**：能表示的列进 `actions`，不能表示的写进 `compat_warnings` 并说明第几步。

这条与写路径未适配（A6/W6）叠加时才是安全的：App 打开一个含等待的场景，看到的动作列表是短的；但它保存走的是**旧的写接口**，不会把新规则改短。接管时必须同时处理这两件事，否则"打开→保存"会真的丢步骤。

### 4.4 `/scene/log` 与 `/scene_automations/log` 是两条路径

旧系统里"场景"和"场景联动"各有自己的日志接口。适配器把两条都指向 `GET /v1/rules/:id/runs`，但 `/scene/log` 没有单独的 handler（W3 仍由旧实现服务）。接管时需要补，或者确认控制台场景页的日志按钮在接管后不再被使用。

### 4.5 旧 `execution_result` 只有 S/F，新状态有十个

映射：`succeeded→S`、`failed→F`、其余→`U`（旧客户端只是渲染这一行，不对它做分支）。**不把 `running`/`rejected` 折成 S**——那等于把"还没确认"写成"成功"。

### 4.6 告警状态不是布尔

旧 `alarm_status` 是 `L/M/H`（活动）与 `N`（恢复），**不是 Y/N**。写错会让 App 把每条告警渲染成中性色。适配器按 `state` 与 `level` 分别填 `alarm_status` 与 `alarm_level`，并有专门用例锁住。

---

## 5. 联测证据

`test/automation-rewrite/compat_smoke.py`，对真服务（`encore run` + `cmd/automation-compat`）**41/41**：

```
== 3. App：仪表盘数量（page_size=1，读 total）     ok  total=11 list=1
== 4. App：场景详情（旧编辑器读 actions）           ok  action_type=10 action_param=switch
== 5. App / 面板：激活场景（拒绝时不许说成功）       ok  code=100464 reason=run_rejected
== 6. App：启停开关（先读后写，带版本）             ok  enabled=N，新服务里确实被关掉
== 7. App：执行记录                                 ok  1 行，run=rejected log=U
== 8. App：告警卡与历史                             ok  alarm_status=H，name=高温告警…
== 9. 面板：场景列表                                ok  四个渲染字段齐全
== 10. 没迁移的旧对象                               ok  100404 legacy_ref_missing
== 11. 调用量按客户端归集                           ok  app/web 分开计数
```

Go 用例（`internal/automationcompat/*_test.go`）**37 项**覆盖翻译与拒绝：`TestAnActivationThatDidNotRunIsNotASuccess`、`TestTheAppDashboardGetsTheRealTotal`、`TestAnAmbiguousLegacyIdIsRefusedAndNothingRuns`、`TestTheAdapterNeverInventsATenant`、`TestAlarmStatusIsNotABoolean`、`TestTheSunsetRefusesWithoutTouchingTheCore` 等。

**没证明的部分，明说：**

- 成功激活的完整链路没有在真环境跑过——开发环境没有设备目录，运行必然 `rejected`。成功路径由 Go 用例（假上游）覆盖。
- Node-RED / ThingsVis / 客户脚本的真实请求体没有抓到，只有控制台与 App 的源码。
- App 的告警处置（A9）、场景保存（A6）、控制台的场景/联动保存（W6/W8）都还是旧实现，接管前必须处理。

---

## 6. 接管步骤（A33 执行，A30 不做）

现在只挂**影子路径** `/api/v1/compat/*`，旧引擎继续服务真实路径，所以任何设备都不会被执行两次。接管时：

1. 删掉 `router_init.go` 里 `apps.Model.Scene.Init(v1)` 的注册，同时用 `automationcompat.Mount(v1, h)` 挂到真实路径。两者必须同一个提交——否则 Gin 会因为路由重复 panic。
2. 处理写路径（A6/W4/W6/W8）：要么适配到新规则（`POST/PUT /v1/rules`），要么在对象已迁移时回 `100460` 只读。**不能留成"写进旧表、没人读"**——那是静默丢数据。
3. 补 `/scene/log` 与 `/scene_automations/alarm` 两条读路径。
4. 告警处置（A9）映射到 `POST /v1/alarms/:id/ack`。
5. 把 `AUTOMATION_COMPAT_MODE` 从 `shadow` 改成 `takeover`，观察 7 天的 `automation_compat_calls_total`。

---

## 7. 退出条件（可测量）

| 消费者 | 退出条件 |
| --- | --- |
| `app` | 连续 2 个发布周期 `automation_compat_calls_total{consumer="app"}` 为 0，且 App 商店里最低支持版本已带新接口 |
| `web` | 控制台自身升级完成后为 0（控制台与新接口同仓库，理论上可以到 0） |
| `thingsvis` | 需要先拿到 ThingsVis 的发布节奏；**在拿到之前不设退出日期** |
| `script` / `unknown` | 只能靠 `Sunset` 到期强制；到期前 30 天在响应头 `Warning` 里提示 |

`sunset` 是**必填**的：`ConfigFromEnv` 在没有 `AUTOMATION_COMPAT_SUNSET` 时拒绝启动。没有到期日的兼容层就是第二套实现，而第二套实现没有人删。

---

## 8. 与任务书完成标准的对应

- [x] 每种已存在客户端入口有调用检索和联测证据 —— 见第 2、5 节；App 与面板逐入口给到文件行号，ThingsVis 明确标为"无法检索"。
- [x] 旧详情链接按租户正确打开 —— `GET /scene/detail/:id`、`/scene_automations/detail/:id`、`/alarm/info/history/:id` 三个入口；租户由**调用方自己的 token** 决定，适配器不计算租户（`TestTheAdapterNeverInventsATenant`）。
- [x] 未知外部消费者未清零时不删除对应兼容入口 —— `unknown`/`script` 独立计数，第 7 节的退出条件里它们只有 `Sunset` 一条路。
