#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: export-plugin.sh <smtp|dingtalk|aliyunsms|legacywebhook> <empty-or-new-destination>" >&2
  exit 2
}

[[ $# -eq 2 ]] || usage
provider="$1"
requested_destination="$2"
case "$provider" in
  smtp|dingtalk|aliyunsms|legacywebhook) ;;
  *) usage ;;
esac

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
adapter_root="$(cd -- "$script_dir/.." && pwd -P)"
provider_source="$adapter_root/$provider"
command_source="$adapter_root/cmd/$provider"
[[ -d "$provider_source" && -d "$command_source" ]] || { echo "provider source is incomplete" >&2; exit 1; }

destination_parent="$(cd -- "$(dirname -- "$requested_destination")" && pwd -P)"
destination_name="$(basename -- "$requested_destination")"
[[ "$destination_name" != "." && "$destination_name" != ".." ]] || { echo "destination must name a directory" >&2; exit 1; }
destination="$destination_parent/$destination_name"
case "$destination" in
  "$adapter_root"|"$adapter_root"/*) echo "destination must be outside notification-adapters" >&2; exit 1 ;;
esac
if [[ -e "$destination" ]]; then
  [[ ! -L "$destination" ]] || { echo "destination must not be a symlink" >&2; exit 1; }
  [[ -d "$destination" ]] || { echo "destination exists and is not a directory" >&2; exit 1; }
  actual_destination="$(cd -- "$destination" && pwd -P)"
  case "$actual_destination" in
    "$adapter_root"|"$adapter_root"/*) echo "destination must be outside notification-adapters" >&2; exit 1 ;;
  esac
  [[ -z "$(find "$destination" -mindepth 1 -maxdepth 1 -print -quit)" ]] || { echo "destination must be empty" >&2; exit 1; }
else
  mkdir -p "$destination"
fi

go_version="$(awk '$1 == "go" { print $2; exit }' "$adapter_root/go.mod")"
module_path="plugin.local/${provider}-notification"
mkdir -p "$destination/cmd/plugin"
cp -R "$adapter_root/sdk" "$destination/sdk"
cp -R "$adapter_root/mock" "$destination/mock"
cp -R "$provider_source" "$destination/$provider"
cp "$command_source"/*.go "$destination/cmd/plugin/"
cat > "$destination/go.mod" <<EOF
module $module_path

go $go_version
EOF

while IFS= read -r -d '' go_file; do
  temp_file="$go_file.export-tmp"
  sed "s#encore.app/notification-adapters#$module_path#g" "$go_file" > "$temp_file"
  mv "$temp_file" "$go_file"
done < <(find "$destination" -type f -name '*.go' -print0)

if grep -R -E -n 'encore\.app/|thingspanel-backend|encore-api/' "$destination" --include='*.go' >/dev/null; then
  echo "export contains a workspace/Core import" >&2
  exit 1
fi

cat > "$destination/README.md" <<'EOF'
# Independent @PROVIDER@ notification plugin

Generated locally by notification-adapters/scripts/export-plugin.sh. This tree
contains the selected provider, its executable and tests, the portable SDK
source snapshot, and the mock package used by SDK contract tests. It has no
external Go dependencies and no Encore/Core imports.

The SDK is not yet published. The module path `@MODULE_PATH@` is a local-only
placeholder so this export compiles without claiming a public download path.
Before publishing a repository, replace it with the repository module path you
control and review the copied SDK against the canonical source.

Build and verify offline with the pinned Go toolchain:

```sh
GOPROXY=off GOSUMDB=off GOMAXPROCS=2 GOFLAGS=-p=2 go test ./...
GOPROXY=off GOSUMDB=off GOMAXPROCS=2 GOFLAGS=-p=2 go vet ./...
GOPROXY=off GOSUMDB=off GOMAXPROCS=2 GOFLAGS=-p=2 go build -o /tmp/@PROVIDER@-notification-plugin ./cmd/plugin
```

The checksum list in `EXPORT-MANIFEST.sha256` covers every exported file
except itself. No secrets or runtime configuration are included.
EOF
sed -e "s/@PROVIDER@/$provider/g" -e "s#@MODULE_PATH@#$module_path#g" "$destination/README.md" > "$destination/README.md.tmp"
mv "$destination/README.md.tmp" "$destination/README.md"

if command -v shasum >/dev/null 2>&1; then
  checksum_command=(shasum -a 256)
elif command -v sha256sum >/dev/null 2>&1; then
  checksum_command=(sha256sum)
else
  echo "shasum or sha256sum is required" >&2
  exit 1
fi
(
  cd "$destination"
  find . -type f ! -name EXPORT-MANIFEST.sha256 -print | LC_ALL=C sort | while IFS= read -r file; do
    "${checksum_command[@]}" "$file"
  done > EXPORT-MANIFEST.sha256
)
echo "exported $provider plugin to $destination"
