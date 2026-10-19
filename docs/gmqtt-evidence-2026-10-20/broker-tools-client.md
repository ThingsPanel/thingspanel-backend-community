# Broker tools transport client evidence

`cmd/tp-broker-tools-client` is a bounded community CLI transport shim for the trusted Python adapter. It accepts one strict stdin JSON object `{token, operation, input}` and POSTs `input` to the fixed `/api/v1/broker-ai/tools/{operation}` route. The CLI accepts only the four fixed operations and a 256 KiB request envelope with at most 128 KiB input. It does not read the bearer from arguments or environment.

HTTPS is required by default. `--ca-file` adds a trusted CA to the system root pool. `--development-http` permits HTTP only for loopback IP literals. The client disables proxy lookup, follows no redirects, verifies TLS, bounds dialing/handshake/headers/body and the full request to 5 seconds, and returns one fixed JSON result. Upstream errors map only from the known API categories; raw response bodies and transport errors never reach stdout/stderr. Success responses retain the complete safe API object, including operation, tenant, subject and device identity for the Python adapter to compare before exposing `data` to the model.

Validation command: `go test -race -count=1 ./cmd/tp-broker-tools-client -v`. The test log is stored beside this note. The tests use only local `httptest` servers and temporary CA files; no real API, user credential, provider or model is used.
