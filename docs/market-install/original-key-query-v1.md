# A08 original-key read contract v1

This closes the specific recovery gap where `POST /device/market/bundles/install` commits an Encore operation but its first response is lost, so the client has its original idempotency key and no installation ID. The original Encore A08 ledger remains the only owner. This endpoint performs no submission, preparation, resume, authorization issuance, or business resource write.

## Transport and identity

- Community: `GET /api/v1/device/market/bundles/installation-operations?operationKey=<URL-encoded original key>`.
- Encore: `GET /api/v2/market-installation-operations?operationKey=<URL-encoded original key>`.
- The original key is 8–128 bytes. It is a query parameter so existing opaque keys containing `/`, `+`, spaces, `?`, or `#` survive routing unchanged. Community rejects multiple `operationKey` values.
- Both new entrypoints require the current live user session, installation-management roles, and the current canonical `api/v1/device/market/bundles/install` Casbin resource permission. A missing canonical resource mapping, removed role/policy, inactive/moved user, unavailable enforcer, or failed database lookup prevents the read. A separate policy on the new query URL cannot substitute for the install permission. Community forwards the original `x-token` header to the configured Encore coordinator; a token is never added to the URL or persisted here. Bearer-only/API-key callers are not silently converted to this user-session contract.
- The Encore reader calls the existing `store.Get(ctx, currentTenant, currentSubject, originalKey)`. A caller cannot select another tenant or subject. A record owned by a different subject or tenant is not exposed.
- A current authorized user can read their historical result after reauthentication. The old execution grant is not required to remain unexpired for this read. This does not renew that grant, authorize further execution, or resume a paused operation.
- Native `GetByKey` first uses the existing auth handler and `installationIdentity`, then the existing trusted provider transport calls the new internal `POST /internal/market-install/read-authorize`. Its body contains only `protocolVersion:1`, the already-authenticated `tenantId`, and `subjectId`. The resource is fixed by the service, not accepted from the caller. This route is registered within the existing deployment-token boundary before body/operation logging. It checks current users and Casbin through the same helper as the Community query and returns an exact version/identity/resource echo. The native endpoint reads no ledger row unless that echo is affirmative and consistent. No user JWT, execution grant, or operation plan is sent on this internal authorization call; the only credential is the existing provider service token. The provider client refuses redirects.
- Community requires `TP_MARKETINSTALL_OWNER=encore`. The legacy installation ledger lacks an original-subject column, so this new contract has no tenant-only fallback. The owner may be disabled for writes while its historical ledger remains readable.

The shared helper reads the existing user table and current in-process Casbin enforcer; it does not create an identity module. The native auth owner continues to own session, auth-version, and delegation checks. This narrowly defined permission assertion must not be reused as device execution or activation authorization. Both public read responses and the internal permission response use `Cache-Control: no-store`.

## Consistent current management permission

The same canonical resource check also guards the existing management entrypoints, so learning an installation ID cannot bypass the original-key query permission:

| Entry | Permission check before |
| --- | --- |
| Native Install | Coordinator Submit, ledger replay read and provider Prepare |
| Native Get by ID | Ledger GetByID |
| Native Get by original key | Ledger Get |
| Native Resume | Ledger Resume |
| Community selected-owner Install, legacy or Encore | Legacy writer, authorization-ledger read and remote plan preparation |
| Community selected-owner Get by ID, legacy or Encore | Legacy ledger or native forwarding |
| Community issue execution grant | Session marker and grant creation; repeats the initial check after remote plan preparation |

The Go helpers are named `AuthorizeCurrentMarketInstallationManagement` and `requireCurrentInstallationManagement` to make this limited management-access decision explicit. The internal wire name `read-authorize` remains unchanged because the call only reads current permission and never issues execution authority. Native Install still supplies the unchanged original grant to the coordinator/provider. Native Resume keeps the existing original intent, receipts, generation and state transitions; it does not renew grants. Grant issuance retains current account, auth-version, delegated actor, token expiry, original Redis session marker and immutable request/plan digest checks independently of Casbin permission.

Missing canonical policy remains closed on existing as well as new entrypoints. Deployments must explicitly provide the approved existing resource mapping and role policy before enabling management; code does not create production policy. Tests seed policies only in their isolated fixture. Current in-process policy checks do not establish multi-instance Casbin propagation or a distributed execution fence.

The subsequent provider-grant permission patch also makes `readCurrentMarketInstallGrant` recheck this same current-user/canonical-Casbin helper on every successful original grant read, after the existing grant/account/session/plan-integrity checks and before operation binding or business dispatch. This covers preparation, provider query/execute, and the authorization function called by the ThingsVis callback. Withdrawing the canonical resource, role policy or subject role after grant issuance therefore prevents a new entry through the current provider instance. An unavailable permission owner remains unknown/unavailable, not a confirmed denial or success. The check neither renews nor changes the original grant. It does not close a revocation race after the check, invalidate an already-dispatched business side effect, or prove that another instance has received the policy update; receiver transaction/generation fencing and multi-instance policy propagation remain separate requirements.

## Responses

The Community response uses the existing outer `{code,message,data}` envelope. Its `data` is:

```json
{
  "state": "confirmed",
  "operationKey": "original-key",
  "result": {
    "installationId": "original-operation-id",
    "bundleKey": "original-bundle",
    "version": "1.0.0",
    "status": "RUNNING",
    "isIdempotent": false
  }
}
```

`confirmed` means the read observed the original committed ledger record. It does not mean installation completed. State mapping is the existing `installationForwardResponse` mapping:

| Encore state | Community result.status |
| --- | --- |
| queued | QUEUED |
| running | RUNNING |
| paused | COMPENSATION_REQUIRED |
| completed | COMPLETED |
| rejected | FAILED |

The response preserves the real original ID, bundle, version, and error code. It does not fabricate resource mappings, device bindings, timestamps, or a successful completion. The native response has the same `state` and `operationKey`, with the native operation under `operation` instead of the Community `result` envelope. Private identity, plan, and authorization fields keep their existing non-JSON visibility.

Only an actual scoped `app.ErrNotFound` returns:

```json
{"state":"not_observed","operationKey":"original-key"}
```

A concurrent initial submission might still commit later. **`not_observed` never authorizes a new POST or a new key.** Keep the original unresolved intent and query again. Network failure, authentication denial, a missing route, database error, invalid/mismatched response, or an unknown state remain an error with no confirmed/absent result. Authentication errors deny access; other uncertain outcomes use `INSTALLATION_QUERY_UNCONFIRMED`. An unsupported owner uses `INSTALLATION_QUERY_OWNER_UNAVAILABLE`.

Existing rows with empty metadata from before migration 0002 cannot be matched reliably to the client bundle/version. They remain unconfirmed until an owner-reviewed repair; the query does not infer or write metadata.

## Implementation and tests

- `encore-api/marketinstall/app/lookup.go`: read-only application boundary; its interface exposes only `Get`.
- `encore-api/marketinstall/lookup.go`: authenticated typed Encore entrypoint, reusing `installationIdentity`.
- `encore-api/marketinstall/lookup_authorization.go`: permission check over the existing `httpProvider` transport before any ledger read. The existing provider's non-200 error gains a typed status while preserving its message, so known 401/403 denial is not mislabeled as an unknown permission result.
- `internal/service/market_install_lookup.go`: one authenticated owner GET and strict result validation.
- `internal/service/market_install_read_authorization.go`: shared current-user and canonical-resource permission check, with no execution-grant access.
- `internal/api/market_install_lookup.go`: Community adapter with `Cache-Control: no-store`.
- `internal/api/market_install_provider.go`: one purpose-limited read-authorize route inside its existing service credential boundary.
- `internal/model/market_install_lookup.go`: response contract.
- `router/apps/device.go`: one GET registration beside the existing install route; existing middleware remains in force.

Run from the backend root after applying the zero-row regression packet:

```sh
go test ./internal/service ./internal/api -run '^TestMarketInstallation(Lookup|ReadAuthorization|Management)' -count=1 -v
cd encore-api
go test ./marketinstall/app -run '^TestLookupByKey' -count=1 -v
TP_A08_STORE_TEST_DSN='<dedicated owner connection>' go test ./marketinstall/store -run '^TestPostgresLookup' -count=1 -v
encore test ./marketinstall -run '^TestMarketInstallation(ReadAuthorization|Management)' -count=1 -v
```

The actual PostgreSQL tests reuse the dedicated-schema fixture from `store/store_postgres_test.go`: the database name must begin `tp_takeover_` or `tp_a08_`, and only a generated schema is created/dropped. They discard the original Create response, reopen the real store, recover by the original opaque key, check tenant/subject isolation and unchanged operation counts, observe actual durable running/completed transitions, and prove a closed database is an error rather than absence. Without the explicit DSN these tests skip and are not database evidence.

The backend `TestMarketInstallationReadAuthorizationPostgres` uses the same explicit DSN and database-name guard in a separate `tp_a08_permission_*` schema. It tests the real permission query inside a PostgreSQL read-only transaction, then current-user changes and actual Casbin policy/role/resource removal. Other SQL/Casbin unit tests use real temporary SQLite and the project's actual matcher. Neither category establishes multi-instance policy reload/propagation. No test grant is created to make historical read recovery pass.

The consumer and handler tests use real HTTP requests against a synthetic owner to verify the wire contract, rejection, response states, URL encoding, trusted service credential, and absence of secondary mutation/retry. Native contract tests verify strict permission echo, refusal of redirects, and zero ledger reads on a denied, unavailable, or mismatched permission result. Additional tests call the actual four native Service methods and the Community selected-owner methods: denied management checks must precede ledger access, provider calls and grant/session writes. A preparation-time revocation is rechecked at actual grant issuance. A positive management assertion must not turn a separately rejected original execution grant into a created operation. These are not end-to-end JWT/session or product-chain acceptance; native unit tests use the auth owner's existing test principal. The native auth transport keeps the existing production native-session or Community-session recheck. Full acceptance must exercise all public entrypoints with current, revoked, and foreign sessions on the fixed integration artifacts.

Deployment must register/grant the existing install resource in the canonical Casbin policy; unknown policy intentionally stays closed. The same access guard now protects new and retained installation-management entrypoints. This does not restore retry/compensation/binding updates or Agent activation, or establish the multi-database matrix. The existing real PostgreSQL/Redis grant fixture explicitly seeds its isolated Casbin permission and must still pass its original logout, user/actor revocation, expiry, tamper and scope tests (`TestMarketInstallAuthorizationPostgresRedis`). Without the configured real database and Redis, that fixture's skip remains a skip.

`TestMarketInstallGrantPolicyRevocationPostgresRedis` extends that same PostgreSQL/Redis fixture. It issues real stored grants before removing the resource mapping, role policy or subject role, or making the current enforcer unavailable. It then exercises preparation, actual provider `Step` in query/execute modes, and the exact authorization function used by the ThingsVis callback. It asserts no business-owner HTTP request, no observed GORM mutation, no returned plan/receipt, and no change to the original persisted grant or session marker; restoring only the private fixture policy permits reading the same grant without renewal. The database, Redis and Casbin matcher are real. The native downstream is an HTTP probe, and the ThingsVis callback is tested at its production authorization function rather than through a ThingsVis process. This is not end-to-end session authentication, policy propagation or receiver execution-fence evidence.

```sh
# Explicit isolated TEST_BROKER_DATABASE_DSN and TEST_DEVICE_DELETE_REDIS_ADDR.
go test -count=1 -v ./internal/service -run '^TestMarketInstall(AuthorizationPostgresRedis|GrantPolicyRevocationPostgresRedis)$'
```
