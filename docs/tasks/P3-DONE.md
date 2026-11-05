# P3 完成记录：Broker 接收后设备回执超时

## 交付目标

在 durable command 路径里，Broker 接收发布但设备始终不回执时，命令不再无限保持待处理或显示成功；超时后通过 API 与设备命令列表显示失败，并消费之后到达的迟到回执。正常设备回执行为保持不变。

## 修改与原因

- 在 `mqtt.command_ack_timeout` 增加可配置的设备回执时限，默认 30 秒；现有 durable 命令 TTL 也是 30 秒，因此默认值避免设备长时间显示未决。环境变量可用 `GOTP_MQTT_COMMAND_ACK_TIMEOUT` 覆盖。
- worker 原有 stale sweep 现在将超过设备回执时限的 `broker_acknowledged` 请求，在一个 PG 事务内标记为 `failed` 并同步 `command_set_logs`。日志状态写既有数字值 `2`，错误栏写明“device acknowledgement timed out; no device response received”。同一事务先锁住仍待回执的请求，超时与 ACK 竞争时仅一个终态更新能成功；重复 sweep 不会重复修改。
- worker 每轮先执行到期 sweep，再尝试 route `runtimeSync`。若同步失败，会保留现有 1 秒退避并暂停派发，但仍能继续下一轮 sweep；超时失败不再依赖路由同步成功，也不会因同步错误忙循环。
- ACK 处理遇到状态竞争错误时重新读取 durable 状态；如果 sweep 已先提交超时，就将迟到回执作为终态空操作消费，避免可靠入口无限重试，也不覆盖失败结果。若正常 ACK 先提交，原成功状态不变。
- 前端命令列表按 `command_set_logs.status=2` 显示通用“发送失败”，并单独显示 `error_message`。因此用户能看到未收到设备响应的原因，但状态标签不是专门的“设备回执超时”；本任务未改动前端。

## 工作分支与提交

- 分支：`wp/p3-device-ack-timeout`
- 已 rebase 到当前 `develop` 的 `ec0f51c9`。
- 当前 `git log --oneline develop..HEAD`：

  ```text
  f163117b fix(commands): sweep ack timeouts during route sync failures
  2db6faf9 docs: record P3 acknowledgement timeout delivery
  a3049d8c fix(commands): fail broker-accepted commands after ack timeout
  ```

## 验证

- 真实 PostgreSQL，受控发布回调：`TestPostgresExpiryFencingAndStaleDispatchNeverRequeues` 通过。测试经过实际 `Store.Dispatch` 路径；受控 Broker 回调表示发布已接受但不产生设备回执，随后用短测试时限 sweep，检查 durable 状态为 `failed`、列表投影为状态 `2` 且错误明确、第二次 sweep 无重复修改。
- 真实 PostgreSQL，正常设备 ACK：`TestReliableDurableCommandAckAssociatesLogicalChildAndRootTransport` 通过，日志和 durable 状态正常写入。
- 真实 PostgreSQL，迟到 ACK：`TestLateAckAfterCommandTimeoutIsConsumedWithoutChangingFailure` 通过，delivery 被消费且超时状态/日志不变。
- worker sweep 配置测试：`TestBrokerCommandWorkerUsesConfiguredDeviceAcknowledgementTimeout` 通过；验证默认 30 秒和 10 秒配置值会传入 worker 实际调用的 sweep 方法。该测试使用 SQLite 作为轻量单元夹具，状态转移本身另由上面的真实 PG 测试验证。
- runtimeSync 失败回归：`TestBrokerCommandWorkerSweepsTimeoutWhileRuntimeSyncFails` 通过；启动 worker loop 并让同步持续失败，确认 worker 仍执行 sweep、将已超时命令置为失败，随后可正常取消退出。该测试使用 SQLite；真实 PG 状态转换由 `TestPostgresExpiryFencingAndStaleDispatchNeverRequeues` 覆盖。
- 接续复核日志：`/Volumes/KIOXIA/archive/evidence/2026-11-05-P3-resume/p3-targeted-tests.log`；记录以上 app、真实 PG durable command、正常 ACK 和迟到 ACK 四项定向复测。
- `git diff --check` 通过。

## 未验证范围

- 未在运行测试中等待完整的一分钟 sweep 周期；同步持续失败时 worker loop 的 timeout sweep 调用已通过回归验证。
- 未连接真实 Broker；发布已接受但无设备响应由真实 PG 测试中的受控 publish 回调模拟。真实 Broker 的权限范围/静默丢弃场景尚未做端到端验收。
- worker 启动时会同步执行首次 `runtimeSync`；若该调用失败，`startBrokerCommandWorker` 仍返回错误，worker loop 和 timeout sweep 不会启动。此次只修复运行中的 loop 在后续同步失败时仍执行 sweep，启动恢复策略未改。
- UI 展示只核对了现有组件读取状态和 `error_message` 的方式；未做浏览器运行验收。当前列表显示“发送失败”加具体超时错误，不是专用超时标签。
- P3 已完成的验收只覆盖 durable command 路径；不代表 legacy command 路径或整栈 HA 命令验收完成。

## 现场

- 未推送、未合并。
- 为真实 PG 验收新建了 `p3-device-ack-pg-20261105` 容器；按现场规则保留，未删除。移除容器前需列清单并确认。
