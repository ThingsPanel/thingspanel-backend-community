# 社区 MQTT 当前验收增量（2026-10-20）

本清单修正当前实现与证据边界，不调整原计划73项验收要求，不授予任何Gate或完整AC通过。62项社区AC继续逐项保持 PARTIAL/PENDING；企业专项11项继续 OUT_OF_SCOPE。

- **源码快照：**本轮报告基于 source checkpoint `ad2cbd7f` 加其后的未提交源码快照。它不是一个新的可复现commit；root尚未提供新的冻结commit，候选最终18项回归也仍在运行，本清单不把其结果提前计入。原工作树还包含其他dirty项目，candidate只可按明确文件清单同步。
- **计划命令原生子项：**[planned-command native Mosquitto E2E](gmqtt-evidence-2026-10-19/planned-command-native-mosquitto-e2e-2026-10-20.md) 的 opt-in 测试已通过：隔离PostgreSQL/Redis、固定Mosquitto与社区插件、实际Adapter/dispatcher及Paho测试设备；设备在线状态由真实连接产生，preview不投递，真人授权后同键幂等提交一次，Paho设备收到私有帧剥离后的命令并发送 scoped ACK。Encore语义服务是精确响应 `httptest` stub，不是live Encore；Paho测试客户端的ACK不是物理执行证据。SQL013计划功能仍默认关闭。
- **service级计划证据：**[service集成记录](gmqtt-evidence-2026-10-19/command-plan-service-integration-2026-10-20.md) 仍提供PG/Redis/Encore-stub下preview、authorize、submit、回滚、改参/状态/语义fence证据。它与上述有限原生测试互补，均未覆盖完整AC-A01–A06、真人产品流程或真实设备效果。
- **SIGKILL子项：**[SIGKILL证据目录](gmqtt-evidence-2026-10-20/sigkill/) 有两次Mosquitto进程 `SIGKILL` 后重连/重投记录；两份 `result.json` 都记录 `source_event_id:null`、以 `ingestion_id` 去重、重投标记 `duplicate:true` 且 `business_effect_count=1`。这是特定image、卷和fixture下的broker进程重启子项，不证明硬重启、磁盘故障或RPO0。
- **制品/SBOM子项：**[Syft provenance](gmqtt-evidence-2026-10-20/sbom/provenance.json) 与 SPDX/Syft inventories 对一个冻结本地镜像 `sha256:ac1a075...` 生成了静态清单；记录的build context和镜像身份见provenance。它不是漏洞扫描、法律意见、外部制品完备性保证或生产镜像准入，也不替代另一台干净机器重建与运行配置认证。
- **路由管理CLI：**新增 `tp-broker-cutover`，经reviewed、原始字节SHA256绑定的plan调用现有SQL012状态机；仅freeze将actor与raw plan hash持久到owner记录/事件，prepare/activate/rebase只在命令输出该hash，尚无完整阶段操作审计。该CLI不制造runtime ACK或越过barrier；membership retirement流程及双cell真实灰度/回滚仍缺验。详见[CLI说明](../deploy/mosquitto-community/CUTOVER-CLI.md)。
- **遥测证据时间：**[telemetry evidence API记录](gmqtt-evidence-2026-10-19/community-telemetry-evidence-api.md) 只查询processed trusted receipts，按observed_at过滤。当前writer没有独立设备reported timestamp，API明确 `reported_at:null` / `reported_time_available:false`；不会把broker观测时刻伪装成设备采样时间。空或缺失单位返回 `unit_known:false`。
- **可靠命令的终态：**网络不确定或到期后可保持/进入 `unknown` 且不自动重发；该状态不证明设备已执行或未执行。ingress_v1当前明确拒绝enabled custom topic mapping和旧service/protocol/raw legacy downlink入口，这些是实际兼容缺口，不能标N/A。
- **仍未验收：**72小时稳定性、完整旧新栈成对基线、全量故障矩阵、完整两cell route barrier/灰度/回滚、完整社区AI盲测/人效/成本以及物理设备效果仍未完成。不得生产切流，也不据局部测试或包级通过率宣称完工。

## 受限 AI 工具增量（2026-10-20，集成中）

新增默认关闭的四个固定工具、120 秒/20 次/明确设备范围的委托凭证，复用既有用户会话、Casbin、语义与命令 owner。真实私有 PG/Redis 的 HTTP 正向 describe/telemetry/preview 和结果读取、撤销/父会话失效、OperationLogs 保密子项已执行。preview 只创建计划，不创建命令。可选 ChatBridge dispatcher→Python adapter→真实 Go 客户端→TLS Gin→私有 PG/Redis 结果读取已通过隔离串联；尚无部署、真实模型调用或物理效果。详见[委托证据](gmqtt-evidence-2026-10-20/ai-delegation-test-plan.md)。AC-A01–A06 仍保持 PENDING；不因这些工具子项提高完整项目通过率。

## 两个原生cell往返子项

[隔离native A/1→B/2→A/3](gmqtt-evidence-2026-10-20/two-cell-native/README.md)的精确测试源在Luna独立race count2中两次通过、无skip；只新增测试，没有更改生产逻辑。验证单binding的真实运行成员/屏障、旧源publish拒绝、目标真实连接和pending→processed业务收据；未验证目标配置cert、命令/物理执行、存储故障或正式灰度。AC-D01与D05为PARTIAL，完整两cell与生产门禁仍未完成。
