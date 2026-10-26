# MQTT 接入与服务生命周期

## 职责与边界

社区后端通过 MQTT Adapter 接入设备消息。`legacy` 保留现有入口；候选 `ingress_v1` 配合 Mosquitto 原生插件、Owner、版本化映射及 PostgreSQL 持久收据工作。完整协议与范围见 `docs/broker-contract-v1/README.md`，未完成验收见 `docs/gmqtt-community-acceptance-tracker-2026-10-19.md`；该历史台账早于最后几次兼容功能提交，不能作为最终源码的完整认证。

## 输入、输出与主流程

- 未设置 `mqtt.input_profile` 时使用 `legacy`；显式选择 `ingress_v1` 后校验身份配置和数据库结构，只订阅新入口，不同时消费旧原始主题。
- 新入口经可信元数据与路由校验、持久收据和业务处理，生成业务数据与后续动作；协议确认不代表设备物理执行成功。
- 主程序依注册顺序启动服务，退出时逆序停止。通知源中继和认证功能沿用目标分支现有配置与初始化入口。

## 本次已集成的生命周期规则

用户于 2026-10-26 确认先合并候选、后单独清理过程文件。合并不启用新入口、不执行数据库迁移或生产切流。

- 启动前注册退出信号，向支持 `StartContext` 的服务传递取消信号，保证 MQTT 等待期间可以退出。
- 任一服务启动失败或启动被取消时，清理失败组件及此前已启动的组件；后续服务不启动，等待计数不泄漏。
- 启动成功后等待同一取消信号，再进行有超时的逆序关闭，不先等待只能由关闭操作释放的 WaitGroup。

## 验收与入口

主要入口：`main.go`、`internal/app/service.go`、`internal/app/mqtt_service.go`、`internal/app/broker_ingress.go`。

合并后验证正常退出、启动失败回滚、启动取消、MQTT 生命周期及可信接入/命令相关回归，并做全树编译检查。外部 PostgreSQL、Redis、原生 Mosquitto 及 Linux C 插件测试须分别记录实际执行范围；未运行不记为通过。

2026-10-26 集成验证：全树编译通过，22 个相关包组合回归通过，生命周期专项 11 个测试通过 race；环境依赖及 Windows 专项跳过明细见 `docs/GMQTT-MERGE-2026-10-26.md`。
