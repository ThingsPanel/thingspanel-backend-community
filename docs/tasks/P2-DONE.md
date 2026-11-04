# P2 完成说明：HA 令牌不再泄露到日志和 URL

分支：`wp/p2-voucher-leak`（基于 develop `09fe900a`）

## 做了什么
三条泄露路径，加一个通用脱敏包 `pkg/logredact`（字段名含 `voucher` / `token` / `password` / `secret`，不分大小写，即 `access_token`、`client_secret` 也算）：

1. **请求日志和操作日志表**：`internal/middleware/operations_log.go` 的请求体、响应体先脱敏，再打印并写入 `operation_logs`。JSON 按结构递归脱敏；请求体被截断到 10000 字节、已不是合法 JSON 时，退回正则脱敏，不会漏。
2. **设备列表拼 URL**：`third_party/others/http_client/procotol_plugin.go` 的 `GetServiceAccessDeviceList` 改用 `url.Values` 编码。插件收到的仍是解码后的 voucher，所以紧凑 JSON 继续可用，带空格或换行的 JSON 也不再出 400。
3. **错误日志**：`request_method.go` 的 `Get`、`Post`、`Delete` 和 `GetServiceAccessDeviceList` 不再打印 URL。`net/http` 的 `*url.Error` 自带完整 URL，用 `logredact.Error` 重建后再打印和返回。这些错误文本原本还会通过 105001 返回给前端，现在同样是脱敏后的。
4. 额外发现并修复：`gin.Default()` 的访问日志会把 `GET /device/list?voucher=…` 整条打印。改为 `middleware.AccessLogger()`（`internal/middleware/access_logger.go`），查询串和错误信息都脱敏，其他格式与 gin 默认一致。

## 验证
- `go test ./pkg/logredact ./internal/middleware ./third_party/others/http_client`：通过。
- 回归测试 `internal/api/service_access_voucher_leak_test.go`（真实 PG：容器 `p2-pg`，`postgres:16`，端口 57201，库名 `p2`）：
  - 运行方式：`P2_TEST_PG_DSN='postgres://postgres:p2pw@127.0.0.1:57201/p2?sslmode=disable' go test ./internal/api -run TestVoucherNeverLeaks`，未设置该变量时跳过。
  - 带一个假令牌走完三条链路：创建接入点（真实 handler + 操作日志中间件 + `operation_logs` 表）、设备列表成功（插件收到的 voucher 与发送的完全一致，含换行）、设备列表出错（插件端口拒绝连接）。
  - 检查 logrus 输出、gin 访问日志、`operation_logs` 表、HTTP 响应，原文和 URL 编码两种形式的令牌出现次数都必须为 0：通过。
  - 对照：把操作日志中间件和 `http_client` 恢复为 develop 版本，同一测试失败，响应里带出了完整 URL。
- 我没有把令牌 grep 到真实运行的栈上，这是单元和集成测试，不是端到端验收。

## 已知情况
- `internal/api` 里 `TestTelemetryHTTPDeviceAuthorization/owner_key` 在 develop 上也失败（code=40103），与本卡无关，没有处理。
- 容器 `p2-pg` 还在运行，等审查通过后由负责人确认再删。

## 没做
- 数据库里 `service_access.voucher` 仍是明文：这是 P2b，需要先出设计。测试里也断言了它仍被原样保存。
- `GET /device/list` 仍通过 query 传 voucher 给前端，按卡片要求只脱敏日志。改成 POST 需要前端配合，没有做。
- 没有登记 `docs/WORKSPACES.md`：本 worktree 是任务启动时创建的，登记请负责人决定。
- 设备（`devices.voucher`）等其他表的类似日志没有逐一排查；`logredact` 已是通用工具，其他调用点可复用。
