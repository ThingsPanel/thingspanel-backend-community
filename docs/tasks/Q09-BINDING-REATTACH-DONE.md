# Q09 绑定重新启用返回持久 ID

线上重新绑定相同规则与作用范围时，数据库重新启用了原记录，接口却返回未入库的新 ID 和零创建时间，随后按返回 ID 解绑得到 404。

修改 `automation/store/binding_store.go`，让同一个 upsert SQL 使用 `RETURNING` 返回实际记录并更新调用对象；`automation/binding_service.go` 直接使用该记录，移除读不到新 ID 后吞错返回临时对象的逻辑。不新增迁移，不改作用范围、认证和缓存失效合同。

基线为已部署 c3fd Encore 精确组合的本地 anchor `e5c45e44`，不是全量主线；候选仅包含两个产品文件及一个测试文件。anchor 不作为合并范围。

验证使用 Encore 隔离 PostgreSQL：新增四组在原代码失败、修复后通过，覆盖设备/模板解绑后重绑、重复绑定、六个并发绑定及租户隔离；另十一组已有绑定、参数、继承和 pending 窗口回归通过，均无跳过。没有在此工作树执行线上请求、推送或部署；线上原失败记录不改写为通过。

外置证据：`F:/coding/artifacts/20261111-coordinated-rollout/Q09-binding-reattach-review.json`，关联 before-v2 / after / regression JSONL 与线上原始 scope 失败、精确残留清理记录。

## 主分支验证

2026-11-13 将该修复应用到 main（基线 9257d16a），使用 Encore 隔离 PostgreSQL 再次执行四组持久 ID、重复绑定、并发绑定和租户隔离测试：4 通过、0 跳过。证据为外置 main-binding-reattach-tests.jsonl。线上验收待主分支部署后执行。
