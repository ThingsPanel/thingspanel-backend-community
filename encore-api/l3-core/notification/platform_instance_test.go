package notification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func TestPlatformSelectedTenantOwnsAccountWritesAndValidation(t *testing.T) {
	const jwtKey = "platform-instance-fixture-jwt-key"
	t.Setenv("TP_JWT_KEY", jwtKey)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	store, _ := newMigratedTestStore(t)

	var validateCalls, sendCalls atomic.Int32
	pluginServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/validate":
			validateCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer fixture-plugin-auth" {
				t.Error("validation did not use the encrypted registration credential")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"valid":true,"errors":[]}`))
		case "/v1/send":
			sendCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer pluginServer.Close()

	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: "platform-smtp-fixture", Name: "Platform SMTP fixture", PluginVersion: "1.0.0",
		Channels: []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema: json.RawMessage(`{"type":"object","properties":{"password":{"type":"string"}}}`), RecipientSchema: json.RawMessage(`{"type":"object"}`), SecretFields: []string{"password"},
	}
	manifestBytes, _ := json.Marshal(manifest)
	manifestSum := sha256.Sum256(manifestBytes)
	digest := "sha256:" + hex.EncodeToString(manifestSum[:])
	registered, err := store.RegisterPlatformPlugin(context.Background(), "platform-fixture-actor", "platform-fixture-registration", strings.Repeat("a", 64), contract.PluginRegistration{
		PluginID: manifest.PluginID, PluginVersion: manifest.PluginVersion, Origin: pluginServer.URL, AuthSecret: "fixture-plugin-auth", ManifestDigest: digest,
	}, manifest)
	if err != nil {
		t.Fatalf("register platform fixture plugin: %v", err)
	}
	_, _, err = store.SetPluginGrant(context.Background(), "tenant-selected", registered.ID, "platform-fixture-grant", strings.Repeat("b", 64), contract.PluginGrantUpdate{
		PluginRegistrationID: registered.ID, TenantID: "tenant-selected", Enabled: true, ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("grant plugin to selected tenant: %v", err)
	}

	infraPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(infraPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + infraPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct privacy fixture")
	}
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	if err != nil {
		t.Fatal("construct fixture plugin client")
	}
	directoryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-directory-token-at-least-32" || r.Header.Get("X-Notification-Deployment") != "deployment-test" {
			t.Error("directory lookup did not use its scoped read identity")
		}
		if r.URL.Path != "/api/v1/notification/source-tenants/tenant-selected" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenantId":"tenant-selected","exists":true}`))
	}))
	defer directoryServer.Close()
	directory, err := NewLegacyDirectoryReader(directoryServer.URL, "fixture-directory-token-at-least-32", "deployment-test", LegacySessionOptions{TestMode: true, AllowHTTPTestEndpoint: true})
	if err != nil {
		t.Fatal("construct scoped directory reader")
	}
	service, err := NewManagementService(store, store, store, store, store, validationSessionFixture{}, client, newTestSnapshotProtector(), privacy, ManagementOptions{
		TestMode: true, AllowHTTPTestOrigin: true, TenantDirectory: directory,
	})
	if err != nil {
		t.Fatalf("construct management service: %v", err)
	}
	handler := NewManagementHTTPHandler(service)
	admin := makePluginGrantToken(jwtKey, "platform-admin-selected-tenant", "", "SYS_ADMIN")
	tenantAdmin := makePluginGrantToken(jwtKey, "tenant-admin", "tenant-selected", "TENANT_ADMIN")
	request := func(method, target, token, key string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal("marshal request body")
			}
		}
		req := httptest.NewRequest(method, target, bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	legacyBody := contract.InstanceCreate{PluginRegistrationID: registered.ID, Name: "attempt", Channel: contract.ChannelEmail, Config: json.RawMessage(`{"password":"tenant-secret"}`), ProviderIdentity: map[string]string{}}
	if got := request(http.MethodPost, "/api/v2/notification-instances", tenantAdmin, "tenant-create-denied", legacyBody); got.Code != http.StatusForbidden {
		t.Fatalf("tenant account create status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodPut, "/api/v2/notification-instances/missing", tenantAdmin, "tenant-update-denied", contract.InstanceUpdate{ExpectedVersion: 1}); got.Code != http.StatusForbidden {
		t.Fatalf("tenant account update status=%d", got.Code)
	}
	if got := request(http.MethodPost, "/api/v2/notification-instances/validate", tenantAdmin, "", contract.InstanceValidate{Draft: &contract.InstanceDraft{PluginRegistrationID: registered.ID, Channel: contract.ChannelEmail, Config: json.RawMessage(`{"password":"tenant-secret"}`)}}); got.Code != http.StatusForbidden {
		t.Fatalf("tenant account validation status=%d", got.Code)
	}
	if got := request(http.MethodPost, "/api/v2/notification-instances/missing/receipt-credential", tenantAdmin, "tenant-receipt-denied", nil); got.Code != http.StatusForbidden {
		t.Fatalf("tenant receipt issuance status=%d", got.Code)
	}
	if validateCalls.Load() != 0 || sendCalls.Load() != 0 {
		t.Fatalf("tenant writes reached plugin: validate=%d send=%d", validateCalls.Load(), sendCalls.Load())
	}

	pluginResponse := request(http.MethodGet, "/api/v2/platform/notification-tenants/tenant-selected/notification-plugins", admin, "", nil)
	if pluginResponse.Code != http.StatusOK || !strings.Contains(pluginResponse.Body.String(), registered.ID) {
		t.Fatalf("selected-tenant plugin list status=%d body=%s", pluginResponse.Code, pluginResponse.Body.String())
	}
	if got := request(http.MethodGet, "/api/v2/platform/notification-tenants/not-a-tenant/notification-plugins", admin, "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("unknown selected tenant status=%d", got.Code)
	}
	if got := request(http.MethodGet, "/api/v2/platform/notification-tenants/not-a-tenant/notification-instances", admin, "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("unknown selected tenant instance list status=%d", got.Code)
	}
	if got := request(http.MethodGet, "/api/v2/platform/notification-tenants/tenant-selected/notification-plugins", tenantAdmin, "", nil); got.Code != http.StatusForbidden {
		t.Fatalf("tenant role used platform target route status=%d", got.Code)
	}

	created := request(http.MethodPost, "/api/v2/platform/notification-tenants/tenant-selected/notification-instances", admin, "platform-instance-create", legacyBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("platform account create status=%d body=%s", created.Code, created.Body.String())
	}
	var createdEnvelope contract.Envelope[contract.InstanceView]
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatal("decode created account")
	}
	if createdEnvelope.Data.ID == "" || strings.Contains(created.Body.String(), "tenant-secret") {
		t.Fatalf("created account leaked secret or omitted its ID: %s", created.Body.String())
	}
	if _, err := store.GetInstance(context.Background(), "tenant-selected", createdEnvelope.Data.ID); err != nil {
		t.Fatalf("created platform account was not stored under selected tenant: %v", err)
	}
	if _, err := store.GetInstance(context.Background(), "platform-admin-selected-tenant", createdEnvelope.Data.ID); err == nil {
		t.Fatal("selected tenant path was rewritten to the administrator's tenant")
	}
	expected := createdEnvelope.Data.Version
	validate := request(http.MethodPost, "/api/v2/platform/notification-tenants/tenant-selected/notification-instances/validate", admin, "", contract.InstanceValidate{
		InstanceID: &createdEnvelope.Data.ID, ExpectedVersion: &expected, ConfigPatch: json.RawMessage(`{}`),
	})
	if validate.Code != http.StatusOK || validateCalls.Load() != 1 || sendCalls.Load() != 0 {
		t.Fatalf("platform validation did not remain non-sending: status=%d validate=%d send=%d body=%s", validate.Code, validateCalls.Load(), sendCalls.Load(), validate.Body.String())
	}
	read := request(http.MethodGet, "/api/v2/platform/notification-tenants/tenant-selected/notification-instances/"+createdEnvelope.Data.ID, admin, "", nil)
	if read.Code != http.StatusOK || strings.Contains(read.Body.String(), "tenant-secret") {
		t.Fatalf("platform account summary status=%d body=%s", read.Code, read.Body.String())
	}
}
