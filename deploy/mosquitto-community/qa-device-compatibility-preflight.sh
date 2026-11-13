#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: qa-device-compatibility-preflight.sh --schema NAME [--dsn-env NAME]

Read-only aggregate preflight for an isolated QA schema. The DSN is read from
the named environment variable (default TP_BROKER_MIGRATION_DSN) and never
printed. Output contains counts/categories only, never IDs or voucher values.
USAGE
}

schema=""
dsn_env="TP_BROKER_MIGRATION_DSN"
while (($#)); do
  case "$1" in
    --schema) (($# >= 2)) || { usage; exit 64; }; schema="$2"; shift 2 ;;
    --dsn-env) (($# >= 2)) || { usage; exit 64; }; dsn_env="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument" >&2; usage; exit 64 ;;
  esac
done

[[ "$schema" =~ ^mqtt_qa_[a-z0-9_]{1,54}$ ]] || { echo "schema must use the mqtt_qa_ prefix" >&2; exit 64; }
[[ "$dsn_env" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || { echo "invalid DSN environment variable name" >&2; exit 64; }
dsn="${!dsn_env:-}"
[[ -n "$dsn" ]] || { echo "required database DSN environment variable is unset" >&2; exit 64; }
command -v psql >/dev/null || { echo "psql is required" >&2; exit 69; }

db_name="$(PGOPTIONS="${PGOPTIONS:+$PGOPTIONS }-c statement_timeout=5000" psql "$dsn" -X -v ON_ERROR_STOP=1 -Atqc 'SELECT current_database()' 2>/dev/null)" || {
  echo "preflight failed: QA database connection failed" >&2
  exit 69
}
[[ "$db_name" == "brokerqa" ]] || { echo "preflight refused: database is not brokerqa" >&2; exit 64; }

sql=$(cat <<SQL
BEGIN READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL search_path TO "$schema";
WITH device_counts AS (
  SELECT
    count(*) FILTER (WHERE lower(btrim(is_enabled)) = 'enabled') AS enabled,
    count(*) FILTER (WHERE lower(btrim(is_enabled)) = 'disabled') AS disabled,
    count(*) FILTER (WHERE coalesce(btrim(is_enabled), '') = '') AS empty_enabled_state,
    count(*) FILTER (WHERE lower(btrim(is_enabled)) NOT IN ('enabled','disabled') AND coalesce(btrim(is_enabled),'') <> '') AS other_enabled_state,
    count(*) FILTER (
      WHERE coalesce(btrim(voucher), '') = ''
         OR broker_safe_voucher_v1(voucher) IS NULL
    ) AS invalid_voucher
  FROM devices
), duplicate_numbers AS (
  SELECT count(*) AS duplicate_number_groups
  FROM (
    SELECT device_number FROM devices
    WHERE coalesce(btrim(device_number), '') <> ''
    GROUP BY device_number HAVING count(*) > 1
  ) duplicate_groups
), mapping_counts AS (
  SELECT count(*) AS enabled_custom_down_mappings
  FROM device_topic_mappings
  WHERE enabled IS TRUE AND lower(btrim(direction)) = 'down'
), pending_counts AS (
  SELECT coalesce(jsonb_object_agg(send_type, count), '{}'::jsonb) AS pending_expected_datas_by_send_type
  FROM (
    SELECT send_type, count(*) AS count
    FROM expected_datas WHERE lower(btrim(status)) = 'pending'
    GROUP BY send_type
  ) grouped
)
SELECT jsonb_build_object(
  'device_enabled', device_counts.enabled,
  'device_disabled', device_counts.disabled,
  'device_is_enabled_empty', device_counts.empty_enabled_state,
  'device_is_enabled_other', device_counts.other_enabled_state,
  'device_invalid_voucher', device_counts.invalid_voucher,
  'global_duplicate_device_number_groups', duplicate_numbers.duplicate_number_groups,
  'enabled_custom_down_mappings', mapping_counts.enabled_custom_down_mappings,
  'pending_expected_datas_by_send_type', pending_counts.pending_expected_datas_by_send_type,
  'result_scope', 'read-only aggregate; QA schema only; not G0 certification'
)
FROM device_counts, duplicate_numbers, mapping_counts, pending_counts;
ROLLBACK;
SQL
)

if ! output="$(PGOPTIONS="${PGOPTIONS:+$PGOPTIONS }-c statement_timeout=5000" psql "$dsn" -X -v ON_ERROR_STOP=1 -Atqc "$sql" 2>/dev/null)"; then
  echo "preflight failed: schema missing, required table/function absent, query error, or 5-second timeout" >&2
  exit 2
fi
[[ -n "$output" ]] || { echo "preflight failed: no aggregate result" >&2; exit 2; }
printf '%s\n' "$output"
