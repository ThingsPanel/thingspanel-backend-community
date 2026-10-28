package devicemetadata

import (
	"encoding/json"
	"testing"
)

// TestProtocolMQTTValidation tests MQTT protocol configuration validation
func TestProtocolMQTTValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		shouldErr bool
		errMsg    string
	}{
		{
			name:      "valid MQTT config with broker_url only",
			config:    map[string]interface{}{"broker_url": "mqtt://broker.example.com"},
			shouldErr: false,
		},
		{
			name: "valid MQTT config with all fields",
			config: map[string]interface{}{
				"broker_url":         "mqtt://broker.example.com",
				"qos":                float64(1),
				"client_id_template": "device_{id}",
			},
			shouldErr: false,
		},
		{
			name:      "missing broker_url",
			config:    map[string]interface{}{},
			shouldErr: true,
			errMsg:    "broker_url",
		},
		{
			name:      "empty broker_url",
			config:    map[string]interface{}{"broker_url": ""},
			shouldErr: true,
			errMsg:    "non-empty",
		},
		{
			name:      "invalid QoS (negative)",
			config:    map[string]interface{}{"broker_url": "mqtt://broker", "qos": float64(-1)},
			shouldErr: true,
			errMsg:    "0, 1, or 2",
		},
		{
			name:      "invalid QoS (> 2)",
			config:    map[string]interface{}{"broker_url": "mqtt://broker", "qos": float64(3)},
			shouldErr: true,
			errMsg:    "0, 1, or 2",
		},
		{
			name:      "valid QoS values",
			config:    map[string]interface{}{"broker_url": "mqtt://broker", "qos": float64(0)},
			shouldErr: false,
		},
		{
			name:      "invalid client_id_template type",
			config:    map[string]interface{}{"broker_url": "mqtt://broker", "client_id_template": 123},
			shouldErr: true,
			errMsg:    "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configJSON, _ := json.Marshal(tt.config)
			validator, err := NewProtocolConfigValidator("MQTT", configJSON)
			if err != nil && !tt.shouldErr {
				t.Fatalf("unexpected error creating validator: %v", err)
			}

			if validator != nil {
				err := validator.Validate()
				if (err != nil) != tt.shouldErr {
					t.Fatalf("expected error: %v, got: %v", tt.shouldErr, err)
				}
				if err != nil && tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}

// TestProtocolHTTPValidation tests HTTP protocol configuration validation
func TestProtocolHTTPValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		shouldErr bool
	}{
		{
			name:      "valid HTTP config",
			config:    map[string]interface{}{"endpoint": "https://api.example.com/v1"},
			shouldErr: false,
		},
		{
			name:      "missing endpoint",
			config:    map[string]interface{}{},
			shouldErr: true,
		},
		{
			name:      "empty endpoint",
			config:    map[string]interface{}{"endpoint": ""},
			shouldErr: true,
		},
		{
			name:      "invalid endpoint type",
			config:    map[string]interface{}{"endpoint": 123},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configJSON, _ := json.Marshal(tt.config)
			validator, err := NewProtocolConfigValidator("HTTP", configJSON)
			if err != nil && !tt.shouldErr {
				t.Fatalf("unexpected error creating validator: %v", err)
			}

			if validator != nil {
				err := validator.Validate()
				if (err != nil) != tt.shouldErr {
					t.Fatalf("expected error: %v, got: %v", tt.shouldErr, err)
				}
			}
		})
	}
}

// TestProtocolMODBUSValidation tests Modbus protocol configuration validation
func TestProtocolMODBUSValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		shouldErr bool
	}{
		{
			name:      "valid Modbus config",
			config:    map[string]interface{}{"slave_id": float64(1)},
			shouldErr: false,
		},
		{
			name:      "valid Modbus config with max slave_id",
			config:    map[string]interface{}{"slave_id": float64(247)},
			shouldErr: false,
		},
		{
			name:      "valid Modbus config with min slave_id",
			config:    map[string]interface{}{"slave_id": float64(0)},
			shouldErr: false,
		},
		{
			name:      "missing slave_id",
			config:    map[string]interface{}{},
			shouldErr: true,
		},
		{
			name:      "slave_id out of range (negative)",
			config:    map[string]interface{}{"slave_id": float64(-1)},
			shouldErr: true,
		},
		{
			name:      "slave_id out of range (> 247)",
			config:    map[string]interface{}{"slave_id": float64(248)},
			shouldErr: true,
		},
		{
			name:      "invalid slave_id type",
			config:    map[string]interface{}{"slave_id": "1"},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configJSON, _ := json.Marshal(tt.config)
			validator, err := NewProtocolConfigValidator("MODBUS", configJSON)
			if err != nil && !tt.shouldErr {
				t.Fatalf("unexpected error creating validator: %v", err)
			}

			if validator != nil {
				err := validator.Validate()
				if (err != nil) != tt.shouldErr {
					t.Fatalf("expected error: %v, got: %v", tt.shouldErr, err)
				}
			}
		})
	}
}

// TestProtocolCOAPValidation tests CoAP protocol configuration
func TestProtocolCOAPValidation(t *testing.T) {
	// CoAP is TBD, should accept any config for now
	validator, err := NewProtocolConfigValidator("COAP", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validator.Validate(); err != nil {
		t.Fatalf("CoAP validation should succeed, got: %v", err)
	}
}

// TestUnknownProtocol tests unknown protocol types are allowed
func TestUnknownProtocol(t *testing.T) {
	validator, err := NewProtocolConfigValidator("CUSTOM", json.RawMessage(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validator.Validate(); err != nil {
		t.Fatalf("unknown protocol should be allowed, got: %v", err)
	}
}

// TestStateTransitions tests all valid state transitions
func TestStateTransitions(t *testing.T) {
	tests := []struct {
		from        string
		to          string
		shouldBeOK  bool
		description string
	}{
		// Valid transitions
		{"DRAFT", "PUBLISHED", true, "DRAFT → PUBLISHED (Publish)"},
		{"DRAFT", "ARCHIVED", true, "DRAFT → ARCHIVED (Archive)"},
		{"PUBLISHED", "ARCHIVED", true, "PUBLISHED → ARCHIVED (Archive)"},
		{"PUBLISHED", "DRAFT", true, "PUBLISHED → DRAFT (DeriveDraft creates new)"},

		// Invalid transitions
		{"PUBLISHED", "PUBLISHED", false, "PUBLISHED → PUBLISHED (no-op)"},
		{"DRAFT", "DRAFT", false, "DRAFT → DRAFT (no-op)"},
		{"ARCHIVED", "ARCHIVED", false, "ARCHIVED → ARCHIVED (immutable)"},
		{"ARCHIVED", "PUBLISHED", false, "ARCHIVED → PUBLISHED (cannot unarchive)"},
		{"ARCHIVED", "DRAFT", false, "ARCHIVED → DRAFT (cannot unarchive)"},
		{"DRAFT", "ARCHIVED", true, "DRAFT → ARCHIVED (valid)"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			err := CanTransition(tt.from, tt.to)
			if (err == nil) != tt.shouldBeOK {
				t.Fatalf("expected error: %v, got: %v", !tt.shouldBeOK, err)
			}
		})
	}
}

// TestImmutableStatus tests archived status prevents modifications
func TestImmutableStatus(t *testing.T) {
	tests := []struct {
		status      string
		isImmutable bool
	}{
		{"ARCHIVED", true},
		{"DRAFT", false},
		{"PUBLISHED", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := IsImmutableStatus(tt.status)
			if result != tt.isImmutable {
				t.Fatalf("expected IsImmutableStatus(%s) = %v, got %v", tt.status, tt.isImmutable, result)
			}
		})
	}
}

// TestProtocolConfigValidatorEmptyConfig tests validator with empty config
func TestProtocolConfigValidatorEmptyConfig(t *testing.T) {
	// Empty config should fail for MQTT
	validator, err := NewProtocolConfigValidator("MQTT", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error creating validator: %v", err)
	}
	if err := validator.Validate(); err == nil {
		t.Fatal("MQTT validation should fail with empty config")
	}

	// Empty config for unknown protocol should be OK
	validator, err = NewProtocolConfigValidator("CUSTOM", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error creating validator: %v", err)
	}
	if err := validator.Validate(); err != nil {
		t.Fatalf("unknown protocol validation should succeed, got: %v", err)
	}
}

// TestProtocolConfigValidatorNullConfig tests validator with null/empty JSON
func TestProtocolConfigValidatorNullConfig(t *testing.T) {
	// Null JSON should create empty config
	validator, err := NewProtocolConfigValidator("MQTT", []byte(``))
	if err != nil {
		t.Fatalf("unexpected error creating validator: %v", err)
	}
	if err := validator.Validate(); err == nil {
		t.Fatal("MQTT validation should fail with null config")
	}
}

// TestMQTTQoSValidation tests all valid QoS values
func TestMQTTQoSValidation(t *testing.T) {
	validQoS := []float64{0, 1, 2}
	for _, qos := range validQoS {
		config := map[string]interface{}{
			"broker_url": "mqtt://broker",
			"qos":        qos,
		}
		configJSON, _ := json.Marshal(config)
		validator, _ := NewProtocolConfigValidator("MQTT", configJSON)
		if err := validator.Validate(); err != nil {
			t.Fatalf("QoS %v should be valid, got: %v", qos, err)
		}
	}
}

// TestInvalidJSONInProtocolConfig tests handling of invalid JSON
func TestInvalidJSONInProtocolConfig(t *testing.T) {
	_, err := NewProtocolConfigValidator("MQTT", json.RawMessage(`{invalid json}`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// TestValidTransitionWithCanTransition uses CanTransition for all valid cases
func TestValidTransitionWithCanTransition(t *testing.T) {
	validPairs := []struct {
		from string
		to   string
	}{
		{"DRAFT", "PUBLISHED"},
		{"DRAFT", "ARCHIVED"},
		{"PUBLISHED", "ARCHIVED"},
		{"PUBLISHED", "DRAFT"},
	}

	for _, pair := range validPairs {
		t.Run(pair.from+"→"+pair.to, func(t *testing.T) {
			if err := CanTransition(pair.from, pair.to); err != nil {
				t.Fatalf("valid transition should not error, got: %v", err)
			}
		})
	}
}

// TestInvalidTransitionWithCanTransition uses CanTransition for invalid cases
func TestInvalidTransitionWithCanTransition(t *testing.T) {
	invalidPairs := []struct {
		from string
		to   string
	}{
		{"ARCHIVED", "DRAFT"},
		{"ARCHIVED", "PUBLISHED"},
		{"DRAFT", "DRAFT"},
		{"PUBLISHED", "PUBLISHED"},
	}

	for _, pair := range invalidPairs {
		t.Run(pair.from+"→"+pair.to, func(t *testing.T) {
			if err := CanTransition(pair.from, pair.to); err == nil {
				t.Fatal("invalid transition should error")
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
