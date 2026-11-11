package example

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"plugin.local/example-notification/sdk"
)

func TestSendIsLocalOnlyAndNeverAccepts(t *testing.T) {
	provider := Provider{}
	result, err := provider.Send(context.Background(), sdk.SendRequest{DeliveryID: "fixture-delivery"})
	if err != nil || result.Outcome != "rejected" || result.ReceiptExpected || result.Error == nil || result.Error.Code != "example_no_external_send" {
		t.Fatalf("unexpected local example result: result=%+v err=%v", result, err)
	}
}

func TestManifestMatchesCheckedInExample(t *testing.T) {
	want, err := os.ReadFile("../manifest.example.json")
	if err != nil {
		t.Fatal("manifest example is missing")
	}
	var wantValue any
	if json.Unmarshal(want, &wantValue) != nil {
		t.Fatal("manifest example is invalid JSON")
	}
	got, err := json.Marshal(Provider{}.Manifest())
	if err != nil {
		t.Fatal("provider manifest could not be encoded")
	}
	var gotValue any
	if json.Unmarshal(got, &gotValue) != nil {
		t.Fatal("provider manifest is invalid JSON")
	}
	want, _ = json.Marshal(wantValue)
	got, _ = json.Marshal(gotValue)
	if !bytes.Equal(got, want) {
		t.Fatalf("manifest example drifted: got %s want %s", got, want)
	}
}

func TestValidateRequiresOnlyTheDeclaredLabel(t *testing.T) {
	provider := Provider{}
	valid := provider.Validate(context.Background(), sdk.ValidateRequest{Config: map[string]any{"label": "fixture"}})
	if !valid.Valid || len(valid.Errors) != 0 {
		t.Fatalf("valid config rejected: %+v", valid)
	}
	invalid := provider.Validate(context.Background(), sdk.ValidateRequest{Config: map[string]any{"label": " "}})
	if invalid.Valid || len(invalid.Errors) != 1 || invalid.Errors[0].Field != "label" {
		t.Fatalf("invalid config accepted: %+v", invalid)
	}
}
