# 外部执行器样例（A29）

两个样例做同一件事：**外部引擎按事先登记的合同，让平台执行一条已发布规则，或者对一条已发布策略下一个判定。**

样例故意做得很小。这里要证明的不是"Node-RED 能编排"，而是**一个不认识 ThingsPanel 代码的进程，能不能只靠 HTTP 说对话**——说得对就执行一次，说得不对就拿到一个能照着改的拒绝。

```
samples/
  node-red/   flows.json（可导入 Node-RED）+ 生成脚本 + 说明
  flink/      最小 DataStream 作业（Java）+ pom + 说明
```

## 合同长什么样

登记一次，之后每条消息都带同样的四样东西。

| 字段 | 作用 | 搞错的后果 |
| --- | --- | --- |
| `epoch` | 登记代际。重新登记 = 代际 +1 | 旧部署继续发消息 → `superseded_epoch` |
| `seq` | 代际内单调递增 | 重放或乱序 → `replayed_sequence`，不会执行第二次 |
| `contract_hash` | 这条消息是按哪份合同生成的 | 换过合同还在发 → `stale_contract` |
| `idempotency_key` | 调用方自己的稳定键 | 换了键的重试 = 一次新的物理动作 |

`issued_at` / `expires_at` 必填，窗口不能超过 1 小时。过期的消息不再受理——一条迟到的"开门"比没有更危险。

## 两种意图

**动作意图（intent）**只点名"执行哪条已发布规则的哪一版"，**不带步骤**：

```json
{
  "epoch": 1, "seq": 7, "contract_hash": "6b8ec7d8eb5312c1",
  "issued_at": "2026-10-26T14:00:00Z", "expires_at": "2026-10-26T14:01:00Z",
  "idempotency_key": "nr-intent-<executor>-7",
  "rule_id": "<rule>", "revision": 3, "device_id": "<device>"
}
```

允许外部引擎自带步骤，就会出现第二条执行路径，两条路径同时拥有同一台设备——`single`/`restart`/`queued`/`parallel` 这些运行模式也就各管一半。所以这里只给一个规则 id，剩下的交给平台既有的运行、命令、告警 owner。

**告警判定（verdict）**必须点名策略版本：

```json
{
  "epoch": 1, "seq": 8, "contract_hash": "6b8ec7d8eb5312c1",
  "issued_at": "...", "expires_at": "...", "idempotency_key": "nr-verdict-<executor>-8",
  "policy_id": "<policy>", "policy_revision": 2,
  "mode": "raw", "observed": true,
  "subject_id": "<device>", "action_kind": "qualified"
}
```

`mode` 有两种，**不能混**：

- `raw` = "现在为真"。持续时长由平台按已发布的 `for` 自己算。带 `since` 会被拒（`mixed_verdict_semantics`）。
- `qualified` = "我算过持续时长了"。必须带 `since`，平台**重算**一遍；不够长就是 `hold_not_satisfied`，引擎说什么都不算数。

## 三种回答

HTTP 200 不等于做成了。回答在 body 里：

| 回答 | 意思 | 该做什么 |
| --- | --- | --- |
| `accepted: true` | 已受理 | 看 `outcome` / `status` / `run_id` |
| `accepted: true, replayed: true` | 同一个幂等键的重投，**没有再次执行** | 什么都不用做 |
| `accepted: false, reason_code: ...` | 被拒 | 按 `issues[].field` 改请求再发 |

`accepted: false` 里最常见的三个：

- `out_of_scope` — 设备不在登记的 `target_scope` 里。合同是白名单，空表示"什么都不允许"。
- `stale_policy_revision` / `stale_rule_revision` — 你按旧版本算的。重新读一次再发，不要原样重试。
- `replayed_sequence` — 序号没有前进。换新的 `idempotency_key` 并把 `seq` 加一，或者就此停下。

**被拒绝的请求不要原样重试。** 同样的 body 只会拿到同样的拒绝，而每次重试都在消耗调用方的耐心。

## 怎么验证

```bash
# 1. 起服务
cd encore-api && TP_AUTH_MODE=dev encore run --port 14000

# 2. 协议层：不依赖 Node-RED 或 Flink，纯 HTTP 走一遍（33 项断言）
python3 test/automation-rewrite/executor_smoke.py

# 3. 两个引擎样例
见 node-red/README.md 与 flink/README.md
```

`executor_smoke.py` 发的是和两个样例**同一组请求**。样例的价值在于"引擎里怎么写"，冒烟脚本的价值在于"线上协议确实是这样"——两者分开，才不会用一句"样例跑通了"代替协议验证。

## 这一版没做的

- **限流**：`max_rate_per_min` 进了合同、能被读出来，但还没有真正的限流器执行它。超频目前不会被拒。
- **凭据**：`credential_id` 只存指纹，凭据的签发与校验仍在既有鉴权体系里，没有"每个执行者一把钥匙"。
- **改合同**：合同登记后不能原地改，只能抬代际重新登记。这条路径有存储层实现和单测，但还没有对外端点。
- **页面**：登记、健康、消息流水只有接口，前端页面未做。
