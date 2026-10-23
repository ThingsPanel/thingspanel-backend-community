package notification

import (
	"bytes"
	"encoding/json"
	"errors"

	"encore.app/l3-core/notification/contract"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	ErrNotificationSchema = errors.New("invalid_notification_schema")
	ErrNotificationValue  = errors.New("invalid_notification_value")
)

// Notification schemas are self-contained metadata. Remote/file references
// must never turn plugin registration or configuration validation into I/O.
type denySchemaLoader struct{}

func (denySchemaLoader) Load(string) (any, error) { return nil, ErrNotificationSchema }

// CompileNotificationSchema uses one generic validator for every plugin;
// adding a provider must not introduce field-specific branches in the core.
// Compiler errors contain schema literals, so only fixed errors leave here.
func CompileNotificationSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, ErrNotificationSchema
	}
	value, err := notificationDynamicObject(raw)
	if err != nil {
		return nil, ErrNotificationSchema
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.UseLoader(denySchemaLoader{})
	const location = "https://notification.invalid/schema.json"
	if err := compiler.AddResource(location, value); err != nil {
		return nil, ErrNotificationSchema
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return nil, ErrNotificationSchema
	}
	return schema, nil
}

// ValidateNotificationSchema never exposes validator errors: they can contain
// credentials, destinations or content from the rejected value.
func ValidateNotificationSchema(schema *jsonschema.Schema, raw json.RawMessage) error {
	if schema == nil {
		return ErrNotificationSchema
	}
	value, err := notificationDynamicObject(raw)
	if err != nil || schema.Validate(value) != nil {
		return ErrNotificationValue
	}
	return nil
}

func notificationDynamicObject(raw json.RawMessage) (any, error) {
	var envelope struct {
		Value json.RawMessage `json:"value" strictjson:"dynamic-object"`
	}
	wrapped := append([]byte(`{"value":`), raw...)
	wrapped = append(wrapped, '}')
	if err := contract.DecodeStrictJSON(wrapped, &envelope); err != nil {
		return nil, ErrNotificationValue
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(envelope.Value))
}
