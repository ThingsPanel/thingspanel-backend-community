# Linux Compose 发布包

这是现有部署升级用的应用包，不是空机器一键初始化器。只支持 Linux amd64/arm64；Windows EXE 自动触发已暂停。镜像均固定摘要，Encore 和通知核心共用同一构建镜像、分别读取运行配置。

1. 核对 `release-manifest.json` 和 SHA256SUMS。先准备已有 PostgreSQL、Redis、ThingsVis 和通知插件；设置 `TP_NETWORK` 为它们所在的 Docker 网络，ThingsVis 名称为 `thingsvis-studio`、`thingsvis-server`。本包不重建或清空数据库，不打包客户数据。
2. 建立权限受限的 `private/`，从当前有效私有部署配置调整以下文件；不要复制仓库默认密码作为生产凭据。

| 文件 | 要求 |
| --- | --- |
| backend.yml、backend.env | 数据库/Redis/MQTT 现有配置；MQTT 指向 owner:1883，启用已部署的 ingress_v1 与服务允许列表；GOTP_AUTOMATION_MODE=native、GOTP_AUTOMATION_BASE_URL=http://encore-api:14000、独立 GOTP_AUTOMATION_TOKEN 与 GOTP_AUTOMATION_DEPLOYMENT_ID |
| runtime.encore.json、encore.env | Encore 1.58.6 infra 配置；hosted_services 为 authn/thingmodel/automation；三个资源数据库保留原 logical name；TP_AUTH_MODE=legacy-jwt、同社区 JWT key、TP_COMMUNITY_BASE_URL 和 TP_COMMUNITY_SESSION_BASE_URL=http://backend:9999；匹配服务令牌和部署标识 |
| runtime.notification.json、notification.env | hosted_services 为 authn/notification；原通知数据库、密钥环、source 与投影授权；真实通知插件地址、私有 CA、source reader 等按现有配置准备 |
| owner.env、owner-tokens.json、services.json | Broker Owner 数据库 DSN、独立服务凭据和严格允许列表；不要匿名开放 |
| broker.env、plugin-token | TP_BROKER_CELL、TP_BROKER_INSTANCE、插件令牌；挂载文件必须允许 UID 1883 读取 |
| scheduler.env | TP_AUTOMATION_SERVICE_TOKEN、TP_SOURCE_DEPLOYMENT_ID 与 Encore 相同 |

3. 在停写窗口审核并应用 `migrations/` 中尚未执行的迁移，按原部署迁移台账核对版本和校验和；不要对已有库重跑社区初始化 SQL。本包刻意不自动执行 SQL，也不迁移历史规则。Encore 自托管镜像不会自动管理数据库迁移。
4. 执行（Linux 服务器）：

```sh
export TP_NETWORK=你的已有网络
docker compose --env-file images.env config --quiet
docker compose --env-file images.env pull
docker compose --env-file images.env up -d
docker compose --env-file images.env ps
```

HTTP 默认 8080，MQTT TCP 1883。TLS 证书与入口由现有网关维护；本示例不宣称提供 8883。前端代理 `/automation-core/`，内部服务路由拒绝外部访问。若修改网络/服务名，同步调整代理及运行配置。

通知 source 身份需首次显式授权；包内两种架构 `notification-source-provision` 用于维护既有授权，不能从本包获得测试服凭据。通知续期 timer 需沿现有部署保留；`*.test-host-reference.py` 仅是 47 测试服维护参考，路径固定，不能直接在新环境运行。未配置通知授权及续期前，不声称通知可持续运行。

升级后检查登录、原生接口、MQTT 上报与命令、定时任务及通知。首次实际镜像发布前只做了配置检查，不应把本包模板视为已在空服务器完成安装验收。
