# 北向服务（数据转发）维护手册

给值班的人看：**看什么、什么算异常、出问题从哪儿查、哪些操作不要做。**

部署见 `DEPLOYMENT.md`，功能怎么用见 `USAGE.md`，接口语义见 `CONTRACT-v1.md`。

---

## 1. 每天要看的四个数字

打开「数据服务 → 数据转发 → 总览」，只看四个数字，再看一张表。

| 数字 | 正常 | 什么算异常 | 异常先查 |
|---|---|---|---|
| 等待投递 | 稳态下接近 0，随数据量小幅波动 | **持续上涨不回落** | 第 3.1 节 |
| 已送达 | 持续增长 | **停止增长但事件还在进** | 第 3.2 节 |
| 结果未知 | 偶发个位数 | **持续增长** | 第 3.3 节 |
| 因容量未登记 | 0 | **非 0** | 第 3.4 节 |

「连接器」表里每个插件的状态必须是「已就绪」。出现「不可达」说明插件进程挂了或
端口不通，此时所有投递都会失败。

> 「等待投递」和「因容量未登记」是两件事，**不能相加**。前者会继续发，后者永远
> 不会补发。把两者加在一起当「待发送」是最常见的误判。

---

## 2. 日志在哪

| 进程 | 日志位置 | 看什么关键字 |
|---|---|---|
| 社区后端 | `log.adapter_type` 决定（0=控制台 / 1=文件 / 2=两者） | `northbound relay`、`northbound: 登记失败`、`交接` |
| Encore 核心 | `encore run` 的 stdout | `northbound: delivery insert failed`、`pipeline version transform is unreadable` |
| HTTP 插件 | stdout | `publish_failed`、`401`、`timeout` |
| MQTT 插件 | stdout | `publish_failed`、`unknown payload type`、`connect` |

**已知的一个坑**：Encore 的 `rlog.Error` 只在真出错时打，正常路径不刷日志。如果你
看到 `northbound: delivery insert failed`，那一定是有真实 SQL 错误，不是噪声。

---

## 3. 故障定位

### 3.1 「等待投递」一直涨

这是「源端登记了，但交接不出去」。按顺序查：

```sql
-- ① 源端 Outbox 里的行停在哪个状态？
SELECT state, count(*), max(attempts) AS max_attempts,
       max(next_attempt_at) AS next_try
FROM northbound_source_outbox
GROUP BY state;
```

- 全是 `pending` 且 `attempts` 在涨、`next_try` 在未来 → relay 在重试，看
  `failure_code`：

```sql
SELECT failure_code, count(*) FROM northbound_source_outbox
WHERE state = 'pending' GROUP BY failure_code ORDER BY 2 DESC;
```

| `failure_code` | 含义 | 怎么办 |
|---|---|---|
| `northbound_unreachable` | 核心没起来、地址错、网络不通 | 查第 2 节的核心日志、确认端口与 `encore_base_url` |
| `handoff_status_401` | 服务令牌不一致 | 比对 `NB_SERVICE_TOKEN` 与 `northbound.service_token` |
| `handoff_status_503` | 核心在，但它的数据库或依赖不可用 | 查核心日志 |
| `handoff_status_<其它>` | 核心返回了非 200 | 看日志里那一条的响应体（relay 会把前 200 字节记进「最近错误」） |
| `handoff_bad_response` | 核心返回的不是预期的 JSON（通常是版本不匹配） | 确认后端与核心是配套版本 |
| `capacity_exceeded` | 北向侧积压满，**保留登记继续重试**（不是放弃） | 见 3.4 |
| `encode_failed` | 事件序列化失败，属于缺陷 | 抓一条样本报缺陷 |

- 状态是 `failed` 的行**不会再被自动重试**：要么是核心明确拒绝了这条事件（比如
  `invalid_argument`），要么是重试次数用尽。这类要人工判断后再处理，不会自愈。

- 全是 `leased` 且 `lease_until` 已经过去 → relay 进程死了。这是**正常可恢复**的：
  下一个 relay 周期会把过期租约收回重发，不需要人工干预。如果超过 5 分钟还没动，
  说明 relay 根本没在跑，检查社区后端的 `northbound.enabled`。

```sql
-- ② 交接到底成没成？看最近成功与最近失败
SELECT state, count(*), max(handed_off_at) AS last_ok, max(updated_at) AS last_touch
FROM northbound_source_outbox GROUP BY state;
```

### 3.2 「已送达」不涨，但事件还在进

```sql
-- 投递都堆在哪个状态？
SELECT state, count(*) FROM nb_deliveries GROUP BY state ORDER BY 2 DESC;
```

- 堆在 `pending` 且 `next_attempt_at` 一直往后 → worker 没在跑。查核心日志有没有
  `delivery insert failed` 或启动失败。
- 堆在 `leased` 且 `lease_until` 过期 → worker 崩了。租约到期会自动回收，不需要
  手工清理。
- 堆在 `dead` → 目标明确拒绝。看失败原因：

```sql
SELECT failure_code, count(*), max(failure_detail) AS sample
FROM nb_deliveries WHERE state = 'dead' GROUP BY failure_code ORDER BY 2 DESC;
```

| failure_code | 通常原因 |
|---|---|
| `http_401` / `http_403` / `authentication_failed` | 凭证过期或权限变了。连接会被自动标记 `needs_repair`，改完凭证后积压会继续发 |
| `http_404` | 目标地址写错 |
| `http_400` | 目标不接受我们的报文格式 |
| `plugin_rejected` | 插件侧的配置校验没过 |
| `attempts_exhausted` | 重试 12 次都没成功 |

### 3.3 「结果未知」持续增长

`unknown` 表示**请求发出去了，但没拿到确定答复**。目标可能已经写入，也可能没有。

**不要批量重放 unknown。** 每一条都要先到目标系统核实。批量重放的后果是目标侧
可能出现大量重复业务数据，而且平台无法替你判断这能不能接受。

```sql
-- 哪些 unknown 集中在同一个目标？集中说明是那个目标的问题
SELECT connection_id, count(*) FROM nb_deliveries
WHERE state = 'unknown' GROUP BY connection_id ORDER BY 2 DESC;
```

如果是某个目标整体超时（`failure_code` 集中在 `timeout`），先修那个目标的响应
时间，再处理已经变成 unknown 的记录。

### 3.4 「因容量未登记」不为 0

这些数据**没有进任何队列，不会补发**。非 0 就是真实的数据丢失。

```sql
-- 社区侧：谁被拒了、为什么（按分钟分桶，所以要看 sum）
SELECT tenant_id, reason, sum(count) AS n, max(last_at) AS last_at
FROM northbound_source_rejects GROUP BY 1, 2 ORDER BY n DESC;

-- 北向侧：受理上限被打满
SELECT tenant_id, pending_events, rejected_total, last_reject_code
FROM nb_tenant_quota WHERE rejected_total > 0;
```

两种原因，处理方式不同：

| `reason` | 含义 | 处理 |
|---|---|---|
| `capacity_exceeded` | 该租户积压已达 `max_backlog_events`，平台**主动选择不登记** | 先疏通（3.1/3.2），再考虑调大 `NB_MAX_BACKLOG_EVENTS` 与源端 `max_backlog_events` |
| `invalid_event` | 事件本身存不下：序列化失败，或单条超过 `max_event_bytes` | 这是**缺陷或配置不匹配**，必须查清楚是哪种。只有确认是「报文确实合法但偏大」且目标能接收，调大 `NB_MAX_EVENT_BYTES` 才是对的 |

调大上限是缓解，不是修复。如果积压是因为目标慢，调大上限只是把爆点往后推。

---

## 4. 数据保留与清理

| 数据 | 保留 | 清理条件 |
|---|---|---|
| 源端 Outbox | 7 天（`northbound.retention`） | 只清 `handed_off` / `failed`，**不动 `pending` / `leased`** |
| 北向事件 | 7 天（`NB_EVENT_RETENTION`） | 只清没有未完成投递的事件 |
| 投递记录 | 30 天（`NB_DELIVERY_RETENTION`） | 只清 `delivered` / `dead` |

`pending`、`retry_wait`、`unknown`、`leased` **永远不会被自动清理**。这是刻意的：
自动清掉未完成的东西等于悄悄丢数据。

> **注意**：保留期清理的定时触发本次**没有等到真实窗口验证**（7 天/30 天），只做了
> 代码审查。上线后第一周请手工核对下面的查询，确认清理真的在跑：

```sql
-- 应该有数据但不应无限增长
SELECT count(*), min(created_at) FROM nb_events;
SELECT count(*), min(created_at) FROM nb_deliveries WHERE state IN ('delivered','dead');
```

如果 `min(created_at)` 早于保留期很多天，说明清理没生效。

---

## 5. 容量与背压调参

只调这几个，其余不要动：

| 参数 | 位置 | 什么时候调 |
|---|---|---|
| `NB_MAX_BACKLOG_EVENTS` | Encore 环境变量 | 北向侧经常因容量拒绝，且已确认目标是健康的 |
| `northbound.max_backlog_events` | 社区后端配置 | 源端经常拒绝登记，且核心侧有明显余量 |
| `NB_WORKERS` | Encore 环境变量 | 积压堆积但每个投递都很快（目标不是瓶颈，是我们并发不够） |
| `NB_MAX_ATTEMPTS` | Encore 环境变量 | 目标偶发长时间不可用，12 次不够 |

调 `NB_WORKERS` 前先确认插件和目标扛得住。加大并发最容易的后果是把目标打挂。

---

## 6. 重放的纪律

重放是**人工决定**，平台不替你判断。规矩：

1. 只对 `dead` 和 `unknown` 重放。`delivered` 不允许。
2. 必须填原因，会进审计。
3. `unknown` 重放前先到目标核实。查不到再重放；查得到就不动。
4. 核实不了又要重放，勾「确认重复写入风险」——这一步是在书面接受重复写入。
5. **幂等键不变**。目标侧如果按幂等键去重，重放是安全的；不认幂等键的目标，
   重放就是真写第二次。

批量场景下先小批量试一条，确认目标行为符合预期再放量。

---

## 7. 备份

需要备份两处，缺一不可：

```bash
# ① Encore 库：配置 + 账本 + 凭证密文
pg_dump -Fc "$NORTHBOUND_DSN" > northbound-$(date +%F).dump

# ② 社区库里的源端 Outbox：已登记未交接的数据
pg_dump -Fc -t northbound_source_outbox -t northbound_source_rejects \
  "$COMMUNITY_DSN" > northbound-source-$(date +%F).dump
```

**凭证密文单独备份没有用**：`nb_connection_secrets.ciphertext` 是用 `NB_SECRET_KEY`
加密的，密钥不在库里。`NB_SECRET_KEY` 丢了，所有租户凭证都要重新录入。密钥请按
秘密管理流程单独保管，不要和数据库备份放在一起。

---

## 8. 巡检 SQL 清单

贴进 `psql` 就能跑，不用改。

```sql
-- ① 源端：有没有卡住的
SELECT state, count(*) AS n,
       count(*) FILTER (WHERE attempts > 5) AS retried_many
FROM northbound_source_outbox GROUP BY state;

-- ② 北向：投递分布
SELECT state, count(*) FROM nb_deliveries GROUP BY state ORDER BY 2 DESC;

-- ③ 北向：近一小时失败原因 TOP 5
SELECT failure_code, count(*) FROM nb_deliveries
WHERE state IN ('retry_wait','dead','unknown') AND updated_at > now() - interval '1 hour'
GROUP BY 1 ORDER BY 2 DESC LIMIT 5;

-- ④ 连接健康：有没有 needs_repair
SELECT id, name, state, failure_code, updated_at FROM nb_connections
WHERE state <> 'active';

-- ⑤ 任务积压：哪些任务在堆
SELECT p.name, p.state, q.pending, q.rejected_total
FROM nb_pipeline_quota q JOIN nb_pipelines p ON p.id = q.pipeline_id
WHERE q.pending > 0 ORDER BY q.pending DESC;

-- ⑥ 租约是否卡死（应该接近 0）
SELECT count(*) FROM nb_deliveries
WHERE state = 'leased' AND lease_until < now() - interval '5 minutes';

-- ⑦ 尝试记录：某条投递到底试了什么
SELECT attempt, outcome, ack_level, error_code, retry_after_ms, duration_ms, created_at
FROM nb_delivery_attempts WHERE delivery_id = '<投递 id>' ORDER BY attempt;
```

第 ⑥ 条长期大于 0 说明 worker 反复崩溃，去看核心进程的重启记录。

---

## 9. 不要做的操作

- **不要手工 `UPDATE nb_deliveries SET state='pending'`**。绕过重放接口就等于绕过
  幂等键与审计，而且 `attempts` 不会重置，会在下一次尝试后直接进死信。
- **不要 `TRUNCATE northbound_source_outbox`**。里面是已登记未交接的数据，删了就是
  真的丢了。要停就关 `northbound.enabled`。
- **不要为了「清理」删 `unknown` 记录**。它们是「可能已经写进目标」的唯一证据。
- **不要把 `nb_tenant_quota.rejected_total` 清零**。它是「因容量未登记」的唯一账本，
  清零等于把数据丢失这件事抹掉。
- **不要在没有备份的情况下改 `NB_SECRET_KEY`**。改了之后所有已存凭证都解不开，
  所有连接会变成 `needs_repair`。
