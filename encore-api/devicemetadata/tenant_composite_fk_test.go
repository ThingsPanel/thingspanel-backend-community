package devicemetadata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type tenantCompositeRelation struct {
	name            string
	childTable      string
	childColumns    []string
	parentTable     string
	parentColumns   []string
	onDeleteCode    string
	parentMatchExpr string
	mismatchExpr    string
}

var tenantCompositeRelations = []tenantCompositeRelation{
	{
		name:       "thing_model_items -> thing_models",
		childTable: "thing_model_items", childColumns: []string{"tenant_id", "thing_model_id"},
		parentTable: "thing_models", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "c",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.thing_model_id",
		mismatchExpr:    "c.thing_model_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_models p WHERE p.tenant_id=c.tenant_id AND p.id=c.thing_model_id)",
	},
	{
		name:       "thing_model_snapshots -> thing_models",
		childTable: "thing_model_snapshots", childColumns: []string{"tenant_id", "thing_model_id"},
		parentTable: "thing_models", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "a",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.thing_model_id",
		mismatchExpr:    "c.thing_model_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_models p WHERE p.tenant_id=c.tenant_id AND p.id=c.thing_model_id)",
	},
	{
		name:       "device_access_templates -> thing_models",
		childTable: "device_access_templates", childColumns: []string{"tenant_id", "thing_model_id"},
		parentTable: "thing_models", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "a",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.thing_model_id",
		mismatchExpr:    "c.thing_model_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_models p WHERE p.tenant_id=c.tenant_id AND p.id=c.thing_model_id)",
	},
	{
		name:       "device_access_templates -> thing_model_snapshots",
		childTable: "device_access_templates", childColumns: []string{"tenant_id", "thing_model_id", "thing_model_snapshot_id"},
		parentTable: "thing_model_snapshots", parentColumns: []string{"tenant_id", "thing_model_id", "id"}, onDeleteCode: "a",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.thing_model_id=c.thing_model_id AND p.id=c.thing_model_snapshot_id",
		mismatchExpr:    "c.thing_model_snapshot_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_model_snapshots p WHERE p.tenant_id=c.tenant_id AND p.thing_model_id=c.thing_model_id AND p.id=c.thing_model_snapshot_id)",
	},
	{
		name:       "products -> device_access_templates",
		childTable: "products", childColumns: []string{"tenant_id", "device_template_id"},
		parentTable: "device_access_templates", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "a",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.device_template_id",
		mismatchExpr:    "c.device_template_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM device_access_templates p WHERE p.tenant_id=c.tenant_id AND p.id=c.device_template_id)",
	},
	{
		name:       "devices -> device_access_templates",
		childTable: "devices", childColumns: []string{"tenant_id", "device_template_id"},
		parentTable: "device_access_templates", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "a",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.device_template_id",
		mismatchExpr:    "c.device_template_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM device_access_templates p WHERE p.tenant_id=c.tenant_id AND p.id=c.device_template_id)",
	},
	{
		name:       "devices -> products",
		childTable: "devices", childColumns: []string{"tenant_id", "product_id"},
		parentTable: "products", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "a",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.product_id",
		mismatchExpr:    "c.product_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM products p WHERE p.tenant_id=c.tenant_id AND p.id=c.product_id)",
	},
	{
		name:       "custom_command_controls -> thing_models",
		childTable: "custom_command_controls", childColumns: []string{"tenant_id", "thing_model_id"},
		parentTable: "thing_models", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "c",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.thing_model_id",
		mismatchExpr:    "c.thing_model_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_models p WHERE p.tenant_id=c.tenant_id AND p.id=c.thing_model_id)",
	},
	{
		name:       "thing_model_outbox -> thing_models",
		childTable: "thing_model_outbox", childColumns: []string{"tenant_id", "model_id"},
		parentTable: "thing_models", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "r",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.model_id",
		mismatchExpr:    "c.model_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_models p WHERE p.tenant_id=c.tenant_id AND p.id=c.model_id)",
	},
	{
		name:       "thing_model_resources -> thing_models",
		childTable: "thing_model_resources", childColumns: []string{"tenant_id", "thing_model_id"},
		parentTable: "thing_models", parentColumns: []string{"tenant_id", "id"}, onDeleteCode: "c",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.id=c.thing_model_id",
		mismatchExpr:    "c.thing_model_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_models p WHERE p.tenant_id=c.tenant_id AND p.id=c.thing_model_id)",
	},
	{
		name:       "thing_models.current_snapshot_id -> thing_model_snapshots",
		childTable: "thing_models", childColumns: []string{"tenant_id", "id", "current_snapshot_id"},
		parentTable: "thing_model_snapshots", parentColumns: []string{"tenant_id", "thing_model_id", "id"}, onDeleteCode: "a",
		parentMatchExpr: "p.tenant_id=c.tenant_id AND p.thing_model_id=c.id AND p.id=c.current_snapshot_id",
		mismatchExpr:    "c.current_snapshot_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM thing_model_snapshots p WHERE p.tenant_id=c.tenant_id AND p.thing_model_id=c.id AND p.id=c.current_snapshot_id)",
	},
}

func TestTenantCompositeFKHistoricalRowsHaveNoCrossTenantMismatches(t *testing.T) {
	if testing.Short() {
		t.Skip("requires isolated Encore PostgreSQL test database")
	}
	ctx := context.Background()
	for _, relation := range tenantCompositeRelations {
		t.Run(relation.name, func(t *testing.T) {
			var count int
			query := fmt.Sprintf("SELECT COUNT(*) FROM %s c WHERE %s", relation.childTable, relation.mismatchExpr)
			if err := db.QueryRow(ctx, query).Scan(&count); err != nil {
				t.Fatalf("check existing same-database relation rows: %v", err)
			}
			if count != 0 {
				t.Fatalf("found %d existing rows with missing, cross-tenant, or cross-model target in %s", count, relation.name)
			}
		})
	}
}

func TestTenantCompositeFKCatalogCoversEverySameDatabaseRelation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires isolated Encore PostgreSQL test database")
	}
	ctx := context.Background()
	for _, relation := range tenantCompositeRelations {
		t.Run(relation.name, func(t *testing.T) {
			var found bool
			rows, err := db.Query(ctx, `
				SELECT
					(SELECT string_agg(a.attname, ',' ORDER BY k.ord)
					 FROM unnest(fk.conkey) WITH ORDINALITY AS k(attnum, ord)
					 JOIN pg_catalog.pg_attribute AS a ON a.attrelid=fk.conrelid AND a.attnum=k.attnum),
					(SELECT string_agg(a.attname, ',' ORDER BY k.ord)
					 FROM unnest(fk.confkey) WITH ORDINALITY AS k(attnum, ord)
					 JOIN pg_catalog.pg_attribute AS a ON a.attrelid=fk.confrelid AND a.attnum=k.attnum),
					fk.confdeltype::text
				FROM pg_catalog.pg_constraint AS fk
				WHERE fk.contype='f'
				  AND fk.conrelid=to_regclass($1)
				  AND fk.confrelid=to_regclass($2)
				  AND fk.convalidated`, relation.childTable, relation.parentTable)
			if err != nil {
				t.Fatalf("query composite FK catalog: %v", err)
			}
			wantChild := strings.Join(relation.childColumns, ",")
			wantParent := strings.Join(relation.parentColumns, ",")
			for rows.Next() {
				var childColumns, parentColumns, deleteCode string
				if err := rows.Scan(&childColumns, &parentColumns, &deleteCode); err != nil {
					rows.Close()
					t.Fatalf("scan composite FK catalog: %v", err)
				}
				if childColumns == wantChild && parentColumns == wantParent && deleteCode == relation.onDeleteCode {
					found = true
					break
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				t.Fatalf("read composite FK catalog: %v", err)
			}
			rows.Close()
			if !found {
				t.Fatalf("missing validated composite FK %s(%s) -> %s(%s) with confdeltype %s", relation.childTable, wantChild, relation.parentTable, wantParent, relation.onDeleteCode)
			}
		})
	}
}

func TestTenantCompositeFKRejectsCrossTenantDirectWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("requires isolated Encore PostgreSQL test database")
	}
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	modelA, snapshotA := createPublishedThingModel(t, ctx, tenantA)
	modelB, snapshotB := createPublishedThingModel(t, ctx, tenantB)

	insertTemplate := func(tenantID, modelID, snapshotID, name string) (string, error) {
		id := uuid.NewString()
		_, err := db.Exec(ctx, `
			INSERT INTO device_access_templates (id,tenant_id,name,thing_model_id,thing_model_snapshot_id)
			VALUES ($1,$2,$3,$4,$5)`, id, tenantID, name, modelID, snapshotID)
		return id, err
	}
	templateA, err := insertTemplate(tenantA, modelA.ID, snapshotA.ID, "composite-template-a-"+uuid.NewString())
	if err != nil {
		t.Fatalf("same-tenant device template insert should succeed: %v", err)
	}
	templateB, err := insertTemplate(tenantB, modelB.ID, snapshotB.ID, "composite-template-b-"+uuid.NewString())
	if err != nil {
		t.Fatalf("second same-tenant device template insert should succeed: %v", err)
	}
	expectTenantFKViolation(t, "device template to cross-tenant ThingModel", func() error {
		_, err := insertTemplate(tenantA, modelB.ID, snapshotB.ID, "bad-template-model-"+uuid.NewString())
		return err
	})
	expectTenantFKViolation(t, "device template to another model snapshot", func() error {
		_, err := insertTemplate(tenantA, modelA.ID, snapshotB.ID, "bad-template-snapshot-"+uuid.NewString())
		return err
	})

	itemID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO thing_model_items (id,thing_model_id,tenant_id,type,identifier,name_i18n,value_type,access)
		VALUES ($1,$2,$3,'PROPERTY',$4,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb)`, itemID, modelA.ID, tenantA, "composite_"+strings.ReplaceAll(itemID, "-", "")); err != nil {
		t.Fatalf("same-tenant ThingModel item insert should succeed: %v", err)
	}
	expectTenantFKViolation(t, "ThingModel item to cross-tenant ThingModel", func() error {
		_, err := db.Exec(ctx, `
			INSERT INTO thing_model_items (id,thing_model_id,tenant_id,type,identifier,name_i18n,value_type,access)
			VALUES ($1,$2,$3,'PROPERTY',$4,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb)`, uuid.NewString(), modelB.ID, tenantA, "bad_"+uuid.NewString())
		return err
	})

	goodSnapshotID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO thing_model_snapshots (id,thing_model_id,tenant_id,version,content,published_by)
		VALUES ($1,$2,$3,900001,'{}'::jsonb,$4)`, goodSnapshotID, modelA.ID, tenantA, testActorID()); err != nil {
		t.Fatalf("same-tenant ThingModel snapshot insert should succeed: %v", err)
	}
	expectTenantFKViolation(t, "ThingModel snapshot to cross-tenant ThingModel", func() error {
		_, err := db.Exec(ctx, `
			INSERT INTO thing_model_snapshots (id,thing_model_id,tenant_id,version,content,published_by)
			VALUES ($1,$2,$3,900002,'{}'::jsonb,$4)`, uuid.NewString(), modelB.ID, tenantA, testActorID())
		return err
	})
	if _, err := db.Exec(ctx, `UPDATE thing_models SET current_snapshot_id=$1 WHERE tenant_id=$2 AND id=$3`, goodSnapshotID, tenantA, modelA.ID); err != nil {
		t.Fatalf("same-tenant current snapshot pointer should succeed: %v", err)
	}
	expectTenantFKViolation(t, "ThingModel pointer to another model snapshot", func() error {
		_, err := db.Exec(ctx, `UPDATE thing_models SET current_snapshot_id=$1 WHERE tenant_id=$2 AND id=$3`, snapshotB.ID, tenantA, modelA.ID)
		return err
	})

	productA, productB := uuid.NewString(), uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id,tenant_id,name,device_template_id) VALUES ($1,$2,$3,$4)`, productA, tenantA, "composite-product-a-"+uuid.NewString(), templateA); err != nil {
		t.Fatalf("same-tenant product insert should succeed: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO products (id,tenant_id,name,device_template_id) VALUES ($1,$2,$3,$4)`, productB, tenantB, "composite-product-b-"+uuid.NewString(), templateB); err != nil {
		t.Fatalf("second same-tenant product insert should succeed: %v", err)
	}
	expectTenantFKViolation(t, "product to cross-tenant device template", func() error {
		_, err := db.Exec(ctx, `INSERT INTO products (id,tenant_id,name,device_template_id) VALUES ($1,$2,$3,$4)`, uuid.NewString(), tenantA, "bad-product-"+uuid.NewString(), templateB)
		return err
	})

	insertDevice := func(tenantID, templateID string, productID *string) error {
		_, err := db.Exec(ctx, `INSERT INTO devices (id,tenant_id,device_template_id,product_id,sn) VALUES ($1,$2,$3,$4,$5)`, uuid.NewString(), tenantID, templateID, productID, "composite-device-"+uuid.NewString())
		return err
	}
	if err := insertDevice(tenantA, templateA, &productA); err != nil {
		t.Fatalf("same-tenant device/template/product insert should succeed: %v", err)
	}
	if err := insertDevice(tenantA, templateA, nil); err != nil {
		t.Fatalf("nullable device product_id should remain valid: %v", err)
	}
	expectTenantFKViolation(t, "device to cross-tenant device template", func() error { return insertDevice(tenantA, templateB, nil) })
	expectTenantFKViolation(t, "device to cross-tenant product", func() error { return insertDevice(tenantA, templateA, &productB) })

	controlID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO custom_command_controls (id,tenant_id,thing_model_id,name,content) VALUES ($1,$2,$3,$4,'{}')`, controlID, tenantA, modelA.ID, "control-"+controlID); err != nil {
		t.Fatalf("same-tenant command control insert should succeed: %v", err)
	}
	expectTenantFKViolation(t, "command control to cross-tenant ThingModel", func() error {
		_, err := db.Exec(ctx, `INSERT INTO custom_command_controls (id,tenant_id,thing_model_id,name,content) VALUES ($1,$2,$3,$4,'{}')`, uuid.NewString(), tenantA, modelB.ID, "bad-control-"+uuid.NewString())
		return err
	})

	if _, err := db.Exec(ctx, `INSERT INTO thing_model_outbox (id,tenant_id,model_id,operation,draft_revision,payload) VALUES ($1,$2,$3,'update',1,'{}'::jsonb)`, uuid.NewString(), tenantA, modelA.ID); err != nil {
		t.Fatalf("same-tenant outbox insert should succeed: %v", err)
	}
	expectTenantFKViolation(t, "outbox to cross-tenant ThingModel", func() error {
		_, err := db.Exec(ctx, `INSERT INTO thing_model_outbox (id,tenant_id,model_id,operation,draft_revision,payload) VALUES ($1,$2,$3,'update',1,'{}'::jsonb)`, uuid.NewString(), tenantA, modelB.ID)
		return err
	})

	if _, err := db.Exec(ctx, `INSERT INTO thing_model_resources (tenant_id,thing_model_id) VALUES ($1,$2)`, tenantA, modelA.ID); err != nil {
		t.Fatalf("same-tenant model resources insert should succeed: %v", err)
	}
	expectTenantFKViolation(t, "model resources to cross-tenant ThingModel", func() error {
		_, err := db.Exec(ctx, `INSERT INTO thing_model_resources (tenant_id,thing_model_id) VALUES ($1,$2)`, tenantA, modelB.ID)
		return err
	})
}

func expectTenantFKViolation(t *testing.T, name string, attempt func() error) {
	t.Helper()
	err := attempt()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("%s must fail with foreign_key_violation SQLSTATE 23503, got %v", name, err)
	}
}
