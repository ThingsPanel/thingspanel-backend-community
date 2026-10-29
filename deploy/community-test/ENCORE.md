# 社区 Encore 测试服自动部署

推送 `main` 且修改 `encore-api/` 时，`deploy-encore.yml` 自动更新 `47.92.253.145` 的 Encore 和通知核心。修改部署脚本或手工运行 workflow 也会触发。

CI 使用固定 Encore CLI 1.58.6，先执行迁移校验测试、原生服务编译和回归，再上传带提交号及校验值的纯源码包。服务器限制构建并发并复用编译缓存，镜像只保存在测试服，不发布镜像仓库。

服务器从 `/opt/tp-community-main-20261028/compose.encore.private.json` 读取现有环境，仅更新 `encore-api` 与 `notification-core` 的镜像。三个资源库先检查历史迁移校验和与版本，再执行新增 up 迁移。不会重建数据库，也不会更新 Mosquitto。

发布后核验运行镜像、健康接口、匿名请求拦截，以及独立验收租户登录和物模型、通知组、规则列表。成功记录位于 `/opt/tp-community-actions/encore-releases/<提交号>/deployment.json`；编译和命令错误保留在该目录的私有日志中。

按用户对纯测试环境的要求，不新增兼容层，不额外备份，不自动回退；失败直接停止并修复。已有私有配置及自动化执行模式不变。服务器维护入口见工作区 `AGENTS.md` 链接的部署文档。
