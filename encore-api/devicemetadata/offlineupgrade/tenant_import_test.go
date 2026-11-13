package offlineupgrade

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func tenantImportFixture() TenantImportInput {
	g, d := uuid.NewString(), uuid.NewString()
	return TenantImportInput{SourceDatabase: "ThingsPanel", TargetDatabase: "test_native", SourceDeploymentID: "community-fixture", SourceTenantID: "test-tenant", SourceArchiveSHA256: strings.Repeat("a", 64), TargetArchiveSHA256: strings.Repeat("b", 64), ResolutionsSHA256: strings.Repeat("c", 64), RecordedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC), Groups: []json.RawMessage{fixtureGroup(g, "test-tenant", "0", "imported-group")}, Devices: []json.RawMessage{fixtureDevice(d, "test-tenant", "imported-device", "")}, Members: []SourceGroupMember{{TenantID: "test-tenant", GroupID: g, DeviceID: d}}}
}

func TestTenantImportPlanPreservesSourceAndScope(t *testing.T) {
	in := tenantImportFixture()
	p, e := PlanTenantImport(in)
	if e != nil {
		t.Fatal(e)
	}
	if p.Devices[0].Row.ID != in.Members[0].DeviceID || p.Groups[0].Row.ID != in.Members[0].GroupID || p.Devices[0].Row.AdministrativeState != "disabled" || p.Origins[0].SourceTemplateID != nil {
		t.Fatal("source identity/state changed")
	}
	for name, change := range map[string]func(*TenantImportInput){
		"foreign source":     func(x *TenantImportInput) { x.SourceTenantID = "foreign" },
		"foreign relation":   func(x *TenantImportInput) { x.Members[0].TenantID = "foreign" },
		"missing endpoint":   func(x *TenantImportInput) { x.Members[0].DeviceID = uuid.NewString() },
		"wrong source db":    func(x *TenantImportInput) { x.SourceDatabase = "other" },
		"missing digest":     func(x *TenantImportInput) { x.SourceArchiveSHA256 = "" },
		"missing deployment": func(x *TenantImportInput) { x.SourceDeploymentID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			x := tenantImportFixture()
			change(&x)
			if _, e := PlanTenantImport(x); e == nil {
				t.Fatal("invalid closure accepted")
			}
		})
	}
	// Existing rehearsal public contracts remain strict after mapper extraction.
	if _, e := PlanGroups(GroupInput{SourceDatabase: "ThingsPanel", TargetDatabase: "native"}); e == nil {
		t.Fatal("rehearsal protection removed")
	}
	if _, e := PlanDeviceIdentities(DeviceIdentityInput{SourceDatabase: "ThingsPanel", TargetDatabase: "native"}); e == nil {
		t.Fatal("device rehearsal protection removed")
	}
	if _, e := PlanRelations(RelationsInput{SourceDatabase: "ThingsPanel", TargetDatabase: "native"}); e == nil {
		t.Fatal("relation rehearsal protection removed")
	}
}

func archiveFixture(in *TenantImportInput, approved TenantImportResolutions) ([]byte, []byte, []byte) {
	groups, devices, templates, members := in.Groups, in.Devices, in.Templates, in.Members
	if groups == nil {
		groups = []json.RawMessage{}
	}
	if devices == nil {
		devices = []json.RawMessage{}
	}
	if templates == nil {
		templates = []json.RawMessage{}
	}
	if members == nil {
		members = []SourceGroupMember{}
	}
	src, _ := json.Marshal(map[string]any{"groups": groups, "devices": devices, "templates": templates, "members": members})
	dst := []byte(`{"schema":40,"groups":[],"devices":[],"bindings":[],"members":[]}`)
	rules, _ := json.Marshal(approved)
	in.SourceArchiveSHA256 = rawHash(src)
	in.TargetArchiveSHA256 = rawHash(dst)
	in.ResolutionsSHA256 = rawHash(rules)
	return src, dst, rules
}

func TestTenantImportArchiveBindsRowsClosuresAndResolutions(t *testing.T) {
	in := tenantImportFixture()
	src, dst, rules := archiveFixture(&in, TenantImportResolutions{})
	if e := ValidateTenantImportArchives(in, src, dst, rules); e != nil {
		t.Fatal(e)
	}
	for name, change := range map[string]func(*TenantImportInput){
		"borrowed hash forged name": func(x *TenantImportInput) {
			var d map[string]any
			_ = json.Unmarshal(x.Devices[0], &d)
			d["name"] = "forged"
			x.Devices[0], _ = json.Marshal(d)
		},
		"missing source group":    func(x *TenantImportInput) { x.Groups = nil },
		"missing original member": func(x *TenantImportInput) { x.Members = nil },
		"extra device": func(x *TenantImportInput) {
			x.Devices = append(x.Devices, fixtureDevice(uuid.NewString(), x.SourceTenantID, "extra", ""))
		},
		"unapproved repair": func(x *TenantImportInput) { x.NameFromNumber = []string{x.Members[0].DeviceID} },
	} {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(in)
			var x TenantImportInput
			_ = json.Unmarshal(b, &x)
			change(&x)
			if e := ValidateTenantImportArchives(x, src, dst, rules); e == nil {
				t.Fatal("unarchived input accepted")
			}
		})
	}
	// Empty organizational groups are valid; no device/credential is invented.
	empty := tenantImportFixture()
	empty.Devices = nil
	empty.Members = nil
	s, d, r := archiveFixture(&empty, TenantImportResolutions{})
	if e := ValidateTenantImportArchives(empty, s, d, r); e != nil {
		t.Fatal(e)
	}
	// Explicit original cross-tenant relation remains in the input/archive but
	// is quarantined; it cannot be used to remove a same-tenant membership.
	x := tenantImportFixture()
	foreignID := uuid.NewString()
	group := x.Members[0].GroupID
	x.Members = append(x.Members, SourceGroupMember{TenantID: x.SourceTenantID, GroupID: group, DeviceID: foreignID})
	x.ExcludedMembers = []MemberExclusion{{GroupID: group, DeviceID: foreignID}}
	s, d, r = archiveFixture(&x, TenantImportResolutions{ExcludedMembers: x.ExcludedMembers})
	var source map[string]json.RawMessage
	_ = json.Unmarshal(s, &source)
	rows := append(append([]json.RawMessage{}, x.Devices...), fixtureDevice(foreignID, "another-tenant", "foreign", ""))
	source["devices"], _ = json.Marshal(rows)
	s, _ = json.Marshal(source)
	x.SourceArchiveSHA256 = rawHash(s)
	if e := ValidateTenantImportArchives(x, s, d, r); e != nil {
		t.Fatal(e)
	}
	x.ExcludedMembers = nil
	if e := ValidateTenantImportArchives(x, s, d, r); e == nil {
		t.Fatal("missing approved quarantine accepted")
	}
}

func TestTenantImportPostgresEmptyGroupsAndRealSourceAccess(t *testing.T) {
	db := tenantImportPG(t)
	in := tenantImportFixture()
	_ = db.Raw("SELECT current_database()").Scan(&in.TargetDatabase).Error
	empty := in
	empty.Devices = nil
	empty.Members = nil
	if r, e := ApplyTenantImportPG(context.Background(), db, empty); e != nil || r.Devices != 0 || r.Groups != 1 {
		t.Fatalf("empty groups: %+v %v", r, e)
	}
	// A separate tenant's real protocol metadata stays source-owned; native
	// template/configuration/provider capabilities are not fabricated.
	in = tenantImportFixture()
	_ = db.Raw("SELECT current_database()").Scan(&in.TargetDatabase).Error
	in.SourceTenantID = "source-access-tenant"
	g, d := in.Members[0].GroupID, in.Members[0].DeviceID
	in.Groups = []json.RawMessage{fixtureGroup(g, in.SourceTenantID, "0", "source-access-group")}
	in.Members[0].TenantID = in.SourceTenantID
	var raw map[string]any
	_ = json.Unmarshal(fixtureDevice(d, in.SourceTenantID, "source-device", ""), &raw)
	tid := uuid.NewString()
	raw["device_config_id"] = tid
	raw["protocol"] = "device-protocol"
	raw["access_way"] = "B"
	raw["service_access_id"] = "real-service-id"
	in.Devices[0], _ = json.Marshal(raw)
	template, _ := json.Marshal(map[string]any{"id": tid, "tenant_id": in.SourceTenantID, "name": "Source HTTP", "device_type": "1", "protocol_type": "template-protocol"})
	in.Templates = []json.RawMessage{template}
	p, e := PlanTenantImport(in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ApplyTenantImportPG(context.Background(), db, in); e != nil {
		t.Fatal(e)
	}
	var o ImportedOrigin
	if e := db.Table("imported_device_origins").Where("tenant_id=?", p.TenantID).Take(&o).Error; e != nil {
		t.Fatal(e)
	}
	if text(o.SourceProtocol) != "device-protocol" || text(o.SourceAccessWay) != "B" || text(o.SourceServiceAccessID) != "real-service-id" || text(o.SourceTemplateID) != tid || text(o.SourceTemplateSHA256) != rawHash(template) {
		t.Fatalf("source metadata changed: %+v", o)
	}
	var count int64
	db.Table("device_access_templates").Count(&count)
	if count != 0 {
		t.Fatal("fake native template created")
	}
}

func tenantImportPG(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TENANT_IMPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("TENANT_IMPORT_TEST_DSN required")
	}
	c, e := pgx.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	if c.Host != "127.0.0.1" && c.Host != "localhost" {
		t.Fatal("local isolated PostgreSQL required")
	}
	admin := stdlib.OpenDB(*c)
	t.Cleanup(func() { admin.Close() })
	schema := "tenant_import_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e := admin.Exec(`CREATE SCHEMA "` + schema + `"`); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); e != nil {
			t.Error(e)
		}
	})
	c.RuntimeParams["search_path"] = schema + ",public"
	pool := stdlib.OpenDB(*c)
	pool.SetMaxOpenConns(2)
	t.Cleanup(func() { pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	paths, e := filepath.Glob("../migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if e := db.Exec(string(b)).Error; e != nil {
			t.Fatalf("%s: %v", filepath.Base(p), e)
		}
	}
	if e := db.Exec("CREATE TABLE schema_migrations(version bigint,dirty boolean); INSERT INTO schema_migrations VALUES(41,false)").Error; e != nil {
		t.Fatal(e)
	}
	return db
}

func TestTenantImportPostgresAtomicityIsolationReplay(t *testing.T) {
	db := tenantImportPG(t)
	ctx := context.Background()
	in := tenantImportFixture()
	if e := db.Raw("SELECT current_database()").Scan(&in.TargetDatabase).Error; e != nil {
		t.Fatal(e)
	}
	p, e := PlanTenantImport(in)
	if e != nil {
		t.Fatal(e)
	}
	// Preserve a pre-existing native resource, including another tenant.
	native := p.Devices[0].Row
	native.ID = uuid.NewString()
	native.TenantID = uuid.NewString()
	native.DeviceNumber = "native-existing"
	native.Name = "must-survive"
	if e := db.Table("devices").Create(&native).Error; e != nil {
		t.Fatal(e)
	}
	// A late constraint failure must roll back devices, runtime bindings and origins.
	if e := db.Exec("ALTER TABLE device_groups ADD CONSTRAINT fixture_reject CHECK(name <> 'imported-group')").Error; e != nil {
		t.Fatal(e)
	}
	if _, e := ApplyTenantImportPG(ctx, db, in); e == nil {
		t.Fatal("late failure accepted")
	}
	for _, table := range []string{"devices", "device_access_bindings", "imported_device_origins", "device_groups", "device_group_members", "metadata_audit_events"} {
		var n int64
		if e := db.Table(table).Where("tenant_id=?", p.TenantID).Count(&n).Error; e != nil || n != 0 {
			t.Fatalf("partial transaction %s: %d %v", table, n, e)
		}
	}
	if e := db.Exec("ALTER TABLE device_groups DROP CONSTRAINT fixture_reject").Error; e != nil {
		t.Fatal(e)
	}
	r, e := ApplyTenantImportPG(ctx, db, in)
	if e != nil {
		t.Fatal(e)
	}
	if r.State != "imported" || r.Groups != 1 || r.Members != 1 || r.Devices != 1 || r.RuntimeOwnershipChanged {
		t.Fatalf("wrong receipt %+v", r)
	}
	var binding struct {
		TemplateID                                                               *string
		DirectAccess                                                             *string
		RuntimeProviderID, RuntimeNamespace, RuntimeExternalID, DeviceParameters string
	}
	if e := db.Table("device_access_bindings").Where("tenant_id=?", p.TenantID).Take(&binding).Error; e != nil {
		t.Fatal(e)
	}
	if binding.TemplateID != nil || binding.DirectAccess != nil || binding.RuntimeProviderID != importedCommunityRuntimeProviderID || binding.RuntimeNamespace != "community" || binding.RuntimeExternalID != p.Devices[0].Row.ID || binding.DeviceParameters != "{}" {
		t.Fatalf("fabricated access: %+v", binding)
	}
	var origin ImportedOrigin
	if e := db.Table("imported_device_origins").Where("tenant_id=?", p.TenantID).Take(&origin).Error; e != nil {
		t.Fatal(e)
	}
	if origin.SourceRowSHA256 != rawHash(in.Devices[0]) || origin.SourceArchiveSHA256 != in.SourceArchiveSHA256 {
		t.Fatal("provenance lost")
	}
	if e := db.Exec("UPDATE devices SET name='user-new-name' WHERE id=?", p.Devices[0].Row.ID).Error; e != nil {
		t.Fatal(e)
	}
	r, e = ApplyTenantImportPG(ctx, db, in)
	if e != nil || r.State != "already-imported-no-changes" {
		t.Fatalf("retry: %+v %v", r, e)
	}
	var name string
	db.Table("devices").Select("name").Where("id=?", p.Devices[0].Row.ID).Scan(&name)
	if name != "user-new-name" {
		t.Fatal("retry overwrote user edit")
	}
	db.Table("devices").Select("name").Where("id=?", native.ID).Scan(&name)
	if name != "must-survive" {
		t.Fatal("other tenant changed")
	}
	changed := in
	changed.RecordedAt = changed.RecordedAt.Add(time.Second)
	if _, e := ApplyTenantImportPG(ctx, db, changed); e == nil {
		t.Fatal("different same-archive input accepted")
	}
	collision := in
	collision.SourceArchiveSHA256 = strings.Repeat("d", 64)
	if _, e := ApplyTenantImportPG(ctx, db, collision); e == nil {
		t.Fatal("unowned collision overwritten")
	}
	var audit string
	db.Table("metadata_audit_events").Select("redacted_changes::text").Where("tenant_id=?", p.TenantID).Scan(&audit)
	if strings.Contains(audit, "private_connector_value") {
		t.Fatal("private source leaked")
	}
}

func TestTenantImportPostgresGatewayClosure(t *testing.T) {
	db := tenantImportPG(t)
	in := tenantImportFixture()
	if e := db.Raw("SELECT current_database()").Scan(&in.TargetDatabase).Error; e != nil {
		t.Fatal(e)
	}
	parent := in.Members[0].DeviceID
	child := uuid.NewString()
	var row map[string]any
	_ = json.Unmarshal(fixtureDevice(child, in.SourceTenantID, "child", parent), &row)
	row["sub_device_addr"] = "01"
	raw, _ := json.Marshal(row)
	in.Devices = append(in.Devices, raw)
	in.Members = append(in.Members, SourceGroupMember{TenantID: in.SourceTenantID, GroupID: in.Members[0].GroupID, DeviceID: child})
	p, e := PlanTenantImport(in)
	if e != nil {
		t.Fatal(e)
	}
	if r, e := ApplyTenantImportPG(context.Background(), db, in); e != nil || r.Gateways != 1 || r.Devices != 2 || r.Members != 2 {
		t.Fatalf("gateway closure: %+v %v", r, e)
	}
	var link GatewayMember
	if e := db.Table("gateway_memberships").Where("tenant_id=?", p.TenantID).Take(&link).Error; e != nil {
		t.Fatal(e)
	}
	if link.ChildDeviceID != child || link.GatewayDeviceID != parent || link.SubdeviceAddress != "01" {
		t.Fatal("gateway relationship changed")
	}
	var n int64
	db.Table("device_access_bindings").Where("tenant_id=? AND runtime_provider_id=?", p.TenantID, importedCommunityRuntimeProviderID).Count(&n)
	if n != 2 {
		t.Fatal("missing source runtime identity")
	}
}

func reviewedSelfGatewayFixture() TenantImportInput {
	in := tenantImportFixture()
	parent := in.Members[0].DeviceID
	child := uuid.NewString()
	in.Devices[0] = fixtureDevice(parent, in.SourceTenantID, "reviewed-self-gateway", parent)
	var row map[string]any
	_ = json.Unmarshal(fixtureDevice(child, in.SourceTenantID, "valid-child", parent), &row)
	row["sub_device_addr"] = "valid01"
	raw, _ := json.Marshal(row)
	in.Devices = append(in.Devices, raw)
	// Only child is a direct member; parent must survive the dependency closure.
	in.Members[0].DeviceID = child
	in.ExcludedGateways = []DeviceRelation{{DeviceID: parent, TargetID: parent}}
	return in
}

func TestTenantImportArchiveReviewedGatewayExclusions(t *testing.T) {
	in := reviewedSelfGatewayFixture()
	src, dst, rules := archiveFixture(&in, TenantImportResolutions{ExcludedGateways: in.ExcludedGateways})
	if e := ValidateTenantImportArchives(in, src, dst, rules); e != nil {
		t.Fatal(e)
	}
	for name, change := range map[string]func(*TenantImportInput){
		"omit approved pair": func(x *TenantImportInput) { x.ExcludedGateways = nil },
		"forged pair":        func(x *TenantImportInput) { x.ExcludedGateways[0].TargetID = uuid.NewString() },
		"extra exclusion": func(x *TenantImportInput) {
			x.ExcludedGateways = append(x.ExcludedGateways, DeviceRelation{DeviceID: uuid.NewString(), TargetID: uuid.NewString()})
		},
	} {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(in)
			var x TenantImportInput
			_ = json.Unmarshal(b, &x)
			change(&x)
			if e := ValidateTenantImportArchives(x, src, dst, rules); e == nil {
				t.Fatal("unauthorized gateway exclusion accepted")
			}
		})
	}
	// Even a resolution file cannot call a valid same-tenant child edge erroneous.
	bad := reviewedSelfGatewayFixture()
	parent := bad.ExcludedGateways[0].DeviceID
	child := bad.Members[0].DeviceID
	bad.ExcludedGateways = append(bad.ExcludedGateways, DeviceRelation{DeviceID: child, TargetID: parent})
	s, d, r := archiveFixture(&bad, TenantImportResolutions{ExcludedGateways: bad.ExcludedGateways})
	if e := ValidateTenantImportArchives(bad, s, d, r); e == nil {
		t.Fatal("valid child edge was discarded")
	}
	// A real cross-tenant original edge can be quarantined without pulling the
	// foreign parent into the target tenant or rewriting the child's raw row.
	cross := tenantImportFixture()
	child = cross.Members[0].DeviceID
	foreign := uuid.NewString()
	cross.Devices[0] = fixtureDevice(child, cross.SourceTenantID, "cross-child", foreign)
	cross.ExcludedGateways = []DeviceRelation{{DeviceID: child, TargetID: foreign}}
	s, d, r = archiveFixture(&cross, TenantImportResolutions{ExcludedGateways: cross.ExcludedGateways})
	var source map[string]json.RawMessage
	_ = json.Unmarshal(s, &source)
	all := append(append([]json.RawMessage{}, cross.Devices...), fixtureDevice(foreign, "foreign-tenant", "foreign-parent", ""))
	source["devices"], _ = json.Marshal(all)
	s, _ = json.Marshal(source)
	cross.SourceArchiveSHA256 = rawHash(s)
	if e := ValidateTenantImportArchives(cross, s, d, r); e != nil {
		t.Fatal(e)
	}
}

func TestTenantImportPostgresReviewedSelfGatewayKeepsChild(t *testing.T) {
	db := tenantImportPG(t)
	in := reviewedSelfGatewayFixture()
	if e := db.Raw("SELECT current_database()").Scan(&in.TargetDatabase).Error; e != nil {
		t.Fatal(e)
	}
	s, d, r := archiveFixture(&in, TenantImportResolutions{ExcludedGateways: in.ExcludedGateways})
	if e := ValidateTenantImportArchives(in, s, d, r); e != nil {
		t.Fatal(e)
	}
	original := string(in.Devices[0])
	parent := in.ExcludedGateways[0].DeviceID
	child := in.Members[0].DeviceID
	receipt, e := ApplyTenantImportPG(context.Background(), db, in)
	if e != nil {
		t.Fatal(e)
	}
	if receipt.Devices != 2 || receipt.Members != 1 || receipt.Gateways != 1 || receipt.QuarantinedGateways != 1 {
		t.Fatalf("incomplete closure: %+v", receipt)
	}
	if string(in.Devices[0]) != original {
		t.Fatal("original self-parent source row rewritten")
	}
	var links []GatewayMember
	if e := db.Table("gateway_memberships").Where("tenant_id=?", receipt.TenantID).Find(&links).Error; e != nil {
		t.Fatal(e)
	}
	if len(links) != 1 || links[0].ChildDeviceID != child || links[0].GatewayDeviceID != parent || links[0].SubdeviceAddress != "valid01" {
		t.Fatal("valid child edge lost or self-loop imported")
	}
	var identities []DeviceIdentity
	if e := db.Table("devices").Where("tenant_id=?", receipt.TenantID).Find(&identities).Error; e != nil {
		t.Fatal(e)
	}
	if len(identities) != 2 {
		t.Fatal("quarantined edge removed a device")
	}
	for _, identity := range identities {
		if identity.ID == parent && identity.DeviceKind != "gateway" {
			t.Fatal("valid child no longer belongs to gateway")
		}
	}
	var o ImportedOrigin
	if e := db.Table("imported_device_origins").Where("tenant_id=? AND device_id=?", receipt.TenantID, parent).Take(&o).Error; e != nil {
		t.Fatal(e)
	}
	if o.SourceRowSHA256 != rawHash([]byte(original)) {
		t.Fatal("original source provenance lost")
	}
}
