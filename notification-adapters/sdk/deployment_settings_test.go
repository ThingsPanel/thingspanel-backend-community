package sdk

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeploymentSettingsLifecycle(t *testing.T) {
	const token = "unit-test-only-service-key-123456789"
	const credential = "fixture-cloud-secret"
	file := filepath.Join(t.TempDir(), "provider.json")
	build := func(v DeploymentSettings) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(v.Address)) }), nil
	}
	start := func() http.Handler {
		h, err := WithDeploymentSettings(http.NotFoundHandler(), token, file, DeploymentSettings{}, build)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	h := start()
	call := func(method, body, auth string, want int) string {
		r := httptest.NewRequest(method, "/v1/deployment-settings", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want || strings.Contains(w.Body.String(), credential) {
			t.Fatalf("unexpected settings response %d", w.Code)
		}
		return w.Body.String()
	}
	call("GET", "", "wrong", 401)
	body := `{"address":"https://example.com","secret":"fixture-cloud-secret","version":0}`
	call("PUT", body, token, 200)
	call("PUT", body, token, 409)
	raw, err := os.ReadFile(file)
	if err != nil || strings.Contains(string(raw), credential) {
		t.Fatal("invalid persisted configuration")
	}
	h = start()
	if !strings.Contains(call("GET", "", token, 200), `"hasSecret":true`) {
		t.Fatal("saved credential not restored")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/probe", nil))
	if w.Body.String() != "https://example.com" {
		t.Fatal("saved provider not active")
	}
}
