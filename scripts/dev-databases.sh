#!/usr/bin/env bash
# Start throwaway databases for the PostgreSQL-vs-MySQL differential tests and print the environment to export.
# Usage: scripts/dev-databases.sh up|down        (eval "$(scripts/dev-databases.sh env)" to export the variables)
# Containers are named tp-dev-*; "down" removes them. Nothing here touches a real installation.
set -euo pipefail
PG_ORACLE=tp-dev-pg-oracle   # PostgreSQL + TimescaleDB with sql/1.sql..32.sql and the component migrations applied
MY=tp-dev-mysql
PG_AUTO=tp-dev-pg-plain
MY_AUTO=tp-dev-mysql-auto
cmd="${1:-up}"
root="$(cd "$(dirname "$0")/.." && pwd)"
case "$cmd" in
up)
  docker rm -f $PG_ORACLE $MY $PG_AUTO $MY_AUTO >/dev/null 2>&1 || true
  docker run -d --name $PG_ORACLE -e POSTGRES_PASSWORD=pw -p 56001:5432 timescale/timescaledb:2.31.0-pg16 >/dev/null
  docker run -d --name $MY -e MYSQL_ROOT_PASSWORD=pw -e MYSQL_DATABASE=plat -p 56002:3306 mysql:8.4 --log-bin-trust-function-creators=1 >/dev/null
  docker run -d --name $PG_AUTO -e POSTGRES_PASSWORD=pw -p 56003:5432 postgres:16 >/dev/null
  docker run -d --name $MY_AUTO -e MYSQL_ROOT_PASSWORD=pw -e MYSQL_DATABASE=auto -p 56004:3306 mysql:8.4 >/dev/null
  until docker exec $PG_ORACLE pg_isready -U postgres >/dev/null 2>&1; do sleep 2; done; sleep 4
  for i in $(seq 1 32); do docker exec -i $PG_ORACLE psql -U postgres -q -v ON_ERROR_STOP=0 < "$root/sql/$i.sql" >/dev/null 2>&1 || true; done
  for f in "$root"/sql/service_access/*.sql; do docker exec -i $PG_ORACLE psql -U postgres -q < "$f" >/dev/null 2>&1 || true; done
  until docker exec $MY mysqladmin ping -uroot -ppw --silent >/dev/null 2>&1; do sleep 2; done
  until docker exec $MY_AUTO mysqladmin ping -uroot -ppw --silent >/dev/null 2>&1; do sleep 2; done
  sleep 3
  echo "ready; run: eval \"\$(scripts/dev-databases.sh env)\""
  ;;
env)
  cat <<ENV
export TEST_PG_SCHEMA_DSN='postgres://postgres:pw@127.0.0.1:56001/postgres?sslmode=disable'
export TP_MYSQL_TEST_HOST=127.0.0.1 TP_MYSQL_TEST_PORT=56002 TP_MYSQL_TEST_USER=root TP_MYSQL_TEST_PASSWORD=pw TP_MYSQL_TEST_DB=plat
export TP_AUTO_SCHEMA_PG_DSN='postgres://postgres:pw@127.0.0.1:56003/postgres?sslmode=disable'
export TP_AUTO_SCHEMA_MYSQL_DSN='root:pw@tcp(127.0.0.1:56004)/auto?parseTime=true&clientFoundRows=true&loc=UTC&multiStatements=true'
ENV
  ;;
down)
  docker rm -f $PG_ORACLE $MY $PG_AUTO $MY_AUTO >/dev/null 2>&1 || true
  ;;
*) echo "usage: $0 up|env|down" >&2; exit 2;;
esac
