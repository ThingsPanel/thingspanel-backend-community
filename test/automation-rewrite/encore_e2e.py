#!/usr/bin/env python3
"""A03/A04 end-to-end proof for the Encore automation owner.

This script drives the *new* service over real HTTP and lets it call the *old*
community backend as the device-command owner. Nothing is mocked:

  1. the community backend is used to create a real device fixture and to accept
     the downlink command the automation produces;
  2. the automation service validates, stores, simulates, ingests and executes a
     rule through its public API;
  3. the run explain view shows what left the process and at what proof level.

Usage:
    python3 encore_e2e.py --community http://127.0.0.1:19999 \
        --automation http://127.0.0.1:14000
"""
from __future__ import annotations

import argparse
import base64
import json
import sys
import time
import urllib.error
import urllib.request

COMMUNITY_TOKEN: str | None = None
AUTOMATION_TOKEN = "e2e-user:unset:tenant_admin:*"


def jwt_tenant(token: str) -> str:
    """Read the tenant id out of the community JWT.

    The automation service must run under the *same* tenant identity as the
    community backend, otherwise the device directory legitimately hides the
    device and target resolution reports device_not_found.
    """
    payload = token.split(".")[1]
    payload += "=" * (-len(payload) % 4)
    return json.loads(base64.urlsafe_b64decode(payload))["tenant_id"]

FAILURES: list[str] = []


def http(base: str, method: str, path: str, body: dict | None = None,
         headers: dict | None = None) -> dict:
    url = base.rstrip("/") + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    # The sandbox exports HTTP_PROXY; localhost must bypass it.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req, timeout=60) as resp:
            raw = resp.read().decode()
            return json.loads(raw) if raw.strip() else {}
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode()
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return {"code": exc.code, "message": raw}


def community(path: str, method: str = "GET", body: dict | None = None) -> dict:
    h = {"x-token": COMMUNITY_TOKEN} if COMMUNITY_TOKEN else {}
    return http(COMMUNITY, method, path, body, h)


def automation(path: str, method: str = "GET", body: dict | None = None) -> dict:
    return http(AUTOMATION, method, path, body,
                {"Authorization": "Bearer " + AUTOMATION_TOKEN})


def check(name: str, ok: bool, detail: str = "") -> None:
    print(("  PASS  " if ok else "  FAIL  ") + name + (("  -> " + detail) if detail else ""))
    if not ok:
        FAILURES.append(name + ((": " + detail) if detail else ""))


def main() -> int:
    global COMMUNITY_TOKEN
    ap = argparse.ArgumentParser()
    ap.add_argument("--community", default="http://127.0.0.1:19999")
    ap.add_argument("--automation", default="http://127.0.0.1:14000")
    ap.add_argument("--email", default="tenant@tenant.cn")
    ap.add_argument("--password", default="123456")
    args = ap.parse_args()
    globals()["COMMUNITY"] = args.community.rstrip("/")
    globals()["AUTOMATION"] = args.automation.rstrip("/")

    print("== 1. health ==")
    health = automation("/v1/health")
    check("automation health", health.get("status") == "ok", json.dumps(health, ensure_ascii=False))

    print("== 2. community fixture (real device) ==")
    login = community("/api/v1/login", "POST", {"email": args.email, "password": args.password})
    if login.get("code") != 200:
        print("login failed:", login)
        return 2
    COMMUNITY_TOKEN = login["data"]["token"]
    tenant_id = jwt_tenant(COMMUNITY_TOKEN)
    globals()["AUTOMATION_TOKEN"] = f"e2e-user:{tenant_id}:tenant_admin:*"
    print("  community tenant =", tenant_id)

    suffix = str(int(time.time()))
    cfg = community("/api/v1/device_config", "POST", {
        "name": f"ENCORE-E2E-CFG-{suffix}",
        "device_type": "1",
        "protocol_type": "MQTT",
        "description": "encore automation e2e fixture",
    })
    cfg_id = (cfg.get("data") or {}).get("id")
    check("device_config created", cfg.get("code") == 200 and bool(cfg_id), str(cfg.get("message")))

    dev = community("/api/v1/device", "POST", {
        "name": f"ENCORE-E2E-DEV-{suffix}",
        "device_number": f"ENCORE-E2E-{suffix}",
        "device_config_id": cfg_id,
        "protocol": "MQTT",
        "label": "encore-e2e",
        "description": "encore automation e2e fixture",
    })
    device_id = (dev.get("data") or {}).get("id")
    check("device created", dev.get("code") == 200 and bool(device_id), str(dev.get("message")))
    if not device_id:
        return 2
    print("  device_id =", device_id)

    print("== 3. rule authoring ==")
    definition = {
        "kind": "automation",
        "mode": "single",
        "scope": "device",
        "trigger": {
            "groups": [{"conditions": [{
                "source": "self", "signal": "telemetry", "key": "temperature",
                "operator": ">", "value": "30", "unit": "℃",
            }]}],
            "for": "0s",
        },
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [device_id]},
        "steps": [{
            "kind": "action", "action": "telemetry", "identifier": "switch",
            "value": "{\"switch\":1}",
        }],
    }

    invalid = automation("/v1/rule-validation", "POST", {
        "definition": {**definition, "trigger": {"groups": [], "for": "0s"}},
    })
    check("validation rejects an empty trigger",
          invalid.get("valid") is False and any(
              i.get("code") == "empty_trigger" for i in invalid.get("issues", [])),
          json.dumps(invalid, ensure_ascii=False))

    valid = automation("/v1/rule-validation", "POST", {"definition": definition})
    check("validation accepts the rule",
          valid.get("valid") is True and valid.get("step_count") == 1,
          json.dumps(valid, ensure_ascii=False))

    created = automation("/v1/rules", "POST", {
        "name": f"ENCORE-E2E-RULE-{suffix}",
        "definition": definition,
        "enabled": True,
    })
    rule_id = created.get("id")
    check("rule created at revision 1",
          bool(rule_id) and created.get("revision") == 1,
          json.dumps(created, ensure_ascii=False)[:400])
    if not rule_id:
        return 2
    print("  rule_id =", rule_id)

    print("== 4. simulation (no side effects) ==")
    sim = automation(f"/v1/rules/{rule_id}/simulate", "POST", {"samples": [
        {"at": "0s", "device_id": device_id, "signal": "telemetry",
         "key": "temperature", "value": "35", "unit": "℃"},
    ]})
    check("simulation predicts a run", sim.get("would_run", 0) >= 1,
          json.dumps(sim, ensure_ascii=False)[:300])
    check("simulation has zero side effects", sim.get("side_effects") == 0,
          str(sim.get("side_effects")))
    before = automation(f"/v1/rules/{rule_id}/runs")
    check("simulation created no run", len(before.get("runs") or []) == 0,
          str(len(before.get("runs") or [])))

    print("== 5. ingress (idempotent) ==")
    ev_id = f"encore-e2e-{suffix}"
    first = automation("/v1/ingress/events", "POST", {
        "event_id": ev_id, "device_id": device_id, "signal": "telemetry",
        "key": "temperature", "value": "35", "unit": "℃",
    })
    check("event accepted", first.get("duplicate") is False and first.get("seq", 0) > 0,
          json.dumps(first, ensure_ascii=False))
    second = automation("/v1/ingress/events", "POST", {
        "event_id": ev_id, "device_id": device_id, "signal": "telemetry",
        "key": "temperature", "value": "35", "unit": "℃",
    })
    check("duplicate delivery is rejected by event_id", second.get("duplicate") is True,
          json.dumps(second, ensure_ascii=False))

    print("== 6. run explain ==")
    runs = automation(f"/v1/rules/{rule_id}/runs")
    run_list = runs.get("runs") or []
    check("exactly one run exists", len(run_list) == 1, f"{len(run_list)} run(s)")
    if not run_list:
        return 2
    run_id = run_list[0]["id"]
    run = automation(f"/v1/runs/{run_id}")
    check("run succeeded", run.get("status") == "succeeded",
          f"{run.get('status')} / {run.get('reason_code')}")
    targets = run.get("targets") or []
    check("target snapshot has the device selected",
          len(targets) == 1 and targets[0].get("device_id") == device_id,
          json.dumps(targets, ensure_ascii=False))
    outbox = run.get("outbox") or []
    check("one outbound intent recorded", len(outbox) == 1,
          json.dumps(outbox, ensure_ascii=False))
    if outbox:
        check("command owner accepted the intent", outbox[0].get("state") == "accepted",
              f"{outbox[0].get('state')} / {outbox[0].get('last_error')}")
        check("intent carries an idempotency key", bool(outbox[0].get("idempotency_key")),
              outbox[0].get("idempotency_key", ""))
    steps = run.get("steps") or []
    check("step log recorded", len(steps) == 1 and steps[0].get("status") == "accepted",
          json.dumps(steps, ensure_ascii=False))

    print("== 7. alarm lifecycle ==")
    alarm_def = {
        "kind": "alarm_policy",
        "mode": "single",
        "scope": "device",
        "trigger": {
            "groups": [{"conditions": [{
                "source": "self", "signal": "telemetry", "key": "temperature",
                "operator": ">", "value": "30", "unit": "℃",
            }]}],
            "for": "0s",
        },
        "recovery": {
            "groups": [{"conditions": [{
                "source": "self", "signal": "telemetry", "key": "temperature",
                "operator": "<", "value": "25", "unit": "℃",
            }]}],
        },
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [device_id]},
        "steps": [{"kind": "notify", "notification_policy_id": "np-e2e", "value": "温度过高"}],
    }
    policy = automation("/v1/rules", "POST", {
        "name": f"ENCORE-E2E-ALARM-{suffix}", "definition": alarm_def, "enabled": True,
    })
    policy_id = policy.get("id")
    check("alarm policy created", bool(policy_id), json.dumps(policy, ensure_ascii=False)[:300])

    automation("/v1/ingress/events", "POST", {
        "event_id": f"{ev_id}-alarm", "device_id": device_id, "signal": "telemetry",
        "key": "temperature", "value": "35", "unit": "℃",
    })
    alarms = automation(f"/v1/alarms?policy_id={policy_id}&state=active")
    active = alarms.get("alarms") or []
    check("alarm opened", len(active) == 1, json.dumps(alarms, ensure_ascii=False)[:300])

    if active:
        episode_id = active[0]["id"]
        ack = automation(f"/v1/alarms/{episode_id}/ack", "POST", {"actor": "e2e"})
        check("acknowledge recorded", ack.get("acknowledged") is True,
              json.dumps(ack, ensure_ascii=False))
        still = automation(f"/v1/alarms?policy_id={policy_id}&state=active")
        check("acknowledge does not clear the alarm",
              len(still.get("alarms") or []) == 1,
              json.dumps(still, ensure_ascii=False)[:200])

        automation("/v1/ingress/events", "POST", {
            "event_id": f"{ev_id}-recover", "device_id": device_id, "signal": "telemetry",
            "key": "temperature", "value": "20", "unit": "℃",
        })
        cleared = automation(f"/v1/alarms?policy_id={policy_id}&state=active")
        check("recovery condition clears the alarm",
              len(cleared.get("alarms") or []) == 0,
              json.dumps(cleared, ensure_ascii=False)[:200])
        trs = automation(f"/v1/alarms/{episode_id}/transitions")
        kinds = [t.get("kind") for t in (trs.get("transitions") or [])]
        check("lifecycle history is complete",
              kinds.count("qualified") == 1 and "ack" in kinds and "clear" in kinds,
              ",".join(kinds))

    print("== 8. operations ==")
    tick = automation("/v1/tick", "POST")
    check("tick completed", isinstance(tick, dict) and "outbox_drained" in tick,
          json.dumps(tick, ensure_ascii=False))
    backlog = automation("/v1/backlog")
    check("backlog readable", "unprocessed_events" in backlog,
          json.dumps(backlog, ensure_ascii=False))
    events = automation("/v1/events?after=0&limit=50")
    kinds = [e.get("kind") for e in (events.get("events") or [])]
    check("transition feed published alarm facts",
          "alarm.qualified" in kinds and "alarm.cleared" in kinds,
          ",".join(sorted(set(kinds))))

    print("== 9. legacy command record (reuse proof) ==")
    # The action kind decides which legacy log table records the downlink, so the
    # check follows the same split the owner uses: telemetry -> telemetry set log.
    logs = community(
        f"/api/v1/telemetry/datas/set/logs?device_id={device_id}&page=1&page_size=10")
    data = logs.get("data") or {}
    rows = data.get("list") if isinstance(data, dict) else None
    check("community backend recorded the downlink", bool(rows),
          json.dumps(logs, ensure_ascii=False)[:300])
    if rows:
        payloads = {r.get("data") for r in rows}
        statuses = {r.get("status") for r in rows}
        check("recorded payload matches the rule action",
              payloads == {'{"switch":1}'}, ",".join(sorted(str(p) for p in payloads)))
        check("recorded status is send-success (1)", statuses == {"1"},
              ",".join(sorted(str(x) for x in statuses)))

    print("== 10. uplink adapter path (A05) ==")
    # A separate rule on a separate key, so the earlier single-run assertion is
    # not disturbed. This drives the same endpoint the community uplink would
    # call after decode and transform.
    a05_def = {
        "kind": "automation", "mode": "single", "scope": "device",
        "trigger": {"for": "0s", "groups": [{"conditions": [{
            "source": "self", "signal": "telemetry", "key": "pressure",
            "operator": ">", "value": "10", "unit": "kPa"}]}]},
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [device_id]},
        "steps": [{"kind": "action", "action": "telemetry",
                   "identifier": "switch", "value": "2"}],
    }
    a05_rule = automation("/v1/rules", "POST", {
        "name": f"A05-{suffix}", "definition": a05_def, "enabled": True})
    a05_rule_id = a05_rule.get("id")
    check("A05 rule created", bool(a05_rule_id), json.dumps(a05_rule, ensure_ascii=False)[:300])

    uplink = {
        "class": "telemetry", "device_id": device_id,
        "observed_at": "2026-10-23T10:00:00Z",
        "values": json.dumps({"pressure": 12.5}), "source": "e2e-relay",
    }
    first_up = automation("/v1/ingress/uplink", "POST", uplink)
    check("adapter accepted the first delivery",
          first_up.get("accepted") == 1 and first_up.get("duplicates") == 0,
          json.dumps(first_up, ensure_ascii=False))
    retry = automation("/v1/ingress/uplink", "POST", uplink)
    check("relay retry is recognised as a duplicate",
          retry.get("duplicates") == 1 and retry.get("accepted") == 0,
          json.dumps(retry, ensure_ascii=False))
    check("retry keeps the same event id",
          (retry.get("event_ids") or [None])[0] == (first_up.get("event_ids") or [None])[0],
          f"{first_up.get('event_ids')} vs {retry.get('event_ids')}")

    a05_runs = automation(f"/v1/rules/{a05_rule_id}/runs").get("runs") or []
    check("redelivery produced exactly one run", len(a05_runs) == 1,
          f"{len(a05_runs)} run(s)")
    if a05_runs:
        detail = automation(f"/v1/runs/{a05_runs[0]['id']}")
        check("adapter run succeeded", detail.get("status") == "succeeded",
              f"{detail.get('status')} / {detail.get('reason_code')}")
        a05_outbox = detail.get("outbox") or []
        check("one command per accepted delivery", len(a05_outbox) == 1,
              json.dumps(a05_outbox, ensure_ascii=False))

    print("== 11. gateway and status adapters ==")
    gw = automation("/v1/ingress/uplink", "POST", {
        "class": "gateway_telemetry", "device_id": device_id,
        "gateway_id": "gw-e2e", "message_id": f"gw-{suffix}",
        "values": json.dumps({"battery": 88}), "source": "e2e-gateway",
    })
    check("gateway telemetry accepted", gw.get("accepted") == 1,
          json.dumps(gw, ensure_ascii=False))
    online = automation("/v1/ingress/uplink", "POST", {
        "class": "status", "device_id": device_id,
        "values": json.dumps({"online": "1"}), "source": "e2e-status",
    })
    check("status adapter accepted", online.get("accepted") == 1,
          json.dumps(online, ensure_ascii=False))
    bad = automation("/v1/ingress/uplink", "POST", {
        "class": "event", "device_id": device_id,
        "values": json.dumps({"level": 3}), "source": "e2e-status",
    })
    check("event without identifier is rejected", bad.get("code") != "ok",
          json.dumps(bad, ensure_ascii=False))

    print()
    if FAILURES:
        print(f"RESULT: {len(FAILURES)} check(s) failed")
        for f in FAILURES:
            print("  -", f)
        return 1
    print("RESULT: all checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
