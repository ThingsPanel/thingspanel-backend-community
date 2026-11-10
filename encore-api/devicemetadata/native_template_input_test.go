package devicemetadata

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"encore.app/devicemetadata/accessconfig"
)

const validTemplateConfiguration = `{"deviceKind":"standalone","model":null,"access":{"mode":"direct","protocol":"mqtt","providerId":null,"providerVersion":null,"schemaDigest":"0000000000000000000000000000000000000000000000000000000000000000","parameters":{"precise":9007199254740993123456789},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":30},"connectionDirection":"device_to_platform","serviceInstanceId":null},"topicMappings":[],"processingScripts":[],"customFieldDefinitions":[]}`

func TestParseNativeTemplateCreateDefaultsAndCanonicalPrecision(t *testing.T) {
	body := `{"name":"模板","configuration":` + validTemplateConfiguration + `}`
	got, err := ParseNativeTemplateCreate([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "模板" || got.Tags == nil || len(got.Tags) != 0 {
		t.Fatalf("unexpected name/tags: %#v %v", got.Name, got.Tags)
	}
	for _, field := range []string{`"description":null`, `"brand":null`, `"productName":null`, `"modelNumber":null`, `"imageAssetId":null`, `"tags":[]`} {
		if !bytes.Contains(got.CanonicalBody, []byte(field)) {
			t.Errorf("canonical body missing %s: %s", field, got.CanonicalBody)
		}
	}
	if !bytes.Contains(got.CanonicalBody, []byte("9007199254740993123456789")) {
		t.Fatalf("configuration number lost precision: %s", got.CanonicalBody)
	}
	if got.Description.Present != true || !got.Description.Null {
		t.Fatalf("omitted nullable default not explicit null: %#v", got.Description)
	}
	explicitNull, err := ParseNativeTemplateCreate([]byte(`{"name":"模板","configuration":` + validTemplateConfiguration + `,"imageAssetId":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.CanonicalBody, explicitNull.CanonicalBody) {
		t.Fatalf("omitted and explicit-null imageAssetId must canonicalize alike: %s / %s", got.CanonicalBody, explicitNull.CanonicalBody)
	}
}

func TestParseNativeTemplateCreateValidation(t *testing.T) {
	good := `{"name":"x","configuration":` + validTemplateConfiguration + `,"tags":["a","中"]}`
	if _, err := ParseNativeTemplateCreate([]byte(good)); err != nil {
		t.Fatalf("valid unicode input rejected: %v", err)
	}
	cases := []string{
		`{"configuration":` + validTemplateConfiguration + `}`,
		`{"name":"","configuration":` + validTemplateConfiguration + `}`,
		`{"name":"x","configuration":null}`,
		`{"name":"x","configuration":` + validTemplateConfiguration + `,"tags":null}`,
		`{"name":"x","configuration":` + validTemplateConfiguration + `,"tags":["a","a"]}`,
		`{"name":"x","configuration":` + validTemplateConfiguration + `,"tags":[""]}`,
		`{"name":"x","configuration":` + validTemplateConfiguration + `,"imageAssetId":"00000000-0000-0000-0000-000000000000"}`,
		`{"name":"x","configuration":` + validTemplateConfiguration + `,"tenantId":"x"}`,
		`{"name":"x","name":"y","configuration":` + validTemplateConfiguration + `}`,
		`{"name":"x","configuration":` + strings.Replace(validTemplateConfiguration, `"topicMappings":[]`, `"topicMappings":null`, 1) + `}`,
		`{"name":"x","configuration":` + strings.Replace(validTemplateConfiguration, `"mode":"direct"`, `"mode":"direct","mode":"direct"`, 1) + `}`,
		`{"name":"x","configuration":` + strings.Replace(validTemplateConfiguration, `"precise":9007199254740993123456789`, `"nested":{"x":1,"x":2}`, 1) + `}`,
		`{"name":"x","configuration":` + strings.Replace(validTemplateConfiguration, `"processingScripts":[]`, `"processingScripts":[{"id":"00000000-0000-4000-8000-000000000001","name":"script","description":null,"phase":"decode","language":"lua","entrypoint":"main","content":"","enabled":false,"lastSampleInput":{"x":1,"x":2}}]`, 1) + `}`,
	}
	for i, body := range cases {
		if _, err := ParseNativeTemplateCreate([]byte(body)); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	tooLong := strings.Repeat("界", 129)
	if _, err := ParseNativeTemplateCreate([]byte(`{"name":"` + tooLong + `","configuration":` + validTemplateConfiguration + `}`)); err == nil {
		t.Fatal("rune-length overflow accepted")
	}
	tooLongTag := strings.Repeat("界", 65)
	if _, err := ParseNativeTemplateCreate([]byte(`{"name":"x","configuration":` + validTemplateConfiguration + `,"tags":["` + tooLongTag + `"]}`)); err == nil {
		t.Fatal("tag rune-length overflow accepted")
	}
	tooMany := make([]string, 33)
	for i := range tooMany {
		tooMany[i] = string(rune('a' + i))
	}
	raw, _ := json.Marshal(tooMany)
	if _, err := ParseNativeTemplateCreate([]byte(`{"name":"x","configuration":` + validTemplateConfiguration + `,"tags":` + string(raw) + `}`)); err == nil {
		t.Fatal("too many tags accepted")
	}
}

func TestParseNativeTemplateCreatePreservesDistinctNumericInputs(t *testing.T) {
	for _, number := range []string{"9007199254740993123456789", "1.2345678901234567890123456789e+42"} {
		configuration := strings.Replace(validTemplateConfiguration, "9007199254740993123456789", number, 1)
		got, err := ParseNativeTemplateCreate([]byte(`{"name":"x","configuration":` + configuration + `}`))
		if err != nil {
			t.Fatalf("number %s rejected: %v", number, err)
		}
		if !bytes.Contains(got.CanonicalBody, []byte(number)) {
			t.Errorf("number precision/lexeme not retained: want %s in %s", number, got.CanonicalBody)
		}
	}
}

func TestParseNativeTemplateConfigurationDuplicateIsStructuralInputError(t *testing.T) {
	configuration := strings.Replace(validTemplateConfiguration, `"mode":"direct"`, `"mode":"direct","mode":"direct"`, 1)
	_, err := ParseNativeTemplateCreate([]byte(`{"name":"x","configuration":` + configuration + `}`))
	var inputErr *NativeDeviceInputError
	if !errors.As(err, &inputErr) || !inputErr.Structural {
		t.Fatalf("duplicate nested key should be a structural input error, got %v", err)
	}
}

func TestParseNativeTemplatePatchPresenceNullAndWholeConfiguration(t *testing.T) {
	got, err := ParseNativeTemplatePatch([]byte(`{"description":null,"brand":"","tags":[],"administrativeState":"disabled","configuration":` + validTemplateConfiguration + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Description.Present || !got.Description.Null || !got.Brand.Present || got.Brand.Null || got.Brand.Value != "" {
		t.Fatalf("presence/null distinction lost: %#v %#v", got.Description, got.Brand)
	}
	if !got.Tags.Present || got.Tags.Value == nil || len(got.Tags.Value) != 0 || !got.Configuration.Present || !got.ConfigurationJSON.Present {
		t.Fatalf("replacement fields missing: %#v", got)
	}
	if got.AdministrativeState.Value != "disabled" {
		t.Fatalf("state: %#v", got.AdministrativeState)
	}
	if !bytes.Contains(got.CanonicalBody, []byte(`"description":null`)) || !bytes.Contains(got.CanonicalBody, []byte(`"tags":[]`)) {
		t.Fatalf("canonical presence mismatch: %s", got.CanonicalBody)
	}
	noop, err := ParseNativeTemplatePatch([]byte(`{}`))
	if err != nil || len(noop.CanonicalBody) != 2 || string(noop.CanonicalBody) != "{}" {
		t.Fatalf("empty patch should be no-op: %s %v", noop.CanonicalBody, err)
	}
}

func TestParseNativeTemplatePatchRejectsInvalidFieldsAndJSON(t *testing.T) {
	cases := []string{
		`{"name":null}`, `{"name":""}`, `{"description":1}`, `{"tags":null}`, `{"tags":["a","a"]}`,
		`{"configuration":null}`, `{"administrativeState":"archived"}`, `{"imageAssetId":"not-uuid"}`,
		`{"imageUrl":"https://example.invalid"}`, `{"resourceVersion":1}`, `{"actor":"x"}`,
		`{"name":"a","name":"b"}`, `{} {}`, `[]`,
	}
	for i, body := range cases {
		if _, err := ParseNativeTemplatePatch([]byte(body)); err == nil {
			t.Errorf("case %d accepted: %s", i, body)
		}
	}
	if _, err := ParseNativeTemplatePatch([]byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 0xff, '"', '}'}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	var inputErr *NativeDeviceInputError
	_, err := ParseNativeTemplatePatch([]byte(`{"tenantId":"secret-value"}`))
	if !errors.As(err, &inputErr) || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("unsafe/incorrect error: %v", err)
	}
}

func TestParseNativeTemplateCustomFieldChangesContractAndPrecision(t *testing.T) {
	body := []byte(`{"description":"note","customFieldChanges":{"upsert":[{"key":"existing","defaultValue":1.2345678901234567890123456789},{"key":"new","name":"New","type":"number","defaultValue":0,"description":null,"enabled":true,"enumValues":[]}],"remove":["gone"]}}`)
	got, err := ParseNativeTemplatePatch(body)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CustomFieldChanges.Present || len(got.CustomFieldChanges.Value.Upsert) != 2 || len(got.CustomFieldChanges.Value.Remove) != 1 {
		t.Fatalf("custom field patch lost: %#v", got.CustomFieldChanges)
	}
	if !bytes.Contains(got.CanonicalBody, []byte("1.2345678901234567890123456789")) {
		t.Fatalf("custom-field patch number lost precision: %s", got.CanonicalBody)
	}
	for _, invalidBody := range []string{
		`{"customFieldChanges":{"upsert":[],"remove":[]}}`,
		`{"customFieldChanges":{"upsert":[{"key":"x"}]}}`,
		`{"configuration":` + validTemplateConfiguration + `,"customFieldChanges":{"upsert":[],"remove":["x"]}}`,
		`{"customFieldChanges":{"upsert":[{"key":"x"},{"key":"x"}],"remove":[]}}`,
		`{"customFieldChanges":{"upsert":[{"key":"x"}],"remove":["x"]}}`,
		`{"customFieldChanges":{"upsert":[{"key":"x","enabled":null}],"remove":[]}}`,
		`{"customFieldChanges":{"upsert":[{"key":"x","type":"bad"}],"remove":[]}}`,
		`{"customFieldChanges":{"upsert":[{"key":"x","defaultValue":{}}],"remove":[]}}`,
		`{"customFieldChanges":{"upsert":[{"key":"x","extra":true}],"remove":[]}}`,
	} {
		if _, err := ParseNativeTemplatePatch([]byte(invalidBody)); err == nil {
			t.Errorf("invalid custom field patch accepted: %s", invalidBody)
		}
	}
}

func TestMergeNativeCustomFieldChangesPreservesUnchangedJSON(t *testing.T) {
	configuration := json.RawMessage(`{"deviceKind":"standalone","model":null,"access":{"mode":"direct","protocol":"mqtt","providerId":null,"parameters":{"precise":9007199254740993123456789},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":60},"connectionDirection":"device_to_platform","serviceInstanceId":null},"topicMappings":[],"processingScripts":[],"customFieldDefinitions":[{"key":"old","name":"Old","type":"number","defaultValue":1.2345678901234567890123456789,"description":null,"enabled":true,"enumValues":[]}]}`)
	changes, err := decodeNativeCustomFieldChanges([]byte(`{"upsert":[{"key":"old","name":"Renamed"},{"key":"next","name":"Next","type":"string","defaultValue":"","description":null,"enabled":true,"enumValues":[]}],"remove":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	merged, oldDefs, nextDefs, err := mergeNativeCustomFieldChanges(configuration, changes)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldDefs) != 1 || len(nextDefs) != 2 || nextDefs[0].Name != "Renamed" || nextDefs[0].DefaultValue == nil {
		t.Fatalf("unexpected merged definitions: old=%+v next=%+v", oldDefs, nextDefs)
	}
	for _, number := range []string{"9007199254740993123456789", "1.2345678901234567890123456789"} {
		if !bytes.Contains(merged, []byte(number)) {
			t.Errorf("merge changed untouched number %s: %s", number, merged)
		}
	}
	if !bytes.Contains(merged, []byte(`"name":"Renamed"`)) {
		t.Fatal("upsert did not apply the explicit name change")
	}
}

func TestMergeNativeCustomFieldChangesUsesStrictDefinitionSchema(t *testing.T) {
	for _, patch := range []string{
		`{"upsert":[{"key":"new","name":"New","type":"string","defaultValue":"","description":null,"enumValues":[]}],"remove":[]}`,
		`{"upsert":[{"key":"new","name":"New","type":"enum","defaultValue":"safe","description":null,"enabled":true,"enumValues":[{"value":"safe","label":"Safe","extra":true}]}],"remove":[]}`,
	} {
		changes, err := decodeNativeCustomFieldChanges([]byte(patch))
		if err != nil {
			t.Fatalf("valid patch envelope rejected before merged schema validation: %v", err)
		}
		merged, _, _, err := mergeNativeCustomFieldChanges(json.RawMessage(validTemplateConfiguration), changes)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := accessconfig.DecodeConfiguration(merged); err == nil {
			t.Errorf("strict full-configuration shape accepted invalid upsert: %s", patch)
		}
	}
}
