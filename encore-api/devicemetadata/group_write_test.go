package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestCreateDeviceGroupCreatesRootChildAndReplays(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	principal := groupWritePrincipal{tenantID: tenantID, subjectID: "group-create-" + uuid.NewString(), actorID: "group-create-actor"}
	rootKey := "root-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	rootName := "root-" + uuid.NewString()
	rootReq := &CreateDeviceGroupReq{Name: rootName, Description: stringPtr("root description"), SortOrder: 4}
	first := mustCreateDeviceGroup(t, ctx, principal, rootKey, rootReq)
	rootID := first.Data.ID
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenantID, []string{rootID})
		deleteTestGroupReceipts(t, ctx, tenantID, principal.subjectID)
	})
	if first.Meta.RequestID == "" || first.Data.Name == "" || first.Data.PathComplete != true || first.Data.SortOrder != 4 {
		t.Fatalf("unexpected created root response: %#v", first)
	}
	assertCreatedGroupReadback(t, ctx, tenantID, rootID, nil, first.Data.Name)

	// Same body and scope must replay the persisted response, including identity.
	secondResult, err := createDeviceGroupIdempotently(ctx, principal, rootKey, rootReq, "replay-request")
	if err != nil {
		t.Fatalf("same-key replay failed: %v", err)
	}
	var second DeviceGroupResponse
	if err := json.Unmarshal(secondResult.response, &second); err != nil {
		t.Fatal(err)
	}
	if second.Data.ID != rootID || second.Meta.RequestID != first.Meta.RequestID {
		t.Fatalf("replay changed identity or persisted response metadata: first=%q second=%q", rootID, second.Data.ID)
	}
	var receiptCount, groupCount, auditCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, tenantID, principal.subjectID, createDeviceGroupOperation, createDeviceGroupPath, rootKey).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, rootID).Scan(&groupCount); err != nil || receiptCount != 1 || groupCount != 1 {
		t.Fatalf("expected one group and one receipt, got groups=%d receipts=%d err=%v", groupCount, receiptCount, err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name=$3 AND request_id=$4 AND outcome='succeeded' AND before_version IS NULL AND after_version=1`, tenantID, rootID, createDeviceGroupOperation, first.Meta.RequestID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("expected one transactional creation audit, got count=%d err=%v", auditCount, err)
	}

	childKey := "child-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	child := mustCreateDeviceGroup(t, ctx, principal, childKey, &CreateDeviceGroupReq{Name: "child-" + uuid.NewString(), ParentID: &rootID})
	childID := child.Data.ID
	t.Cleanup(func() { deleteTestDeviceGroups(t, ctx, tenantID, []string{childID}) })
	if child.Data.PathComplete != true || len(child.Data.Path) != 2 || child.Data.Path[0].ID != rootID {
		t.Fatalf("created child response omitted its root-to-child path: %#v", child.Data.Path)
	}
	assertCreatedGroupReadback(t, ctx, tenantID, childID, &rootID, child.Data.Name)
}

func TestCreateDeviceGroupIdempotencyConflictsIsolationAndExpiry(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	subjectA, subjectB := "same-key-a-"+uuid.NewString(), "same-key-b-"+uuid.NewString()
	key := "scope-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	principalA := groupWritePrincipal{tenantID: tenantA, subjectID: subjectA, actorID: "actor-a"}
	principalB := groupWritePrincipal{tenantID: tenantB, subjectID: subjectA, actorID: "actor-b"}
	principalOtherSubject := groupWritePrincipal{tenantID: tenantA, subjectID: subjectB, actorID: "actor-c"}
	bodyA := &CreateDeviceGroupReq{Name: "scope-a-" + uuid.NewString()}
	first := mustCreateDeviceGroup(t, ctx, principalA, key, bodyA)
	tenantBResult := mustCreateDeviceGroup(t, ctx, principalB, key, &CreateDeviceGroupReq{Name: "scope-b-" + uuid.NewString()})
	subjectBResult := mustCreateDeviceGroup(t, ctx, principalOtherSubject, key, &CreateDeviceGroupReq{Name: "scope-c-" + uuid.NewString()})
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenantA, []string{first.Data.ID, subjectBResult.Data.ID})
		deleteTestDeviceGroups(t, ctx, tenantB, []string{tenantBResult.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenantA, subjectA)
		deleteTestGroupReceipts(t, ctx, tenantA, subjectB)
		deleteTestGroupReceipts(t, ctx, tenantB, subjectA)
	})
	if first.Data.ID == tenantBResult.Data.ID || first.Data.ID == subjectBResult.Data.ID {
		t.Fatal("same key in another tenant or subject reused a resource")
	}
	if _, err := createDeviceGroupIdempotently(ctx, principalA, key, &CreateDeviceGroupReq{Name: "different-" + uuid.NewString()}, "request-different"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("same key with different request should conflict, got %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE metadata_idempotency SET replay_until=NOW()-INTERVAL '1 second' WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, tenantA, subjectA, createDeviceGroupOperation, createDeviceGroupPath, key); err != nil {
		t.Fatal(err)
	}
	if _, err := createDeviceGroupIdempotently(ctx, principalA, key, bodyA, "request-expired"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("expired key should return conflict, got %v", err)
	}
	var state string
	var responseIsNull bool
	if err := db.QueryRow(ctx, `SELECT state,response IS NULL FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, tenantA, subjectA, createDeviceGroupOperation, createDeviceGroupPath, key).Scan(&state, &responseIsNull); err != nil || state != "expired" || !responseIsNull {
		t.Fatalf("expired receipt should be a response-free tombstone, state=%q responseNull=%v err=%v", state, responseIsNull, err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantA, first.Data.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired request executed again or original group disappeared: count=%d err=%v", count, err)
	}
}

func TestCreateDeviceGroupRejectsParentsDuplicatesAndDepth(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	principalA := groupWritePrincipal{tenantID: tenantA, subjectID: "parent-a-" + uuid.NewString(), actorID: "actor-a"}
	principalB := groupWritePrincipal{tenantID: tenantB, subjectID: "parent-b-" + uuid.NewString(), actorID: "actor-b"}
	parent := mustCreateDeviceGroup(t, ctx, principalB, "parent-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "private-parent-" + uuid.NewString()})
	deletedParent := mustCreateDeviceGroup(t, ctx, principalA, "deleted-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "deleted-parent-" + uuid.NewString()})
	if _, err := db.Exec(ctx, `UPDATE device_groups SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantA, deletedParent.Data.ID); err != nil {
		t.Fatal(err)
	}
	duplicateName := "same-parent-" + uuid.NewString()
	root := mustCreateDeviceGroup(t, ctx, principalA, "root-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: duplicateName})
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenantA, []string{root.Data.ID, deletedParent.Data.ID})
		deleteTestDeviceGroups(t, ctx, tenantB, []string{parent.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenantA, principalA.subjectID)
		deleteTestGroupReceipts(t, ctx, tenantB, principalB.subjectID)
	})
	if _, err := createDeviceGroupIdempotently(ctx, principalA, "missing-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "missing-parent", ParentID: stringPtr(uuid.NewString())}, "r1"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("missing parent should be not found, got %v", err)
	}
	if _, err := createDeviceGroupIdempotently(ctx, principalA, "foreign-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "foreign-parent", ParentID: &parent.Data.ID}, "r2"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("cross-tenant parent should be hidden as not found, got %v", err)
	}
	if _, err := createDeviceGroupIdempotently(ctx, principalA, "deleted-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "deleted-parent-child", ParentID: &deletedParent.Data.ID}, "r2b"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("deleted parent should be hidden as not found, got %v", err)
	}
	if _, err := createDeviceGroupIdempotently(ctx, principalA, "dup-root-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: duplicateName}, "r3"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("duplicate root name should conflict, got %v", err)
	}
	child := mustCreateDeviceGroup(t, ctx, principalA, "child-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: duplicateName, ParentID: &root.Data.ID})
	t.Cleanup(func() { deleteTestDeviceGroups(t, ctx, tenantA, []string{child.Data.ID}) })
	if _, err := createDeviceGroupIdempotently(ctx, principalA, "dup-child-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: duplicateName, ParentID: &root.Data.ID}, "r4"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("duplicate sibling name should conflict, got %v", err)
	}

	chainTenant := testTenantID()
	chainPrincipal := groupWritePrincipal{tenantID: chainTenant, subjectID: "depth-" + uuid.NewString(), actorID: "depth-actor"}
	parents := make([]string, 0, maxDeviceGroupDepth)
	var parentID *string
	for depth := 1; depth < maxDeviceGroupDepth; depth++ {
		resp := mustCreateDeviceGroup(t, ctx, chainPrincipal, "depth-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: fmt.Sprintf("depth-%02d-%s", depth, uuid.NewString()), ParentID: parentID})
		parents = append(parents, resp.Data.ID)
		parentID = &parents[len(parents)-1]
	}
	t.Cleanup(func() {
		deleteTestDeviceGroupsReverse(t, ctx, chainTenant, parents)
		deleteTestGroupReceipts(t, ctx, chainTenant, chainPrincipal.subjectID)
	})
	depth32 := mustCreateDeviceGroup(t, ctx, chainPrincipal, "depth-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "depth-32-" + uuid.NewString(), ParentID: parentID})
	parents = append(parents, depth32.Data.ID)
	if len(depth32.Data.Path) != maxDeviceGroupDepth {
		t.Fatalf("32nd level should be accepted with full path, got %d", len(depth32.Data.Path))
	}
	if _, err := createDeviceGroupIdempotently(ctx, chainPrincipal, "depth-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "depth-33-" + uuid.NewString(), ParentID: &depth32.Data.ID}, "r5"); !hasErrCode(err, errs.InvalidArgument) {
		t.Fatalf("33rd level should be rejected, got %v", err)
	}
}

func TestCreateDeviceGroupConcurrentSameKeyCreatesOneResource(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	principal := groupWritePrincipal{tenantID: tenantID, subjectID: "concurrent-" + uuid.NewString(), actorID: "concurrent-actor"}
	key := "parallel-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	req := &CreateDeviceGroupReq{Name: "parallel-" + uuid.NewString()}
	const workers = 12
	type outcome struct {
		result *groupCreateResult
		err    error
	}
	outcomes := make(chan outcome, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := createDeviceGroupIdempotently(ctx, principal, key, req, "concurrent-request")
			outcomes <- outcome{result, err}
		}()
	}
	wg.Wait()
	close(outcomes)
	var id string
	newResults := 0
	for got := range outcomes {
		if got.err != nil {
			t.Fatalf("concurrent same-key request failed: %v", got.err)
		}
		var response DeviceGroupResponse
		if err := json.Unmarshal(got.result.response, &response); err != nil {
			t.Fatal(err)
		}
		if id == "" {
			id = response.Data.ID
		} else if response.Data.ID != id {
			t.Fatalf("concurrent replay returned different IDs: %q vs %q", id, response.Data.ID)
		}
		if !got.result.replayed {
			newResults++
		}
	}
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenantID, []string{id})
		deleteTestGroupReceipts(t, ctx, tenantID, principal.subjectID)
	})
	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&count); err != nil || count != 1 || newResults != 1 {
		t.Fatalf("expected one committed group and one first response, count=%d first=%d err=%v", count, newResults, err)
	}
}

func TestCreateDeviceGroupTransactionFailureLeavesNoGroupOrReceipt(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	principal := groupWritePrincipal{tenantID: tenantID, subjectID: "rollback-" + uuid.NewString(), actorID: "rollback-actor"}
	key := "rollback-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	name := "rollback-group-" + uuid.NewString()
	// The group and completed receipt are written before the audit insert. A
	// request ID beyond the frozen VARCHAR limit makes that final insert fail,
	// proving the preceding writes roll back together without test-only DDL.
	if _, err := createDeviceGroupIdempotently(ctx, principal, key, &CreateDeviceGroupReq{Name: name}, strings.Repeat("r", 129)); err == nil {
		t.Fatal("expected audit insert to fail and roll back transaction")
	}
	var groupCount, receiptCount, auditCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_groups WHERE tenant_id=$1 AND name=$2`, tenantID, name).Scan(&groupCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, tenantID, principal.subjectID, createDeviceGroupOperation, createDeviceGroupPath, key).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND operation_name=$2 AND request_id=$3`, tenantID, createDeviceGroupOperation, strings.Repeat("r", 129)).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if groupCount != 0 || receiptCount != 0 || auditCount != 0 {
		t.Fatalf("transaction failure left partial state: groups=%d receipts=%d audits=%d", groupCount, receiptCount, auditCount)
	}
}

func TestCreateDeviceGroupRawRequiresTenantAdminAndReturnsLocation(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	key := "http-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	data := &authn.Data{UserID: "group-http-" + uuid.NewString(), TenantID: tenantID, Roles: []string{"TENANT_USER"}, Scopes: []string{"group:write"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)
	request := httptest.NewRequest(http.MethodPost, createDeviceGroupPath, strings.NewReader(`{"name":"should-not-create"}`))
	request.Header.Set("Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	(&Service{}).CreateDeviceGroup(recorder, request)
	if recorder.Code != http.StatusForbidden || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("forged scope must not authorize write: status=%d content-type=%q body=%s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}

	data.Roles = []string{"TENANT_ADMIN"}
	body := `{"name":"http-created-` + uuid.NewString() + `"}`
	request = httptest.NewRequest(http.MethodPost, createDeviceGroupPath, strings.NewReader(body))
	request.Header.Set("Idempotency-Key", key)
	recorder = httptest.NewRecorder()
	(&Service{}).CreateDeviceGroup(recorder, request)
	if recorder.Code != http.StatusCreated || recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("Location") == "" || recorder.Header().Get("X-Request-Id") == "" {
		t.Fatalf("successful raw create headers mismatch: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	var response DeviceGroupResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Meta.RequestID != recorder.Header().Get("X-Request-Id") || createGroupLocation(response.Data.ID) != recorder.Header().Get("Location") {
		t.Fatalf("successful create body does not match headers: response=%#v err=%v", response, err)
	}
	request = httptest.NewRequest(http.MethodPost, createDeviceGroupPath, strings.NewReader(body))
	request.Header.Set("Idempotency-Key", key)
	recorder = httptest.NewRecorder()
	(&Service{}).CreateDeviceGroup(recorder, request)
	var replayed DeviceGroupResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if recorder.Code != http.StatusCreated || replayed.Data.ID != response.Data.ID || replayed.Meta.RequestID != response.Meta.RequestID || recorder.Header().Get("X-Request-Id") != response.Meta.RequestID {
		t.Fatalf("same-key HTTP retry should replay the persisted success, status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	data.Roles = []string{"TENANT_USER"}
	request = httptest.NewRequest(http.MethodPost, createDeviceGroupPath, strings.NewReader(body))
	request.Header.Set("Idempotency-Key", key)
	recorder = httptest.NewRecorder()
	(&Service{}).CreateDeviceGroup(recorder, request)
	if recorder.Code != http.StatusForbidden || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("replay must re-run write authorization, status=%d content-type=%q body=%s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenantID, []string{response.Data.ID})
		deleteTestGroupReceipts(t, ctx, normalizePrincipalID(tenantID), data.UserID)
	})
}

func TestCreateDeviceGroupRawMapsInvalidBodyToProblem422(t *testing.T) {
	data := &authn.Data{UserID: "group-invalid-" + uuid.NewString(), TenantID: testTenantID(), Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)
	request := httptest.NewRequest(http.MethodPost, createDeviceGroupPath, strings.NewReader(`{"name":"   "}`))
	request.Header.Set("Idempotency-Key", "invalid-"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	recorder := httptest.NewRecorder()
	(&Service{}).CreateDeviceGroup(recorder, request)
	var problem GroupProblem
	if recorder.Code != http.StatusUnprocessableEntity || recorder.Header().Get("Content-Type") != "application/problem+json" || json.Unmarshal(recorder.Body.Bytes(), &problem) != nil || problem.Status != http.StatusUnprocessableEntity || problem.RequestID == "" {
		t.Fatalf("invalid create body should be RFC9457 422, status=%d content-type=%q body=%s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func callCreateDeviceGroupHTTP(t *testing.T, principal groupWritePrincipal, key, body string, wantStatus int) DeviceGroupResponse {
	t.Helper()
	data := &authn.Data{UserID: principal.subjectID, TenantID: principal.tenantID, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)
	request := httptest.NewRequest(http.MethodPost, createDeviceGroupPath, strings.NewReader(body))
	request.Header.Set("Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	(&Service{}).CreateDeviceGroup(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("create device group status=%d want=%d body=%s", recorder.Code, wantStatus, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected JSON response, got %q", recorder.Header().Get("Content-Type"))
	}
	var response DeviceGroupResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode group response: %v: %s", err, recorder.Body.String())
	}
	if response.Meta.RequestID == "" || recorder.Header().Get("Location") != createGroupLocation(response.Data.ID) {
		t.Fatalf("response metadata or Location missing: %#v headers=%v", response, recorder.Header())
	}
	return response
}

func mustCreateDeviceGroup(t *testing.T, ctx context.Context, principal groupWritePrincipal, key string, req *CreateDeviceGroupReq) DeviceGroupResponse {
	t.Helper()
	result, err := createDeviceGroupIdempotently(ctx, principal, key, req, "test-request-"+uuid.NewString())
	if err != nil {
		t.Fatalf("create group %q: %v", req.Name, err)
	}
	var response DeviceGroupResponse
	if err := json.Unmarshal(result.response, &response); err != nil {
		t.Fatalf("decode persisted group response: %v", err)
	}
	return response
}

func assertCreatedGroupReadback(t *testing.T, ctx context.Context, tenantID, id string, parentID *string, name string) {
	t.Helper()
	var gotTenant, gotName string
	var gotParent sql.NullString
	if err := db.QueryRow(ctx, `SELECT tenant_id::text,name,parent_id::text FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&gotTenant, &gotName, &gotParent); err != nil {
		t.Fatal(err)
	}
	if gotTenant != tenantID || gotName != name || parentID == nil && gotParent.Valid || parentID != nil && (!gotParent.Valid || gotParent.String != *parentID) {
		t.Fatalf("persisted group mismatch: tenant=%q name=%q parent=%#v", gotTenant, gotName, gotParent)
	}
}

func deleteTestDeviceGroups(t *testing.T, ctx context.Context, tenantID string, ids []string) {
	t.Helper()
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, err := db.Exec(ctx, `DELETE FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND resource_kind='device_group'`, tenantID, id); err != nil {
			t.Errorf("delete test group audit: %v", err)
		}
		if _, err := db.Exec(ctx, `DELETE FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
			t.Errorf("delete test group %s: %v", id, err)
		}
	}
}

func deleteTestDeviceGroupsReverse(t *testing.T, ctx context.Context, tenantID string, ids []string) {
	t.Helper()
	for index := len(ids) - 1; index >= 0; index-- {
		deleteTestDeviceGroups(t, ctx, tenantID, []string{ids[index]})
	}
}

func deleteTestGroupReceipts(t *testing.T, ctx context.Context, tenantID, subjectID string) {
	t.Helper()
	if _, err := db.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3`, tenantID, subjectID, createDeviceGroupOperation); err != nil {
		t.Errorf("delete test idempotency receipts: %v", err)
	}
}

func hasErrCode(err error, code errs.ErrCode) bool {
	var apiErr *errs.Error
	return errors.As(err, &apiErr) && apiErr.Code == code
}

func stringPtr(value string) *string { return &value }

func requireDeviceGroupPostgres(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires isolated Encore PostgreSQL test database")
	}
}

var _ = fmt.Sprintf
