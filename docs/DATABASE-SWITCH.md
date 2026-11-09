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
