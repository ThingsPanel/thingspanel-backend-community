package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestNativeTemplateListFiltersPagingAndScopedCounts(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	tx, err := beginTenantScopedSQLTx(ctx, tenantA, false)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	modelID, _ := seedRevisionSchemaModel(t, ctx, tx, tenantA, "template-list-model-", "PUBLISHED", []revisionSchemaSnapshot{
		{version: 1, canonical: true, canonicalContent: []byte(`{"fields":[]}`), semanticDigest: strings.Repeat("a", 64), resourcesDigest: strings.Repeat("b", 64)},
	})
	configurationWithModel := json.RawMessage(stringsReplaceTemplateModel(accessTemplateStoreFixtureConfiguration, modelID))
	templateA1 := insertTemplateListFixture(t, ctx, tx, tenantA, "list-a-z-"+uuid.NewString(), "Acme", []string{"edge", "mqtt"}, json.RawMessage(accessTemplateStoreFixtureConfiguration))
	templateA2 := insertTemplateListFixture(t, ctx, tx, tenantA, "list-a-a-"+uuid.NewString(), "Acme", []string{"edge"}, configurationWithModel)
	templateA3 := insertTemplateListFixture(t, ctx, tx, tenantA, "other-"+uuid.NewString(), "Other", []string{"other"}, json.RawMessage(accessTemplateStoreFixtureConfiguration))
	templateA4 := insertTemplateListFixture(t, ctx, tx, tenantA, "blank-brand-"+uuid.NewString(), "", []string{}, json.RawMessage(accessTemplateStoreFixtureConfiguration))
	templateB := insertTemplateListFixture(t, ctx, tx, tenantB, "list-b-"+uuid.NewString(), "Acme", []string{"edge"}, json.RawMessage(accessTemplateStoreFixtureConfiguration))

	// Equal timestamps make the ID tie-breaker observable and deterministic.
	for _, row := range []*accessTemplateConfigurationRecord{templateA1, templateA2, templateA3, templateA4, templateB} {
		if _, err := tx.Exec(ctx, `UPDATE device_access_templates SET created_at='2026-01-01T00:00:00Z' WHERE tenant_id=$1 AND id=$2`, row.TenantID, row.ID); err != nil {
			t.Fatal(err)
		}
	}
	dev1 := insertNativeTemplateRecordBoundDevice(t, ctx, tx, tenantA, templateA1.ID, "list-device-1", false)
	insertNativeTemplateRecordBoundDevice(t, ctx, tx, tenantA, templateA1.ID, "list-device-2", false)

	filtered, total, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, nil, nativeTemplateListQuery{
		Search: "list-a", DeviceKind: "standalone", Protocol: "mqtt", Brand: "Acme", Tag: "edge", ModelID: modelID,
		AdministrativeState: "enabled", Sort: "name",
	})
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if total != 1 || len(filtered) != 1 || filtered[0].ID != templateA2.ID || filtered[0].DeviceCount != 0 {
		t.Fatalf("filtered templates total=%d items=%+v", total, filtered)
	}
	for _, tc := range []struct {
		name  string
		query nativeTemplateListQuery
		want  int64
	}{
		{name: "empty protocol is a present filter", query: nativeTemplateListQuery{ProtocolSet: true}, want: 0},
		{name: "empty tag is a present filter", query: nativeTemplateListQuery{TagSet: true}, want: 0},
		{name: "empty brand matches stored empty string", query: nativeTemplateListQuery{BrandSet: true}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, nil, tc.query)
			if err != nil || total != tc.want || int64(len(items)) != tc.want {
				t.Fatalf("empty-filter result total=%d items=%d err=%v, want %d", total, len(items), err, tc.want)
			}
			if tc.name == "empty brand matches stored empty string" && items[0].ID != templateA4.ID {
				t.Fatalf("empty brand returned %s, want %s", items[0].ID, templateA4.ID)
			}
		})
	}

	first, total, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, nil, nativeTemplateListQuery{Page: 1, PageSize: 1, Sort: "createdAt"})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	second, total2, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, nil, nativeTemplateListQuery{Page: 2, PageSize: 1, Sort: "createdAt"})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if total != 4 || total2 != total || len(first) != 1 || len(second) != 1 || first[0].ID >= second[0].ID {
		t.Fatalf("stable page ordering total=%d/%d first=%+v second=%+v", total, total2, first, second)
	}
	defaultFirst, _, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, nil, nativeTemplateListQuery{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("default page: %v", err)
	}
	defaultSecond, _, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, nil, nativeTemplateListQuery{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("default second page: %v", err)
	}
	if defaultFirst[0].ID <= defaultSecond[0].ID {
		t.Fatalf("default descending createdAt tie-breaker is not descending: first=%s second=%s", defaultFirst[0].ID, defaultSecond[0].ID)
	}

	filteredByScope, total, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, []string{dev1}, nativeTemplateListQuery{Search: "list-a-z"})
	if err != nil {
		t.Fatalf("scoped list: %v", err)
	}
	if total != 1 || len(filteredByScope) != 1 || filteredByScope[0].ID != templateA1.ID || filteredByScope[0].DeviceCount != 1 {
		t.Fatalf("scope result total=%d items=%+v", total, filteredByScope)
	}
	if _, _, err := listNativeDeviceAccessTemplatesTx(ctx, tx, tenantA, []string{"not-a-canonical-id"}, nativeTemplateListQuery{}); errs.Code(err) != errs.Unauthenticated {
		t.Fatalf("invalid authorized scope error=%v, want Unauthenticated", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ tenantID, id string }{{tenantA, templateA1.ID}, {tenantA, templateA2.ID}, {tenantA, templateA3.ID}, {tenantA, templateA4.ID}, {tenantB, templateB.ID}} {
		var count int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, check.tenantID, check.id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("list fixture leaked id=%s count=%d err=%v", check.id, count, err)
		}
	}
}

func insertTemplateListFixture(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, name, brand string, tags []string, configuration json.RawMessage) *accessTemplateConfigurationRecord {
	t.Helper()
	row, err := insertAccessTemplateConfigurationTx(ctx, tx, tenantID, "template-list-test", &accessTemplateConfigurationRecord{
		Name: name, Brand: &brand, Tags: tags, Configuration: configuration, AdministrativeState: "enabled",
	})
	if err != nil {
		t.Fatalf("insert list fixture: %v", err)
	}
	return row
}

func stringsReplaceTemplateModel(configuration, modelID string) string {
	return strings.Replace(string(configuration), `"model":null`, `"model":{"modelId":"`+modelID+`","publishedRevision":1}`, 1)
}

func boolPtr(value bool) *bool { return &value }
