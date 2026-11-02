package automationbridge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeTickTransportReceiptAndIdentity(t *testing.T) {
	token := strings.Repeat("s", 32)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		valid  bool
	}{
		{"valid", 200, `{"replayed":1,"tick":{"schedule_materialized":1,"schedule_executed":1,"outbox_drained":1}}`, true},
		{"HTTP failure", 503, `{}`, false},
		{"missing receipt", 200, `{}`, false},
		{"negative count", 200, `{"replayed":0,"tick":{"schedule_materialized":0,"schedule_executed":-1,"outbox_drained":0}}`, false},
		{"trailing payload", 200, `{"replayed":0,"tick":{"schedule_materialized":0,"schedule_executed":0,"outbox_drained":0}} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPost || r.URL.Path != "/internal/automation/tick" || string(body) != "{}" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get(DeploymentHeader) != "deployment-a" || r.Header.Get(TenantHeader) != "" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected scheduler request method=%s path=%s body=%s", r.Method, r.URL.Path, body)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := &Client{url: server.URL, token: token, deployment: "deployment-a", http: server.Client()}
			receipt, err := client.Tick(context.Background())
			if (err == nil) != tc.valid {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			if tc.valid && (receipt.Replayed != 1 || receipt.Counters["schedule_executed"] != 1) {
				t.Fatalf("bad counters: %+v", receipt)
			}
		})
	}
}
