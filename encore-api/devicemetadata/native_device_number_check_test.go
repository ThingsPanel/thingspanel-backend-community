package devicemetadata

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeDeviceNumberCheck(t *testing.T) {
	if got, err := decodeDeviceNumberCheck([]byte(`{"deviceNumber":"SN-001","excludeDeviceId":null}`)); err != nil || got.DeviceNumber != "SN-001" {
		t.Fatalf("valid: %+v %v", got, err)
	}
	for name, body := range map[string]string{
		"empty":         `{"deviceNumber":""}`,
		"too long":      `{"deviceNumber":"` + strings.Repeat("a", 129) + `"}`,
		"control char":  "{\"deviceNumber\":\"a\\u0001b\"}",
		"leading space": `{"deviceNumber":" a"}`,
		"bad exclude":   `{"deviceNumber":"a","excludeDeviceId":"nope"}`,
		"unknown field": `{"deviceNumber":"a","tenantId":"x"}`,
		"missing":       `{}`,
	} {
		if _, err := decodeDeviceNumberCheck([]byte(body)); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestCheckNativeDeviceNumberCountsDeletedAndStaysInTenant(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, otherTenant := testTenantID(), testTenantID()
	live, deleted := uuid.NewString(), uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	liveNumber, deletedNumber := "CHK-"+live[:8], "CHK-"+deleted[:8]
	for _, d := range []struct{ id, number string }{{live, liveNumber}, {deleted, deletedNumber}} {
		if err := insertNativeReadDevice(ctx, tx, tenantID, d.id, d.number, "n", "{}", true); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, deleted); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(ctx, `DELETE FROM device_access_bindings WHERE tenant_id=$1`, tenantID)
		db.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1`, tenantID)
	})
	check := func(tenant, number string, exclude *string) bool {
		got, err := checkNativeDeviceNumber(ctx, tenant, deviceNumberCheckInput{DeviceNumber: number, ExcludeDeviceID: exclude})
		if err != nil {
			t.Fatal(err)
		}
		return got.Available
	}
	if check(tenantID, liveNumber, nil) {
		t.Error("a live number must be unavailable")
	}
	if check(tenantID, deletedNumber, nil) {
		t.Error("a soft-deleted device still reserves its number")
	}
	if !check(tenantID, liveNumber, &live) {
		t.Error("the device being edited must not collide with itself")
	}
	if !check(tenantID, "CHK-fresh-"+live[:8], nil) {
		t.Error("an unused number must be available")
	}
	if !check(otherTenant, liveNumber, nil) {
		t.Error("another tenant's numbers must not leak or collide")
	}
}
