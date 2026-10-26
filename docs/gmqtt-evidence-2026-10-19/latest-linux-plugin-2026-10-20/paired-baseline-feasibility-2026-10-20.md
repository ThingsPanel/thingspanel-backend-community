# GMQTT / Mosquitto 完整栈配对基线：可行性与 60 秒试跑方案

日期：2026-10-20。此文件记录只读环境盘点和建议，不代表已运行 old/new 完整栈基线。

## 结论

当前可以准备小规模配对试跑，但还不能开始：QA PostgreSQL 可达，且提供了 0600 DSN；没有发现专用 QA Redis，现存 Redis 属于 Copilot 服务，不能拿来当测试依赖。当前 Mac 的 Docker VM 为 8 vCPU、约 8 GiB，容器没有 CPU/RAM cgroup 上限，其他服务占用也在变化；因此现状不能直接作为稳定性能比较环境。旧 GMQTT 容器绑定生产网络并公开 1883，不能拿它本身做压测；应基于固定旧镜像在隔离网络另起实例，并让旧、新栈串行使用相同资源配额。

只读确认的旧镜像为 `registry.cn-hangzhou.aliyuncs.com/thingspanel/thingspanel-gmqtt:v1.1.7`，容器 image ID `sha256:c3ad43c34675210012ffd5559dfe3574af4a42ea6f65465f6d1b32cfe4119660`；容器启动参数引用 `/gmqttd/default_config.yml`，持久卷挂在 `/gmqttd`。现有实例连接 ThingsPanel 网络，环境变量包含数据库与 Redis 配置项；本报告未读取或复制其值。镜像 tag 和 image ID 可固定二进制，但还需要记录对应 GMQTT 源码 commit，不能把 tag 当源码身份。

QA PostgreSQL 为 `tp-mqtt-qa-pg-20261019`，端口仅映射到 `127.0.0.1:35432`，image ID `sha256:3b9219ac41bf4fafdce609eb637113ca7c798f9d8239f0b3b825d82d88423880`。只读 schema 枚举显示已有多个并行 QA schema；新试验必须创建专属 schema、限定 `search_path`，并且不能清理其他 agent 的 schema。DSN 文件权限为 0600。现存 `tp-copilot-redis` 是 `redis:7-alpine`，端口 `127.0.0.1:6380`，有自己的数据挂载；它不是此项目专用 QA Redis，不应连接或清理。

主机是 Apple M1、8 核、16 GiB；Docker daemon 报告 aarch64、8 核、约 8 GiB。当前 Docker 容器均未设置 CPU 与内存限制。现有工作区 HEAD 为 `0e3ba828`，而待测后端候选基线由项目负责人指定为 `18ee34b6`；正式开始前需冻结准确的 40 位 commit/源码快照并固定旧 GMQTT 源码身份，避免当前有并行修改的工作区漂移。

## 60 秒健康可行性 profile（不是验收 profile）

建议先串行运行每个栈，避免同一台 Mac 上争用 CPU、磁盘和数据库。每轮使用同一套 QA tenant/device fixture、消息 ID 序列、后端代码和 schema 数据规模；每个栈固定 100 个持久会话，MQTT 3.1.1 QoS1 上行与 QoS1 Adapter 订阅，60 秒固定开放环速率 100 条/秒，payload 按 90% 512 B、9% 4 KiB、1% 64 KiB 分布。运行前 30 秒用于连接和预热，稳定计数窗口 60 秒，停止注入后最多 30 秒排空，再检查 SQL 计数。每次预计产生约 6,000 条样本，足以检查链路可运行和明显丢失，不满足计划中至少 10,000 样本的关键延迟验收要求。

建议先做 3 组配对健康轮并交替顺序（旧/新、新/旧、旧/新），每组各自重置或分配独立 schema、队列与设备命名空间。每次报告资源限制、CPU/RSS、连接成功数、计划/发送/ACK/业务阶段计数、SQL 唯一事件计数、每阶段延迟分布与发生器漏发数。此规模只验证端到端 harness 与隔离，没有资格证明 1.5×吞吐、P99 门槛或生产容量；这些仍按计划 §9 的正式合同执行。

## 必需的 harness 与阻塞项

1. 提供统一负载生成器：固定设备身份、稳定 `source_event_id`、单调时间戳和确定性 512 B/4 KiB/64 KiB payload；开放环计划发送并记录计划/实际发出时刻、MQTT PUBACK 和超时。旧、新栈可使用各自必要的 topic 适配，但业务输入必须语义相同。
2. 提供完整链路采集：真实设备认证 → Broker → MQTT Adapter → Uplink → Storage batch → SQL。将 `planned / sent / broker_ack / ingress_seen / accepted / terminal / sideeffects` 分列记录；Broker ACK 不得映射为 accepted。每条 accepted ID 必须能和数据库唯一 receipt/telemetry 行对应，并有业务终态计数；当前 ASan MQTT stress 只观察到 ACK 和 ingress，不含 SQL accepted/terminal/sideeffects。
3. 提供隔离 fixture 装配：QA PostgreSQL 独立 schema、专属 Redis 容器/volume、旧 GMQTT 独立 Docker network/loopback host port、新 Mosquitto 同样隔离；不读取生产服务凭据，不连接现存 GMQTT/Redis/公共后端。为旧、新实例导入等价租户、设备、凭据与映射。
4. 记录同语义配额：建议首轮仅设置统一 Docker daemon 资源规划，不在当前共享 VM 上直接声称正式硬件基线。正式比较按计划建议 Linux 8 vCPU/16 GiB 起步，压测发生器独立；若在本 Mac 做健康轮，至少给每个栈相同的 Broker、后端、PG、Redis配额并串行运行，写明 Docker VM 内存上限和宿主其他负载。
5. 固定身份与交付账本：old GMQTT image ID 已确认；需由 owner 给出 GMQTT 对应源码 commit/配置 hash，后端冻结至 `18ee34b6` 的完整 commit ID，Mosquitto 插件与 broker image ID、binary/plugin hashes、配置、seed 数据 manifest 也应一并固化。

Redis 隔离和业务端到端 harness 是启动 60 秒试跑前的硬依赖。请 root 决定由谁提供专用 QA Redis 与双栈负载/SQL 对账 runner；在这两项具备前不启动试跑。完成健康轮后仍需按计划 §9.4 至少 3 次配对正式轮、≥10,000 延迟样本及故障/容量测试，不能将健康轮写成正式性能验收。
