#!/usr/bin/env bash
# 只读体检：看一个 ThingsPanel 测试栈的 HA 插件安装是不是健康。不改任何东西。
#   用法： check-test-env.sh <栈前缀> [后端端口]
#   例：   check-test-env.sh tp-ha-ui-acceptance-20261106 58999
#   栈前缀就是 `docker ps` 里 postgres/backend 容器名 “-postgres-1” 前面那一段。
set -uo pipefail
P="${1:?用法: $0 <栈前缀> [后端端口]}"
PORT="${2:-}"
PG="${P}-postgres-1"
q() { docker exec "$PG" sh -c "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -At -F ' | ' -c \"$1\"" 2>&1; }

echo "== 1. 容器（只列本栈和 Agent / 插件）"
docker ps -a --format '{{.Names}}\t{{.Status}}\t{{.Image}}' | grep -E "^(${P}|tp-agent-)" | sed 's/^/   /'

echo; echo "== 2. 后端健康"
if [ -n "$PORT" ]; then
  echo "   http://127.0.0.1:$PORT/health -> $(curl -s -m5 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/health")"
else echo "   （未给端口，跳过）"; fi

echo; echo "== 3. 插件安装记录 plugin_installations（state=enabled 且 health=ok 才算装好）"
q "select plugin_id, state, desired, 'generation='||generation, 'catalog='||catalog_version, 'health='||health, 'error='||error_code, 'step='||op_step, updated_at from plugin_installations"

echo; echo "== 4. 最近 3 条运行时身份（revoked=t 表示已吊销；最新一代应为 f）"
q "select 'gen='||generation, 'revoked='||(revoked_at is not null), 'expires='||expires_at from plugin_runtime_identities order by generation desc limit 3"

echo; echo "== 5. 服务插件登记 service_plugins（应有 1 行 homeassistant，sub_topic_prefix=plugin/homeassistant/）"
q "select service_identifier, name, service_config from service_plugins where service_identifier='homeassistant'"

echo; echo "== 6. 最近 8 条安装事件（失败时看这里的原因）"
q "select created_at, step, outcome, code from plugin_install_events order by id desc limit 8"
