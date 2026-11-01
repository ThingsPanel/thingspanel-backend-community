package devicemetadata

import (
	"context"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestListQueriesReturnNonNilEmptySlicesFromEncoreDB(t *testing.T) {
	ctx := context.Background()
	tenantID := testTenantID()
	userID := testActorID()
	et.OverrideAuthInfo(auth.UID(userID), &authn.Data{
		UserID: userID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"},
	})
	defer et.OverrideAuthInfo("", nil)

	service := &Service{}

	thingModels, err := service.ListThingModels(ctx, &ThingModelListReq{})
	assertEmptyList(t, "thing models", thingModels, err)

	items, err := service.ListItems(ctx, uuid.NewString(), &ListItemsReq{})
	assertEmptyList(t, "thing model items", items, err)

	templates, err := service.ListTemplates(ctx, &ListDeviceTemplatesReq{})
	assertEmptyList(t, "device templates", templates, err)

	products, err := service.ListProducts(ctx, &ListProductsReq{})
	assertEmptyList(t, "products", products, err)

	devices, err := service.ListDevices(ctx, &ListDevicesReq{})
	assertEmptyList(t, "devices", devices, err)

	controls, err := service.ListCustomCommandControls(ctx, &CustomCommandControlListReq{ThingModelID: uuid.NewString()})
	assertEmptyList(t, "custom command controls", controls, err)

	history, err := service.ListMigrationHistory(ctx, &ListMigrationHistoryReq{})
	assertEmptyList(t, "migration history", history, err)
}

func assertEmptyList[T any](t *testing.T, name string, response *listResponse[T], err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("list %s: %v", name, err)
	}
	if response == nil {
		t.Fatalf("list %s returned a nil response", name)
	}
	if response.Items == nil {
		t.Fatalf("list %s returned a nil slice", name)
	}
	if len(response.Items) != 0 || response.Total != 0 {
		t.Fatalf("list %s should be empty: len(items)=%d total=%d", name, len(response.Items), response.Total)
	}
}
