# `expected_datas` reliable-dispatch review

Date: 2026-10-20. This is a source review only; no expected-data rows or business code were changed.

## Existing owner and lifecycle

`expected_datas` is the existing API/UI intent table. Its model fields are `id`, `device_id`, `send_type`, `payload`, `created_at`, `send_time`, `status`, `message`, `expiry_time`, `label`, and `tenant_id` (`internal/model/expected_datas.gen.go`). The API accepts `telemetry`, `attribute`, or `command`; commands store a merged JSON payload with `method` and optional `params` (`internal/model/expected_datas.http.go`, `internal/service/expected_data.go:44-73`). Creation writes a `pending` row, checks online status, and calls `Send` immediately if online.

`ExpectedData.Send` reads pending rows for a device. It marks expired rows `expired`; otherwise it calls the legacy telemetry/attribute/command put-message service and marks success as `sent`, recording `send_time`. Any send error is also made terminal `expired`; an unknown send type is logged and left pending (`internal/service/expected_data.go:136-178`, `:180-260`). The row is retained until explicit API deletion; device deletion cascades through the table FK (`sql/3.sql:56-68`).

The DAL fetch is filtered by `device_id` and `status='pending'`, but not tenant (`internal/dal/expected_datas.go:88-104`). Updates and deletes filter only by row ID; there is no claim/compare-and-set, dispatch key, lease, or atomic outbox (`internal/dal/expected_datas.go:26-38`, `:106-120`). Page listing is tenant/device scoped. Online status calls `Send` from `StatusUplink`, and the legacy telemetry/event/attribute uplinks also invoke it after a delay (`internal/uplink/status.go:252-255`, `:397-408`; `telemetry.go:660`, `event.go:434`, `attribute.go:435`). In the new broker profile, legacy status/application hooks are suppressed, so this direct legacy trigger is not a reliable broker-profile dispatcher.

For command rows, `Send` calls `CommandPutMessage` and treats a nil return as `sent`. That means accepted by the legacy command enqueue path, not device-reported success. The row's status does not capture durable command receipt state or device ACK. A crash between publish/enqueue and status update can resend the same pending row, while concurrent Send calls can race on it.

## Suggested bridge to durable commands

Keep `expected_datas` as the user's original intent and add an explicit bridge into `broker_command_requests`. A worker should claim due rows in a tenant/device-scoped transaction (`FOR UPDATE SKIP LOCKED` or equivalent), validate the current enabled device, active route binding, expiry, and supported command shape, then create/lookup a durable command using a stable idempotency key derived from the expected row ID (for example, a namespaced `expected-data:<id>` key). Insert the command request, command log, and dispatch/audit linkage in the same transaction as the row's dispatch marker. A retry should resolve to that same command ID; it must not enqueue a second command.

Preserve the distinction between queued/accepted and device-reported completion. Either add a durable command ID and explicit dispatch state to the expected-data record or use a dedicated linkage table; do not claim physical/device success from enqueue return. Expiry should be enforced before command creation, and row status updates should be conditional so competing workers cannot both dispatch. Keep API reads/deletes tenant-scoped. This review recommends a narrow bridge and does not implement or claim one.
