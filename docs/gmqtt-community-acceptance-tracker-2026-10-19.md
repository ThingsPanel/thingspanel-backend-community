# GMQTT → Mosquitto 社区一期验收跟踪

更新：2026-10-20。范围仅社区 Mosquitto；企业 VerneMQ/EMQX 不开发、不测试、不计一期欠项。本文随实施更新，初始只读盘点结论不再作为当前运行事实。源码、单测、原生协议测试、完整栈验证和生产准入分别判定。

## 当前结论

工程正在实施，**尚未完工，不得生产切流**。固定 Mosquitto 2.1.2 的 native/C-shared/owner/社区 Adapter/私有 PostgreSQL 验证链已有更多真实测试；各子链 PASS 不代表 G0–G5 或单项完整 AC 全部通过。

| Gate / 工作包 | 当前实际进度 | 尚未放行的主要原因 |
| --- | --- | --- |
| G0-C / W01–02 | 已记录社区运行 GMQTT/后端镜像、挂载和端口；固定社区 HEAD、Mosquitto commit、合同与内部前缀修订 | 生产黄金样本、最终 C/R/S/QoS/L/F/H/V 冻结、完整旧/新栈同条件配对基线未完成 |
| G1-C / W03 | Native macOS 与 Linux/arm64 c-shared 构建/加载、身份封装、ACL、即时Will、platform-retained、shared 前缀已有分项证据 | Linux/arm64协议与离线持久会话/受控Broker重启命令帧矩阵有PASS证据；pending认证断开仍只有有界超时，立即取消不支持；延迟Will与Will重启恢复未认证；MaxPending重叠压力和sanitizer门槛未完成 |
| G2-C / W06–10 | Owner、不可变mapper快照、手动第二跳ACK、持久收据/业务事务/intent、命令幂等/冻结编码/最终fence、生命周期与SQL013持久命令计划代码已实现；automation链、状态投影、quarantine、SIGKILL redelivery、Linux离线/重启及计划命令的单次原生Mosquitto子项有证据 | 计划命令原生E2E使用Encore HTTP stub和Paho测试设备，证明一条协议级投递/设备ACK路径，不证明物理执行或live Encore；feature默认关闭，完整故障/幂等矩阵及旧下行unsupported边界仍待验 |
| G3-C / W11–13 | Luna独立QA已有raw NDJSON与分阶段判定器；建立独立PG、原生Broker、Linux构建及干净候选工作树 | 全量适用AC、配对性能/容量、故障命中与对账、72小时长稳、安装接班未完成；无生产证书 |
| G4-C / W17 | 旧运行1883/9999未切换，隔离实验端口/卷独立；SQL012 runtime单cell freeze与一条binding的两个native cell A/1→B/2→A/3均有真实集成子项；新增plan-based `tp-broker-cutover`可调用freeze/prepare/activate/rebase | Luna独立两轮native往返通过，但未完成50 binding、多家族/命令/旧系统退役和生产式灰度。CLI只有freeze持久化actor与plan hash，其他阶段缺完整操作审计；membership retirement与全量阶段故障验收待完成 |
| G5-C / W14–16 | 只读核验现有Encore原生模型API/快照/授权/receipt；command-plan service集成和一条原生Mosquitto/Paho测试设备协议路径均有分项证据 | 原生计划测试使用Encore `httptest` stub，不代表live Encore或物理效果；AI产品切片、盲测/人效/成本/留出集尚未完成，也未进行模型付费调用 |

逐 AC 当前判定见[社区 AC 逐项执行台账](gmqtt-community-ac-ledger-2026-10-20.md)。只有完整覆盖单项计划断言才会标 PASS。

## 当前候选与证据

- 本轮报告使用 source checkpoint `ad2cbd7f` 加后续未提交源码快照；它尚不是新的可复现commit。root尚未提供新的冻结commit，候选最终18项回归仍在运行，结果暂不纳入。原工作树含其他任务dirty文件，候选同步需按文件清单摘取。
- 独立候选：`/Users/junhong/.codex/worktrees/gmqtt-community/thingspanel-backend-community`，分支 `codex/gmqtt-community-20261019`。仅复制明确社区实现与通用生命周期依赖，排除notification启动钩子、通知源码与产品版本号变更。第一版干净候选已成功 `go build ./`，全仓race编译检查通过；该检查使用过滤器未运行测试，不能称全仓用例PASS。仍有后续实现待同步，未提交完成候选。
- Mosquitto：`v2.1.2` / `99fa50f30e325609394c324c8ff71cfbbe95d8ab`，固定上游源构建；宿主机旧2.0.18不作版本认证。
- `docs/gmqtt-evidence-2026-10-19/`：分项真实协议、shared无插件对照、GMQTT基线、原始账本及SHA证据。`docs/gmqtt-evidence-2026-10-20/sbom/`另有冻结本地镜像的静态Syft/SPDX清单与provenance；它不是漏洞扫描或生产镜像准入。最新精确结果以带run/制品hash的具体报告为准，不把旧PoC PASS套用到变化后的源码。
- `docs/gmqtt-community-execution-log-2026-10-19.md`：实现与缺口持续记录；目前不是完工报告。

## 2026-10-20 状态更新

本轮新增差异与尚未认证项见[当前验收增量清单](gmqtt-community-acceptance-delta-2026-10-20.md)。局部原生测试不提升整项AC状态：计划命令只有一条stub+Paho测试设备的Mosquitto协议ACK路径，SIGKILL只覆盖特定进程重启与ingestion_id重投；不代表物理动作、所有故障窗口或完整AC。

- **已通过的子项（不等于完整 AC PASS）：**状态activity/freshness/timeout和状态投影有真实PostgreSQL测试；SQL011 safe-voucher、SQL012 route-change相关PG/迁移检查通过；automation limiter每设备burst=10、每3秒补1、设备隔离和失败回滚通过PG测试。EVT/ATTR/TEL原生ingress到durable automation command及Paho测试设备ACK有E2E子项；Linux离线/重启矩阵有通过证据。另有[计划命令原生Mosquitto E2E](gmqtt-evidence-2026-10-19/planned-command-native-mosquitto-e2e-2026-10-20.md)、[SIGKILL redelivery](gmqtt-evidence-2026-10-20/sigkill/)与[静态SBOM](gmqtt-evidence-2026-10-20/sbom/provenance.json)子项。它们分别只证明计划命令stub+Paho测试客户端ACK、特定Mosquitto进程重启后的ingestion_id去重、冻结本地镜像的静态清单。详细旧证据见[状态边界](gmqtt-evidence-2026-10-19/broker-state-time-boundary-regression.md)、[quarantine](gmqtt-evidence-2026-10-19/broker-quarantine-regression.md)、[Owner/ledger/automation E2E](gmqtt-evidence-2026-10-19/native-mosquitto-owner-ledger-e2e.md)、[Linux命令帧QA](gmqtt-evidence-2026-10-19/linux-command-frame-qa.md)。
- **计划与切换能力修正：**SQL009 automation snapshot/window与EVT/ATTR/TEL durable intent有PG/native子项。SQL013计划API为 `POST /api/v1/command/datas/plans/preview`、`/:plan_id/authorize`、`/:plan_id/submit`，要求真人会话/服务端授权且 `mqtt.command_plans_enabled` 默认关闭；service集成和单个原生Mosquitto/Paho设备ACK子项现均有证据，但Encore端是stub，不能据此称live Encore或物理执行。SQL012 runtime Freeze自动ACK有单cell和单binding native双cell往返子项；`tp-broker-route`只读，`tp-broker-cutover`可应用受审plan。只有Freeze持久化actor/raw hash；其他阶段操作审计、membership retirement与两cell完整迁移仍未通过。AC-A01–A06仍未通过。
- **仍未过的关键门槛：**G0完整baseline、完整栈容量/故障与72小时长稳、完整两cell灰度/epoch升级/回滚、AI W14–16盲测/人效/成本及正式试点评估均未通过。整体仍PARTIAL，不可据此切流。
- **迁移前置检查：**QA迁移检查器按当前全部编号迁移动态发现并校验必需列；brokerqa 私有 schema 的 fresh apply、repeat apply、列检查均通过。该脚本不是生产自动迁移工具。

## 明确的能力边界

1. 内部规范前缀改为受ACL保护的 `tp-internal/ingress/v1` / `tp-internal/state/v1`。固定Mosquitto2.1.2对`$share/group/$tp/...`的无插件对照出现SUBACK成功但无数据；不用私有Broker fork，设备外部主题和字节不变。
2. `legacy`仍为默认配置；`ingress_v1`显式启用且要求SQL schema齐全，不能新旧原始入口双消费。
3. MQTT3 PUBACK只记`broker_acknowledged`，不等同Broker规则放行、设备收到或物理执行。持久命令的不确定发送/到期状态可终结为`unknown`且不自动重发；未知不是“已失败/未执行”的证明。
4. 命令私有存储帧在每次READ/MESSAGE_OUT验证目标/epoch/mapping/expiry后才剥离。当前只认证候选canonical command主题；MQTT3/5离线队列、expiry及受控Broker重启矩阵已通过，仍不等于OS崩溃/断电、设备物理效果或完整命令AC通过。
5. 新profile下受支持的durable automation path需要可信policy snapshot/proof。命令计划API另要求真人授权；有一个真实Mosquitto/Paho测试设备协议ACK子项，但Encore语义端是stub，且协议ACK不证明物理动作。custom topic mapping与service/protocol/raw legacy downlink在ingress_v1明确unsupported/拒绝；QoS2、wire-retained、延迟/恢复Will没有完整认证，不能假设不在用或算PASS。
6. 关键事件/属性业务行与automation intent同事务落库；automation policy/execution-window及事件驱动MQTT command有分项PG/native ACK证据，unsupported action只记录审计。计划API另有一次原生MQTT/Paho子测试，但不等于完整规则覆盖或物理设备效果。
7. 普通Telemetry evidence API只返回processed trusted receipt，当前reported时间不可用（`reported_at:null`），不会把broker observed time标为设备采样时间；units缺失时`unit_known=false`。见[遥测证据API](gmqtt-evidence-2026-10-19/community-telemetry-evidence-api.md)。
8. Native pending认证断开无插件回调，使用最长2秒截止而不解引用悬空client指针。Linux曾发现ready但未完成认证回调的pending计数缺口并已补修；当前最大并发断言仍需barrier-controlled overlap验证。

### 一期适用 AC 范围

计划定义 73 个跨产品验收 ID，其中 **AC-V01–V04、AC-E01–E07 共 11 项企业专项留二期；其余 62 个 ID 按社区 Mosquitto profile 执行适用断言**。三 provider/集群/跨 provider 子场景不执行、不阻断一期；在用 retained、OTA、网关等功能仍须覆盖。

- **AC-Q01–Q03：** 社区源码/运行基线与 owner 清单、判定器自检、干净构建及制品/配置认证。
- **AC-C01–C20：** 认证身份与 ACL、原字节及单入口 mapper、连接状态/撤权、retain 三种 binding、命令 ACK 发布者关联、在用旧协议/OTA、会话生命周期、跨租户隔离及管理能力。按实际在用能力确定样本；没有样本或安全环境时填 BLOCKED，只有 G0 证明不适用且关闭入口后才能填 N/A。
- **AC-M01–M04：** Mosquitto 原生插件/ABI 与 sanitizer，异步鉴权生命周期，回调/内部发布/retain/fence 顺序，镜像内插件和持久化/卷故障。此组全部为一期关键专项。
- **AC-R01–R12：** 接受边界与两跳 ACK、队列/DB 故障、命令幂等与 Unknown、过期/背压、进程/硬重启/存储故障及适用恢复。集群专属断言若社区 profile 未声明集群则由 G0 明确排除；单节点/卷的承诺仍必须实测。
- **AC-P01–P06：** 同条件完整栈基线、容量、安全窗口、72 小时长稳、重连、租户噪声；AI 隔离和 AI 成本在 G5-C 对应切片验收。
- **AC-D01–D06：** 社区 binding 切换屏障、epoch、迟到事件、Unknown 命令对账及 G4 灰度/回退。企业 provider 场景不执行；社区实际迁移路径仍需验收。
- **AC-O01–O05：** 独立安装、备份恢复、告警/runbook、凭据轮换、制品完整性和接班。社区发布交付效率按 G0 冻结的目标验收。
- **AC-A01–A06：** 只属于社区 AI 切片及 G5-C；不得作为 Broker 契约通过的替代证据。

## 验收状态规则

每个适用用例独立记录PASS / FAIL / BLOCKED / INCONCLUSIVE / N/A。发生器饱和、故障未命中或没有真实并发重叠证据时不能推断产品上限；未测能力不得发证。原始账本分别记录planned、sent、broker_ack、ingress_seen、accepted、terminal、sideeffects；缺后续阶段保持INCOMPLETE。仅经G0确认不适用且关闭入口的子场景可记N/A。

## 组织与后续顺序

总6步：1盘点；2合同/原生PoC；3可靠链集成；4独立完整质量/故障/性能验收；5灰度与回滚接管；6技术总监、产品经理、老板三份Markdown完工汇报及记忆收据。当前第4步；并行补隔离迁移子项，尚未进入生产灰度。主代理负责复杂设计和集成，三个Luna负责模块实现、独立QA和测试；不把未达到的后续Gate写成完成。

- **两cell边界更新：**[native往返证据](gmqtt-evidence-2026-10-20/two-cell-native/README.md)在两个真实Mosquitto与两个独立后端进程中验证四角色真实ACK、同wire ClientID的新连接、新代pending receipt与激活后唯一业务行/effect intent。精确源SHA的Luna两轮均PASS、0skip；root更早版本首轮亦PASS但没有后续断言加强。ready hash是fixture声明，不是目标配置认证；未做Broker重启、磁盘故障、命令在途和legacy退役。AC-D01/D05改PARTIAL，当前社区24 PARTIAL、38 PENDING、0完整PASS；Gate仍未放行。
