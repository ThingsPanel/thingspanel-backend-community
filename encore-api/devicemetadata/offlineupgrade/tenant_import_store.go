package offlineupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"encore.app/internal/tenantidentity"
	"gorm.io/gorm"
)

// Matches devicemetadata.ImportedCommunityRuntimeProviderID. This standalone
// offline command must not import/initialize an Encore service to use its DB.
const importedCommunityRuntimeProviderID = "760c2298-8b97-51f1-bd71-2737d6278f21"

type TenantImportReceipt struct {
	State                   string `json:"state"`
	TenantID                string `json:"tenant_id"`
	InputSHA256             string `json:"input_sha256"`
	SourceArchiveSHA256     string `json:"source_archive_sha256"`
	TargetArchiveSHA256     string `json:"target_archive_sha256"`
	ResolutionsSHA256       string `json:"resolutions_sha256"`
	Groups                  int    `json:"groups"`
	Devices                 int    `json:"devices"`
	SourceTemplates         int    `json:"source_templates"`
	Members                 int    `json:"members"`
	Gateways                int    `json:"gateways"`
	RuntimeOwnershipChanged bool   `json:"runtime_ownership_changed"`
	Scope                   string `json:"scope"`
}

// ApplyTenantImportPG writes one exact tenant closure atomically. Existing
// native rows are never updated, even on retry after users subsequently edit
// imported metadata. The receipt proves this batch was committed; it does not
// claim later edits still equal their historical source values.
func ApplyTenantImportPG(ctx context.Context, db *gorm.DB, in TenantImportInput) (TenantImportReceipt, error) {
	p, e := PlanTenantImport(in)
	if e != nil {
		return TenantImportReceipt{}, e
	}
	r := TenantImportReceipt{State: "imported", TenantID: p.TenantID, InputSHA256: p.Digest, SourceArchiveSHA256: in.SourceArchiveSHA256, Groups: len(p.Groups), Devices: len(p.Devices), SourceTemplates: len(in.Templates), Members: len(p.Members), Gateways: len(p.Gateways), Scope: "tenant metadata and memberships; source runtime, credentials and access templates preserved"}
	r.TargetArchiveSHA256 = in.TargetArchiveSHA256
	r.ResolutionsSHA256 = in.ResolutionsSHA256
	if db == nil || db.Dialector.Name() != "postgres" {
		return TenantImportReceipt{}, fmt.Errorf("PostgreSQL owner required")
	}
	e = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var database string
		if e := tx.Raw("SELECT current_database()").Scan(&database).Error; e != nil {
			return e
		}
		if database != in.TargetDatabase {
			return fmt.Errorf("target database differs from verified input")
		}
		for _, q := range []string{"SET LOCAL lock_timeout='10s'", "SET LOCAL statement_timeout='60s'"} {
			if e := tx.Exec(q).Error; e != nil {
				return e
			}
		}
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 41112026))", "tenant-metadata-import:"+p.TenantID).Error; e != nil {
			return e
		}
		var schema []struct {
			Version int64
			Dirty   bool
		}
		if e := tx.Table("schema_migrations").Find(&schema).Error; e != nil {
			return e
		}
		if len(schema) != 1 || schema[0].Version != 41 || schema[0].Dirty {
			return fmt.Errorf("clean native schema v41 required")
		}
		eventID := tenantidentity.CanonicalIdentity("tenant-import", p.TenantID+"/"+in.SourceArchiveSHA256)
		var previous auditEvent
		err := tx.Table("metadata_audit_events").Where("id=?", eventID).Take(&previous).Error
		if err == nil {
			var saved TenantImportReceipt
			if previous.TenantID != p.TenantID || previous.OperationName != "offline.tenant.import" || previous.Outcome != "succeeded" || json.Unmarshal([]byte(previous.RedactedChanges), &saved) != nil || saved.InputSHA256 != p.Digest {
				return fmt.Errorf("existing import receipt differs; refusing replay")
			}
			r = saved
			r.State = "already-imported-no-changes"
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Every insert is strict. Primary/unique keys and tenant-composite FKs
		// reject concurrent collisions and cross-tenant links without overwrites.
		for _, d := range p.Devices {
			row := d.Row
			if e := tx.Table("devices").Create(&row).Error; e != nil {
				return fmt.Errorf("device import rejected: %w", e)
			}
		}
		for _, o := range p.Origins {
			if e := tx.Exec(`INSERT INTO device_access_bindings
			(tenant_id,device_id,device_parameters,configuration_generation,runtime_provider_id,runtime_namespace,runtime_external_id,service_instance_id,created_at,updated_at,updated_by)
			VALUES (?::uuid,?::uuid,'{}'::jsonb,1,?::uuid,'community',?,?,?, ?,?)`, p.TenantID, o.DeviceID, importedCommunityRuntimeProviderID, o.SourceDeviceID, o.SourceServiceAccessID, in.RecordedAt, in.RecordedAt, p.Actor).Error; e != nil {
				return fmt.Errorf("runtime reference import rejected: %w", e)
			}
			if e := tx.Table("imported_device_origins").Create(&o).Error; e != nil {
				return fmt.Errorf("source provenance import rejected: %w", e)
			}
		}
		for _, g := range p.Groups {
			row := g.Row
			if e := tx.Table("device_groups").Create(&row).Error; e != nil {
				return fmt.Errorf("group import rejected: %w", e)
			}
		}
		if len(p.Members) > 0 {
			if e := tx.Table("device_group_members").Create(&p.Members).Error; e != nil {
				return fmt.Errorf("membership import rejected: %w", e)
			}
		}
		if len(p.Gateways) > 0 {
			if e := tx.Table("gateway_memberships").Create(&p.Gateways).Error; e != nil {
				return fmt.Errorf("gateway import rejected: %w", e)
			}
		}
		changes, _ := json.Marshal(r)
		event := auditEvent{ID: eventID, TenantID: p.TenantID, CreatedAt: in.RecordedAt, CreatedBy: p.Actor, RequestID: "tenant-import:" + p.Digest, OperationName: "offline.tenant.import", ResourceKind: "device_group", ResourceID: p.Groups[0].Row.ID, AfterVersion: 1, Outcome: "succeeded", RedactedChanges: string(changes)}
		if e := tx.Table("metadata_audit_events").Create(&event).Error; e != nil {
			return e
		}
		return nil
	})
	if e != nil {
		return TenantImportReceipt{}, e
	}
	return r, nil
}
