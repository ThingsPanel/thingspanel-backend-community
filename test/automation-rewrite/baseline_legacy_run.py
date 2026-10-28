#!/usr/bin/env python3
"""A01/A05 baseline reproduction against the *legacy* community backend.

Creates an isolated tenant-scoped fixture (device config + device + scene
automation), publishes one telemetry sample over MQTT and then reads back the
legacy execution log. This is the "one original business run" evidence for the
handoff round: it exercises the real uplink -> automate -> command chain, not a
mock.

Usage:
    python3 baseline_legacy_run.py --base http://127.0.0.1:19999 \
        --mqtt 127.0.0.1:11885 --device-number AUTO-BASE-001
"""
from __future__ import annotations

import argparse
import base64
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

TOKEN: str | None = None


def call(base: str, method: str, path: str, body: dict | None = None) -> dict:
    url = base.rstrip("/") + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if TOKEN:
        req.add_header("x-token", TOKEN)
    # The sandbox exports HTTP_PROXY; localhost must bypass it.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req, timeout=30) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as exc:  # surface the server error body
        return {"code": exc.code, "message": exc.read().decode()}


def login(base: str, email: str, password: str) -> str:
    out = call(base, "POST", "/api/v1/login", {"email": email, "password": password})
    if out.get("code") != 200:
        raise SystemExit(f"login failed: {out}")
    return out["data"]["token"]


def mqtt_publish(host: str, port: int, topic: str, payload: dict) -> None:
    # mosquitto_pub ships with the broker image; fall back to docker exec.
    cmd = [
        "docker", "exec", "tp-auto-mqtt", "mosquitto_pub",
        "-h", "127.0.0.1", "-p", "1883", "-t", topic, "-m", json.dumps(payload), "-q", "1",
    ]
    subprocess.run(cmd, check=True, capture_output=True)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:19999")
    ap.add_argument("--mqtt-host", default="127.0.0.1")
    ap.add_argument("--mqtt-port", type=int, default=11885)
    ap.add_argument("--email", default="tenant@tenant.cn")
    ap.add_argument("--password", default="123456")
    ap.add_argument("--device-number", default="")
    ap.add_argument("--value", default="31.5")
    args = ap.parse_args()

    global TOKEN
    TOKEN = login(args.base, args.email, args.password)
    print("login ok")

    suffix = str(int(time.time()))
    device_number = args.device_number or f"AUTO-BASE-{suffix}"
    cfg = call(args.base, "POST", "/api/v1/device_config", {
        "name": f"AUTO-BASE-CFG-{suffix}",
        "device_type": "1",
        "protocol_type": "MQTT",
        "description": "automation rewrite baseline fixture",
    })
    print("device_config:", cfg.get("code"), cfg.get("message"))
    cfg_id = (cfg.get("data") or {}).get("id")

    dev = call(args.base, "POST", "/api/v1/device", {
        "name": f"AUTO-BASE-DEV-{suffix}",
        "device_number": device_number,
        "device_config_id": cfg_id,
        "protocol": "MQTT",
        "label": "automation-baseline",
        "description": "automation rewrite baseline fixture",
    })
    print("device:", dev.get("code"), dev.get("message"))
    dev_id = (dev.get("data") or {}).get("id")
    if not dev_id:
        raise SystemExit("device creation failed")

    rule = call(args.base, "POST", "/api/v1/scene_automations", {
        "name": f"AUTO-BASE-RULE-{suffix}",
        "description": "temperature > 30 -> telemetry control",
        "enabled": "N",
        "trigger_condition_groups": [[{
            "trigger_conditions_type": "10",
            "trigger_source": dev_id,
            "trigger_param_type": "TEL",
            "trigger_param": "temperature",
            "trigger_operator": ">",
            "trigger_value": "30",
        }]],
        "actions": [{
            "action_type": "10",
            "action_target": dev_id,
            "action_param_type": "TEL",
            "action_param": "switch",
            "action_value": "{\"switch\":1}",
        }],
    })
    print("rule:", rule.get("code"), rule.get("message"))
    rule_id = (rule.get("data") or {}).get("scene_automation_id")
    if not rule_id:
        raise SystemExit("rule creation failed")

    sw = call(args.base, "POST", f"/api/v1/scene_automations/switch/{rule_id}")
    print("switch on:", sw.get("code"), sw.get("message"))

    # Give the automation cache a moment, then push the real uplink sample.
    time.sleep(2)
    # This build declares `values []byte`, so the wire value is the base64 of
    # the JSON object. Recorded in the A01 ledger as the observed contract.
    values = base64.b64encode(
        json.dumps({"temperature": float(args.value)}).encode()
    ).decode()
    mqtt_publish(args.mqtt_host, args.mqtt_port, "devices/telemetry", {
        "device_id": dev_id,
        "values": values,
    })
    time.sleep(3)

    logs = call(args.base, "GET", f"/api/v1/scene_automations/log?scene_automation_id={rule_id}&page=1&page_size=10")
    print(json.dumps({
        "device_id": dev_id,
        "device_config_id": cfg_id,
        "scene_automation_id": rule_id,
        "log_response": logs,
    }, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
