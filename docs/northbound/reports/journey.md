# 北向服务 journey 验收（真实链路）

- 运行时间（UTC）：2026-10-21T10:29:37Z
- 结论：**通过**
- 断言：27 通过 / 0 失败 / 0 跳过

| # | 断言 | 结果 | 说明 |
|---|---|---|---|
| 1 | 北向服务可访问 | 通过 | HTTP 200 |
| 2 | HTTP 插件已就绪 | 通过 | HTTP 数据出口 |
| 3 | MQTT 插件已就绪 | 通过 | MQTT 数据出口 |
| 4 | 创建 HTTP 连接 | 通过 | HTTP 200 id=18a26145-0b61-4758-a514-3a185f8df709 |
| 5 | 连接校验三项全绿 | 通过 | connector=True / config=True / destination=True |
| 6 | 创建任务为草稿 | 通过 | state=draft revision=1 |
| 7 | 预览命中过滤 | 通过 | matched |
| 8 | 预览输出符合变换 | 通过 | {"humidity": 50, "site": "journey", "temp_c": 24.5} |
| 9 | 预览返回幂等键 | 通过 | nb1:bb15f3f55909d394950e... |
| 10 | 启用任务 | 通过 | activeVersion=1 |
| 11 | 测试发送被受理 | 通过 | delivery=63adde0e-99a5-569a-a3a0-94042a11218b |
| 12 | 投递到达终态 delivered | 通过 | state=delivered |
| 13 | 目标确认级别为 destination_acked | 通过 | destination_acked |
| 14 | 接收端真实收到该投递 | 通过 | 接收端共 2 条记录 |
| 15 | 送达数据等于预览输出（不是原始事件） | 通过 | {"humidity": 50, "site": "journey", "temp_c": 24.5} |
| 16 | 幂等键随请求头送达且与账本一致 | 通过 | nb1:4d003086a85435631422... |
| 17 | 重复事件被识别为 duplicate | 通过 | accepted=0 duplicate=1 |
| 18 | 同一事件只有一条投递 | 通过 | count=1 |
| 19 | 创建 MQTT 连接 | 通过 | HTTP 200 |
| 20 | MQTT 投递被 Broker 确认 | 通过 | state=delivered |
| 21 | QoS1 记为 destination_acked | 通过 | destination_acked |
| 22 | 真实订阅端收到消息 | 通过 | journey/dev-78579243-65842/dev-journey-mqtt/telemetry {"values":{"temperature":24.5}} |
| 23 | 总览可读 | 通过 | HTTP 200 |
| 24 | 总览同时给出等待数与拒绝数 | 通过 | pending=6 rejects=0 |
| 25 | 总览声明保证起点而不是端到端 | 通过 | event accepted into the Community durable outbox |
| 26 | 暂停任务不产生新投递 | 通过 | 2 -> 2 |
| 27 | 暂停期间事件仍被受理 | 通过 | accepted=1 |

## 说明

- 本脚本不覆盖：设备 → Broker → 厂商 Mosquitto 插件 这一段；它属于已验收的 broker 改造。

> 本报告由脚本生成。跳过的断言不等于通过；失败项保留原始输出以便复核。
