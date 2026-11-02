package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeCommandValidationUsesPinnedSnapshotAndRejectsInvalidPayload(t *testing.T) {
	raw := json.RawMessage(`{"fields":[{"kind":"COMMAND","identifier":"set","nameI18n":{"default":"Set"},"access":{"read":false,"write":true,"observe":false},"parameters":{"schemaVersion":1,"items":[{"identifier":"value","nameI18n":{"default":"Value"},"valueType":{"kind":"NUMBER"}}]}}]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/thing-models/model/versions/7" || r.Header.Get("Authorization") != "Bearer session" {
			t.Errorf("unexpected snapshot request %s", r.URL.Path)
		}
		writeNativeCommandSnapshot(t, w, "model", 7, "snapshot", raw)
	}))
	defer server.Close()
	t.Setenv("TP_THINGMODEL_NATIVE_URL", server.URL)
	config := nativeCommandDeviceConfig("model", "snapshot", 7)
	for _, tc := range []struct {
		value string
		valid bool
	}{{`{"value":3}`, true}, {`{"value":"3"}`, false}, {`{"value":3,"extra":1}`, false}, {`{"value":1,"value":2}`, false}, {`{}`, false}} {
		err := validateNativeDeviceCommand(context.Background(), "session", config, "set", &tc.value)
		if (err == nil) != tc.valid {
			t.Fatalf("value %s: %v", tc.value, err)
		}
	}
	value := `{"value":3}`
	if validateNativeDeviceCommand(context.Background(), "", config, "set", &value) == nil {
		t.Fatal("missing session accepted")
	}
	wrong := "other"
	config.NativeThingModelSnapshotID = &wrong
	if validateNativeDeviceCommand(context.Background(), "session", config, "set", &value) == nil {
		t.Fatal("wrong pinned snapshot accepted")
	}
}
