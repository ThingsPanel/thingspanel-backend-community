// Package accessconfig contains transport-independent values for one current
// device access template configuration. It validates shape and local invariants only;
// resource ownership and provider capabilities belong to their authoritative
// owners and are intentionally not inferred here.
package accessconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Configuration struct {
	DeviceKind             string                  `json:"deviceKind"`
	Model                  json.RawMessage         `json:"model"` // object or explicit null
	Access                 AccessConfiguration     `json:"access"`
	TopicMappings          []TopicMapping          `json:"topicMappings"`
	ProcessingScripts      []ProcessingScript      `json:"processingScripts"`
	CustomFieldDefinitions []CustomFieldDefinition `json:"customFieldDefinitions"`
}

type AccessConfiguration struct {
	Mode                string           `json:"mode"`
	Protocol            string           `json:"protocol"`
	ProviderID          json.RawMessage  `json:"providerId"`
	ProviderVersion     json.RawMessage  `json:"providerVersion,omitempty"`
	SchemaDigest        json.RawMessage  `json:"schemaDigest,omitempty"`
	Parameters          json.RawMessage  `json:"parameters"`
	CredentialPolicy    CredentialPolicy `json:"credentialPolicy"`
	OnlinePolicy        OnlinePolicy     `json:"onlinePolicy"`
	ConnectionDirection string           `json:"connectionDirection"`
	ServiceInstanceID   json.RawMessage  `json:"serviceInstanceId"`
}

type CredentialPolicy struct {
	Kind         string `json:"kind"`
	AutoRegister bool   `json:"autoRegister"`
}
type OnlinePolicy struct {
	Mode           string `json:"mode"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}
type TopicMapping struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    json.RawMessage `json:"description"`
	Direction      string          `json:"direction"`
	SourceTopic    string          `json:"sourceTopic"`
	TargetTopic    string          `json:"targetTopic"`
	DataIdentifier json.RawMessage `json:"dataIdentifier"`
	Priority       int             `json:"priority"`
	Enabled        bool            `json:"enabled"`
	DataKind       json.RawMessage `json:"dataKind"`
}
type ProcessingScript struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     json.RawMessage `json:"description"`
	Phase           string          `json:"phase"`
	Language        string          `json:"language"`
	Entrypoint      string          `json:"entrypoint"`
	Content         string          `json:"content"`
	Enabled         bool            `json:"enabled"`
	LastSampleInput json.RawMessage `json:"lastSampleInput"`
}
type CustomFieldDefinition struct {
	Key          string          `json:"key"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	DefaultValue json.RawMessage `json:"defaultValue"`
	Description  json.RawMessage `json:"description"`
	Enabled      bool            `json:"enabled"`
	EnumValues   []EnumValue     `json:"enumValues"`
}
type EnumValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ExternalValidator is the integration boundary for checks owned elsewhere:
// published model resolution/tenant ownership, provider and service-instance
// capability/tenant checks, and fixed-model topic identifier resolution.
// Implementations must use trusted tenant context; client payloads are not an
// authority source. This package deliberately provides no default validator.
type ExternalValidator interface {
	ValidateModel(context.Context, json.RawMessage) error
	ValidateAccess(context.Context, AccessConfiguration) error
	ValidateTopicMappings(context.Context, json.RawMessage, []TopicMapping) error
}

// ValidateExternal delegates resource and capability checks after local shape
// validation. Callers must first obtain the trusted tenant from service auth.
func (d Configuration) ValidateExternal(ctx context.Context, validator ExternalValidator) error {
	if validator == nil {
		return invalid("external validator is required")
	}
	if err := d.Validate(); err != nil {
		return err
	}
	if !isNull(d.Model) {
		if err := validator.ValidateModel(ctx, append(json.RawMessage(nil), d.Model...)); err != nil {
			return err
		}
	}
	if err := validator.ValidateAccess(ctx, d.Access); err != nil {
		return err
	}
	return validator.ValidateTopicMappings(ctx, append(json.RawMessage(nil), d.Model...), d.TopicMappings)
}

var fieldKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// DecodeConfiguration performs strict JSON decoding and validates only the contract's
// self-contained shape and value rules. model/provider/instance existence,
// tenant ownership, schema compatibility, secret scanning, and topic/model
// reference resolution require their owning services.
func DecodeConfiguration(data []byte) (Configuration, error) {
	var d Configuration
	if err := validateStructure(data, "configuration"); err != nil {
		return d, err
	}
	if err := decodeStrict(data, &d); err != nil {
		return d, err
	}
	if err := d.Validate(); err != nil {
		return Configuration{}, err
	}
	return d, nil
}

func (d Configuration) Validate() error {
	if !oneOf(d.DeviceKind, "standalone", "gateway", "subdevice") {
		return invalid("deviceKind")
	}
	if err := validateModel(d.Model); err != nil {
		return err
	}
	if err := d.Access.validate(); err != nil {
		return err
	}
	if len(d.TopicMappings) > 1000 {
		return invalid("topicMappings exceeds 1000 items")
	}
	if d.TopicMappings == nil {
		return invalid("topicMappings must be an array")
	}
	if len(d.ProcessingScripts) > 100 {
		return invalid("processingScripts exceeds 100 items")
	}
	if d.ProcessingScripts == nil {
		return invalid("processingScripts must be an array")
	}
	if len(d.CustomFieldDefinitions) > 100 {
		return invalid("customFieldDefinitions exceeds 100 items")
	}
	if d.CustomFieldDefinitions == nil {
		return invalid("customFieldDefinitions must be an array")
	}
	ids := map[string]bool{}
	pairs := map[string]bool{}
	for i, t := range d.TopicMappings {
		p := fmt.Sprintf("topicMappings[%d]", i)
		if !validUUID(t.ID) || ids[t.ID] {
			return invalid(p + ".id invalid or duplicated")
		}
		ids[t.ID] = true
		if !length(t.Name, 1, 128) || !nullableString(t.Description, 0, 2000) || !oneOf(t.Direction, "up", "down") || !length(t.SourceTopic, 1, 1024) || !length(t.TargetTopic, 1, 1024) || t.Priority < 0 || t.Priority > 10000 {
			return invalid(p)
		}
		if (isNull(t.DataKind)) != (isNull(t.DataIdentifier)) {
			return invalid(p + " dataKind and dataIdentifier must both be null or non-null")
		}
		if !isNull(t.DataKind) {
			var k string
			if json.Unmarshal(t.DataKind, &k) != nil || !oneOf(k, "TELEMETRY", "ATTRIBUTE", "EVENT", "COMMAND") {
				return invalid(p + ".dataKind")
			}
			var id string
			if json.Unmarshal(t.DataIdentifier, &id) != nil || id == "" {
				return invalid(p + ".dataIdentifier")
			}
		}
		pair := t.Direction + "\x00" + t.SourceTopic + "\x00" + t.TargetTopic
		if pairs[pair] {
			return invalid(p + " duplicate direction/sourceTopic/targetTopic")
		}
		pairs[pair] = true
	}
	enabledPhases := map[string]bool{}
	for i, s := range d.ProcessingScripts {
		p := fmt.Sprintf("processingScripts[%d]", i)
		if !validUUID(s.ID) || ids[s.ID] {
			return invalid(p + ".id invalid or duplicated")
		}
		ids[s.ID] = true
		if !length(s.Name, 1, 128) || !nullableString(s.Description, 0, 2000) || !oneOf(s.Phase, "telemetry_up", "telemetry_down", "attribute_up", "attribute_down", "command_down", "event_up") || s.Language != "lua" || s.Entrypoint != "encodeInp" || !length(s.Content, 1, 262144) || !nullableString(s.LastSampleInput, 0, 262144) {
			return invalid(p)
		}
		if s.Enabled {
			if enabledPhases[s.Phase] {
				return invalid("multiple enabled scripts for phase " + s.Phase)
			}
			enabledPhases[s.Phase] = true
		}
	}
	keys := map[string]bool{}
	for i, f := range d.CustomFieldDefinitions {
		p := fmt.Sprintf("customFieldDefinitions[%d]", i)
		if f.EnumValues == nil || !fieldKeyPattern.MatchString(f.Key) || len(f.Key) > 64 || keys[f.Key] || !length(f.Name, 1, 128) || !oneOf(f.Type, "string", "number", "boolean", "enum") || !nullableString(f.Description, 0, 2000) || len(f.EnumValues) > 1000 {
			return invalid(p)
		}
		keys[f.Key] = true
		if err := validateDefault(f.Type, f.DefaultValue); err != nil {
			return fmt.Errorf("%s.defaultValue: %w", p, err)
		}
		values := map[string]bool{}
		for j, e := range f.EnumValues {
			if !length(e.Value, 1, 256) || !length(e.Label, 1, 256) || values[e.Value] {
				return invalid(fmt.Sprintf("%s.enumValues[%d]", p, j))
			}
			values[e.Value] = true
		}
		if f.Type == "enum" {
			if len(f.EnumValues) == 0 {
				return invalid(p + " enum needs values")
			}
			if !isNull(f.DefaultValue) {
				var v string
				if json.Unmarshal(f.DefaultValue, &v) != nil || !values[v] {
					return invalid(p + " enum default is not an option")
				}
			}
		} else if len(f.EnumValues) != 0 {
			return invalid(p + " enumValues must be empty for non-enum type")
		}
	}
	return nil
}

func (a AccessConfiguration) validate() error {
	if !oneOf(a.Mode, "direct", "protocol_connector", "service_connector", "gateway") || !length(a.Protocol, 1, 64) || !rawNullableString(a.ProviderID, 36) || !optionalNullableString(a.ProviderVersion, 128) || !optionalDigest(a.SchemaDigest) || len(a.Parameters) == 0 || len(a.Parameters) > 65536 || !oneOf(a.ConnectionDirection, "device_to_platform", "platform_to_device") || !rawNullableString(a.ServiceInstanceID, 256) {
		return invalid("access")
	}
	if !validNullableUUID(a.ProviderID) {
		return invalid("access.providerId")
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(a.Parameters, &params) != nil || params == nil {
		return invalid("access.parameters must be an object")
	}
	if !oneOf(a.CredentialPolicy.Kind, "none", "token", "username_password", "x509") {
		return invalid("access.credentialPolicy")
	}
	if !oneOf(a.OnlinePolicy.Mode, "transport", "heartbeat", "activity") || a.OnlinePolicy.TimeoutSeconds < 1 || a.OnlinePolicy.TimeoutSeconds > 86400 {
		return invalid("access.onlinePolicy")
	}
	if a.Mode == "service_connector" && (isNull(a.ServiceInstanceID) || rawString(a.ServiceInstanceID) == "") {
		return invalid("service_connector requires serviceInstanceId")
	}
	return nil
}

func validateModel(raw json.RawMessage) error {
	if len(raw) == 0 {
		return invalid("model is required")
	}
	if isNull(raw) {
		return nil
	}
	var v struct {
		ModelID  string      `json:"modelId"`
		Revision json.Number `json:"publishedRevision"`
	}
	if err := decodeStrict(raw, &v); err != nil {
		return fmt.Errorf("model: %w", err)
	}
	var model map[string]json.RawMessage
	_ = json.Unmarshal(raw, &model)
	revisionRaw := bytes.TrimSpace(model["publishedRevision"])
	if len(revisionRaw) == 0 || revisionRaw[0] == '"' {
		return invalid("model.publishedRevision must be a JSON number")
	}
	revision, err := strconv.ParseInt(string(v.Revision), 10, 64)
	if !validUUID(v.ModelID) || err != nil || revision < 1 || revision > 9007199254740991 {
		return invalid("model")
	}
	return nil
}
func validateDefault(t string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return invalid("required")
	}
	if isNull(raw) {
		return nil
	}
	switch t {
	case "string":
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return invalid("must be string or null")
		}
	case "number":
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		token, err := dec.Token()
		if _, ok := token.(json.Number); err != nil || !ok {
			return invalid("must be number or null")
		}
		if _, err := dec.Token(); err != io.EOF {
			return invalid("must be one JSON number")
		}
	case "boolean":
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return invalid("must be boolean or null")
		}
	case "enum":
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return invalid("must be string or null")
		}
	default:
		return invalid("unknown type")
	}
	return nil
}
func decodeStrict(b []byte, dst any) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil || obj == nil {
		return invalid("expected object")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return invalid("trailing JSON")
	}
	return nil
}
func validateStructure(raw json.RawMessage, kind string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return invalid(kind + " must be an object")
	}
	fields := map[string][]string{
		"configuration":    {"deviceKind", "model", "access", "topicMappings", "processingScripts", "customFieldDefinitions"},
		"model":            {"modelId", "publishedRevision"},
		"access":           {"mode", "protocol", "providerId", "parameters", "credentialPolicy", "onlinePolicy", "connectionDirection", "serviceInstanceId"},
		"credentialPolicy": {"kind", "autoRegister"}, "onlinePolicy": {"mode", "timeoutSeconds"},
		"topic":       {"id", "name", "description", "direction", "sourceTopic", "targetTopic", "dataIdentifier", "priority", "enabled", "dataKind"},
		"script":      {"id", "name", "description", "phase", "language", "entrypoint", "content", "enabled", "lastSampleInput"},
		"customField": {"key", "name", "type", "defaultValue", "description", "enabled", "enumValues"}, "enumValue": {"value", "label"},
	}
	known, ok := fields[kind]
	if !ok {
		return invalid("internal schema")
	}
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
		if _, exists := m[k]; !exists {
			return invalid(kind + " missing required field " + k)
		}
	}
	if kind == "access" {
		set["providerVersion"] = true
		set["schemaDigest"] = true
	}
	for k := range m {
		if !set[k] {
			return invalid(kind + " unknown field " + k)
		}
	}
	if err := validateJSONKinds(kind, m); err != nil {
		return err
	}
	if kind == "access" {
		if raw, ok := m["providerVersion"]; ok && !matchesJSONType(raw, "nullable-string") {
			return invalid("access.providerVersion has wrong JSON type")
		}
		if raw, ok := m["schemaDigest"]; ok && !matchesJSONType(raw, "string") {
			return invalid("access.schemaDigest has wrong JSON type")
		}
	}
	check := func(key, child string) error { return validateStructure(m[key], child) }
	switch kind {
	case "configuration":
		if !isNull(m["model"]) {
			if err := check("model", "model"); err != nil {
				return err
			}
		}
		if err := check("access", "access"); err != nil {
			return err
		}
		for _, x := range []struct{ k, t string }{{"topicMappings", "topic"}, {"processingScripts", "script"}, {"customFieldDefinitions", "customField"}} {
			var a []json.RawMessage
			if isNull(m[x.k]) || json.Unmarshal(m[x.k], &a) != nil {
				return invalid(x.k + " must be array")
			}
			for _, v := range a {
				if err := validateStructure(v, x.t); err != nil {
					return err
				}
			}
		}
	case "access":
		if err := check("credentialPolicy", "credentialPolicy"); err != nil {
			return err
		}
		return check("onlinePolicy", "onlinePolicy")
	case "customField":
		var a []json.RawMessage
		if isNull(m["enumValues"]) || json.Unmarshal(m["enumValues"], &a) != nil {
			return invalid("enumValues must be array")
		}
		for _, v := range a {
			if err := validateStructure(v, "enumValue"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateJSONKinds(kind string, m map[string]json.RawMessage) error {
	expected := map[string]map[string]string{
		"configuration":    {"deviceKind": "string", "model": "nullable-object", "access": "object", "topicMappings": "array", "processingScripts": "array", "customFieldDefinitions": "array"},
		"model":            {"modelId": "string", "publishedRevision": "number"},
		"access":           {"mode": "string", "protocol": "string", "providerId": "nullable-string", "parameters": "object", "credentialPolicy": "object", "onlinePolicy": "object", "connectionDirection": "string", "serviceInstanceId": "nullable-string"},
		"credentialPolicy": {"kind": "string", "autoRegister": "boolean"}, "onlinePolicy": {"mode": "string", "timeoutSeconds": "number"},
		"topic":       {"id": "string", "name": "string", "description": "nullable-string", "direction": "string", "sourceTopic": "string", "targetTopic": "string", "dataIdentifier": "nullable-string", "priority": "number", "enabled": "boolean", "dataKind": "nullable-string"},
		"script":      {"id": "string", "name": "string", "description": "nullable-string", "phase": "string", "language": "string", "entrypoint": "string", "content": "string", "enabled": "boolean", "lastSampleInput": "nullable-string"},
		"customField": {"key": "string", "name": "string", "type": "string", "defaultValue": "nullable-scalar", "description": "nullable-string", "enabled": "boolean", "enumValues": "array"},
		"enumValue":   {"value": "string", "label": "string"},
	}
	for key, typ := range expected[kind] {
		if !matchesJSONType(m[key], typ) {
			return invalid(kind + "." + key + " has wrong JSON type")
		}
	}
	return nil
}
func matchesJSONType(raw json.RawMessage, typ string) bool {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return false
	}
	if bytes.Equal(b, []byte("null")) {
		return strings.HasPrefix(typ, "nullable-")
	}
	switch typ {
	case "nullable-object":
		typ = "object"
	case "nullable-string":
		typ = "string"
	case "nullable-scalar":
		return b[0] == '"' || b[0] == '-' || (b[0] >= '0' && b[0] <= '9') || bytes.Equal(b, []byte("true")) || bytes.Equal(b, []byte("false"))
	}
	switch typ {
	case "object":
		return b[0] == '{'
	case "array":
		return b[0] == '['
	case "string":
		return b[0] == '"'
	case "boolean":
		return bytes.Equal(b, []byte("true")) || bytes.Equal(b, []byte("false"))
	case "number":
		return b[0] == '-' || (b[0] >= '0' && b[0] <= '9')
	}
	return false
}
func isNull(r json.RawMessage) bool      { return bytes.Equal(bytes.TrimSpace(r), []byte("null")) }
func rawString(r json.RawMessage) string { var s string; _ = json.Unmarshal(r, &s); return s }
func validUUID(s string) bool {
	u, e := uuid.Parse(s)
	return e == nil && u.String() == strings.ToLower(s) && u != uuid.Nil
}
func validNullableUUID(r json.RawMessage) bool {
	if isNull(r) {
		return true
	}
	var s string
	return json.Unmarshal(r, &s) == nil && validUUID(s)
}
func rawNullableString(r json.RawMessage, max int) bool {
	if len(r) == 0 {
		return false
	}
	if isNull(r) {
		return true
	}
	var s string
	if json.Unmarshal(r, &s) != nil {
		return false
	}
	return length(s, 1, max)
}
func optionalNullableString(r json.RawMessage, max int) bool {
	return len(r) == 0 || rawNullableString(r, max)
}
func optionalDigest(r json.RawMessage) bool {
	if len(r) == 0 {
		return true
	}
	var digest string
	return json.Unmarshal(r, &digest) == nil && regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest)
}
func nullableString(r json.RawMessage, min, max int) bool {
	if len(r) == 0 {
		return false
	}
	if isNull(r) {
		return true
	}
	var s string
	if json.Unmarshal(r, &s) != nil {
		return false
	}
	return length(s, min, max)
}
func length(s string, min, max int) bool { return len([]rune(s)) >= min && len([]rune(s)) <= max }
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func invalid(s string) error { return fmt.Errorf("invalid TemplateConfiguration: %s", s) }
