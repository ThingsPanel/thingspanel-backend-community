package devicemetadata

import (
	"context"
	"encoding/json"
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

func TestDecodeSetGroupMembersBodyStrictly(t *testing.T) {
	id1, id2 := uuid.NewString(), uuid.NewString()
	valid := `{"deviceIds":["` + id2 + `","` + id1 + `"],"expectedGroupVersion":2}`
	decoded, err := decodeSetGroupMembersBody([]byte(valid))
	if err != nil || len(decoded.DeviceIDs) != 2 || decoded.DeviceIDs[0] > decoded.DeviceIDs[1] || decoded.ExpectedGroupVersion != 2 {
		t.Fatalf("valid set body should normalize and sort device IDs: req=%#v err=%v", decoded, err)
	}
	for _, body := range []string{
		``, `[]`, `{"deviceIds":[],"expectedGroupVersion":1}`,
		`{"deviceIds":["` + id1 + `","` + id1 + `"],"expectedGroupVersion":1}`,
		`{"deviceIds":["` + id1 + `"],"expectedGroupVersion":0}`,
		`{"deviceIds":["` + id1 + `"],"expectedGroupVersion":1,"tenantId":"other"}`,
		valid + ` {}`,
	} {
		if _, err := decodeSetGroupMembersBody([]byte(body)); err == nil {
			t.Errorf("expected strict body rejection for %q", body)
		}
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = `"` + uuid.NewString() + `"`
	}
	if _, err := decodeSetGroupMembersBody([]byte(`{"deviceIds":[` + strings.Join(many, ",") + `],"expectedGroupVersion":1}`)); err == nil {
		t.Fatal("more than 100 members must be rejected")
	}
}

func TestSetDeviceGroupMembersRawRejectsForgedScopeWithoutTenantAdmin(t *testing.T) {
	data := &authn.Data{UserID: "group-member-read-only-" + uuid.NewString(), TenantID: testTenantID(), Roles: []string{"TENANT_USER"}, Scopes: []string{"group:write"}}
	et.OverrideAuthInfo(auth.UID(data.UserID), data)
	defer et.OverrideAuthInfo("", nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v2/device-groups/"+uuid.NewString()+"/member-additions", strings.NewReader(`{"deviceIds":["`+uuid.NewString()+`"],"expectedGroupVersion":1}`))
	request.Header.Set("Idempotency-Key", "member-denied-123")
	recorder := httptest.NewRecorder()
	(&Service{}).AddDeviceGroupMembers(recorder, request)
	if recorder.Code != http.StatusForbidden || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("a fabricated scope must not authorize membership writes: status=%d content-type=%q body=%s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func TestSetDeviceGroupMembersPersistsCASReplayAndNoOp(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-members-" + uuid.NewString(), actorID: "members-actor"}
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	firstDevice, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-" + uuid.NewString()})
	if err != nil {
		t.Fatalf("create first test device: %v", err)
	}
	secondDevice, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-" + uuid.NewString()})
	if err != nil {
		t.Fatalf("create second test device: %v", err)
	}
	thirdDevice, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-" + uuid.NewString()})
	if err != nil {
		t.Fatalf("create third test device: %v", err)
	}
	group := mustCreateDeviceGroup(t, ctx, principal, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "members-" + uuid.NewString()})
	t.Cleanup(func() {
		cleanupMembershipScenario(t, ctx, tenant, principal, group.Data.ID, []string{firstDevice.ID, secondDevice.ID, thirdDevice.ID})
	})
	ids := []string{secondDevice.ID, firstDevice.ID}
	key := "add-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	first, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, key, &SetGroupMembersReq{DeviceIDs: ids, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "members-add-first")
	if err != nil {
		t.Fatalf("add two members: %v", err)
	}
	var firstResponse DeviceGroupResponse
	if err := json.Unmarshal(first.response, &firstResponse); err != nil {
		t.Fatal(err)
	}
	if first.status != 200 || firstResponse.Data.ResourceVersion != 2 || firstResponse.Data.DirectDeviceCount != 2 {
		t.Fatalf("first add should commit two members and one group version: status=%d group=%#v", first.status, firstResponse.Data)
	}
	replayed, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, key, &SetGroupMembersReq{DeviceIDs: []string{firstDevice.ID, secondDevice.ID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "ignored-replay-request")
	if err != nil {
		t.Fatalf("replay membership add: %v", err)
	}
	var replayResponse DeviceGroupResponse
	if err := json.Unmarshal(replayed.response, &replayResponse); err != nil {
		t.Fatal(err)
	}
	if replayResponse.Meta.RequestID != firstResponse.Meta.RequestID || replayResponse.Data.ResourceVersion != 2 {
		t.Fatalf("same-key replay did not return stored success: first=%#v replay=%#v", firstResponse, replayResponse)
	}
	if _, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, key, &SetGroupMembersReq{DeviceIDs: []string{firstDevice.ID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "different-payload"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("same key with another set must conflict, got %v", err)
	}
	noop, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "noop-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: ids, ExpectedGroupVersion: 2}, addDeviceGroupMembersOperation, "members-add-noop")
	if err != nil {
		t.Fatalf("duplicate set add should be a no-op: %v", err)
	}
	var noopResponse DeviceGroupResponse
	if err := json.Unmarshal(noop.response, &noopResponse); err != nil || noopResponse.Data.ResourceVersion != 2 || noopResponse.Data.DirectDeviceCount != 2 {
		t.Fatalf("no-op add changed membership version/count: group=%#v err=%v", noopResponse.Data, err)
	}
	removeKey := "remove-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := db.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenant, firstDevice.ID); err != nil {
		t.Fatal(err)
	}
	removeReq := &SetGroupMembersReq{DeviceIDs: []string{firstDevice.ID, thirdDevice.ID}, ExpectedGroupVersion: 2}
	removed, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, removeKey, removeReq, removeDeviceGroupMembersOperation, "members-remove")
	if err != nil {
		t.Fatalf("remove one member: %v", err)
	}
	var removeResponse DeviceGroupResponse
	if err := json.Unmarshal(removed.response, &removeResponse); err != nil {
		t.Fatal(err)
	}
	if removeResponse.Data.ResourceVersion != 3 || removeResponse.Data.DirectDeviceCount != 1 {
		t.Fatalf("remove should update one relationship and one group version: %#v", removeResponse.Data)
	}
	var auditJSON []byte
	if err := db.QueryRow(ctx, `SELECT redacted_changes FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND request_id='members-remove'`, tenant, group.Data.ID).Scan(&auditJSON); err != nil {
		t.Fatal(err)
	}
	var changes map[string][]string
	if err := json.Unmarshal(auditJSON, &changes); err != nil || len(changes["removedDeviceIds"]) != 1 || changes["removedDeviceIds"][0] != firstDevice.ID {
		t.Fatalf("audit must list only actually removed IDs: %s err=%v", auditJSON, err)
	}
	if strings.Contains(string(auditJSON), "secret") || strings.Contains(string(auditJSON), thirdDevice.ID) {
		t.Fatalf("audit included a non-member/request-only ID or secret: %s", auditJSON)
	}
	replayedRemoval, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, removeKey, removeReq, removeDeviceGroupMembersOperation, "members-remove-replay")
	if err != nil || string(replayedRemoval.response) != string(removed.response) {
		t.Fatalf("replayed removal must return the original response without another mutation: same=%v err=%v", err == nil && string(replayedRemoval.response) == string(removed.response), err)
	}
	var tombstoneExists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NOT NULL)`, tenant, firstDevice.ID).Scan(&tombstoneExists); err != nil || !tombstoneExists {
		t.Fatalf("removal must not delete or revive the tombstone device: exists=%v err=%v", tombstoneExists, err)
	}
	missingRemoval, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "remove-absent-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{firstDevice.ID}, ExpectedGroupVersion: 3}, removeDeviceGroupMembersOperation, "members-remove-absent")
	if err != nil {
		t.Fatalf("removing an existing target with no membership should be a no-op: %v", err)
	}
	var absentResponse DeviceGroupResponse
	if err := json.Unmarshal(missingRemoval.response, &absentResponse); err != nil || absentResponse.Data.ResourceVersion != 3 || absentResponse.Data.DirectDeviceCount != 1 {
		t.Fatalf("removing absent membership changed aggregate: %#v err=%v", absentResponse.Data, err)
	}
	var count, audits int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, group.Data.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one remaining membership, count=%d err=%v", count, err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name IN ($3,$4)`, tenant, group.Data.ID, addDeviceGroupMembersOperation, removeDeviceGroupMembersOperation).Scan(&audits); err != nil || audits != 4 {
		t.Fatalf("expected one audit per first successful write (including no-op) and none for replay, count=%d err=%v", audits, err)
	}
}

func TestSetDeviceGroupMembersRemovalSucceedsAndRollsBackAuditFailure(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-members-owner-" + uuid.NewString(), actorID: "members-actor"}
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	device, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-owner-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	group := mustCreateDeviceGroup(t, ctx, principal, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "members-owner-" + uuid.NewString()})
	t.Cleanup(func() { cleanupMembershipScenario(t, ctx, tenant, principal, group.Data.ID, []string{device.ID}) })
	add, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "owner-add-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "owner-add")
	if err != nil {
		t.Fatal(err)
	}
	_ = add

	removed, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "remove-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 2}, removeDeviceGroupMembersOperation, "owner-free-removal")
	if err != nil {
		t.Fatalf("same-tenant membership removal should not need an external-reference owner: %v", err)
	}
	var removedResponse DeviceGroupResponse
	if err := json.Unmarshal(removed.response, &removedResponse); err != nil || removedResponse.Data.ResourceVersion != 3 || removedResponse.Data.DirectDeviceCount != 0 {
		t.Fatalf("successful removal should advance the group version and clear its direct count: %#v err=%v", removedResponse.Data, err)
	}
	var memberships int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, group.Data.ID).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatalf("successful removal must delete the membership row: count=%d err=%v", memberships, err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO device_group_members (tenant_id,group_id,device_id,created_by) VALUES ($1,$2,$3,$4)`, tenant, group.Data.ID, device.ID, principal.actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE device_groups SET resource_version=4 WHERE tenant_id=$1 AND id=$2`, tenant, group.Data.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "audit-fail-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 4}, removeDeviceGroupMembersOperation, strings.Repeat("r", 129)); err == nil {
		t.Fatal("invalid audit request id should fail the transaction")
	}
	var restoredMembers int
	var restoredVersion int64
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2 AND device_id=$3`, tenant, group.Data.ID, device.ID).Scan(&restoredMembers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, group.Data.ID).Scan(&restoredVersion); err != nil {
		t.Fatal(err)
	}
	if restoredMembers != 1 || restoredVersion != 4 {
		t.Fatalf("audit failure must roll back relation and version: members=%d version=%d", restoredMembers, restoredVersion)
	}
	var removals, audits int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3`, tenant, principal.subjectID, removeDeviceGroupMembersOperation).Scan(&removals); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name=$3`, tenant, group.Data.ID, removeDeviceGroupMembersOperation).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if removals != 1 || audits != 1 {
		t.Fatalf("only the preceding successful removal may leave a receipt/audit: receipts=%d audits=%d", removals, audits)
	}
}

func TestSetDeviceGroupMembersAddsExactly100LiveDevicesAndDuplicateSetIsNoOp(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-members-100-" + uuid.NewString(), actorID: "members-actor"}
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	ids := make([]string, 0, maxDeviceGroupMembersPerRequest)
	for i := 0; i < maxDeviceGroupMembersPerRequest; i++ {
		device, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: fmt.Sprintf("group-member-capacity-%d-%s", i, uuid.NewString())})
		if err != nil {
			t.Fatalf("create live capacity fixture %d: %v", i, err)
		}
		ids = append(ids, device.ID)
	}
	group := mustCreateDeviceGroup(t, ctx, principal, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "members-100-" + uuid.NewString()})
	t.Cleanup(func() { cleanupMembershipScenario(t, ctx, tenant, principal, group.Data.ID, ids) })
	added, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "add-100-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: ids, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "members-add-100")
	if err != nil {
		t.Fatalf("add exactly 100 live devices: %v", err)
	}
	var response DeviceGroupResponse
	if err := json.Unmarshal(added.response, &response); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, group.Data.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 100 || response.Data.DirectDeviceCount != 100 || response.Data.ResourceVersion != 2 {
		t.Fatalf("100-member add mismatch: rows=%d count=%d version=%d", count, response.Data.DirectDeviceCount, response.Data.ResourceVersion)
	}
	noop, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "add-100-repeat-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: ids, ExpectedGroupVersion: 2}, addDeviceGroupMembersOperation, "members-add-100-noop")
	if err != nil {
		t.Fatalf("repeat exact 100-device set: %v", err)
	}
	if err := json.Unmarshal(noop.response, &response); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, group.Data.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 100 || response.Data.DirectDeviceCount != 100 || response.Data.ResourceVersion != 2 {
		t.Fatalf("repeat set must remain a no-op: rows=%d count=%d version=%d", count, response.Data.DirectDeviceCount, response.Data.ResourceVersion)
	}
}

func TestSetDeviceGroupMembersRemovalPreservesOtherGroupAndDevice(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-members-cross-group-" + uuid.NewString(), actorID: "members-actor"}
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	device, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-cross-group-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	groupA := mustCreateDeviceGroup(t, ctx, principal, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "members-a-" + uuid.NewString()})
	groupB := mustCreateDeviceGroup(t, ctx, principal, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "members-b-" + uuid.NewString()})
	t.Cleanup(func() {
		cleanupMembershipScenario(t, ctx, tenant, principal, groupA.Data.ID, []string{device.ID})
		cleanupMembershipScenario(t, ctx, tenant, principal, groupB.Data.ID, []string{device.ID})
	})
	for _, group := range []DeviceGroupResponse{groupA, groupB} {
		if _, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "cross-add-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "cross-group-add"); err != nil {
			t.Fatalf("add device to both groups: %v", err)
		}
	}
	if _, err := setDeviceGroupMembersIdempotently(ctx, principal, groupA.Data.ID, "cross-remove-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 2}, removeDeviceGroupMembersOperation, "cross-group-remove"); err != nil {
		t.Fatalf("remove group A relation: %v", err)
	}
	var groupACount, groupBCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2 AND device_id=$3`, tenant, groupA.Data.ID, device.ID).Scan(&groupACount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2 AND device_id=$3`, tenant, groupB.Data.ID, device.ID).Scan(&groupBCount); err != nil {
		t.Fatal(err)
	}
	var deviceExists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`, tenant, device.ID).Scan(&deviceExists); err != nil {
		t.Fatal(err)
	}
	if groupACount != 0 || groupBCount != 1 || !deviceExists {
		t.Fatalf("removal must preserve other group relation and device: groupA=%d groupB=%d deviceLive=%v", groupACount, groupBCount, deviceExists)
	}
}

func TestSetDeviceGroupMembersRejectsWholeBatchOnInvalidDeviceAndStaleVersion(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-members-atomic-" + uuid.NewString(), actorID: "members-actor"}
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	device, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-atomic-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	foreignTenant := testTenantID()
	foreignTemplate := createPublishedDeviceTemplate(t, ctx, foreignTenant)
	foreignDevice, err := createDevice(ctx, foreignTenant, &Device{DeviceTemplateID: foreignTemplate.ID, SN: "group-member-foreign-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	group := mustCreateDeviceGroup(t, ctx, principal, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "members-atomic-" + uuid.NewString()})
	t.Cleanup(func() {
		cleanupMembershipScenario(t, ctx, tenant, principal, group.Data.ID, []string{device.ID})
		if _, err := db.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, foreignTenant, foreignDevice.ID); err != nil {
			t.Errorf("soft-delete foreign test device: %v", err)
		}
	})
	missing := uuid.NewString()
	for _, invalidDeviceID := range []string{missing, foreignDevice.ID} {
		if _, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "atomic-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID, invalidDeviceID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "invalid-member"); !hasErrCode(err, errs.NotFound) {
			t.Fatalf("batch containing unknown or foreign-tenant device should fail as not found, got %v", err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenant, device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "deleted-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, "deleted-member"); !hasErrCode(err, errs.NotFound) {
		t.Fatalf("deleted device must not be added as a member, got %v", err)
	}
	var count int
	var version int64
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, group.Data.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, group.Data.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if count != 0 || version != 1 {
		t.Fatalf("invalid batch left partial state: members=%d version=%d", count, version)
	}
	if _, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, "stale-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &SetGroupMembersReq{DeviceIDs: []string{device.ID}, ExpectedGroupVersion: 2}, addDeviceGroupMembersOperation, "stale-member"); !hasErrCode(err, errs.FailedPrecondition) {
		t.Fatalf("stale expectedGroupVersion should reject, got %v", err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, group.Data.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale version left membership, count=%d err=%v", count, err)
	}
}

func TestSetDeviceGroupMembersConcurrentCASSerializesSameGroup(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "group-members-race-" + uuid.NewString(), actorID: "members-actor"}
	template := createPublishedDeviceTemplate(t, ctx, tenant)
	deviceA, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-race-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	deviceB, err := createDevice(ctx, tenant, &Device{DeviceTemplateID: template.ID, SN: "group-member-race-" + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	group := mustCreateDeviceGroup(t, ctx, principal, "group-"+strings.ReplaceAll(uuid.NewString(), "-", ""), &CreateDeviceGroupReq{Name: "members-race-" + uuid.NewString()})
	t.Cleanup(func() {
		cleanupMembershipScenario(t, ctx, tenant, principal, group.Data.ID, []string{deviceA.ID, deviceB.ID})
	})
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, deviceID := range []string{deviceA.ID, deviceB.ID} {
		wg.Add(1)
		go func(i int, deviceID string) {
			defer wg.Done()
			<-start
			_, err := setDeviceGroupMembersIdempotently(ctx, principal, group.Data.ID, fmt.Sprintf("race-%d-%s", i, strings.ReplaceAll(uuid.NewString(), "-", "")), &SetGroupMembersReq{DeviceIDs: []string{deviceID}, ExpectedGroupVersion: 1}, addDeviceGroupMembersOperation, fmt.Sprintf("race-request-%d", i))
			results <- err
		}(i, deviceID)
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if hasErrCode(err, errs.FailedPrecondition) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent membership result: %v", err)
	}
	var count int
	var version int64
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, group.Data.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, group.Data.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if successes != 1 || conflicts != 1 || count != 1 || version != 2 {
		t.Fatalf("concurrent CAS should have one winner: successes=%d conflicts=%d members=%d version=%d", successes, conflicts, count, version)
	}
}

func cleanupMembershipScenario(t *testing.T, ctx context.Context, tenant string, principal groupWritePrincipal, groupID string, deviceIDs []string) {
	t.Helper()
	for _, operation := range []string{addDeviceGroupMembersOperation, removeDeviceGroupMembersOperation} {
		if _, err := db.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3`, tenant, principal.subjectID, operation); err != nil {
			t.Errorf("delete member receipts: %v", err)
		}
	}
	if _, err := db.Exec(ctx, `DELETE FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2`, tenant, groupID); err != nil {
		t.Errorf("delete membership test audit: %v", err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM device_group_members WHERE tenant_id=$1 AND group_id=$2`, tenant, groupID); err != nil {
		t.Errorf("delete membership test rows: %v", err)
	}
	deleteTestDeviceGroups(t, ctx, tenant, []string{groupID})
	for _, deviceID := range deviceIDs {
		if _, err := db.Exec(ctx, `UPDATE devices SET deleted_at=NOW() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenant, deviceID); err != nil {
			t.Errorf("soft-delete test device: %v", err)
		}
	}
}
