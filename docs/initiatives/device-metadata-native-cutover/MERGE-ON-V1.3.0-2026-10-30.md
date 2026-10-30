# 设备元数据功能合并到 v1.3.0 发布（2026-10-30）

> 目的：同事已发布 `v1.3.0`（backend `main` @ `7f694779`，frontend `master` @ `b911926f`，10/29）。本文说明我们的设备元数据功能如何落到这版发布上，已验证到什么程度，以及合并前要确认的事。**取代 `HANDOFF-ENTRY.md` §3 的“合并顺序”**（那一节以 kernel `main-1.3.0` 为基线，已过期）。

## 1. 先知道的事实

| 事实 | 说明 |
|---|---|
| 发布构建的是 backend 仓库里的 `encore-api/` | `deploy-encore.yml` 就构建它，不是 kernel 仓库 |
| 发布里没有我们的设备元数据功能 | 发布的 `devicemetadata` 只有旧的 36 个文件，我们的是 184 个 |
| 发布的 Encore 应用有意瘦身 | 删掉了旧的 `l3-core/{automation,control,devices,pluginmgr,telemetry}`、`internal/legacy`、`internal/closedloop`、`l5-ops`、连接器部署命令、迁移 `0017` |
| 发布自带自动化引擎 `encore-api/automation` | 与我们 kernel 的 `automationowner` 有 57 条相同路由，所以不再带 `automationowner` |

## 2. 两个新分支（只在本地/已推送，未碰 `main`/`master`）

| 仓库 | 分支 | 基于 | 内容 |
|---|---|---|---|
| backend | `codex/metadata-menus-on-v1.3.0` | `origin/main` @ `7f694779` | 把设备元数据代码移植进 `encore-api/`；合入交接文档 |
| frontend | `codex/metadata-menus-on-v1.3.0` | `origin/master` @ `b911926f`（合并） | 合入 `master`，解决 4 个冲突 |
| kernel | `codex/metadata-menus-native-completion` | `main-1.3.0` | 代码来源，保留历史；今后以 backend 分支为准 |

### 后端移植做了什么
- 以 kernel `main-1.3.0` 为基线做文件级三方合并：新增约 210 个文件、自动合并 5 个（`internal/authz/engine.go`、`l1-access/authn/auth.go`、`thingmodel/postgres*.go`、`thingmodel/service.go`）；不带 `automationowner`（发布的 `automation` 路由相同）。
- **不是把发布删掉的东西原样搬回去**。发布瘦身是对的，我们只保留设备元数据真正需要的，并改成职责清楚的样子：

| 模块 | 做法 | 理由 |
|---|---|---|
| `internal/communityclient`（原 `internal/legacy`） | 保留，改名；**去掉默认地址 `demo.thingspanel.cn` 和默认账号 `test@test.cn/123456`**；没配置时所有调用返回明确的“设备运行时未配置”（不会被当成 Community 拒绝）；优先用 API Key | 设备元数据的读数、命令、导出、下发都要经它到 Community，职责是“设备运行时客户端”；默认账号和演示服务器绝不能进产品 |
| `l3-core/connectorcatalog`（原 `pluginmgr`） | 只保留“连接器目录”（`/api/v2/connection-providers` 及其库、模式校验）；**不带回**原来的 SSH 安装适配器、K8s 运行器、旧设备连接器 | 模板编辑器要选连接器，所以要目录；远程执行命令的安装功能不该出现在发布里 |
| `internal/closedloop` | 不带 | 没有任何代码使用 |
| 迁移 `0017` | 不带 | 发布已删；我们从 `0018` 起，空缺不影响 |
| `go` / `encore.dev` | `go` 1.24.0、`encore.dev` v1.57.13 | `fileassets`（模板封面图上传）用到 `encore.dev/storage/objects`，旧版没有；发布流水线按 `go.mod` 取 Go 版本 |

- **环境变量统一到发布的命名**：`TP_COMMUNITY_BASE_URL`（与发布的自动化共用）、`TP_COMMUNITY_API_KEY`（推荐）或 `TP_COMMUNITY_EMAIL` + `TP_COMMUNITY_PASSWORD`（专用服务账号）。旧的 `TP_BASE / TP_API_KEY / TP_EMAIL / TP_PASSWORD` 不再识别。
- `l1-access/authn/auth_test.go` 改成新的 `AuthHandler(ctx, *AuthParams)` 调用。

### 前端合并做了什么
冲突 4 个：`thingsvis-auth.test.ts`（取 master）、`router/routes/index.ts`（两边的导入都保留）、`device/details/index.vue`（去掉 master 里一行多余的 `ref('')`）、`device/manage/index.vue`（保留 master 的统计接口 404 容错）。另删除 4 个 master 没有的旧自动化文件，`permission.test.ts` 用 master 的，告警列表客户端兼容发布的响应（只有 `alarms`，没有 `total/limit/offset`）。

## 3. 在合并结果上的验证

| 项目 | 结果 |
|---|---|
| 后端全包测试（`thingmodel` `devicemetadata` `internal` `l1-access` `l3-core` `fileassets` `automation` `northbound`） | 全部通过 |
| 合并后的完整应用启动（新库，迁移从头跑） | 正常，约 24 秒 |
| 纯 HTTP 端到端 | 43/44；唯一失败是脚本要求“无 Community 时读数返回 503”，这次接着替身所以是 200，属环境差异 |
| 接 Community 替身的端到端 | 31/31 |
| 前端完整类型检查 `npm run typecheck` | 0 错误 |
| 前端单元测试 | 313 个通过；node:test 相关文件通过 |
| 页面（合并后前端 + 合并后后端） | 设备“连接”（凭据已生效、轮换/撤销）、“自动化”（显示我们创建的规则，来自发布的引擎）、“告警”、“设置”（模板只读）都正常 |

合并结果上另外走过：模板列表复制（201，跳到新模板编辑页）、删除被设备占用的模板（提示点名“占用设备”）、删除未占用的模板、数值历史曲线、文本历史表（288 行）和 CSV 导出、命令下发（设备已应答）、期望消息创建与取消。

## 4. 合并前需要你们确认的事

1. **设备元数据通过 Community 的 REST 接口（`communityclient`）取读数、发命令、下发改动**。这是目前唯一的实现方式；若发布希望换成 Community 的内部专用接口（像自动化的 `/internal/automation`），需要在 Community 侧新增，那是另一项工作。
2. **迁移 `0017` 被发布删除**：确认线上库没有依赖它。
3. 部署需要配置（Compose / 部署脚本里给 Encore 服务）：`TP_COMMUNITY_BASE_URL`、`TP_COMMUNITY_API_KEY`、`METADATA_CREDENTIAL_KEYRING_B64` 与 `METADATA_CREDENTIAL_ACTIVE_KEY_REF`，自托管必须 `TP_BACKGROUND_WORKERS=on`，要让改编号/删除/绑定/解绑/凭据真正下发要 `TP_RUNTIME_OUTBOX_DISPATCH=on`。**目前发布的部署脚本（`deploy/community-test`）没有这些变量，需要加。**

## 5. 仍然没做
整块签收 0/27：以上都只在 Community 测试替身下验证，真实 Community 联调、Linux 环境完整验收还没做。
