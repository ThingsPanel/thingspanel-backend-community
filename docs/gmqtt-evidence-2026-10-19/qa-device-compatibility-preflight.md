# QA device compatibility preflight

Added `deploy/mosquitto-community/qa-device-compatibility-preflight.sh` as a bounded, read-only aggregate check for the specific compatibility questions raised in the community broker review. It requires the `brokerqa` database and a schema named `mqtt_qa_*`, reads its DSN only from an environment variable, runs in a `READ ONLY` transaction with a 5-second statement timeout, and prints counts/categories only. It fails closed if the expected tables or `broker_safe_voucher_v1(text)` function are missing. It performs no automatic normalization, edits, or migrations.

The JSON output includes enabled/disabled/blank/other `devices.is_enabled` counts; blank or invalid vouchers according to the installed safe-voucher function; global duplicate `device_number` group count; enabled `device_topic_mappings` rows with `direction='down'`; and pending `expected_datas` grouped by `send_type`. No device IDs, numbers, voucher values, mapping names, topics, or payloads are selected into output.

## QA fixture verification

Ran against a generated private `mqtt_qa_*` schema in the dedicated `brokerqa` QA database. The fixture installed the repository's `sql/broker/011_safe_device_voucher.sql`, created only minimal synthetic tables/rows, and was dropped after the check. Expected aggregate categories were returned, including one enabled and one blank-state device, one invalid voucher, one duplicate-number group, one enabled custom down mapping, and one pending telemetry row. No real business schema or device data was queried or modified. The fixture covered successful output; missing-schema behavior is generic fail-closed and does not disclose connection details.

This establishes only that the aggregate SQL runs against a synthetic schema. It is **not G0 certification**. QoS and retained-message usage, actual broker behavior, credential compatibility on real records, and representative device samples remain unknown. A later authorized QA read-only run is needed for real counts; any remediation requires separate review and explicit change control.
