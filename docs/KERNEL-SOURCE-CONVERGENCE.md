# Kernel 成果归并

## 构建边界

社区后端的 Encore 应用在 `encore-api/`。独立通知 HTTP 插件在
`notification-adapters/`，是独立 Go 模块和进程，不注册为 Encore 服务。
手机接收与云函数仍属于 App 仓库。部署来源按服务记录，不能用 Gin
后端版本号代表 Encore 或插件版本。

本次适配器基线为 Kernel `077f89029699f103851672f8a4646e76127689ce`，
与 2026-11-10 测试服实际运行的 UniPush 二进制版本一致。完整保留该基线
的 SDK、UniPush、SMTP、钉钉、阿里云短信和 legacy webhook 源码及测试。
其他适配器保留各自已有的支持边界；源码归并不代表新增渠道已完成真实供应商验收。
Community develop 的插件 HTTP 和通知 worker 集成测试一并保留。

UniPush 的 plugin ID、manifest version、配置 schema、凭据变量和 HTTP
协议不变。已有注册、授权、渠道、策略、手机绑定及数据库无需重新创建。
保留 Community 的自动安装身份和短期作用域凭据，不恢复旧手工续期脚本。

## 验证与发布

`.github/workflows/notification-adapters.yml` 在独立 Linux 环境测试适配器，
并用隔离 PostgreSQL、真实通知 worker 和独立插件进程验证投递。
目录和 UniCloud 使用本地替身；该测试不代表真实手机送达。
阿里云短信账本使用 Linux 文件锁，完整适配器测试需在 Linux 运行。

`.github/workflows/notification-adapter-images.yml` 手动从选定的 Community
提交构建带源码 revision 的镜像，保存为 CI artifact，不自动发布或切换线上。
运维切换需使用经过验证的精确镜像 ID，保留原容器配置、凭据挂载和网络；
先保存原镜像 ID，再检查鉴权、manifest 与真实策略投递，异常时恢复原镜像。
不可用 develop 中旧插件副本覆盖本次基线。

## 尚不能宣布 Kernel 退役

设备元数据批次在 Community develop，涉及设备运行时及配套前端，不能为了
统一仓库而整支覆盖 main。模型删除的发布快照契约和名称唯一约束也需要单独
检查迁移与消费者行为。以上内容通过验收、归并清单闭合之前，保留旧仓库，
不删除分支，不声称整个 Kernel 已完成退役。
