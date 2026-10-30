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

## 4. 让功能真正进入发布（已在两个分支里做好）

只合并代码不等于功能进发布：发布的镜像原来只构建 `authn, thingmodel, automation, notification` 四个服务，前端镜像只把 `thing-models` 和通知路径转给 Encore。已做：

| 位置 | 改动 |
|---|---|
| `.github/workflows/encore-image-build.yml`、`deploy/community-test/deploy_encore.py` | 构建服务列表加入 `devicemetadata, fileassets, connectorcatalog` |
| `deploy/community-test/deploy_encore.py` | 迁移计划加入 `file_storage`、`connector_runtime` 两个库；缺库时在**停止现有服务之前**报“Database … is not provisioned”，线上服务不受影响 |
| `.github/workflows/deploy-encore.yml` | CI 测试范围加入 `devicemetadata`、`fileassets`、`connectorcatalog`、`internal` |
| 前端 `nginx.conf` | 新增一条路由，把 12 个 `/api/v2/…` 前缀（设备、模板、分组、文件、连接器目录等）转给 `encore-api`；否则这些请求落到 Community 后端返回 404 |
| 前端开发代理 | 与 nginx 共用同一份前缀清单（原来还漏了 `connection-providers`）；`native-api-routing.test.ts` 保证两份清单一致 |
| `deploy/community-test/ENCORE.md` | 新增“设备元数据”一节：要建的库、要给 `encore-api` 加的环境变量、密钥环生成方法、上线检查清单 |

**这些只能在服务器上做，我没法代做**：在 `runtime.encore.private.json` 里建好 `file_storage`、`connector_runtime`；把 `ENCORE.md` 里的环境变量写进 `compose.encore.private.json`（含 `TP_COMMUNITY_API_KEY` 和密钥环）。没做完时部署会在停服前就报错，不会影响线上。

## 5. 我自己判断的几件事（不再是待确认项）

| 事项 | 结论 |
|---|---|
| 设备元数据通过 Community 的 REST 接口取读数、发命令 | 这是当前的实现，用专用 API Key、没有默认账号。若以后要改成 Community 的内部专用接口，是新增接口的另一项工作，不阻塞本次发布 |
| 迁移 `0017` | 它是“物模型删除后名称可复用”的修复，属于物模型，发布自己没带；我们的代码和测试不依赖它，空缺无影响 |
| `go` 1.24 / `encore.dev` v1.57.13 | 必须：`fileassets` 用 `encore.dev/storage/objects`；流水线按 `go.mod` 取 Go 版本 |
| 镜像构建 | **我在本机没有跑镜像构建**（此前在 Mac 上构建会卡死）；服务列表是否构建成功要看 CI。后端全包测试和完整应用启动都在本机验证过 |

## 6. 仍然没做
整块签收 0/27：以上都只在 Community 测试替身下验证，真实 Community 联调、Linux 环境完整验收还没做。

## 7. 新旧接口覆盖核对（移交前把关，2026-10-30）

方法：对比 master 与本分支前端 API 模块的导出和使用，再从 `src/views/device/*/index.vue` 沿 import 图查可达性；后端以 e2e 31/31 和页面实走为证。**结论：用户要求保留的功能都已由新接口覆盖；下列是有意去掉或仍走旧路径的项，移交人需知道。**

| 项 | 状态 | 说明 |
|---|---|---|
| 设备/模板/分组的增删改查、复制、删除引用检查 | 已覆盖 | 原生 `/api/v2/*` |
| 凭据激活/轮换/撤销、子设备解绑 | 已覆盖 | 经 outbox 调 Community `update/voucher`、`sub-remove` |
| 设备启停、换模板、批量导入/换模板/绑网关、模板导入导出分享 | 有意不做 | 产品确认社区版不需要 |
| 模板“发布到资源中心” | 已移除 | 属模板分享，与上条同口径；此前是永久禁用按钮，已删，列表页不再出现 |
| 属性下发/下发记录/属性删除、遥测上报日志、删除最新值 | 有意去掉 | 原生契约没有对应接口；需要时另提契约 |
| 告警历史删除 | 已移到自动化引擎 | 不在设备元数据服务内 |
| 曲线历史、紧凑读数当前值、`details-child` / `device-analysis` / `join` | 仍走旧路径 | 可达但未迁移；按迁移期工具对待，别当作已原生化 |
| 9 个无引用旧文件（config-detail 五个模块、config-modal、publish-confirm-modal、add-devices-step2、AutomaticModeStep） | 已删除 | 前端提交 fac4fda7 |

验证：`npm run typecheck` 0 错误；`vitest src/router src/__tests__/unit` 40 个文件中仅 `notification-plugins-page` 偶发失败一次（单跑一次失败一次通过，该文件与 master 一致，与本功能无关）。后端本轮未改动，沿用此前全包通过和 e2e 31/31。发布镜像本地构建（`encore build docker ... --arch=amd64 --skip-config`）在本机试了 3 次（25 分钟上限一次、无上限一次跑满 69 分钟），始终没有任何输出和子编译进程，判定为本机挂起，已手动终止，**没有得到成功或失败的构建结论**。服务清单变更已由 `encore check`、`encore test` 和 e2e 覆盖；镜像能否构建出来，以流水线 `encore-image-build.yml` 第一次运行为准，接手人合并后请先看它。
