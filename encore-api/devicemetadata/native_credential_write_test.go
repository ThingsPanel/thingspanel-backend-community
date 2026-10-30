package devicemetadata

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestDecodeNativeCreateCredentialAndValidateCertificate(t *testing.T) {
	for _, body := range []string{
		`{"kind":"token"}`,
		`{"kind":"token","secret":"imported"}`,
		`{"kind":"username_password","secret":null,"publicIdentifier":""}`,
	} {
		if _, err := decodeNativeCreateCredential([]byte(body)); err != nil {
			t.Errorf("valid credential request rejected: %v", err)
		}
	}
	pemText := testPublicCredentialCertificate(t)
	input, err := decodeNativeCreateCredential(mustJSON(t, map[string]any{"kind": "x509", "certificatePem": pemText}))
	if err != nil {
		t.Fatalf("valid certificate request rejected: %v", err)
	}
	if _, err := parseNativePublicCertificate(*input.CertificatePEM); err != nil {
		t.Fatalf("valid public certificate rejected: %v", err)
	}
	for _, body := range []string{
		`{"kind":"token","unknown":true}`,
		`{"kind":"token","kind":"x509"}`,
		`{"kind":"token","secret":""}`,
		`{"kind":"token","certificatePem":"bad"}`,
		`{"kind":"token","publicIdentifier":"\u0000"}`,
		`{"kind":"x509"}`,
		`{"kind":"x509","secret":"not-allowed","certificatePem":"bad"}`,
	} {
		if _, err := decodeNativeCreateCredential([]byte(body)); err == nil {
			t.Errorf("invalid credential request accepted: %s", body)
		}
	}
	badCertificate, err := decodeNativeCreateCredential([]byte(`{"kind":"x509","certificatePem":"not a certificate"}`))
	if err != nil {
		t.Fatalf("x509 request shape rejected before certificate parsing: %v", err)
	}
	if _, err := parseNativePublicCertificate(*badCertificate.CertificatePEM); err == nil {
		t.Fatal("malformed certificate accepted")
	}
	if _, err := parseNativePublicCertificate(pemText + "\n" + pemText); err == nil {
		t.Fatal("multiple certificates were accepted")
	}
	privateBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("synthetic private material")})
	if _, err := parseNativePublicCertificate(string(privateBlock)); err == nil {
		t.Fatal("private key PEM was accepted as a public certificate")
	}
	if _, err := parseNativePublicCertificate("ignored preamble\n" + pemText); err == nil {
		t.Fatal("certificate after PEM preamble was accepted")
	}
	if _, err := parseNativePublicCertificate(string(privateBlock) + "\n" + pemText); err == nil {
		t.Fatal("certificate after a private-key block was accepted")
	}
	oversized := `{"kind":"token","secret":"` + strings.Repeat("s", maxCredentialSecretBytes+1) + `"}`
	if _, err := decodeNativeCreateCredential([]byte(oversized)); err == nil {
		t.Fatal("oversized UTF-8 secret was accepted")
	}
}

func TestCreateNativeDeviceCredentialStoresOnlyProtectedMaterialAndReplaysSafely(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "credential-create-test"
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceID := insertCredentialSchemaDevice(t, ctx, tx, tenantID)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	key := "credential-create-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	path := "/api/v2/devices/" + deviceID + "/credentials"
	cleanup := func() {
		cleanupTx, err := db.Begin(ctx)
		if err != nil {
			t.Errorf("begin credential create cleanup: %v", err)
			return
		}
		defer cleanupTx.Rollback()
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name='createDeviceCredential' AND canonical_path=$3`, tenantID, subjectID, path); err != nil {
			t.Errorf("delete credential receipts: %v", err)
			return
		}
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM metadata_audit_events WHERE tenant_id=$1 AND resource_kind='device_credential' AND resource_id IN (SELECT id FROM device_credentials WHERE tenant_id=$1 AND device_id=$2)`, tenantID, deviceID); err != nil {
			t.Errorf("delete credential audit fixtures: %v", err)
			return
		}
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM device_credentials WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID); err != nil {
			t.Errorf("delete credential fixtures: %v", err)
			return
		}
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID); err != nil {
			t.Errorf("delete runtime outbox fixtures: %v", err)
			return
		}
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM metadata_operations WHERE tenant_id=$1 AND resource_kind='device' AND resource_id=$2`, tenantID, deviceID); err != nil {
			t.Errorf("delete operation fixtures: %v", err)
			return
		}
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID); err != nil {
			t.Errorf("delete device fixture: %v", err)
			return
		}
		if err := cleanupTx.Commit(); err != nil {
			t.Errorf("commit credential create cleanup: %v", err)
		}
	}
	t.Cleanup(cleanup)
	data := &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(subjectID), data)
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeCredentialWriteIdentity()
	if err != nil {
		t.Fatalf("admin credential identity rejected: %v", err)
	}
	protector := testCredentialProtector(t)
	const importedSecret = "credential-write-test-secret-never-print"
	body := []byte(`{"kind":"token","secret":"` + importedSecret + `"}`)
	first, err := createNativeDeviceCredential(ctx, identity, deviceID, key, body, "credential-create-first", protector)
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	firstSecret := decodeCredentialSecretResponse(t, first.Body)
	if first.Status != http.StatusCreated || first.Location != path+"/"+firstSecret.Credential.ID || firstSecret.Secret == nil || *firstSecret.Secret != importedSecret || firstSecret.Credential.Status != "pending" {
		t.Fatal("create response did not match the one-time credential contract")
	}
	replayed, err := createNativeDeviceCredential(ctx, identity, deviceID, key, body, "credential-create-replay", protector)
	if err != nil || !replayed.Replayed || string(replayed.Body) != string(first.Body) {
		t.Fatalf("same request replay did not return the encrypted receipt: replayed=%v err=%v", replayed != nil && replayed.Replayed, err)
	}
	changedBody := []byte(`{"kind":"token","secret":"different-synthetic-secret"}`)
	if _, err := createNativeDeviceCredential(ctx, identity, deviceID, key, changedBody, "credential-create-conflict", protector); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("changed request with same key error=%v, want AlreadyExists", err)
	}
	var response sql.NullString
	var responseCiphertext []byte
	var receiptRequestHash string
	if err := db.QueryRow(ctx, `SELECT response::text,response_ciphertext,request_hash FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name='createDeviceCredential' AND canonical_path=$3 AND idempotency_key=$4`, tenantID, subjectID, path, key).Scan(&response, &responseCiphertext, &receiptRequestHash); err != nil {
		t.Fatal("read protected receipt")
	}
	if response.Valid || len(responseCiphertext) == 0 || strings.Contains(string(responseCiphertext), importedSecret) || receiptRequestHash == string(body) {
		t.Fatal("idempotency receipt contains plaintext response or secret request material")
	}
	var storedCiphertext []byte
	var storedVerifier, auditText sql.NullString
	if err := db.QueryRow(ctx, `SELECT ciphertext,verifier FROM device_credentials WHERE tenant_id=$1 AND id=$2`, tenantID, firstSecret.Credential.ID).Scan(&storedCiphertext, &storedVerifier); err != nil {
		t.Fatal("read credential protected material")
	}
	if strings.Contains(string(storedCiphertext), importedSecret) || storedVerifier.Valid {
		t.Fatal("credential material was not stored as protected ciphertext")
	}
	if err := db.QueryRow(ctx, `SELECT redacted_changes::text FROM metadata_audit_events WHERE tenant_id=$1 AND resource_kind='device_credential' AND resource_id=$2`, tenantID, firstSecret.Credential.ID).Scan(&auditText); err != nil {
		t.Fatal("read redacted credential audit")
	}
	if strings.Contains(auditText.String, importedSecret) || strings.Contains(auditText.String, "secret") {
		t.Fatal("audit contains secret material")
	}

	generatedKey := key + "-generated"
	generated, err := createNativeDeviceCredential(ctx, identity, deviceID, generatedKey, []byte(`{"kind":"token"}`), "credential-create-generated", protector)
	if err != nil {
		t.Fatalf("create generated token: %v", err)
	}
	generatedSecret := decodeCredentialSecretResponse(t, generated.Body)
	if generatedSecret.Secret == nil || len(*generatedSecret.Secret) != 43 {
		t.Fatal("missing secret was not replaced with a generated 32-byte credential")
	}
	usernameKey := key + "-username"
	usernameBody := []byte(`{"kind":"username_password","secret":"username-secret-synthetic"}`)
	username, err := createNativeDeviceCredential(ctx, identity, deviceID, usernameKey, usernameBody, "credential-create-username", protector)
	if err != nil {
		t.Fatalf("create username/password credential: %v", err)
	}
	usernameSecret := decodeCredentialSecretResponse(t, username.Body)
	if usernameSecret.Credential.PublicIdentifier == nil || *usernameSecret.Credential.PublicIdentifier != "tp-"+usernameSecret.Credential.ID {
		t.Fatal("missing username was not assigned from the generated credential ID")
	}
	x509Key := key + "-x509"
	certificatePEM := testPublicCredentialCertificate(t)
	x509Body := mustJSON(t, map[string]any{"kind": "x509", "certificatePem": certificatePEM})
	x509Result, err := createNativeDeviceCredential(ctx, identity, deviceID, x509Key, x509Body, "credential-create-x509", protector)
	if err != nil {
		t.Fatalf("create public certificate credential: %v", err)
	}
	x509Secret := decodeCredentialSecretResponse(t, x509Result.Body)
	if x509Secret.Secret != nil || len(x509Secret.Credential.Fingerprint) != 64 {
		t.Fatal("x509 response returned material or omitted its public fingerprint")
	}

	// The HTTP entry checks the active authorization again on every call. Revoke
	// the role after the receipt exists; a same-key replay must be denied.
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_VIEWER"}})
	if _, err := currentNativeCredentialWriteIdentity(); errs.Code(err) != errs.PermissionDenied {
		t.Fatalf("revoked credential-write role error=%v, want PermissionDenied", err)
	}
	if _, err := createNativeDeviceCredential(ctx, identity, deviceID, key, body, "credential-create-revoked-replay", protector); errs.Code(err) != errs.PermissionDenied {
		t.Fatalf("replay after permission revocation error=%v, want PermissionDenied", err)
	}
}

func TestCreateNativeDeviceCredentialRequiresConfiguredKeyring(t *testing.T) {
	t.Setenv(credentialKeyringEnv, "")
	t.Setenv(credentialActiveKeyEnv, "")
	if _, err := NewCredentialSecretProtectorFromEnv(); !errors.Is(err, ErrCredentialKeyUnavailable) {
		t.Fatal("credential creation keyring absence was not rejected")
	}
}

func decodeCredentialSecretResponse(t *testing.T, raw []byte) nativeCredentialSecret {
	t.Helper()
	var response nativeCredentialSecretResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal("decode credential response")
	}
	return response.Data
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal("marshal credential test body")
	}
	return encoded
}

func testPublicCredentialCertificate(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate synthetic certificate key")
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "credential-test.invalid"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create synthetic certificate")
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
