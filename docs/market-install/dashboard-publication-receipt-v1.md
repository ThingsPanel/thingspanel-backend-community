# Dashboard publication original-key query v1

Owner: existing MarketDashboardBundleService (乙 marketplace/A08 boundary). Horizon remains the publication metadata/receipt owner; this proxy has no duplicate business ledger and never writes Horizon tables.

## Contract

New dashboard publish requests supply `operationKey` (1–128 bytes, no surrounding whitespace/control characters). The coordinator derives the Horizon key from JSON `[dashboard-publication-v1, verified tenant ID, verified platform subject ID, client operationKey]` using SHA256. Horizon separately derives its personal publisher from the supplied market access token. A caller cannot choose either authenticated identity.

The existing POST `/api/v1/device/market/dashboard-bundles` returns original `versionId`, current `targetState`, and client `operationKey` along with the original bundle/version/contentHash/status. No explicit key retains the previous tenant/bundle/version identity for old clients; it does not acquire an automatic-retry guarantee or the new explicit-key query contract.

The new read-only GET `/api/v1/device/market/dashboard-bundles/publication-receipts?operationKey=...` takes the current personal market token only in `X-Market-Token`, plus the normal platform credential. Tokens do not enter the URL. It rechecks the current verified platform identity and the existing registered `api/v1/device/market/dashboard-bundles` Casbin resource; missing registry, enforcer, role or permission fails closed. Deployment must actually register/grant this existing publication resource. Responses set `Cache-Control: no-store`.

Response data is `{state, operationKey, targetState?, result?}`. `confirmed` means a durable historical receipt exists; it does not imply the original target is presently downloadable. `deleted` and `unknown` are preserved. Historical receipts without original version ID are observed as `unknown`, never assigned a replacement version. `not_observed` is only a current read observation, not permission to retry publication with a new key. HTTP/database/auth uncertainty remains an error.

Query calls only Horizon GET `/api/market/bundles/publication-receipts/:scopedKey`. It does not analyze/export the dashboard, reconstruct its dependencies, upload, submit, resume, or require the source dashboard to exist. This is why a lost publish response can be recovered without republishing changed source data.

Direct publication success requires a known original version and a current pending_review/published target. Historical pending_review status with a deleted/unknown/rejected/unpublished target does not produce a new successful submission. Actual upstream HTTP 409 is preserved as an explicit publication conflict; because Horizon combines key and immutable-version conflicts, the client must not assume changing a version alone resolves it.

## Validation

```sh
go test -race -count=1 -v ./internal/service -run 'Test(DashboardPublicationOperationIdentity|PublicationReceipt|MarketPublicationHTTPConflict|RequireSuccessfulHorizonPublish|MarketClient_PublishBundle)'
go test -race -count=1 -v ./internal/api -run TestPublicationReceiptQueryUsesCurrentRegisteredPublishPermission
```

These tests run real Go proxy/validation code with explicit httptest HTTP responses and the actual Casbin model. They prove transport, identity scoping, result handling and fail-closed permission rules, not a full deployed browser/Horizon/ThingsVis chain. Horizon PostgreSQL/MinIO owner tests are separately pinned to commit 850b2766e3714fd12c06f22f6f6b855583765549. Full installation, approval-version isolation, trust chain and profile gates remain separate.

Existing Analyze/Export authorization contract is unchanged by this receipt query. It must be verified separately; the query intentionally does not depend on ThingsVis source authorization.
