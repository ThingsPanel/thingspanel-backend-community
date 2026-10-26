# 社区 Mosquitto 候选合并记录

## 来源与范围

- 目标：`main-1.3.0`，合并前先快进到远端 `33466805`，包含最新通知和原生认证配置管理。
- 来源：`codex/gmqtt-community-20261019`，固定 `1a1ef0a005a64a8d80e627cb8546794e018e2c9d`，保留来源提交历史。
- 本次只合并代码；过程文档、历史日志和证据的清理另行处理。没有修改 `configs/`、执行数据库迁移、启动或切换 Broker，也没有部署远程服务。
- 来源验收台账早于最后的期望消息、标准控制、自定义主题和插件上下文提交，不将历史测试结果视为最终源码完整认证。仍以默认 `legacy` 运行；启用 `ingress_v1` 需另外验证配置、数据库和实际设备兼容范围。

## 冲突处理与必要修正

1. `internal/app/service.go`：组合来源的 `StartAllContext` 与目标的启动失败回滚。上下文取消时清理失败组件和此前已启动组件，保留逆序停止与等待计数释放。
2. `main.go`：启动前用 `signal.NotifyContext` 注册退出信号，将同一取消信号用于启动和退出等待；保留目标分支的通知中继注册。更新退出回归测试，增加“已有生产者运行时，后续启动取消”的组合回归。
3. 市场发布测试辅助函数与来源生产代码 `stringPtr` 重名，重命名市场测试辅助函数，无业务行为变动。
4. 原生 SIGKILL 测试改用 `os.Process.Kill`，Unix 仍验证退出信号为 SIGKILL；Windows 明确跳过该专项，避免 `syscall.Kill` 阻断测试编译。
5. 切换工具将 Unix 0600 权限测试与错误脱敏测试分开，Windows 只跳过权限专项；生产文件权限检查没有放宽，Windows 可使用既有 DSN 环境变量入口。对账工具检查符号链接夹具创建结果，Windows 无权限时明确跳过，其他系统创建失败仍报错。

## 验证

- `go test ./... -run '^$'`：当前根 Go 模块全树编译检查通过，不包含独立嵌套模块或带 `mosquitto_plugin` 标签的 C-shared 插件构建。
- 22 个相关包回归：首轮 20 包通过，2 个工具包因 Windows 文件权限/符号链接夹具前提失败；仅修正上述测试后，2 包完整复跑通过。组合结果为 688 个测试/子测试 PASS、205 个 SKIP、0 个未解决 FAIL，不能描述为单轮全绿。
- 范围覆盖根程序、app、MQTT Adapter、brokeraitools/commands/contract/control/ledger/mapper/semantic/state、Mosquitto driver、service、api、uplink、storage、audit/cutover/reconcile/route/tools-client 和 MQTT 协议测试工具。
- SKIP 包括未配置隔离 PostgreSQL、Redis、原生 Broker、ChatBridge、私有子进程入口，以及 Windows 不具备的 Unix 权限/SIGKILL/符号链接前提；没有把这些测试计为通过。没有执行真实设备、72 小时长稳、切换灰度或生产认证。
- 生命周期竞态检查：`go test -race -json -timeout 120s . ./internal/app ./internal/adapter/mqttadapter -run 'Test(TerminationSignal|Startup|ServiceManager|MQTTService|CreateMQTTClient|MQTTClientManualAck)'`，3 个包、11 个测试通过，0 skip，无竞态报告。

历史文档的 Markdown 行尾双空格保留，不在合并中进行无关格式清理。最终提交版本可通过 `git log --merges main-1.3.0` 查询本合并提交及其双父节点。
