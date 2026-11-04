# P2-fix 完成说明：补齐凭据脱敏边界

## 分支与提交

- 分支：`wp/p2-fix-redaction`，已对齐最新 `develop`（`47fb5715`）。
- `git log --oneline develop..HEAD`（本次交付说明更新前）：

```text
3892ff6f fix(security): redact quoted credential fields fail closed
de3a49d5 docs: record P2-fix review corrections
c42d220e fix(security): close redaction suffix and parse-error leaks
62e60bf6 docs: record P2-fix redaction handoff
1e9d545c fix(security): redact encoded credentials and URL userinfo
```

## 修改内容

- `pkg/logredact.URL` 在 URL 没有 query 时也清除 userinfo，避免 `https://user:password@host/path` 进入日志时泄漏凭据。
- `pkg/logredact.Text` 对 form/query key 做 URL 解码后再判定敏感字段，覆盖 `v%6fucher`、`voucher%5Btoken%5D`。遇到带 `%` 的非法编码 key 时采取保守脱敏。
- 新增 `internal/middleware` 回归：经 `AccessLogger` 与 `OperationLogs` 处理带编码 query key 和 form key 的请求，检查日志与 operation log 内容不含假凭据，且 operation log 的请求路径不包含 query。
- 在 `docs/WORKSPACES.md` 登记此 worktree。

## 补充审查修复

- 对敏感参数值保留 `?` 后的后缀也一并脱敏，因此 `voucher=secret?suffix&next=ok` 不再暴露 `secret?suffix`；对非敏感 `page=1?next=ok` 保持原文，并继续识别后缀中的独立敏感参数。
- URL 解析失败时也先清除 authority 中的 userinfo，再脱敏其余文本；覆盖 `https://operator:pw@host.test/path%ZZ`。
- 系统化处理 JSON/form/query/错误文本里的 `=`、`:`、单/双引号和结构值边界。敏感值有引号时保留引号并遮罩内容；未闭合引号/结构值会遮罩到文本末尾；未加引号的敏感值保守地遮罩到下一个 `&`。URL query 解码失败时改走同一 fail-closed 路径。
- 补充回归：敏感参数问号后缀、普通参数不变、嵌套敏感 query 与 malformed URL userinfo。
- 错误文本回归覆盖 quoted voucher 和包含 userinfo 的 malformed URL；操作日志回归的真实 middleware 请求体加入了带引号的 voucher。
- 总监复审补充：多层 URL 编码的敏感字段名、截断 JSON 中 Unicode 转义字段名也需脱敏；字段名按有限层数解码，无法确定的转义按敏感字段处理。增加对应回归，包含有效 JSON、截断 JSON、普通文本和 URL。

## 验证

通过：

```text
git diff --check
GOCACHE=/tmp/p2-fix-redaction-gocache go test ./pkg/logredact ./internal/middleware ./third_party/others/http_client
```

三个包均通过，包括补充审查后的最终运行和对齐 `develop` 后的复测。测试使用 task-local 临时 Go 缓存；系统共用的 `/Volumes/500G/.gocache` 曾报 `illegal byte sequence`，未清理或改动该缓存。

中间件回归使用内存 SQLite 验证 operation log 写入。总监另在独占 PostgreSQL 16 容器的新建专用 `p2_review` 库执行 `go test ./internal/api -run '^TestVoucherNeverLeaksIntoLogsURLsOrOperationLogs$' -count=1`，通过；未做完整平台端到端验证。

## 未完成事项

- 仅针对已覆盖的脱敏输入和路径验证，不能证明所有可能的编码或日志来源均无泄漏。
- 未推送、未合并；等待总监审查。
