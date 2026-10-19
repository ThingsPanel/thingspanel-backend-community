#!/usr/bin/env bash
set -euo pipefail

# Isolated exercise only: a generated schema in the brokerqa database and a
# temporary 127.0.0.1 MQTT listener. It never changes production routes/devices.
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
checker="$script_dir/check-broker-schema.sh"
dsn="${TP_BROKER_MIGRATION_DSN:-}"
[[ -n "$dsn" ]] || { echo "TP_BROKER_MIGRATION_DSN is required" >&2; exit 64; }
command -v psql >/dev/null || { echo "psql is required" >&2; exit 69; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 69; }
[[ -x "$checker" ]] || { echo "migration checker is unavailable" >&2; exit 66; }

broker_bin="${MOSQUITTO_BIN:-/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto}"
default_pub="$(command -v mosquitto_pub || true)"
default_sub="$(command -v mosquitto_sub || true)"
pub_bin="${MOSQUITTO_PUB:-${default_pub:-/tmp/tp-mqtt-20261019/build/install/bin/mosquitto_pub}}"
sub_bin="${MOSQUITTO_SUB:-${default_sub:-/tmp/tp-mqtt-20261019/build/install/bin/mosquitto_sub}}"
for binary in "$broker_bin" "$pub_bin" "$sub_bin"; do
  [[ -x "$binary" ]] || { echo "required isolated Mosquitto binary is unavailable" >&2; exit 69; }
done

database="$(psql "$dsn" -X -v ON_ERROR_STOP=1 -Atqc 'SELECT current_database()' 2>/dev/null)" || {
  echo "QA database connection failed" >&2; exit 69;
}
[[ "$database" == "brokerqa" ]] || { echo "refusing drill outside brokerqa" >&2; exit 64; }

schema="mqtt_qa_drill_$(date +%s)_$$${RANDOM}"
[[ "$schema" =~ ^mqtt_qa_drill_[a-z0-9_]+$ ]] || { echo "generated schema name invalid" >&2; exit 70; }
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/tp-broker-drill.XXXXXX")"
schema_created=0
broker_pid=""
cleanup() {
  status=$?
  if [[ -n "$broker_pid" ]] && kill -0 "$broker_pid" 2>/dev/null; then
    kill -TERM "$broker_pid" 2>/dev/null || true
    wait "$broker_pid" 2>/dev/null || true
  fi
  if ((schema_created)); then
    psql "$dsn" -X -v ON_ERROR_STOP=1 -c "DROP SCHEMA IF EXISTS \"$schema\" CASCADE" >/dev/null 2>&1 || {
      echo "warning: isolated QA schema cleanup failed: $schema" >&2
      status=1
    }
  fi
  rm -rf "$work_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

psql "$dsn" -X -v ON_ERROR_STOP=1 -c "CREATE SCHEMA \"$schema\"" >/dev/null
schema_created=1
"$checker" --schema "$schema" --apply --target qa --confirm-qa-apply
"$checker" --schema "$schema" --apply --target qa --confirm-qa-apply
"$checker" --schema "$schema"

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
[[ "$port" != 1883 && "$port" != 9999 ]] || { echo "refusing reserved listener port" >&2; exit 70; }
nonce="$(python3 -c 'import secrets; print(secrets.token_hex(12))')"
cat > "$work_dir/mosquitto.conf" <<CONF
listener $port 127.0.0.1
allow_anonymous true
persistence false
log_dest file $work_dir/mosquitto.log
CONF
"$broker_bin" -c "$work_dir/mosquitto.conf" >"$work_dir/broker.stdout" 2>&1 &
broker_pid=$!
ready=0
for _ in $(seq 1 40); do
  if ! kill -0 "$broker_pid" 2>/dev/null; then
    echo "isolated broker exited before readiness" >&2
    tail -n 20 "$work_dir/broker.stdout" >&2 || true
    exit 1
  fi
  if python3 -c 'import socket,sys; s=socket.socket(); s.settimeout(.2); s.connect(("127.0.0.1",int(sys.argv[1]))); s.close()' "$port" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.1
done
if ((ready == 0)); then
  echo "isolated broker did not open its loopback port" >&2
  tail -n 20 "$work_dir/broker.stdout" >&2 || true
  exit 1
fi
"$sub_bin" -h 127.0.0.1 -p "$port" -t "qa/migration-drill/$nonce" -C 1 -W 8 >"$work_dir/sub.out" 2>"$work_dir/sub.err" &
sub_pid=$!
sleep 0.25
if ! kill -0 "$sub_pid" 2>/dev/null; then
  echo "isolated broker subscribe did not connect" >&2
  cat "$work_dir/sub.err" >&2 || true
  exit 1
fi
if ! "$pub_bin" -h 127.0.0.1 -p "$port" -t "qa/migration-drill/$nonce" -m "$nonce" -q 1 >/dev/null 2>&1; then
  echo "isolated broker publish smoke failed" >&2
  tail -n 20 "$work_dir/broker.stdout" >&2 || true
  exit 1
fi
if ! wait "$sub_pid"; then
  echo "isolated broker subscribe smoke failed" >&2
  cat "$work_dir/sub.err" >&2 || true
  exit 1
fi
[[ "$(cat "$work_dir/sub.out")" == "$nonce" ]] || { echo "isolated broker payload mismatch" >&2; exit 1; }
echo "QA migration/repeat/check passed; isolated MQTT loopback smoke passed on port $port"
echo "rollback drill: temporary schema $schema will be dropped; no route-control API/CLI was exercised"
echo "route cutover and rollback still require a separately designed, audited control plane"
