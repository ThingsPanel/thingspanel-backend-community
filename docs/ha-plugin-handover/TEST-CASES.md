# HA 插件一键安装 — 测试用例与证据模板

> 配套入口文档：[`../HA-PLUGIN-HANDOVER.md`](../HA-PLUGIN-HANDOVER.md)。先读入口文档，再来这里逐条做。
> 约定：**每条用例都要留证据**（命令输出、截图、SQL 结果），**证据不进仓库**，放到
> `/Volumes/KIOXIA/archive/evidence/<日期>-HA-HANDOVER/`，仓库里只写一行索引。
> 约定：**HA 令牌、数据库密码不进日志、截图、聊天、文档**。令牌只放环境变量或 0600 文件。

结果只能填四种：**通过 / 失败 / 阻塞（没法做，写原因）/ 未做**。失败的用例写清现象和证据路径，**不要在这个任务里去修**，记到 `docs/TODO.md`。

---

## 0. 准备（做完才能开始 A/B）

| 编号 | 动作 | 通过标准 |
|---|---|---|
| P0-1 | 在**自己的**独立栈上测试（入口文档 §4）。不要动别人的容器、卷、目录。 | 栈起来，后端 `/health` 返回 200 |
| P0-2 | `tools/check-test-env.sh <栈前缀> <后端端口>` | 第 1、2 项正常；第 3 项 `state=enabled` 且 `health=ok`（装之前为空行是正常的） |
| P0-3 | `tools/verify-public-release.sh both` | 最后一行“全部通过” |
| P0-4 | 起演示 HA（`tools/ha-demo/`），做 HA 首次向导，创建长期访问令牌（入口文档 §5） | 浏览器能打开 HA，`Developer tools → States` 里有 `light.bed_light`、`sensor.outside_temperature` |
| P0-5 | 向负责人要两个账号：平台超管（`SYS_ADMIN`）一个；某租户下有接入点管理权限的账号一个（最好再要第二个租户的账号，用于 B6） | 能分别登录 |

---

## A. 安装（平台超管）

| 编号 | 步骤 | 期望结果 | 在哪里看 |
|---|---|---|---|
| A1 | 超管登录，进入 **应用管理 → 插件管理 → 插件安装** | 出现 Home Assistant 卡片，版本 `1.0.0`；点安装后的确认页**没有** HA 地址、令牌输入框 | 页面截图 |
| A2 | 点安装并确认 | 步骤依次走完（check、identity、prepare、grant、activate、register、health），约十几秒到一两分钟后显示“已启用 / 健康: ok” | 页面；`check-test-env.sh` 第 3、6 项：`state=enabled`、`health=ok`、事件最后一条 `health ok` |
| A3 | 看容器 | 有 `tp-agent-prepared-<hash>` 容器在运行，`RestartCount=0`；`docker inspect -f '{{.RestartCount}}' <容器>` | 终端 |
| A4 | 看服务登记 | `service_plugins` 有且只有 1 行 `homeassistant`，`sub_topic_prefix=plugin/homeassistant/` | `check-test-env.sh` 第 5 项 |
| A5 | 租户管理员（非超管）进同一个页面或直接调接口 | 看不到安装入口，或接口返回 403（不是 200） | 页面 / `curl` 响应码 |
| A6 | 连续点两次安装，或再开一个浏览器同时点 | 只有 1 行安装记录、1 个插件容器，没有第二个实例 | `check-test-env.sh`；`docker ps` |
| A7 | **停用**再**启用** | 停用后容器停止、该代次身份被吊销；启用后换更高代次、容器重新运行 | `check-test-env.sh` 第 3、4 项（generation 变大，旧代次 `revoked=true`） |
| A8 | **卸载**（冒烟已做一次，2026-11-09，通过，见入口文档 §3；你们要独立重做并留证据）（先做 B 组再做这条，否则没数据可验证保留） | 身份吊销、容器停止；**已建的设备、历史数据、接入点保留**；用旧身份调平台接口被拒（503） | `check-test-env.sh`；设备页面 |
| A9 | 卸载后**重装**（冒烟已做一次，通过） | 代次比之前更高；仍然只有 1 行 1 容器；设备还在 | 同上 |
| A10 | 稳定性：安装成功后什么都不动，观察 **30 分钟** | generation 不变，容器 `RestartCount` 不变，健康一直 ok，没有出现 `renewal_failed` 事件 | 每分钟记一次 `check-test-env.sh` 第 3、4 项（可参考旧脚本 `/Volumes/KIOXIA/CodexTests/thingspanel-ha-ui-acceptance-20261106/evidence/monitor-ha-ui-install.sh`） |
| A11 | 篡改被拒：运行 `verify-public-release.sh`，看“篡改后的目录被拒绝” | 该行为 PASS | 脚本输出 |
| A12 | （进阶，可选）在你的栈里改动一份目录或镜像归档再触发安装 | 安装在 `check`/`prepare` 步失败，**不会**起容器 | 事件表；`docker ps` |
| A13 | **恢复性**：停掉 Agent 容器 2 分钟再起；再单独停后端 2 分钟再起 | 目录接口显示 `AGENT_OFFLINE`；Agent 回来后自动恢复；**记录**后端/Agent 停多久之后会进入 `failed / renewal_failed`，以及点 Retry 能否恢复（见入口文档“已知问题 1”） | 页面；`check-test-env.sh`；记下时间点 |

---

## B. 租户接入点与数据（上行 / 下行）

> 2026-11-09 整理时我已在验收栈上**各走通一遍**（B1–B5、B7 的隔离部分，演示 HA，租户 A）：页面路径走得通。**这不等于验收**：只做了一次，没做 B6、B8、B9。你们要按本表独立重做并留证据；已有的数据可以复用，也可以在租户 B 下新建。账号、HA 令牌位置见 [`STACK.md`](STACK.md)。

| 编号 | 步骤 | 期望结果 | 在哪里看 |
|---|---|---|---|
| B1 | 租户账号登录，进入 **设备 → 三方接入**（前端路由 `/device/integration`），选 **Home Assistant**，点 **新建接入点**；填接入点名称、HA 地址（见入口文档 §5）、长期访问令牌，保存 | 保存成功；列表出现该接入点。如果菜单打不开或没有 Home Assistant 服务，**记录**（这是已知的未验证点） | 页面截图 |
| B2 | 在接入点里查看 / 选择 HA 实体 | 列出实体（插件一次最多列 100 个，demo HA 一共约 30 多个）；能选 `light.bed_light`、`sensor.outside_temperature`、`sensor.outside_humidity` | 页面 |
| B3 | 把选中的实体建成设备 | 平台出现 3 台设备；不要手工改 Broker 配置、不要登录服务器 | 设备列表 |
| B4 | **上行**：在 HA 里把 `sensor.outside_temperature` 改成一个新值（HA 的 Developer tools → States 里可以直接设；每次用不同的值，避免和旧值混淆），再改一次 | 平台设备“当前值”变成新值；历史里有两条变化记录。插件**只在变化时上报**，刚接入的设备在变化前没数据是正常的（已知问题 3） | 页面；SQL：`select * from telemetry_current_datas where device_id='<设备ID>'`、`telemetry_datas` |
| B5 | **下行**：平台对灯设备下发 `switch`（开），再下发关 | HA 里灯状态跟着变；平台命令记录状态 3、回执 `{"result":0,"message":"success"}` | HA 页面；SQL：`select * from command_set_logs order by created_at desc limit 5` |
| B6 | **新设备无需手工刷新**：在 B3 之后**再**建第 4 台设备，等最多 90 秒，对它做一次上行 + 一次下行 | 不需要手工执行任何命令就通；如果不通，记录等了多久、插件日志和 Broker 日志里有没有 `not authorised`（已知问题 4） | 同上；`docker logs <插件容器>` |
| B7 | **租户隔离**：用第二个租户账号登录 | 看不到第一个租户的接入点、设备、凭据；用对方设备 ID 直接调接口返回 403 / 找不到 | 页面；接口响应码 |
| B8 | **凭据不外泄**：把令牌放进环境变量 `HATOKEN`（不要 echo），然后对后端、Agent、插件容器日志做 `docker logs <容器> 2>&1 \| grep -c "$HATOKEN"` | 每个都是 0。**注意**：令牌存在数据库 `service_access.voucher` 明文里，是已知问题 2，不算这条失败；但日志和操作日志表里不应出现 | 终端（只记次数，不贴令牌） |
| B9 | **填错**：新建一个 HA 地址或令牌故意写错的接入点 | 记录平台给出的提示。已知：插件“健康”仍显示 ok（已知问题 5），所以这里要看接入点本身有没有错误状态 | 页面 |

---

## 结果汇总表（复制到你的记录里，每条填一次）

| 编号 | 结果 | 日期时间 | 证据路径 | 备注（失败现象 / 阻塞原因） |
|---|---|---|---|---|
| P0-1 … | | | | |
| A1 … | | | | |
| … | | | | |

## 记录失败时必须包含的内容

1. 用例编号和你做的具体步骤（不要只写“失败了”）。
2. 栈前缀、后端镜像 ID（`docker inspect -f '{{.Image}}' <后端容器>`）、插件镜像 ID。
3. `check-test-env.sh` 的完整输出（它不含密码和令牌）。
4. 相关容器日志的**末尾 200 行**，先用 `grep -v` 或肉眼确认里面没有令牌再保存。
5. 时间点：做了什么操作、什么时候出现的现象。
