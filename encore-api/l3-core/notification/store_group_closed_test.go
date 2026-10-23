package notification

import (
	"context"
	"encore.app/l3-core/notification/contract"
	"strings"
	"testing"
)

func TestClosedGroupCanRetainClosedInstanceButCannotPublishIt(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instance := createFixtureInstance(t, store, "tenant-import")
	if _, err := db.ExecContext(ctx, `UPDATE notification_instances SET enabled=FALSE WHERE tenant_id=$1 AND id=$2`, "tenant-import", instance); err != nil {
		t.Fatal(err)
	}
	request := contract.GroupCreate{Name: "Closed import", Enabled: false, Bindings: []contract.GroupBinding{{BindingID: "fixture", InstanceID: instance, RecipientSource: []byte(`{}`), ContentBinding: []byte(`{}`)}}}
	group, err := store.CreateGroup(ctx, "tenant-import", "closed-import-key", strings.Repeat("a", 64), request)
	if err != nil || group.Enabled || group.MigrationState != "native" || len(group.Bindings) != 1 {
		t.Fatalf("closed import lost configuration: %+v %v", group, err)
	}
	if _, err := store.ResolvePublished(ctx, "tenant-import", group.ID, group.Revision); !isStoreError(err, StoreErrorNotFound) {
		t.Fatal("closed import became a published group")
	}
	request.Enabled = true
	if _, err := store.UpdateGroup(ctx, "tenant-import", group.ID, "publish-closed-key", strings.Repeat("b", 64), contract.GroupUpdate{GroupCreate: request, ExpectedVersion: 1}); !isStoreError(err, StoreErrorNotFound) {
		t.Fatalf("closed instance published: %v", err)
	}
	current, err := store.GetGroup(ctx, "tenant-import", group.ID)
	if err != nil || current.Version != 1 || current.Enabled {
		t.Fatal("failed publish changed group")
	}
}
