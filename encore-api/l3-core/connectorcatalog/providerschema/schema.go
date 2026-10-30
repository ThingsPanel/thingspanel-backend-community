// Package providerschema compiles and validates the public parameter schemas
// attached to an immutable device-provider release. It intentionally has no
// dependency on the catalog service or Encore service initialization.
package providerschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Domain identifies which independent provider schema failed.
type Domain string

const (
	ConfigurationDomain    Domain = "configuration"
	DeviceParametersDomain Domain = "device_parameters"
	Draft202012SchemaURI          = "https://json-schema.org/draft/2020-12/schema"
)

// ErrorKind distinguishes invalid schema definitions from invalid instances.
type ErrorKind string

const (
	SchemaError   ErrorKind = "schema"
	InstanceError ErrorKind = "instance"
)

// Error contains only stable, non-sensitive classification. It never carries
// validator details, schema fragments, or instance data.
type Error struct {
	Domain Domain
	Kind   ErrorKind
}

func (e *Error) Error() string {
	if e == nil {
		return "invalid provider schema"
	}
	if e.Kind == SchemaError {
		if e.Domain == DeviceParametersDomain {
			return "invalid device-parameter schema"
		}
		return "invalid configuration schema"
	}
	if e.Domain == DeviceParametersDomain {
		return "invalid device parameters"
	}
	return "invalid configuration parameters"
}

// CompiledPair holds two independently compiled schemas. Its compiled schemas
// are private and immutable to callers; validation never applies defaults or
// mutates the input JSON.
type CompiledPair struct {
	configuration    *jsonschema.Schema
	deviceParameters *jsonschema.Schema
}

// CompilePair compiles both required JSON Schema documents as one all-or-error
// pair. Omitted $schema defaults to Draft 2020-12. Only document-local
// references are accepted; all external resource loading is denied.
func CompilePair(configuration, deviceParameter json.RawMessage) (*CompiledPair, error) {
	config, err := compileOne(configuration, ConfigurationDomain)
	if err != nil {
		return nil, err
	}
	device, err := compileOne(deviceParameter, DeviceParametersDomain)
	if err != nil {
		return nil, err
	}
	return &CompiledPair{configuration: config, deviceParameters: device}, nil
}

// ValidateConfiguration validates one raw JSON instance without converting
// numbers through float64 or otherwise changing its representation.
func (p *CompiledPair) ValidateConfiguration(rawJSON json.RawMessage) error {
	if p == nil || p.configuration == nil {
		return &Error{Domain: ConfigurationDomain, Kind: SchemaError}
	}
	return validate(p.configuration, rawJSON, ConfigurationDomain)
}

// ValidateDeviceParameters validates one raw per-device JSON instance. This
// schema is separate from the access-point configuration schema.
func (p *CompiledPair) ValidateDeviceParameters(rawJSON json.RawMessage) error {
	if p == nil || p.deviceParameters == nil {
		return &Error{Domain: DeviceParametersDomain, Kind: SchemaError}
	}
	return validate(p.deviceParameters, rawJSON, DeviceParametersDomain)
}

func compileOne(raw json.RawMessage, domain Domain) (*jsonschema.Schema, error) {
	fail := func() (*jsonschema.Schema, error) {
		return nil, &Error{Domain: domain, Kind: SchemaError}
	}
	if len(raw) == 0 {
		return fail()
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fail()
	}
	root, ok := value.(map[string]any)
	if !ok {
		return fail()
	}
	if !supportedDraft(root) || !localSchemaReferencesAndDraftsOnly(root) {
		return fail()
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyLoader{})
	location := "https://providerschema.invalid/" + string(domain) + ".json"
	if err := compiler.AddResource(location, root); err != nil {
		return fail()
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return fail()
	}
	return schema, nil
}

func supportedDraft(root map[string]any) bool {
	draft, exists := root["$schema"]
	if !exists {
		return true
	}
	uri, ok := draft.(string)
	return ok && supportedDraftURI(uri)
}

func supportedDraftURI(uri string) bool {
	// jsonschema/v6 recognizes the HTTP alias as the same official 2020-12
	// meta-schema. No other dialect or external custom meta-schema is accepted.
	return uri == Draft202012SchemaURI || uri == "http://json-schema.org/draft/2020-12/schema"
}

func localSchemaReferencesAndDraftsOnly(schema any) bool {
	object, ok := schema.(map[string]any)
	if !ok {
		// Boolean subschemas are valid and cannot contain resource references.
		_, booleanSchema := schema.(bool)
		return booleanSchema
	}
	for _, key := range []string{"$schema"} {
		if draft, exists := object[key]; exists {
			uri, ok := draft.(string)
			if !ok || !supportedDraftURI(uri) {
				return false
			}
		}
	}
	for _, key := range []string{"$ref", "$dynamicRef", "$recursiveRef"} {
		if rawRef, exists := object[key]; exists {
			ref, ok := rawRef.(string)
			if !ok || !strings.HasPrefix(ref, "#") {
				return false
			}
		}
	}

	for _, keyword := range []string{"$defs", "definitions", "properties", "patternProperties", "dependentSchemas"} {
		if children, ok := object[keyword].(map[string]any); ok {
			for _, child := range children {
				if !localSchemaReferencesAndDraftsOnly(child) {
					return false
				}
			}
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		if children, ok := object[keyword].([]any); ok {
			for _, child := range children {
				if !localSchemaReferencesAndDraftsOnly(child) {
					return false
				}
			}
		}
	}
	for _, keyword := range []string{
		"additionalProperties", "additionalItems", "unevaluatedProperties", "unevaluatedItems",
		"propertyNames", "contains", "contentSchema", "not", "if", "then", "else", "items",
	} {
		if child, exists := object[keyword]; exists {
			if schemas, ok := child.([]any); ok && keyword == "items" {
				for _, itemSchema := range schemas {
					if !localSchemaReferencesAndDraftsOnly(itemSchema) {
						return false
					}
				}
			} else if !localSchemaReferencesAndDraftsOnly(child) {
				return false
			}
		}
	}
	if dependencies, ok := object["dependencies"].(map[string]any); ok {
		for _, dependency := range dependencies {
			if _, isSchema := dependency.(map[string]any); isSchema && !localSchemaReferencesAndDraftsOnly(dependency) {
				return false
			}
		}
	}
	return true
}

type denyLoader struct{}

func (denyLoader) Load(string) (any, error) {
	return nil, errors.New("external provider schema references are disabled")
}

func validate(schema *jsonschema.Schema, rawJSON json.RawMessage, domain Domain) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawJSON))
	if err != nil || schema.Validate(value) != nil {
		return &Error{Domain: domain, Kind: InstanceError}
	}
	return nil
}
