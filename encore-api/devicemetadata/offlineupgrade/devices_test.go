package offlineupgrade

import (
	"encoding/json"
	"testing"
)

func fixtureDevices() DeviceIdentityInput {
	g := fixtureInput()
	return DeviceIdentityInput{SourceDatabase: g.SourceDatabase, TargetDatabase: g.TargetDatabase,
		SourceArchiveSHA256: g.SourceArchiveSHA256, TargetArchiveSHA256: g.TargetArchiveSHA256, QuarantineSHA256: g.QuarantineSHA256}
}

func fixtureDevice(id, tenant, name, parent string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"id": id, "tenant_id": tenant, "name": name, "device_number": id, "parent_id": parent,
		"is_enabled": "", "activate_flag": "active", "activate_at": nil, "created_at": "2020-01-01T00:00:00Z", "update_at": "2020-01-02T00:00:00Z",
		"location": ",", "label": "tag,123", "additional_info": map[string]any{"mqtt_config": map[string]any{"private_connector_value": "never_project"}, "extendedInfo": []any{map[string]any{"name": "power", "value": 123}}}})
	return raw
}

func TestDeviceIdentityPlanPreservesSemanticsAndOnlyApprovedNames(t *testing.T) {
	in := fixtureDevices()
	in.Devices = []json.RawMessage{fixtureDevice("gateway", "one", "gateway", ""), fixtureDevice("child", "one", "", "gateway")}
	in.NameFromNumber = []string{"child"}
	p, err := PlanDeviceIdentities(in)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]PlannedDevice{}
	for _, d := range p.Devices {
		byID[d.SourceID] = d
	}
	if byID["gateway"].Row.DeviceKind != "gateway" || byID["child"].Row.DeviceKind != "subdevice" {
		t.Fatal("trusted hierarchy was not retained")
	}
	if byID["child"].Row.Name != "child" || !byID["child"].NameFromNumber {
		t.Fatal("reviewed display name missing")
	}
	for _, d := range p.Devices {
		if d.Row.AdministrativeState != "disabled" || d.Row.ActivationTimeSource != "source_unknown" || d.Row.ActivatedAt != nil {
			t.Fatal("invented lifecycle state")
		}
		if d.Row.Location != nil || d.Row.Tags != `["tag","123"]` || d.Row.CustomFields != `{"power":123}` {
			t.Fatal("source metadata mapping incorrect")
		}
	}
	in.NameFromNumber = nil
	if _, err := PlanDeviceIdentities(in); err == nil {
		t.Fatal("invented an unapproved display name")
	}
}

func TestDeviceIdentityPlanRejectsOwnershipAndStaleResolutions(t *testing.T) {
	in := fixtureDevices()
	in.Devices = []json.RawMessage{fixtureDevice("gateway", "one", "gateway", ""), fixtureDevice("child", "two", "child", "gateway")}
	if _, err := PlanDeviceIdentities(in); err == nil {
		t.Fatal("cross-tenant hierarchy accepted")
	}
	in.ExcludedGatewayRelations = []DeviceRelation{{DeviceID: "child", TargetID: "gateway"}}
	if _, err := PlanDeviceIdentities(in); err != nil {
		t.Fatal(err)
	}
	in.ExcludedGatewayRelations[0].TargetID = "wrong"
	if _, err := PlanDeviceIdentities(in); err == nil {
		t.Fatal("stale relation approval accepted")
	}
}

func TestDeviceIdentityPlanRejectsAmbiguousCustomMetadata(t *testing.T) {
	for _, raw := range []string{`{"extendedInfo":[{"name":"x","value":1},{"name":"x","value":2}]}`, `{"other":{"nested":1}}`, `{"extendedInfo":[{"name":"x"}]}`} {
		if _, err := legacyCustomFields([]byte(raw)); err == nil {
			t.Fatal("ambiguous source metadata accepted")
		}
	}
	for _, value := range []string{"NaN,0", "181,0", "1,91", "1,", "0,Infinity"} {
		if _, err := legacyLocation(value); err == nil {
			t.Fatal("invalid coordinates accepted")
		}
	}
}
