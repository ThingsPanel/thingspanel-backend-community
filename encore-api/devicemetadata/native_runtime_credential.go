package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// enqueueFirstCredentialActivationTx schedules the activation of a newly
// created credential when the device has no live credential and no activation
// already waiting. It writes the operation and the outbox row in the caller's
// transaction.
func enqueueFirstCredentialActivationTx(ctx context.Context, tx *sqldb.Tx, tenantID, subjectID, deviceID, credentialID, requestID string) error {
	var blocked bool
	if err := tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM device_credentials WHERE tenant_id=$1 AND device_id=$2 AND purpose='device_access' AND status='active' AND id<>$3)
			OR EXISTS (SELECT 1 FROM device_runtime_outbox WHERE tenant_id=$1 AND device_id=$2 AND kind=$4
				AND state IN ('pending','failed') AND desired->>'change'='activation')`,
		tenantID, deviceID, credentialID, operationKindCredentialChange).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return nil
	}
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT resource_version FROM devices WHERE tenant_id=$1 AND id=$2`, tenantID, deviceID).Scan(&generation); err != nil {
		return err
	}
	digest := operationRequestDigest("POST", "/api/v2/devices/"+deviceID+"/credentials", "", []byte(credentialID))
	operationID, err := createDeviceOperationTx(ctx, tx, tenantID, subjectID, operationKindCredentialChange, deviceID,
		"createDeviceCredential", requestID, digest, map[string]any{"awaiting": "runtime-application", "change": "activation"})
	if err != nil {
		return err
	}
	return enqueueDeviceRuntimeTx(ctx, tx, tenantID, deviceID, operationID, operationKindCredentialChange, generation,
		map[string]any{"deviceId": deviceID, "change": "activation", "credentialId": credentialID})
}

// Community keeps one voucher per device, so a credential change is applied by
// replacing that voucher: the old credential stops logging in at once and the
// "overlap" window of a rotation cannot be honoured. Only credentials whose
// secret we can decrypt (token and username/password, encrypted) can be sent;
// certificates and verifier-only credentials are reported as unsupported.

type credentialChangeDesired struct {
	Change                  string `json:"change"`
	CredentialID            string `json:"credentialId"`
	ReplacementCredentialID string `json:"replacementCredentialId"`
}

type credentialVoucherPlan struct {
	Voucher     string
	FailureCode string
	Retryable   bool
}

func voucherForCredential(kind, publicIdentifier, secret string) (string, bool) {
	var payload map[string]string
	switch kind {
	case "token":
		payload = map[string]string{"username": secret}
	case "username_password":
		if publicIdentifier == "" {
			return "", false
		}
		payload = map[string]string{"username": publicIdentifier, "password": secret}
	default:
		return "", false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

// unusableVoucher is what a device is left with when its last credential is
// revoked: a value nobody knows, so nothing can log in as the device.
func unusableVoucher() string {
	encoded, _ := json.Marshal(map[string]string{"username": "revoked-" + uuid.NewString()})
	return string(encoded)
}

func loadCredentialVoucher(ctx context.Context, tx *sqldb.Tx, protector *CredentialSecretProtector, tenantID, deviceID, whereSQL string, args ...any) (string, bool, error) {
	var (
		id, kind, protection string
		publicIdentifier     sql.NullString
		protected            ProtectedCredential
	)
	query := `SELECT id::text,kind,protection_mode,public_identifier,ciphertext,key_id,cipher_suite,nonce FROM device_credentials
		WHERE tenant_id=$1 AND device_id=$2 AND purpose='device_access' AND ` + whereSQL
	err := tx.QueryRow(ctx, query, append([]any{tenantID, deviceID}, args...)...).
		Scan(&id, &kind, &protection, &publicIdentifier, &protected.Ciphertext, &protected.KeyID, &protected.CipherSuite, &protected.Nonce)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqldb.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if protection != "encrypted" {
		return "", false, fmt.Errorf("credential %s is not stored in a recoverable form", id)
	}
	secret, err := protector.Open(CredentialAAD{TenantID: tenantID, OwnerKind: CredentialOwnerDevice, OwnerID: deviceID,
		CredentialID: id, Purpose: CredentialPurposeDeviceAccess, Use: CredentialUseMaterial}, protected)
	if err != nil {
		return "", false, err
	}
	voucher, ok := voucherForCredential(kind, publicIdentifier.String, string(secret))
	if !ok {
		return "", false, fmt.Errorf("credential %s has an unsupported kind", id)
	}
	return voucher, true, nil
}

// planCredentialVoucher decides what voucher Community must be given for one
// credential_change outbox row.
func planCredentialVoucher(ctx context.Context, tenantID, deviceID string, desired credentialChangeDesired) credentialVoucherPlan {
	protector, err := NewCredentialSecretProtectorFromEnv()
	if err != nil {
		return credentialVoucherPlan{FailureCode: "CREDENTIAL_PROTECTION_UNAVAILABLE", Retryable: true}
	}
	tx, err := beginTenantScopedSQLTx(ctx, tenantID, true)
	if err != nil {
		return credentialVoucherPlan{FailureCode: "RUNTIME_UNAVAILABLE", Retryable: true}
	}
	defer tx.Rollback()
	switch desired.Change {
	case "rotation":
		voucher, found, err := loadCredentialVoucher(ctx, tx, protector, tenantID, deviceID, `id=$3 AND status='pending'`, desired.ReplacementCredentialID)
		if err != nil {
			return credentialVoucherPlan{FailureCode: "CREDENTIAL_UNSUPPORTED"}
		}
		if !found {
			return credentialVoucherPlan{FailureCode: "CREDENTIAL_NOT_FOUND"}
		}
		return credentialVoucherPlan{Voucher: voucher}
	case "activation":
		voucher, found, err := loadCredentialVoucher(ctx, tx, protector, tenantID, deviceID, `id=$3 AND status='pending'`, desired.CredentialID)
		if err != nil {
			return credentialVoucherPlan{FailureCode: "CREDENTIAL_UNSUPPORTED"}
		}
		if !found {
			return credentialVoucherPlan{FailureCode: "CREDENTIAL_NOT_FOUND"}
		}
		return credentialVoucherPlan{Voucher: voucher}
	case "revocation":
		// Another live credential keeps the device reachable; with none left the
		// device gets a voucher nobody holds.
		voucher, found, err := loadCredentialVoucher(ctx, tx, protector, tenantID, deviceID,
			`id<>$3 AND status='active' ORDER BY activated_at DESC,id DESC LIMIT 1`, desired.CredentialID)
		if err != nil {
			return credentialVoucherPlan{FailureCode: "CREDENTIAL_UNSUPPORTED"}
		}
		if !found {
			voucher = unusableVoucher()
		}
		return credentialVoucherPlan{Voucher: voucher}
	default:
		return credentialVoucherPlan{FailureCode: "DESIRED_INVALID"}
	}
}

// applyCredentialChangeReceiptTx records, in the receipt's transaction, what an
// applied credential change did: a rotation makes the replacement the live
// credential and retires the one it replaced. A revocation was already recorded
// when it was requested.
func applyCredentialChangeReceiptTx(ctx context.Context, tx *sqldb.Tx, tenantID, deviceID string, desiredJSON []byte) error {
	var desired credentialChangeDesired
	if err := json.Unmarshal(desiredJSON, &desired); err != nil {
		return err
	}
	if desired.Change == "activation" {
		_, err := tx.Exec(ctx, `UPDATE device_credentials
			SET status='active',activated_at=NOW(),updated_by='runtime',updated_at=NOW(),resource_version=resource_version+1
			WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND status='pending'`, tenantID, deviceID, desired.CredentialID)
		return err
	}
	if desired.Change != "rotation" {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE device_credentials
		SET status='active',activated_at=NOW(),updated_by='runtime',updated_at=NOW(),resource_version=resource_version+1
		WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND status='pending'`, tenantID, deviceID, desired.ReplacementCredentialID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE device_credentials
		SET status='revoked',revoked_at=NOW(),revoked_by='runtime',revocation_reason='rotated',
			updated_by='runtime',updated_at=NOW(),resource_version=resource_version+1
		WHERE tenant_id=$1 AND device_id=$2 AND id=$3 AND status<>'revoked'`, tenantID, deviceID, desired.CredentialID)
	return err
}
