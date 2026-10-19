# 社区 MQTT 当前验收增量（2026-10-20）

本清单修正当前实现、范围与证据边界，不调整原计划73项验收要求，不授予任何Gate或完整AC通过。按用户限定的社区Mosquitto一期，61项社区AC继续逐项保持 PARTIAL/PENDING；12项二期OUT_OF_SCOPE为V01–V04/E01–E07的11项企业专项及EMQX纯native专属D03。D03不算PASS，也不减少D01/D02/D04–D06或其他社区适用断言；集群等子句按G0 profile判定。

**本轮交付目标：**结构完整与主链冒烟；已有主链分项证据，实际兼容入口范围仍待判断，尚未宣告完工。72小时稳态、配对性能、极限容量/压力、完整故障矩阵和真人盲测安排为用户后续测试，不作为本轮开发阻塞；正式证据状态仍保持未完成，不提升任何 AC/Gate。锚点 `9c262135` 与 `e565a0ae` 只分别指向对应代码/测试快照，不代表当前分支 tip 或完整认证。OTA、预期数据、旧插件/custom profile、AI 对话与前端的实际使用范围待用户判断；Go 聊天 BFF 尚未落地，新的 Python Host/Vue 组件是冻结未验证草稿。

- **源码快照：**已确认的工程锚点包括离线判定器提交 `27e4fa6d`、审计CLI提交 `860bdb9d` 与 native auth 测试源 `e565a0ae`；不声明当前分支tip，也不代表完整验收。审计CLI 39项测试/0 skip、一次native复跑和build已核实通过；Linux镜像尚未因新增代码重建。原工作树仍有其他任务的dirty文件，candidate同步按明确文件清单进行。
- **计划命令原生子项：**[planned-command native Mosquitto E2E](gmqtt-evidence-2026-10-19/planned-command-native-mosquitto-e2e-2026-10-20.md) 的 opt-in 测试已通过：隔离PostgreSQL/Redis、固定Mosquitto与社区插件、实际Adapter/dispatcher及Paho测试设备；设备在线状态由真实连接产生，preview不投递，调用人工授权接口后同键幂等提交一次，Paho设备收到私有帧剥离后的命令并发送 scoped ACK。Encore语义服务是精确响应 `httptest` stub，不是live Encore；Paho测试客户端的ACK不是物理执行证据。SQL013计划功能仍默认关闭。
- **service级计划证据：**[service集成记录](gmqtt-evidence-2026-10-19/command-plan-service-integration-2026-10-20.md) 仍提供PG/Redis/Encore-stub下preview、authorize、submit、回滚、改参/状态/语义fence证据。它与上述有限原生测试互补，均未覆盖完整AC-A01–A06、真人产品流程或真实设备效果。
- **SIGKILL子项：**[SIGKILL证据目录](gmqtt-evidence-2026-10-20/sigkill/)记录Adapter subscriber子进程在receipt提交后、MQTT ACK前被杀的重投，Mosquitto持续运行；有、无设备source ID的两个结果分别保留，无source ID路径依靠ingestion_id去重。无source ID的count2只在固定文件名保留最后一次原始日志。这是特定image/fixture下的订阅者崩溃子项，业务记录/effect intent唯一不等于已执行副作用；不证明Broker崩溃、硬重启、磁盘故障或RPO0。
- **制品/SBOM子项：**[Syft provenance](gmqtt-evidence-2026-10-20/sbom/provenance.json) 与 SPDX/Syft inventories 对一个冻结本地镜像 `sha256:ac1a075...` 生成了静态清单；记录的build context和镜像身份见provenance。它不是漏洞扫描、法律意见、外部制品完备性保证或生产镜像准入，也不替代另一台干净机器重建与运行配置认证。
- **路由管理CLI：**新增 `tp-broker-cutover`，经reviewed、原始字节SHA256绑定的plan调用现有SQL012状态机；freeze将actor/raw plan hash写入route change，prepare/activate/rebase在同事务追加带操作者标签和raw plan hash的`operator_apply`事件，成功重试可能追加事件。标签不是签名身份或审批证明；该CLI不制造runtime ACK或越过barrier，membership retirement流程及双cell真实灰度/回滚仍缺验。详见[CLI说明](../deploy/mosquitto-community/CUTOVER-CLI.md)。
- **遥测证据时间：**[telemetry evidence API记录](gmqtt-evidence-2026-10-19/community-telemetry-evidence-api.md) 只查询processed trusted receipts，按observed_at过滤。当前writer没有独立设备reported timestamp，API明确 `reported_at:null` / `reported_time_available:false`；不会把broker观测时刻伪装成设备采样时间。空或缺失单位返回 `unit_known:false`。
- **Native auth pending overlap 子项：**[证据记录](gmqtt-evidence-2026-10-20/native-auth-pending/README.md) 对固定测试源 `b08d07c3fff9fa6e6ba354c00a3e62827e994256a1ca612b5478ccc1742cdc93`：fake Owner grant 下128个异步认证同时到达、峰值128，额外探针收到CONNACK 5且pre-release extra为0；释放128个后均成功，新连接恢复成功。原工作树一次、candidate `-race -count=2 -p 1`两次均通过、0 skip。仅macOS native/fake Owner：runtime fencing为0，未覆盖ready-but-uncompleted refresh混合、real Owner/PG、Linux ASan或性能认证；全部MaxPending压力门槛仍未完成，G1/AC-M02状态不变。
- **可靠命令的终态：**网络不确定或到期后可保持/进入 `unknown` 且不自动重发；该状态不证明设备已执行或未执行。ingress_v1当前明确拒绝enabled custom topic mapping和旧service/protocol/raw legacy downlink入口，这些是实际兼容缺口，不能标N/A。
- **正式证据状态：**72小时稳定性、旧新栈配对性能、极限压力、完整故障矩阵、完整两cell route barrier/灰度/回滚、真人盲测/人效/成本及物理设备效果尚未形成正式证据；前述长稳、性能/压力、故障矩阵与真人盲测由用户后续测试。不得据局部冒烟或包级通过率宣称 AC/Gate PASS。

## 受限 AI 工具增量（2026-10-20，集成中）

新增默认关闭的四个固定工具、120 秒/20 次/明确设备范围的委托凭证，复用既有用户会话、Casbin、语义与命令 owner。真实私有 PG/Redis 的 HTTP 正向 describe/telemetry/preview 和结果读取、撤销/父会话失效、OperationLogs 保密子项已执行。preview 只创建计划，不创建命令。此前可选 ChatBridge dispatcher→Python adapter→真实 Go 客户端→TLS Gin→私有 PG/Redis 结果读取的隔离串联子项通过；这不等于新聊天 BFF 或产品对话/前端接线。Go 聊天 BFF 尚未落地，新的 Python Host/Vue 组件仍是冻结未验证草稿；尚无部署、真实模型调用或物理效果。详见[委托证据](gmqtt-evidence-2026-10-20/ai-delegation-test-plan.md)。AC-A01–A06 仍保持 PENDING；不因这些工具子项提高完整项目通过率。

## 两个原生cell往返子项

[隔离native A/1→B/2→A/3](gmqtt-evidence-2026-10-20/two-cell-native/README.md)的精确测试源在Luna独立race count2中两次通过、无skip；只新增测试，没有更改生产逻辑。验证单binding的真实运行成员/屏障、旧源publish拒绝、目标真实连接和pending→processed业务收据；未验证目标配置cert、命令/物理执行、存储故障或正式灰度。AC-D01与D05为PARTIAL，完整两cell与生产门禁仍未完成。
