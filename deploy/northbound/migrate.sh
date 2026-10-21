#!/usr/bin/env bash
# 北向服务（数据转发）数据库迁移。
#
# 把北向服务需要的表和动态菜单补齐，然后验证记录存在。重复执行安全。
#
# 为什么需要这个脚本：sql/northbound/001_source_outbox.sql 放在子目录里，不在
# initialize/pg_init.go 的自动执行范围（那个只按顺序扫 sql/<数字>.sql），所以
# 升级时很容易漏掉。漏掉的后果不是报错，而是源端登记静默失败——数据照常入库，
# 但一条都不会转发。这种错必须用脚本堵住。
#
# 用法：
#   COMMUNITY_DSN='postgres://user:pass@host:5432/db?sslmode=disable' \
#     ./migrate.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

: "${COMMUNITY_DSN:?必须设置 COMMUNITY_DSN（社区主库连接串）}"

psql_run() { psql "$COMMUNITY_DSN" -v ON_ERROR_STOP=1 -q "$@"; }

echo "==> 1/4 broker 上下文表（ingress_v1 启动前置条件）"
for f in "$repo/sql/broker/015_expected_context.sql" "$repo/sql/broker/016_plugin_context.sql"; do
  if [ -f "$f" ]; then
    echo "    $(basename "$f")"
    psql_run -f "$f"
  else
    echo "    跳过 $(basename "$f")：文件不存在（该版本可能已内置）"
  fi
done

echo "==> 2/4 源端 Outbox"
psql_run -f "$repo/sql/northbound/001_source_outbox.sql"

echo "==> 3/4 动态菜单和权限"
psql_run -f "$repo/sql/northbound/002_data_forwarding_menu.sql"

echo "==> 4/4 验证"
missing=0
for t in northbound_source_outbox northbound_source_rejects; do
  n=$(psql_run -tAc "SELECT to_regclass('public.$t') IS NOT NULL")
  if [ "$n" = "t" ]; then
    echo "    OK  $t"
  else
    echo "    缺失 $t" >&2
    missing=1
  fi
done
menu_count=$(psql_run -tAc "SELECT count(*) FROM public.sys_ui_elements WHERE element_code='data-service_northbound' AND route_path='view.data-service_northbound'")
if [ "$menu_count" = "1" ]; then
  echo "    OK  data-service_northbound menu"
else
  echo "    缺失或重复 data-service_northbound menu ($menu_count)" >&2
  missing=1
fi

if [ "$missing" -ne 0 ]; then
  echo "迁移失败：有表没建出来，不要继续启动服务。" >&2
  exit 1
fi

echo
echo "完成。Encore 侧的库由 encore run 自动建库并跑 encore-api/northbound/migrations/，不需要手工处理。"
