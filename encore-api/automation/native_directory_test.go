package automation

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"encore.app/devicemetadata"
	"encore.app/internal/tenantidentity"
	"encore.dev/et"
)

func TestNativeDirectoryUsesOwnerRPCSelectors(t *testing.T) {
	var received []*devicemetadata.ResolveAutomationDirectoryRequest
	et.MockEndpoint(devicemetadata.ResolveAutomationDirectory, func(_ context.Context, req *devicemetadata.ResolveAutomationDirectoryRequest) (*devicemetadata.ResolveAutomationDirectoryResponse, error) {
		received = append(received, req)
		return &devicemetadata.ResolveAutomationDirectoryResponse{Complete: true}, nil
	})
	d := nativeDirectory{}
	ctx := context.Background()
	_, e1 := d.ByIDs(ctx, "63a4b529", []string{"device"})
	_, e2 := d.ByDeviceConfig(ctx, "63a4b529", []string{"template"})
	_, e3 := d.ByLabels(ctx, "63a4b529", []string{"floor=1"}, true)
	_, e4 := d.ByType(ctx, "63a4b529", "standalone")
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		t.Fatalf("RPC errors: %v %v %v %v", e1, e2, e3, e4)
	}
	want := []*devicemetadata.ResolveAutomationDirectoryRequest{
		{TenantID: "63a4b529", Selector: "ids", IDs: []string{"device"}},
		{TenantID: "63a4b529", Selector: "templates", IDs: []string{"template"}},
		{TenantID: "63a4b529", Selector: "labels", Labels: []string{"floor=1"}, MatchAll: true},
		{TenantID: "63a4b529", Selector: "kind", DeviceKind: "standalone"},
	}
	if !reflect.DeepEqual(received, want) {
		t.Fatalf("owner selectors: %+v", received)
	}
}

func TestNativeDirectoryPreservesImportedIdentityAndTemplate(t *testing.T) {
	tenant := "63a4b529"
	template := "f5ae7dfb-fa67-4e36-99b5-f00b0d36ac9d"
	response := &devicemetadata.ResolveAutomationDirectoryResponse{Complete: true, Devices: []devicemetadata.NativeAutomationDirectoryDevice{{
		ID: "3246c612-8e97-44f9-95aa-92f5f6b661c4", TenantID: tenantidentity.NormalizePrincipalID(tenant), TemplateID: &template,
		Labels: json.RawMessage(`{"floor":"1"}`), DeviceKind: "standalone",
	}}}
	devices, err := nativeDirectoryDevices(tenant, response)
	if err != nil || len(devices) != 1 || devices[0].ID != response.Devices[0].ID || devices[0].TenantID != tenant || devices[0].DeviceConfigID != template || devices[0].Label != "floor=1" {
		t.Fatalf("native directory mapping: %+v %v", devices, err)
	}
	response.Complete = false
	if _, err := nativeDirectoryDevices(tenant, response); err == nil {
		t.Fatal("partial directory must not appear complete")
	}
	response.Complete = true
	response.Devices[0].TenantID = tenantidentity.NormalizePrincipalID("other-tenant")
	if _, err := nativeDirectoryDevices(tenant, response); err == nil {
		t.Fatal("cross-tenant directory accepted")
	}
}

func TestNativeDirectoryDoesNotTruncateCompleteOwnerResults(t *testing.T) {
	response := &devicemetadata.ResolveAutomationDirectoryResponse{Complete: true, Devices: make([]devicemetadata.NativeAutomationDirectoryDevice, 10000)}
	for i := range response.Devices {
		response.Devices[i] = devicemetadata.NativeAutomationDirectoryDevice{ID: "native-device", TenantID: tenantidentity.NormalizePrincipalID("63a4b529")}
	}
	got, err := nativeDirectoryDevices("63a4b529", response)
	if err != nil || len(got) != 10000 {
		t.Fatalf("complete result truncated: count=%d err=%v", len(got), err)
	}
}
