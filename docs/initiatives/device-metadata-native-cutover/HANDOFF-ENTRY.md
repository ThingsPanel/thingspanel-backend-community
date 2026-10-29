# 交接入口（先读这个）— 设备 / 设备模板 / 设备分组原生化

更新：2026-10-29。目标读者：把这批改动合并进 `main-1.3.0` 的同事。读完本页应能回答：改了什么、在哪个分支、怎么合、怎么验、哪些没做、哪些有风险。

## 1. 一句话结论

- **代码和自动化测试已完成**，三个菜单的 27 个功能块都有对应的 Encore 接口和页面，后端和前端自动化测试通过（见 §5）。
- **没有任何一块做过真实端到端验收（0/27）**：没有在“真实浏览器 + 真实 Community 运行时”下跑过。这台开发机（Mac）起不来完整 Encore 应用（`notification` 服务读 `/proc/self/environ`，只能 Linux），在 Mac 上构建 Linux 镜像两次都卡死，已终止。**合并后必须在 Linux/CI 上按 `E2E-CHECKLIST-2026-10-29.md` 走一遍。**
- 有 5 类操作提交后会**一直显示“待处理”**，因为 Community 没有对应能力（见 §6）。这是有意的诚实表现，不是缺陷。

## 2. 代码在哪（三个仓库，同名分支 `codex/metadata-menus-native-completion`）

| 仓库 | 远程 | 基线 | 本分支提交 | 与远程 `main-1.3.0` 的关系 |
|---|---|---|---|---|
| kernel | `zjhong/iot-api-encore-kernel` | `fafe0f6f`（= `main-1.3.0`） | 7 个 | 只领先 7、不落后，**可直接快进合并** |
| frontend | `ThingsPanel/thingspanel-frontend-community` | `7d331d15` | 2 个功能提交 + 1 个合并提交 | 原分支 `codex/metadata-menus-native-completion` 落后 9；**已另建集成分支 `codex/metadata-menus-native-completion-on-1.3`，已合入 `main-1.3.0` 并解决 16 个冲突，领先 3、不落后，可快进合并**（§4） |
| backend | `ThingsPanel/thingspanel-backend-community` | `d638af3e` | 仅文档（`docs/initiatives/device-metadata-native-cutover/`） | 无代码改动；文档是新目录 |

kernel 的 7 个提交（按顺序）：
1. `build(encore-api): add gopher-lua …`：只含 `go.mod/go.sum`，便于单独核对依赖。
2. `feat(tpctl-go): …`：Go CLI。
3. `feat(devicemetadata): database migrations 0018-0039 …`：22 个迁移，**必须整段一起合并**。
4. `feat(encore-api): native device, template, group, runtime and export APIs`：接口、业务代码、测试。
5. `docs: handoff entry, …`：`docs/handoff-device-metadata/` 下的交接文档副本。
6. `test(devicemetadata): keep credential fixture timestamps …`：修复凭据测试的偶发失败。
7. `test: give legacy l3-core and verifier tests …`：旧服务测试显式声明身份（见 §5）。

frontend 的 2 个提交：功能（页面、传输层、测试）；以及把 4 个从未被 vitest 收集的 API 单元测试挪到 `src/__tests__/unit` 使其真正运行。

## 3. 合并顺序（建议）

1. **kernel**：先合 `codex/metadata-menus-native-completion` 到 `main-1.3.0`（快进）。数据库迁移目录以 `encore-api/devicemetadata/db.go` 的声明为准，不是仓库根的 `migrations/`。
2. **frontend**：合并**集成分支** `codex/metadata-menus-native-completion-on-1.3`（冲突已解决，§4），再跑 §5 的前端测试。
3. **backend**：合并文档目录（无代码）。
4. 在 Linux 环境启动完整应用，按 `E2E-CHECKLIST-2026-10-29.md` 做真实验收，逐块签收。

部署前要设置的环境变量与权限见 `HANDOFF-MERGE-1.3-2026-10-29.md` §8（重点：`TP_RUNTIME_OUTBOX_DISPATCH=on` 才会真正向 Community 下发改编号/删除/绑定子设备；`TP_BASE`/`TP_API_KEY` 等指向 Community；`METADATA_CREDENTIAL_KEYRING_B64`、`METADATA_CREDENTIAL_ACTIVE_KEY_REF` 凭据加密；`TP_DEVICE_CONNECTION_ENDPOINT`；`TP_AUTH_MODE`）。

## 4. 前端合并冲突（已解决，在集成分支里）

`main-1.3.0` 上另有人合入了自动化页面（场景、联动、告警接到新引擎，并下线了独立的规则列表页）。原分支与它有 16 个冲突文件，几乎都在自动化，与设备元数据无关。集成分支 `codex/metadata-menus-native-completion-on-1.3` 的处理（合并提交 `ee04653b`）：

- 自动化相关文件（`views/automation/*`、`service/api/automation-*`、`utils/automation/*`、`router/elegant/*`、`typings/elegant-router.d.ts`）和设备/模板详情里的两个“自动化”tab（`views/device/details/modules/automate.vue`、`views/device/config-detail/modules/automate.vue`）**全部以 `main-1.3.0` 为准**，即它们用的是新引擎的 `EffectiveRules` + `sceneLinkage`，不再是本分支早期的 `NativeAutomationPanel`。
- `typings/env.d.ts` 两边声明都保留。
- 去掉了本分支对 `rule-manage` 路由的预注册（该页面在 `main-1.3.0` 已删除）及对应权限测试用例，删除了残留的 `views/automation/rule-manage/`。
- 删除了 `__tests__/unit/automation-rules-api.test.ts`：它测的是被新引擎取代的旧版自动化接口，`main-1.3.0` 自带新的自动化测试。
- `views/device/shared/native-automation-panel.vue`、`native-device-alarms.vue` **仍在使用**：分别是模板“告警”tab（`config-detail/modules/alarm-info.vue`）和设备“告警”tab（`details/modules/give-an-alarm.vue`），不是遗留文件，不要删。
- **后端依赖**：新自动化 tab 调用的 `/v1/device-automation/...`、`/v1/rules/...` 等接口由 kernel 的 `encore-api/automationowner/` 提供（105 个文件，`main-1.3.0` 上没有，随本次 kernel 提交带入）。合并前端而不合并 kernel 的这一部分，自动化 tab 会没有后端。`automationowner` 的单元测试全部通过，但没有在真实环境跑过。
- 提醒：设备/模板的“自动化”tab 现在用的是 `main-1.3.0` 的实现，**我没有在真实环境验证它在这批新后端下的表现**。

## 5. 测试状态（如实）

| 范围 | 命令 | 结果 |
|---|---|---|
| 后端（需 Encore） | 在 `kernel/encore-api` 执行 `encore test ./devicemetadata ./internal/...` | 通过。整个 Encore 模块 `encore test ./... -count=1` 通过，仅 `cmd/notification-migrate` 和 `devicemetadata/migrations/scripts` 两个 `package main` 工具包在 `encore test` 下无法链接（前者在 `main-1.3.0` 上同样如此，后者用 `go test ./devicemetadata/migrations/scripts` 通过）。曾有 `TestDeviceCredentialsSchemaConstraints` 偶发失败（Go 时间与数据库 `NOW()` 的微秒差触发 `activated_at >= created_at` 检查），已把夹具时间提前一分钟修复；之后无缓存全量运行全部通过 |
| 前端 vitest | 在 `frontend`（集成分支）执行 `npx vitest run src/router src/__tests__/unit` | 167 过、1 败：`thingsvis-auth`“网络失败后进入冷却”，报 SSO authority 配置错误，**与本次改动无关**，未处理 |
| 前端 node:test | `npx tsx --test src/service/api/<文件>.test.ts` | 通过 |
| 真实端到端 | — | **0/27** |

汇总（以 `E2E-CHECKLIST-2026-10-29.md` 为准）：代码 27/27，自动化 27/27，有局部端到端证据 10/27（旧文档继承、合成登录），整块签收 0/27。

**测试身份变更**：单元测试里的默认身份由 `platform_admin` 收紧为 `TENANT_ADMIN`（`authn.CurrentData`），所以 `l3-core/automation`、`l3-core/pluginmgr`、`l5-ops/verifier` 里依赖旧超级身份的测试已显式声明 `platform_admin`。新写测试时注意默认身份没有 `device:manage`/`plugin:manage`。

注意：`encore check` 在本机会因 notification 服务配置卡住，不要用它当门禁；用 `encore test` 和 `go vet ./...`。

## 6. 提交后一直“待处理”的操作（Community 缺能力）

设备启停、换模板、凭据轮换/撤销、解绑子设备。原因：Community 的更新接口没有启用字段，`is_enabled` 不参与 MQTT 鉴权，凭据没有按 generation 生效的账本；MQTT 接入强制层不在本仓库。要让它们真正生效，需要在 Community / MQTT 鉴权层补能力。改设备编号、删除设备、绑定子设备由 outbox 投递器真实下发（需开 `TP_RUNTIME_OUTBOX_DISPATCH=on`，**尚未与真实 Community 联调**）。

## 7. 有意没做的

| 项 | 原因 |
|---|---|
| `latest-reading-deletions`（删除最新值） | Community 的删除接口会连历史一起删，而契约要求只删最新值缓存，硬接会误删历史 |
| 属性导出 / 事件导出 | 属性只存最新值无历史；事件列表接口没有时间范围参数 |
| 超过 100 万行或 60 MiB 的导出 | 超限时任务失败并给出原因，不产生半截文件 |
| 实时 WebSocket | 仍走旧路径 |
| 取消长导出、多副本抢任务压测 | 未做 |
| `simulator-config` 的 `topics` | 恒为空数组：本仓库没有按用途的 topic 和样例报文来源，不编造 |

图表历史曲线是“原生优先、失败回退旧接口”，所以不会退化，但未在真实浏览器验证。

## 8. 本阶段新增能力速览（详见 `HANDOFF-MERGE-1.3-2026-10-29.md`）

- 运行时回执入库 + outbox 投递器（改编号/删除/绑定子设备）。
- Lua 脚本沙箱：模板“数据处理”可调试运行（超时、内存、禁用 `os`/`io`）。
- 设备模拟器（profile / session / run）、`simulator-config`。
- 遥测聚合读数；历史导出：≤24 小时同步，24 小时到 31 天为后台分片长导出（每分钟定时任务，可续传，迁移 0039）；`GET /reading-exports/{operationId}`；过期文件每小时清理。
- 设备凭据列表直接显示明文（已撤销/证书除外）；设备编号唯一性校验。
- 图表/子设备/事件上报页面测试补齐。

## 9. 文档地图（都在本目录）

| 文件 | 用途 |
|---|---|
| **HANDOFF-ENTRY.md** | 本页，入口 |
| **HANDOFF-MERGE-1.3-2026-10-29.md** | 完整交接：27 个功能块逐块状态、56+ 条路由、迁移表、待处理操作、环境变量、风险清单、合并范围 |
| **E2E-CHECKLIST-2026-10-29.md** | 验收勾选表（代码/自动化/E2E 三栏）与外部依赖清单 |
| TEST-DEVICE-2026-10-29.md | 测试记录 |
| FUNCTION-STATUS-2026-10-27.md | 功能状态（个别描述已过时，以 HANDOFF-MERGE §2 为准） |
| API-CONTRACT.md / openapi.yaml | 接口契约 |
| DATABASE.md、DECISIONS.md、TASKS.md | 设计、决策、任务 |

kernel 仓库里的副本：`docs/handoff-device-metadata/`（入口、完整交接、验收清单），保证只拿 kernel 的人也能看到。

## 10. 合并前必须知道的风险

1. 迁移 0018–0039 必须整段应用，缺一个应用起不来；0039 是本次最后一个（长导出分片表）。
2. 前端设备/模板“自动化”tab 现在是 `main-1.3.0` 的实现，未在真实环境验证（§4）。
3. 没有真实端到端；outbox 投递器和长导出从未对真实 Community 运行。
4. 长导出每分钟由 cron 领取，用 `FOR UPDATE SKIP LOCKED`，逻辑上多副本安全，但未压测。
5. 凭据明文显示是产品决定，请确认权限（`device:credentials:read`）只授予租户管理员。
6. kernel 根目录有 4 个 `group-delete-targeted*.log` 临时日志，**未提交**，可直接删除。
