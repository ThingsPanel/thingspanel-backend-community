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

`.github/workflows/notification-adapter-images.yml` 从选定的 Community
提交构建带源码 revision 的镜像，保存为 CI artifact，不自动发布或切换线上。
候选分支改动适配器时自动构建，也可手动触发。
运维切换需使用经过验证的精确镜像 ID，保留原容器配置、凭据挂载和网络；
先保存原镜像 ID，再检查鉴权、manifest 与真实策略投递，异常时恢复原镜像。
不可用 develop 中旧插件副本覆盖本次基线。

## 按功能归并范围

| 成果 | 本次处理 |
| --- | --- |
| UniPush 及其他已有通知适配器 | 以 Kernel `077f890` 保留实际运行版本和协议，统一源码、测试、镜像构建来源 |
| 设备、模板、分组、凭据、导出、文件资产 | 采用 Community 已有移植 `38191b80`，并保留 `8c146860` 的命名整理与旧 demo 清理 |
| CLI | 采用 Community 的 `e67e4c12`、`3c1220a5`、`081b535f`、`6a35466c`，保留当前社区命令并补充原生命令 |
| 模型删除 | 移植 Kernel `59a8bdca`、`e5c469e9`；只隐藏可编辑目录，保留已发布快照和已有引用，允许复用删除后的名称 |
| 数据库迁移 | 保持原 0001–0016 文件不变，保留元数据 0018–0039，新增模型删除 0040；不向已升级数据库回填旧序号 0017 |
| 账号兼容 | 修正原移植批次对 TENANT_USER 的误拒绝，保留普通用户/只读角色的模型只读能力；不授予写入、删除或通配权限 |
| 通知凭据 | 保留 main 的自动安装身份和短期租约，不回灌旧 provisioning/renewal 脚本 |
| 数据库兼容和 HA 改造 | 不在本次范围，不整支合并 develop，不引入其 SQL 翻译层 |
| Kernel 备份分支与旧部署命令 | 保留历史，不用旧工作区备份覆盖现有实现，不恢复已被替代的网关与 demo |

`kernel-convergence.yml` 使用生产部署相同的 Encore 1.58.6 在 Linux
测试相关服务并构建当前生产服务组合。升级测试使用独立 PostgreSQL 事务：
先创建含数据的 v16 schema，再升级，验证绑定/快照保留、名称约束及脏引用
拒绝时的事务回滚。部署脚本中的异常恢复保护会恢复原 Compose 配置并启动
原服务；它不撤销已提交的数据库迁移，也不能代替数据库备份。

源码归并与运行切换分开记录。当前生产构建仍选择
`authn,thingmodel,automation,notification`；本次不自动开启新增元数据/文件
服务、对象存储或后台设备写入任务。其配套前端、运行时身份和数据归属切换
需要独立验收，不能因源码已在 main 就宣称已经切换。

旧仓库先保留。插件部署切换、所有使用方核对完成之前，不删除原分支或仓库。
本次核查与验证证据位于 `F:/coding/artifacts/20261110-kernel-convergence/`。
