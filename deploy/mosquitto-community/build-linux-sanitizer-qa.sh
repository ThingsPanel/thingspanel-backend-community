#!/bin/sh
set -eu

repo=$(cd "$(dirname "$0")/../.." && pwd)
stage=$(mktemp -d /tmp/tp-mosquitto-sanitizer-qa.XXXXXX)
trap 'rm -rf "$stage"' EXIT INT TERM
tar -C "$repo" \
	--exclude=.git --exclude=.cache --exclude=node_modules --exclude=docs \
	--exclude=files --exclude=vendor --exclude=*.log \
	-cf - . | tar -C "$stage" -xf -
docker buildx build --load \
	--build-context mosquitto-src=/tmp/tp-mqtt-20261019/mosquitto \
	-f "$stage/deploy/mosquitto-community/Dockerfile.linux-sanitizer-qa" \
	-t tp-mosquitto-plugin-asan-qa:linux-arm64 "$stage"
