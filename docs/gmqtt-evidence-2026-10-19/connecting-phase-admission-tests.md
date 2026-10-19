# Connecting-phase admission tests

Connecting-phase tests pin the intended admission boundary across the owner, ledger, state projector, and native driver:

- Owner real-PostgreSQL coverage confirms `ResolveAdmissionBinding` returns the connecting binding while `ResolveActiveBinding` rejects it; an owner unit vector confirms the resulting device Grant carries `binding_phase=connecting` and the current epoch.
- Ledger real-PostgreSQL coverage accepts an ingress receipt as pending while the route is connecting. A processing attempt does not call the business handler or fence the receipt. Once the binding becomes active, the same receipt processes successfully.
- State real-PostgreSQL coverage retains an observation and pending projection work during connecting. `Project` rejects projection until activation; activation projects the retained observation.
- Driver unit coverage denies READ delivery of command topics for a connecting device grant while allowing that device to publish and rewrite ordinary telemetry into trusted ingress. Active device grants can receive their command. A service grant can read the internal ingress topic.

Validation command:

```sh
TEST_BROKER_DATABASE_DSN="$(cat /tmp/tp-mqtt-20261019/qa-dsn)" go test -race -count=1 ./internal/brokercontrol ./internal/brokerledger ./internal/brokerstate ./internal/mosquittodriver
```

Result: all four packages passed; owner, ledger, and state ran their private-schema PostgreSQL tests against the isolated QA database. Driver vectors ran under the race detector.
