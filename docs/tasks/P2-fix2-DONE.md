# P2-fix2 完成说明：补齐凭据脱敏递归边界

## 分支与范围

- 基线：本地 `develop@5c926033`。
- 分支：`wp/p2-fix2-form-brackets`；worktree 已登记在 `docs/WORKSPACES.md`。
- 本任务只修复操作日志/访问日志中的方括号字段名和嵌套凭据值泄漏。

## 修改

- assignment scanner 把原样方括号字段名作为整体判定，覆盖 `voucher[]`、`voucher[code]`、`user[voucher]`、`plugin[voucher][id]`。
- `Text` 递归检查 JSON 字符串、表单字段和至多四层 URL 编码的嵌套值；`URL` 也检查非敏感 query 参数中的嵌套 JSON、URL 和 token 参数。覆盖 `payload=%7B%22voucher%22...`、`return_to=https%3A...%3Ftoken...` 及内嵌转义 JSON。
- 嵌套扫描使用每次调用独立的预算，最多处理 1 MiB 输入、4 MiB 累计递归内容、8 层嵌套和 16,384 个 assignment/query 字段。超过边界时 fail closed：超大文本/URL 或超多 URL query 字段整体返回 `***`，超字段数时遮住未处理尾部，过深/过度编码值遮住该值，畸形百分号编码值也遮住。
- 对无敏感内容的普通字段保留原文，包括 `filter[code]`、`user[name]`、普通嵌套 JSON 和 `+`。优化无敏感文本的快路径；逐字段处理，不保留全量匹配索引。
- middleware 回归使用实际 `AccessLogger`、`OperationLogs` 和 SQLite 内存库。检查日志输出、操作日志正文与访问 URL 中的嵌套凭据均不可见；同时断言 handler 收到的请求正文逐字节不变。

## 验证

通过：

```text
GOCACHE=/tmp/p2-fix2-redact-gocache go test -race ./pkg/logredact ./internal/middleware ./third_party/others/http_client -count=1
git diff --check develop
```

Apple M1、近 1 MiB 输入、单次基准：

```text
BenchmarkTextLargeBody:                 1.84 ms/op, 568.40 MB/s, 984 B/op, 8 allocs/op
BenchmarkTextDenseShortSensitiveFields: 11.89 ms/op, 88.16 MB/s, 1,965,960 B/op, 16,444 allocs/op
```

密集字段扫描遇到第 16,385 个 assignment 后遮盖剩余文本；分配结果是全次调用累计分配，不是峰值驻留内存。未连接真实 PostgreSQL 或 Broker，也未做完整服务端端到端验证。测试使用任务独立 Go 缓存。

## 契约与限制

- HTTP/API 响应和实际请求内容不变；变化只影响日志表示，敏感内容以 `***` 遮盖。超过保护上限的日志文本可能被整体或部分遮盖，日志可读性会降低。
- 递归支持 JSON、常见 assignment/form/query 与最多四层百分号/query 编码；不会解码 Base64、自定义编码或未知二进制封装，无法对未知不透明格式中的凭据作保证。
- 尚不能据这些回归证明所有日志来源和所有编码形式均无泄漏。
- 未推送、未合并，等待独立审查。
