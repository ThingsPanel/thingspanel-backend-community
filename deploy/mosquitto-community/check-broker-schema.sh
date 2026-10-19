#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage:
  check-broker-schema.sh --schema NAME [--dsn-env NAME]
  check-broker-schema.sh --schema mqtt_qa_NAME --apply --target qa --confirm-qa-apply [--dsn-env NAME]

Read-only check is the default. Apply is restricted to a database whose name
contains "qa" and a schema prefixed "mqtt_qa_". The DSN is read from the named
environment variable and is never printed.
USAGE
}

schema=""
dsn_env="TP_BROKER_MIGRATION_DSN"
apply=0
target=""
confirm=0
while (($#)); do
  case "$1" in
    --schema) (($# >= 2)) || { usage; exit 64; }; schema="$2"; shift 2 ;;
    --dsn-env) (($# >= 2)) || { usage; exit 64; }; dsn_env="$2"; shift 2 ;;
    --apply) apply=1; shift ;;
    --target) (($# >= 2)) || { usage; exit 64; }; target="$2"; shift 2 ;;
    --confirm-qa-apply) confirm=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 64 ;;
  esac
done

[[ "$schema" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] || { echo "invalid schema name" >&2; exit 64; }
[[ "$dsn_env" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || { echo "invalid DSN environment variable name" >&2; exit 64; }
dsn="${!dsn_env:-}"
[[ -n "$dsn" ]] || { echo "required database DSN environment variable is unset" >&2; exit 64; }
command -v psql >/dev/null || { echo "psql is required" >&2; exit 69; }

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
migration_dir="$repo_root/sql/broker"
contract_file="$script_dir/migration-required-columns.tsv"
[[ -d "$migration_dir" && -r "$contract_file" ]] || { echo "migration inputs are unavailable" >&2; exit 66; }
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

migrations=()
while IFS= read -r path; do migrations+=("$path"); done < <(find "$migration_dir" -maxdepth 1 -type f -name '[0-9][0-9][0-9]_*.sql' | sort)
((${#migrations[@]} > 0)) || { echo "no broker migrations found" >&2; exit 66; }
expected=1
for path in "${migrations[@]}"; do
  actual="$(basename "$path" | cut -c1-3)"
  wanted="$(printf '%03d' "$expected")"
  [[ "$actual" == "$wanted" ]] || { echo "broker migration sequence gap at $wanted" >&2; exit 66; }
  ((expected += 1))
done

if ((apply)); then
  [[ "$target" == "qa" && "$confirm" == 1 && "$schema" =~ ^mqtt_qa_[a-z0-9_]+$ ]] || {
    echo "apply requires --target qa, --confirm-qa-apply, and schema mqtt_qa_*" >&2
    exit 64
  }
  db_name="$(psql "$dsn" -X -v ON_ERROR_STOP=1 -Atqc 'SELECT current_database()')" || {
    echo "database connection failed" >&2; exit 69;
  }
  [[ "${db_name,,}" == "brokerqa" ]] || {
    echo "apply refused: explicit apply is limited to the brokerqa database" >&2
    exit 64
  }
  for path in "${migrations[@]}"; do
    name="$(basename "$path")"
    transformed="$temp_dir/$name"
    {
      printf 'SET LOCAL client_min_messages TO warning;\n'
      printf 'SET LOCAL search_path TO "%s";\n' "$schema"
      sed "s/public\\./\"$schema\"./g" "$path"
    } > "$transformed"
    echo "applying ${name}"
    if ! psql "$dsn" -X -v ON_ERROR_STOP=1 --single-transaction -f "$transformed" >/dev/null; then
      echo "migration failed: ${name}" >&2
      exit 1
    fi
  done
fi

contract_sql="$temp_dir/check-required-columns.sql"
{
  printf 'WITH expected(table_name, column_name) AS (VALUES\n'
  first=1
  while IFS=$'\t' read -r table columns || [[ -n "${table:-}${columns:-}" ]]; do
    [[ -z "${table:-}" || "$table" == \#* ]] && continue
    IFS=',' read -r -a column_list <<< "$columns"
    for column in "${column_list[@]}"; do
      if ((first)); then first=0; else printf ',\n'; fi
      printf "('%s','%s')" "$table" "$column"
    done
  done < "$contract_file"
  printf '\n)\n'
  cat <<'SQL'
SELECT expected.table_name || '.' || expected.column_name
FROM expected
LEFT JOIN information_schema.columns actual
  ON actual.table_schema = :'schema'
 AND actual.table_name = expected.table_name
 AND actual.column_name = expected.column_name
WHERE actual.column_name IS NULL
ORDER BY expected.table_name, expected.column_name;
SQL
} > "$contract_sql"

missing="$(psql "$dsn" -X -v ON_ERROR_STOP=1 -v schema="$schema" -At -F . -f "$contract_sql")" || {
  echo "schema check query failed" >&2
  exit 69
}
if [[ -n "$missing" ]]; then
  echo "broker schema is incomplete for ingress_v1; missing required columns:" >&2
  printf '%s\n' "$missing" >&2
  exit 2
fi
voucher_function="$(psql "$dsn" -X -v ON_ERROR_STOP=1 -v schema="$schema" -Atqc \
  "SELECT CASE WHEN to_regprocedure('${schema}.broker_safe_voucher_v1(text)') IS NULL THEN 'broker_safe_voucher_v1(text)' ELSE '' END")" || {
  echo "schema function check query failed" >&2
  exit 69
}
if [[ -n "$voucher_function" ]]; then
  echo "broker schema is incomplete for ingress_v1; missing required function: broker_safe_voucher_v1(text)" >&2
  exit 2
fi
echo "broker schema check passed: required columns and broker_safe_voucher_v1(text) exist in schema ${schema}"
