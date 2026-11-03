#!/usr/bin/env bash
# MVP-1 ingest smoke: create a device, publish one telemetry message on devices/telemetry (the payload shape the
# adapter expects: device_id + base64 JSON values), then read the value back through the HTTP API.
# Usage: scripts/smoke_mvp1_ingest.sh http://127.0.0.1:19999 tenant@tenant.cn <password> <mosquitto container>
set -u
base="${1:?}"; user="${2:?}"; pass="${3:?}"; broker="${4:?}"
json() { python3 -c "import sys,json; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1" 2>/dev/null; }
token=$(curl -s -m 20 -X POST "$base/api/v1/login" -H 'Content-Type: application/json' -d "{\"email\":\"$user\",\"password\":\"$pass\"}" | json "d['data']['token']")
call() { curl -s -m 20 -X "$1" "$base$2" -H "x-token: $token" -H 'Content-Type: application/json' ${3:+-d "$3"}; }
name="ingest-$(date +%s)"
id=$(call POST /api/v1/device "{\"name\":\"$name\",\"device_number\":\"$name\"}" | json "d['data']['id']")
[ -n "$id" ] || { echo "create device: FAILED"; exit 1; }
echo "device $id"
values=$(printf '{"temp":23.5,"online":true,"label":"hello"}' | base64 | tr -d '\n')
docker exec "$broker" mosquitto_pub -h 127.0.0.1 -t devices/telemetry -m "{\"device_id\":\"$id\",\"values\":\"$values\"}" || { echo "publish: FAILED"; exit 1; }
sleep 4
r=$(call GET "/api/v1/telemetry/datas/current/$id")
echo "current: $r" | head -c 400; echo
echo "$r" | json "[x['key']+'='+str(x.get('value')) for x in d['data']]" | grep -q "temp=23.5" && echo "ingest: ok" || { echo "ingest: FAILED (value not readable)"; exit 1; }
r=$(call GET "/api/v1/telemetry/datas/history?device_id=$id&key=temp&start_time=1&end_time=$(( $(date +%s) * 1000 + 60000 ))")
echo "history: $r" | head -c 300; echo
echo "$r" | json "len(d['data'])" | grep -qE '^[1-9]' && echo "history: ok" || { echo "history: FAILED (no rows)"; call DELETE "/api/v1/device/$id" >/dev/null; exit 1; }
call DELETE "/api/v1/device/$id" | json "d['code']" | sed 's/^/delete device code: /'
