package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

type pluginGrantDirectoryFixture map[string]bool

func (d pluginGrantDirectoryFixture) Exists(_ context.Context, tenantID string) (bool, error) {
	return d[tenantID], nil
}

func makePluginGrantToken(key, userID, tenantID, role string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claimsJSON, _ := json.Marshal(map[string]any{"id": userID, "tenant_id": tenantID, "authority": role, "exp": time.Now().Add(time.Hour).Unix()})
	claims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsigned := header + "." + claims
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestPluginGrantHTTPPlatformScopeReplayAndRevocation(t *testing.T) {
	const jwtKey = "fixture-plugin-grant-jwt-key"
	t.Setenv("TP_JWT_KEY", jwtKey)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	store, _ := newMigratedTestStore(t)
	privacyPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(privacyPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + privacyPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + privacyPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct privacy fixture")
	}
	pluginHTTP, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true})
	if err != nil {
		t.Fatal("construct plugin client")
	}
	directory := pluginGrantDirectoryFixture{"tenant-target": true}
	service, err := NewManagementService(store, store, store, store, store, validationSessionFixture{}, pluginHTTP, newTestSnapshotProtector(), privacy, ManagementOptions{
		TestMode: true, AllowHTTPTestOrigin: true, TenantDirectory: directory,
	})
	if err != nil {
		t.Fatalf("construct management service: %v", err)
	}
	handler := NewManagementHTTPHandler(service)
	rawHandler := &NotificationHTTPHandler{
		Verifier: &coreVerifierFixture{user: UserPrincipal{UserID: "verified-native-route", TenantID: "fixture-tenant", Scopes: []string{"notification:read"}}},
		Core:     &Core{}, Store: store, Management: handler, Ready: func() error { return nil },
	}
	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: "grant-fixture", Name: "Grant fixture", PluginVersion: "1.0.0",
		Channels: []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema: json.RawMessage(`{"type":"object"}`), RecipientSchema: json.RawMessage(`{"type":"object"}`), SecretFields: []string{"token"},
	}
	registered, err := store.RegisterPlugin(context.Background(), "platform-admin-tenant", "grant-fixture-registration", strings.Repeat("a", 64), contract.PluginRegistration{
		PluginID: manifest.PluginID, PluginVersion: manifest.PluginVersion, Origin: "https://grant-fixture.example.test", AuthSecret: "fixture-auth", ManifestDigest: strings.Repeat("b", 64),
	}, manifest)
	if err != nil {
		t.Fatalf("register global plugin: %v", err)
	}
	adminToken := makePluginGrantToken(jwtKey, "platform-admin", "platform-admin-tenant", "SYS_ADMIN")
	tenantToken := makePluginGrantToken(jwtKey, "tenant-admin", "tenant-target", "TENANT_ADMIN")
	installerToken := makePluginGrantToken(jwtKey, "installer-admin", "platform-admin-tenant", "TENANT_ADMIN")
	request := func(method, target, bearer, idempotencyKey string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
		}
		req := httptest.NewRequest(method, target, bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+bearer)
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	rawRequest := func(method, target, bearer, idempotencyKey string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal raw request: %v", err)
			}
		}
		req := httptest.NewRequest(method, target, bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+bearer)
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		rawHandler.ServeHTTP(response, req)
		return response
	}
	decodeGrant := func(response *httptest.ResponseRecorder) contract.PluginGrantView {
		t.Helper()
		var envelope contract.Envelope[contract.PluginGrantView]
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode grant response: %v (%s)", err, response.Body.String())
		}
		return envelope.Data
	}
	path := "/api/v2/notification-plugin-grants"
	if got := request(http.MethodGet, "/api/v2/notification-plugins/"+registered.ID, installerToken, "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("global registration implicitly granted installer tenant: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodGet, "/api/v2/notification-plugins/"+registered.ID, tenantToken, "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("ungranted tenant saw global plugin: status=%d body=%s", got.Code, got.Body.String())
	}
	grant := contract.PluginGrantUpdate{PluginRegistrationID: registered.ID, TenantID: "tenant-target", Enabled: true, ExpectedVersion: 0}
	created := rawRequest(http.MethodPut, path, adminToken, "grant-create-001", grant)
	if created.Code != http.StatusCreated {
		t.Fatalf("grant create status=%d body=%s", created.Code, created.Body.String())
	}
	createdView := decodeGrant(created)
	if createdView.Version != 1 || !createdView.Enabled || createdView.TenantID != "tenant-target" {
		t.Fatalf("unexpected created grant: %+v", createdView)
	}
	instance, err := store.CreateInstance(context.Background(), "tenant-target", "grant-instance-create", strings.Repeat("c", 64), contract.InstanceCreate{
		PluginRegistrationID: registered.ID, Name: "grant fixture account", Channel: contract.ChannelEmail,
		Config: json.RawMessage(`{"region":"fixture"}`), ProviderIdentity: map[string]string{"account": "fixture"},
	}, map[string]string{"token": "fixture-instance-secret"})
	if err != nil {
		t.Fatalf("create tenant instance under grant: %v", err)
	}
	enabled := true
	if _, err := store.UpdateInstance(context.Background(), "tenant-target", instance.ID, "grant-instance-enable", strings.Repeat("d", 64), contract.InstanceUpdate{ExpectedVersion: 1, Enabled: &enabled}); err != nil {
		t.Fatalf("enable tenant instance: %v", err)
	}
	submission := fixtureSubmission(instance.ID, "recipient@example.test")
	if _, err := store.AcceptSubmission(context.Background(), "tenant-target", "grant-submit-001", strings.Repeat("e", 64), submission); err != nil {
		t.Fatalf("queue delivery before grant revoke: %v", err)
	}
	if got := request(http.MethodGet, "/api/v2/notification-plugins/"+registered.ID, tenantToken, "", nil); got.Code != http.StatusOK {
		t.Fatalf("enabled grant did not expose plugin to its tenant: status=%d body=%s", got.Code, got.Body.String())
	}
	updated := grant
	updated.Enabled = false
	updated.ExpectedVersion = 1
	updateResponse := request(http.MethodPut, path, adminToken, "grant-disable-001", updated)
	if updateResponse.Code != http.StatusOK || decodeGrant(updateResponse).Version != 2 {
		t.Fatalf("grant revoke status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	claims, err := store.ClaimDeliveries(context.Background(), 1, time.Minute)
	if err != nil || len(claims) != 0 {
		t.Fatalf("revoked grant allowed queued claim: claims=%d err=%v", len(claims), err)
	}
	stale := updated
	stale.ExpectedVersion = 1
	if got := request(http.MethodPut, path, adminToken, "grant-stale-version", stale); got.Code != http.StatusConflict {
		t.Fatalf("stale grant version was not rejected: status=%d body=%s", got.Code, got.Body.String())
	}
	directory["tenant-target"] = false
	replay := request(http.MethodPut, path, adminToken, "grant-create-001", grant)
	if replay.Code != http.StatusCreated || decodeGrant(replay) != createdView {
		t.Fatalf("old grant key did not replay immutable original response: status=%d body=%s", replay.Code, replay.Body.String())
	}
	if got := request(http.MethodGet, "/api/v2/notification-plugins/"+registered.ID, tenantToken, "", nil); got.Code != http.StatusNotFound {
		t.Fatalf("revoked tenant grant still exposes plugin: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodPut, path, adminToken, "grant-missing-target", contract.PluginGrantUpdate{PluginRegistrationID: registered.ID, TenantID: "tenant-missing", Enabled: true, ExpectedVersion: 0}); got.Code != http.StatusNotFound {
		t.Fatalf("grant to missing tenant was not rejected: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodGet, path, tenantToken, "", nil); got.Code != http.StatusForbidden {
		t.Fatalf("tenant admin listed global grants: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := rawRequest(http.MethodPut, path, tenantToken, "tenant-grant-attempt", grant); got.Code != http.StatusForbidden {
		t.Fatalf("tenant admin created global grant: status=%d body=%s", got.Code, got.Body.String())
	}
	conflict := grant
	conflict.Enabled = false
	if got := request(http.MethodPut, path, adminToken, "grant-create-001", conflict); got.Code != http.StatusConflict {
		t.Fatalf("same grant key with a different request was not rejected: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodGet, path+"?unexpected=1", adminToken, "", nil); got.Code != http.StatusBadRequest {
		t.Fatalf("unknown grant query parameter accepted: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(http.MethodGet, path+"?page=1&page=2", adminToken, "", nil); got.Code != http.StatusBadRequest {
		t.Fatalf("duplicate grant query parameter accepted: status=%d body=%s", got.Code, got.Body.String())
	}
	listed := rawRequest(http.MethodGet, path+"?pluginRegistrationId="+registered.ID+"&page=1&pageSize=10", adminToken, "", nil)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"pageSize":10`)) || !bytes.Contains(listed.Body.Bytes(), []byte(`"tenantId":"tenant-target"`)) {
		t.Fatalf("sys admin grant list failed: status=%d body=%s", listed.Code, listed.Body.String())
	}
}
