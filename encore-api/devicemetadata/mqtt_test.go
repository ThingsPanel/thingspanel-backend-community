package devicemetadata

import (
	"encoding/json"
	"testing"
)

func TestExtractSN(t *testing.T) {
	if got := extractSN("devices/SN001/property/up"); got != "SN001" {
		t.Fatalf("expected SN001, got %q", got)
	}
}

func TestValidateItemValueType(t *testing.T) {
	spec := json.RawMessage(`{"kind":"FLOAT","constraint":{"min":0,"max":100}}`)
	if err := validateItemValueType(spec, 42.5); err != nil {
		t.Fatalf("expected value to pass validation: %v", err)
	}
	if err := validateItemValueType(spec, 142.5); err == nil {
		t.Fatal("expected value above max to fail")
	}
}
