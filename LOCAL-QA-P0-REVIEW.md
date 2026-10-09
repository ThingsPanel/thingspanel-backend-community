# Local P0 review: group, history, E2E, and rollback

This review maps local evidence to the requested acceptance cases without changing the external acceptance ledger. `passed_fixture` is limited to the described isolated tests; it is not a production sign-off.

| Case | Local status | Evidence and limit |
| --- | --- | --- |
| GRP-04 | `passed_fixture` | `internal/service/notification_native_projection_cas_test.go`, `TestNativeProjectionSuccessThenLegacyCASConflictKeepsRouteLegacy`: a real native Core projection POST returns 200 for the exact tenant/deployment/group/revision; a TLS fixture then changes only a new old-BE group before CAS. The caller sees `source route update failed`; the isolated old DB still has no Encore route (`legacy`, version 0). No pending-sync field or new API status was added. This tests the operator's visible failure and safe ownership, not a browser migration-state display. |
| LEG-05 | `passed_fixture` | `internal/api/notification_history_tenant_test.go`, `TestLegacyNotificationHistoryHTTPUsesPrincipalTenantAndHidesUnscopedDemo`: old HTTP history uses the authenticated tenant even when the request supplies another tenant, excludes other-tenant and empty-tenant demo rows, and preserves `SUCCESS`. Native Core evidence is the existing `TestNotificationListFindsBlockedRequestsAndKeepsTenantScope` plus cross-tenant `GetNotification` coverage in `store_submission_test.go`; native history schema requires tenant IDs. The old and new history queries were verified in their respective isolated layers, not one shared UI session. |
| E2E-01 | `partial` | `T07-NATIVE-E2E.md` and `TestNativeEncoreSourceBridgeEndToEnd` cover old alarm source → outbox → actual Core → fake SMTP; `LOCAL-QA-UI.md` separately covers browser config/list/detail behavior. The browser configuration and the alarm result query are separate fixtures/runs, so the full user loop is not claimed. |
| E2E-02 | `partial` | Local evidence separately covers source HTTP 202 ACK loss and stable retry, native submit ACK loss, and native Core process kill/restart recovery (`T07-NATIVE-ACK-LOSS.md`, `NATIVE-FAULT-INVARIANTS.md`). It does not combine community PG relay, native PG worker, receipt, and process failures into one two-database fault matrix. |
| REL-01 | `partial` | SQL 23/24 route history and startup guards have isolated PG coverage; native migration/startup small-fixture evidence is recorded in `T07-NATIVE-E2E.md`. No full old-release backup/restore and target-environment upgrade drill was run. |
| REL-02 | `passed_fixture` | `internal/dal/notification_source_rollback_test.go` now checks pending, retry_wait/unknown, leased/in-flight, and handed_off rows and immutable projection history through rollback. `internal/service/notification_alarm_source_test.go` verifies the old sender's count stays unchanged for historical Encore rows and only a new post-rollback alarm increments local SMTP. |

Focused commands passed with the approved isolated fixture:

```sh
NOTIFICATION_TEST_DSN='postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test' GOMAXPROCS=2 /Users/junhong/Downloads/code/notification-workspaces/tools/encore-1.58.6/encore-go/bin/go test -p 2 ./internal/api ./internal/service ./internal/dal -run '^(TestLegacyNotificationHistoryHTTPUsesPrincipalTenantAndHidesUnscopedDemo|TestAlarmProducerSendsLegacyOnlyAndSuppressesOnSourcePersistenceFailure|TestSourceRouteExplicitRollbackPreservesImmutableHistoryAndOutbox)$' -count=1 -v
NOTIFICATION_TEST_DSN='postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test' NOTIFICATION_NATIVE_E2E=1 GOMAXPROCS=2 /Users/junhong/Downloads/code/notification-workspaces/tools/encore-1.58.6/encore-go/bin/go test -p 2 ./internal/service -run '^TestNativeProjectionSuccessThenLegacyCASConflictKeepsRouteLegacy$' -count=1 -v
NOTIFICATION_TEST_DSN='postgres://notification_test:fixture-only-password@127.0.0.1:25543/notification_test' GOMAXPROCS=2 /Users/junhong/Downloads/code/notification-workspaces/tools/encore-1.58.6/encore-go/bin/go test -p 2 ./internal/api ./internal/service ./internal/dal -count=1
```

All three commands exited 0. The full API/service/DAL suite reported 1.229s, 1.895s, and 4.776s respectively. The native projection/CAS run returned Core HTTP 200, visible local error, and route `legacy` at version 0.

The Core fixture already contains the native SMTP instance/group used by the E2E helper; management helpers replay their fixed idempotency keys. The CAS experiment adds one new old-BE group/route tuple, and the proxy changes only that new PG group after the Core's successful projection. The native Core was not restarted. No real provider was called. `execution/acceptance.json` was not edited.
