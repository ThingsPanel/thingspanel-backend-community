# PLUGIN-UI — 插件一键安装界面

未推送、未合并。设计见 `docs/PLUGIN-INSTALL-UI-DESIGN.md`（总监已同意 1、3；2 由我判断后实现，见 §1）。证据在 `/Volumes/KIOXIA/archive/evidence/2026-11-05-PLUGIN-UI/`（日志已 grep：HA 令牌 0 命中）。

## 三个仓库、三个分支

| 仓库 | worktree | 分支 | 提交 |
|---|---|---|---|
| 后端 | `.claude/worktrees/wp-plugin-install-ui-8bf83e` | `claude/wp-plugin-install-ui-8bf83e`（基于 develop `520a3222`） | 设计 `141eba0d`、引擎与 API `b32fcf8b`、菜单 + 收尾（见 `git log`） |
| 安装器 | `~/Downloads/code/thingspanel-installer-wp-pui` | `wp/pui-agent`（基于 develop `4a8f286`） | `d1a87f1` Agent，`1d2546e` agent_host + 迁移，`13b2366` Python 改薄客户端 |
| 前端 | `~/Downloads/code/thingspanel-frontend-wp-pui` | `wp/pui-frontend`（基于 develop `1da66eca`） | `88916dc4`、`2d80bd6e` |

## 1. 关于“Agent 改动”（设计里的第 2 项）
必须改。后端容器既不能改写 `profiles.json`，也不能重启 Agent 容器（原 Python 脚本就是这么注入令牌的）。所以 Agent 现在：自己验签目录、自己按目录条目拼容器 profile、自己保管秘密。后端只能调用 `catalog / config-put / config-keys / config-clear / accept-generation / prepare / grant / activate / revoke / retire`，没有 profile、镜像、命令、挂载参数。`config-put` 只接受目录里声明为 secret 的字段。

## 2. 做了什么
- **后端** `internal/plugininstall`（不依赖 gin，GORM）：`plugin_installations` + `plugin_install_events`（`sql/34.sql`，MySQL `016/017`）；引擎（提交、配置、启用/停用/重试/卸载、30 秒对账循环、租约抢占、5 分钟续 Agent 租约与身份）；Agent 客户端（Unix socket + 令牌文件）；`service_plugins` 登记（先查后改，不重复）；健康检查走网络别名。接口 `GET plugin/catalog`、`GET/POST plugin/installs`、`GET/DELETE plugin/installs/:id`、`PUT .../config`、`POST .../enable|disable|retry`，仅租户管理员；未配置 `TP_AGENT_SOCKET` 时 503，其余行为不变。`pluginidentity.Store.Renew`（同代次续租，令牌不变；没有开 HTTP 入口，只在进程内调用）。`/api/v1/plugin/installs*` 不记请求体日志。
- **菜单**：安装只能由租户管理员做（身份签发的既有规则），而“插件管理”菜单只给系统管理员，所以新增租户菜单“设备接入 → 插件安装”（`sql/35.sql`，MySQL `018`）。
- **Agent**（安装器 `agent/`）：`catalog.go`（ECDSA P-256 验签、过期、条目复核）、`secrets.go`（0600 秘密库；profile 与 `state.json` 里只有 `@secret:` 占位符，创建/校验容器时才解析）、`service.go` 新接口、动态 profile 重启后只在目录担保时才恢复。
- **安装器**：`tools/agent_host.py up|down`（目录模式启动 Agent，只把 `run/` 交给后端，同时起 Broker 范围监视进程）；`prepare.py` 把 `34.sql`、`35.sql` 纳入自动迁移；`plugin_install.py` 对 `binding: none` 插件改为调用平台安装 API 的薄客户端，**删除了它自己的 service_plugins 登记代码和测试**（单一实现）。
- **前端**：`src/views/apply/plugin/InstallPlugins.vue`（目录卡片、安装抽屉、详情抽屉：步骤、动作、状态标签）；同一组件挂在“插件管理”第三个 Tab 和租户页 `/device/plugin-install`；令牌框 `type=password`，提交后清空，不进存储；服务端只回“已设置”。

## 3. 验证
**单元/集成（诚实声明：这些不是端到端）**
- 后端 `go test -race`：`plugininstall` 12 项（假 Agent：正常路径且库/事件里无秘密、8 并发提交一行、部分配置→补全、非法值、Agent 离线→等待→恢复/超限转失败→重试、失败步骤吊销身份且重试用新代次、健康失败、租约过期后接管、停用/启用/卸载/重装、续租与被拒后自愈、租户隔离、service_plugins 只登记一次保留他人键）；`internal/api` 3 项（非租户管理员 403、未配置 503、响应不回显秘密）；PG 上 2 项（8 并发只有一个租约持有者、并发提交一行）和 `Renew` 1 项（`TEST_PG_SCHEMA_DSN`，容器 `pui-pg`）。**MySQL 文件（016–018）已写并接入迁移名单，未在 MySQL 上跑**（本机没有 MySQL 镜像，未执行）。
- Agent（Linux 容器内 `go test`）：新增 3 项（验签/篡改/过期/他钥、秘密不进 `state.json` 而进容器环境、重启后恢复需目录担保、`config-*` 只接受声明的秘密）；原有全部通过。
- 安装器 Python：`tests/` 113 项、`tools` 17 项、`config.native` 8 项全过。前端 vitest 10 项（状态视图、空秘密不覆盖、URL 校验、连点只提交一次、令牌提交后页面与 localStorage 中为 0、Agent 离线提示）。`vue-tsc` 仅剩 2 个与本次无关的既有错误（`packages/axios`、`notification.ts`）。

**端到端（独立栈 `pui-stack`，端口 58xxx，容器均 `pui-` 前缀 + Agent 的 `tp-agent-pui-stack`/`tp-agent-prepared-*`）**
| 步 | 结果 | 证据 |
|---|---|---|
| 浏览器登录租户管理员，菜单“设备接入 → 插件安装”，点安装，只填 HA 地址和令牌 | 步骤依次完成，约 10 秒“已启用”，健康 ok；库中 `secret_keys=["HA_ACCESS_TOKEN"]`；令牌只在 Agent 的 `secrets.json`（0600）里；Postgres 全库转储、后端日志、Agent 日志、`state.json` 命中 0 | `step1-ui-install-state.log` |
| 平台选 HA 实体建设备（沿用 P1 的 API 模拟，未走设备页面） | 发现 33 个实体，建 Bed Light、Outside Temperature | `step2-create-devices.log` |
| 上行 | HA 里改温度，平台当前值更新（最终用新值 71 复验，避免旧值误判） | `step3-uplink.log`、`step8-…log` |
| 下行 | switch false→true→false，HA 灯 off/on/off，三次回执 `result:0`、状态 3（安装/重装多次后复验仍通过） | `step4-downlink.log`、`step8-…log` |
| 重复安装 | 同时 5 个安装请求：1 行 1 个容器；已启用时相同地址不重启 | `step5-lifecycle.log`（该次运行的版本尚会因“带值提交”重启一次，之后已改为相同值不重启并加单测，未重跑 e2e） |
| 停用/启用、卸载、再装 | 停用后身份吊销、容器停；启用换更高代次；卸载后身份吊销、Agent 秘密清空、`service_plugins` 行与设备保留、再次卸载 409；重装代次更高、仍 1 行 1 容器 | `step5-lifecycle.log` |
| Agent 离线 | 目录接口 503 `AGENT_OFFLINE`；停用请求停在等待（不判失败），Agent 回来后自动完成 | `step6-resilience.log` |
| Agent 容器重启 | 重装 Agent 后状态仍 enabled/ok | 同上 |
| 命令行 | `plugin_install.py uninstall/install` 走同一 API，成功，并保证 Broker 范围监视进程在 | `step7-cli-thin-client.log` |

## 4. 没做 / 局限（请审）
1. **没有演示到“安装进行中杀后端”**：我试了两种办法（容器暂停、错误地址），安装在 2–8 秒内就完成，窗口抓不到。已验证的是：租约过期后被接管（单测）、后端重启后行与状态保留并继续维护（e2e）。进行中重启的 e2e 未做。
2. **插件健康不反映 HA 连通性**：HA 插件的 `/health` 来自 SDK，HA 地址/令牌填错时仍显示“已启用 / 健康 ok”（我用错误地址实测过）。需要插件侧在 HA 认证失败时让 `/health` 非 ok，或目录加“就绪探针”（插件 `:9002/sync-status`）。**这是用户体验上最大的缺口。**
3. **令牌要填两次**：安装填一次；之后“服务接入点”里建接入点的 voucher 仍要填 HA 地址和令牌（既有行为，T7c 已记，HA 令牌仍明文进 `service_access.voucher`）。
4. **Agent 与后端的权限模型**：socket 与令牌文件为 root 0600，后端容器以 root 运行才能读（实测可用）。后端换非 root 用户时需要给 Agent 加 `-socket-mode` 或单独的后端令牌，未做。后端持有 Agent 令牌即可启动目录内任一插件（仅限已签名目录里的插件，不能任意）。
5. **安装器接线只做了验收脚手架**：compose 里没有正式加后端对 `agent/run` 的挂载和 `TP_AGENT_*` 变量（验收用 override），`install.sh` 也没有调用 `agent_host.py up`。这是 T8（单一发行版）要接的线。**`prepare.py` 对全新安装的预检在 develop 上就失败**（`validate_supported_backend_database` 看到 `${TP_DATABASE_PASSWORD:-…}` 的 `$`，在没有 `.env` 时）：我是预先写好 `.env` 才绕过的，与本卡无关，另报。
6. `binding: template` 插件（http-adapter）仍走旧路径，Python 里还有一份编排；等引擎支持模板绑定后删除。目录验签现在 Go（Agent）与 Python（`plugin_catalog.py`，发布与自检）各一份，Python 端运行时验签随上一条一起删。
7. Broker 设备范围同步仍是主机监视进程（`agent_host.py up` 起），不是系统服务；界面“已启用”不含它，新设备最长约 80 秒后可用。
8. “选实体建设备”用的是 API 模拟，没有在浏览器里点设备页面；HA 令牌在验收中以明文传给浏览器输入框（测试令牌，栈内创建）。
9. 单实例：每租户每插件一个实例；无升级入口（社区版不做 OTA）；MySQL 未验证（见上）。

## 5. 清理清单（等确认再删）
容器：`pui-stack-{postgres,redis,broker-owner,mosquitto,backend}-1`、`pui-frontend`、`pui-homeassistant`、`pui-pg`、`tp-agent-pui-stack`、`tp-agent-prepared-91b3…`（Agent 创建，名字固定无前缀）；监视进程（pid 在 `pui-stack/agent/`）；镜像 `thingspanel-go:pui`；目录 `/Volumes/KIOXIA/pui-stack/`（含 `.ha-token`、`.ui-pass`、`.env`、`agent/state/secrets.json` 等凭据，删除前不要提交）、`/Volumes/KIOXIA/.gomodcache-pui`、`.gocache-pui(-linux)`；三个 worktree（后端、安装器 `…-wp-pui`、前端 `…-wp-pui`）。已在 `docs/WORKSPACES.md` 登记。
