# App 通知页面与真机验证（2026-10-22）

## 本次解决的问题

- 本地 Encore 与社区后端 JWT key 不一致：统一私有运行配置，页面不再因 401 退出。
- 默认策略接口 503：配置本地可信 TLS 代理、source/projection 凭据并启用社区桥接；页面可读取默认策略。
- 旧 App 发送链路忽略 HTTP 错误、组历史假成功：补齐超时、响应长度限制、HTTP 状态和嵌套 provider 错误检查；设备失败会反映到组历史。
- 新增 `notification-adapters/unipush`：复用原云函数，以指定 CID 发送文本，接入新版试发和发送记录。没有重写 Core 或 App CID 注册。
- App 云函数的错误检查已在本地修正并测试，**未发布到云端**。新适配器也能识别当前云端旧包装中的嵌套错误。

## 已验证

- 浏览器实际点击模拟实例：受理和拒绝分别显示正确结果，无自动重复提交。
- 后端 App 专项测试通过，含 Android/iOS、注销与跨租户排除、HTTP 500、嵌套业务错误、组失败记录。
- 插件 SDK/全部适配器测试通过；云函数 7 个本地测试通过。
- 用户连接的 Android 手机运行了 `F:\coding\app`，取到并绑定 CID（不在仓库保存完整设备标识）。
- 完整云函数地址是 `http://push.thingspanel.cn/push-message`，根路径不可调用。
- 第一次直接测试：provider `errCode=0`、`successed_online`；App 收到消息，但因通知权限关闭未创建通知。
- 开启已连接调试 App 的 Android 通知权限后，通过浏览器 **App UniPush 真机测试** 实例发送第二条：
  - notificationId：`89c3da35-1111-4ae7-be7d-5502b64f8b38`
  - 页面：服务商已受理，通道不提供送达回执。
  - 手机日志：`message.receive`、`notification.created`。
  - Android 系统通知记录含标题“ThingsPanel 新通知插件测试”和测试编号 `TP-20261022-1548`。
- 总计两条真实测试消息；模拟测试没有向手机发送。

## 如何再次测试

1. 打开 `http://localhost:5002/alarm/notification-group`，使用本地租户管理员登录。
2. 选择“可用服务” → “App UniPush 真机测试” → “发送测试”。
3. 接收目标填当前测试手机的 UniPush CID；填写标题和文本，确认一次。
4. 点击“查询状态”，并在“告警 → 通知记录”查看投递结果；手机上确认通知栏。
5. 若只验证失败/成功分支，选择“App UniPush 本地模拟（不会发到手机）”：`local-test-accepted` 模拟受理，`local-test-rejected` 模拟拒绝。

CID 从 HBuilderX 的 `[PushDebug] cid.success` / `bind.success` 日志取得。
这次手机保留原来的 cloud.thingspanel.cn 登录会话；新版指定 CID 试发不要求它登录本地服务器。
“本地按成员选择手机”则要求 App 登录本地后端并注册设备，不应从其他环境数据库隐式复制绑定。

## 范围与运行配置

- 当前支持指定 CID 在线推送；按成员解析多设备 CID、通知点击跳转实际告警、离线厂商推送、小程序订阅消息未在本次实现/验证。
- “受理”来自 provider；真机显示的证据来自手机日志及系统通知记录，未伪造送达回执。
- 本地模拟服务和真机服务分开运行；真实服务不会在启动时发送通知，未设置自动告警到真实手机的默认策略。
- 私有环境文件、凭据及设备测试记录在 OS 临时目录 `thingspanel-local-20261022`，不提交 Git。
- source/projection 凭据是 8 小时本地联调凭据；过期后需用授权管理员/租户会话重新签发。未作为生产部署。
- 本地测试管理员 `repro-local@thingspanel.test` 的密码仅在开发副本中恢复为现有测试租户密码；原行备份保存在私有目录。未修改远程账号。
- HTTP 推送地址是用户提供的现有部署，适配器通过显式 `NOTIFICATION_UNIPUSH_ALLOW_HTTP=1` 接入。生产建议提供可信 HTTPS。
- App 云函数的本地回归：`node scripts/test-push-message.cjs`。
