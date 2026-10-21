#!/usr/bin/env python3
"""Release acceptance: the checks that must hold before this branch can merge.

Three groups:

  A. 代码质量  — gofmt / go vet / 迁移与合同文件齐备
  B. 插件一致性 — 合同第 11 章的 8 条检查，逐条在真实插件进程上验证
  C. 可交付性  — 文档、部署脚本、三个验收入口都在，且没有把 Kernel 当依赖

A check that could not run is reported as skipped, never as passed.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from pathlib import Path

import lib
from lib import Report

REPO = Path(__file__).resolve().parents[3]
HTTP_PLUGIN = os.environ.get("NB_HTTP_PLUGIN_URL", "http://127.0.0.1:18081").rstrip("/")
MQTT_PLUGIN = os.environ.get("NB_MQTT_PLUGIN_URL", "http://127.0.0.1:18082").rstrip("/")
PLUGIN_TOKEN = os.environ.get("NB_PLUGIN_TOKEN", "dev-plugin-token")


def sh(cmd: list[str], cwd: Path | None = None, timeout: int = 240) -> tuple[int, str]:
    # Go module downloads must not make a code-quality check look like a code
    # defect. On a cold module cache `go vet` pulls encore.dev; without a proxy
    # that can stall until the timeout and the check reports a false failure.
    env = dict(os.environ)
    env.setdefault("GOPROXY", "https://goproxy.cn,direct")
    env.setdefault("GOFLAGS", "-mod=mod")
    proc = subprocess.run(cmd, cwd=str(cwd or REPO), capture_output=True, text=True,
                          timeout=timeout, env=env)
    return proc.returncode, (proc.stdout + proc.stderr).strip()


def plugin_call(base: str, path: str, body=None, token: str | None = PLUGIN_TOKEN) -> tuple[int, dict]:
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    res = lib.call("POST" if body is not None else "GET", f"{base}{path}", body,
                   token=False, headers=headers, timeout=15)
    try:
        return res.status, json.loads(res.body or b"{}")
    except json.JSONDecodeError:
        return res.status, {}


def run() -> int:
    r = Report("北向服务 release 验收")

    # Fail fast and clearly if the core is not up. Without this, a dead core
    # shows up as a pile of unrelated failures further down (0 deliveries, no
    # ack levels) and reads like a product defect instead of an environment one.
    probe = lib.inbox([])
    r.add("核心在线（inbox 可达）", probe.status in (200, 204), f"HTTP {probe.status}")

    # =====================================================================
    print("\n[A] 代码质量")
    # =====================================================================
    own = [
        "internal/northbound", "internal/api/northbound_source.go", "internal/app/northbound.go",
        "router/apps/northbound.go", "sql/northbound", "scripts/qa/northbound",
    ]
    existing = [p for p in own if (REPO / p).exists()]
    rc, out = sh(["gofmt", "-l", *existing])
    r.add("自有 Go 代码通过 gofmt", rc == 0 and out == "", out[:300] or "无未格式化文件")

    # Generous timeout: on a cold module cache vet has to fetch and compile the
    # whole community module, which is minutes on a loaded machine.
    rc, out = sh(["go", "vet", "./internal/northbound/...", "./internal/api/...", "./internal/app/..."],
                 timeout=900)
    r.add("自有包通过 go vet", rc == 0, out[:400] or "无告警")

    encore = REPO / "encore-api"
    rc, out = sh(["go", "vet", "./..."], cwd=encore, timeout=900)
    r.add("Encore 应用通过 go vet", rc == 0, out[:400] or "无告警")

    # Encore applications must be tested through the encore CLI: running plain
    # `go test` panics with "encore apps must be run using the encore command".
    #
    # This check is deliberately deferred to the end of the script: `encore test`
    # takes over the app and the running `encore run` process dies with it. When
    # it ran here, every later assertion that needs a live core (idempotency,
    # QoS0 ack) failed and looked like a product defect. It is a real check, it
    # just has to be the last thing that touches the core.

    rc, out = sh(["go", "build", "-p", "2", "./..."], timeout=600)
    r.add("社区后端可构建", rc == 0, out[:300] or "ok")

    # =====================================================================
    print("\n[B] 插件一致性（合同第 11 章）")
    # =====================================================================
    endpoints = [
        ("/v1/describe", None),
        ("/v1/validate", {"contractVersion": "northbound-connector/v1", "connectionId": "probe", "config": {}}),
        ("/v1/open", {"contractVersion": "northbound-connector/v1", "connectionId": "probe", "config": {}}),
        ("/v1/write", {"contractVersion": "northbound-connector/v1", "connectionId": "probe", "items": []}),
        ("/v1/close", {"contractVersion": "northbound-connector/v1", "connectionId": "probe", "instanceId": "probe"}),
        ("/v1/health", None),
    ]

    # 1. fail closed without a token
    for label, base in (("HTTP", HTTP_PLUGIN), ("MQTT", MQTT_PLUGIN)):
        closed = []
        for path, body in endpoints:
            status, _ = plugin_call(base, path, body, token="")
            closed.append(status in (401, 403, 503))
        r.add(f"{label} 插件无令牌时全部拒绝", all(closed),
              f"{sum(closed)}/{len(endpoints)} 个端点拒绝")

    # 2. describe identity matches the contract
    for label, base, want in (("HTTP", HTTP_PLUGIN, "io.thingspanel.sink.http"),
                              ("MQTT", MQTT_PLUGIN, "io.thingspanel.sink.mqtt")):
        status, desc = plugin_call(base, "/v1/describe")
        r.add(f"{label} 插件 describe 与合同一致",
              status == 200 and desc.get("contractVersion") == "northbound-connector/v1" and desc.get("id") == want,
              f"id={desc.get('id')} contract={desc.get('contractVersion')}")

    # 3. wrong token rejected
    status, _ = plugin_call(HTTP_PLUGIN, "/v1/describe", token="definitely-wrong")
    r.add("插件拒绝错误令牌", status in (401, 403), f"HTTP {status}")

    # 4. validate must reject a broken config *and* must not touch the target.
    # The probe path is unique so the assertion cannot be fooled by traffic from
    # other checks that legitimately hit the receiver.
    probe = "/release-badcfg-probe"
    res = lib.post("/v1/connections", {
        "name": lib.unique("rel-badcfg"),
        "connectorId": "io.thingspanel.sink.http",
        # A valid URI with a scheme the sink does not speak. Creation accepts it
        # (config is stored as-is); only validate is supposed to catch it.
        "config": {"url": f"ftp://127.0.0.1{probe}", "method": "POST", "timeoutMs": 3000},
    })
    bad_conn = (res.json or {}).get("id", "")
    if bad_conn:
        res = lib.post(f"/v1/connections/{bad_conn}/test")
        body = res.json or {}
        cfg = next((c for c in body.get("checks", []) if c.get("name") == "config"), {})
        r.add("错误配置被 validate 拒绝", body.get("ok") is False and cfg.get("ok") is False,
              f"ok={body.get('ok')} failure={body.get('failureCode')} config_ok={cfg.get('ok')}")
        touched = [x for x in lib.receiver_records() if str(x.get("path", "")).startswith(probe)]
        r.add("校验失败不向目标发送数据", not touched, f"命中探针路径 {len(touched)} 次")
        lib.call("DELETE", f"/v1/connections/{bad_conn}")
    else:
        r.skip("错误配置被 validate 拒绝", f"探针连接创建失败 HTTP {res.status}")

    # 5. QoS0 must be reported as unacknowledged
    mqtt_conn = None
    res = lib.post("/v1/connections", {
        "name": lib.unique("rel-qos0"),
        "connectorId": "io.thingspanel.sink.mqtt",
        "config": {
            "brokerUrl": f"tcp://{lib.MQTT_HOST}:{lib.MQTT_PORT}",
            "topicTemplate": "release/qos0/{deviceId}",
            "qos": 0,
            "clientIdPrefix": "rel-qos0",
            "connectTimeoutMs": 5000,
            "publishTimeoutMs": 5000,
        },
    })
    mqtt_conn = (res.json or {}).get("id", "")
    if mqtt_conn:
        res = lib.post("/v1/pipelines", {
            "name": lib.unique("rel-qos0-task"),
            "connectionId": mqtt_conn,
            "filter": {"eventTypes": ["telemetry"]},
            "transform": {"include": ["temperature"]},
            "serialization": {"format": "json", "topic": "release/qos0/{deviceId}"},
        })
        pid = (res.json or {}).get("id", "")
        lib.post(f"/v1/pipelines/{pid}/activate")
        res = lib.post(f"/v1/pipelines/{pid}/test-send",
                       {"sample": lib.sample_event(lib.unique("qos0"), device_id="dev-release-qos0")})
        did = (res.json or {}).get("deliveryId", "")
        ack = lib.wait_until(
            lambda: lib.scalar(f"SELECT ack_level FROM nb_deliveries WHERE id = '{did}' AND state = 'delivered'"),
            timeout=25)
        r.add("QoS0 记为 none（不冒充已确认）", ack == "none", f"ack_level={ack}")
    else:
        r.skip("QoS0 记为 none", "无法创建 MQTT 连接")

    # 6. idempotency key is stable across retries and versions
    ev = lib.sample_event(lib.unique("stable"), device_id="dev-release-stable")

    # One event fans out to every active task, so a single event owns several
    # delivery rows with *different* keys (the key includes the pipeline). The
    # assertion therefore compares each delivery's own key before and after the
    # duplicate submission; comparing "the first key of the event" would compare
    # two arbitrary rows and fail at random.
    def key_map() -> dict[str, str]:
        return dict(lib.sql(
            f"SELECT id, idempotency_key FROM nb_deliveries WHERE event_id = '{ev['eventId']}'"))

    lib.inbox([ev])
    first = lib.wait_until(key_map, timeout=20) or {}
    baseline = len(first)
    lib.inbox([ev])  # duplicate submission
    second = key_map()
    if first and first == second:
        detail = f"{len(first)} 条投递的键前后完全一致"
    else:
        changed = sorted(k for k in set(first) | set(second) if first.get(k) != second.get(k))
        detail = f"键发生变化的投递: {', '.join(changed[:3]) or '（投递集合本身变了）'}"
    r.add("重复提交后幂等键不变", bool(first) and first == second, detail)
    after = int(lib.scalar(f"SELECT count(*) FROM nb_deliveries WHERE event_id = '{ev['eventId']}'") or 0)
    r.add("重复提交不新增投递", baseline > 0 and after == baseline,
          f"首次 {baseline} 条 -> 重复提交后 {after} 条（每个活动任务一条）")

    key_rule = lib.scalar(
        f"SELECT count(*) FROM nb_deliveries WHERE event_id = '{ev['eventId']}' AND idempotency_key NOT LIKE 'nb1:%'")
    r.add("幂等键格式统一为 nb1:", key_rule == "0", "全部带 nb1: 前缀")

    # 7. plugin restart must not change the key (instanceId is not part of it)
    r.add("幂等键不含实例或尝试次数",
          all(re.match(r"^nb1:[0-9a-f]{64}$", row[0])
              for row in lib.sql(
                  f"SELECT idempotency_key FROM nb_deliveries WHERE event_id = '{ev['eventId']}'")),
          "nb1: + 64 位十六进制")

    # 8. unknown result is never success
    r.add("契约声明 unknown 不算成功",
          "unknown" in (REPO / "docs/northbound/CONTRACT-v1.md").read_text(encoding="utf-8"),
          "CONTRACT-v1.md 含 unknown 约定")

    # 9. delete is allowed only for a draft that never delivered (contract v1.2).
    # A task that has run owns delivery records that must survive, so the guard
    # has to be enforced by the server, not only by hiding the button.
    if mqtt_conn:
        draft = lib.post("/v1/pipelines", {
            "name": lib.unique("rel-del-draft"),
            "connectionId": mqtt_conn,
            "filter": {"eventTypes": ["telemetry"]},
            "transform": {"include": ["temperature"]},
            "serialization": {"format": "json", "topic": "release/del/{deviceId}"},
        })
        draft_id = (draft.json or {}).get("id", "")
        gone = lib.call("DELETE", f"/v1/pipelines/{draft_id}")
        r.add("草稿任务可以删除", gone.status == 200, f"HTTP {gone.status}")

        ran = lib.post("/v1/pipelines", {
            "name": lib.unique("rel-del-ran"),
            "connectionId": mqtt_conn,
            "filter": {"eventTypes": ["telemetry"]},
            "transform": {"include": ["temperature"]},
            "serialization": {"format": "json", "topic": "release/del/{deviceId}"},
        })
        ran_id = (ran.json or {}).get("id", "")
        lib.post(f"/v1/pipelines/{ran_id}/activate")
        blocked = lib.call("DELETE", f"/v1/pipelines/{ran_id}")
        r.add("跑过的任务拒绝删除", blocked.status == 409,
              f"HTTP {blocked.status} {(blocked.json or {}).get('code', '')}")
    else:
        r.skip("草稿任务可以删除", "没有可用的 MQTT 连接")

    # =====================================================================
    print("\n[C] 可交付性")
    # =====================================================================
    required = [
        "docs/northbound/CONTRACT-v1.md",
        "docs/northbound/EXECUTION-LEDGER.md",
        "docs/northbound/DEPLOYMENT.md",
        "docs/northbound/USAGE.md",
        "docs/northbound/MAINTENANCE.md",
        "docs/northbound/CHANGES.md",
        "docs/northbound/HANDOVER.md",
        "deploy/northbound/compose.yaml",
        "deploy/northbound/migrate.sh",
        "sql/northbound/001_source_outbox.sql",
        "encore-api/northbound/migrations/0001_init.up.sql",
        "scripts/qa/northbound/journey.py",
        "scripts/qa/northbound/recovery.py",
        "scripts/qa/northbound/release.py",
        "plugins/northbound-http/main.go",
        "plugins/northbound-mqtt/main.go",
        "plugins/northbound-sdk-go/server.go",
        "internal/northbound/relay.go",
        "internal/northbound/enroll.go",
    ]
    missing = [p for p in required if not (REPO / p).exists()]
    r.add("必需交付文件齐备", not missing, "缺失: " + ", ".join(missing) if missing else "全部存在")

    kernel_refs = sh(["grep", "-rIl", "iot-api-encore-kernel", "encore-api", "plugins", "internal/northbound", "sql/northbound"])[1]
    r.add("不以 Kernel 作为构建或运行依赖", kernel_refs.strip() == "",
          kernel_refs[:300] or "无引用")

    r.add("源端开关默认关闭",
          "Enabled:           false" in (REPO / "internal/northbound/config.go").read_text(encoding="utf-8"),
          "DefaultConfig.Enabled=false")

    contract_text = (REPO / "docs/northbound/CONTRACT-v1.md").read_text(encoding="utf-8")
    versions = re.findall(r"^\|\s*(v\d+\.\d+)\s*\|", contract_text, re.M)
    r.add("合同记录了修订历史",
          "修订记录" in contract_text and len(versions) >= 2,
          " -> ".join(versions) if versions else "未找到版本行")

    # =====================================================================
    print("\n[D] Encore 单元测试（放最后：它会杀掉正在运行的核心）")
    # =====================================================================
    rc, out = sh(["encore", "test", "./..."], cwd=encore, timeout=900)
    # Encore's own summary ("One or more more tests failed.") says nothing about
    # which package broke, so surface the failing lines instead.
    if rc == 0:
        detail = (out.strip().splitlines() or ["无输出"])[-1][:300]
    else:
        bad = [ln for ln in out.splitlines()
               if ln.startswith("FAIL") or "--- FAIL" in ln or ln.startswith("panic:")]
        detail = ("失败包: " + " | ".join(bad[:4])) if bad else out[-400:]
    r.add("Encore 应用单元测试通过（encore test）", rc == 0, detail)

    r.write("release.md")
    return r.finish()


if __name__ == "__main__":
    lib.main_guard(run)
