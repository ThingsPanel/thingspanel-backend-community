#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
adapter_root="$(cd -- "$script_dir/.." && pwd -P)"
template_root="$adapter_root/templates/minimal-plugin"

compare_snapshot() {
  local path="$1"
  diff -u "$adapter_root/$path" <(sed 's#plugin.local/example-notification#encore.app/notification-adapters#g' "$template_root/$path")
}

for file in "$adapter_root"/sdk/*.go; do
  compare_snapshot "sdk/$(basename "$file")"
done
compare_snapshot "mock/provider.go"
echo "template SDK and mock sources match notification-adapters"
