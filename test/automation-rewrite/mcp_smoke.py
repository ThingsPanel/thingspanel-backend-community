#!/usr/bin/env python3
"""A27 smoke test: the MCP server against the *running* service.

The Go tests cover the protocol in isolation with a fake backend. This one
covers the thing that actually matters to an assistant: that the same bad
definition gets the same answer whether it arrives over raw HTTP, through
tpctl, or through an MCP tool call — and that a read-only tool really is one.

Usage:
    cd encore-api && TP_AUTH_MODE=dev encore run --port 14000
    python3 test/automation-rewrite/mcp_smoke.py

Environment:
    TPCTL_MCP      path to the built MCP server (default /tmp/tpctl-mcp)
    TPCTL          path to the built CLI (default /tmp/tpctl)
    TPCTL_BASE_URL default http://127.0.0.1:14000
    TPCTL_TOKEN    default a dev token in tenant e2e-a27
"""

import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
import uuid

BASE = os.environ.get("TPCTL_BASE_URL", "http://127.0.0.1:14000")
MCP = os.environ.get("TPCTL_MCP", "/tmp/tpctl-mcp")
TPCTL = os.environ.get("TPCTL", "/tmp/tpctl")
TOKEN = os.environ.get("TPCTL_TOKEN", "a27-user:e2e-a27:tenant_admin:*")
OTHER_TOKEN = os.environ.get("TPCTL_OTHER_TOKEN", "b27-user:e2e-b27:tenant_admin:*")

# Every run gets its own names. The idempotency key is derived from the
# arguments, so a fixed name would make the second run of this script replay
# the first run's answer — correct behaviour, useless test.
RUN = uuid.uuid4().hex[:8]
DEVICE = "dev-a27-mcp-" + RUN

PASS = 0
FAIL = 0


def check(name, ok, detail=""):
    global PASS, FAIL
    if ok:
        PASS += 1
        print(f"  ok   {name}" + (f"  ({detail})" if detail else ""))
    else:
        FAIL += 1
        print(f"  FAIL {name}" + (f"  ({detail})" if detail else ""))
    return ok


def say(title):
    print(f"\n== {title}")


# ---------------------------------------------------------------------------
# A minimal MCP client: newline-delimited JSON-RPC over a child process.
# ---------------------------------------------------------------------------


class MCPClient:
    def __init__(self, token):
        env = dict(os.environ)
        env["TPCTL_BASE_URL"] = BASE
        env["TPCTL_TOKEN"] = token
        env.pop("TPCTL_TENANT", None)
        self.proc = subprocess.Popen(
            [MCP],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
            env=env,
        )
        self.next_id = 0

    def send(self, method, params=None, notify=False):
        msg = {"jsonrpc": "2.0", "method": method}
        if not notify:
            self.next_id += 1
            msg["id"] = self.next_id
        if params is not None:
            msg["params"] = params
        self.proc.stdin.write(json.dumps(msg) + "\n")
        self.proc.stdin.flush()
        if notify:
            return None
        line = self.proc.stdout.readline()
        if not line:
            err = self.proc.stderr.read()
            raise RuntimeError(f"server closed the stream: {err}")
        return json.loads(line)

    def tool(self, name, arguments=None):
        reply = self.send("tools/call", {"name": name, "arguments": arguments or {}})
        if "error" in reply:
            raise RuntimeError(f"protocol error calling {name}: {reply['error']}")
        return reply["result"]

    def close(self):
        try:
            self.proc.stdin.close()
        except Exception:
            pass
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()


def structured(result):
    """The structured half of a tool result, or the decoded text half."""
    if "structuredContent" in result:
        return result["structuredContent"]
    content = result.get("content") or []
    if content and content[0].get("type") == "text":
        return json.loads(content[0]["text"])
    return {}


def error_of(result):
    """The error envelope of a failed tool call."""
    body = structured(result)
    return body.get("error") or {}


def http_call(path, body):
    """Send the same request over plain HTTP and return (status, payload).

    Localhost is opened without a proxy on purpose: a corporate proxy answers
    a dead service with an HTML 502, and the test would then compare a code
    against a web page.
    """
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", "Authorization": "Bearer " + TOKEN},
        method="POST",
    )
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        raw = e.read().decode() or "{}"
        try:
            return e.code, json.loads(raw)
        except json.JSONDecodeError:
            raise RuntimeError(f"{path} answered with a non-JSON body: {raw[:200]}")


BAD_DEFINITION = {
    "kind": "automation",
    "mode": "single",
    "scope": "device",
    "trigger": {"groups": []},
    "targets": {},
    "steps": [],
}


def good_definition(limit):
    # The declared parameter must actually be referenced: a parameter nobody
    # uses is refused as unused_param, which is the check working correctly.
    return {
        "kind": "automation",
        "mode": "single",
        "scope": "device",
        "params": [
            {"key": "limit", "type": "number", "default": limit, "min": 0, "max": 100}
        ],
        "trigger": {
            "groups": [
                {
                    "conditions": [
                        {
                            "source": "self",
                            "signal": "telemetry",
                            "key": "temperature",
                            "operator": ">",
                            "value": "{{limit}}",
                            "unit": "℃",
                        }
                    ]
                }
            ]
        },
        "freshness": {"max_age": "10m", "max_gap": "10m"},
        "targets": {"device_ids": [DEVICE]},
        "steps": [
            {"kind": "action", "action": "telemetry", "identifier": "switch", "value": "1"}
        ],
    }


# ---------------------------------------------------------------------------


def main():
    c = MCPClient(TOKEN)
    rule_id = None

    try:
        # -------------------------------------------------------------------
        say("1. 握手")
        init = c.send(
            "initialize",
            {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "mcp-smoke", "version": "1"},
            },
        )
        res = init.get("result") or {}
        check("initialize 返回协议版本", res.get("protocolVersion") == "2025-06-18",
              res.get("protocolVersion", ""))
        check("声明了 tools 能力", "tools" in (res.get("capabilities") or {}))
        c.send("notifications/initialized", notify=True)

        # A notification must not be answered. If it were, the next read would
        # return that stray reply instead of the real one.
        ping = c.send("ping")
        check("通知不应答，ping 仍能对上", ping.get("id") == c.next_id)

        # -------------------------------------------------------------------
        say("2. 工具表")
        listed = (c.send("tools/list").get("result") or {}).get("tools") or []
        names = [t["name"] for t in listed]
        check("工具数量", len(names) == 23, str(len(names)))
        check("按名字排序（列表稳定）", names == sorted(names))
        for required in ("validate_rule", "preview_rule_change", "simulate_rule", "run_rule"):
            check(f"包含 {required}", required in names)

        # -------------------------------------------------------------------
        say("3. 发现：内置模板")
        bp = c.tool("list_blueprints")
        templates = (structured(bp) or {}).get("blueprints") or []
        check("能列出内置模板", len(templates) >= 4, f"{len(templates)} 个")

        # -------------------------------------------------------------------
        say("4. 同一份非法定义，三种入口说同一句话")
        # Validation answers 200 with a report, not an error: "no" is a
        # result, and the fields are the useful half of it.
        report = structured(c.tool("validate_rule", {"definition": BAD_DEFINITION}))
        check("MCP 判为不合法", report.get("valid") is False, str(report.get("valid")))
        mcp_fields = sorted({i.get("field") for i in (report.get("issues") or [])})
        check("MCP 给出字段级问题", bool(mcp_fields), ", ".join(mcp_fields[:3]))

        status, payload = http_call("/v1/rule-validation", {"definition": BAD_DEFINITION})
        check("HTTP 同样是 200 + valid=false", status == 200 and payload.get("valid") is False,
              f"HTTP {status}")
        http_fields = sorted({i.get("field") for i in (payload.get("issues") or [])})
        check("HTTP 与 MCP 报同样的字段", http_fields == mcp_fields,
              f"{http_fields} vs {mcp_fields}")

        cli_path = os.path.join("/tmp", "mcp-smoke-bad.json")
        with open(cli_path, "w") as f:
            json.dump({"definition": BAD_DEFINITION}, f)
        env = dict(os.environ, TPCTL_BASE_URL=BASE, TPCTL_TOKEN=TOKEN)
        cli = subprocess.run(
            [TPCTL, "rules", "validate", "--file", cli_path],
            capture_output=True, text=True, env=env,
        )
        check("CLI 退出码非零", cli.returncode == 4, str(cli.returncode))
        cli_fields = []
        try:
            cli_fields = sorted({f.get("field") for f in json.loads(cli.stdout)["error"]["fields"]})
        except Exception:
            pass
        check("CLI 与 MCP 报同样的字段", cli_fields == mcp_fields,
              f"{cli_fields} vs {mcp_fields}")
        os.unlink(cli_path)

        # -------------------------------------------------------------------
        say("5. 建规则 → 预览 → 带令牌保存")
        created = structured(c.tool("create_rule", {
            "name": "A27 MCP 高温提醒 " + RUN,
            "enabled": True,
            "definition": good_definition("30"),
        }))
        rule_id = created.get("id")
        check("创建成功", bool(rule_id), str(rule_id))
        version = created.get("version", 1)

        preview = structured(c.tool("preview_rule_change", {
            "rule_id": rule_id, "definition": good_definition("40"),
        }))
        token = preview.get("token")
        check("预览返回令牌", bool(token), str(token))

        saved = structured(c.tool("update_rule", {
            "rule_id": rule_id, "definition": good_definition("40"),
            "version": version, "preview_token": token,
        }))
        check("带令牌保存成功", saved.get("version") == version + 1,
              f"v{version} → v{saved.get('version')}")

        stale = c.tool("update_rule", {
            "rule_id": rule_id, "definition": good_definition("50"),
            "version": version + 1, "preview_token": token,
        })
        stale_err = error_of(stale)
        check("旧令牌被拒", stale.get("isError") is True)
        check("拒绝原因是预览失效",
              (stale_err.get("app_code") or stale_err.get("code")) == "preview_stale",
              str(stale_err.get("app_code") or stale_err.get("code")))

        # -------------------------------------------------------------------
        say("6. 模拟：只看不动")
        before = structured(c.tool("list_runs", {"rule_id": rule_id}))
        sim = structured(c.tool("simulate_rule", {
            "rule_id": rule_id,
            "samples": [
                {"at": "0s", "device_id": DEVICE, "signal": "telemetry",
                 "key": "temperature", "value": "60"},
            ],
        }))
        check("模拟会触发一次", sim.get("would_run") == 1, str(sim.get("would_run")))
        check("模拟零副作用", sim.get("side_effects") == 0, str(sim.get("side_effects")))
        check("模拟报告了会做什么", bool(sim.get("actions")), str(len(sim.get("actions") or [])))
        after = structured(c.tool("list_runs", {"rule_id": rule_id}))
        check("模拟没有产生运行记录", after.get("total") == before.get("total"),
              f"{before.get('total')} → {after.get('total')}")

        # -------------------------------------------------------------------
        say("7. 真实执行 + 超时重试不重复动手")
        run1 = structured(c.tool("run_rule", {"rule_id": rule_id, "device_id": DEVICE}))
        check("执行产生了运行", bool(run1.get("id")), str(run1.get("id")))
        run2 = structured(c.tool("run_rule", {"rule_id": rule_id, "device_id": DEVICE}))
        check("同样的调用是同一个运行（没有开两次门）",
              run1.get("id") == run2.get("id"),
              f"{run1.get('id')} vs {run2.get('id')}")
        runs = structured(c.tool("list_runs", {"rule_id": rule_id}))
        check("运行记录只有一条", runs.get("total") == 1, str(runs.get("total")))

        # -------------------------------------------------------------------
        say("8. 排障")
        explain = structured(c.tool("explain_rule", {"rule_id": rule_id, "device_id": DEVICE}))
        check("能解释为什么跑/没跑", bool(explain), str(list(explain.keys())[:3]))
        detail = structured(c.tool("get_run", {"run_id": run1.get("id")}))
        check("能读到运行详情", detail.get("id") == run1.get("id"))

        # -------------------------------------------------------------------
        say("9. 租户不能被模型指定")
        for key in ("tenant", "tenant_id", "tenantId"):
            bad = c.tool("list_rules", {key: "someone-else"})
            check(f"拒绝 {key} 参数", bad.get("isError") is True,
                  error_of(bad).get("code", ""))

        # -------------------------------------------------------------------
        say("10. 跨租户：别人的规则读不到")
        other = MCPClient(OTHER_TOKEN)
        try:
            other.send("initialize", {"protocolVersion": "2025-06-18"})
            stolen = other.tool("get_rule", {"rule_id": rule_id})
            check("另一个租户读不到这条规则", stolen.get("isError") is True,
                  error_of(stolen).get("code", ""))
        finally:
            other.close()

        # -------------------------------------------------------------------
        say("11. 删除")
        gone = c.tool("delete_rule", {"rule_id": rule_id})
        check("删除成功", gone.get("isError") is not True)
        missing = c.tool("get_rule", {"rule_id": rule_id})
        check("删掉后读不到", missing.get("isError") is True,
              error_of(missing).get("code", ""))
        rule_id = None

    finally:
        if rule_id:
            try:
                c.tool("delete_rule", {"rule_id": rule_id, "force": True})
            except Exception:
                pass
        c.close()

    print(f"\n{'=' * 46}")
    print(f"通过 {PASS} 项，失败 {FAIL} 项")
    return 0 if FAIL == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
