# 交接提示语：完成「设备」菜单原生改造（2026-10-28）

> 这是一份可直接发给接手人/接手 Agent 的任务提示语。请先完整读一遍再动手。

## 0. 你的任务一句话

在**不改动设备模板、设备分组、自动化/告警**的前提下，把「设备」菜单剩下的 9 个详情标签接上 Encore 原生 v2 链路，并为已完成的 4 个标签补齐缺失的写入能力，做到每块「页面 → 真实 v2 接口 → 存储/权限 → 刷新读回」可复核。

## 1. 工作区与铁律（先做这一步，别跳过）

三个仓在同一工作树父目录下，均已切到同一分支：

| 仓 | 路径 | 当前状态 |
|---|---|---|
| backend（只放任务书与证据，不写业务代码） | `/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/backend` | 分支 `codex/metadata-menus-native-completion`，158 项未提交（docs 目录未跟踪） |
| frontend（thingspanel-vue） | `/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/frontend` | 同分支，105 项未提交，base commit `7d331d15329af0d636265a8a97b9534fff435a21` |
| kernel（`zjhong/iot-api-encore-kernel`，原生服务在 `encore-api/`） | `/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel` | 同分支，284 项未提交 |

**铁律**：

- **禁止** `git reset` / `git clean` / `git stash` / 整树合并 / 全量提交。这些都是别人未提交的实现，覆盖即毁。
- 动手前先核对指纹：`evidence/resume-20261027/frontend-source-manifest-template-abatch-v1.json`（105 文件）与 `_preserve-20261027/SNAPSHOT-INDEX.json`。若指纹不符，先停下来查清原因，不要继续改。
- 文档里说的「backend 479/480 文件」指的是 **kernel 仓的 `encore-api`**，不是 `thingspanel-backend-community` 仓，别改错仓。
- 不要碰前端这 18 个已冻结文件（模板三 Tab 的成果）：`src/service/api/template-*.ts`、`src/views/device/config-detail/modules/native-template-*.vue`、`src/views/device/config-detail/index.vue`。改动前必须先跑 `template-*.test.ts`（当前 55/55 通过），改完必须仍是 55/55。

## 2. 必读文档（按顺序）

1. `docs/initiatives/device-metadata-native-cutover/HANDOFF-2026-10-27.md` —— 总交接
2. `DESIGN-CORRECTION-2026-10-27.md` —— 设计纠偏：**模板不做草稿/版本/发布/切版**
3. `FUNCTION-STATUS-2026-10-27.md` —— 27 个功能块的必要性与当前证据
4. `EVIDENCE.md` 最后一节 —— 本轮（模板三 Tab）做到什么程度、什么没做
5. `TASKS.md` + `tasks.json` —— 188 条任务，设备相关是 `DM-DEV-*`、`DM-RT-*`、`DM-FE-0xx`
6. `API-CONTRACT.md`、`api-operations.json` —— 104 个 v2 操作声明

## 3. 范围：设备菜单 14 块（1 列表 + 13 标签）

现状是实测的，不是估计：

| 功能块 | 现状 | 起点文件 |
|---|---|---|
| 列表/创建/编辑 | 原生主链路已通；批量创建、导入导出、编号变更未做 | `src/views/device/manage/index.vue`、`src/service/api/device-native.ts` |
| 图表 | 部分：模型/图表引用原生，**数据仍走旧接口** | `src/views/device/details/modules/telemetry-chart.vue` |
| 遥测 | 旧接口 | `modules/telemetry/`（组件未改） |
| 连接 | 原生完成（凭据摘要/创建/一次性显示/断线恢复）；缺轮换、撤销、reveal、启停 | `modules/native-device-credentials.vue`、`native-device-credentials-flow.ts` |
| 子设备 | **占位**（`NativeDetailPending`） | `details/index.vue` 的 `device-analysis` 项 |
| 信息 | 原生完成 | `modules/native-device-info.vue`、`device-info-state.ts` |
| 属性 | 旧接口 | `modules/stats.vue` |
| 事件上报 | 旧接口 | `modules/event-report.vue` |
| 指令下发 | 旧接口 | `modules/command-delivery.vue` |
| 期望消息 | 旧接口 | `modules/expect-message.vue` |
| 自动化 | 原生（外部 owner，**不重复开发**） | `modules/automate.vue` |
| 告警 | 原生（外部 owner，**不重复开发**） | `modules/give-an-alarm.vue` |
| 诊断 | 旧接口 | `modules/device-diagnosis.vue` |
| 设置 | 旧接口 + Tab 基本占位 | `modules/settings.vue` |

判断「旧接口」的方法：文件里 `import ... from '@/service/api'`（axios 旧层，走 `/api/v1`）就是没接原生；`from '@/service/api/xxx-native'` 才是原生。

## 4. 端点现状（关键：先补后端，否则前端只能造假）

### 4.1 设备域**已实现**的 v2 端点（`kernel/encore-api/devicemetadata/`）

```
GET    /api/v2/devices
POST   /api/v2/devices
GET    /api/v2/devices/:deviceID
PATCH  /api/v2/devices/:deviceID
GET    /api/v2/devices/:deviceID/credentials
POST   /api/v2/devices/:deviceID/credentials
GET    /api/v2/devices/:deviceID/groups
GET    /api/v2/devices/:deviceID/template-binding
```

注意 Encore 路由用 `:deviceID`，OAS 里写的是 `{deviceId}`，做路径比对时别把「已实现」误判成「缺失」。

### 4.2 设备侧**缺失**端点（按任务 ID，必须先补再接线）

| 任务 | 缺失端点 | 支撑哪个 Tab |
|---|---|---|
| DM-DEV-005 | `DELETE /api/v2/devices/:deviceID` | 设备删除 |
| DM-DEV-006 | `GET/PUT /api/v2/devices/:deviceID/administrative-state` | 设置 Tab 启停 |
| DM-DEV-007 | `POST /api/v2/devices/:deviceID/activations` | 激活状态机 |
| DM-DEV-010 | `POST .../credentials/:credentialId/rotations`、`.../revocations` | 连接 Tab 轮换/撤销 |
| DM-DEV-011 | `POST .../credentials/:credentialId/reveals` | 连接 Tab 明文揭示（策略待产品确认） |
| DM-DEV-017 | `PUT /api/v2/devices/:deviceID/template-binding` | 设置 Tab 换模板 |
| DM-DEV-018 | `GET/PUT /api/v2/devices/:deviceID/gateway-membership` | 子设备 Tab |
| DM-DEV-019 | `GET /api/v2/devices/:deviceID/subdevices` | 子设备 Tab |
| DM-DEV-021 | `POST /api/v2/devices/:deviceID/number-changes` | 编号变更 |
| DM-RT-002/003 | `GET .../runtime-state`、`.../connection-info` | 图表/连接真实状态 |
| DM-RT-004 | `POST/DELETE .../debug-sessions`、`GET .../debug-logs` | 诊断 Tab |
| DM-RT-005 | `GET .../readings` | 遥测/属性当前值 |
| DM-RT-006/014 | `POST/GET .../command-executions`、`GET .../{executionId}`、`POST .../cancellations` | 指令下发、期望消息 |
| DM-RT-015 | `GET .../status-history`、`.../diagnostics`、`.../simulator-config` | 诊断/在线历史 |
| DM-RT-016/018 | `POST .../latest-reading-deletions`、`.../reading-exports` | 属性删除、历史导出 |
| DM-RT-019 | `.../simulator-profile`、`.../simulator-sessions`、`.../simulation-runs` | 模拟器 |

补端点时必须沿用既有约定，不要另造一套：

- `//encore:api auth raw method=… path=…`，响应信封 `{data, meta}`，Problem JSON 带 `type/title/status/detail/instance/code/requestId`
- 强 ETag + `If-Match`（不匹配 412）、写操作 `Idempotency-Key`（同 key 同 body 重放、异 body 409）
- 租户隔离用 `(tenant_id, id)` 复合键，权限用 `device:read` / `device:write` 这类既有 scope
- 迁移只加不改已有表，编号递增放 `devicemetadata/migrations/`

## 5. 前端硬性技术约束（违反即返工）

1. **不要解析后重编码 JSON 再回传**。设备 PATCH 与模板 PATCH 一样，未改动的值必须原样保留原 token，否则 `>2^53` 的整数（如 `9007199254740993`）和长小数（如 `1.000000000000000001`）会被舍入。已有现成工具：`src/service/api/template-configuration-raw.ts` 的 `replaceConfigurationPath`（按字节拼接，支持嵌套路径与标量）。
2. **未知结果只能重试原请求**。请求发出后结果不明（超时/网络断/5xx），必须冻结 `payload + ETag + Idempotency-Key` 原样重试；参考 `template-configuration-attempt.ts`，按 `(资源 id, scope)` 冻结，身份变化清空。
3. **412 不能覆盖用户输入**。拿到 412 先重读最新资源，保留用户 dirty 字段，让用户确认后再提交。
4. **不要造假入口**。端点没实现就不要放可点按钮，页面直接写明「该能力需后端 X 端点，尚未接入」。这条比"看起来完整"重要。
5. **不要改 `@/service/api` 旧层**。旧组件保留是刻意的（回滚用），新页面用 `*-native.ts` 客户端；删旧层属于另一个任务（DM-FE-014），不在你的范围。

## 6. 环境与验收方法

**本机 macOS 无法跑全应用**，这点已实测，别浪费时间：

- `encore run` 会因 `notification` 服务的隐私守卫（硬性要求 `GOOS=linux`，`/proc/self/environ`）初始化失败而致命退出，且 `encore run` 没有 `--services` 参数
- `encore build docker --services … --skip-config` 在 Encore CLI 1.58.6 上会挂起（仓库 README 已记录该循环问题）

**可行路径**：走仓库自带的 Linux 容器通道（`kernel/docker-compose.yml` 的 `encore` 服务，或 `kernel/deployments/notification/compose.yaml` 的 infra 配置形态），重建 harness。合成身份三要素：

```
TP_AUTH_MODE=legacy-jwt
TP_JWT_KEY=<本地自签 HS256 密钥，放 Git 外的 0600 文件>
TP_COMMUNITY_SESSION_BASE_URL=http://127.0.0.1:<stub 端口>
```

stub 需要 `/api/v1/user/detail` 按 `x-token` 精确命中 `records.json` 里的会话，且 token 必须是**带签名的 JWT**（不是 JSON 串）。前端 `localStg` 是 `JSON.stringify` 存储，浏览器里写 token 要 `JSON.stringify(token)`。

**证据落盘**：一律写进 `backend/docs/.../evidence/resume-20261027/<批次名>/`，**不要放 `/tmp`**（上一轮的 `/tmp/ev/` 被系统清理，导致已完成的协议配置 HTTP 证据不可复核，这是真实教训）。

## 7. 验收口径（不满足就不算完成）

每块都要有：

1. 页面实际操作（浏览器，含失败/冲突路径），不是只看代码
2. 真实 v2 请求与响应（状态码、ETag、Idempotency 重放、412/409/422 分支）
3. 存储侧核对（刷新后读回、DB 值、权限与租户隔离）
4. 冻结本批源码 manifest（`files[]` 带 sha256）+ tar.gz，并核对与上一版无意外漂移

**局部通过不自动升格为整块签收**。合成身份验收不等于真实 IDP；有端点缺失的 Tab 必须写明缺什么，不能算完成。

完成后更新：`EVIDENCE.md`（新增一节，写清做到哪、没做什么、为什么）、`FUNCTION-STATUS-2026-10-27.md`（对应行与顶部统计口径）。`tasks.json` 的 state 只有在真正变化时才改，且保持 JSON 结构可被 `validate-plan.py` 校验。

## 8. 建议执行顺序

1. **先补最小端点**（DM-DEV-005/006/017/018/019 + DM-RT-005/006）——没有它们，子设备、设置、指令、遥测四个 Tab 无法真接
2. **再做 4 个"有端点可接"的 Tab**：属性、事件上报、指令下发、诊断（读链路）
3. **然后做需要新端点的**：子设备、设置（启停/换模板）、遥测/图表（readings + runtime-state）
4. **最后补列表侧剩余**：批量创建、导入导出、编号变更
5. 每完成 1-2 块就冻结一次源码 + 写证据，不要攒到最后

## 9. 明确不要做的事

- 不要给设备模板加版本、草稿、发布、历史、切版（用户已明确否定）
- 不要重复开发/验收自动化与告警（外部 owner，`tp-automation-rewrite` / `tp-automation-frontend` 分支）
- 不要为了"看起来完成"放宽校验、伪造接口返回、把占位组件包装成已完成
- 不要 reset/clean/stash 三仓，不要整树合并
- 不要把历史 188 任务数或 580 用例数当成本轮完成率
- 不要引入真实 IDP、真实 MQTT 物理设备、生产数据；本地合成身份足够，但要在证据里写明边界

## 2026-10-29 续：回执核心、凭据明文、原始历史读数（已测，未联调）

- DM-RT-007：新增 `devicemetadata/native_runtime_receipt.go`（`recordRuntimeReceiptTx`），按回执把 outbox 与 metadata 操作推进到 succeeded/failed；仅 applied 完成操作，received/stale 不完成，可重试失败保持 pending，不可重试失败写 problem。真实数据库测试通过。**尚无投递器和受服务鉴权的回执入口**，Community 也没有按 generation 应用配置的能力，所以界面上的操作仍会停在待处理。缺号缓冲、跨语言 JCS 摘要黄金向量未做。
- 凭据：`GET /api/v2/devices/:id/credentials` 每条返回 `secret`（x509、已撤销为 null，no-store）；`revealDeviceCredential` 作废；手工修改=导入密钥创建+轮换。
- 读数：`listDeviceReadings` 支持 telemetry 的 raw 历史（identifier+from+to，[from,to)，≤24h）；聚合与属性历史仍返回 400。`internal/legacy.ListTelemetryHistory` 对不分页的时序后端本地切片。
- 验证：`encore test ./devicemetadata ./internal/legacy/...` 通过；前端 node:test 127 项（此前）通过，`vue-tsc` 对改动文件无报错；未做真实浏览器与真实 Community 联调。

## 2026-10-29 续2：模板脚本沙箱调试（已测，未联调）

- 新增 `POST /api/v2/processing-script-tests`（`devicemetadata/native_processing_script.go`，权限 `template:diagnose`，TENANT_ADMIN 已授予）和 `internal/luasandbox`：只开放 base/table/string/math 并去掉 `load/dofile/require` 等，`require("json")` 是唯一可用模块；1 秒超时，输出 256KiB，输入 256KiB；错误码 SCRIPT_SYNTAX_ERROR/RUNTIME_ERROR/TIMEOUT/MEMORY_LIMIT/BAD_RETURN/ENTRYPOINT_MISSING/OUTPUT_TOO_LARGE/INPUT_TOO_LARGE。
- 内存上限只是近似：gopher-lua 自带的 `SetMx` 超限会直接 `os.Exit(3)`，不能用，改为串行执行 + 每 2ms 检查进程堆增长超过 32MiB 就取消。堆是进程级的，高并发下可能误报，已串行化降低影响。
- 依赖：go.mod 新增 `github.com/yuin/gopher-lua v0.0.0-20220504180219-658193537a64`（本机模块缓存里唯一版本，Community 用 v1.1.1）；加入时用了 `GOPROXY=off GOSUMDB=off`，go.sum 条目来自本地缓存，**未经校验和数据库核对**。CI 联网时请确认或改用 v1.1.1。
- 前端：模板“数据处理”脚本编辑框新增“调试运行”（topic 输入、示例输入运行、结果展示）。
- 契约里该操作归 connector-runtime，目前先放在 devicemetadata；如以后拆出需迁移。
- 验证：`encore test ./devicemetadata ./internal/...` 通过，含沙箱的越权、超时、内存、输出过大用例；前端 vue-tsc 无新增错误。

## 2026-10-29 续3：outbox 投递器（已测，未联调）

- 新增 `devicemetadata/native_runtime_dispatch.go`：定时任务 `dispatch-device-runtime-outbox`（每分钟）调用私有端点 `DispatchDeviceRuntimeOutbox`，仅当 `TP_RUNTIME_OUTBOX_DISPATCH=on` 才工作。取 `pending`/`failed` 且操作仍 `pending` 的 outbox 行，交给 `runtimeApplier`，再用 `recordRuntimeReceiptTx` 记录结果。
- `communityRuntimeApplier` 只处理 Community 真有对应能力的三类：`device_renumber`→`PUT /api/v1/device {device_number}`；`device_deletion`→`DELETE /api/v1/device/{id}`；`gateway_binding`（仅绑定）→`PUT {parent_id, sub_device_addr}`。其余类型和解绑返回 Skip，不记录任何回执，操作保持待处理。
- 结果映射：Community 成功→applied；业务码非 200 或 4xx→不可重试失败（操作失败，带 problem）；超时/5xx/找不到 Community 身份→可重试失败（操作仍待处理，下一轮再试）。
- 已知限制：投递不加锁，多副本可能重复调用；改编号和删除对 Community 是幂等或可重复的，但删除被 Community 拒绝时，会再用 `DeviceGone` 确认设备确实已不存在才算已生效（避免“删除成功但回执没记上”被误判为失败），无法确认时保持原结果。
- 验证：`encore test ./devicemetadata ./internal/...` 通过（含用脚本化执行器的真实数据库测试，和对 Community 请求体的单测）；`encore check` 能编译并注册 240 个端点、迁移全部应用，最后因 notification 服务缺隐私配置退出，与本改动无关。未对真实 Community 联调。

## 2026-10-29 续4：设备模拟器（已测，未联调）

- 迁移 0037：`device_simulator_profiles`、`device_simulation_sessions`、`device_simulation_runs`（含租户不可变触发器）。不存 broker 地址、端口或凭据。
- 接口（`devicemetadata/native_device_simulator.go`，权限 `device:simulate`，TENANT_ADMIN 已授予）：`GET/PUT /simulator-profile`（ETag `"sim-profile-N"`，首次保存 `If-Match: "sim-profile-0"`，需 Idempotency-Key）、`POST /simulator-sessions`、`DELETE /simulator-sessions/:id`、`POST /simulation-runs`、`GET /simulation-runs/:id`。
- 行为：会话绑定保存时的配置版本，配置更新后旧会话不能再运行；会话 TTL 60–3600 秒；关闭幂等；运行按 messageCount 逐条调用 Community `simulation/send`，部分失败记 `failed` 并带 problem，同一 Idempotency-Key 重放不重复发送。
- 与契约的差异：运行同步且限 20 条（契约最多 1000）、不按 `intervalSeconds` 间隔发送；只支持 telemetry；未实现 `simulator-config`；未保存时 GET 为 404。
- 验证：`encore test ./devicemetadata ./internal/...` 通过（含配置/会话/运行/越权/跨租户/重放测试）。未对真实 Community 联调。前端没有调用模拟接口的组件，未做页面。
