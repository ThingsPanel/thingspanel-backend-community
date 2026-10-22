# Notification merge and App verification — 2026-10-22

## Local merge

Notification source: `codex/notification-preview-20261010`.
Target: local `main-1.3.0`; no remote push or production release performed.

| Repository | Merge commit |
| --- | --- |
| Community backend | `7c5fffae` |
| Community frontend | `c7f235e2` |
| Encore kernel | `e140ff7` |

Each repository has `backup/main-1.3.0-before-notification-20261022`.
Merge commits preserve source ancestry. The Encore merge deliberately keeps
the target branch's device metadata, onboarding, legacy device controls,
facade, authentication, unrelated CI/scripts and device contracts. Only
notification core, adapters, contracts, deployment assets and notification CLI
changes are incorporated. Do not infer that every feature in the source
branch's ancestry was enabled by this scoped merge.

Integration fixes:

- Retain both native thing-model and notification development proxies.
- Freeze source event timestamps at PostgreSQL microsecond precision before
  computing the immutable body; otherwise persisted events fail relay checks.
- Freeze plugin grant timestamps at the same precision so concurrent and
  later idempotent responses are identical.
- Update account-editing tests to use the platform-admin selected-tenant
  routes, retaining tenant isolation and the existing permission-denial tests.
- Align notification UI types with the existing request wrapper and validate
  the tenant field before using it.

## Verification

- Community API, application, DAL, service, middleware and notification-source
  command tests passed with an isolated PostgreSQL fixture and local Redis.
- Community full package compilation passed (`go test ./... -run '^$'`).
- Encore notification core/contract, migration command and provision command
  checks passed using `-tags notification_cli`; core/contract race tests passed.
  This tag only supplies the database declaration stub for standalone tests;
  the real Encore application was separately compiled and started below.
- Notification adapters/SDK and CLI tests passed.
- Frontend notification suite: 12 files, 82 tests passed. The final TSX test
  typing adjustment was also retested (7 group-page tests passed).
- The unrelated ThingsVis auth test fails on both the baseline and merged
  tree. Full frontend type checking also fails on the baseline and merged
  tree with extensive existing diagnostics; this is not a clean full build
  or a production release sign-off.
- Linux Encore image built from `e140ff7`; its real `/__encore/healthz` reports
  all services initialized. Windows `encore check` cannot initialize this
  notification service because its privacy guard requires Linux self-hosting.

## App result

The existing App is UniPush-based: `app/services/notification-permission.js`
gets a CID and binds it through `/api/v1/message_push`; the Community sender
posts to `app/uniCloud-aliyun/cloudfunctions/push-message/index.js`'s deployed
endpoint. This endpoint URL and a real device are still needed for a real send.

All sends in this verification used local fake providers. The backend tests
cover success (`errCode: 0` and legacy `code: 200`), business rejection,
malformed responses, Android/iOS target selection, logged-out device exclusion,
tenant selection, alarm context, and group-vs-device result consistency.
The cloud function was executed with a mocked `uniCloud.getPushManager`:
accepted result, thrown provider error, missing CID, and nonzero `errCode`.

Confirmed legacy limitations, intentionally characterized rather than silently
changed during the merge:

1. HTTP 500 with a success-shaped body is logged as success by the old sender.
2. A rejected device push can still produce `SUCCESS` in group history.
3. The cloud function wraps any resolved provider response in `code: 200`,
   even when the mocked provider response has a nonzero `errCode`.

Conclusion: continuing the existing legacy App path does not require a new
plugin. Integrating App into native notification groups, per-device delivery
state, retries and receipts requires a UniPush/App adapter conforming to plugin
HTTP v1 plus explicit CID resolution. Reuse the existing App registration
flow, but do not treat the void legacy send-and-log method or group SUCCESS
as proof of provider acceptance or phone delivery. A separate WeChat Mini
Program adapter is still needed. No real phone delivery is claimed.

## Local runtime

Frontend `http://localhost:5002`, Community `http://localhost:9999`, Encore
`http://localhost:4000`. Encore now runs as `thingspanel-encore-dev-130` sharing
the Community development container's network namespace. Local private runtime
files remain outside Git under the OS temporary `thingspanel-local-20261022`
directory. The frontend's ignored `.env.development.local` enables the native
notification proxy. Fixture mode permits only the local HTTP identity path;
this configuration is not a production deployment profile.

The Community source relay remains disabled and no tenant native notification
route or real provider account was enabled. Source relay behavior was tested
in isolated integration fixtures. A live native alarm send still requires
explicit source/projection credentials, trusted TLS, provider configuration
and a published tenant group. Old App groups remain on the legacy path.
