# Native device-runtime service identity audit

Audited against develop `1f15015bfbae25efd48db983680f3a0a291493ba` (the owning
source files below are unchanged in this runtime branch). This is source evidence,
not deployment or runtime acceptance. No services or migrations were executed.

## Result

No formal scoped service identity for **Encore devicemetadata → Community general
runtime APIs** is present. `TP_COMMUNITY_SERVICE_*` is not referenced by this
backend tree or the clean develop checkout. Do not invent these environment
variables, provision a normal tenant user, or repurpose an unrelated service token.
The initial launcher therefore rejected provisioning. Under the subsequent explicit authorization for a fresh loopback-only test stack, the launcher now uses the existing broad API-key/TENANT_ADMIN contract for one new test tenant. It does not claim to implement the missing scoped production identity. Database ownership proof and private randomized credentials gate issuance/revocation.

## Owning contracts

| Owner | Actual behavior | Why it does not solve this boundary |
|---|---|---|
| `encore-api/internal/communityclient/client.go:24-51,63-78,112-121` | Only reads BASE_URL, API_KEY, EMAIL, PASSWORD; sends API key or logs into ordinary `/api/v1/login` | No service principal, operation/device scopes, delegated tenant assertion or service-token verifier |
| `internal/model/open_api_keys.gen.go:14-23` | Key has tenant, creator, status and name | No service kind, permitted operations/device list, audience or expiry scope |
| `internal/dal/open_api_keys.go:223-259` | Reads key from primary DB, checks active status and nonempty tenant/creator | Revocation is checked, but no device/operation scope is enforced |
| `internal/middleware/jwt_auth.go:127-147` | API key becomes claims with key tenant/creator and Authority=TENANT_ADMIN | An API key is not a reduced-privilege service principal |
| `internal/middleware/casbin_middle.go:13-39` | Checks Casbin only if URL has a g2 mapping; unregistered paths pass | No default-deny machine policy; cannot claim minimal permission from empty custom roles |
| `internal/api/automation_owner.go:13-27,70-113`; `internal/automationbridge/bridge.go:99-120` | Dedicated automation bearer/deployment plus tenant header; only internal automation device listing/command routes | Real purpose-specific service contract, but not a credential for Community general `/api/v1` APIs; devicemetadata client does not call these routes |
| `internal/api/notification_identity.go:15-76` | Rechecks original user JWT/session, explicitly refuses API-key alternative | Session assertion for notification, not general machine identity |
| `encore-api/l1-access/authn/auth.go:75-132,247-285` | Inbound Encore auth supports native/legacy-jwt and development token modes | Inbound scopes/dev tokens do not authenticate outbound Community requests; development modes must not bypass this gap |
| `deploy/community-test/ENCORE.md:56-62` | Self-hosted device runtime recommends API key or dedicated user/password | Deployment documentation establishes supported connectivity, not a scoped service security contract |
| `deploy/community-test/ENCORE.md:19,33` | Automation token and notification source/projection renewal are documented purpose-specific identities | Neither authorizes generic device-runtime APIs |

## Exact missing pieces

1. An owner-validated, revocable machine principal with an explicit audience and
   finite tenant/device/operation scope, distinct from human TENANT_ADMIN claims.
2. Default-deny authorization of this principal on every requested runtime route,
   including dynamic device identifiers, query/body targets and mutations.
3. A `communityclient` transport that supplies this identity to the owning service
   and carries the intended tenant/device scope without accepting forged callers.
4. A provisioning/revocation contract and positive/negative tests covering wrong
   audience, tenant/device, operation, disabled/revoked credentials and expiry.

Implementing those pieces changes the cross-service authentication contract and
must be a separately reviewed backend/Encore change, not hidden in this local
launcher. The core UI/login and native metadata database services may be started,
but live device runtime reads/commands/outbox acceptance remains blocked.
