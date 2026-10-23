package notification

import (
	"context"
	"strings"
	"testing"

	"encore.app/l3-core/notification/contract"
)

func TestGroupRevisionEncryptsBindingsAndTypedReplayReturnsOriginalVersion(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-groups")
	create := contract.GroupCreate{Name: "Engineering alerts", Enabled: true, Bindings: []contract.GroupBinding{{BindingID: "binding-one", InstanceID: instanceID, RecipientSource: []byte(`{"kind":"user","address":"sensitive-recipient@example.test"}`), ContentBinding: []byte(`{"kind":"text","body":"sensitive binding body"}`)}}}
	first, err := store.CreateGroup(ctx, "tenant-groups", "group-create-0001", strings.Repeat("1", 64), create)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	var recipientCipher, contentCipher []byte
	if err := db.QueryRowContext(ctx, `SELECT recipient_source_ciphertext,content_binding_ciphertext FROM notification_group_bindings WHERE tenant_id=$1 AND group_id=$2`, "tenant-groups", first.ID).Scan(&recipientCipher, &contentCipher); err != nil {
		t.Fatal("read protected group binding")
	}
	if strings.Contains(string(recipientCipher), "sensitive-recipient") || strings.Contains(string(contentCipher), "sensitive binding body") {
		t.Fatal("group binding plaintext reached PostgreSQL")
	}
	loaded, err := store.ResolvePublished(ctx, "tenant-groups", first.ID, 1)
	if err != nil || len(loaded.Bindings) != 1 || string(loaded.Bindings[0].RecipientSource) != string(create.Bindings[0].RecipientSource) {
		t.Fatalf("resolve protected group revision: (%+v,%v)", loaded, err)
	}
	secondRequest := contract.GroupUpdate{ExpectedVersion: 1, GroupCreate: contract.GroupCreate{Name: "Engineering alerts v2", Enabled: false, Bindings: []contract.GroupBinding{}}}
	second, err := store.UpdateGroup(ctx, "tenant-groups", first.ID, "group-update-0001", strings.Repeat("2", 64), secondRequest)
	if err != nil || second.Version != 2 {
		t.Fatalf("update group: (%+v,%v)", second, err)
	}
	replayed, ok, err := store.FindGroupReplay(ctx, "tenant-groups", first.ID, "group.update:"+first.ID, "group-update-0001", strings.Repeat("2", 64))
	if err != nil || !ok || replayed.Version != 2 || replayed.Name != "Engineering alerts v2" {
		t.Fatalf("typed group replay: (%+v,%v,%v)", replayed, ok, err)
	}
	if _, err := store.GetGroup(ctx, "tenant-other", first.ID); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("cross-tenant group lookup: %v", err)
	}
}

func TestSourceProjectionPinsTargetAndCapsHistory(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	instanceID := createFixtureInstance(t, store, "tenant-projection")
	group, err := store.CreateGroup(ctx, "tenant-projection", "projection-group-0001", strings.Repeat("3", 64), contract.GroupCreate{Name: "Projection group", Enabled: true, Bindings: []contract.GroupBinding{{BindingID: "b", InstanceID: instanceID, RecipientSource: []byte(`{}`), ContentBinding: []byte(`{}`)}}})
	if err != nil {
		t.Fatal("create projection group")
	}
	principal := SourceProjectionPrincipal{DeploymentID: "deployment-projection", TenantIDs: []string{"tenant-projection"}}
	request := contract.SourceGroupProjectionRequest{SourceDeploymentID: principal.DeploymentID, TenantID: "tenant-projection", LegacyGroupID: "legacy-1", NotificationGroupID: group.ID, GroupRevision: 1}
	projection, err := store.RegisterGroupProjection(ctx, principal, request)
	if err != nil || projection.NotificationGroupID != group.ID {
		t.Fatalf("register projection: (%+v,%v)", projection, err)
	}
	changed := request
	changed.NotificationGroupID = "other-group"
	if _, err := store.RegisterGroupProjection(ctx, principal, changed); !isStoreError(err, StoreErrorConflict) {
		t.Fatalf("projection target rewrite error=%v", err)
	}
	if _, err := store.RegisterGroupProjection(ctx, SourceProjectionPrincipal{DeploymentID: "other-deployment", TenantIDs: principal.TenantIDs}, request); !isStoreError(err, StoreErrorInvalid) {
		t.Fatalf("cross-deployment projection error=%v", err)
	}
	list, err := store.ListGroupProjections(ctx, "tenant-projection", group.ID)
	if err != nil || len(list) != 1 || list[0].GroupRevision != 1 {
		t.Fatalf("projection history=(%+v,%v)", list, err)
	}
}
