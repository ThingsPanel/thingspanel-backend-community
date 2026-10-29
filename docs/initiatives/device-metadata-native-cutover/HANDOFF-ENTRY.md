# 交接入口（先读这个）— 设备 / 设备模板 / 设备分组原生化

更新：2026-10-29。目标读者：把这批改动合并进 `main-1.3.0` 的同事。读完本页应能回答：改了什么、在哪个分支、怎么合、怎么验、哪些没做、哪些有风险。

## 1. 一句话结论

- **代码和自动化测试已完成**，三个菜单的 27 个功能块都有对应的 Encore 接口和页面，后端和前端自动化测试通过（见 §5）。
- **本机真实 HTTP 端到端（后端子集）44/44 通过，并在真实浏览器里打开了设备管理、设备详情（10 个 tab）、设备模板、分组管理页面，且在页面里新建了分组并在后端核对**（登录用测试替身，数据来自真实 Encore 后端）：完整 Encore 应用 + 真实 Postgres + 真实请求，覆盖认证权限、设备、分组、凭据、模拟器、导出、操作，见 `LOCAL-E2E.md`。它没有浏览器、没有 Community，notification 被替换成空实现，所以不等于整块签收。
- **用 Community 测试替身跑通了需要 Community 的后端路径 31/31**（读数、历史、聚合、短/长导出、改编号和删除的真实下发与回执），并因此修了“删除成功后设备仍在列表里”的 bug；替身不是真实 Community，见 `LOCAL-E2E.md`。
- **没有任何一块做过“真实浏览器 + 真实 Community”的整块验收（0/27）**；这次还跑出并修了租户管理员无权使用指令下发/期望消息/调试的真实问题：没有在“真实浏览器 + 真实 Community 运行时”下跑过。这台开发机（Mac）起不来完整 Encore 应用（`notification` 服务读 `/proc/self/environ`，只能 Linux），在 Mac 上构建 Linux 镜像两次都卡死，已终止。**合并后必须在 Linux/CI 上按 `E2E-CHECKLIST-2026-10-29.md` 走一遍。**
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

**自托管部署必须设置 `TP_BACKGROUND_WORKERS=on`**：Encore 的 `cron` 只在 Encore Cloud 触发，本机和自托管默认不会运行长导出、过期文件清理、outbox 投递器；这个开关在服务进程内运行同样三个任务（见 `LOCAL-E2E.md`）。

部署前要设置的环境变量与权限见 `HANDOFF-MERGE-1.3-2026-10-29.md` §8（重点：`TP_RUNTIME_OUTBOX_DISPATCH=on` 才会真正向 Community 下发改编号/删除/绑定子设备；`TP_BASE`/`TP_API_KEY` 等指向 Community；`METADATA_CREDENTIAL_KEYRING_B64`、`METADATA_CREDENTIAL_ACTIVE_KEY_REF` 凭据加密；`TP_DEVICE_CONNECTION_ENDPOINT`；`TP_AUTH_MODE`）。

## 4. 前端合并冲突（已解决，在集成分支里）

`main-1.3.0` 上另有人合入了自动化页面（场景、联动、告警接到新引擎，并下线了独立的规则列表页）。原分支与它有 16 个冲突文件，几乎都在自动化，与设备元数据无关。集成分支 `codex/metadata-menus-native-completion-on-1.3` 的处理（合并提交 `ee04653b`）：

- 自动化相关文件（`views/automation/*`、`service/api/automation-*`、`utils/automation/*`、`router/elegant/*`、`typings/elegant-router.d.ts`）和设备/模板详情里的两个“自动化”tab（`views/device/details/modules/automate.vue`、`views/device/config-detail/modules/automate.vue`）**全部以 `main-1.3.0` 为准**，即它们用的是新引擎的 `EffectiveRules` + `sceneLinkage`，不再是本分支早期的 `NativeAutomationPanel`。
- `typings/env.d.ts` 两边声明都保留。
- 去掉了本分支对 `rule-manage` 路由的预注册（该页面在 `main-1.3.0` 已删除）及对应权限测试用例，删除了残留的 `views/automation/rule-manage/`。
- `service/api/automation-native.ts` 以 `main-1.3.0` 为准；我们原来的原生自动化客户端放在新文件 `service/api/automation-native-client.ts`，`native-automation-panel.vue`、`native-device-alarms.vue`（模板/设备“告警”tab）改为从它引用。不要再把它们改回 `automation-native`，否则页面加载失败。
- 删除了 `__tests__/unit/automation-rules-api.test.ts`：它测的是被新引擎取代的旧版自动化接口，`main-1.3.0` 自带新的自动化测试。
- `views/device/shared/native-automation-panel.vue`、`native-device-alarms.vue` **仍在使用**：分别是模板“告警”tab（`config-detail/modules/alarm-info.vue`）和设备“告警”tab（`details/modules/give-an-alarm.vue`），不是遗留文件，不要删。
- **后端依赖**：新自动化 tab 调用的 `/v1/device-automation/...`、`/v1/rules/...` 等接口由 kernel 的 `encore-api/automationowner/` 提供（105 个文件，`main-1.3.0` 上没有，随本次 kernel 提交带入）。合并前端而不合并 kernel 的这一部分，自动化 tab 会没有后端。`automationowner` 的单元测试全部通过，但没有在真实环境跑过。
- 提醒：设备/模板的“自动化”tab 现在用的是 `main-1.3.0` 的实现，**我没有在真实环境验证它在这批新后端下的表现**。

## 5. 测试状态（如实）

| 范围 | 命令 | 结果 |
|---|---|---|
| 后端（需 Encore） | 在 `kernel/encore-api` 执行 `encore test ./devicemetadata ./internal/...` | 通过。整个 Encore 模块 `encore test ./... -count=1` 通过，仅 `cmd/notification-migrate` 和 `devicemetadata/migrations/scripts` 两个 `package main` 工具包在 `encore test` 下无法链接（前者在 `main-1.3.0` 上同样如此，后者用 `go test ./devicemetadata/migrations/scripts` 通过）。曾有 `TestDeviceCredentialsSchemaConstraints` 偶发失败（Go 时间与数据库 `NOW()` 的微秒差触发 `activated_at >= created_at` 检查），已把夹具时间提前一分钟修复；之后无缓存全量运行全部通过 |
| 前端 vitest | 在 `frontend`（集成分支）执行 `npx vitest run src/router src/__tests__/unit` | **168 个全部通过**。`thingsvis-auth` 原先的失败是测试数据缺 `authority`（源码要求它，`main-1.3.0` 上同样失败），已修 |
| 前端 node:test | `npx tsx --test src/service/api/<文件>.test.ts` | 通过 |
| 真实端到端 | — | **0/27** |

汇总（以 `E2E-CHECKLIST-2026-10-29.md` 为准）：代码 27/27，自动化 27/27，有局部端到端证据 10/27（旧文档继承、合成登录），整块签收 0/27。

**测试身份变更**：单元测试里的默认身份由 `platform_admin` 收紧为 `TENANT_ADMIN`（`authn.CurrentData`），所以 `l3-core/automation`、`l3-core/pluginmgr`、`l5-ops/verifier` 里依赖旧超级身份的测试已显式声明 `platform_admin`。新写测试时注意默认身份没有 `device:manage`/`plugin:manage`。

注意：`encore check` 在本机会在 notification 服务初始化处失败（它要求特定的 Linux 自托管配置），在 `main-1.3.0` 上也一样；用 `encore test` 和 `go vet ./...` 当门禁。本机想跑完整应用见 `LOCAL-E2E.md`。

## 6. 提交后一直“待处理”的操作（Community 缺能力）

设备启停、换模板、凭据轮换/撤销、解绑子设备。原因：Community 的更新接口没有启用字段，`is_enabled` 不参与 MQTT 鉴权，凭据没有按 generation 生效的账本；MQTT 接入强制层不在本仓库。要让它们真正生效，需要在 Community / MQTT 鉴权层补能力；具体要什么、做到什么算完成、联调怎么验收，见 `COMMUNITY-CAPABILITY-REQUEST.md`。改设备编号、删除设备、绑定子设备由 outbox 投递器真实下发（需开 `TP_RUNTIME_OUTBOX_DISPATCH=on`，**尚未与真实 Community 联调**）。

## 7. 有意没做的

| 项 | 原因 |
|---|---|
| `latest-reading-deletions`（删除最新值） | Community 的删除接口会连历史一起删，而契约要求只删最新值缓存，硬接会误删历史 |
| 属性导出 / 事件导出 | 属性只存最新值无历史；事件列表接口没有时间范围参数 |
| 超过 100 万行或 60 MiB 的导出 | 超限时任务失败并给出原因，不产生半截文件 |
| 实时 WebSocket | 仍走旧路径 |
| 取消长导出、多副本抢任务压测 | 未做 |
| `simulator-config` 的 `topics` | 恒为空数组：本仓库没有按用途的 topic 和样例报文来源，不编造 |

图表历史曲线是“原生优先、失败回退旧接口”，所以不会退化；在真实浏览器里用 Community 测试替身走过（数值读数的历史曲线、文本读数的原生历史表和导出，见 `LOCAL-E2E.md`），真实 Community 下仍未验证。取消长导出在 API 契约里没有定义，属于可选增强，不是缺口。

## 7.1 契约有、后端没有的端点（2026-10-29 逐条对照）

方法：把 `API-CONTRACT.md` 里全部 `/api/v2` 端点（104 条）和后端 `//encore:api` 路由（77 条）逐条比对，缺 33 条（其中“删除设备模板”已在本阶段补上，不在下表；表中“已做（本轮补）”的两条是对照后又补的，所以现在实际缺 30 条）。**前端页面没有调用其中任何一条**（只出现在自动生成的类型里），所以现有页面不会因此 404。

| 类别 | 端点 | 说明 |
|---|---|---|
| 有意不做 | `POST /devices/:id/activations` | 社区版不做产品/二维码激活（DM-DEV-007/022） |
| 有意不做 | `POST /devices/:id/latest-reading-deletions` | Community 删除会连历史一起删（见第 7 节） |
| 有意不做 | `POST /devices/:id/credentials/:id/reveals` | 凭据列表已直接显示明文，不需要二次揭示 |
| 有意不做 | `GET /products`、`/products/:id` | 社区版没有产品概念 |
| 改用别的路径 | `GET /reading-exports/:id/download` | 已由 `GET /metadata-downloads/:id` 承担 |
| 已做（本轮补） | 模板校验 `POST /device-access-templates/:id/validations`；模板引用 `GET /device-access-templates/:id/references` | `validations`：与保存同一套校验，但不保存；配置有问题时返回 `valid:false` 和带 JSON 指针的问题列表（HTTP 200），只有依赖不可用才返回错误；`sourceDigest` 是所校验内容的 SHA-256；对看不见的模板返回 404。`references`：分页列出仍绑定该模板的未删除设备（`kind:"device"`），看不见的模板返回 404；只有设备一类，产品/自动化/市场包等要等跨服务引用协议。两条都有数据库测试，**页面还没有调用** |
| 已做（本轮补） | 模板复制 `POST /device-access-templates/:id/copies` | 保留描述/品牌/标签/封面/整份配置，不复制设备；有数据库测试，并在页面里验证（模板列表“复制”） |
| 保留能力、非核心验收 | 模板导入 `POST /device-access-template-imports`；模板导出 `GET /device-access-templates/:id/export` | 契约写明“列为保留能力，不纳入本轮核心验收必做” |
| 保留能力、非核心验收 | 设备导入导出 `POST /device-imports`、`GET /device-imports/:id`、`POST /device-imports/:id/commits`、`GET /device-import-format`、`POST /device-exports`；批量 `POST /device-batches`、`/device-template-binding-batches`、`/gateway-membership-batches`；`GET /metadata-operations/:id/items` | 批量与导入导出，页面目前走旧路径或没有入口 |
| 保留能力、非核心验收 | `POST /device-runtime-state-queries`、`GET /device-observation-stream` | 批量在线状态查询、实时流；实时 WebSocket 仍走旧路径 |
| 保留能力、非核心验收 | `POST/GET/DELETE /device-access-templates/:id/enrollment-credentials…`、`POST /device-enrollments`、`GET /device-enrollment-operations/:id`、`GET /connection-providers/:id/instances/:id/discovered-devices` | 设备登记凭据与发现；依赖连接器能力 |
| 跨服务协议 | `/internal/metadata-references…`（登记、确认、释放、续期、查询共 5 条） | 其他服务向本服务声明“我引用了这个模板/设备”的内部协议；模板删除要检查产品、OTA、规则、市场包引用时需要它，目前未实现，所以删除只检查设备 |

是否排期实现这些保留能力，取决于业务优先级；它们都不阻塞三个菜单的现有页面。

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
| **LOCAL-E2E.md** | 本机真实 HTTP 端到端：40 项结果、没覆盖什么、复现步骤、跑出来并已修的两个真实问题 |
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
