# 交接单：自动化重写合并到 1.3 分支

> 给执行合并的人。照这份做，不需要再看别的文档。
> 出具时间：2026-10-28 ｜ 出具人：开发（自动化重写）

---

## 一、一句话说清这件事

两个仓库各有一个 `feat/automation-rewrite-20261023` 分支，**已经推到 GitHub**，
要合到各自的 **`main-1.3.0`** 分支。合并冲突很少（后端 3 个、前端 2 个），
**下面每个冲突都给了解法，其中后端 3 个和前端 2 个我都实际预演过**。

⚠️ **最重要的一条：这不是"整个后端换成 Encore"。**
合并后部署单元从 1 个变 2 个，多一个 Encore 服务进程和一套库。
这一点必须先和运维对齐，否则上线会少起一个进程，页面全部报错。

---

## 二、合并对象

| | 后端 | 前端 |
|---|---|---|
| 仓库 | `ThingsPanel/thingspanel-backend-community` | `ThingsPanel/thingspanel-frontend-community` |
| 源分支 | `feat/automation-rewrite-20261023` | `feat/automation-rewrite-20261023` |
| 源 HEAD | `4e15c11e` | `436b2d83` |
| 目标分支 | `main-1.3.0` | `main-1.3.0` |
| 相对 main/master 的提交 | 113 个 | 4 个 |
| 分叉点（merge-base） | `48616009` | `c6968c77` |

两个源分支都已推送，`git ls-remote --heads origin feat/automation-rewrite-20261023` 可验证。

**合并顺序：先后端，再前端。** 前端调的是后端新起的 Encore 服务。

---

## 三、合并步骤

### 3.1 后端

```bash
git clone https://github.com/ThingsPanel/thingspanel-backend-community.git
cd thingspanel-backend-community
git checkout main-1.3.0
git pull

git merge --no-ff feat/automation-rewrite-20261023
# 预期：3 个冲突，全部是 add/add，全部在 encore-api/ 下
```

### 3.2 前端

```bash
git clone https://github.com/ThingsPanel/thingspanel-frontend-community.git
cd thingspanel-frontend-community
git checkout main-1.3.0
git pull

git merge --no-ff feat/automation-rewrite-20261023
# 预期：2 个冲突
```

---

## 四、冲突解法（核心，按这个做就行）

### 4.1 后端：3 个冲突，全部取 1.3.0 的版本

冲突文件：

```
encore-api/go.mod
encore-api/go.sum
encore-api/l1-access/authn/auth.go
```

**解法：三个都取 1.3.0 那一侧（ours）。**

```bash
git checkout --ours encore-api/go.mod encore-api/go.sum encore-api/l1-access/authn/auth.go
git add encore-api/go.mod encore-api/go.sum encore-api/l1-access/authn/auth.go
cd encore-api && go mod tidy && cd ..
```

**为什么取 1.3.0 而不是合并：**

1. **`authn/auth.go` 两边的函数签名根本不同，不能文本合并。**
   - 1.3.0：`func AuthHandler(ctx context.Context, params *AuthParams)` —— Encore 原生 auth handler
   - 自动化分支：`func AuthHandler(ctx context.Context, token string)` —— 旧的自定义中间件写法
   - 而且 1.3.0 把会话校验拆到了单独的 `community_session.go`（106 行），自动化分支是写在 auth.go 里的。
   两份是同一件事的两种写法，选 1.3.0 的（更新、符合 Encore 规范）。

2. **`go.mod` / `go.sum`：1.3.0 是超集。**
   1.3.0 有 casbin、go-oidc、paho.mqtt、golang-jwt、pgx v5.7.2、go-redis v9 等；
   自动化分支只多了一个 pgx v5.2.0（旧版本）。取 1.3.0 后再 `go mod tidy` 补齐即可。

**已实际验证**：按上述方式解决后，`encore-api` 目录 `go build ./...` **编译通过**，
说明自动化服务的代码与 1.3.0 的 authn 实现兼容，不需要改 automation 包。
仓库根目录（社区后端）`go build ./...` 同样**编译通过**。

### 4.2 前端：2 个冲突，一个取我方、一个手工

#### ① `src/views/automation/linkage-edit/modules/edit-action.vue` —— 取自动化分支

```bash
git checkout --theirs src/views/automation/linkage-edit/modules/edit-action.vue
git add src/views/automation/linkage-edit/modules/edit-action.vue
```

原因：1.3.0 这一侧是空的，自动化分支在这里加了 `placeholderMap`（动作值输入框的示例值）。
取我方即可，没有需要保留的对方内容。

#### ② `src/views/alarm/warning-message/components/pop-up.vue` —— **不能机械解决，按下面三步**

这个文件是唯一需要动脑子的地方。背景：

- 1.3.0 在这一版里**新增了"租户默认策略"选择器**（+143 / −80 行），是别人做的功能，**不能丢**。
- 自动化分支在这一版里只改了 6 行：把告警接口从旧 `@/service/api/alarm` 换成新引擎的 `@/service/api/alarm-native`。

**第一步：以 1.3.0 为基线**

```bash
git checkout --ours src/views/alarm/warning-message/components/pop-up.vue
```

**第二步：改 import（第 13 行附近）**

```diff
- import { addWarningMessage, editInfo } from '@/service/api/alarm'
+ import { addWarningMessage, editInfo } from '@/service/api/alarm-native'
```

**第三步：改返回值判断（约第 313 行，`addWarningMessage` 调用处）**

```diff
  const res = await addWarningMessage(data)
- if (res) {
+ if (res.data) {
```

**这一步千万不能漏。** 两个接口的返回形状不同：

- 旧 `alarm.ts` 的 `addWarningMessage` 返回裸对象，`if (res)` 判断的是"有没有返回"。
- 新 `alarm-native.ts` 返回 `{ data, error }`（`FlatResult`）。**`res` 永远是非空对象，
  `if (res)` 永远为真** —— 不改的话，添加失败也会弹"添加成功"。

`editInfo` 那边是 `const { data } = await editInfo(datas)`，`FlatResult` 本身有 `data` 字段，
所以不用改，但合并后要确认它确实是解构 `data` 而不是整个对象。

```bash
git add src/views/alarm/warning-message/components/pop-up.vue
```

**已实际验证**：按上述方式解决后，这两个文件 `eslint` 检查 **0 error**
（只剩一条原有的 `'e' is defined but never used` warning，与本次无关）。

### 4.3 解决完提交

```bash
git commit    # 用 git 默认的 merge commit message 即可
git push origin main-1.3.0
```

---

## 五、合并后必须做的两件事（漏了会出问题）

### 5.1 补一条菜单记录（已有环境必做，新装环境自动带上）

前端是动态路由，路由完全由后端 `sys_ui_elements` 表下发。
规则编辑页 `/automation/rule-edit` 不在表里，**客户从设备详情点"生效规则"里某条规则，会直接看到 404**。

新装环境由 `sql/1.sql` 自动带上；**已经跑过初始化的库不会重跑，要手工执行一次**：

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

- `param3='1'` 表示**不在侧栏显示**——它是个落点页，不该出现在菜单里。
- `parent_id` 是"自动化"目录的 id。如果你们的库里不一样，用
  `select id from sys_ui_elements where element_code='automation'` 查出来替换。

### 5.2 网关加一条反向代理（生产必做）

前端默认**同源**访问自动化服务，前缀 `/automation-core`，由反向代理剥掉前缀转发。

```nginx
location /automation-core/ {
    proxy_pass http://<自动化服务地址>:<端口>/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header Authorization $http_authorization;
}
```

不加这条，所有自动化页面 404。

如果不想用同源反代，也可以在构建时给绝对地址：

```bash
VITE_AUTOMATION_API_BASE_URL=https://automation.example.com npm run build
```

---

## 六、部署形态：两个进程、两套库

| | 进程 | 模块 | 职责 | 数据库 |
|---|---|---|---|---|
| A | 社区后端（Gin，原有） | 仓库根 `module project` | 设备、用户、租户、MQTT 接入、物模型 | 原有 ThingsPanel 库 |
| B | 自动化服务（Encore，新增） | `encore-api/`，`module encore.app` | 规则、场景、联动、告警、执行记录 | Encore 托管的 `automation` 库 |

调用关系：设备上报 → A 接收 → HTTP 转发 `/v1/ingress/uplink` → B 判定 → B 回调 A 的命令接口下发。
**命令下发、通知、设备目录都还是原来的模块，没有第二套实现。**

### 启动命令

```bash
# A：原有方式不变
go build -o tp-backend . && ./tp-backend

# B：首次必须先 tidy，否则 encore run 会卡在联网下载
cd encore-api && go mod tidy
encore run --port=14000 --browser=never     # 生产用 encore build docker 产物
```

### 数据库迁移

- 社区后端：**没有新增迁移**（只有 5.1 那条菜单记录）。
- 自动化服务：`encore-api/automation/migrations/` 下 16 个文件，`1_init` 到 `16_migration_item_enabled`。
  **Encore 部署时自动执行，不要手工跑。**

### 切回旧引擎（紧急回退用）

```bash
GOTP_AUTOMATION_MODE=legacy
```

默认值就是 `legacy`，也就是**什么都不配等于维持现状**。`automation` 库留着不影响原有功能。

---

## 七、环境变量清单

### 社区后端（进程 A）新增

| 变量 | 必填 | 说明 |
|---|---|---|
| `GOTP_AUTOMATION_MODE` | 否 | `legacy`（默认）/ `native` |
| `GOTP_AUTOMATION_BASE_URL` | `native` 时必填 | 自动化服务地址 |
| `GOTP_AUTOMATION_TOKEN` | `native` 时必填 | 调自动化服务的令牌 |
| `GOTP_AUTOMATION_TIMEOUT_MS` | 否 | 默认 5000 |
| `GOTP_AUTOMATION_QUEUE` | 否 | 转发队列长度，默认 1024 |
| `TP_THINGMODEL_NATIVE_URL` | 用到原生模型时 | 原生物模型服务地址 |

> `native` 但没给 `BASE_URL` 时，进程会记一条 error 并**回退到内置引擎**，不会静默丢上报。

### 自动化服务（进程 B）

| 变量 | 必填 | 说明 |
|---|---|---|
| `TP_AUTH_MODE` | 是 | `legacy-jwt`（生产）。`dev` / `passthrough` 禁止上生产，启动会打 WARN |
| `TP_JWT_KEY` | `legacy-jwt` 时必填 | 与社区后端同一个签名密钥 |
| `TP_COMMUNITY_SESSION_BASE_URL` | 是 | 社区后端地址，用于会话校验 |
| `TP_COMMUNITY_BASE_URL` | 是 | 社区后端地址，用于读设备目录 |
| `TP_COMMUNITY_TOKEN` | 是 | 服务间调用令牌 |
| `TP_NOTIFICATION_URL` / `TP_NOTIFICATION_TOKEN` | 否 | 通知服务；不配则通知记为"未知"，不会假装成功 |
| `TP_SOURCE_DEPLOYMENT_ID` | 否 | 默认 `automation-rewrite` |

**已知坑**：社区版 `/api/v1/user/detail` 不返回 `tenant_id`，会话校验会失败。
1.3.0 分支的 `authn` 已经处理过这个问题（这就是为什么 authn 要取 1.3.0 的版本），
合并后请确认一次登录能拿到 `tenant_id`。

---

## 八、合并后怎么验证（10 分钟，按顺序）

### 8.1 两个进程都活着

```bash
curl -s -o /dev/null -w "community=%{http_code}\n" \
  "http://<社区后端>/api/v1/device?page=1&page_size=1" -H "x-token: <token>"
curl -s -o /dev/null -w "automation=%{http_code}\n" \
  "http://<自动化服务>/v1/health"
```

### 8.2 规则能读出来

```bash
curl -s "http://<自动化服务>/v1/rules?limit=5" -H "Authorization: Bearer <token>"
```

### 8.3 一条上报打通到底（**这一条是唯一能证明真的通了的**）

往 MQTT 发一条会命中规则的遥测，然后看两件事：

1. 设备的命令主题上有没有收到命令
2. 运行记录的 `status` 是不是 `succeeded`

```bash
curl -s "http://<自动化服务>/v1/rules/<rule-id>/runs?limit=1" -H "Authorization: Bearer <token>"
```

只看页面能打开、接口返回 200，**都不能算通**。

### 8.4 页面点一遍

1. 场景联动 —— 有卡片，开关能点，刷新后状态还在
2. 场景管理 —— 列表能出来
3. 新建场景 —— 选「操作设备」→「单个设备」，设备下拉能列出真实设备
4. 设备详情 →「生效规则」→ 点某条规则 —— 能打开「编辑规则」（验证 5.1 那条 SQL 生效了）
5. 告警信息 —— 点「详情」，状态显示"未恢复/已确认/已恢复"，**不是**"所有状态"
6. 告警配置 → 新增一条 —— 保存成功提示要真实（验证 4.2 ③ 那个 `res.data` 改了）

第 3 步里如果选完设备后属性下拉是空的，那是物模型服务没起来，不是本次代码的问题（见第十节）。

---

## 九、回退

| 场景 | 做法 |
|---|---|
| 只想关掉新引擎，保留代码 | 社区后端设 `GOTP_AUTOMATION_MODE=legacy`，重启进程 A |
| 要撤掉整个合并 | `git revert -m 1 <merge-commit>`，前后端各撤一次 |
| 自动化库要不要清 | 不用。`automation` 库独立，回退后不再被写入，留着不影响原有功能 |

---

## 十、已知限制（合并前请知情，不是本次引入的）

| # | 问题 | 影响 | 状态 |
|---|---|---|---|
| 1 | 物模型服务（原生）不可用时，"操作设备"动作选不出属性 | 只影响新建动作时选属性，已有规则照常跑 | 依赖外部服务，不在本次范围 |
| 2 | 单模式规则在突发流量下会"忙时忽略"（`mode_single_busy`） | 设计行为，防命令风暴，不是失败 | 符合预期 |
| 3 | 定时规则要外部 cron 打 `/v1/tick` 才会触发 | 客户配"每天 8 点执行"不会自己跑，需要配定时任务 | **待接线** |
| 4 | 通知未做真实端到端联测 | 配了通知可能发不出去，目前只有接口级证据 | **待联测** |
| 5 | 真实部署验收（U01–U09、V01–V32）未执行 | 交付前的正式验收缺口 | **待补** |
| 6 | 数据保留与配额未做 | `automation` 库会一直增长 | **待补** |

第 1、2 条是设计边界；**第 3 条如果客户要用定时规则，必须先配 cron**，这条最容易漏。

---

## 十一、本次改动里最值得看的两处（review 重点）

如果只想看关键 diff：

**① `87e6aef7` 上报链路按开关切换新引擎**
旧引擎和新引擎此前是两条互不相干的路径。要切就必须二选一，否则一条遥测会触发两次真实下发，
设备收到两遍命令。用一个值决定（`GOTP_AUTOMATION_MODE`），默认 `legacy`。
转发走有界队列 + 4 worker，队列满则丢弃并计数——慢引擎的代价是丢自动化事件，不是拖慢遥测。

**② `5697a462` 命令受理守卫移到响应中间件之外**
守卫原来注册在 `handler.Middleware()` 里面，它自己写响应头后，响应中间件跳过加成功包，
调用方拿到**空体 200**；自动化服务把空体读成 `code 0`，于是每条命令都记 `command_rejected`
——**而设备其实已经动作了**。症状极隐蔽：MQTT 上命令确实到了，运行记录却是失败。
已补回归测试锁住中间件顺序。

---

## 十二、测试证据（本地已跑过）

| 类型 | 内容 | 结果 |
|---|---|---|
| Encore 全量单测 | `encore test ./automation/...` 七个包 | 全过（automation 14.7s + 6 个包） |
| 命令守卫 | 12 条，含响应包顺序回归 | 全过 |
| 上报转发 | 9 条，含"native 无地址必须回退" | 全过 |
| 编译静态检查 | `go build ./...`、`go vet` | 干净 |
| 真实端到端 | MQTT 上报 → 规则命中 → 命令到设备 → 运行记录 `succeeded` | 通 |
| 压测 | 30 条间隔 0.1s / 100 条无间隔 | 全命中、0 丢弃；突发 32 条 `mode_single_busy` |
| 浏览器实测 | 场景联动/场景管理/新建场景/联动编辑/告警/规则编辑 | 通，真实数据 |

---

## 十三、遇到问题

优先看 `automation-rewrite-progress.md`（A01–A32 每项都有六句话说明和当轮缺陷清单），
以及仓库根目录的 `MERGE-AUTOMATION-REWRITE.md`（架构与环境的更详细版本）。
