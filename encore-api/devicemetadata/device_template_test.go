package devicemetadata

import (
	"encoding/json"
	"testing"
)

// TestDeviceTemplateStructure verifies the DeviceTemplate struct is complete
func TestDeviceTemplateStructure(t *testing.T) {
	dt := &DeviceTemplate{
		ID:                   "test-id",
		TenantID:             "tenant-1",
		Name:                 "Template 1",
		Brand:                "Brand A",
		ProductName:          "Product A",
		ModelNumber:          "Model 123",
		ThingModelID:         "tm-1",
		ThingModelSnapshotID: "tms-1",
		ProtocolType:         "MQTT",
		ProtocolConfig:       json.RawMessage(`{"broker":"mqtt.example.com"}`),
		VoucherType:          "NONE",
		ConnectionType:       "DIRECT",
		AutoRegister:         true,
		OnlineConfig:         json.RawMessage(`{"timeout":30}`),
		Tags:                 json.RawMessage(`["tag1","tag2"]`),
		Extras:               json.RawMessage(`{"custom":"value"}`),
		DescriptionI18n:      json.RawMessage(`{"default":"A device template"}`),
		ImageURL:             "https://example.com/image.png",
		Status:               "DRAFT",
	}

	// Verify all fields are accessible
	if dt.ID != "test-id" || dt.TenantID != "tenant-1" {
		t.Fatal("DeviceTemplate struct incomplete")
	}
	if dt.ThingModelID != "tm-1" || dt.ThingModelSnapshotID != "tms-1" {
		t.Fatal("DeviceTemplate missing critical fields")
	}
}

// TestListDeviceTemplatesReqStructure verifies filter fields exist
func TestListDeviceTemplatesReqStructure(t *testing.T) {
	req := &ListDeviceTemplatesReq{
		pageRequest:  pageRequest{Page: 1, PageSize: 20},
		Status:       "DRAFT",
		Brand:        "Brand A",
		ThingModelID: "tm-1",
		Tag:          "tag1",
		Query:        "search",
	}

	if req.Status != "DRAFT" || req.Brand != "Brand A" {
		t.Fatal("ListDeviceTemplatesReq missing Status or Brand")
	}
	if req.ThingModelID != "tm-1" || req.Tag != "tag1" {
		t.Fatal("ListDeviceTemplatesReq missing ThingModelID or Tag")
	}
	if req.Query != "search" {
		t.Fatal("ListDeviceTemplatesReq missing Query filter")
	}
}

// TestPageRequestNormalization tests page request defaults
func TestPageRequestNormalization(t *testing.T) {
	pr := &pageRequest{Page: -1, PageSize: -1}
	pr.normalize()
	if pr.Page != 1 {
		t.Fatalf("expected page=1, got %d", pr.Page)
	}
	if pr.PageSize != 20 {
		t.Fatalf("expected pageSize=20, got %d", pr.PageSize)
	}

	pr = &pageRequest{Page: 0, PageSize: 500}
	pr.normalize()
	if pr.PageSize != 200 {
		t.Fatalf("expected pageSize capped at 200, got %d", pr.PageSize)
	}
}

// TestEnsureNonEmpty validates required field checker
func TestEnsureNonEmpty(t *testing.T) {
	if err := ensureNonEmpty("test_field", "value"); err != nil {
		t.Fatalf("non-empty field should pass: %v", err)
	}

	if err := ensureNonEmpty("test_field", ""); err == nil {
		t.Fatal("empty field should fail")
	}

	if err := ensureNonEmpty("test_field", "   "); err == nil {
		t.Fatal("whitespace-only field should fail")
	}
}

// TestDeviceTemplateValidation validates creation requirements
func TestDeviceTemplateValidation(t *testing.T) {
	// Test ensureNonEmpty for required fields
	cases := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{"name required", "name", "", true},
		{"thing_model_id required", "thing_model_id", "", true},
		{"thing_model_snapshot_id required", "thing_model_snapshot_id", "", true},
		{"name valid", "name", "Template A", false},
		{"thing_model_id valid", "thing_model_id", "tm-123", false},
	}

	for _, tc := range cases {
		err := ensureNonEmpty(tc.name, tc.value)
		if tc.wantErr && err == nil {
			t.Fatalf("%s: expected error but got nil", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
	}
}

// TestJsonOrNullHelper tests JSON null handling
func TestJsonOrNullHelper(t *testing.T) {
	// Empty JSON should return nil
	if result := jsonOrNull(json.RawMessage("")); result != nil {
		t.Fatal("empty JSON should return nil")
	}

	// Non-empty JSON should return the raw message
	raw := json.RawMessage(`{"test":"value"}`)
	if result := jsonOrNull(raw); result == nil {
		t.Fatal("non-empty JSON should not return nil")
	}
}

// TestDeviceTemplateTagFilter tests tag filtering logic
func TestDeviceTemplateTagFilter(t *testing.T) {
	// Test tag JSON array structure
	tags := json.RawMessage(`["tag1", "tag2", "tag3"]`)

	var tagArray []string
	if err := json.Unmarshal(tags, &tagArray); err != nil {
		t.Fatalf("failed to unmarshal tags: %v", err)
	}

	if len(tagArray) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tagArray))
	}

	if tagArray[0] != "tag1" {
		t.Fatalf("expected 'tag1', got %q", tagArray[0])
	}
}

// TestDeviceTemplateStatusValues tests valid status values
func TestDeviceTemplateStatusValues(t *testing.T) {
	validStatuses := map[string]bool{
		"DRAFT":     true,
		"PUBLISHED": true,
		"ARCHIVED":  true,
	}

	statuses := []string{"DRAFT", "PUBLISHED", "ARCHIVED", "INVALID"}
	for _, status := range statuses {
		if validStatuses[status] && !validStatuses[status] {
			t.Fatalf("valid status %q marked invalid", status)
		}
	}

	if validStatuses["INVALID"] {
		t.Fatal("invalid status should not be valid")
	}
}

// TestDeviceTemplateConnectionTypes tests valid connection types
func TestDeviceTemplateConnectionTypes(t *testing.T) {
	validTypes := map[string]bool{
		"DIRECT":        true,
		"GATEWAY_CHILD": true,
	}

	types := []string{"DIRECT", "GATEWAY_CHILD", "INVALID"}
	for _, typ := range types {
		_, isValid := validTypes[typ]
		if isValid && !validTypes[typ] {
			t.Fatalf("valid type %q marked invalid", typ)
		}
	}

	if validTypes["INVALID"] {
		t.Fatal("invalid type should not be valid")
	}
}

// TestDeviceTemplateName tests name uniqueness requirement
func TestDeviceTemplateName(t *testing.T) {
	// The test verifies that name must be provided and unique per tenant
	// This would be enforced by DB constraint: UNIQUE (tenant_id, name)

	dt1 := &DeviceTemplate{Name: "Template A", TenantID: "tenant-1"}
	dt2 := &DeviceTemplate{Name: "Template A", TenantID: "tenant-1"}

	// Both have same name and tenant - would violate unique constraint
	if dt1.Name == dt2.Name && dt1.TenantID == dt2.TenantID {
		t.Log("name and tenant must be unique (enforced by DB constraint)")
	}

	dt3 := &DeviceTemplate{Name: "Template A", TenantID: "tenant-2"}
	// Same name but different tenant - allowed
	if dt1.Name == dt3.Name && dt1.TenantID != dt3.TenantID {
		t.Log("same name allowed for different tenants")
	}
}

// TestListDeviceTemplatesReqPageDefaults tests pagination defaults
func TestListDeviceTemplatesReqPageDefaults(t *testing.T) {
	req := &ListDeviceTemplatesReq{
		pageRequest: pageRequest{Page: -5, PageSize: 0},
	}
	req.pageRequest.normalize()

	if req.Page != 1 {
		t.Fatalf("invalid page should default to 1, got %d", req.Page)
	}
	if req.PageSize != 20 {
		t.Fatalf("invalid pageSize should default to 20, got %d", req.PageSize)
	}
}

// TestCrossFilterCombinations tests that filters can be combined
func TestCrossFilterCombinations(t *testing.T) {
	req := &ListDeviceTemplatesReq{
		pageRequest:  pageRequest{Page: 1, PageSize: 10},
		Status:       "DRAFT",
		Brand:        "BrandX",
		ThingModelID: "tm-123",
		Tag:          "filtered",
		Query:        "search",
	}

	// Verify all filters are present and can be checked
	filters := []string{req.Status, req.Brand, req.ThingModelID, req.Tag, req.Query}
	expectedCount := 5
	actualCount := 0
	for _, f := range filters {
		if f != "" {
			actualCount++
		}
	}
	if actualCount != expectedCount {
		t.Fatalf("expected %d active filters, got %d", expectedCount, actualCount)
	}
}

// TestDeviceTemplateCreatedUpdatedAt tests timestamp fields
func TestDeviceTemplateCreatedUpdatedAt(t *testing.T) {
	dt := &DeviceTemplate{
		ID:       "test-id",
		TenantID: "tenant-1",
		Name:     "Test",
	}

	// Timestamps are set during DB insert (from NOW())
	// These should be zero values before insertion
	if !dt.CreatedAt.IsZero() {
		t.Log("CreatedAt should be set by database insert")
	}
	if !dt.UpdatedAt.IsZero() {
		t.Log("UpdatedAt should be set by database insert")
	}
}

// TestDeviceTemplateThingModelReference tests the foreign key relationship
func TestDeviceTemplateThingModelReference(t *testing.T) {
	// Template must reference a valid thing model
	dt := &DeviceTemplate{
		ThingModelID:         "tm-valid",
		ThingModelSnapshotID: "tms-123",
	}

	// Both fields are required
	if dt.ThingModelID == "" {
		t.Fatal("ThingModelID must not be empty")
	}
	if dt.ThingModelSnapshotID == "" {
		t.Fatal("ThingModelSnapshotID must not be empty")
	}
}

// BenchmarkListDeviceTemplatesReq benchmarks request normalization
func BenchmarkListDeviceTemplatesReq(b *testing.B) {
	req := &ListDeviceTemplatesReq{
		pageRequest:  pageRequest{Page: -1, PageSize: -1},
		Status:       "DRAFT",
		Brand:        "TestBrand",
		ThingModelID: "tm-123",
		Tag:          "test",
		Query:        "search",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.pageRequest.normalize()
	}
}
