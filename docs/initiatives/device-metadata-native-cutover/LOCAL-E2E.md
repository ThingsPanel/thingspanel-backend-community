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

**指令下发、期望消息（真实后端 + 测试替身，页面操作）**：先创建含 `COMMAND` 字段“重启”的物模型并发布，在模板编辑器里选该版本并保存，再建绑定该模板的设备。设备详情“指令下发”→“下发命令”，命令标识符下拉里出现物模型的“重启”（说明绑定的快照传到了页面）；下发后列表出现 `reboot`，状态“设备已应答”（替身的状态轮询返回 `device_succeeded`）。打开“期望消息”开关并填 1 小时过期后下发，“期望消息”标签出现一条 `reboot`、状态“运行时已接收”、带“取消”；点取消并确认后该行消失。替身收到的调用依次是：`POST /command/datas/pub` → `GET /command/datas/status/:id`；`POST /expected/data` → `GET /expected/data/list` → `DELETE /expected/data/:id`，与后端客户端的预期一致。**带参数的命令（页面走通）**：物模型里的命令“设模式”带整数参数“档位”，“下发命令”对话框自动生成“档位”输入框；不填参数直接下发，后端拒绝并提示（“one or more device fields are invalid / 验证失败”）；填 3 后下发，列表显示 `setmode`、状态“设备已应答”、内容 `{"level":3}`，Community 替身收到 `POST /command/datas/pub`，`value` 为 `{"level":3}`。命令状态在页面里的表现（替身用环境变量 `STUB_COMMAND_STATUS` 控制返回的状态）：返回 `timeout` 时列表显示“超时未应答”；返回未识别的状态时显示“结果未知”；发布请求正好落空（替身重启的瞬间）的那条命令一直显示“提交中（结果待确认）”，不谎报成功或失败。Community 拒绝“上线后发送”（替身用 `STUB_EXPECTED_REJECT=1` 让 `POST /expected/data` 返回 400，模拟 `ingress_v1` 等拒绝场景）：下发后“指令下发”列表里该命令显示“失败”，原因栏为 `the device runtime did not accept the command`（后端写入的英文原文；前端现已把后端自己写的两条原因译成中文“设备运行时明确拒绝了该命令”“设备运行时未确认是否收到该命令”，运行时返回的文本原样显示；该翻译已在页面里复走：失败行的原因栏显示“设备运行时明确拒绝了该命令”）；明确拒绝记为失败，没应答才记为未知，两者区分正确。后端单元测试覆盖了超时/未知状态和“上线后发送”的校验（`encore test ./devicemetadata -run 'TestNativeCommand|TestNativeCancelReason|TestApplyingADeletion|TestRuntimeReceipt|TestDispatchRuntimeOutbox'` 通过）：必填参数缺失/未声明参数被拒，`timeout→timed_out`、未知状态→`unknown`，过期时间必须在未来，必填参数缺失/未声明参数被拒。`ingress_v1` 是 Community 侧行为，本仓库代码里没有该标识，无法在此验证。

**设备模板的“协议配置”和“关联设备”（真实后端 + 测试替身，页面操作，T4/T2）**：协议配置页显示接入信息（direct / mqtt / 凭据类型 none / 在线判定 transport 60 秒）；“新增 Topic 映射”对话框能填名称、方向、源主题、目标主题，未选物模型版本时数据引用被禁用并提示原因；确定后 `PATCH` 200、列表出现该行；启用开关关闭、编辑改名都是 `PATCH` 200，接口读回 `enabled:false` 和新名称；删除后列表回到“此模板当前没有 Topic 映射”。关联设备页列出绑定该模板的设备（名称可点、编码、类型、管理状态）；通过 `PUT /devices/:id/template-binding` 把其中一台的 `templateId` 置空（202），模板的关联列表只剩另一台。没有走通的是设备设置页里“清空下拉并保存绑定”的页面点击（下拉清除图标在自动化点击下没有响应，改用同一接口验证）。顺带修了关联设备和子设备两处表格把 `enabled` 原样显示的问题，现显示“已启用 / 已停用”。

**设备模板删除（DM-TPL-009，T1；本轮新实现）**：契约定义了 `DELETE /api/v2/device-access-templates/{templateId}`，但后端和页面此前都没有。现已实现：要求 `If-Match` 为模板当前 ETag（否则 412）、`Idempotency-Key` 幂等（重放返回同样结果且不重复写审计）、仍有未删除设备绑定该模板时返回 409、其余软删除并写审计；新增权限 `template:delete`（TENANT_ADMIN）。数据库测试 `TestNativeTemplateDeleteRefusesUsedTemplatesAndIsConditionalAndIdempotent` 覆盖过期 ETag、被使用、跨租户（NotFound）、成功、重放、读取变 NotFound、审计恰好一条。页面：模板列表（卡片和列表视图）新增“删除”，点击后确认，先取最新 ETag 再删；在真实浏览器里验证——被设备使用的模板提示“仍有设备在使用该模板，请先把这些设备解绑后再删除”且仍在列表；未被使用的模板删除后列表少一项，接口读回 404。**只检查了本服务能看到的引用（设备）；契约还要求产品、OTA、自动化规则、市场包等引用，它们在别的服务里，本仓库无法跨服务检查，联调时需要补。**

**设备模板复制（DM-TPL-011，T1；本轮新实现，页面验证）**：`POST /api/v2/device-access-templates/:id/copies`，请求体只有 `name`，新建一个已启用的模板，保留描述、品牌、标签、封面和整份配置（包括所选物模型版本），不复制设备；幂等键重放返回同样结果；重名返回 409；跨租户返回 404；源模板不被改动。数据库测试覆盖以上各条。页面：模板列表每个模板新增“复制”，弹窗预填“原名 副本”，提交后跳到新模板的编辑页；接口读回新模板版本 1、描述和标签与源一致；用已存在的名称再复制，提示“已有同名模板，请换一个名称”，弹窗保留可改名。

**子设备解绑、凭据轮换/撤销、模板固定（产品范围决定后新做，真实后端 + 测试替身）**：
- **解绑子设备**：网关绑定子设备后再解绑（`PUT /devices/:id/gateway-membership` 置空，202），投递器依次向 Community 发出 `PUT /device {parent_id, sub_device_addr}` 和 `PUT /device/sub-remove {sub_device_id}`；子设备的网关归属读回为空，可再绑别处。
- **凭据轮换/撤销**：数据库测试（`TestCredentialChangeIsPlannedFromStoredSecretsAndAppliedByReceipt`）覆盖：轮换时按替换凭据的解密密文计划出 `{"username":…}`；已应用回执使替换凭据变有效、旧凭据作废；撤销时设备还有别的有效凭据就退回用它，一个都不剩就用随机不可用值；替换凭据不存在返回 `CREDENTIAL_NOT_FOUND`。**没有做端到端**：现有流程里没有对外的“激活凭据”入口，不好造数据。
- **模板固定**：已绑模板的设备改绑另一个模板、或解绑，后端返回 409“the device template cannot be changed after the device is created”；没绑模板的设备首次绑定正常。设备设置页：去掉“管理状态/停用”，“设备模板”只读显示模板名并提示“设备创建后不能更换模板”，设备编号和删除设备保留。已知问题：对已绑模板的设备再提交同一个模板返回 503（该路径本轮没有改动，页面不再提供，接口层面待排查）。

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
