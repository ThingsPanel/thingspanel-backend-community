# 社区 Encore 测试服自动部署

推送 `main` 后由 `deploy.yml` 先验证、部署社区后端，再调用 `deploy-encore.yml` 更新 `47.92.253.145` 的 Encore 和通知核心。两步使用同一提交，避免内部身份协议升级时只更新一端。

CI 使用固定 Encore CLI 1.58.6，先执行迁移校验测试、原生服务编译和回归，再上传带提交号及校验值的纯源码包。服务器限制构建并发并复用编译缓存，镜像只保存在测试服，不发布镜像仓库。

迁移 SQL 在仓库中固定使用 LF 换行，历史记录已核对实际 SQL 内容后统一校验值。发布中断后可直接重跑 Action，同一提交重新校验源码并执行发布。

服务器从 `/opt/tp-community-main-20261028/compose.encore.private.json` 读取现有环境，自动接线安装级通知身份，并更新 `encore-api` 与 `notification-core` 的镜像。三个资源库先检查历史迁移校验和与版本，再执行新增 up 迁移。不会重建数据库，也不会更新 Mosquitto。

发布后核验运行镜像、健康接口、匿名请求拦截，以及独立验收租户登录和物模型、通知组、规则列表。成功记录位于 `/opt/tp-community-actions/encore-releases/<提交号>/deployment.json`；编译和命令错误保留在该目录的私有日志中。

按用户对纯测试环境的要求，不新增兼容层，不额外备份，不自动回退；失败直接停止并修复。已有私有配置及自动化执行模式不变。服务器维护入口见工作区 `AGENTS.md` 链接的部署文档。

## 自动化执行接线

Mosquitto 的 `ingress_v1` 持久化事件由实际引擎模式选择消费者。native 模式只投递 Encore，不再执行旧场景。HTTP 调用在数据库事务外进行，effect ID 用于重复投递去重。

服务凭据保存在服务器 `automation-service.private.json`。社区进程使用 `GOTP_AUTOMATION_MODE`、`GOTP_AUTOMATION_BASE_URL`、`GOTP_AUTOMATION_TOKEN`、`GOTP_AUTOMATION_DEPLOYMENT_ID`；Encore 使用对应的 `TP_AUTOMATION_SERVICE_TOKEN` 和 `TP_SOURCE_DEPLOYMENT_ID`。这是一套独立服务身份，不使用用户登录 Token。`/internal/automation/` 不经浏览器网关开放。

命令仍由社区现有持久化队列与 MQTT dispatcher 下发，新引擎只提交带租户、幂等键、scope 和 generation 的意图。首次切换与真实设备闭环由验收步骤执行，自动部署保留当前模式。

## 定时与重试唤醒

Action 发布原生服务后安装并验证 `tp-automation-tick.timer`，每 5 秒运行一个不重叠的 oneshot。它从服务器现有 `automation-service.private.json` 读取服务身份，调用回环地址 `/internal/automation/tick`；先恢复已持久化未处理事件，再处理到期规则、等待续跑及 outbox。浏览器 `/v1/tick`、`/v1/recover` 已移除，不能用个人登录身份执行全库扫描。

服务器用 `systemctl status tp-automation-tick.timer tp-automation-tick.service` 检查；最近结果在 `/opt/tp-community-actions/automation-scheduler/last-tick.json`。service 单次失败会在下一轮重新运行，错误不包含凭据；失败不切旧引擎。修改定时规则不需要重新部署。

## 通知身份自动管理

部署程序首次运行生成安装级机器身份，保存在服务器 `notification-service.identity.private.json`（0600），自动接入社区后端、自动化引擎和通知核心。重复部署复用身份；部署 ID 不一致时停止，不拿另一套环境的凭据凑数。联系人读取使用独立的安装级只读身份，SQL 按租户与成员匹配，不再维护租户名单。

实际发送时，调用方通过内部接口 `/internal/notification/source-credentials` 领取有效期 15 分钟的凭据，只允许一个租户、一个部署和 source 或 projection 用途。数据库只存哈希，调用方只在内存缓存并提前刷新。授权依据是当前平台插件租户授权，租户继续通过渠道和策略控制接收方式。用户登录 Token 不参与后台签发或续期。

凭据固定对应授权版本与插件状态版本；撤销授权后旧凭据失效，重新授权不会恢复旧凭据。机器身份轮换也会使旧租期失效。服务重启或停机超过租期后自动重新获取；遇到 401 刷新一次，使用原事件和幂等键重试，不能重复产生业务通知。

首次升级通过通知库 0004 迁移撤销旧 source/projection 凭据。自动部署删除旧租户 Token 映射、旧静态发送 Token、手填租户名单、手动签发工具及通知续期 timer；现有插件授权、策略、设备绑定、发送记录和服务商配置保留。后台进程通过完整环境重建，避免 PM2 update-env 遗留已删除变量。

安装级身份端点仅用于内部服务通信。跨主机必须 HTTPS；本部署同机进程使用字面回环 IP。应用层拒绝重定向，不向其他目标转发凭据。公共网关不要映射 `/internal/notification/`。

UniPush、微信、短信等服务商密钥仍通过已有渠道配置管理。添加租户时只需要平台授权插件、租户配置渠道和启用通知策略，不需要运维修改任何凭据文件。

验收需包含：多租户隔离、授权撤销及重新授权、服务重启、凭据到期自动刷新，以及 MQTT 上报触发告警后手机实际接收。通知核心受理成功不等同于手机已送达。
