package offlineupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"encore.app/internal/tenantidentity"
	"gorm.io/gorm"
)

type DeviceIdentityReceipt struct {
	State                    string `json:"state"`
	SourceArchiveSHA256      string `json:"source_archive_sha256"`
	TargetDatabase           string `json:"target_database"`
	SourceDevices            int    `json:"source_devices"`
	QuarantinedDevices       int    `json:"quarantined_devices"`
	ConvertedIdentities      int    `json:"converted_identities"`
	PendingAccessConversions int    `json:"pending_access_conversions"`
	RowsSHA256               string `json:"rows_sha256"`
	MigrationComplete        bool   `json:"migration_complete"`
}

// ApplyDeviceIdentitiesPG stages identity only, into an isolated empty owner.
// No access binding or credential is fabricated. Missing bindings explicitly
// keep the deployment preflight closed until the other conversions finish.
func ApplyDeviceIdentitiesPG(ctx context.Context, db *gorm.DB, input DeviceIdentityInput, progress ...func(string, int)) (DeviceIdentityReceipt, error) {
	step := func(stage string, rows int) {
		for _, report := range progress {
			if report != nil {
				report(stage, rows)
			}
		}
	}
	step("validate-source", 0)
	p, err := PlanDeviceIdentities(input)
	if err != nil {
		return DeviceIdentityReceipt{}, err
	}
	if db.Dialector.Name() != "postgres" {
		return DeviceIdentityReceipt{}, fmt.Errorf("PostgreSQL rehearsal required")
	}
	step("source-validated", len(p.Devices))
	r := DeviceIdentityReceipt{SourceArchiveSHA256: input.SourceArchiveSHA256, TargetDatabase: input.TargetDatabase,
		SourceDevices: len(input.Devices), QuarantinedDevices: len(input.ExcludedDevices), ConvertedIdentities: len(p.Devices), PendingAccessConversions: len(p.Devices)}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var database string
		if e := tx.Raw("SELECT current_database()").Scan(&database).Error; e != nil {
			return e
		}
		if database != input.TargetDatabase {
			return fmt.Errorf("target database does not match verified rehearsal")
		}
		if e := tx.Exec("SET LOCAL lock_timeout='10s'").Error; e != nil {
			return e
		}
		if e := tx.Exec("SET LOCAL statement_timeout='120s'").Error; e != nil {
			return e
		}
		if e := tx.Exec("LOCK TABLE devices, device_access_bindings, metadata_audit_events IN EXCLUSIVE MODE").Error; e != nil {
			return e
		}
		var schema []struct {
			Version int64
			Dirty   bool
		}
		if e := tx.Table("schema_migrations").Find(&schema).Error; e != nil {
			return e
		}
		if len(schema) != 1 || schema[0].Version != 40 || schema[0].Dirty {
			return fmt.Errorf("verified native schema v40 required")
		}
		var bindings int64
		if e := tx.Table("device_access_bindings").Count(&bindings).Error; e != nil {
			return e
		}
		if bindings != 0 {
			return fmt.Errorf("access conversion already started; identity stage must not be replayed")
		}
		var before []DeviceIdentity
		if e := tx.Table("devices").Order("id").Find(&before).Error; e != nil {
			return e
		}
		step("read-existing-target", len(before))
		expected := make([]DeviceIdentity, 0, len(p.Devices))
		events := make([]auditEvent, 0, len(p.Devices))
		for _, d := range p.Devices {
			expected = append(expected, d.Row)
			changes, _ := json.Marshal(map[string]any{"sourceArchiveSHA256": input.SourceArchiveSHA256,
				"sourceRowSHA256": d.SourceRowSHA256, "quarantineSHA256": input.QuarantineSHA256,
				"targetArchiveSHA256": input.TargetArchiveSHA256, "sourceID": d.SourceID,
				"nameFromNumber": d.NameFromNumber, "scope": "identity-only; access and runtime conversion pending"})
			events = append(events, auditEvent{ID: tenantidentity.CanonicalIdentity("upgrade-device-identity-event", input.SourceArchiveSHA256+"/"+d.SourceID),
				TenantID: d.Row.TenantID, CreatedAt: time.Now().UTC(), CreatedBy: p.Actor,
				RequestID: "device-identity:" + input.SourceArchiveSHA256, OperationName: "offline.device.identity.convert", ResourceKind: "device",
				ResourceID: d.Row.ID, AfterVersion: 1, Outcome: "succeeded", RedactedChanges: string(changes)})
		}
		if len(before) > 0 {
			if !sameDeviceIdentities(before, expected) {
				return fmt.Errorf("target device identities differ; refusing overwrite")
			}
			var stored []auditEvent
			if e := tx.Table("metadata_audit_events").Where("operation_name = ? AND request_id = ?", "offline.device.identity.convert", "device-identity:"+input.SourceArchiveSHA256).Find(&stored).Error; e != nil {
				return e
			}
			if len(stored) != len(events) {
				return fmt.Errorf("device conversion provenance incomplete")
			}
			byID := map[string]auditEvent{}
			for _, e := range stored {
				byID[e.ID] = e
			}
			for _, e := range events {
				old, ok := byID[e.ID]
				if !ok {
					return fmt.Errorf("device conversion audit absent")
				}
				old.CreatedAt = e.CreatedAt
				if !sameJSON(old.RedactedChanges, e.RedactedChanges) {
					return fmt.Errorf("device conversion provenance differs")
				}
				old.RedactedChanges = e.RedactedChanges
				if !reflect.DeepEqual(old, e) {
					return fmt.Errorf("device conversion audit identity differs")
				}
			}
			r.State = "verified-existing-rehearsal-identities"
		} else {
			if len(expected) > 0 {
				step("insert-device-identities", 0)
				for start := 0; start < len(expected); start += 250 {
					end := min(start+250, len(expected))
					batch := expected[start:end]
					if e := tx.Table("devices").Create(&batch).Error; e != nil {
						return e
					}
					step("insert-device-identities", end)
				}
				step("insert-device-provenance", 0)
				for start := 0; start < len(events); start += 250 {
					end := min(start+250, len(events))
					batch := events[start:end]
					if e := tx.Table("metadata_audit_events").Create(&batch).Error; e != nil {
						return e
					}
					step("insert-device-provenance", end)
				}
			}
			r.State = "converted-and-verified-rehearsal-identities"
		}
		var after []DeviceIdentity
		step("reconcile-target", 0)
		if e := tx.Table("devices").Order("id").Find(&after).Error; e != nil {
			return e
		}
		if !sameDeviceIdentities(after, expected) {
			return fmt.Errorf("post-insert full device identity reconciliation failed")
		}
		step("reconciled-target", len(after))
		data, _ := json.Marshal(expected)
		sum := sha256.Sum256(data)
		r.RowsSHA256 = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return DeviceIdentityReceipt{}, err
	}
	return r, nil
}

func sameJSON(a, b string) bool {
	var x, y any
	dx := json.NewDecoder(strings.NewReader(a))
	dx.UseNumber()
	dy := json.NewDecoder(strings.NewReader(b))
	dy.UseNumber()
	return dx.Decode(&x) == nil && dy.Decode(&y) == nil && reflect.DeepEqual(x, y)
}

func sameDeviceIdentities(a, b []DeviceIdentity) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if !x.CreatedAt.Equal(y.CreatedAt) || !x.UpdatedAt.Equal(y.UpdatedAt) {
			return false
		}
		x.CreatedAt = y.CreatedAt
		x.UpdatedAt = y.UpdatedAt
		if (x.ActivatedAt == nil) != (y.ActivatedAt == nil) {
			return false
		}
		if x.ActivatedAt != nil {
			if !x.ActivatedAt.Equal(*y.ActivatedAt) {
				return false
			}
			x.ActivatedAt = y.ActivatedAt
		}
		if !sameJSON(x.Labels, y.Labels) || !sameJSON(x.Tags, y.Tags) || !sameJSON(x.CustomFields, y.CustomFields) {
			return false
		}
		x.Labels = y.Labels
		x.Tags = y.Tags
		x.CustomFields = y.CustomFields
		if (x.Location == nil) != (y.Location == nil) {
			return false
		}
		if x.Location != nil {
			if !sameJSON(*x.Location, *y.Location) {
				return false
			}
			x.Location = y.Location
		}
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}
