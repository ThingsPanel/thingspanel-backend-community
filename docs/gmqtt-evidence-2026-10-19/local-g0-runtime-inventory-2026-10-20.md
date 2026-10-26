# Local G0 runtime inventory (bounded, read-only)

Date: 2026-10-20 (Asia/Shanghai)

This is a snapshot of the developer machine's currently running `thingspanel-install-backend-1` and `thingspanel-install-gmqtt-1` containers. It is not a customer deployment, customer data sample, or physical-device verification.

The backend process environment was read in-process to obtain its database and Redis connection settings. Credentials and full connection strings were kept out of terminal output and this report. Database checks ran in a read-only transaction with a 5-second statement timeout and returned only aggregate counts/categories. Redis was checked with `PING` using the backend runtime credential; the result was `PONG`. No MQTT publish or broker configuration change was made.

| Read-only observation | Result |
| --- | --- |
| Device rows | 3 |
| `is_enabled` categories | empty value: 3 |
| Activation categories | `active`: 3 |
| Device type / protocol categories | type `1`, protocol `MQTT`: 3 |
| Custom topic mappings | 0 total, 0 enabled |
| Expected-data rows | 0 total, 0 with non-empty payload |
| Backend-configured Redis connectivity | PONG |
| GMQTT runtime config capability settings | `retain_available=true`, `maximum_qos=2` |
| Retained-message count / observed per-message QoS | Not determined |
| Retain/QoS metric families on the checked local `/metrics` endpoints | None found |

The three device records have an empty `is_enabled` value, so they are not counted as enabled. The runtime configuration exposes broker capability settings, but the checked metrics endpoints did not expose retained-message or per-message QoS counters. A true `retain_available` setting does not establish that retained messages currently exist.

## Empty `is_enabled` compatibility note

Read-only source inspection of the locally pinned legacy GMQTT plugin found that `OnBasicAuthWrapper` builds an exact voucher JSON from the supplied username/password and calls `GetDeviceByVoucher` (`/tmp/tp-mqtt-20261019/gmqtt/plugin/thingspanel/hooks.go:58-66`). Its database lookup is `WHERE voucher = ?` only (`db.go:162-170`); the cached-ID path subsequently loads by ID (`db.go:183-189`, `db.go:197-208`). Neither path checks `is_enabled` or `activate_flag`. Therefore an exact voucher match can authenticate even if `is_enabled` is empty. The source comment documents `enabled`/`disabled` values and a disabled default (`db.go:25`), but the authentication query does not enforce that default.

The new Owner deliberately selects only `is_enabled='enabled' AND activate_flag='active'` (`internal/brokercontrol/gorm_repository.go:48-54`). This is a compatibility tightening, not evidence that blank values were disabled by the old login path. Do not normalize blanks to enabled automatically. Before migration, produce a tenant-scoped report of blank rows and have the owner explicitly confirm each row's intended state; then explicitly set approved rows to `enabled` and the remainder to `disabled`, and verify aggregate counts afterward. No device data was changed for this inspection.

This inventory supports only local schema/runtime readiness observations. It does not show real customer onboarding, configured custom mappings, populated expected-data, retained message usage, or device-side effects. To support those claims, provide an authorized customer environment, tenant-scoped aggregate queries, and an agreed read-only broker statistics interface that exposes retained/QoS usage without returning topics, payloads, or credentials.
