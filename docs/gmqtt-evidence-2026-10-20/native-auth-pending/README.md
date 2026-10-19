# Native pending authentication capacity increment

Community-only isolated macOS Mosquitto 2.1.2 test; no production traffic. This is a real broker/normal c-shared plugin test with a test-only static-grant Owner HTTP handler. It does not certify real Owner authorization, PostgreSQL, device effect, connection throughput or performance. The outer Go test uses `-race`; the normally built c-shared plugin is not a race/ASan-instrumented plugin.

## Source and execution

- Test source: `internal/app/broker_auth_pending_native_test.go`, SHA-256 `b08d07c3fff9fa6e6ba354c00a3e62827e994256a1ca612b5478ccc1742cdc93`. Only this test file was added; production source and production authentication timeout were unchanged. Root candidate base HEAD: `35ece1c0` plus that untracked frozen test source at execution.
- Luna original-tree run: `TEST_MQTT_NATIVE_AUTH_BOUND_E2E=1 go test -json -race -p 1 -count=1 ./internal/app -run '^TestBrokerAuthPendingNativeOverlap$'`: actual PASS, 1.62s test time, zero skip/fail. Original JSON is `luna-original-race.json`. First Owner request to release: 16.624ms.
- Root managed-candidate repeat: `TEST_MQTT_NATIVE_AUTH_BOUND_E2E=1 go test -json -race -p 1 -count=2 -timeout 90s ./internal/app -run '^TestBrokerAuthPendingNativeOverlap$'`: two actual PASS, zero skip/fail, source hash identical. Original JSON and command/exit status are retained. Test elapsed and measured barrier times are in the raw JSON, not latency SLO measurements.
- A separate Luna agent reviewed assertions and cleanup without modifying or rerunning the test; root reviewed the test and its boundary conditions before accepting it.

## Asserted behavior

The private loopback broker is explicitly configured with `MaxPending=128`, `MaxSessions=256`, runtime fencing off, persistence off and a mode-0600 test token. At 128 held Owner requests, arrived/active/peak all equal 128. Only then a 129th CONNECT is sent. All three archived executions received actual MQTT 3.1.1 CONNACK code 5 for the probe, with zero extra Owner calls before release; a timeout would fail. The barrier is released within 1.5 seconds from the first Owner request, below the unchanged 2-second driver timeout. All 128 initial CONNECTs then receive successful CONNACK; a new recovery CONNECT also succeeds and final Owner requests equal 129. Each run closes sockets, releases the barrier and stops its own broker before closing the handler. No DB/Redis/container setup is needed. Explicit opt-in with missing native dependencies fails rather than skipping.

This covers held HTTP authentication overlap and recovery at the configured cap. It does not cover every ready-but-uncompleted callback overlap, refresh mix, disconnect/reuse race, sustained stress, real Owner dependency slowdown or Linux sanitizer gate. The preexisting driver ready-pending unit test is separate evidence, not part of this native test. AC-M02 remains PARTIAL and no Gate is released.
