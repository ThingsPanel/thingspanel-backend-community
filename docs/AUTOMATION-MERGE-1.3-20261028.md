# 自动化合入 1.3：当前操作说明

本说明替代源分支 HANDOVER-MERGE-TO-1.3.md 中的切换、通知与数据库说明。仅本地集成，未授权推送、部署、修改远程数据库或启用调度服务。

## 默认与切换

默认 GOTP_AUTOMATION_MODE=legacy。GET /api/v1/automation/runtime（登录态）返回社区后端实际生效的模式；前端的场景、联动和告警均据此选择接口，不需要重新构建。查询失败停止该操作，不自动选择另一个库。native 没给 BASE_URL 时，上报与页面均使用 legacy。原生专属规则页面在 legacy 下拒绝原生请求。

native 配置地址和令牌后，必须先迁移原有规则、核对租户/设备范围、对账场景与告警数量，并验证上报至真实命令的闭环；合并不会自动迁移旧业务数据。GOTP_AUTOMATION_TOKEN 仍是原有实现中的调用身份，切换前必须验证它可以覆盖目标租户和设备，不能用单租户成功冒充多租户验收。

当前 1.3 已有 Encore 模块及独立 IoT 服务，部署前核对实际镜像和路由。不能把此合并简单描述为所有现有环境都从一个进程变两个，也不要把整套社区业务替换成 Encore。

## 社区数据库升级

启动会通过 ApplyCommandGuardSchema 自动执行嵌入的 sql/28.sql，创建 automation_scope_generation、automation_command_guard，并记录校验和账本。legacy 模式也会执行。部署前核对建表权限、已有同名表和启动回退；不要手工覆盖数据。

旧环境的隐藏规则编辑菜单可执行 sql/29.sql（仅菜单，先备份核对；初始化版本计数不会自动运行 29）。该脚本以 element_code 判重，从现有 automation 目录解析父 ID，不新增已下线的规则列表菜单。新装环境 sql/1.sql 带隐藏编辑入口。

Encore automation 的 migrations 由部署工具管理，与旧业务数据迁移分开。目录当前有 15 个文件，最高版本为 16（没有版本 13），不能按“16 个文件”核对。

## 告警通知

新策略将默认选择或显式社区通知组别名写入 AlarmSpec。告警触发/恢复时读取当前有效原生组和修订，随告警状态变化事务保存到 outbox；重试使用冻结快照。未设置默认只记录告警；策略不可用或读取失败记录 unresolved，不换用其他策略，也不回滚告警。原生告警策略不再依靠不会执行的 notify 步骤发送通知。

新增 TP_NOTIFICATION_SOURCE_READER_TOKEN（Encore 侧）：与社区侧 NOTIFICATION_MEMBER_READER_TOKEN 使用同一份独立服务凭据。社区侧 NOTIFICATION_MEMBER_READER_DEPLOYMENTS 和 NOTIFICATION_MEMBER_READER_TENANT_IDS（逗号分隔）允许列表必须包含相同部署和所有目标租户；TP_SOURCE_DEPLOYMENT_ID 必须与社区 source bridge 的部署 ID 一致。读取地址为 TP_COMMUNITY_BASE_URL/api/v1/notification/source-policies/:tenant，接口只返回原生组与修订，拒绝未允许的部署、租户和不明确查询。不要使用用户 JWT 替代此凭据。

TP_NOTIFICATION_URL / TP_NOTIFICATION_TOKEN 继续用于原生通知核心受理。读取策略和发送通知使用不同凭据。通知组发布快照必须可用；受理成功不代表供应商实际送达。

## 调度安装材料（尚未在服务器执行）

deploy/automation-rewrite/automation-tick.service 与 .timer 提供每秒一次的受限调度驱动。由运维在 native 切换验收时安装；不启动仓库中的旧本地 Compose。

在服务器 /etc/thingspanel/automation-tick.private.conf 放 curl 配置（权限 0600，服务账户可读）：url 指向内网 Encore /v1/tick，header 为有效 Authorization，禁止把真实令牌写入仓库。接口需认证，必须管理令牌有效期；失效会令 oneshot 失败，需对失败单元和最后成功调度时间设置监控。

检查 curl 路径、运行身份、路由和认证后，才安装并启用 timer。先人工验证一次返回正常，再验证定时规则、等待后继续和通知 outbox。单元运行时不会并发启动另一个同名单元；运行时的数据库领取机制仍负责多副本幂等。

## 回退

先停止 automation-tick.timer 和正在执行的 automation-tick.service，停止新原生入口；确认在途命令结束，再将社区后端设为 legacy 并重启，刷新页面。需要隔离整个新引擎时停止 automation 服务。前端随实际模式恢复旧接口，原生库保留，不把原生期间新增/修改的数据自动写回旧库。

## 发布门槛

本地编译与单测只证明代码层面的集成。原库升级、原有规则迁移、真实 MQTT 命令、定时驱动、通知默认/显式/不可用路径，以及同步回退，仍须在既有远程测试组件上完成验收。不得为补验收自动新建本机基础组件或操作其他业务库。
