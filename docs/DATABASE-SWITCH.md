# Database selection

## 时序库

平台关系库仍由 `relational.driver` 选择。时序数据默认跟随平台 PostgreSQL；选择 TDengine 时，在 `storage` 下配置：

```yaml
storage:
  history_backend: tdengine
  tdengine:
    url: http://tdengine:6041
    user: root
    password: "<your-tdengine-password>"
    database: thingspanel_history
```

`url` 使用可从后端容器访问的 TDengine REST endpoint。`user`、`password` 和 `database` 必须对应已部署的 TDengine 服务。启动时后端会校验配置并确保历史表结构可用；TDengine 不可达或配置不合法时启动失败，不会静默切回 PostgreSQL。无需配置实验开关。

选择 Apache IoTDB 2.x Table 模型时，配置 IoTDB Table REST 服务：

```yaml
storage:
  history_backend: iotdb
  iotdb:
    url: http://iotdb:18080
    user: root
    password: "<your-iotdb-password>"
    database: thingspanel_history
```

IoTDB REST 服务必须启用，并且账号有创建数据库/表及读写历史数据的权限。IoTDB 后端使用 Table 模型 `telemetry` 表，通过 PostgreSQL/MySQL 中的历史意图队列重放写入；最新值、属性和设备状态仍使用平台关系库。启动时会确保 IoTDB 数据库和表存在；IoTDB 不可达或配置不合法时启动失败，不会静默切回关系库。现有 PostgreSQL 历史记录不会自动迁移。

设置 `TP_IOTDB_TEST_URL`（可选 `TP_IOTDB_TEST_USER` 和 `TP_IOTDB_TEST_PASSWORD`）后，可运行 `go test ./internal/iotdbhistory -run '^TestIoTDBRemoteSmoke$' -v`。烟测使用随机命名的隔离数据库，并在结束时删除。
