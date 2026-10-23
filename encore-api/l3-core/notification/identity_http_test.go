package notification

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

type observedBody struct {
	reads int
	data  []byte
}

func TestPublicValidationResultNeverReturnsPluginDiagnostics(t *testing.T) {
	secret := "config-secret-sentinel"
	result := publicValidationResult(contract.PluginValidateResultV1{
		Valid:  false,
		Errors: []contract.PluginFieldErrorV1{{Field: "password=" + secret, Code: "secret-code", Message: "rejected " + secret}},
	})
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "secret-code") || strings.Contains(string(encoded), "password=") {
		t.Fatalf("public validation result exposed plugin diagnostics: %s", encoded)
	}
	if result.Valid || len(result.Errors) != 1 || result.Errors[0] != (contract.FieldError{Field: "config", Reason: "plugin_validation_failed"}) {
		t.Fatalf("unexpected safe validation result: %+v", result)
	}
}

func (b *observedBody) Read(p []byte) (int, error) {
	b.reads++
	return bytes.NewReader(b.data).Read(p)
}

func (*observedBody) Close() error { return nil }

var _ io.ReadCloser = (*observedBody)(nil)

func TestManagementHTTPRejectsUserBeforeReadingSecretBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(path, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal(err)
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + path + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + path + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("fixture privacy guard did not initialize")
	}
	service := &ManagementService{privacy: privacy, clock: time.Now}
	body := &observedBody{data: []byte(`{"authSecret":"body-secret-sentinel"}`)}
	request := httptest.NewRequest(http.MethodPost, "/api/v2/notification-plugins", body)
	request.Header.Set("Authorization", "Bearer malformed")
	response := httptest.NewRecorder()
	NewManagementHTTPHandler(service).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid user status=%d body=%s", response.Code, response.Body.String())
	}
	if body.reads != 0 {
		t.Fatalf("handler read sensitive body %d times before user verification", body.reads)
	}
	if strings.Contains(response.Body.String(), "body-secret-sentinel") {
		t.Fatal("response echoed a sensitive request value")
	}
}
