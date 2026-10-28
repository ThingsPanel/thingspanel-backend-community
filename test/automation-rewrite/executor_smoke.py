#!/usr/bin/env python3
"""A29 smoke test: the external executor contract over real HTTP.

The Go tests in `automation/a28_executor_test.go` cover behaviour. This script
covers the wire: that a plain HTTP client with a bearer token can register, be
refused with a readable field list, run a rule, be replayed safely, and be told
that a stale epoch or a stale policy revision is not acceptable.

It exists because "the sample works" is a claim about a real deployment, not
about a struct. The two engine samples in `samples/` send the same bodies; this
script is the part of them that can run without Node-RED or a Flink cluster.

Usage:
    cd encore-api && TP_AUTH_MODE=dev encore run --port 14000
    python3 test/automation-rewrite/executor_smoke.py

Environment:
    TPCTL_BASE_URL  default http://127.0.0.1:14000
    TPCTL_TOKEN     default a dev token in tenant e2e-a29
"""

import json
import os
import sys
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timedelta, timezone

BASE = os.environ.get("TPCTL_BASE_URL", "http://127.0.0.1:14000").rstrip("/")
TOKEN = os.environ.get("TPCTL_TOKEN", "a29-user:e2e-a29:tenant_admin:*")

# A fresh suffix every run: names are unique per tenant, and the idempotency keys
# below are derived from it, so a second run is a new conversation rather than a
# replay of the first one.
RUN = uuid.uuid4().hex[:8]
DEVICE = "dev-a29-exec-" + RUN
POLICY_KEY = "policy.a29." + RUN

PASS = 0
FAIL = 0


def check(name, ok, detail=""):
    global PASS, FAIL
    # Collapse whitespace: a pretty-printed JSON body in a one-line result makes
    # a passing run unreadable, which is how people stop reading the output.
    detail = " ".join(str(detail).split())[:120]
    if ok:
        PASS += 1
        print(f"  ok   {name}" + (f"  ({detail})" if detail else ""))
    else:
        FAIL += 1
        print(f"  FAIL {name}" + (f"  ({detail})" if detail else ""))
    return ok


def call(method, path, body=None, token=TOKEN):
    """Returns (status, parsed-json-or-None, raw-text)."""
    data = None
    headers = {"authorization": "Bearer " + token}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["content-type"] = "application/json"
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as res:
            raw = res.read().decode("utf-8")
            return res.status, (json.loads(raw) if raw else None), raw
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8")
        try:
            return e.code, json.loads(raw), raw
        except json.JSONDecodeError:
            return e.code, None, raw


def now_iso(offset_seconds=0):
    return (datetime.now(timezone.utc) + timedelta(seconds=offset_seconds)).strftime("%Y-%m-%dT%H:%M:%SZ")


def contract(device, outputs=("action_intent", "alarm_verdict")):
    return {
        "engine": "generic",
        "input_scope": {"device_ids": [device]},
        "outputs": list(outputs),
        "target_scope": {"device_ids": [device]},
        "max_targets": 5,
        "max_rate_per_min": 60,
    }


def fields_of(err):
    """The field list a caller branches on, as it arrives on the wire."""
    details = (err or {}).get("details") or {}
    return [f.get("field") for f in (details.get("fields") or [])]


def code_of(err):
    details = (err or {}).get("details") or {}
    return details.get("code")


def envelope(reg, seq, key, **extra):
    body = {
        "epoch": reg["epoch"],
        "seq": seq,
        "contract_hash": reg["contract_hash"],
        "issued_at": now_iso(),
        "expires_at": now_iso(60),
        "idempotency_key": key,
    }
    body.update(extra)
    return body


def automation_definition():
    """A one-step automation rule aimed at DEVICE.

    The device is not in this deployment's directory, so the run will be refused
    with `target_resolution_failed`. That is still the right test: the point here
    is that the intent reaches the *existing* run owner and leaves a run record,
    not that a device command went out.
    """
    return {
        "kind": "automation",
        "mode": "single",
        "scope": "device",
        "trigger": {"groups": [{"conditions": [{
            "source": "self", "signal": "telemetry", "key": "temperature",
            "operator": ">", "value": "30", "unit": "C",
        }]}]},
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [DEVICE]},
        "steps": [{"kind": "action", "action": "telemetry", "identifier": "switch", "value": "1"}],
    }


def alarm_definition(executor_id):
    return {
        "kind": "alarm_policy",
        "mode": "single",
        "scope": "device",
        "trigger": {"groups": [{"conditions": [{
            "source": "self", "signal": "telemetry", "key": "temperature",
            "operator": ">", "value": "30", "unit": "C",
        }]}]},
        "recovery": {"groups": [{"conditions": [{
            "source": "self", "signal": "telemetry", "key": "temperature",
            "operator": "<", "value": "25", "unit": "C",
        }]}]},
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [DEVICE]},
        "steps": [],
        "alarm": {
            "key": POLICY_KEY,
            "sources": ["event"],
            # The event channel is the only one an outside caller can reach, so it
            # is the only one that needs a whitelist. This line is what lets the
            # executor below qualify this policy and nothing else qualify it.
            "allowed_sources": [executor_id],
            "level": "H",
        },
    }


def main():
    print(f"\n== 0. 服务可达 ({BASE})")
    status, body, raw = call("GET", "/v1/executors?limit=1")
    if not check("GET /v1/executors", status == 200, f"HTTP {status}"):
        print(raw[:400])
        print("\n服务没起来：cd encore-api && TP_AUTH_MODE=dev encore run --port 14000")
        return 1

    print("\n== 1. 登记：空范围必须被拒，并且要说出是哪个字段")
    bad = contract(DEVICE)
    bad["target_scope"] = {}
    status, err, raw = call("POST", "/v1/executors", {"name": "a29-bad-" + RUN, "contract": bad})
    check("空 target_scope 被拒", status == 400, f"HTTP {status}")
    check("错误码是 executor_contract_invalid", code_of(err) == "executor_contract_invalid", str(code_of(err)))
    check("字段出进程：contract.target_scope", "contract.target_scope" in fields_of(err), str(fields_of(err)))

    print("\n== 2. 正式登记")
    status, reg, raw = call("POST", "/v1/executors", {"name": "a29-nodered-" + RUN, "contract": contract(DEVICE)})
    if not check("登记成功", status == 200, f"HTTP {status} {raw[:120]}"):
        return 1
    check("拿到 id / epoch / contract_hash",
          bool(reg.get("id")) and reg.get("epoch") == 1 and bool(reg.get("contract_hash")),
          f"epoch={reg.get('epoch')} hash={reg.get('contract_hash')}")
    check("登记即视为活着（healthy）", reg.get("healthy") is True, str(reg.get("healthy")))

    print("\n== 3. 同名不能登记两次")
    status, err, _ = call("POST", "/v1/executors", {"name": "a29-nodered-" + RUN, "contract": contract(DEVICE)})
    check("重名被拒 409", status == 409, f"HTTP {status}")
    check("错误码是 duplicate_name", code_of(err) == "duplicate_name", str(code_of(err)))

    print("\n== 4. intent：指向不存在的规则，被拒且带字段")
    key = f"a29-intent-missing-{RUN}"
    status, res, raw = call("POST", f"/v1/executors/{reg['id']}/intents",
                            envelope(reg, 1, key, rule_id="rule-does-not-exist", revision=1, device_id=DEVICE))
    check("200 且 accepted=false", status == 200 and res.get("accepted") is False, f"HTTP {status} {raw[:120]}")
    check("reason_code=rule_missing", res.get("reason_code") == "rule_missing", str(res.get("reason_code")))

    print("\n== 5. 同一个 key 重发：被拒的消息可以重发（没有被当成重放）")
    status, res2, _ = call("POST", f"/v1/executors/{reg['id']}/intents",
                           envelope(reg, 1, key, rule_id="rule-does-not-exist", revision=1, device_id=DEVICE))
    check("仍是 rule_missing 而不是 replayed", res2.get("reason_code") == "rule_missing", str(res2.get("reason_code")))
    check("replayed 为假", res2.get("replayed") in (False, None), str(res2.get("replayed")))

    print("\n== 6. intent：走到既有 run owner，运行记录必须存在")
    status, rule, raw = call("POST", "/v1/rules", {
        "name": "a29 外部意图规则 " + RUN, "definition": automation_definition(), "enabled": True})
    if not check("规则创建成功", status == 200, f"HTTP {status} {raw[:200]}"):
        return 1
    ikey = f"a29-intent-{RUN}"
    status, ires, raw = call("POST", f"/v1/executors/{reg['id']}/intents",
                             envelope(reg, 2, ikey, rule_id=rule["id"], revision=rule["revision"], device_id=DEVICE))
    check("200 且 accepted=true", status == 200 and ires.get("accepted") is True, f"HTTP {status} {raw[:120]}")
    check("拿到 run_id", bool(ires.get("run_id")), str(ires.get("run_id")))
    status, runs, _ = call("GET", f"/v1/rules/{rule['id']}/runs")
    run_ids = [r.get("id") for r in (runs or {}).get("runs", [])]
    check("运行记录里能找到这条 intent 的运行", ires.get("run_id") in run_ids, str(run_ids))

    print("\n== 7. 建一条只接受这个执行者的告警策略")
    status, policy, raw = call("POST", "/v1/rules", {
        "name": "a29 外部判定策略 " + RUN,
        "definition": alarm_definition(reg["id"]),
        "enabled": True,
    })
    if not check("策略创建成功", status == 200, f"HTTP {status} {raw[:200]}"):
        return 1
    check("revision=1", policy.get("revision") == 1, str(policy.get("revision")))

    print("\n== 8. verdict：合法判定进入既有告警 owner")
    vkey = f"a29-verdict-{RUN}"
    verdict = envelope(reg, 3, vkey, policy_id=policy["id"], policy_revision=1,
                       mode="raw", observed=True, subject_id=DEVICE, action_kind="qualified")
    status, vres, raw = call("POST", f"/v1/executors/{reg['id']}/verdicts", verdict)
    check("200 且 accepted=true", status == 200 and vres.get("accepted") is True, f"HTTP {status} {raw[:120]}")
    check("outcome=qualified", vres.get("outcome") == "qualified", str(vres.get("outcome")))
    check("开出了告警身份", bool(vres.get("episode_id")), str(vres.get("episode_id")))

    print("\n== 9. 同一个 key 重发判定：同一条判定，不是第二次告警")
    status, vres2, _ = call("POST", f"/v1/executors/{reg['id']}/verdicts", verdict)
    check("replayed=true", vres2.get("replayed") is True, str(vres2.get("replayed")))
    check("仍是同一个 episode", vres2.get("episode_id") in (None, vres.get("episode_id")),
          f"{vres2.get('episode_id')} vs {vres.get('episode_id')}")

    print("\n== 10. 新 key 但序号不前进：重放被拒")
    stale = envelope(reg, 3, f"a29-verdict-stale-{RUN}", policy_id=policy["id"], policy_revision=1,
                     mode="raw", observed=True, subject_id=DEVICE, action_kind="qualified")
    status, sres, _ = call("POST", f"/v1/executors/{reg['id']}/verdicts", stale)
    check("accepted=false", sres.get("accepted") is False, str(sres.get("accepted")))
    check("reason_code=replayed_sequence", sres.get("reason_code") == "replayed_sequence", str(sres.get("reason_code")))

    print("\n== 11. 旧代际：登记换代后旧部署的消息不再受理")
    status, err, raw = call("POST", "/v1/executors", {
        "name": "a29-nodered-v2-" + RUN, "contract": contract(DEVICE)})
    old_epoch = dict(verdict)
    old_epoch["epoch"] = reg["epoch"] + 7
    old_epoch["seq"] = 99
    old_epoch["idempotency_key"] = f"a29-epoch-{RUN}"
    status, eres, _ = call("POST", f"/v1/executors/{reg['id']}/verdicts", old_epoch)
    check("accepted=false", eres.get("accepted") is False, str(eres.get("accepted")))
    check("reason_code=superseded_epoch", eres.get("reason_code") == "superseded_epoch", str(eres.get("reason_code")))

    print("\n== 12. 旧策略版本：上周的阈值不能开今天的告警")
    old_policy = dict(verdict)
    old_policy["seq"] = 100
    old_policy["policy_revision"] = 0
    old_policy["idempotency_key"] = f"a29-oldrev-{RUN}"
    status, ores, _ = call("POST", f"/v1/executors/{reg['id']}/verdicts", old_policy)
    check("reason_code=stale_policy_revision", ores.get("reason_code") == "stale_policy_revision",
          str(ores.get("reason_code")))

    print("\n== 13. raw 不能冒充已满足持续时长（策略要求 0 秒时不适用，这里验证字段语义）")
    mixed = dict(verdict)
    mixed["seq"] = 101
    mixed["idempotency_key"] = f"a29-mixed-{RUN}"
    mixed["since"] = now_iso(-30)
    status, mres, _ = call("POST", f"/v1/executors/{reg['id']}/verdicts", mixed)
    check("raw 带 since 被拒", mres.get("reason_code") == "mixed_verdict_semantics", str(mres.get("reason_code")))

    print("\n== 14. 消息流水：被拒的和被受理的都在")
    status, log, raw = call("GET", f"/v1/executors/{reg['id']}/messages?limit=50")
    check("流水可读", status == 200, f"HTTP {status}")
    keys = {m["idempotency_key"]: m for m in (log or {}).get("messages", [])}
    check("被拒的消息留档", keys.get(key, {}).get("outcome") == "denied", str(keys.get(key, {}).get("outcome")))
    check("被受理的判定留档", keys.get(vkey, {}).get("outcome") == "qualified", str(keys.get(vkey, {}).get("outcome")))
    check("拒绝原因可查", keys.get(key, {}).get("reason_code") == "rule_missing",
          str(keys.get(key, {}).get("reason_code")))

    print("\n== 15. 列表：合同摘要与健康")
    status, listing, _ = call("GET", "/v1/executors?limit=100")
    me = next((e for e in (listing or {}).get("executors", []) if e["id"] == reg["id"]), None)
    check("在列表里", me is not None)
    if me:
        check("摘要说明能产出什么", "action_intent" in me.get("summary", ""), me.get("summary"))
        check("健康", me.get("healthy") is True, str(me.get("healthy")))

    print(f"\n{PASS} passed, {FAIL} failed")
    return 0 if FAIL == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
