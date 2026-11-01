> **先读 `HANDOFF-ENTRY.md`（入口）。** 本文件是完整细节；其中个别“未提交”的旧表述已以入口为准。

# 设备 / 设备模板 / 设备分组：完成度汇报与合并到 1.3 的交接（2026-10-29）

> 这份文件同时是**汇报**和**交接**。写法原则：只写核对过的事实；凡是继承自旧文档、本次没有重新验证的内容，都单独标注「继承」；没做的、做了一半的、有风险的都写明。

## 0. 一句话结论

**设备和设备模板没有“全部完成”。** 27 个页面功能块里，代码都已写（27/27），自动化测试覆盖 27/27（D2、D5、D8 的页面测试在 2026-10-29 补齐，共 11 个用例，vitest 通过），但**没有一块**在“真实浏览器 + 真实后端 + 真实 Community 运行时”下完成整块签收（0/27）。其中有 10 块有「合成登录下的局部流程」证据（来自旧文档，本次未重跑）。

要合并进 1.3 的人必须先知道的 5 件事：

1. **改动已提交并推送**到三个仓库的 `codex/metadata-menus-native-completion` 分支（kernel 7 个提交，frontend 2 个功能提交 + 1 个合并提交（集成分支）；见 `HANDOFF-ENTRY.md` §2）。kernel 可快进合并；frontend 原分支落后 `main-1.3.0` 9 个提交，已在集成分支 `codex/metadata-menus-native-completion-on-1.3` 解决 16 个冲突。
2. **工作区里混着不属于本任务的东西**（自动化 owner、Provider 目录、日志文件、本机路径的验收配置），见 §9。
3. **有些操作永远停在“待处理”**：启停、换模板、凭据轮换/撤销、解绑子设备——Community 没有对应能力，不是代码没写，见 §6。
4. **新增的 outbox 投递器默认关闭**，且 Community 客户端在未配置环境变量时默认指向 `https://demo.thingspanel.cn`，开启前必须先配置，见 §8。
5. **旧的模板草稿/发布/归档接口仍在代码里**，与“设备模板不做草稿、版本、发布”的产品决定矛盾，需要决定是否在合并时删除，见 §10。

---

## 0.5 每个 tab 现在到底调的是 Encore 新接口还是旧接口（按代码实际 import 核对）

「新接口」= 前端调 `/api/v2/...`（Encore）。「仍有旧调用」= 页面里还有直接打 Community 旧接口的地方。

**设备详情 13 个 tab**

| tab | 接口 | 仍有的旧调用 / 备注 |
|---|---|---|
| 图表 | 新接口（**本次刚切**） | 实时推送仍走旧 WebSocket；图表里用字段“名称”回退匹配的旧逻辑丢了（原生读数只有标识符） |
| 遥测 | 新接口 | 实时推送走旧 WebSocket；历史表的「导出」按钮仍走 Community 旧接口 |
| 连接 | 新接口 | — |
| 子设备 | 新接口 | — |
| 附加信息 | 新接口 | — |
| 属性 | 新接口 | — |
| 事件上报 | 新接口 | — |
| 指令下发 | 新接口 | — |
| 期望消息 | 新接口 | — |
| 自动化 | 新接口（外部 owner） | — |
| 告警 | 新接口（外部 owner） | — |
| 诊断 | 新接口 | 统计数字 Community 提供不了，显示「--」 |
| 设置 | 新接口 | — |

**设备模板详情 8 个 tab**：关联设备、物模型、协议配置、数据处理、自动化、告警、扩展信息、设置——**全部是新接口**。

**设备分组**：列表、详情、成员、编辑——全部新接口。

**设备列表页**：列表/统计/创建/编辑是新接口；「编号查重」**本次已切到新接口** `POST /api/v2/device-number-validations`；还剩 2 处旧调用——服务接入的「协议字典」（属于连接器运行时负责人）和「激活」（社区版明确不做激活）。

> 另外，仓库里还有几个**没被任何 tab 使用的旧页面文件**（如 `details/modules/settings.vue`、`message.vue`、`config-detail/modules/associated-devices.vue`、`attribute-info.vue` 等）里仍有旧调用，但页面不会加载它们。是否清理待定。

## 1. 仓库、分支、基线

三个仓库都在同一个工作区 `~/.codex/worktrees/device-metadata-native-cutover-1.3/`，分支都是 `codex/metadata-menus-native-completion`。

| 仓库 | HEAD | 相对 `origin/main-1.3.0` | 未提交改动 | 说明 |
|---|---|---|---|---|
| `kernel` | `fafe0f6f` | **落后 0 提交**（HEAD 与 main-1.3.0 完全一致） | 修改 17 + 未跟踪 180 | Encore 后端，主要工作所在。可直接在 main-1.3.0 上提交，无基线冲突 |
| `frontend` | `7d331d15` | **落后 9 提交** | 修改 34 + 未跟踪 91（`git status` 共 125 项） | 合并前要先 rebase/merge main-1.3.0，路由生成文件容易冲突 |
| `backend`（Community） | `d638af3e` | **落后 70 提交** | **没有任何代码改动**，仅 `docs/initiatives/` 整个目录未跟踪 | 本任务没有改 Community 代码；这里只放文档 |

> 注意：这个工作区的 `kernel` 根目录是 `kernel/`，Go 模块在 `kernel/encore-api/`。命令请在对应模块目录执行（仓库根没有统一 `go.mod`）。

---

## 2. 完成度总表（27 个页面功能块）

**读法**：
- **代码** = 接口和页面已写；**自动化** = 有通过的自动化测试覆盖其核心逻辑；**E2E** = 真实浏览器+真实后端走通。
- E2E 栏的 ✔ **全部是继承**自 `FUNCTION-STATUS-2026-10-27.md` / `EVIDENCE.md` 的记录，**合成登录**下的局部流程，本次没有重跑，也不等于整块签收。
- 「整块签收」要求页面、接口、数据库、权限、关键操作在真实链路上全部走通：**27 块无一达成**。

### 2.1 设备（14 块）

| # | 功能块 | 代码 | 自动化 | E2E(继承) | 现状（如实） | 缺口 / 阻塞 |
|---|---|---|---|---|---|---|
| D1 | 列表/创建/编辑 | ✔ | ✔ | ✔局部 | 原生分页/搜索/模板筛选/组树、登记、改名/标签已有 | 凭据、完整接入/运行筛选、真实登录未验 |
| D2 | 图表 | ✔ | ✔ | ✘ | 原生模型/图表引用可读 | 图表的最新值已走新接口；页面测试：`src/__tests__/unit/device-chart-page.test.ts`（5 例）；聚合读数后端已做（§7.3），图表页面尚未使用 |
| D3 | 遥测 | ✔ | ✔ | ✘ | 当前值原生；**本次新增**原始历史（≤24h）；**本次新增**模拟器后端 + 前端面板 | 实时 WebSocket 仍走旧路径；删除最新值、聚合、历史导出未做；模拟器未在真实浏览器验证 |
| D4 | 连接 | ✔ | ✔ | ✘ | 接入信息、凭据创建/列表（**本次**列表直接返回明文）、轮换/撤销受理 | 轮换/撤销在 Community 没有原语，永远待处理 |
| D5 | 子设备 | ✔ | ✔ | ✘ | 绑定/解绑页面和接口已有；**本次**绑定可由投递器真实下发 | 页面测试：`src/__tests__/unit/device-subdevices-page.test.ts`（4 例）；解绑无 Community 原语，待处理 |
| D6 | 信息 | ✔ | ✔ | ✔局部 | 位置/地图入口、扩展值、412 不覆盖 | 真实身份属整体后续 |
| D7 | 属性 | ✔ | ✔ | ✘ | 属性当前值只读 | 下发/日志/删除未原生化；Community 属性无历史（所以历史请求返回 400 是正确的） |
| D8 | 事件上报 | ✔ | ✔ | ✘ | 事件列表分页读取 | 页面测试：`src/__tests__/unit/device-event-report-page.test.ts`（2 例，已跑通） |
| D9 | 指令下发 | ✔ | ✔ | ✘ | 提交/回执/历史/超时 unknown；“上线后发送” | Community 配置 `ingress_v1` 时“上线后发送”被拒；未联调 |
| D10 | 期望消息 | ✔ | ✔ | ✘ | 列表按状态过滤；仅“等待上线发送”可取消 | 同 D9 |
| D11 | 自动化 | ✔ | ✔ | ✔局部 | 接外部 owner（`automationowner`，**未跟踪**） | 整块真实权限/升级/运行联测未签收；owner 属外部 |
| D12 | 告警 | ✔ | ✔ | ✔局部 | 分页/筛选/确认；合成遥测触发 | 真实设备/MQTT/通知未验 |
| D13 | 诊断 | ✔ | ✔ | ✘ | 阶段标签、调试开关、最近失败 | Community 计数无统计窗口、失败记录无 id/code，**统计不能原生化**，页面显示「--」 |
| D14 | 设置 | ✔ | ✔ | ✘ | 启停、换模板、改编号、申请删除，均返回“已受理” | 改编号/删除可由投递器真实生效（需开关）；**启停/换模板无原语，待处理** |

### 2.2 设备模板（9 块）

| # | 功能块 | 代码 | 自动化 | E2E(继承) | 现状（如实） | 缺口 / 阻塞 |
|---|---|---|---|---|---|---|
| T1 | 列表/创建/编辑 | ✔ | ✔ | ✘ | 原生分页搜索、封面；MQTT 模板创建/编辑 | 其它协议、删除未验 |
| T2 | 关联设备 | ✔ | ✔ | ✘ | 关联设备分页读 | 创建/绑定/解绑完整页面流程未收尾 |
| T3 | 物模型 | ✔ | ✔ | ✘ | 详情显示所选发布版本；复用 1.3 物模型选择 | 纠偏后完整联验未收尾 |
| T4 | 协议配置 | ✔ | ✔ | ✘ | Topic 映射增删改启停；接入信息只读 | 真实 HTTP/UI 因验收环境丢失未签收 |
| T5 | 数据处理 | ✔ | ✔ | ✘ | 六阶段 Lua 脚本增删改启停；**本次新增**沙箱调试运行 | 真实 HTTP/UI 未验；gopher-lua 版本待核对（§8） |
| T6 | 自动化 | ✔ | ✔ | ✔局部 | 同 D11 | 同 D11 |
| T7 | 告警 | ✔ | ✔ | ✔局部 | 新建策略并关联、设备继承 | 同 D12 |
| T8 | 扩展信息 | ✔ | ✔ | ✔局部 | 列表/新增/编辑/启停/删除、已有值 409 | 真实 IDP/升级未签收 |
| T9 | 设置 | ✔ | ✔ | ✘ | 封面上传、自动注册/在线判定、启停 | 真实运行验收未签收 |

### 2.3 设备分组（4 块）

| # | 功能块 | 代码 | 自动化 | E2E(继承) | 缺口 |
|---|---|---|---|---|---|
| G1 | 列表/组树 | ✔ | ✔ | ✔局部 | 真实登录/升级 |
| G2 | 子分组 | ✔ | ✔ | ✔局部 | 真实登录/升级 |
| G3 | 设备 | ✔ | ✔ | ✔局部 | 旧组引用迁移未验 |
| G4 | 设置 | ✔ | ✔ | ✔局部 | 真实登录/权限 |

### 2.4 汇总

| 栏 | 设备 14 | 模板 9 | 分组 4 | 合计 /27 |
|---|---|---|---|---|
| 代码已写 | 14 | 9 | 4 | **27** |
| 自动化通过 | 11 | 9 | 4 | **24**（缺：D2、D5、D8） |
| E2E 局部证据（继承） | 3 | 3 | 4 | **10** |
| **整块签收** | 0 | 0 | 0 | **0** |

> 旧文档 `FUNCTION-STATUS-2026-10-27.md` 里「6 个已验 / 9 个部分 / 6 个待接」是 10-27 的快照，之后又有改动，**以本表为准**。完整可勾选版本见 `E2E-CHECKLIST-2026-10-29.md`。

### 2.5 明确不做（产品决定，不是漏做）

| 项 | 依据 |
|---|---|
| 模板草稿、模板版本表、发布/历史、切版预览/回滚 | 用户 2026-10-27 否定；物模型版本保留 |
| 企业版扫码/产品管理激活、按编号领取资格 | 社区版范围排除（DM-DEV-007/022） |
| `revealDeviceCredential` 单独揭示接口 | 用户 2026-10-29：凭据明文直接显示，已作废 |
| 手工修改凭据（独立接口） | 契约无此接口；“导入指定密钥创建新凭据 → 轮换替换”已满足 |

---

## 3. 本次会话（2026-10-29）我做了什么

这一节只列**本会话**做的，方便评审时聚焦。此前会话遗留的大量未提交改动见 §9。

| # | 事项 | 做到什么程度 | 主要文件 |
|---|---|---|---|
| 1 | DM-RT-007 回执入库逻辑 | **完成并有真实数据库测试**：只有 `applied` 回执才把操作变成功；`received`/`stale` 不完成；可重试失败保持待处理；不可重试失败写 problem；摘要/generation/租户不符被拒；重放幂等 | `devicemetadata/native_runtime_receipt.go` + `_test.go` |
| 2 | outbox 投递器 | **完成，默认关闭，未联调**：只投递改编号、删除设备、绑定子设备；成功→已生效，Community 明确拒绝→失败，超时/5xx→保持待处理重试；删除被拒时用 `DeviceGone` 确认“已不存在”才算成功 | `devicemetadata/native_runtime_dispatch.go` + `_test.go`；`internal/communityclient/devices.go` |
| 3 | 凭据明文显示 | **完成**：`GET /api/v2/devices/{id}/credentials` 每条带 `secret`（x509、已撤销为 null），响应 `no-store`；OpenAPI 新增 `CredentialDetail`；前端连接页显示 | `native_credential_read.go`；`openapi.yaml`；`native-device-credentials.vue` |
| 4 | 原始历史读数 | **完成**：`GET .../readings?kind=telemetry&identifier&from&to`，区间 [from,to)，单次 ≤24h；前端遥测历史表改用；聚合/属性历史仍 400 | `native_runtime_read.go`；`internal/communityclient/runtime.go`；`history-data.vue` |
| 5 | 模板脚本沙箱调试 | **完成**：`POST /api/v2/processing-script-tests`，隔离 Lua，1 秒/256KiB；前端脚本编辑框加“调试运行” | `native_processing_script.go`；`internal/luasandbox/*`；`native-template-data-processing.vue` |
| 6 | 设备模拟器 | **后端完成；前端面板已写，未在真实浏览器验证**：6 个接口 + 迁移 0037；运行同步且限 20 条 | `native_device_simulator.go`；`migrations/0037_*` |
| 7 | 单一 E2E checklist | 完成 | `E2E-CHECKLIST-2026-10-29.md` |
| 8 | 权限 | 新增 `template:diagnose`、`device:simulate`、`device:data:export`、`device:diagnose`（仅 TENANT_ADMIN） | `internal/authz/engine.go` |
| 9 | 文档 | 更新契约（reveal 作废）、测试清单、状态表、交接记录 | 见 §12 |

**本会话没有改的**：Community（backend）代码；`l1-access/authn`、`thingmodel`、`cli/tpctl-go` 的既有未提交改动（它们是此前会话遗留，我没有评审过）。

---

## 4. 接口清单（`/api/v2`，共 58 条）

「新」= 本会话新增或扩展。其余为此前会话已写、当前**未提交**的代码。

| 域 | 方法 路径 | 备注 |
|---|---|---|
| 模板 | GET/POST `/device-access-templates`；GET/PATCH `/device-access-templates/{id}` | 无 DELETE（模板删除未做） |
| 分组 | GET `/device-group-tree`；GET/POST `/device-groups`；GET/PATCH/DELETE `/device-groups/{id}`；POST `/member-additions`、`/member-removals`、`/moves`；GET `/members` | |
| 统计 | GET `/device-statistics` | |
| 设备 | GET/POST `/devices`；GET/PATCH/DELETE `/devices/{id}` | |
| 设备-启停 | GET/PUT `/devices/{id}/administrative-state` | 受理后待处理，无 Community 原语 |
| 设备-模板绑定 | GET/PUT `/devices/{id}/template-binding` | 待处理 |
| 设备-网关 | GET/PUT `/devices/{id}/gateway-membership`；GET `/subdevices` | 绑定可投递；解绑待处理 |
| 设备-编号 | POST `/devices/{id}/number-changes` | 可投递 |
| 设备-分组 | GET `/devices/{id}/groups` | |
| 凭据 | GET（**新：含 secret**）/POST `/credentials`；POST `/credentials/:id/rotations`、`/revocations` | 轮换/撤销待处理 |
| 连接 | GET `/connection-info` | |
| 运行态 | GET `/runtime-state`；GET `/status-history`；GET `/diagnostics` | |
| 读数 | GET `/readings`（**新：raw 历史**） | 聚合/属性历史 400 |
| 命令 | GET/POST `/command-executions`；GET `/:executionID`；POST `/:executionID/cancellations` | |
| 调试 | POST `/debug-sessions`；DELETE `/debug-sessions/:id`；GET `/debug-logs` | |
| 模拟器（**新**） | GET/PUT `/simulator-profile`；POST `/simulator-sessions`；DELETE `/simulator-sessions/:id`；POST `/simulation-runs`；GET `/simulation-runs/:id`；GET `/simulator-config`（**续**） | `simulator-config` 只返回连接信息和模型绑定，`topics` 恒为空数组（本仓库没有存按用途的 topic 和样例报文，不编造）；需 `device:diagnose` |
| 操作 | GET `/metadata-operations/:operationID` | |
| 编号查重（**新**） | POST `/device-number-validations` | 把已软删除设备的编号也算占用 |
| 历史导出（**新**） | POST `/devices/{id}/reading-exports`；GET `/metadata-downloads/:downloadID` | 见 §7.6；GET `/reading-exports/:operationID`（**续**）：仅发起人且仍有导出权限可查，其余一律 404 |
| 脚本调试（**新**） | POST `/processing-script-tests` | |

另有 **60 条旧版 `/api/...` 路由**仍在 `devicemetadata`（`/api/devices`、`/api/device-templates` 及其 `publish`/`archive`/`derive-draft`、`/api/migrate/*` 等），见 §10。

**契约**：`openapi.yaml` 是权威来源；本会话只改了 `CredentialDetail`。前端类型 `src/service/api/generated/device-metadata/types.ts` 我**手工**补了 `CredentialDetail`，**不是**重新生成的——合并后应跑生成脚本核对。

---

## 5. 数据库迁移（`encore-api/devicemetadata/migrations/`）

**22 个迁移（0018–0039）**（`main-1.3.0` 上的最后一个是 0017_native_model_deletion），必须整段一起合并，缺一个应用就起不来。0018–0027 是更早会话的设备分组、幂等、审计、租户不可变、设备身份、操作、接入绑定、网关成员表，下表只展开 0028–0037。迁移目录以 `devicemetadata/db.go` 声明为准，不是仓库根的 `migrations/`。

| 迁移 | 内容 | 本会话 |
|---|---|---|
| 0018–0027 | 设备分组、元数据幂等、审计事件、租户不可变触发器、租户复合外键、模板修订（0023，已被 0028 取代）、设备身份、元数据操作、接入绑定（0026，部分被 0028 取代）、网关成员 | 否 |
| 0028 | 模板当前配置（取代 0023/0026 的部分设计） | 否 |
| 0029 | 模板继承物模型 | 否 |
| 0030 | 设备凭据（AES-256-GCM 加密存储） | 否 |
| 0031 | 秘密回执 | 否 |
| 0032 | 租户不可变触发器回填 | 否 |
| 0033 | `device_runtime_outbox` | 否 |
| 0034 | `device_command_executions` | 否 |
| 0035 | outbox 增加 `credential_change` | 否 |
| 0036 | outbox 增加 `device_renumber` | 否 |
| **0038** | **`metadata_downloads`**（导出文件存储，含租户不可变触发器） | **是** |
| **0039** | **`metadata_export_chunks`**（长导出的分片暂存，随任务完成/失败清理） | **是** |
| **0037** | **`device_simulator_profiles` / `device_simulation_sessions` / `device_simulation_runs`**（含租户不可变触发器） | **是** |

验证：`encore test` 会按序应用全部迁移并通过。`encore check` 也能应用迁移并编译、注册 240 个端点，但**最后因 `notification` 服务缺少隐私配置而退出**，与本任务无关（环境配置缺失）。

> 约定：每张带 `tenant_id` 的表必须挂 `trg_tenant_id_immutable`，否则 `TestTenantIDImmutableTriggerCoversPackageTablesAndRejectsReassignment` 失败（本会话踩到过一次，已修）。

---

## 6. 为什么有些操作永远“待处理”（不是 bug，是缺能力）

| 操作 | outbox 类型 | 能否真实生效 | 原因（已对照 Community 源码核对） |
|---|---|---|---|
| 改设备编号 | `device_renumber` | ✔ 可（需开关） | `PUT /api/v1/device {device_number}`，Community 校验唯一 |
| 删除设备 | `device_deletion` | ✔ 可（需开关） | `DELETE /api/v1/device/{id}` |
| 绑定子设备 | `gateway_binding`（有 gatewayId） | ✔ 可（需开关） | `PUT {parent_id, sub_device_addr}` |
| 解绑子设备 | `gateway_binding`（gatewayId 为空） | ✘ | 更新请求里空值=“不修改”，无法表达解绑 |
| **启用/停用** | `administrative_state` | ✘ | Community 更新请求**没有启用字段**；`is_enabled` **不参与 MQTT 接入鉴权**（`mqtt/`、`internal/` 下无检查） |
| 换模板 | `template_binding` | ✘ | 原生模板与 Community 的设备配置不是同一套，无映射 |
| 凭据轮换/撤销 | `credential_change` | ✘ | 凭据没有按 generation 生效的账本；MQTT 接入强制层**不在本仓库** |

这些行投递器返回 `Skip`，**不记任何回执**，操作在界面上保持“待处理”。这是刻意的：避免把没生效的事报成成功。

其它被外部能力卡住的项：

| 项 | 卡点 |
|---|---|
| **本机无法起完整应用做真实端到端** | `notification` 服务启动时读 `/proc/self/environ`，只能在 Linux 上运行；这台 Mac 上 `encore run` 必然在它这里失败。真实端到端需在 Linux/容器里起 Encore，并给 `notification` 配齐密钥环境变量 |
| 聚合读数的性能上限 | Community 自带聚合不返回每桶样本数，所以改为取原始样本后在 Encore 里自己聚合（§7.3）：精确，但一次请求范围内的原始样本超过 5 万条就拒绝，不做近似 |
| 历史导出的属性/事件 | 只做了遥测（§7.6）；属性无历史、事件接口无时间过滤，见 §7.6 |
| 诊断统计 | Community 计数无统计窗口 |
| 模拟器的 interval 与 1000 条 | 需要异步运行器（当前同步） |
| 真实 MQTT / 物理设备 | 本表不覆盖 |

---

## 7. 新增行为说明（给评审和测试的人）

### 7.1 outbox 投递器
- 定时任务 `dispatch-device-runtime-outbox` 每分钟调用私有端点 `DispatchDeviceRuntimeOutbox`。
- **只有 `TP_RUNTIME_OUTBOX_DISPATCH=on` 才工作**；默认什么都不做，操作保持待处理。
- 取 `state IN ('pending','failed')` 且对应操作仍 `pending` 的行，最多 50 条，按创建时间。
- 结果：成功→applied（操作成功）；Community 业务码非 200 或 4xx→不可重试失败（操作失败）；超时/5xx/找不到 Community 身份→可重试失败（操作仍待处理，下一轮再试）。
- Community 设备 ID 取自 `device_access_bindings.runtime_namespace/runtime_external_id`（对已软删除的设备也可读）；否则须设 `TP_RUNTIME_DEVICE_ID_MODE=identity`。
- **已知限制**：没有分布式锁，多副本可能重复调用（改编号、绑定幂等；删除靠 `DeviceGone` 兜底）；摘要用“键排序 JSON + SHA-256”，**不是**严格 RFC 8785，跨语言黄金向量测试没做；契约要求的“缺号缓冲”没做（各类写操作的 generation 不是同一连续序列）。
- **回执入口未做**：目前投递器靠 Community 同步响应判定，没有 Community 主动回报的入口，也没有服务间鉴权方案。

### 7.2 凭据明文
- 未撤销的加密凭据在列表里返回 `secret`；x509 和已撤销为 `null`；响应 `Cache-Control: no-store`。
- 权限仍是 `device:credentials:read`；**有该权限的人能看到该设备所有未撤销凭据的明文**（这是产品决定）。
- 需要环境变量 `METADATA_CREDENTIAL_KEYRING_B64` / `METADATA_CREDENTIAL_ACTIVE_KEY_REF`，缺失时列表返回 503（凭据保护不可用）。
- 前端连接页仍保留旧提示文案“请先查看或清除这次仅显示一次的凭据内容”，现已不准确，未改。

### 7.3 原始历史读数
- 必须同时带 `identifier`、`from`、`to`（RFC 3339），`from < to`，跨度 ≤ 24 小时，`kind=telemetry`。
- Community 区间是闭区间，所以请求时用 `to - 1ms`。Community 时序库后端会忽略分页，客户端本地切片并以全长为 total。
- **聚合读数（新）**：`aggregation=min|max|avg|sum|diff|last` 加 `window=30s|1m|2m|5m|10m|30m|1h|3h|6h|1d|7d|30d`，必须同时带 `identifier`、`from`、`to`，跨度 ≤366 天；`raw` 不得带 `window`，聚合必须带 `window`。
  - 做法：从 Community 取区间内全部原始样本（每页 1000），在 Encore 里按**固定 UTC 桶**聚合（对齐 Unix 纪元，`30d` 就是 30 天而不是日历月，`7d` 也不是日历周），空桶不出现，结果按桶起点倒序，分页在聚合之后。`sampleCount` 是真实参与数，`observedAt = windowStart`，`windowEnd` 为桶的开区间右端，`snapshotId`/`cacheVersion` 为 null，`sampleId` 为稳定的桶标识。`diff = max - min`；`last` 取桶内时间最晚的一条，支持文本。数值聚合遇到非数字样本返回 400。
  - 限制：区间内原始样本超过 5 万条直接拒绝（400），不做近似；属性、事件的历史与聚合仍返回 400。
  - **为什么图表的历史曲线没有切到它**：图表组件（`ThingsVisWidget`）的历史曲线走旧的 `/telemetry/datas/statistic`，范围可到 1 年、窗口自动选取（如 30 天用 3 小时窗）。新接口为保证 `sampleCount` 精确，原始样本超过 5 万条就拒绝，上报频繁的设备（如每 30 秒一条，30 天约 8.6 万条）的长范围曲线会变成空图。所以不是可以直接替换的等价物，需要等 Community/时序库把聚合下推并返回样本数后再切换。
- 前端：历史表默认最近 24 小时；选超过 24 小时提示且不发请求；**导出已走原生接口**（见 §7.6）。

### 7.4 脚本沙箱
- 只开放 base/table/string/math；删除了 `load`、`dofile`、`require`（改为只认 `json`）、`print`、`collectgarbage`、`getfenv` 等；没有 `os`、`io`、`package`、`debug`。
- 限制：1 秒、输入/输出各 256 KiB。错误码：`SCRIPT_SYNTAX_ERROR`、`SCRIPT_RUNTIME_ERROR`、`SCRIPT_TIMEOUT`、`SCRIPT_MEMORY_LIMIT`、`SCRIPT_BAD_RETURN`、`SCRIPT_ENTRYPOINT_MISSING`、`SCRIPT_OUTPUT_TOO_LARGE`、`SCRIPT_INPUT_TOO_LARGE`。
- **内存上限只是近似**：gopher-lua 自带的 `SetMx` 超限会直接 `os.Exit(3)` 杀整个服务，所以没用；改为串行执行 + 每 2ms 检查**进程**堆增长超过 32 MiB 就取消。堆是进程级的，高并发下可能误报。
- 契约把该接口归 `connector-runtime`，目前放在 `devicemetadata`，将来拆出需迁移。

### 7.5 设备模拟器
- 配置带 ETag `"sim-profile-N"`；**首次保存用 `If-Match: "sim-profile-0"`**；需要 `Idempotency-Key`。
- 会话绑定保存时的配置版本；配置更新后旧会话不能再运行；TTL 60–3600 秒；关闭幂等。
- 运行**同步**、**单次 ≤20 条**（契约允许 1000）、**不按 `intervalSeconds` 间隔发送**、**只支持 telemetry**；发送委托 Community `simulation/send`，不传 server/port/topic。
- 未保存配置时 GET 返回 404；`simulator-config` 已做（`topics` 为空，见 §4）。
- 前端：遥测 tab 顶部新增「模拟上报」面板（`telemetry/native-device-simulator.vue`，客户端 `device-simulator-native*.ts`，4 项测试）：填 JSON、选条数（≤20）、点发送，依次保存配置→创建会话→发送→关闭会话。**未在真实浏览器里点过。**

### 7.6 历史导出（DM-RT-018，第一版）
- `POST /api/v2/devices/{id}/reading-exports`（需 `Idempotency-Key`，权限 `device:data:export`，TENANT_ADMIN 已授予）：在请求内同步生成文件，返回 202 和一个**已经是成功状态**的操作，操作 `result` 里带 `downloadId` 和 `checksum`；文件存进新表 `metadata_downloads`（BYTEA，24 小时过期）。
- 过期导出文件由每小时的定时任务 `purge-expired-metadata-downloads` 删除（操作记录保留作审计）；有真实数据库测试。
- `GET /api/v2/metadata-downloads/:downloadID`：只有**创建者本人**、同租户、且仍有导出权限才能下载，其余一律 404；过期返回 410；响应带 `ETag: "sha256-…"` 和 `Content-Disposition`。
- **比契约更严格的限制**（超出一律拒绝，不产生半截文件）：只支持 `telemetry`、聚合 ≤366 天、标识符 ≤10 个；原始样本 ≤24 小时为同步（读取 ≤50,000 条）；**原始样本 24 小时到 31 天为长导出（异步，见下条）**。格式 csv / jsonl。
- 区间 [from,to)，用 `to-1ms` 换算 Community 的闭区间；排序为标识符升序、时间倒序。
- 前端：遥测历史表的「导出」按钮改走这个流程（提交导出 → 带令牌下载）；不再走 Community 的 Excel 导出。
- **聚合导出（新）**：`aggregation` 非 `raw` 且 `window` 为支持的窗口时，单次范围可到 366 天；文件列为 `identifier,windowStart,windowEnd,aggregation,value,sampleCount`（jsonl 同字段）。读取的原始样本仍受 5 万行上限，超过则拒绝。
- **没做且有原因**：属性导出（Community 属性只存最新值，没有历史可导）；事件导出（Community 事件列表 `/api/v1/event/datas` 没有时间范围参数，只能翻遍全部事件再本地过滤，数量不可控，需要 Community 先加时间过滤）。其余没做（均是设计取舍，不是遗漏）：图表历史曲线改为**原生优先、失败回退旧接口**（`frontend/src/utils/thingsvis/native-history.ts`，5 个用例）：窗口在原生支持范围内（图表的 `1mo` 不在内）且读取成功时用 Encore 聚合；窗口不支持、结果超过分页上限或请求失败（含繁忙设备触发 5 万样本上限）时自动回退旧聚合接口，所以不会退化。没有数据的窗口返回空，不回退。未在真实浏览器验证；彻底去掉旧路径需先让 Community 聚合返回每桶样本数并把聚合下推。超过 100 万行或 60 MiB 的导出（上限由代码和数据库约束保证，超出时任务失败并给出原因，不产生半截文件）。
- **长导出（新，`native_reading_export_async.go`）**：原始遥测跨度 24 小时到 31 天时，`POST /devices/{id}/reading-exports` 返回 202 的 `pending` 操作；每分钟的定时任务 `process-long-reading-exports` 按 1 小时一片、从新到旧读取，每片写入 `metadata_export_chunks` 并把位置记入操作的 checkpoint；进程重启或租约（2 分钟）过期后从 checkpoint 继续；读取失败退避 60 秒，累计 10 次失败则任务失败；全部完成后拼成一个 `metadata_downloads` 文件并删除分片。上限：100 万行、60 MiB。进度用 `GET /api/v2/reading-exports/{operationId}` 查询（`progress.completed/total` 为已完成片数）。前端导出按钮旁有「导出区间（最多 31 天）」，提交后每 2 秒轮询直到完成再下载。有真实数据库测试（拆片、中途失败续传无重复无遗漏、表头只出现一次、超限失败）。**未做**：取消长导出、多副本同时抢同一任务的压测（用 `FOR UPDATE SKIP LOCKED` 领取，逻辑上安全）。

---

## 8. 配置、依赖、权限变更

### 8.1 环境变量

| 变量 | 作用 | 默认 | 备注 |
|---|---|---|---|
| `TP_RUNTIME_OUTBOX_DISPATCH` | 开启 outbox 投递器 | 关 | **新**，值 `on` 才开启 |
| `TP_COMMUNITY_BASE_URL` / `TP_COMMUNITY_API_KEY` / `TP_COMMUNITY_EMAIL` / `TP_COMMUNITY_PASSWORD` | Community 客户端地址与账号 | **`https://demo.thingspanel.cn` / 空 / `test@test.cn` / `123456`** | ⚠ 未配置时默认指向公网演示站；开启投递器前**必须**配置，否则会向演示站发写请求 |
| `TP_RUNTIME_DEVICE_ID_MODE` | 设为 `identity` 时把原生 UUID 当 Community 设备 ID | 空 | |
| `TP_RUNTIME_COMMUNITY_NAMESPACE` | Community 运行时引用的命名空间 | `community` | |
| `TP_DEVICE_CONNECTION_ENDPOINT` | 连接信息里展示的接入地址 | 未配置显示“未配置” | |
| `METADATA_CREDENTIAL_KEYRING_B64` / `METADATA_CREDENTIAL_ACTIVE_KEY_REF` | 凭据加解密密钥 | 无 | 缺失时凭据创建/列表失败 |
| `DEVICE_METADATA_DEFAULT_TENANT` / `TP_LEGACY_DATABASE_URL` | 旧版路由使用 | | 此前已有 |

### 8.2 Go 依赖（`encore-api/go.mod`，**未提交**）

| 变化 | 说明 |
|---|---|
| `go 1.23 → 1.24.0` | 此前会话改的，我没动 |
| `encore.dev v1.30.0 → v1.57.13`、`pgx v5.7.2 → v5.7.6`、`testify v1.8.1 → v1.11.1`、`x/crypto/net/sync/text` 升级 | 同上，此前会话 |
| **新增 `github.com/yuin/gopher-lua v0.0.0-20220504180219-658193537a64`** | **本会话**。因 Go 代理不可达，用的是本机模块缓存里唯一的版本，`go.sum` 条目来自本地缓存，**没有经过校验和数据库核对**；Community 用的是 `v1.1.1`。**CI 联网时请核对或改用 v1.1.1** |

### 8.3 权限（`internal/authz/engine.go`）
本会话新增：`TENANT_ADMIN` 的 `template:diagnose`、`device:simulate`、`device:data:export`、`device:diagnose`。其余策略行为此前会话已加。**其它角色（`tenant_operator`、`tenant_viewer` 等）没有这两个权限。**

---

## 9. 合并范围：哪些该合、哪些别合

### 9.1 `kernel`（Encore）未提交内容分组

| 分组 | 数量/位置 | 属于本任务？ | 建议 |
|---|---|---|---|
| `encore-api/devicemetadata/` 新文件 | 源码 61 + 测试 64 + 迁移 22（0018–0039）+ `accessconfig/` 子包 2 | **是** | 合并 |
| `encore-api/devicemetadata/` 既有文件改动 | `custom_control_service.go`、`migration_service.go`、`models.go`、`store.go`、`device_crud_test.go` | 是（此前会话） | 合并，建议评审 diff |
| `encore-api/internal/{legacy,authz,nativehttp,tenantidentity,luasandbox}` | | 是 | 合并（`legacy/client.go`、`authz/engine.go` 是修改，其余新增） |
| `encore-api/fileassets/` | 未跟踪，整包 | **是，被依赖**：`devicemetadata/native_template_write.go` 导入它（模板封面） | **必须一起合并**，否则编译失败 |
| `encore-api/go.mod`、`go.sum` | 修改 | 是 | 合并并按 §8.2 核对 |
| `encore-api/l1-access/authn/{auth.go,community_session.go}`、`thingmodel/*` | 修改（此前会话） | 间接 | **本会话没评审**；合并前请相关负责人确认 |
| `cli/tpctl-go/`（12 新文件 + `main.go` 改） | 设备/分组/模板/文件资产命令 | 是 | 合并；`go test ./...` 通过（独立模块） |
| `encore-api/automationowner/` | 未跟踪，**自成一套**（含 13 个迁移） | **外部 owner**（设备/模板的“自动化/告警”Tab 对接它） | 与 devicemetadata **无代码依赖**；是否随本次合并需与自动化负责人确认 |
| `encore-api/l3-core/pluginmgr/` | 未跟踪的 Provider 目录/发布（含 2 个迁移） | **否**，与设备三菜单无关 | 建议**拆出**，单独评审 |
| `tools/device-metadata-preflight/` | 独立 Go 模块 | 辅助工具 | 视需要 |
| `deployments/device-metadata-ingress/` | 独立 Go 模块 | 辅助部署 | 视需要 |
| `group-delete-targeted*.log`（4 个，仓库根） | 测试日志 | **否** | **不要提交，删除** |

### 9.2 `frontend`
| 分组 | 说明 | 建议 |
|---|---|---|
| `src/service/api/*`（65 未跟踪）、`src/views/device/*`（19 未跟踪 + 修改） | 原生接口适配与页面 | 合并 |
| `src/views/automation/*`、`src/utils/automation/*`、`src/service/api/automation-*` | 自动化 Tab 对接 | 随 §9.1 自动化 owner 的结论一起处理 |
| `src/router/elegant/*`、`src/router/routes/index.ts`、`src/typings/elegant-router.d.ts` | 路由生成文件 + 手写路由 | **冲突高发区**；见 §10 第 4 项 |
| `build/config/proxy.ts` | 开发代理 | 评审 |
| **`vite.device-acceptance-7312c5b5.mts`** | 内含本机绝对路径 `/Volumes/DevScratch/...` 的验收配置 | **不要提交** |

### 9.3 `backend`（Community）
无代码改动。`docs/initiatives/` 整个目录未跟踪（本任务的全部设计/契约/证据文档都在里面，`device-metadata-native-cutover/` 下 48 个文件，`initiatives/` 共 162 个文件，其中 `openapi.yaml` 约 380 KB）。**是否把文档提交进 Community 仓库需要团队决定**。

### 9.4 建议的合并顺序
1. kernel：先删日志；按 §9.1 拆成几个提交（迁移 0018–0039 + devicemetadata + internal、fileassets、cli、可选的 automationowner/pluginmgr/tools）；`go.mod/go.sum` 单独提交以便核对；然后在 `origin/main-1.3.0`（当前即 HEAD）上推送——**无基线差距**。
2. frontend：先合并 `origin/main-1.3.0`（落后 9 提交），解决路由生成文件冲突，再提交。
3. 合并后在 CI 里跑 §11 的验证命令。

---

## 10. 已知问题 / 风险清单（如实）

| # | 严重度 | 问题 | 位置 / 说明 |
|---|---|---|---|
| 1 | 高 | **全部未提交**，仍在工作区；任何清理/切分支操作都可能丢失 | kernel 197、frontend 125 |
| 2 | 高 | 投递器开启时若未配置 `TP_COMMUNITY_BASE_URL`，会向 `https://demo.thingspanel.cn` 发写请求 | `internal/communityclient/client.go`（此前代码，默认值如此） |
| 3 | 高 | 凭据明文：有 `credentials:read` 即可见所有未撤销凭据 | 产品决定，需安全评审知情 |
| 4 | 中 | 前端 `vue-tsc` 全项目 **1316** 个类型错误；落在本分支改动/新增文件里的有 **25** 个：`src/router/routes/index.ts` 12 个（手写路由名不在生成的 `elegant-router.d.ts` 类型里，**需重新生成路由类型**）、`permission.test.ts` 3、`automation-rules-api.test.ts` 5、`device-info-state.test.ts` 2、`scoped-create-flow.ts` 2、`automation-rules.ts` 1。其余 1291 个在既有文件（如 `src/service/api/device.ts` 91 个，该文件本分支未改）。**我没有逐个确认哪些是本分支引入的** | |
| 5 | 中 | 旧版模板 `draft/publish/archive/derive-draft/bind-thing-model` 接口及 `/api/migrate/*` 仍在代码里（HEAD 已有），与“模板不做草稿/版本/发布”的决定矛盾 | `devicemetadata/` 旧路由（60 条 `/api/...`）。**需决定合并时是否删除**；我没有删 |
| 6 | 中 | 脚本沙箱内存上限是近似的（进程级堆检查） | §7.4 |
| 7 | 中 | gopher-lua 版本与校验和未核对 | §8.2 |
| 8 | 中 | 投递器无分布式锁；摘要非严格 RFC 8785；无“缺号缓冲” | §7.1 |
| 9 | 中 | 前端对 `generated/types.ts` 的 `CredentialDetail` 是**手工补**的，不是生成的 | §4 |
| 10 | 低 | 7 个既有 Go 文件 `gofmt` 不通过（`errcode/constants.go`、`legacydb.go`、`middleware/{audit,auth,middleware_test}.go`、`native_device_operation.go`、`service.go`），我没改 | 合并时 CI 若有 gofmt 门禁会报 |
| 11 | 低 | 前端连接页“仅显示一次”的旧提示文案已过时 | `native-device-credentials.vue` |
| 12 | 低 | 页面级 vitest（`src/__tests__/unit/device-*.test.ts` 等）**本次没有跑成**：它们不在默认 vitest include，我在仓库外放临时配置找不到 vitest | 需在仓库内用正确配置跑 |
| 13 | 低 | `encore check` 因 `notification` 服务缺隐私配置退出 | 环境，非代码 |
| 14 | 低 | `FUNCTION-STATUS-2026-10-27.md` 里个别描述已过时（数据处理页“不可用”已改，其余按 §2 为准） | |
| 15 | 信息 | 契约 `latest-reading-deletions` **有意不实现**：Community 的 `DELETE /api/v1/telemetry/datas` 会同时删除历史和最新值（`DeleteTelemetrData` + `DeleteCurrentTelemetryData`），而契约要求只删 latest 缓存、不删历史，硬接会误删历史数据，必须等 Community 提供“只删最新值”的能力。另外（`simulator-config`、`GET /reading-exports/{operationId}` 已在续里补上） | `openapi.yaml` 里有，代码里没有；做 CI“契约覆盖”门禁时会暴露 |

---

## 11. 验证结果与复现命令

### 11.1 本次新鲜结果（2026-10-29，本机）

| 范围 | 命令（工作目录） | 结果 |
|---|---|---|
| 后端设备元数据 | `encore test ./devicemetadata -count=1`（`kernel/encore-api`） | **ok**，325 个顶层测试通过 |
| 后端其余 | `encore test ./internal/... ./thingmodel/... -count=1` | **ok**：`internal/authz`、`internal/communityclient`、`internal/luasandbox`、`thingmodel`、`thingmodel/app`、`thingmodel/core` 全部通过 |
| CLI | `go test ./...`（`kernel/cli/tpctl-go`） | **ok** |
| 前端 node:test | 见下 | **129 项全部通过**，0 失败、0 跳过 |
| 前端类型检查 | `npx vue-tsc --noEmit -p tsconfig.json`（`frontend`） | **1316 个错误**，其中 25 个在本分支改动文件里，见 §10 第 4 项 |
| Encore 编译/迁移 | `encore check`（`kernel/encore-api`） | 编译、迁移 0001–0037 应用、注册 240 个端点成功；随后因 `notification` 缺配置退出 |

### 11.2 复现命令

```bash
# 后端（必须用 encore test；裸 go test 会在 init 的 cron 处 panic）
cd kernel/encore-api && encore test ./devicemetadata ./internal/... ./thingmodel/... -count=1

# 前端 node:test（这些文件不在默认 vitest include）
cd frontend && grep -l "node:test" \
  src/service/api/device-*.test.ts src/service/api/template-*.test.ts \
  src/service/api/native-template-binding-transport.test.ts \
  src/views/device/details/modules/*.test.ts src/views/device/manage/modules/*.test.ts \
  | xargs node --import tsx --test

# CLI
cd kernel/cli/tpctl-go && go test ./...
```

注意：
- `encore test` 首次可能 4–6 分钟无输出；若卡死，需清理残留的 `encore test`/`encore daemon` 进程后单独重跑；不要同时跑 `go vet`/`go build`（构建缓存竞争）。
- 在 zsh 里不要用 `$files` 变量展开给 `node --test`（不会按空格拆分），用 `xargs`。
- 本机没有 `timeout` 命令。

### 11.3 **没有**做过的验证
- 没有在真实浏览器里点过任何页面。
- 没有对真实 Community 联调（投递器、模拟器、历史读数、凭据全是用假执行器/测试服务器验证的）。
- 没有跑真实 MQTT / 物理设备。
- 页面级 vitest 没跑成（§10 第 12 项）。
- 没有做权限矩阵的全角色实测，只测了 `TENANT_ADMIN` 通过、`tenant_viewer` 被拒、跨租户 404。

---

## 12. 文档索引（`backend/docs/initiatives/device-metadata-native-cutover/`）

| 文档 | 用途 |
|---|---|
| **本文件** `HANDOFF-MERGE-1.3-2026-10-29.md` | 合并交接 + 总汇报 |
| `E2E-CHECKLIST-2026-10-29.md` | 27 块可勾选 checklist + 外部依赖 |
| `TEST-DEVICE-2026-10-29.md` | 设备 Tab 手工测试步骤与期望 |
| `HANDOFF-DEVICE-2026-10-28.md` | 设备原生接口历次交接记录（末尾 4 个“续”是本会话） |
| `FUNCTION-STATUS-2026-10-27.md` | 27 块进度（10-27 快照，部分已过时） |
| `openapi.yaml` / `API-CONTRACT.md` | 契约（权威） |
| `TASKS.md` / `tasks.json` / `test-cases.json` / `TEST-PLAN.md` | 历史规划：188 任务 / 580 用例（**历史工程计数，不是当前完成率**） |
| `EVIDENCE.md`、`evidence/`、`DECISIONS.md`、`DESIGN-CORRECTION-2026-10-27.md` | 证据与决定 |

---

## 13. 建议接手后的顺序

1. **先提交**（§9.4），避免继续丢失风险。
2. 决定 §10 的待定项：旧模板 draft/publish 接口是否删除；automationowner 与 pluginmgr 是否同批合并；文档是否进 Community 仓库。
3. 重新生成前端路由类型和 `generated/types.ts`，清掉 §10 第 4、9 项。
4. 核对 gopher-lua 版本和 `go.sum`（§8.2）。
5. 在测试环境配置 `TP_COMMUNITY_BASE_URL` 等变量，再开 `TP_RUNTIME_OUTBOX_DISPATCH=on`，用一个测试设备联调改编号/删除/绑定。
6. 给我们一套固定的测试租户和账号，补真实浏览器 + 真实后端的端到端，逐块更新 `E2E-CHECKLIST-2026-10-29.md`。
7. 在 Community 接入层补“启用状态参与鉴权”和“凭据按 generation 生效”后，再扩展投递器处理启停/凭据（这是解开 D4、D14 的关键）。
8. 之后再做：图表页面接聚合读数、更大的聚合范围（需要 Community 或时序库侧下推聚合并返回样本数）、模拟器异步化。
