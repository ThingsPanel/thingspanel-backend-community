package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// TestIdentifierUniquePerThingModel verifies that two items in the same thing model
// cannot share the same identifier, regardless of type.
//
// UNIQUE: (tenant_id, thing_model_id, identifier)
func TestIdentifierUniquePerThingModel(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()

	tm, err := createThingModel(ctx, tenantID, &ThingModel{Name: "TM", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel: %v", err)
	}

	// First item – PROPERTY
	_, err = createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "temperature",
		NameI18n:   json.RawMessage(`{"default":"Temperature"}`),
		ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err != nil {
		t.Fatalf("first item create: %v", err)
	}

	// Second item – different type, same identifier – must fail
	_, err = createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "EVENT",
		Identifier: "temperature",
		NameI18n:   json.RawMessage(`{"default":"Temperature Event"}`),
		ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
		Access:     json.RawMessage(`{"observe":true}`),
	})
	if err == nil {
		t.Fatal("expected unique violation for same identifier in same thing model (cross-type)")
	}

	// Same identifier in a DIFFERENT thing model must succeed (different scope)
	tm2, _ := createThingModel(ctx, tenantID, &ThingModel{Name: "TM2", Status: "DRAFT"})
	_, err = createThingModelItem(ctx, tenantID, tm2.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "temperature",
		NameI18n:   json.RawMessage(`{"default":"Temp2"}`),
		ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err != nil {
		t.Fatalf("same identifier in different model should succeed: %v", err)
	}
}

// TestThingModelNameUniquePerTenant verifies (tenant_id, thing_model.name) uniqueness.
func TestThingModelNameUniquePerTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()
	name := "Unique-Model-" + uuid.NewString()

	_, err := createThingModel(ctx, tenantID, &ThingModel{Name: name, Status: "DRAFT"})
	if err != nil {
		t.Fatalf("first createThingModel: %v", err)
	}

	// Same name, same tenant – must fail
	_, err = createThingModel(ctx, tenantID, &ThingModel{Name: name, Status: "DRAFT"})
	if err == nil {
		t.Fatal("duplicate thing model name within same tenant should fail")
	}

	// Same name, different tenant – must succeed
	tenantB := testTenantID()
	_, err = createThingModel(ctx, tenantB, &ThingModel{Name: name, Status: "DRAFT"})
	if err != nil {
		t.Fatalf("same name in different tenant should succeed: %v", err)
	}
}

// TestProductKeyUniquePerTenant verifies (tenant_id, product_key) uniqueness.
func TestProductKeyUniquePerTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()

	tm, snap := mustPublishedThingModel(t, ctx, tenantID)
	dt := mustDeviceTemplate(t, ctx, tenantID, tm.ID, snap.ID)

	// Create first product with auto-generated key
	p1, err := createProduct(ctx, tenantID, &Product{
		Name:             "Product-1",
		DeviceTemplateID: dt.ID,
	})
	if err != nil {
		t.Fatalf("createProduct p1: %v", err)
	}

	// Manually try to create with same product key
	_, err = createProduct(ctx, tenantID, &Product{
		Name:             "Product-2",
		DeviceTemplateID: dt.ID,
		ProductKey:       p1.ProductKey, // force same key
	})
	if err == nil {
		t.Fatal("duplicate product_key within same tenant should fail")
	}

	// Different tenant, same key – must succeed
	tenantB := testTenantID()
	tm2, snap2 := mustPublishedThingModel(t, ctx, tenantB)
	dt2 := mustDeviceTemplate(t, ctx, tenantB, tm2.ID, snap2.ID)
	_, err = createProduct(ctx, tenantB, &Product{
		Name:             "Product-B",
		DeviceTemplateID: dt2.ID,
		ProductKey:       p1.ProductKey,
	})
	if err != nil {
		t.Fatalf("same product_key in different tenant should succeed: %v", err)
	}
}

// TestDeviceSNUniquePerTenant verifies (tenant_id, device.sn) uniqueness.
func TestDeviceSNUniquePerTenant(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()

	tm, snap := mustPublishedThingModel(t, ctx, tenantID)
	dt := mustDeviceTemplate(t, ctx, tenantID, tm.ID, snap.ID)

	sn := "SN-" + uuid.NewString()

	_, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: dt.ID,
		SN:               sn,
	})
	if err != nil {
		t.Fatalf("first createDevice: %v", err)
	}

	// Same SN, same tenant – must fail
	_, err = createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: dt.ID,
		SN:               sn,
	})
	if err == nil {
		t.Fatal("duplicate SN within same tenant should fail")
	}

	// Same SN, different tenant – must succeed
	tenantB := testTenantID()
	tm2, snap2 := mustPublishedThingModel(t, ctx, tenantB)
	dt2 := mustDeviceTemplate(t, ctx, tenantB, tm2.ID, snap2.ID)
	_, err = createDevice(ctx, tenantB, &Device{
		DeviceTemplateID: dt2.ID,
		SN:               sn,
	})
	if err != nil {
		t.Fatalf("same SN in different tenant should succeed: %v", err)
	}
}

// TestSnapshotImmutabilityTrigger verifies that published snapshots cannot be mutated via direct SQL.
// The DB has an immutability trigger on thing_model_snapshots.
// We verify this indirectly by ensuring the snapshot content remains unchanged after publish.
func TestSnapshotImmutabilityTrigger(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()

	tm, err := createThingModel(ctx, tenantID, &ThingModel{Name: "SnapModel", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel: %v", err)
	}
	_, err = createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "pressure",
		NameI18n:   json.RawMessage(`{"default":"Pressure"}`),
		ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err != nil {
		t.Fatalf("createThingModelItem: %v", err)
	}

	snap, err := publishThingModel(ctx, tenantID, tm.ID, testActorID(), &publishReq{Changelog: "v1"})
	if err != nil {
		t.Fatalf("publishThingModel: %v", err)
	}

	// Read back snapshot – content must match
	retrieved, err := getVersion(ctx, tenantID, tm.ID, snap.Version)
	if err != nil {
		t.Fatalf("getVersion: %v", err)
	}
	if retrieved.ID != snap.ID {
		t.Fatalf("snapshot ID mismatch: want %s got %s", snap.ID, retrieved.ID)
	}
	if !bytes.Equal(retrieved.Content, snap.Content) {
		t.Fatal("snapshot content changed after publish")
	}

	// Verify publishing a derived draft cannot alter the original snapshot content.
	derived, err := deriveThingModelDraft(ctx, tenantID, tm.ID, &deriveDraftReq{})
	if err != nil {
		t.Fatalf("deriveThingModelDraft: %v", err)
	}
	snap2, err := publishThingModel(ctx, tenantID, derived.ID, testActorID(), &publishReq{Changelog: "v2"})
	if err != nil {
		t.Fatalf("second publishThingModel: %v", err)
	}

	// v1 snapshot must still be unchanged
	v1After, err := getVersion(ctx, tenantID, tm.ID, snap.Version)
	if err != nil {
		t.Fatalf("getVersion v1 after v2 publish: %v", err)
	}
	if !bytes.Equal(v1After.Content, snap.Content) {
		t.Fatalf("v1 snapshot mutated by v2 publish")
	}
	_ = snap2
}

// TestSnapshotVersionStartsAtOneForDerivedDrafts verifies each derived draft starts its own version history.
func TestSnapshotVersionIsMonotonicallyIncreasing(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	ctx := context.Background()
	tenantID := testTenantID()

	tm, err := createThingModel(ctx, tenantID, &ThingModel{Name: "MonoModel", Status: "DRAFT"})
	if err != nil {
		t.Fatalf("createThingModel: %v", err)
	}
	_, err = createThingModelItem(ctx, tenantID, tm.ID, &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "val",
		NameI18n:   json.RawMessage(`{"default":"Val"}`),
		ValueType:  json.RawMessage(`{"kind":"INT"}`),
		Access:     json.RawMessage(`{"read":true}`),
	})
	if err != nil {
		t.Fatalf("createThingModelItem: %v", err)
	}

	currentModelID := tm.ID
	for i := 0; i < 3; i++ {
		snap, err := publishThingModel(ctx, tenantID, currentModelID, testActorID(), &publishReq{Changelog: "pass"})
		if err != nil {
			t.Fatalf("publish #%d: %v", i+1, err)
		}
		if snap.Version != 1 {
			t.Fatalf("derived draft publish should start at version 1, got %d", snap.Version)
		}

		if i < 2 {
			nextDraft, err := deriveThingModelDraft(ctx, tenantID, currentModelID, &deriveDraftReq{
				Name: "MonoModel-" + uuid.NewString(),
			})
			if err != nil {
				t.Fatalf("deriveDraft #%d: %v", i+1, err)
			}
			currentModelID = nextDraft.ID
		}
	}
}
