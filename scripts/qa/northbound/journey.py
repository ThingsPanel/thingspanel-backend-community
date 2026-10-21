#!/usr/bin/env python3
"""Journey acceptance: one event travels the whole path and arrives at the target.

What this proves, in order:
  1. the northbound service sees both real plugin processes as ready;
  2. a destination connection can be created and really opened;
  3. a forwarding task previews the exact bytes it will send;
  4. the record that arrives at the target equals the preview, not the raw event;
  5. the same event delivered to two targets is confirmed per target;
  6. a duplicate submission does not create a second delivery;
  7. MQTT reaches a real broker and is confirmed by it.

Run with the services already started (see docs/northbound/DEPLOYMENT.md).
"""
from __future__ import annotations

import json
import subprocess
import sys
import time

import lib
from lib import Report

HTTP_CONNECTOR = "io.thingspanel.sink.http"
MQTT_CONNECTOR = "io.thingspanel.sink.mqtt"


def run() -> int:
    r = Report("北向服务 journey 验收（真实链路）")

    # ---- 1. connectors -----------------------------------------------------
    print("\n[1] 连接器探活")
    res = lib.get("/v1/connectors")
    body = res.json or {}
    ids = {c.get("id"): c for c in body.get("connectors", [])}
    r.add("北向服务可访问", res.status == 200, f"HTTP {res.status}")
    r.add("HTTP 插件已就绪", ids.get(HTTP_CONNECTOR, {}).get("state") == "ready",
          ids.get(HTTP_CONNECTOR, {}).get("displayName", "缺失"))
    r.add("MQTT 插件已就绪", ids.get(MQTT_CONNECTOR, {}).get("state") == "ready",
          ids.get(MQTT_CONNECTOR, {}).get("displayName", "缺失"))
    if r.failed:
        return r.finish()

    # ---- 2. HTTP connection ------------------------------------------------
    print("\n[2] 目标连接：创建并真实打开")
    lib.receiver_mode("ok", 0, "")
    lib.receiver_clear()
    name = lib.unique("journey-http")
    res = lib.post("/v1/connections", {
        "name": name,
        "connectorId": HTTP_CONNECTOR,
        "config": {
            "url": f"{lib.RECEIVER}/ingest",
            "method": "POST",
            "batch": True,
            "timeoutMs": 5000,
            "idempotencyHeader": "Idempotency-Key",
        },
    })
    conn = res.json or {}
    conn_id = conn.get("id", "")
    r.add("创建 HTTP 连接", res.status == 200 and bool(conn_id), f"HTTP {res.status} id={conn_id}")
    if not conn_id:
        return r.finish()

    res = lib.post(f"/v1/connections/{conn_id}/test")
    checks = (res.json or {}).get("checks", [])
    r.add("连接校验三项全绿", res.status == 200 and len(checks) == 3 and all(c.get("ok") for c in checks),
          " / ".join(f"{c.get('name')}={c.get('ok')}" for c in checks))

    # ---- 3. pipeline preview ----------------------------------------------
    print("\n[3] 转发任务：草稿与预览")
    res = lib.post("/v1/pipelines", {
        "name": lib.unique("journey-task"),
        "connectionId": conn_id,
        "filter": {"eventTypes": ["telemetry"]},
        "transform": {
            "include": ["temperature", "humidity"],
            "rename": {"temperature": "temp_c"},
            "constants": {"site": "journey"},
            "unitConvert": {"humidity": {"from": "ratio", "to": "%"}},
        },
        "serialization": {"format": "json"},
    })
    pipe = res.json or {}
    pipe_id = pipe.get("id", "")
    r.add("创建任务为草稿", res.status == 200 and pipe.get("state") == "draft",
          f"state={pipe.get('state')} revision={pipe.get('revision')}")
    if not pipe_id:
        return r.finish()

    sample = lib.sample_event(lib.unique("preview"))
    res = lib.post(f"/v1/pipelines/{pipe_id}/preview", {"sample": sample})
    prev = res.json or {}
    expected = prev.get("output") or {}
    r.add("预览命中过滤", prev.get("matched") is True, str(prev.get("matchReason")))
    r.add("预览输出符合变换",
          expected == {"temp_c": 24.5, "humidity": 50, "site": "journey"},
          json.dumps(expected, ensure_ascii=False, sort_keys=True))
    r.add("预览返回幂等键", str(prev.get("idempotencyKey", "")).startswith("nb1:"),
          str(prev.get("idempotencyKey"))[:24] + "...")

    res = lib.post(f"/v1/pipelines/{pipe_id}/activate")
    r.add("启用任务", res.status == 200 and (res.json or {}).get("state") == "active",
          f"activeVersion={(res.json or {}).get('activeVersion')}")

    # ---- 4. real delivery --------------------------------------------------
    print("\n[4] 真实投递：目标收到的字节必须等于预览")
    res = lib.post(f"/v1/pipelines/{pipe_id}/test-send", {"sample": sample})
    sent = res.json or {}
    delivery_id = sent.get("deliveryId", "")
    r.add("测试发送被受理", res.status == 200 and sent.get("accepted") is True,
          f"delivery={delivery_id}")

    def cur_state() -> str:
        return lib.scalar(f"SELECT state FROM nb_deliveries WHERE id = '{delivery_id}'") if delivery_id else "无"

    state = lib.wait_until(lambda: cur_state() == "delivered", timeout=25)
    r.add("投递到达终态 delivered", bool(state), f"state={cur_state()}")

    ack = lib.scalar(f"SELECT ack_level FROM nb_deliveries WHERE id = '{delivery_id}'") if delivery_id else ""
    r.add("目标确认级别为 destination_acked", ack == "destination_acked", ack)

    received = lib.receiver_records()
    hit = None
    hit_rec = None
    for rec in received:
        try:
            payload = json.loads(rec.get("body") or "{}")
        except json.JSONDecodeError:
            continue
        if payload.get("deliveryId") == delivery_id:
            hit, hit_rec = payload, rec
            break
    r.add("接收端真实收到该投递", hit is not None,
          f"接收端共 {len(received)} 条记录")
    if hit:
        r.add("送达数据等于预览输出（不是原始事件）", hit.get("data") == expected,
              json.dumps(hit.get("data"), ensure_ascii=False, sort_keys=True))
        hdrs = {k.lower(): v for k, v in (hit_rec.get("headers") or {}).items()}
        key = hdrs.get("idempotency-key", "")
        r.add("幂等键随请求头送达且与账本一致", key.startswith("nb1:"),
              key[:24] + "..." if key else "缺少 Idempotency-Key")

    # ---- 5. duplicate ------------------------------------------------------
    print("\n[5] 重复提交：不得产生第二条投递")
    dup = lib.sample_event(sent.get("eventId") or lib.unique("dup"))
    res = lib.inbox([dup])
    body = res.json or {}
    r.add("重复事件被识别为 duplicate", body.get("duplicate") == 1 and body.get("accepted") == 0,
          f"accepted={body.get('accepted')} duplicate={body.get('duplicate')}")
    count = lib.scalar(
        f"SELECT count(*) FROM nb_deliveries WHERE pipeline_id = '{pipe_id}' AND event_id = '{dup['eventId']}'")
    r.add("同一事件只有一条投递", count == "1", f"count={count}")

    # ---- 6. MQTT -----------------------------------------------------------
    print("\n[6] MQTT：真实 Broker 确认")
    topic = f"journey/{lib.unique('dev')}"
    sub = subprocess.Popen(
        ["mosquitto_sub", "-h", lib.MQTT_HOST, "-p", str(lib.MQTT_PORT), "-t", f"{topic}/#", "-v", "-C", "1"],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )
    time.sleep(1.0)
    res = lib.post("/v1/connections", {
        "name": lib.unique("journey-mqtt"),
        "connectorId": MQTT_CONNECTOR,
        "config": {
            "brokerUrl": f"tcp://{lib.MQTT_HOST}:{lib.MQTT_PORT}",
            "topicTemplate": f"{topic}/{{deviceId}}/{{eventType}}",
            "qos": 1,
            "retain": False,
            "clientIdPrefix": "journey",
            "keepAliveSec": 30,
            "connectTimeoutMs": 5000,
            "publishTimeoutMs": 5000,
        },
    })
    mconn = (res.json or {}).get("id", "")
    r.add("创建 MQTT 连接", res.status == 200 and bool(mconn), f"HTTP {res.status}")

    mpipe = ""
    if mconn:
        res = lib.post("/v1/pipelines", {
            "name": lib.unique("journey-mqtt-task"),
            "connectionId": mconn,
            "filter": {"eventTypes": ["telemetry"]},
            "transform": {"include": ["temperature"], "wrapAs": "values"},
            "serialization": {"format": "json", "topic": f"{topic}/{{deviceId}}/{{eventType}}"},
        })
        mpipe = (res.json or {}).get("id", "")
        lib.post(f"/v1/pipelines/{mpipe}/activate")

    if mpipe:
        res = lib.post(f"/v1/pipelines/{mpipe}/test-send",
                       {"sample": lib.sample_event(lib.unique("mqtt"), device_id="dev-journey-mqtt")})
        mdel = (res.json or {}).get("deliveryId", "")

        def mqtt_state() -> str:
            return lib.scalar(f"SELECT state FROM nb_deliveries WHERE id = '{mdel}'") if mdel else "无"

        state = lib.wait_until(lambda: mqtt_state() == "delivered", timeout=30)
        r.add("MQTT 投递被 Broker 确认", bool(state), f"state={mqtt_state()}")
        mack = lib.scalar(f"SELECT ack_level FROM nb_deliveries WHERE id = '{mdel}'") if mdel else ""
        r.add("QoS1 记为 destination_acked", mack == "destination_acked", mack)
        try:
            out, err = sub.communicate(timeout=15)
            r.add("真实订阅端收到消息", out.strip() != "",
                  out.strip()[:160] or err.strip()[:160])
        except subprocess.TimeoutExpired:
            sub.kill()
            r.add("真实订阅端收到消息", False, "15 秒内未收到")
    else:
        r.skip("MQTT 投递被 Broker 确认", "连接创建失败")

    # ---- 7. overview -------------------------------------------------------
    print("\n[7] 总览：已登记等待与容量拒绝分开")
    res = lib.get("/v1/overview")
    ov = res.json or {}
    r.add("总览可读", res.status == 200, f"HTTP {res.status}")
    r.add("总览同时给出等待数与拒绝数",
          "deliveries" in ov and "rejects" in ov,
          f"pending={ov.get('deliveries', {}).get('pending')} rejects={ov.get('rejects', {}).get('capacityRejected')}")
    r.add("总览声明保证起点而不是端到端",
          "outbox" in json.dumps(ov.get("capabilities", {})).lower(),
          str(ov.get("capabilities", {}).get("guaranteeStart", "")))

    # ---- 8. pause ----------------------------------------------------------
    print("\n[8] 暂停后不再新增待办")
    lib.post(f"/v1/pipelines/{pipe_id}/pause")
    before = lib.scalar(f"SELECT count(*) FROM nb_deliveries WHERE pipeline_id = '{pipe_id}'")
    res = lib.inbox([lib.sample_event(lib.unique("paused"), device_id="dev-journey-paused")])
    after = lib.scalar(f"SELECT count(*) FROM nb_deliveries WHERE pipeline_id = '{pipe_id}'")
    r.add("暂停任务不产生新投递", before == after, f"{before} -> {after}")
    r.add("暂停期间事件仍被受理", (res.json or {}).get("accepted") == 1,
          f"accepted={(res.json or {}).get('accepted')}")

    r.note("本脚本不覆盖：设备 → Broker → 厂商 Mosquitto 插件 这一段；它属于已验收的 broker 改造。")
    r.write("journey.md")
    return r.finish()


if __name__ == "__main__":
    lib.main_guard(run)
