package accessconfig

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type recordingValidator struct{ calls []string }

func (v *recordingValidator) ValidateModel(context.Context, json.RawMessage) error {
	v.calls = append(v.calls, "model")
	return nil
}
func (v *recordingValidator) ValidateAccess(context.Context, AccessConfiguration) error {
	v.calls = append(v.calls, "access")
	return nil
}
func (v *recordingValidator) ValidateTopicMappings(context.Context, json.RawMessage, []TopicMapping) error {
	v.calls = append(v.calls, "topics")
	return nil
}

const validConfiguration = `{"deviceKind":"standalone","model":null,"access":{"mode":"direct","protocol":"mqtt","providerId":null,"providerVersion":null,"schemaDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parameters":{"ratio":1.234567890123456789,"opaque":{"x":true}},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":60},"connectionDirection":"device_to_platform","serviceInstanceId":null},"topicMappings":[],"processingScripts":[],"customFieldDefinitions":[]}`

func TestDecodeConfigurationAcceptsExplicitNullModelAndPreservesRawNumbers(t *testing.T) {
	d, err := DecodeConfiguration([]byte(validConfiguration))
	if err != nil {
		t.Fatal(err)
	}
	if string(d.Model) != "null" {
		t.Fatalf("model raw = %q", d.Model)
	}
	if got := string(d.Access.Parameters); !strings.Contains(got, "1.234567890123456789") {
		t.Fatalf("parameter precision lost: %s", got)
	}
}

func TestExternalValidationBoundarySkipsAbsentModelAndDelegatesOwnerChecks(t *testing.T) {
	d, err := DecodeConfiguration([]byte(validConfiguration))
	if err != nil {
		t.Fatal(err)
	}
	v := &recordingValidator{}
	if err := d.ValidateExternal(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(v.calls, ","); got != "access,topics" {
		t.Fatalf("external calls = %s", got)
	}
}

func TestDecodeConfigurationRejectsMissingUnknownAndImplicitNullFields(t *testing.T) {
	cases := map[string]string{
		"missing required top field":  strings.Replace(validConfiguration, `,"model":null`, "", 1),
		"unknown top field":           strings.Replace(validConfiguration, `"deviceKind":"standalone"`, `"deviceKind":"standalone","extra":1`, 1),
		"missing required field":      strings.Replace(validConfiguration, `,"serviceInstanceId":null`, "", 1),
		"unknown closed nested field": strings.Replace(validConfiguration, `"credentialPolicy":{"kind":"none"`, `"credentialPolicy":{"extra":true,"kind":"none"`, 1),
		"null required boolean":       strings.Replace(validConfiguration, `"autoRegister":false`, `"autoRegister":null`, 1),
		"null required string":        strings.Replace(validConfiguration, `"protocol":"mqtt"`, `"protocol":null`, 1),
		"unknown access field":        strings.Replace(validConfiguration, `"protocol":"mqtt"`, `"protocol":"mqtt","surprise":true`, 1),
		"missing online timeout":      strings.Replace(validConfiguration, `,"timeoutSeconds":60`, "", 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeConfiguration([]byte(body)); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestAccessProviderVersionAndSchemaDigestAreOptionalButStrictWhenPresent(t *testing.T) {
	withoutOptional := strings.Replace(validConfiguration, `,"providerVersion":null`, "", 1)
	withoutOptional = strings.Replace(withoutOptional, `,"schemaDigest":"`+strings.Repeat("a", 64)+`"`, "", 1)
	if _, err := DecodeConfiguration([]byte(withoutOptional)); err != nil {
		t.Fatalf("omitted provider version and schema digest rejected: %v", err)
	}
	decoded, err := DecodeConfiguration([]byte(withoutOptional))
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var roundTripMap map[string]json.RawMessage
	if err := json.Unmarshal(roundTrip, &roundTripMap); err != nil {
		t.Fatal(err)
	}
	var accessMap map[string]json.RawMessage
	if err := json.Unmarshal(roundTripMap["access"], &accessMap); err != nil {
		t.Fatal(err)
	}
	if _, ok := accessMap["providerVersion"]; ok {
		t.Fatal("omitted providerVersion reappeared as null after marshal")
	}
	if _, ok := accessMap["schemaDigest"]; ok {
		t.Fatal("omitted schemaDigest reappeared as null after marshal")
	}
	if _, err := DecodeConfiguration(roundTrip); err != nil {
		t.Fatalf("configuration failed after decode/marshal/decode: %v", err)
	}
	withNullVersion := strings.Replace(withoutOptional, `"providerId":null`, `"providerId":null,"providerVersion":null`, 1)
	if _, err := DecodeConfiguration([]byte(withNullVersion)); err != nil {
		t.Fatalf("explicit nullable provider version rejected: %v", err)
	}
	for name, body := range map[string]string{
		"version wrong type":  strings.Replace(withoutOptional, `"providerId":null`, `"providerId":null,"providerVersion":1`, 1),
		"version too long":    strings.Replace(withoutOptional, `"providerId":null`, `"providerId":null,"providerVersion":"`+strings.Repeat("v", 129)+`"`, 1),
		"digest wrong length": strings.Replace(withoutOptional, `"parameters":`, `"schemaDigest":"abc","parameters":`, 1),
		"digest uppercase":    strings.Replace(withoutOptional, `"parameters":`, `"schemaDigest":"`+strings.Repeat("A", 64)+`","parameters":`, 1),
		"digest null":         strings.Replace(withoutOptional, `"parameters":`, `"schemaDigest":null,"parameters":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeConfiguration([]byte(body)); err == nil {
				t.Fatal("invalid optional field accepted")
			}
		})
	}
}

func TestDecodeConfigurationValidatesTopicScriptsAndExtensions(t *testing.T) {
	good := strings.Replace(validConfiguration, `"topicMappings":[]`, `"topicMappings":[{"id":"11111111-1111-4111-8111-111111111111","name":"up","description":null,"direction":"up","sourceTopic":"a","targetTopic":"b","dataIdentifier":null,"priority":0,"enabled":true,"dataKind":null}]`, 1)
	good = strings.Replace(good, `"processingScripts":[]`, `"processingScripts":[{"id":"22222222-2222-4222-8222-222222222222","name":"decode","description":null,"phase":"telemetry_up","language":"lua","entrypoint":"encodeInp","content":"return input","enabled":true,"lastSampleInput":null}]`, 1)
	good = strings.Replace(good, `"customFieldDefinitions":[]`, `"customFieldDefinitions":[{"key":"mode","name":"Mode","type":"enum","defaultValue":"safe","description":null,"enabled":true,"enumValues":[{"value":"safe","label":"Safe"}]}]`, 1)
	if _, err := DecodeConfiguration([]byte(good)); err != nil {
		t.Fatalf("valid configuration: %v", err)
	}
	bad := strings.Replace(good, `"defaultValue":"safe"`, `"defaultValue":1`, 1)
	if _, err := DecodeConfiguration([]byte(bad)); err == nil {
		t.Fatal("enum numeric default accepted")
	}
	bad = strings.Replace(good, `"dataKind":null`, `"dataKind":"TELEMETRY"`, 1)
	if _, err := DecodeConfiguration([]byte(bad)); err == nil {
		t.Fatal("unpaired topic model reference accepted")
	}
	bad = strings.Replace(good, `"phase":"telemetry_up"`, `"phase":"unknown"`, 1)
	if _, err := DecodeConfiguration([]byte(bad)); err == nil {
		t.Fatal("unknown script phase accepted")
	}
}

func TestDecodeConfigurationRejectsDuplicateIDsEnabledPhaseAndBounds(t *testing.T) {
	topic := `{"id":"11111111-1111-4111-8111-111111111111","name":"up","description":null,"direction":"up","sourceTopic":"a","targetTopic":"b","dataIdentifier":null,"priority":0,"enabled":true,"dataKind":null}`
	script := `{"id":"22222222-2222-4222-8222-222222222222","name":"s","description":null,"phase":"event_up","language":"lua","entrypoint":"encodeInp","content":"x","enabled":true,"lastSampleInput":null}`
	base := strings.Replace(validConfiguration, `"topicMappings":[]`, `"topicMappings":[`+topic+`]`, 1)
	base = strings.Replace(base, `"processingScripts":[]`, `"processingScripts":[`+script+`,`+strings.Replace(script, `22222222-2222-4222-8222-222222222222`, `33333333-3333-4333-8333-333333333333`, 1)+`]`, 1)
	if _, err := DecodeConfiguration([]byte(base)); err == nil {
		t.Fatal("two enabled scripts in one phase accepted")
	}
	duplicate := strings.Replace(base, `33333333-3333-4333-8333-333333333333`, `11111111-1111-4111-8111-111111111111`, 1)
	if _, err := DecodeConfiguration([]byte(duplicate)); err == nil {
		t.Fatal("duplicate config ID accepted")
	}
	oversized := strings.Replace(validConfiguration, `"topicMappings":[]`, `"topicMappings":[`+strings.TrimSuffix(strings.Repeat(topic+",", 1001), ",")+`]`, 1)
	if _, err := DecodeConfiguration([]byte(oversized)); err == nil {
		t.Fatal("topic limit exceeded without error")
	}
}

func TestDecodeConfigurationRequiresIntegerModelRevisionAndValidatesNullability(t *testing.T) {
	for _, revision := range []string{`0`, `1.5`, `9007199254740992`, `"1"`} {
		body := strings.Replace(validConfiguration, `"model":null`, `"model":{"modelId":"44444444-4444-4444-8444-444444444444","publishedRevision":`+revision+`}`, 1)
		if _, err := DecodeConfiguration([]byte(body)); err == nil {
			t.Errorf("revision %s accepted", revision)
		}
	}
	body := strings.Replace(validConfiguration, `"serviceInstanceId":null`, `"serviceInstanceId":"instance"`, 1)
	body = strings.Replace(body, `"mode":"direct"`, `"mode":"service_connector"`, 1)
	if _, err := DecodeConfiguration([]byte(body)); err != nil {
		t.Fatalf("service instance value rejected: %v", err)
	}
}

func TestNumberRawMessageIsValidJSON(t *testing.T) {
	var r json.RawMessage = json.RawMessage(`1.234567890123456789`)
	var n json.Number
	d := json.NewDecoder(strings.NewReader(string(r)))
	d.UseNumber()
	if err := d.Decode(&n); err != nil || n.String() != string(r) {
		t.Fatalf("number parse %q %v", n, err)
	}
}

func TestNumberDefaultRequiresNumericJSONTokenAndKeepsPrecision(t *testing.T) {
	const precise = `12345678901234567890.1234567890123456789`
	if err := validateDefault("number", json.RawMessage(precise)); err != nil {
		t.Fatalf("precise JSON number rejected: %v", err)
	}
	body := strings.Replace(validConfiguration, `"customFieldDefinitions":[]`, `"customFieldDefinitions":[{"key":"ratio","name":"Ratio","type":"number","defaultValue":`+precise+`,"description":null,"enabled":true,"enumValues":[]}]`, 1)
	configuration, err := DecodeConfiguration([]byte(body))
	if err != nil {
		t.Fatalf("precise number configuration rejected: %v", err)
	}
	if got := string(configuration.CustomFieldDefinitions[0].DefaultValue); got != precise {
		t.Fatalf("default precision changed: %s", got)
	}
	for _, raw := range []string{`"1"`, `true`, `{}`, `1 2`} {
		if err := validateDefault("number", json.RawMessage(raw)); err == nil {
			t.Errorf("number default %s accepted", raw)
		}
	}
}

func TestProviderAndServiceStringLimitsCountUnicodeCodePoints(t *testing.T) {
	if !rawNullableString(json.RawMessage(`"`+strings.Repeat("界", 128)+`"`), 128) {
		t.Fatal("128-codepoint providerVersion rejected")
	}
	if rawNullableString(json.RawMessage(`"`+strings.Repeat("界", 129)+`"`), 128) {
		t.Fatal("129-codepoint providerVersion accepted")
	}
	if !rawNullableString(json.RawMessage(`"`+strings.Repeat("界", 256)+`"`), 256) {
		t.Fatal("256-codepoint serviceInstanceId rejected")
	}
	if rawNullableString(json.RawMessage(`"`+strings.Repeat("界", 257)+`"`), 256) {
		t.Fatal("257-codepoint serviceInstanceId accepted")
	}
}

func TestValidateRejectsNilRequiredArrays(t *testing.T) {
	base, err := DecodeConfiguration([]byte(validConfiguration))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Configuration){
		"topics":      func(d *Configuration) { d.TopicMappings = nil },
		"scripts":     func(d *Configuration) { d.ProcessingScripts = nil },
		"definitions": func(d *Configuration) { d.CustomFieldDefinitions = nil },
		"enum values": func(d *Configuration) {
			d.CustomFieldDefinitions = []CustomFieldDefinition{{Key: "field", Name: "Field", Type: "string", DefaultValue: json.RawMessage(`null`), Description: json.RawMessage(`null`), EnumValues: nil}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			d := base
			mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("nil required array accepted")
			}
		})
	}
}
