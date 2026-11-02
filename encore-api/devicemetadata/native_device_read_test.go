package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

func TestReadNativeDevicePageAndGroupMemberPage(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	fixture := seedNativeDeviceReadFixture(t, ctx)

	pageOne := decodeNativeDeviceReadQuery(t, "page=1&pageSize=7&sort=-createdAt")
	got, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, pageOne, "native-read-page-1")
	if err != nil {
		t.Fatalf("read page one: %v", err)
	}
	if got.Data.Total != 12 || len(got.Data.Items) != 7 || got.Data.Page != 1 || got.Data.PageSize != 7 {
		t.Fatalf("page one metadata total=%d items=%d page=%d size=%d", got.Data.Total, len(got.Data.Items), got.Data.Page, got.Data.PageSize)
	}
	wantPageOne := []string{fixture.devices[11], fixture.devices[10], fixture.devices[9], fixture.devices[8], fixture.devices[7], fixture.devices[6], fixture.devices[5]}
	if gotIDs := nativeReadDeviceIDs(got.Data.Items); !reflect.DeepEqual(gotIDs, wantPageOne) {
		t.Fatalf("stable page one IDs = %v, want %v", gotIDs, wantPageOne)
	}
	pageTwo := decodeNativeDeviceReadQuery(t, "page=2&pageSize=7&sort=-createdAt")
	gotTwo, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, pageTwo, "native-read-page-2")
	if err != nil {
		t.Fatalf("read page two: %v", err)
	}
	wantPageTwo := []string{fixture.devices[4], fixture.devices[3], fixture.devices[2], fixture.devices[1], fixture.devices[0]}
	if gotTwo.Data.Total != 12 || !reflect.DeepEqual(nativeReadDeviceIDs(gotTwo.Data.Items), wantPageTwo) {
		t.Fatalf("stable page two total=%d IDs=%v, want total=12 IDs=%v", gotTwo.Data.Total, nativeReadDeviceIDs(gotTwo.Data.Items), wantPageTwo)
	}

	for _, tc := range []struct {
		query string
		want  int64
	}{
		{"search=Needle", 1},
		{"label=zone%3Dnorth", 6},
		{"hasTemplate=false", 12},
		{"groupScope=ungrouped", 10},
	} {
		filtered, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, decodeNativeDeviceReadQuery(t, tc.query), "native-read-filter")
		if err != nil || filtered.Data.Total != tc.want {
			t.Errorf("filter %q total=%v err=%v, want %d", tc.query, pageTotal(filtered), err, tc.want)
		}
	}

	// The identity's device scope and tenant fence are applied before count/page.
	scoped, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA, deviceIDs: []string{fixture.devices[3], fixture.tenantBDevice}}, decodeNativeDeviceReadQuery(t, "pageSize=100"), "native-read-scope")
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Data.Total != 1 || len(scoped.Data.Items) != 1 || scoped.Data.Items[0].ID != fixture.devices[3] {
		t.Fatalf("tenant and resource scope not applied before count: %#v", scoped.Data)
	}

	rootQuery := decodeNativeGroupMemberReadQuery(t, "page=1&pageSize=10&includeDescendants=true")
	members, err := readNativeGroupMemberPage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.rootGroup, rootQuery, "native-group-members")
	if err != nil {
		t.Fatalf("read recursive group members: %v", err)
	}
	if members.Data.Total != 2 || len(members.Data.Items) != 2 {
		t.Fatalf("recursive members total=%d items=%d, want 2", members.Data.Total, len(members.Data.Items))
	}
	byID := make(map[string]NativeGroupMember, len(members.Data.Items))
	for _, member := range members.Data.Items {
		byID[member.Device.ID] = member
	}
	direct := byID[fixture.devices[0]]
	if !direct.DirectMembership || !reflect.DeepEqual(direct.MatchedGroupIDs, fixture.groupIDs) {
		t.Fatalf("direct+descendant membership projection = direct:%v groups:%v, want direct and unique groups %v", direct.DirectMembership, direct.MatchedGroupIDs, fixture.groupIDs)
	}
	indirect := byID[fixture.devices[1]]
	if indirect.DirectMembership || !reflect.DeepEqual(indirect.MatchedGroupIDs, []string{fixture.childGroup}) {
		t.Fatalf("descendant-only membership projection = direct:%v groups:%v", indirect.DirectMembership, indirect.MatchedGroupIDs)
	}

	directOnly, err := readNativeGroupMemberPage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.rootGroup, decodeNativeGroupMemberReadQuery(t, "pageSize=1&includeDescendants=false"), "native-group-direct-only")
	if err != nil || directOnly.Data.Total != 1 || len(directOnly.Data.Items) != 1 || directOnly.Data.Items[0].Device.ID != fixture.devices[0] || !directOnly.Data.Items[0].DirectMembership || !reflect.DeepEqual(directOnly.Data.Items[0].MatchedGroupIDs, []string{fixture.rootGroup}) {
		t.Fatalf("direct-only member projection = %#v err=%v", directOnly, err)
	}
	memberPageTwo, err := readNativeGroupMemberPage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.rootGroup, decodeNativeGroupMemberReadQuery(t, "page=2&pageSize=1&includeDescendants=true"), "native-group-member-page-2")
	if err != nil || memberPageTwo.Data.Total != 2 || len(memberPageTwo.Data.Items) != 1 || memberPageTwo.Data.Items[0].Device.ID != fixture.devices[0] {
		t.Fatalf("member page two = %#v err=%v", memberPageTwo, err)
	}

	allGroupsPage1, err := readGroupsForNativeDevice(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.devices[0], 1, 1, "native-device-groups-page-1")
	if err != nil || allGroupsPage1.Data.Total != 2 || len(allGroupsPage1.Data.Items) != 1 {
		t.Fatalf("reverse group page 1 = %#v err=%v", allGroupsPage1, err)
	}
	allGroupsPage2, err := readGroupsForNativeDevice(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.devices[0], 2, 1, "native-device-groups-page-2")
	if err != nil || allGroupsPage2.Data.Total != 2 || len(allGroupsPage2.Data.Items) != 1 || allGroupsPage1.Data.Items[0].ID == allGroupsPage2.Data.Items[0].ID {
		t.Fatalf("reverse group page 2 = %#v err=%v", allGroupsPage2, err)
	}
	groupIDsDescending := []string{fixture.groupIDs[1], fixture.groupIDs[0]}
	if allGroupsPage1.Data.Items[0].ID != groupIDsDescending[0] || allGroupsPage2.Data.Items[0].ID != groupIDsDescending[1] {
		t.Fatalf("reverse group ID tie-break page1=%s page2=%s want=%v", allGroupsPage1.Data.Items[0].ID, allGroupsPage2.Data.Items[0].ID, groupIDsDescending)
	}
	scopedGroups, err := readGroupsForNativeDevice(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA, deviceIDs: []string{fixture.devices[0]}}, fixture.devices[0], 1, 10, "native-device-groups-scoped")
	if err != nil || scopedGroups.Data.Total != 2 || len(scopedGroups.Data.Items) != 2 {
		t.Fatalf("scoped reverse groups = %#v err=%v", scopedGroups, err)
	}
	for _, group := range scopedGroups.Data.Items {
		if group.DirectDeviceCount != 1 || group.DescendantDeviceCount > 1 {
			t.Fatalf("group %s leaked out-of-scope member counts: direct=%d descendant=%d", group.ID, group.DirectDeviceCount, group.DescendantDeviceCount)
		}
	}
	unscopedGroups, err := readGroupsForNativeDevice(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.devices[0], 1, 10, "native-device-groups-unscoped")
	if err != nil {
		t.Fatal(err)
	}
	var childCount, rootDescendantCount int
	for _, group := range unscopedGroups.Data.Items {
		if group.ID == fixture.childGroup {
			childCount = group.DirectDeviceCount
		}
		if group.ID == fixture.rootGroup {
			rootDescendantCount = group.DescendantDeviceCount
		}
	}
	if childCount != 2 || rootDescendantCount != 2 {
		t.Fatalf("unscoped control counts child=%d root-descendants=%d, want both 2", childCount, rootDescendantCount)
	}
	if _, err := readGroupsForNativeDevice(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.tenantBDevice, 1, 10, "native-device-groups-cross-tenant"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("cross-tenant device error=%v, want NotFound", err)
	}
	if _, err := readGroupsForNativeDevice(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA, deviceIDs: []string{fixture.devices[0]}}, fixture.devices[1], 1, 10, "native-device-groups-out-of-scope"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("out-of-scope device error=%v, want NotFound", err)
	}

	successRecorder := httptest.NewRecorder()
	successPayload := *scopedGroups
	successPayload.Meta.RequestID = "native-read-header-ok"
	writeGroupReadJSON(successRecorder, "native-read-header-ok", "/api/v2/devices/id/groups", 200, "", &successPayload)
	if successRecorder.Code != 200 || successRecorder.Header().Get("Content-Type") != "application/json" || successRecorder.Header().Get("X-Request-Id") != "native-read-header-ok" {
		t.Fatalf("success response headers/status: status=%d headers=%v", successRecorder.Code, successRecorder.Header())
	}
	var successBody DeviceGroupPageResponse
	if err := json.Unmarshal(successRecorder.Body.Bytes(), &successBody); err != nil || successBody.Meta.RequestID != "native-read-header-ok" {
		t.Fatalf("success body request ID mismatch response=%#v err=%v", successBody, err)
	}
	problemRecorder := httptest.NewRecorder()
	writeGroupReadProblem(problemRecorder, "native-read-header-problem", "/api/v2/devices/missing/groups", &errs.Error{Code: errs.NotFound, Message: "device not found"})
	var problem GroupProblem
	if err := json.Unmarshal(problemRecorder.Body.Bytes(), &problem); err != nil || problemRecorder.Code != 404 || problemRecorder.Header().Get("Content-Type") != "application/problem+json" || problemRecorder.Header().Get("X-Request-Id") != "native-read-header-problem" || problem.RequestID != "native-read-header-problem" {
		t.Fatalf("problem response status=%d headers=%v body=%#v err=%v", problemRecorder.Code, problemRecorder.Header(), problem, err)
	}

	if _, err := readNativeGroupMemberPage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, uuid.NewString(), rootQuery, "native-group-missing"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("missing group error = %v, want NotFound", err)
	}
	if _, err := readNativeGroupMemberPage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, fixture.tenantBGroup, rootQuery, "native-group-cross-tenant"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("cross-tenant group error = %v, want NotFound", err)
	}

	for _, query := range []string{"connectivity=offline", "firmwareVersion=1.2", "alarmState=active"} {
		if _, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantA}, decodeNativeDeviceReadQuery(t, query), "native-unowned-filter"); !hasErrCode(err, errs.Unavailable) {
			t.Errorf("unowned filter %q error=%v, want Unavailable", query, err)
		}
	}
	for _, id := range []string{fixture.unconvertedDevice, fixture.missingBindingDevice} {
		if _, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: fixture.tenantC, deviceIDs: []string{id}}, decodeNativeDeviceReadQuery(t, ""), "native-incomplete-device"); !hasErrCode(err, errs.Unavailable) {
			t.Errorf("incomplete device %s error=%v, want Unavailable", id, err)
		}
	}
}

func TestNativeDeviceReadFiltersUseCurrentTemplateBindingSnapshotAndAccess(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	t.Cleanup(func() {
		cleanupNativeTemplateBindingMutableRows(t, ctx, tenantA)
		cleanupNativeTemplateBindingMutableRows(t, ctx, tenantB)
	})

	fixtureTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := revisionSchemaSnapshot{version: 1, canonical: true, canonicalContent: []byte(`{"fields":[]}`), semanticDigest: strings.Repeat("a", 64), resourcesDigest: strings.Repeat("b", 64)}
	modelA, _ := seedRevisionSchemaModel(t, ctx, fixtureTx, tenantA, "device-read-filter-model-a-", "PUBLISHED", []revisionSchemaSnapshot{snapshot})
	modelB, _ := seedRevisionSchemaModel(t, ctx, fixtureTx, tenantB, "device-read-filter-model-b-", "PUBLISHED", []revisionSchemaSnapshot{snapshot})
	if err := fixtureTx.Commit(); err != nil {
		t.Fatal(err)
	}

	templateAResult, err := createNativeTemplate(ctx, nativeTemplateWriteTestIdentity(tenantA), "read-filter-template-a-"+uuid.NewString(),
		nativeTemplateWriteTestBody(t, "read-filter-template-a-"+uuid.NewString(), nativeTemplateBindingConfiguration(modelA, 1)), "read-filter-template-a-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatalf("create tenant A template: %v", err)
	}
	templateA := nativeTemplateWriteTestResult(t, templateAResult)
	templateBResult, err := createNativeTemplate(ctx, nativeTemplateWriteTestIdentity(tenantB), "read-filter-template-b-"+uuid.NewString(),
		nativeTemplateWriteTestBody(t, "read-filter-template-b-"+uuid.NewString(), nativeTemplateBindingConfiguration(modelB, 1)), "read-filter-template-b-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatalf("create tenant B template: %v", err)
	}
	templateB := nativeTemplateWriteTestResult(t, templateBResult)

	boundA := createNativeTemplateBoundDevice(t, ctx, tenantA, templateA.ID, "read-filter-bound-a")
	boundB := createNativeTemplateBoundDevice(t, ctx, tenantB, templateB.ID, "read-filter-bound-b")
	directBody := nativeWriteBody(t, map[string]any{
		"name": "read-filter-direct-a", "deviceNumber": "read-filter-direct-" + uuid.NewString(), "deviceKind": "standalone",
		"directAccess": json.RawMessage(validNativeDirectAccess), "initialGroupIds": []string{},
	})
	directResult, err := createNativeDevice(ctx, nativeWriteIdentity(tenantA), "read-filter-direct-"+uuid.NewString(), directBody, "read-filter-direct-request", validateNativeDeviceConfiguration)
	if err != nil {
		t.Fatalf("create direct-MQTT device: %v", err)
	}
	direct := nativeWriteDevice(t, directResult)

	read := func(tenant, query string) *NativeDevicePageResponse {
		t.Helper()
		page, err := readNativeDevicePage(ctx, nativeDeviceReadIdentity{tenantID: tenant}, decodeNativeDeviceReadQuery(t, query), "read-current-binding-filter")
		if err != nil {
			t.Fatalf("query %q failed: %v", query, err)
		}
		return page
	}
	assertOnly := func(query string, want ...string) {
		t.Helper()
		got := read(tenantA, query)
		ids := nativeReadDeviceIDs(got.Data.Items)
		wantIDs := append([]string{}, want...)
		sort.Strings(ids)
		sort.Strings(wantIDs)
		if got.Data.Total != int64(len(wantIDs)) || !reflect.DeepEqual(ids, wantIDs) {
			t.Fatalf("query %q total/IDs=%d/%v want=%d/%v", query, got.Data.Total, ids, len(wantIDs), wantIDs)
		}
	}
	assertOnly("templateId="+templateA.ID, boundA.ID)
	assertOnly("modelId="+modelA, boundA.ID)
	assertOnly("modelId=" + modelB)
	assertOnly("protocol=mqtt", boundA.ID, direct.ID)
	if otherTenant := read(tenantB, "protocol=mqtt"); otherTenant.Data.Total != 1 || len(otherTenant.Data.Items) != 1 || otherTenant.Data.Items[0].ID != boundB.ID {
		t.Fatalf("tenant B MQTT filter leaked/missed rows: total=%d IDs=%v", otherTenant.Data.Total, nativeReadDeviceIDs(otherTenant.Data.Items))
	}
	assertOnly("templateId=" + templateB.ID)
}

func TestDecodeNativeDeviceReadQueriesStrictly(t *testing.T) {
	for _, raw := range []string{
		"unknown=1", "page=1&page=2", "page=", "pageSize=101", "page=0", "deviceKind=unknown", "hasTemplate=yes", "groupId=not-a-uuid", "label=bad%20key%3Dx", "search=%zz", "search=%FF",
	} {
		t.Run(raw, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v2/devices?"+raw, nil)
			values, err := parseNativeReadValues(r, nativeDeviceReadQueryKeys)
			if err == nil {
				_, err = decodeNativeDeviceListQuery(values)
			}
			if err == nil {
				t.Fatalf("invalid read query %q accepted", raw)
			}
		})
	}
	values, err := url.ParseQuery("search=&sort=-createdAt&page=1&pageSize=7")
	if err != nil {
		t.Fatal(err)
	}
	query, err := decodeNativeDeviceListQuery(values)
	if err != nil || query.values.Get("search") != "" || query.page != 1 || query.pageSize != 7 {
		t.Fatalf("empty search should be valid: query=%#v err=%v", query, err)
	}
	memberValues, err := url.ParseQuery("search=设备")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeNativeGroupMemberQuery(memberValues); err != nil {
		t.Fatalf("valid Unicode search rejected: %v", err)
	}
	memberValues.Set("extra", "1")
	if _, err := decodeNativeGroupMemberQuery(memberValues); err == nil {
		t.Fatal("unknown group-member query parameter accepted")
	}
}

func TestCurrentNativeDeviceReadIdentityRequiresMappedRole(t *testing.T) {
	defer et.OverrideAuthInfo("", nil)
	tenantID := testTenantID()
	allowedID := uuid.NewString()
	for _, tc := range []struct {
		name  string
		role  string
		scope []string
		allow bool
	}{
		{name: "tenant admin", role: "TENANT_ADMIN", scope: []string{allowedID}, allow: true},
		{name: "sys admin no tenant delegation", role: "SYS_ADMIN", allow: false},
		{name: "viewer forged scope", role: "TENANT_VIEWER", scope: []string{"device:read", "*"}, allow: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &authn.Data{UserID: "native-read-test-user", TenantID: tenantID, Roles: []string{tc.role}, AuthorizedDeviceIDs: tc.scope}
			et.OverrideAuthInfo(auth.UID(data.UserID), data)
			identity, err := currentNativeDeviceReadIdentity()
			if tc.allow {
				if err != nil || identity.tenantID != tenantID || !reflect.DeepEqual(identity.deviceIDs, tc.scope) {
					t.Fatalf("tenant administrator identity=%#v err=%v", identity, err)
				}
			} else if !hasErrCode(err, errs.PermissionDenied) {
				t.Fatalf("unmapped role granted native device read: identity=%#v err=%v", identity, err)
			}
		})
	}
	data := &authn.Data{UserID: "native-read-invalid-scope", TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}, AuthorizedDeviceIDs: []string{"not-a-uuid"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	if _, err := currentNativeDeviceReadIdentity(); !hasErrCode(err, errs.Unauthenticated) {
		t.Fatalf("invalid authenticated device scope error=%v, want Unauthenticated", err)
	}
}

func decodeNativeDeviceReadQuery(t *testing.T, raw string) nativeDeviceListQuery {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/v2/devices?"+raw, nil)
	values, err := parseNativeReadValues(r, nativeDeviceReadQueryKeys)
	if err != nil {
		t.Fatalf("parse device query %q: %v", raw, err)
	}
	query, err := decodeNativeDeviceListQuery(values)
	if err != nil {
		t.Fatalf("decode device query %q: %v", raw, err)
	}
	return query
}

func decodeNativeGroupMemberReadQuery(t *testing.T, raw string) nativeDeviceListQuery {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/v2/device-groups/x/members?"+raw, nil)
	values, err := parseNativeReadValues(r, []string{"page", "pageSize", "includeDescendants", "search"})
	if err != nil {
		t.Fatalf("parse member query %q: %v", raw, err)
	}
	query, err := decodeNativeGroupMemberQuery(values)
	if err != nil {
		t.Fatalf("decode member query %q: %v", raw, err)
	}
	return query
}

func nativeReadDeviceIDs(devices []NativeDevice) []string {
	ids := make([]string, len(devices))
	for i := range devices {
		ids[i] = devices[i].ID
	}
	return ids
}

func pageTotal(page *NativeDevicePageResponse) int64 {
	if page == nil {
		return -1
	}
	return page.Data.Total
}

type nativeDeviceReadFixture struct {
	tenantA, tenantB, tenantC string
	devices                   []string
	tenantBDevice             string
	rootGroup, childGroup     string
	groupIDs                  []string
	tenantBGroup              string
	unconvertedDevice         string
	missingBindingDevice      string
}

func seedNativeDeviceReadFixture(t *testing.T, ctx context.Context) nativeDeviceReadFixture {
	t.Helper()
	f := nativeDeviceReadFixture{
		tenantA: testTenantID(), tenantB: testTenantID(), tenantC: testTenantID(),
		rootGroup: uuid.NewString(), childGroup: uuid.NewString(), tenantBGroup: uuid.NewString(),
		devices: make([]string, 12), tenantBDevice: uuid.NewString(),
		unconvertedDevice: uuid.NewString(), missingBindingDevice: uuid.NewString(),
	}
	for i := range f.devices {
		f.devices[i] = uuid.NewString()
	}
	sort.Strings(f.devices)
	f.groupIDs = []string{f.rootGroup, f.childGroup}
	if f.groupIDs[0] > f.groupIDs[1] {
		f.groupIDs[0], f.groupIDs[1] = f.groupIDs[1], f.groupIDs[0]
	}
	t.Cleanup(func() { cleanupNativeDeviceReadFixture(t, ctx, f) })

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := insertNativeReadGroup(ctx, tx, f.tenantA, f.rootGroup, nil, "read-root"); err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadGroup(ctx, tx, f.tenantA, f.childGroup, &f.rootGroup, "read-child"); err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadGroup(ctx, tx, f.tenantB, f.tenantBGroup, nil, "read-other-root"); err != nil {
		t.Fatal(err)
	}
	for i, id := range f.devices {
		name := fmt.Sprintf("Device-%02d", i)
		if i == 4 {
			name = "Needle Device"
		}
		labels := `{"zone":"south"}`
		if i%2 == 0 {
			labels = `{"zone":"north"}`
		}
		if err := insertNativeReadDevice(ctx, tx, f.tenantA, id, fmt.Sprintf("read-%02d", i), name, labels, true); err != nil {
			t.Fatalf("insert device fixture %d: %v", i, err)
		}
	}
	if err := insertNativeReadGroupMember(ctx, tx, f.tenantA, f.rootGroup, f.devices[0]); err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadGroupMember(ctx, tx, f.tenantA, f.childGroup, f.devices[0]); err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadGroupMember(ctx, tx, f.tenantA, f.childGroup, f.devices[1]); err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, f.tenantB, f.tenantBDevice, "tenant-b-1", "Tenant B device", `{}`, true); err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, f.tenantC, f.unconvertedDevice, "incomplete-1", "", `{}`, true); err != nil {
		t.Fatal(err)
	}
	if err := insertNativeReadDevice(ctx, tx, f.tenantC, f.missingBindingDevice, "missing-binding-1", "Missing binding", `{}`, false); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return f
}

func insertNativeReadGroup(ctx context.Context, tx *sqldb.Tx, tenantID, id string, parent *string, name string) error {
	_, err := tx.Exec(ctx, `INSERT INTO device_groups(id,tenant_id,parent_id,name,created_by,updated_by) VALUES($1,$2,$3,$4,'native-read-fixture','native-read-fixture')`, id, tenantID, parent, name)
	return err
}

func insertNativeReadGroupMember(ctx context.Context, tx *sqldb.Tx, tenantID, groupID, deviceID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO device_group_members(tenant_id,group_id,device_id,created_by) VALUES($1,$2,$3,'native-read-fixture')`, tenantID, groupID, deviceID)
	return err
}

func insertNativeReadDevice(ctx context.Context, tx *sqldb.Tx, tenantID, id, number, name, labels string, withBinding bool) error {
	row := validDeviceIdentitySchemaRow(tenantID)
	row.id = id
	row.deviceNumber = deviceIdentityString(number)
	if name != "" {
		row.name = deviceIdentityString(name)
	} else {
		row.name = nil
	}
	row.deviceKind = deviceIdentityString("standalone")
	row.activationState = deviceIdentityString("inactive")
	row.activationTimeSource = deviceIdentityString("not_activated")
	row.createdBy = deviceIdentityString("native-read-fixture")
	row.updatedBy = deviceIdentityString("native-read-fixture")
	row.labels = labels
	if err := insertDeviceIdentitySchemaRow(ctx, tx, row); err != nil {
		return err
	}
	if withBinding {
		_, err := tx.Exec(ctx, `INSERT INTO device_access_bindings(tenant_id,device_id,updated_by) VALUES($1,$2,'native-read-fixture')`, tenantID, id)
		return err
	}
	return nil
}

func cleanupNativeDeviceReadFixture(t *testing.T, ctx context.Context, f nativeDeviceReadFixture) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Errorf("begin native read fixture cleanup: %v", err)
		return
	}
	defer tx.Rollback()
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM device_group_members WHERE tenant_id=$1`, []any{f.tenantA}},
		{`DELETE FROM device_group_members WHERE tenant_id=$1`, []any{f.tenantB}},
		{`DELETE FROM gateway_memberships WHERE tenant_id=$1`, []any{f.tenantA}},
		{`DELETE FROM gateway_memberships WHERE tenant_id=$1`, []any{f.tenantB}},
		{`DELETE FROM device_access_bindings WHERE tenant_id=$1`, []any{f.tenantA}},
		{`DELETE FROM device_access_bindings WHERE tenant_id=$1`, []any{f.tenantB}},
		{`DELETE FROM device_access_bindings WHERE tenant_id=$1`, []any{f.tenantC}},
		{`DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, []any{f.tenantA, f.childGroup}},
		{`DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, []any{f.tenantA, f.rootGroup}},
		{`DELETE FROM device_groups WHERE tenant_id=$1`, []any{f.tenantB}},
		{`DELETE FROM devices WHERE tenant_id=$1`, []any{f.tenantA}},
		{`DELETE FROM devices WHERE tenant_id=$1`, []any{f.tenantB}},
		{`DELETE FROM devices WHERE tenant_id=$1`, []any{f.tenantC}},
	} {
		if _, err := tx.Exec(ctx, query.sql, query.args...); err != nil {
			t.Errorf("cleanup native read fixture: %v", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("commit native read fixture cleanup: %v", err)
	}
}
