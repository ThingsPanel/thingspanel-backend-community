package providercatalog

import (
	"errors"
	"strings"
	"testing"
)

const validManifestTemplate = `{
	"formatVersion":1,
	"providerId":"123e4567-e89b-12d3-a456-426614174000",
	"name":"demo connector",
	"version":"v1+opaque",
	"artifactDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	"protocols":["mqtt","modbus"],
	"deviceKinds":["standalone","gateway","subdevice"],
	"configurationSchema": {"type":"object", "properties":{"host":{"type":"string"}}},
	"deviceParameterSchema":{"type":"object","additionalProperties":false}
}`

func TestValidateManifestAcceptsContractAndPreservesSchemaBytes(t *testing.T) {
	input := []byte(validManifestTemplate)
	manifest, err := ValidateManifest(input)
	if err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
	if manifest.FormatVersion() != 1 || manifest.ProviderID() != "123e4567-e89b-12d3-a456-426614174000" || manifest.Version() != "v1+opaque" {
		t.Fatalf("manifest identity not preserved")
	}
	if got := string(manifest.OriginalConfigurationSchema()); got != `{"type":"object", "properties":{"host":{"type":"string"}}}` {
		t.Fatalf("configuration schema bytes changed: %s", got)
	}
	if len(manifest.SchemaDigest()) != 64 {
		t.Fatalf("schema digest length = %d", len(manifest.SchemaDigest()))
	}

	configuration := manifest.OriginalConfigurationSchema()
	configuration[0] = '['
	if strings.HasPrefix(string(manifest.OriginalConfigurationSchema()), "[") {
		t.Fatal("schema accessor returned aliased storage")
	}
	protocols := manifest.Protocols()
	protocols[0] = "mutated"
	if manifest.Protocols()[0] != "mqtt" {
		t.Fatal("protocol accessor returned aliased storage")
	}
}

func TestSchemaDigestGoldenVectorAndFraming(t *testing.T) {
	configuration := []byte(`{"type":"object"}`)
	deviceParameters := []byte(` { "type" : "object" } `)
	got := computeSchemaDigest(configuration, deviceParameters)
	const want = "c22b6e3331c6e017e48f4d743404ac472defb88f2b29338e704f0082cbcebd75"
	if got != want {
		t.Fatalf("schema digest = %s, want %s", got, want)
	}
	if got == computeSchemaDigest(configuration, []byte(`{"type":"object"}`)) {
		t.Fatal("raw schema bytes must participate in identity")
	}
}

func TestValidateManifestRejectsMalformedStrictJSON(t *testing.T) {
	valid := []byte(validManifestTemplate)
	duplicate := strings.Replace(string(valid), `"name":"demo connector"`, `"name":"demo connector","name":"second"`, 1)
	duplicateSchemaKey := strings.Replace(string(valid), `"configurationSchema": {"type":"object", "properties":{"host":{"type":"string"}}}`, `"configurationSchema":{"type":"object","type":"array"}`, 1)
	unknown := strings.Replace(string(valid), `"name":"demo connector"`, `"name":"demo connector","unexpected":true`, 1)
	for name, input := range map[string][]byte{
		"duplicate root key":   []byte(duplicate),
		"duplicate schema key": []byte(duplicateSchemaKey),
		"unknown root key":     []byte(unknown),
		"trailing value":       append(append([]byte(nil), valid...), []byte(` {}`)...),
		"invalid utf8":         append(append([]byte(nil), valid[:len(valid)-1]...), 0xff, '}'),
		"array root":           []byte(`[]`),
		"malformed":            []byte(`{"formatVersion":`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateManifest(input)
			requireManifestError(t, err)
		})
	}
}

func TestValidateManifestFieldConstraints(t *testing.T) {
	for name, replacement := range map[string]string{
		"null protocols":        `"protocols":null`,
		"nil provider uuid":     `"providerId":"00000000-0000-0000-0000-000000000000"`,
		"uppercase uuid":        `"providerId":"123E4567-E89B-12D3-A456-426614174000"`,
		"artifact uppercase":    `"artifactDigest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789ABCDEF"`,
		"duplicate protocol":    `"protocols":["mqtt","mqtt"]`,
		"protocol nul":          `"protocols":["test\u0000protocol"]`,
		"long protocol":         `"protocols":["` + strings.Repeat("p", MaxProtocolCharacters+1) + `"]`,
		"unknown device kind":   `"deviceKinds":["sensor"]`,
		"duplicate device kind": `"deviceKinds":["gateway","gateway"]`,
		"unsupported format":    `"formatVersion":2`,
		"name nul":              `"name":"demo\u0000connector"`,
		"version nul":           `"version":"v1\u0000opaque"`,
	} {
		t.Run(name, func(t *testing.T) {
			input := replaceManifestField(t, validManifestTemplate, replacement)
			_, err := ValidateManifest([]byte(input))
			requireManifestError(t, err)
		})
	}
}

func TestManifestMaxLengthUsesUnicodeCodePointsAndEmptyArraysStayArrays(t *testing.T) {
	input := replaceManifestField(t, validManifestTemplate, `"name":"`+strings.Repeat("文", MaxNameCharacters)+`"`)
	input = replaceManifestField(t, input, `"version":"`+strings.Repeat("版", MaxVersionCharacters)+`"`)
	input = replaceManifestField(t, input, `"protocols":["`+strings.Repeat("协", MaxProtocolCharacters)+`"]`)
	input = replaceManifestField(t, input, `"deviceKinds":[]`)
	manifest, err := ValidateManifest([]byte(input))
	if err != nil {
		t.Fatalf("maximum Unicode codepoint lengths should pass: %v", err)
	}
	if manifest.DeviceKinds() == nil {
		t.Fatal("empty deviceKinds must be a non-nil empty slice")
	}
	if len(manifest.DeviceKinds()) != 0 {
		t.Fatalf("deviceKinds = %v, want empty", manifest.DeviceKinds())
	}

	input = replaceManifestField(t, validManifestTemplate, `"name":"`+strings.Repeat("文", MaxNameCharacters+1)+`"`)
	if _, err := ValidateManifest([]byte(input)); err == nil {
		t.Fatal("name beyond the Unicode codepoint limit accepted")
	}
	input = replaceManifestField(t, validManifestTemplate, `"protocols":["`+strings.Repeat("协", MaxProtocolCharacters+1)+`"]`)
	if _, err := ValidateManifest([]byte(input)); err == nil {
		t.Fatal("protocol beyond the Unicode codepoint limit accepted")
	}

	input = replaceManifestField(t, validManifestTemplate, `"protocols":[]`)
	emptyProtocols, err := ValidateManifest([]byte(input))
	if err != nil {
		t.Fatalf("empty protocols should pass: %v", err)
	}
	if emptyProtocols.Protocols() == nil || len(emptyProtocols.Protocols()) != 0 {
		t.Fatalf("empty protocols accessor = %#v, want non-nil empty slice", emptyProtocols.Protocols())
	}
}

func TestValidateManifestSeparatesSchemaErrorsWithoutEcho(t *testing.T) {
	secretSentinel := "private-schema-sentinel"
	input := replaceManifestField(t, validManifestTemplate,
		`"configurationSchema":{"$ref":"https://example.invalid/`+secretSentinel+`.json"}`)
	_, err := ValidateManifest([]byte(input))
	requireManifestError(t, err)
	if strings.Contains(err.Error(), secretSentinel) || strings.Contains(err.Error(), "example.invalid") {
		t.Fatalf("schema error echoed caller data: %v", err)
	}

	input = replaceManifestField(t, validManifestTemplate, `"configurationSchema":{"type":"object"}`)
	input = replaceManifestField(t, input, `"deviceParameterSchema":{"type":17}`)
	_, err = ValidateManifest([]byte(input))
	var typed *ManifestError
	if !errors.As(err, &typed) || typed.Field != manifestFieldDeviceParams || typed.Kind != "invalid_schema" {
		t.Fatalf("device parameter schema error classification = %#v, err=%v", typed, err)
	}
}

func TestValidateManifestSchemaSizeLimits(t *testing.T) {
	tooLarge := `"configurationSchema":{"type":"object","description":"` + strings.Repeat("x", MaxSchemaBytes) + `"}`
	input := replaceManifestField(t, validManifestTemplate, tooLarge)
	_, err := ValidateManifest([]byte(input))
	requireManifestError(t, err)

	if _, err := ValidateManifest(make([]byte, MaxManifestBytes+1)); err == nil {
		t.Fatal("manifest above maximum size accepted")
	}
}

func replaceManifestField(t *testing.T, input, replacement string) string {
	t.Helper()
	field := replacement[:strings.IndexByte(replacement, ':')]
	start := strings.Index(input, field+":")
	if start < 0 {
		t.Fatalf("field %s not found", field)
	}
	valueStart := start + len(field) + 1
	depth := 0
	quoted, escaped := false, false
	for i := valueStart; i < len(input); i++ {
		ch := input[i]
		if quoted {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		if ch == '"' {
			quoted = true
			continue
		}
		if ch == '{' || ch == '[' {
			depth++
		} else if ch == '}' || ch == ']' {
			if depth == 0 {
				return input[:start] + replacement + input[i:]
			}
			depth--
		} else if ch == ',' && depth == 0 {
			return input[:start] + replacement + input[i:]
		}
	}
	t.Fatalf("end of value for field %s not found", field)
	return ""
}

func requireManifestError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected manifest validation error")
	}
	var typed *ManifestError
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want *ManifestError", err)
	}
	if typed.Field == "" || typed.Kind == "" {
		t.Fatalf("incomplete safe error: %#v", typed)
	}
}
