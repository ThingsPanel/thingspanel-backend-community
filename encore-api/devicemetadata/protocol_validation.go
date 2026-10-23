package devicemetadata

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ProtocolConfigValidator validates protocol configurations
type ProtocolConfigValidator struct {
	protocolType string
	config       map[string]interface{}
}

// NewProtocolConfigValidator creates a new validator for the given protocol config
func NewProtocolConfigValidator(protocolType string, configJSON json.RawMessage) (*ProtocolConfigValidator, error) {
	var config map[string]interface{}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &config); err != nil {
			return nil, fmt.Errorf("invalid json in protocol_config: %w", err)
		}
	} else {
		config = make(map[string]interface{})
	}

	return &ProtocolConfigValidator{
		protocolType: protocolType,
		config:       config,
	}, nil
}

// Validate validates the protocol configuration according to protocol type
func (v *ProtocolConfigValidator) Validate() error {
	switch v.protocolType {
	case "MQTT":
		return v.validateMQTT()
	case "HTTP":
		return v.validateHTTP()
	case "COAP":
		return v.validateCOAP()
	case "MODBUS":
		return v.validateMODBUS()
	default:
		return nil // Unknown protocols are allowed
	}
}

// validateMQTT validates MQTT protocol config
// Required: broker_url
// Optional: qos (0-2), client_id_template
func (v *ProtocolConfigValidator) validateMQTT() error {
	if _, ok := v.config["broker_url"]; !ok {
		return errors.New("MQTT protocol requires 'broker_url' field")
	}

	brokerURL, ok := v.config["broker_url"].(string)
	if !ok || brokerURL == "" {
		return errors.New("MQTT 'broker_url' must be a non-empty string")
	}

	// Validate QoS if present
	if qos, ok := v.config["qos"]; ok {
		qosVal, ok := qos.(float64)
		if !ok {
			return errors.New("MQTT 'qos' must be a number")
		}
		qosInt := int(qosVal)
		if qosInt < 0 || qosInt > 2 {
			return errors.New("MQTT 'qos' must be 0, 1, or 2")
		}
	}

	// Validate client_id_template if present
	if clientID, ok := v.config["client_id_template"]; ok {
		if _, ok := clientID.(string); !ok {
			return errors.New("MQTT 'client_id_template' must be a string")
		}
	}

	return nil
}

// validateHTTP validates HTTP protocol config
// Required: endpoint
func (v *ProtocolConfigValidator) validateHTTP() error {
	if _, ok := v.config["endpoint"]; !ok {
		return errors.New("HTTP protocol requires 'endpoint' field")
	}

	endpoint, ok := v.config["endpoint"].(string)
	if !ok || endpoint == "" {
		return errors.New("HTTP 'endpoint' must be a non-empty string")
	}

	return nil
}

// validateCOAP validates CoAP protocol config
// For MVP, CoAP is TBD - no validation required
func (v *ProtocolConfigValidator) validateCOAP() error {
	// TBD - to be implemented in future
	return nil
}

// validateMODBUS validates Modbus protocol config
// Required: slave_id
func (v *ProtocolConfigValidator) validateMODBUS() error {
	if _, ok := v.config["slave_id"]; !ok {
		return errors.New("MODBUS protocol requires 'slave_id' field")
	}

	slaveID, ok := v.config["slave_id"].(float64)
	if !ok {
		return errors.New("MODBUS 'slave_id' must be a number")
	}

	slaveIDInt := int(slaveID)
	if slaveIDInt < 0 || slaveIDInt > 247 {
		return errors.New("MODBUS 'slave_id' must be between 0 and 247")
	}

	return nil
}

// StateTransition represents allowed state transitions
type StateTransition struct {
	From string
	To   string
}

// IsValidTransition checks if a state transition is allowed
func IsValidTransition(from, to string) bool {
	// Define all valid transitions
	validTransitions := []StateTransition{
		{"DRAFT", "PUBLISHED"},
		{"DRAFT", "ARCHIVED"},
		{"PUBLISHED", "ARCHIVED"},
		{"PUBLISHED", "DRAFT"}, // via DeriveDraft (creates new template)
	}

	for _, t := range validTransitions {
		if t.From == from && t.To == to {
			return true
		}
	}
	return false
}

// CanTransition checks if a transition is allowed and returns an error if not
func CanTransition(from, to string) error {
	if !IsValidTransition(from, to) {
		return fmt.Errorf("cannot transition from %s to %s", from, to)
	}
	return nil
}

// IsImmutableStatus checks if a status prevents modifications
func IsImmutableStatus(status string) bool {
	return status == "ARCHIVED"
}
