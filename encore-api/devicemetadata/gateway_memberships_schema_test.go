package devicemetadata

import (
	"context"
	"strings"
	"testing"
	"time"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestGatewayMembershipsSchemaAndTenantConstraints(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var columns int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='gateway_memberships'
	`).Scan(&columns); err != nil || columns != 7 {
		t.Fatalf("gateway_memberships columns=%d err=%v, want seven contract fields", columns, err)
	}
	for _, expected := range []struct{ name, reference string }{
		{"gateway_memberships_child_fk", "FOREIGN KEY (tenant_id, child_device_id) REFERENCES devices(tenant_id, id)"},
		{"gateway_memberships_gateway_fk", "FOREIGN KEY (tenant_id, gateway_device_id) REFERENCES devices(tenant_id, id)"},
	} {
		var definition string
		if err := tx.QueryRow(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_catalog.pg_constraint
			WHERE conrelid='gateway_memberships'::regclass AND conname=$1
		`, expected.name).Scan(&definition); err != nil || !strings.Contains(definition, expected.reference) || !strings.Contains(definition, "ON DELETE RESTRICT") {
			t.Fatalf("constraint %s definition=%q err=%v", expected.name, definition, err)
		}
	}
	var primaryKeyDefinition string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_catalog.pg_constraint
		WHERE conrelid='gateway_memberships'::regclass AND conname='gateway_memberships_pk'
	`).Scan(&primaryKeyDefinition); err != nil || primaryKeyDefinition != "PRIMARY KEY (tenant_id, child_device_id)" {
		t.Fatalf("gateway membership primary key definition=%q err=%v", primaryKeyDefinition, err)
	}
	var addressCollation string
	if err := tx.QueryRow(ctx, `
		SELECT c.collname
		FROM pg_catalog.pg_attribute AS a
		JOIN pg_catalog.pg_class AS r ON r.oid=a.attrelid
		JOIN pg_catalog.pg_namespace AS n ON n.oid=r.relnamespace
		JOIN pg_catalog.pg_collation AS c ON c.oid=a.attcollation
		WHERE n.nspname=current_schema() AND r.relname='gateway_memberships' AND a.attname='subdevice_address'
	`).Scan(&addressCollation); err != nil || addressCollation != "C" {
		t.Fatalf("subdevice_address collation=%q err=%v, want C", addressCollation, err)
	}
	var treeTrigger, immutableTrigger, timestampTrigger bool
	if err := tx.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='gateway_memberships'::regclass AND tgname='trg_gateway_memberships_validate_tree' AND NOT tgisinternal),
			EXISTS (SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='gateway_memberships'::regclass AND tgname='trg_gateway_memberships_identity_immutable' AND NOT tgisinternal),
			EXISTS (SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid='gateway_memberships'::regclass AND tgname='trg_gateway_memberships_updated_at' AND NOT tgisinternal)
	`).Scan(&treeTrigger, &immutableTrigger, &timestampTrigger); err != nil || !treeTrigger || !immutableTrigger || !timestampTrigger {
		t.Fatalf("membership triggers missing tree=%v identity=%v updated_at=%v err=%v", treeTrigger, immutableTrigger, timestampTrigger, err)
	}
	var triggerDefinition string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_functiondef('validate_gateway_membership_tree()'::regprocedure)
	`).Scan(&triggerDefinition); err != nil || !strings.Contains(triggerDefinition, "pg_advisory_xact_lock") ||
		!strings.Contains(triggerDefinition, "FOR SHARE") || !strings.Contains(triggerDefinition, "WITH RECURSIVE") {
		t.Fatalf("tree guard lacks tenant lock, endpoint row lock, or recursive validation: err=%v", err)
	}
	var deferredDeviceStateTrigger bool
	if err := tx.QueryRow(ctx, `
		SELECT tg.tgdeferrable AND tg.tginitdeferred
		FROM pg_catalog.pg_trigger AS tg
		WHERE tg.tgrelid='devices'::regclass AND tg.tgname='trg_devices_gateway_membership_state' AND NOT tg.tgisinternal
	`).Scan(&deferredDeviceStateTrigger); err != nil || !deferredDeviceStateTrigger {
		t.Fatalf("device state constraint trigger is not initially deferred: found=%v err=%v", deferredDeviceStateTrigger, err)
	}
	var addressUnique bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_constraint
			WHERE conrelid='gateway_memberships'::regclass
			  AND conname='gateway_memberships_parent_address_unique'
			  AND contype='u'
			  AND pg_get_constraintdef(oid)='UNIQUE (tenant_id, gateway_device_id, subdevice_address)'
		)
	`).Scan(&addressUnique); err != nil || !addressUnique {
		t.Fatalf("gateway address uniqueness contract missing: found=%v err=%v", addressUnique, err)
	}

	tenantA, tenantB := testTenantID(), testTenantID()
	root := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", false)
	childGateway := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", false)
	grandchildGateway := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", false)
	leaf := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "subdevice", false)
	secondLeaf := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "subdevice", false)
	standalone := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "standalone", false)
	otherTenantGateway := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantB, "gateway", false)
	otherTenantChild := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantB, "subdevice", false)
	crossParentChild := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "subdevice", false)

	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, childGateway, root, "port-1", "gateway-schema-test"); err != nil {
		t.Fatalf("insert gateway-to-gateway relation: %v", err)
	}
	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, leaf, childGateway, "port-2", "gateway-schema-test"); err != nil {
		t.Fatalf("insert gateway-to-subdevice relation: %v", err)
	}
	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, grandchildGateway, childGateway, "gateway-port-3", "gateway-schema-test"); err != nil {
		t.Fatalf("insert three-level gateway relation: %v", err)
	}
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_tenant_immutable", "23514", func() error {
		_, err := tx.Exec(ctx, `UPDATE gateway_memberships SET tenant_id=$1 WHERE tenant_id=$2 AND child_device_id=$3`, tenantB, tenantA, leaf)
		return err
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_child_identity_immutable", "23514", func() error {
		_, err := tx.Exec(ctx, `UPDATE gateway_memberships SET child_device_id=$1 WHERE tenant_id=$2 AND child_device_id=$3`, secondLeaf, tenantA, leaf)
		return err
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_parent_kind_change_with_live_edge", "23514", func() error {
		if _, err := tx.Exec(ctx, `UPDATE devices SET device_kind='subdevice' WHERE tenant_id=$1 AND id=$2`, tenantA, root); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `SET CONSTRAINTS trg_devices_gateway_membership_state IMMEDIATE`)
		return err
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_child_soft_delete_with_live_edge", "23514", func() error {
		if _, err := tx.Exec(ctx, `UPDATE devices SET deleted_at=NOW(), deleted_by='schema-test' WHERE tenant_id=$1 AND id=$2`, tenantA, leaf); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `SET CONSTRAINTS trg_devices_gateway_membership_state IMMEDIATE`)
		return err
	})
	var createdAt, updatedAt time.Time
	var actor string
	if err := tx.QueryRow(ctx, `
		SELECT created_at, updated_at, updated_by FROM gateway_memberships
		WHERE tenant_id=$1 AND child_device_id=$2
	`, tenantA, leaf).Scan(&createdAt, &updatedAt, &actor); err != nil {
		t.Fatal(err)
	}
	if createdAt.IsZero() || updatedAt.IsZero() || actor == "" {
		t.Fatalf("membership metadata missing created=%s updated=%s actor=%q", createdAt, updatedAt, actor)
	}
	var controlledUpdatedAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE gateway_memberships SET updated_at='2000-01-01T00:00:00Z'
		WHERE tenant_id=$1 AND child_device_id=$2 RETURNING updated_at
	`, tenantA, leaf).Scan(&controlledUpdatedAt); err != nil || !controlledUpdatedAt.After(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("membership updated_at not server controlled: value=%s err=%v", controlledUpdatedAt, err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE gateway_memberships SET subdevice_address='renamed-port'
		WHERE tenant_id=$1 AND child_device_id=$2
	`, tenantA, leaf); err != nil {
		t.Fatalf("update valid membership address: %v", err)
	}

	for _, test := range []struct {
		name, address, sqlState string
	}{
		{"empty_address", "", "23514"},
		{"leading_space", " port", "23514"},
		{"trailing_space", "port ", "23514"},
		{"control_character", "port\t1", "23514"},
		{"unicode_leading_space", "\u2003port", "23514"},
		{"unicode_trailing_space", "port\u00a0", "23514"},
		{"c1_control", "port\u0085x", "23514"},
		{"address_over_128_codepoints", strings.Repeat("界", 129), "22001"},
	} {
		test := test
		expectDeviceIdentitySQLState(t, ctx, tx, "gateway_"+test.name, test.sqlState, func() error {
			return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, secondLeaf, root, test.address, "gateway-schema-test")
		})
	}
	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, secondLeaf, root, strings.Repeat("界", 128), "gateway-schema-test"); err != nil {
		t.Fatalf("128 Unicode codepoints should fit address limit: %v", err)
	}

	duplicateAddressChild := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "subdevice", false)
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_duplicate_address", "23505", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, duplicateAddressChild, root, "port-1", "gateway-schema-test")
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_self_parent", "23514", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, root, root, "self", "gateway-schema-test")
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_cross_tenant_parent", "23503", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, crossParentChild, otherTenantGateway, "cross-parent", "gateway-schema-test")
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_cross_tenant_child", "23503", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, otherTenantChild, root, "cross-child", "gateway-schema-test")
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_parent_must_be_gateway", "23514", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, duplicateAddressChild, standalone, "wrong-parent", "gateway-schema-test")
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_child_cannot_be_standalone", "23514", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, standalone, root, "wrong-child", "gateway-schema-test")
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_updated_by_nonempty", "23514", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, duplicateAddressChild, root, "actor", " ")
	})

	deletedGateway := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", true)
	deletedChild := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "subdevice", true)
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_parent_must_be_live", "23514", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, duplicateAddressChild, deletedGateway, "deleted-parent", "gateway-schema-test")
	})
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_child_must_be_live", "23514", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, deletedChild, root, "deleted-child", "gateway-schema-test")
	})

	// Moving gateway childGateway below its descendant grandchildGateway would create a cycle.
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_cycle_on_move", "23514", func() error {
		_, err := tx.Exec(ctx, `
			UPDATE gateway_memberships SET gateway_device_id=$1, subdevice_address='cycle-move'
			WHERE tenant_id=$2 AND child_device_id=$3
		`, grandchildGateway, tenantA, childGateway)
		return err
	})
	if _, err := tx.Exec(ctx, `DELETE FROM gateway_memberships WHERE tenant_id=$1 AND child_device_id=$2`, tenantA, leaf); err != nil {
		t.Fatalf("delete edge under tree lock: %v", err)
	}

	// Build a 30-level parent chain and a three-level subtree. Moving the
	// subtree under depth 30 must fail (33 total); depth 29 must succeed (32).
	chain := make([]string, 30)
	for i := range chain {
		chain[i] = insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", false)
		if i > 0 {
			if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, chain[i], chain[i-1], "chain-"+uuid.NewString(), "gateway-schema-test"); err != nil {
				t.Fatalf("insert parent chain depth %d: %v", i+1, err)
			}
		}
	}
	subtreeRoot := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", false)
	subtreeMiddle := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", false)
	subtreeLeaf := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "subdevice", false)
	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, subtreeMiddle, subtreeRoot, "subtree-1", "gateway-schema-test"); err != nil {
		t.Fatalf("insert subtree middle: %v", err)
	}
	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, subtreeLeaf, subtreeMiddle, "subtree-2", "gateway-schema-test"); err != nil {
		t.Fatalf("insert subtree leaf: %v", err)
	}
	expectDeviceIdentitySQLState(t, ctx, tx, "gateway_move_subtree_depth_33", "23514", func() error {
		return insertGatewayMembershipSchemaRow(ctx, tx, tenantA, subtreeRoot, chain[29], "move-depth-33", "gateway-schema-test")
	})
	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, subtreeRoot, chain[28], "move-depth-32", "gateway-schema-test"); err != nil {
		t.Fatalf("subtree whose deepest descendant is level 32 should be accepted: %v", err)
	}

	unboundGateway := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "gateway", false)
	unboundChild := insertGatewayMembershipSchemaDevice(t, ctx, tx, tenantA, "subdevice", false)
	if err := insertGatewayMembershipSchemaRow(ctx, tx, tenantA, unboundChild, unboundGateway, "unbind-before-delete", "gateway-schema-test"); err != nil {
		t.Fatalf("insert relation for deferred unbind/delete contract: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM gateway_memberships WHERE tenant_id=$1 AND child_device_id=$2`, tenantA, unboundChild); err != nil {
		t.Fatalf("unbind gateway child before same-transaction soft delete: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET deleted_at=NOW(), deleted_by='schema-test' WHERE tenant_id=$1 AND id=$2`, tenantA, unboundGateway); err != nil {
		t.Fatalf("soft-delete unbound gateway in same transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS trg_devices_gateway_membership_state IMMEDIATE`); err != nil {
		t.Fatalf("deferred device state check should accept an unbound final state: %v", err)
	}
}

func insertGatewayMembershipSchemaDevice(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID, kind string, deleted bool) string {
	t.Helper()
	row := validDeviceIdentitySchemaRow(tenantID)
	row.deviceNumber = deviceIdentityString("gateway-device-" + uuid.NewString())
	row.name = deviceIdentityString("gateway fixture")
	row.deviceKind = deviceIdentityString(kind)
	if deleted {
		row.deletedBy = deviceIdentityString("gateway-schema-test")
	}
	if err := insertDeviceIdentitySchemaRow(ctx, tx, row); err != nil {
		t.Fatalf("insert %s device fixture: %v", kind, err)
	}
	if deleted {
		if _, err := tx.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, row.id); err != nil {
			t.Fatalf("soft-delete %s fixture: %v", kind, err)
		}
	}
	return row.id
}

func insertGatewayMembershipSchemaRow(ctx context.Context, tx *sqldb.Tx, tenantID, childID, gatewayID, address, actor string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO gateway_memberships(tenant_id, child_device_id, gateway_device_id, subdevice_address, updated_by)
		VALUES($1,$2,$3,$4,$5)
	`, tenantID, childID, gatewayID, address, actor)
	return err
}
