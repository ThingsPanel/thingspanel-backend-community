# Community AI: reuse of the existing ChatBridge owner

审计日期：2026-10-20。目的在于判断社区 MQTT 新 API 能否接到现有 ChatBridge agent owner，不构建第二套 LLM/agent 框架。本页仅依据源码，不代表 ChatBridge 或社区服务已部署、工具已安装、模型已调用或生产验收通过。

## 结论

可以复用 ChatBridge 的 `AgentRuntimeKernel`、`RuntimeDispatcher`、租户/用户安装解析、模型调用预算和审计链。当前不能把社区 API 直接配置成现成 HTTP tool：源码声明了 `runtime=http_tool`，但启动时没有注册 HTTP adapter；未知 runtime 会被 dispatcher 跳过。因此需要增加一个受控的社区 API adapter 和对应安装 manifest，并在服务端取得、验证用户委托凭证。ChatBridge 当前传入 runtime 的只有 tenant、system user、thread 等 actor context，没有 ThingsPanel `x-token`；既有 ThingsPanel capability HTTP client 使用服务端配置的 actor 数据，也不是用户委托 JWT。

最小接入应限定在三类已存在的社区能力：只读 semantic evidence、创建命令预览、查询命令状态。提交命令必须由独立的人类确认界面完成精确 plan hash 授权，再由 backend 的受认证用户会话提交。模型工具不应拥有 authorize 能力，也不应接收或选择 API URL、tenant、user 或 bearer token。

## ChatBridge owner、安装与 HTTP 能力

`AgentToolExecutor.execute` 在 runtime kernel 缺失时明确失败关闭；正常路径组装 `actor_context` 为 `tenant_id`、`actor_id`、`thread_id`，并通过 runtime kernel 执行已安装工具。`ChannelBinding` 包含 `tenant_id`、`system_user_id`、`role_key` 等身份字段，没有 ThingsPanel session token。见 ChatBridge `src/chatbridge/services/agent_tool_executor.py:95-110,136-150` 和 `src/chatbridge/domain/models.py:57-72`。

启动时 `RuntimeAdapterRegistry` 注册 Capability、CLI、Script、Legacy Skill Bridge 和 Prompt Playbook adapter，没有 HTTP adapter。`RuntimeDispatcher._adapter_for_installation` 按安装版本的 runtime 查 registry，未注册则返回空并略过。虽然 `RuntimeKind.HTTP_TOOL = "http_tool"` 已定义，但该值本身不提供 HTTP 调用实现。见 `src/chatbridge/bootstrap/builder.py:396-402`、`src/chatbridge/runtime/dispatcher.py:21-41,126-132`、`src/chatbridge/skillspec/models.py:32-42`。

已有 control plane 可管理 skill package/version 和 installation。package 输入含 `package_id`、`version`、`runtime`、`manifest_json`、`metadata`；installation 输入含 `package_id`、`version`、`scope`、`scope_key`、`status`、`tenant_id`、`actor_id`、`metadata`。安装范围支持 `tenant`、`user`、`platform`，创建和更新入口要求 admin 权限；运行时解析启用的安装。见 `src/chatbridge/domain/models.py:223-244`、`src/chatbridge/api/routers/control.py:61-82,151-177`。这提供了授权安装记录的基础，但没有针对 HTTP endpoint、允许路径/方法、请求 schema、固定目标域、超时和响应限制的专用 manifest 契约，也没有 HTTP adapter 来执行它。不能把 `manifest_json` 任意填一个 URL 就视为可用或安全的工具。

现有 `CapabilityHttpClient` 调用固定的 `/api/chatbridge/query`、`/api/chatbridge/object` 和 action 路径，并把 tenant 与 actor 放在请求体。它从 `CHATBRIDGE_CAPABILITY_BASE_URL` 取固定服务地址，默认 loopback `http://127.0.0.1:3010`；这是专用 capability adapter，不是任意 tenant-scoped HTTP tool 安装入口，也没有把 ThingsPanel `x-token` 传下去。见 `src/chatbridge/services/property_system.py:113-145` 与 `src/chatbridge/config.py:166-167`。源码默认值不证明该服务已部署或 URL 已正确配置。

## 社区 API 与凭证边界

当前社区源码提供：

- `GET /api/v1/device/semantic/evidence/:id`：要求 `x-token` 和 middleware 已验证的用户 claims；服务校验 JWT、Redis 会话、用户/租户及设备范围，返回已发布语义和只读状态证据。见 `router/apps/device.go:77`、`internal/api/device_broker_semantic.go:13-39`、`internal/service/device_broker_semantic.go:89-119`。
- `POST /api/v1/command/datas/plans/preview`、`POST /api/v1/command/datas/plans/:plan_id/authorize`、`POST /api/v1/command/datas/plans/:plan_id/submit`：命令计划 API 要求 `x-token` 与 claims，preview 只创建冻结提案；authorize 是对精确 hash 的人类确认，submit 再验证身份和授权。feature 需同时满足 `mqtt.input_profile=ingress_v1`、`mqtt.route_fencing=true`、`mqtt.command_plans_enabled=true`，不能假设默认启用。见 `internal/api/command_plans.go:21-43,77-105`、`internal/service/command_plans.go:48-49` 和 `router/apps/command_data.go:11-17`。
- `GET /api/v1/command/datas/status/:message_id`：查询命令状态，不应解释为物理动作成功；设备报告成功与 broker 协议确认是不同状态。见 `router/apps/command_data.go:19-26`、`internal/api/command_set_log.go:51`。

语义 API 和计划 API 都依赖 ThingsPanel 用户 session token。ChatBridge `AgentToolExecutor` 当前只传 actor identity，不持有或取得 ThingsPanel `x-token`。可行接入需要受信的服务端委托链：由已验证的 ChatBridge 用户身份换取短时、受众受限的 ThingsPanel token，或由用户在受认证前端直接调用 ThingsPanel API；不能把静态服务 token 当成人的委托，也不能让模型提供 token。任何服务端 token 交换都应绑定 tenant、subject、API audience、有效期和允许操作，并由 ThingsPanel 复核当前用户/会话。

## 人工授权、模型预算和审计

ChatBridge 有 `operations_ui` 和 Telegram 的人工确认流程，但代码用途是规则/策略候选确认，不是 ThingsPanel command plan 的 hash-bound 授权；审批流程区分 human/agent/system，并限制 agent 不能冒充人的永久授权。见 `src/chatbridge/services/policy_confirmation.py:1-7,34-72`。不能直接复用其确认记录为设备命令授权。未来可以复用现有人机界面作为确认入口，但它必须显示 plan 的目标设备、规范化命令和 hash，确认人身份必须与 ThingsPanel 当前用户绑定，且最后调用 ThingsPanel authorize API。

模型调用预算当前默认每日 500,000 额度、速率限制冷却默认 300 秒；额度 scope 由 provider endpoint 和 API key 派生，不是 tenant 独立额度。预估消费用 request body 字节数加最大输出 token cap，供应商返回 usage 后结算。可作为现有 owner 的成本护栏，但不能据此声称有 per-tenant AI budget。见 `src/chatbridge/services/model_call_guard.py:49-74,334-365`。

外部模型请求/响应写入 `CHATBRIDGE_STATE_DIR/ai_request_audit.jsonl`，文件按 owner-only 权限创建，发送前必须成功记下 request，响应记录 best-effort；审计内容包括实际 prompt/request 和 response，须按敏感数据管理。见 `src/chatbridge/services/ai_call_audit.py:1-6,24-26,46-66,69-88`。agent tool outcome / runtime audit 是另一条链，不能用模型调用账本替代命令计划与设备回执审计。

Governor 的历史回放默认跳过既有 backlog；设置 `CHATBRIDGE_GOVERNOR_REPLAY_HISTORY=1` 才从 row 0 回放。该开关属于后台治理消费者，不构成用户对 agent 历史数据的读取权限，也不是设备命令状态回放接口。见 `src/chatbridge/governor/service.py:605-627`。

## 部署状态与最小缺口

ChatBridge 仓库在审计时 HEAD 为 `6c844d1`，工作树存在大量未提交改动。本文中关于未提交代码的描述仅对应当时工作树；不能把源码或 control-plane API 存在当作线上部署、安装状态或已启用配置。未启动 ChatBridge、未调用模型、未读取任何密钥环境变量，也未访问外部 API。

最小落地顺序建议：

1. 在 ChatBridge runtime 新增固定社区 API adapter，只允许配置好的 HTTPS base URL 和三类固定操作；manifest 只描述工具名、输入 schema、允许操作，不接受任意 URL、header 或认证字段。按 tenant/user installation 解析工具，并对响应大小、超时、重定向和错误分类设硬限制。
2. 建立服务端委托凭证方案，使 adapter 拿到与当前 `ChannelBinding` tenant/user 对应的短时 ThingsPanel token；绑定身份必须由可信登录/session 服务核验。当前没有可复用的 bearer 来源。
3. 把 semantic evidence、preview、status 暴露为模型工具；submit 只能在用户明确确认后由 UI/service 发起。authorize 端点不加入 model tool surface。保留 plan hash、授权人、提交收据和最终设备 ACK 的关联审计。
4. 先在单租户、测试设备和默认关闭的 feature 下验证安装/身份/撤销/跨租户边界，再由运营显式配置部署。模型调用预算、敏感 prompt 审计与请求响应保留策略也要纳入产品配置。

结论是“复用 owner 与运行时骨架，补受控 API adapter 和可验证委托身份”，不是另建模型/Agent 框架，也不是现有 ChatBridge 已可直接驱动社区命令。
