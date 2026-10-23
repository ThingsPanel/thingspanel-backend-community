package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type provisionDirectoryFixture map[string]bool

func (d provisionDirectoryFixture) Exists(_ context.Context, tenantID string) (bool, error) {
	return d[tenantID], nil
}

func TestProvisionSourceProjectionRequiresLivePlatformAndTenantAndPersistsHashOnly(t *testing.T) {
	const jwtKey = "fixture-provision-jwt-key"
	const deployment = "fixture-provision-deployment"
	t.Setenv("TP_AUTH_MODE", "legacy-jwt")
	t.Setenv("TP_JWT_KEY", jwtKey)
	t.Setenv("NOTIFICATION_SOURCE_DEPLOYMENT_ID", deployment)
	store, db := newMigratedTestStore(t)
	privacyPath := filepath.Join(t.TempDir(), "infra.json")
	if err := os.WriteFile(privacyPath, []byte(`{"log_config":"info"}`), 0600); err != nil {
		t.Fatal("write privacy fixture")
	}
	env := []byte("ENCORE_INFRA_CONFIG_PATH=" + privacyPath + "\x00NOTIFICATION_ENCORE_INFRA_CONFIG_PATH=" + privacyPath + "\x00")
	privacy, err := newPrivacyGuard(func() ([]byte, error) { return env, nil }, "linux")
	if err != nil {
		t.Fatal("construct privacy fixture")
	}
	pluginHTTP, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, AllowedOrigins: []string{"http://127.0.0.1:19587"}})
	if err != nil {
		t.Fatal("construct plugin client")
	}
	service, err := NewManagementService(store, store, store, store, store, validationSessionFixture{}, pluginHTTP, newTestSnapshotProtector(), privacy, ManagementOptions{
		TestMode: true, AllowHTTPTestOrigin: true, TenantDirectory: provisionDirectoryFixture{"tenant-provision": true},
	})
	if err != nil {
		t.Fatal("construct management service")
	}
	now := time.Now()
	admin := fixtureJWT(jwtKey, `{"id":"platform-provision","tenant_id":"","authority":"SYS_ADMIN","exp":`+strconv.FormatInt(now.Add(time.Hour).Unix(), 10)+`}`)
	tenant := fixtureJWT(jwtKey, `{"id":"tenant-provision-user","tenant_id":"tenant-provision","authority":"TENANT_ADMIN","exp":`+strconv.FormatInt(now.Add(time.Hour).Unix(), 10)+`}`)
	result, err := service.ProvisionSourceProjectionCredentials(context.Background(), admin, tenant, deployment, 2*time.Hour)
	if err != nil {
		t.Fatalf("provision credential pair: %v", err)
	}
	if result.DeploymentID != deployment || len(result.AllowedTenantIDs) != 1 || result.AllowedTenantIDs[0] != "tenant-provision" || result.SourceCredential == result.ProjectionCredential || !result.ExpiresAt.After(now.Add(time.Hour)) || !result.ExpiresAt.Before(now.Add(3*time.Hour)) {
		t.Fatal("provisioned pair has unexpected scope, separation, or expiry")
	}
	for purpose, token := range map[CredentialPurpose]string{
		CredentialPurposeSource:     result.SourceCredential,
		CredentialPurposeProjection: result.ProjectionCredential,
	} {
		hash := sha256.Sum256([]byte(token))
		stored, err := store.FindNotificationCredential(context.Background(), purpose, hex.EncodeToString(hash[:]))
		if err != nil || stored.DeploymentID != deployment || len(stored.AllowedTenantIDs) != 1 || stored.AllowedTenantIDs[0] != "tenant-provision" {
			t.Fatalf("lookup %s credential failed: %v", purpose, err)
		}
		var hashStored, tokenStored bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM notification_credentials WHERE token_hash=$1), EXISTS(SELECT 1 FROM notification_credentials WHERE token_hash=$2)`, hex.EncodeToString(hash[:]), token).Scan(&hashStored, &tokenStored); err != nil || !hashStored || tokenStored {
			t.Fatal("credential hash-only persistence check failed")
		}
	}
	if _, err := service.ProvisionSourceProjectionCredentials(context.Background(), admin, tenant, deployment, 2*time.Hour); err == nil {
		t.Fatal("duplicate active pair was provisioned")
	}
	if _, err := service.ProvisionSourceProjectionCredentials(context.Background(), admin, tenant, deployment, 9*time.Hour); err == nil {
		t.Fatal("overlong credential lifetime was accepted")
	}
	if _, err := service.ProvisionSourceProjectionCredentials(context.Background(), admin, tenant, "another-deployment", 2*time.Hour); err == nil {
		t.Fatal("unconfigured deployment was accepted")
	}

	renewed, err := service.RenewSourceProjectionCredentials(context.Background(), admin, tenant, result, 4*time.Hour)
	if err != nil || !renewed.ExpiresAt.After(result.ExpiresAt) || renewed.SourceCredential != result.SourceCredential || renewed.ProjectionCredential != result.ProjectionCredential {
		t.Fatalf("renewal failed to preserve tokens and extend validity: %v", err)
	}
	if _, err := service.RenewSourceProjectionCredentials(context.Background(), tenant, tenant, result, time.Hour); err == nil {
		t.Fatal("renewal accepted non-platform identity")
	}
	if _, err := service.RenewSourceProjectionCredentials(context.Background(), admin, tenant, result, 9*time.Hour); err == nil {
		t.Fatal("renewal exceeded maximum lifetime")
	}
	wrongScope := result
	wrongScope.AllowedTenantIDs = []string{"another-tenant"}
	if _, err := service.RenewSourceProjectionCredentials(context.Background(), admin, tenant, wrongScope, time.Hour); err == nil {
		t.Fatal("renewal changed tenant scope")
	}
	tampered := result
	tampered.ProjectionCredential = "invalid-projection-token-with-at-least-32-characters"
	if _, err := service.RenewSourceProjectionCredentials(context.Background(), admin, tenant, tampered, 6*time.Hour); err == nil {
		t.Fatal("renewal accepted unmatched pair")
	}
	var expiry time.Time
	if err := db.QueryRow(`SELECT expires_at FROM notification_credentials WHERE id=$1`, result.SourceCredentialID).Scan(&expiry); err != nil || expiry.Sub(renewed.ExpiresAt).Abs() > time.Millisecond {
		t.Fatal("failed pair renewal partially changed source credential")
	}

	if _, err := db.Exec(`UPDATE notification_credentials SET expires_at=$3 WHERE id IN ($1,$2)`, result.SourceCredentialID, result.ProjectionCredentialID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RenewSourceProjectionCredentials(context.Background(), admin, tenant, result, time.Hour); err == nil {
		t.Fatal("renewal revived expired credentials")
	}
	if err := store.RevokeSourceProjectionCredentialPair(context.Background(), result.SourceCredentialID, result.ProjectionCredentialID); err != nil {
		t.Fatalf("atomic rollback of pair: %v", err)
	}

	if _, err := service.RenewSourceProjectionCredentials(context.Background(), admin, tenant, result, time.Hour); err == nil {
		t.Fatal("renewal revived revoked credentials")
	}
	if _, err := service.ProvisionSourceProjectionCredentials(context.Background(), admin, tenant, deployment, 2*time.Hour); err != nil {
		t.Fatalf("provision after atomic rollback: %v", err)
	}
	if _, err := service.ProvisionSourceProjectionCredentials(context.Background(), admin, fixtureJWT(jwtKey, `{"id":"wrong-tenant","tenant_id":"other-tenant","authority":"TENANT_ADMIN","exp":`+strconv.FormatInt(now.Add(time.Hour).Unix(), 10)+`}`), deployment, 2*time.Hour); err == nil {
		t.Fatal("tenant absent from legacy directory was accepted")
	}
}
