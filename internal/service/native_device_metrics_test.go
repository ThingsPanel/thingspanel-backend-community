package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"project/internal/model"
	"project/pkg/utils"
)

func TestNativeMetricMenusIncludeUnreportedFieldsAndRestrictKinds(t *testing.T) {
	raw := json.RawMessage(`{"fields":[
 {"kind":"TELEMETRY","identifier":"temperature","nameI18n":{"default":"温度"},"unit":"℃","valueType":{"kind":"FLOAT"}},
 {"kind":"ATTRIBUTE","identifier":"mode","nameI18n":{"default":"模式"},"valueType":{"kind":"ENUM","baseKind":"STRING","enumValues":[{"code":"1","label":{"default":"自动"}}]}},
 {"kind":"EVENT","identifier":"alarm","nameI18n":{"default":"告警"},"parameters":{"schemaVersion":1,"items":[{"identifier":"level","nameI18n":{"default":"级别"},"valueType":{"kind":"ENUM","baseKind":"NUMBER","enumValues":[{"code":1,"label":{"default":"严重"}}]}}]}},
 {"kind":"COMMAND","identifier":"restart","nameI18n":{"default":"重启"},"parameters":{"schemaVersion":1,"items":[]}}
 ]}`)
	all, err := nativeMetricGroups(raw, "TELEMETRY", "ATTRIBUTE", "EVENT", "COMMAND")
	if err != nil || len(all) != 4 {
		t.Fatalf("all = %#v, %v", all, err)
	}
	if all[0].Options[0].Label != "温度" || all[0].Options[0].Unit != "℃" || all[0].Options[0].DataType != "Number" {
		t.Fatalf("telemetry metadata = %#v", all[0])
	}
	if all[1].DataSourceType != "attributes" || string(all[1].Options[0].Enum[0].Value) != `"1"` {
		t.Fatalf("attribute enum = %#v", all[1])
	}
	var params []legacyCommandParameter
	if err := json.Unmarshal([]byte(all[2].Options[0].Params), &params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 || params[0].DataName != "级别" || string(params[0].EnumConfig[0].Value) != "1" {
		t.Fatalf("event params = %#v", params)
	}
	conditions, err := nativeMetricGroups(raw, "TELEMETRY", "ATTRIBUTE", "EVENT")
	if err != nil || len(conditions) != 3 {
		t.Fatalf("conditions = %#v, %v", conditions, err)
	}
	actions, err := nativeMetricGroups(raw, "COMMAND")
	if err != nil || len(actions) != 1 || actions[0].Options[0].Key != "restart" || actions[0].Options[0].DataType != "String" {
		t.Fatalf("actions = %#v, %v", actions, err)
	}
}

func TestNativeMetricEnumsKeepJSONTypesAndRejectInvalidCodes(t *testing.T) {
	for _, tc := range []struct{ base, code string }{{"NUMBER", "1.5"}, {"STRING", `"1"`}, {"BOOLEAN", "false"}} {
		raw := json.RawMessage(`{"fields":[{"kind":"TELEMETRY","identifier":"state","nameI18n":{"default":"状态"},"valueType":{"kind":"ENUM","baseKind":"` + tc.base + `","enumValues":[{"code":` + tc.code + `,"label":{"default":"映射"}}]}}]}`)
		groups, err := nativeMetricGroups(raw, "TELEMETRY")
		if err != nil {
			t.Fatal(err)
		}
		item := groups[0].Options[0].Enum[0]
		if string(item.Value) != tc.code || item.ValueType != tc.base {
			t.Fatalf("enum = %#v", item)
		}
	}
	for _, raw := range []string{`{}`, `{"fields":null}`, `{"fields":[{"kind":"TELEMETRY","identifier":"bad","nameI18n":{"default":"Bad"},"valueType":{"kind":"ENUM","baseKind":"NUMBER","enumValues":[{"code":"1","label":{"default":"Bad"}}]}}]}`} {
		if _, err := nativeMetricGroups(json.RawMessage(raw), "TELEMETRY"); err == nil {
			t.Fatalf("accepted invalid model %s", raw)
		}
	}
}

func TestNativeMetricMenusRequireTenantBeforeDatabaseAccess(t *testing.T) {
	for _, claims := range []*utils.UserClaims{nil, {}} {
		if _, err := nativeDeviceMetricMenu(context.Background(), "device", claims, "token", "TELEMETRY"); err == nil {
			t.Fatal("accepted session without tenant")
		}
		if _, err := nativeConfigMetricMenu(context.Background(), "config", claims, "token", "COMMAND"); err == nil {
			t.Fatal("accepted session without tenant")
		}
	}
}

func TestNativeMetricBindingRejectsUnboundAndPartialReferences(t *testing.T) {
	for _, config := range []*model.DeviceConfig{nil, {}, nativeCommandDeviceConfig("m", "", 1), {NativeBindingRevision: 4}} {
		if _, err := readNativeBindingSnapshot(context.Background(), "token", config); err == nil || !strings.Contains(err.Error(), "binding is incomplete") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestNativeMetricBoolLabelsKeepBooleanCodes(t *testing.T) {
	groups, err := nativeMetricGroups(json.RawMessage(`{"fields":[{"kind":"TELEMETRY","identifier":"power","nameI18n":{"default":"电源"},"valueType":{"kind":"BOOL","constraint":{"trueText":"开启","falseText":"关闭"}}}]}`), "TELEMETRY")
	if err != nil {
		t.Fatal(err)
	}
	options := groups[0].Options[0].Enum
	if len(options) != 2 || string(options[0].Value) != "true" || options[1].Description != "关闭" {
		t.Fatalf("bool labels = %#v", options)
	}
}
