# 社区 Encore 测试服自动部署

推送 `main` 且修改 `encore-api/` 时，`deploy-encore.yml` 自动更新 `47.92.253.145` 的 Encore 和通知核心。修改部署脚本或手工运行 workflow 也会触发。

CI 使用固定 Encore CLI 1.58.6，先执行迁移校验测试、原生服务编译和回归，再上传带提交号及校验值的纯源码包。服务器限制构建并发并复用编译缓存，镜像只保存在测试服，不发布镜像仓库。

迁移 SQL 在仓库中固定使用 LF 换行，历史记录已核对实际 SQL 内容后统一校验值。发布中断后可直接重跑 Action，同一提交重新校验源码并执行发布。

服务器从 `/opt/tp-community-main-20261028/compose.encore.private.json` 读取现有环境，仅更新 `encore-api` 与 `notification-core` 的镜像。三个资源库先检查历史迁移校验和与版本，再执行新增 up 迁移。不会重建数据库，也不会更新 Mosquitto。

发布后核验运行镜像、健康接口、匿名请求拦截，以及独立验收租户登录和物模型、通知组、规则列表。成功记录位于 `/opt/tp-community-actions/encore-releases/<提交号>/deployment.json`；编译和命令错误保留在该目录的私有日志中。

按用户对纯测试环境的要求，不新增兼容层，不额外备份，不自动回退；失败直接停止并修复。已有私有配置及自动化执行模式不变。服务器维护入口见工作区 `AGENTS.md` 链接的部署文档。

## 自动化执行接线

Mosquitto 的 `ingress_v1` 持久化事件由实际引擎模式选择消费者。native 模式只投递 Encore，不再执行旧场景。HTTP 调用在数据库事务外进行，effect ID 用于重复投递去重。

服务凭据保存在服务器 `automation-service.private.json`。社区进程使用 `GOTP_AUTOMATION_MODE`、`GOTP_AUTOMATION_BASE_URL`、`GOTP_AUTOMATION_TOKEN`、`GOTP_AUTOMATION_DEPLOYMENT_ID`；Encore 使用对应的 `TP_AUTOMATION_SERVICE_TOKEN` 和 `TP_SOURCE_DEPLOYMENT_ID`。这是一套独立服务身份，不使用用户登录 Token。`/internal/automation/` 不经浏览器网关开放。

命令仍由社区现有持久化队列与 MQTT dispatcher 下发，新引擎只提交带租户、幂等键、scope 和 generation 的意图。首次切换与真实设备闭环由验收步骤执行，自动部署保留当前模式。

## 定时与重试唤醒

Action 发布原生服务后安装并验证 `tp-automation-tick.timer`，每 5 秒运行一个不重叠的 oneshot。它从服务器现有 `automation-service.private.json` 读取服务身份，调用回环地址 `/internal/automation/tick`；先恢复已持久化未处理事件，再处理到期规则、等待续跑及 outbox。浏览器 `/v1/tick`、`/v1/recover` 已移除，不能用个人登录身份执行全库扫描。

服务器用 `systemctl status tp-automation-tick.timer tp-automation-tick.service` 检查；最近结果在 `/opt/tp-community-actions/automation-scheduler/last-tick.json`。service 单次失败会在下一轮重新运行，错误不包含凭据；失败不切旧引擎。修改定时规则不需要重新部署。

## 通知身份持续运行

首次 source/projection 授权仍由现有运维工具验证平台及租户真实会话。之后由 root 私有 `notification-renew-policy.private.json` 明确登记既有部署、单租户、凭据文件及实际运行配置。它授权本机运维续期，不创建或扩展身份；不使用管理员账号或用户 JWT 维持后台服务。

CLI `notification-source-provision --service-policy <绝对私有策略路径> --ttl 8h` 仅允许 Linux root，检查策略、凭据、配置及其父目录归属与权限。只延长数据库中现有、未撤销、未过期且双哈希和范围匹配的 source/projection 对；任一不匹配则整笔回滚，不能复活失效凭据。

Action 构建工具并校验发布包；存在该私有策略时，安装 `tp-notification-renew.timer` 每小时续期一次，并立即验证。最近脱敏结果在服务器 `notification-renew-result.json`，私有凭据原子更新到期时间，令牌保持不变。进程无须为续期重启。

若机器停止超过当前 8 小时租期，过期身份不自动恢复，需按首次签发流程重新授权。停止续期 timer 只停止延长，立即撤销须走既有凭据撤销入口。发布不覆盖策略、令牌及原有授权范围；临时验收管理员保持禁用。
