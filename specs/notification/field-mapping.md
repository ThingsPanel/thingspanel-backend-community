# Notification contract field mapping plan

Status: business wire contract revision .8 frozen by the coordinator on
2026-10-09. This does not mean the notification product or its business
handlers are complete.
The canonical business contract is `../openapi/notification-v2.yaml`. Plugin
wire v1 remains snake_case in `plugin-v1.openapi.yaml`; source ingress is the
separate `source-event-v1.schema.json` schema.

## Go mapping plan

At the Encore HTTP boundary, request and response structs must use the JSON
property names and `encore` tags required by the selected Encore toolchain.
Business JSON stays camelCase. A `contract` package under the existing
`l3-core/notification` service can provide the shared semantic DTOs and explicit
conversion functions, while route-specific query/header/path DTOs stay adjacent
to the service handlers. Do not map Encore DTOs or database models directly to
one another.

| Contract schema | Go type proposal | Rules |
|---|---|---|
| `PluginRegistration`, `PluginUpdate`, `Plugin` | `PluginRegistration`, `PluginUpdate`, `PluginView` | Never serialize `authSecret`; response exposes safe manifest schemas/capabilities, origin, digest, health and version. |
| `PluginGrantUpdate`, `PluginGrant`, `PluginGrantList` | `PluginGrantUpdate`, `PluginGrantView`, `PluginGrantPage` | Only verified SYS_ADMIN can inspect or set global grants; tenant IDs are explicit opaque targets, never authentication identity. Grant version and history are independent from registration versions. |
| `InstanceCreate`, `InstanceUpdate`, `Instance` | `InstanceCreate`, `InstanceUpdate`, `InstanceView` | Tenant comes from verified identity; response includes non-secret config and `secretState` booleans only; secret updates distinguish omitted, `set`, and `clear`. |
| `Recipient`, `Content`, `Source` | `Recipient`, tagged `Content`, `SourceRef` | Content is a strict tagged union. Never accept a caller-supplied tenant in explicit submit. |
| `NotificationSubmit`, `Accepted`, `Notification` | `SubmitRequest`, `AcceptedResponse`, `NotificationView` | Hash canonical intent/source/expiry; stable IDs are generated server-side; 202 follows durable commit. |
| `GroupBinding`, `GroupCreate`, `GroupUpdate`, `Group`, `SourceGroupProjection` | `GroupBinding`, `GroupCreate`, `GroupUpdate`, `GroupView`, `SourceGroupProjectionRequest`, `SourceGroupProjection` | Revisions are immutable; ordinary user requests cannot write source mappings; a deployment-scoped source projection identity can map only its own deployment/allowed tenant to an already-published group revision. Member user IDs remain distinct from provider user/chat IDs. |
| `Delivery`, `DeliverySummary`, statuses | `DeliveryView`, `DeliveryStatus` | Dispatch and delivery status are separate typed enums; config version and masked recipient are visible, with redacted errors only. |
| `SourceEvent` schema | `SourceEventV1` | Validate source deployment/tenant binding against the relay principal; never trust body tenant alone. |
| plugin `Manifest`, `ValidateRequest`, `SendRequest`, `SendResult`, `Receipt` | `PluginManifestV1`, `PluginValidateRequestV1`, `PluginSendRequestV1`, `PluginSendResultV1`, `PluginReceiptV1` | Preserve snake_case and plugin-owned semantics; serialize using explicit wire structs. |

Go JSON decoding for security-sensitive inputs must reject duplicate object
keys, trailing JSON, excessive nesting/body size, unknown DTO fields and null
in structural fields. Optional structural fields are omitted rather than set to
null; secret clearing uses the explicit `clear` list. Dynamic JSON fields retain
null wherever their schema allows it, including template `params`, plugin
`config`, source `variables`, and `legacyAlertJson`. Standard `encoding/json`
alone does not reject duplicate keys; the implementation must use a bounded,
schema-aware decoder before mapping into DTOs. Cross-request identity, tenant
scope, idempotency and provider-side effects remain semantic checks, not schema
validation.

## TypeScript mapping plan

TypeScript types are mapped in `notification-v2.types.d.ts` from the business
OpenAPI document. The frontend task should consume generated or reviewed types through a
notification-only API client and a local error normalizer. It must not change
the shared legacy request client or infer fields from SQL/old API payloads.

Use discriminated unions for `Content`, `recipientSource`, and
`contentBinding`; string unions for all state enums; and optional properties
only where the OpenAPI schema permits omission. `null` is not a synonym for
omitted. `SecretPatch` must encode omitted/set/clear distinctly and must reject
masked values as updates. HTTP status remains separate from `body.code`; a
same-key update replay returns the original resource ID/version while its
`requestId` is generated for the current HTTP request. The receipt ACK is a
standalone `{ accepted: true }` type with no envelope.

Legacy group projection is registered through `POST
/api/v2/notification-group-projections` using a dedicated source-projection
bearer, not a user JWT. The bearer is bound to a source deployment and allowed
tenant IDs; the service checks the request deployment and tenant against that
identity and verifies the target group revision is published in that tenant.
The stable source tuple `(sourceDeploymentId, tenantId, legacyGroupId)` binds
permanently to one `notificationGroupId`; a separate immutable record is keyed
by that tuple plus `groupRevision`. Exact same-revision replays return their
original record. A different target for the tuple or an existing revision
conflicts. A missing historical revision may be backfilled, but it never lowers
the latest revision, which is the maximum recorded revision. V1 allows at most
128 records per source tuple; the complete read-only history is returned in the
group view, and T08 owns future pagination/retention work. The legacy adapter
persists the returned projection before switching its group in a transaction
that compare-and-swaps the expected old revision and rejects downgrades.
User-created or edited groups expose validated projections as read-only data.

## Coordinator amendment .7: explicit tenant plugin grants

The global plugin registration directory is managed by verified `SYS_ADMIN`
users with `notification:plugin:manage`. Its list/get operations are global
for SYS_ADMIN; ordinary tenant users see only enabled grants attached to their
verified tenant. Registering a global plugin does not implicitly grant it to
the administrator's tenant. Instance create/update continues deriving the
tenant only from the verified user principal.

`GET /api/v2/notification-plugin-grants` is SYS_ADMIN-only and returns a
page of all current grant rows, including disabled rows. Its only filters are
the optional `pluginRegistrationId`, `page`, and `pageSize`. The response is
`Envelope<Page<PluginGrantView>>`; each view has exactly
`pluginRegistrationId`, opaque `tenantId`, `enabled`, `version`, `createdAt`,
and `updatedAt`. It contains no plugin origin credentials, secret, ciphertext,
or manifest.

`PUT /api/v2/notification-plugin-grants` uses `PluginGrantUpdate` and requires
`Idempotency-Key`. The caller must explicitly provide `pluginRegistrationId`,
`tenantId` (non-empty, at most 128 characters; no UUID assumption), `enabled`,
and `expectedVersion`. Version zero creates a missing grant at version one;
an existing grant requires its exact current version and increments it by one.
The grant has its own version history independent of plugin registration
version. Setting `enabled` false is a soft revoke: it blocks future uses but
does not delete the grant, recall a provider request already in flight, or
change accepted/unknown terminal delivery outcomes.

Idempotency is scoped by the target tenant, the operation
`plugin.grant.set:<pluginRegistrationId>`, and key. A same-key/same-body replay
returns the original grant view/version and original HTTP status (201 for
creation, 200 for update), even if the grant has since changed; the response
envelope still receives a fresh request ID. Reusing that key with a different
body is 409. A replay is resolved before checking `expectedVersion` or querying
the tenant directory. A new write checks tenant existence through a private
read adapter: missing tenant or registration is 404; directory failure is
503. The tenant-directory adapter is internal and adds no public wire route.

## Coordinator amendment .8: immutable plugin identity fields

Plugin wire `Manifest.identity_fields` is optional and remains under
`api_version: "1.0"`. If supplied, it must contain 1–64 unique field names;
each name is at most 128 characters and matches
`^[A-Za-z_][A-Za-z0-9_]*$`. An empty array is invalid. If the field is absent,
the manifest is legacy and the platform must not guess which config values
identify the provider account. The business safe manifest summary maps the
optional field to camelCase `identityFields`; legacy summaries may omit it.

For a plugin that declares identity fields, instance updates compare each
field's canonical JSON value in the complete effective configuration,
including secret values. Adding, removing, or changing an identity value
returns `409 identity_change_requires_new_instance`; create a replacement
instance to change accounts, regions, or identity URLs. A secret may rotate in
place only when that field is not declared as identity. Other non-identity
configuration may change. For a legacy plugin with no declaration, existing
instances may still change `name` or `enabled`, but any config or secret change
requires a new instance. New instance creation remains supported.

The plugin protocol's `api_version` stays `1.0`; this optional field preserves
compatibility with existing manifests. Contract fixtures cover legacy
omission, a declared identity list, an empty list, duplicate names, and an
invalid field name.

The business contract is now `notification-contracts-2026-10-09.8`. The .7 →
.8 change adds the optional plugin `identity_fields` declaration, safe summary
mapping, and immutable identity update rules above. Plugin `api_version` and
source event v1 remain unchanged.

The legacy APP group model can have several active push targets per member.
The current member binding resolves one selected contact field and therefore
does not preserve that APP fan-out. Until a dedicated adapter passes T08's
fan-out conformance cases, APP groups that need multiple active targets remain
on the legacy engine; they must not be represented as fully projected.

## Frozen wire and fixture boundaries

`plugin-v1.openapi.yaml` is the canonical plugin wire contract for api_version
1.0, including the optional .8 `identity_fields` addition. Its digest is
recorded in `contract-digests.json`. Business camelCase
must never be mechanically applied to plugin snake_case. Source-event identity
is `(sourceDeploymentId, sourceEventId)` under the bound source credential;
tenantId in the body is checked against that credential's allowlist.

Fixtures under `fixtures/` are consumer examples and negative-case inputs. They
do not prove HTTP behavior, database commits, real auth, plugin delivery, or G1.
`TestConsumerWireDriftRejected` in the Go contract package decodes the business,
source-event, plugin, and receipt golden fixtures, then rejects business
camelCase changed to snake_case, plugin snake_case changed to camelCase, and
unknown fields. `consumer-fixtures.ts` uses these business/source/secret types
and `@ts-expect-error` cases; it passes strict no-emit compilation with the
frontend workspace's pinned TypeScript 5.8.3 using
`../frontend/node_modules/.bin/tsc --noEmit --strict --skipLibCheck --target ES2022 --module ESNext --moduleResolution Bundler specs/notification/consumer-fixtures.ts` from the contracts worktree root.

Coordinator froze G1 on 2026-10-09 for Encore framework/toolchain and contract
wire plumbing. The scratch probe evidence is in `../../../evidence/t01-http-probe.json`,
`../../../evidence/t01-source-probe-final.log`,
`../../../evidence/t01-source-probe-sensitive.log`,
`../../../evidence/t01-existing-app-check-final.log`, and
`../../../evidence/T00-REPORT.md`. It demonstrates framework HTTP 201/202/401/409,
exact conflict reason, raw receipt ACK, routing, SQL rollback/commit, init and
shutdown. It does not prove product handlers, durable business stores,
deployment/tenant authorization, projection CAS, provider delivery, or
production privacy; those remain T03/T07/T08 acceptance work. The typed
sensitive-body fake sentinel had zero trace-event matches, while fake
Authorization and ordinary raw-body sentinels each appeared twice in local
traces. T03 must enforce a self-hosted no-trace guard and sentinel check before
real credentials are used.
