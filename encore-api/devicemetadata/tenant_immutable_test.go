package devicemetadata

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTenantIDImmutableTriggerCoversPackageTablesAndRejectsReassignment(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()

	rows, err := db.Query(ctx, `
		SELECT c.relname
		FROM pg_catalog.pg_class AS c
		JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_attribute AS a ON a.attrelid = c.oid
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND a.attname = 'tenant_id'
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		  AND NOT EXISTS (
		      SELECT 1
		      FROM pg_catalog.pg_trigger AS tg
		      WHERE tg.tgrelid = c.oid
		        AND tg.tgname = 'trg_tenant_id_immutable'
		        AND NOT tg.tgisinternal
		  )
		ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("query tenant trigger coverage: %v", err)
	}
	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan uncovered tenant table: %v", err)
		}
		missing = append(missing, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("read tenant trigger coverage: %v", err)
	}
	rows.Close()
	if len(missing) != 0 {
		t.Fatalf("tenant-bearing tables without immutable-tenant trigger: %v", missing)
	}

	tenantID := testTenantID()
	principal := groupWritePrincipal{tenantID: tenantID, subjectID: "tenant-immutable-" + uuid.NewString(), actorID: "tenant-immutable-test"}
	group := mustCreateDeviceGroup(t, ctx, principal, "tenant-immutable-group-"+uuid.NewString(), &CreateDeviceGroupReq{Name: "tenant-immutable-group-" + uuid.NewString()})
	t.Cleanup(func() { deleteTestDeviceGroups(t, ctx, tenantID, []string{group.Data.ID}) })

	model := createTestThingModel(t, ctx, tenantID)
	t.Cleanup(func() {
		if err := deleteThingModel(ctx, tenantID, model.ID); err != nil {
			t.Errorf("soft-delete tenant-immutability fixture ThingModel: %v", err)
		}
	})

	otherTenant := testTenantID()
	for _, tc := range []struct {
		name  string
		table string
		id    string
	}{
		{name: "native device_groups", table: "device_groups", id: group.Data.ID},
		{name: "legacy ThingModel owner", table: "thing_models", id: model.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Exec(ctx, "UPDATE "+tc.table+" SET tenant_id=tenant_id WHERE id=$1", tc.id); err != nil {
				t.Fatalf("no-op tenant assignment must be allowed: %v", err)
			}
			_, err := db.Exec(ctx, "UPDATE "+tc.table+" SET tenant_id=$1 WHERE id=$2", otherTenant, tc.id)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" || !strings.Contains(pgErr.Message, "tenant_id is immutable") {
				t.Fatalf("tenant reassignment must fail with stable SQLSTATE 23514, got %v", err)
			}
		})
	}
}
