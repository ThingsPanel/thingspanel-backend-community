package devicemetadata

import (
	"strings"
	"testing"

	"encore.dev/beta/errs"
)

func TestDecodeProcessingScriptTest(t *testing.T) {
	valid := `{"script":{"id":"s1","name":"n","description":null,"phase":"telemetry_up","language":"lua","entrypoint":"encodeInp","content":"function encodeInp(m,t) return m end","enabled":true,"lastSampleInput":null},"input":"{}","topic":"a/b","templateId":null}`
	got, err := decodeProcessingScriptTest([]byte(valid))
	if err != nil || got.Input != "{}" || got.Topic != "a/b" || !strings.Contains(got.Script.Content, "encodeInp") {
		t.Fatalf("valid request: %+v %v", got, err)
	}
	for name, body := range map[string]string{
		"unknown field":   strings.Replace(valid, `"topic"`, `"extra":1,"topic"`, 1),
		"wrong language":  strings.Replace(valid, `"lua"`, `"python"`, 1),
		"wrong entry":     strings.Replace(valid, `"encodeInp","content"`, `"main","content"`, 1),
		"empty content":   strings.Replace(valid, `"function encodeInp(m,t) return m end"`, `""`, 1),
		"missing input":   strings.Replace(valid, `"input":"{}",`, "", 1),
		"bad template id": strings.Replace(valid, `"templateId":null`, `"templateId":"nope"`, 1),
		"trailing data":   valid + `{}`,
	} {
		if _, err := decodeProcessingScriptTest([]byte(body)); err == nil {
			t.Errorf("%s must be rejected", name)
		} else if errs.Code(err) != errs.InvalidArgument {
			t.Errorf("%s: want InvalidArgument, got %v", name, err)
		}
	}
}

func TestProcessingScriptTestResultNeverClaimsDeviceEffect(t *testing.T) {
	req, err := decodeProcessingScriptTest([]byte(`{"script":{"id":"s1","name":"n","description":null,"phase":"command_down","language":"lua","entrypoint":"encodeInp","content":"function encodeInp(m,t) return m .. t end","enabled":true,"lastSampleInput":null},"input":"x","topic":"y","templateId":null}`))
	if err != nil {
		t.Fatal(err)
	}
	result := runProcessingScriptTest(t.Context(), req)
	if !result.Succeeded || result.Output == nil || *result.Output != "xy" || result.ErrorCode != nil || result.EngineVersion == "" {
		t.Fatalf("%+v", result)
	}
}
