# 北向服务 recovery 验收（真实故障注入）

- 运行时间（UTC）：2026-10-21T09:24:15Z
- 结论：**通过**
- 断言：22 通过 / 0 失败 / 0 跳过

| # | 断言 | 结果 | 说明 |
|---|---|---|---|
| 1 | 500 后进入 retry_wait | 通过 | state=retry_wait |
| 2 | 失败码记录为 http_500 | 通过 | http_500 |
| 3 | 失败会被再次尝试（次数递增） | 通过 | 1 -> 2 |
| 4 | 每次尝试都有独立记录 | 通过 | nb_delivery_attempts 行数 |
| 5 | 429 后进入 retry_wait | 通过 | retry_wait |
| 6 | Retry-After 被记录 | 通过 | retry_after_ms=2000 |
| 7 | 超时后进入 unknown | 通过 | state=unknown |
| 8 | unknown 不会被自动重试 | 通过 | attempts=1 |
| 9 | unknown 保留插件给出的原因 | 通过 | target_result_unknown |
| 10 | 未确认风险时拒绝重放 | 通过 | HTTP 409 aborted |
| 11 | 确认风险后允许重放 | 通过 | HTTP 200 |
| 12 | 重放后真实送达 | 通过 | state=delivered |
| 13 | 重放不改变幂等键 | 通过 | nb1: 前缀保持 |
| 14 | 两个事件都被受理 | 通过 | accepted=2 |
| 15 | 成功的一条 delivered | 通过 | delivered |
| 16 | 失败的一条独立 retry_wait | 通过 | retry_wait |
| 17 | 401 后记录进入 dead | 通过 | state=dead |
| 18 | 连接被标记为 needs_repair | 通过 | needs_repair |
| 19 | 过期租约被自动回收并送达 | 通过 | 无需人工操作 |
| 20 | 超限事件被拒绝 | 通过 | [{"eventId": "capacc-over-001", "code": "capacity_exceeded", "detail": "tenant backlog reached 20000 records"}] |
| 21 | 拒绝后不留半写事件 | 通过 | nb_events 无该事件 |
| 22 | 拒绝计入独立拒绝账 | 通过 | nb_tenant_quota.rejected_total |

## 说明

- 容量场景使用独立租户 t-cap-acceptance，跑完即清理，不影响其它断言。

> 本报告由脚本生成。跳过的断言不等于通过；失败项保留原始输出以便复核。
