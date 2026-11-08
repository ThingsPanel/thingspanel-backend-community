# HA 第三方服务插件：发布与安装任务卡

范围是 Home Assistant 第三方服务插件；不含协议插件或通用市场。按闭环推进，每个闭环单独验收。代码开工前核对所有 worktree 的分支、提交和 dirty 状态；保留未提交改动，不 stash/reset/覆盖，不推送/合并。

## 现状先核实，不重复建设

- 后端候选入口 `internal/api/plugin_install.go` 和测试 `internal/api/plugin_install_test.go` 当前要求租户管理员；identity 按租户绑定。目标由用户指定为超管安装、租户配置接入点。执行人必须检查既有超管身份/系统管理权限、安装对象作用域、租户服务身份和接入点授权，提出最小必要契约并做跨租户隔离验证；不得把租户管理员现状说成已实现，不得未经确认选系统级/逐租户安装架构。
- Horizon 当前版本是否有可复用的资源中心 HA 入口、如何到平台安装，需先核实其现有页面与合同；复用已存在路径，不假设要新增跨站认证或另建市场。
- `docs/tasks/T7b-DONE.md` §8 已记录真实 HA 灯上报/开关下发通过，前提是手动迁移、刷新 Broker 范围并重启 owner/mosquitto/插件；`PLUGIN-UI-DONE.md` 的上下行通过中建设备用 API 模拟。T7 初测失败，T7c 仅最小栈。只复验当前固定版本和自动化缺口，不重做已通功能。
- 续租候选仍未提交、未充分验证。Agent `/v1/grant` 的 409 是旧/过期代次拒绝；已知候选处理可将拒绝误判“不可信”并升代重装，从而形成重启风暴。需读后端分类与事件、Agent 响应及真实运行日志，区分 409 可恢复/需接管与身份确实无效；不要仅凭静态现象宣称正式版故障。证据入口：安装器 `agent/README.md` grant/旧代次语义、`agent/renew_test.go`；后端 dirty `internal/plugininstall` renewal/reconcile。

## 闭环 1：运行稳定（可先独立验收）

**用户可做：**安装后持续运行，不因续租而换代或重复重装；后端/Agent 短暂中断后可恢复。

**执行范围：**社区后端 `internal/plugininstall`、`internal/pluginidentity`；安装器 Agent `agent/activate.go`、`agent/service.go`、renew 候选。核实 grant 409 错误分类，修正必要分支；实现有界退避/jitter、状态事件和同代续 identity+Agent lease。明确续租不能换 token、升 generation、重建容器。安装中杀后端实测：固定 Owner 立即接管；重建导致 hostname 不同的 Owner 最多等旧租约到期。短 TTL 栈和常规运行分开记录。

**验收：**真实连续至少 30 分钟 generation 不变、无 `renew_refused`、容器无重启；短 TTL 下多周期观察到两类 lease 延长；断联恢复有界续作，过期按策略停止；故意注入身份无效、旧代次和瞬时网络错误，各自行为正确。单元测试不能替代连续运行证据。

**依赖/负责人：**后端与安装器 Agent 单一协同 owner；开工先审当前 dirty candidate。此闭环不依赖真实 HA 数据链。

## 闭环 2：官方制品正式发布与本机安装

**用户可做：**超管安装已审核 HA 插件，安装不输入 HA 凭据；失败有明确原因，可安全重试。

**执行范围：**安装器 `plugins/homeassistant.json`、`agent/catalog.go`、`tools/agent_host.py`、正式 compose、`install.sh`/升级入口、迁移脚本；后端现有安装 API/迁移。核实现有 catalog 和入口后复用。发布必须有受信签名、公钥来源、真实制品匹配的不可变 digest。检查 agent/run 权限、socket/token 挂载和 `TP_AGENT_SOCKET`/`TP_AGENT_TOKEN_FILE`，新装与升级都先 `agent_host.py up`；修复独立新装预检问题。HA catalog 不声明 HA 凭据；保留其他插件 `env_user`/secret。PG 迁移检查新装/升级；MySQL 未测不自动执行。

**验收：**干净受支持环境新装、升级均先启动 Agent 再启后端；未授权主体不能读 Agent token；签名或 digest 被篡改时失败关闭；幂等重试不产生重复实例；迁移自动执行。签名不是本地临时测试 key，固定制品和公钥指纹留档。

**依赖/负责人：**闭环1；安装器 + 后端。现有官方签名目录若已可用则只核验，不重造发布系统。在线同步、自助上架、复杂密钥治理延期。

## 闭环 3：超管安装，租户配置自己的 HA 接入点

**用户可做：**平台超管在现有插件/资源中心入口安装一次官方 HA 插件；租户用户随后只管理自己租户的 HA 接入点、实体和设备，无法读取或操作其他租户数据。

**执行范围：**先在 backend `router/`、`internal/api/plugin_install.go`、identity/store 和权限 middleware 检查当前安装者角色、租户上下文、实例及身份绑定；前端 `InstallPlugins.vue` 与资源中心 Horizon 页面/路由核实现存入口；HA `handler.go`/`startup_sync.go` 核实 voucher 读取与身份请求。按发现结果提出最小必要权限/作用域契约并交 owner 决策后实施，不预设超管跨租户写入或某种全局实例架构。复用已有页面、API、菜单和接入点表单。安装零 HA 字段；接入点凭据由所属租户认证配置；进程健康与每个接入点状态独立；凭据不入日志。

**验收：**超管可通过已存在并确认的入口安装；普通用户和租户管理员权限符合批准矩阵；两个租户的接入点、凭据、实体及设备严格隔离；租户可配置认证、选择实体、查看本接入点状态。实际验证 HA 请求身份头及 service/tenant scope。真实 HA/Broker 上下行与回执通过。

**依赖/负责人：**闭环2；backend + frontend + HA connector，Horizon 仅在入口复用确需改动时参与。租户/系统安装关系未决时先停止该功能实施，只完成只读现状核对和最小契约选项，不冒充已验收。

## 闭环 4：当前固定版本端到端与发布确认

**用户可做：**从核实后的现有资源中心/平台入口完成官方插件安装、租户接入点配置、实体上报、命令控制和卸载；安装后能持续稳定运行。

**验收顺序：**记录各仓 commit/dirty、签名目录与公钥指纹、镜像 digest/架构、migration → 受支持新装与升级 → 超管安装授权 → 租户隔离和接入点健康 → 真实 HA 当前值/历史上报 → 命令与 HA 状态/平台回执 → 闭环1的 30 分钟、短 TTL、进程崩溃接管 → 卸载吊销、容器停止、旧身份被拒。逐项引用 T7b/PLUGIN-UI 历史通过范围，只补当前版本差异和未自动化步骤。

**负责人/依赖：**发布负责人协调 backend、installer/Agent、frontend、HA connector；依赖闭环1–3。每项独立验收，不以单测、API 模拟或旧版本记录代替真实路径。正式签名制品和凭据不入日志是发布门槛。
