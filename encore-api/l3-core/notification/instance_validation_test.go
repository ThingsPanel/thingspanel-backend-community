package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

type validationSessionFixture struct{}

type validationTenantDirectory struct{}

func (validationTenantDirectory) Exists(_ context.Context, tenantID string) (bool, error) {
	return tenantID != "", nil
}

func (validationSessionFixture) CheckUserSession(context.Context, string, UserPrincipal) error {
	return nil
}

func makeValidationTestToken(key, userID, tenantID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claimsJSON, _ := json.Marshal(map[string]any{
		"id": userID, "tenant_id": tenantID, "authority": "TENANT_ADMIN", "exp": time.Now().Add(time.Hour).Unix(),
	})
	claims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsigned := header + "." + claims
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func newValidationHTTPHandler(t *testing.T, store *Store, client *SafePluginClient) http.Handler {
	t.Helper()
	infraPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(infraPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct privacy fixture")
	}
	service, err := NewManagementService(store, store, store, store, store, validationSessionFixture{}, client, newTestSnapshotProtector(), privacy, ManagementOptions{TestMode: true, AllowHTTPTestOrigin: true, TenantDirectory: validationTenantDirectory{}})
	if err != nil {
		t.Fatalf("construct management service: %v", err)
	}
	return NewManagementHTTPHandler(service)
}

func TestDisabledInstanceCanBeValidatedWithoutChangingConfigOrSending(t *testing.T) {
	const jwtKey = "fixture-validation-jwt-key"
	t.Setenv("TP_JWT_KEY", jwtKey)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	store, db := newMigratedTestStore(t)
	var validateCalls, sendCalls atomic.Int32
	var validationRequest contract.PluginValidateRequestV1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/validate":
			validateCalls.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-worker-auth" {
				t.Error("validation request did not use the granted registration credential")
			}
			if err := json.NewDecoder(r.Body).Decode(&validationRequest); err != nil {
				t.Error("decode plugin validation request")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"valid":true,"errors":[]}`))
		case "/v1/send":
			sendCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected plugin endpoint %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	instanceID := createWorkerFixtureInstance(t, store, "validation-disabled-tenant", server.URL)
	current, err := store.GetInstance(context.Background(), "validation-disabled-tenant", instanceID)
	if err != nil {
		t.Fatalf("read enabled fixture instance: %v", err)
	}
	disabled := false
	if _, err := store.UpdateInstance(context.Background(), "validation-disabled-tenant", instanceID, "validation-disable", strings.Repeat("f", 64), contract.InstanceUpdate{ExpectedVersion: current.Version, Enabled: &disabled}); err != nil {
		t.Fatalf("disable fixture instance: %v", err)
	}
	view, err := store.GetInstance(context.Background(), "validation-disabled-tenant", instanceID)
	if err != nil || view.Enabled {
		t.Fatalf("fixture instance is not disabled: view=%+v err=%v", view, err)
	}
	var beforeConfigRows int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM notification_instance_configs WHERE tenant_id=$1 AND instance_id=$2`, "validation-disabled-tenant", instanceID).Scan(&beforeConfigRows); err != nil {
		t.Fatal("count frozen config snapshots before validation")
	}
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	if err != nil {
		t.Fatal("construct fixture-only plugin client")
	}
	handler := newValidationHTTPHandler(t, store, client)
	requestBody := func(tenantID string, expectedVersion int64, patch json.RawMessage) string {
		body, marshalErr := json.Marshal(contract.InstanceValidate{InstanceID: &instanceID, ExpectedVersion: &expectedVersion, ConfigPatch: patch})
		if marshalErr != nil {
			t.Fatal("encode validation request")
		}
		return string(body)
	}
	call := func(tenantID, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/platform/notification-tenants/"+tenantID+"/notification-instances/validate", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+makePluginGrantToken(jwtKey, "fixture-user", "", "SYS_ADMIN"))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	patch := json.RawMessage(`{"region":"validation-only"}`)
	success := call("validation-disabled-tenant", requestBody("validation-disabled-tenant", view.Version, patch))
	if success.Code != http.StatusOK || validateCalls.Load() != 1 {
		t.Fatalf("disabled instance validation status=%d calls=%d body=%s", success.Code, validateCalls.Load(), success.Body.String())
	}
	var validatedConfig map[string]json.RawMessage
	if err := json.Unmarshal(validationRequest.Config, &validatedConfig); err != nil || string(validatedConfig["region"]) != `"validation-only"` || string(validatedConfig["token"]) != `"fixture-worker-secret"` || validationRequest.ConfigVersion != view.ConfigVersion {
		t.Fatalf("validation did not use exact frozen config plus in-memory patch: config=%s version=%d err=%v", validationRequest.Config, validationRequest.ConfigVersion, err)
	}
	if sendCalls.Load() != 0 {
		t.Fatalf("validation invoked plugin send endpoint %d times", sendCalls.Load())
	}
	emptyPatchValidation := call("validation-disabled-tenant", requestBody("validation-disabled-tenant", view.Version, json.RawMessage(`{}`)))
	if emptyPatchValidation.Code != http.StatusOK || validateCalls.Load() != 2 {
		t.Fatalf("empty configPatch validation status=%d calls=%d body=%s", emptyPatchValidation.Code, validateCalls.Load(), emptyPatchValidation.Body.String())
	}
	var unchangedConfig map[string]json.RawMessage
	if err := json.Unmarshal(validationRequest.Config, &unchangedConfig); err != nil || string(unchangedConfig["region"]) != `"fixture"` || string(unchangedConfig["token"]) != `"fixture-worker-secret"` {
		t.Fatalf("empty configPatch did not validate the original full config: config=%s err=%v", validationRequest.Config, err)
	}

	crossTenant := call("validation-other-tenant", requestBody("validation-other-tenant", view.Version, patch))
	if crossTenant.Code != http.StatusNotFound || validateCalls.Load() != 2 {
		t.Fatalf("cross-tenant validation status=%d validation calls=%d", crossTenant.Code, validateCalls.Load())
	}
	stale := call("validation-disabled-tenant", requestBody("validation-disabled-tenant", view.Version-1, patch))
	if stale.Code != http.StatusConflict || validateCalls.Load() != 2 {
		t.Fatalf("stale expectedVersion status=%d validation calls=%d", stale.Code, validateCalls.Load())
	}
	nilPatch := fmt.Sprintf(`{"instanceId":%q,"expectedVersion":%d}`, instanceID, view.Version)
	missingPatch := call("validation-disabled-tenant", nilPatch)
	if missingPatch.Code != http.StatusBadRequest || validateCalls.Load() != 2 {
		t.Fatalf("nil configPatch status=%d validation calls=%d", missingPatch.Code, validateCalls.Load())
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE notification_plugin_grants SET enabled=FALSE WHERE tenant_id=$1 AND registration_id=$2`, "validation-disabled-tenant", view.PluginRegistrationID); err != nil {
		t.Fatal("revoke fixture plugin grant")
	}
	revokedGrant := call("validation-disabled-tenant", requestBody("validation-disabled-tenant", view.Version, patch))
	if revokedGrant.Code != http.StatusNotFound || validateCalls.Load() != 2 {
		t.Fatalf("revoked-grant validation status=%d validation calls=%d", revokedGrant.Code, validateCalls.Load())
	}
	var enabled bool
	var currentConfigVersion, currentVersion int64
	var afterConfigRows int
	if err := db.QueryRowContext(context.Background(), `SELECT enabled,current_config_version,version FROM notification_instances WHERE tenant_id=$1 AND id=$2`, "validation-disabled-tenant", instanceID).Scan(&enabled, &currentConfigVersion, &currentVersion); err != nil {
		t.Fatal("read instance state after validation attempts")
	}
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM notification_instance_configs WHERE tenant_id=$1 AND instance_id=$2`, "validation-disabled-tenant", instanceID).Scan(&afterConfigRows); err != nil {
		t.Fatal("count frozen config snapshots after validation")
	}
	if enabled || currentConfigVersion != view.ConfigVersion || currentVersion != view.Version || beforeConfigRows != afterConfigRows {
		t.Fatalf("validation changed persisted config/state: enabled=%v configVersion=%d version=%d configRows=%d->%d", enabled, currentConfigVersion, currentVersion, beforeConfigRows, afterConfigRows)
	}
}
