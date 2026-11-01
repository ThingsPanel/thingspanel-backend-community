package devicemetadata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxDeviceJSONBytes = 64 * 1024

var (
	deviceLabelKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// NativeDeviceField records whether an UpdateDevice field was supplied. Null
// is distinct from omission; Value is meaningful when Present && !Null.
type NativeDeviceField[T any] struct {
	Present bool
	Null    bool
	Value   T
}

// NativeCreateDeviceInput is the locally validated CreateDevice request.
// Raw JSON fields deliberately retain their source bytes and numeric precision.
type NativeCreateDeviceInput struct {
	Name                       string
	DeviceNumber               string
	DeviceNumberPresent        bool
	DeviceKind                 string
	ProductID                  NativeDeviceField[string]
	Description                NativeDeviceField[string]
	Labels                     json.RawMessage
	Location                   NativeDeviceField[json.RawMessage]
	TemplateID                 NativeDeviceField[string]
	GatewayID                  NativeDeviceField[string]
	SubdeviceAddress           NativeDeviceField[string]
	InitialAdministrativeState string
	Tags                       []string
	CustomFields               json.RawMessage
	DirectAccess               NativeDeviceField[json.RawMessage]
	InitialGroupIDs            NativeDeviceField[[]string]
	BatchNumber                NativeDeviceField[string]
	DeviceParameters           json.RawMessage
}

// NativeUpdateDeviceInput expresses explicit partial replacement. JSON-backed
// object/array fields remain RawMessage so large numbers are never rounded.
type NativeUpdateDeviceInput struct {
	Name         NativeDeviceField[string]
	Description  NativeDeviceField[string]
	Labels       NativeDeviceField[json.RawMessage]
	Location     NativeDeviceField[json.RawMessage]
	Tags         NativeDeviceField[[]string]
	CustomFields NativeDeviceField[json.RawMessage]
	ProductID    NativeDeviceField[string]
}

// DecodeNativeCreateDevice validates only the request's local OAS shape.
// Tenant ownership, catalog/provider authorization, and schema compatibility
// remain service-owner responsibilities.
func DecodeNativeCreateDevice(data []byte) (NativeCreateDeviceInput, error) {
	var out NativeCreateDeviceInput
	fields, err := strictJSONObject(data)
	if err != nil {
		return out, err
	}
	allowed := setOf("name", "deviceNumber", "deviceKind", "productId", "description", "labels", "location", "templateId", "gatewayId", "subdeviceAddress", "initialAdministrativeState", "tags", "customFields", "directAccess", "initialGroupIds", "batchNumber", "deviceParameters")
	if err := rejectUnknown(fields, allowed); err != nil {
		return out, err
	}
	if err := requiredString(fields, "name", 1, 128, &out.Name); err != nil {
		return out, err
	}
	if _, ok := fields["deviceNumber"]; ok {
		if err := requiredDeviceNumber(fields, &out.DeviceNumber); err != nil {
			return out, err
		}
		out.DeviceNumberPresent = true
	}
	if err := requiredEnum(fields, "deviceKind", []string{"standalone", "gateway", "subdevice"}, &out.DeviceKind); err != nil {
		return out, err
	}

	out.InitialAdministrativeState = "enabled"
	if raw, ok := fields["initialAdministrativeState"]; ok {
		if err := decodeString(raw, "initialAdministrativeState", &out.InitialAdministrativeState); err != nil {
			return out, err
		}
		if out.InitialAdministrativeState != "enabled" && out.InitialAdministrativeState != "disabled" {
			return out, invalid("initialAdministrativeState", "must be enabled or disabled")
		}
	}
	out.Labels = json.RawMessage(`{}`)
	if raw, ok := fields["labels"]; ok {
		if err := validateLabels(raw); err != nil {
			return out, err
		}
		out.Labels = cloneRaw(raw)
	}
	out.CustomFields = json.RawMessage(`{}`)
	if raw, ok := fields["customFields"]; ok {
		if err := validateCustomFields(raw); err != nil {
			return out, err
		}
		out.CustomFields = cloneRaw(raw)
	}
	out.DeviceParameters = json.RawMessage(`{}`)
	if raw, ok := fields["deviceParameters"]; ok {
		if err := validateJSONMap(raw, "deviceParameters", 100, maxDeviceJSONBytes); err != nil {
			return out, err
		}
		out.DeviceParameters = cloneRaw(raw)
	}
	out.Tags = []string{}
	if raw, ok := fields["tags"]; ok {
		if err := decodeTags(raw, &out.Tags); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["location"]; ok {
		if err := decodeNullableJSON(raw, "location", validateLocation, &out.Location); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["productId"]; ok {
		if err := decodeNullableString(raw, "productId", 36, true, &out.ProductID); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["description"]; ok {
		if err := decodeNullableString(raw, "description", 2000, false, &out.Description); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["templateId"]; ok {
		if err := decodeNullableString(raw, "templateId", 36, true, &out.TemplateID); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["gatewayId"]; ok {
		if err := decodeNullableString(raw, "gatewayId", 36, true, &out.GatewayID); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["subdeviceAddress"]; ok {
		if err := decodeNullableString(raw, "subdeviceAddress", 128, false, &out.SubdeviceAddress); err != nil {
			return out, err
		}
		if !out.SubdeviceAddress.Null && out.SubdeviceAddress.Value == "" {
			return out, invalid("subdeviceAddress", "must not be empty")
		}
	}
	if raw, ok := fields["batchNumber"]; ok {
		if err := decodeNullableString(raw, "batchNumber", 128, false, &out.BatchNumber); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["directAccess"]; ok {
		if err := decodeNullableJSON(raw, "directAccess", validateDirectAccess, &out.DirectAccess); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["initialGroupIds"]; ok {
		var ids []string
		if err := decodeStringArray(raw, "initialGroupIds", 100, 36, true, &ids); err != nil {
			return out, err
		}
		out.InitialGroupIDs = NativeDeviceField[[]string]{Present: true, Value: ids}
	}
	if out.TemplateID.Present && !out.TemplateID.Null && out.DirectAccess.Present && !out.DirectAccess.Null {
		return out, invalid("directAccess", "cannot be combined with templateId")
	}
	if nonNull(out.GatewayID) != nonNull(out.SubdeviceAddress) {
		return out, invalid("gatewayId", "gatewayId and subdeviceAddress must both be non-null or both be null")
	}
	return out, nil
}

// DecodeNativeUpdateDevice validates the OAS UpdateDevice shape. Unknown
// properties (including readonly activation/runtime/model-binding fields) fail.
func DecodeNativeUpdateDevice(data []byte) (NativeUpdateDeviceInput, error) {
	var out NativeUpdateDeviceInput
	fields, err := strictJSONObject(data)
	if err != nil {
		return out, err
	}
	allowed := setOf("name", "description", "labels", "location", "tags", "customFields", "productId")
	if err := rejectUnknown(fields, allowed); err != nil {
		return out, err
	}
	if raw, ok := fields["name"]; ok {
		if err := decodeFieldString(raw, "name", 1, 128, false, &out.Name); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["description"]; ok {
		if err := decodeFieldString(raw, "description", 0, 2000, true, &out.Description); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["labels"]; ok {
		if err := validateLabels(raw); err != nil {
			return out, err
		}
		out.Labels = NativeDeviceField[json.RawMessage]{Present: true, Value: cloneRaw(raw)}
	}
	if raw, ok := fields["location"]; ok {
		if err := decodeNullableJSON(raw, "location", validateLocation, &out.Location); err != nil {
			return out, err
		}
	}
	if raw, ok := fields["tags"]; ok {
		var tags []string
		if err := decodeStringArray(raw, "tags", 100, 128, false, &tags); err != nil {
			return out, err
		}
		out.Tags = NativeDeviceField[[]string]{Present: true, Value: tags}
	}
	if raw, ok := fields["customFields"]; ok {
		if err := validateCustomFields(raw); err != nil {
			return out, err
		}
		out.CustomFields = NativeDeviceField[json.RawMessage]{Present: true, Value: cloneRaw(raw)}
	}
	if raw, ok := fields["productId"]; ok {
		if err := decodeNullableString(raw, "productId", 36, true, &out.ProductID); err != nil {
			return out, err
		}
	}
	return out, nil
}

func strictJSONObject(data []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, invalidStructure("JSON must be valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, invalidStructure("invalid JSON")
	}
	if tok != json.Delim('{') {
		return nil, &NativeDeviceInputError{Field: "$", Reason: "must be an object", Structural: true}
	}
	out := make(map[string]json.RawMessage)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, invalidStructure("invalid JSON")
		}
		key, ok := tok.(string)
		if !ok {
			return nil, invalidStructure("object key must be a string")
		}
		if _, exists := out[key]; exists {
			return nil, &NativeDeviceInputError{Field: key, Reason: "duplicate property", Structural: true}
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, invalidStructure("invalid JSON")
		}
		out[key] = raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, invalidStructure("invalid JSON")
	}
	if err := ensureEOF(dec); err != nil {
		return nil, err
	}
	return out, nil
}

func rejectUnknown(fields map[string]json.RawMessage, allowed map[string]struct{}) error {
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return invalid(key, "unknown property")
		}
	}
	return nil
}

func setOf(keys ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		out[k] = struct{}{}
	}
	return out
}
func invalid(field, message string) error {
	return &NativeDeviceInputError{Field: field, Reason: message}
}

func invalidStructure(message string) error {
	return &NativeDeviceInputError{Field: "$", Reason: message, Structural: true}
}

func wrapNestedJSONObjectError(err error, field, message string) error {
	var inputErr *NativeDeviceInputError
	if errors.As(err, &inputErr) && inputErr.Structural && inputErr.Reason != "must be an object" {
		return inputErr
	}
	return invalid(field, message)
}

func cloneRaw(raw []byte) json.RawMessage        { return append(json.RawMessage(nil), raw...) }
func nonNull[T any](f NativeDeviceField[T]) bool { return f.Present && !f.Null }

func requiredString(fields map[string]json.RawMessage, key string, min, max int, dst *string) error {
	raw, ok := fields[key]
	if !ok {
		return invalid(key, "required")
	}
	if err := decodeString(raw, key, dst); err != nil {
		return err
	}
	return checkText(key, *dst, min, max)
}
func requiredDeviceNumber(fields map[string]json.RawMessage, dst *string) error {
	if err := requiredString(fields, "deviceNumber", 1, 128, dst); err != nil {
		return err
	}
	return validateIdentityText("deviceNumber", *dst)
}
func requiredEnum(fields map[string]json.RawMessage, key string, values []string, dst *string) error {
	if err := requiredString(fields, key, 1, 64, dst); err != nil {
		return err
	}
	for _, value := range values {
		if *dst == value {
			return nil
		}
	}
	return invalid(key, "value is not in the allowed enum")
}
func decodeString(raw []byte, field string, dst *string) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return invalid(field, "must be a string")
	}
	if !utf8.ValidString(*dst) {
		return invalid(field, "must be valid UTF-8")
	}
	return nil
}
func checkText(field, value string, min, max int) error {
	n := utf8.RuneCountInString(value)
	if n < min || n > max {
		return invalid(field, fmt.Sprintf("length must be between %d and %d", min, max))
	}
	return nil
}

func validateIdentityText(field, value string) error {
	if strings.TrimFunc(value, unicode.IsSpace) != value {
		return invalid(field, "leading or trailing whitespace is not allowed")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return invalid(field, "control characters are not allowed")
		}
	}
	return nil
}
func decodeFieldString(raw []byte, field string, min, max int, nullable bool, dst *NativeDeviceField[string]) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if !nullable {
			return invalid(field, "must not be null")
		}
		*dst = NativeDeviceField[string]{Present: true, Null: true}
		return nil
	}
	var value string
	if err := decodeString(raw, field, &value); err != nil {
		return err
	}
	if err := checkText(field, value, min, max); err != nil {
		return err
	}
	*dst = NativeDeviceField[string]{Present: true, Value: value}
	return nil
}
func decodeNullableString(raw []byte, field string, max int, uuidValue bool, dst *NativeDeviceField[string]) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*dst = NativeDeviceField[string]{Present: true, Null: true}
		return nil
	}
	var value string
	if err := decodeString(raw, field, &value); err != nil {
		return err
	}
	min := 0
	if uuidValue {
		min = 36
	}
	if field == "subdeviceAddress" {
		min = 1
	}
	if err := checkText(field, value, min, max); err != nil {
		return err
	}
	if field == "subdeviceAddress" {
		if err := validateIdentityText(field, value); err != nil {
			return err
		}
	}
	if uuidValue {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != strings.ToLower(value) {
			return invalid(field, "must be a canonical UUID")
		}
	}
	*dst = NativeDeviceField[string]{Present: true, Value: value}
	return nil
}
func decodeNullableJSON(raw []byte, field string, validate func([]byte) error, dst *NativeDeviceField[json.RawMessage]) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*dst = NativeDeviceField[json.RawMessage]{Present: true, Null: true}
		return nil
	}
	if err := validate(raw); err != nil {
		return err
	}
	*dst = NativeDeviceField[json.RawMessage]{Present: true, Value: cloneRaw(raw)}
	return nil
}
func decodeStringArray(raw []byte, field string, maxItems, maxText int, uuidItems bool, dst *[]string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return invalid(field, "must be an array")
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return invalid(field, "must be an array of strings")
	}
	if len(values) > maxItems {
		return invalid(field, "contains too many items")
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := checkText(field, value, 1, maxText); err != nil {
			return err
		}
		if uuidItems {
			parsed, err := uuid.Parse(value)
			if err != nil || parsed.String() != strings.ToLower(value) {
				return invalid(field, "items must be canonical UUIDs")
			}
		}
		if _, ok := seen[value]; ok {
			return invalid(field, "items must be unique")
		}
		seen[value] = struct{}{}
	}
	*dst = values
	return nil
}
func decodeTags(raw []byte, dst *[]string) error {
	return decodeStringArray(raw, "tags", 100, 128, false, dst)
}

func validateLabels(raw []byte) error {
	fields, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, "labels", "must be an object")
	}
	if len(fields) > 32 {
		return invalid("labels", "contains more than 32 properties")
	}
	for key, valueRaw := range fields {
		if !deviceLabelKeyPattern.MatchString(key) {
			return invalid("labels", "property name has invalid format")
		}
		if trimmed := bytes.TrimSpace(valueRaw); len(trimmed) == 0 || trimmed[0] != '"' {
			return invalid("labels", "values must be strings")
		}
		var value string
		if err := json.Unmarshal(valueRaw, &value); err != nil {
			return invalid("labels", "values must be strings")
		}
		if err := checkText("labels", value, 0, 256); err != nil {
			return err
		}
	}
	return nil
}
func validateCustomFields(raw []byte) error {
	fields, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, "customFields", "must be an object")
	}
	if len(fields) > 100 {
		return invalid("customFields", "contains more than 100 properties")
	}
	for _, value := range fields {
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(value))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			return invalid("customFields", "contains invalid JSON")
		}
		switch v := decoded.(type) {
		case nil, bool, json.Number:
		case string:
			if !utf8.ValidString(v) {
				return invalid("customFields", "string must be valid UTF-8")
			}
		default:
			return invalid("customFields", "values must be scalar")
		}
	}
	return nil
}
func validateJSONMap(raw []byte, field string, maxProperties, maxBytes int) error {
	if len(raw) > maxBytes {
		return invalid(field, "JSON exceeds 64 KiB")
	}
	fields, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, field, "must be an object")
	}
	if len(fields) > maxProperties {
		return invalid(field, "contains too many properties")
	}
	return nil
}
func validateLocation(raw []byte) error {
	fields, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, "location", "must be an object")
	}
	if err := rejectUnknown(fields, setOf("latitude", "longitude", "address")); err != nil {
		return err
	}
	for _, key := range []string{"latitude", "longitude", "address"} {
		if _, ok := fields[key]; !ok {
			return invalid("location", "latitude, longitude, and address are required")
		}
	}
	latNull, lat, err := nullableJSONNumber(fields["latitude"], "location.latitude", -90, 90)
	if err != nil {
		return err
	}
	lonNull, lon, err := nullableJSONNumber(fields["longitude"], "location.longitude", -180, 180)
	if err != nil {
		return err
	}
	if latNull != lonNull {
		return invalid("location", "latitude and longitude must both be null or both be numbers")
	}
	_ = lat
	_ = lon
	if bytes.Equal(bytes.TrimSpace(fields["address"]), []byte("null")) {
		return nil
	}
	var address string
	if err := json.Unmarshal(fields["address"], &address); err != nil {
		return invalid("location.address", "must be a string or null")
	}
	return checkText("location.address", address, 0, 512)
}
func nullableJSONNumber(raw []byte, field string, min, max float64) (bool, float64, error) {
	token := bytes.TrimSpace(raw)
	if bytes.Equal(token, []byte("null")) {
		return true, 0, nil
	}
	if len(token) == 0 || (token[0] != '-' && (token[0] < '0' || token[0] > '9')) {
		return false, 0, invalid(field, "must be a number or null")
	}
	var number json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&number); err != nil {
		return false, 0, invalid(field, "must be a number or null")
	}
	v, err := number.Float64()
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) || v < min || v > max {
		return false, 0, invalid(field, "number is out of range")
	}
	// JSONB retains decimal precision. A value just outside an integral bound
	// can round onto that bound as float64; reject it before a DB CHECK turns
	// a locally accepted request into a persistence failure. Parse an exact
	// rational only at the boundary, keeping huge-exponent zero/tiny inputs
	// from allocating an unnecessarily enormous integer during validation.
	if v == min || v == max {
		exact, ok := new(big.Rat).SetString(number.String())
		if !ok || exact.Cmp(big.NewRat(int64(min), 1)) < 0 || exact.Cmp(big.NewRat(int64(max), 1)) > 0 {
			return false, 0, invalid(field, "number is out of range")
		}
	}
	return false, v, nil
}
func validateDirectAccess(raw []byte) error {
	fields, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, "directAccess", "must be an object")
	}
	if err := rejectUnknown(fields, setOf("access", "externalDeviceId")); err != nil {
		return err
	}
	access, ok := fields["access"]
	if !ok {
		return invalid("directAccess.access", "required")
	}
	if err := validateAccessConfiguration(access); err != nil {
		return err
	}
	ext, ok := fields["externalDeviceId"]
	if !ok {
		return invalid("directAccess.externalDeviceId", "required")
	}
	if !bytes.Equal(bytes.TrimSpace(ext), []byte("null")) {
		var value string
		if err := json.Unmarshal(ext, &value); err != nil {
			return invalid("directAccess.externalDeviceId", "must be a string or null")
		}
		if err := checkText("directAccess.externalDeviceId", value, 1, 256); err != nil {
			return err
		}
	}
	return nil
}
func validateAccessConfiguration(raw []byte) error {
	f, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, "directAccess.access", "must be an object")
	}
	allowed := setOf("mode", "protocol", "providerId", "providerVersion", "schemaDigest", "parameters", "credentialPolicy", "onlinePolicy", "connectionDirection", "serviceInstanceId")
	if err := rejectUnknown(f, allowed); err != nil {
		return err
	}
	for _, key := range []string{"mode", "protocol", "providerId", "parameters", "credentialPolicy", "onlinePolicy", "connectionDirection", "serviceInstanceId"} {
		if _, ok := f[key]; !ok {
			return invalid("directAccess.access."+key, "required")
		}
	}
	if !rawStringIn(f["mode"], "direct", "protocol_connector", "service_connector", "gateway") {
		return invalid("directAccess.access.mode", "invalid enum value")
	}
	if err := rawText(f["protocol"], "directAccess.access.protocol", 1, 64, false); err != nil {
		return err
	}
	if err := rawNullableString(f["providerId"], "directAccess.access.providerId", 36, true); err != nil {
		return err
	}
	if raw, exists := f["providerVersion"]; exists {
		if err := rawNullableText(raw, "directAccess.access.providerVersion", 1, 128); err != nil {
			return err
		}
	}
	if raw, exists := f["schemaDigest"]; exists {
		var digest string
		if err := json.Unmarshal(raw, &digest); err != nil || len(digest) != 64 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
			return invalid("directAccess.access.schemaDigest", "must be 64 lowercase hexadecimal characters")
		}
	}
	if err := validateJSONMap(f["parameters"], "directAccess.access.parameters", math.MaxInt, maxDeviceJSONBytes); err != nil {
		return err
	}
	if err := validateCredentialPolicy(f["credentialPolicy"]); err != nil {
		return err
	}
	if err := validateOnlinePolicy(f["onlinePolicy"]); err != nil {
		return err
	}
	if !rawStringIn(f["connectionDirection"], "device_to_platform", "platform_to_device") {
		return invalid("directAccess.access.connectionDirection", "invalid enum value")
	}
	if err := rawNullableText(f["serviceInstanceId"], "directAccess.access.serviceInstanceId", 1, 256); err != nil {
		return err
	}
	return nil
}
func validateCredentialPolicy(raw []byte) error {
	f, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, "directAccess.access.credentialPolicy", "must be an object")
	}
	if err := rejectUnknown(f, setOf("kind", "autoRegister")); err != nil {
		return err
	}
	if !rawStringIn(f["kind"], "none", "token", "username_password", "x509") {
		return invalid("directAccess.access.credentialPolicy.kind", "invalid enum value")
	}
	if bytes.Equal(bytes.TrimSpace(f["autoRegister"]), []byte("true")) {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(f["autoRegister"]), []byte("false")) {
		return nil
	}
	return invalid("directAccess.access.credentialPolicy.autoRegister", "must be a boolean")
}
func validateOnlinePolicy(raw []byte) error {
	f, err := strictJSONObject(raw)
	if err != nil {
		return wrapNestedJSONObjectError(err, "directAccess.access.onlinePolicy", "must be an object")
	}
	if err := rejectUnknown(f, setOf("mode", "timeoutSeconds")); err != nil {
		return err
	}
	if !rawStringIn(f["mode"], "transport", "heartbeat", "activity") {
		return invalid("directAccess.access.onlinePolicy.mode", "invalid enum value")
	}
	var n int64
	if err := json.Unmarshal(f["timeoutSeconds"], &n); err != nil || n < 1 || n > 86400 {
		return invalid("directAccess.access.onlinePolicy.timeoutSeconds", "must be an integer from 1 to 86400")
	}
	return nil
}
func rawStringIn(raw []byte, values ...string) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func rawText(raw []byte, field string, min, max int, nullable bool) error {
	if nullable && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return invalid(field, "must be a string")
	}
	return checkText(field, value, min, max)
}
func rawNullableString(raw []byte, field string, max int, uuidValue bool) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return invalid(field, "must be a string or null")
	}
	min := 0
	if uuidValue {
		min = 36
	}
	if err := checkText(field, value, min, max); err != nil {
		return err
	}
	if uuidValue {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.String() != strings.ToLower(value) {
			return invalid(field, "must be a canonical UUID")
		}
	}
	return nil
}

func rawNullableText(raw []byte, field string, min, max int) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return invalid(field, "must be a string or null")
	}
	return checkText(field, value, min, max)
}
func ensureEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return invalidStructure("invalid trailing JSON")
	}
	return invalidStructure("must contain exactly one JSON value")
}
