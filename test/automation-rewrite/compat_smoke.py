#!/usr/bin/env python3
"""A30 smoke test: the legacy compatibility adapter against a real automation service.

The Go tests in `internal/automationcompat` cover translation and refusals with a
fake upstream. This script covers the thing a fake cannot: that the requests the
App and the console actually send come back in the shape those clients parse,
from a real process talking to a real service, with a real run and a real alarm
episode behind them.

It replays the exact call sites found in the consumer ledger
(docs/A30-consumer-ledger-2026-10-26.md):

  App       app/pages/automation/index.vue:258   POST /api/v1/scene/active/:id
  App       app/pages/automation/index.vue:246   POST /api/v1/scene_automations/switch/:id
  App       app/pages/dashboard/index.vue:182    GET  /api/v1/scene_automations/list?page_size=1
  App       app/pages/automation/scene.vue:157   GET  /api/v1/scene/detail/:id
  App       app/pages/alarms/index.vue:177       GET  /api/v1/alarm/info
  Console   views/automation/scene-manage:48     POST /api/v1/scene/active/:id
  Console   views/automation/scene-manage:64     GET  /api/v1/scene

Usage:
    cd encore-api && TP_AUTH_MODE=dev encore run --port 14001
    go run ./cmd/automation-compat --addr :14002 --core http://127.0.0.1:14001 \\
        --sunset 2027-04-30
    python3 test/automation-rewrite/compat_smoke.py

Environment:
    COMPAT_BASE_URL  default http://127.0.0.1:14002
    COMPAT_CORE_URL  default http://127.0.0.1:14001
    COMPAT_TOKEN     default a dev token in tenant e2e-a30
"""

import json
import os
import sys
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timedelta, timezone

COMPAT = os.environ.get("COMPAT_BASE_URL", "http://127.0.0.1:14002").rstrip("/")
CORE = os.environ.get("COMPAT_CORE_URL", "http://127.0.0.1:14001").rstrip("/")
TOKEN = os.environ.get("COMPAT_TOKEN", "a30-user:e2e-a30:tenant_admin:*")

RUN = uuid.uuid4().hex[:8]
DEVICE = "dev-a30-" + RUN
SCENE_LEGACY = "scene-legacy-" + RUN
AUTO_LEGACY = "auto-legacy-" + RUN
POLICY_LEGACY = "policy-legacy-" + RUN
POLICY_KEY = "policy.a30." + RUN

APP_UA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0) uni-app HBuilderX"
WEB_UA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/129.0 Safari/537.36"

PASS = 0
FAIL = 0


def check(name, ok, detail=""):
    global PASS, FAIL
    detail = " ".join(str(detail).split())[:130]
    if ok:
        PASS += 1
        print(f"  ok   {name}" + (f"  ({detail})" if detail else ""))
    else:
        FAIL += 1
        print(f"  FAIL {name}" + (f"  ({detail})" if detail else ""))
    return ok


def http(method, url, body=None, ua=None):
    data = None
    headers = {"authorization": "Bearer " + TOKEN, "x-token": TOKEN}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["content-type"] = "application/json"
    if ua:
        headers["user-agent"] = ua
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as res:
            raw = res.read().decode("utf-8")
            return res.status, (json.loads(raw) if raw else None), dict(res.headers)
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8")
        try:
            return e.code, json.loads(raw), dict(e.headers)
        except json.JSONDecodeError:
            return e.code, None, dict(e.headers)


def raw_get(url):
    """GET that returns text, for the Prometheus exposition endpoint."""
    req = urllib.request.Request(url, headers={"authorization": "Bearer " + TOKEN})
    try:
        with urllib.request.urlopen(req, timeout=20) as res:
            return res.status, res.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8")


def core(method, path, body=None):
    status, parsed, _ = http(method, CORE + path, body)
    return status, parsed


def compat(method, path, body=None, ua=APP_UA):
    status, parsed, headers = http(method, COMPAT + path, body, ua)
    return status, parsed, headers


def now_iso(offset=0):
    return (datetime.now(timezone.utc) + timedelta(seconds=offset)).strftime("%Y-%m-%dT%H:%M:%SZ")


def scene_definition():
    return {
        "kind": "scene",
        "mode": "single",
        "scope": "rule",
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [DEVICE]},
        "steps": [{
            "kind": "action", "action": "telemetry", "identifier": "switch", "value": "1",
        }],
    }


def automation_definition():
    return {
        "kind": "automation",
        "mode": "single",
        "scope": "device",
        "trigger": {"groups": [{"conditions": [{
            "source": "self", "signal": "telemetry", "key": "temperature",
            "operator": ">", "value": "30",
        }]}]},
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [DEVICE]},
        "steps": [{
            "kind": "action", "action": "telemetry", "identifier": "switch", "value": "1",
        }],
    }


def alarm_definition(executor_id):
    return {
        "kind": "alarm_policy",
        "mode": "single",
        "scope": "device",
        "trigger": {"groups": [{"conditions": [{
            "source": "self", "signal": "telemetry", "key": "temperature",
            "operator": ">", "value": "30",
        }]}]},
        "recovery": {"groups": [{"conditions": [{
            "source": "self", "signal": "telemetry", "key": "temperature",
            "operator": "<", "value": "25",
        }]}]},
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [DEVICE]},
        "steps": [],
        "alarm": {
            "key": POLICY_KEY,
            "sources": ["event"],
            "allowed_sources": [executor_id],
            "level": "H",
        },
    }


def main():
    print(f"== 0. 环境 {COMPAT} -> {CORE}")

    status, health, _ = http("GET", COMPAT + "/healthz")
    if not check("适配器在线", status == 200, f"HTTP {status}"):
        return 1
    status, _ = core("GET", "/v1/health")
    if not check("自动化服务在线", status == 200, f"HTTP {status}"):
        return 1

    # ------------------------------------------------------------------
    # Fixtures: two migrated rules and one alarm policy, each carrying the
    # legacy id the old client still uses.
    # ------------------------------------------------------------------
    print("\n== 1. 造三个已迁移对象（带 legacy_id）")
    status, scene = core("POST", "/v1/rules", {
        "name": "门厅灯光 " + RUN,
        "definition": scene_definition(),
        "enabled": True,
        "legacy_id": SCENE_LEGACY,
    })
    if not check("场景规则创建成功", status == 200, json.dumps(scene, ensure_ascii=False)[:120] if status != 200 else scene.get("id")):
        return 1

    status, auto = core("POST", "/v1/rules", {
        "name": "有人经过 " + RUN,
        "definition": automation_definition(),
        "enabled": True,
        "legacy_id": AUTO_LEGACY,
    })
    if not check("场景联动规则创建成功", status == 200, json.dumps(auto, ensure_ascii=False)[:120] if status != 200 else auto.get("id")):
        return 1

    # A second automation, so the dashboard's page_size=1 really is a page of a
    # bigger set. With only one rule, total==len(list) and the bug this asserts
    # against would stay invisible.
    status, auto2 = core("POST", "/v1/rules", {
        "name": "第二台 " + RUN,
        "definition": automation_definition(),
        "enabled": True,
    })
    if not check("第二条联动规则创建成功", status == 200,
                 json.dumps(auto2, ensure_ascii=False)[:120] if status != 200 else auto2.get("id")):
        return 1

    status, reg = core("POST", "/v1/executors", {
        "name": "a30-" + RUN,
        "contract": {
            "engine": "generic",
            "input_scope": {"device_ids": [DEVICE]},
            "outputs": ["alarm_verdict"],
            "target_scope": {"device_ids": [DEVICE]},
            "max_targets": 5,
            "max_rate_per_min": 60,
        },
    })
    if not check("执行者登记成功", status == 200, json.dumps(reg, ensure_ascii=False)[:120] if status != 200 else reg.get("id")):
        return 1

    status, policy = core("POST", "/v1/rules", {
        "name": "高温告警 " + RUN,
        "definition": alarm_definition(reg["id"]),
        "enabled": True,
        "legacy_id": POLICY_LEGACY,
    })
    if not check("告警策略创建成功", status == 200, json.dumps(policy, ensure_ascii=False)[:120] if status != 200 else policy.get("id")):
        return 1

    print("\n== 2. 让这条策略真的开一个告警（外部判定）")
    status, verdict = core("POST", f"/v1/executors/{reg['id']}/verdicts", {
        "epoch": reg["epoch"],
        "seq": 1,
        "contract_hash": reg["contract_hash"],
        "issued_at": now_iso(),
        "expires_at": now_iso(60),
        "idempotency_key": f"a30-verdict-{RUN}",
        "policy_id": policy["id"],
        "policy_revision": policy["revision"],
        "policy_definition_hash": policy["definition_hash"],
        "mode": "raw",
        "observed": True,
        "subject_id": DEVICE,
        "action_kind": "qualified",
    })
    if not check("判定被受理", status == 200 and (verdict or {}).get("accepted"), json.dumps(verdict, ensure_ascii=False)[:120] if status != 200 else verdict):
        return 1

    # ------------------------------------------------------------------
    # The App
    # ------------------------------------------------------------------
    print("\n== 3. App：仪表盘数量（page_size=1，读 total）")
    status, body, headers = compat("GET", "/api/v1/compat/scene_automations/list?page=1&page_size=1")
    ok = check("返回旧信封 code=200", status == 200 and body.get("code") == 200,
               f"HTTP {status} code={(body or {}).get('code')}")
    data = (body or {}).get("data") or {}
    # The tenant accumulates rules across runs, so the assertion is the
    # invariant, not a number: with page_size=1 total must exceed the page.
    ok = check("total 是匹配总数而不是本页条数",
               data.get("total", 0) > len(data.get("list") or []) and data.get("total", 0) >= 2,
               f"total={data.get('total')} list={len(data.get('list') or [])}") and ok
    ok = check("本页只有一条", len(data.get("list") or []) == 1,
               f"{len(data.get('list') or [])} 条") and ok
    ok = check("响应带 Sunset 头", bool(headers.get("Sunset") or headers.get("sunset")),
               headers.get("Sunset") or headers.get("sunset")) and ok
    if not ok:
        return 1

    print("\n== 4. App：场景详情（旧编辑器读 actions）")
    status, body, _ = compat("GET", f"/api/v1/compat/scene/detail/{SCENE_LEGACY}")
    data = (body or {}).get("data") or {}
    actions = data.get("actions") or []
    if not check("详情返回 code=200", (body or {}).get("code") == 200, (body or {}).get("message")):
        return 1
    check("actions 里是旧的动作形状", bool(actions) and actions[0].get("action_type") == "10"
          and actions[0].get("action_param") == "switch",
          json.dumps(actions[0], ensure_ascii=False) if actions else "空")
    check("行按旧 id 寻址", data.get("id") == SCENE_LEGACY, data.get("id"))

    print("\n== 5. App / 面板：激活场景（拒绝时不许说成功）")
    # This environment has no device directory, so the runtime rejects the run:
    # the target cannot be resolved. That is exactly the case worth testing —
    # the old client shows a success toast for any code 200, so the adapter has
    # to answer with a business failure or the customer is told the relay moved
    # when nothing happened.
    before = core("GET", f"/v1/rules/{scene['id']}/runs?limit=50")[1]
    before_total = (before or {}).get("total", 0)
    status, body, _ = compat("POST", f"/api/v1/compat/scene/active/{SCENE_LEGACY}", {})
    code = (body or {}).get("code")
    if not check("激活没有回 200", code == 100464, f"code={code} {(body or {}).get('message')}"):
        return 1
    check("原因可读且说明没执行", "没有执行" in ((body or {}).get("message") or ""),
          (body or {}).get("message"))
    check("reason_code 可分支", ((body or {}).get("data") or {}).get("reason_code", "").startswith("run_"),
          (body or {}).get("data"))
    after = core("GET", f"/v1/rules/{scene['id']}/runs?limit=50")[1]
    check("新服务里确实记下了这次尝试", (after or {}).get("total", 0) == before_total + 1,
          f"{before_total} -> {(after or {}).get('total')}")

    print("\n== 6. App：启停开关（先读后写，带版本）")
    status, body, _ = compat("POST", f"/api/v1/compat/scene_automations/switch/{AUTO_LEGACY}", {})
    if not check("开关返回 code=200", (body or {}).get("code") == 200,
                 f"HTTP {status} {(body or {}).get('message')}"):
        return 1
    check("返回旧的 Y/N", ((body or {}).get("data") or {}).get("enabled") == "N",
          ((body or {}).get("data") or {}).get("enabled"))
    status, rule = core("GET", f"/v1/rules/{auto['id']}")
    check("新服务里的规则真的被关掉了", (rule or {}).get("enabled") is False, (rule or {}).get("enabled"))

    print("\n== 7. App：执行记录")
    status, body, _ = compat("GET",
                             f"/api/v1/compat/scene_automations/log?scene_automation_id={SCENE_LEGACY}&page=1&page_size=10")
    data = (body or {}).get("data") or {}
    rows = data.get("list") or []
    if not check("日志返回 code=200", (body or {}).get("code") == 200, (body or {}).get("message")):
        return 1
    check("日志里有刚跑的那一次", len(rows) >= 1, f"{len(rows)} 行")
    check("execution_result 是旧的两值/三值枚举", all(r.get("execution_result") in ("S", "F", "U") for r in rows),
          [r.get("execution_result") for r in rows])
    # The run above was rejected, so its log row must not read as success. The
    # old enum had only S/F; the adapter must not invent an S for a run whose
    # command was never confirmed — that is the fake-success defect A01 D6.
    real_runs = (core("GET", f"/v1/rules/{scene['id']}/runs?limit=50")[1] or {}).get("runs") or []
    if real_runs:
        real_status = real_runs[0].get("status")
        row = next((r for r in rows if r.get("id") == real_runs[0].get("id")), None)
        if row:
            check("运行没有成功时日志不说成功",
                  not (real_status != "succeeded" and row.get("execution_result") == "S"),
                  f"run={real_status} log={row.get('execution_result')}")

    print("\n== 8. App：告警卡与历史")
    status, body, _ = compat("GET", "/api/v1/compat/alarm/info?page=1&page_size=10")
    data = (body or {}).get("data") or {}
    rows = data.get("list") or []
    if not check("告警卡返回 code=200", (body or {}).get("code") == 200, (body or {}).get("message")):
        return 1
    check("活动告警里能看到刚才那一条", any(r.get("id") for r in rows), f"{len(rows)} 条")
    if rows:
        row = rows[0]
        check("alarm_status 是等级字母而不是布尔", row.get("alarm_status") in ("H", "M", "L", "N"),
              row.get("alarm_status"))
        check("告警名来自策略而不是空", bool(row.get("name")), row.get("name"))
        check("设备列表可导航", bool(row.get("alarm_device_list")), row.get("alarm_device_list"))

    status, body, _ = compat("GET", "/api/v1/compat/alarm/info/history?page=1&page_size=10")
    check("历史返回 code=200", (body or {}).get("code") == 200, (body or {}).get("message"))
    check("历史里没有活动告警（状态过滤生效）",
          all(r.get("alarm_status") == "N" for r in ((body or {}).get("data") or {}).get("list") or []),
          len(((body or {}).get("data") or {}).get("list") or []))

    print("\n== 9. 面板：场景列表")
    status, body, _ = compat("GET", "/api/v1/compat/scene?page=1&page_size=10", ua=WEB_UA)
    data = (body or {}).get("data") or {}
    rows = data.get("list") or []
    if not check("场景列表返回 code=200", (body or {}).get("code") == 200, (body or {}).get("message")):
        return 1
    check("列表里能按旧 id 找到刚建的那条", any(r.get("id") == SCENE_LEGACY for r in rows),
          f"{len(rows)} 条")
    check("行里有面板渲染的四个字段",
          all(k in (rows[0] if rows else {}) for k in ("name", "description", "created_at", "updated_at")),
          list((rows[0] if rows else {}).keys()))

    print("\n== 10. 没迁移的旧对象：说清楚，而不是假装成功")
    status, body, _ = compat("POST", "/api/v1/compat/scene/active/never-migrated-" + RUN, {})
    check("返回 404 语义的业务码", (body or {}).get("code") == 100404, (body or {}).get("code"))
    check("reason_code 可分支", ((body or {}).get("data") or {}).get("reason_code") == "legacy_ref_missing",
          (body or {}).get("data"))

    print("\n== 11. 调用量按客户端归集（退出条件的依据）")
    status, stats, _ = http("GET", COMPAT + "/api/v1/compat/stats")
    calls = (stats or {}).get("calls") or []
    consumers = {c.get("consumer") for c in calls}
    check("统计里同时有 app 与 web", {"app", "web"}.issubset(consumers), sorted(consumers))
    check("统计里能看出被拒的调用", any(c.get("outcome") == "not_found" for c in calls),
          sorted({c.get("outcome") for c in calls}))
    status, text = raw_get(COMPAT + "/api/v1/compat/metrics")
    check("Prometheus 文本可抓", status == 200 and "automation_compat_calls_total" in text,
          f"HTTP {status} {len(text)} 字节")

    print(f"\n{'='*60}\n通过 {PASS} / {PASS + FAIL}")
    return 0 if FAIL == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
