# tp-broker-reconcile

This offline checker validates a fixed evidence pack for `qos1-durable-event-business-row-v1`. It does not connect to MQTT, PostgreSQL, an owner service, or a model. A `PASS` means only that the supplied files satisfy this format's seven-stage consistency checks; it does not prove broker behavior, durable delivery, physical action, or a complete AC-Q02 acceptance result.

## Input

Run `go run ./cmd/tp-broker-reconcile --dir /path/to/pack`. The directory must contain `manifest.json` and exactly-addressed files `planned.ndjson`, `sent.ndjson`, `broker_ack.ndjson`, `ingress_seen.ndjson`, `accepted.ndjson`, `terminal.ndjson`, and `sideeffects.ndjson`. The manifest pins each raw stage file's SHA-256. JSON objects reject duplicate and unknown fields. Stage files are streamed with limits: 64 KiB manifest, 128 MiB total inputs, 8 KiB per line, 700,000 records, and at most 100 diagnostic examples.

Each planned `event_id` must occur once in every stage with the same run, tenant, device, stream, and clock identity. `accepted` and `terminal` records share a globally unique receipt ID. Every stage elapsed time must be at least the planned elapsed time; a terminal `failure`, receipt reuse, late terminal, or additional business row fails validation. Each `sideeffects` record has an independent business-row effect ID. Reusing an effect ID or adding a second distinct effect ID for one event fails validation; repeating the same effect ID with contradictory record data makes the pack invalid. The caller must provide evidence timestamps from the same declared `clock_id`; elapsed time is compared from planned to terminal against `deadline_ns`. This shared-clock declaration is a checker precondition, not proof that clocks were synchronized. Stage timestamps are collector observation times: the checker does not prove causal ordering among intermediate stages, clock synchronization, HDR latency, or P99/SLO.

The business-row effect is distinct from a physical device effect. The checker does not validate MQTT retransmission attribution; this profile expects an independent observer to correlate those records. Exact duplicate stage records make the evidence invalid rather than representing a physical duplicate.

## Exit status

- `0`: `PASS` — pack is internally consistent under this profile.
- `1`: `FAIL` — evidence shows loss, scope mismatch, deadline violation, or duplicate business effect.
- `2`: `INVALID` — malformed/ambiguous input, unknown fields, hash mismatch, duplicate record, symlink, or violated input limits.
- `3`: `BLOCKED` — required evidence or planned event set is absent.

Output is one JSON object containing status and bounded counts/examples, including `time_order`, `receipt_conflicts`, and `terminal_failures` when applicable. Examples contain fixed reason codes and short event-ID fingerprints, never raw IDs. It never emits payloads or credentials and never trusts an input summary string as a verdict.
