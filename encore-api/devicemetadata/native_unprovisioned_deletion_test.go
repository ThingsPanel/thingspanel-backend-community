package devicemetadata

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestUnprovisionedDeletionRequiresProofAndPreservesTenantBoundary(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	for _, scenario := range []string{"never-registered", "unprovisioned-child", "unprovisioned-grandchild", "runtime-reference", "activation-attempt", "wrong-tenant", "stale-generation", "child-stale-generation", "registered-parent", "registered-grandparent", "parent-activation-attempt", "child-binding-attempt", "gateway-with-child", "missing-binding"} {
		t.Run(scenario, func(t *testing.T) {
			tenant, device := testTenantID(), uuid.NewString()
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := insertNativeReadDevice(ctx, tx, tenant, device, "DEL-"+device[:8], "unprovisioned fixture", "{}", true); err != nil {
				t.Fatal(err)
			}
			var parent, grandparent string
			edges := 0
			if strings.Contains(scenario, "child") || strings.Contains(scenario, "parent") {
				parent = uuid.NewString()
				if err := insertNativeReadDevice(ctx, tx, tenant, parent, "GW-"+parent[:8], "unprovisioned gateway", "{}", true); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `UPDATE devices SET device_kind=CASE WHEN id=$2 THEN 'subdevice' ELSE 'gateway' END WHERE tenant_id=$1`, tenant, device); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `INSERT INTO gateway_memberships(tenant_id,child_device_id,gateway_device_id,subdevice_address,updated_by) VALUES($1,$2,$3,'01','tester')`, tenant, device, parent); err != nil {
					t.Fatal(err)
				}
				edges++
				if strings.Contains(scenario, "grand") {
					grandparent = uuid.NewString()
					if err := insertNativeReadDevice(ctx, tx, tenant, grandparent, "GW-"+grandparent[:8], "ancestor gateway", "{}", true); err != nil {
						t.Fatal(err)
					}
					if _, err := tx.Exec(ctx, `UPDATE devices SET device_kind='gateway' WHERE tenant_id=$1 AND id=$2`, tenant, grandparent); err != nil {
						t.Fatal(err)
					}
					if _, err := tx.Exec(ctx, `INSERT INTO gateway_memberships(tenant_id,child_device_id,gateway_device_id,subdevice_address,updated_by) VALUES($1,$2,$3,'02','tester')`, tenant, parent, grandparent); err != nil {
						t.Fatal(err)
					}
					edges++
				}
				if scenario == "gateway-with-child" {
					// Delete the parent instead: a pending operation is not authority
					// to discard children that now exist.
					device, parent = parent, device
				}
			}
			op, err := createDeviceOperationTx(ctx, tx, tenant, "tester", operationKindDeviceDeletion, device, "test", "test", strings.Repeat("c", 64), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := enqueueDeviceRuntimeTx(ctx, tx, tenant, device, op, operationKindDeviceDeletion, 1, map[string]any{"deviceId": device}); err != nil {
				t.Fatal(err)
			}
			item := runtimeOutboxItem{TenantID: tenant, DeviceID: device, Kind: operationKindDeviceDeletion, Generation: 1}
			if err := tx.QueryRow(ctx, `SELECT id::text,desired FROM device_runtime_outbox WHERE operation_id=$1`, op).Scan(&item.EventID, &item.Desired); err != nil {
				t.Fatal(err)
			}
			if scenario == "runtime-reference" || scenario == "registered-parent" || scenario == "registered-grandparent" {
				registered := device
				if scenario == "registered-parent" {
					registered = parent
				} else if scenario == "registered-grandparent" {
					registered = grandparent
				}
				if _, err := tx.Exec(ctx, `UPDATE device_access_bindings SET runtime_namespace='community',runtime_external_id=$2,runtime_provider_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' WHERE tenant_id=$1 AND device_id=$2`, tenant, registered); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "activation-attempt" || scenario == "parent-activation-attempt" || scenario == "child-binding-attempt" {
				attempted, kind := device, operationKindCredentialChange
				if scenario == "parent-activation-attempt" {
					attempted = parent
				} else if scenario == "child-binding-attempt" {
					kind = operationKindGatewayBinding
				}
				activation, err := createDeviceOperationTx(ctx, tx, tenant, "tester", kind, attempted, "test", "test", strings.Repeat("d", 64), nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := enqueueDeviceRuntimeTx(ctx, tx, tenant, attempted, activation, kind, 1, map[string]any{"change": "activation"}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "missing-binding" {
				if _, err := tx.Exec(ctx, `DELETE FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2`, tenant, device); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, table := range []string{"gateway_memberships", "device_runtime_outbox", "metadata_operations", "device_access_bindings", "devices"} {
					if _, err := db.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
						t.Error(err)
					}
				}
			})
			if scenario == "wrong-tenant" {
				item.TenantID = testTenantID()
			}
			if strings.HasSuffix(scenario, "stale-generation") {
				item.Generation = 99
			}
			completed, err := completeUnprovisionedDeviceDeletion(ctx, item)
			if strings.HasSuffix(scenario, "stale-generation") {
				if err == nil {
					t.Fatal("stale event was accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var state string
			var deleted bool
			if err := db.QueryRow(ctx, `SELECT state FROM metadata_operations WHERE tenant_id=$1 AND id=$2`, tenant, op).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM devices WHERE tenant_id=$1 AND id=$2`, tenant, device).Scan(&deleted); err != nil {
				t.Fatal(err)
			}
			success := scenario == "never-registered" || scenario == "unprovisioned-child" || scenario == "unprovisioned-grandchild"
			if success {
				if !completed || !deleted || state != "succeeded" {
					t.Fatalf("not completed atomically: %v %v %s", completed, deleted, state)
				}
			} else if completed || deleted || state != "pending" {
				t.Fatalf("unsafe completion: %v %v %s", completed, deleted, state)
			}
			if success && parent != "" {
				edges--
			}
			var actualEdges int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM gateway_memberships WHERE tenant_id=$1`, tenant).Scan(&actualEdges); err != nil || actualEdges != edges {
				t.Fatalf("edge removal not atomic: got %d want %d err=%v", actualEdges, edges, err)
			}
			if parent != "" {
				var parentDeleted bool
				if err := db.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM devices WHERE tenant_id=$1 AND id=$2`, tenant, parent).Scan(&parentDeleted); err != nil || parentDeleted {
					t.Fatalf("unrelated parent/child was deleted: %v %v", parentDeleted, err)
				}
			}
		})
	}
}
