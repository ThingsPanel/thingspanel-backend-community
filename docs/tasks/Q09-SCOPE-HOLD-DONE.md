# Q09 触发范围与持续时间修复

基线 `9d42e119` 是已部署 Encore 的精确组合：`ece5c7ef` 加 `ce9097ab` 五个隔离文件、`47405bfd` 四个命令 owner 文件。该基线只为复现现场，不能用完整工作树替换主线或扩大部署范围。当前仅本地修复，未推送、未部署。

## 行为

- 有绑定历史的规则只接受当前有效成员；解绑保留历史，不再退回无范围的 self 匹配。显式 `source=device A` 仍能控制目标 B。
- 无绑定的 self 条件按已选目标集限定上下文。现有 DSL 没有独立 self 来源选择器；`targets=[A,B]` 不能表达“只有 A 触发 B”，该意图必须使用显式 source A。动作数量上限不截断来源成员判断。
- 无参数绑定同样记录 binding ID/source；绑定查询保留 tenant 范围。未触发的 pending 在规则停用、删除、解绑后不能产生新动作；已受理运行不撤回。
- 正常值取消未满足时长的异常等待，中间区间/再次异常取消未满足时长的恢复等待。连续正常值只刷新证据，不延后首次恢复 deadline。已有连续异常 deadline 保持逻辑不变。

## 验证

真实隔离 PostgreSQL 首先复现未选设备、其他模板、绑定非成员误触发与正常值未取消 pending；随后独立复现第二条正常值延后恢复期限。定向 24 顶层回归通过；最后删除规则 pending 边界及恢复/停用/解绑 4 顶层测试通过，0 跳过。外部命令与目录为测试替身，不能称线上设备验收。

证据索引：`F:/coding/artifacts/20261111-coordinated-rollout/Q09-scope-hold-review.json`，内含前后日志与测试矩阵。

## 限制与后续

- 原 A19 测试虽名为“解绑停止触发”，却明确期望解绑后继续运行；按本次用户要求修正该预期，属于行为变化，发布需审查。
- PostgreSQL 已实际验证。MySQL 候选查询保持对应分支，但本轮未运行 MySQL 集成。
- 通用 self 成员判断复用目标解析，会增加目录读取；部署前应评审高频/大量目标性能。
- 单个新入口事件偶发产生一个成功运行及一个 ignored 运行已单独留证。同步 Ingest 与 RecoverAfterRestart 间没有共同 claim 是待验证路径，本修复不包含并发去重改造，不以之后成功重试掩盖该异常。
- 不改 SQL 迁移、数据库所有权、通知、设备协议、发布脚本或另一路 owner 上报合同。

## 复审：查询失败与解绑竞争

- CandidateRules 在同一 SQL 读取规则与绑定历史标志 RequiresBinding；随后绑定被撤销时不得将该候选解释成未绑定规则。
- 绑定目录、模板存在性、有效绑定查询分别返回错误；接收事件保留未处理状态，恢复任务可重试。pending 的临时解析故障计入 deferred，保留原 StartedAt/Deadline；确认解绑、删除、停用仍丢弃 pending。
- 真实 PostgreSQL 故障注入在候选读取后实施解绑/一次查询失败。夹具仍执行真实 SQL、事务和 prepared statement；无故障对照确实产生运行。166320be 基线对照通过而四项缺陷断言失败，修复后五项通过。追加验证失败 receipt 恢复恰好一个运行，再恢复不重复。
- 相关绑定、计时、模拟、无数据和租户入口共 47 个顶层测试有通过证据，0 跳过。合并回归运行 46 通过、1 因同时启动同工作树 Encore 测试导致测试数据库连接失败；停止并发后该项及增强的 receipt 恢复测试串行通过。原始日志保留，未将基础设施失败改写成成功。
- 最初故障夹具缺 Prepare 透传，会使负向断言假绿；该版本日志明确作废，不作为红绿依据。有效证据为 Q09-binding-review-before-v2.jsonl、after-v3.jsonl、regression.jsonl、sequential-recheck.jsonl。
- 本次不解决 previewScope 的目录查询错误被吞掉问题：模板影响范围可能不完整却生成确认 token，须另行 fail-closed 审查。同步 Ingest / RecoverAfterRestart 的共同处理竞争同样另列。

## 第一阶段：事件运行身份幂等

同一真实 PostgreSQL receipt 的 live/recovery 由 SQL barrier 控制交错，原实现产生 2 个 succeeded run 和 2 次测试命令出口调用。补丁仅将即时 ingress 事件的 run ID 固定为 ingress-event / tenant / event_id / rule_id / device_id 的 SHA-256 身份；排除 revision，复用 AdmitRun 原有事务重放。原决策已完成时不会再进入执行；未完成则沿原 run 的检查点、租约和 outbox 身份继续。手动、日程和 pending timer 保持原身份。

安全共用的尾部增加 owned receipt processed 复查、因果跳数限制、候选评估、processed 标记；它不是 claim，不能把前置复查描述为排他锁。恢复未投影 state、重复 status 的前态判定等需要单独处理，当前不强行合并。

验证：可控 barrier 从 2 run / 2 command 变成 1 / 1；新 5 顶层测试（包含四种 mode 子例）、相关既有 14 项以及手动/日程 2 项全部通过，0 跳过。包含动作已完成但 processed 写入失败、规则 revision 改变后重入、部分多规则完成后恢复、独立 pending 窗口；均使用隔离 PostgreSQL，命令出口是测试替身，不能称线上设备验收。日志前缀 Q09-event-identity，原红日志 Q09-live-recovery-barrier-red.jsonl 保留。

本阶段明确限制：

- 不是 receipt 全面 exactly-once；没有新增 claim、lease 或迁移。状态投影、pending、告警转换与故障重试整体仍需独立并发/崩溃设计。
- 升级前已产生随机 run ID、但 receipt 尚未 processed 的历史记录，不会自动匹配新稳定 ID。发布前必须审查兼容映射或安全排空，不能直接重放这类历史记录。
- 混合旧/新 worker 不具备统一事件身份，不能据本地测试声称滚动并存安全。
- 范围匹配全量设备目录扫描成本尚未优化，scope 候选仍不可直接发布。

## 升级兼容补丁

后续补丁已处理上节的旧随机 ID 缺口：AdmitEventRun 在现有 admission 事务/作用域锁内，先匹配稳定 ID，再按 tenant/event/rule/device 精确查旧 trigger_event_id，重放旧 run 与 snapshot、targets、status、generation、outbox key。历史已有多条 run 时按 started_at/id 复用首条，不新增，也不取消或改写其他已受理动作。它不允许旧/新版本同时并存的写入者凭空获得一致身份，发布仍需控制 worker 切换。

真实 PG 手工构造旧随机 ID：completed 与 waiting 两子例都经过 processed 标记故障、revision 更新和恢复，原 ID/冻结决策不变；waiting 最终只执行原动作一次。另有两条历史重复 run 的用例，恢复没有再添第三条或改历史。034b06c8 精确基线加同一测试夹具时这些断言均失败；补丁后通过，事件并发与四种 mode/部分多规则回归合计 6 顶层通过。证据：Q09-upgrade-compatibility-before.jsonl、Q09-event-identity-upgrade-compatibility.jsonl。

未增加 SQL migration。旧事件查找可用现有 rule_id/started_at 索引，但没有完整复合事件索引；高历史量性能仍需上线前核对，不能称该兼容查询已完成负载验收。

## 有界设备目录依赖 delta

Gin 独立 owner 增量 c4197017 基于现场 9766c010，仅新增 /internal/automation/devices/lookup。该服务必须先部署，旧 Encore 不受影响。当前 Encore delta 的 ByIDs 仅使用此接口，每请求至多 200 个 ID，较大显式目标集分批；严格校验响应 tenant、ID、重复项、返回数量，旧服务 404 明确失败，绝不回退完整列表。

self 来源成员只取当前设备的权威字段，依照原选择器 union、标签分支 AND/OR、capability 后置交集判断；area 仍未支持，MaxTargets 仍只约束动作数量。模板/类型/标签来源判定不再展开所有目标。一次事件内多规则共享单源查询结果，下一事件重新读取；未新增跨请求缓存。原有 binding 缓存保留；缓存失效时与单源读取共用本次请求结果。

Encore 定向 15 顶层测试全部通过，0 跳过：端口真实 HTTP 404 无 fallback、批量/恶意响应/缺失 ID；成员语义矩阵；真实 PG 六个模板候选且目录含 3000 其他设备时每事件仅一次 exact lookup、零完整模板扫描；既有绑定故障/范围用例。证据 Q09-directory-encore-tests.jsonl。Gin 自身真实 PG 3 项通过记录另见 Q09-directory-gin-tests.jsonl。尚未上线，不称在线验收；实际动作目标展开不在本次重构范围。

独立 agent 发现同 Runtime 执行 lease token 复入会导致同 idempotency key 的两个重叠 Dispatch，正在另一个 worktree 修复；本目录 delta 不修改 executeRunAt，不将稳定 run 身份描述为完整动作 exactly-once。
