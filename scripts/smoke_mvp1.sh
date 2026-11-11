#!/usr/bin/env bash
# MVP-1 smoke test against a running platform (any database): login, menus, group, device CRUD, device list,
# board counters. Prints one line per step and exits non-zero on the first unexpected answer.
# Usage: scripts/smoke_mvp1.sh http://127.0.0.1:19999 tenant@tenant.cn <password>
set -u
base="${1:?base url}"; user="${2:?email}"; pass="${3:?password}"
fail=0
json() { python3 -c "import sys,json; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1" 2>/dev/null; }
step() { printf '%-46s %s\n' "$1" "$2"; }
call() { # method path [body]
  curl -s -m 20 -X "$1" "$base$2" -H "x-token: ${token:-}" -H 'Content-Type: application/json' ${3:+-d "$3"}
}
expect200() { # label response
  code=$(printf '%s' "$2" | json "d.get('code')")
  if [ "$code" = "200" ]; then step "$1" "ok"; else step "$1" "FAILED: $(printf '%s' "$2" | head -c 300)"; fail=1; fi
}
resp=$(curl -s -m 20 -X POST "$base/api/v1/login" -H 'Content-Type: application/json' -d "{\"email\":\"$user\",\"password\":\"$pass\"}")
token=$(printf '%s' "$resp" | json "d['data']['token']")
[ -n "$token" ] && step "login" "ok" || { step "login" "FAILED: $resp"; exit 1; }
r=$(call GET /api/v1/user/detail); expect200 "user detail" "$r"
r=$(call GET /api/v1/sys_ui_elements/menu 2>/dev/null); step "menu (informational)" "$(printf '%s' "$r" | head -c 80)"
name="smoke-$(date +%s)"
r=$(call POST /api/v1/device/group "{\"name\":\"$name\",\"parent_id\":\"0\"}"); expect200 "create group" "$r"
r=$(call GET "/api/v1/device/group/tree"); expect200 "group tree" "$r"
r=$(call POST /api/v1/device "{\"name\":\"$name\",\"device_number\":\"$name\"}"); expect200 "create device" "$r"
id=$(printf '%s' "$r" | json "d['data']['id']")
r=$(call GET "/api/v1/device?page=1&page_size=10"); expect200 "list devices" "$r"
n=$(printf '%s' "$r" | json "d['data']['total']"); step "  total devices" "${n:-?}"
if [ -n "$id" ]; then
  r=$(call GET "/api/v1/device/detail/$id"); expect200 "device detail" "$r"
  r=$(call PUT /api/v1/device "{\"id\":\"$id\",\"name\":\"$name-renamed\"}"); expect200 "update device" "$r"
  r=$(call GET "/api/v1/device/group/relation/list?page=1&page_size=10"); step "group relations (informational)" "$(printf '%s' "$r" | head -c 60)"
  r=$(call DELETE "/api/v1/device/$id"); expect200 "delete device" "$r"
fi
r=$(call GET /api/v1/board/tenant/device/info); expect200 "board device counters" "$r"
r=$(call GET "/api/v1/board/trend"); expect200 "board trend" "$r"
exit $fail
