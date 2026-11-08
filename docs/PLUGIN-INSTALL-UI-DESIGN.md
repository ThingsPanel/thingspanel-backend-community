# 插件一键安装界面 — 设计（第一阶段，待总监审查）

任务：PLUGIN-UI。依据：插件设计 §5.1/§7/§9/§11.3、`PROJECT-AUDIT` §1.2 A1–A5、`docs/plugin-runtime-identity.md`、安装器 `tools/plugin_install.py` 与 `agent/`（develop `4a8f286`）。本文只设计，不含代码。

## 1. 安装管理放哪：老 Gin 的“插件管理”模块（新包 `internal/plugininstall`）

| 候选 | 结论 | 理由 |
|---|---|---|
| Encore `marketinstall` | 否 | 它的协议明确只有 thingmodel/社区配置/ThingsVis 三类 owner 动作，“没有 prepare/activate/revoke 或任意命令”；仅 PG、需 `TP_MARKETINSTALL_ENABLED`；它装的是模型/看板资源，不是运行容器。硬塞会改它的安全边界 |
| **插件管理（Gin：`service_plugins` + 运行时身份所在处）** | **选它** | 运行时身份签发/吊销（`internal/pluginidentity`）、`service_plugins` 登记都已在这里；同进程直接调用，不产生 Encore→Gin 回调（A5）。受管服务插件由平台超级管理员安装一次，`plugin_installations.tenant_id=''` 表示唯一的平台级实例；租户仍通过既有服务接入点 API 管理各自配置。 |

约束：`plugininstall` 是**不依赖 gin 的领域包**（GORM repository + 引擎 + 接口），Gin handler 只做薄适配；Gin 退役时整包平移到 Encore。关系库只用 GORM，PG/MySQL 各一份迁移文件（同 `sql/33.sql` + `sql/mysql/015` 做法）；不用 `sqldialect`/`dbx`。

## 2. 后端怎么调用 Agent；Python 与 Go 的分工

**现状缺口**：`plugin_install.py` 靠“改写 `profiles.json` + `docker restart` Agent 容器”给容器注入令牌和用户环境变量——后端做不到也不该做。所以 Agent 需要小改（安装器仓库，卡 UI-A）。

- **连接**：后端容器只读挂载 Agent 目录里一个专用子目录 `run/`（仅含 `agent.sock`、`token`），环境变量 `TP_AGENT_SOCKET`、`TP_AGENT_TOKEN_FILE`。仍是 Unix socket + bearer；不开 TCP，不挂 `state/`、`profiles.json`、docker.sock。凭据来源：安装器创建的 `token` 文件（0640，仅后端运行用户组可读）。
- **只允许的操作**（后端 client 里写死白名单，Agent 端本来就只有这些路由）：`catalog`（读已验签目录的公开字段）、`config-put/config-keys`（见 §5）、`accept-generation`、`prepare`、`grant`、`activate`、`revoke`、`retire`、`status`。没有 exec/任意 profile/任意镜像/任意挂载。
- **Agent 新增两件事**（Go，写一次）：
  1. 目录验签搬进 Agent：Agent 启动时用固定信任根验证 `plugin-catalog.json`，**profile 由 Agent 按目录条目自己拼**（镜像 ID、入口、网络别名、`env_from_stack` 从主机 `.env` 读取，MQTT 凭据不经过后端）。后端只传“插件 ID + 实例 + 代次 + 用户值引用 + 一次性身份令牌”。取代“写 profiles.json 再重启”。
  2. 秘密/用户配置接口（§5）。
- **分工（只留一份实现）**：

| 步骤 | 归属 |
|---|---|
| 目录发布/签名（`plugin_catalog.py publish`） | 留在安装器（发布工具，后端不签名） |
| 目录**运行时**验签、条目校验 | Agent（Go）。Python `load_verified` 与它共用同一组 fixture 做一致性测试；**两份的理由**：发布端要在 CI 里无 Go 环境运行。收敛：UI-D 之后 Python 的 install/uninstall/renew 不再自己验签，只剩发布与自检 |
| 签发/吊销身份、代次分配、登记 `service_plugins`、编排各步、健康检查、续租、卸载 | **后端 Go（`plugininstall`）**。`plugin_install.py install/uninstall/renew` 在 UI-D 改为**调用同一个后端 API 的薄客户端**，原编排代码删除。这是唯一的收敛点 |
| 启动 Agent 容器本身、Broker 设备范围同步监视进程（`scope_sync`，需要 `docker compose restart`） | 留在安装器/主机（后端无权也不应重启 broker）。界面“已启用”**不含**范围同步；新设备最长约 20 秒后上下行才通（已知，界面提示） |

- **新增后端小改**：身份“同代次续租”（`POST /plugin/runtime/identity/renew`，只延长 `expires_at`，令牌不变）。后台对账循环每 5 分钟给 Agent 续 grant 并续身份，替代 cron `renew`；否则插件每 24 小时失联（现状的隐患）。

## 3. 前端：放在现有 `src/views/apply/plugin/`，不新建一级菜单

现有页是 Tab（服务插件 / 通知插件）。**新增第三个 Tab“插件安装”**（新文件 `InstallPlugins.vue` + `components/install/*`，API 封装 `src/service/api/plugin-install.ts`，只调用后端新接口）。

1. **目录卡片列表**：名称、版本、说明、平台安装状态徽标（未安装 / 待配置 / 已安装 / 已启用 / 失败 / 卸载中）、主机 Agent 在线灯。
2. **安装抽屉**：先列“将做什么/权限”（运行容器、受管身份、登记服务插件、不读取遥测）；通用插件表单继续由目录 `env_user` 渲染。HA 安装项不声明 HA 地址或令牌字段，凭据由租户在既有服务接入点页填写。
3. **实例详情抽屉**：步骤时间线（检查→身份→准备→启动→登记→健康）、当前错误（脱敏码+建议）、健康；按钮：修改配置、启用/停用、重试、卸载（弹窗列出依赖它的服务接入点/设备，默认保留数据）。
4. 完成后给“去添加设备”链接，跳到现有服务接入点页（选实体建设备走原有流程）。
- 每处用户可见文案走 `$t` 双语；不做 OTA/升级入口（社区版）。

安装目录和生命周期接口（均 `SYS_ADMIN` JWT，`/api/v1/plugin/...`，信封与现有一致）：

| 方法 路径 | 用途 |
|---|---|
| `GET catalog` | 目录公开字段（含通用 env_user 规格，**不含**任何值）+ 平台安装状态 |
| `GET agent/status` | Agent 在线/版本 |
| `POST installs` + `Idempotency-Key` | 创建安装（含 config）→ 202 `{id, operation_id}` |
| `GET installs` / `GET installs/{id}` | 状态、步骤、健康、非秘密配置值、秘密“已设置”标记 |
| `PUT installs/{id}/config` | 改配置；秘密字段缺省=不变 |
| `POST installs/{id}/enable` `disable` `retry` | 生命周期 |
| `DELETE installs/{id}` | 卸载（v1 不提供“清除数据”） |

平台安装的 HA 实例使用空租户标记的运行身份，只能读取自身服务标识下的各租户服务接入点；该服务插件运行身份不改变面向用户的租户隔离规则。现有租户服务接入点页面/API 仍按登录用户所属租户授权。

## 4. 状态机与恢复

一行记录 `plugin_installations`：唯一键（租户, plugin_id）→ **重复点击只会命中同一行**；另有 `plugin_install_events`（步骤日志，无秘密）。记录只存“期望”和“已观察”，后台对账循环（进程内，30 秒一轮，非 Encore 定时器）推进。

| 用户可见状态 | 含义 | 条件 |
|---|---|---|
| 待配置 `pending_config` | 记录已建，无容器、无身份，不收任务 | 必填项缺失，或最近校验失败 |
| 安装中 `installing` | 有操作在跑，显示当前步骤 | 租约内 |
| 已安装 `installed` | 实例/配置/登记齐全，**已停用**，身份已吊销 | 用户停用后 |
| 已启用 `enabled` | 容器运行、身份有效、健康检查（经网络别名）通过 | 另附健康：正常/降级/异常 |
| 失败 `failed` | 记录失败步骤+错误码，可 `retry`（从该步继续） | |
| 卸载中 `uninstalling` → 保留墓碑 | 吊销→停容器→retire；`service_plugins` 行与代次计数保留 | |

- **幂等/防重**：每个操作行有 `op_id`、租约（`lease_owner/lease_until`，GORM 条件 UPDATE 抢占，受影响行=1 才执行）；重复 POST 返回同一操作；`Idempotency-Key` 冲突内容返回 409。第二个独立实例必须是新 `plugin_id/instance`，v1 不开放。
- **代次**：存在表里，先落库再签发（`generation = 旧值+1`），不再依赖 Agent 状态文件；签发返回 409 则对齐到已知最大值后重试。令牌仅签发时返回一次，若在交给 Agent 前崩溃→下一轮直接再升一代（旧的已被吊销）。接管旧命令行安装的实例：读 Agent `status` 取其代次建行。
- **每步幂等**：Agent 调用带固定 `request_key`；登记 `service_plugins` 先查后 `PUT`，并受记录租约串行；崩溃后租约过期，对账循环先查询（Agent status / `service_plugins` / 身份），再补做缺的步。
- **断线**：操作在服务端继续，页面只轮询（3 秒）；浏览器关闭无影响。Agent 离线→停在 `installing/waiting_agent`，指数退避，不标失败；超 30 分钟转 `failed(agent_offline)`，可重试。后端重启→从表恢复。

## 5. 秘密字段路径（HA_ACCESS_TOKEN）

浏览器密码框（`autocomplete=off`，不进 Pinia/localStorage/URL，提交后清空）→ HTTPS `POST installs` 或 `PUT config`，仅在请求体；该路由禁用请求体日志，响应 `no-store` → 后端校验（按目录的 `validate/pattern/换行`）→ **不入库、不写日志**，立刻经 Unix socket 调 Agent `config-put`（该实例，key，value）→ **Agent 主机本地秘密库**（`state/` 内 0600 文件，只有 Agent 能读；不写 `profiles.json`）→ 创建/重建容器时 Agent 把它作为环境变量注入。后端表里只有 `secret_keys=["HA_ACCESS_TOKEN"]` 与时间；API 只回“已设置”，永不回读。非秘密值（`HA_BASE_URL`）存后端表用于回显，并在每次启用时随调用传给 Agent。审计事件只记键名。

残余风险（如实写）：容器环境变量可被 `docker inspect` 看到（T5 已记录）；HA 令牌之后在平台建设备时仍会进 `service_access.voucher`（T7c 已记录，属范围外，记 TODO）。身份令牌同样只走后端内存→socket→Agent→容器，不落盘。

## 6. 验收与工作量

**验收**（独立栈，容器名 `pui-` 前缀，证据放 `/Volumes/KIOXIA/archive/evidence/2026-11-05-PLUGIN-UI/`）：干净栈 → 前端登录 → 点安装 HA、只填地址+令牌 → 到“已启用” → 在平台选实体建设备 → 上行入库、下行命令回执 `result:0`；再测：安装中连点 5 次/刷新页面只有 1 个实例；安装中杀后端再起能继续；Agent 停止再恢复；停用→启用；卸载后无容器、身份已吊销、`service_plugins` 行保留；再装代次更高；日志与 DB 中 grep 令牌=0。单元：引擎对假 Agent 的步骤/崩溃点表驱动测试、仓库层、handler 越权（非租户管理员 403）、前端 vitest。**诚实声明**：单元通过≠端到端通过；Broker 范围同步依赖主机监视进程。

**估算 ≈ 8 人天，超过 5 天，拆 4 张卡**（同时改代码不超过 2 条线）：

| 卡 | 内容 | 仓库 | 天 |
|---|---|---|---|
| UI-A | Agent：目录验签+按条目拼 profile、config-put/keys、catalog 读取接口、测试 | 安装器 | 2 |
| UI-B | 后端：表+迁移、`plugininstall` 引擎+对账、Agent client（白名单）、身份续租、handler、测试（假 Agent） | 后端 | 3 |
| UI-C | 前端第三个 Tab、抽屉、API 封装、vitest（可对 UI-B 的接口契约并行，用 mock） | 前端 | 2 |
| UI-D | compose 挂载 `run/`、Python 改薄客户端、独立栈全流程验收、`PLUGIN-UI-DONE.md` | 三仓 | 1 |

A 与 B 先行（可并行），C 随 B 契约，D 最后。

## 需要总监拍板
1. 同意 Gin 的插件管理为落点（Encore 迁移时整包平移）？
2. 同意 Agent 增加“验签+拼 profile+秘密库”（UI-A，改安装器 Agent）？
3. 同意 UI-B 里加“身份同代次续租”这个小接口？
