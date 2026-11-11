# HA 服务插件发布与安装收尾记录（2026-11-06）

状态：**隔离 Agent 续租长跑、现有 ThingsPanel 页面一键安装及安装后固定组合 30 分钟稳定观察已通过。候选可审查，正式签名材料尚缺，不可称为正式发布。**

## 范围决定

本次按用户最新决定只收尾 HA 第三方服务插件的一键安装。安装不收集 HA 地址或令牌；租户接入点配置以及设备上报/控制验收不在本次范围。协议插件、通用市场和自动升级均未处理。

## 已有实现与改动

- 后端：`claude/wp-plugin-install-ui-8bf83e`。功能提交 `f0c493d6ab2ade47dd0aa80d323708eaebd24323`，本交接记录提交 `c2279c21a90f778e2592ec9f7112611df050e1aa`，后续权限回归提交 `d56d6151998951b5f7aff067c853b205469d3f1c`。沿用现有插件安装引擎和 UI API；安装范围为平台级，由 `SYS_ADMIN` 操作，非超管拒绝。续租调用独立 Agent renew，不换 generation、不重新 grant、不重启容器；瞬时失败最多 5 次，退避后失败会吊销身份。Agent 拒绝续租时封存当前代次；需显式 Retry 才能开始新代次。
- 安装器：`wp/pui-agent`。Agent 启动与包外信任根提交 `f1514a020b5b17c68eac39c1f36d8e32c28927f0`；候选清单对齐提交 `b918cd8dd81bcb2b42fc7951c047b15d32ae45ac`；无标签 r2 候选提交 `08045080f5573d08d8ff75a1fe440cfaf733f7b2`。要求 catalog 信任根从安装包外部提供；验签失败时不得启动 Agent。`plugins/homeassistant.json` 现对齐 r2 arm64 候选摘要。
- 前端：`wp/pui-frontend`，已提交 HEAD `c40ad2171e81e6677f24002f16c58e4cd65cbebc`。为避免纳入该工作区的 4 个未提交路由调整，隔离平台静态资源由该提交的干净 archive 构建并复载；页面仍显示候选“已启用 / 健康: ok”和既有接入点入口。构建成功；UnoCSS 对实验性 container query 与两个缺失图标有警告。原前端工作区未改。
- HA 仓库：候选构建取自干净提交 `4d8d45bd6ae67bf329a45f95dbfdac2fbda903eb`。工作区另有未提交修改，均保留且没有混入候选。

## 候选制品

目录：`/tmp/thingspanel-ha-candidate-20261106/release-r2`。版本 `1.0.0-candidate.20261106-4d8d45b-r2`，目标架构 `linux/arm64`，镜像 ID/digest `sha256:0747be19506d38d110a61519964c0f5b6617886d5eb475b303a17ff1c8e6a21c`。HA 安装字段为零；catalog 不包含 HA 地址或 HA token。初版 r1 因镜像 OCI 标签被 Agent allowlist 拒绝；r2 从同一干净源码提交去除镜像标签重建，已通过 Agent allowlist。

候选制品已复制到 500G 卷上的隔离证据工作区 `/Volumes/KIOXIA/CodexTests/thingspanel-ha-ui-acceptance-20261106/artifacts/release-r2-candidate/`；镜像 tar、catalog、插件 manifest 与 Agent 二进制 SHA-256 均与表格一致。

| 文件 | SHA-256 |
|---|---|
| `plugins/homeassistant-image.tar` | `5421f8ec4d62c5fabe4b5abd26312d5f13e9f4fe041fe0977dd932265e6bf220` |
| `plugins/plugin-catalog.json` | `3f07117cc601496d9df904a5dac9c0175bbfbb69049268a9475c0198eddd25d2` |
| `plugins/homeassistant.json` | `5203e2707b0aa1510b92725c36c25fb0e65736051a216067848e3a4b8ebecda7` |
| `bin/tp-agent` | `a741baddac7acb16127638dfd9aa17f8a1da49810a16c65ee34b58583e0fa78f` |

隔离 UI 安装固定组合：后端任务分支代码提交 `96d1964c652a91575de35f6193376bd44cde8552`（容器镜像 `thingspanel-backend-ha-ui:20261106`，image ID `sha256:bfea694a1db8bb0c76b318505c76a01a97451417fae655261a72b9844374136a`）；前端干净提交 `c40ad2171e81e6677f24002f16c58e4cd65cbebc`；安装器/Agent `wp/pui-agent` 提交 `08045080f5573d08d8ff75a1fe440cfaf733f7b2`；HA 源码取自干净提交 `4d8d45bd6ae67bf329a45f95dbfdac2fbda903eb`；插件版本 `1.0.0-candidate.20261106-4d8d45b-r2`、linux/arm64、镜像 digest `sha256:0747be19506d38d110a61519964c0f5b6617886d5eb475b303a17ff1c8e6a21c`。测试 UI 安装通过临时 test-only 签名目录执行，不能代替正式签名。

候选 release-r2 目录中的 catalog 未签名，不能通过签名强制安装器，也不能对外发布。为真实 Agent 运行验证，在独立隔离目录使用临时 test-only key 对同一 catalog bytes 签名；该测试签名、公钥和容器都不能冒充正式发布。还缺负责人提供/配置的正式 catalog 签名，以及平台安装环境中包外的对应可信公钥。

## 验证记录

- 后端：`go test ./internal/plugininstall ./internal/pluginidentity` 通过；定向安装 API、超级管理员身份范围测试通过。已有测试覆盖非 `SYS_ADMIN` 拒绝、平台安装作用域和身份租户隔离。
- 补充回归：`go test ./internal/api -run 'TestPluginInstallRequiresPlatformAdmin|TestPluginInstallRejectsDelegatedPlatformAdmin|TestPluginInstallDoesNotCollectTenantCredentials|TestPluginInstallNotConfiguredIsUnavailable' -count=1` 通过；新增委派 `SYS_ADMIN` 不能读取目录或提交安装的断言。
- 安装器：`python3 -m unittest tests.test_update_transaction tests.test_reconcile_activation tools.test_plugin_install -q`，69 项通过。
- 候选清单更新后：`python3 -m unittest tools.test_plugin_install -q`，18 项通过；候选 JSON 通过 `tools.plugin_catalog.check_entry` 校验，`env_user_fields=0`。
- r2 隔离实装：用临时 test-only 签名目录通过真实 `tp-agent` CatalogSource，Agent 在新建独立 Docker 网络中创建并启动镜像摘要 `sha256:0747be19506d38d110a61519964c0f5b6617886d5eb475b303a17ff1c8e6a21c`；插件 `/health` 返回 `status=ok`，容器 `restart_count=0`。
- Agent：linux/arm64 测试二进制在隔离 Alpine 容器运行，全套状态测试通过；目录签名/篡改测试、续租仅延长已启动且已接受代次测试通过。需要专用真实 Docker 验收授权的用例跳过。
- 续租拒绝、瞬时失败次数上限、无代次变化及身份吊销由后端引擎定向测试覆盖。首轮隔离长跑用 120 秒 TTL、45 秒间隔，在第 25 次成功后续租请求重试耗尽，租约到期后 Agent 吊销身份并正常停止插件；这次约 27 分钟的观察未通过。控制器没有记录最初一次续租失败的具体原因，不能归因于插件故障或外部宿主机问题。
- 按 Agent 的正确恢复规则没有续活过期的 generation 2，而是在全新 Agent 状态上显式接受 generation 3 并重新安装同一 r2 制品；以 600 秒 TTL、119 秒间隔连续运行 1800 秒，17 次同代续租均成功。插件于 `2026-11-06T03:15:35Z` 启动，至 `2026-11-06T03:49:11Z` 最终核查仍运行；generation=3、started=true、revoked=false，最后租期延至 `1793937518`，镜像摘要不变、`restart_count=0`。Agent 在运行中重启一次后，持久状态恢复为 generation=3、started=true、revoked=false，grant ID 未变；之后续租继续成功。`/health` 返回 `status=ok`。该验证是隔离 Agent/容器真实运行，不是 ThingsPanel 管理页面安装验收。
- 前端定向 lint 已通过；类型检查仍有既存错误：`packages/axios/src/index.ts:29`、`src/service/api/notification.ts:134`。本轮干净 archive 的 Vite build 也通过，只有上述 UnoCSS 警告。
- 续租解释：历史设计文档记载身份到期后需用更高代次重新签发；对已运行插件这会走换代流程，正是“每隔一段时间重新授权导致容器重装/重启”的风险来源。当前默认身份和 Agent grant TTL 都是 24 小时，管理程序每 5 分钟给“当前已运行代次”续期；同一 token、grant 和 generation 不变，不会重新授权或重启。到期边界的作用是在控制面长期失联时限制旧身份继续访问，并让 Agent 停止失去管理的容器；本轮未找到产品材料说明“24 小时”这个具体数值不可更改。永不过期会改变控制面失联时的行为和撤销保证；本次没有去掉到期保护，也没有证据表明租期导致这次 UI 安装失败。
- 500G 磁盘：当前挂载为 `/Volumes/KIOXIA`，卷校验通过，可用约 390 GiB。专用测试栈的工作目录、源码副本、候选包与验收证据放在 `/Volumes/KIOXIA/CodexTests/thingspanel-ha-ui-acceptance-20261106/`。Docker 上下文实际为 OrbStack；PostgreSQL 使用 Docker 管理的命名卷，容器内路径为 `/var/lib/docker/volumes/tp-ha-ui-acceptance-20261106_postgres_data/_data`，数据库约 111 MB。OrbStack 数据位于内置盘的 `~/Library/Group Containers/HUAQ24HBR6.dev.orbstack/data`，约 26 GiB；`~/OrbStack` 虚拟盘约 42 GiB、可用约 16 GiB，内置盘总可用约 17 GiB。没有迁移整个 OrbStack 数据目录，因为那会影响其他容器/卷；也没有替换现有测试数据库容器。
- 隔离 UI 安装验收：从任务后端分支构建后端镜像、从前端候选分支构建静态资源，并启动 PostgreSQL + TimescaleDB、Redis、Broker、平台服务及 Agent。首次管理员初始化成功，现有“应用管理 → 插件管理 → 插件安装”页面显示 `1.0.0-candidate.20261106-4d8d45b-r2`，安装确认页不要求 HA 地址或令牌。首次尝试在 `agent_prepare` 被 Agent 拒绝。对照 HA catalog 的 `env_from_stack` 和 Agent 实际栈 env 文件，确认源变量名不匹配：catalog 要求 `TP_PROTOCOL_MQTT_USER/PASSWORD`，隔离 Agent 文件原来写成插件内目标变量名。测试 Agent 把此类配置问题统一回报成 `agent_rejected`，因此页面没有给出可操作原因。修正隔离栈 env、轮换该隔离栈 Broker 凭据并刷新 Broker owner 后，通过已有 Retry 流程用新 generation=4 安装成功；页面显示“已启用 / 健康: ok”，并出现“去配置 HA 接入点”入口。数据库事件显示 check、identity、prepare、grant、activate、register、health 全部通过。Agent 插件容器使用相同版本和镜像 digest，`restart_count=0`。
- 隔离栈创建时的调试命令曾误回显测试 Broker 凭据；已在该隔离栈轮换凭据并刷新 Broker owner/Mosquitto，未改正式环境或共享容器。没有记录或回显新凭据。
- 隔离验收期间曾为诊断 Agent socket 增加本地代理配置，执行 `docker compose up -d backend` 时 Compose 替换了该专用测试栈的 backend 容器，违反了本任务“不删除容器”的现场要求。已告知负责人；数据库及配置绑定数据保留，其他容器/数据卷没有被删除或重建。之后改用原容器原位 `docker restart` 验证恢复。
- 当前固定组合的正式 30 分钟监测于 `2026-11-06T05:15:59Z` 开始，于 `2026-11-06T05:46:14Z` 完成：31 个逐分钟样本全部通过。每分钟检查平台身份 expiry、Agent grant expiry（允许两次独立续期最多 60 秒时差）、generation、插件健康、容器 restart count 和固定容器 ID；generation 始终为 4，安装状态 enabled、失败/重试计数为 0；多次续期均延长有效期，容器 ID 和镜像 digest 保持不变，restart_count=0，健康状态为 ok。证据位于隔离栈目录 `evidence/ha-ui-install-stability-final-v2-20261106.txt`（不入仓库）。早期观察分别因脚本布尔值解析、补充容器 ID 检查及 expiry 精确相等假设而停止，没有发现产品运行异常；正式计时采用最终校验逻辑。
- 用户对续租必要性的追问后核对：后台的 5 分钟 reconcile 行锁只防并发；身份和 Agent 运行 grant 的 24 小时有效期每 5 分钟由后台延长。有效期不出现在插件安装表单，也不会让正常插件换代或重启。它为控制面长期失联时提供有界停止与令牌失效边界；本轮隔离安装故障是测试 Agent 的 `stack.env` 源变量名与插件 catalog `env_from_stack` 不匹配，修正后现有 UI 安装成功，因此当前证据不支持“租期造成无法安装”。改为永不过期会改变离线失联、撤销和 Agent 恢复时的安全契约；本轮沿用任务原有“活身份续期、过期不复活”的要求，没有擅自改为永久身份。
- 本轮回归：`go test ./internal/plugininstall ./internal/pluginidentity` 通过；`go test ./internal/api -run '^TestPluginInstall' -count=1` 通过。较宽的 `go test ./internal/plugininstall ./internal/pluginidentity ./internal/api` 有既存失败 `TestTelemetryHTTPDeviceAuthorization/owner_key`（`telemetry_http_auth_test.go:371`，预期 owner_key 被允许但返回 `40103`），不属于 HA 安装代码。Agent 全包测试受 Linux build tag 限制，不能在当前 macOS host 直接执行；此前已在 Linux/arm64 隔离容器记录相关 Agent 测试结果。
- 没有验证 HA 实体数据链，按用户最新范围不属于本次验收。

## 交接前置项

1. 负责人提供正式签名流程/密钥管理方签出的 catalog 签名，并在目标主机包外分发匹配的信任根；随后重核 catalog、镜像摘要和目标机器架构。
2. 本任务固定组合的 UI 一键安装与 30 分钟稳定监测已完成；另已对 backend 和 Agent 各做一次原位 restart，generation、插件进程和健康状态均恢复。
3. 目前隔离 UI 栈已建在 500G 外置盘，足够复用后续验收；该栈使用本地 test-only catalog 签名和测试凭据，不可当正式发布环境。

各工作区与分支未删除或重置；未推送、未合并、未发布。诊断期间一次 Compose 操作意外替换了本任务独占的 backend 容器（详见上文）；除此以外没有删除或重建容器、数据卷，也没有操作其他容器或卷。
