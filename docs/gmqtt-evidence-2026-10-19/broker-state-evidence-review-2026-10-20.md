# Broker status evidence API and current-state review

Review scope: community `ingress_v1` current-state trust path only. This is a source/test review, not a G0 or live-device certification.

## Findings

The new `GET /api/v1/device/status/evidence/:id` path is exposed under the authenticated device router (`router/apps/device.go:74-76`). Its handler sets `Cache-Control: no-store`, uses only `claims.TenantID`, maps missing identity to 401, unsupported legacy profile to 409, tenant-scoped device absence to 404, and transient storage failures to 503 (`internal/api/device_broker_evidence.go:13-34`; `internal/service/device_broker_evidence.go:15-25`). The response is an as-of observation: `device_id`, `status` (`online|offline|unknown`), `fresh`, active `connections`, `source`, `as_of`, transport device, broker cell, route epoch and route phase. It must not authorize a command without a fresh final command guard (`internal/brokerstate/evidence.go:13-31`).

`InspectCurrent` is a read-only repeatable-read transaction with a 2-second context bound and 1.5-second statement timeout. It scopes device and route lookup by the caller's tenant, returns unknown for disabled/inactive devices and non-active/missing routes, and invokes the shared heartbeat evaluator without projection writes (`evidence.go:28-79`; `activity.go:56-67,103-128`). This is preferable to reusing the legacy endpoint: `/api/v1/device/online/status/:id` returns only the stored `devices.is_online` bit, with no freshness, route epoch, source or unknown state (`router/apps/device.go:74-76`; `internal/api/device.go:828-835`; `internal/service/device.go:1958-1985`). `Snapshot` and `ProjectCurrent` are package methods, not directly routed HTTP APIs; `ProjectCurrent` mutates projection state, whereas `InspectCurrent` is the suitable read API.

For gateway children, an active gateway socket is transport reachability, not proof that each logical child is alive. The evaluator now returns unknown for a heartbeat-disabled child instead of inheriting a gateway connection or stale direct-child history (`internal/brokerstate/activity.go:116-123`). Without an activity record, route resolution still defaults to the device itself; a child with no own route therefore does not get a broker decision from this path. A fresh child telemetry activity record can identify its transport root, after which current parent ancestry and route epoch are checked.

## Test evidence

The new isolated PostgreSQL tests cover direct-device heartbeat online/timeout offline, old route epoch, draining, disabled and inactive device, tenant concealment as not-found, gateway-child old-parent fencing, and a gateway-online/no-heartbeat child remaining unknown despite stale direct-child connection history. They also verify the read endpoint does not update `next_check_at`, legacy `is_online`, status history, state-transition receipts, effects or projection queue (`internal/brokerstate/evidence_postgres_test.go`). Service/API tests cover missing subject/tenant, unsupported legacy profile, unknown response, `Cache-Control: no-store`, and cross-tenant 404 (`internal/service/device_broker_evidence_test.go`, `internal/api/device_broker_evidence_test.go`).

With `TEST_BROKER_DATABASE_DSN` loaded from the dedicated QA DSN file and all fixtures in unique private schemas, these commands passed after the GORM query-chain fix:

```sh
go test -race -p 1 ./internal/brokerstate
go test -race -p 1 ./internal/service ./internal/api -run 'BrokerStatusEvidence|InspectCurrent'
```

The complete brokerstate PostgreSQL race suite passed, including the prior gateway-child/reparent projection case. The evidence API/service race tests also passed. The tests use unique private schemas; no real business schema was read or modified.

Unknown status remains intentionally conservative. The evidence is a point-in-time read and may become stale immediately after it returns; downstream command execution must continue its existing final route/authorization checks.
