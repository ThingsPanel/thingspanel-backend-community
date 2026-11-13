#!/bin/sh
set -eu

umask 077
token_file=/tmp/tp-broker-fixture.token
head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > "$token_file"
chgrp mosquitto "$token_file"
chmod 0640 "$token_file"
export TP_BROKER_OWNER_URL=http://127.0.0.1:18084
export TP_BROKER_TOKEN_FILE="$token_file"
export TP_BROKER_CELL=qa-community
export TP_BROKER_INSTANCE=mosquitto-plugin-qa
export TP_BROKER_MAX_PENDING=${TP_QA_MAX_PENDING:-2}
export TP_BROKER_MAX_SESSIONS=${TP_QA_MAX_SESSIONS:-128}
printf 'Linux plugin QA config: owner=%s cell=%s instance=%s pending=%s sessions=%s token_bytes=%s\n' \
	"$TP_BROKER_OWNER_URL" "$TP_BROKER_CELL" "$TP_BROKER_INSTANCE" \
	"$TP_BROKER_MAX_PENDING" "$TP_BROKER_MAX_SESSIONS" "$(wc -c < "$token_file" | tr -d ' ')"

/usr/local/bin/mqtt-community-qa fixture -listen 127.0.0.1:18084 -token-file "$token_file" &
fixture_pid=$!
mkdir -p /mosquitto/data
chown mosquitto:mosquitto /mosquitto/data
broker_pid_file=/tmp/tp-mosquitto-qa.pid
restart_marker=/tmp/tp-mosquitto-qa.restart
/usr/local/sbin/mosquitto -c /mosquitto/config/mosquitto.conf &
broker_pid=$!
printf '%s\n' "$broker_pid" > "$broker_pid_file"
(
	while :; do
		if [ -f "$restart_marker" ]; then
			rm -f "$restart_marker"
			old_pid=$(cat "$broker_pid_file")
			kill "$old_pid" 2>/dev/null || true
			i=0
			while kill -0 "$old_pid" 2>/dev/null && [ "$i" -lt 100 ]; do sleep 0.05; i=$((i+1)); done
			sleep 0.5
			/usr/local/sbin/mosquitto -c /mosquitto/config/mosquitto.conf &
			broker_pid=$!
			printf '%s\n' "$broker_pid" > "$broker_pid_file"
		fi
		sleep 0.05
	done
) &
restart_watcher_pid=$!
cleanup() {
	current_broker_pid=$(cat "$broker_pid_file" 2>/dev/null || printf '%s' "$broker_pid")
	kill "$restart_watcher_pid" "$current_broker_pid" "$fixture_pid" 2>/dev/null || true
	wait "$restart_watcher_pid" "$broker_pid" "$fixture_pid" 2>/dev/null || true
	rm -f "$token_file"
	rm -f "$broker_pid_file" "$restart_marker"
}
trap cleanup EXIT INT TERM

sleep 1
kill -0 "$fixture_pid"
kill -0 "$broker_pid"
qa_raw_path=${TP_QA_RAW_PATH:-/tmp/mqtt-community-qa.raw.ndjson}
if [ "${TP_QA_SMOKE_ONLY:-0}" = "1" ]; then
	/usr/local/bin/mqtt-community-qa test -smoke-only -broker 127.0.0.1:1883 -fixture http://127.0.0.1:18084 -raw "$qa_raw_path"
elif [ "${TP_QA_REFRESH_REVOCATION:-0}" = "1" ]; then
	/usr/local/bin/mqtt-community-qa test -refresh-revocation-only \
		-refresh-report "${TP_QA_STRESS_REPORT:-/tmp/mqtt-community-refresh-revocation.json}" \
		-broker 127.0.0.1:1883 -fixture http://127.0.0.1:18084
elif [ "${TP_QA_TICK_PROBE:-0}" = "1" ]; then
	/usr/local/bin/mqtt-community-qa test -tick-probe \
		-stress-report "${TP_QA_STRESS_REPORT:-/tmp/mqtt-community-tick-probe.json}" \
		-broker 127.0.0.1:1883 -fixture http://127.0.0.1:18084
elif [ "${TP_QA_STRESS_MESSAGES:-0}" != "0" ] || [ "${TP_QA_STRESS_RECONNECTS:-0}" != "0" ]; then
	/usr/local/bin/mqtt-community-qa test -stress-messages "${TP_QA_STRESS_MESSAGES:-0}" \
		-stress-reconnects "${TP_QA_STRESS_RECONNECTS:-0}" \
		-stress-reconnect-concurrency "${TP_QA_STRESS_RECONNECT_CONCURRENCY:-1}" \
		-stress-report "${TP_QA_STRESS_REPORT:-/tmp/mqtt-community-stress.json}" \
		-broker 127.0.0.1:1883 -fixture http://127.0.0.1:18084
else
	/usr/local/bin/mqtt-community-qa test -broker 127.0.0.1:1883 -fixture http://127.0.0.1:18084 -raw "$qa_raw_path"
fi
