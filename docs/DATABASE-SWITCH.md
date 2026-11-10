# Database selection

## 平台关系库

默认发行版仍使用 PostgreSQL + TimescaleDB。MariaDB 仅是可配置替代，不改变默认安装，也不单独发布 MariaDB 发行版。

MariaDB 通过 MySQL driver 下的 flavor 选择；不要设置 `relational.driver: mariadb`：

```yaml
relational:
  driver: mysql
  mysql:
    flavor: mariadb
    platform:
      host: 127.0.0.1
      port: 3306
      database: thingspanel
      username: thingspanel_app
      password: "<从密钥管理注入>"
      time_zone: UTC
```

支持 MariaDB 10.11 LTS 与 11.4 LTS（对应的 10.11.x、11.4.x 补丁版本）。启动时会核对 flavor 与服务器版本；MariaDB 连接不能省略 `flavor: mariadb`。MySQL 8 使用同一个 `driver: mysql`，省略 flavor 即按 MySQL 处理。MariaDB 专用迁移覆盖位于 `sql/mariadb/`，其余兼容迁移共用 `sql/mysql/`。

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
