# 安装版 MQTT 接收链路修复

现象：设备可连接新 Mosquitto，平台无遥测和在线状态。23.254.236.109 的安装栈已确认连接日志和设备记录存在，但 broker_ingress_receipts、broker_connection_observations 均为空。

根因：WithConfig 只将 config.AllKeys() 复制到全局 Viper；GOTP 自动环境变量仅配置在独立实例。旧配置文件缺少 mqtt.input_profile、broker_cell、broker_instance，新安装 Compose 中的环境变量未能到达仍读取全局 Viper 的接收模块，因此接收模块仍按 legacy 工作，无法消费受保护的内部消息。

修复：全局 Viper 同样配置 GOTP 前缀、点号转下划线与自动环境变量读取，保留已有显式键。回归测试以没有原生 MQTT 键的旧配置验证环境变量能被接收模块读取。不增加旧主题兼容路径，不放宽 Broker 权限。

按 AGENTS.md 的授权流程提交推送，更新 v1.3.0 构建引用并重跑镜像 Action。部署后验收必须包括独立设备实际连接、发布遥测、平台读取及断开后的状态更新；仅健康接口通过不算设备链路通过。
