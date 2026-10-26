#!/usr/bin/env python3
"""Generate samples/node-red/flows.json.

The flow's JavaScript lives in function nodes, so writing the file by hand means
escaping every newline and quote inside a JSON string. Generating it keeps the
JSON valid and the JS readable in this file.

Run from the repo root:
    python3 samples/node-red/build_flow.py
"""

import json
import os
import subprocess

HERE = os.path.dirname(os.path.abspath(__file__))

# Every function node sets the same two things before it returns: the URL it
# wants to hit and the auth header. Doing it per node keeps each one readable on
# its own — a shared "prep" subflow would hide the part a reader needs to copy.
PRELUDE = r"""
const base = (env.get("TP_BASE") || "http://127.0.0.1:14000").replace(/\/+$/, "");
const token = env.get("TP_TOKEN");
if (!token) { node.warn("TP_TOKEN 未设置：请在 settings.js 的 functionExternalModules/环境变量里给出"); return null; }
const device = env.get("TP_DEVICE") || "demo-device-1";
const auth = { authorization: "Bearer " + token, "content-type": "application/json" };
"""

BUILD_REGISTER = PRELUDE + r"""
// 登记是唯一一次"声明权限"的动作：scope 是白名单，空表示什么都不允许，
// 所以 device 必须显式写进去。engine 只是记录，不改变语义。
msg.url = base + "/v1/executors";
msg.headers = auth;
msg.payload = {
    name: "node-red-" + device,
    contract: {
        engine: "node_red",
        input_scope: { device_ids: [device] },
        outputs: ["action_intent", "alarm_verdict"],
        target_scope: { device_ids: [device] },
        max_targets: 5,
        max_rate_per_min: 60
    }
};
return msg;
"""

KEEP_REGISTRATION = r"""
// 合同一旦登记就不可改：改范围要重新登记、代际 +1，旧部署的消息会被拒。
// 所以这里把 epoch 和 contract_hash 一起记住，后面每条消息都要带上。
if (!msg.payload || !msg.payload.id) {
    node.warn("登记失败：" + JSON.stringify(msg.payload));
    msg.payload = { ok: false, answer: msg.payload };
    return msg;
}
flow.set("executor", msg.payload);
msg.payload = {
    ok: true,
    id: msg.payload.id,
    epoch: msg.payload.epoch,
    contract_hash: msg.payload.contract_hash,
    summary: msg.payload.summary,
    healthy: msg.payload.healthy
};
return msg;
"""

# The envelope is the part a reader must copy. Sequence is kept in flow context
# so a retry of the same intent reuses its key — changing the key turns a retry
# into a second physical action.
ENVELOPE = r"""
const reg = flow.get("executor");
if (!reg) { node.warn("先跑 ① 登记"); return null; }
const seq = (flow.get("seq") || 0) + 1;
flow.set("seq", seq);
const now = Date.now();
"""

BUILD_INTENT = PRELUDE + ENVELOPE + r"""
msg.url = base + "/v1/executors/" + reg.id + "/intents";
msg.headers = auth;
msg.payload = {
    epoch: reg.epoch,
    seq: seq,
    contract_hash: reg.contract_hash,
    issued_at: new Date(now).toISOString(),
    expires_at: new Date(now + 60000).toISOString(),
    idempotency_key: "nr-intent-" + reg.id + "-" + seq,
    // intent 只点名"执行哪条已发布规则的哪一版"，不带步骤：
    // 让外部引擎自带步骤就会出现第二条执行路径，两条路径同时拥有同一台设备。
    rule_id: env.get("TP_RULE_ID"),
    revision: Number(env.get("TP_RULE_REVISION") || 1),
    device_id: device
};
return msg;
"""

BUILD_VERDICT = PRELUDE + ENVELOPE + r"""
msg.url = base + "/v1/executors/" + reg.id + "/verdicts";
msg.headers = auth;
msg.payload = {
    epoch: reg.epoch,
    seq: seq,
    contract_hash: reg.contract_hash,
    issued_at: new Date(now).toISOString(),
    expires_at: new Date(now + 60000).toISOString(),
    idempotency_key: "nr-verdict-" + reg.id + "-" + seq,
    policy_id: env.get("TP_POLICY_ID"),
    policy_revision: Number(env.get("TP_POLICY_REVISION") || 1),
    // raw = "现在为真"，持续时长由平台按发布值自己算。
    // 想让引擎自己算就发 qualified，并且必须带 since，平台会重算而不是相信标签。
    mode: "raw",
    observed: true,
    subject_id: device,
    action_kind: "qualified"
};
return msg;
"""

# Turning the answer into one line is the whole point of the sample: `denied`
# carries `fields`, and `replayed_sequence` means "do not resend, change the key
# or stop". A flow that only looks at the HTTP status learns none of that.
READ_ANSWER = r"""
const a = msg.payload || {};
if (a.accepted === true) {
    msg.payload = a.replayed
        ? { 结果: "已受理（重投，未再次执行）", run_id: a.run_id, status: a.status, outcome: a.outcome }
        : { 结果: "已受理", run_id: a.run_id, status: a.status, outcome: a.outcome };
    return msg;
}
const fields = (a.issues || []).map(function (i) { return i.field + ": " + i.message; }).join("\n");
if (a.reason_code === "replayed_sequence") {
    msg.payload = { 结果: "序号没有前进：这是重放或乱序", 该做什么: "不要重发；换新的 idempotency_key 并把 seq +1", 详情: fields };
} else {
    msg.payload = { 结果: "被拒绝", reason_code: a.reason_code, 该做什么: "按 fields 改请求再发，不要原样重试", 详情: fields };
}
return msg;
"""


def node(**kw):
    kw.setdefault("z", "tab-a29")
    return kw


def inject(nid, label, x, y, wire):
    return node(id=nid, type="inject", name=label, props=[{"p": "payload"}], repeat="",
                crontab="", once=False, onceDelay=0.1, topic="", payload="",
                payloadType="date", x=x, y=y, wires=[[wire]])


def function(nid, label, code, x, y, wire):
    return node(id=nid, type="function", name=label, func=code, outputs=1, timeout=0,
                noerr=0, initialize="", finalize="", libs=[], x=x, y=y, wires=[[wire]])


def http(nid, label, x, y, wire):
    return node(id=nid, type="http request", name=label, method="POST", ret="obj",
                paytoqs="ignore", url="", tls="", persist=False, proxy="",
                insecureHTTPParser=False, authType="", senderr=False, headers=[],
                x=x, y=y, wires=[[wire]])


def debug(nid, label, x, y):
    return node(id=nid, type="debug", name=label, active=True, tosidebar=True,
                console=False, tostatus=False, complete="payload", targetType="msg",
                statusVal="", statusType="auto", x=x, y=y, wires=[])


flow = [
    {"id": "tab-a29", "type": "tab",
     "label": "ThingsPanel 自动化 · 外部执行器最小样例",
     "disabled": False,
     "info": "先跑 ①，再跑 ② 或 ③。环境变量：TP_BASE / TP_TOKEN / TP_DEVICE / TP_RULE_ID / "
             "TP_RULE_REVISION / TP_POLICY_ID / TP_POLICY_REVISION。"},

    inject("i1", "① 登记执行者", 160, 80, "f1"),
    function("f1", "构造登记体", BUILD_REGISTER, 380, 80, "h1"),
    http("h1", "POST /v1/executors", 620, 80, "f2"),
    function("f2", "记住 id / epoch / hash", KEEP_REGISTRATION, 880, 80, "d1"),
    debug("d1", "登记结果", 1120, 80),

    inject("i2", "② 发一条 intent", 160, 220, "f3"),
    function("f3", "构造 intent", BUILD_INTENT, 380, 220, "h2"),
    http("h2", "POST /v1/executors/:id/intents", 620, 220, "f4"),
    function("f4", "读懂回答", READ_ANSWER, 900, 220, "d2"),
    debug("d2", "intent 结果", 1120, 220),

    inject("i3", "③ 发一条判定", 160, 360, "f5"),
    function("f5", "构造 verdict", BUILD_VERDICT, 380, 360, "h3"),
    http("h3", "POST /v1/executors/:id/verdicts", 620, 360, "f6"),
    function("f6", "读懂回答", READ_ANSWER, 900, 360, "d3"),
    debug("d3", "verdict 结果", 1120, 360),
]

with open(os.path.join(HERE, "flows.json"), "w", encoding="utf-8") as fh:
    json.dump(flow, fh, ensure_ascii=False, indent=2)
    fh.write("\n")


def validate(nodes):
    """Structural checks a broken flow would fail silently on.

    Node-RED happily imports a flow whose wires point at nothing: the tab just
    looks wrong and a click does nothing. Since the flow cannot be clicked in CI,
    these are the parts that can be checked.
    """
    problems = []
    ids = [n["id"] for n in nodes]
    if len(ids) != len(set(ids)):
        problems.append("node ids are not unique")

    known = set(ids)
    for n in nodes:
        for wire in n.get("wires", []):
            for target in wire:
                if target not in known:
                    problems.append(f"{n['id']} wires to missing node {target}")

    # Every http request must be preceded by something that sets msg.url and the
    # auth header, otherwise it posts to an empty URL and reports a 404 that
    # reads like "the endpoint is gone".
    for n in nodes:
        if n["type"] != "http request":
            continue
        sources = [m for m in nodes if n["id"] in [t for w in m.get("wires", []) for t in w]]
        if not sources or not any("msg.url" in m.get("func", "") for m in sources):
            problems.append(f"{n['name']} has no upstream node that sets msg.url")
        if not sources or not any("authorization" in m.get("func", "") for m in sources):
            problems.append(f"{n['name']} has no upstream node that sets the auth header")

    # The function bodies are JavaScript. A syntax error there only shows up when
    # the flow is clicked, so parse each one here.
    for n in nodes:
        if n["type"] != "function":
            continue
        proc = subprocess.run(
            ["node", "-e", "new Function(require('fs').readFileSync(0,'utf8'))"],
            input=n["func"], text=True, capture_output=True)
        if proc.returncode != 0:
            problems.append(f"{n['name']}: {proc.stderr.strip().splitlines()[-1] if proc.stderr else 'parse error'}")
    return problems


problems = validate(flow)
for p in problems:
    print("  FAIL", p)
if problems:
    raise SystemExit(1)

print("wrote flows.json with %d nodes (%d function nodes parsed by node)"
      % (len(flow), sum(1 for n in flow if n["type"] == "function")))

