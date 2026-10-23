package notification

import (
	"bytes"
	"embed"
	"errors"
	"sync"

	"encore.app/l3-core/notification/contract"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

//go:embed wire_schema/notification-v2.yaml wire_schema/source-event-v1.schema.json wire_schema/plugin-v1.openapi.yaml
var notificationWireSchemas embed.FS

var wireSchemaOnce sync.Once
var compiledWireSchemas map[string]*jsonschema.Schema
var wireSchemaError error
var ErrNotificationBody = errors.New("invalid_notification_body")

func compileNotificationWireSchemas() {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.UseLoader(denySchemaLoader{})
	const businessLocation = "https://notification.invalid/specs/openapi/notification-v2.yaml"
	const sourceLocation = "https://notification.invalid/specs/notification/source-event-v1.schema.json"
	businessRaw, err := notificationWireSchemas.ReadFile("wire_schema/notification-v2.yaml")
	if err != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	var business map[string]any
	if yaml.Unmarshal(businessRaw, &business) != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	sourceRaw, err := notificationWireSchemas.ReadFile("wire_schema/source-event-v1.schema.json")
	if err != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	source, err := jsonschema.UnmarshalJSON(bytes.NewReader(sourceRaw))
	if err != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	if compiler.AddResource(businessLocation, business) != nil || compiler.AddResource(sourceLocation, source) != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	components, ok := business["components"].(map[string]any)
	if !ok {
		wireSchemaError = ErrNotificationSchema
		return
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		wireSchemaError = ErrNotificationSchema
		return
	}
	compiledWireSchemas = map[string]*jsonschema.Schema{}
	for name := range schemas {
		schema, err := compiler.Compile(businessLocation + "#/components/schemas/" + name)
		if err != nil {
			wireSchemaError = ErrNotificationSchema
			return
		}
		compiledWireSchemas[name] = schema
	}
	schema, err := compiler.Compile(sourceLocation)
	if err != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	compiledWireSchemas["SourceEventV1"] = schema
	pluginRaw, err := notificationWireSchemas.ReadFile("wire_schema/plugin-v1.openapi.yaml")
	if err != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	var plugin map[string]any
	if yaml.Unmarshal(pluginRaw, &plugin) != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	const pluginLocation = "https://notification.invalid/specs/notification/plugin-v1.openapi.yaml"
	if compiler.AddResource(pluginLocation, plugin) != nil {
		wireSchemaError = ErrNotificationSchema
		return
	}
	pluginComponents, ok := plugin["components"].(map[string]any)
	if !ok {
		wireSchemaError = ErrNotificationSchema
		return
	}
	pluginSchemas, ok := pluginComponents["schemas"].(map[string]any)
	if !ok {
		wireSchemaError = ErrNotificationSchema
		return
	}
	for name := range pluginSchemas {
		schema, err := compiler.Compile(pluginLocation + "#/components/schemas/" + name)
		if err != nil {
			wireSchemaError = ErrNotificationSchema
			return
		}
		compiledWireSchemas["Plugin"+name] = schema
	}
}

// DecodeNotificationBody enforces the actual frozen schema on raw bytes,
// before omitted required booleans/strings disappear into Go zero values.
// Call this from every raw Encore notification HTTP write boundary.
func DecodeNotificationBody(raw []byte, schemaName string, dst any) error {
	if contract.DecodeStrictJSON(raw, dst) != nil {
		return ErrNotificationBody
	}
	wireSchemaOnce.Do(compileNotificationWireSchemas)
	if wireSchemaError != nil {
		return wireSchemaError
	}
	schema := compiledWireSchemas[schemaName]
	if schema == nil {
		return ErrNotificationSchema
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil || schema.Validate(value) != nil {
		return ErrNotificationBody
	}
	return nil
}
