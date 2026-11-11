package offlineupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"encore.app/internal/tenantidentity"
	"gorm.io/gorm"
)

type RelationsReceipt struct {
	State               string `json:"state"`
	SourceArchiveSHA256 string `json:"source_archive_sha256"`
	TargetDatabase      string `json:"target_database"`
	Members             int    `json:"converted_members"`
	Gateways            int    `json:"converted_gateways"`
	QuarantinedMembers  int    `json:"quarantined_members"`
	QuarantinedGateways int    `json:"quarantined_gateways"`
	RowsSHA256          string `json:"rows_sha256"`
	MigrationComplete   bool   `json:"migration_complete"`
}

// ApplyRelationsPG retains tenant-composite FKs and the native gateway tree
// trigger. A bad endpoint, kind, address or cycle rolls back both relation sets.
func ApplyRelationsPG(ctx context.Context, db *gorm.DB, in RelationsInput) (RelationsReceipt, error) {
	p, err := PlanRelations(in)
	if err != nil {
		return RelationsReceipt{}, err
	}
	if db.Dialector.Name() != "postgres" {
		return RelationsReceipt{}, fmt.Errorf("PostgreSQL rehearsal required")
	}
	r := RelationsReceipt{SourceArchiveSHA256: in.SourceArchiveSHA256, TargetDatabase: in.TargetDatabase, Members: len(p.Members), Gateways: len(p.Gateways), QuarantinedMembers: len(in.ExcludedMembers), QuarantinedGateways: len(in.ExcludedGateways)}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var database string
		if e := tx.Raw("SELECT current_database()").Scan(&database).Error; e != nil {
			return e
		}
		if database != in.TargetDatabase {
			return fmt.Errorf("target database does not match verified rehearsal")
		}
		if e := tx.Exec("SET LOCAL lock_timeout='10s'").Error; e != nil {
			return e
		}
		if e := tx.Exec("SET LOCAL statement_timeout='60s'").Error; e != nil {
			return e
		}
		if e := tx.Exec("LOCK TABLE devices, device_groups, device_group_members, gateway_memberships, metadata_audit_events IN EXCLUSIVE MODE").Error; e != nil {
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
		members, gateways, e := readRelations(tx)
		if e != nil {
			return e
		}
		events := make([]auditEvent, 0, len(p.Members)+len(p.Gateways))
		add := func(kind, tenant, resource, key string, relation any) {
			changes, _ := json.Marshal(map[string]any{"sourceArchiveSHA256": in.SourceArchiveSHA256, "targetArchiveSHA256": in.TargetArchiveSHA256,
				"quarantineSHA256": in.QuarantineSHA256, "relation": relation, "recordTimeSource": "migration; historical link time unavailable"})
			events = append(events, auditEvent{ID: tenantidentity.CanonicalIdentity("upgrade-"+kind+"-event", in.SourceArchiveSHA256+"/"+key), TenantID: tenant,
				CreatedAt: in.RecordedAt.UTC(), CreatedBy: p.Actor, RequestID: "relations:" + in.SourceArchiveSHA256, OperationName: "offline." + kind + ".convert",
				ResourceKind: map[string]string{"membership": "device_group", "gateway": "device"}[kind], ResourceID: resource, AfterVersion: 1, Outcome: "succeeded", RedactedChanges: string(changes)})
		}
		for _, m := range p.Members {
			add("membership", m.TenantID, m.GroupID, m.GroupID+"/"+m.DeviceID, m)
		}
		for _, g := range p.Gateways {
			add("gateway", g.TenantID, g.ChildDeviceID, g.ChildDeviceID, g)
		}
		if len(members) > 0 || len(gateways) > 0 {
			if !sameRelations(members, gateways, p) {
				return fmt.Errorf("target relations differ; refusing overwrite")
			}
			var stored []auditEvent
			if e := tx.Table("metadata_audit_events").Where("request_id = ? AND operation_name IN ?", "relations:"+in.SourceArchiveSHA256, []string{"offline.membership.convert", "offline.gateway.convert"}).Find(&stored).Error; e != nil {
				return e
			}
			if len(stored) != len(events) {
				return fmt.Errorf("relationship provenance incomplete")
			}
			byID := map[string]auditEvent{}
			for _, old := range stored {
				byID[old.ID] = old
			}
			for _, event := range events {
				old, ok := byID[event.ID]
				if !ok || !old.CreatedAt.Equal(event.CreatedAt) || !sameJSON(old.RedactedChanges, event.RedactedChanges) {
					return fmt.Errorf("relationship provenance differs")
				}
				old.CreatedAt = event.CreatedAt
				old.RedactedChanges = event.RedactedChanges
				if !reflect.DeepEqual(old, event) {
					return fmt.Errorf("relationship audit identity differs")
				}
			}
			r.State = "verified-existing-rehearsal-relations"
		} else {
			if len(p.Members) > 0 {
				if e := tx.Table("device_group_members").Create(&p.Members).Error; e != nil {
					return e
				}
			}
			if len(p.Gateways) > 0 {
				if e := tx.Table("gateway_memberships").Create(&p.Gateways).Error; e != nil {
					return e
				}
			}
			if len(events) > 0 {
				if e := tx.Table("metadata_audit_events").Create(&events).Error; e != nil {
					return e
				}
			}
			r.State = "converted-and-verified-rehearsal-relations"
		}
		members, gateways, e = readRelations(tx)
		if e != nil {
			return e
		}
		if !sameRelations(members, gateways, p) {
			return fmt.Errorf("full relationship reconciliation failed")
		}
		data, _ := json.Marshal(struct {
			Members  []GroupMember
			Gateways []GatewayMember
		}{p.Members, p.Gateways})
		sum := sha256.Sum256(data)
		r.RowsSHA256 = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return RelationsReceipt{}, err
	}
	return r, nil
}

func readRelations(tx *gorm.DB) ([]GroupMember, []GatewayMember, error) {
	members := []GroupMember{}
	gateways := []GatewayMember{}
	if e := tx.Table("device_group_members").Order("tenant_id,group_id,device_id").Find(&members).Error; e != nil {
		return nil, nil, e
	}
	if e := tx.Table("gateway_memberships").Order("tenant_id,child_device_id").Find(&gateways).Error; e != nil {
		return nil, nil, e
	}
	for i := range members {
		members[i].CreatedAt = members[i].CreatedAt.UTC()
	}
	for i := range gateways {
		gateways[i].CreatedAt = gateways[i].CreatedAt.UTC()
		gateways[i].UpdatedAt = gateways[i].UpdatedAt.UTC()
	}
	return members, gateways, nil
}

func sameRelations(m []GroupMember, g []GatewayMember, p RelationsPlan) bool {
	// Empty arrays and nil plans both represent an empty relation set.
	if len(m) != len(p.Members) || len(g) != len(p.Gateways) {
		return false
	}
	for i := range m {
		if !reflect.DeepEqual(m[i], p.Members[i]) {
			return false
		}
	}
	for i := range g {
		if !reflect.DeepEqual(g[i], p.Gateways[i]) {
			return false
		}
	}
	return true
}
