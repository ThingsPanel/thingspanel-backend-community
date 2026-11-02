# Community installation grant: exact plan representation

## Contract and ownership

The existing Community `market-install` owner keeps
`market_install_authorizations`; Encore's A08 coordinator continues to own its
separate operation/step ledger. This change adds no owner, identity module or
step journal. The common representation name is `json-number-lexeme-v1`.

The existing v1 plan/request/step digest algorithms are unchanged. JSONB can
rewrite a JSON number such as `1e+20` to `100000000000000000000`, while the v1
digest preserves number lexemes. New grants therefore persist the original
`json.Marshal(plan)` text in `plan_exact`, with `plan_encoding` set to the known
representation. The old `plan` JSONB column remains a compatibility copy.

Both new fields are nullable TEXT. Both NULL means an older representation.
A partial pair, an explicit empty encoding or any unknown encoding is rejected;
it is never treated as a legacy row. The database enforces the NULL pair and
the known non-NULL encoding. The service additionally verifies JSON and the
original plan digest before returning an executable plan.

## Writes and recovery

New grant creation writes the original identity, request key/digest, plan
digest, JSONB copy, exact text and encoding in one explicit transaction. It
requires one inserted row and reads back the immutable identity and exact
text. PostgreSQL checks the compatibility JSONB value against the submitted
plan; the service continues to use exact text for the original v1 digest.
Timestamp input is truncated to PostgreSQL microsecond precision before the
immutable write/readback comparison.

A legacy grant may acquire the exact representation only when its existing
JSONB plan still validates against its **original** `PlanDigest`. The existing
subject/tenant, expiry, revocation, account/auth-version, delegated actor,
session marker and current Casbin checks remain mandatory. A current policy
denial performs no metadata write. After those checks, the repository locks
the original grant, checks the same immutable identity and expiry again, and
updates only the two NULL representation fields. It checks exactly one update
and reads back the result in the same transaction. Concurrent readers can reuse
the same already-sealed representation; they cannot create another grant.

This conversion does not change the original digest, operation binding,
request key, session marker, expiry or creation timestamp. An older grant whose
JSONB conversion already invalidated its original hash remains blocked. Its
receipt and original hashes remain available for explicit reconciliation; this
change does not guess missing number lexemes or authorize a replacement plan.

Failed SQL and suppressed or altered writes never return a confirmed grant or
plan. A commit/rollback acknowledgement error is an unknown persistence outcome.
The caller must query the original `(tenant, subject, request key)`; it must not
create a new key to guess success. A lost acknowledgement may leave a committed
grant or exact representation, which the original-key/reference read can observe
without repeating the original side effect. The provider's existing unknown
error response remains in use; no success notification is emitted by this path.

## Migration and deployment order

`ApplyMarketInstallSchema` uses the existing Community component migration
runner, schema exclusion and independent `market-install` version receipts.
It appends `market_install/003_exact_plan.sql` after the unmodified `30.sql` and
`32.sql`. The migration adds nullable columns and constraints only; it does not
rewrite existing plans, hashes or owner receipts. Startup verifies the live
column types/nullability and constraints, including when migration receipts
already exist.

Run the Community owner migration before deploying code that reads the new
columns. All old grant writers must be stopped or fenced before the owner
switch. A rollback to a binary unaware of the exact fields is not a validated
recovery path: it would again use JSONB for exponent-sensitive plans. The
coordinator needs its corresponding additive exact-plan migration and reads;
fixing only this owner does not complete the product chain.

## Verification commands and evidence boundary

Use the already-isolated Team A PostgreSQL and Redis fixtures. Do not use
application defaults or a customer database.

```sh
go test -count=1 -v ./internal/service -run '^TestMarketInstall(GrantExactPlanRoundTripPostgresRedis|GrantPlanEncoding|GrantLegacyPlanRecoveryPostgresRedis|GrantExactPlanWriteAcknowledgementPostgresRedis|AuthorizationPostgresRedis|GrantPolicyRevocationPostgresRedis)$'
go test -count=1 -v ./initialize -run '^TestMarketInstallSchemaPostgres$'
```

The service fixture uses `TEST_BROKER_DATABASE_DSN` and
`TEST_DEVICE_DELETE_REDIS_ADDR`; the schema fixture requires
`TEST_COMPONENT_MIGRATION_DSN` and verifies the database is exactly
`qa_component_migration_test`. The schema fixture intentionally owns and resets
its private public-schema installation tables. The service fixture creates a
separate schema per fixture and exercises the real historical SQL plus this
new migration.

The tests cover exponent/fraction/integer round trips with the original v1
hashes; compatible and incompatible legacy grants; twelve concurrent readers;
policy denial before metadata writes; database constraints; SQL trigger
RETURN NULL, RAISE and altered values; real transactions followed by injected
lost commit/rollback acknowledgements at the GORM connection boundary; original
key/reference recovery; prior owner upgrade; concurrent migration startup;
preserved historical checksums and receipts; and live schema drift rejection.
The acknowledgement fixture executes PostgreSQL commit/rollback before the
injection. It is not a server restart or network-proxy test.

At packet creation, only local Go parsing/formatting is verified. Root records
the actual database runs against the integrated source and evidence manifest.
This change does not claim MySQL Community grant support, multi-instance Casbin
propagation, receiver transaction/instance fencing, grant renewal, SDK activation
or any J-G1–J-G4 product-chain pass.
