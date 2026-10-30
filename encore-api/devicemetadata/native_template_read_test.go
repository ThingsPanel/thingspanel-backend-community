package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestDecodeNativeTemplateListQueryContract(t *testing.T) {
	for _, raw := range []string{
		"page=0", "page=1000001", "pageSize=0", "pageSize=101", "deviceKind=", "deviceKind=invalid",
		"modelId=", "modelId=not-a-uuid", "administrativeState=", "administrativeState=active", "sort=", "sort=updatedAt",
		"search=" + strings.Repeat("a", 129), "protocol=" + strings.Repeat("p", 65),
	} {
		t.Run(raw, func(t *testing.T) {
			values, err := url.ParseQuery(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeNativeTemplateListQuery(values); err == nil {
				t.Fatalf("invalid template list query accepted: %q", raw)
			}
		})
	}
	values, err := url.ParseQuery("page=1&pageSize=100&search=%E8%AE%BE%E5%A4%87&protocol=&brand=&tag=")
	if err != nil {
		t.Fatal(err)
	}
	query, err := decodeNativeTemplateListQuery(values)
	if err != nil {
		t.Fatalf("valid Unicode/empty filters rejected: %v", err)
	}
	if query.Page != 1 || query.PageSize != 100 || query.Search != "设备" || query.Protocol != "" || query.Brand != "" || query.Tag != "" || !query.ProtocolSet || !query.BrandSet || !query.TagSet {
		t.Fatalf("decoded template query lost presence/value: %+v", query)
	}
	defaults, err := decodeNativeTemplateListQuery(url.Values{})
	if err != nil || defaults.Page != 1 || defaults.PageSize != 20 || defaults.Sort != "-createdAt" {
		t.Fatalf("template list defaults=%+v err=%v", defaults, err)
	}
}

func TestParseNativeTemplateReadValuesRejectsUntrustedQuery(t *testing.T) {
	for _, raw := range []string{
		"tenantId=" + uuid.NewString(), "search=a&search=b", "search=%FF", "unknown=x", "published=true",
	} {
		t.Run(raw, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v2/device-access-templates?"+raw, nil)
			if _, err := parseNativeReadValues(req, nativeTemplateReadQueryKeys); err == nil {
				t.Fatalf("untrusted query accepted: %q", raw)
			}
		})
	}
}

func TestCurrentNativeTemplateReadIdentityUsesTenantAdminAndTrustedScope(t *testing.T) {
	defer et.OverrideAuthInfo("", nil)
	tenantID := testTenantID()
	deviceID := uuid.NewString()
	for _, tc := range []struct {
		name  string
		role  string
		scope []string
		want  bool
	}{
		{name: "tenant admin with resource scope", role: "TENANT_ADMIN", scope: []string{deviceID}, want: true},
		{name: "tenant admin tenant wide", role: "TENANT_ADMIN", want: true},
		{name: "sys admin is not delegated", role: "SYS_ADMIN"},
		{name: "forged viewer scope is ignored", role: "TENANT_VIEWER", scope: []string{deviceID, "*"}},
		{name: "scope without role is ignored", scope: []string{"template:read", "*"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &authn.Data{UserID: "native-template-read-test", TenantID: tenantID, Roles: []string{tc.role}, AuthorizedDeviceIDs: tc.scope}
			et.OverrideAuthInfo(auth.UID(data.UserID), data)
			identity, err := currentNativeTemplateReadIdentity()
			if tc.want {
				if err != nil || identity.tenantID != tenantID || !reflect.DeepEqual(identity.deviceIDs, tc.scope) {
					t.Fatalf("authenticated template identity=%+v err=%v", identity, err)
				}
			} else if errs.Code(err) != errs.PermissionDenied {
				t.Fatalf("unauthorized identity=%+v err=%v, want PermissionDenied", identity, err)
			}
		})
	}
	invalid := &authn.Data{UserID: "native-template-read-invalid-scope", TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}, AuthorizedDeviceIDs: []string{"fake-scope"}}
	et.OverrideAuthInfo(auth.UID(invalid.UserID), invalid)
	if _, err := currentNativeTemplateReadIdentity(); errs.Code(err) != errs.Unauthenticated {
		t.Fatalf("invalid authenticated scope error=%v, want Unauthenticated", err)
	}
}

func TestListNativeDeviceAccessTemplatesRejectsRawQueryBeforeDB(t *testing.T) {
	defer et.OverrideAuthInfo("", nil)
	data := &authn.Data{UserID: "native-template-raw-query", TenantID: testTenantID(), Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	req := httptest.NewRequest("GET", "/api/v2/device-access-templates?tenantId="+uuid.NewString(), nil)
	response := httptest.NewRecorder()
	(&Service{}).ListNativeDeviceAccessTemplates(response, req)
	var problem GroupProblem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || response.Code != 400 || problem.Code != errs.InvalidArgument.String() {
		t.Fatalf("raw invalid query response status=%d body=%s problem=%+v err=%v", response.Code, response.Body.String(), problem, err)
	}
}

func TestNativeTemplateReadServiceTenantIsolationETagAndIncompleteShell(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	defer et.OverrideAuthInfo("", nil)
	fixture := seedNativeTemplateReadFixture(t, ctx)
	dataA := &authn.Data{UserID: "native-template-read-service-a", TenantID: fixture.tenantA, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(dataA.UserID), dataA)
	identityA, err := currentNativeTemplateReadIdentity()
	if err != nil {
		t.Fatalf("build authenticated tenant A identity: %v", err)
	}
	pageOne, err := readNativeTemplatePage(ctx, identityA, nativeTemplateListQuery{Page: 1, PageSize: 6}, "template-read-page-1")
	if err != nil {
		t.Fatalf("read first page: %v", err)
	}
	pageTwo, err := readNativeTemplatePage(ctx, identityA, nativeTemplateListQuery{Page: 2, PageSize: 6}, "template-read-page-2")
	if err != nil {
		t.Fatalf("read second page: %v", err)
	}
	if pageOne.Data.Total != 12 || pageTwo.Data.Total != 12 || pageOne.Data.Page != 1 || pageTwo.Data.Page != 2 || len(pageOne.Data.Items) != 6 || len(pageTwo.Data.Items) != 6 {
		t.Fatalf("tenant A page metadata=%+v/%+v", pageOne.Data, pageTwo.Data)
	}
	wantIDs := append([]string(nil), fixture.tenantAIDs...)
	sort.Sort(sort.Reverse(sort.StringSlice(wantIDs)))
	if got := append(templateIDs(pageOne.Data.Items), templateIDs(pageTwo.Data.Items)...); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("tenant A stable pages IDs=%v want=%v", got, wantIDs)
	}
	dataB := &authn.Data{UserID: "native-template-read-service-b", TenantID: fixture.tenantB, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(dataB.UserID), dataB)
	identityB, err := currentNativeTemplateReadIdentity()
	if err != nil {
		t.Fatalf("build authenticated tenant B identity: %v", err)
	}
	pageB, err := readNativeTemplatePage(ctx, identityB, nativeTemplateListQuery{}, "template-read-page-b")
	if err != nil || pageB.Data.Total != 3 || len(pageB.Data.Items) != 3 {
		t.Fatalf("tenant B page=%+v err=%v", pageB, err)
	}
	for _, item := range pageB.Data.Items {
		if item.TenantID != fixture.tenantB {
			t.Fatalf("tenant B list leaked template: %+v", item)
		}
	}

	templateID := fixture.tenantAIDs[0]
	projected, err := readNativeTemplate(ctx, identityA, templateID)
	if err != nil {
		t.Fatalf("read tenant A template: %v", err)
	}
	if projected.ID != templateID || projected.TenantID != fixture.tenantA || projected.CreatedBy != "native-template-read-fixture" || projected.UpdatedBy != "native-template-read-fixture" || projected.ResourceVersion != 1 || projected.DeviceCount != 0 {
		t.Fatalf("OAS template projection has unexpected identity/state: %+v", projected)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 17 {
		// The authoritative DeviceAccessTemplate schema has 17 properties: current configuration and asset identity.
		t.Fatalf("target template projection fields=%d json=%s err=%v", len(fields), encoded, err)
	}
	etag1, err := nativeTemplateETag(projected)
	if err != nil {
		t.Fatal(err)
	}
	projectedAgain, err := readNativeTemplate(ctx, identityA, templateID)
	if err != nil {
		t.Fatal(err)
	}
	etag2, err := nativeTemplateETag(projectedAgain)
	if err != nil {
		t.Fatal(err)
	}
	if etag1 != etag2 || !regexp.MustCompile(`^"sha256-[0-9a-f]{64}"$`).MatchString(etag1) {
		t.Fatalf("template ETag is not stable strong SHA-256: %q vs %q", etag1, etag2)
	}
	if _, err := readNativeTemplate(ctx, identityA, fixture.tenantBIDs[0]); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant template read error=%v, want NotFound", err)
	}
	dataC := &authn.Data{UserID: "native-template-read-service-c", TenantID: fixture.tenantC, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(dataC.UserID), dataC)
	identityC, err := currentNativeTemplateReadIdentity()
	if err != nil {
		t.Fatalf("build authenticated tenant C identity: %v", err)
	}
	if _, err := readNativeTemplate(ctx, identityC, fixture.incompleteID); errs.Code(err) != errs.Unavailable {
		t.Fatalf("incomplete S3 template read error=%v, want Unavailable", err)
	} else {
		problemResponse := httptest.NewRecorder()
		writeNativeTemplateReadProblem(problemResponse, httptest.NewRequest("GET", "/api/v2/device-access-templates/"+fixture.incompleteID, nil), "template-read-incomplete", err)
		var problem GroupProblem
		if decodeErr := json.Unmarshal(problemResponse.Body.Bytes(), &problem); decodeErr != nil || problemResponse.Code != 503 || problem.Code != errs.Unavailable.String() {
			t.Fatalf("incomplete S3 template problem status=%d body=%s problem=%+v err=%v", problemResponse.Code, problemResponse.Body.String(), problem, decodeErr)
		}
	}
}

type nativeTemplateReadFixture struct {
	tenantA, tenantB, tenantC string
	tenantAIDs, tenantBIDs    []string
	incompleteID              string
}

func seedNativeTemplateReadFixture(t *testing.T, ctx context.Context) nativeTemplateReadFixture {
	t.Helper()
	f := nativeTemplateReadFixture{tenantA: testTenantID(), tenantB: testTenantID(), tenantC: testTenantID(), tenantAIDs: make([]string, 12), tenantBIDs: make([]string, 3), incompleteID: uuid.NewString()}
	for i := range f.tenantAIDs {
		f.tenantAIDs[i] = uuid.NewString()
	}
	for i := range f.tenantBIDs {
		f.tenantBIDs[i] = uuid.NewString()
	}
	t.Cleanup(func() { cleanupNativeTemplateReadFixture(t, ctx, f) })
	tx, err := beginTenantScopedSQLTx(ctx, f.tenantA, false)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range f.tenantAIDs {
		row, err := insertAccessTemplateConfigurationTx(ctx, tx, f.tenantA, "native-template-read-fixture", &accessTemplateConfigurationRecord{
			Name: fmt.Sprintf("read-template-a-%02d-%s", i, id), Tags: []string{}, Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration), AdministrativeState: "enabled",
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed tenant A template: %v", err)
		}
		f.tenantAIDs[i] = row.ID
		if _, err := tx.Exec(ctx, `UPDATE device_access_templates SET created_at='2026-01-01T00:00:00Z' WHERE tenant_id=$1 AND id=$2`, f.tenantA, row.ID); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	for i, id := range f.tenantBIDs {
		row, err := insertAccessTemplateConfigurationTx(ctx, tx, f.tenantB, "native-template-read-fixture", &accessTemplateConfigurationRecord{
			Name: fmt.Sprintf("read-template-b-%02d-%s", i, id), Tags: []string{}, Configuration: json.RawMessage(accessTemplateStoreFixtureConfiguration), AdministrativeState: "enabled",
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed tenant B template: %v", err)
		}
		f.tenantBIDs[i] = row.ID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO device_access_templates(id,tenant_id,name) VALUES($1,$2,$3)`, f.incompleteID, f.tenantC, "incomplete-native-template-"+f.incompleteID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return f
}

func cleanupNativeTemplateReadFixture(t *testing.T, ctx context.Context, f nativeTemplateReadFixture) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Errorf("begin template read fixture cleanup: %v", err)
		return
	}
	defer tx.Rollback()
	for _, row := range []struct {
		tenant string
		ids    []string
	}{{f.tenantA, f.tenantAIDs}, {f.tenantB, f.tenantBIDs}, {f.tenantC, []string{f.incompleteID}}} {
		for _, id := range row.ids {
			if _, err := tx.Exec(ctx, `DELETE FROM device_access_templates WHERE tenant_id=$1 AND id=$2`, row.tenant, id); err != nil {
				t.Errorf("delete template fixture %s: %v", id, err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("commit template read fixture cleanup: %v", err)
	}
}

func templateIDs(items []NativeDeviceAccessTemplate) []string {
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	return ids
}
