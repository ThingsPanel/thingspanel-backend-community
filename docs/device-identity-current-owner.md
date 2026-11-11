# 设备永久身份与租户最新遥测

## 身份契约

设备 ID 是全局永久身份。新自定义 ID 仍沿用原有 8–36 字符校验；第一次创建成功后，该 ID 永久保留，删除设备也不释放。再次使用已删除 ID 会返回明确的“permanently reserved; choose a new ID”拒绝，不会创建新的同名设备，也不会覆盖其他租户的保留记录。设备名称、设备编号和其他可变业务字段不承担身份保留职责。

`device_id_reservations` 由现有设备 DAL 管理，主键为 ID，保存不可由业务更新的租户和 `active` / `tombstoned` 状态。标识列使用 TEXT，以免新增账本比现存设备模型更窄；这不放宽 HTTP 新建参数校验。

新建和批量新建在设备、默认分组写入的同一事务中先插入保留记录，要求每个 ID 恰好新增一行。批量请求按 ID 排序争用保留记录。删除沿用租户设备行锁和依赖屏障，在同一事务内将对应 `active` 保留记录 CAS 为 `tombstoned` 并删除设备及相关数据。缺失或归属不一致的保留记录导致事务失败，不会在运行时补造记录。

Commit/Rollback 回包丢失仍属于结果未知。调用方应核对租户授权范围内的设备和保留记录，不应删除保留记录后盲重试。缓存失效和插件通知只在确认业务事务提交后进行。

## 切换要求与证据边界

`sql/31.sql` 是独立 `device-identity` 组件迁移，不能修改已有迁移历史。迁移为当前仍存在的设备回填 `active` 记录，并在发现设备与既有保留记录冲突时失败。迁移不会复活墓碑。

迁移在同一事务先取得 devices 表的 SHARE ROW EXCLUSIVE 锁，等待在途设备写事务结束并阻止新设备写入，然后回填、校验和安装数据库 fence trigger；提交时这些变化一起生效。trigger 强制 INSERT 已有同租户 active reservation、DELETE 已有同租户 tombstone，且拒绝修改 ID 或 tenant。旧路径不能绕过该设备数据库协议。

切换前仍应停止或隔离旧实例并排空或 fence 在途外部副作用。数据库 trigger 不能阻止存活旧进程在忽略提交错误后仍调用插件，也不能隔离其外部 TSDB 写；完整跨进程撤销依赖安装／执行 owner 的代次屏障。不能把本数据库 fence 当成全栈进程隔离。回滚到旧写者前同样需要停写和明确评审，不能以删除保留表作为回滚方案。

切换前已经删除、且没有历史保留账本的 ID 无法从当前 `devices` 表重建。该迁移不声称追溯证明这些历史 ID，也没有为外部 TSDB 制造删除世代或跨库原子删除协议。

## 最新租户遥测

`grpc.tptodb_type` 为空或 `NONE` 时，汇总只从当前关系库 owner 读取。单条 SQL 在一个语句快照中联合设备、同租户 `active` 保留记录、同租户 current 行和可用的同租户配置／模板／物模型展示字段。排序为设备最新时间降序、设备 ID 升序，稳定取三个设备；设备内数据按时间降序、key 升序。

返回结构保留 `device_id`、`device_name`、`is_online`、`last_push_time` 和 `telemetry_data`，后者包含 `key`、合法标量 `value`、可选 `label` / `unit`。不匹配租户的展示元数据不会被读取。合法空结果是空数组；SQL 失败和身份 schema 未就绪不会伪装成空成功。空租户被拒绝。

TSDB、KINGBASE、POLARDB 和未知配置仍明确不可用。此端点不会通过无租户身份的外部 RPC 或旧多段查询回落读取。

## 原生自动化调度

应用生命周期在 native 模式注册一个周期工作者，使用同一冻结配置和部署 token 调用原生 owner 的 `/internal/automation/tick`。请求没有租户 header；原生 owner 执行恢复和调度。失败可观察，并在下一个周期重试；应用 context 或关闭会取消在途 HTTP 请求。legacy 模式不启动该工作者，native 模式不注册旧自动化 cron。

本次调度接线不把旧社区场景 ID 自动解释为原生 rule ID，也不替代旧内存 uplink 队列的持久重试协议。原生定时回归通过真实 OwnerTick、恢复／调度和 PostgreSQL 运行／触发／outbox 存储，命令端使用受控测试 owner，不代表真实设备执行验收。
