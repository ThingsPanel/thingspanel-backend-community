package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

const managedFixtureIdentity = "installation-fixture-identity-not-a-real-secret"

func TestManagedSourceLifecycleAndScope(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	createFixtureInstance(t, store, "tenant-a")
	plugins, err := store.ListPlugins(ctx, "tenant-a")
	if err != nil || len(plugins) != 1 {
		t.Fatal("fixture plugin unavailable")
	}
	grantFixturePlugin(t, store, "tenant-b", plugins[0].ID)
	store.managedSourceDeployment = "deployment-a"
	store.managedSourceFingerprint = serviceFingerprint(managedFixtureIdentity)
	service := &ManagementService{credentials: store, tenantDirectory: pluginGrantDirectoryFixture{"tenant-a": true, "tenant-b": true}, privacy: crossProcessPrivacyFixture(t), clock: time.Now}
	handler := &managedSourceHandler{store: store, token: managedFixtureIdentity, deployment: "deployment-a", directory: pluginGrantDirectoryFixture{"tenant-a": true, "tenant-b": true, "no-grant": true}, ready: func() error { return nil }}
	issue := func(identity, deployment, tenant, purpose string) (int, managedSourceLease) {
		body, _ := json.Marshal(map[string]string{"tenantId": tenant, "purpose": purpose})
		req := httptest.NewRequest("POST", "/internal/notification/source-credentials", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+identity)
		req.Header.Set("X-Notification-Deployment", deployment)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential response is cacheable")
		}
		var envelope struct {
			Data managedSourceLease `json:"data"`
		}
		json.Unmarshal(response.Body.Bytes(), &envelope)
		return response.Code, envelope.Data
	}
	for _, tc := range []struct {
		identity, deployment, tenant, purpose string
		status                                int
	}{
		{"user-jwt", "deployment-a", "tenant-a", "source", 401},
		{managedFixtureIdentity, "another-installation", "tenant-a", "source", 401},
		{managedFixtureIdentity, "deployment-a", "unknown", "source", 403},
		{managedFixtureIdentity, "deployment-a", "no-grant", "source", 403},
		{managedFixtureIdentity, "deployment-a", "tenant-a", "receipt", 400},
	} {
		status, _ := issue(tc.identity, tc.deployment, tc.tenant, tc.purpose)
		if status != tc.status {
			t.Fatalf("scope %s/%s status=%d want=%d", tc.tenant, tc.purpose, status, tc.status)
		}
	}
	status, a := issue(managedFixtureIdentity, "deployment-a", "tenant-a", "source")
	if status != 200 || a.TenantID != "tenant-a" || a.Purpose != CredentialPurposeSource || a.ExpiresAt.Sub(time.Now()) > 15*time.Minute {
		t.Fatal("invalid managed lease")
	}
	_, b := issue(managedFixtureIdentity, "deployment-a", "tenant-b", "source")
	_, projection := issue(managedFixtureIdentity, "deployment-a", "tenant-a", "projection")
	principal, err := service.VerifySource(ctx, a.Credential)
	if err != nil || len(principal.TenantIDs) != 1 || principal.TenantIDs[0] != "tenant-a" {
		t.Fatalf("source scope: %v", err)
	}
	if _, err := service.VerifySource(ctx, projection.Credential); err == nil {
		t.Fatal("projection credential accepted as source")
	}
	if _, err := service.VerifySourceProjection(ctx, a.Credential); err == nil {
		t.Fatal("source credential accepted as projection")
	}
	if _, err := service.VerifySourceProjection(ctx, projection.Credential); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(a.Credential))
	record, err := store.FindNotificationCredential(ctx, CredentialPurposeSource, hex.EncodeToString(hash[:]))
	if err != nil || record.TokenHash == a.Credential || record.TokenHash != hex.EncodeToString(hash[:]) {
		t.Fatal("credential not stored as hash")
	}
	var registration string
	if err := db.QueryRowContext(ctx, `SELECT registration_id FROM notification_managed_credentials WHERE credential_id=$1`, record.ID).Scan(&registration); err != nil {
		t.Fatal(err)
	}
	update := contract.PluginGrantUpdate{PluginRegistrationID: registration, TenantID: "tenant-a", Enabled: false, ExpectedVersion: 1}
	if _, _, err := store.SetPluginGrant(ctx, "tenant-a", registration, "revoke-managed-0001", strings.Repeat("1", 64), update); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifySource(ctx, a.Credential); err == nil {
		t.Fatal("revoked grant left cached credential active")
	}
	if status, _ := issue(managedFixtureIdentity, "deployment-a", "tenant-a", "source"); status != 403 {
		t.Fatal("revoked tenant can acquire credentials")
	}
	if _, err := service.VerifySource(ctx, b.Credential); err != nil {
		t.Fatal("revoking tenant-a affected tenant-b")
	}
	update.Enabled = true
	update.ExpectedVersion = 2
	if _, _, err := store.SetPluginGrant(ctx, "tenant-a", registration, "enable-managed-0001", strings.Repeat("2", 64), update); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifySource(ctx, a.Credential); err == nil {
		t.Fatal("reenabling grant revived previous credential")
	}
	_, renewed := issue(managedFixtureIdentity, "deployment-a", "tenant-a", "source")
	if renewed.Credential == a.Credential {
		t.Fatal("new authorization reused old credential")
	}
	if _, err := service.VerifySource(ctx, renewed.Credential); err != nil {
		t.Fatal(err)
	}
	// Restarting a caller reacquires a lease; expiration never depends on an operator timer.
	service.clock = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if _, err := service.VerifySource(ctx, renewed.Credential); err == nil {
		t.Fatal("expired lease accepted")
	}
	service.clock = time.Now
	store.managedSourceFingerprint = serviceFingerprint("rotated-machine-identity-00000000000000")
	if _, err := service.VerifySource(ctx, renewed.Credential); err == nil {
		t.Fatal("identity rotation did not invalidate leases")
	}
}

func TestManagedSourceDoesNotAcceptManuallyInsertedCredential(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	store.managedSourceDeployment = "deployment-a"
	store.managedSourceFingerprint = serviceFingerprint(managedFixtureIdentity)
	record, token, err := newDeploymentCredential(CredentialPurposeSource, "deployment-a", []string{"tenant-a"}, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNotificationCredential(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	service := &ManagementService{credentials: store, tenantDirectory: pluginGrantDirectoryFixture{"tenant-a": true, "tenant-b": true}, privacy: crossProcessPrivacyFixture(t), clock: time.Now}
	if _, err := service.VerifySource(context.Background(), token); err == nil {
		t.Fatal("old manual credential accepted")
	}
}
