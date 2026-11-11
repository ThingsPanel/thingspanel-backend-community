package offlineupgrade

import (
	"context"
	"testing"
	"time"

	"encore.app/internal/tenantidentity"
	"gorm.io/gorm"
)

func fixtureRelations() RelationsInput {
	b := fixtureInput()
	return RelationsInput{SourceDatabase: b.SourceDatabase, TargetDatabase: b.TargetDatabase, SourceArchiveSHA256: b.SourceArchiveSHA256,
		TargetArchiveSHA256: b.TargetArchiveSHA256, QuarantineSHA256: b.QuarantineSHA256, RecordedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestRelationPlanDoesNotExpandExclusions(t *testing.T) {
	in := fixtureRelations()
	in.Gateways = []SourceGateway{{TenantID: "tenant", DeviceID: "child", ParentID: "parent", Address: "1"}}
	in.ExcludedGateways = []DeviceRelation{{DeviceID: "child", TargetID: "wrong"}}
	if _, err := PlanRelations(in); err == nil {
		t.Fatal("stale gateway exclusion accepted")
	}
	in.ExcludedGateways[0].TargetID = "parent"
	p, err := PlanRelations(in)
	if err != nil || len(p.Gateways) != 0 {
		t.Fatal("matching exclusion not applied")
	}
	in.ExcludedMembers = []MemberExclusion{{GroupID: "absent", DeviceID: "child"}}
	if _, err := PlanRelations(in); err == nil {
		t.Fatal("missing approved source member accepted")
	}
}

func verifyRelationshipRehearsal(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	for id, kind := range map[string]string{"one": "gateway", "two": "subdevice"} {
		if e := db.Table("devices").Where("id = ?", tenantidentity.CanonicalIdentity("device", id)).Update("device_kind", kind).Error; e != nil {
			t.Fatal(e)
		}
	}
	if e := db.Exec("ALTER TABLE gateway_memberships ADD CONSTRAINT fixture_gateway_failure CHECK (subdevice_address <> 'fail')").Error; e != nil {
		t.Fatal(e)
	}
	in := fixtureRelations()
	in.Members = []SourceGroupMember{{TenantID: "tenant", GroupID: "a", DeviceID: "two"}}
	in.Gateways = []SourceGateway{{TenantID: "tenant", DeviceID: "two", ParentID: "one", Address: "fail"}}
	if _, err := ApplyRelationsPG(ctx, db, in); err == nil {
		t.Fatal("gateway fixture constraint must fail")
	}
	var count int64
	db.Table("device_group_members").Count(&count)
	if count != 0 {
		t.Fatal("membership survived relation transaction rollback")
	}
	in.Gateways[0].Address = "1"
	in.Members[0].TenantID = "other-tenant"
	if _, err := ApplyRelationsPG(ctx, db, in); err == nil {
		t.Fatal("native composite FK accepted another tenant")
	}
	in.Members[0].TenantID = "tenant"
	if err := db.Table("devices").Where("id = ?", tenantidentity.CanonicalIdentity("device", "two")).Update("device_kind", "gateway").Error; err != nil {
		t.Fatal(err)
	}
	in.Gateways = append(in.Gateways, SourceGateway{TenantID: "tenant", DeviceID: "one", ParentID: "two", Address: "2"})
	if _, err := ApplyRelationsPG(ctx, db, in); err == nil {
		t.Fatal("native hierarchy trigger accepted a cycle")
	}
	in.Gateways = in.Gateways[:1]
	if err := db.Table("devices").Where("id = ?", tenantidentity.CanonicalIdentity("device", "two")).Update("device_kind", "subdevice").Error; err != nil {
		t.Fatal(err)
	}
	r, err := ApplyRelationsPG(ctx, db, in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Members != 1 || r.Gateways != 1 || r.MigrationComplete {
		t.Fatal("incorrect relation accounting")
	}
	retry, err := ApplyRelationsPG(ctx, db, in)
	if err != nil {
		t.Fatal(err)
	}
	if retry.RowsSHA256 != r.RowsSHA256 || retry.State != "verified-existing-rehearsal-relations" {
		t.Fatal("retry did not reconcile content")
	}
	in.Members[0].TenantID = "other-tenant"
	if _, err := ApplyRelationsPG(ctx, db, in); err == nil {
		t.Fatal("cross-tenant reassignment accepted")
	}
}
