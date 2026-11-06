# HA 插件与可执行连接器资源中心：任务卡（2026-11-06）

配套 [目标分析](HA-PLUGIN-TARGET-ANALYSIS-2026-11-06.md)。按依赖顺序推进；每卡从所属仓库批准的 local develop/基线开个人 worktree。入口是定位建议，不代表源码已合入或部署。最终发布需负责人确认。

## 依赖顺序

```text
P0 现状/合同/信任根冻结
 ├─ P1 Horizon connector发现与官方审核发布
 ├─ P2 官方签名目录交付 + Agent本机验签
 ├─ P3 受管身份请求、同代续期、签发中断恢复
 └─ P4 HA零字段安装、接入点凭据和连接状态
       ↓
P5 安装器迁移/恢复/有界退避
       ↓
P6 干净PG+Timescale用户路径和版本冻结
```

## P0：现状、接口与正式发布门槛

- **仓库/入口：**社区后端 `docs/plugin-runtime-identity.md`、`sql/33.sql`–`35.sql`、`internal/plugininstall`；安装器 `plugins/homeassistant.json`、`agent/catalog.go`、`config/native/prepare.py`；HA `startup_sync.go`、`main.go`、`handler.go`；SDK `platform.go`；Horizon `services/market-service`、Portal market APIs。
- **工作：**各仓记录 commit、dirty/untracked 文件、镜像 tag/digest/架构、SDK 版本、migration 集合；核对 PG 33–35 是否在最新安装/升级路径。MySQL 未测不得加进默认迁移执行，须负责人明确范围。逐请求追踪 HA `runStartupSync` 构造、SDK/HTTP transport、Bearer 注入、后端认证及 voucher scope；当前源码片段与 T5 报告不一致，应在准备发布二进制上验证，暂不判定成败。
- **P0 阻断项：**正式签名 root 不存在；目录指向旧 ARM64 image digest；HA 双架构新制品未冻结；迁移需按最新 develop 重验；身份注入路径需实测。
- **验收：**版本矩阵区分源码候选/已提交/已构建/已部署/已实测；每项有 commit、摘要、架构、migration 和结果。不得复制任何真实凭据。
- **冲突：**本卡只读。发现的 dirty worktree 保留，不 cherry-pick 或覆盖。
- **待决：**MySQL 是否属本期发布；正式 Registry、离线 root 与在线 signer 维护责任人。

## P1：Horizon 连接器发现与审核发布

- **仓库/入口：**Horizon `/Users/junhong/Downloads/code/thingspanel-takeover-final-20261102/horizon` 的 `services/market-service` 模板/Bundle handler、repo、迁移；Portal `src/pages/market`、`src/api/bundle-market.ts`。实施时从 Horizon 认可的基线单独开 worktree。
- **工作：**新增 connector 资源类型、详情/版本/兼容架构/权限/数据范围/审核状态；作者投稿进入审核，批准后关联官方签名目录版本和 immutable image digest。现有模板/看板/Bundle 不变；Bundle `plugin_dependencies` 不是可执行插件市场。
- **验收：**draft/rejected 不显示安装；approved 有安装入口及权限/兼容信息；下架/撤回能说明不可安装；普通资源路径无回归；Portal 不接触签名私钥或 Registry 凭据。
- **冲突：**Horizon schema、migrations、DTO 单一 writer，避免多个分支改同一迁移序号；后端/Agent 不在此卡改。
- **待决：**market-service 存储目录本体还是只存 release pointer；作者、审核者角色授权。

## P2：官方目录交付及 Agent 双重验签

- **仓库/入口：**安装器 `tools/plugin_catalog.py`、`config/native/` 下载/更新流程；Agent `agent/catalog.go`、`agent/service.go`、`cmd/tp-agent`。
- **工作：**先决策目录交付：在线 sync（安装器仅访问官方 allowlist，原子更新 catalog+signature 缓存）或首期受控分发同格式签名文件。Agent 启动和执行前继续用预置 root 验签。目录只给不可变 digest；市场 API 不直接下发任意镜像/URL。
- **验收：**跨语言签名 fixture 一致；篡改、过期、回滚、未知根、重定向外域、mutable tag、错误架构、摘要不匹配都失败关闭；在线方案断网保留 last-known-good，受控文件方案原子校验且损坏文件不覆盖有效目录；Agent 能拒绝未签名条目。
- **冲突：**`agent/catalog.go`、schema、verifier 在 P0/P2 期间各只有一个 writer；Horizon可基于已冻结 OpenAPI 并行做展示。
- **待决：**首期在线 sync 还是受控签名目录分发；企业私有官方端点是否允许及如何受信。

## P3：受管身份、续期、失败退避和重启恢复

- **仓库/入口：**社区后端 `internal/pluginidentity`、`internal/api/plugin_runtime_identity.go`、`internal/middleware/plugin_runtime_identity.go`、`internal/plugininstall/{engine,reconcile,repo}.go`；安装器 Agent `agent/{activate,service,state}.go`；SDK/HA startup sync。
- **工作：**让身份有效期与 Agent 执行租约在同 generation 续期，不换 token、不重启容器；明确 offline、lease refused、identity expired、auth/scope error 的状态和恢复策略。状态机持久化 operation_id、步骤、request_key、次数、下次重试和 Agent receipt；采用指数退避+jitter、并发上限、预算和可观察告警，避免每轮都从头签新 generation。
- **恢复设计前置决策：**身份 token 只回传一次而数据库只存哈希。选择短期加密 issuance outbox（Agent ACK 后删密文）或按租户/实例/operation_id 绑定的安全幂等重放；不得明文落普通表/日志。未决前不得声称后端崩溃可自动续作。
- **验收：**匿名、用户 JWT、伪造头、错租户/service、旧代/吊销/过期均拒绝；连续运行至少 30 分钟，generation 不变、没有 `renew_refused`、容器未重启；再把测试环境 lease/identity TTL 临时缩短，在多个续期周期观察有效期推进，以证明续租真实生效。Agent 离线恢复、进程重启、请求已到但响应丢失/请求未到、身份续租失败均不快速重签代次；租约过期会停止容器，恢复后有界重试并可观测。
- **冲突：**`internal/plugininstall/*`、identity renew API/migration 单一 writer；不能两条后端 worktree并改状态机、代次或迁移序号。
- **待决：**outbox 与幂等重放选型；续期周期、TTL、退避上限、运营介入状态。

## P4：HA 零字段安装和接入点状态

- **仓库/入口：**前端 `/Users/junhong/Downloads/code/thingspanel-frontend-wp-pui` 安装抽屉/资源中心路由/安装状态页；安装器 `plugins/homeassistant.json`、`tools/plugin_install.py`、Agent catalog；HA `startup_sync.go`、`handler.go`；后端 `internal/api/{service_access,protocol_plugin}.go` 与日志脱敏。
- **工作：**前端串起资源中心发现→详情→安装抽屉→零字段安装→安装后跳转服务接入点创建/配置→实体选择；只允许租户管理员执行安装/重试，其他角色显示权限错误/只读状态。显示安装进度、失败步骤与可重试入口；进程级 HA 插件健康与每个接入点各自的 HA 可达/认证/同步状态分开显示。安装字段保持零个，不恢复通用 `env_user`；HA URL/token 在服务接入点填写，并由受管身份拉取 voucher。
- **验收：**租户管理员从资源中心完成零字段安装并被引导到接入点；非管理员不能安装/重试；失败后按失败类型提供重试并保持单实例。一个插件进程健康状态不能代替每个接入点状态：分别验证两个接入点一好一坏时状态准确。有效 voucher 可读 HA，错误 token 显示认证失败；凭据轮换有效；日志/操作记录不含凭据（不把非凭据 URL 一律列为禁止项）；真实请求 Bearer 和租户/service scope 已验证。voucher 加密与密钥保护另列决策。
- **冲突：**HA目录条目、签名目录和 digest 由 release owner 单独维护；HA `startup_sync.go` 同一时段只交给一个 connector owner。
- **待决：**voucher 加密与 key management；连接状态由 HA 插件还是控制面汇报。

## P5：安装/升级迁移与主机准备

- **仓库/入口：**安装器 `docker-compose.yml`、`tools/agent_host.py`、`install.sh`/`install.bash`、`upgrade.sh`/`upgrade.ps1`、`config/native/prepare.py` 与 bootstrap/update scripts。
- **工作：**正式 Compose 预建 Agent `run/` 目录并设最小权限，权限按 Agent 写入、backend 实际 UID 只读来配置；`run/` 只读挂载 backend，明确设置 `TP_AGENT_SOCKET`、`TP_AGENT_TOKEN_FILE`。`install.sh` 正式入口及升级入口都调用 `agent_host.py up`（含 install.bash/upgrade.* 调用链），保证 Agent/socket/token 先于 backend 可用；验证旧栈升级兼容。PG migration 33–35 与 Broker 001–017 纳入幂等流程；不要把未验证 MySQL migration 放入默认流程。修复独立新装预检 bug，并为 install 与 upgrade 各覆盖一次。
- **验收：**独立干净新装预检通过，Agent启动并创建 socket/token；backend 实际 UID 可通过只读 `run/` 挂载连接 socket、读取 token，未授权用户不可读；新装和升级日志/进程状态都证明 `agent_host.py up` 先于 backend 启动且重复执行幂等。升级旧栈可补建 `run/`。PG新装/升级迁移和 Broker 授权刷新真实通过；MySQL单独待决。
- **冲突：**`docker-compose.yml`、`agent_host.py`、`install.*`/`upgrade.*`、`prepare.py` 和 migration list 由一个 installer bootstrap owner 修改；不得与 HA catalog release 并行改同一 plugin entry。
- **待决：**MySQL 支持决定；Broker 刷新由 install command 触发还是 Agent monitor。

## P6：真实最终用户路径和版本冻结

- **涉及仓库：**Horizon、安装器/Agent、社区后端、HA/SDK及必要前端；用隔离 PG+Timescale 栈和测试 HA。
- **最终路径：**前端资源中心发现→详情/权限→安装抽屉零字段安装→管理员权限验证/安装状态轮询/失败重试→跳转服务接入点→每个接入点状态→实体选择→上行/下行→重装→续租/重启恢复→卸载。
- **验收：**安装进行中杀掉 backend 再启动：固定 Owner 重启后应立即接管 unfinished rows 并续作，不重签代次/不重复建容器；模拟 hostname 重建导致 Owner 不同，应最多等待原 lease 到期后接管，并在 UI 显示等待。连续至少 30 分钟 generation 不变、无 `renew_refused`；缩短测试 TTL 后观察多个周期的 lease/identity 到期时间推进，证明续期实际生效。双接入点分别显示连接状态；真实 HA MQTT 上下行；跨租户/伪造身份拒绝；凭据在日志和事件 grep 为零。记录四仓 commit、dirty状态、digest、架构、migration、目录版本和root指纹。静态市场页200、mock或单测不算完整路径。
- **冲突：**安排一个固定测试窗口和版本矩阵；测试期间不可并行更新其中任何服务或签名制品。
- **待决：**端到端验收使用独立测试根还是正式根；voucher 加密在真实凭据进入前必须决策。

## 并行规则与交付边界

| 文件/接口 | 单一 writer | 允许并行 | 禁止并行 |
|---|---|---|---|
| `plugins/homeassistant.json`、signed catalog、digest | T6/release owner | Horizon mock UI | 多条线同时改字段/版本/digest |
| Agent `catalog.go`、trust root、schema | P2 Agent owner，待P0 contract冻结 | Horizon基于fixture展示 | 两个 worktree同时改验签/schema |
| 后端 `plugininstall/*`、identity renew | P3 backend owner | 其他仓只读核对 | 两个后端 worktree改状态机/代次/migration |
| HA `startup_sync.go` | HA connector owner | 后端验证冻结的 API | 多 worktree同时改同文件/先于源码发镜像 |
| Horizon migration/API DTO | Horizon owner | Portal fixture页面 | 多 worktree改同迁移序号/shared DTO |
| PG migrations 33–35 与 bootstrap清单 | 后端/安装器 migration owner 联签 | 只读版本核查 | 单边改号或默认自动运行未验证 MySQL migrations |

本次任务只交付文档，不改代码、不发布、不推送、不合并。证据、真实 HA token、签名私钥、JWT、Agent bearer 和完整环境文件留在批准的仓外位置；仓库仅留脱敏索引。实现卡结束时分别汇报候选代码、聚焦测试、跨服务验收、迁移/升级和未验证风险。
