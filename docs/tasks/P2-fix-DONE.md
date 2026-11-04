# P2-fix 完成说明：补齐凭据脱敏边界

## 分支与提交

- 分支：`wp/p2-fix-redaction`，从当前 `develop`（`85b04e7b`）创建。
- `git log --oneline develop..HEAD`（实现提交）：

```text
b7f6d59d fix(security): redact encoded credentials and URL userinfo
```

## 修改内容

- `pkg/logredact.URL` 在 URL 没有 query 时也清除 userinfo，避免 `https://user:password@host/path` 进入日志时泄漏凭据。
- `pkg/logredact.Text` 对 form/query key 做 URL 解码后再判定敏感字段，覆盖 `v%6fucher`、`voucher%5Btoken%5D`。遇到带 `%` 的非法编码 key 时采取保守脱敏。
- 新增 `internal/middleware` 回归：经 `AccessLogger` 与 `OperationLogs` 处理带编码 query key 和 form key 的请求，检查日志与 operation log 内容不含假凭据，且 operation log 的请求路径不包含 query。
- 在 `docs/WORKSPACES.md` 登记此 worktree。

## 验证

通过：

```text
git diff --check
GOCACHE=/tmp/p2-fix-redaction-gocache go test ./pkg/logredact ./internal/middleware ./third_party/others/http_client
```

三个包均通过。最终测试使用 task-local 临时 Go 缓存；系统共用的 `/Volumes/500G/.gocache` 曾报 `illegal byte sequence`，未清理或改动该缓存。

中间件回归使用内存 SQLite 验证 operation log 写入；本卡未重跑依赖真实 PostgreSQL 的既有 P2 API 集成测试，也未做完整平台端到端验证。

## 未完成事项

- 无本卡范围内的待实现项。
- 未推送、未合并；等待总监审查。
