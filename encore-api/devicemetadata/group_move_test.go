package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestDecodeMoveDeviceGroupBodyStrictly(t *testing.T) {
	got, err := decodeMoveDeviceGroupBody([]byte(`{"parentId":null,"expectedResourceVersion":2}`))
	if err != nil || got.ParentID != nil || got.ExpectedResourceVersion != 2 {
		t.Fatalf("valid root move body did not decode: %#v %v", got, err)
	}
	for _, body := range []string{`null`, `[]`, `{}`, `{"parentId":null}`, `{"expectedResourceVersion":1}`, `{"parentId":3,"expectedResourceVersion":1}`, `{"parentId":null,"expectedResourceVersion":0}`, `{"parentId":null,"expectedResourceVersion":1.5}`, `{"parentId":null,"expectedResourceVersion":1,"extra":true}`, `{} {}`} {
		t.Run(body, func(t *testing.T) {
			if _, err := decodeMoveDeviceGroupBody([]byte(body)); err == nil {
				t.Fatalf("expected invalid move body to be rejected: %s", body)
			}
		})
	}
}

func TestGroupPrincipalForWriteRequiresTenantAdmin(t *testing.T) {
	defer et.OverrideAuthInfo("", nil)
	for _, tc := range []struct {
		name  string
		role  string
		ten   string
		scope []string
		allow bool
	}{
		{name: "tenant viewer with forged scope", role: "TENANT_VIEWER", ten: testTenantID(), scope: []string{"group:write", "*"}},
		{name: "tenant user with forged scope", role: "TENANT_USER", ten: testTenantID(), scope: []string{"group:write", "*"}},
		{name: "sys admin with tenant but no delegation", role: "SYS_ADMIN", ten: testTenantID()},
		{name: "sys admin without tenant", role: "SYS_ADMIN"},
		{name: "tenant admin", role: "TENANT_ADMIN", ten: testTenantID(), allow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &authn.Data{UserID: "group-move-reader", TenantID: tc.ten, Roles: []string{tc.role}, Scopes: tc.scope}
			et.OverrideAuthInfo(auth.UID(data.UserID), data)
			principal, err := groupPrincipalForWrite()
			if tc.allow {
				if err != nil || principal.tenantID == "" || principal.subjectID != data.UserID {
					t.Fatalf("trusted tenant-admin principal rejected: %#v %v", principal, err)
				}
			} else if !hasErrCode(err, errs.PermissionDenied) {
				t.Fatalf("untrusted role/scope must not write groups, got principal=%#v err=%v", principal, err)
			}
		})
	}
}

func TestMoveDeviceGroupPreservesSubtreeAndReplays(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "move-" + uuid.NewString(), actorID: "move-actor"}
	root := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "move-root-" + uuid.NewString()})
	newParent := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "move-dest-" + uuid.NewString()})
	child := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "move-child-" + uuid.NewString(), ParentID: &root.Data.ID})
	grandchild := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "move-grandchild-" + uuid.NewString(), ParentID: &child.Data.ID})
	ids := []string{newParent.Data.ID, root.Data.ID, child.Data.ID, grandchild.Data.ID}
	t.Cleanup(func() {
		deleteTestDeviceGroupsReverse(t, ctx, tenant, ids)
		deleteTestGroupReceipts(t, ctx, tenant, principal.subjectID)
		deleteMoveReceipts(t, ctx, tenant, principal.subjectID)
	})

	key := moveKey()
	first, err := moveDeviceGroupIdempotently(ctx, principal, root.Data.ID, key, &MoveDeviceGroupReq{ParentID: &newParent.Data.ID, ExpectedResourceVersion: root.Data.ResourceVersion}, "move-request")
	if err != nil {
		t.Fatal(err)
	}
	var moved DeviceGroupResponse
	if err := json.Unmarshal(first.response, &moved); err != nil {
		t.Fatal(err)
	}
	if moved.Data.ID != root.Data.ID || moved.Data.ParentID == nil || *moved.Data.ParentID != newParent.Data.ID || moved.Data.ResourceVersion != root.Data.ResourceVersion+1 {
		t.Fatalf("move did not preserve group identity/update its parent and version: %#v", moved.Data)
	}
	replay, err := moveDeviceGroupIdempotently(ctx, principal, root.Data.ID, key, &MoveDeviceGroupReq{ParentID: &newParent.Data.ID, ExpectedResourceVersion: root.Data.ResourceVersion}, "ignored-retry")
	if err != nil {
		t.Fatal(err)
	}
	var replayed DeviceGroupResponse
	if err := json.Unmarshal(replay.response, &replayed); err != nil {
		t.Fatal(err)
	}
	if !replay.replayed || replayed.Data.ID != moved.Data.ID || replayed.Data.ResourceVersion != moved.Data.ResourceVersion || replayed.Meta.RequestID != "move-request" {
		t.Fatalf("same-key replay changed persisted response: %#v %#v", replay, replayed)
	}
	if _, err := moveDeviceGroupIdempotently(ctx, principal, root.Data.ID, key, &MoveDeviceGroupReq{ParentID: nil, ExpectedResourceVersion: root.Data.ResourceVersion}, "different"); !hasErrCode(err, errs.AlreadyExists) {
		t.Fatalf("same key with different request should conflict, got %v", err)
	}

	var childParent, grandchildParent string
	if err := db.QueryRow(ctx, `SELECT parent_id::text FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, child.Data.ID).Scan(&childParent); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT parent_id::text FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, grandchild.Data.ID).Scan(&grandchildParent); err != nil {
		t.Fatal(err)
	}
	if childParent != root.Data.ID || grandchildParent != child.Data.ID {
		t.Fatalf("move changed descendant links: childParent=%s grandchildParent=%s", childParent, grandchildParent)
	}
	// Moving a descendant to its current tree ancestor is valid and must not be
	// rejected as a cycle.
	_, err = moveDeviceGroupIdempotently(ctx, principal, child.Data.ID, moveKey(), &MoveDeviceGroupReq{ParentID: &newParent.Data.ID, ExpectedResourceVersion: child.Data.ResourceVersion}, "ancestor-move")
	if err != nil {
		t.Fatalf("move to an existing ancestor should be allowed: %v", err)
	}
	var ancestorParent string
	if err := db.QueryRow(ctx, `SELECT parent_id::text FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, child.Data.ID).Scan(&ancestorParent); err != nil || ancestorParent != newParent.Data.ID {
		t.Fatalf("valid ancestor move not persisted: parent=%s err=%v", ancestorParent, err)
	}
	// A request that leaves the parent unchanged is an idempotent no-op: it
	// records its receipt without incrementing resource_version.
	noOp, err := moveDeviceGroupIdempotently(ctx, principal, root.Data.ID, moveKey(), &MoveDeviceGroupReq{ParentID: &newParent.Data.ID, ExpectedResourceVersion: moved.Data.ResourceVersion}, "noop-move")
	if err != nil {
		t.Fatalf("same-parent move should succeed as a no-op: %v", err)
	}
	var noOpResponse DeviceGroupResponse
	if err := json.Unmarshal(noOp.response, &noOpResponse); err != nil {
		t.Fatal(err)
	}
	if noOpResponse.Data.ResourceVersion != moved.Data.ResourceVersion {
		t.Fatalf("same-parent move incremented resource version: before=%d after=%d", moved.Data.ResourceVersion, noOpResponse.Data.ResourceVersion)
	}
	var receiptCount, auditCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND canonical_path=$4 AND idempotency_key=$5`, tenant, principal.subjectID, moveDeviceGroupOperation, "/api/v2/device-groups/"+root.Data.ID+"/moves", key).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name=$3 AND request_id='move-request' AND before_version=$4 AND after_version=$5`, tenant, root.Data.ID, moveDeviceGroupOperation, root.Data.ResourceVersion, moved.Data.ResourceVersion).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if receiptCount != 1 || auditCount != 1 {
		t.Fatalf("move should create one receipt and audit row: receipts=%d audits=%d", receiptCount, auditCount)
	}
}

func TestMoveDeviceGroupRejectsCyclesTenantDepthAndDuplicates(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantA, tenantB := testTenantID(), testTenantID()
	principal := groupWritePrincipal{tenantID: tenantA, subjectID: "move-validation-" + uuid.NewString(), actorID: "move-validator"}
	foreign := groupWritePrincipal{tenantID: tenantB, subjectID: "move-foreign-" + uuid.NewString(), actorID: "foreign"}
	root := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "valid-root-" + uuid.NewString()})
	destination := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "valid-destination-" + uuid.NewString()})
	child := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "valid-child-" + uuid.NewString(), ParentID: &root.Data.ID})
	grandchild := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "valid-grandchild-" + uuid.NewString(), ParentID: &child.Data.ID})
	duplicate := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: root.Data.Name, ParentID: &destination.Data.ID})
	foreignParent := mustCreateDeviceGroup(t, ctx, foreign, moveKey(), &CreateDeviceGroupReq{Name: "foreign-parent-" + uuid.NewString()})
	ids := []string{destination.Data.ID, duplicate.Data.ID, root.Data.ID, child.Data.ID, grandchild.Data.ID}
	t.Cleanup(func() {
		deleteTestDeviceGroupsReverse(t, ctx, tenantA, ids)
		deleteTestDeviceGroupsReverse(t, ctx, tenantB, []string{foreignParent.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenantA, principal.subjectID)
		deleteTestGroupReceipts(t, ctx, tenantB, foreign.subjectID)
		deleteMoveReceipts(t, ctx, tenantA, principal.subjectID)
	})

	for _, tc := range []struct {
		name    string
		group   string
		parent  *string
		version int64
		code    errs.ErrCode
	}{
		{"self", root.Data.ID, &root.Data.ID, root.Data.ResourceVersion, errs.AlreadyExists},
		{"descendant", root.Data.ID, &grandchild.Data.ID, root.Data.ResourceVersion, errs.AlreadyExists},
		{"foreign", child.Data.ID, &foreignParent.Data.ID, child.Data.ResourceVersion, errs.NotFound},
		{"missing parent", child.Data.ID, stringPtr(uuid.NewString()), child.Data.ResourceVersion, errs.NotFound},
		{"stale", child.Data.ID, nil, child.Data.ResourceVersion + 1, errs.FailedPrecondition},
		{"duplicate sibling", root.Data.ID, &destination.Data.ID, root.Data.ResourceVersion, errs.AlreadyExists},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := moveDeviceGroupIdempotently(ctx, principal, tc.group, moveKey(), &MoveDeviceGroupReq{ParentID: tc.parent, ExpectedResourceVersion: tc.version}, "rejected-"+tc.name)
			if !hasErrCode(err, tc.code) {
				t.Fatalf("expected %v, got %v", tc.code, err)
			}
		})
	}

	// A 30-level destination plus a 2-edge subtree would exceed the frozen limit.
	depthTenant := testTenantID()
	depthPrincipal := groupWritePrincipal{tenantID: depthTenant, subjectID: "move-depth-" + uuid.NewString(), actorID: "depth-actor"}
	chain := make([]string, 0, 32)
	var parent *string
	for i := 0; i < 30; i++ {
		g := mustCreateDeviceGroup(t, ctx, depthPrincipal, moveKey(), &CreateDeviceGroupReq{Name: fmt.Sprintf("depth-%02d-%s", i, uuid.NewString()), ParentID: parent})
		chain = append(chain, g.Data.ID)
		parent = &chain[len(chain)-1]
	}
	subtreeRoot := mustCreateDeviceGroup(t, ctx, depthPrincipal, moveKey(), &CreateDeviceGroupReq{Name: "depth-subtree-" + uuid.NewString()})
	subtreeChild := mustCreateDeviceGroup(t, ctx, depthPrincipal, moveKey(), &CreateDeviceGroupReq{Name: "depth-subchild-" + uuid.NewString(), ParentID: &subtreeRoot.Data.ID})
	subtreeGrandchild := mustCreateDeviceGroup(t, ctx, depthPrincipal, moveKey(), &CreateDeviceGroupReq{Name: "depth-subgrandchild-" + uuid.NewString(), ParentID: &subtreeChild.Data.ID})
	chain = append(chain, subtreeRoot.Data.ID, subtreeChild.Data.ID, subtreeGrandchild.Data.ID)
	t.Cleanup(func() {
		deleteTestDeviceGroupsReverse(t, ctx, depthTenant, chain)
		deleteTestGroupReceipts(t, ctx, depthTenant, depthPrincipal.subjectID)
		deleteMoveReceipts(t, ctx, depthTenant, depthPrincipal.subjectID)
	})
	if _, err := moveDeviceGroupIdempotently(ctx, depthPrincipal, subtreeRoot.Data.ID, moveKey(), &MoveDeviceGroupReq{ParentID: parent, ExpectedResourceVersion: subtreeRoot.Data.ResourceVersion}, "too-deep"); !hasErrCode(err, errs.InvalidArgument) {
		t.Fatalf("move exceeding depth 32 should be rejected, got %v", err)
	}
}

func TestMoveDeviceGroupAtoBAndBtoAConcurrentNeverCreatesCycle(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "move-race-" + uuid.NewString(), actorID: "race-actor"}
	a := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "race-a-" + uuid.NewString()})
	b := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "race-b-" + uuid.NewString()})
	t.Cleanup(func() {
		// Exactly one move wins, so either group may be the child. Disconnect
		// both rows before deleting to avoid depending on the winning direction.
		if _, err := db.Exec(ctx, `UPDATE device_groups SET parent_id = NULL
			WHERE tenant_id = $1 AND id IN ($2, $3)`, tenant, a.Data.ID, b.Data.ID); err != nil {
			t.Errorf("disconnect concurrent move fixtures: %v", err)
		}
		deleteTestDeviceGroups(t, ctx, tenant, []string{a.Data.ID, b.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenant, principal.subjectID)
		deleteMoveReceipts(t, ctx, tenant, principal.subjectID)
	})
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pair := range []struct {
		group   string
		parent  string
		version int64
	}{{a.Data.ID, b.Data.ID, a.Data.ResourceVersion}, {b.Data.ID, a.Data.ID, b.Data.ResourceVersion}} {
		pair := pair
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := moveDeviceGroupIdempotently(ctx, principal, pair.group, moveKey(), &MoveDeviceGroupReq{ParentID: &pair.parent, ExpectedResourceVersion: pair.version}, "race-"+pair.group)
			results <- err
		}()
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
		if hasErrCode(err, errs.AlreadyExists) || hasErrCode(err, errs.FailedPrecondition) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent move result: %v", err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("A→B/B→A race must allow exactly one acyclic move: successes=%d conflicts=%d", successes, conflicts)
	}
	var cycleCount int
	if err := db.QueryRow(ctx, `WITH RECURSIVE walk(id,parent_id,path,cycle) AS (
		SELECT id,parent_id,ARRAY[id]::uuid[],false FROM device_groups WHERE tenant_id=$1 AND id IN ($2,$3)
		UNION ALL SELECT p.id,p.parent_id,w.path||p.id,p.id=ANY(w.path) FROM walk w JOIN device_groups p ON p.tenant_id=$1 AND p.id=w.parent_id WHERE NOT w.cycle
	) SELECT COUNT(*) FROM walk WHERE cycle`, tenant, a.Data.ID, b.Data.ID).Scan(&cycleCount); err != nil || cycleCount != 0 {
		t.Fatalf("post-race tree contains a cycle: count=%d err=%v", cycleCount, err)
	}
}

func TestMoveDeviceGroupAuditFailureRollsBackMoveAndReceipt(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	principal := groupWritePrincipal{tenantID: tenant, subjectID: "move-rollback-" + uuid.NewString(), actorID: "rollback-actor"}
	oldParent := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "rollback-old-" + uuid.NewString()})
	newParent := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "rollback-new-" + uuid.NewString()})
	child := mustCreateDeviceGroup(t, ctx, principal, moveKey(), &CreateDeviceGroupReq{Name: "rollback-child-" + uuid.NewString(), ParentID: &oldParent.Data.ID})
	t.Cleanup(func() {
		deleteTestDeviceGroupsReverse(t, ctx, tenant, []string{newParent.Data.ID, oldParent.Data.ID, child.Data.ID})
		deleteTestGroupReceipts(t, ctx, tenant, principal.subjectID)
		deleteMoveReceipts(t, ctx, tenant, principal.subjectID)
	})
	key := moveKey()
	if _, err := moveDeviceGroupIdempotently(ctx, principal, child.Data.ID, key, &MoveDeviceGroupReq{ParentID: &newParent.Data.ID, ExpectedResourceVersion: child.Data.ResourceVersion}, strings.Repeat("r", 129)); err == nil {
		t.Fatal("overlong audit request ID should fail the transaction")
	}
	var parentID string
	var version int64
	if err := db.QueryRow(ctx, `SELECT parent_id::text,resource_version FROM device_groups WHERE tenant_id=$1 AND id=$2`, tenant, child.Data.ID).Scan(&parentID, &version); err != nil || parentID != oldParent.Data.ID || version != child.Data.ResourceVersion {
		t.Fatalf("failed audit left a partial group move: parent=%s version=%d err=%v", parentID, version, err)
	}
	var receipts, audits int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3 AND idempotency_key=$4`, tenant, principal.subjectID, moveDeviceGroupOperation, key).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM metadata_audit_events WHERE tenant_id=$1 AND resource_id=$2 AND operation_name=$3`, tenant, child.Data.ID, moveDeviceGroupOperation).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || audits != 0 {
		t.Fatalf("failed transaction left receipt/audit rows: receipts=%d audits=%d", receipts, audits)
	}
}

func moveKey() string { return "move-" + strings.ReplaceAll(uuid.NewString(), "-", "") }

func deleteMoveReceipts(t *testing.T, ctx context.Context, tenantID, subjectID string) {
	t.Helper()
	if _, err := db.Exec(ctx, `DELETE FROM metadata_idempotency WHERE tenant_id=$1 AND subject_id=$2 AND operation_name=$3`, tenantID, subjectID, moveDeviceGroupOperation); err != nil {
		t.Errorf("delete move receipts: %v", err)
	}
}
