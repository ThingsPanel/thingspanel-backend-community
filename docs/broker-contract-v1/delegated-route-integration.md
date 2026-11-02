# Shared plugin connections: incremental route fencing

The shared service connection remains an actor, not a device. Its existing
immutable `PluginContext` is the sole target authorization source. Device IDs
inside SDK wrappers only select within this owner snapshot.

## Contract

`PluginTarget` gains optional `transport_device_id` and `transport_route_epoch`.
Omitted together means the historical self transport. Partial values, zero
transport epochs and inconsistent epochs for a self transport are invalid.
Both fields participate in the existing context hash, without changing hashes
for historical snapshots that omit the fields.

Normalization creates an internal `Delegation` proof containing context ID,
service actor, connection, target epoch, transport epoch, mapping and observed
broker timestamp. The wire envelope cannot set this internal field. The proof
is persisted in receipts and effect payloads. Every durable effect revalidates
the immutable owner context and both current bindings in its own transaction.

Lock order is tenant admission advisory lock, lexically sorted binding IDs,
then active tenant-local ancestry. Connecting routes may accept receipts but
workers defer execution. Superseded, disabled, cross-tenant, cyclic, missing or
reparented scopes fail closed. No fence helper creates or claims a route.
Read-only evidence uses a separate non-locking check and cannot authorize writes.

Migration `017_delegated_routes.sql` adds nullable proof columns to connection
observations and telemetry activity. Apply after existing broker migrations and
before upgrading ingress_v1. Startup validates both columns. Native rows keep
NULL; historical native fixtures without the columns remain readable through
`to_jsonb` inspection. Plugin status writes require the migration.

## Delivery status

The initial strict SDK wrapper commit is f6929665. Receipt admission/processing, telemetry batch writes, plugin state projection
and delayed automation now carry and revalidate the proof. These changes remain
unvalidated until the queued package and isolated PostgreSQL checks complete.
Owner root derivation and durable control delivery are implemented in a later
checkpoint and must pass fixed-commit validation before advertising support for
non-self transport targets.
Existing mapping certification, canonical topic scope, caller authorization,
telemetry commit-unknown handling and transactional winner selection are retained.
A separate legacy TelemetryFrame publish path is deliberately not introduced.

## Required acceptance

Use isolated PostgreSQL schemas to test independent target/root epoch changes,
ancestor disable/reparent, forged context/actor/selector, connecting admission,
rollback, concurrent migration versus commit and delayed state projection.
Then exercise real plugin MQTT telemetry, online/offline and durable control
including matching response family and root migration between enqueue/delivery.

## Owner and durable controls

The owner derives every transport root from current active tenant-local parent
rows. A non-self root must also be an explicitly allowlisted plugin target in
the same immutable context; native roots are not implicitly granted. The owner
freezes the target and root epochs separately. Mapped plugin targets continue
to fail closed under the existing certification restriction.

PreparedCommand now freezes `transport_route` within its existing durable JSON.
Preparation captures the route; Submit, Claim, Dispatch and ACK revalidate it.
The existing command frame carries the physical route alongside its target
route. Broker delivery compares both to the current owner lease and runtime
fences; no legacy TelemetryFrame publish path is used.

Protocol plugins (service_type=1) receive an owner-derived
`legacy_telemetry_id` flag. Only their telemetry control family may translate
from the canonical device-number topic to
`<registered-prefix>devices/telemetry/control/<device-id>`. The private frame
carries the canonical number and explicit alias flag. Both wire and canonical
publish scope must be allowed. Other plugin types retain device-number topics.
The historical `<registered-prefix>#` subscription request is accepted only
for this owner-marked legacy case; actual READ and MESSAGE_OUT still require a
finite owner target topic and a valid private frame with both epochs.

Existing durable plugin-child commands lacking the new frozen root proof fail
closed at delivery/guard; they are never silently rebound to the current root.

Direct per-device plugin credentials do not implicitly gain access to a physical
parent; a child with a separate parent uses the shared service grant with both
explicit targets. Existing direct root-plugin credentials retain self routing.
The nullable migration columns can remain on rollback to the previous binary.
No credentials, runtime settings, deployment or production migration are part
of these source changes.
