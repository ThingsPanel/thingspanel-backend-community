package communityclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticatedDiagnosticsUseCallerWithoutServiceFallback(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-token") != "caller" || r.Header.Get("x-api-key") != "" {
			t.Error("wrong identity")
		}
		fmt.Fprint(w, `{"code":200,"data":{"enabled":true,"expire_at":123}}`)
	}))
	defer ts.Close()
	c := &Client{baseURL: ts.URL, apiKey: "service-key", http: ts.Client()}
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Authorization", "Bearer caller")
	ctx := WithAuthenticatedDiagnosticRequest(context.Background(), r)
	if err := c.SimulationSend(ctx, "own-device", `{"qa":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetDebug(ctx, "own-device", true, 123); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetDebugState(ctx, "own-device"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/device", "/api/v1/login", "/api/v1/device/d/debug/extra", "/api/v1/device/d/debug?x=1"} {
		if _, err := c.doRaw(ctx, "POST", path, "", nil); !errors.Is(err, ErrRequestAuthentication) {
			t.Errorf("allowed %s", path)
		}
	}
	empty := WithAuthenticatedDiagnosticRequest(context.Background(), httptest.NewRequest("POST", "/", nil))
	if err := c.SimulationSend(empty, "d", "{}"); !errors.Is(err, ErrRequestAuthentication) {
		t.Fatal("missing token fell back")
	}
	if calls != 3 {
		t.Fatalf("unexpected calls %d", calls)
	}
}
func TestCancellationRequestScope(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Authorization", "Bearer caller")
	a := WithAuthenticatedCommandCancellationRequest(context.Background(), r, "d", "m").Value(authenticatedReadKey{}).(authenticatedRead)
	for _, row := range []struct {
		method, path string
		want         bool
	}{{"DELETE", "/api/v1/expected/data/m", true}, {"DELETE", "/api/v1/expected/data/other", false}, {"GET", "/api/v1/expected/data/list?device_id=d&send_type=command", true}, {"GET", "/api/v1/expected/data/list?device_id=other&send_type=command", false}, {"GET", "/api/v1/expected/data/list?device_id=d&device_id=other&send_type=command", false}, {"POST", "/api/v1/expected/data", false}} {
		if got := a.permits(row.method, row.path); got != row.want {
			t.Errorf("%s %s = %v", row.method, row.path, got)
		}
	}
}
