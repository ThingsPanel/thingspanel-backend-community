package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestTypedNativeFieldsPersistThroughCRUDAndRollback(t *testing.T) {
	ctx := context.Background()
	tenant := testTenantID()
	actor := testActorID()
	model, err := createThingModel(ctx, tenant, &ThingModel{Name: "typed native fields", Status: "DRAFT"})
	if err != nil {
		t.Fatal(err)
	}

	telemetry, err := createThingModelItem(ctx, tenant, model.ID, &ThingModelItem{
		Type: "TELEMETRY", Identifier: "mode", NameI18n: json.RawMessage(`{"default":"Mode"}`),
		ValueType: json.RawMessage(`{"kind":"ENUM","enum_base_kind":"STRING","enum_values":[{"value":"auto","label":{"default":"Automatic"}}],"constraint":{"unit":"state"}}`),
		Access:    json.RawMessage(`{"read":true,"observe":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	command, err := createThingModelItem(ctx, tenant, model.ID, &ThingModelItem{
		Type: "COMMAND", Identifier: "set_mode", NameI18n: json.RawMessage(`{"default":"Set mode"}`),
		Parameters: json.RawMessage(`{"schema_version":1,"items":[{"identifier":"mode","name":{"default":"Mode"},"value_type":{"kind":"STRING"}}]}`),
		Access:     json.RawMessage(`{"write":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := createThingModelItem(ctx, tenant, model.ID, &ThingModelItem{
		Type: "EVENT", Identifier: "mode_changed", NameI18n: json.RawMessage(`{"default":"Mode changed"}`),
		Parameters: json.RawMessage(`{"schema_version":1,"items":[{"identifier":"old_mode","name":{"default":"Old mode"},"value_type":{"kind":"STRING"}}]}`),
		Access:     json.RawMessage(`{"observe":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := getThingModelItem(ctx, tenant, model.ID, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(got.Parameters, command.Parameters) {
		t.Fatalf("get lost command parameters: %s", got.Parameters)
	}
	listed, _, err := listThingModelItems(ctx, tenant, model.ID, "EVENT", "", pageRequest{Page: 1, PageSize: 20})
	if err != nil || len(listed) != 1 || !bytes.Equal(listed[0].Parameters, event.Parameters) {
		t.Fatalf("list lost event parameters: items=%+v err=%v", listed, err)
	}
	if got, err := getThingModelItem(ctx, tenant, model.ID, telemetry.ID); err != nil || !jsonEqual(got.ValueType, telemetry.ValueType) || !bytes.Contains(got.ValueType, []byte(`"unit"`)) {
		t.Fatalf("get lost typed enum: item=%+v err=%v", got, err)
	}

	first, err := publishThingModel(ctx, tenant, model.ID, actor, &publishReq{Changelog: "typed fields"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first.Content, []byte(`"parameters"`)) || !bytes.Contains(first.Content, []byte(`"enum_base_kind"`)) {
		t.Fatalf("snapshot omitted typed fields: %s", first.Content)
	}
	command.Parameters = json.RawMessage(`{"schema_version":1,"items":[{"identifier":"mode","name":{"default":"Mode"},"value_type":{"kind":"STRING"}},{"identifier":"fade","name":{"default":"Fade"},"value_type":{"kind":"BOOL"}}]}`)
	if _, err := updateThingModelItem(ctx, tenant, model.ID, command.ID, command); err != nil {
		t.Fatal(err)
	}
	if _, err := publishThingModel(ctx, tenant, model.ID, actor, &publishReq{Changelog: "add fade parameter"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rollbackThingModel(ctx, tenant, model.ID, actor, &rollbackReq{TargetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	restoredItems, _, err := listThingModelItems(ctx, tenant, model.ID, "COMMAND", "", pageRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(restoredItems) != 1 || !jsonEqual(restoredItems[0].Parameters, json.RawMessage(`{"schema_version":1,"items":[{"identifier":"mode","name":{"default":"Mode"},"value_type":{"kind":"STRING"}}]}`)) {
		t.Fatalf("rollback did not restore typed parameters: %+v", restoredItems)
	}
}

func jsonEqual(a, b json.RawMessage) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func TestLegacyPropertyEnumAndCommandSnapshotRemainReadable(t *testing.T) {
	legacyProperty := ThingModelItem{
		Type: "PROPERTY", Identifier: "legacy_state", NameI18n: json.RawMessage(`{"default":"State"}`),
		ValueType: json.RawMessage(`{"kind":"ENUM","enum_values":[{"code":"ON","label":{"default":"On"}}]}`),
		Access:    json.RawMessage(`{"read":true}`),
	}
	if issues := validateThingModelItemPayload(&legacyProperty); len(issues) != 0 {
		t.Fatalf("legacy PROPERTY enum should remain valid: %+v", issues)
	}
	legacyCommand := ThingModelItem{
		Type: "COMMAND", Identifier: "legacy_command", NameI18n: json.RawMessage(`{"default":"Legacy command"}`),
		ValueType: json.RawMessage(`{"kind":"BOOL"}`), Access: json.RawMessage(`{"write":true}`),
	}
	if err := validateSnapshotItems([]ThingModelItem{legacyCommand}); err != nil {
		t.Fatalf("legacy scalar command snapshot should remain readable: %v", err)
	}
	if issues := validateThingModelItemPayload(&legacyCommand); len(issues) == 0 {
		t.Fatal("new command writes must require typed parameters")
	}
	invalidTelemetry := ThingModelItem{
		Type: "TELEMETRY", Identifier: "bad_telemetry", NameI18n: json.RawMessage(`{"default":"Bad"}`),
		ValueType: json.RawMessage(`{"kind":"BOOL"}`), Parameters: json.RawMessage(`{"schema_version":1,"items":[]}`),
		Access: json.RawMessage(`{"read":true}`),
	}
	if issues := validateThingModelItemPayload(&invalidTelemetry); len(issues) == 0 {
		t.Fatal("telemetry must reject parameters")
	}
	invalidCommand := ThingModelItem{
		Type: "COMMAND", Identifier: "bad_command", NameI18n: json.RawMessage(`{"default":"Bad"}`),
		ValueType: json.RawMessage(`{"kind":"OBJECT"}`), Parameters: json.RawMessage(`{"schema_version":1,"items":[]}`),
		Access: json.RawMessage(`{"write":true}`),
	}
	if issues := validateThingModelItemPayload(&invalidCommand); len(issues) == 0 {
		t.Fatal("COMMAND must reject parent value_type instead of using OBJECT")
	}
}

func TestRollbackLegacySnapshotRestoresCommandWithoutInventingParameters(t *testing.T) {
	ctx := context.Background()
	tenant := testTenantID()
	model, err := createThingModel(ctx, tenant, &ThingModel{Name: "legacy snapshot compatibility", Status: "DRAFT"})
	if err != nil {
		t.Fatal(err)
	}
	content := json.RawMessage(`{"thing_model_id":"` + model.ID + `","name":"legacy snapshot compatibility","version":1,"items":[{"type":"COMMAND","identifier":"legacy_toggle","name_i18n":{"default":"Toggle"},"value_type":{"kind":"BOOL"},"access":{"write":true},"meta_items":[],"sort_order":100}]}`)
	if _, err := db.Exec(ctx, `INSERT INTO thing_model_snapshots (id, thing_model_id, tenant_id, version, content, changelog, published_by) VALUES ($1, $2, $3, 1, $4, 'legacy fixture', $5)`, newID(), model.ID, tenant, content, testActorID()); err != nil {
		t.Fatal(err)
	}
	if _, err := rollbackThingModel(ctx, tenant, model.ID, testActorID(), &rollbackReq{TargetVersion: 1}); err != nil {
		t.Fatal(err)
	}
	items, _, err := listThingModelItems(ctx, tenant, model.ID, "COMMAND", "", pageRequest{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].Parameters) != 0 || !jsonEqual(items[0].ValueType, json.RawMessage(`{"kind":"BOOL"}`)) {
		t.Fatalf("legacy scalar command snapshot was not restored as stored: %+v", items)
	}
}
