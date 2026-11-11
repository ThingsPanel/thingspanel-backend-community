package offlineupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	"encore.app/internal/tenantidentity"
	"gorm.io/gorm"
)

type GroupReceipt struct {
	State               string `json:"state"`
	SourceArchiveSHA256 string `json:"source_archive_sha256"`
	TargetDatabase      string `json:"target_database"`
	SourceGroups        int    `json:"source_groups"`
	QuarantinedGroups   int    `json:"quarantined_groups"`
	ConvertedGroups     int    `json:"converted_groups"`
	RowsSHA256          string `json:"rows_sha256"`
	MigrationComplete   bool   `json:"migration_complete"` // group conversion alone is never cutover acceptance
}

type auditEvent struct {
	ID              string
	TenantID        string
	CreatedAt       time.Time `gorm:"autoCreateTime:false"`
	CreatedBy       string
	RequestID       string
	OperationName   string
	ResourceKind    string
	ResourceID      string
	AfterVersion    int64
	Outcome         string
	RedactedChanges string `gorm:"type:jsonb"`
}

// ApplyGroupsPG is intentionally rehearsal-only. It inserts into an empty
// isolated native owner, or verifies an exact retry. It never upserts source
// data over an existing resource. PG locking and introspection stay here in the
// owning repository; ordinary reads/inserts use GORM.
func ApplyGroupsPG(ctx context.Context, db *gorm.DB, input GroupInput) (GroupReceipt, error) {
	p, err := PlanGroups(input)
	if err != nil {
		return GroupReceipt{}, err
	}
	if db.Dialector.Name() != "postgres" {
		return GroupReceipt{}, fmt.Errorf("PostgreSQL rehearsal required")
	}
	r := GroupReceipt{SourceArchiveSHA256: input.SourceArchiveSHA256, TargetDatabase: input.TargetDatabase,
		SourceGroups: len(input.Groups), QuarantinedGroups: len(input.ExcludedGroups), ConvertedGroups: len(p.Groups)}
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
		if e := tx.Exec("SET LOCAL statement_timeout='60s'").Error; e != nil {
			return e
		}
		if e := tx.Exec("LOCK TABLE device_groups, metadata_audit_events IN EXCLUSIVE MODE").Error; e != nil {
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
		var before []Group
		if e := tx.Table("device_groups").Order("id").Find(&before).Error; e != nil {
			return e
		}
		expected := make([]Group, 0, len(p.Groups))
		events := make([]auditEvent, 0, len(p.Groups))
		for _, g := range p.Groups {
			expected = append(expected, g.Row)
			changes, _ := json.Marshal(map[string]string{
				"sourceArchiveSHA256": input.SourceArchiveSHA256, "sourceRowSHA256": g.SourceRowSHA256,
				"quarantineSHA256": input.QuarantineSHA256, "targetArchiveSHA256": input.TargetArchiveSHA256,
				"sourceID": g.SourceID, "scope": "group-only; full migration incomplete",
			})
			events = append(events, auditEvent{
				ID:       tenantidentity.CanonicalIdentity("upgrade-group-event", input.SourceArchiveSHA256+"/"+g.SourceID),
				TenantID: g.Row.TenantID, CreatedAt: time.Now().UTC(), CreatedBy: p.Actor,
				RequestID:     "group-conversion:" + input.SourceArchiveSHA256,
				OperationName: "offline.group.convert", ResourceKind: "device_group", ResourceID: g.Row.ID,
				AfterVersion: 1, Outcome: "succeeded", RedactedChanges: string(changes),
			})
		}
		sort.Slice(expected, func(i, j int) bool { return expected[i].ID < expected[j].ID })
		if len(before) != 0 {
			if !sameGroups(before, expected) {
				return fmt.Errorf("target groups differ; refusing to overwrite")
			}
			for _, e := range events {
				var stored auditEvent
				if err := tx.Table("metadata_audit_events").Where("id = ?", e.ID).Take(&stored).Error; err != nil {
					return fmt.Errorf("missing conversion provenance")
				}
				stored.CreatedAt = e.CreatedAt
				// PostgreSQL JSONB canonical spacing is not the source encoding.
				var got, want map[string]string
				if json.Unmarshal([]byte(stored.RedactedChanges), &got) != nil || json.Unmarshal([]byte(e.RedactedChanges), &want) != nil || !reflect.DeepEqual(got, want) {
					return fmt.Errorf("conversion provenance differs")
				}
				stored.RedactedChanges = e.RedactedChanges
				if !reflect.DeepEqual(stored, e) {
					return fmt.Errorf("conversion audit identity differs")
				}
			}
			r.State = "verified-existing-rehearsal-groups"
		} else {
			for _, g := range p.Groups { // topological order satisfies same-tenant parent FK
				row := g.Row
				if e := tx.Table("device_groups").Create(&row).Error; e != nil {
					return e
				}
			}
			if len(events) > 0 {
				if e := tx.Table("metadata_audit_events").CreateInBatches(events, 100).Error; e != nil {
					return e
				}
			}
			r.State = "converted-and-verified-rehearsal-groups"
		}
		var after []Group
		if e := tx.Table("device_groups").Order("id").Find(&after).Error; e != nil {
			return e
		}
		if !sameGroups(after, expected) {
			return fmt.Errorf("post-insert full group reconciliation failed")
		}
		data, _ := json.Marshal(expected)
		sum := sha256.Sum256(data)
		r.RowsSHA256 = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return GroupReceipt{}, err
	}
	return r, nil
}

func sameGroups(a, b []Group) bool {
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
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}
