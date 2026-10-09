# Native notification group publishing

This bridge lets a tenant administrator expose one enabled Core notification
group in the existing community alarm group's selector. It does not change the
Core API contract and does not edit or replace an existing EMAIL group.

The community API takes the tenant only from the verified user JWT claims. Both
operations require a tenant administrator (or a system administrator acting
within the tenant in the verified claims). The request body and query string
cannot select a tenant.

First read the route state:

```http
GET /api/v1/notification_group/native-publish?nativeGroupId=<core-group-id>
```

An unpublished route has `published=false`, `status=unpublished`,
`routeVersion=0`, and `effectiveGroupRevision=0`. A stopped route keeps its
deterministic alias and historical `groupRevision`, while
`effectiveGroupRevision=0`; stopping affects future alarm events only.

Publish a selected revision with the route version returned by GET:

```http
POST /api/v1/notification_group/native-publish
Idempotency-Key: <stable-key>
Content-Type: application/json

{"operation":"publish","nativeGroupId":"<core-group-id>","groupRevision":1,"name":"Alarm notifications","expectedRouteVersion":0}
```

Before opening the alias, the service reads the exact tenant-scoped Core group
snapshot. The group and every referenced instance and plugin registration must
be enabled and present under the tenant's active grant. It then registers the
source projection and atomically updates the community alias and source route.
The alias ID is deterministic from deployment, tenant, and Core group. A
projection or database failure leaves the alias closed. First publish returns
201; an already-applied identical published state returns 200.

Stop future alarm routing explicitly:

```http
POST /api/v1/notification_group/native-publish
Idempotency-Key: <stable-key>
Content-Type: application/json

{"operation":"unpublish","nativeGroupId":"<core-group-id>","expectedRouteVersion":1}
```

The route switches to legacy mode and the ENCORE alias closes in one database
transaction. Existing queued events retain their original group revision and
are not rewritten or requeued. Stopping does not edit the old EMAIL group.

`expectedRouteVersion` is a compare-and-swap guard. On 409, read GET again and
ask the operator to review the current state. If a response is lost, query GET
and retry only the exact original body with the same idempotency key. The
bridge guarantees deterministic-alias and route/revision replay behavior; it
does not provide a general same-key/different-body idempotency ledger.

The API returns status, IDs, enabled/published state, name, and route/revision
numbers only. It does not return plugin credentials or instance secrets. A
successful publish confirms route setup, not delivery by a real provider.
