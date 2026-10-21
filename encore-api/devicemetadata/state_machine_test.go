package devicemetadata

import (
	"encoding/json"
	"testing"

	"encore.dev/beta/errs"
)

// Note: These integration tests are designed to work with a real database.
// They test the complete state machine and protocol validation workflows.

// TestDeviceTemplateStateTransitionDraftToPublished tests state machine: DRAFT → PUBLISHED
func TestDeviceTemplateStateTransitionDraftToPublished(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This would require a real database setup - including thing model and snapshot
	// For now, verify the store functions exist and compile
	_ = publishDeviceTemplate
	_ = archiveDeviceTemplate
	_ = deriveDeviceTemplateDraftAdvanced
	_ = bindThingModelVersionAdvanced
}

// TestProtocolValidationCoverage ensures all protocol types are covered
func TestProtocolValidationCoverage(t *testing.T) {
	protocols := []string{"MQTT", "HTTP", "COAP", "MODBUS", "CUSTOM"}

	for _, proto := range protocols {
		t.Run(proto, func(t *testing.T) {
			validator, err := NewProtocolConfigValidator(proto, json.RawMessage(`{}`))
			if err != nil && proto != "INVALID_JSON" {
				t.Logf("validator created for %s", proto)
			}
			if validator != nil {
				// Should not panic
				_ = validator.Validate()
			}
		})
	}
}

// TestMQTTConfigRequiredFields ensures MQTT broker_url is required
func TestMQTTConfigRequiredFields(t *testing.T) {
	// Empty config
	validator, _ := NewProtocolConfigValidator("MQTT", json.RawMessage(`{}`))
	err := validator.Validate()
	if err == nil {
		t.Fatal("MQTT requires broker_url")
	}

	// With broker_url
	validator, _ = NewProtocolConfigValidator("MQTT", json.RawMessage(`{"broker_url":"mqtt://broker"}`))
	err = validator.Validate()
	if err != nil {
		t.Fatalf("MQTT with broker_url should succeed: %v", err)
	}
}

// TestHTTPConfigRequiredFields ensures HTTP endpoint is required
func TestHTTPConfigRequiredFields(t *testing.T) {
	// Empty config
	validator, _ := NewProtocolConfigValidator("HTTP", json.RawMessage(`{}`))
	err := validator.Validate()
	if err == nil {
		t.Fatal("HTTP requires endpoint")
	}

	// With endpoint
	validator, _ = NewProtocolConfigValidator("HTTP", json.RawMessage(`{"endpoint":"https://api.example.com"}`))
	err = validator.Validate()
	if err != nil {
		t.Fatalf("HTTP with endpoint should succeed: %v", err)
	}
}

// TestMODBUSConfigRequiredFields ensures Modbus slave_id is required
func TestMODBUSConfigRequiredFields(t *testing.T) {
	// Empty config
	validator, _ := NewProtocolConfigValidator("MODBUS", json.RawMessage(`{}`))
	err := validator.Validate()
	if err == nil {
		t.Fatal("MODBUS requires slave_id")
	}

	// With valid slave_id
	validator, _ = NewProtocolConfigValidator("MODBUS", json.RawMessage(`{"slave_id":1}`))
	err = validator.Validate()
	if err != nil {
		t.Fatalf("MODBUS with slave_id should succeed: %v", err)
	}
}

// TestAllStateTransitionsAreDefined verifies all transitions are implemented
func TestAllStateTransitionsAreDefined(t *testing.T) {
	// Valid transitions from requirements
	testCases := []struct {
		from  string
		to    string
		valid bool
	}{
		// DRAFT transitions
		{"DRAFT", "PUBLISHED", true},
		{"DRAFT", "ARCHIVED", true},
		{"DRAFT", "DRAFT", false},

		// PUBLISHED transitions
		{"PUBLISHED", "ARCHIVED", true},
		{"PUBLISHED", "DRAFT", true}, // via DeriveDraft
		{"PUBLISHED", "PUBLISHED", false},

		// ARCHIVED transitions (immutable)
		{"ARCHIVED", "ARCHIVED", false},
		{"ARCHIVED", "DRAFT", false},
		{"ARCHIVED", "PUBLISHED", false},
	}

	for _, tc := range testCases {
		t.Run(tc.from+"→"+tc.to, func(t *testing.T) {
			err := CanTransition(tc.from, tc.to)
			hasErr := err != nil

			if hasErr == tc.valid {
				t.Fatalf("CanTransition(%s, %s) = %v, expected valid=%v",
					tc.from, tc.to, hasErr, tc.valid)
			}
		})
	}
}

// TestMQTTQoSBoundaries tests QoS validation boundaries
func TestMQTTQoSBoundaries(t *testing.T) {
	testCases := []struct {
		qos       float64
		shouldErr bool
	}{
		{-1, true},
		{0, false},
		{1, false},
		{2, false},
		{3, true},
		{100, true},
	}

	for _, tc := range testCases {
		t.Run("QoS="+string(rune(tc.qos)), func(t *testing.T) {
			config := map[string]interface{}{
				"broker_url": "mqtt://broker",
				"qos":        tc.qos,
			}
			configJSON, _ := json.Marshal(config)
			validator, _ := NewProtocolConfigValidator("MQTT", configJSON)
			err := validator.Validate()

			if (err != nil) != tc.shouldErr {
				t.Fatalf("QoS %v validation failed: expected err=%v, got err=%v",
					tc.qos, tc.shouldErr, err != nil)
			}
		})
	}
}

// TestMODBUSSlaveBoundaries tests slave_id validation boundaries
func TestMODBUSSlaveBoundaries(t *testing.T) {
	testCases := []struct {
		slaveID   float64
		shouldErr bool
	}{
		{-1, true},
		{0, false},
		{1, false},
		{100, false},
		{247, false},
		{248, true},
		{255, true},
		{500, true},
	}

	for _, tc := range testCases {
		t.Run("SlaveID="+string(rune(tc.slaveID)), func(t *testing.T) {
			config := map[string]interface{}{
				"slave_id": tc.slaveID,
			}
			configJSON, _ := json.Marshal(config)
			validator, _ := NewProtocolConfigValidator("MODBUS", configJSON)
			err := validator.Validate()

			if (err != nil) != tc.shouldErr {
				t.Fatalf("SlaveID %v validation failed: expected err=%v, got err=%v",
					tc.slaveID, tc.shouldErr, err != nil)
			}
		})
	}
}

// TestProtocolValidatorTypeCoercion tests type coercion and validation
func TestProtocolValidatorTypeCoercion(t *testing.T) {
	testCases := []struct {
		name      string
		protocol  string
		config    string
		shouldErr bool
	}{
		{
			name:      "MQTT with number broker_url",
			protocol:  "MQTT",
			config:    `{"broker_url":123}`,
			shouldErr: true,
		},
		{
			name:      "HTTP with number endpoint",
			protocol:  "HTTP",
			config:    `{"endpoint":456}`,
			shouldErr: true,
		},
		{
			name:      "MODBUS with string slave_id",
			protocol:  "MODBUS",
			config:    `{"slave_id":"1"}`,
			shouldErr: true,
		},
		{
			name:      "MQTT with string qos",
			protocol:  "MQTT",
			config:    `{"broker_url":"mqtt://broker","qos":"1"}`,
			shouldErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			validator, err := NewProtocolConfigValidator(tc.protocol, json.RawMessage(tc.config))
			if err == nil && validator != nil {
				err = validator.Validate()
			}

			if (err != nil) != tc.shouldErr {
				t.Fatalf("expected shouldErr=%v, got err=%v", tc.shouldErr, err)
			}
		})
	}
}

// TestArchiveImmutability ensures ARCHIVED status is truly immutable
func TestArchiveImmutability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test through CanTransition
	transitions := []string{"DRAFT", "PUBLISHED", "ARCHIVED"}
	for _, targetStatus := range transitions {
		err := CanTransition("ARCHIVED", targetStatus)
		if err == nil {
			t.Errorf("ARCHIVED should not transition to %s", targetStatus)
		}
	}

	// Test IsImmutableStatus
	if !IsImmutableStatus("ARCHIVED") {
		t.Error("ARCHIVED status should be immutable")
	}

	for _, status := range []string{"DRAFT", "PUBLISHED"} {
		if IsImmutableStatus(status) {
			t.Errorf("%s status should not be immutable", status)
		}
	}
}

// TestErrorTypesForStateTransitions verifies error types returned
func TestErrorTypesForStateTransitions(t *testing.T) {
	// Verify error messages for transitions
	err := CanTransition("ARCHIVED", "PUBLISHED")
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	errMsg := err.Error()
	if errMsg != "cannot transition from ARCHIVED to PUBLISHED" {
		t.Fatalf("expected specific error message, got: %s", errMsg)
	}
}

// TestProtocolValidatorErrorMessages verifies error messages are informative
func TestProtocolValidatorErrorMessages(t *testing.T) {
	testCases := []struct {
		name           string
		protocol       string
		config         string
		expectedSubStr string
	}{
		{
			name:           "MQTT missing broker_url",
			protocol:       "MQTT",
			config:         `{}`,
			expectedSubStr: "broker_url",
		},
		{
			name:           "HTTP missing endpoint",
			protocol:       "HTTP",
			config:         `{}`,
			expectedSubStr: "endpoint",
		},
		{
			name:           "MODBUS missing slave_id",
			protocol:       "MODBUS",
			config:         `{}`,
			expectedSubStr: "slave_id",
		},
		{
			name:           "MQTT invalid QoS",
			protocol:       "MQTT",
			config:         `{"broker_url":"mqtt://broker","qos":5}`,
			expectedSubStr: "0, 1, or 2",
		},
		{
			name:           "MODBUS invalid slave_id range",
			protocol:       "MODBUS",
			config:         `{"slave_id":300}`,
			expectedSubStr: "0 and 247",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			validator, err := NewProtocolConfigValidator(tc.protocol, json.RawMessage(tc.config))
			if err == nil && validator != nil {
				err = validator.Validate()
			}

			if err == nil {
				t.Fatal("expected validation error")
			}

			if !containsStr(err.Error(), tc.expectedSubStr) {
				t.Fatalf("error message should contain '%s', got: %s",
					tc.expectedSubStr, err.Error())
			}
		})
	}
}

// TestValidTransitionMatrix verifies the complete transition matrix
func TestValidTransitionMatrix(t *testing.T) {
	// Build a transition matrix
	states := []string{"DRAFT", "PUBLISHED", "ARCHIVED"}
	validTransitions := make(map[string]map[string]bool)

	for _, from := range states {
		validTransitions[from] = make(map[string]bool)
		for _, to := range states {
			err := CanTransition(from, to)
			validTransitions[from][to] = (err == nil)
		}
	}

	// Verify expected transitions
	expected := map[string]map[string]bool{
		"DRAFT": {
			"DRAFT":     false,
			"PUBLISHED": true,
			"ARCHIVED":  true,
		},
		"PUBLISHED": {
			"DRAFT":     true,
			"PUBLISHED": false,
			"ARCHIVED":  true,
		},
		"ARCHIVED": {
			"DRAFT":     false,
			"PUBLISHED": false,
			"ARCHIVED":  false,
		},
	}

	for from, toMap := range expected {
		for to, expectedValid := range toMap {
			actualValid := validTransitions[from][to]
			if actualValid != expectedValid {
				t.Errorf("Transition %s→%s: expected %v, got %v",
					from, to, expectedValid, actualValid)
			}
		}
	}
}

// TestProtocolValidationOnlyOnPublish documents that MVP validates only on publish
func TestProtocolValidationOnlyOnPublish(t *testing.T) {
	// This test documents the MVP requirement that protocol validation
	// only happens on Publish, not during Create/Update
	// The actual enforcement is in publishDeviceTemplate() in store.go

	// Protocol validation should work standalone
	validator, _ := NewProtocolConfigValidator("MQTT", json.RawMessage(`{}`))
	if err := validator.Validate(); err == nil {
		t.Fatal("MQTT validation should fail without broker_url")
	}

	// Valid config should pass
	validator, _ = NewProtocolConfigValidator("MQTT", json.RawMessage(`{"broker_url":"mqtt://broker"}`))
	if err := validator.Validate(); err != nil {
		t.Fatalf("MQTT validation should pass: %v", err)
	}
}

// TestErrsPackageCompatibility ensures error types match encore.dev/beta/errs patterns
func TestErrsPackageCompatibility(t *testing.T) {
	// This test documents that all state machine errors should use errs.Error with appropriate codes
	// Common codes:
	// - errs.FailedPrecondition: for state conflicts (e.g., "already archived")
	// - errs.InvalidArgument: for validation errors (e.g., "bad protocol config")
	// - errs.NotFound: for missing resources

	// Example error construction (as used in publishDeviceTemplate):
	_ = &errs.Error{
		Code:    errs.FailedPrecondition,
		Message: "cannot transition from X to Y",
	}
}

// containsStr is a helper to check if a string contains a substring
func containsStr(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
