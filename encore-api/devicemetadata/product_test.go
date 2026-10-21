package devicemetadata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestProductStructure verifies the Product struct includes all required fields
func TestProductStructure(t *testing.T) {
	p := &Product{
		ID:               "prod-1",
		TenantID:         "tenant-1",
		Name:             "Product A",
		DeviceTemplateID: "dt-1",
		ProductKey:       "ABC123DEF456",
		SKU:              "SKU-001",
		ImageURL:         "https://example.com/image.png",
		OTAPackageID:     nil,
		DescriptionI18n:  json.RawMessage(`{"default":"A product"}`),
	}

	if p.ID != "prod-1" || p.TenantID != "tenant-1" {
		t.Fatal("Product struct missing basic fields")
	}
	if p.DeviceTemplateID != "dt-1" || p.ProductKey != "ABC123DEF456" {
		t.Fatal("Product struct missing template_id or product_key")
	}
	if p.DescriptionI18n == nil {
		t.Fatal("Product struct missing DescriptionI18n")
	}
}

// TestProductWithDeletedAt verifies the Product struct has DeletedAt field
func TestProductWithDeletedAt(t *testing.T) {
	p := &Product{
		ID:        "prod-1",
		TenantID:  "tenant-1",
		Name:      "Product A",
		DeletedAt: nil,
	}

	if p.DeletedAt != nil {
		t.Fatal("Product should initially have nil DeletedAt")
	}

	deletedTime := jsonTimeNow()
	p.DeletedAt = &deletedTime

	if p.DeletedAt == nil {
		t.Fatal("Product should support DeletedAt timestamp")
	}
}

// TestProductKeyGeneration verifies product key generation works and produces valid keys
func TestProductKeyGeneration(t *testing.T) {
	key1, err := generateProductKey()
	if err != nil {
		t.Fatalf("Failed to generate product key: %v", err)
	}
	if len(key1) == 0 {
		t.Fatal("Generated product key is empty")
	}

	key2, err := generateProductKey()
	if err != nil {
		t.Fatalf("Failed to generate second product key: %v", err)
	}
	if len(key2) == 0 {
		t.Fatal("Generated product key is empty")
	}

	if key1 == key2 {
		t.Fatal("Generated keys should be different (or extremely unlikely to be identical)")
	}

	if !isBase32ValidKey(key1) {
		t.Fatalf("Generated key is not valid base32: %s", key1)
	}

	if !isBase32ValidKey(key2) {
		t.Fatalf("Generated key is not valid base32: %s", key2)
	}

	if len(key1) < 14 || len(key1) > 18 {
		t.Fatalf("Generated key length is invalid (expected ~16): %d", len(key1))
	}

	if len(key2) < 14 || len(key2) > 18 {
		t.Fatalf("Generated key length is invalid (expected ~16): %d", len(key2))
	}
}

// TestProductKeyProperties tests that auto-generated keys are not empty
func TestProductKeyProperties(t *testing.T) {
	for i := 0; i < 5; i++ {
		key, err := generateProductKey()
		if err != nil {
			t.Fatalf("iteration %d: failed to generate key: %v", i, err)
		}
		if key == "" {
			t.Fatalf("iteration %d: generated empty key", i)
		}
		if len(key) < 14 {
			t.Fatalf("iteration %d: key too short (%d): %s", i, len(key), key)
		}
	}
}

// TestListProductsReqStructure verifies filter fields exist
func TestListProductsReqStructure(t *testing.T) {
	req := &ListProductsReq{
		pageRequest:      pageRequest{Page: 1, PageSize: 20},
		DeviceTemplateID: "dt-1",
		Name:             "Product A",
	}

	if req.DeviceTemplateID != "dt-1" {
		t.Fatal("ListProductsReq missing DeviceTemplateID")
	}
	if req.Name != "Product A" {
		t.Fatal("ListProductsReq missing Name")
	}
	if req.Page != 1 || req.PageSize != 20 {
		t.Fatal("ListProductsReq missing pageRequest fields")
	}
}

// TestListProductsReqPagination verifies pagination fields work
func TestListProductsReqPagination(t *testing.T) {
	req := &ListProductsReq{
		pageRequest: pageRequest{Page: 5, PageSize: 50},
	}
	req.pageRequest.normalize()

	if req.Page != 5 {
		t.Fatal("ListProductsReq pagination broken")
	}
	if req.PageSize != 50 {
		t.Fatal("ListProductsReq page size broken")
	}
}

// TestGetProductByKeyReqStructure verifies the structure exists
func TestGetProductByKeyReqStructure(t *testing.T) {
	req := &GetProductByKeyReq{
		ProductKey: "TEST123KEY",
	}

	if req.ProductKey != "TEST123KEY" {
		t.Fatal("GetProductByKeyReq missing ProductKey")
	}
}

// TestGetProductByKeyReqEmpty verifies empty product key handling
func TestGetProductByKeyReqEmpty(t *testing.T) {
	req := &GetProductByKeyReq{
		ProductKey: "",
	}

	if req.ProductKey != "" {
		t.Fatal("GetProductByKeyReq should allow empty ProductKey")
	}
}

// Helper to get current time for testing
func jsonTimeNow() time.Time {
	return time.Now()
}

// Helper to validate base32 encoding (allows lowercase and no padding)
// This matches the generateProductKey function's output format
func isBase32ValidKey(key string) bool {
	if len(key) == 0 {
		return false
	}
	validChars := "abcdefghijklmnopqrstuvwxyz234567"
	for _, ch := range key {
		if !strings.ContainsRune(validChars, ch) {
			return false
		}
	}
	return true
}
