# 社区 GMQTT 替换执行日志（进行中）

范围：仅社区 Mosquitto；企业 VerneMQ/EMQX 不实施。依据：gmqtt-replacement-plan-2026-10-19.md。
本文是工程进行记录，**不是完工或生产放行报告**。技术总监、产品经理、公司老板三份最终汇报待实际验收后统一写入完工报告；不把未完成项写成完成。

## 组织与当前阶段

总步骤：1盘点、2契约/原生PoC、3可靠链集成、4独立质量/性能故障验收、5部署灰度/回滚、6分角色汇报与记忆收据。
当前：第3步。主代理负责关键设计、C/Go shim、状态/业务事务和跨模块集成；三个 Luna 负责契约/owner/mapper、生命周期/收据/应用接线与端到端测试、独立原生MQTT QA/部署/基线证据。
工作区原有通知、物模型等大量dirty改动保留；尚未把本任务提交为独立候选，后续须干净工作树重建，不能将其他任务改动冒充本次成果。

## 已实现及实际验证

- 固定上游 Mosquitto 2.1.2源码：99fa50f30e325609394c324c8ff71cfbbe95d8ab；本机旧brew2.0.18不用作版本认证；官方Docker tag 2.1.2拉取不存在，采用源码构建。
- `internal/brokercontract`：认证grant、有限ACL、原字节一次Base64、稳定tenant/device/topic键、平台retained真0字节清槽、显式受信state；单元和race通过。
- `cmd/tp-mosquitto-plugin` + `internal/mosquittodriver`：有界异步认证、主线程完成、按实际连接保留principal、5秒策略刷新/60秒授权、失败即拒绝、WRITE/SUBSCRIBE/每次READ授权、原位消息转换、连接观测、即时Will受信旁路；本地原生MQTT3.1.1/5测试与race有分项证据。
- `internal/brokercontrol` + owner cmd：社区主数据唯一身份owner，JSONB精确凭据、username-only、空网关类型与子设备直接登录限制、持久ClientID归属、active/cell/epoch授权；真实私有PG schema事务/并发测试通过。
- `internal/brokermapper`：原有topic mappings迁入社区Go owner，内容寻址不可变快照、有界缓存、标准/自定义上/下行；不支持的规则整份拒绝；PG及race通过。Adapter接线在集成中。
- MQTT启动可取消、Stop真实断连/失败清理、Bus有界取消与send-close防竞态；Paho ingress_v1手动ACK，可靠接受前不确认第二跳；legacy保持原配置默认。
- `internal/brokerledger`：可靠接受、源ID幂等、无源ID合法重复、tenant限额、binding/epoch事务锁、重试/dead/fenced；100并发与失败回滚等真实PG测试通过。
- `internal/uplink/reliable.go`：同一receipt事务同步写已有事件/属性表与automation intent，scoped command ACK；真实PG已验证业务与收据/意图同事务、失败回滚、重复不多写、错设备回执不更新。
- `internal/brokerstate`：同route锁记录每连接序号与tombstone，多连接聚合，迟到offline不关闭新连接，stale为unknown；真实PG通过。
- 应用启动context已接至MQTT，在服务启动前注册OS取消；app/root测试通过。

## PoC导致的必要契约修订

Mosquitto2.1.2 `src/topic_tok.c` 处理 `$share/group/$tp/...` 首token与`$tp/...`发布不一致，真实shared订阅SUBACK成功但无数据。原内部前缀是方案建议，尚未生产发布，现改为 `tp-internal/ingress/v1` 与 `tp-internal/state/v1`，显式ACL仍拒绝设备访问，新旧`$tp`别名都保护。设备外部topic和字节不变、不维护私有broker fork。新前缀真实共享回归由独立QA执行，未回报前不记PASS。见方案附录D。

## 当前必须继续处理的缺口

1. 新前缀后重新归档实际shared/Will/retain结果；Go schema/doc/test与固定制品一致。
2. Luna进行 real owner→native plugin→Paho Adapter→PostgreSQL receipt→domain/intent 的端到端验证，真实Redis子进程/私有PG schema；不能用仅brokerACK/ingress代替accepted或terminal。
3. 自动化意图目前仅耐久保存，dispatch/Unknown、命令原子受理/outbox/过期/受权重验仍需接入。不得声称现规则已完成可靠执行或物理效果。
4. Gateway子设备ACK/深层设备黄金样本、OTA、wire-retained、MQTT5延迟/重启恢复Will仍缺完整认证。当前即时Will成功，超过闭合授权窗口的Will拒绝不等于兼容通过。
5. QoS2当前profile明确限制（MaxQoS1）；须以G0在用样本决定放行范围，不能默默将其当N/A。
6. 入口消息metadata去重不能误吞合法同值采样；普通attribute里的source_event_id可能本来是业务点位，不据字段名改变业务契约；事件显式source ID单独处理。
7. 真实GMQTT配对基线、独立Linux/ASan/UBSan构建、故障命中/对账、容量/72小时长稳、真实灰度观察及回滚尚未完成。
8. 社区AI场景W14–16、留出集/人效/成本评估、最终三个角色报告与正式Candidate Memory收据尚未完成。

## 本机隔离资源（不含秘密）

- 源码、临时构建：`/tmp/tp-mqtt-20261019/`；native2.1.2 binary=`build/install/sbin/mosquitto`；固定gmqtt源码也在该目录。
- 实验broker仅127.0.0.1:32883/32884、fixture127.0.0.1:18084；不改变现用1883/9999。
- 独立QA PG容器 `tp-mqtt-qa-pg-20261019`，127.0.0.1:35432，各测试私有schema。DSN保存在该临时目录0600文件，不写报告或日志。
- SQL：`sql/broker/001_broker_owner_tables.sql`、002_ledger、003_state、004_mapping_snapshots、005_ingress_effects；安装需显式迁移，不在启动中偷偷AutoMigrate。
- 详细原始账本/哈希/真实步骤见 `docs/gmqtt-evidence-2026-10-19/`；支持矩阵仍应逐项判定，已有运行不能代表G3/G4/G5通过。

## 增量检查点：命令收口与Linux原生差异

- 新增 `broker_command_requests`（006迁移）：作用域幂等提交、同事务日志、到期、并发claim、binding→command统一锁序、最终发布事务再次复核，崩溃/网络歧义标记unknown而不自动重发。正在与实际命令API集成验证，未作完整验收。
- 命令内部存储帧 `\x00TP-COMMAND/1\n` 携带command_id、tenant、目标root、epoch、mapping、有效期和原字节；Broker存储保留帧，每次READ/MESSAGE_OUT检查后仅向设备剥离。设备wire topic/payload不变。当前认证候选仅canonical devices/gateway command主题；custom command wire mapping未认证。
- MQTT3 PUBACK可能发生在插件拒绝消息的路径，因此收据使用 `broker_acknowledged` 表示协议确认，不能把Paho token成功说成已投递或执行；到期仍无设备报告标记unknown。
- 早期“新profile机器自动命令一律暂拒绝”状态已被 SQL009 durable planner 取代：同源 EVT/ATTR、携带可信策略快照/proof 的 MQTT command 可生成机器caller命令并走既有dispatcher；无proof、跨设备条件和unsupported动作仍拒绝或留unsupported audit。native EVT/ATTR/TEL 到测试设备ACK另见本轮证据。旧配置仍保留原行为，自动化整体并未完成。
- Linux/arm64 c-shared已编译并实际加载。首个init失败根因是root:root 0600令牌在Broker降权后不可读，QA改为root:mosquitto 0640。保持secret不进入日志；生产需按实际Broker UID配置。
- Linux矩阵发现pending认证连接断开无插件回调：固定上游context.c delayed_auth分支直接return。立即取消不支持，不能沿用早先host PoC的笼统PASS；采用最长2秒请求截止和并发上限，QA继续实测超时回收/迟到结果隔离。禁止在Tick解引用可能已释放的client指针。root补了uintptr复用时旧pending索引清除，回归进行中。
- `shared-subscription-prefix-control.md` 无插件对照已证实新前缀原因；复用结论已正式提交远程Candidate Memory，返回stored，memory_id=`564858cc-ef07-47f3-bc18-bda6c0fc0207`，evidence_id=`lev-f8c236f09b3a7f12e4f441eb`。这不是生产认证记忆。
- `/tmp/tp-mqtt-20261019/candidate-file-manifest.json` 是隔离候选源码清单；未stage/commit/reset当前混合工作区。最终需移除无关notification启动钩子与VERSION_NUMBER修改后在干净候选重建。

## 2026-10-20 00:30 增量检查点（第3/6步，非完工）

前面的清单保留为当时记录；当前精确状态以本节和逐AC台账为准。

- native Mosquitto 2.1.2 / owner / Redis / Paho / PostgreSQL ingress E2E 新一轮PASS 4.68秒；durable command E2E PASS 4.63秒。真实命令ACK后设备禁用、改号、TTL经过仍能由有权原调用者同key取得同receipt，冻结用户拒绝；无第二次MQTT发布。短legacy命令ID保留tenant-scoped回退。
- SQL007：连接观察和状态投影待办同事务；worker投影device/status history并清待办。真实PG验证迟到offline、多连接、重试、heartbeat owner跳过、历史不重复；未来10秒observed_at和超过60秒received_at均unknown且不误写离线。
- SQL008：可信、永久业务错误可进入7天隔离表，tenant1000条/64MiB，重复增加occurrences；只有隔离事务成功才确认第二跳，未给业务接受或设备执行成功。DB故障仍重试/noACK。实际native wrong-device ACK隔离通过。
- 命令帧采用两阶段检查：离线READ可将合法service存储帧排队，真正网络MESSAGE_OUT始终要求当前lease、目标tenant/device、epoch、mapping、expiry、ACL；保留存储帧，仅剥离网络副本。Linux MQTT3/5 离线重连及受控持久化重启TTL/epoch/异principal/撤订阅权限拒绝均有原始证据。非硬断电测试。
- SQL009：触发effect与命令动作的规划事务已实现，保留完整策略内容快照与hash、每动作audit、明确机器caller、稳定幂等键；命令复用既有durable dispatcher。人工/HTTP operationType=2不能借用任意用户绕过机器proof。独立真实PG race验证同源EVT/ATTR条件、并发幂等、source fence/TTL、策略变更、跨tenant拒绝、unsupported审计、故障全回滚与五次失败上限。TEL仅比较器可用，不代表遥测可靠入口或完整自动化兼容。
- 新增已通过的PG子项：SQL010 bound telemetry activity freshness/timeout projection；SQL011 safe voucher function、SQL012 route-change schema相关PG checks；automation action limiter每设备burst=10、1 token/3秒、失败事务不消耗额度。具体AC仍PARTIAL，完整stack压力、迁移barrier、72h及AI评估尚未通过。
- 当前自动化首切片只执行同源EVT/ATTR条件中的MQTT command动作。跨设备条件、告警/服务/遥测下发/属性下发等尚未接可靠动作handler，记录unsupported；不是完整规则验收。真实event到实际MQTT command/ACK全链由Luna继续验证。
- 内部Grant增加可选binding_phase（active/connecting，省略沿用active），准备阶段可连接与暂存可信上行，业务ledger不执行、不fence该代次，command READ/MESSAGE_OUT拒绝。draining不给grant。目标实际接入与激活支持正在联测；这本身不是迁移barrier，也未切任何真实设备。
- Linux/arm64 broker+Cshim+Go c-shared ASan/UBSan smoke实跑通过，leak检测关闭，压力还未完成。MaxPending真实owner HTTP barrier确认同时held2、峰值2，仅2个CONNECT成功；保留此前失败与修复关系。
- 生产候选镜像实际build+认证smoke通过，image ID `sha256:50ad4e10be39b9c38db3b76b61275e27000e11f7bfde89919c2f8513ee6f3928`，只对应当时context `02afc01ee71eb33e41b654258c1cb727c7f178101afc11673700aa16a3c880b8`，后续源码改动尚未重建，不能冒用。UID1883/read-only root、插件加载、owner readiness、正常设备PUBACK与错误凭证拒绝已实测。没有SBOM产物，未声明完整制品认证。
- 默认只读迁移检查器及001–009 QA fresh/repeat/缺列拒绝验证通过；只允许显式QA模式apply。真实生产按受控SQL部署单执行，工具不自动改生产。

继续欠项：普通遥测内存/批量异步链的最终route fence与规则副作用、完整legacy协议/OTA/retained/custom command兼容、source到target迁移barrier和回退、配对完整栈容量/故障/72h、原生物模型接线和首个AI切片。所有Gate仍未放行，1883/9999未切换。

### 2026-10-20 — 路由运行节点隔离与内部重投窗口

- 干净候选检查点已提交为 `18ee34b6`（分支 `codex/gmqtt-community-20261019`）；14包真实PG/native race通过。该提交不含随后SQL012/runtime/ingestion_id变更，也不是生产放行。
- SQL012及route owner CAS支持freeze、generation-bound barrier、target connecting/epoch+1、实际connection+trusted ingress后activate；failed-connecting回退重新冻结并再次递增epoch，前一变更保留superseded审计。只读route CLI与真实PG状态机/rollback/deadlock测试已增加。
- 实际Broker控制RPC+Adapter/effects/dispatcher已自动注册与确认。真实原生E2E四角色自动ACK后，额外PUBLISH被Mosquitto明确拒绝且SQL无receipt。控制失联10秒后关闭权限，HTTP旧结果不能因主线程延迟安装续成新租约。尚不代表完整多cell或legacy退役通过。
- 修复无source_event_id消息commit之后第二跳ACK之前的内部重投：可信封装新增boot+seq ingestion_id，Adapter/最终遥测批量边界据其固定receipt UUID。相同封装重投幂等，新的相同设备采样独立；设备source_event_id仍跨传输去重。相关PG/unit/native回归通过，真实持久会话断连重投专项仍在补。
- 审计已证明不可逆退役的旧source cell/epoch，可持久quarantine再ACK，不会永久阻塞旧retained/offline重放；无history/未知inactive/未来epoch/依赖故障仍backpressure。真实PG验证覆盖拒绝和事务失败。
- 独立QA冻结的pre-runtime Linux ASan/UBSan轮：100,000条broker_ack/ingress_seen、10,000次重连全部对齐，约11分30秒，无sanitizer诊断（leak检测关闭）。不覆盖后来runtime/ingestion_id源码，也未观察business accepted/terminal/physical effects；完整矩阵仍INCOMPLETE，pre-CONNACK立即取消保持UNSUPPORTED。
- 完整新链首个5秒健康profile：100连接/1逻辑device，500条planned/sent/ack/ingress/accepted/SQL/effect intent均对齐；先前475/500是runner未等待最后批量flush，失败记录保留，增加有界SQL drain后复跑通过。后续另建100逻辑设备profile，不混算容量。

仍在第3步收口并启动第4步隔离健康检查。G0真实黄金样本和规模、旧/新完整栈配对收益、全故障/72h、完整legacy灰度与回退、AI场景/留出评测未完成。没有生产切流，没有最终完工报告或生产证书。

远程Candidate Memory补充收据：`stored`，memory_id `e39ac3d2-a3e5-4da0-b8ea-a1900ea79611`，evidence_id `lev-690b2afdcdd7f2fbb3ec2119`。内容为平台ingestion_id与设备source_event_id的分工、commit/ACK重投窗口，以及主线程安装fence后才确认的边界；未把尚未完成的Gate记为通过。
