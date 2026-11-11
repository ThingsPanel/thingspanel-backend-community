package offlineupgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"encore.app/devicemetadata/credentialsecret"
	"encore.app/internal/tenantidentity"
	"gorm.io/gorm"
)

type credentialRow struct {
	ID                     string
	TenantID               string
	DeviceID               *string
	TemplateID             *string
	Purpose                string
	Kind                   string
	ProtectionMode         string
	Status                 string
	PublicIdentifier       *string
	Verifier               *string
	FingerprintKeyID       string
	Fingerprint            []byte
	Ciphertext             []byte
	KeyID                  string
	CipherSuite            string
	Nonce                  []byte
	PublicCertificate      *string
	RotationOperationID    *string
	SupersedesCredentialID *string
	CreatedAt              time.Time `gorm:"autoCreateTime:false"`
	CreatedBy              string
	UpdatedAt              time.Time `gorm:"autoUpdateTime:false"`
	UpdatedBy              string
	ResourceVersion        int64
	ActivatedAt            *time.Time
	RevokedAt              *time.Time
	RevokedBy              *string
	RevocationReason       *string
	ExpiresAt              *time.Time
}
type CredentialReceipt struct {
	State                string         `json:"state"`
	SourceArchiveSHA256  string         `json:"source_archive_sha256"`
	TargetDatabase       string         `json:"target_database"`
	SourceDevices        int            `json:"source_devices"`
	ConvertedCredentials int            `json:"converted_credentials"`
	QuarantinedDevices   int            `json:"quarantined_devices"`
	Pending              map[string]int `json:"pending_reasons"`
	MigrationComplete    bool           `json:"migration_complete"`
}

// ApplyCredentialsPG is confined to a verified isolated native database. Source
// secrets are encrypted with the same implementation used by the live service;
// replay decrypts and compares all protected contents instead of replacing them.
func ApplyCredentialsPG(ctx context.Context, db *gorm.DB, in CredentialInput, protector *credentialsecret.CredentialSecretProtector) (CredentialReceipt, error) {
	plan, err := PlanCredentials(in)
	if err != nil {
		return CredentialReceipt{}, err
	}
	if protector == nil || db.Dialector.Name() != "postgres" {
		return CredentialReceipt{}, fmt.Errorf("credential protector and PostgreSQL rehearsal required")
	}
	receipt := CredentialReceipt{SourceArchiveSHA256: in.SourceArchiveSHA256, TargetDatabase: in.TargetDatabase, SourceDevices: len(in.Devices), ConvertedCredentials: plan.Selected, QuarantinedDevices: plan.Quarantined, Pending: plan.Pending}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var name string
		if err := tx.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
			return err
		}
		if name != in.TargetDatabase {
			return fmt.Errorf("target database does not match verified rehearsal")
		}
		if err := tx.Exec("SET LOCAL lock_timeout='10s'; SET LOCAL statement_timeout='120s'").Error; err != nil {
			return err
		}
		if err := tx.Exec("LOCK TABLE devices IN SHARE MODE; LOCK TABLE device_credentials,metadata_audit_events IN EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var schema []struct {
			Version int64
			Dirty   bool
		}
		if err := tx.Table("schema_migrations").Find(&schema).Error; err != nil {
			return err
		}
		if len(schema) != 1 || schema[0].Version != 40 || schema[0].Dirty {
			return fmt.Errorf("verified native schema v40 required")
		}
		var owners []struct {
			ID         string
			TenantID   string
			DeviceKind string
		}
		if err := tx.Table("devices").Select("id,tenant_id,device_kind").Where("deleted_at IS NULL").Find(&owners).Error; err != nil {
			return err
		}
		ownerMap := map[string]string{}
		for _, o := range owners {
			if o.DeviceKind == "standalone" || o.DeviceKind == "gateway" {
				ownerMap[o.ID] = o.TenantID
			}
		}
		for _, row := range plan.rows {
			if ownerMap[row.DeviceID] != row.TenantID {
				return fmt.Errorf("credential target owner differs from source")
			}
		}
		var before []credentialRow
		if err := tx.Table("device_credentials").Where("purpose = ?", "device_access").Order("id").Find(&before).Error; err != nil {
			return err
		}
		audit := make([]auditEvent, 0, len(plan.rows))
		for _, p := range plan.rows {
			changes, _ := json.Marshal(map[string]any{"sourceID": p.SourceID, "sourceArchiveSHA256": in.SourceArchiveSHA256, "targetArchiveSHA256": in.TargetArchiveSHA256, "quarantineSHA256": in.QuarantineSHA256, "kind": p.Kind, "scope": "existing credential registration; access/runtime migration pending"})
			audit = append(audit, auditEvent{ID: tenantidentity.CanonicalIdentity("upgrade-credential-event", in.SourceArchiveSHA256+"/"+p.SourceID), TenantID: p.TenantID, CreatedAt: in.RecordedAt, CreatedBy: plan.Actor, RequestID: "credential:" + in.SourceArchiveSHA256, OperationName: "offline.device.credential.convert", ResourceKind: "device", ResourceID: p.DeviceID, AfterVersion: 1, Outcome: "succeeded", RedactedChanges: string(changes)})
		}
		if len(before) == 0 {
			for start := 0; start < len(plan.rows); start += 250 {
				end := min(start+250, len(plan.rows))
				batch := make([]credentialRow, 0, end-start)
				for _, p := range plan.rows[start:end] {
					sealed, err := protector.Seal(p.aad(), p.Secret)
					if err != nil {
						return fmt.Errorf("could not protect existing credential")
					}
					fp, err := protector.Fingerprint(p.aad(), p.Secret)
					if err != nil {
						return fmt.Errorf("could not fingerprint existing credential")
					}
					deviceID := p.DeviceID
					at := in.RecordedAt
					batch = append(batch, credentialRow{ID: p.ID, TenantID: p.TenantID, DeviceID: &deviceID, Purpose: "device_access", Kind: p.Kind, ProtectionMode: "encrypted", Status: "active", PublicIdentifier: p.PublicIdentifier, FingerprintKeyID: fp.KeyID, Fingerprint: fp.Fingerprint, Ciphertext: sealed.Ciphertext, KeyID: sealed.KeyID, CipherSuite: sealed.CipherSuite, Nonce: sealed.Nonce, CreatedAt: at, UpdatedAt: at, ActivatedAt: &at, CreatedBy: plan.Actor, UpdatedBy: plan.Actor, ResourceVersion: 1})
				}
				if err := tx.Table("device_credentials").Create(&batch).Error; err != nil {
					return err
				}
				events := audit[start:end]
				if err := tx.Table("metadata_audit_events").Create(&events).Error; err != nil {
					return err
				}
			}
			receipt.State = "converted-and-verified-rehearsal-credentials"
		} else {
			receipt.State = "verified-existing-rehearsal-credentials"
		}
		var after []credentialRow
		if err := tx.Table("device_credentials").Where("purpose = ?", "device_access").Order("id").Find(&after).Error; err != nil {
			return err
		}
		if len(after) != len(plan.rows) {
			return fmt.Errorf("credential conversion coverage differs")
		}
		for i, p := range plan.rows {
			row := after[i]
			if row.ID != p.ID || row.TenantID != p.TenantID || row.DeviceID == nil || *row.DeviceID != p.DeviceID || row.TemplateID != nil || row.Purpose != "device_access" || row.Kind != p.Kind || row.ProtectionMode != "encrypted" || row.Status != "active" || !reflect.DeepEqual(row.PublicIdentifier, p.PublicIdentifier) || row.Verifier != nil || row.PublicCertificate != nil || row.RotationOperationID != nil || row.SupersedesCredentialID != nil || row.RevokedAt != nil || row.RevokedBy != nil || row.RevocationReason != nil || row.ExpiresAt != nil || row.ResourceVersion != 1 || row.CreatedBy != plan.Actor || row.UpdatedBy != plan.Actor || !row.CreatedAt.Equal(in.RecordedAt) || !row.UpdatedAt.Equal(in.RecordedAt) || row.ActivatedAt == nil || !row.ActivatedAt.Equal(in.RecordedAt) {
				return fmt.Errorf("stored credential owner or lifecycle differs")
			}
			plaintext, err := protector.Open(p.aad(), credentialsecret.ProtectedCredential{Ciphertext: row.Ciphertext, KeyID: row.KeyID, CipherSuite: row.CipherSuite, Nonce: row.Nonce})
			if err != nil || !bytes.Equal(plaintext, p.Secret) {
				return fmt.Errorf("stored credential material differs")
			}
			fp, err := protector.Fingerprint(p.aad(), p.Secret)
			if err != nil || row.FingerprintKeyID != fp.KeyID || !bytes.Equal(row.Fingerprint, fp.Fingerprint) {
				return fmt.Errorf("stored credential fingerprint differs")
			}
		}
		var events []auditEvent
		if err := tx.Table("metadata_audit_events").Where("operation_name = ? AND request_id = ?", "offline.device.credential.convert", "credential:"+in.SourceArchiveSHA256).Find(&events).Error; err != nil {
			return err
		}
		if len(events) != len(audit) {
			return fmt.Errorf("credential provenance incomplete")
		}
		byID := map[string]auditEvent{}
		for _, e := range events {
			byID[e.ID] = e
		}
		for _, expected := range audit {
			got, ok := byID[expected.ID]
			if !ok || !got.CreatedAt.Equal(expected.CreatedAt) || !sameJSON(got.RedactedChanges, expected.RedactedChanges) {
				return fmt.Errorf("credential provenance differs")
			}
			got.CreatedAt = expected.CreatedAt
			got.RedactedChanges = expected.RedactedChanges
			if !reflect.DeepEqual(got, expected) {
				return fmt.Errorf("credential audit scope differs")
			}
		}
		return nil
	})
	if err != nil {
		return CredentialReceipt{}, err
	}
	return receipt, nil
}
