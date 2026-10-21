package devicemetadata

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// TestCrossTenantThingModelIsolation verifies that tenant A cannot access tenant B's thing models.
// All store functions must filter by tenantID – this test documents that contract.
func TestCrossTenantThingModelIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantA := testTenantID()
	tenantB := testTenantID()

	// Create a thing-model under tenant A
	tmA, err := createThingModel(ctx, tenantA, &ThingModel{Name: "Model-A", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel tenantA: %v", err)
	}

	// Tenant B must NOT see tenant A's model
	_, err = getThingModelByID(ctx, tenantB, tmA.ID)
	if err == nil {
		t.Fatal("tenantB should not access tenantA's thing model (expected not-found error)")
	}

	// Tenant B list must be empty (no cross-contamination)
	items, total, err := listThingModels(ctx, tenantB, &ThingModelListReq{
		pageRequest: pageRequest{Page: 1, PageSize: 50},
	})
	if err != nil {
		t.Fatalf("listThingModels tenantB: %v", err)
	}
	for _, m := range items {
		if m.TenantID == tenantA {
			t.Fatalf("listThingModels returned tenantA record to tenantB")
		}
	}
	_ = total
}

// TestCrossTenantThingModelItemIsolation verifies item-level cross-tenant isolation.
func TestCrossTenantThingModelItemIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantA := testTenantID()
	tenantB := testTenantID()

	tmA, err := createThingModel(ctx, tenantA, &ThingModel{Name: "ModelA", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	item, err := createThingModelItem(ctx, tenantA, tmA.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "temp",
		NameI18n:   json.RawMessage(`{"default":"Temp"}`),
		ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err != nil {
		t.Fatalf("createThingModelItem: %v", err)
	}

	// Tenant B reads item by ID – must fail
	_, err = getThingModelItem(ctx, tenantB, tmA.ID, item.ID)
	if err == nil {
		t.Fatal("tenantB should not access tenantA's thing model item")
	}

	// Tenant B lists items – must be empty
	listed, _, err := listThingModelItems(ctx, tenantB, tmA.ID, "", "", pageRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("listThingModelItems tenantB: %v", err)
	}
	for _, i := range listed {
		if i.ID == item.ID {
			t.Fatal("tenantB can see tenantA's item")
		}
	}
}

// TestCrossTenantDeviceTemplateIsolation verifies device template cross-tenant isolation.
func TestCrossTenantDeviceTemplateIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantA := testTenantID()
	tenantB := testTenantID()

	// Need a thing model + snapshot for template creation
	tmA, err := createThingModel(ctx, tenantA, &ThingModel{Name: "TM-A", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel: %v", err)
	}
	_, err = createThingModelItem(ctx, tenantA, tmA.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "x",
		NameI18n:   json.RawMessage(`{"default":"X"}`),
		ValueType:  json.RawMessage(`{"kind":"BOOL"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err != nil {
		t.Fatalf("createThingModelItem: %v", err)
	}
	snap, err := publishThingModel(ctx, tenantA, tmA.ID, testActorID(), &publishReq{Changelog: "init"})
	if err != nil {
		t.Fatalf("publishThingModel: %v", err)
	}

	dt, err := createDeviceTemplate(ctx, tenantA, &DeviceTemplate{
		Name:                 "DT-A",
		ThingModelID:         tmA.ID,
		ThingModelSnapshotID: snap.ID,
		ProtocolType:         "MQTT",
		Status:               "DRAFT",
	})
	if err != nil {
		t.Fatalf("createDeviceTemplate: %v", err)
	}

	// Tenant B must not see tenant A's template
	_, err = getDeviceTemplate(ctx, tenantB, dt.ID)
	if err == nil {
		t.Fatal("tenantB should not access tenantA's device template")
	}

	templates, _, err := listDeviceTemplates(ctx, tenantB, &ListDeviceTemplatesReq{
		pageRequest: pageRequest{Page: 1, PageSize: 50},
	})
	if err != nil {
		t.Fatalf("listDeviceTemplates tenantB: %v", err)
	}
	for _, tmpl := range templates {
		if tmpl.ID == dt.ID {
			t.Fatal("tenantB can see tenantA's device template")
		}
	}
}

// TestCrossTenantProductIsolation verifies product cross-tenant isolation.
func TestCrossTenantProductIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantA := testTenantID()
	tenantB := testTenantID()

	// We need a device template first (via thing model)
	tmA, snap := mustPublishedThingModel(t, ctx, tenantA)
	dt := mustDeviceTemplate(t, ctx, tenantA, tmA.ID, snap.ID)

	prod, err := createProduct(ctx, tenantA, &Product{
		Name:             "Prod-A",
		DeviceTemplateID: dt.ID,
	})
	if err != nil {
		t.Fatalf("createProduct: %v", err)
	}

	// Tenant B reads product – must fail
	_, err = getProduct(ctx, tenantB, prod.ID)
	if err == nil {
		t.Fatal("tenantB should not access tenantA's product")
	}

	prods, _, err := listProducts(ctx, tenantB, &ListProductsReq{
		pageRequest: pageRequest{Page: 1, PageSize: 50},
	})
	if err != nil {
		t.Fatalf("listProducts tenantB: %v", err)
	}
	for _, p := range prods {
		if p.ID == prod.ID {
			t.Fatal("tenantB can see tenantA's product")
		}
	}
}

// TestCrossTenantDeviceIsolation verifies device cross-tenant isolation.
func TestCrossTenantDeviceIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantA := testTenantID()
	tenantB := testTenantID()

	tmA, snap := mustPublishedThingModel(t, ctx, tenantA)
	dt := mustDeviceTemplate(t, ctx, tenantA, tmA.ID, snap.ID)

	dev, err := createDevice(ctx, tenantA, &Device{
		DeviceTemplateID: dt.ID,
		SN:               "SN-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("createDevice: %v", err)
	}

	// Tenant B reads device – must fail
	_, err = getDevice(ctx, tenantB, dev.ID)
	if err == nil {
		t.Fatal("tenantB should not access tenantA's device")
	}

	devs, _, err := listDevices(ctx, tenantB, &ListDevicesReq{
		pageRequest: pageRequest{Page: 1, PageSize: 50},
	})
	if err != nil {
		t.Fatalf("listDevices tenantB: %v", err)
	}
	for _, d := range devs {
		if d.ID == dev.ID {
			t.Fatal("tenantB can see tenantA's device")
		}
	}
}

// TestCrossTenantMutationBlocked verifies that tenant B cannot mutate tenant A's resources.
func TestCrossTenantMutationBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantA := testTenantID()
	tenantB := testTenantID()

	tmA, err := createThingModel(ctx, tenantA, &ThingModel{Name: "Model-A", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel: %v", err)
	}

	// Tenant B cannot update tenant A's thing model
	_, err = updateThingModel(ctx, tenantB, tmA.ID, &ThingModel{Name: "Hijacked"})
	if err == nil {
		t.Fatal("tenantB should not update tenantA's thing model")
	}

	// Tenant B cannot delete tenant A's thing model
	err = deleteThingModel(ctx, tenantB, tmA.ID)
	if err == nil {
		t.Fatal("tenantB should not delete tenantA's thing model")
	}

	// Tenant B cannot publish tenant A's thing model
	_, err = publishThingModel(ctx, tenantB, tmA.ID, testActorID(), &publishReq{Changelog: "hack"})
	if err == nil {
		t.Fatal("tenantB should not publish tenantA's thing model")
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func mustPublishedThingModel(t *testing.T, ctx context.Context, tenantID string) (*ThingModel, *ThingModelSnapshot) {
	t.Helper()
	tm, err := createThingModel(ctx, tenantID, &ThingModel{Name: "TM-" + uuid.NewString(), Status: "DRAFT"})
	if err != nil {
		t.Fatalf("mustPublishedThingModel createThingModel: %v", err)
	}
	_, err = createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "v",
		NameI18n:   json.RawMessage(`{"default":"V"}`),
		ValueType:  json.RawMessage(`{"kind":"BOOL"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err != nil {
		t.Fatalf("mustPublishedThingModel createItem: %v", err)
	}
	snap, err := publishThingModel(ctx, tenantID, tm.ID, testActorID(), &publishReq{Changelog: "init"})
	if err != nil {
		t.Fatalf("mustPublishedThingModel publish: %v", err)
	}
	published, err := getThingModelByID(ctx, tenantID, tm.ID)
	if err != nil {
		t.Fatalf("mustPublishedThingModel get: %v", err)
	}
	return published, snap
}

func mustDeviceTemplate(t *testing.T, ctx context.Context, tenantID, thingModelID, snapshotID string) *DeviceTemplate {
	t.Helper()
	dt, err := createDeviceTemplate(ctx, tenantID, &DeviceTemplate{
		Name:                 "DT-" + uuid.NewString(),
		ThingModelID:         thingModelID,
		ThingModelSnapshotID: snapshotID,
		ProtocolType:         "MQTT",
		ProtocolConfig:       json.RawMessage(`{"broker_url":"mqtt://broker.example.com"}`),
		Status:               "DRAFT",
	})
	if err != nil {
		t.Fatalf("mustDeviceTemplate: %v", err)
	}
	published, err := publishDeviceTemplate(ctx, tenantID, dt.ID)
	if err != nil {
		t.Fatalf("mustDeviceTemplate publish: %v", err)
	}
	return published
}
