# ThingsPanel broker contract v1 (community Mosquitto)

This package is the small, versioned boundary between the community Mosquitto
shim and the existing ThingsPanel identity/Adapter owners. It does not define
enterprise VerneMQ or EMQX profiles. Go types live in
`internal/brokercontract`; `schema.json` is the matching JSON shape and
`golden-envelope.json` is a byte-for-byte serialization vector.

## Go integration surface

- `AuthRequest` and `Grant` carry the authentication request and bounded local
  policy snapshot. The shim should validate a grant before caching it.
- `Principal`, `Policy`, `AuthorizeTopic` and `FilterMatches` provide the
  finite ACL check. Publish entries are MQTT filters matched against each
  concrete publish topic. Subscribe requests must be a proven subset of an approved filter;
  `FilterContains` rejects broader wildcard requests. Shared groups require an
  explicit group-scoped filter and delivery checks its underlying topic. `ActionReceive` checks an actual topic
  against the subscribe filters, while `ActionPublish` checks only publish
  topics. Device principals are denied every `tp-internal/` and legacy `$tp` resource; an explicitly
  authorized service principal can receive internal ingress.
- `IngressTopic` encodes tenant/device identifiers using unpadded Base64url.
  The original wire topic key is `b-` plus unpadded Base64url bytes; if the
  resulting internal topic exceeds 65,535 bytes it becomes `h-` plus lowercase
  SHA-256 hex. The envelope preserves the original topic bytes as standard
  padded Base64 so
  the Adapter can recompute either key and detect tampering.
- `EncodeEnvelope`/`ParseIngress` use standard JSON encoding of `[]byte`, which
  means the compatibility `values` field is standard padded Base64 exactly
  once. The maximum input payload is 1 MiB. The broker cell and instance must
  match trusted adapter context; supplying a trusted principal additionally
  enforces the identity-owner result. Contract version, tenant/device key,
  original topic and optional expected connection/policy/epoch fields are
  checked before returning bytes to the mapper.
- Topic text and identifiers must be valid UTF-8, nonempty, bounded, and free
  of MQTT wildcard or control characters where applicable. Empty original
  topics and invalid UTF-8 are rejected.

## Retain and replay

For `live-ingress`, retained input is rejected and an empty live payload stays
an envelope with `values: ""`. For `platform-retained`, retained plus an empty
original payload produces a true zero-byte PUBLISH to clear the broker slot;
an empty non-retained payload remains a regular envelope. `wire-retained`
transformation is explicitly unsupported by this helper and must remain on a
verified original-topic binding. `retained_replay` only records provenance; it
does not authorize a state transition, action, or device-online update.

The internal `tp-internal/` namespace is always denied to device publish/subscribe.
MQTT `+` and terminal `#` are validated as filter syntax, with the system-topic
leading-wildcard rule applied by `FilterMatches`. Policy grants only a finite,
explicit list of publish topics and subscribe filters.

The community capability profile deliberately reports unknown connection
precision as `not_tested` and queue replication as `unsupported`; a release
profile must replace those values with pinned-version test evidence.

## Connection observations

`tp-internal/state/v1/<tenant-key>/<device-key>` carries a `StateObservation` bound to
broker cell/instance, a random process boot ID, a connection nonce, route epoch,
policy/mapping versions and a monotonically increasing per-connection sequence.
Retained state is rejected. A closing observation cannot close another connection,
and stale open observations are reported as unknown. The plugin emits periodic
online observations; this is independent of the existing business heartbeat mode.

## MQTT 3 queued command delivery

Community MQTT 3 does not carry MQTT 5 message-expiry properties. For supported
canonical command topics, the owner publishes a private `TP-COMMAND/1` storage
frame containing the command identity, target device, route epoch, mapping
version, original wire topic, expiry timestamp, and original payload bytes. The
frame is a broker-storage representation only. The Mosquitto `MESSAGE_OUT`
path checks frame syntax, expiry, the authorized device grant, route epoch,
mapping version, and exact topic, then strips the frame and sends the original
payload bytes and topic to the device. Retained command frames are denied.
Invalid, unknown-version, expired, mismatched, or cross-epoch frames fail
closed before delivery. The stored bytes remain framed across offline storage
and retry; only the outgoing device delivery is rewritten.

Expiry is enforced immediately before broker delivery. It cannot impose a
deadline on execution inside a legacy device after that device has received the
command; this contract makes no such claim. The currently authenticated wire
profile accepts only `devices/command/<number>/<message-id>` and
`gateway/command/<number>/<message-id>`, with the final level matching the
frame command ID. Custom-mapped command topics are not certified by this
profile and must remain blocked.

## Prefix decision after the native PoC

The fixed Mosquitto 2.1.2 tokenizer puts a `$share/<group>/$tp/...`
subscription under an empty first-level token, while a `$tp/...` publication
uses `$tp` as its first token. The observed result was successful SUBACK with no
shared delivery. The unpublished contract therefore freezes
`tp-internal/ingress/v1` and `tp-internal/state/v1`. This changes only the internal
transport; device wire topics and bytes are unchanged. Explicit ACLs reject
device publish, subscribe and delivery to this namespace even if a wildcard
rule could otherwise match it. The former `$tp` alias remains reserved and denied.
No private Mosquitto core patch is required.

`Grant.binding_phase` is an optional internal v1 extension. Omission retains the
original active-lease behavior. The community owner now emits `active` or
`connecting`. Connecting leases permit authentication, subscriptions, trusted
state observations and durable ingress admission, but canonical command READ /
MESSAGE_OUT is denied. The ledger keeps matching connecting receipts pending
without running business handlers; they become eligible after activation.
Draining does not issue or refresh a lease. A phase flag is not a completed
migration barrier or proof that old wire sessions were drained.

## Stable platform ingestion identity

The current community shim mints `_tp.ingestion_id` from its random process boot
identity and a monotonic publish sequence. It performs no entropy or network I/O
per publication. A newly published identical sample gets a different ID; the
same persisted internal envelope keeps its ID across subscriber redelivery.
The Adapter derives a scoped stable receipt UUID from this trusted ID. This
closes commit-before-second-hop-ACK retries without deduplicating legitimate
identical samples by their content. Device `source_event_id` remains a separate
cross-publication deduplication contract; MQTT Packet Identifier is never used
as that key. Missing ingestion IDs in older unpublished v1 envelopes preserve
the earlier weaker behavior and must be drained before certifying this profile.

Telemetry derives its final-batch receipt from the same ingestion ID and logical
child device. This prevents duplicate effects after an already committed batch;
it does not make the earlier in-memory telemetry bus durable.

The opt-in strict migration runtime endpoint is `/v1/runtime/sync`, bound to the
broker's existing cell/instance token and actual boot ID. Its bounded HTTP worker
stages fences; only main-thread Tick installs them, and a subsequent request may
ACK them. Control leases expire ten seconds from request initiation. See the
[runtime integration evidence](https://github.com/ThingsPanel/thingspanel-backend-community/blob/1a1ef0a005a64a8d80e627cb8546794e018e2c9d/docs/gmqtt-evidence-2026-10-19/runtime-fence-integration.md)
for implementation boundaries and uncompleted migration scenarios.
