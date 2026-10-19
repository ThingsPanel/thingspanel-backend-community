# 社区可靠链阶段检查点（不是完工/生产准入）

2026-10-20，候选分支 `codex/gmqtt-community-20261019`，基础提交 `0e3ba8281f09c815dabcd03c634d17af845e950b`。仅社区 Mosquitto，没有企业驱动改动。原目录其他通知项目改动与 VERSION 未纳入候选。

本次冻结 235 个明确归属文件，[manifest](phase3-candidate-files.json) SHA256 `875a84395fa6d5a6030fd5e21f682d835901208a0a6923671070c596b8bb2e8f`。这是代码集成检查点；尚未包含正在开发的 SQL012 路由管理状态机及其 runtime 接线。

## 实际验证

- `go build ./` 成功。
- 以真实隔离 PostgreSQL DSN 设置 `TEST_BROKER_DATABASE_DSN`，并设置 `TEST_MQTT_NATIVE_E2E=1`，执行 `go test -race -p 1 -count=1` 下列 14 包，exit 0。DSN 不进入仓库/输出。
- `internal/brokercontract`、`brokercontrol`、`brokerledger`、`brokermapper`、`brokerstate`、`brokercommands`、`mosquittodriver`、`adapter/mqttadapter`、`uplink`、`storage`、`service`、`app`、`cmd/tp-broker-audit`、`test/mosquitto-community`。
- 原生 E2E 在随机隔离 PG schema、随机 loopback Broker 端口、临时 Redis 和测试 MQTT client 上运行。证明的软件接入/业务/持久命令/设备 ACK 边界与物理设备执行不同。
- 原始包结果：[phase3-candidate-race.log](phase3-candidate-race.log)，SHA256 `8dd665097b2eb5fee4a7202790c0ce5a6b7492d630864de3e0269cd71b0b2cb7`。
- 先前全包并发测试出现 `too many clients`；保留其失败事实，降低测试池和包并发后本轮通过。该变化是测试资源控制，不是生产容量结论。

## 验证能够支持的结论

固定2.1.2插件、可信封装、权限、SQL持久受理、幂等/最终fence、状态/活动投影、事务自动化及命令回执已形成可复跑候选。默认仍是legacy，显式启用 ingress_v1 才进入新链。

## 不能支持的结论

G0现场黄金样本/规模和旧新完整栈配对收益、完整故障矩阵、72小时长稳、真实barrier/灰度/回退、安装接班、社区AI留出评测尚未完成。普通遥测从内存总线到最终批量事务仍是明确的best-effort窗口。QoS2、custom command mapping、wire-retained、延迟/重启Will等没有完整认证。原1883/9999与真实设备配置没有切换，不得按此检查点生产发布。
