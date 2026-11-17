# ThingsVis SSO 配套修复

2026-11-17，用户要求补齐社区版、企业版及 ThingsVis 的登录配套并提交。

- 社区版 develop 增加受 JWT 会话保护的 `/api/v1/auth/sso/identity`；main 保留已有接口并修正历史超管空间映射。
- 身份来自当前数据库；拒绝停用账号、过期身份、租户不匹配及委托身份。超管空间为 `thingspanel-sys-admin`，包括历史无租户的超管账号；普通租户不得使用空租户，普通用户角色仍为 VIEWER。
- 测试使用内存 SQLite、隔离 Redis 验证真实 JWT 中间件到接口的 HTTP 响应；覆盖匿名拒绝、当前身份、禁用账号与历史空间。测试依赖 miniredis 仅用于隔离验证。
- 两条分支均通过 `go test -vet=off ./internal/api -run TestPlatformIdentity -count=1` 与 `go build ./router`。
- 对应 ThingsVis 必须配置可达的 `THINGSPANEL_API_URL`。先部署后端，再部署 ThingsVis；实际账号端到端验收还需在部署后执行。
- 只作本地提交，不推送、不执行生产发布，不自动迁移或删除看板数据。
