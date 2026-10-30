package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

type groupTestErrorDetails struct{ Secret string }

func (groupTestErrorDetails) ErrDetails() {}

func TestParseGroupReadQueryStrictly(t *testing.T) {
	for _, raw := range []string{
		"unexpected=1", "page=1&page=2", "page=", "search=%20%20", "page=%zz",
	} {
		t.Run(raw, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v2/device-groups?"+raw, nil)
			if _, err := parseGroupReadQuery(r, "page", "search"); err == nil {
				t.Fatalf("expected query %q to be rejected", raw)
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v2/device-groups?page=2&search=pump", nil)
	values, err := parseGroupReadQuery(r, "page", "search")
	if err != nil || values.Get("page") != "2" || values.Get("search") != "pump" {
		t.Fatalf("valid query should parse, got %#v err=%v", values, err)
	}
}

func TestGroupPrincipalForReadUsesSharedPolicyAndRejectsUndelegatedRoles(t *testing.T) {
	defer et.OverrideAuthInfo("", nil)
	for _, tc := range []struct {
		name  string
		role  string
		ten   string
		scope []string
		allow bool
	}{
		{name: "tenant admin", role: "TENANT_ADMIN", ten: testTenantID(), allow: true},
		{name: "tenant viewer with forged scope", role: "TENANT_VIEWER", ten: testTenantID(), scope: []string{"group:read", "*"}},
		{name: "tenant user with forged scope", role: "TENANT_USER", ten: testTenantID(), scope: []string{"group:read", "*"}},
		{name: "sys admin with tenant but no delegation", role: "SYS_ADMIN", ten: testTenantID()},
		{name: "sys admin without tenant", role: "SYS_ADMIN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &authn.Data{UserID: "group-read-policy-user", TenantID: tc.ten, Roles: []string{tc.role}, Scopes: tc.scope}
			et.OverrideAuthInfo(auth.UID(data.UserID), data)
			tenantID, err := groupPrincipalForRead()
			if tc.allow {
				if err != nil || tenantID == "" {
					t.Fatalf("trusted tenant-admin principal rejected: tenant=%q err=%v", tenantID, err)
				}
			} else if !hasErrCode(err, errs.PermissionDenied) {
				t.Fatalf("untrusted role/scope must not read groups, tenant=%q err=%v", tenantID, err)
			}
		})
	}
}

func TestGroupReadRawRejectsMalformedQueryWithProblemResponse(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{
			name:    "unknown list parameter",
			path:    "/api/v2/device-groups?tenantId=tenant-b",
			handler: (&Service{}).ListDeviceGroups,
		},
		{
			name:    "invalid boolean",
			path:    "/api/v2/device-groups?rootOnly=yes",
			handler: (&Service{}).ListDeviceGroups,
		},
		{
			name:    "conflicting parent and root filter",
			path:    "/api/v2/device-groups?parentId=" + uuid.NewString() + "&rootOnly=true",
			handler: (&Service{}).ListDeviceGroups,
		},
		{
			name:    "invalid tree depth",
			path:    "/api/v2/device-group-tree?maxDepth=deep",
			handler: (&Service{}).GetDeviceGroupTree,
		},
		{
			name:    "tree depth below minimum",
			path:    "/api/v2/device-group-tree?maxDepth=0",
			handler: (&Service{}).GetDeviceGroupTree,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			recorder := httptest.NewRecorder()
			tc.handler(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/problem+json" {
				t.Fatalf("expected application/problem+json, got %q", got)
			}
			var problem GroupProblem
			if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode Problem response: %v", err)
			}
			if problem.Status != recorder.Code || problem.RequestID == "" || problem.Instance != req.URL.Path {
				t.Fatalf("Problem must match HTTP status and identify request/path: %#v", problem)
			}
			if strings.Contains(recorder.Body.String(), "tenant-b") {
				t.Fatalf("Problem response must not echo tenant input: %s", recorder.Body.String())
			}
		})
	}
}

func TestGroupReadRawListHandlerReadsPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Encore PostgreSQL")
	}
	ctx := context.Background()
	tenantID := testTenantID()
	marker := "handler-list-" + uuid.NewString()
	groupID := insertTestDeviceGroup(t, ctx, tenantID, nil, marker)
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, groupID); err != nil {
			t.Errorf("delete test device group: %v", err)
		}
	})

	data := &authn.Data{UserID: "group-read-test-user", TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/device-groups?page=1&pageSize=5&search="+marker, nil)
	recorder := httptest.NewRecorder()
	(&Service{}).ListDeviceGroups(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json, got %q", got)
	}
	var response DeviceGroupPageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Meta.RequestID == "" || response.Data.Page != 1 || response.Data.PageSize != 5 || response.Data.Total != 1 || len(response.Data.Items) != 1 {
		t.Fatalf("unexpected page envelope: %#v", response)
	}
	if response.Data.Items[0].ID != groupID || response.Data.Items[0].Name != marker {
		t.Fatalf("handler did not return the inserted PostgreSQL row: %#v", response.Data.Items)
	}
}

func TestGroupReadRawTreeHandlerReturnsDatabaseTruncation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Encore PostgreSQL")
	}
	ctx := context.Background()
	tenantID := testTenantID()
	rootID := insertTestDeviceGroup(t, ctx, tenantID, nil, "handler-tree-root-"+uuid.NewString())
	childID := insertTestDeviceGroup(t, ctx, tenantID, &rootID, "handler-tree-child-"+uuid.NewString())
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, childID); err != nil {
			t.Errorf("delete test child group: %v", err)
		}
		if _, err := db.Exec(ctx, `DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, rootID); err != nil {
			t.Errorf("delete test root group: %v", err)
		}
	})

	data := &authn.Data{UserID: "group-tree-test-user", TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v2/device-group-tree?rootId="+rootID+"&maxDepth=1", nil)
	recorder := httptest.NewRecorder()
	(&Service{}).GetDeviceGroupTree(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("ETag"); got == "" {
		t.Fatal("expected an ETag for the tree representation")
	}
	var response GroupTreeResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Meta.RequestID == "" || !response.Data.Truncated || len(response.Data.Nodes) != 1 {
		t.Fatalf("expected one explicitly truncated root node: %#v", response)
	}
	root := response.Data.Nodes[0]
	if root.Group.ID != rootID || !root.HasMoreChildren || len(root.Children) != 0 {
		t.Fatalf("truncation must be explicit on the root node: %#v", root)
	}
}

func TestGroupTreeRejectsDatabaseCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Encore PostgreSQL")
	}
	ctx := context.Background()
	tenantID := testTenantID()
	rootID := insertTestDeviceGroup(t, ctx, tenantID, nil, "cycle-root-"+uuid.NewString())
	var childID string
	t.Cleanup(func() {
		cleanupTx, err := db.Stdlib().BeginTx(ctx, nil)
		if err != nil {
			t.Errorf("begin cycle fixture cleanup: %v", err)
			return
		}
		defer cleanupTx.Rollback()
		if _, err := cleanupTx.ExecContext(ctx, `UPDATE device_groups SET parent_id=NULL WHERE tenant_id=$1 AND id=$2`, tenantID, rootID); err != nil {
			t.Errorf("break cycle before cleanup: %v", err)
			return
		}
		if childID != "" {
			if _, err := cleanupTx.ExecContext(ctx, `DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, childID); err != nil {
				t.Errorf("delete cycle child: %v", err)
				return
			}
		}
		if _, err := cleanupTx.ExecContext(ctx, `DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, rootID); err != nil {
			t.Errorf("delete cycle root: %v", err)
			return
		}
		if err := cleanupTx.Commit(); err != nil {
			t.Errorf("commit cycle fixture cleanup: %v", err)
		}
	})
	childID = insertTestDeviceGroup(t, ctx, tenantID, &rootID, "cycle-child-"+uuid.NewString())
	if _, err := db.Exec(ctx, `UPDATE device_groups SET parent_id=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, rootID, childID); err != nil {
		t.Fatalf("create device group cycle: %v", err)
	}

	data := &authn.Data{UserID: "group-cycle-test-user", TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)

	_, err := (&Service{}).getDeviceGroupTree(ctx, &DeviceGroupTreeReq{}, "cycle-test")
	if got := errs.Code(err); got != errs.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition for cyclic group data, got %v (err=%v)", got, err)
	}
}

func TestUnknownLongGroupQueryKeyUsesBoundedProblemDetail(t *testing.T) {
	longKey := strings.Repeat("x", 4096)
	r := httptest.NewRequest(http.MethodGet, "/api/v2/device-groups?"+longKey+"=1", nil)
	_, err := parseGroupReadQuery(r, "page")
	if err == nil {
		t.Fatal("expected unknown query key to be rejected")
	}
	rr := httptest.NewRecorder()
	writeGroupReadProblem(rr, "request-long-key", r.URL.Path, err)
	var got GroupProblem
	if decodeErr := json.Unmarshal(rr.Body.Bytes(), &got); decodeErr != nil {
		t.Fatalf("decode problem: %v", decodeErr)
	}
	if got.Detail != "unsupported query parameter" || len(got.Detail) > 2048 {
		t.Fatalf("unexpected or unbounded detail: %d bytes", len(got.Detail))
	}
	if strings.Contains(rr.Body.String(), longKey) {
		t.Fatal("problem response echoed the unknown query key")
	}
}

func TestGroupReadProblemHasTopLevelFieldsAndHTTPStatus(t *testing.T) {
	rr := httptest.NewRecorder()
	writeGroupReadProblem(rr, "request-123", "/api/v2/device-groups", invalidGroupQuery("bad page"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	for _, key := range []string{"type", "title", "status", "detail", "instance", "code", "requestId"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("problem is missing top-level %q field: %s", key, rr.Body.String())
		}
	}
	if len(got) != 7 {
		t.Fatalf("problem must contain only the contracted fields, got %#v", got)
	}
	if got["type"] != "about:blank" || got["title"] != "Bad Request" || got["status"] != float64(http.StatusBadRequest) ||
		got["detail"] != "bad page" || got["instance"] != "/api/v2/device-groups" ||
		got["requestId"] != "request-123" || got["code"] != "invalid_argument" {
		t.Fatalf("unexpected problem: %#v", got)
	}
}

func TestGroupReadProblemHidesInternalDetails(t *testing.T) {
	rr := httptest.NewRecorder()
	writeGroupReadProblem(rr, "request-500", "/api/v2/device-groups", &errs.Error{
		Code: errs.Internal, Message: "database password leaked", Details: groupTestErrorDetails{Secret: "do-not-return"},
	})
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if rr.Code != http.StatusInternalServerError || got["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("expected 500 status to match HTTP response, got http=%d body=%#v", rr.Code, got)
	}
	if got["detail"] != "internal server error" || len(got) != 7 ||
		strings.Contains(rr.Body.String(), "password") || strings.Contains(rr.Body.String(), "do-not-return") {
		t.Fatalf("problem leaked internal error detail or extra fields: %s", rr.Body.String())
	}
}

func TestGroupReadSuccessKeepsRequestIDAndETagHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	payload := &DeviceGroupResponse{Meta: RequestMeta{RequestID: "request-456"}, ETag: `"sha256-test"`}
	writeGroupReadJSON(rr, "request-456", "/api/v2/device-groups/123", http.StatusOK, payload.ETag, payload)
	if rr.Header().Get("ETag") != payload.ETag {
		t.Fatalf("expected ETag response header, got %q", rr.Header().Get("ETag"))
	}
	if strings.Contains(rr.Body.String(), "ETag") {
		t.Fatalf("ETag should not be serialized into response body: %s", rr.Body.String())
	}
	var got DeviceGroupResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.Meta.RequestID != "request-456" {
		t.Fatalf("success response should carry shared requestId: %#v err=%v", got, err)
	}
}

func TestNormalizeDeviceGroupID(t *testing.T) {
	if got, err := normalizeDeviceGroupID("", true); err != nil || got != "" {
		t.Fatalf("optional empty ID should remain absent, got %q err=%v", got, err)
	}
	valid := uuid.NewString()
	if got, err := normalizeDeviceGroupID(valid, false); err != nil || got != valid {
		t.Fatalf("valid UUID should be preserved, got %q err=%v", got, err)
	}
	for _, raw := range []string{"", "not-a-uuid", " " + valid, valid + " "} {
		if _, err := normalizeDeviceGroupID(raw, false); err == nil {
			t.Fatalf("expected invalid group ID %q to be rejected", raw)
		}
	}
}

func TestRequiredTenantIDRejectsPlatformAdminWithoutDelegation(t *testing.T) {
	for _, tenantID := range []string{"", "tenant-target"} {
		data := &authn.Data{UserID: "sys-admin", TenantID: tenantID, Roles: []string{"SYS_ADMIN"}}
		if _, err := requiredTenantID(data); err == nil {
			t.Fatalf("tenant-scoped metadata accepted a platform admin with tenant_id %q but no delegation", tenantID)
		}
	}
}

func TestRequiredTenantIDAllowsOnlyTenantAdmin(t *testing.T) {
	if got, err := requiredTenantID(&authn.Data{UserID: "tenant-admin", TenantID: "tenant-a", Roles: []string{"TENANT_ADMIN"}}); err != nil || got != "tenant-a" {
		t.Fatalf("TENANT_ADMIN should retain its trusted tenant: got=%q err=%v", got, err)
	}
	for _, role := range []string{"TENANT_USER", "TENANT_VIEWER", "unknown"} {
		data := &authn.Data{UserID: "limited-user", TenantID: "tenant-a", Roles: []string{role}, Scopes: []string{"*"}}
		if _, err := requiredTenantID(data); err == nil {
			t.Errorf("%s gained tenant-admin metadata access from tenant id or wildcard scope", role)
		}
	}
}

func TestNormalizeDeviceGroupMaxDepth(t *testing.T) {
	if got, err := normalizeDeviceGroupMaxDepth(""); err != nil || got != maxDeviceGroupDepth {
		t.Fatalf("omitted depth should default to %d, got %d err=%v", maxDeviceGroupDepth, got, err)
	}
	for _, raw := range []string{"0", "-1", "33", "deep"} {
		if _, err := normalizeDeviceGroupMaxDepth(raw); err == nil {
			t.Fatalf("expected invalid maxDepth %q to be rejected", raw)
		}
	}
	for _, raw := range []string{"1", "32"} {
		if _, err := normalizeDeviceGroupMaxDepth(raw); err != nil {
			t.Fatalf("expected maxDepth %q to be accepted: %v", raw, err)
		}
	}
}

func TestNormalizeDeviceGroupPagination(t *testing.T) {
	page, pageSize, err := normalizeDeviceGroupPagination("", "")
	if err != nil || page != 1 || pageSize != 20 {
		t.Fatalf("omitted pagination should default to 1/20, got %d/%d err=%v", page, pageSize, err)
	}

	for _, tc := range []struct {
		name     string
		page     string
		pageSize string
	}{
		{name: "page zero", page: "0"},
		{name: "negative page", page: "-1"},
		{name: "page over maximum", page: "1000001"},
		{name: "page size zero", pageSize: "0"},
		{name: "page size over maximum", pageSize: "101"},
		{name: "malformed page", page: "one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := normalizeDeviceGroupPagination(tc.page, tc.pageSize); err == nil {
				t.Fatal("expected explicit invalid pagination to be rejected")
			}
		})
	}

	page, pageSize, err = normalizeDeviceGroupPagination("2", "100")
	if err != nil || page != 2 || pageSize != 100 {
		t.Fatalf("valid pagination should be preserved, got %d/%d err=%v", page, pageSize, err)
	}
}

func TestGroupTreeETagTracksReturnedTree(t *testing.T) {
	first := GroupTree{Nodes: []GroupTreeNode{{Group: DeviceGroup{ID: "root", Name: "Root"}, Children: []GroupTreeNode{}}}}
	same := GroupTree{Nodes: []GroupTreeNode{{Group: DeviceGroup{ID: "root", Name: "Root"}, Children: []GroupTreeNode{}}}}
	changed := GroupTree{Nodes: []GroupTreeNode{{Group: DeviceGroup{ID: "root", Name: "Renamed"}, Children: []GroupTreeNode{}}}}
	if got, want := groupTreeETag(first), groupTreeETag(same); got != want {
		t.Fatalf("same tree must have a stable ETag: %q != %q", got, want)
	}
	if groupTreeETag(first) == groupTreeETag(changed) {
		t.Fatal("ETag must change when the returned tree changes")
	}
}

func TestBuildGroupReadTreeOrdersAndPreservesHierarchy(t *testing.T) {
	parent := "root"
	groups := []DeviceGroup{
		{ID: "child-b", ParentID: &parent, Name: "B", SortOrder: 1},
		{ID: "root", Name: "Root"},
		{ID: "child-a", ParentID: &parent, Name: "A", SortOrder: 1},
	}
	nodes, truncated := buildGroupReadTree(groups, "", 32)
	if truncated || len(nodes) != 1 {
		t.Fatalf("expected one complete root, got nodes=%d truncated=%v", len(nodes), truncated)
	}
	children := nodes[0].Children
	if len(children) != 2 || children[0].Group.ID != "child-a" || children[1].Group.ID != "child-b" {
		t.Fatalf("children were not ordered by name and id: %#v", children)
	}
}

func TestBuildGroupReadTreeMarksDepthTruncation(t *testing.T) {
	root := DeviceGroup{ID: "root", Name: "Root"}
	parent := root.ID
	groups := []DeviceGroup{root, {ID: "child", ParentID: &parent, Name: "Child"}}
	nodes, truncated := buildGroupReadTree(groups, "", 1)
	if !truncated || len(nodes) != 1 || !nodes[0].HasMoreChildren || len(nodes[0].Children) != 0 {
		t.Fatalf("expected explicit truncation at depth one: nodes=%#v truncated=%v", nodes, truncated)
	}
}

func TestBuildGroupReadTreeCanStartAtSubtree(t *testing.T) {
	root := DeviceGroup{ID: "root", Name: "Root"}
	parent := root.ID
	groups := []DeviceGroup{root, {ID: "child", ParentID: &parent, Name: "Child"}}
	nodes, truncated := buildGroupReadTree(groups, "child", 32)
	if truncated || len(nodes) != 1 || nodes[0].Group.ID != "child" {
		t.Fatalf("expected selected subtree root, got %#v (truncated=%v)", nodes, truncated)
	}
}

func TestBuildGroupReadTreeHonorsDepth32AndDepth2Truncation(t *testing.T) {
	groups := make([]DeviceGroup, 32)
	for i := range groups {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
		var parentID *string
		if i > 0 {
			parent := groups[i-1].ID
			parentID = &parent
		}
		groups[i] = DeviceGroup{ID: id, ParentID: parentID, Name: fmt.Sprintf("level-%02d", i+1)}
	}

	complete, truncated := buildGroupReadTree(groups, "", 32)
	if truncated || len(complete) != 1 {
		t.Fatalf("exactly 32 levels should be complete: roots=%d truncated=%v", len(complete), truncated)
	}
	depth := 0
	for node := &complete[0]; node != nil; {
		depth++
		if len(node.Children) == 0 {
			break
		}
		node = &node.Children[0]
	}
	if depth != 32 || complete[0].HasMoreChildren {
		t.Fatalf("expected a complete 32-level tree, got depth=%d root=%#v", depth, complete[0])
	}

	limited, truncated := buildGroupReadTree(groups, "", 2)
	if !truncated || len(limited) != 1 || len(limited[0].Children) != 1 ||
		!limited[0].Children[0].HasMoreChildren || len(limited[0].Children[0].Children) != 0 {
		t.Fatalf("maxDepth=2 must expose truncation at level two: nodes=%#v truncated=%v", limited, truncated)
	}
}

func TestBuildGroupReadTreeIsIndependentOfInputOrder(t *testing.T) {
	rootID := "00000000-0000-4000-8000-000000000001"
	childID := "00000000-0000-4000-8000-000000000002"
	groups := []DeviceGroup{
		{ID: "00000000-0000-4000-8000-000000000004", ParentID: &childID, Name: "grandchild"},
		{ID: rootID, Name: "root"},
		{ID: "00000000-0000-4000-8000-000000000003", ParentID: &rootID, Name: "z-child", SortOrder: 2},
		{ID: childID, ParentID: &rootID, Name: "a-child", SortOrder: 1},
	}
	want, truncated := buildGroupReadTree(groups, "", 32)
	if truncated {
		t.Fatal("test tree should not be truncated")
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected tree: %v", err)
	}
	orders := [][]int{{3, 2, 1, 0}, {1, 0, 3, 2}}
	for _, order := range orders {
		permuted := make([]DeviceGroup, 0, len(order))
		for _, i := range order {
			permuted = append(permuted, groups[i])
		}
		got, gotTruncated := buildGroupReadTree(permuted, "", 32)
		gotJSON, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal permuted tree: %v", err)
		}
		if gotTruncated || string(gotJSON) != string(wantJSON) {
			t.Fatalf("tree changed with input order %v: truncated=%v got=%s want=%s", order, gotTruncated, gotJSON, wantJSON)
		}
	}
}

func TestDeviceGroupMigrationEnforcesTenantAndSiblingBoundaries(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Encore PostgreSQL")
	}
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	rootA := insertTestDeviceGroup(t, ctx, tenantA, nil, "shared-name")
	rootB := insertTestDeviceGroup(t, ctx, tenantB, nil, "shared-name")
	if rootA == rootB {
		t.Fatal("UUIDs unexpectedly collided")
	}

	if _, err := db.Exec(ctx, `INSERT INTO device_groups (id,tenant_id,parent_id,name,created_by,updated_by) VALUES ($1,$2,$3,$4,'test','test')`, uuid.NewString(), tenantB, rootA, "cross-tenant-child"); err == nil {
		t.Fatal("cross-tenant parent reference should fail")
	}
	if _, err := db.Exec(ctx, `INSERT INTO device_groups (id,tenant_id,name,created_by,updated_by) VALUES ($1,$2,$3,'test','test')`, uuid.NewString(), tenantA, "shared-name"); err == nil {
		t.Fatal("duplicate root name in the same tenant should fail")
	}
	child := insertTestDeviceGroup(t, ctx, tenantA, &rootA, "shared-name")
	if child == "" {
		t.Fatal("expected sibling-level name to be allowed")
	}
}

func TestDeviceGroupMemberMigrationRejectsCrossTenantDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Encore PostgreSQL")
	}
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	template := createPublishedDeviceTemplate(t, ctx, tenantA)
	device, err := createDevice(ctx, tenantA, &Device{DeviceTemplateID: template.ID, SN: "group-test-" + uuid.NewString()})
	if err != nil {
		t.Fatalf("create test device: %v", err)
	}
	groupB := insertTestDeviceGroup(t, ctx, tenantB, nil, "other-tenant")
	if _, err := db.Exec(ctx, `INSERT INTO device_group_members (tenant_id,group_id,device_id,created_by) VALUES ($1,$2,$3,'test')`, tenantB, groupB, device.ID); err == nil {
		t.Fatal("cross-tenant device membership should fail")
	}
}

func TestHydrateGroupReadMetadataBuildsPathsAndDeduplicatedCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Encore PostgreSQL")
	}
	ctx := context.Background()
	tenantID := testTenantID()
	template := createPublishedDeviceTemplate(t, ctx, tenantID)
	device, err := createDevice(ctx, tenantID, &Device{DeviceTemplateID: template.ID, SN: "group-path-" + uuid.NewString()})
	if err != nil {
		t.Fatalf("create test device: %v", err)
	}
	rootID := insertTestDeviceGroup(t, ctx, tenantID, nil, "root")
	childID := insertTestDeviceGroup(t, ctx, tenantID, &rootID, "child")
	if _, err := db.Exec(ctx, `INSERT INTO device_group_members (tenant_id,group_id,device_id,created_by) VALUES ($1,$2,$3,'test')`, tenantID, childID, device.ID); err != nil {
		t.Fatalf("insert device membership: %v", err)
	}
	groups := []DeviceGroup{{ID: rootID}, {ID: childID}}
	if err := hydrateGroupReadMetadata(ctx, tenantID, groups, db.Stdlib()); err != nil {
		t.Fatalf("hydrate group metadata: %v", err)
	}
	if len(groups[0].Path) != 1 || groups[0].DescendantDeviceCount != 1 || groups[0].DirectDeviceCount != 0 {
		t.Fatalf("unexpected root metadata: %#v", groups[0])
	}
	if len(groups[1].Path) != 2 || groups[1].Path[0].ID != rootID || groups[1].DirectDeviceCount != 1 {
		t.Fatalf("unexpected child metadata: %#v", groups[1])
	}
	// A device can belong to both the selected group and its child. The
	// subtree total must match an EXISTS-filtered list, not direct+descendant.
	if _, err := db.Exec(ctx, `INSERT INTO device_group_members (tenant_id,group_id,device_id,created_by) VALUES ($1,$2,$3,'test')`, tenantID, rootID, device.ID); err != nil {
		t.Fatalf("insert overlapping parent membership: %v", err)
	}
	if err := hydrateGroupReadMetadata(ctx, tenantID, groups, db.Stdlib()); err != nil {
		t.Fatal(err)
	}
	if groups[0].DirectDeviceCount != 1 || groups[0].DescendantDeviceCount != 1 || groups[0].SubtreeDeviceCount != 1 || groups[1].SubtreeDeviceCount != 1 {
		t.Fatalf("overlapping memberships must count once per subtree: %#v", groups)
	}
}

func insertTestDeviceGroup(t *testing.T, ctx context.Context, tenantID string, parentID *string, name string) string {
	t.Helper()
	id := uuid.NewString()
	var parent any
	if parentID != nil {
		parent = *parentID
	}
	if _, err := db.Exec(ctx, `INSERT INTO device_groups (id,tenant_id,parent_id,name,created_by,updated_by) VALUES ($1,$2,$3,$4,'test','test')`, id, tenantID, parent, name); err != nil {
		t.Fatalf("insert group %q: %v", name, err)
	}
	return id
}
