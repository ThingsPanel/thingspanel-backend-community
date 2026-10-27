# 自动化重写：合并入口

> 这份文件是给执行合并的人看的**唯一入口**。读完它不需要再翻别的文档。
> 最后更新：2026-10-28

---

## 0. 先纠正一个容易误解的点

**不是"整个后端都换成 Encore 了"。**

这个仓库里现在有**两个可以独立启动的进程**：

| | 进程 | 模块 | 职责 | 端口 |
|---|---|---|---|---|
| A | 社区后端（原有，Gin） | `module project`（仓库根目录） | 设备、用户、租户、MQTT 接入、物模型 | 由 `GOTP_SERVICE_HTTP_PORT` 决定 |
| B | 自动化服务（新增，Encore） | `module encore.app`（`encore-api/`） | 规则、场景、联动、告警、执行记录 | Encore 分配（本地 `encore run --port`） |

两者关系：**A 收到设备上报后，把上报 HTTP 转发给 B；B 判定命中后，再通过 A 已有的命令接口下发。**
命令下发、通知、设备目录都还是走原来的模块，没有第二套实现。

**两套数据库**：A 用原来的 ThingsPanel 库；B 用 Encore 自己托管的 `automation` 库（16 个迁移跟着服务走，可独立部署和回滚）。

所以合并后**部署单元从 1 个变成 2 个**，这是这次合并最需要提前和运维对齐的一件事。

---

## 1. 要合并什么

### 后端

- 仓库：`https://github.com/ThingsPanel/thingspanel-backend-community.git`
- 分支：`feat/automation-rewrite-20261023`
- HEAD：`5697a462`
- 相对 `origin/main`：**113 个提交**（A01–A32 全套自动化重写）
- 远端状态：**已推送**，`git ls-remote` 可见

### 前端

- 仓库：`https://github.com/ThingsPanel/thingspanel-frontend-community.git`
- 分支：`feat/automation-rewrite-20261023`
- 相对 `origin/master`：**4 个提交**（A22/A23/A24 页面、A25 接新引擎、两笔显示修复）
- 远端状态：见前端仓库同名的 `MERGE-AUTOMATION-REWRITE.md`

### 合并顺序

1. 先合后端 —— Encore 服务是规则的新 owner，前端要调它的接口
2. 再合前端
3. 部署时两个进程都要起来，缺一个页面会全部报错

---

## 2. 后端怎么启动

### A. 社区后端（原有方式，不变）

```bash
go build -o tp-backend .
./tp-backend          # 配置仍走 configs/conf.yml，可用 GOTP_* 覆盖
```

### B. 自动化服务（Encore）

```bash
cd encore-api
go mod tidy           # 首次必须，否则 encore run 会卡在联网下载
encore run --port=14000 --browser=never
```

生产环境用 Encore 的构建产物（`encore build docker`），不要直接 `encore run`。

---

## 3. 环境变量清单

### 社区后端（进程 A）新增

| 变量 | 必填 | 说明 |
|---|---|---|
| `GOTP_AUTOMATION_MODE` | 否 | `legacy`（默认）或 `native`。**不填 = 保持原有行为** |
| `GOTP_AUTOMATION_BASE_URL` | `native` 时必填 | 自动化服务地址，如 `http://automation:14000` |
| `GOTP_AUTOMATION_TOKEN` | `native` 时必填 | 调自动化服务的令牌 |
| `GOTP_AUTOMATION_TIMEOUT_MS` | 否 | 默认 5000 |
| `GOTP_AUTOMATION_QUEUE` | 否 | 转发队列长度，默认 1024 |
| `TP_THINGMODEL_NATIVE_URL` | 用到原生模型时必填 | 原生物模型服务地址 |

> `native` 但没给 `BASE_URL` 时，进程会记一条 error 并**回退到内置引擎**，不会静默丢上报。

### 自动化服务（进程 B）

| 变量 | 必填 | 说明 |
|---|---|---|
| `TP_AUTH_MODE` | 是 | `legacy-jwt`（生产）/ `dev` / `passthrough`（后两个禁止上生产，启动会打 WARN） |
| `TP_JWT_KEY` | `legacy-jwt` 时必填 | 与社区后端同一个签名密钥 |
| `TP_COMMUNITY_SESSION_BASE_URL` | 是 | 社区后端地址，用于会话校验（走 `/api/v1/user/detail`） |
| `TP_COMMUNITY_BASE_URL` | 是 | 社区后端地址，用于读设备目录 |
| `TP_COMMUNITY_TOKEN` | 是 | 服务间调用令牌 |
| `TP_NOTIFICATION_URL` | 否 | 通知服务地址；不配则通知记为"未知"，不会假装成功 |
| `TP_NOTIFICATION_TOKEN` | 否 | 同上 |
| `TP_SOURCE_DEPLOYMENT_ID` | 否 | 默认 `automation-rewrite`，用于区分来源 |

**已知坑**：社区版 `/api/v1/user/detail` 不返回 `tenant_id`，会话校验会失败。要么用返回 `tenant_id` 的后端版本，要么在 `authn` 里放宽。这是合并时**必须确认**的一条。

---

## 4. 数据库

- 社区后端：**不动**，没有新增迁移。**但已有环境要补一条菜单记录**，见下。
- 自动化服务：Encore 托管的 `automation` 库，迁移在 `encore-api/automation/migrations/`（16 个文件，`1_init` 到 `16_migration_item_enabled`）。Encore 部署时自动执行，**不要手工跑**。
- 切回旧引擎：`GOTP_AUTOMATION_MODE=legacy` 即可，`automation` 库留着不影响原有功能。

### 4.1 已有环境必须补一条菜单记录

新装环境会由 `sql/1.sql` 带上，**已经跑过初始化的库不会重跑**，所以要手工执行一次：

```sql
INSERT INTO public.sys_ui_elements
  (id, parent_id, element_code, element_type, orders, param1, param2, param3,
   authority, description, created_at, remark, multilingual, route_path)
VALUES
  ('4b1f2c7e-8a3d-4e15-9c62-0d7a5b3f8e21', '676e8f33-875a-0473-e9ca-c82fd09fef57',
   'automation_rule-edit', 3, 1144, '/automation/rule-edit', '', '1',
   '["TENANT_ADMIN","SYS_ADMIN"]'::json, '规则编辑', now(), '',
   'route.automation_rule-edit', 'view.automation_rule-edit')
ON CONFLICT (id) DO NOTHING;
```

**为什么需要**：前端是动态路由，路由由这张表下发。规则编辑页是设备详情 / 设备配置详情里"生效规则"点进来的落点，缺了这条记录，客户从设备详情点某条规则会直接看到 404。`param3='1'` 表示不在侧栏显示——它不该是一个能独立进入的页面。

`parent_id` 是"自动化"目录的 id，如果你们的库里这个 id 不一样，按 `element_code='automation'` 查一下替换。

---

## 5. 合并后怎么验证（10 分钟）

```bash
# 1. 两个进程都活着
curl -s -o /dev/null -w "community=%{http_code}\n" "http://<community>/api/v1/device?page=1&page_size=1" -H "x-token: <token>"
curl -s -o /dev/null -w "automation=%{http_code}\n" "http://<automation>/v1/health"

# 2. 规则能读出来
curl -s "http://<automation>/v1/rules?limit=5" -H "Authorization: Bearer <token>"

# 3. 关键：一条上报能打通到底
#    往 MQTT 发一条遥测，然后看两件事：
#    a) 设备命令主题上有没有收到命令
#    b) 运行记录的 status 是不是 succeeded
curl -s "http://<automation>/v1/rules/<rule-id>/runs?limit=1" -H "Authorization: Bearer <token>"
```

**第 3 步是唯一能证明"真的通了"的检查。** 只看页面能打开、接口返回 200 都不算。

页面侧要点一遍：场景联动（列表/启停）、场景管理、新建场景（选设备能列出真实设备）、告警信息（详情弹窗状态不是"所有状态"）。

---

## 6. 已知限制（合并前请知情）

| # | 问题 | 影响 | 状态 |
|---|---|---|---|
| 1 | 物模型服务（原生物模型）不可用时，"操作设备"动作选不出属性 | 只影响新建动作时选属性，已有规则照常跑 | 依赖外部服务，不在本次范围 |
| 2 | 独立的"规则列表"页已下线，规则统一从"场景联动"进入 | 无 | 本次已处理 |
| 3 | 单模式规则在突发流量下会"忙时忽略"（`mode_single_busy`） | 设计行为，防命令风暴，不是失败 | 符合预期 |
| 4 | 通知（G7）未做真实端到端联测 | 通知链路只有接口级证据 | 待补 |
| 5 | A33 真实部署验收（U01–U09、V01–V32）未执行 | 交付前的正式验收缺口 | 待补 |

---

## 7. 本次改动里最值得看的两处

如果合并时只想看关键 diff，看这两笔：

1. **`87e6aef7`** 上报链路按开关切换新引擎。
   旧引擎和新引擎此前是两条互不相干的路径。要切就必须二选一，否则一条遥测会触发两次真实下发，设备收到两遍命令。
   用一个值决定（`GOTP_AUTOMATION_MODE`），默认 `legacy`，不填等于维持现状。
   转发走有界队列 + 4 worker，队列满则丢弃并计数——**慢引擎的代价是丢自动化事件，不是拖慢遥测**。

2. **`5697a462`** 命令受理守卫移到响应中间件之外。
   守卫原来注册在 `handler.Middleware()` 里面，它自己写响应头后，响应中间件跳过加成功包，调用方拿到**空体 200**；自动化服务把空体读成 `code 0`，于是每条命令都记 `command_rejected`——**而设备其实已经动作了**。
   症状极隐蔽：MQTT 上命令确实到了，运行记录却是失败。已补回归测试锁住中间件顺序。

---

## 8. 测试证据

- `encore test ./automation/...`：automation / blueprint / bundle / domain / executor / ingress / legacy 七个包全 ok
- `go test ./internal/automationguard/`：12 条（含响应包回归 `TestTheResponseEnvelopeSurvivesTheGuard`）
- `go test ./internal/uplink/`：9 条（native 切换，含"native 无地址必须回退"）
- `go build ./...` / `go vet`：干净
- 真实端到端：MQTT 上报 → 规则命中 → 命令到达设备主题 → 运行记录 `succeeded`
- 压测：30 条间隔 0.1s 全部成功；100 条无间隔突发 68 条执行、32 条 `mode_single_busy`、转发队列丢弃 0

---

## 9. 有疑问找谁

合并过程中如果遇到这份文件没覆盖的情况，优先看 `docs/` 下的进度记录（A01–A32 每项都有六句话说明和当轮缺陷清单）。
