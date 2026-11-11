# A08 coordinator and business provider v1

The unique installation coordinator is `encore-api/marketinstall`. It owns only durable operations, steps, leases and progress. `thingmodel`, Community configuration, and ThingsVis retain business resources, authorization, receipts and their own migration ledgers. A08 never writes their tables. This first profile explicitly requires PostgreSQL for the coordinator; it does not establish the MySQL matrix.

## Activation and public transport

The service starts its real bounded worker only with `TP_MARKETINSTALL_ENABLED=true`, a valid `TP_MARKETINSTALL_PROVIDER_BASE_URL`, and `TP_MARKETINSTALL_PROVIDER_TOKEN`. Invalid explicit configuration fails initialization. Disabled mode rejects installation and resume. The release must select `marketinstall` and make the legacy A08 writer and this owner mutually exclusive. Leaving both writers enabled is forbidden. Existing unrelated native-template installation is a business owner dependency, not a second A08 coordinator.

Authenticated admin callers use:

- `POST /api/v2/market-installations`, header `Idempotency-Key`, body `{authorizationRef,request:{bundleKey,version,overwritePolicy,deviceBindings:[{bindingKey,localDeviceId}]}}`.
- `GET /api/v2/market-installations/:id` reads only the current tenant and original subject's operation.
- `POST /api/v2/market-installations/:id/resume` requeues only a paused operation. It does not replace the identity, payload, authorization reference or keys. The worker queries the original key before taking another action.

Tenant and subject come exclusively from verified Encore `authn.CurrentData()`. The request cannot select either identity or supply a step plan/provider URL. Current tenant/platform administration permission is required at the transport. Provider authorization is independently rechecked on every step. `authorizationRef` is an opaque, revocable reference created by the existing identity/provider owner and scoped to tenant, subject, intended request and purpose; it is not a user bearer token. The coordinator stores no bearer token. Authorization-ref issuance and subject/session-version revocation must use the established identity owner; an arbitrary nonempty reference must never authorize work.

## Provider HTTP contract

All calls use `POST` and `Content-Type: application/json` at a single configured provider base, authenticated with `X-TP-Service-Token`. Redirects are refused. Non-200, malformed, oversized, transport or timeout responses are errors, never evidence of nonexecution. No response/error body or reference is logged. Provider paths are:

1. `/internal/market-install/prepare`
2. `/internal/market-install/query`
3. `/internal/market-install/execute`

Each request includes:

```json
{"protocolVersion":1,"tenantId":"verified-tenant","subjectId":"verified-subject","authorizationRef":"opaque-revocable-reference"}
```

The provider accepts these identities only from the authenticated coordinator service and verifies that the reference matches identity, request, purpose, current permissions and revocation state. It must not authorize anonymous caller-supplied identity headers.

Prepare adds `request` in the public shape above, with sorted unique bindings and `overwritePolicy:"default"`. The provider fetches and validates the immutable artifact, authorizes each resource/action, and derives a plan. Prepare must not create business resources. It returns:

```json
{"artifactDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","steps":[{"id":"sensor:create","owner":"thingmodel","action":"create","resourceKey":"sensor","payload":{"model":{},"resources":{}}}]}
```

The artifact hash is a verified immutable-content fingerprint, not a trusted signature by itself. No credentials may be included in a plan/payload. The provider handles existing Bundle trust, secret rejection, full native dependencies and readback requirements. The coordinator does not reinterpret unsigned client plans as trusted.

Allowed owner/actions are `thingmodel:create`, `thingmodel:publish`, `thingmodel:readback`, `community-config:bind`, and `thingsvis:import`. Plan order encodes dependencies; prior results are passed in order for typed provider resolution. IDs must be unique, 1–200 bytes; resource keys 1–128 bytes; at most 256 steps. Payload must be valid non-null JSON and no more than 2 MiB per step. There are no prepare/activate/revoke or arbitrary command actions in this first implementation. A model creation receipt is not installation ownership or authority to delete it.

Query and execute add:

```json
{"operationId":"coordinator-generated-uuid","generation":1,"stepId":"sensor:create","idempotencyKey":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","payloadHash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","step":{"id":"sensor:create","owner":"thingmodel","action":"create","resourceKey":"sensor","payload":{"model":{},"resources":{}}},"priorResults":[]}
```

The `payloadHash` is the coordinator v1 canonical step hash. It is distinct from the native command owner's semantic payload hash or artifact digest. `idempotencyKey` derives from coordinator operation ID, step ID, owner, action and v1 protocol; it never changes with a lease/generation. Providers must bind the actual effective request to this original key and hash and reject same-key changed content. They may resolve typed prior results, but cannot substitute another resource on retry. Generation fences local coordinator progress only; receiver-side epoch fences are not implied. This is why activation, device commands and revocation are excluded.

The response is a direct JSON object:

```json
{"state":"confirmed","receipt":{"owner":"thingmodel","resourceId":"original-resource-id","payloadHash":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","data":{"providerReceipt":"original-durable-owner-evidence"}}}
```

Supported states:

| State | Coordinator behavior |
| --- | --- |
| `confirmed` | Verify owner, exact step payload hash, nonempty resource ID and JSON receipt data; commit durable receipt and next operation state under tenant/subject/generation/unexpired lease CAS. |
| `not_observed` | Query only: no committed result at the provider's authoritative serialization point. Persist dispatch intent, then use the same key and original payload with the same idempotent provider. This does not prove an older request can never arrive later. Execute returning this state is unknown. |
| `expired` | Pause, retain original identity and receipts. No replacement key or inferred failure. |
| `rejected` | Stop operation as rejected; `errorCode` may contain uppercase ASCII letters, digits and underscore, at most 80 characters. No destructive compensation follows. |
| `unknown` or invalid/unavailable response | Pause with an explicit reconciliation reason. No automatic resend; an authorized resume first queries the original key. |

All owner confirmation and local step/operation transitions occur in short local transactions. No provider HTTP call is inside one. A local commit failure leaves outcome unknown; the worker does not advance. Expired leases can be reclaimed with incremented generation; the next worker queries the same step key. A stale worker cannot confirm/hold/advance using an old generation or expired lease. Automatic lease reclaim is not proof an old remote writer stopped; safety also requires the provider's exact-key idempotency/receipt semantics.

## Existing native configuration mapping

`market_native_model_imports` remains the Community shared immutable dependency mapping, keyed by tenant, market template and version. Its dependency identity and original-installer checks are reused. It cannot replace per-installation operation/step identities or claim that a shared model/config is owned by an installation. Configuration binding must retain native binding revision CAS and full fixed snapshot readback. The provider needs a durable per-step receipt/query bridge and current authorization; a missing mapping does not prove no external model was created.

## ThingsVis adapter

C07 supplies `POST /api/internal/market-dashboards/import/v1` and `GET` on the same path with `operationKey` and `payloadHash` queries. The Community provider forwards only after authorization, using ThingsVis internal-token, tenant and user headers. Instance key is the coordinator UUID; resource key remains the plan resource key. ThingsVis returns durable original target IDs and independently checks actual effective payload content. The outer provider maps `resourceId` to dashboard ID and preserves the full original ThingsVis receipt under `data`.

## Verification status

Tests and code are implementation candidates until run against the isolated provider and PostgreSQL components. Synthetic HTTP or in-memory tests alone do not close J-G1/J-G2. Final integration must prove the legacy writer is disabled, the service is in release artifacts, authorization references recheck revocation, each configured provider exposes real receipts, original-key restart recovery works, stale generations cannot advance, and supported storage profiles are declared honestly.
