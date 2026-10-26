# 社区 Mosquitto 验证摘要

## 当前结论与适用范围

候选 `1a1ef0a0` 已在合并提交 `d336de64` 集成到 `main-1.3.0`。默认入口仍为 `legacy`，没有执行数据库迁移或生产切流。当前根 Go 模块编译与相关包回归结果见[合并记录](GMQTT-MERGE-2026-10-26.md)。

历史正式验收口径为 61 项社区 AC：24 PARTIAL、37 PENDING、0 完整 PASS；这是 2026-10-20 台账状态，不是最新源码全量认证。后续提交补充的期望消息、标准控制、插件上下文与有限自定义主题映射，须按实际使用场景另行验证。完整断言和缺项见[AC 台账](gmqtt-community-ac-ledger-2026-10-20.md)，目标与门槛见[实施计划](gmqtt-replacement-plan-2026-10-19.md)。

## 保留的历史结论

以下均为旧源码、指定镜像或隔离夹具的分项测试记录，没有在本次整理中重新执行。

| 专项 | 历史记录支持的结论 | 不能据此推断 |
| --- | --- | --- |
| Mosquitto 原生插件 | 固定 2.1.2 / `99fa50f30e325609394c324c8ff71cfbbe95d8ab`，有 macOS native、Linux/arm64 C-shared 加载与 MQTT 3/5 分项记录；使用受 ACL 保护的 `tp-internal/` 内部前缀 | 当前镜像已重建、其他架构认证或完整 ABI/故障验收通过 |
| 身份与状态 | 隔离 Owner/PG、ACL、连接状态、活动时间与投影，以及异常消息隔离有分项记录；fake Owner 下128个异步授权重叠/释放恢复有独立测试记录 | 完整重连压力、真实 Owner 下所有异步状态组合、延迟 Will 或重启后 Will 恢复通过 |
| 可靠入账 | 原生上行到持久收据、业务行和 automation intent 有分项记录；遥测最终批次有真实 PG 的路由锁、回滚、去重和混合 legacy/trusted 路径记录 | 早期 MQTT ACK 等于遥测落库，或所有窗口均零丢失 |
| SIGKILL 重投 | 指定镜像下 subscriber 在 receipt 提交后、ACK 前被杀；无 source ID 的重投保持相同 ingestion_id/receipt，最终业务行和 intent 各一条 | Broker 崩溃、断电、磁盘故障已覆盖；intent 已执行或设备动作发生 |
| 持久命令与计划 | 幂等、冻结编码、expiry/epoch fence、离线队列/受控重启有分项记录；preview→authorize→submit→原生 MQTT→Paho 测试设备回执有隔离链路记录，另有100调用者授权并发测试 | live Encore、物理执行或完整命令验收通过；Encore 语义端为 HTTP stub |
| Automation | EVT/ATTR/TEL 到持久命令和 Paho ACK 有软件链路记录；设备限流、执行窗口和业务事务有 PG 分项记录 | 所有动作类型、旧插件与网关组合均兼容 |
| 切换与审计 | 单 binding 在两原生 cell 中 A/1→B/2→A/3 往返有独立复跑；切换审计失败回滚与只读 audit CLI 有 PG 记录 | 50 binding、多家族、在途命令、存储故障、旧系统退役及生产灰度通过；actor 标签不是签名审批 |
| 离线判定器 | 正确1k合成账本和各100条丢失、重复副作用、错租户、超时等注入自检有记录 | 真实业务性能、零丢失或正式采集已通过验收 |
| AI 工具与证据 API | 受限委托、租户/用户/设备范围及只读 RPC 有隔离 PG/Redis/TLS 记录；telemetry API 不把观测时间伪装为设备采样时间，缺单位明确未知 | 可用聊天产品、前端/BFF、真实模型调用、人效评测或物理效果已交付 |
| 历史镜像与 SBOM | Linux/arm64 镜像 `sha256:ac1a075beb345ea8475d385d2b37d0af9a5bde5cbdcc66ae906f0f061b07fa8e` 有运行与 Syft/SPDX 静态清单记录 | 漏洞扫描、法律准入或当前可部署制品；后续核查未找到原镜像及可加载归档，需重建 |

历史包级测试曾分别出现权限夹具、活动委托夹具清理等失败，修正后局部复跑通过；不能把不同源码/不同轮次结果合并描述为单轮全绿。本次 Windows 合并回归的编译问题、修正、实际 PASS/SKIP 数与 race 结果单独保留在合并记录中。

## 尚未完成的验收

- 完整生产样本与支持范围冻结、旧新栈配对性能/容量、极限压力、完整故障矩阵、72小时长稳。
- 完整两 cell 灰度/回滚、membership retirement、命令在途与磁盘故障、物理设备效果。
- 在用 OTA、网关、旧协议/插件与自定义映射的实际组合验收；`ingress_v1` 的 OTA 入口仍明确拒绝。
- AI 聊天/BFF/前端产品闭环、live Encore、真人盲测、人效和成本验证。

## 历史证据追溯

2026-10-26 将过程日志、原始 NDJSON/JSON、旧构建清单/哈希、重复阶段汇报从当前检出清理；没有修改 Git 历史，也没有删除现行源码、测试、迁移、契约或可维护部署工具。需要逐条复核时读取固定提交，不把旧证据恢复为当前认证：

- [2026-10-19 历史证据](https://github.com/ThingsPanel/thingspanel-backend-community/tree/1a1ef0a005a64a8d80e627cb8546794e018e2c9d/docs/gmqtt-evidence-2026-10-19)
- [2026-10-20 历史证据与 SBOM](https://github.com/ThingsPanel/thingspanel-backend-community/tree/1a1ef0a005a64a8d80e627cb8546794e018e2c9d/docs/gmqtt-evidence-2026-10-20)
- 本机无需网络亦可按原路径查看：`git show 1a1ef0a0:docs/<原路径>`；Windows 合并后原始文件可从 `d336de64` 查看。
