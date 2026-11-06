# HA 插件与可执行连接器资源中心：任务卡（2026-11-06）

配套 [目标分析](HA-PLUGIN-TARGET-ANALYSIS-2026-11-06.md)。按依赖顺序推进；每卡从所属仓库批准的 local develop/基线开个人 worktree。入口是定位建议，不代表源码已合入或部署。最终发布需负责人确认。

## 依赖顺序

```text
P0 现状/合同/信任根冻结
 ├─ P1 Horizon connector发现与官方审核发布
 ├─ P2 官方签名目录在线同步 + Agent本机验签
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

## P2：官方目录同步及 Agent 双重验签

- **仓库/入口：**安装器 `tools/plugin_catalog.py`、`config/native/` 下载/更新流程；Agent `agent/catalog.go`、`agent/service.go`、`cmd/tp-agent`。
- **工作：**按 P0 冻结的 schema 实现安装器只访问官方 allowlist 的在线 sync，原子更新本机 catalog+signature 缓存；Agent 启动和执行前继续用预置 root 验签。目录只给不可变 digest；市场 API 不直接下发任意镜像/URL。
- **验收：**跨语言签名 fixture 一致；篡改、过期、回滚、未知根、重定向外域、mutable tag、错误架构、摘要不匹配都失败关闭；断网保留 last-known-good，损坏的新文件不破坏缓存；Agent 能拒绝未签名条目。
- **冲突：**`agent/catalog.go`、schema、verifier 在 P0/P2 期间各只有一个 writer；Horizon可基于已冻结 OpenAPI 并行做展示。
- **待决：**企业私有官方端点是否允许；如果允许，其信任根如何受控更新。

## P3：受管身份、续期、失败退避和重启恢复

- **仓库/入口：**社区后端 `internal/pluginidentity`、`internal/api/plugin_runtime_identity.go`、`internal/middleware/plugin_runtime_identity.go`、`internal/plugininstall/{engine,reconcile,repo}.go`；安装器 Agent `agent/{activate,service,state}.go`；SDK/HA startup sync。
- **工作：**让身份有效期与 Agent 执行租约在同 generation 续期，不换 token、不重启容器；明确 offline、lease refused、identity expired、auth/scope error 的状态和恢复策略。状态机持久化 operation_id、步骤、request_key、次数、下次重试和 Agent receipt；采用指数退避+jitter、并发上限、预算和可观察告警，避免每轮都从头签新 generation。
- **恢复设计前置决策：**身份 token 只回传一次而数据库只存哈希。选择短期加密 issuance outbox（Agent ACK 后删密文）或按租户/实例/operation_id 绑定的安全幂等重放；不得明文落普通表/日志。未决前不得声称后端崩溃可自动续作。
- **验收：**匿名、用户 JWT、伪造头、错租户/service、旧代/吊销/过期均拒绝；持续续租 generation/token不变且容器不重启；Agent 离线恢复、进程重启、请求已到但响应丢失/请求未到、身份续租失败均不快速重签代次；租约过期会停止容器，恢复后有界重试并可观测。
- **冲突：**`internal/plugininstall/*`、identity renew API/migration 单一 writer；不能两条后端 worktree并改状态机、代次或迁移序号。
- **待决：**outbox 与幂等重放选型；续期周期、TTL、退避上限、运营介入状态。

## P4：HA 零字段安装和接入点状态

- **仓库/入口：**安装器 `plugins/homeassistant.json`、`tools/plugin_install.py`、Agent catalog；HA `startup_sync.go`、`handler.go`；后端 `internal/api/{service_access,protocol_plugin}.go` 与日志脱敏。
- **工作：**安装字段保持零个，不恢复通用 `env_user`；HA URL/token 由用户之后在平台服务接入点填写，并由受管身份拉取 voucher。安装后引导“配置接入点→检查连接→选择 HA 实体建设设备”。分开报告容器进程健康、身份、voucher读取、HA可达/认证、初始同步和设备数据状态。
- **验收：**零字段可安装；未建接入点显示待配置而非已接通；有效 voucher 可读 HA 状态，错误 token 显示认证失败；凭据轮换有效；日志/操作记录无 URL/token；真实请求的 Bearer 和租户/service scope 已验证。当前 voucher 明文存储与密钥保护另列决策，不能认为安装器 secret store 已保护它。
- **冲突：**HA目录条目、签名目录和 digest 由 release owner 单独维护；HA `startup_sync.go` 同一时段只交给一个 connector owner。
- **待决：**voucher 加密与 key management；连接状态由 HA 插件还是控制面汇报。

## P5：安装/升级迁移与主机准备

- **仓库/入口：**安装器 `config/native/prepare.py`、bootstrap/update scripts、`tools/plugin_install.py`、Agent host service。
- **工作：**PG migration 33–35 和 Broker 001–017 纳入幂等新装/升级；Broker 插件设备授权范围刷新后按 owner→Mosquitto 顺序重载。不要把未验证 MySQL migration 自动放入默认流程。UI/CLI 提示安装与 HA 接入为不同步骤。
- **验收：**clean PG+Timescale 新装和支持版本升级均自动迁移，重复执行安全；失败明确阻断；新增插件设备后授权范围刷新并完成真实上下行。MySQL单独待负责人决定后验证。
- **冲突：**`prepare.py`、升级脚本和 migration list 由一个 installer bootstrap owner 修改；不得和 HA catalog release 同时改同一 plugin entry。
- **待决：**MySQL 支持决定；Broker 刷新由 install command 触发还是 Agent monitor。

## P6：真实最终用户路径和版本冻结

- **涉及仓库：**Horizon、安装器/Agent、社区后端、HA/SDK及必要前端；用隔离 PG+Timescale 栈和测试 HA。
- **最终路径：**资源中心发现官方 connector→详情/权限→同步官方目录→不提供 HA 字段安装→Agent验签/按digest启动→进程健康→平台建服务接入点→HA连接就绪→选实体建设备→当前值/历史可读→下发命令并确认真实 HA 状态/设备回执→重复安装仍单实例→后端重启、Agent断线恢复、同代续租→卸载吊销、停容器、旧 token 被拒绝。
- **验收：**真实 HA MQTT 上下行；跨租户/伪造身份拒绝；HA凭据在日志和事件中 grep 为零；四仓 commit、dirty状态、digest、架构、migration、目录版本和root指纹入脱敏记录。静态市场页 200、mock或单测不算完整路径。
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
