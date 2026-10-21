#!/usr/bin/env python3
"""Shared helpers for the northbound acceptance entry points.

Everything here talks to the real components: the northbound service, the two
plugin processes, a real PostgreSQL, a real Mosquitto and a real local HTTP
receiver. Nothing is mocked and nothing is stubbed.

Configuration comes from environment variables so the same scripts run on a
developer machine and in CI:

  NB_ENCORE_URL        northbound service base URL      (default http://127.0.0.1:4020)
  NB_TOKEN             management API bearer token      (default dev token)
  NB_SERVICE_TOKEN     source-to-service token
  NB_EN_DSN            northbound PostgreSQL DSN
  NB_COMMUNITY_DSN     community PostgreSQL DSN (optional, journey only)
  NB_RECEIVER_URL      local HTTP receiver              (default http://127.0.0.1:18090)
  NB_MQTT_HOST/PORT    Mosquitto for the MQTT plugin
  NB_REPORT_DIR        where reports are written
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

ENCORE = os.environ.get("NB_ENCORE_URL", "http://127.0.0.1:4020").rstrip("/")
TOKEN = os.environ.get("NB_TOKEN", "u1:t1:tenant_admin")
SERVICE_TOKEN = os.environ.get("NB_SERVICE_TOKEN", "dev-service-token")
RECEIVER = os.environ.get("NB_RECEIVER_URL", "http://127.0.0.1:18090").rstrip("/")
EN_DSN = os.environ.get("NB_EN_DSN", "postgresql://bw4dm:local@127.0.0.1:9500/northbound")
COMMUNITY_DSN = os.environ.get("NB_COMMUNITY_DSN", "")
MQTT_HOST = os.environ.get("NB_MQTT_HOST", "127.0.0.1")
MQTT_PORT = int(os.environ.get("NB_MQTT_PORT", "18830"))
REPORT_DIR = Path(os.environ.get("NB_REPORT_DIR", "docs/northbound/reports"))

TENANT = os.environ.get("NB_TENANT", "t1")


# --------------------------------------------------------------------------
# results
# --------------------------------------------------------------------------


@dataclass
class Check:
    name: str
    ok: bool
    detail: str = ""
    skipped: bool = False


@dataclass
class Report:
    title: str
    checks: list[Check] = field(default_factory=list)
    started: str = ""
    notes: list[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        self.started = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    def add(self, name: str, ok: bool, detail: str = "") -> bool:
        self.checks.append(Check(name, ok, detail))
        mark = "PASS" if ok else "FAIL"
        print(f"  [{mark}] {name}" + (f" — {detail}" if detail else ""))
        return ok

    def skip(self, name: str, why: str) -> None:
        self.checks.append(Check(name, False, why, skipped=True))
        print(f"  [SKIP] {name} — {why}")

    def note(self, text: str) -> None:
        self.notes.append(text)
        print(f"  note: {text}")

    @property
    def failed(self) -> list[Check]:
        return [c for c in self.checks if not c.ok and not c.skipped]

    @property
    def skipped(self) -> list[Check]:
        return [c for c in self.checks if c.skipped]

    def write(self, filename: str) -> Path:
        REPORT_DIR.mkdir(parents=True, exist_ok=True)
        path = REPORT_DIR / filename
        passed = len([c for c in self.checks if c.ok])
        lines = [
            f"# {self.title}",
            "",
            f"- 运行时间（UTC）：{self.started}",
            f"- 结论：**{'通过' if not self.failed else '未通过'}**",
            f"- 断言：{passed} 通过 / {len(self.failed)} 失败 / {len(self.skipped)} 跳过",
            "",
            "| # | 断言 | 结果 | 说明 |",
            "|---|---|---|---|",
        ]
        for i, c in enumerate(self.checks, 1):
            state = "跳过" if c.skipped else ("通过" if c.ok else "**失败**")
            detail = c.detail.replace("|", "\\|").replace("\n", " ")
            lines.append(f"| {i} | {c.name} | {state} | {detail} |")
        if self.notes:
            lines += ["", "## 说明", ""]
            lines += [f"- {n}" for n in self.notes]
        lines += [
            "",
            "> 本报告由脚本生成。跳过的断言不等于通过；失败项保留原始输出以便复核。",
            "",
        ]
        path.write_text("\n".join(lines), encoding="utf-8")
        print(f"\n报告已写入 {path}")
        return path

    def finish(self) -> int:
        if self.failed:
            print(f"\n{len(self.failed)} 项断言失败")
            return 1
        print("\n全部断言通过")
        return 0


# --------------------------------------------------------------------------
# HTTP
# --------------------------------------------------------------------------


class HttpResult:
    def __init__(self, status: int, body: bytes):
        self.status = status
        self.body = body

    @property
    def json(self):
        try:
            return json.loads(self.body)
        except Exception:
            return None

    def __repr__(self) -> str:
        return f"HTTP {self.status} {self.body[:200]!r}"


def call(method: str, path: str, body=None, *, token: str | None = None,
         headers: dict | None = None, timeout: float = 20.0) -> HttpResult:
    url = path if path.startswith("http") else ENCORE + path
    data = None
    hdrs = {"Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode()
        hdrs["Content-Type"] = "application/json"
    if token is not False:
        hdrs["Authorization"] = f"Bearer {token or TOKEN}"
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(url, data=data, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return HttpResult(resp.status, resp.read())
    except urllib.error.HTTPError as exc:
        return HttpResult(exc.code, exc.read())
    except urllib.error.URLError as exc:
        return HttpResult(0, str(exc).encode())


def get(path: str, **kw) -> HttpResult:
    return call("GET", path, **kw)


def post(path: str, body=None, **kw) -> HttpResult:
    return call("POST", path, body if body is not None else {}, **kw)


def inbox(events: list[dict], *, service_token: str | None = None) -> HttpResult:
    """Post source events through the real inbox door."""
    return post(
        "/v1/inbox/events",
        {"contractVersion": "northbound-source/v1", "sourceProfile": "ingress_v1", "events": events},
        token=False,
        headers={"X-Northbound-Token": service_token or SERVICE_TOKEN},
    )


# --------------------------------------------------------------------------
# PostgreSQL
# --------------------------------------------------------------------------


def sql(query: str, dsn: str | None = None, timeout: float = 30.0) -> list[list[str]]:
    """Run one query with psql and return rows as text cells."""
    dsn = dsn or EN_DSN
    proc = subprocess.run(
        ["psql", dsn, "-t", "-A", "-F", "\x1f", "-v", "ON_ERROR_STOP=1", "-c", query],
        capture_output=True, text=True, timeout=timeout,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"psql failed: {proc.stderr.strip()}")
    rows = []
    for line in proc.stdout.splitlines():
        if line == "":
            continue
        rows.append(line.split("\x1f"))
    return rows


def scalar(query: str, dsn: str | None = None) -> str:
    rows = sql(query, dsn)
    return rows[0][0] if rows and rows[0] else ""


def wait_until(fn, *, timeout: float = 30.0, interval: float = 0.5):
    """Poll fn until it returns a truthy value; return it or None."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        value = fn()
        if value:
            return value
        time.sleep(interval)
    return None


# --------------------------------------------------------------------------
# receiver control
# --------------------------------------------------------------------------


def receiver_mode(mode: str = "ok", delay: float = 0.0, fail_contains: str = "") -> None:
    post(f"{RECEIVER}/control", {"mode": mode, "delay": delay, "failContains": fail_contains},
         token=False)


def receiver_records(path: str = "/tmp/nb-qa/received.jsonl") -> list[dict]:
    p = Path(path)
    if not p.exists():
        return []
    out = []
    for line in p.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            out.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return out


def receiver_clear(path: str = "/tmp/nb-qa/received.jsonl") -> None:
    Path(path).write_text("", encoding="utf-8")


# --------------------------------------------------------------------------
# fixtures
# --------------------------------------------------------------------------


def unique(prefix: str) -> str:
    return f"{prefix}-{int(time.time() * 1000) % 100000000}-{os.getpid()}"


def sample_event(event_id: str, device_id: str = "dev-journey-01", event_type: str = "telemetry") -> dict:
    return {
        "contractVersion": "northbound-source/v1",
        "eventId": event_id,
        "tenantId": TENANT,
        "deviceId": device_id,
        "eventType": event_type,
        "occurredAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "receivedAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "source": "mqtt:ingress_v1",
        "subject": device_id,
        "data": {"temperature": 24.5, "humidity": 0.5, "battery": 91, "rssi": -60},
    }


def ensure_connector(connector_id: str) -> bool:
    res = get("/v1/connectors")
    body = res.json or {}
    for c in body.get("connectors", []):
        if c.get("id") == connector_id and c.get("state") == "ready":
            return True
    return False


def main_guard(fn) -> None:
    try:
        sys.exit(fn())
    except KeyboardInterrupt:
        sys.exit(130)
    except Exception as exc:  # noqa: BLE001 - a crashed script must not look like a pass
        print(f"\n脚本异常终止：{exc}", file=sys.stderr)
        sys.exit(2)
