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
- 以 kernel `main-1.3.0` 为基线做文件级三方合并：新增 212 个文件、改 8 个、自动合并 5 个（`internal/authz/engine.go`、`l1-access/authn/auth.go`、`thingmodel/postgres*.go`、`thingmodel/service.go`）。
- 整包恢复 `internal/legacy`、`internal/closedloop`、`l3-core/pluginmgr`：新代码的命令、读数、下发、凭据和“连接器目录/设备配置校验”都依赖它们。**发布有意删掉了它们，这是需要你们确认的取舍（见 §4）。**
- `go.mod`/`go.sum` 以发布版本为准，再 `go mod tidy`：`go` 1.23.0 → 1.24.0，`encore.dev` v1.30.0 → v1.57.13，`pgx` 5.7.2 → 5.7.6，新增 `miniredis`、`testify` 升级。发布流水线用 `go-version-file: encore-api/go.mod`，所以会自动用 1.24。
- `l1-access/authn/auth_test.go` 改成新的 `AuthHandler(ctx, *AuthParams)` 调用。
- 迁移：发布到 `0016`，我们从 `0018` 起；`0017` 被发布删除，**没有恢复**（空缺不影响，全包测试和启动都通过）。

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

没有重新验证的：模板列表复制/删除、历史表与导出、命令/期望消息等页面（代码未改，但没在合并结果上再走一遍）。

## 4. 合并前需要你们确认的事

1. **是否接受设备元数据继续依赖 `internal/legacy` 去调 Community（REST）**。发布有意删了它；不恢复的话命令、读数、导出、凭据/子设备/改号/删除下发都无法工作。
2. **`go` 1.24 与 `encore.dev` v1.57.13** 的版本提升是否可接受（发布原为 1.23 / v1.30.0）。
3. **迁移 `0017` 被删**：确认线上库没有依赖它。
4. 部署环境变量见 `HANDOFF-MERGE-1.3-2026-10-29.md` §8；自托管必须 `TP_BACKGROUND_WORKERS=on`，要让改编号/删除/绑定/解绑/凭据真正下发要 `TP_RUNTIME_OUTBOX_DISPATCH=on`，并配 `TP_BASE` 等指向 Community，以及 `METADATA_CREDENTIAL_KEYRING_B64`。

## 5. 仍然没做
整块签收 0/27：以上都只在 Community 测试替身下验证，真实 Community 联调、Linux 环境完整验收还没做。
