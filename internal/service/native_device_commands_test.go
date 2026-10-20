package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/model"
)

func TestNativeDeviceCommandsReadsPinnedSnapshotAndMapsLegacyUIContract(t *testing.T) {
	nativeModel := json.RawMessage(`{
		"name":"QA model",
		"fields":[
			{"kind":"ATTRIBUTE","identifier":"status","nameI18n":{"default":"Status"}},
			{"kind":"COMMAND","identifier":"setMode","nameI18n":{"default":"Set mode"},"descriptionI18n":{"default":"Change mode"},"parameters":{"schemaVersion":1,"items":[
				{"identifier":"mode","nameI18n":{"default":"Mode"},"descriptionI18n":{"default":"Choose mode"},"valueType":{"kind":"ENUM","baseKind":"STRING","enumValues":[{"code":"auto","label":{"default":"Automatic"}},{"code":"manual","label":{"default":"Manual"}}]}},
				{"identifier":"enabled","nameI18n":{"default":"Enabled"},"valueType":{"kind":"BOOL"}},
				{"identifier":"level","nameI18n":{"default":"Level"},"valueType":{"kind":"FLOAT"}},
				{"identifier":"numericMode","nameI18n":{"default":"Numeric mode"},"valueType":{"kind":"ENUM","baseKind":"NUMBER","enumValues":[{"code":2,"label":{"default":"Low"}},{"code":2.5,"label":{"default":"High"}}]}},
				{"identifier":"booleanMode","nameI18n":{"default":"Boolean mode"},"valueType":{"kind":"ENUM","baseKind":"BOOLEAN","enumValues":[{"code":true,"label":{"default":"On"}},{"code":false,"label":{"default":"Off"}}]}},
				{"identifier":"note","nameI18n":{"default":"Note"},"valueType":{"kind":"STRING"}}
			]}},
			{"kind":"EVENT","identifier":"reported","nameI18n":{"default":"Reported"}}
		]
	}`)
	const modelID, snapshotID, token = "native-model-1", "snapshot-7", "qa-session-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/thing-models/native-model-1/versions/7" {
			t.Errorf("requested non-pinned snapshot path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("authorization = %q, want normalized Community bearer", got)
		}
		writeNativeCommandSnapshot(t, w, modelID, 7, snapshotID, nativeModel)
	}))
	defer server.Close()
	t.Setenv("TP_THINGMODEL_NATIVE_URL", server.URL)

	config := nativeCommandDeviceConfig(modelID, snapshotID, 7)
	commands, err := nativeDeviceCommands(context.Background(), token, config)
	if err != nil {
		t.Fatalf("nativeDeviceCommands() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("command count = %d, want only the COMMAND field", len(commands))
	}
	command := commands[0]
	if command.Name != "Set mode" || command.Identifier != "setMode" || command.Description != "Change mode" {
		t.Fatalf("command metadata = %#v", command)
	}
	var params []legacyCommandParameter
	if err := json.Unmarshal([]byte(command.Params), &params); err != nil {
		t.Fatalf("decode params string: %v", err)
	}
	if len(params) != 6 {
		t.Fatalf("parameter count = %d, want 6", len(params))
	}
	wantIdentifiers := []string{"mode", "enabled", "level", "numericMode", "booleanMode", "note"}
	for index, want := range wantIdentifiers {
		if params[index].DataIdentifier != want {
			t.Errorf("parameter[%d] identifier = %q, want preserved order value %q", index, params[index].DataIdentifier, want)
		}
	}
	wantTypes := [][2]string{{"Enum", "STRING"}, {"Boolean", "Boolean"}, {"Number", "Number"}, {"Enum", "NUMBER"}, {"Enum", "BOOLEAN"}, {"string", "String"}}
	for index, want := range wantTypes {
		if params[index].ParamType != want[0] || params[index].DataType != want[1] {
			t.Errorf("parameter[%d] types = (%q,%q), want (%q,%q)", index, params[index].ParamType, params[index].DataType, want[0], want[1])
		}
		if params[index].ID != params[index].DataIdentifier {
			t.Errorf("parameter[%d] id and data_identifier differ: %#v", index, params[index])
		}
	}
	if params[0].ID != "mode" || params[0].DataName != "Mode" || params[0].Description != "Choose mode" ||
		len(params[0].EnumConfig) != 2 || string(params[0].EnumConfig[1].Value) != `"manual"` || params[0].EnumConfig[1].Desc != "Manual" {
		t.Fatalf("enum parameter was not preserved: %#v", params[0])
	}
	if params[1].EnumConfig == nil || len(params[1].EnumConfig) != 0 {
		t.Fatalf("non-enum parameter enum_config = %#v, want empty array", params[1].EnumConfig)
	}
	if string(params[3].EnumConfig[1].Value) != "2.5" || string(params[4].EnumConfig[0].Value) != "true" {
		t.Fatalf("numeric or boolean enum code changed: numeric=%s boolean=%s", params[3].EnumConfig[1].Value, params[4].EnumConfig[0].Value)
	}
}

func TestHistoricalBindingRevisionDoesNotSelectNativeReadAfterUnbind(t *testing.T) {
	config := &model.DeviceConfig{NativeBindingRevision: 4}
	if hasNativeDeviceConfigBinding(config) {
		t.Fatal("historical binding revision alone must not select native snapshot read after unbind")
	}
}

func TestNativeDeviceCommandsFailsClosedOnMissingOrMismatchedSnapshot(t *testing.T) {
	modelRaw := json.RawMessage(`{"fields":[]}`)
	tests := []struct {
		name       string
		status     int
		modelID    string
		revision   int
		snapshotID string
		wantErr    string
	}{
		{name: "native authorization denied", status: http.StatusForbidden, modelID: "native-model-1", revision: 7, snapshotID: "snapshot-7", wantErr: "status 403"},
		{name: "snapshot identity mismatch", status: http.StatusOK, modelID: "native-model-1", revision: 7, snapshotID: "snapshot-other", wantErr: "does not match the device-config binding"},
		{name: "model identity mismatch", status: http.StatusOK, modelID: "other-model", revision: 7, snapshotID: "snapshot-7", wantErr: "identity does not match"},
		{name: "revision mismatch", status: http.StatusOK, modelID: "native-model-1", revision: 8, snapshotID: "snapshot-7", wantErr: "identity does not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status != http.StatusOK {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(`{"message":"denied"}`))
					return
				}
				writeNativeCommandSnapshot(t, w, tt.modelID, tt.revision, tt.snapshotID, modelRaw)
			}))
			defer server.Close()
			t.Setenv("TP_THINGMODEL_NATIVE_URL", server.URL)

			_, err := nativeDeviceCommands(context.Background(), "qa-session-token", nativeCommandDeviceConfig("native-model-1", "snapshot-7", 7))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestNativeDeviceCommandsRejectsIncompleteBindingBeforeHTTP(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("TP_THINGMODEL_NATIVE_URL", server.URL)

	tests := []struct {
		name   string
		config *model.DeviceConfig
	}{
		{name: "partial native reference", config: nativeCommandDeviceConfig("native-model-1", "", 7)},
		{name: "legacy native mixed binding", config: func() *model.DeviceConfig {
			c := nativeCommandDeviceConfig("native-model-1", "snapshot-7", 7)
			legacy := "legacy-template"
			c.DeviceTemplateID = &legacy
			return c
		}()},
		{name: "no authenticated community token", config: nativeCommandDeviceConfig("native-model-1", "snapshot-7", 7)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := "qa-session-token"
			if tt.name == "no authenticated community token" {
				token = ""
			}
			if _, err := nativeDeviceCommands(context.Background(), token, tt.config); err == nil {
				t.Fatal("expected native binding/token error")
			}
		})
	}
	if requests != 0 {
		t.Fatalf("native service requests = %d, want none for invalid bindings or missing token", requests)
	}
}

func TestNativeCommandListRejectsUnsupportedAndUnsafeEnumValues(t *testing.T) {
	tests := []struct {
		name      string
		valueType string
		wantError string
	}{
		{name: "unsupported parameter kind", valueType: `{"kind":"OBJECT"}`, wantError: "fields[0].parameters.items[0].valueType.kind"},
		{name: "null string enum code", valueType: `{"kind":"ENUM","baseKind":"STRING","enumValues":[{"code":null,"label":{"default":"invalid"}}]}`, wantError: "enum code cannot be null"},
		{name: "null boolean enum code", valueType: `{"kind":"ENUM","baseKind":"BOOLEAN","enumValues":[{"code":null,"label":{"default":"invalid"}}]}`, wantError: "enum code cannot be null"},
		{name: "unsafe integer enum", valueType: `{"kind":"ENUM","baseKind":"NUMBER","enumValues":[{"code":9007199254740993,"label":{"default":"too large"}}]}`, wantError: "safe integer range"},
		{name: "number changes in JavaScript", valueType: `{"kind":"ENUM","baseKind":"NUMBER","enumValues":[{"code":1.0000000000000001,"label":{"default":"rounded"}}]}`, wantError: "change in the JavaScript UI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(fmt.Sprintf(`{"fields":[{"kind":"COMMAND","identifier":"set","nameI18n":{"default":"Set"},"parameters":{"schemaVersion":1,"items":[{"identifier":"value","nameI18n":{"default":"Value"},"valueType":%s}]}}]}`, tt.valueType))
			_, err := nativeCommandListFromModel(raw)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func nativeCommandDeviceConfig(modelID, snapshotID string, revision int) *model.DeviceConfig {
	config := &model.DeviceConfig{NativeBindingRevision: 1}
	if modelID != "" {
		config.NativeThingModelID = &modelID
	}
	if snapshotID != "" {
		config.NativeThingModelSnapshotID = &snapshotID
	}
	if revision != 0 {
		config.NativeThingModelRevision = &revision
	}
	return config
}

func writeNativeCommandSnapshot(t *testing.T, w http.ResponseWriter, modelID string, revision int, snapshotID string, nativeModel json.RawMessage) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	response := fmt.Sprintf(`{"modelID":%q,"publishedRevision":%d,"snapshotID":%q,"checksum":"fixture-checksum","model":%s,"resources":{"modelID":%q,"revision":1,"profile":{},"webChartConfig":null,"appChartConfig":null}}`,
		modelID, revision, snapshotID, nativeModel, modelID)
	_, _ = w.Write([]byte(response))
}
