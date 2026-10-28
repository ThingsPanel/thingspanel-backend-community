package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testToken() string {
	return "tpai_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
}

func testRequest(token, operation string, input string) string {
	return fmt.Sprintf(`{"token":%q,"operation":%q,"input":%s}`, token, operation, input)
}

func invoke(ctx context.Context, args []string, body string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(ctx, args, strings.NewReader(body), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decodeEnvelope(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	if strings.Count(raw, "\n") != 1 || !strings.HasSuffix(raw, "\n") {
		t.Fatalf("stdout must contain exactly one JSON line, got %q", raw)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	return envelope
}

func tlsCAFile(t *testing.T, server *httptest.Server) string {
	t.Helper()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	path := filepath.Join(t.TempDir(), "trusted-ca.pem")
	if err := os.WriteFile(path, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunHTTPSFixedRouteAndTrustedCA(t *testing.T) {
	token := testToken()
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/broker-ai/tools/commands.getResult" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "bad request details must not escape", http.StatusBadRequest)
			return
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input["device_id"] != "device-a" {
			http.Error(w, "bad tool input", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"operation":"commands.getResult","tenant_id":"tenant-a","subject_id":"user-a","device_id":"device-a","data":{"status":"done"}}`)
	}))
	defer server.Close()
	caFile := tlsCAFile(t, server)
	code, stdout, stderr := invoke(context.Background(), []string{"--origin", server.URL, "--ca-file", caFile}, testRequest(token, "commands.getResult", `{"device_id":"device-a","message_id":"00000000-0000-4000-8000-000000000001"}`))
	if code != 0 || stderr != "" || calls.Load() != 1 {
		t.Fatalf("run code=%d calls=%d stderr=%q stdout=%q", code, calls.Load(), stderr, stdout)
	}
	envelope := decodeEnvelope(t, stdout)
	if string(envelope["ok"]) != "true" || string(envelope["data"]) != `{"operation":"commands.getResult","tenant_id":"tenant-a","subject_id":"user-a","device_id":"device-a","data":{"status":"done"}}` || strings.Contains(stdout, token) {
		t.Fatalf("unexpected safe response: %s", stdout)
	}
}

func TestRunRejectsMalformedAndOverbroadRequestsBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	ca := tlsCAFile(t, server)
	token := testToken()
	validArgs := []string{"--origin", server.URL, "--ca-file", ca}
	tests := []struct {
		name, body string
	}{
		{"duplicate top field", fmt.Sprintf(`{"token":%q,"token":%q,"operation":"devices.describe","input":{"device_id":"d"}}`, token, token)},
		{"duplicate nested field", testRequest(token, "devices.describe", `{"device_id":"a","nested":{"x":1,"x":2}}`)},
		{"unknown top field", fmt.Sprintf(`{"token":%q,"operation":"devices.describe","input":{"device_id":"d"},"url":"https://attacker.invalid"}`, token)},
		{"unknown operation", testRequest(token, "commands.submit", `{"device_id":"d"}`)},
		{"invalid token", testRequest("parent-jwt", "devices.describe", `{"device_id":"d"}`)},
		{"null input", testRequest(token, "devices.describe", `null`)},
		{"array input", testRequest(token, "devices.describe", `[]`)},
		{"unknown input field", testRequest(token, "devices.describe", `{"device_id":"d","tenant_id":"tenant-b"}`)},
		{"unknown nested command field", testRequest(token, "commands.preview", `{"device_id":"d","command":{"device_id":"d","identify":"x","tenant_id":"tenant-b"}}`)},
		{"input contains token", testRequest(token, "devices.describe", fmt.Sprintf(`{"device_id":"d","token":%q}`, token))},
		{"trailing value", testRequest(token, "devices.describe", `{"device_id":"d"}`) + ` {}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := invoke(context.Background(), validArgs, tc.body)
			if code == 0 || stderr != "" || string(decodeEnvelope(t, stdout)["error"]) != `"invalid_request"` {
				t.Fatalf("code=%d stderr=%q stdout=%q", code, stderr, stdout)
			}
		})
	}
	tooLarge := strings.Repeat(" ", maxInputBytes+1)
	code, stdout, stderr := invoke(context.Background(), validArgs, tooLarge)
	if code == 0 || stderr != "" || string(decodeEnvelope(t, stdout)["error"]) != `"invalid_request"` {
		t.Fatalf("oversized stdin code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid requests reached network %d times", calls.Load())
	}
	largeToolInput := fmt.Sprintf(`{"device_id":"d","window":{"keys":[%q]}}`, strings.Repeat("x", maxToolInput))
	code, stdout, stderr = invoke(context.Background(), validArgs, testRequest(token, "telemetry.query", largeToolInput))
	if code == 0 || stderr != "" || string(decodeEnvelope(t, stdout)["error"]) != `"invalid_request"` || calls.Load() != 0 {
		t.Fatalf("oversized tool input code=%d calls=%d stderr=%q stdout=%q", code, calls.Load(), stderr, stdout)
	}
}

func TestRunOriginRulesAndDevelopmentHTTP(t *testing.T) {
	invalid := []string{
		"https://user:secret@example.test", "https://example.test/path", "https://example.test/?q=x",
		"https://example.test/#fragment", "http://127.0.0.1:1234", "http://localhost:1234",
		"http://192.0.2.1:1234", "file:///tmp/x", "https://example.test:bad",
	}
	for _, origin := range invalid {
		if _, err := parseOrigin(origin, false); err == nil {
			t.Errorf("parseOrigin(%q) unexpectedly accepted", origin)
		}
	}
	if _, err := parseOrigin("https://127.0.0.1:8443", true); err == nil {
		t.Fatal("development-http flag should not apply to HTTPS")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"device-a","data":{"id":"device-a"}}`)
	}))
	defer server.Close()
	body := testRequest(testToken(), "devices.describe", `{"device_id":"device-a"}`)
	if code, stdout, _ := invoke(context.Background(), []string{"--origin", server.URL}, body); code == 0 || string(decodeEnvelope(t, stdout)["error"]) != `"invalid_request"` {
		t.Fatalf("HTTP without opt-in accepted: code=%d stdout=%q", code, stdout)
	}
	code, stdout, stderr := invoke(context.Background(), []string{"--origin", server.URL, "--development-http"}, body)
	if code != 0 || stderr != "" || string(decodeEnvelope(t, stdout)["ok"]) != "true" {
		t.Fatalf("loopback development HTTP code=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if _, err := parseOrigin("http://localhost:8080", true); err == nil {
		t.Fatal("development HTTP must require a loopback IP literal")
	}
}

func TestRunRedirectDoesNotForwardBearer(t *testing.T) {
	token := testToken()
	var destinationCalls atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("redirect destination received Authorization")
		}
		destinationCalls.Add(1)
		fmt.Fprint(w, `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{}}`)
	}))
	defer destination.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("initial request missing bearer")
		}
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirect.Close()
	code, stdout, stderr := invoke(context.Background(), []string{"--origin", redirect.URL, "--ca-file", tlsCAFile(t, redirect)}, testRequest(token, "devices.describe", `{"device_id":"d"}`))
	if code == 0 || stderr != "" || destinationCalls.Load() != 0 || strings.Contains(stdout, token) {
		t.Fatalf("redirect handling leaked/followed request: code=%d calls=%d stderr=%q stdout=%q", code, destinationCalls.Load(), stderr, stdout)
	}
	if string(decodeEnvelope(t, stdout)["error"]) != `"temporarily_unavailable"` {
		t.Fatalf("redirect error category: %s", stdout)
	}
}

func TestRunKeepsTLSVerificationEnabledWithoutCustomCA(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{}}`)
	}))
	defer server.Close()
	code, stdout, stderr := invoke(context.Background(), []string{"--origin", server.URL}, testRequest(testToken(), "devices.describe", `{"device_id":"d"}`))
	if code == 0 || stderr != "" || calls.Load() != 0 || string(decodeEnvelope(t, stdout)["error"]) != `"temporarily_unavailable"` {
		t.Fatalf("untrusted self-signed server accepted: code=%d calls=%d stderr=%q stdout=%q", code, calls.Load(), stderr, stdout)
	}
}

func TestRunMapsFixedErrorsAndBlocksSecretOrOversizeResponse(t *testing.T) {
	token := testToken()
	for _, tc := range []struct {
		name, status, body, want string
	}{
		{"known category", "401 Unauthorized", `{"error":"not_authorized"}`, `"not_authorized"`},
		{"unknown category", "503 Service Unavailable", `{"error":"db password leaked"}`, `"temporarily_unavailable"`},
		{"secret reflection", "200 OK", "", `"temporarily_unavailable"`},
		{"oversized", "200 OK", "", `"temporarily_unavailable"`},
		{"duplicate response", "200 OK", `{"operation":"devices.describe","operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{}}`, `"temporarily_unavailable"`},
		{"device mismatch", "200 OK", `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"other-device","data":{}}`, `"temporarily_unavailable"`},
		{"operation mismatch", "200 OK", `{"operation":"telemetry.query","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{}}`, `"temporarily_unavailable"`},
		{"missing tenant", "200 OK", `{"operation":"devices.describe","subject_id":"user-a","device_id":"d","data":{}}`, `"temporarily_unavailable"`},
		{"missing subject", "200 OK", `{"operation":"devices.describe","tenant_id":"tenant-a","device_id":"d","data":{}}`, `"temporarily_unavailable"`},
		{"unknown success field", "200 OK", `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{},"extra":true}`, `"temporarily_unavailable"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status == "401 Unauthorized" || tc.status == "503 Service Unavailable" {
					w.WriteHeader(map[string]int{"401 Unauthorized": 401, "503 Service Unavailable": 503}[tc.status])
					fmt.Fprint(w, tc.body)
					return
				}
				if tc.name == "secret reflection" {
					fmt.Fprintf(w, `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{"leak":%q}}`, token)
					return
				}
				if tc.name == "oversized" {
					fmt.Fprintf(w, `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{"padding":%q}}`, strings.Repeat("x", maxResponseBytes))
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			code, stdout, stderr := invoke(context.Background(), []string{"--origin", server.URL, "--ca-file", tlsCAFile(t, server)}, testRequest(token, "devices.describe", `{"device_id":"d"}`))
			if code == 0 || stderr != "" || string(decodeEnvelope(t, stdout)["error"]) != tc.want || strings.Contains(stdout, token) || strings.Contains(stdout, "db password") {
				t.Fatalf("code=%d stderr=%q stdout=%q", code, stderr, stdout)
			}
		})
	}
}

func TestRunHonorsParentDeadlineForSlowHeaderAndBody(t *testing.T) {
	for _, mode := range []string{"header", "body"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "body" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{"x":"`))
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
					<-r.Context().Done()
					return
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(300 * time.Millisecond):
				}
				fmt.Fprint(w, `{"operation":"devices.describe","tenant_id":"tenant-a","subject_id":"user-a","device_id":"d","data":{}}`)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			start := time.Now()
			code, stdout, stderr := invoke(ctx, []string{"--origin", server.URL, "--ca-file", tlsCAFile(t, server)}, testRequest(testToken(), "devices.describe", `{"device_id":"d"}`))
			if code == 0 || stderr != "" || string(decodeEnvelope(t, stdout)["error"]) != `"temporarily_unavailable"` || time.Since(start) > 250*time.Millisecond {
				t.Fatalf("deadline not bounded: code=%d elapsed=%s stderr=%q stdout=%q", code, time.Since(start), stderr, stdout)
			}
		})
	}
}
