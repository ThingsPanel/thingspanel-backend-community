# Self-hosted notification database migrations

The notification service keeps its SQL files in `l3-core/notification/migrations`. Encore's self-hosted export packages the service; it does not apply those SQL files to an external PostgreSQL database. Run this deployment-only command before starting the new service version. It maintains its own ledger in the separate `notification_deploy` schema; Encore-managed migration metadata is not imported or modified.

The command reads connection settings from standard PostgreSQL environment variables and `PGPASSFILE`. It never accepts a password or DSN argument. The passfile must be a regular file with mode `0600`. Run the command from the `encore-api` directory so its checked-in migration directory is used.

Start with status mode, which is the default:

```sh
export PGHOST='db.example.internal'
export PGPORT='5432'
export PGUSER='notification_deployer'
export PGDATABASE='thingspanel_notification'
export PGPASSFILE='/run/secrets/notification-pgpass'
export NOTIFICATION_MIGRATE_EXPECTED_DB='thingspanel_notification'
export NOTIFICATION_MIGRATE_EXPECTED_SCHEMA='public'

go run ./cmd/notification-migrate
```

Review the reported database, host, schema, migration versions, and SHA-256 checksums. Apply explicitly only after the deployment backup and target identity are verified:

```sh
NOTIFICATION_MIGRATE_MODE=apply go run ./cmd/notification-migrate
```

`NOTIFICATION_MIGRATE_EXPECTED_SCHEMA` is required and must name an existing target schema. `NOTIFICATION_MIGRATE_LEDGER_SCHEMA` defaults to `notification_deploy` and must be different. The command takes a PostgreSQL advisory lock for the target database/schema. Every migration's DDL and ledger row commit in the same transaction. Re-running verifies checksums and skips recorded versions. A checksum change, missing migration version, unknown ledger row, or notification-owned objects (`notification_*` relations or `notification_reject_immutable_change`) without this tool's ledger stops the run. Unrelated objects and rows in the explicitly named schema are left untouched; the tool does not adopt or manage them. Use a deployment-dedicated database where practical, and never point the command at an existing notification schema without its matching ledger. There is no automatic down, force, ledger adoption, or history cleanup operation.

Before production use, preserve a database snapshot and record its identity, PostgreSQL version, current notification table counts/IDs, and the migration file hashes printed by status mode. Apply to a restored copy first, then verify queued, accepted, unknown, receipt inbox, source dedupe, and management idempotency records against the saved baseline. Apply the same command to the production target only after that rehearsal succeeds. Keep the backup until the new service reports ready and the same business-row checks pass. Routine rollback switches future source routing and, when compatible, the service artifact while preserving the current schema, deployment ledger, and all accepted/unknown/idempotency/receipt facts; this tool does not execute down migrations. Restoring a pre-upgrade backup can erase sends accepted after the snapshot and permit duplicates. Treat backup restore as an isolated disaster-recovery operation: stop sends, reconcile both source and Core records with all later acceptance/receipt evidence, and require an explicit recovery decision before any sender resumes. Never restore an old production snapshot as an automatic release rollback.

The `notification_test` integration tests are separate from the command's release workflow. They require the explicit `NOTIFICATION_MIGRATE_TEST_DSN`, validate the designated loopback fixture, and create/drop only uniquely named temporary schemas. They do not touch the native Core fixture database.
