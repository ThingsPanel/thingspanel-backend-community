# tpctl

ThingsPanel 自动化的命令行入口。它只做一件事：**把同一条 HTTP 接口包成命令**。
没有本地执行内核，没有第二套校验，也没有另一份凭据管理——`tpctl` 里看到的规则、
预览、运行记录，就是网页上看到的那一份。

```
tpctl [全局参数] <组> <动作> [参数] [--标志]
```

## 为什么是"扩展"而不是"新写"

Kernel 里原有 `cli/tpctl-go`，它的 `automations create/list/switch/delete` 是很多
实施脚本和运维手册里的既定动作。本模块是它的迁移落点：

| 旧命令 | 现在的等价写法 | 说明 |
| --- | --- | --- |
| `tpctl automations create --file r.json` | `tpctl rules create --file r.json` | 请求体、路径、校验完全一致 |
| `tpctl automations list` | `tpctl rules list` | 输出结构一致 |
| `tpctl automations switch <id> [--off]` | `tpctl rules enable\|disable <id>` | 同一端点 `POST /v1/rules/:id/enabled` |
| `tpctl automations delete <id>` | `tpctl rules delete <id>` | 被引用时同样 412 并点名 |

旧名保留为**别名**（`tpctl legacy` 打印映射表），它们和新名走同一个 `command` 表项，
所以不存在"旧命令行为漂移"的可能。旧名不带新参数（`--version` / `--force` /
`--preview-token`），需要这些能力时改用新名。

## 退出码

脚本判断结果不该读中文，所以退出码是契约的一部分：

| 码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 服务端拒绝，且无法归类 |
| 2 | 命令行本身写错了（未知标志、缺参数、文件读不了） |
| 3 | 不存在 |
| 4 | 参数或校验失败 |
| 5 | 未认证 / 无权限 |
| 6 | 冲突（版本冲突、预览过期、已存在、导入需 resume） |
| 7 | **结果未知**：请求没拿到响应，可能已经生效 |
| 8 | 服务不可用或超时 |

`7` 是刻意与 `1` 分开的。一次写入超时之后，唯一诚实的说法是"不知道"，而不是
"失败了"——说成失败会让人再点一次，然后灯开了两次。

## JSON

stdout 永远是同一个信封，失败时也是（`| jq` 因此可以无条件使用）：

```json
{
  "ok": false,
  "command": "rules.update",
  "request_id": "tpctl-1793009580031307000-b4eccb2e",
  "error": {
    "code": "failed_precondition",
    "app_code": "preview_stale",
    "message": "预览与当前内容不再一致，请重新预览再保存",
    "fields": [{"field": "bindings", "code": "binding_incompatible", "message": "…"}],
    "http_status": 412,
    "exit_code": 6,
    "retryable": true
  }
}
```

- `code` 是 gRPC 风格的大类，`app_code` 是我们自己的细分码（`preview_stale`、
  `binding_incompatible`、`referenced_by_rules`、`import_not_resumed`…）。
- `fields` 指出**哪个输入**错了，表单可以直接标红，脚本不必解析句子。
- 人读的部分同时在 stderr，所以交互使用不会只看到 JSON。
- `--raw` 只输出服务端原始 body，`--compact` 输出单行。

## 幂等

会改变状态的命令自动带 `Idempotency-Key`，键由**命令 + 请求体**推导（`sha256`
前 16 位）。同一个意图重试就是同一个键，所以超时后重跑不会开两次灯。
`--idempotency-key` 可以钉死一个键，供脚本跨不同形态的调用复用。

读命令不带键：一次读重试两次没有副作用，加个头只是噪音。

## 常用

```bash
# 发现能力
tpctl blueprints list

# 先校验、再预览、最后带令牌保存
tpctl rules validate --file rule.json
tpctl rules preview  <id> --file rule.json
tpctl rules update   <id> --file rule.json --version 3 --preview-token pv1:...

# 模拟（无副作用）与真实执行是两条命令、两个端点
tpctl rules simulate <id> --samples samples.json
tpctl rules run      <id> --device dev-1

# 排障
tpctl rules runs   <id> --status failed,expired --limit 20
tpctl runs  get    <runId>
tpctl rules explain <id> --device dev-1 --from 2026-10-01T00:00:00Z

# 资源包
tpctl bundles export --rules r1,r2 --key door-left-open --version 1 > pkg.json
tpctl bundles preview --file pkg.json --role door=dev-1,dev-2 --slot on-call=pol-1
tpctl bundles import  --file pkg.json --role door=dev-1,dev-2 --slot on-call=pol-1
```

## 环境

| 变量 | 作用 |
| --- | --- |
| `TPCTL_BASE_URL` | 服务地址（默认 `http://localhost:4000`） |
| `TPCTL_TOKEN` | 访问令牌（也接受 `TP_AUTH_TOKEN`） |
| `TPCTL_TENANT` | 租户（可选） |

## 测试

```bash
go test ./...                                   # 20 个用例，对 httptest 服务端断言契约
bash ../../test/automation-rewrite/tpctl_smoke.sh   # 27 项，对真实服务端
```

冒烟脚本需要一个以 `TP_AUTH_MODE=dev` 启动的实例：

```bash
cd encore-api && TP_AUTH_MODE=dev encore run --port 14000
```

单元测试断言的是"命令发出去了什么"（路径、方法、请求体、幂等键、退出码）；
冒烟脚本断言的是"真实服务端怎么答"。两者缺一不可：启停端点用错动词时，单元测试
看不出问题，真实服务返回的是 404 `endpoint not found`——看起来像"规则不存在"。
