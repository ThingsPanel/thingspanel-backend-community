package thingmodel

import (
	"context"
	"encoding/json"
	"encore.app/thingmodel/core"
	"github.com/google/uuid"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The identical fixture is consumed by Community's compiled uplink validator.
// This test actually publishes it and compares Version's DTO, not just a mock.
func TestPostgresOwnerUplinkPublishedCoreParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/uplink-core-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Model ModelDraft `json:"model"`
		Cases []struct {
			Family  string          `json:"family"`
			Payload json.RawMessage `json:"payload"`
			Valid   bool            `json:"valid"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	m, err := fixture.Model.coreModel()
	if err != nil {
		t.Fatal(err)
	}
	p := core.Principal{SubjectID: uuid.NewString(), TenantID: core.TenantID(uuid.NewString()), Capabilities: []string{string(core.CapabilityRead), string(core.CapabilityWrite), string(core.CapabilityPublish)}}
	svc := application()
	ctx := context.Background()
	created, err := svc.Create(ctx, p, uuid.NewString(), m)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, p, uuid.NewString(), core.ModelID(created.ModelID), created.DraftRevision, "core parity")
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Version(ctx, p, core.ModelID(created.ModelID), *published.PublishedRevision)
	if err != nil {
		t.Fatal(err)
	}
	got := draftFromCore(core.Model{Name: v.Snapshot.Name, Description: v.Snapshot.Description, Category: v.Snapshot.Category, Fields: v.Snapshot.Fields})
	if !reflect.DeepEqual(got, fixture.Model) {
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(fixture.Model)
		t.Fatalf("published DTO differs: got=%s want=%s", a, b)
	}
	for i, tc := range fixture.Cases {
		var values map[string]json.RawMessage
		if err = json.Unmarshal(tc.Payload, &values); err != nil {
			t.Fatal(err)
		}
		valid := true
		if tc.Family == "event" {
			var method string
			json.Unmarshal(values["method"], &method)
			var params map[string]json.RawMessage
			json.Unmarshal(values["params"], &params)
			for _, f := range v.Snapshot.Fields {
				if f.Kind == core.FieldEvent && f.Identifier == method {
					for _, param := range f.Parameters.Items {
						if core.ValidateScalar(param.ValueType, params[param.Identifier]) != nil {
							valid = false
						}
					}
				}
			}
		} else {
			for key, value := range values {
				for _, f := range v.Snapshot.Fields {
					if string(f.Kind) == strings.ToUpper(tc.Family) && f.Identifier == key {
						if core.ValidateScalar(*f.ValueType, value) != nil {
							valid = false
						}
					}
				}
			}
		}
		if valid != tc.Valid {
			t.Fatalf("core scalar case%d valid=%v want=%v", i, valid, tc.Valid)
		}
	}
}
