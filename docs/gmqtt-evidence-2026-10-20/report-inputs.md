# 报告素材：社区 MQTT 当前增量与待办

本页整理技术负责人、产品经理和管理层可复用的事实，不是完工报告，也不构成生产准入结论。

## 当前快照

- 候选分支tip为 `860bdb9d`，包含先前 `27e4fa6d` 离线事件判定器和审计CLI提交，不代表完整验收。审计CLI 39项测试/0 skip、一次native复跑和build已核实通过；Linux镜像尚未因新增代码重建。
- 社区验收仍按原 73 项计划：62 项社区 AC 保持 PARTIAL/PENDING，企业专项 11 项 OUT_OF_SCOPE。没有 Gate 或完整 AC 因局部用例而通过。

## 技术负责人可引用

- 原生计划命令用例通过一次：真实 Mosquitto/社区插件、隔离 PG/Redis、应用 Adapter/dispatcher 和 Paho 测试设备完成 preview、真人授权、幂等提交、命令收取及 scoped ACK。语义 API 是精确响应的 `httptest` stub，不是 live Encore；设备 ACK 只证明测试设备的协议回执，不证明物理效果。见[planned-command native E2E](../gmqtt-evidence-2026-10-19/planned-command-native-mosquitto-e2e-2026-10-20.md)。
- 两个独立结果目录记录 adapter subscriber 子进程在 receipt 提交后、MQTT ACK 前遭 `SIGKILL` 的重投测试；Mosquitto broker 在测试中持续运行，无 `source_event_id` 的样本依靠 `ingestion_id` 去重，重投为 duplicate，业务 effect 计数为 1。结果分别覆盖有、无 `source_event_id` 两种 payload；无 source ID 的 `-count=2` 只将最后一次成功运行的原始日志归档到固定文件名。此测试不覆盖 broker 进程重启、硬重启、磁盘故障或 RPO0。见[sigkill evidence](sigkill/)。
- 对一个冻结本地镜像已生成 Syft provenance、SPDX 与 Syft inventory。它们是静态软件库存，不是漏洞扫描、生产制品认证或法律意见。见[SBOM provenance](sbom/provenance.json)。
- SQL012 状态机有真实 Mosquitto 单 cell 自动 ACK 子项；`tp-broker-cutover` 可按审核计划执行 freeze/prepare/activate/rebase。freeze持久保存actor/raw plan hash，prepare/activate/rebase在同事务追加操作者标签/raw hash的`operator_apply`事件，成功重试可能追加事件；标签不是签名身份或审批证明。membership retirement 与双 cell 灰度/回退仍待运行验证。见[cutover CLI限制](../../deploy/mosquitto-community/CUTOVER-CLI.md)。
- telemetry evidence 只查询 trusted processed receipt；当前 writer 没有独立设备 reported timestamp，API 返回 `reported_at:null`、`reported_time_available:false`，缺单位时 `unit_known=false`。见[telemetry evidence API](../gmqtt-evidence-2026-10-19/community-telemetry-evidence-api.md)。

尚待技术验收的重点包括 72 小时稳定性、完整新旧栈配对基线、全量故障矩阵、完整两 cell route barrier/灰度/回退，以及真实物理设备效果。`ingress_v1` 下 custom topic mapping 与旧 service/protocol/raw legacy downlink 是明确 unsupported 边界。

## 产品经理可引用

- 当前命令状态可区分持久接受、协议确认、设备报告与 unknown。测试设备 ACK 不能展示成物理动作成功；遇到 `unknown` 不能推断已执行或未执行。
- 计划命令原生链使用 Encore semantic stub，仍须验证真实 Encore 返回、设备/模型/资源绑定、缺失或冲突语义的处理，以及真人产品授权流程。
- 遥测证据不会将 broker observed time 标成设备采样时间；当前可用单位缺失时会明确 `unit_known=false`。需要设备实际采样时间的产品呈现仍缺数据契约。
- 新 profile 仍有兼容缺口：custom mapping 与旧 service/protocol/raw downlink 被拒绝。产品范围、迁移策略及用户提示尚需确认和验证。

## 管理层可引用

- 当前有多个可复核的工程子项：真实 Mosquitto/PG 的 durable ingress 与命令路径、有限 SIGKILL 重投、单次计划命令链及一个本地镜像 SBOM。它们不等于完整验收或生产准入。
- 当前没有 72 小时稳定性结果、完整 paired baseline、完整两 cell 生产切换与回退演练、AI 盲测/人效/成本结论或物理设备效果证据。
- 报告口径应保持：62 项社区 AC 仍 PARTIAL/PENDING，11 项企业 AC 为 OUT_OF_SCOPE；不得以包级测试率、单次 native E2E、静态 SBOM 或计划 API 存在替代原计划 Gate。
