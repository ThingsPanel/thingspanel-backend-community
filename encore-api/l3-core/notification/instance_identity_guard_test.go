package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

func createIdentityGuardPlugin(t *testing.T, store *Store, tenantID, pluginID string, identityFields []string) string {
	t.Helper()
	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: pluginID, Name: "Identity guard fixture", PluginVersion: "1.0.0",
		IdentityFields: identityFields,
		Channels:       []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema:    json.RawMessage(`{"type":"object","properties":{"region":{"type":"string"},"from_email":{"type":"string"},"callback_url":{"type":"string"},"password":{"type":"string"},"host":{"type":"string"}},"additionalProperties":false}`),
		RecipientSchema: json.RawMessage(`{"type":"object"}`), SecretFields: []string{"callback_url", "password"},
	}
	registered, err := store.RegisterPlugin(context.Background(), tenantID, "identity-plugin-register-"+pluginID, strings.Repeat("a", 64), contract.PluginRegistration{
		PluginID: pluginID, PluginVersion: manifest.PluginVersion, Origin: "https://identity-fixture.example.test", AuthSecret: "fixture-auth", ManifestDigest: strings.Repeat("b", 64),
	}, manifest)
	if err != nil {
		t.Fatalf("register identity fixture plugin: %v", err)
	}
	grantFixturePlugin(t, store, tenantID, registered.ID)
	return registered.ID
}

func createIdentityGuardInstance(t *testing.T, store *Store, tenantID, registrationID, suffix string) contract.InstanceView {
	t.Helper()
	instance, err := store.CreateInstance(context.Background(), tenantID, "identity-instance-"+suffix, strings.Repeat("c", 64), contract.InstanceCreate{
		PluginRegistrationID: registrationID, Name: "Identity fixture", Channel: contract.ChannelEmail,
		Config:           json.RawMessage(`{"region":"us-east-1","from_email":"old@example.test","host":"smtp-old.example.test"}`),
		ProviderIdentity: map[string]string{"account": "provider-account-1"},
	}, map[string]string{"callback_url": "https://provider.example.test/callback/old", "password": "old-password"})
	if err != nil {
		t.Fatalf("create identity fixture instance: %v", err)
	}
	return instance
}

func identityGuardHTTPHandler(t *testing.T, store *Store) http.Handler {
	t.Helper()
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true})
	if err != nil {
		t.Fatal("construct fixture plugin client")
	}
	return newValidationHTTPHandler(t, store, client)
}

func TestIdentityMetadataGuardsConfigAndSecretChangesOverRealHTTPAndPostgres(t *testing.T) {
	const jwtKey = "fixture-identity-guard-jwt-key"
	t.Setenv("TP_JWT_KEY", jwtKey)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	store, db := newMigratedTestStore(t)
	const tenantID = "identity-guard-tenant"
	registrationID := createIdentityGuardPlugin(t, store, tenantID, "identity-aware-fixture", []string{"region", "from_email", "callback_url"})
	instance := createIdentityGuardInstance(t, store, tenantID, registrationID, "aware")
	enabled := true
	instance, err := store.UpdateInstance(context.Background(), tenantID, instance.ID, "identity-enable-aware", strings.Repeat("d", 64), contract.InstanceUpdate{ExpectedVersion: 1, Enabled: &enabled})
	if err != nil || instance.ConfigVersion != 2 {
		t.Fatalf("enable fixture instance: view=%+v err=%v", instance, err)
	}
	if _, err := store.AcceptSubmission(context.Background(), tenantID, "identity-inflight-submit", strings.Repeat("e", 64), fixtureSubmission(instance.ID, "recipient@example.test")); err != nil {
		t.Fatalf("submit in-flight fixture delivery: %v", err)
	}
	handler := identityGuardHTTPHandler(t, store)
	bearer := makePluginGrantToken(jwtKey, "identity-admin", "", "SYS_ADMIN")
	put := func(key string, update contract.InstanceUpdate) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(update)
		if err != nil {
			t.Fatalf("marshal instance update: %v", err)
		}
		request := httptest.NewRequest(http.MethodPut, "/api/v2/platform/notification-tenants/"+tenantID+"/notification-instances/"+instance.ID, bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bearer)
		request.Header.Set("Idempotency-Key", key)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	assertIdentityConflict := func(response *httptest.ResponseRecorder) {
		t.Helper()
		var domainError contract.DomainError
		if err := json.Unmarshal(response.Body.Bytes(), &domainError); err != nil {
			t.Fatalf("decode expected identity conflict: %v (%s)", err, response.Body.String())
		}
		if response.Code != http.StatusConflict || domainError.Details.Reason != "identity_change_requires_new_instance" {
			t.Fatalf("identity change response=%d %+v body=%s", response.Code, domainError, response.Body.String())
		}
	}
	patch := func(raw string, expected int64) contract.InstanceUpdate {
		return contract.InstanceUpdate{ExpectedVersion: expected, ConfigPatch: json.RawMessage(raw)}
	}
	assertIdentityConflict(put("identity-region-change", patch(`{"region":"eu-west-1"}`, 2)))
	assertIdentityConflict(put("identity-from-change", patch(`{"from_email":"new@example.test"}`, 2)))
	assertIdentityConflict(put("identity-secret-change", contract.InstanceUpdate{ExpectedVersion: 2, Secrets: &contract.SecretPatch{Set: map[string]string{"callback_url": "https://provider.example.test/callback/new"}}}))
	assertIdentityConflict(put("identity-secret-clear", contract.InstanceUpdate{ExpectedVersion: 2, Secrets: &contract.SecretPatch{Clear: []string{"callback_url"}}}))

	noOp := put("identity-region-same", patch(`{"region":"us-east-1"}`, 2))
	if noOp.Code != http.StatusOK {
		t.Fatalf("same identity value was rejected: status=%d body=%s", noOp.Code, noOp.Body.String())
	}
	var envelope contract.Envelope[contract.InstanceView]
	if json.Unmarshal(noOp.Body.Bytes(), &envelope) != nil || envelope.Data.ConfigVersion != 3 {
		t.Fatalf("same-value identity update did not preserve normal version behavior: %s", noOp.Body.String())
	}
	hostPatch := patch(`{"host":"smtp-new.example.test"}`, 3)
	hostUpdate := put("identity-host-safe-change", hostPatch)
	if hostUpdate.Code != http.StatusOK {
		t.Fatalf("non-identity config change rejected: status=%d body=%s", hostUpdate.Code, hostUpdate.Body.String())
	}
	if json.Unmarshal(hostUpdate.Body.Bytes(), &envelope) != nil || envelope.Data.ConfigVersion != 4 {
		t.Fatalf("safe config update version incorrect: %s", hostUpdate.Body.String())
	}
	claims, err := store.ClaimDeliveries(context.Background(), 1, time.Minute)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim in-flight old snapshot: claims=%d err=%v", len(claims), err)
	}
	var claimConfig map[string]json.RawMessage
	if json.Unmarshal(claims[0].Request.Config, &claimConfig) != nil || string(claimConfig["host"]) != `"smtp-old.example.test"` || string(claimConfig["region"]) != `"us-east-1"` || string(claimConfig["callback_url"]) != `"https://provider.example.test/callback/old"` {
		t.Fatalf("in-flight delivery did not retain configVersion %d snapshot", claims[0].Request.ConfigVersion)
	}
	passwordRotation := put("identity-password-rotation", contract.InstanceUpdate{ExpectedVersion: 4, Secrets: &contract.SecretPatch{Set: map[string]string{"password": "new-password"}}})
	if passwordRotation.Code != http.StatusOK {
		t.Fatalf("non-identity secret rotation rejected: status=%d body=%s", passwordRotation.Code, passwordRotation.Body.String())
	}
	var rotated contract.Envelope[contract.InstanceView]
	if json.Unmarshal(passwordRotation.Body.Bytes(), &rotated) != nil || rotated.Data.ConfigVersion != 5 {
		t.Fatalf("password rotation version incorrect: %s", passwordRotation.Body.String())
	}
	callbackNoOp := put("identity-secret-same", contract.InstanceUpdate{ExpectedVersion: 5, Secrets: &contract.SecretPatch{Set: map[string]string{"callback_url": "https://provider.example.test/callback/old"}}})
	if callbackNoOp.Code != http.StatusOK {
		t.Fatalf("same identity secret was rejected: status=%d body=%s", callbackNoOp.Code, callbackNoOp.Body.String())
	}
	if replay := put("identity-host-safe-change", hostPatch); replay.Code != http.StatusOK {
		t.Fatalf("old successful config update did not replay after later versions: status=%d body=%s", replay.Code, replay.Body.String())
	} else {
		var replayed contract.Envelope[contract.InstanceView]
		if json.Unmarshal(replay.Body.Bytes(), &replayed) != nil || replayed.Data.ConfigVersion != 4 {
			t.Fatalf("config update replay drifted from original version: %s", replay.Body.String())
		}
	}
	var rejectedCommandCount int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM notification_idempotency WHERE tenant_id=$1 AND operation='instance.update' AND idempotency_key IN ($2,$3,$4,$5)`, tenantID, "identity-region-change", "identity-from-change", "identity-secret-change", "identity-secret-clear").Scan(&rejectedCommandCount); err != nil || rejectedCommandCount != 0 {
		t.Fatalf("rejected identity mutations persisted commands: rows=%d err=%v", rejectedCommandCount, err)
	}
	if _, err := store.UpdateInstance(context.Background(), tenantID, instance.ID, "identity-store-bypass", strings.Repeat("f", 64), patch(`{"region":"ap-south-1"}`, 6)); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("direct storage identity bypass error=%v, want conflict", err)
	}
	crossTenant := makePluginGrantToken(jwtKey, "other-admin", "", "SYS_ADMIN")
	crossRequest := httptest.NewRequest(http.MethodPut, "/api/v2/platform/notification-tenants/identity-other-tenant"+"/notification-instances/"+instance.ID, bytes.NewBufferString(`{"expectedVersion":6,"configPatch":{"region":"ap-south-1"}}`))
	crossRequest.Header.Set("Authorization", "Bearer "+crossTenant)
	crossRequest.Header.Set("Idempotency-Key", "identity-cross-tenant")
	crossResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossResponse, crossRequest)
	if crossResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant identity update status=%d body=%s", crossResponse.Code, crossResponse.Body.String())
	}
	current, err := store.GetInstance(context.Background(), tenantID, instance.ID)
	if err != nil || current.ConfigVersion != 6 || string(current.Config["region"]) != `"us-east-1"` {
		t.Fatalf("identity guard changed persisted instance: view=%+v err=%v", current, err)
	}
}

func TestLegacyInstanceWithoutIdentityMetadataFreezesActualConfigAndSecretChanges(t *testing.T) {
	const jwtKey = "fixture-legacy-identity-jwt-key"
	t.Setenv("TP_JWT_KEY", jwtKey)
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	store, _ := newMigratedTestStore(t)
	const tenantID = "legacy-identity-tenant"
	registrationID := createIdentityGuardPlugin(t, store, tenantID, "identity-legacy-fixture", nil)
	instance := createIdentityGuardInstance(t, store, tenantID, registrationID, "legacy")
	handler := identityGuardHTTPHandler(t, store)
	bearer := makePluginGrantToken(jwtKey, "legacy-admin", "", "SYS_ADMIN")
	put := func(key string, update contract.InstanceUpdate) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(update)
		if err != nil {
			t.Fatalf("marshal legacy update: %v", err)
		}
		request := httptest.NewRequest(http.MethodPut, "/api/v2/platform/notification-tenants/"+tenantID+"/notification-instances/"+instance.ID, bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bearer)
		request.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	assertConflict := func(response *httptest.ResponseRecorder) {
		t.Helper()
		var domainError contract.DomainError
		if json.Unmarshal(response.Body.Bytes(), &domainError) != nil || response.Code != http.StatusConflict || domainError.Details.Reason != "identity_change_requires_new_instance" {
			t.Fatalf("legacy config mutation response=%d body=%s", response.Code, response.Body.String())
		}
	}
	assertConflict(put("legacy-region-change", contract.InstanceUpdate{ExpectedVersion: 1, ConfigPatch: json.RawMessage(`{"region":"eu-west-1"}`)}))
	assertConflict(put("legacy-password-change", contract.InstanceUpdate{ExpectedVersion: 1, Secrets: &contract.SecretPatch{Set: map[string]string{"password": "new-password"}}}))
	assertConflict(put("legacy-password-clear", contract.InstanceUpdate{ExpectedVersion: 1, Secrets: &contract.SecretPatch{Clear: []string{"password"}}}))
	noOp := put("legacy-no-op-config", contract.InstanceUpdate{ExpectedVersion: 1, ConfigPatch: json.RawMessage(`{"region":"us-east-1"}`), Secrets: &contract.SecretPatch{Set: map[string]string{"password": "old-password"}}})
	if noOp.Code != http.StatusOK {
		t.Fatalf("legacy same-value patch was rejected: status=%d body=%s", noOp.Code, noOp.Body.String())
	}
	name := "Renamed display only"
	nameUpdate := put("legacy-name-update", contract.InstanceUpdate{ExpectedVersion: 2, Name: &name})
	if nameUpdate.Code != http.StatusOK {
		t.Fatalf("legacy name-only update was rejected: status=%d body=%s", nameUpdate.Code, nameUpdate.Body.String())
	}
	_ = createIdentityGuardInstance(t, store, tenantID, registrationID, "legacy-new")
}
