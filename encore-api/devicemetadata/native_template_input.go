package devicemetadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"

	"encore.app/devicemetadata/accessconfig"
)

var nativeCustomFieldKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// NativeTemplateCreateInput is a locally validated create request. ConfigurationJSON
// retains exact numeric values; Configuration is the validated, convenient value form.
type NativeTemplateCreateInput struct {
	Name              string
	Description       NativeDeviceField[string]
	Brand             NativeDeviceField[string]
	ProductName       NativeDeviceField[string]
	ModelNumber       NativeDeviceField[string]
	Tags              []string
	Configuration     accessconfig.Configuration
	ConfigurationJSON json.RawMessage
	ImageAssetID      NativeDeviceField[string]
	CanonicalBody     []byte
}

// NativeTemplatePatchInput preserves field presence for explicit partial
// replacement. A supplied Configuration is a complete replacement, never a merge.
type NativeTemplatePatchInput struct {
	Name                NativeDeviceField[string]
	Description         NativeDeviceField[string]
	Brand               NativeDeviceField[string]
	ProductName         NativeDeviceField[string]
	ModelNumber         NativeDeviceField[string]
	Tags                NativeDeviceField[[]string]
	Configuration       NativeDeviceField[accessconfig.Configuration]
	ConfigurationJSON   NativeDeviceField[json.RawMessage]
	CustomFieldChanges  NativeDeviceField[NativeCustomFieldChanges]
	AdministrativeState NativeDeviceField[string]
	ImageAssetID        NativeDeviceField[string]
	CanonicalBody       []byte
}

// NativeCustomFieldChanges is a field-level edit against the current template
// definitions. Raw values preserve JSON number precision and explicit nulls.
type NativeCustomFieldChanges struct {
	Upsert []NativeCustomFieldUpsert
	Remove []string
}

type NativeCustomFieldUpsert struct {
	Key    string
	Fields map[string]json.RawMessage
}

// ParseNativeTemplateCreate validates only the local request contract. It does
// not resolve catalog ownership or provider/model capabilities.
func ParseNativeTemplateCreate(data []byte) (*NativeTemplateCreateInput, error) {
	fields, err := strictJSONObject(data)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknown(fields, setOf("name", "description", "brand", "productName", "modelNumber", "tags", "configuration", "imageAssetId")); err != nil {
		return nil, err
	}
	out := &NativeTemplateCreateInput{Tags: []string{}}
	if err := requiredString(fields, "name", 1, 128, &out.Name); err != nil {
		return nil, err
	}
	if raw, ok := fields["configuration"]; !ok {
		return nil, invalid("configuration", "required")
	} else if err := decodeTemplateConfiguration(raw, &out.Configuration, &out.ConfigurationJSON); err != nil {
		return nil, err
	}
	if err := decodeTemplateOptionalString(fields, "description", 0, 2000, &out.Description); err != nil {
		return nil, err
	}
	if err := decodeTemplateOptionalString(fields, "brand", 0, 64, &out.Brand); err != nil {
		return nil, err
	}
	if err := decodeTemplateOptionalString(fields, "productName", 0, 128, &out.ProductName); err != nil {
		return nil, err
	}
	if err := decodeTemplateOptionalString(fields, "modelNumber", 0, 64, &out.ModelNumber); err != nil {
		return nil, err
	}
	if raw, ok := fields["tags"]; ok {
		if err := decodeStringArray(raw, "tags", 32, 64, false, &out.Tags); err != nil {
			return nil, err
		}
	}
	if raw, ok := fields["imageAssetId"]; ok {
		if err := decodeTemplateImageAssetID(raw, &out.ImageAssetID); err != nil {
			return nil, err
		}
	}
	body := map[string]any{"name": out.Name, "description": nullableFieldValue(out.Description), "brand": nullableFieldValue(out.Brand), "productName": nullableFieldValue(out.ProductName), "modelNumber": nullableFieldValue(out.ModelNumber), "tags": out.Tags, "configuration": out.ConfigurationJSON, "imageAssetId": nullableFieldValue(out.ImageAssetID)}
	out.CanonicalBody, err = json.Marshal(body)
	if err != nil {
		return nil, invalidStructure("could not canonicalize request")
	}
	return out, nil
}

// ParseNativeTemplatePatch validates a strict partial replacement request.
// Empty objects are accepted as contract-valid no-ops.
func ParseNativeTemplatePatch(data []byte) (*NativeTemplatePatchInput, error) {
	fields, err := strictJSONObject(data)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknown(fields, setOf("name", "description", "brand", "productName", "modelNumber", "tags", "configuration", "customFieldChanges", "administrativeState", "imageAssetId")); err != nil {
		return nil, err
	}
	out := &NativeTemplatePatchInput{}
	if raw, ok := fields["name"]; ok {
		if err := decodeFieldString(raw, "name", 1, 128, false, &out.Name); err != nil {
			return nil, err
		}
	}
	for _, spec := range []struct {
		key string
		max int
		dst *NativeDeviceField[string]
	}{{"description", 2000, &out.Description}, {"brand", 64, &out.Brand}, {"productName", 128, &out.ProductName}, {"modelNumber", 64, &out.ModelNumber}} {
		if raw, ok := fields[spec.key]; ok {
			if err := decodeTemplateOptionalStringValue(raw, spec.key, 0, spec.max, spec.dst); err != nil {
				return nil, err
			}
		}
	}
	if raw, ok := fields["tags"]; ok {
		var tags []string
		if err := decodeStringArray(raw, "tags", 32, 64, false, &tags); err != nil {
			return nil, err
		}
		out.Tags = NativeDeviceField[[]string]{Present: true, Value: tags}
	}
	if raw, ok := fields["configuration"]; ok {
		if _, present := fields["customFieldChanges"]; present {
			return nil, invalid("customFieldChanges", "cannot be combined with configuration")
		}
		var d accessconfig.Configuration
		var rawConfiguration json.RawMessage
		if err := decodeTemplateConfiguration(raw, &d, &rawConfiguration); err != nil {
			return nil, err
		}
		out.Configuration = NativeDeviceField[accessconfig.Configuration]{Present: true, Value: d}
		out.ConfigurationJSON = NativeDeviceField[json.RawMessage]{Present: true, Value: rawConfiguration}
	}
	if raw, ok := fields["customFieldChanges"]; ok {
		changes, err := decodeNativeCustomFieldChanges(raw)
		if err != nil {
			return nil, err
		}
		out.CustomFieldChanges = NativeDeviceField[NativeCustomFieldChanges]{Present: true, Value: changes}
	}
	if raw, ok := fields["administrativeState"]; ok {
		var state string
		if err := decodeString(raw, "administrativeState", &state); err != nil {
			return nil, err
		}
		if state != "enabled" && state != "disabled" {
			return nil, invalid("administrativeState", "value is not in the allowed enum")
		}
		out.AdministrativeState = NativeDeviceField[string]{Present: true, Value: state}
	}
	if raw, ok := fields["imageAssetId"]; ok {
		if err := decodeTemplateImageAssetID(raw, &out.ImageAssetID); err != nil {
			return nil, err
		}
	}
	body := make(map[string]any, len(fields))
	for key, field := range map[string]NativeDeviceField[string]{"name": out.Name, "description": out.Description, "brand": out.Brand, "productName": out.ProductName, "modelNumber": out.ModelNumber, "administrativeState": out.AdministrativeState, "imageAssetId": out.ImageAssetID} {
		if field.Present {
			body[key] = nullableFieldValue(field)
		}
	}
	if out.Tags.Present {
		body["tags"] = out.Tags.Value
	}
	if out.ConfigurationJSON.Present {
		body["configuration"] = out.ConfigurationJSON.Value
	}
	if out.CustomFieldChanges.Present {
		canonicalChanges, err := marshalNativeCustomFieldChanges(out.CustomFieldChanges.Value)
		if err != nil {
			return nil, invalidStructure("could not canonicalize custom field changes")
		}
		body["customFieldChanges"] = canonicalChanges
	}
	out.CanonicalBody, err = json.Marshal(body)
	if err != nil {
		return nil, invalidStructure("could not canonicalize request")
	}
	return out, nil
}

func decodeTemplateOptionalString(fields map[string]json.RawMessage, key string, min, max int, dst *NativeDeviceField[string]) error {
	raw, ok := fields[key]
	if !ok {
		*dst = NativeDeviceField[string]{Present: true, Null: true}
		return nil
	}
	return decodeTemplateOptionalStringValue(raw, key, min, max, dst)
}

func decodeNativeCustomFieldChanges(raw []byte) (NativeCustomFieldChanges, error) {
	fields, err := strictJSONObject(raw)
	if err != nil {
		return NativeCustomFieldChanges{}, err
	}
	if err := rejectUnknown(fields, setOf("upsert", "remove")); err != nil {
		return NativeCustomFieldChanges{}, err
	}
	upsertRaw, hasUpsert := fields["upsert"]
	removeRaw, hasRemove := fields["remove"]
	if !hasUpsert || !hasRemove {
		return NativeCustomFieldChanges{}, invalid("customFieldChanges", "upsert and remove are required")
	}
	var upserts []json.RawMessage
	if err := json.Unmarshal(upsertRaw, &upserts); err != nil || upserts == nil {
		return NativeCustomFieldChanges{}, invalid("customFieldChanges.upsert", "must be an array")
	}
	var removes []string
	if err := json.Unmarshal(removeRaw, &removes); err != nil || removes == nil {
		return NativeCustomFieldChanges{}, invalid("customFieldChanges.remove", "must be an array")
	}
	if len(upserts)+len(removes) < 1 || len(upserts)+len(removes) > 100 {
		return NativeCustomFieldChanges{}, invalid("customFieldChanges", "must contain 1 to 100 operations")
	}
	result := NativeCustomFieldChanges{Upsert: make([]NativeCustomFieldUpsert, 0, len(upserts)), Remove: removes}
	keys := make(map[string]bool, len(upserts)+len(removes))
	for i, item := range upserts {
		object, err := strictJSONObject(item)
		if err != nil {
			return NativeCustomFieldChanges{}, invalid(fmt.Sprintf("customFieldChanges.upsert[%d]", i), "must be an object")
		}
		if err := rejectUnknown(object, setOf("key", "name", "type", "defaultValue", "description", "enabled", "enumValues")); err != nil {
			return NativeCustomFieldChanges{}, err
		}
		var key string
		if err := requiredString(object, "key", 1, 64, &key); err != nil || !nativeCustomFieldKeyPattern.MatchString(key) {
			return NativeCustomFieldChanges{}, invalid(fmt.Sprintf("customFieldChanges.upsert[%d].key", i), "is invalid")
		}
		if keys[key] {
			return NativeCustomFieldChanges{}, invalid("customFieldChanges", "keys must be unique and upsert/remove cannot overlap")
		}
		keys[key] = true
		patchFields := make(map[string]json.RawMessage, len(object)-1)
		for name, value := range object {
			if name == "key" {
				continue
			}
			if err := validateNativeCustomFieldPatchValue(name, value); err != nil {
				return NativeCustomFieldChanges{}, err
			}
			patchFields[name] = append(json.RawMessage(nil), value...)
		}
		result.Upsert = append(result.Upsert, NativeCustomFieldUpsert{Key: key, Fields: patchFields})
	}
	for i, key := range removes {
		if !nativeCustomFieldKeyPattern.MatchString(key) || len(key) > 64 {
			return NativeCustomFieldChanges{}, invalid(fmt.Sprintf("customFieldChanges.remove[%d]", i), "is invalid")
		}
		if keys[key] {
			return NativeCustomFieldChanges{}, invalid("customFieldChanges", "keys must be unique and upsert/remove cannot overlap")
		}
		keys[key] = true
	}
	return result, nil
}

func validateNativeCustomFieldPatchValue(name string, raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if name == "defaultValue" || name == "description" {
			return nil
		}
		return invalid("customFieldChanges.upsert."+name, "null is not allowed")
	}
	switch name {
	case "name", "type":
		var value string
		if json.Unmarshal(raw, &value) != nil || value == "" {
			return invalid("customFieldChanges.upsert."+name, "must be a non-empty string")
		}
		if name == "type" && value != "string" && value != "number" && value != "boolean" && value != "enum" {
			return invalid("customFieldChanges.upsert.type", "value is not in the allowed enum")
		}
	case "description":
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return invalid("customFieldChanges.upsert.description", "must be a string or null")
		}
	case "enabled":
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return invalid("customFieldChanges.upsert.enabled", "must be a boolean")
		}
	case "enumValues":
		var value []accessconfig.EnumValue
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return invalid("customFieldChanges.upsert.enumValues", "must be an array")
		}
	case "defaultValue":
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return invalid("customFieldChanges.upsert.defaultValue", "must be a scalar or null")
		}
		switch value.(type) {
		case string, json.Number, bool:
		default:
			return invalid("customFieldChanges.upsert.defaultValue", "must be a scalar or null")
		}
	default:
		return invalid("customFieldChanges.upsert", "unknown field")
	}
	return nil
}

func decodeTemplateOptionalStringValue(raw []byte, key string, min, max int, dst *NativeDeviceField[string]) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*dst = NativeDeviceField[string]{Present: true, Null: true}
		return nil
	}
	var value string
	if err := decodeString(raw, key, &value); err != nil {
		return err
	}
	if err := checkText(key, value, min, max); err != nil {
		return err
	}
	*dst = NativeDeviceField[string]{Present: true, Value: value}
	return nil
}

func decodeTemplateImageAssetID(raw []byte, dst *NativeDeviceField[string]) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*dst = NativeDeviceField[string]{Present: true, Null: true}
		return nil
	}
	var value string
	if err := decodeString(raw, "imageAssetId", &value); err != nil {
		return err
	}
	canonical, err := nativeReadUUID(value)
	if err != nil {
		return invalid("imageAssetId", "must be a canonical non-zero UUID")
	}
	*dst = NativeDeviceField[string]{Present: true, Value: canonical}
	return nil
}

func decodeTemplateConfiguration(raw []byte, dst *accessconfig.Configuration, canonical *json.RawMessage) error {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	configuration, err := accessconfig.DecodeConfiguration(raw)
	if err != nil {
		return invalid("configuration", "does not match the TemplateConfiguration contract")
	}
	encoded, err := canonicalizeJSON(raw)
	if err != nil {
		return invalid("configuration", "invalid JSON value")
	}
	*dst = configuration
	*canonical = encoded
	return nil
}

// rejectDuplicateJSONKeys walks every object, including arbitrary JSON values
// such as access parameters and script samples. Decoder.Token applies Go's
// standard JSON nesting limit; errors deliberately do not include key values.
func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return invalid("configuration", "invalid JSON value")
	}
	if err := walkUniqueJSONValue(dec, tok); err != nil {
		return err
	}
	if err := ensureEOF(dec); err != nil {
		return invalid("configuration", "invalid JSON value")
	}
	return nil
}

func walkUniqueJSONValue(dec *json.Decoder, tok json.Token) error {
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return invalid("configuration", "invalid JSON value")
			}
			key, ok := keyToken.(string)
			if !ok {
				return invalid("configuration", "invalid JSON value")
			}
			if _, exists := seen[key]; exists {
				return invalidStructure("duplicate property")
			}
			seen[key] = struct{}{}
			value, err := dec.Token()
			if err != nil {
				return invalid("configuration", "invalid JSON value")
			}
			if err := walkUniqueJSONValue(dec, value); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return invalid("configuration", "invalid JSON value")
		}
	case '[':
		for dec.More() {
			value, err := dec.Token()
			if err != nil {
				return invalid("configuration", "invalid JSON value")
			}
			if err := walkUniqueJSONValue(dec, value); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return invalid("configuration", "invalid JSON value")
		}
	default:
		return invalid("configuration", "invalid JSON value")
	}
	return nil
}

func canonicalizeJSON(raw []byte) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureEOF(dec); err != nil {
		return nil, err
	}
	out, err := json.Marshal(value)
	return json.RawMessage(out), err
}

func nullableFieldValue(field NativeDeviceField[string]) any {
	if !field.Present || field.Null {
		return nil
	}
	return field.Value
}
