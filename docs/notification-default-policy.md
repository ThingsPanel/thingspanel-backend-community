# Tenant default notification policy

The tenant default is a separate pointer to an already published native notification group. It does not rewrite existing alarm configuration or replace an alarm's explicit group selection.

## HTTP contract

Both endpoints are tenant-scoped and require a signed `TENANT_ADMIN` or `SYS_ADMIN` session that has a tenant claim. The tenant ID always comes from that claim; callers cannot submit or override it.

- `GET /api/v1/notification-default-policy` returns `{version, selected, availablePolicies}`.
- `PUT /api/v1/notification-default-policy` accepts exactly `{nativeGroupId, expectedVersion}`. An empty `nativeGroupId` clears the default. A stale `expectedVersion` returns HTTP 409. Missing, disabled, unpublished, or cross-tenant targets return the same HTTP 404 response.

Each policy summary contains exactly `nativeGroupId`, `aliasGroupId`, `name`, `groupRevision`, `routeVersion`, `status`, and `ready`. `ready=true` means the source route is published and a current Core snapshot confirms the exact published group revision and its currently enabled bound instances and plugins. The check uses the published revision, not the group's latest draft. `selected` is retained when this check fails, with `status=unavailable` and `ready=false`, so the administrator can replace a stale selection; `availablePolicies` excludes policies whose current snapshot cannot be confirmed. The snapshot check runs for management reads and writes, outside alarm persistence transactions.

The Casbin resource is registered by migration 25 for tenant administrators and system administrators. For this endpoint only, Casbin evaluates the authority from the signed JWT directly because the legacy user-ID to custom-role-ID mapping is not stable across tenants. The handler performs a second role check and scopes every store operation to the signed tenant claim.

## Event resolution and concurrency

An alarm with a nonempty `notification_group_id` keeps its explicit route. An empty value resolves the current tenant default when `AddAlarmInfo` or `AlarmExecute` creates the event. If no default exists, the alarm is stored without a notification outbox item. A later default change affects only future alarm events.

Migration 25 stores one versioned pointer per `(source_deployment_id, tenant_id)`. Both policy updates and alarm event creation acquire the same tenant advisory transaction lock before reading or changing that pointer. When a native alias is needed, both paths then acquire the same route lock. This lock order covers first-row creation as well as ordinary updates; a row lock alone cannot protect a missing row. The selected route and outbox intent are written in the same transaction as the alarm, so a concurrent default change cannot redirect an already-created event.

If the selected policy is stopped or its published route is no longer active, the alarm remains saved and gets a durable `blocked` outbox diagnostic (`default_policy_unavailable`). The existing tenant-scoped alarm list (`GET /api/v1/alarm/info`) and automation history (`GET /api/v1/alarm/info/history`) expose the alarm's `remark=default_policy_unavailable`; the outbox retains `state=blocked` and `failure_code=default_policy_unavailable` for operational inspection. It does not fall back to an old legacy sender. An empty alarm group remains empty in storage; clearing it through `UpdateAlarmConfig` is persisted explicitly so the next trigger can inherit the tenant default.

When no default pointer exists, the alarm is still saved but there is no notification outbox row and no blocked remark. This distinguishes a deliberate “no default configured” state from a selected policy that later became unavailable.

## Validation

The integration tests use a unique schema in the approved `notification_test` PostgreSQL fixture. They cover CAS conflicts, initial-row races, tenant isolation, default changes during event creation, explicit-group priority, clearing, stopped-policy blocking, and both alarm creation paths. They do not send email or invoke an external notification provider.
