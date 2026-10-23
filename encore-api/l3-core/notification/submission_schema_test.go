package notification

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPluginSchemaLocalReferences(t *testing.T) {
	schema, err := CompileNotificationSchema(json.RawMessage(`{"type":"object","properties":{"nullable":{"$ref":"#/$defs/value"}},"$defs":{"value":{"type":["string","null"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateNotificationSchema(schema, json.RawMessage(`{"nullable":null}`)); err != nil {
		t.Fatal(err)
	}
}

func TestPluginSchemaConstraintsAndSafeFailures(t *testing.T) {
	schema, err := CompileNotificationSchema(json.RawMessage(`{"type":"object","additionalProperties":false,"required":["credential","options"],"properties":{"credential":{"type":"string","minLength":8},"options":{"type":"object","required":["region"],"properties":{"region":{"enum":["fixture"]},"nullable":{"type":["string","null"]}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateNotificationSchema(schema, json.RawMessage(`{"credential":"fixture-only-value","options":{"region":"fixture","nullable":null}}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"credential":"SECRET-SENTINEL","options":{"region":"bad"}}`,
		`{"credential":"SECRET-SENTINEL","options":{}}`,
		`{"credential":"short","options":{"region":"fixture"}}`,
		`{"credential":"SECRET-SENTINEL","options":{"region":"fixture"},"extra":true}`,
		`{"credential":"first","credential":"SECRET-SENTINEL","options":{"region":"fixture"}}`,
		`{"credential":"SECRET-SENTINEL","options":{"region":"fixture"}} {}`,
	} {
		err := ValidateNotificationSchema(schema, json.RawMessage(raw))
		if err == nil || strings.Contains(err.Error(), "SENTINEL") {
			t.Fatalf("unsafe validation result: %v", err)
		}
	}
}

func TestPluginSchemaCannotLoadNetworkOrFiles(t *testing.T) {
	for _, ref := range []string{"https://127.0.0.1:1/secret", "file:///etc/passwd"} {
		raw, _ := json.Marshal(map[string]string{"$ref": ref})
		if _, err := CompileNotificationSchema(raw); err == nil {
			t.Fatal("external schema reference accepted")
		}
	}
	if _, err := CompileNotificationSchema(json.RawMessage(`{"type":"object","properties":{"secret":{"type":"unknown-SENTINEL"}}}`)); err == nil || strings.Contains(err.Error(), "SENTINEL") {
		t.Fatal("schema error exposed literals")
	}
	if _, err := CompileNotificationSchema(json.RawMessage(`{"type":"object","type":"string"}`)); err == nil {
		t.Fatal("duplicate schema key accepted")
	}
}
