package providerschema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const anyObjectSchema = `{"type":"object"}`

func TestCompilePairAndValidateConfigurationContract(t *testing.T) {
	pair, err := CompilePair(
		json.RawMessage(`{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":"object",
			"properties":{
				"connection":{"type":"object","properties":{"mode":{"enum":["cloud","lan"]}},"required":["mode"],"additionalProperties":false},
				"displayName":{"type":"string"}
			},
			"required":["connection"],
			"additionalProperties":false
		}`),
		json.RawMessage(anyObjectSchema),
	)
	if err != nil {
		t.Fatalf("CompilePair: %v", err)
	}

	for _, raw := range []string{
		`{"connection":{"mode":"cloud"}}`,
		`{"connection":{"mode":"lan"},"displayName":"sensor"}`,
	} {
		if err := pair.ValidateConfiguration(json.RawMessage(raw)); err != nil {
			t.Errorf("expected valid configuration %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{}`,
		`{"connection":{}}`,
		`{"connection":{"mode":"unknown"}}`,
		`{"connection":{"mode":"cloud","extra":true}}`,
		`{"connection":{"mode":"cloud"},"extra":true}`,
		`{"connection":{"mode":3}}`,
		`[]`,
	} {
		if err := pair.ValidateConfiguration(json.RawMessage(raw)); err == nil {
			t.Errorf("expected invalid configuration %s", raw)
		}
	}
}

func TestCompilePairAllowsLocalDefsReferences(t *testing.T) {
	pair, err := CompilePair(
		json.RawMessage(`{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"$defs":{"mode":{"type":"string","enum":["direct","gateway"]}},
			"type":"object",
			"properties":{"mode":{"$ref":"#/$defs/mode"}},
			"required":["mode"]
		}`),
		json.RawMessage(`{"type":"object","properties":{"enabled":{"type":"boolean"}}}`),
	)
	if err != nil {
		t.Fatalf("CompilePair with local ref: %v", err)
	}
	if err := pair.ValidateConfiguration(json.RawMessage(`{"mode":"direct"}`)); err != nil {
		t.Fatalf("local $defs reference should validate: %v", err)
	}
	if err := pair.ValidateConfiguration(json.RawMessage(`{"mode":"cloud"}`)); err == nil {
		t.Fatal("expected enum violation through local reference")
	}
}

func TestCompilePairRejectsNestedExternalReferencesButAllowsRefNamedProperty(t *testing.T) {
	_, err := CompilePair(
		json.RawMessage(`{"type":"object","properties":{"child":{"$ref":"https://127.0.0.1:1/private.json"}}}`),
		json.RawMessage(anyObjectSchema),
	)
	requireTypedError(t, err, ConfigurationDomain, SchemaError)

	pair, err := CompilePair(
		json.RawMessage(`{"type":"object","properties":{"$ref":{"type":"string"}},"required":["$ref"]}`),
		json.RawMessage(anyObjectSchema),
	)
	if err != nil {
		t.Fatalf("a data property named $ref must not be treated as a reference keyword: %v", err)
	}
	if err := pair.ValidateConfiguration(json.RawMessage(`{"$ref":"value"}`)); err != nil {
		t.Fatalf("property named $ref should validate: %v", err)
	}
}

func TestCompilePairRejectsExternalReferencesWithoutExposingReference(t *testing.T) {
	for _, ref := range []string{
		"https://127.0.0.1:1/private-schema.json",
		"file:///private/schema.json",
		"urn:private:schema",
	} {
		secretSentinel := "secret-reference-sentinel"
		refWithSentinel := strings.Replace(ref, "private", secretSentinel, 1)
		raw, err := json.Marshal(map[string]any{"$ref": refWithSentinel})
		if err != nil {
			t.Fatal(err)
		}
		_, err = CompilePair(raw, json.RawMessage(anyObjectSchema))
		requireTypedError(t, err, ConfigurationDomain, SchemaError)
		if strings.Contains(err.Error(), secretSentinel) || strings.Contains(err.Error(), refWithSentinel) {
			t.Fatalf("schema error exposed reference content: %q", err.Error())
		}
	}
}

func TestCompilePairRejectsUnsupportedDraftAndNonObjectSchemas(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"$schema":"https://json-schema.org/draft/2019-09/schema","type":"object"}`),
		json.RawMessage(`{"$schema":7,"type":"object"}`),
		json.RawMessage(`{"type":"object","properties":{"nested":{"$schema":"https://json-schema.org/draft/2019-09/schema","type":"string"}}}`),
		json.RawMessage(`[]`),
		json.RawMessage(`true`),
		json.RawMessage(`{"type":`),
		nil,
	} {
		_, err := CompilePair(raw, json.RawMessage(anyObjectSchema))
		requireTypedError(t, err, ConfigurationDomain, SchemaError)
	}
}

func TestValidationDomainsAreIndependentAndErrorsDoNotEchoInput(t *testing.T) {
	pair, err := CompilePair(
		json.RawMessage(`{"type":"object","required":["configuration"],"properties":{"configuration":{"type":"string"}},"additionalProperties":false}`),
		json.RawMessage(`{"type":"object","required":["deviceParameters"],"properties":{"deviceParameters":{"type":"integer"}},"additionalProperties":false}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	secretSentinel := "instance-secret-sentinel"
	requireTypedError(t, pair.ValidateConfiguration(json.RawMessage(`{"deviceParameters":"`+secretSentinel+`"}`)), ConfigurationDomain, InstanceError)
	if err := pair.ValidateConfiguration(json.RawMessage(`{"configuration":"ready"}`)); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}
	requireTypedError(t, pair.ValidateDeviceParameters(json.RawMessage(`{"configuration":"ready"}`)), DeviceParametersDomain, InstanceError)
	if err := pair.ValidateDeviceParameters(json.RawMessage(`{"deviceParameters":1}`)); err != nil {
		t.Fatalf("valid device parameters rejected: %v", err)
	}
	if err := pair.ValidateConfiguration(json.RawMessage(`{"configuration":"` + secretSentinel + `"}`)); err != nil {
		t.Fatal(err)
	}
	if err := pair.ValidateConfiguration(json.RawMessage(`{"configuration":"` + secretSentinel + `","other":"` + secretSentinel + `"}`)); err == nil || strings.Contains(err.Error(), secretSentinel) {
		t.Fatalf("instance error should be safe and fixed, got %v", err)
	}
}

func TestValidationPreservesJSONNumberPrecisionAndDoesNotApplyDefaults(t *testing.T) {
	pair, err := CompilePair(
		json.RawMessage(`{"type":"object","properties":{"counter":{"type":"integer","minimum":9007199254740993,"default":7}},"additionalProperties":false}`),
		json.RawMessage(anyObjectSchema),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := pair.ValidateConfiguration(json.RawMessage(`{"counter":9007199254740992}`)); err == nil {
		t.Fatal("rounded value below the exact minimum must fail")
	}
	if err := pair.ValidateConfiguration(json.RawMessage(`{"counter":9007199254740993}`)); err != nil {
		t.Fatalf("exact boundary value should pass: %v", err)
	}
	withoutDefault := json.RawMessage(`{}`)
	original := append(json.RawMessage(nil), withoutDefault...)
	if err := pair.ValidateConfiguration(withoutDefault); err != nil {
		t.Fatalf("optional defaulted property may be omitted: %v", err)
	}
	if string(withoutDefault) != string(original) {
		t.Fatalf("validation changed raw JSON: got %s want %s", withoutDefault, original)
	}
}

func TestCompilePairReportsTheFailingSchemaDomain(t *testing.T) {
	_, err := CompilePair(json.RawMessage(anyObjectSchema), json.RawMessage(`{"$ref":"http://127.0.0.1:1/device.json"}`))
	requireTypedError(t, err, DeviceParametersDomain, SchemaError)
}

func requireTypedError(t *testing.T, err error, domain Domain, kind ErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if typed.Domain != domain || typed.Kind != kind {
		t.Fatalf("error classification = (%q,%q), want (%q,%q)", typed.Domain, typed.Kind, domain, kind)
	}
}
