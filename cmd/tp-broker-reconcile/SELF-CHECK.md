# Offline checker self-check

Validation command: `go test -race -count=1 ./cmd/tp-broker-reconcile`

Result: `ok project/cmd/tp-broker-reconcile 6.036s` (2026-10-20). Raw `go test -json` output: [go-test-race.json](../../docs/gmqtt-evidence-2026-10-20/reconcile-selfcheck/go-test-race.json).

The test pack contains 1,000 valid events across two tenant IDs and ten device IDs and returns PASS. Mutated, rehashed packs detect exactly 100 each of missing accepted records, excess side-effect rows, wrong-scope terminal records, and late terminal records. A 3,001-effect single-event pack detects all 3,000 excess effects. Count-compensation, earlier-than-planned timestamps, reused receipts, terminal failure, contradictory same-effect records, missing evidence, altered hash, duplicate JSON keys, unknown fields, duplicate records, symlink files, missing required fields, and configured size bounds are also checked. Manifest case aliases (alone or beside canonical keys) are rejected. These are checker self-tests, not evidence of broker or business-system behavior.

Standalone build command: `go build -o /tmp/tp-broker-reconcile-check ./cmd/tp-broker-reconcile`

Standalone binary SHA-256: `2aafaaf52572c675d7432b23a508e5396c6bafc820625baad20a814098d2d0c4`.

Source SHA-256 (`main.go`): `d58b86f1ae513f625445c344c8ba725739a6e5c966fbef04889ce84d46a0c0b4`.

Test source SHA-256 (`main_test.go`): `281bb5439a8fc9305c04286051e460ed6fec3c84c1d2b3b367fdca91fc211452`.

Raw JSON test log SHA-256: `60ee5fef017ac2a1701194a3c00c43a6865eee999b33e8a1252b5439539d3a73`.
