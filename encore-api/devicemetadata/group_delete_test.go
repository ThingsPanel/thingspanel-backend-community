package devicemetadata

import (
	"context"
	"encoding/json"
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

func TestDeleteDeviceGroupEmptyLeafSoftDeletesAndReplays(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-delete-" + uuid.NewString(), actorID: "group-delete-actor"}
	group := mustCreateDeviceGroup(t, ctx, principal, "delete-create-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-empty-" + uuid.NewString()})
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenant, []string{group.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenant, principal.subjectID)
		deleteDeviceGroupReceipts(t, ctx, tenant, principal.subjectID)
	})

	key := "delete-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	etag := groupReadETag(group.Data)
	first, err := deleteDeviceGroupIdempotently(ctx, principal, group.Data.ID, key, etag, "delete-request-1")
	if err != nil {
		t.Fatal(err)
	}
	var deleted DeviceGroupResponse
	if err := json.Unmarshal(first.response, &deleted); err != nil {
		t.Fatal(err)
	}
	if first.status != 200 || first.etag != groupReadETag(deleted.Data) || deleted.Data.ResourceVersion != group.Data.ResourceVersion+1 || deleted.Data.UpdatedBy != principal.actorID || deleted.Data.DirectDeviceCount != 0 || deleted.Data.SubtreeDeviceCount != 0 {
		t.Fatalf("delete result must be the updated group snapshot: result=%#v data=%#v", first, deleted.Data)
	}
	var softDeleted bool
	if err := db.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, group.Data.ID).Scan(&softDeleted); err != nil || !softDeleted {
		t.Fatalf("empty group should be soft deleted: deleted=%v err=%v", softDeleted, err)
	}
	replay, err := deleteDeviceGroupIdempotently(ctx, principal, group.Data.ID, key, etag, "ignored-retry")
	if err != nil || !replay.replayed || replay.status != 200 || replay.etag != first.etag || string(replay.response) != string(first.response) {
		t.Fatalf("same-key replay must restore the original response after deletion: replay=%#v err=%v", replay, err)
	}
	if _, err := deleteDeviceGroupIdempotently(ctx, principal, group.Data.ID, key, `"sha256-`+strings.Repeat("a", 64)+`"`, "changed-etag"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("same key with changed If-Match must conflict, got %v", err)
	}
	var audits int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name=$3`, tenant, group.Data.ID, deleteDeviceGroupOperation).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("delete must audit exactly once: audits=%d err=%v", audits, err)
	}
	data := &authn.Data{UserID: principal.subjectID, TenantID: tenant, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	_, readErr := (&Service{}).getDeviceGroup(ctx, group.Data.ID, "after-delete")
	et.OverrideAuthInfo("", nil)
	if !hasErrCode(readErr, errs.NotFound) {
		t.Fatalf("deleted group must be hidden from GET, got %v", readErr)
	}
}

func TestDeleteDeviceGroupRejectsChildrenAndMembersWithoutDeletingDevice(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-delete-ref-" + uuid.NewString(), actorID: "group-delete-actor"}
	parent := mustCreateDeviceGroup(t, ctx, principal, "delete-parent-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-parent-" + uuid.NewString()})
	child := mustCreateDeviceGroup(t, ctx, principal, "delete-child-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-child-" + uuid.NewString(), ParentID: &parent.Data.ID})
	leaf := mustCreateDeviceGroup(t, ctx, principal, "delete-member-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-member-" + uuid.NewString()})
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	device, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "delete-member-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO device_group_members (tenant_id,group_id,device_id,created_by) VALUES ($1,$2,$3,$4)`, tenant, leaf.Data.ID, device.ID, principal.actorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deleteTestGroupReceipts(t, ctx, tenant, principal.subjectID)
		deleteDeviceGroupReceipts(t, ctx, tenant, principal.subjectID)
		cleanupMembershipScenario(t, ctx, tenant, principal, leaf.Data.ID, []string{device.ID})
		deleteTestDeviceGroupsReverse(t, ctx, tenant, []string{parent.Data.ID, child.Data.ID})
	})
	if _, err := deleteDeviceGroupIdempotently(ctx, principal, parent.Data.ID, "delete-has-child-"+strings.ReplaceAll(uuid.NewString(), "-", ""), groupReadETag(parent.Data), "delete-child-conflict"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("group with a child must conflict, got %v", err)
	}
	data := &authn.Data{UserID: principal.subjectID, TenantID: tenant, Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	currentLeaf, readErr := (&Service{}).getDeviceGroup(ctx, leaf.Data.ID, "delete-member-current")
	et.OverrideAuthInfo("", nil)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if _, err := deleteDeviceGroupIdempotently(ctx, principal, leaf.Data.ID, "delete-has-member-"+strings.ReplaceAll(uuid.NewString(), "-", ""), currentLeaf.ETag, "delete-member-conflict"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("group with a member must conflict, got %v", err)
	}
	var liveDevice bool
	if err := db.QueryRow(ctx, `SELECT deleted_at IS NULL FROM devices WHERE tenant_id=$1 AND id=$2`, tenant, device.ID).Scan(&liveDevice); err != nil || !liveDevice {
		t.Fatalf("blocked group delete must not delete the device: live=%v err=%v", liveDevice, err)
	}
}

func TestDeleteDeviceGroupEnforcesTenantAndCAS(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	principalA := groupWritePrincipal{tenantID: tenantA, subjectID: "group-delete-a-" + uuid.NewString(), actorID: "group-delete-actor-a"}
	principalB := groupWritePrincipal{tenantID: tenantB, subjectID: principalA.subjectID, actorID: "group-delete-actor-b"}
	group := mustCreateDeviceGroup(t, ctx, principalA, "delete-cas-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-cas-" + uuid.NewString()})
	t.Cleanup(func() {
		deleteTestDeviceGroups(t, ctx, tenantA, []string{group.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenantA, principalA.subjectID)
		deleteDeviceGroupReceipts(t, ctx, tenantA, principalA.subjectID)
	})
	if _, err := deleteDeviceGroupIdempotently(ctx, principalB, group.Data.ID, "delete-foreign-"+strings.ReplaceAll(uuid.NewString(), "-", ""), groupReadETag(group.Data), "delete-foreign"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("cross-tenant delete must be not found, got %v", err)
	}
	if _, err := deleteDeviceGroupIdempotently(ctx, principalA, group.Data.ID, "delete-stale-"+strings.ReplaceAll(uuid.NewString(), "-", ""), `"sha256-`+strings.Repeat("a", 64)+`"`, "delete-stale"); !hasErrCode(err, errs.FailedPrecondition) {
		t.Fatalf("stale ETag must fail CAS, got %v", err)
	}
}

func TestDeleteDeviceGroupSerializesAgainstChildCreationAndMemberAddition(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-delete-race-" + uuid.NewString(), actorID: "group-delete-race-actor"}
	parent := mustCreateDeviceGroup(t, ctx, principal, "delete-race-parent-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-race-parent-" + uuid.NewString()})
	memberGroup := mustCreateDeviceGroup(t, ctx, principal, "delete-race-member-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-race-member-" + uuid.NewString()})
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	device, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "delete-race-device-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	var childID string
	t.Cleanup(func() {
		deleteTestGroupReceipts(t, ctx, tenant, principal.subjectID)
		deleteDeviceGroupReceipts(t, ctx, tenant, principal.subjectID)
		for _, operation := range []string{addDeviceGroupMembersOperation, removeDeviceGroupMembersOperation} {
			if _, err := db.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3`, tenant, principal.subjectID, operation); err != nil {
				t.Errorf("delete concurrent member receipts: %v", err)
			}
		}
		if _, err := db.Exec(ctx, `DELETE FROM device_group_members WHERE tenant_id=$1 AND group_id IN ($2,$3)`, tenant, parent.Data.ID, memberGroup.Data.ID); err != nil {
			t.Errorf("delete concurrent membership rows: %v", err)
		}
		if childID != "" {
			deleteTestDeviceGroups(t, ctx, tenant, []string{childID})
		}
		deleteTestDeviceGroupsReverse(t, ctx, tenant, []string{parent.Data.ID, memberGroup.Data.ID})
		if _, err := db.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenant, device.ID); err != nil {
			t.Errorf("soft-delete concurrent test device: %v", err)
		}
	})

	t.Run("child-create", func(t *testing.T) {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var deleteErr, createErr error
		var created *groupCreateResult
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, deleteErr = deleteDeviceGroupIdempotently(ctx, principal, parent.Data.ID, "race-delete-child-"+strings.ReplaceAll(uuid.NewString(), "-", ""), groupReadETag(parent.Data), "race-delete-child")
		}()
		go func() {
			defer wg.Done()
			<-start
			created, createErr = createDeviceGroupIdempotently(ctx, principal, "race-create-child-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "delete-race-child-" + uuid.NewString(), ParentID: &parent.Data.ID}, "race-create-child")
		}()
		close(start)
		wg.Wait()
		if deleteErr != nil && createErr != nil {
			t.Fatalf("serialized delete/create should leave one valid outcome: delete=%v create=%v", deleteErr, createErr)
		}
		var deleted bool
		var children int
		if err := db.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, parent.Data.ID).Scan(&deleted); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_groups WHERE tenant_id=$1 AND parent_id=$2 AND deleted_at IS NULL`, tenant, parent.Data.ID).Scan(&children); err != nil {
			t.Fatal(err)
		}
		if deleted && children != 0 || !deleted && children != 1 {
			t.Fatalf("delete/create race left invalid parent/child state: deleted=%v liveChildren=%d", deleted, children)
		}
		if created != nil && createErr == nil {
			var response DeviceGroupResponse
			if err := json.Unmarshal(created.response, &response); err != nil {
				t.Fatal(err)
			}
			childID = response.Data.ID
		}
	})

	t.Run("member-add", func(t *testing.T) {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var deleteErr, addErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, deleteErr = deleteDeviceGroupIdempotently(ctx, principal, memberGroup.Data.ID, "race-delete-member-"+strings.ReplaceAll(uuid.NewString(), "-", ""), groupReadETag(memberGroup.Data), "race-delete-member")
		}()
		go func() {
			defer wg.Done()
			<-start
			_, addErr = setDeviceGroupMembersIdempotently(ctx, principal, memberGroup.Data.ID, "race-add-member-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "race-add-member")
		}()
		close(start)
		wg.Wait()
		if deleteErr != nil && addErr != nil {
			t.Fatalf("serialized delete/member-add should leave one valid outcome: delete=%v add=%v", deleteErr, addErr)
		}
		var deleted bool
		var members int
		if err := db.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, memberGroup.Data.ID).Scan(&deleted); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, memberGroup.Data.ID).Scan(&members); err != nil {
			t.Fatal(err)
		}
		if deleted && members != 0 || !deleted && members != 1 {
			t.Fatalf("delete/member race left invalid group/member state: deleted=%v members=%d", deleted, members)
		}
	})
}

func TestDeleteDeviceGroupRawRequiresHeadersAndEmptyBody(t *testing.T) {
	data := &authn.Data{UserID: "group-delete-http-" + uuid.NewString(), TenantID: testTenantID(), Roles: []string{"TENANT_ADMIN"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)
	handler := (&Service{}).DeleteDeviceGroup
	request := httptest.NewRequest("DELETE", "/api/v2/device-groups/"+uuid.NewString(), nil)
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != 428 || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("missing If-Match must return Problem 428, status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	request = httptest.NewRequest("DELETE", "/api/v2/device-groups/"+uuid.NewString(), nil)
	request.Header.Set("If-Match", `"sha256-`+strings.Repeat("a", 64)+`"`)
	response = httptest.NewRecorder()
	handler(response, request)
	if response.Code != 400 || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("missing Idempotency-Key must return Problem 400, status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	request = httptest.NewRequest("DELETE", "/api/v2/device-groups/"+uuid.NewString()+"?unexpected=true", nil)
	response = httptest.NewRecorder()
	handler(response, request)
	if response.Code != 400 || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("unknown query must return Problem 400, status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	request = httptest.NewRequest("DELETE", "/api/v2/device-groups/"+uuid.NewString(), strings.NewReader(`{}`))
	request.Header.Set("If-Match", `"sha256-`+strings.Repeat("a", 64)+`"`)
	request.Header.Set("Idempotency-Key", "delete-body-123")
	response = httptest.NewRecorder()
	handler(response, request)
	if response.Code != 400 || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("non-empty DELETE body must return Problem 400, status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}

func deleteDeviceGroupReceipts(t *testing.T, ctx context.Context, tenantID, subjectID string) {
	t.Helper()
	if _, err := db.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3`, tenantID, subjectID, deleteDeviceGroupOperation); err != nil {
		t.Errorf("delete group delete receipts: %v", err)
	}
}
