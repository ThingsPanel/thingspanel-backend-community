package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func lifecycleKey(label string) string {
	return "lifecycle-" + label + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func lifecycleDeviceVersion(t *testing.T, ctx context.Context, tenantID, deviceID string) int64 {
	t.Helper()
	var version int64
	if err := db.QueryRow(ctx, `SELECT resource_version FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID).Scan(&version); err != nil {
		t.Fatalf("read device version: %v", err)
	}
	return version
}

func lifecycleBody(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	return mustJSON(t, fields)
}

func TestCredentialRotationRevocationAndRenumberAreDurableAndSafe(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "credential-lifecycle-test"
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceID := insertCredentialSchemaDevice(t, ctx, tx, tenantID)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		type cleanup struct {
			sql  string
			args []any
		}
		for _, statement := range []cleanup{
			{`DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2`, []any{tenantID, subjectID}},
			{`DELETE FROM metadata_audit_events WHERE tenant_id=$1 AND (resource_id=$2::uuid OR resource_id IN (SELECT id FROM device_credentials WHERE tenant_id=$1 AND device_id=$2::uuid))`, []any{tenantID, deviceID}},
			{`DELETE FROM device_credentials WHERE tenant_id=$1 AND device_id=$2::uuid`, []any{tenantID, deviceID}},
			{`DELETE FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2::uuid`, []any{tenantID, deviceID}},
			{`DELETE FROM metadata_operations WHERE tenant_id=$1 AND resource_id=$2::uuid`, []any{tenantID, deviceID}},
			{`DELETE FROM devices WHERE tenant_id=$1 AND id=$2::uuid`, []any{tenantID, deviceID}},
		} {
			if _, err := db.Exec(ctx, statement.sql, statement.args...); err != nil {
				t.Errorf("cleanup %q: %v", statement.sql, err)
			}
		}
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeCredentialWriteIdentity()
	if err != nil {
		t.Fatalf("admin identity rejected: %v", err)
	}
	protector := testCredentialProtector(t)
	newCredential := func(label string) string {
		result, err := createNativeDeviceCredential(ctx, identity, deviceID, lifecycleKey(label), []byte(`{"kind":"token"}`), "lifecycle-create-"+label, protector)
		if err != nil {
			t.Fatalf("create %s credential: %v", label, err)
		}
		return decodeCredentialSecretResponse(t, result.Body).Credential.ID
	}
	active, replacement, spare := newCredential("active"), newCredential("replacement"), newCredential("spare")
	if _, err := db.Exec(ctx, `UPDATE device_credentials SET status='active',activated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, active); err != nil {
		t.Fatalf("activate fixture credential: %v", err)
	}
	version := lifecycleDeviceVersion(t, ctx, tenantID, deviceID)

	// Rotation: only an active credential, with a pending unused replacement, at the current version.
	rotate := func(key, credential string, fields map[string]any) (*metadataWriteResponse, error) {
		return rotateNativeDeviceCredential(ctx, identity, deviceID, credential, key, lifecycleBody(t, fields), "lifecycle-rotate")
	}
	rotateFields := map[string]any{"replacementCredentialId": replacement, "overlapSeconds": 30, "expectedDeviceVersion": version}
	if _, err := rotate(lifecycleKey("stale"), active, map[string]any{"replacementCredentialId": replacement, "overlapSeconds": 0, "expectedDeviceVersion": version + 1}); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("stale device version error=%v, want FailedPrecondition", err)
	}
	if _, err := rotate(lifecycleKey("pending"), spare, map[string]any{"replacementCredentialId": replacement, "overlapSeconds": 0, "expectedDeviceVersion": version}); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("rotating a pending credential error=%v, want FailedPrecondition", err)
	}
	rotateKey := lifecycleKey("rotate")
	first, err := rotate(rotateKey, active, rotateFields)
	if err != nil || first.Status != 202 {
		t.Fatalf("rotate: status=%v err=%v", first, err)
	}
	replayed, err := rotate(rotateKey, active, rotateFields)
	if err != nil || !replayed.Replayed || string(replayed.Body) != string(first.Body) {
		t.Fatalf("same-key rotation replay did not return the stored response: %v", err)
	}
	var supersedes, operationID string
	if err := db.QueryRow(ctx, `SELECT supersedes_credential_id::text,rotation_operation_id::text FROM device_credentials WHERE tenant_id=$1 AND id=$2`, tenantID, replacement).Scan(&supersedes, &operationID); err != nil || supersedes != active {
		t.Fatalf("replacement was not linked to the rotated credential: %v supersedes=%q", err, supersedes)
	}
	var opState string
	if err := db.QueryRow(ctx, `SELECT state FROM metadata_operations WHERE tenant_id=$1 AND id=$2`, tenantID, operationID).Scan(&opState); err != nil || opState != "pending" {
		t.Fatalf("rotation operation must stay pending until the runtime reports: state=%q err=%v", opState, err)
	}
	var outboxCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2::uuid AND kind='credential_change' AND state='pending' AND desired->>'change'='rotation'`, tenantID, deviceID).Scan(&outboxCount); err != nil || outboxCount != 1 {
		t.Fatalf("exactly one pending credential_change rotation outbox row expected, got %d (%v)", outboxCount, err)
	}
	if _, err := rotate(lifecycleKey("again"), active, map[string]any{"replacementCredentialId": spare, "overlapSeconds": 0, "expectedDeviceVersion": version}); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("second rotation of the same credential error=%v, want FailedPrecondition", err)
	}
	if _, err := rotate(lifecycleKey("self"), active, map[string]any{"replacementCredentialId": active, "overlapSeconds": 0, "expectedDeviceVersion": version}); err == nil {
		t.Fatal("rotating a credential onto itself was accepted")
	}

	// Revocation: immediate and one-way; the reason never reaches audit.
	const reason = "leaked-in-a-ticket-synthetic-reason"
	revoke := func(key, credential string) (*metadataWriteResponse, error) {
		return revokeNativeDeviceCredential(ctx, identity, deviceID, credential, key, lifecycleBody(t, map[string]any{"expectedDeviceVersion": version, "reason": reason}), "lifecycle-revoke")
	}
	revokeKey := lifecycleKey("revoke")
	if result, err := revoke(revokeKey, spare); err != nil || result.Status != 202 {
		t.Fatalf("revoke: %v %v", result, err)
	}
	var status string
	var revokedBy *string
	if err := db.QueryRow(ctx, `SELECT status,revoked_by FROM device_credentials WHERE tenant_id=$1 AND id=$2`, tenantID, spare).Scan(&status, &revokedBy); err != nil || status != "revoked" || revokedBy == nil || *revokedBy != subjectID {
		t.Fatalf("credential not revoked by the caller: status=%q err=%v", status, err)
	}
	if _, err := revoke(lifecycleKey("revoke-again"), spare); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("revoking twice error=%v, want FailedPrecondition", err)
	}
	if _, err := revoke(lifecycleKey("missing"), uuid.NewString()); errs.Code(err) != errs.NotFound {
		t.Fatalf("revoking an unknown credential error=%v, want NotFound", err)
	}
	var auditText string
	if err := db.QueryRow(ctx, `SELECT string_agg(redacted_changes::text,' ') FROM metadata_audit_events WHERE tenant_id=$1 AND resource_kind='device_credential' AND resource_id IN ($2,$3)`, tenantID, spare, active).Scan(&auditText); err != nil || strings.Contains(auditText, reason) {
		t.Fatalf("audit must not carry the revocation reason: %v", err)
	}

	// Renumbering moves only the business number, once per version, never onto a taken number.
	renumber := func(key, number string, expected int64) (*metadataWriteResponse, error) {
		return renumberNativeDevice(ctx, nativeDeviceWriteIdentity{nativeDeviceReadIdentity: identity.nativeDeviceReadIdentity, subjectID: subjectID}, deviceID, key,
			lifecycleBody(t, map[string]any{"deviceNumber": number, "expectedDeviceVersion": expected, "reason": "relabel"}), "lifecycle-renumber")
	}
	current := lifecycleDeviceVersion(t, ctx, tenantID, deviceID)
	newNumber := "renumbered-" + uuid.NewString()
	if _, err := renumber(lifecycleKey("renumber-stale"), newNumber, current+5); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("stale renumber error=%v, want FailedPrecondition", err)
	}
	if result, err := renumber(lifecycleKey("renumber"), newNumber, current); err != nil || result.Status != 202 {
		t.Fatalf("renumber: %v %v", result, err)
	}
	var stored string
	if err := db.QueryRow(ctx, `SELECT device_number FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID).Scan(&stored); err != nil || stored != newNumber {
		t.Fatalf("device number not updated: %q %v", stored, err)
	}
	if got := lifecycleDeviceVersion(t, ctx, tenantID, deviceID); got != current+1 {
		t.Fatalf("version = %d, want %d", got, current+1)
	}
	if _, err := renumber(lifecycleKey("renumber-same"), newNumber, current+1); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("renumber to the current number error=%v, want FailedPrecondition", err)
	}
	var renumberOutbox json.RawMessage
	if err := db.QueryRow(ctx, `SELECT desired FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2::uuid AND kind='device_renumber'`, tenantID, deviceID).Scan(&renumberOutbox); err != nil || !strings.Contains(string(renumberOutbox), newNumber) {
		t.Fatalf("renumber outbox row missing or wrong: %v", err)
	}
}
