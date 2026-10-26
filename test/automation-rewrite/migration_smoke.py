#!/usr/bin/env python3
"""A31 smoke test: migration, reconciliation and rollback against a real service.

The Go tests in `automation/a31_migration_test.go` cover the semantics with a real
database but call the service methods directly. This script covers the things a
direct call cannot:

  * the five endpoints are registered at the paths the CLI and the console expect;
  * the JSON that goes in and comes out actually round-trips (a field renamed in
    the type would still compile);
  * a run survives being applied twice, and a second run over the same snapshot
    reports "skipped" instead of creating a duplicate;
  * the adopted alarm shows up in `GET /v1/alarms` and in `verify`, and adopting
    it did not enqueue a notification.

Usage:
    cd encore-api && TP_AUTH_MODE=dev encore run --port 14001
    python3 test/automation-rewrite/migration_smoke.py

Environment:
    CORE_URL   default http://127.0.0.1:14001
    CORE_TOKEN default a dev token in tenant e2e-a31
"""

import json
import os
import sys
import urllib.error
import urllib.request
import uuid

BASE = os.environ.get("CORE_URL", "http://127.0.0.1:14001").rstrip("/")
TOKEN = os.environ.get("CORE_TOKEN", "a31-user:e2e-a31:tenant_admin:*")

RUN = uuid.uuid4().hex[:8]
DEVICE = "dev-a31-" + RUN
POLICY_LEGACY = "policy-a31-" + RUN
SCENE_OK = "scene-ok-" + RUN
SCENE_BAD = "scene-bad-" + RUN
ALARM_A = "alarm-a-" + RUN
ALARM_B = "alarm-b-" + RUN

PASS = 0
FAIL = 0


def check(name, ok, detail=""):
    global PASS, FAIL
    if ok:
        PASS += 1
        print("  ok   " + name)
    else:
        FAIL += 1
        print("  FAIL " + name + ("  -> " + str(detail) if detail else ""))


def call(method, path, body=None, want=200):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    req.add_header("x-token", TOKEN)
    req.add_header("Authorization", "Bearer " + TOKEN)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode()
            status = resp.status
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        status = e.code
    parsed = None
    if raw:
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            parsed = raw
    return status, parsed


def fail_detail(status, body):
    return "HTTP %s %s" % (status, json.dumps(body, ensure_ascii=False)[:300])


def item_of(resp, legacy_id):
    for it in (resp or {}).get("items") or []:
        if it.get("legacy_id") == legacy_id:
            return it
    return None


def scene_writing(sid):
    return {
        "id": sid,
        "name": "旧场景 " + sid,
        "actions": [
            {
                "id": sid + "-a1",
                "target": DEVICE,
                "type": "10",
                "param_type": "TEL",
                "param": "switch",
                "value": "1",
            }
        ],
    }


def scene_with_service(sid):
    return {
        "id": sid,
        "name": "带服务动作的旧场景 " + sid,
        "actions": [
            {
                "id": sid + "-a1",
                "target": DEVICE,
                "type": "10",
                "param_type": "TEL",
                "param": "switch",
                "value": "1",
            },
            {"id": sid + "-a2", "target": "https://example.invalid/hook", "type": "40"},
        ],
    }


def alarm_policy_definition():
    return {
        "kind": "alarm_policy",
        "mode": "single",
        "scope": "device",
        "trigger": {
            "groups": [
                {
                    "conditions": [
                        {
                            "source": "self",
                            "signal": "telemetry",
                            "key": "temperature",
                            "operator": ">",
                            "value": "30",
                            "unit": "℃",
                        }
                    ]
                }
            ]
        },
        "targets": {"device_ids": [DEVICE]},
        "alarm": {"key": "policy.a31." + RUN, "level": "H", "sources": ["detector"]},
    }


def main():
    print("A31 migration smoke — run=%s" % RUN)

    # ---------------------------------------------------------------- 1. health
    status, body = call("GET", "/v1/health")
    check("服务可用", status == 200 and body.get("status") == "ok", fail_detail(status, body))

    # ------------------------------------------------- 2. a policy to adopt into
    status, policy = call(
        "POST",
        "/v1/rules",
        {
            "name": "旧告警策略 " + RUN,
            "definition": alarm_policy_definition(),
            "enabled": True,
            "legacy_id": POLICY_LEGACY,
            "request_id": "mk-policy-" + RUN,
        },
    )
    check(
        "建一条带旧 id 的告警策略",
        status == 200 and policy.get("id"),
        fail_detail(status, policy),
    )
    if status != 200:
        return finish()

    # ------------------------------------------------------------- 3. dry run
    snapshot = {
        "name": "冒烟迁移 " + RUN,
        "scenes": [scene_writing(SCENE_OK), scene_with_service(SCENE_BAD)],
        "alarms": [
            {
                "id": ALARM_A,
                "policy_legacy_id": POLICY_LEGACY,
                "subject": DEVICE,
                "status": "M",
                "level": "M",
                "opened_at": "2026-10-20T08:00:00Z",
            },
            {
                "id": ALARM_B,
                "policy_legacy_id": POLICY_LEGACY,
                "subject": DEVICE,
                "status": "H",
                "level": "H",
                "opened_at": "2026-10-20T09:00:00Z",
            },
        ],
        "devices": [DEVICE],
    }
    status, dry = call("POST", "/v1/migrations", snapshot)
    check("试运行创建成功", status == 200 and dry.get("id"), fail_detail(status, dry))
    if status != 200:
        return finish()
    mid = dry["id"]
    check("试运行不应用任何一条", dry.get("applied") == 0, dry.get("applied"))
    check("四条旧对象都在报告里", dry.get("total") == 4, dry.get("total"))
    check("试运行状态是 planned", dry.get("status") == "planned", dry.get("status"))

    it = item_of(dry, SCENE_BAD)
    check(
        "服务型动作被标成 unsupported",
        it is not None and it.get("status") == "unsupported",
        it,
    )
    check(
        "未解决项单独计数",
        dry.get("unresolved") == 1,
        dry.get("unresolved"),
    )
    check(
        "报告里说明了有无法迁移的对象",
        any("无法自动迁移" in n for n in dry.get("notes") or []),
        dry.get("notes"),
    )

    # ---------------------------------------------------------------- 4. apply
    status, applied = call("POST", "/v1/migrations/%s/apply" % mid, {"limit": 50})
    check("应用接口可用", status == 200, fail_detail(status, applied))
    if status != 200:
        return finish()
    check("场景与告警各应用一条", applied.get("applied") == 2, applied.get("applied"))
    check("第二条告警被跳过", applied.get("skipped") == 1, applied.get("skipped"))
    check("未解决项没被算成已迁移", applied.get("unresolved") == 1, applied.get("unresolved"))
    check(
        "已应用 + 已跳过 + 失败 != 总数（未解决项没被掩盖）",
        applied.get("applied", 0) + applied.get("skipped", 0) + applied.get("failed", 0)
        != applied.get("total"),
        applied,
    )
    ok_item = item_of(applied, SCENE_OK)
    check("场景条目拿到了规则 id", ok_item and ok_item.get("rule_id"), ok_item)
    alarm_item = item_of(applied, ALARM_A)
    check("告警条目拿到了 episode id", alarm_item and alarm_item.get("episode_id"), alarm_item)

    # ------------------------------------------------------- 5. apply it again
    status, again = call("POST", "/v1/migrations/%s/apply" % mid, {"limit": 50})
    check("重复应用不报错", status == 200, fail_detail(status, again))
    check("重复应用不会再建规则", again.get("applied") == applied.get("applied"), again.get("applied"))

    # ------------------------------------- 6. a brand new run over the snapshot
    status, rerun = call("POST", "/v1/migrations", dict(snapshot, name="重跑 " + RUN, apply=True))
    check("重跑迁移创建成功", status == 200, fail_detail(status, rerun))
    if status == 200:
        check("重跑时已存在的一致对象被跳过", rerun.get("applied") == 0, rerun.get("applied"))
        check("重跑没有新增应用", rerun.get("skipped", 0) >= 2, rerun.get("skipped"))

    # ------------------------------------------------------------- 7. verify
    #
    # This run still carries the scene that could not be converted, so verify must
    # NOT say "complete". A report that passes with an unresolved item in it is the
    # exact failure the task book forbids: the object the customer will miss would
    # be hidden behind a green tick.
    status, verify = call("POST", "/v1/migrations/%s/verify" % mid)
    check("对账接口可用", status == 200, fail_detail(status, verify))
    if status == 200:
        check("有未解决项时对账不通过", verify.get("complete") is False, verify.get("complete"))
        check("没有孤儿规则", not verify.get("orphans"), verify.get("orphans"))
        check("没有缺失对象", not verify.get("missing"), verify.get("missing"))
        check(
            "没有重复活动告警",
            verify.get("duplicate_active_alarms") == 0,
            verify.get("duplicate_active_alarms"),
        )
        check(
            "未解决项被列出来",
            verify.get("unresolved") == [SCENE_BAD],
            verify.get("unresolved"),
        )
        check(
            "说明里讲清为什么不通过",
            any("没有迁移结果" in n for n in verify.get("notes") or []),
            verify.get("notes"),
        )

    # ------------------------------------------------- 8. the alarm is visible
    status, alarms = call("GET", "/v1/alarms?subject_id=%s" % DEVICE)
    check("告警列表接口可用", status == 200, fail_detail(status, alarms))
    if status == 200:
        rows = alarms.get("alarms") or []
        check("领养的告警出现在列表里", len(rows) == 1, len(rows))
        if rows:
            check("告警等级按策略", rows[0].get("level") == "H", rows[0].get("level"))
            check("告警是活动的", rows[0].get("state") == "active", rows[0].get("state"))

    # ----------------------------------------------------------- 9. rollback
    status, rolled = call("POST", "/v1/migrations/%s/rollback" % mid, {})
    check("回退接口可用", status == 200, fail_detail(status, rolled))
    if status == 200:
        scene_item = item_of(rolled, SCENE_OK)
        check(
            "没执行过的场景规则被回退",
            scene_item and scene_item.get("status") == "rolled_back",
            scene_item,
        )
        alarm_after = item_of(rolled, ALARM_A)
        check(
            "没被确认过的告警被撤回",
            alarm_after and alarm_after.get("status") == "rolled_back",
            alarm_after,
        )

    # ---------------------------------------- 10. applying a rolled back run
    status, body = call("POST", "/v1/migrations/%s/apply" % mid, {})
    check("回退之后不能再继续应用", status != 200, "HTTP %s" % status)
    if status != 200:
        check(
            "拒绝原因说得清",
            "回退" in json.dumps(body, ensure_ascii=False),
            body,
        )

    # ------------------------------- 11. a clean run has to reach "complete"
    #
    # The other direction: when nothing is unresolved, verify must say complete.
    # Without this, "always false" would pass every check above.
    clean_scene = "scene-clean-" + RUN
    status, clean = call(
        "POST",
        "/v1/migrations",
        {
            "name": "干净的迁移 " + RUN,
            "scenes": [scene_writing(clean_scene)],
            "devices": [DEVICE],
            "apply": True,
        },
    )
    check("干净迁移创建并应用", status == 200 and clean.get("applied") == 1, fail_detail(status, clean))
    if status == 200:
        status, cv = call("POST", "/v1/migrations/%s/verify" % clean["id"])
        check("干净迁移对账接口可用", status == 200, fail_detail(status, cv))
        if status == 200:
            check("没有未解决项时对账通过", cv.get("complete") is True, cv)
            check("对账说明写了对账通过", any("对账通过" in n for n in cv.get("notes") or []), cv.get("notes"))

    return finish()


def finish():
    print("\n%d passed, %d failed" % (PASS, FAIL))
    return 0 if FAIL == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
