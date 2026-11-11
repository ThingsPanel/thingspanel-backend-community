# 北向服务 release 验收

- 运行时间（UTC）：2026-10-21T14:09:28Z
- 结论：**通过**
- 断言：25 通过 / 0 失败 / 0 跳过

| # | 断言 | 结果 | 说明 |
|---|---|---|---|
| 1 | 核心在线（inbox 可达） | 通过 | HTTP 200 |
| 2 | 自有 Go 代码通过 gofmt | 通过 | 无未格式化文件 |
| 3 | 自有包通过 go vet | 通过 | 无告警 |
| 4 | Encore 应用通过 go vet | 通过 | 无告警 |
| 5 | 社区后端可构建 | 通过 | ok |
| 6 | HTTP 插件无令牌时全部拒绝 | 通过 | 6/6 个端点拒绝 |
| 7 | MQTT 插件无令牌时全部拒绝 | 通过 | 6/6 个端点拒绝 |
| 8 | HTTP 插件 describe 与合同一致 | 通过 | id=io.thingspanel.sink.http contract=northbound-connector/v1 |
| 9 | MQTT 插件 describe 与合同一致 | 通过 | id=io.thingspanel.sink.mqtt contract=northbound-connector/v1 |
| 10 | 插件拒绝错误令牌 | 通过 | HTTP 401 |
| 11 | 错误配置被 validate 拒绝 | 通过 | ok=False failure=config_invalid config_ok=False |
| 12 | 校验失败不向目标发送数据 | 通过 | 命中探针路径 0 次 |
| 13 | QoS0 记为 none（不冒充已确认） | 通过 | ack_level=none |
| 14 | 重复提交后幂等键不变 | 通过 | 18 条投递的键前后完全一致 |
| 15 | 重复提交不新增投递 | 通过 | 首次 18 条 -> 重复提交后 18 条（每个活动任务一条） |
| 16 | 幂等键格式统一为 nb1: | 通过 | 全部带 nb1: 前缀 |
| 17 | 幂等键不含实例或尝试次数 | 通过 | nb1: + 64 位十六进制 |
| 18 | 契约声明 unknown 不算成功 | 通过 | CONTRACT-v1.md 含 unknown 约定 |
| 19 | 草稿任务可以删除 | 通过 | HTTP 200 |
| 20 | 跑过的任务拒绝删除 | 通过 | HTTP 409 aborted |
| 21 | 必需交付文件齐备 | 通过 | 全部存在 |
| 22 | 不以 Kernel 作为构建或运行依赖 | 通过 | 无引用 |
| 23 | 源端开关默认关闭 | 通过 | DefaultConfig.Enabled=false |
| 24 | 合同记录了修订历史 | 通过 | v1.0 -> v1.1 -> v1.2 |
| 25 | Encore 应用单元测试通过（encore test） | 通过 | ok  	encore.app/thingmodel/core	(cached) |

> 本报告由脚本生成。跳过的断言不等于通过；失败项保留原始输出以便复核。
