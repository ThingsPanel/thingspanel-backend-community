# Community runtime fence integration (2026-10-20)

This is an incremental engineering result, not G4 certification or a production cutover.

## Actual paths

- The existing route owner now has SQL012, a scoped CAS state machine, immutable stage events, exact member generations and barrier acknowledgements. `Freeze` uses the same tenant and binding locks as final business/command writes, retires pending receipts/effects and unsent commands as fenced, and records potentially transmitted commands as unknown. It never deletes or replays commands.
- `PrepareTarget` requires the complete current source roster and target registrations, increments epoch, and keeps commands paused in connecting. `Activate` requires both a fresh actual connection observation and trusted nonempty pending ingress accepted in this connecting epoch. Failure while connecting can be frozen again; the previous change becomes superseded and rollback increments epoch again.
- Opt-in `/v1/runtime/sync` authenticates the same cell+instance broker token and a random real process boot ID. HTTP workers only stage bounded control data. The native main-thread Tick installs denied epochs before a subsequent HTTP request reports the ACK. A five-minute terminal tombstone prevents completion from briefly reviving an unrefreshed old grant.
- Strict control leases last ten seconds starting at HTTP request initiation. A stalled main loop cannot renew an old response at installation time. Outage/overflow/malformed response fails closed, including ordinary unframed outgoing delivery. The added control RPC is opt-in, not a change to external device topics or bytes.
- The ingress_v1 Adapter/effects/dispatcher register stable replica instance IDs and fresh worker boots before processing. They attest the actual authoritative SQL fence and retirement boundary, not a manually checked box. Every final write still verifies the binding under lock; membership ACK does not replace that check. A replacement generation invalidates the frozen manifest, and explicit Rebase clears all old ACKs.

## Tests actually run

`TEST_MQTT_NATIVE_E2E=1 go test -race -p 1 ./internal/app -run '^TestBrokerIngressNativeMosquittoOwnerPostgresE2E$' -count=1` passed in 12.064s on the original implementation tree, using the newly built native 2.1.2 plugin, private PostgreSQL schema, temporary Redis and random loopback MQTT port.

The test has four REAL registered roles (broker, Adapter, effects, dispatcher), performs Freeze and waits for their four automatic ACKs. It then publishes through the real device connection, observes an additional Mosquitto `Denied PUBLISH` log and verifies no new business receipt. The test does not seed a member or ACK. It also retains EVT/ATTR/TEL to durable-command/Paho-device-ACK coverage earlier in the same run.

Separate race unit tests cover staged-vs-installed data, ACK ordering, denied old and valid higher epochs, bounded fences/ACKs, ordinary outgoing gating, malformed/redirect/duplicate/unknown HTTP replies and expiry including delayed installation. Isolated PostgreSQL tests cover CAS, generation/rebase, transaction rollback, opposite-direction concurrency and failed-connecting rollback.

## Configuration and limitations

Strict broker: `TP_BROKER_ROUTE_FENCING=1`. Strict backend: `mqtt.input_profile=ingress_v1`, `mqtt.route_fencing=true`, and a stable unique `mqtt.runtime_instance` per replica. SQL012 is mandatory when enabling the backend flag. Default remains explicit opt-in until the release profile is certified.

At most 64 members and 256 live/recent fence records are supported by this initial bounded profile; excess pauses the operation. Old GMQTT/legacy workers are not covered by SQL-final-fence attestation and must not be falsely registered through that method. Real legacy retirement/drain, a complete two-cell transition, membership scale-down retirement, 50-binding stage faults, actual rollback and independent release certification remain open. The pre-runtime Linux sanitizer/stress image proves its frozen earlier source only.
