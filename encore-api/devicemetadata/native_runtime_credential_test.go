package devicemetadata

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"encore.dev/beta/auth"
	"encore.dev/et"
	"github.com/google/uuid"

	"encore.app/l1-access/authn"
)

func TestVoucherForCredential(t *testing.T) {
	if v, ok := voucherForCredential("token", "", "s3cret"); !ok || v != `{"username":"s3cret"}` {
		t.Fatalf("token: %q %v", v, ok)
	}
	if v, ok := voucherForCredential("username_password", "dev-1", "pw"); !ok || v != `{"password":"pw","username":"dev-1"}` {
		t.Fatalf("basic: %q %v", v, ok)
	}
	if _, ok := voucherForCredential("username_password", "", "pw"); ok {
		t.Fatal("a username/password credential without a username cannot be sent")
	}
	if _, ok := voucherForCredential("x509", "", "pem"); ok {
		t.Fatal("certificates cannot be sent as a voucher")
	}
	a, b := unusableVoucher(), unusableVoucher()
	if a == b || !strings.Contains(a, `"username":"revoked-`) {
		t.Fatalf("unusable vouchers must be unique and unguessable: %q %q", a, b)
	}
}

func TestCredentialChangeIsPlannedFromStoredSecretsAndAppliedByReceipt(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "credential-dispatch-test"
	key := bytes.Repeat([]byte{0x5a}, 32)
	keyring, _ := json.Marshal(map[string]string{"key-v1": base64.StdEncoding.EncodeToString(key)})
	t.Setenv(credentialKeyringEnv, base64.StdEncoding.EncodeToString(keyring))
	t.Setenv(credentialActiveKeyEnv, "key-v1")

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := insertCredentialSchemaDevice(t, ctx, tx, tenantID)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM metadata_audit_events WHERE tenant_id=$1 AND resource_kind='device_credential'`, tenantID)
		_, _ = db.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2`, tenantID, subjectID)
		_, _ = db.Exec(ctx, `DELETE FROM device_credentials WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID)
		_, _ = db.Exec(ctx, `DELETE FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID)
		_, _ = db.Exec(ctx, `DELETE FROM metadata_operations WHERE tenant_id=$1 AND resource_kind='device' AND resource_id=$2`, tenantID, deviceID)
		_, _ = db.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID)
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeCredentialWriteIdentity()
	if err != nil {
		t.Fatal(err)
	}
	protector := testCredentialProtector(t)
	create := func(secret string) string {
		res, err := createNativeDeviceCredential(ctx, identity, deviceID, "cred-"+strings.ReplaceAll(uuid.NewString(), "-", ""), []byte(`{"kind":"token","secret":"`+secret+`"}`), "req-"+secret, protector)
		if err != nil {
			t.Fatal(err)
		}
		return decodeCredentialSecretResponse(t, res.Body).Credential.ID
	}
	activations := func() int {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2 AND kind=$3 AND desired->>'change'='activation'`,
			tenantID, deviceID, operationKindCredentialChange).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	applyReceipt := func(desired credentialChangeDesired) {
		applyTx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(desired)
		if err := applyCredentialChangeReceiptTx(ctx, applyTx, tenantID, deviceID, encoded); err != nil {
			applyTx.Rollback()
			t.Fatal(err)
		}
		if err := applyTx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// The first credential goes live through the runtime: it is queued for
	// activation, sent as the voucher, and becomes active when the receipt arrives.
	oldID := create("secret-old")
	if activations() != 1 {
		t.Fatalf("the first credential must queue one activation, got %d", activations())
	}
	activation := credentialChangeDesired{Change: "activation", CredentialID: oldID}
	if plan := planCredentialVoucher(ctx, tenantID, deviceID, activation); plan.FailureCode != "" || plan.Voucher != `{"username":"secret-old"}` {
		t.Fatalf("activation plan: %+v", plan)
	}
	applyReceipt(activation)
	// A second credential waits as the replacement; it queues no activation of its own.
	newID := create("secret-new")
	if activations() != 1 {
		t.Fatalf("a device with a live credential must not queue another activation, got %d", activations())
	}

	rotation := credentialChangeDesired{Change: "rotation", CredentialID: oldID, ReplacementCredentialID: newID}
	plan := planCredentialVoucher(ctx, tenantID, deviceID, rotation)
	if plan.FailureCode != "" || plan.Voucher != `{"username":"secret-new"}` {
		t.Fatalf("rotation plan: %+v", plan)
	}
	if missing := planCredentialVoucher(ctx, tenantID, deviceID, credentialChangeDesired{Change: "rotation", CredentialID: oldID, ReplacementCredentialID: uuid.NewString()}); missing.FailureCode != "CREDENTIAL_NOT_FOUND" {
		t.Fatalf("unknown replacement: %+v", missing)
	}

	// An applied receipt makes the replacement live and retires the old credential.
	applyReceipt(rotation)
	statuses := map[string]string{}
	rows, err := db.Query(ctx, `SELECT id::text,status FROM device_credentials WHERE tenant_id=$1 AND device_id=$2`, tenantID, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatal(err)
		}
		statuses[id] = status
	}
	rows.Close()
	if statuses[newID] != "active" || statuses[oldID] != "revoked" {
		t.Fatalf("after rotation receipt: %v", statuses)
	}

	// Revoking the live credential leaves nothing to log in with: an unusable voucher.
	revoke := planCredentialVoucher(ctx, tenantID, deviceID, credentialChangeDesired{Change: "revocation", CredentialID: newID})
	if revoke.FailureCode != "" || !strings.Contains(revoke.Voucher, `"username":"revoked-`) {
		t.Fatalf("revocation with no other credential: %+v", revoke)
	}
	// With another live credential, the device falls back to it.
	spareID := create("secret-spare")
	if _, err := db.Exec(ctx, `UPDATE device_credentials SET status='active',activated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, spareID); err != nil {
		t.Fatal(err)
	}
	fallback := planCredentialVoucher(ctx, tenantID, deviceID, credentialChangeDesired{Change: "revocation", CredentialID: newID})
	if fallback.FailureCode != "" || fallback.Voucher != `{"username":"secret-spare"}` {
		t.Fatalf("revocation with a spare credential: %+v", fallback)
	}
}
