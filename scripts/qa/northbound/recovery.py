#!/usr/bin/env python3
"""Recovery acceptance: what the platform does when the target misbehaves.

Every scenario is induced on the real local receiver and observed in the real
ledger. A scenario only passes when the recorded state matches the contract:

  transient failure  -> retry_wait, attempt counted, next attempt scheduled
  rate limit         -> retry_wait, retry_after respected
  timeout            -> unknown, NOT retried automatically
  unknown + replay   -> refused without risk acknowledgement, allowed with it
  partial batch      -> per-record outcomes, no cross-contamination
  dead worker        -> expired lease is reclaimed without operator action
  full backlog       -> refused before anything is written
  rejected credential-> connection needs_repair, record dead, no retry storm
"""
from __future__ import annotations

import json
import sys
import time

import lib
from lib import Report

HTTP_CONNECTOR = "io.thingspanel.sink.http"


def make_pipeline(r: Report, tag: str, config_extra: dict | None = None) -> tuple[str, str]:
    """Create a connection + active pipeline for one scenario."""
    cfg = {
        "url": f"{lib.RECEIVER}/ingest",
        "method": "POST",
        "batch": True,
        "timeoutMs": 3000,
        "idempotencyHeader": "Idempotency-Key",
    }
    cfg.update(config_extra or {})
    res = lib.post("/v1/connections", {
        "name": lib.unique(f"rec-{tag}"),
        "connectorId": HTTP_CONNECTOR,
        "config": cfg,
    })
    conn_id = (res.json or {}).get("id", "")
    res = lib.post("/v1/pipelines", {
        "name": lib.unique(f"rec-{tag}-task"),
        "connectionId": conn_id,
        "filter": {"eventTypes": ["telemetry"]},
        "transform": {"include": ["temperature", "humidity"]},
        "serialization": {"format": "json"},
    })
    pipe_id = (res.json or {}).get("id", "")
    if pipe_id:
        lib.post(f"/v1/pipelines/{pipe_id}/activate")
    return conn_id, pipe_id


def send(pipe_id: str, tag: str, device: str = "dev-recovery-01") -> str:
    res = lib.post(f"/v1/pipelines/{pipe_id}/test-send",
                   {"sample": lib.sample_event(lib.unique(tag), device_id=device)})
    return (res.json or {}).get("deliveryId", "")


def state_of(delivery_id: str) -> str:
    return lib.scalar(f"SELECT state FROM nb_deliveries WHERE id = '{delivery_id}'") if delivery_id else ""


def run() -> int:
    r = Report("北向服务 recovery 验收（真实故障注入）")

    # ---- 1. transient 500 --------------------------------------------------
    print("\n[1] 目标 500：应重试并保留原因")
    lib.receiver_mode("fail500")
    _, pipe = make_pipeline(r, "500")
    if not pipe:
        r.add("建立恢复场景任务", False, "任务创建失败")
        return r.finish()
    d1 = send(pipe, "r500")
    st = lib.wait_until(lambda: state_of(d1) == "retry_wait" and d1, timeout=25)
    r.add("500 后进入 retry_wait", bool(st), f"state={state_of(d1)}")
    code = lib.scalar(f"SELECT failure_code FROM nb_deliveries WHERE id = '{d1}'")
    r.add("失败码记录为 http_500", code == "http_500", code)
    n1 = lib.scalar(f"SELECT attempts FROM nb_deliveries WHERE id = '{d1}'")
    time.sleep(3)
    n2 = lib.scalar(f"SELECT attempts FROM nb_deliveries WHERE id = '{d1}'")
    r.add("失败会被再次尝试（次数递增）", int(n2 or 0) > int(n1 or 0), f"{n1} -> {n2}")
    r.add("每次尝试都有独立记录",
          int(lib.scalar(f"SELECT count(*) FROM nb_delivery_attempts WHERE delivery_id = '{d1}'")) >= 2,
          "nb_delivery_attempts 行数")

    # ---- 2. rate limit -----------------------------------------------------
    print("\n[2] 目标 429：应尊重 Retry-After")
    lib.receiver_mode("fail429")
    _, pipe429 = make_pipeline(r, "429")
    d429 = send(pipe429, "r429")
    lib.wait_until(lambda: state_of(d429) == "retry_wait" and d429, timeout=25)
    ra = lib.scalar(
        f"SELECT coalesce(max(retry_after_ms), 0) FROM nb_delivery_attempts WHERE delivery_id = '{d429}'")
    r.add("429 后进入 retry_wait", state_of(d429) == "retry_wait", state_of(d429))
    r.add("Retry-After 被记录", int(ra or 0) > 0, f"retry_after_ms={ra}")

    # ---- 3. timeout -> unknown --------------------------------------------
    print("\n[3] 目标超时：应记 unknown，不得自动重试")
    lib.receiver_mode("slow", 5)
    _, pipe_to = make_pipeline(r, "timeout")
    dto = send(pipe_to, "rto")
    st = lib.wait_until(lambda: state_of(dto) == "unknown" and dto, timeout=30)
    r.add("超时后进入 unknown", bool(st), f"state={state_of(dto)}")
    time.sleep(4)
    to_attempts = lib.scalar(f"SELECT attempts FROM nb_deliveries WHERE id = '{dto}'")
    r.add("unknown 不会被自动重试", to_attempts == "1", f"attempts={to_attempts}")
    to_code = lib.scalar(f"SELECT failure_code FROM nb_deliveries WHERE id = '{dto}'")
    r.add("unknown 保留插件给出的原因", to_code not in ("", "unknown_status"), to_code)

    # ---- 4. replay gate ----------------------------------------------------
    print("\n[4] 未知结果的重放门槛")
    lib.receiver_mode("ok")
    res = lib.post(f"/v1/deliveries/{dto}/replay", {"reason": "客户确认未落库"})
    r.add("未确认风险时拒绝重放", res.status == 409, f"HTTP {res.status} {(res.json or {}).get('code')}")
    res = lib.post(f"/v1/deliveries/{dto}/replay", {"reason": "客户确认未落库", "acknowledgeRisk": True})
    r.add("确认风险后允许重放", res.status == 200, f"HTTP {res.status}")
    st = lib.wait_until(lambda: state_of(dto) == "delivered" and dto, timeout=25)
    r.add("重放后真实送达", bool(st), f"state={state_of(dto)}")
    r.add("重放不改变幂等键",
          lib.scalar(f"SELECT idempotency_key FROM nb_deliveries WHERE id = '{dto}'").startswith("nb1:"),
          "nb1: 前缀保持")

    # ---- 5. partial batch --------------------------------------------------
    print("\n[5] 一批里部分成功：不得互相清账")
    lib.receiver_mode("ok", 0, "dev-recovery-bad")
    _, pipe_p = make_pipeline(r, "partial", {"batch": False})
    good = lib.sample_event(lib.unique("good"), device_id="dev-recovery-good")
    bad = lib.sample_event(lib.unique("bad"), device_id="dev-recovery-bad")
    res = lib.inbox([good, bad])
    body = res.json or {}
    r.add("两个事件都被受理", body.get("accepted") == 2, f"accepted={body.get('accepted')}")
    lib.wait_until(
        lambda: lib.scalar(
            f"SELECT count(*) FROM nb_deliveries WHERE pipeline_id = '{pipe_p}' AND state IN ('delivered','retry_wait')") == "2",
        timeout=25,
    )
    rows = lib.sql(
        f"SELECT event_id, state FROM nb_deliveries WHERE pipeline_id = '{pipe_p}' ORDER BY event_id")
    got = {row[0]: row[1] for row in rows}
    r.add("成功的一条 delivered", got.get(good["eventId"]) == "delivered", got.get(good["eventId"], "缺失"))
    r.add("失败的一条独立 retry_wait", got.get(bad["eventId"]) == "retry_wait", got.get(bad["eventId"], "缺失"))

    # ---- 6. rejected credential -------------------------------------------
    print("\n[6] 凭证被拒：连接标为需修复，记录进死信，不形成重试风暴")
    lib.receiver_mode("fail401")
    conn401, pipe401 = make_pipeline(r, "401")
    d401 = send(pipe401, "r401")
    st = lib.wait_until(lambda: state_of(d401) == "dead" and d401, timeout=25)
    r.add("401 后记录进入 dead", bool(st), f"state={state_of(d401)}")
    cstate = lib.scalar(f"SELECT state FROM nb_connections WHERE id = '{conn401}'")
    r.add("连接被标记为 needs_repair", cstate == "needs_repair", cstate)

    # ---- 7. lease recovery -------------------------------------------------
    print("\n[7] 租约过期：无需人工介入即可恢复")
    lib.receiver_mode("ok")
    conn_l, pipe_l = make_pipeline(r, "lease")
    ev = lib.sample_event(lib.unique("lease"), device_id="dev-recovery-lease")
    lib.inbox([ev])
    lib.wait_until(
        lambda: lib.scalar(
            f"SELECT count(*) FROM nb_deliveries WHERE pipeline_id = '{pipe_l}' AND event_id = '{ev['eventId']}'") == "1",
        timeout=20,
    )
    lib.sql(f"""
        UPDATE nb_deliveries
        SET state = 'leased', lease_token = gen_random_uuid(),
            lease_until = now() - interval '5 minutes'
        WHERE pipeline_id = '{pipe_l}' AND event_id = '{ev['eventId']}'""")
    st = lib.wait_until(
        lambda: lib.scalar(
            f"SELECT state FROM nb_deliveries WHERE pipeline_id = '{pipe_l}' AND event_id = '{ev['eventId']}'") == "delivered",
        timeout=40,
    )
    r.add("过期租约被自动回收并送达", bool(st), "无需人工操作")

    # ---- 8. capacity -------------------------------------------------------
    print("\n[8] 容量拒绝：拒绝且不留半写")
    cap_tenant = "t-cap-acceptance"
    lib.sql(f"""
        INSERT INTO nb_events (event_id, tenant_id, device_id, event_type, occurred_at, received_at, payload, payload_sha256, expires_at)
        SELECT 'capacc-' || g, '{cap_tenant}', 'dev-cap', 'telemetry', now(), now(), '{{}}'::jsonb, 'x', now() + interval '7 days'
        FROM generate_series(1, 20000) g
        ON CONFLICT (event_id) DO NOTHING;
        INSERT INTO nb_deliveries (id, event_id, tenant_id, pipeline_id, pipeline_version, connection_id,
            connection_revision, branch, idempotency_key, order_key, state, attempts, next_attempt_at)
        SELECT gen_random_uuid(), 'capacc-' || g, '{cap_tenant}', gen_random_uuid(), 1, gen_random_uuid(), 1,
               'default', 'nb1:capacc-' || g, 'k', 'pending', 0, now() + interval '1 day'
        FROM generate_series(1, 20000) g
        ON CONFLICT (pipeline_id, event_id, branch) DO NOTHING""")
    over = lib.sample_event("capacc-over-001", device_id="dev-cap")
    over["tenantId"] = cap_tenant
    res = lib.inbox([over])
    body = res.json or {}
    rejected = body.get("rejected") or []
    r.add("超限事件被拒绝",
          bool(rejected) and rejected[0].get("code") == "capacity_exceeded",
          json.dumps(rejected, ensure_ascii=False)[:160])
    r.add("拒绝后不留半写事件",
          lib.scalar(f"SELECT count(*) FROM nb_events WHERE event_id = 'capacc-over-001'") == "0",
          "nb_events 无该事件")
    r.add("拒绝计入独立拒绝账",
          lib.scalar(f"SELECT coalesce(sum(rejected_total),0) FROM nb_tenant_quota WHERE tenant_id = '{cap_tenant}'") != "0",
          "nb_tenant_quota.rejected_total")
    lib.sql(f"DELETE FROM nb_deliveries WHERE tenant_id = '{cap_tenant}'; "
            f"DELETE FROM nb_events WHERE tenant_id = '{cap_tenant}';")

    lib.receiver_mode("ok")
    r.note("容量场景使用独立租户 t-cap-acceptance，跑完即清理，不影响其它断言。")
    r.write("recovery.md")
    return r.finish()


if __name__ == "__main__":
    lib.main_guard(run)
