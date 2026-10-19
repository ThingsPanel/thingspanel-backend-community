# Broker migration checker QA evidence

Tested 2026-10-20 against the isolated PostgreSQL database named `brokerqa`. The DSN was read from the local QA-only secret file and was not printed. Each schema used a random `mqtt_qa_*` name and was dropped after its test.

Both original and candidate worktrees passed migrations 001–011 against separate fresh private schemas, repeat application, and final read-only checks. Required-column validation covered `broker_telemetry_activity` and all six `broker_automation_execution_windows` columns. The original and candidate `009_automation_actions.sql` and `011_safe_device_voucher.sql` files matched. In the original worktree, the checker passed with `broker_safe_voucher_v1(text)` present; after dropping that function in an isolated schema, read-only validation exited 2 with a fixed missing-function message. Temporary schemas were removed.

Read-only checks against an empty private schema exited with code 2 and left its table count at zero. After removing a required column from a migrated test schema, the check exited with code 2 and named that missing column. An apply request for schema `public` exited with code 64 before executing migrations.

All temporary schemas were removed. This demonstrates checker behavior and repeatability of migrations 001–011 in both worktrees against the isolated QA database. It does not certify production schema contents, validate indexes, or replace application startup's fail-closed schema check. The checker has no migration ledger/checksum and is not a production migration executor.

The original worktree also passed fresh/repeat application through migration 011 and the required-function check. Dropping `broker_safe_voucher_v1(text)` from a private schema made the read-only checker exit 2 with a fixed missing-function message. Candidate validation through migration 011 remains pending synchronization of that migration.


## QA migration/rollback drill

`drill-migration-rollback-qa.sh` completed against the isolated `brokerqa` database: migrations 001–011 applied and reapplied in a generated `mqtt_qa_drill_*` schema, schema/function checks passed, and a local Mosquitto subscriber received its matching publisher payload on loopback port 56208. The script then terminated its own broker process and dropped only the generated schema. A second rerun initially exposed a broker-start/subscriber timing race; adding a bounded TCP-ready check before starting the subscriber fixed it, and the rerun passed. No existing broker port or device was accessed. This is migration plus anonymous local MQTT plumbing only; it does not test owner authorization, device traffic, or route cutover/rollback.

The QA PostgreSQL observation after test suites had 2 sessions out of `max_connections=100` (one Timescale background scheduler and the inspection `psql`, with no idle-in-transaction sessions). Broker ledger, command, and telemetry PG fixtures close their pools in `t.Cleanup`; the native app E2E intentionally retains its pools until its test process exits because generated query objects cannot be reset to nil. A transient `too many clients` happened while broad package tests were running, followed by the low session count after they ended; this points to overlapping per-package pool caps rather than a persistent DB-session leak. This is an inference from the timing and snapshots, not a captured peak. The ledger test pool cap was lowered from 32 to 12 and telemetry from 16 to 8.

Repository review found no route migration API/CLI: binding management is limited to owner-side lookup/admission and tests issue direct SQL updates. The QA drill intentionally does not simulate a production route update.

Later status note (2026-10-20): the observation above is preserved as the state at that review point. SQL012 now provides a route-change state machine and `tp-broker-cutover` applies reviewed freeze/prepare/activate/rebase plans; its CLI audit boundaries and remaining operational gaps are documented in [CUTOVER-CLI.md](CUTOVER-CLI.md). This migration drill itself still does not test route transitions or rollback.
