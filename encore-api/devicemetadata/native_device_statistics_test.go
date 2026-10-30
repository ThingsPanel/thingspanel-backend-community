package devicemetadata

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"encore.dev/beta/errs"
)

func TestNativeDeviceStatisticsMatchesListScopeAndFilters(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	fixture := seedNativeDeviceReadFixture(t, ctx)
	if _, err := db.Exec(ctx, `UPDATE devices SET administrative_state='disabled',activation_state='active',activation_time_source='source_record',activated_at=now() WHERE tenant_id=$1 AND id=$2`, fixture.tenantA, fixture.devices[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE devices SET activation_state='active',activation_time_source='source_record',activated_at=now() WHERE tenant_id=$1 AND id=$2`, fixture.tenantA, fixture.devices[2]); err != nil {
		t.Fatal(err)
	}

	query := decodeNativeDeviceStatisticsQuery(t, "sort=-createdAt")
	got, err := readNativeDeviceStatistics(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, query, "statistics-one")
	if err != nil {
		t.Fatal(err)
	}
	if got.Data.Total != 12 || got.Data.Enabled != 11 || got.Data.Disabled != 1 || got.Data.Active != 2 || got.Data.Inactive != 10 || got.Data.Ungrouped != 10 {
		t.Fatalf("unexpected tenant counts: %#v", got.Data)
	}
	if got.Data.Runtime.Online != nil || got.Data.Runtime.Offline != nil || got.Data.Runtime.Unknown != 12 || got.Data.Runtime.ObservedAt != nil || got.Data.Alarms.AffectedDevices != nil || got.Data.Alarms.ActiveAlarms != nil || got.Data.Alarms.ObservedAt != nil {
		t.Fatalf("unowned runtime/alarm values must be null with unknown denominator: %#v", got.Data)
	}
	gotAgain, err := readNativeDeviceStatistics(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, query, "different-request-id")
	if err != nil || got.ETag == "" || got.ETag != gotAgain.ETag {
		t.Fatalf("ETag should depend on metadata values and omit request ID: first=%q second=%q err=%v", got.ETag, gotAgain.ETag, err)
	}

	page, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, decodeNativeDeviceReadQuery(t, "sort=-createdAt"), "statistics-list-parity")
	if err != nil || page.Data.Total != got.Data.Total {
		t.Fatalf("statistics/list total mismatch statistics=%d list=%d err=%v", got.Data.Total, pageTotal(page), err)
	}
	scoped, err := readNativeDeviceStatistics(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA, deviceIDs: []string{fixture.devices[3], fixture.tenantBDevice}}, decodeNativeDeviceStatisticsQuery(t, ""), "statistics-resource-scope")
	if err != nil || scoped.Data.Total != 1 || scoped.Data.Ungrouped != 1 || scoped.Data.Runtime.Unknown != 1 {
		t.Fatalf("tenant/resource scope must constrain every count: %#v err=%v", scoped, err)
	}

	grouped, err := readNativeDeviceStatistics(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, decodeNativeDeviceStatisticsQuery(t, "groupId="+fixture.rootGroup+"&includeDescendants=true"), "statistics-group-subtree")
	if err != nil || grouped.Data.Total != 2 || grouped.Data.Ungrouped != 0 || grouped.Data.Runtime.Unknown != 2 {
		t.Fatalf("recursive group scope should deduplicate descendant devices: %#v err=%v", grouped, err)
	}
	ungrouped, err := readNativeDeviceStatistics(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, decodeNativeDeviceStatisticsQuery(t, "groupScope=ungrouped"), "statistics-ungrouped")
	if err != nil || ungrouped.Data.Total != 10 || ungrouped.Data.Ungrouped != 10 {
		t.Fatalf("ungrouped filter count = %#v err=%v", ungrouped, err)
	}
}

func TestNativeDeviceStatisticsRejectsUnavailableFiltersAndPagination(t *testing.T) {
	for _, raw := range []string{
		"connectivity=offline", "firmwareVersion=1.2", "alarmState=active",
	} {
		query := decodeNativeDeviceStatisticsQuery(t, raw)
		if _, err := readNativeDeviceStatistics(context.Background(), nativeDeviceReadIdentity{tenantID: "tenant"}, query, "statistics-unavailable-filter"); !hasErrCode(err, errs.Unavailable) {
			t.Errorf("filter %q error=%v, want Unavailable", raw, err)
		}
	}
	for _, raw := range []string{"page=1", "pageSize=20", "sort=name&sort=-name"} {
		r := httptest.NewRequest("GET", "/api/v2/device-statistics?"+raw, nil)
		if _, err := parseNativeDeviceStatisticsQuery(r); !hasErrCode(err, errs.InvalidArgument) {
			t.Errorf("query %q error=%v, want InvalidArgument", raw, err)
		}
	}
}

func TestNativeDeviceStatisticsResponseHasOASNullShape(t *testing.T) {
	response := NativeDeviceStatisticsResponse{
		Data: NativeDeviceStatistics{Total: 3, Enabled: 2, Disabled: 1, Active: 1, Inactive: 2, Ungrouped: 3, Runtime: NativeDeviceRuntimeStats{Unknown: 3}},
		Meta: RequestMeta{RequestID: "request-a"}, ETag: `"etag"`,
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(body, &shape); err != nil {
		t.Fatal(err)
	}
	data := shape["data"].(map[string]any)
	runtime := data["runtime"].(map[string]any)
	alarms := data["alarms"].(map[string]any)
	if _, ok := data["ungrouped"]; !ok || runtime["online"] != nil || runtime["offline"] != nil || runtime["observedAt"] != nil || alarms["affectedDevices"] != nil || alarms["activeAlarms"] != nil || alarms["observedAt"] != nil {
		t.Fatalf("response null shape does not match OAS: %s", body)
	}
	if _, ok := shape["etag"]; ok {
		t.Fatalf("ETag must be a header, not a JSON field: %s", body)
	}
}

func decodeNativeDeviceStatisticsQuery(t *testing.T, raw string) nativeDeviceListQuery {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	query, err := decodeNativeDeviceListQuery(values)
	if err != nil {
		t.Fatal(err)
	}
	return query
}
