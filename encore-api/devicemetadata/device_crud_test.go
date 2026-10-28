package devicemetadata

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func testTenantID() string {
	return uuid.NewString()
}

func testActorID() string {
	return uuid.NewString()
}

func setupTestTenant(t *testing.T) string {
	t.Helper()
	return testTenantID()
}

func createTestThingModel(t *testing.T, ctx context.Context, tenantID string) *ThingModel {
	tm := &ThingModel{
		Name:     "test-model-" + uuid.NewString(),
		Category: "test",
		Status:   "DRAFT",
	}
	created, err := createThingModel(ctx, tenantID, tm)
	require.NoError(t, err)
	return created
}

func createPublishedThingModel(t *testing.T, ctx context.Context, tenantID string) (*ThingModel, *ThingModelSnapshot) {
	tm := createTestThingModel(t, ctx, tenantID)

	// Add at least one item
	item := &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "power",
		NameI18n:   json.RawMessage(`{"default":"Power"}`),
		ValueType:  json.RawMessage(`{"kind":"bool"}`),
		Access:     json.RawMessage(`{"read":true,"write":true}`),
	}
	_, err := createThingModelItem(ctx, tenantID, tm.ID, item)
	require.NoError(t, err)

	// Publish
	snap, err := publishThingModel(ctx, tenantID, tm.ID, uuid.NewString(), &publishReq{Changelog: "test"})
	require.NoError(t, err)

	// Refresh to get PUBLISHED status
	published, err := getThingModelByID(ctx, tenantID, tm.ID)
	require.NoError(t, err)

	return published, snap
}

func createTestDeviceTemplate(t *testing.T, ctx context.Context, tenantID string, status string) *DeviceTemplate {
	tm, snap := createPublishedThingModel(t, ctx, tenantID)

	dt := &DeviceTemplate{
		Name:                 "test-template-" + uuid.NewString(),
		ThingModelID:         tm.ID,
		ThingModelSnapshotID: snap.ID,
		ProtocolType:         "MQTT",
		ProtocolConfig:       json.RawMessage(`{"broker_url":"mqtt://broker.example.com"}`),
		Status:               status,
	}
	created, err := createDeviceTemplate(ctx, tenantID, dt)
	require.NoError(t, err)
	return created
}

func createPublishedDeviceTemplate(t *testing.T, ctx context.Context, tenantID string) *DeviceTemplate {
	dt := createTestDeviceTemplate(t, ctx, tenantID, "DRAFT")
	published, err := publishDeviceTemplate(ctx, tenantID, dt.ID)
	require.NoError(t, err)
	return published
}

func createTestProduct(t *testing.T, ctx context.Context, tenantID string, templateID string) *Product {
	p := &Product{
		Name:             "test-product-" + uuid.NewString(),
		DeviceTemplateID: templateID,
	}
	created, err := createProduct(ctx, tenantID, p)
	require.NoError(t, err)
	return created
}

// CRUD Tests

func TestCreateDevice_Success(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)

	req := &Device{
		DeviceTemplateID: tmpl.ID,
		ActivateFlag:     "INACTIVE",
	}
	device, err := createDevice(ctx, tenantID, req)

	require.NoError(t, err)
	assert.NotEmpty(t, device.ID)
	assert.Equal(t, tenantID, device.TenantID)
	assert.Equal(t, tmpl.ID, device.DeviceTemplateID)
	assert.NotEmpty(t, device.SN)       // Auto-generated
	assert.Len(t, device.SN, 32)        // Nanoid-style 32 chars
	assert.NotEmpty(t, device.Voucher)  // Returned on create
	assert.Equal(t, "INACTIVE", device.ActivateFlag)
	assert.Equal(t, 0, device.IsOnline)
}

func TestCreateDevice_AutoGenerateSN(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)

	req := &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "", // Empty, should auto-generate
	}
	device, err := createDevice(ctx, tenantID, req)

	require.NoError(t, err)
	assert.NotEmpty(t, device.SN)
	assert.Len(t, device.SN, 32)
}

func TestCreateDevice_CustomSN(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)

	customSN := "CUSTOM-SN-12345"
	req := &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               customSN,
	}
	device, err := createDevice(ctx, tenantID, req)

	require.NoError(t, err)
	assert.Equal(t, customSN, device.SN)
}

func TestCreateDevice_TemplateNotFound(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	req := &Device{
		DeviceTemplateID: uuid.NewString(), // Non-existent
		SN:               "test-sn",
	}
	_, err := createDevice(ctx, tenantID, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "device template not found")
}

func TestCreateDevice_TemplateNotPublished(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	// Create DRAFT template
	dt := createTestDeviceTemplate(t, ctx, tenantID, "DRAFT")

	req := &Device{
		DeviceTemplateID: dt.ID,
		SN:               "test-sn",
	}
	_, err := createDevice(ctx, tenantID, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be PUBLISHED")
}

func TestCreateDevice_SNUniquenessPerTenant(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)

	sn := "UNIQUE-SN-" + uuid.NewString()
	req := &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               sn,
	}

	// Create first device
	device1, err := createDevice(ctx, tenantID, req)
	require.NoError(t, err)
	assert.Equal(t, sn, device1.SN)

	// Try to create another with same SN
	req2 := &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               sn,
	}
	_, err = createDevice(ctx, tenantID, req2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestCreateDevice_ProductMismatch(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl1 := createPublishedDeviceTemplate(t, ctx, tenantID)
	tmpl2 := createPublishedDeviceTemplate(t, ctx, tenantID)

	// Create product for template1
	prod := createTestProduct(t, ctx, tenantID, tmpl1.ID)

	// Try to create device with template2 but product from template1
	req := &Device{
		DeviceTemplateID: tmpl2.ID,
		ProductID:        &prod.ID,
		SN:               "test-sn",
	}
	_, err := createDevice(ctx, tenantID, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestCreateDevice_ParentDeviceNotGateway(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)

	// Create first device (not a gateway)
	parent, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "parent-sn",
	})
	require.NoError(t, err)

	// Try to use non-gateway as parent
	req := &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "child-sn",
		ParentDeviceID:   &parent.ID,
	}
	_, err = createDevice(ctx, tenantID, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a GATEWAY")
}

func TestGetDevice_Success(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	created, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	// Get device
	device, err := getDevice(ctx, tenantID, created.ID)
	require.NoError(t, err)

	assert.Equal(t, created.ID, device.ID)
	assert.Empty(t, device.Voucher) // Voucher should be sanitized
}

func TestGetDevice_VoucherSanitized(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	_, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	// Get device - voucher should not be in response
	devices, _, err := listDevices(ctx, tenantID, &ListDevicesReq{pageRequest: pageRequest{Page: 1, PageSize: 10}})
	require.NoError(t, err)
	assert.Len(t, devices, 1)
	assert.Empty(t, devices[0].Voucher)
}

func TestGetDevice_NotFound(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	_, err := getDevice(ctx, tenantID, uuid.NewString())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetDeviceBySN_Success(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	sn := "unique-sn-" + uuid.NewString()
	created, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               sn,
	})
	require.NoError(t, err)

	device, err := getDeviceBySN(ctx, tenantID, sn)
	require.NoError(t, err)
	assert.Equal(t, created.ID, device.ID)
}

func TestListDevices_WithFilters(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	prod := createTestProduct(t, ctx, tenantID, tmpl.ID)

	// Create devices
	_, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		ProductID:        &prod.ID,
		SN:               "device-1",
		ActivateFlag:     "ACTIVE",
	})
	require.NoError(t, err)

	_, err = createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "device-2",
		ActivateFlag:     "INACTIVE",
	})
	require.NoError(t, err)

	// Filter by SN
	devices, total, err := listDevices(ctx, tenantID, &ListDevicesReq{
		pageRequest: pageRequest{Page: 1, PageSize: 10},
		SN:          "device-1",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, devices, 1)

	// Filter by status
	devices, total, err = listDevices(ctx, tenantID, &ListDevicesReq{
		pageRequest: pageRequest{Page: 1, PageSize: 10},
		Status:      "ACTIVE",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, devices, 1)
	assert.Equal(t, "device-1", devices[0].SN)
}

func TestUpdateDevice_OnlyAllowedFields(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	created, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "test-sn",
		ActivateFlag:     "INACTIVE",
	})
	require.NoError(t, err)

	// Update activate flag
	updated, err := updateDevice(ctx, tenantID, created.ID, &Device{
		ActivateFlag: "ACTIVE",
	})
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", updated.ActivateFlag)
	assert.Equal(t, created.SN, updated.SN) // SN unchanged
}

func TestDeleteDevice_SoftDelete(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	created, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	// Delete
	err = deleteDevice(ctx, tenantID, created.ID)
	require.NoError(t, err)

	// Verify it's deleted (GetDevice should fail)
	_, err = getDevice(ctx, tenantID, created.ID)
	require.Error(t, err)
}

// Actuate Tests

func TestActuate_PropertyWriteSuccess(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	// Create template with writable property
	tm, snap := createPublishedThingModel(t, ctx, tenantID)

	tmpl := &DeviceTemplate{
		Name:                 "template-" + uuid.NewString(),
		ThingModelID:         tm.ID,
		ThingModelSnapshotID: snap.ID,
		ProtocolType:         "MQTT",
		ProtocolConfig:       json.RawMessage(`{"broker_url":"mqtt://broker.example.com"}`),
		Status:               "DRAFT",
	}
	created, err := createDeviceTemplate(ctx, tenantID, tmpl)
	require.NoError(t, err)

	published, err := publishDeviceTemplate(ctx, tenantID, created.ID)
	require.NoError(t, err)

	device, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: published.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	// Actuate
	resp, err := actuate(ctx, tenantID, device.ID, &ActuateReq{
		Identifier: "power",
		Value:      json.RawMessage(`true`),
	})

	require.NoError(t, err)
	assert.True(t, resp.Valid)
	assert.Empty(t, resp.Errors)
}

func TestActuate_ReadOnlyProperty(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tm := createTestThingModel(t, ctx, tenantID)

	// Add read-only property
	item := &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "temperature",
		NameI18n:   json.RawMessage(`{"default":"Temperature"}`),
		ValueType:  json.RawMessage(`{"kind":"int"}`),
		Access:     json.RawMessage(`{"read":true,"write":false}`), // Read-only
	}
	_, err := createThingModelItem(ctx, tenantID, tm.ID, item)
	require.NoError(t, err)

	snap, err := publishThingModel(ctx, tenantID, tm.ID, uuid.NewString(), &publishReq{Changelog: "test"})
	require.NoError(t, err)

	tmpl := &DeviceTemplate{
		Name:                 "template-" + uuid.NewString(),
		ThingModelID:         tm.ID,
		ThingModelSnapshotID: snap.ID,
		ProtocolType:         "MQTT",
		ProtocolConfig:       json.RawMessage(`{"broker_url":"mqtt://broker.example.com"}`),
		Status:               "DRAFT",
	}
	dt, err := createDeviceTemplate(ctx, tenantID, tmpl)
	require.NoError(t, err)

	published, err := publishDeviceTemplate(ctx, tenantID, dt.ID)
	require.NoError(t, err)

	device, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: published.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	// Try to actuate (should fail - not writable)
	resp, err := actuate(ctx, tenantID, device.ID, &ActuateReq{
		Identifier: "temperature",
		Value:      json.RawMessage(`25`),
	})

	require.NoError(t, err)
	assert.False(t, resp.Valid)
	assert.Len(t, resp.Errors, 1)
	assert.Equal(t, "BAD_ACCESS", resp.Errors[0].Code)
}

func TestActuate_IdentifierNotFound(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	device, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	resp, err := actuate(ctx, tenantID, device.ID, &ActuateReq{
		Identifier: "nonexistent",
	})

	require.NoError(t, err)
	assert.False(t, resp.Valid)
	assert.Len(t, resp.Errors, 1)
	assert.Equal(t, "IDENTIFIER_NOT_FOUND", resp.Errors[0].Code)
}

func TestActuate_InvalidValue(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tm := createTestThingModel(t, ctx, tenantID)

	// Add int property with constraints
	item := &ThingModelItem{
		Type:       "PROPERTY",
		Identifier: "level",
		NameI18n:   json.RawMessage(`{"default":"Level"}`),
		ValueType:  json.RawMessage(`{"kind":"int","constraint":{"min":0,"max":100}}`),
		Access:     json.RawMessage(`{"read":true,"write":true}`),
	}
	_, err := createThingModelItem(ctx, tenantID, tm.ID, item)
	require.NoError(t, err)

	snap, err := publishThingModel(ctx, tenantID, tm.ID, uuid.NewString(), &publishReq{Changelog: "test"})
	require.NoError(t, err)

	tmpl := &DeviceTemplate{
		Name:                 "template-" + uuid.NewString(),
		ThingModelID:         tm.ID,
		ThingModelSnapshotID: snap.ID,
		ProtocolType:         "MQTT",
		ProtocolConfig:       json.RawMessage(`{"broker_url":"mqtt://broker.example.com"}`),
		Status:               "DRAFT",
	}
	dt, err := createDeviceTemplate(ctx, tenantID, tmpl)
	require.NoError(t, err)

	published, err := publishDeviceTemplate(ctx, tenantID, dt.ID)
	require.NoError(t, err)

	device, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: published.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	// Try to actuate with invalid value (150 > max 100)
	resp, err := actuate(ctx, tenantID, device.ID, &ActuateReq{
		Identifier: "level",
		Value:      json.RawMessage(`150`),
	})

	require.NoError(t, err)
	assert.False(t, resp.Valid)
	assert.Len(t, resp.Errors, 1)
	assert.Equal(t, "VALUE_INVALID", resp.Errors[0].Code)
}

func TestActuate_Idempotency(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	device, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	idempotencyKey := "test-key-" + uuid.NewString()

	req := &ActuateReq{
		Identifier:     "power",
		Value:          json.RawMessage(`true`),
		IdempotencyKey: idempotencyKey,
	}

	resp1, err := actuate(ctx, tenantID, device.ID, req)
	require.NoError(t, err)

	// Same request again should return cached result
	resp2, err := actuate(ctx, tenantID, device.ID, req)
	require.NoError(t, err)

	assert.Equal(t, resp1.Valid, resp2.Valid)
	assert.Equal(t, resp1.Errors, resp2.Errors)
}

func TestActuate_CrossTenantNotFound(t *testing.T) {
	ctx := context.Background()
	tenantID := setupTestTenant(t)

	tmpl := createPublishedDeviceTemplate(t, ctx, tenantID)
	device, err := createDevice(ctx, tenantID, &Device{
		DeviceTemplateID: tmpl.ID,
		SN:               "test-sn",
	})
	require.NoError(t, err)

	// Try to access with different tenant
	otherTenant := setupTestTenant(t)
	_, err = getDevice(ctx, otherTenant, device.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateDevice_CrossTenantIsolation(t *testing.T) {
	ctx := context.Background()
	tenant1 := setupTestTenant(t)
	tenant2 := setupTestTenant(t)

	tmpl1 := createPublishedDeviceTemplate(t, ctx, tenant1)
	sn := "shared-sn"

	// Create device in tenant1
	_, err := createDevice(ctx, tenant1, &Device{
		DeviceTemplateID: tmpl1.ID,
		SN:               sn,
	})
	require.NoError(t, err)

	// Same SN should be allowed in different tenant
	tmpl2 := createPublishedDeviceTemplate(t, ctx, tenant2)
	device2, err := createDevice(ctx, tenant2, &Device{
		DeviceTemplateID: tmpl2.ID,
		SN:               sn,
	})
	require.NoError(t, err)
	assert.Equal(t, sn, device2.SN)
}
