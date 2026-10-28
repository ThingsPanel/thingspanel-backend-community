# tpctl-mcp

把 ThingsPanel 的自动化能力以 MCP（Model Context Protocol）工具的形式交给助手。

它不是第二套实现：每个工具打到的是网页和 `tpctl` 用的同一个端点，租户来自
进程启动时带的凭据，写工具带的幂等键和 `tpctl` 推导出来的完全一样。所以
"助手说它建了规则" 和 "网页上能看到这条规则" 是同一件事。

## 启动

```bash
go build -o tpctl-mcp .

TPCTL_BASE_URL=http://127.0.0.1:4000 \
TPCTL_TOKEN=<你的 token> \
./tpctl-mcp
```

走 stdio，按行分隔的 JSON-RPC 2.0。适合被宿主当子进程拉起，不适合直接手敲。

| 环境变量 | 含义 | 默认 |
| --- | --- | --- |
| `TPCTL_BASE_URL` | 服务地址 | `http://localhost:4000` |
| `TPCTL_TOKEN` | 凭据；租户由它决定 | 回退到 `TP_AUTH_TOKEN` |
| `TPCTL_TENANT` | 显式租户头，一般不用 | 空 |
| `TPCTL_TIMEOUT` | 单次请求超时 | `30s` |

## 协议

- 支持 `2024-11-05` / `2025-03-26` / `2025-06-18`；客户端报的版本在支持范围内就原样回。
- 没有 `id` 的是通知，**永不应答**。答了会让宿主把回复错配给另一个请求。
- `tools/list` 按名字排序且稳定，模型缓存工具表不会看到它抖动。
- `tools/call` 成功时同时给 `content`（文本）和 `structuredContent`（对象），
  两者是同一份字节，不可能互相矛盾。
- `tools/call` 失败时给 `isError: true` 的**工具结果**，不是协议错误。
  协议错误在很多宿主里显示成"服务器坏了"，而这里要表达的是"你的参数有问题，
  改这个字段"。

## 工具

23 个，与 `tpctl` 的命令一一对应。只读的带 `readOnlyHint: true`，删除类带
`destructiveHint: true`。

| 工具 | 端点 | 读写 |
| --- | --- | --- |
| `list_rules` / `get_rule` | `GET /v1/rules`、`/v1/rules/:id` | 读 |
| `validate_rule` | `POST /v1/rule-validation` | 读（只算不存） |
| `preview_rule_change` | `POST /v1/rules/:id/change-preview` | 读（只算不存） |
| `create_rule` / `update_rule` | `POST /v1/rules`、`PUT /v1/rules/:id` | 写 |
| `set_rule_enabled` / `delete_rule` | `POST .../enabled`、`DELETE /v1/rules/:id` | 写 |
| `simulate_rule` | `POST /v1/rules/:id/simulate` | 读（零副作用） |
| `explain_rule` / `list_runs` / `get_run` | `.../explain`、`.../runs`、`/v1/runs/:id` | 读 |
| `run_rule` | `POST /v1/rules/:id/run` | 写（会动设备） |
| `list_bindings` / `attach_binding` / `detach_binding` | `/v1/rules/:id/bindings`、`/v1/bindings/:id` | 读/写/写 |
| `device_automation` | `GET /v1/device-automation/:id` | 读 |
| `list_alarms` / `ack_alarm` | `GET /v1/alarms`、`POST /v1/alarms/:id/ack` | 读/写 |
| `list_blueprints` | `GET /v1/blueprints` | 读 |
| `export_bundle` / `preview_bundle` / `import_bundle` | `/v1/bundles/{export,preview,import}` | 读/读/写 |

## 三条硬规则

**租户不是参数。** 传 `tenant` / `tenant_id` / `tenantId` 会被直接拒绝并点名
这个字段，请求不会发出去。模型不能选自己在替哪个客户干活。

**只读工具不带幂等键。** 读两次不是问题。写工具带，而且键是从"工具名 + 参数"
推出来的：同一个意图重试，键不变；改了参数就是另一个意图。

**键同时放在 header 和 body 里。** header 给代理和日志看，body 给服务端看——
Encore 的类型化处理器读不到 header，只放 header 的键等于服务端从没收到过。

## 失败怎么读

工具结果里的 `error` 是 `tpctl` 同款信封：

```json
{"error": {"code": "invalid_argument", "app_code": "invalid_definition",
           "message": "params[0]: 参数 limit 已声明但正文中没有任何 {{limit}} 引用",
           "fields": [{"field": "params[0]", "code": "unused_param", ...}],
           "http_status": 400}}
```

`outcome_unknown` 是单独一档：写请求没拿到响应，**不能当作失败**。它带
`retryable: true` 和一个 hint，说清楚先查什么、重试时沿用哪个键。

## 测试

```bash
go test ./...                      # 协议与工具表，对着假后端
python3 ../test/automation-rewrite/mcp_smoke.py   # 对着真服务
```

冒烟脚本会验证：同一份非法定义经 HTTP、`tpctl`、MCP 三个入口报出**同样的字段**；
模拟不产生运行记录；同一个 `run_rule` 调用两次只产生一条运行；另一个租户读不到
这条规则。
