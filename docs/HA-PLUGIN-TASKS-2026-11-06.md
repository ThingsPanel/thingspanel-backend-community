# HA 类型第三方服务插件：五个可独立验收闭环

配套 [目标与现状](HA-PLUGIN-TARGET-ANALYSIS-2026-11-06.md)。范围只限 HA 类型第三方 service plugin，不做协议插件或通用插件市场。每个闭环能单独演示、验收和交付；产品体验与技术实现分别列出。开始任何代码派工前，重新查所有仓库分支/commit/dirty 状态并保留未提交工作；不得覆盖、stash 或 reset。

## 现有现场与证据使用

- T5 身份安装/卸载/重装曾对 T4 worktree 后端做过真实栈回归；`sql/33.sql` 当时手工执行，不能视为当前 develop 自动迁移已通。参考安装器 `docs/tasks/T5-DONE.md`。
- T7 初测上/下行失败；后续 `docs/tasks/T7b-DONE.md` §8 已用真实 HA 灯打通上报和开关下发，但当时依赖手动迁移、刷新 Broker 范围及重启 owner、mosquitto、插件。`docs/tasks/PLUGIN-UI-DONE.md` 也有安装 UI 后上下行通过记录，建设备则用 API 模拟。T7c 周期同步只在无真实 Broker/HA 的最小栈验证。复验当前固定版本，不重复开发已通功能，并确认这些手动步骤已自动化。
- HA `startup_sync` 的 Bearer 注入在历史交接和当前未提交源码中证据不一致；要以要发布的 commit/二进制真实请求确认。
- 上次状态快照里，后端 `wp-plugin-install-ui-8bf83e` dirty 5 个安装文件；安装器 `wp/pui-agent` dirty Agent/service/catalog 并有未跟踪 renew test；HA `main` 有未提交功能改动；前端 `wp/pui-frontend` 干净。实现 owner 开工前重新核对并保留原工作目录。旧提交/日志不可代表当前可发布版本。

## 闭环 1：装好后稳定续租

**用户侧**

管理员安装 HA connector 后，不必每天重装或接受容器重启，平台身份和 Agent 执行租约能持续有效；失联时平台能显示需恢复/等待状态。

**技术派工**

- **仓库/负责人：**社区后端 `/Users/junhong/Downloads/code/thingspanel-backend-community`：`internal/plugininstall`、`internal/pluginidentity`、renew API；安装器 `/Users/junhong/Downloads/code/thingspanel-installer-wp-pui`：Agent `agent/activate.go`、`agent/service.go` 和 `agent/state.go`。
- **工作：**先审查现有 dirty renew 实现，基于证据补齐同 generation 续 identity + Agent lease；续租不得换 token、升代、重建容器。补好错误分类、有界退避/jitter、状态可观测和 backend restart 接管。对签发一次性 token 在崩溃中的恢复，先由后端/Agent owner 选安全方案；不新增未批准的公开契约。
- **完成验收：**连续运行至少 30 分钟 generation 不变、无 `renew_refused`、容器不重启；将测试栈 TTL 临时缩短，在多个周期看到两个 lease 延长。断开/恢复 Agent 后有界续作；lease 到期会停止容器。真实被拒身份、跨租户和旧代次仍拒绝。
- **依赖/复用：**复用 T5 已验证的签发/吊销和当前 dirty renew candidate；先做当前源码审查，已通的签发路径不重写。
- **单独验收：**使用固定栈与模拟 HA 即可验证续租；不宣称真实 HA 数据链通过。

## 闭环 2：可信的本机安装能稳定工作

**用户侧**

有安装权限的用户可在目标平台安装官方审核的 HA connector；安装时不填写 HA URL/token。安装成功能看到进程状态，失败能重试或得到明确原因。

**技术派工**

- **仓库/负责人：**安装器 `thingspanel-installer-wp-pui`：`plugins/homeassistant.json`、`agent/catalog.go`、`tools/agent_host.py`、`docker-compose.yml`、`install.sh`/`install.bash`、`upgrade.*`、`config/native/prepare.py`；社区后端：现有 `plugininstall` API、PG migrations。
- **工作：**HA catalog entry 不声明 HA 凭据字段；保留通用 `env_user`/秘密字段供其他插件。正式发布前用受信发布 key 签名审核过的目录，digest 固定到真实可发布制品。首期目录可人工受控分发；在线同步不在本闭环。Agent `run/` 预建最小权限，只读挂载 backend，设置 `TP_AGENT_SOCKET`/`TP_AGENT_TOKEN_FILE`。正式 `install.sh` 和升级链都先运行 `agent_host.py up`。修复独立新装预检 bug。PG migrations 33–35 核验并纳入幂等安装/升级；MySQL 未测不自动运行。
- **完成验收：**干净 PG+Timescale 新装与受支持升级都能先启动 Agent，再启动 backend；真实 backend UID 能只读访问 socket/token，未授权用户读不到 token。管理员零字段安装得到单容器、受管身份，Agent 本机验签并按 signed digest 启动；同一请求重试不建第二个实例。篡改目录/签名或 image digest 时失败关闭。正式 release 的签名根/公钥来自受控渠道，不用本地测试密钥冒充可发布。
- **依赖/复用：**闭环1稳定后；复用 T5/T7 的安装、重装、卸载候选证据。迁移必须用当前 develop 重验，不重复抄手工执行结果。
- **单独验收：**本机平台/CLI 安装闭环可先过；不要求网站跳转，也不宣称上/下行通过。

## 闭环 3：在平台内发现、安装、配置并操作 HA

**用户侧**

管理员在平台资源/插件页面找到 HA connector，点击安装后看到进度；装好后手动创建一个需要认证的 HA 服务接入点，提交 URL/token，选择实体建设备，并看到真实上行和下行回执。安装与接入凭据配置是两个步骤。

**技术派工**

- **仓库/负责人：**前端 `/Users/junhong/Downloads/code/thingspanel-frontend-wp-pui` 安装抽屉、页面路由和状态展示；后端 `/Users/junhong/Downloads/code/thingspanel-backend-community` 现有 catalog/install/access APIs、权限与脱敏；HA `/Users/junhong/Downloads/code/thingspanel-device-connector-homeassistant` service access/startup sync；SDK repo 只在 Bearer 注入缺失时按已批准接口修复。
- **工作：**平台页面暴露已审核 HA 条目；仅租户管理员可安装/重试，安装失败显示步骤与原因。HA 安装零字段，不要求通用插件市场。完成后导向平台已有服务接入点表单；凭据从接入点 voucher 由受管身份读取。分别展示容器进程 health 和每个接入点的认证/同步/连接状态。复核真实 HA 请求 Bearer 头及租户/service scope；不能直接假设 SDK 已注入。
- **完成验收：**租户管理员能在平台内完成发现→零字段安装→配置认证接入点→选择实体；非管理员不能安装/重试。两个接入点一好一坏时独立显示状态；凭据不进日志/事件。真实 HA 与 Broker 上行落入平台当前值/历史；平台发控制命令后 HA 状态改变且设备回执可读。
- **依赖/复用：**闭环2；复用 T7 已有接入点/设备配置与 T7c 周期同步代码，不重复造流程；只补源码/发布版本差异和未通过的真实 Broker 链路。
- **单独验收：**完全在平台内完成；不依赖 Horizon 网站入口。

## 闭环 4：从资源中心网站进入指定平台安装

**用户侧目标（需决策）**

用户在资源中心网站发现 HA connector，点击“在我的平台安装”，能选定或确认一个 ThingsPanel 平台实例，在该平台完成认证和租户确认，然后看到闭环3的安装页面。不能让网站替用户偷偷选租户或静默安装。

**技术派工与待决边界**

- **仓库/负责人：**Horizon `/Users/junhong/Downloads/code/thingspanel-takeover-final-20261102/horizon` Portal/market；平台前端 `/Users/junhong/Downloads/code/thingspanel-frontend-wp-pui` 接收入口；身份认证/租户 owner 决定授权方式。
- **第一步仅做设计核对：**当前没有批准的跨网站登录、target platform/tenant 选择、授权确认、回跳与防重放合同。先列出可复用的现有认证/深链方案并交负责人决策。不要自行新增 API、单点登录、市场 token、回调协议或 URL bearer。
- **合同确认后再实施：**网站只携带经批准且非凭据的 connector/release 标识；目标平台重新验证用户身份和租户权限，并在平台界面确认后调用闭环3已有安装流程。校验 allowlist return URL、state/nonce、防重复提交和目标实例一致性，安全参数按正式认证方案定。
- **完成验收：**用户从网站跳到正确的平台/租户并明确确认后才能安装；未登录、租户不符、state 失效、取消或回跳篡改均不安装；凭据不经过 URL/浏览器存储。若负责人未批准合同，本闭环以完成边界设计和“不实现未授权接线”为阶段结果，不假报端到端通过。
- **依赖/复用：**闭环3平台内安装入口先验收；仅复用目标平台既有登录/授权能力，不能推定两个域名共享 session。
- **单独验收：**可与闭环5分开验收；此闭环未完成不阻塞平台内 HA 使用。

## 闭环 5：冻结版本并完成真实全链验收

**用户侧**

用户从已验收入口进入，安装 HA connector，配置服务接入点并完成实体上下行；重复安装、故障恢复和卸载后身份行为一致。

**技术派工**

- **仓库/负责人：**Horizon、Platform frontend、community backend、installer/Agent、HA/SDK 的 release owners；隔离 PG+Timescale、真实测试 HA 与支持的 Broker 镜像。
- **验收顺序：**记录每仓 commit/dirty 状态、签名目录版本、公钥指纹、HA 镜像 digest/架构和 migration → 资源发现（网站闭环4按获批边界；平台闭环3必须通过）→ 零字段安装与管理员权限 → 单实例幂等 → 接入点认证与逐点健康 → 真实遥测当前值/历史 → 真实命令及回执 → 闭环1续租/短 TTL证据 → 安装中杀 backend、相同 Owner 立即接管；hostname 重建为不同 Owner 时最多等一个 lease → 卸载吊销身份、停止容器、旧身份拒绝。
- **正式发布底线：**签名和 immutable digest 属于必要门槛；仅本地测试密钥签名、旧 ARM64 digest 或假 HA token 都不能作为正式发布证明。真实 HA token 不进仓库/日志；权限和迁移从当前被批准版本验证。
- **历史证据复用：**T5/T7/T7c 的候选测试只按其原范围引用；安装、卸载、周期同步的已通点不重复开发。仅对当前 release candidate 复核，补真实 HA/Broker 上下行、续租、重启恢复、签名和新装/升级自动迁移的空白。
- **完成标准：**每阶段可单独验收；本阶段全部证据和版本对齐后，负责人才能确认“完整 HA 服务插件用户链已验收”。单测、模拟接口、静态网站页面 200 均不能替代真实业务路径。

## 总体依赖与协作规则

```text
闭环1 稳定续租
   ↓
闭环2 正式本机安装/可信制品
   ↓
闭环3 平台内发现、安装、接入和上下行
   ↓（需认证/租户/回跳合同获批）
闭环4 资源中心网站进入指定平台
   ↓
闭环5 冻结版本，真实全链验收
```

各仓按各自批准基线建 worktree，由一个 owner 写共享文件。backend `internal/plugininstall/*` 和 identity renewal、installer Compose/`agent_host.py`/升级脚本、HA `startup_sync.go`、Horizon migration/DTO、前端页面分别单一 writer；跨仓接口先引用现有 API，任何新认证/跨域契约先获批准。证据留仓外并脱敏；不复制真实凭据。当前所有未提交改动均应保留，实施 owner 需先逐仓复核再开工。
