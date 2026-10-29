#!/bin/sh
set -eu
: "${TP_AUTOMATION_SERVICE_TOKEN:?required}"
: "${TP_SOURCE_DEPLOYMENT_ID:?required}"
while :; do
  if ! wget -q -O /dev/null --timeout=10 \
      --header="Authorization: Bearer $TP_AUTOMATION_SERVICE_TOKEN" \
      --header="X-Automation-Deployment: $TP_SOURCE_DEPLOYMENT_ID" \
      --header='Content-Type: application/json' --post-data='{}' \
      http://encore-api:14000/internal/automation/tick; then
    echo 'Automation scheduler request failed' >&2
    exit 1
  fi
  sleep 5
done
