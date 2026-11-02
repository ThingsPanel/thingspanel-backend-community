# ServiceAccess original-key creation and query v1

This contract belongs to the existing `ServiceAccess` business owner. It adds a
durable result for creation; it does not create another installer, activate a
connector, grant compensation rights, or establish an SDK execution fence.

## Compatible entry points

- `POST /api/v1/service/access` accepts the existing fields plus optional
  `operation_key`. `Idempotency-Key` is an alternative spelling at the HTTP
  boundary; if both are supplied they must match exactly.
- `GET /api/v1/service/access/operations/:key` queries the original creation for
  the authenticated tenant and original subject. The key is 1–200 ASCII bytes,
  starts with an alphanumeric character, and then permits alphanumerics,
  `.`, `_`, `:`, and `-`.
- In-process callers use the same `CreateAccess`/`CreateAccessContext` and
  `GetCreateOperation` use cases. An installation consumer must supply and
  persist its original operation key. A request without a key retains the
  legacy interactive response `{id}` and has no automatic-retry guarantee.

Tenant and subject come from the existing verified request identity. The new
query remains inside the existing authenticated service-access router group. Its
dynamic key URL is canonically authorized against `api/v1/service/access` using
the current Casbin subject and role/function mappings. An absent registered
creation resource, withdrawn permission, missing identity or unavailable
enforcer denies this query. Other legacy routes keep their existing policy.
Another tenant cannot read a receipt. Another subject in the same tenant cannot
take over its key or query its result. The body cannot choose tenant or subject.

The request hash covers protocol version 1, tenant, subject, and the complete
typed create request, excluding the operation key. Empty configuration becomes
`{}`. Other configuration bytes are preserved exactly; a caller must retain its
original payload, not regenerate a differently formatted configuration on retry.
Credentials/configuration are not copied into the receipt or query response.

## Atomicity and observations

`service_access_create_receipts` is owned by ServiceAccess, with primary key
`(tenant_id, operation_key)`. The owner first claims that key, then inserts the
access resource in the same local transaction. A concurrent request waits on the
unique constraint and receives the original result only if subject and request
hash match. Different keys can represent different access instances; keys are
not derived solely from a package/version. A failure inserting either record or
an unexpected affected-row count rolls back the entire transaction.

A keyed create returns `id`, `operation_key`, `request_hash`,
`protocol_version: service-access-create-v1`, `state: confirmed`, `created_at`,
and `replayed`. Same-key changed content returns the existing API error code `201002`
(`CodeOpDenied`) and `data.reason: SERVICE_ACCESS_OPERATION_CONFLICT`. Later
resource drift uses the same code with `SERVICE_ACCESS_RESOURCE_CHANGED`. These
are machine-readable fields in the existing error envelope, independent of the
localized message; no new error-code system is introduced. Unknown transaction
outcomes use existing `101001` with `data.reason: SERVICE_ACCESS_OUTCOME_UNKNOWN`.
A conflict does not replace the request or create another resource.

Original-key query states are:

| State | Meaning and action |
| --- | --- |
| `confirmed` | The creation and receipt committed, and the resource still matches the recorded identity, contents and timestamps. |
| `not_observed` | No committed receipt was visible at the query. An older request may remain in flight. Only resubmitting the same key and original payload is safe. |
| `requires_reconciliation`, `RESOURCE_CHANGED` | The original receipt exists but the resource has been changed. Preserve the original ID and inspect; do not report unchanged completion or blindly compensate. |
| `requires_reconciliation`, `RESOURCE_MISSING` | The original receipt exists but the resource has been deleted or is no longer in the original tenant. The key remains consumed and cannot recreate it. |

Read/database failures are errors, never `not_observed`. Any transaction commit
or rollback error is reported as unknown, without a success response or an
automatic repeat. After response loss, query the same key. The receipt survives
later resource deletion and contains no cascade that releases the key.

The resource fingerprint covers the stored fields, including create/update
timestamps normalized to PostgreSQL microsecond precision. It is evidence for
this creation result, not a cross-process generation or deletion authorization.
No-key legacy creation is not retroactively assigned a receipt.

## Migration ownership and supported profile

`sql/service_access/001_create_receipts.sql` is a new ServiceAccess migration,
embedded by `sql/service_access_embed.go`. `ApplyServiceAccessSchema` invokes the
existing component runner using module `service-access` and its own lock/version
entry. Startup verifies columns and uniqueness/check constraints even on a
ledger hit. Existing migration bytes, other owners' entries and resource data
are not rewritten or adopted. PostgreSQL is the currently implemented profile;
durable create/query explicitly reject other database dialects until their
constraint, lock, migration and failure semantics are implemented and tested.

`PgInit` applies this component after the historical base schema. It does not
run a second global migration ledger or merge owner histories.

## Tests and evidence boundary

Run against the isolated Team A PostgreSQL fixture:

```sh
go test -count=1 -v ./internal/service -run '^TestServiceAccess'
go test -count=1 -v ./internal/api ./internal/middleware -run '^TestServiceAccess'
go test -count=1 -v ./initialize -run '^TestServiceAccessSchemaPostgres$'
```

Service tests require `TEST_BROKER_DATABASE_DSN` in keyword form because the
existing helper adds a private `search_path`. Migration tests require
`TEST_COMPONENT_MIGRATION_DSN` with database `qa_component_migration_test` and
refuse another database. Log skips separately. Tests cover concurrent creation,
tenant/subject boundaries, changed requests, distinct keys, database-trigger
write failures and zero-row writes, deletion/changes, and original-key queries.
Commit/rollback response-loss cases wrap a real PostgreSQL transaction; they are
driver-level injections and do not claim a real network partition experiment.

The update regression additionally checks that `ServiceAccessConfig` is actually
persisted before plugin notification, with empty input normalized without
mutating the caller's request. These module tests do not close J-G1/J-G2: connector
preparation, activation/revocation, SDK trusted binding checks, native transport
migration, and the four database profiles remain separate acceptance items.
