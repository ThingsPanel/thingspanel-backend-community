package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestCancelledOTATaskCanRetry(t *testing.T) {
	if !canRetryOTAStatus(6) {
		t.Fatal("cancelled task must allow retry")
	}
	if !canRetryOTAStatus(5) {
		t.Fatal("failed task must allow retry")
	}
	if canRetryOTAStatus(4) {
		t.Fatal("successful task must not retry")
	}
}

func TestOtaUnfinishedCheckExcludesCurrentTask(t *testing.T) {
	if shouldBlockForUnfinishedOTA(0) {
		t.Fatal("the current newly-created task must not be counted as unfinished previous upgrade")
	}
	if !shouldBlockForUnfinishedOTA(1) {
		t.Fatal("another in-progress task must block")
	}
}

func TestOtaPackageLookupUsesPackageIDNotTaskID(t *testing.T) {
	got := resolveOTAPackageID("task-id", "package-id")
	if got != "package-id" {
		t.Fatalf("got %s", got)
	}
}

func TestActivatePreRegisterRejectsAlreadyActive(t *testing.T) {
	_, _, err := preRegisterActivateState("active")
	if err == nil {
		t.Fatal("already active devices must not be activated again")
	}
}

func TestActivatePreRegisterSetsActiveAndEnabled(t *testing.T) {
	flag, enabled, err := preRegisterActivateState("inactive")
	if err != nil {
		t.Fatal(err)
	}
	if flag != "active" || enabled != "enabled" {
		t.Fatalf("got flag=%s enabled=%s", flag, enabled)
	}
}

func TestEnabledFilterIncludesBlankIsEnabled(t *testing.T) {
	got := enabledStatuses("enabled")
	if len(got) != 2 || got[0] != "enabled" || got[1] != "" {
		t.Fatalf("OTA device picker must include active devices with blank is_enabled, got %#v", got)
	}
	got = enabledStatuses("disabled")
	if len(got) != 1 || got[0] != "disabled" {
		t.Fatalf("got %#v", got)
	}
}

func TestConfigAssociationListIncludesInactiveDevices(t *testing.T) {
	if filterActiveDevicesOnly(true) {
		t.Fatal("device config association list must include inactive bound devices")
	}
	if !filterActiveDevicesOnly(false) {
		t.Fatal("default device list still filters to active")
	}
}

func TestSysAdminSkipsDeviceConfigTenantFilter(t *testing.T) {
	if !skipTenantFilter("SYS_ADMIN") {
		t.Fatal("SYS_ADMIN must see all tenant device configs in OTA dropdown")
	}
	if skipTenantFilter("TENANT_ADMIN") {
		t.Fatal("TENANT_ADMIN must stay scoped to its own tenant")
	}
}

func TestGuardDeleteProductBlocksActiveDevices(t *testing.T) {
	cascade, err := guardDeleteProduct(1, 2)
	if err == nil {
		t.Fatal("expected delete to be blocked when active devices exist")
	}
	if cascade {
		t.Fatal("must not cascade-delete when active devices exist")
	}
	if err.Error() != errProductHasActiveDevices.Error() {
		t.Fatalf("got %v", err)
	}
}

func TestGuardDeleteProductCascadesInactiveDevices(t *testing.T) {
	cascade, err := guardDeleteProduct(0, 3)
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if !cascade {
		t.Fatal("expected inactive pre-register devices to be deleted first")
	}
}

func TestMapDeviceInsertErrorUsesDeviceNumberNotDatabaseError(t *testing.T) {
	raw := errors.New(`ERROR: duplicate key value violates unique constraint "devices_unique" (SQLSTATE 23505)`)
	err := mapDeviceInsertError(raw, "testA001")
	if err == nil || err.Error() != "设备编号 testA001 已存在，请修改模板后重试" {
		t.Fatalf("got %v", err)
	}
}

func TestParsePreRegisterExcelReadsOfficialTemplateColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "batch_template.xlsx")
	xlsx := excelize.NewFile()
	_ = xlsx.SetSheetRow("Sheet1", "A1", &[]string{"deviceNumber", "voucher", "deviceName", "lable"})
	_ = xlsx.SetSheetRow("Sheet1", "A2", &[]string{"testA001", `{"username":"18324866520401"}`, "test001", "testDevice"})
	if err := xlsx.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	filesDir := filepath.Join(dir, "files", "importBatch")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(filesDir, "batch.xlsx")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	rows, err := parsePreRegisterExcel("./files/importBatch/batch.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].DeviceNumber != "testA001" || rows[0].Name != "test001" || rows[0].Label != "testDevice" {
		t.Fatalf("got %+v", rows)
	}
}
