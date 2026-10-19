#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
source_dir=${MOSQUITTO_SOURCE_DIR:-/tmp/tp-mqtt-20261019/mosquitto}
build_root=${MOSQUITTO_QA_BUILD_ROOT:-/tmp/tp-mqtt-20261019}
context="$build_root/linux-plugin-context-$(date +%s)"

test "$(git -C "$source_dir" describe --tags --exact-match)" = v2.1.2
test "$(git -C "$source_dir" rev-parse HEAD)" = 99fa50f30e325609394c324c8ff71cfbbe95d8ab
mkdir -p "$context"
rsync -a \
	--exclude='.git/' \
	--exclude='.cache/' \
	--exclude='files/' \
	--exclude='docs/' \
	--exclude='node_modules/' \
	--exclude='**/node_modules/' \
	"$repo_root/" "$context/"

docker build \
	--build-context "mosquitto-src=$source_dir" \
	-f "$context/deploy/mosquitto-community/Dockerfile.linux-plugin-qa" \
	-t tp-mosquitto-plugin-qa:linux-arm64 \
	"$context"
