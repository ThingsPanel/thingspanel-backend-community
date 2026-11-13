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
	for _, scenario := range []string{"never-registered", "runtime-reference", "activation-attempt", "wrong-tenant", "stale-generation"} {
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
			if scenario == "runtime-reference" {
				if _, err := tx.Exec(ctx, `UPDATE device_access_bindings SET runtime_namespace='community',runtime_external_id=$2,runtime_provider_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' WHERE tenant_id=$1 AND device_id=$2`, tenant, device); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "activation-attempt" {
				activation, err := createDeviceOperationTx(ctx, tx, tenant, "tester", operationKindCredentialChange, device, "test", "test", strings.Repeat("d", 64), nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := enqueueDeviceRuntimeTx(ctx, tx, tenant, device, activation, operationKindCredentialChange, 1, map[string]any{"change": "activation"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, table := range []string{"device_runtime_outbox", "metadata_operations", "device_access_bindings", "devices"} {
					if _, err := db.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err != nil {
						t.Error(err)
					}
				}
			})
			if scenario == "wrong-tenant" {
				item.TenantID = testTenantID()
			}
			if scenario == "stale-generation" {
				item.Generation = 99
			}
			completed, err := completeUnprovisionedDeviceDeletion(ctx, item)
			if scenario == "stale-generation" {
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
			if scenario == "never-registered" {
				if !completed || !deleted || state != "succeeded" {
					t.Fatalf("not completed atomically: %v %v %s", completed, deleted, state)
				}
			} else if completed || deleted || state != "pending" {
				t.Fatalf("unsafe completion: %v %v %s", completed, deleted, state)
			}
		})
	}
}
