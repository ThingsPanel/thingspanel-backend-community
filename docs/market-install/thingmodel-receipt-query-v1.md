# ThingModel owner command receipt query v1

This additive API reads the existing v2 command owner's durable receipts. It does not change the native model, market Bundle envelope, create/update/publish payload, or existing device binding contract. The implementation is `encore-api/thingmodel/receipt_service.go`, application authorization is `app/receipt.go`, and the authoritative PostgreSQL adapter is `receipt_postgres.go`.

## Request and identity

`GET /api/v2/thing-model-commands/receipts/:operation/:key?expectedPayloadHash=<optional owner hash>`

Use the existing authenticated Encore caller. The operation is exactly `create`, `update`, `publish`, or `update-resources`. The original idempotency key contains 8–128 bytes and must be URL encoded as a path value. This request has no tenant, subject, model ID, replacement key, or generation selector. Tenant and subject come exclusively from verified `authn.CurrentData()` through the same canonical identity mapping as the command APIs. Cross-tenant administrators also query only their current tenant and own original subject identity.

The caller must currently have the capability required by the original operation: `thingmodel.write` for create/update/update-resources; `thingmodel.publish` for publish. A former writer with only current read permission cannot read the command result. A different authenticated subject in the same tenant does not inherit the original subject's receipt. The server does not accept an original subject supplied by a worker. Background worker delegation/service identity must be implemented through the existing trusted identity owner before a worker can act as that subject; this endpoint alone does not provide delegation.

`expectedPayloadHash`, when supplied, is the 64-hex-character **owner command payload hash**, not the Bundle fingerprint or the coordinator's request digest. It is compared to the hash persisted by the original command. A mismatch returns the existing conflict/aborted API error, including for an expired tombstone. The query never changes the stored hash. The current create/publish APIs do not expose this hash, so a consumer may first retrieve the authorized original key's receipt and retain the reported owner hash; it must not claim an independently verified submitted-payload comparison where no expected owner hash was available.

## Response

All successful responses include `operation`, `key`, and one of these `state` values:

| State | Meaning and response fields | Permitted recovery |
| --- | --- | --- |
| `confirmed` | A committed receipt exists, its original 30-day replay window has not expired, and its durable result is readable. Includes `payloadHash`, `result`, and `retainedUntil`. `result.replayed` is true. | Use this original result to restore coordinator progress/mappings after checking the planned resource identity and any required content readback. This result is not evidence of installation ownership or permission to revoke. |
| `not_observed` | No committed receipt was present at this query's serialization point on the owner primary database, after waiting for the same-key owner transaction lock. No result/hash is invented. | This is not proof that an older request cannot arrive later. If the consumer is authorized to continue, it may use only the **same original key and original payload** against the same idempotent owner. Never substitute another key, instance, or resource identity based on absence. |
| `expired` | A reserved receipt/tombstone exists but its result is no longer available for replay. Includes the original `payloadHash` and `retainedUntil` when present; no result. | Pause for reconciliation. The original key stays reserved. Do not infer failure, create a replacement operation, or invent resource IDs. |

Database, lock-wait, cancellation, malformed persisted result, and read-transaction commit failures are API errors. They **never** return `not_observed` or `confirmed`. The caller must treat the outcome as unknown and retain the original identity.

Mutation and query share the identical advisory transaction lock over `(tenant, subject, operation, key)`. Mutations commit domain writes, receipt, audit, and outbox in their existing single owner transaction. Queries use a separate short owner transaction and authoritative read. No network call occurs within this transaction. Query does not mutate or delete receipts, invoke a business command, clear a tombstone, or promise cross-service atomicity.

## Fixtures

`receipt-query-v1-fixtures.json` contains field examples for the three observations and conflict/unknown cases. Example hashes/identities are synthetic and are not signed provider evidence. These are API fixtures, not changes to canonical Bundle schemas.

## Verification and scope

`app/receipt_test.go` verifies trusted identity, current privilege checks, validation, and error propagation. `receipt_postgres_test.go` exercises the actual owner receipt table and actual mutations: query before/after create, expected hash conflict, same-tenant foreign subject/cross-tenant absence, expiration and reserved-key replay rejection, lock cancellation, actual domain-write rollback, and concurrent owner commit/query. Tests require the existing Encore isolated test database and do not use production endpoints. Execution results must be recorded separately; adding tests does not assert they passed.

This endpoint covers create/update/publish/update-resources results only. It does not implement installation intent leases, receiver-side generation fences, prepare/activate/revoke, shared-reference protection, user-edit protection, Community configuration binding receipts, ThingsVis import receipts, release packaging, or A08 worker delegation. Those remain explicit provider dependencies for the unique A08 coordinator.
