package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.dev/beta/errs"
)

const accessTemplateStoreFixtureConfiguration = `{"deviceKind":"standalone","model":null,"access":{"mode":"direct","protocol":"mqtt","providerId":null,"providerVersion":null,"schemaDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parameters":{"ratio":1.234567890123456789},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":60},"connectionDirection":"device_to_platform","serviceInstanceId":null},"topicMappings":[],"processingScripts":[],"customFieldDefinitions":[]}`

func TestAccessTemplateConfigurationStoreTenantCASAndRollback(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	actor := "issuer:user/with-punctuation@example.test"
	tx, err := beginTenantScopedSQLTx(ctx, tenantA, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	row := &accessTemplateConfigurationRecord{
		Name: "store-test-" + testTenantID(), Tags: []string{"边缘设备"},
		Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration), AdministrativeState: "enabled",
	}
	created, err := insertAccessTemplateConfigurationTx(ctx, tx, tenantA, actor, row)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.TenantID != tenantA || created.ResourceVersion != 1 || created.CreatedBy != actor || created.UpdatedBy != actor {
		t.Fatalf("identity/version/opaque actor changed: %+v", created)
	}
	if len(created.Tags) != 1 || created.Tags[0] != "边缘设备" || !strings.Contains(string(created.Configuration), "1.234567890123456789") {
		t.Fatal("tags or exact configuration number lost in PG roundtrip")
	}
	if _, err := loadAccessTemplateConfigurationTx(ctx, tx, tenantB, created.ID, false); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant read must be not found: %v", err)
	}
	blank := ""
	created.Description = &blank
	created.Name += "-edited"
	saved, err := saveAccessTemplateConfigurationTx(ctx, tx, tenantA, actor, created.ID, 1, created)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ResourceVersion != 2 || saved.Description == nil || *saved.Description != "" || saved.Name != created.Name {
		t.Fatalf("save lost CAS or explicit empty string: %+v", saved)
	}
	created.Name = "must-not-overwrite"
	if _, err := saveAccessTemplateConfigurationTx(ctx, tx, tenantA, actor, created.ID, 1, created); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("stale version must fail: %v", err)
	}
	after, err := loadAccessTemplateConfigurationTx(ctx, tx, tenantA, created.ID, false)
	if err != nil || after.ResourceVersion != 2 || after.Name != saved.Name {
		t.Fatalf("stale write changed stored row: row=%+v err=%v", after, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	check, err := beginTenantScopedSQLTx(ctx, tenantA, true)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Rollback()
	if _, err := loadAccessTemplateConfigurationTx(ctx, check, tenantA, created.ID, false); errs.Code(err) != errs.NotFound {
		t.Fatalf("repository committed outside caller transaction: %v", err)
	}
}

func TestAccessTemplateConfigurationStoreRejectsIncompleteOfflineRow(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	tx, err := beginTenantScopedSQLTx(ctx, tenant, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id := testTenantID()
	if _, err := tx.Exec(ctx, `INSERT INTO device_access_templates(id,tenant_id,name) VALUES($1,$2,$3)`, id, tenant, "offline-"+id); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAccessTemplateConfigurationTx(ctx, tx, tenant, id, false); errs.Code(err) != errs.FailedPrecondition {
		t.Fatalf("incomplete offline row must not become a fabricated v2 template: %v", err)
	}
}

func TestAccessTemplateConfigurationStoreLocalValidation(t *testing.T) {
	valid := func() *accessTemplateConfigurationRecord {
		return &accessTemplateConfigurationRecord{Name: "名称", Tags: []string{}, Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration), AdministrativeState: "enabled"}
	}
	if err := validateAccessTemplateConfigurationRecord(valid()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", " padded", "padded ", "control\n", strings.Repeat("字", 129)} {
		row := valid()
		row.Name = name
		if err := validateAccessTemplateConfigurationRecord(row); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
	for _, tags := range [][]string{nil, {"a", "a"}, {""}, {strings.Repeat("字", 65)}} {
		row := valid()
		row.Tags = tags
		if err := validateAccessTemplateConfigurationRecord(row); err == nil {
			t.Errorf("accepted invalid tags %q", tags)
		}
	}
	row := valid()
	row.Configuration = json.RawMessage(`{}`)
	if err := validateAccessTemplateConfigurationRecord(row); err == nil {
		t.Fatal("accepted empty configuration")
	}
	if err := validateAccessTemplateStoreContext(nil, testTenantID(), "actor"); errs.Code(err) != errs.Internal {
		t.Fatalf("nil tx: %v", err)
	}
}
