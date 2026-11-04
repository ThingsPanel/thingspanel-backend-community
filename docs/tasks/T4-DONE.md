# T4 交付说明：受管插件运行时身份

分支：`claude/wp-t4-plugin-identity-3cf9d2`（基于 develop；已 cherry-pick `82149323`、`1d30be60`）。未推送、未合并。

## 做了什么
- 懒建表改为迁移：PG `sql/33.sql`（幂等）；MySQL `sql/mysql/015_plugin_runtime_identity.sql`（手写孪生，已加入 `sql/mysql_embed.go`，提供 `MySQLPluginRuntimeIdentityMigrationNames()`）。未改任何已执行迁移。唯一键 (tenant, instance, generation)。
- 数据访问全部 GORM：新包 `internal/pluginidentity`（签发/吊销/认证，时间由 Go 传入，无 SQL 方言函数，无 sqldialect/dbx）。
- 5 个入口：带有效 `Authorization: Bearer <id>.<secret>` 放行，否则统一 503 `PLUGIN_RUNTIME_IDENTITY_REQUIRED`；无任何匿名开关。有效身份越权（他租户、他服务/协议）→ 403 `PLUGIN_RUNTIME_SCOPE_DENIED`。设备列表、接入点列表按身份租户过滤（`GetDevicesByProtocolPluginReq.TenantID`，`json:"-"`，仅 handler 写入）。
- 低代次即使 revoked_at 丢失也被更高代次拦截。
- 文档：`docs/plugin-runtime-identity.md`。

## 测试
环境：自起的独立容器（timescaledb pg16、mysql 8.4、redis 7，已删除）。注意：共享的 `tp-dev-*` 容器在我运行中被别的会话 `down` 掉，所以改用独立名字/端口。
```
TEST_PG_SCHEMA_DSN=... TP_MYSQL_TEST_*=... go test -count=1 ./internal/pluginidentity/        # PG + MySQL 均 PASS
TEST_REDIS_ADDR=... TEST_PG_SCHEMA_DSN=... go test -count=1 -run 'TestManagedIdentity|TestIssueRequires|TestHigherGeneration|TestExpiredLease|TestCrossTenant|TestUserTokens' ./internal/api/   # PASS（真实 PG）
go test ./internal/middleware -run '^TestLegacyPluginRuntimeGuard' ; go test ./sql/ ; go vet ./router/ ...  # PASS
```
覆盖：签发/吊销、高代次吊销低代次、同/低代次 409、并发同代次只成功一个、租约到期、跨租户、协议/服务不匹配、吊销后 5 入口全拒、用户 JWT/伪造头/错误令牌格式不能冒充、插件令牌不能签发身份。HTTP 层测试只跑 PG（MySQL 只测存储层）。

## 没做 / 注意
- 未跑全树 `go test ./...`；未接入真实 Gin 路由 + JWT 的端到端（测试用同样的 handler 与守卫，签发路由的管理员 claims 用测试头模拟）。
- MySQL 迁移尚未接入迁移执行器（`initialize/` 是 T1-0 范围）；PG `33.sql` 也需安装器/升级流程执行到。已更新 `scripts/dev-databases.sh` 的循环到 33。
- 租约心跳不续租，续约 = 用更高代次重签；broker 的 MQTT 凭据未细分（TODO，已写入文档）。
- device/config 对“他租户设备”返回 403、对不存在设备返回未找到，存在性可被区分（同租户管理员签发的身份，风险低，可后续统一）。
- `RejectUnboundPluginRuntime` 保留但不再挂路由；`docs/legacy-plugin-runtime-guard-v1.md` 未更新。

## git log --oneline develop..HEAD
7bbef3ad feat(plugin-identity): migrations, GORM store, five entrypoints gated by managed identity, tests, doc
25332a1c WIP(unverified): managed plugin runtime identities replace the closed anonymous plugin entrypoints
7bfaff53 fix(brokercontrol): do not rely on an implicit uuid = varchar cast when resolving plugin targets
