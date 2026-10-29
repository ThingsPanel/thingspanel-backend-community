# 本机真实 HTTP 端到端（2026-10-29）

范围声明（先读）：这是**完整 Encore 应用在本机真实运行 + 真实 Postgres + 真实 HTTP 请求**的验证，覆盖设备元数据后端接口。它**不是**“真实浏览器 + 真实 Community 运行时”的整块签收：没有浏览器，没有 Community，notification 服务被替换成空实现。`E2E-CHECKLIST` 里的“整块签收”仍是 0/27。

## 结果

`kernel/scripts/e2e-native-http.sh` 对运行中的应用发 44 项真实请求，**44/44 通过**。覆盖：

| 类别 | 验证了什么 |
|---|---|
| 认证与权限 | 无令牌 401；只读角色不能创建（403）、不能读凭据/模拟器/导出任务；另一租户读不到设备、分组（404） |
| 设备 | 创建（默认设备编号=设备 ID）、幂等重放返回同一设备、读取、列表搜索、`If-Match` 更新成功、旧 ETag 返回 412、改名读回 |
| 设备编号变更 | 提交后返回 202 和操作；没有运行时回执时操作保持 `pending` |
| 分组 | 创建、加成员（需 `expectedGroupVersion`）、成员列表、设备所属分组、非空分组删除返回 409 |
| 凭据 | 创建（密文落库）、列表直接显示明文、另一租户 404、只读角色 403、吊销返回 202 且操作保持 `pending` |
| 模拟器 | 首次保存配置（`If-Match: "sim-profile-0"`）、读回、只读角色 403；无接入配置时 `simulator-config` 返回 404 |
| 导出 | 24 小时内导出在没有 Community 时返回运行时不可用而非崩溃；超过 24 小时的长导出返回 202 `pending`，任务进度可查，只有发起人能查 |
| 指令/期望消息/调试 | 租户管理员能读指令执行和期望消息列表（200）；只读角色 403；调试日志对租户管理员不再 403（无 Community 时 503） |
| 后台任务 | 打开 `TP_BACKGROUND_WORKERS=on` 后，长导出任务在 15 秒内被拉起并变为 `running` |

## 真实浏览器（本机，认证替身）

用前端开发服务器连这个后端，在内置浏览器里操作。登录由一个**测试用认证替身**完成（Community 的登录接口在本机没有，替身只回答“已初始化”、登录、用户信息并返回开发令牌）；**页面里的所有设备、分组、模板数据都来自真实的 Encore 后端**。

| 页面 | 看到的 |
|---|---|
| 设备管理 | 登录后显示后端里真实创建的 3 台设备（“3 个设备”），无后端时显示读取失败提示，不是空白 |
| 设备详情 | 名称、设备 ID 来自后端；遥测、连接、信息、属性、事件上报、指令下发、期望消息、自动化、告警、诊断 10 个 tab 都能打开；逐个点开后接口状态：详情 200、凭据 200、诊断 200、指令/期望消息 200（修复后）、连接信息 404（未配置接入，符合预期）、读数 503（没有 Community，符合预期） |
| 设备模板 | 列表请求 200 |
| 分组管理 | 显示后端里的分组；在页面点“新建分组”，请求 `POST 201`，列表刷新，并在后端核对到该分组 |

| 凭据（设备详情→连接） | 点“创建凭据”直接创建 token 凭据并显示领取结果（含系统生成的凭据内容）；后端核对：列表 1 条、状态 pending、内容可读；点“撤销”并确认后依次发出读设备版本、`POST 202` 吊销、刷新列表 |
| 运行时读取失败 | 没有 Community 时，遥测等 tab 现在显示中文“设备运行时暂时不可用，请确认设备平台已连接后重试”（之前是英文 `internal server error`，已修） |
| 设备模板 | 点“新建设备模板”打开编辑器，填名称保存：`POST 201`，随后读回并跳到带 `id` 的编辑页（这一步先发现并修了编辑器编译错误，见下）；详情页 8 个 tab（关联设备、物模型、协议配置、数据处理、自动化、告警、扩展信息、设置）都能打开，模板读取、`/v1/rules`、`/v1/device-configs/:id/bindings` 都是 200；数据处理 tab 显示“六个处理阶段各允许一条启用脚本，语言 lua” |
| 模板的自动化/告警 tab | 渲染 `main-1.3.0` 的新界面（“对这个对象生效的规则”“已绑定 0 条”“这个模板还没有绑定规则”“场景联动”），请求落在我们 kernel 的 `automationowner` 上并返回 200；还没有创建规则，所以没验证到有规则时的显示。Community 的 `/automation/runtime`（返回 `native`）由测试替身模拟 |
| 自动化 tab（设备） | 这个 tab 来自 `main-1.3.0` 的新实现，请求走 `/automation-core/v1/...`。把前端环境变量 `VITE_AUTOMATION_DEV_PROXY_TARGET` 指向本应用（`http://127.0.0.1:4100`）后，我们 kernel 的 `automationowner` 对 `/v1/rules`、`/v1/device-automation/:id` 返回 200，tab 不再报“无法确认自动化服务状态”。**随后用 `POST /v1/blueprints/device_offline/instantiate` 创建了一条真实规则（“设备离线告警”），设备详情的自动化 tab 显示：规则“设备离线告警”、来源“条件匹配（不限设备）”、状态“已启用”，数据来自我们 kernel 的 `automationowner`；告警 tab 也能正常显示（告警历史为空，因为没有真实告警触发）** |

| 遥测 tab（接 Community 测试替身） | 设备详情的遥测 tab 显示替身提供的最新读数：`temp 21.5`、`humidity 48`，时间戳正确，没有错误提示；模拟上报面板可见 |

| 历史表和导出（模板+物模型+设备，接 Community 测试替身） | 创建物模型并发布、在模板编辑器里选已发布版本保存、新建绑定该模板的设备后，设备列表“紧凑读数”视图显示映射后的读数（`温度 21.5`），未在物模型里的字段标“绑定版本未定义”。**数值读数**打开历史曲线（60 个点、平均/最大/最小值，并有前端本地 CSV 导出）；**文本读数**打开原生历史表：默认最近 24 小时共 288 行、分页正常，点“导出”后后台任务完成并下载 `readings-….csv`（1440 行） |

**这一轮在页面里发现并修复的两个问题**

D. **历史表默认就查不出数据。** 原生历史表默认查“最近 24 小时”，但请求结束时间要加 1 毫秒（区间是左闭右开），实际变成 24 小时 + 1 毫秒，后端拒绝（“raw history ranges are limited to 24 hours”），页面只显示“暂无数据”。选择器的校验也有同样的差一问题（刚好 24 小时的区间被当作合法，发出去却被拒）。已改为按包含端点计算跨度（`utils/history-range.ts`，有单元测试），默认起点相应后移 1 毫秒；导出的 31 天上限同理。

E. **开发环境里导出完成后下载 404。** Vite 开发代理没有 `/api/v2/metadata-downloads`、`reading-exports`、`metadata-operations`、`device-number-validations`、`processing-script-tests` 这几条原生路由，请求被 Vite 自己当 404 返回（用 `*/*` 的 curl 会被 SPA 回退成 200，容易误判）。已把它们加入 `build/config/proxy.ts`。线上由网关整体转发，不受影响。

另外，设备详情的遥测卡片本身没有打开历史的入口；历史只能从设备列表的“紧凑读数”进入，且需要设备绑定了模板和物模型。

**设备详情其余标签（真实后端 + 测试替身，未绑定模板的设备）**：连接（凭据创建表单、接入信息）、信息（位置表单）、属性（`fw 1.2.0` 只读）、事件上报（`overheat` 一条）、指令下发（空列表）、期望消息（空列表和筛选）、诊断（检查项 pass/fail/unknown 与“运行时未提供完整统计”）、设置（启停、绑定模板、改编号、申请删除）都能渲染，没有页面报错。发现一处小问题：未绑定模板的设备在连接页“接入信息”直接显示后端英文原文 `device access is not configured`，已改为中文“该设备尚未配置接入信息（需先绑定设备模板）”。指令下发和期望消息在补了替身的指令接口后单独走通（见下）。

**指令下发、期望消息（真实后端 + 测试替身，页面操作）**：先创建含 `COMMAND` 字段“重启”的物模型并发布，在模板编辑器里选该版本并保存，再建绑定该模板的设备。设备详情“指令下发”→“下发命令”，命令标识符下拉里出现物模型的“重启”（说明绑定的快照传到了页面）；下发后列表出现 `reboot`，状态“设备已应答”（替身的状态轮询返回 `device_succeeded`）。打开“期望消息”开关并填 1 小时过期后下发，“期望消息”标签出现一条 `reboot`、状态“运行时已接收”、带“取消”；点取消并确认后该行消失。替身收到的调用依次是：`POST /command/datas/pub` → `GET /command/datas/status/:id`；`POST /expected/data` → `GET /expected/data/list` → `DELETE /expected/data/:id`，与后端客户端的预期一致。带参数的命令、超时变 unknown、`ingress_v1` 下拒绝“上线后发送”没有走。

**数据处理脚本的“测试运行”（真实后端，`POST /api/v2/processing-script-tests`）**：正常脚本返回拼接结果（`{"t":21}|a/b`）；运行时 `error("boom")` 返回 `SCRIPT_RUNTIME_ERROR` 并带堆栈；死循环约 1 秒后返回 `SCRIPT_TIMEOUT`；访问 `os.execute` 被沙箱拦截（`os` 不可用）。引擎为 `gopher-lua-5.1/sandbox-v1`。这只验证了“测试运行”接口；上行数据真正经过脚本处理依赖 Community/MQTT 的数据通路，没有覆盖。

仍未在浏览器里验证：真实 Community 下的遥测值、历史和导出（上面用的是测试替身）。

## 需要 Community 的路径（用测试替身）

`kernel/scripts/community-stub.mjs` 是按 Community 源码接口格式写的**测试替身**（数据是合成的，不是真实 Community）；应用用真实的 HTTP 客户端去调它。`kernel/scripts/e2e-native-community.sh` 共 31 项检查，**31/31 通过**：

- 最新遥测、属性、事件读数；1 小时原始历史（约 60 条）；24 小时聚合（桶里有 `sampleCount`）。
- 短导出：同步完成，下载文件表头正确、约 60 行，另一个用户下载返回 404。
- **长导出（3 天）**：后台任务按小时分片读取 Community，完成后下载一个文件：表头只出现一次、约 4320 行、没有重复时间戳、新到旧排序，进度为全部完成。
- **运行时下发**：改编号提交后，投递器真实调用 Community 的 `PUT /device`，操作变为成功，设备读回新编号；删除设备同理，调用了 `DELETE /device/:id`，操作成功，设备从租户里消失。

启动需要的环境变量：`TP_BASE`（替身地址）、`TP_EMAIL`/`TP_PASSWORD`、`TP_RUNTIME_DEVICE_ID_MODE=identity`、`TP_RUNTIME_OUTBOX_DISPATCH=on`、`TP_BACKGROUND_WORKERS=on`。

这仍然不是真实 Community：真实的鉴权、数据量、时序库行为和 MQTT 没有覆盖。

## 没有覆盖

- 真实浏览器页面；真实 Community（读数、模拟发送、改编号下发、长导出的实际读取）；MQTT；notification 服务（被空实现替换）。
- 需要 Community 才能成功的路径：读数/历史/导出正文、投递器下发。

## 这次跑出来的真实问题（已修）

C. **设备删除“成功”后设备仍然留在列表里。** 删除请求会创建操作并投递给 Community，Community 确认后操作变成成功，但 Encore 里的设备记录一直没有被标记为已删除。已改为：运行时回执确认删除的同一个事务里软删除设备（保留历史）。有数据库测试（删除类回执会移除设备，其它类型的回执不会），并在上面的 31 项里复验。见 `native_runtime_receipt.go`。

A. **新建/编辑设备模板的页面打不开。** `native-template-editor.vue` 里有两处多行 `@click`/`@keydown`，一个表达式里写了几条语句却没有分号，Vue 模板编译失败，页面 500。已改成处理函数，并新增 `device-vue-templates-compile.test.ts`：把设备、自动化、ThingsVis 下的所有页面模板都编译一遍，这类错误以后在单元测试里就会暴露。
B. **前端合并后，所有引用告警面板的页面都起不来。** 合并时 `service/api/automation-native.ts` 被 `main-1.3.0` 同名的另一个模块覆盖，我们的 `AutomationNativeError`、`automationNative` 不存在了，导致 `native-automation-panel.vue`、`native-device-alarms.vue` 以及引用它们的模板详情、设备详情页面加载失败。我们的客户端恢复为 `automation-native-client.ts`，两个面板改为从它引用（只在集成分支，原功能分支不受影响）。

0. **租户管理员被拒绝使用指令下发、期望消息、调试日志（浏览器里看到 403）。** 这些接口要求 `device:control`，而生产里租户管理员的角色名是 `TENANT_ADMIN`，策略表里只有小写的 `tenant_admin` 才有 `control`。单元测试用的是别的身份，所以一直没发现。已给 `TENANT_ADMIN` 补上 `device:control` 和 `device:view`（不给 `manage`），见 `internal/authz/engine.go` 与测试。

1. **`TP_AUTH_MODE=dev` 在 `encore run` 下被误拒绝。** 之前的环境闸门只认类型 `local`/`test`，而 `encore run` 实际报告类型 `development`、名称 `local`、云 `local`（Encore Cloud 的开发环境云是 aws/gcp）。已改为：云为 `local` 且类型为 `development` 也算本机；生产环境永远不算。见 `l1-access/authn/auth.go` 与其测试。
2. **定时任务在本机和自托管里不会自动运行。** Encore 的 `cron` 只在 Encore Cloud 触发。长导出 worker、过期文件清理、outbox 投递器因此在自托管里默认闲置。已加进程内调度：设置 `TP_BACKGROUND_WORKERS=on` 就会在服务进程里按间隔运行同样三个任务（长导出 15 秒、投递器 1 分钟、清理 1 小时），服务关闭时停止。**自托管部署必须设置这个变量**（Encore Cloud 部署可不设，cron 照常）。

## 如何复现

在 Linux 上不需要下面的“空实现”这一步；在 Mac 上因为 notification 服务只能在特定的 Linux 自托管配置下启动，需要一个**临时副本**：

```bash
# 1) 临时副本（不要提交）
git -C kernel worktree add --detach /tmp/rig HEAD
# 2) 仅在副本里，让 notification 初始化可被跳过：
#    编辑 /tmp/rig/encore-api/l3-core/notification/service_runtime.go，在 initService() 开头加
#      if os.Getenv("E2E_RIG_STUB_NOTIFICATION") == "1" { return &Service{}, nil }
# 3) 生成本机测试密钥（仅测试用）
KEY=$(python3 -c "import os,base64,json;print(base64.b64encode(json.dumps({'e2e-key-1':base64.b64encode(os.urandom(32)).decode()}).encode()).decode())")
# 4) 启动（需要 Docker 提供 Postgres）
cd /tmp/rig/encore-api && TP_AUTH_MODE=dev E2E_RIG_STUB_NOTIFICATION=1 TP_BACKGROUND_WORKERS=on \
  METADATA_CREDENTIAL_KEYRING_B64="$KEY" METADATA_CREDENTIAL_ACTIVE_KEY_REF=e2e-key-1 \
  encore run --browser=never --port 4100
# 5) 另一个终端
kernel/scripts/e2e-native-http.sh http://127.0.0.1:4100
```

脚本里的开发令牌格式是 `用户:租户UUID:角色`（如 `u1:<uuid>:TENANT_ADMIN`）。

## 对 Linux/CI 验收的建议

在真实 Linux 环境按同样脚本先跑一遍（不需要替换 notification），再接入真实 Community，执行 `E2E-CHECKLIST-2026-10-29.md` 里需要 Community 的项目：读数、历史、导出正文、改编号/删除/绑定子设备由投递器真实下发。
