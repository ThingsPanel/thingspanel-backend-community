# App 正式通知插件与多设备链路（2026-10-22）

## 已实现

- `thingspanel.unipush` 2.0.0 独立 HTTP 插件与非 root Docker 镜像；部署文件在
  `iot-api-encore-kernel/notification-adapters/deployments/unipush/`。
- App 自动上报安装标识、CID、平台、App ID；服务端从登录会话取得用户/租户。
  新建 `app_push_devices` 表由社区后端启动迁移自动创建；不迁移没有会话证明的旧 CID。
- 同账号多手机分别绑定；同手机切号、CID 变化、会话变化替换旧绑定。
  退出、单端登录踢出、Redis 会话过期、JWT 到期、用户禁用后均不再发送新消息。
  注册事务使用锁和唯一索引，重复/并发请求不制造多个有效绑定。
- 策略使用 `member + appDevice`，按成员展开有效手机、去重、逐设备入队和记录结果。
  一个成员没有有效手机不影响其他成员；全部没有有效手机时明确记录 blocked。
- 插件发送前重新验证绑定及租户，不把业务 userId 当成 CID，不让旧排队任务跟随新账号。
- 告警内容通过 `device_alarm` 模板映射 title/text/alarmId/alarmKind。
  `AlarmExecute` 的历史记录与 `AddAlarmInfo` 的活动记录均有明确详情定位。
- App 对收到和点击的消息检查账号、租户及服务器；详情查询强制租户隔离。
- 新云函数 `push-message-v2` 独立部署，使用服务端凭据和 HTTPS、稳定 request_id。
  保留旧入口，避免未经迁移就中断旧部署；完成正式服迁移后应停用旧的无鉴权入口。
- App manifest 已开启 offline；正式包须重新构建，并配置真实厂商通道/APNs。

## 怎么配置

1. 平台管理员注册 2.0.0 插件、授权目标租户、启用 App 服务实例。
2. 租户进入「告警 → 通知策略」，新建或编辑策略，选择「App 正式通知（多设备）」。
3. 点击「使用设备告警通知（选择成员，自动发送到全部有效手机）」，选择接收成员。
   多个成员添加多条绑定，不填写 CID。
4. 保存并启用当前版本；在告警规则选择该策略，或设为租户默认策略。
5. 手机安装新 App、登录同一后端并允许通知。绑定自动完成。

**同账号同时登录多台手机需要启用系统已有 `shared_account`（共享账号）设置。**
关闭时仍遵守原来的单端登录策略：后一登录使前一会话及其通知资格失效。
不同业务用户分别建账号，并在策略里逐个选择；不能靠共享 CID 区分人员。
会话过期后需要重新登录，插件不会绕开登录失效策略继续发送。

部署参数、云函数私密配置文件、TLS 要求见相邻 kernel 的
`notification-adapters/unipush/README.md`。密钥和真实 CID 不入 Git。
社区后端 `APP_PUSH_APP_ID`、插件 `NOTIFICATION_UNIPUSH_APP_ID` 与云函数 App ID
必须一致，默认项目为 `__UNI__71E31D5`。

## 本机已验证

- 本地页面：`http://localhost:5002/alarm/notification-group`。
- 正式插件镜像：`thingspanel-notification-unipush:2.0.0`，已构建并运行。
- UniCloud 新函数已上传；HTTPS 未带凭据返回 401，正确凭据空请求返回参数错误，未发消息。
- 已连接 Android 手机自动绑定到本地后端。原手机运行基座存储已私密备份。
- 已发布策略 `App 设备告警通知`，接收人为本地租户管理员，未开启全租户默认发送。
- 通过真实 `AlarmExecute` 生成模拟阈值告警和事务 outbox，后台 relay 首次即 handed_off。
- 告警 ID：`45ac7578-2bce-2582-98f3-562698432d81`。
- 通知 ID：`363f5d58-3ad2-40b2-a86a-5bfd46dcfe50`。
- 单设备 delivery：`a0c0ebd8-3fef-47f3-8fa6-579480f274e6`，attemptCount=1，accepted。
- 手机短暂离线后重新连接，于 16:26 收到该告警；日志有 message.receive 与
  notification.created，Android 系统通知记录确认显示 `[ALERT] App 正式链路验证 TP-20261022 [M]`。
- 同租户详情 API 返回上述告警；跨租户读取两类告警的自动测试均不返回记录。
- 浏览器验证 App 成员目标、告警模板预设、保存与发布；不用填写 CID。

## 验证与边界

相关 PostgreSQL/Redis 集成测试、后端全树编译、Core/adapter 测试、前端 27 项通知页面
和工作流测试、App 云函数/消息隔离/会话清理脚本、App 项目结构检查通过。
覆盖多手机、同机切号、并发绑定、注销与过期、禁用用户、跨租户、发送前撤销、模板定位。
全量前端 vue-tsc 未通过：build/plugins、packages/axios、ThingsVis 等其它路径有类型错误；本次修改的通知页面、声明及测试文件没有类型诊断。

真机测试只有一台 Android；多手机的分发由隔离集成测试验证。没有完成全部品牌杀进程、
iOS APNs、正式签名安装包的离线实测，也没有宣称 provider accepted 等于手机必达。
已交给厂商的历史通知不能在注销后撤回；客户端过滤仍会保护后续点击的账户边界。

本次部署的是新云函数和本机服务；正式社区后端/Core/前端服务器仍需发布这些代码，
正式 App 需重新打包。微信小程序订阅通知使用另一套 openid/订阅授权，不能用此 CID 插件替代。
