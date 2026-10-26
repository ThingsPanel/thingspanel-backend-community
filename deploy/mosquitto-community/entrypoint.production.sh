#!/bin/sh
set -eu
case "${1:-broker}" in
  broker)
    exec /usr/local/sbin/mosquitto -c /mosquitto/config/mosquitto.conf
    ;;
  owner)
    exec /usr/local/bin/tp-broker-owner
    ;;
  *)
    echo "usage: tp-broker-entrypoint {broker|owner}" >&2
    exit 64
    ;;
esac
