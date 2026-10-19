# Phase 3 runtime / ingestion checkpoint (2026-10-20)

This is an experimental community-only candidate checkpoint, not production approval or project completion. The original working directory retains unrelated notification work; the managed `codex/gmqtt-community-20261019` worktree includes only the broker allowlist.

## Added since 18ee34b6

- SQL012 route freeze/barrier/prepare/activate owner, source runtime main-thread ACK fence and generation replacement checks; final SQL workers acknowledge under existing binding locks. Native source-side four-role fence proved; actual multi-cell/legacy gray rollout remains unverified.
- Per-publication broker ingestion IDs persist in envelope bytes and deduplicate second-hop redelivery without merging independent equal samples. Audited retired route messages quarantine without relabelling epochs; unknown/future/unproven routes still backpressure.
- Persistent session startup now registers ingress handlers before CONNECT. Real Mosquitto subscriber disconnect after SQL commit/before transport ACK was repeated five times with identical receipt ID, one attribute update and one effect intent. This is controlled disconnect/reconnect, not OS crash certification.
- Additive tenant-scoped read-only status evidence endpoint distinguishes online/offline/unknown. It never turns a stale binary status into a fresh assertion. Gateway children without their own liveness evidence remain unknown.
- Command owner now has SQL013 one-use plan/authorization primitives and atomic consume+command/log tests. These primitives are not yet exposed as a finished AI execution feature. Read-only Encore published-snapshot client has bounded HTTP/identity/pagination/size tests; live authenticated Encore integration remains unverified.
- New full-stack health fixture: 100 synthetic devices, 100 publications/sec, 60 seconds, 6,000 planned/sent/acknowledged/accepted/SQL-terminal/effect-intent records, no wrong bindings/duplicates/extra rows. This is health only; no end-to-end P99, performance benefit, physical effects or long stability claim.

## Candidate validation

`go test -race -p 1 -count=1` passed all 17 selected packages with isolated QA PostgreSQL and `TEST_MQTT_NATIVE_E2E=1`; exact results are in `phase3-runtime-candidate-race.log`. A small subsequent semantic-client fix accepts legitimate resource revision zero (rejecting absent/null); its package race was rerun separately. No skipped native test is used as evidence.

Community G0-G5 remain open: final production profile and representative golden inputs, paired legacy/new full-stack benefit, complete failure matrix, durable disk/node fault boundary, 72-hour run, real gray rollback and complete AI/value scenario are outstanding. Enterprise providers are outside scope. Images/stress tests from older source hashes retain their own scope and do not certify this source.
