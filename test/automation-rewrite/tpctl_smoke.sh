#!/usr/bin/env bash
# A26 smoke test: the CLI against the *running* service.
#
# Everything here goes over real HTTP to the Encore app; nothing is stubbed.
# The point is not that the endpoints work (the Go integration tests cover
# that) but that the CLI reaches the same endpoints with the same results, and
# that its exit codes tell a script what happened without reading Chinese.
#
# Usage:
#   cd encore-api && TP_AUTH_MODE=dev encore run --port 14000
#   bash test/automation-rewrite/tpctl_smoke.sh
set -u

BASE="${TPCTL_BASE_URL:-http://127.0.0.1:14000}"
TOKEN="${TPCTL_TOKEN:-a26-user:e2e-a26:tenant_admin:*}"
TPCTL="${TPCTL:-/tmp/tpctl}"
TMP="$(mktemp -d)"
trap 'if [ -n "${KEEP:-}" ]; then echo "TMP kept: $TMP"; else rm -rf "$TMP"; fi' EXIT

export TPCTL_BASE_URL="$BASE" TPCTL_TOKEN="$TOKEN"

PASS=0
FAIL=0

check() { # check <name> <expected-exit> <actual-exit>
  if [ "$2" = "$3" ]; then
    PASS=$((PASS + 1))
    printf '  ok   %s (exit %s)\n' "$1" "$3"
  else
    FAIL=$((FAIL + 1))
    printf '  FAIL %s (want exit %s, got %s)\n' "$1" "$2" "$3"
  fi
}

# stdout goes to the .json file, stderr to .err, so a human hint can never
# corrupt the JSON a script is parsing.
run_to() { # run_to <basename> <args...>
  local base="$1"; shift
  "$TPCTL" "$@" >"$TMP/$base.json" 2>"$TMP/$base.err"
}

say() { printf '\n== %s\n' "$1"; }

# ---------------------------------------------------------------------------
say "1. 能力/模板发现"
run_to bp blueprints list
check "blueprints list" 0 $?
python3 - "$TMP/bp.json" <<'PY'
import json, sys
env = json.load(open(sys.argv[1]))
assert env["ok"] is True, env
n = len(env["data"]["blueprints"])
print(f"     发现 {n} 个模板")
sys.exit(0 if n >= 4 else 1)
PY
check "至少四个内置模板" 0 $?

# ---------------------------------------------------------------------------
say "2. 非法定义：与网页同一套校验，退出码非零"
cat >"$TMP/body-bad.json" <<'JSON'
{"definition":{"kind":"automation","mode":"single","scope":"device",
 "trigger":{"groups":[]},"targets":{},"steps":[]}}
JSON
run_to bad rules validate --file "$TMP/body-bad.json"
check "非法定义让命令失败" 4 $?
python3 - "$TMP/bad.json" <<'PY'
import json, sys
env = json.load(open(sys.argv[1]))
assert env["ok"] is False, env
err = env["error"]
assert err["code"] == "invalid_definition", err
fields = err.get("fields") or []
assert fields, "没有字段级错误"
print(f"     字段级错误 {len(fields)} 条，例如 {fields[0]['field']}: {fields[0]['message']}")
PY
check "错误体里有字段名与代码" 0 $?

# ---------------------------------------------------------------------------
say "3. 合法定义：创建 → 读回"
cat >"$TMP/body-rule.json" <<'JSON'
{
  "name": "A26 CLI 高温提醒",
  "enabled": false,
  "definition": {
    "kind": "automation",
    "mode": "single",
    "scope": "device",
    "trigger": {"groups": [{"conditions": [{"source": "self", "signal": "telemetry",
      "key": "temperature", "operator": ">", "value": "30", "unit": "℃"}]}]},
    "freshness": {"max_age": "10m", "max_gap": "10m"},
    "targets": {"device_ids": ["dev-a26-cli"]},
    "steps": [{"kind": "action", "action": "telemetry", "identifier": "switch", "value": "1"}]
  }
}
JSON
run_to create rules create --file "$TMP/body-rule.json"
check "创建规则" 0 $?
RULE_ID="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["data"]["id"])' "$TMP/create.json")"
echo "     规则 id = $RULE_ID"

run_to get rules get "$RULE_ID"
check "读回规则" 0 $?
python3 - "$TMP/get.json" "$RULE_ID" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))["data"]
assert d["id"] == sys.argv[2], d
assert d["revision"] >= 1, d
print(f"     版本 {d['version']} 修订 {d['revision']}")
PY
check "读回内容一致" 0 $?

# ---------------------------------------------------------------------------
say "4. 变更预览 → 带令牌保存 → 旧令牌被拒"
python3 - "$TMP/body-rule.json" "$TMP/body-next.json" <<'PY'
import json, sys
body = json.load(open(sys.argv[1]))
body["definition"]["trigger"]["groups"][0]["conditions"][0]["value"] = "40"
json.dump({"definition": body["definition"]}, open(sys.argv[2], "w"))
PY
run_to preview rules preview "$RULE_ID" --file "$TMP/body-next.json"
check "变更预览" 0 $?
TOKEN_PV="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["data"]["token"])' "$TMP/preview.json")"
echo "     预览令牌 = $TOKEN_PV"

python3 - "$TMP/body-next.json" "$TMP/body-save.json" <<'PY'
import json, sys
defn = json.load(open(sys.argv[1]))
json.dump({"name": "A26 CLI 高温提醒", "definition": defn["definition"]}, open(sys.argv[2], "w"))
PY
run_to save rules update "$RULE_ID" --file "$TMP/body-save.json" --version 1 --preview-token "$TOKEN_PV"
check "带令牌保存成功" 0 $?

run_to stale rules update "$RULE_ID" --file "$TMP/body-save.json" --version 2 --preview-token "$TOKEN_PV"
check "同一令牌不能再用（版本已变）" 6 $?
python3 - "$TMP/stale.json" <<'PY'
import json, sys
env = json.load(open(sys.argv[1]))
assert env["error"].get("app_code") == "preview_stale", env["error"]
print("     app_code = preview_stale")
PY
check "冲突带机器可读的 app_code" 0 $?

# ---------------------------------------------------------------------------
say "5. 模拟 vs 真实执行：两条命令、两个端点"
cat >"$TMP/body-samples.json" <<'JSON'
{"samples":[{"at":"0s","device_id":"dev-a26-cli","signal":"telemetry","key":"temperature","value":"45"}]}
JSON
run_to sim rules simulate "$RULE_ID" --samples "$TMP/body-samples.json"
check "模拟" 0 $?
python3 - "$TMP/sim.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))["data"]
assert d["side_effects"] == 0, d
assert d["would_run"] == 1, d
acts = d.get("actions") or []
assert acts and all(a["outcome"] == "would_execute" for a in acts), acts
print(f"     模拟：将执行 {len(acts)} 步，副作用 {d['side_effects']} 次")
PY
check "模拟无副作用且命中一次" 0 $?

# ---------------------------------------------------------------------------
say "6. 运行日志：分页与状态集合"
run_to runs rules runs "$RULE_ID" --limit 5
check "运行日志" 0 $?
python3 - "$TMP/runs.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))["data"]
assert d["limit"] == 5, d
assert "total" in d and "runs" in d, d
print(f"     total={d['total']} 本页 {len(d['runs'])} 条")
PY
check "窗口被回显且带总数" 0 $?

run_to badstatus rules runs "$RULE_ID" --status not_a_status
check "拼错状态是参数错误" 4 $?

# ---------------------------------------------------------------------------
say "7. 旧命令名：同一端点、同一结果"
run_to legacy automations list --limit 3
check "automations list" 0 $?
python3 - "$TMP/legacy.json" <<'PY'
import json, sys
env = json.load(open(sys.argv[1]))
assert env["command"] == "automations.list", env
assert "rules" in env["data"], env
print("     旧名输出结构一致")
PY
check "旧名可用" 0 $?

run_to getnow rules get "$RULE_ID"
VER="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["data"]["version"])' "$TMP/getnow.json")"
run_to switch automations switch "$RULE_ID" --on --version "$VER"
check "automations switch --on" 0 $?
run_to get2 rules get "$RULE_ID"
python3 - "$TMP/get2.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))["data"]
assert d["enabled"] is True, d
print("     规则已启用")
PY
check "开关真的生效" 0 $?

# ---------------------------------------------------------------------------
say "8. 重复提交：同一意图同一个幂等键"
run_to dup1 rules create --file "$TMP/body-rule.json"
check "重复创建（第二次）" 0 $?
python3 - "$TMP/dup1.json" "$TMP/create.json" <<'PY'
import json, sys
a = json.load(open(sys.argv[1]))
b = json.load(open(sys.argv[2]))
assert a["idempotency_key"] == b["idempotency_key"], (a.get("idempotency_key"), b.get("idempotency_key"))
print(f"     幂等键一致：{a['idempotency_key']}")
PY
check "同意图幂等键一致" 0 $?

# ---------------------------------------------------------------------------
say "9. 无权限 / 结果未知"
run_to deny --token "someone-else:e2e-a26:tenant_viewer:*" rules get "$RULE_ID"
code=$?
if [ "$code" = "0" ] || [ "$code" = "5" ]; then
  check "不同角色按权限拒绝或允许" "$code" "$code"
else
  check "不同角色按权限拒绝或允许" 5 "$code"
fi

run_to unknown --base-url "http://127.0.0.1:1" --timeout 2s rules run "$RULE_ID"
check "写入无响应是“结果未知”" 7 $?
python3 - "$TMP/unknown.json" <<'PY'
import json, sys
env = json.load(open(sys.argv[1]))
assert env["error"]["code"] == "outcome_unknown", env["error"]
assert "--idempotency-key" in (env["error"].get("hint") or ""), env["error"]
print("     提示里给了安全的续跑方式")
PY
check "未知结果带续跑提示" 0 $?

# ---------------------------------------------------------------------------
say "10. 清理"
run_to del rules delete "$RULE_ID"
check "删除规则" 0 $?
run_to gone rules get "$RULE_ID"
check "删除后读不到" 3 $?

printf '\n结果: %d 通过, %d 失败\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
