# HA 服务插件发布与安装收尾记录（2026-11-06）

状态：**候选版本与隔离 Agent 长跑/续租验收已完成；现有 ThingsPanel 页面已识别候选，但一次真实 UI 安装在 Agent prepare 步骤失败。正式签名、UI 安装通过和对外发布均未完成。**

## 范围决定

本次按用户最新决定只收尾 HA 第三方服务插件的一键安装。安装不收集 HA 地址或令牌；租户接入点配置以及设备上报/控制验收不在本次范围。协议插件、通用市场和自动升级均未处理。

## 已有实现与改动

- 后端：`claude/wp-plugin-install-ui-8bf83e`。功能提交 `f0c493d6ab2ade47dd0aa80d323708eaebd24323`，本交接记录提交 `c2279c21a90f778e2592ec9f7112611df050e1aa`，后续权限回归提交 `d56d6151998951b5f7aff067c853b205469d3f1c`。沿用现有插件安装引擎和 UI API；安装范围为平台级，由 `SYS_ADMIN` 操作，非超管拒绝。续租调用独立 Agent renew，不换 generation、不重新 grant、不重启容器；瞬时失败最多 5 次，退避后失败会吊销身份。Agent 拒绝续租时封存当前代次；需显式 Retry 才能开始新代次。
- 安装器：`wp/pui-agent`。Agent 启动与包外信任根提交 `f1514a020b5b17c68eac39c1f36d8e32c28927f0`；候选清单对齐提交 `b918cd8dd81bcb2b42fc7951c047b15d32ae45ac`；无标签 r2 候选提交 `08045080f5573d08d8ff75a1fe440cfaf733f7b2`。要求 catalog 信任根从安装包外部提供；验签失败时不得启动 Agent。`plugins/homeassistant.json` 现对齐 r2 arm64 候选摘要。
- 前端：`wp/pui-frontend`，HEAD `c40ad2171e81e6677f24002f16c58e4cd65cbebc`。沿用已有插件页面；安装后指向现有服务接入流程。
- HA 仓库：候选构建取自干净提交 `4d8d45bd6ae67bf329a45f95dbfdac2fbda903eb`。工作区另有未提交修改，均保留且没有混入候选。

## 候选制品

目录：`/tmp/thingspanel-ha-candidate-20261106/release-r2`。版本 `1.0.0-candidate.20261106-4d8d45b-r2`，目标架构 `linux/arm64`，镜像 ID `sha256:0747be19506d38d110a61519964c0f5b6617886d5eb475b303a17ff1c8e6a21c`。HA 安装字段为零；catalog 不包含 HA 地址或 HA token。初版 r1 因镜像 OCI 标签被 Agent allowlist 拒绝；r2 从同一干净源码提交去除镜像标签重建，已通过 Agent allowlist。

| 文件 | SHA-256 |
|---|---|
| `plugins/homeassistant-image.tar` | `5421f8ec4d62c5fabe4b5abd26312d5f13e9f4fe041fe0977dd932265e6bf220` |
| `plugins/plugin-catalog.json` | `3f07117cc601496d9df904a5dac9c0175bbfbb69049268a9475c0198eddd25d2` |
| `plugins/homeassistant.json` | `5203e2707b0aa1510b92725c36c25fb0e65736051a216067848e3a4b8ebecda7` |
| `bin/tp-agent` | `a741baddac7acb16127638dfd9aa17f8a1da49810a16c65ee34b58583e0fa78f` |

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
- 前端定向 lint 已通过；类型检查仍有既存错误：`packages/axios/src/index.ts:29`、`src/service/api/notification.ts:134`。
- 续租解释：当前默认身份和 Agent grant TTL 都是 24 小时，管理程序每 5 分钟给“当前已运行代次”续期；同一 token、grant 和 generation 不变，不会重新授权或重启。它的作用是在控制面长期失联、身份被吊销或 Agent/后端恢复异常时提供到期停止边界，不是要求用户定期重装。真实隔离长跑已证明连续 30 分钟以上稳定；没有证据表明租期导致平台不可用，因此本次没有去掉到期保护。
- 500G 磁盘：当前挂载为 `/Volumes/KIOXIA`，卷校验通过，可用约 390 GiB。专用 ThingsPanel 测试栈和数据目录放在 `/Volumes/KIOXIA/CodexTests/thingspanel-ha-ui-acceptance-20261106/`；系统盘仍只有约 19 GiB 可用。没有改 Docker 全局存储位置，也没有操作已有共享容器/卷。
- 隔离 UI 安装验收：从任务后端分支构建后端镜像、从前端候选分支构建静态资源，并启动 PostgreSQL + TimescaleDB、Redis、Broker、平台服务及 Agent。首次管理员初始化成功，现有“应用管理 → 插件管理 → 插件安装”页面显示 `1.0.0-candidate.20261106-4d8d45b-r2`，安装确认页明确不要求 HA 地址或令牌。确认安装后页面失败；数据库事件顺序为 `check=ok`、`identity=ok`、`agent_prepare=agent_rejected`，没有进入 grant/activate/health。Agent catalog 可列出该候选，Agent socket/token 文件接线可读，候选镜像摘要在 Docker 中存在；现有安装适配器丢弃 Agent 响应体，只留下通用 `agent_rejected`，所以确切的 Agent 拒绝原因尚未取得。没有重复点重试，避免在未定位原因时反复提交新代次。
- 本轮回归：`go test ./internal/plugininstall ./internal/pluginidentity` 通过；`go test ./internal/api -run '^TestPluginInstall' -count=1` 通过。较宽的 `go test ./internal/plugininstall ./internal/pluginidentity ./internal/api` 有既存失败 `TestTelemetryHTTPDeviceAuthorization/owner_key`（`telemetry_http_auth_test.go:371`，预期 owner_key 被允许但返回 `40103`），不属于 HA 安装代码。Agent 全包测试受 Linux build tag 限制，不能在当前 macOS host 直接执行；此前已在 Linux/arm64 隔离容器记录相关 Agent 测试结果。
- 没有验证 HA 实体数据链，按用户最新范围不属于本次验收。

## 交接前置项

1. 负责人提供正式签名流程/密钥管理方签出的 catalog 签名，并在目标主机包外分发匹配的信任根；随后重核 catalog、镜像摘要和目标机器架构。
2. 修正/查明 `agent_prepare` 的 `agent_rejected` 根因后，在此隔离平台显式 Retry 并验证同一页面安装成功；当前候选安装状态为失败，不能交付使用。
3. 目前隔离 UI 栈已建在 500G 外置盘，足够复用后续验收；该栈使用本地 test-only catalog 签名和测试凭据，不可当正式发布环境。

各工作区未推送、未合并、未发布；未删除或重置任何工作区、分支、容器和数据卷。
