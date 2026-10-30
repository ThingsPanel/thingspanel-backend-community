package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestNativeDeviceCredentialSummaryReadPagesAndEnforcesScope(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceA := insertCredentialSchemaDevice(t, ctx, tx, tenantA)
	deviceA2 := insertCredentialSchemaDevice(t, ctx, tx, tenantA)
	deviceB := insertCredentialSchemaDevice(t, ctx, tx, tenantB)
	secretMarker := "synthetic-ciphertext-marker-must-not-appear"
	firstID, secondID, foreignID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, row := range []credentialSchemaRow{
		{id: firstID, tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "encrypted", status: "pending", publicIdentifier: "public-a", ciphertext: []byte(secretMarker), keyID: "fixture-key", cipherSuite: "AES-256-GCM", nonce: make([]byte, 12), fingerprint: []byte(strings.Repeat("f", 32))},
		{id: secondID, tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "username_password", mode: "verifier", status: "revoked", verifier: "synthetic-verifier-must-not-appear", fingerprint: []byte(strings.Repeat("a", 32)), revokedAt: timePointer(time.Now().UTC().Add(time.Second)), revokedBy: "credential-read-test", revocationReason: "synthetic fixture"},
		{id: foreignID, tenantID: tenantB, deviceID: deviceB, purpose: "device_access", kind: "token", mode: "encrypted", status: "pending", ciphertext: []byte(secretMarker), keyID: "fixture-key", cipherSuite: "AES-256-GCM", nonce: make([]byte, 12), fingerprint: []byte(strings.Repeat("b", 32))},
	} {
		if err := execCredentialSchemaRow(ctx, tx, row); err != nil {
			t.Fatalf("insert summary fixture: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE device_credentials SET created_at=CASE id WHEN $1 THEN '2026-01-01T00:00:00Z'::timestamptz WHEN $2 THEN '2026-01-02T00:00:00Z'::timestamptz END WHERE id IN ($1,$2)`, firstID, secondID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupTx, err := db.Begin(ctx)
		if err != nil {
			t.Errorf("begin credential summary cleanup: %v", err)
			return
		}
		defer cleanupTx.Rollback()
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM device_credentials WHERE id IN ($1,$2,$3)`, firstID, secondID, foreignID); err != nil {
			t.Errorf("delete credential fixtures: %v", err)
			return
		}
		if _, err := cleanupTx.Exec(ctx, `DELETE FROM devices WHERE tenant_id IN ($1,$2) AND id IN ($3,$4,$5)`, tenantA, tenantB, deviceA, deviceA2, deviceB); err != nil {
			t.Errorf("delete device fixtures: %v", err)
			return
		}
		if err := cleanupTx.Commit(); err != nil {
			t.Errorf("commit credential summary cleanup: %v", err)
		}
	})

	identity := nativeDeviceReadIdentity{tenantID: tenantA}
	page1, err := readNativeDeviceCredentialSummaries(ctx, identity, deviceA, 1, 1, "credential-summary-p1")
	if err != nil {
		t.Fatalf("read first page: %v", err)
	}
	if page1.Data.Total != 2 || page1.Data.Page != 1 || page1.Data.PageSize != 1 || len(page1.Data.Items) != 1 || page1.Data.Items[0].ID != secondID {
		t.Fatalf("unexpected first summary page: %+v", page1.Data)
	}
	page2, err := readNativeDeviceCredentialSummaries(ctx, identity, deviceA, 2, 1, "credential-summary-p2")
	if err != nil || page2.Data.Total != 2 || len(page2.Data.Items) != 1 || page2.Data.Items[0].ID != firstID {
		t.Fatalf("unexpected second summary page=%+v err=%v", page2, err)
	}
	encoded, err := json.Marshal(page1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretMarker) || strings.Contains(string(encoded), "synthetic-verifier-must-not-appear") || strings.Contains(string(encoded), "ciphertext") || strings.Contains(string(encoded), "verifier") || strings.Contains(string(encoded), "nonce") {
		t.Fatalf("credential material present in summary JSON: %s", encoded)
	}
	if page1.Data.Items[0].Fingerprint == "" || len(page1.Data.Items[0].Fingerprint) != 64 {
		t.Fatalf("fingerprint is not exposed as 32-byte hex: %+v", page1.Data.Items[0])
	}
	if _, err := readNativeDeviceCredentialSummaries(ctx, identity, deviceB, 1, 20, "credential-summary-cross-tenant"); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant device error=%v, want NotFound", err)
	}
	scoped := identity
	scoped.deviceIDs = []string{deviceA2}
	if _, err := readNativeDeviceCredentialSummaries(ctx, scoped, deviceA, 1, 20, "credential-summary-out-of-scope"); errs.Code(err) != errs.NotFound {
		t.Fatalf("out-of-scope device error=%v, want NotFound", err)
	}
	empty, err := readNativeDeviceCredentialSummaries(ctx, identity, deviceA2, 1, 20, "credential-summary-empty")
	if err != nil || empty.Data.Items == nil || len(empty.Data.Items) != 0 || empty.Data.Total != 0 {
		t.Fatalf("empty credential page must serialize as []: page=%+v err=%v", empty, err)
	}
	emptyJSON, err := json.Marshal(empty.Data)
	if err != nil || !strings.Contains(string(emptyJSON), `"items":[]`) {
		t.Fatalf("empty page JSON=%s err=%v", emptyJSON, err)
	}
}

func TestNativeCredentialSummaryQueryOnlyAllowsSinglePageFields(t *testing.T) {
	for _, raw := range []string{"page=", "page=1&page=2", "unknown=x", "pageSize=101"} {
		req := httptest.NewRequest("GET", "/api/v2/devices/00000000-0000-4000-8000-000000000001/credentials?"+raw, nil)
		if _, _, err := parseNativeCredentialPagination(req); err == nil {
			t.Errorf("invalid credential query %q accepted", raw)
		}
	}
	page, pageSize, err := parseNativeCredentialPagination(httptest.NewRequest("GET", "/api/v2/devices/00000000-0000-4000-8000-000000000001/credentials", nil))
	if err != nil || page != 1 || pageSize != 20 {
		t.Fatalf("default credential pagination page=%d size=%d err=%v", page, pageSize, err)
	}
}
