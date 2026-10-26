# Node-RED 样例

一个 16 个节点的 flow，三条流：**登记** / **发一条 intent** / **发一条判定**。

```
inject → function（构造请求）→ http request → function（读懂回答）→ debug
```

## 导入与配置

1. Node-RED 菜单 → Import → 选 `flows.json` → Import。
2. 给 Node-RED 进程加环境变量（`settings.js` 的 `functionExternalModules` 或启动环境都行）：

| 变量 | 说明 |
| --- | --- |
| `TP_BASE` | 平台地址，默认 `http://127.0.0.1:14000` |
| `TP_TOKEN` | 调用方令牌。**必填**，function 节点里没有它就直接 return null |
| `TP_DEVICE` | 被驱动的设备 id。它同时进 `input_scope` 和 `target_scope` |
| `TP_RULE_ID` / `TP_RULE_REVISION` | ② 要执行的规则与版本 |
| `TP_POLICY_ID` / `TP_POLICY_REVISION` | ③ 要判定的告警策略与版本 |

3. 依次点 ① → ②（或 ③），看 debug 面板。

## flows.json 是生成的

手写这个文件意味着把每一段 JS 塞进 JSON 字符串里再转义一遍。所以：

```bash
python3 samples/node-red/build_flow.py
```

生成的同时做四项检查，任何一项不过就退出非零：

1. 节点 id 唯一；
2. 每条 wire 指向真实存在的节点；
3. 每个 `http request` 上游都有节点在设 `msg.url` 和鉴权头——少了它请求会打到一个空地址，报回来的 404 读起来像"接口没了"；
4. 每段 function 的 JS 用 `node` 解析一遍。

第 4 项抓过一个真错：`wires` 写成 `["f1"]` 而不是 `[["f1"]]` 时，Node-RED 会把字符串当输出列表逐字符走，导入后**点任何按钮都没有反应**，也不报错。

## 为什么这么写

**① 登记**是唯一一次"声明权限"。`target_scope` 是白名单，**空表示什么都不允许**——少写一个字段，登记会被拒，而且拒绝里带着 `contract.target_scope` 这个字段名。这是故意的：一个漏了字段的登记如果被当成"全部允许"，就是一次没人发现的越权。

登记的回答里 `epoch` 与 `contract_hash` 被存进 flow context。**合同登记后不可改**，改范围要重新登记、代际 +1，旧部署发来的消息会被拒（`superseded_epoch`）。所以这两个值必须跟着每条消息走，不能"重新读一次"。

**② intent** 只带 `rule_id` + `revision` + `device_id`，**不带步骤**。允许外部引擎自带步骤就会出现第二条执行路径，两条路径同时拥有同一台设备——`single`/`restart`/`queued`/`parallel` 各管一半，`single` 模式下"已有一条在跑"的判断会同时失效。这里让平台既有的运行与命令 owner 决定一切。

**③ verdict** 带 `policy_revision`。这条字段是整条通道存在的理由：**一个部署在上周阈值上的作业，不能对今天的策略下判定**。`mode: "raw"` 表示"现在为真"，持续时长由平台按已发布的 `for` 自己算；想让引擎自己算就发 `qualified`，那时 `since` 必填，而且平台会**重算**而不是相信标签。

**读懂回答**那个 function 节点不是装饰。`accepted: false` 的 body 里带 `issues[].field`，照着改就行；`replayed_sequence` 说明序号没有前进，该换新 key 而不是原样重发。只看 HTTP 状态码的 flow 学不到这两件事——它会把"被拒绝"和"已执行"都当成 200。

## 关于序号

`seq` 存在 flow context 里，每次 +1，`idempotency_key` 由 `executor id + seq` 组成。**重试要复用同一个 key**：换 key 的重试在平台看来就是一次新的物理动作。

Node-RED 重启后 flow context 会丢，`seq` 从 1 开始，于是第一条消息会拿到 `replayed_sequence`。生产里应该把 `seq` 存到 context storage（`contextStorage` 配 file 或 redis），或者用一个时间戳基线——`samples/flink` 里就是这么做的。

## 这一版没验证到的

- **没有在真 Node-RED 里跑过**：本机没装 Node-RED。flow 里的请求体与 `test/automation-rewrite/executor_smoke.py` 发的是同一组字段，冒烟脚本对着真服务 33 项全过，所以"请求对不对"有证据；"Node-RED 里点一下会不会通"没有证据，第一次用请先点 ① 看登记结果。
- flow 只演示 intent 与 verdict 两条路，没有演示心跳（`POST /v1/executors/:id/heartbeat`）和消息流水（`GET /v1/executors/:id/messages`）。两者都是单次 GET/POST，加一个 http request 节点即可。
