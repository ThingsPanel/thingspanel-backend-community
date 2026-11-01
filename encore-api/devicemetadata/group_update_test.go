package devicemetadata

import (
	"context"
	"encoding/json"
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

func TestDecodeUpdateDeviceGroupBodyStrictly(t *testing.T) {
	req, err := decodeUpdateDeviceGroupBody([]byte(`{"name":"renamed","description":null,"sortOrder":5}`))
	if err != nil || req.Name == nil || *req.Name != "renamed" || !req.DescriptionSet || string(req.Description) != "null" || req.SortOrder == nil || *req.SortOrder != 5 {
		t.Fatalf("valid update body should preserve explicit null and values: req=%#v err=%v", req, err)
	}
	for _, body := range []string{
		`null`, `[]`, `{"parentId":null}`, `{"name":null}`, `{"name":"  "}`, `{"description":1}`, `{"sortOrder":-1}`, `{"sortOrder":2147483648}`, `{"sortOrder":1.5}`, `{} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := decodeUpdateDeviceGroupBody([]byte(body)); err == nil {
				t.Fatalf("expected invalid update body to be rejected: %s", body)
			}
		})
	}
}

func TestUpdateDeviceGroupPersistsCASAuditAndIdempotentReplay(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	principal := groupWritePrincipal{tenantID: tenantID, subjectID: "group-update-" + uuid.NewString(), actorID: "group-update-actor"}
	root := mustCreateDeviceGroup(t, ctx, principal, "uproot-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "update-root-" + uuid.NewString()})
	child := mustCreateDeviceGroup(t, ctx, principal, "upchild-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "old-child-" + uuid.NewString(), Description: stringPtr("clear me"), SortOrder: 1, ParentID: &root.Data.ID})
	updatedName, siblingName := "z-child-"+uuid.NewString(), "a-sibling-"+uuid.NewString()
	sibling := mustCreateDeviceGroup(t, ctx, principal, "upsibling-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: siblingName, SortOrder: 7, ParentID: &root.Data.ID})
	if child.Data.ID > sibling.Data.ID {
		updatedName, siblingName = "a-child-"+uuid.NewString(), "z-sibling-"+uuid.NewString()
		// Rename the sibling first so the two same-order names sort opposite IDs.
		currentSibling := sibling
		newSiblingName := siblingName
		if _, err := updateDeviceGroupIdempotently(ctx, principal, currentSibling.Data.ID, "sibren-"+strings.ReplaceAll(uuid.NewString(), "-", ""), groupReadETag(currentSibling.Data), &UpdateDeviceGroupReq{Name: &newSiblingName}, "sibling-rename"); err != nil {
			t.Fatalf("rename sibling fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		deleteTestDeviceGroupsReverse(t, ctx, tenantID, []string{root.Data.ID, child.Data.ID, sibling.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenantID, principal.subjectID)
		deleteTestGroupUpdateReceipts(t, ctx, tenantID, principal.subjectID)
	})

	name := updatedName
	key := "update-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	request := &UpdateDeviceGroupReq{Name: &name, Description: json.RawMessage("null"), DescriptionSet: true, SortOrder: intPtr(7)}
	initialETag := groupReadETag(child.Data)
	first, err := updateDeviceGroupIdempotently(ctx, principal, child.Data.ID, key, initialETag, request, "update-request-1")
	if err != nil {
		t.Fatalf("update group: %v", err)
	}
	var updated DeviceGroupResponse
	if err := json.Unmarshal(first.response, &updated); err != nil {
		t.Fatal(err)
	}
	if first.status != http.StatusOK || first.etag == "" || first.etag != groupReadETag(updated.Data) || updated.Data.Name != name || updated.Data.Description != nil || updated.Data.SortOrder != 7 {
		t.Fatalf("updated response mismatch: result=%#v data=%#v", first, updated.Data)
	}
	if updated.Data.ParentID == nil || *updated.Data.ParentID != root.Data.ID || updated.Data.ResourceVersion != child.Data.ResourceVersion+1 || updated.Data.UpdatedBy != principal.actorID {
		t.Fatalf("update changed hierarchy or missed audit version/actor: %#v", updated.Data)
	}
	if len(updated.Data.Path) != 2 || updated.Data.Path[1].Name != name {
		t.Fatalf("updated name not reflected in group path: %#v", updated.Data.Path)
	}

	data := &authn.Data{UserID: principal.subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	detail, err := (&Service{}).getDeviceGroup(ctx, child.Data.ID, "detail-after-update")
	if err != nil || detail.Data.Name != name || detail.ETag != first.etag {
		t.Fatalf("detail readback did not reflect updated group: detail=%#v err=%v", detail, err)
	}
	tree, err := (&Service{}).getDeviceGroupTree(ctx, &DeviceGroupTreeReq{RootID: root.Data.ID, MaxDepth: "2"}, "tree-after-update")
	if err != nil || len(tree.Data.Nodes) != 1 || len(tree.Data.Nodes[0].Children) != 2 {
		t.Fatalf("tree readback did not include both siblings: tree=%#v err=%v", tree, err)
	}
	ordered := tree.Data.Nodes[0].Children
	if ordered[0].Group.ID > ordered[1].Group.ID {
		t.Fatalf("tree siblings with equal sortOrder must use stable ID order: %q before %q", ordered[0].Group.ID, ordered[1].Group.ID)
	}
	page, err := (&Service{}).listDeviceGroups(ctx, &ListDeviceGroupsReq{ParentID: root.Data.ID, Sort: "sortOrder"}, "list-after-update")
	if err != nil || len(page.Data.Items) != 2 || page.Data.Items[0].ID > page.Data.Items[1].ID {
		t.Fatalf("list siblings with equal sortOrder must use stable ID order: page=%#v err=%v", page, err)
	}
	et.OverrideAuthInfo("", nil)

	var auditCount int
	var auditBefore, auditAfter int64
	var auditChanges []byte
	if err := db.QueryRow(ctx, `SELECT COUNT(*),MIN(before_version),MIN(after_version),MIN(redacted_changes::text)
		FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name=$3 AND request_id=$4`,
		tenantID, child.Data.ID, updateDeviceGroupOperation, "update-request-1").Scan(&auditCount, &auditBefore, &auditAfter, &auditChanges); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || auditBefore != child.Data.ResourceVersion || auditAfter != updated.Data.ResourceVersion || strings.Contains(string(auditChanges), name) || strings.Contains(string(auditChanges), "clear me") {
		t.Fatalf("update audit must record versions without field values: count=%d before=%d after=%d changes=%s", auditCount, auditBefore, auditAfter, auditChanges)
	}

	replay, err := updateDeviceGroupIdempotently(ctx, principal, child.Data.ID, key, initialETag, request, "discarded-retry-request")
	if err != nil {
		t.Fatalf("same-key update replay: %v", err)
	}
	var replayed DeviceGroupResponse
	if err := json.Unmarshal(replay.response, &replayed); err != nil {
		t.Fatal(err)
	}
	if !replay.replayed || replay.status != http.StatusOK || replay.etag != first.etag || replayed.Data.ID != updated.Data.ID || replayed.Meta.RequestID != updated.Meta.RequestID || replayed.Data.ResourceVersion != updated.Data.ResourceVersion {
		t.Fatalf("replay must return saved response and etag: %#v %#v", replay, replayed)
	}
	if _, err := updateDeviceGroupIdempotently(ctx, principal, child.Data.ID, key, initialETag, &UpdateDeviceGroupReq{SortOrder: intPtr(8)}, "different-payload"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("same key with different body should conflict, got %v", err)
	}
	var currentVersion int64
	if err := db.QueryRow(ctx, `SELECT resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, child.Data.ID).Scan(&currentVersion); err != nil || currentVersion != updated.Data.ResourceVersion {
		t.Fatalf("replay or conflict changed group version: current=%d expected=%d err=%v", currentVersion, updated.Data.ResourceVersion, err)
	}
}

func TestUpdateDeviceGroupRejectsStaleDuplicateAndForeignTenant(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	principalA := groupWritePrincipal{tenantID: tenantA, subjectID: "update-a-" + uuid.NewString(), actorID: "actor-a"}
	principalB := groupWritePrincipal{tenantID: tenantB, subjectID: "update-b-" + uuid.NewString(), actorID: "actor-b"}
	root := mustCreateDeviceGroup(t, ctx, principalA, "root-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "root-" + uuid.NewString()})
	group := mustCreateDeviceGroup(t, ctx, principalA, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "target-" + uuid.NewString(), ParentID: &root.Data.ID})
	duplicateName := "duplicate-" + uuid.NewString()
	duplicate := mustCreateDeviceGroup(t, ctx, principalA, "duplicate-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: duplicateName, ParentID: &root.Data.ID})
	t.Cleanup(func() {
		deleteTestDeviceGroupsReverse(t, ctx, tenantA, []string{root.Data.ID, group.Data.ID, duplicate.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenantA, principalA.subjectID)
		deleteTestGroupUpdateReceipts(t, ctx, tenantA, principalA.subjectID)
	})
	newName := "fresh-" + uuid.NewString()
	staleETag := groupReadETag(group.Data)
	if _, err := updateDeviceGroupIdempotently(ctx, principalA, group.Data.ID, "first-"+strings.ReplaceAll(uuid.NewString(), "-", ""), staleETag, &UpdateDeviceGroupReq{Name: &newName}, "first-update"); err != nil {
		t.Fatalf("first update failed: %v", err)
	}
	if _, err := updateDeviceGroupIdempotently(ctx, principalA, group.Data.ID, "stale-"+strings.ReplaceAll(uuid.NewString(), "-", ""), staleETag, &UpdateDeviceGroupReq{SortOrder: intPtr(9)}, "stale-update"); !hasErrCode(err, errs.FailedPrecondition) {
		t.Fatalf("stale ETag should return failed precondition, got %v", err)
	}
	data := &authn.Data{UserID: principalA.subjectID, TenantID: tenantA, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	current, err := (&Service{}).getDeviceGroup(ctx, group.Data.ID, "read-after-stale")
	et.OverrideAuthInfo("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateDeviceGroupIdempotently(ctx, principalA, group.Data.ID, "dup-"+strings.ReplaceAll(uuid.NewString(), "-", ""), current.ETag, &UpdateDeviceGroupReq{Name: &duplicateName}, "duplicate-update"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("same-parent duplicate name should conflict, got %v", err)
	}
	if _, err := updateDeviceGroupIdempotently(ctx, principalB, group.Data.ID, "foreign-"+strings.ReplaceAll(uuid.NewString(), "-", ""), current.ETag, &UpdateDeviceGroupReq{Name: &newName}, "foreign-update"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("foreign tenant must not modify group, got %v", err)
	}
	var persistedName string
	if err := db.QueryRow(ctx, `SELECT name FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantA, group.Data.ID).Scan(&persistedName); err != nil || persistedName != newName {
		t.Fatalf("failed updates changed group name: got=%q expected=%q err=%v", persistedName, newName, err)
	}
}

func TestUpdateDeviceGroupRawPreconditionAndBodyValidation(t *testing.T) {
	tenantID := testTenantID()
	data := &authn.Data{UserID: "group-update-http-" + uuid.NewString(), TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)
	handler := (&Service{}).UpdateDeviceGroup

	request := httptest.NewRequest(http.MethodPatch, "/api/v2/device-groups/"+uuid.NewString(), strings.NewReader(`{"name":"ok"}`))
	request.Header.Set("Idempotency-Key", "raw-"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	if recorder.Code != http.StatusPreconditionRequired || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("missing If-Match must be Problem 428, got status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}

	for _, body := range []string{`{"parentId":null}`, `{"name":"ok","unexpected":true}`} {
		request = httptest.NewRequest(http.MethodPatch, "/api/v2/device-groups/"+uuid.NewString(), strings.NewReader(body))
		request.Header.Set("If-Match", `"sha256-`+strings.Repeat("a", 64)+`"`)
		request.Header.Set("Idempotency-Key", "raw-"+strings.ReplaceAll(uuid.NewString(), "-", ""))
		recorder = httptest.NewRecorder()
		handler(recorder, request)
		if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("invalid PATCH shape should be Problem 400, body=%s status=%d", body, recorder.Code)
		}
	}
	request = httptest.NewRequest(http.MethodPatch, "/api/v2/device-groups/"+uuid.NewString(), strings.NewReader(`{"sortOrder":-1}`))
	request.Header.Set("If-Match", `"sha256-`+strings.Repeat("a", 64)+`"`)
	request.Header.Set("Idempotency-Key", "raw-"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	recorder = httptest.NewRecorder()
	handler(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("invalid field value should be Problem 422, status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func TestUpdateDeviceGroupAuditFailureRollsBackResourceAndReceipt(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	principal := groupWritePrincipal{tenantID: tenantID, subjectID: "update-rollback-" + uuid.NewString(), actorID: "update-rollback-actor"}
	group := mustCreateDeviceGroup(t, ctx, principal, "rollback-create-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "rollback-before-" + uuid.NewString()})
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenantID, []string{group.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenantID, principal.subjectID)
		deleteTestGroupUpdateReceipts(t, ctx, tenantID, principal.subjectID)
	})
	newName := "rollback-after-" + uuid.NewString()
	key := "rollback-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	// The audit request_id limit fails after the group update and completed
	// receipt have been written, proving all three effects share one tx.
	if _, err := updateDeviceGroupIdempotently(ctx, principal, group.Data.ID, key, groupReadETag(group.Data), &UpdateDeviceGroupReq{Name: &newName}, strings.Repeat("r", 129)); err == nil {
		t.Fatal("expected audit insert to fail and roll back the update transaction")
	}
	var name string
	var version int64
	if err := db.QueryRow(ctx, `SELECT name,resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, group.Data.ID).Scan(&name, &version); err != nil {
		t.Fatal(err)
	}
	if name != group.Data.Name || version != group.Data.ResourceVersion {
		t.Fatalf("failed transaction changed group: name=%q version=%d original=%#v", name, version, group.Data)
	}
	var receiptCount, auditCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, tenantID, principal.subjectID, updateDeviceGroupOperation, createGroupLocation(group.Data.ID), key).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND operation_name=$2 AND resource_id=$3 AND request_id=$4`, tenantID, updateDeviceGroupOperation, group.Data.ID, strings.Repeat("r", 129)).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 0 || auditCount != 0 {
		t.Fatalf("failed transaction left receipt or audit row: receipt=%d audit=%d", receiptCount, auditCount)
	}
}

func deleteTestGroupUpdateReceipts(t *testing.T, ctx context.Context, tenantID, subjectID string) {
	t.Helper()
	if _, err := db.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3`, tenantID, subjectID, updateDeviceGroupOperation); err != nil {
		t.Errorf("delete test group update receipts: %v", err)
	}
}

func intPtr(value int) *int { return &value }
