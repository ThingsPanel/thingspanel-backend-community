package thingmodel

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"encore.app/l1-access/authn"
	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
)

func TestNativeTransportRetainsTypedFields(t *testing.T) {
	input := []byte(`{"name":"Smoke model","fields":[{"kind":"TELEMETRY","identifier":"Mode-Wire","nameI18n":{"default":"Mode"},"access":{"read":true,"write":false,"observe":true},"unit":"state","valueType":{"kind":"ENUM","baseKind":"BOOLEAN","enumValues":[{"code":false,"label":{"default":"Off"}},{"code":true,"label":{"default":"On"}}]}},{"kind":"COMMAND","identifier":"Mode-Wire","nameI18n":{"default":"Set mode"},"access":{"read":false,"write":true,"observe":false},"parameters":{"schemaVersion":1,"items":[{"identifier":"target","nameI18n":{"default":"Target"},"valueType":{"kind":"NUMBER","constraint":{"min":0,"max":100,"step":0.5}}}]}}]}`)
	var draft ModelDraft
	if e := json.Unmarshal(input, &draft); e != nil {
		t.Fatal(e)
	}
	m, e := draft.coreModel()
	if e != nil {
		t.Fatal(e)
	}
	m.ID = "model"
	m.TenantID = "tenant"
	if issues := core.ValidateModel(m); len(issues) > 0 {
		t.Fatalf("unexpected validation: %+v", issues)
	}
	if m.Fields[0].Identifier != "Mode-Wire" || m.Fields[0].Unit != "state" || m.Fields[0].ValueType.EnumBaseKind != core.ScalarBool {
		t.Fatalf("wire semantics changed: %+v", m.Fields[0])
	}
	if string(m.Fields[0].ValueType.EnumValues[0].Value) != "false" {
		t.Fatal("boolean enum was coerced")
	}
	got, e := json.Marshal(draftFromCore(m))
	if e != nil {
		t.Fatal(e)
	}
	var original, roundtrip any
	json.Unmarshal(input, &original)
	json.Unmarshal(got, &roundtrip)
	if !reflect.DeepEqual(original, roundtrip) {
		t.Fatalf("transport loses contract data:\n%s", got)
	}

	// Command parameters must survive publication and the same transport
	// projection used by the version-detail endpoint.
	snapshot, e := core.BuildSnapshot(m, core.SnapshotID("snapshot-v1"))
	if e != nil {
		t.Fatal(e)
	}
	versionDraft := draftFromCore(core.Model{
		Name: snapshot.Name, Description: snapshot.Description,
		Category: snapshot.Category, Fields: snapshot.Fields,
	})
	versionJSON, e := json.Marshal(versionDraft)
	if e != nil {
		t.Fatal(e)
	}
	var versionRoundtrip ModelDraft
	if e := json.Unmarshal(versionJSON, &versionRoundtrip); e != nil {
		t.Fatal(e)
	}
	var command *ModelField
	for i := range versionRoundtrip.Fields {
		if versionRoundtrip.Fields[i].Kind == core.FieldCommand {
			command = &versionRoundtrip.Fields[i]
			break
		}
	}
	if command == nil || command.Parameters == nil {
		t.Fatalf("version DTO lost command parameters:\n%s", versionJSON)
	}
	var originalDraft ModelDraft
	if e := json.Unmarshal(input, &originalDraft); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(command.Parameters, originalDraft.Fields[1].Parameters) {
		t.Fatalf("version DTO changed command parameter schema:\n%s", versionJSON)
	}
}

func TestNativeTransportRejectsUnsupportedOrIncompletePayload(t *testing.T) {
	for _, input := range []string{
		`{"name":"x"}`,
		`{"name":"x","fields":[{"access":{"read":true,"write":false}}]}`,
		`{"name":"x","fields":[{"fieldID":"legacy-row","access":{"read":true,"write":false,"observe":true}}]}`,
		`{"name":"x","fields":[{"metaItems":{"unknown":"preserve me"},"access":{"read":true,"write":false,"observe":true}}]}`,
	} {
		var d ModelDraft
		if e := json.Unmarshal([]byte(input), &d); e != nil {
			t.Fatal(e)
		}
		if _, e := d.coreModel(); !errors.Is(e, app.ErrInvalid) {
			t.Errorf("expected explicit error for %s; got %v", input, e)
		}
	}
	var empty ModelDraft
	json.Unmarshal([]byte(`{"name":"Empty draft","fields":[]}`), &empty)
	if _, e := empty.coreModel(); e != nil {
		t.Fatalf("explicit empty field list must remain valid: %v", e)
	}
}

func TestUpdateResourcesRequestDistinguishesOmittedFromExplicitEmptyDocument(t *testing.T) {
	var omitted UpdateResourcesRequest
	if err := json.Unmarshal([]byte(`{"modelID":"model"}`), &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.Resources != nil || omitted.ExpectedRevision != nil {
		t.Fatalf("omitted resources/revision must remain detectable: %+v", omitted)
	}
	var explicit UpdateResourcesRequest
	if err := json.Unmarshal([]byte(`{"modelID":"model","expectedRevision":0,"resources":{}}`), &explicit); err != nil {
		t.Fatal(err)
	}
	if explicit.Resources == nil || explicit.ExpectedRevision == nil || *explicit.ExpectedRevision != 0 {
		t.Fatalf("explicit empty document and revision zero must be preserved: %+v", explicit)
	}
}

func TestIdentityDoesNotCollapseDistinctLegacyIDsOrTrustScopes(t *testing.T) {
	a := canonicalIdentity("tenant", "same-x")
	b := canonicalIdentity("tenant", "same-y")
	if a == b || a != canonicalIdentity("tenant", "same-x") || a == canonicalIdentity("subject", "same-x") {
		t.Fatal("identity mapping is colliding, unstable or lacks a namespace")
	}
	p, e := principalForData(&authn.Data{UserID: "subject", TenantID: "same-x", Roles: []string{"unknown"}, Scopes: []string{"*"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = core.Authorize(p, p.TenantID, core.CapabilityWrite); e == nil {
		t.Fatal("wildcard scope granted an unknown Community role write access")
	}
	p, e = principalForData(&authn.Data{UserID: "subject", TenantID: "same-x", Roles: []string{"TENANT_ADMIN"}})
	if e != nil || core.Authorize(p, p.TenantID, core.CapabilityPublish) != nil {
		t.Fatal("tenant admin mapping failed")
	}
	if core.Authorize(p, core.TenantID(b), core.CapabilityRead) == nil {
		t.Fatal("role mapping granted cross-tenant access")
	}
}

func TestPlatformAdminRequiresAuditedTenantDelegation(t *testing.T) {
	for _, tenantID := range []string{"", "tenant-target"} {
		_, err := principalForData(&authn.Data{
			UserID: "sys-admin", TenantID: tenantID, Roles: []string{"SYS_ADMIN"},
		})
		if err == nil {
			t.Fatalf("platform admin with tenant_id %q but no delegation entered a tenant-scoped model API", tenantID)
		}
	}
}

func TestVersionPaginationRejectsOverflow(t *testing.T) {
	if _, e := paginationStart(int(^uint(0)>>1), 200); !errors.Is(e, app.ErrInvalid) {
		t.Fatalf("overflow must fail before slicing: %v", e)
	}
	if start, e := paginationStart(2, 20); e != nil || start != 20 {
		t.Fatalf("ordinary second page: %d %v", start, e)
	}
}
