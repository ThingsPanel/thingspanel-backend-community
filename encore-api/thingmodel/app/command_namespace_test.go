package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.app/thingmodel/core"
)

// Exercise the actual Create -> Publish -> Version path, rather than granting
// validity to a hand-made snapshot that could never pass model publication.
func TestPublishedCommandKindNamespaceAndVerbatimIdentifiers(t *testing.T) {
	for _, identifier := range []string{"Mode-Wire", "  Vendor.Mode/°C  ", " "} {
		t.Run(identifier, func(t *testing.T) {
			ctx := context.Background()
			service := New(newMemoryRepository(), sequenceIDs())
			principal := writerPrincipal("tenant-a", "user-a")
			parameter := "  target/value  "
			if identifier == " " {
				parameter = " "
			}
			payload := func(value string) json.RawMessage {
				return json.RawMessage(strings.ReplaceAll(value, "  target/value  ", parameter))
			}
			params := &core.Parameters{SchemaVersion: 1, Items: []core.Parameter{{Identifier: parameter, Name: core.I18nString{Default: "Target"}, ValueType: core.ValueType{Kind: core.ScalarBool}}}}
			model := core.Model{Name: "Namespace contract", Fields: []core.Field{
				{Kind: core.FieldTelemetry, Identifier: identifier, Name: core.I18nString{Default: "Reported mode"}, Access: core.Access{Read: true, Observe: true}, ValueType: &core.ValueType{Kind: core.ScalarNumber}},
				{Kind: core.FieldAttribute, Identifier: identifier, Name: core.I18nString{Default: "Mode attribute"}, Access: core.Access{Read: true}, ValueType: &core.ValueType{Kind: core.ScalarString}},
				{Kind: core.FieldEvent, Identifier: identifier, Name: core.I18nString{Default: "Mode event"}, Access: core.Access{Observe: true}, Parameters: params},
				{Kind: core.FieldCommand, Identifier: identifier, Name: core.I18nString{Default: "Set mode"}, Access: core.Access{Write: true}, Parameters: params},
			}}
			created, err := service.Create(ctx, principal, "create-namespace", model)
			if err != nil {
				t.Fatal(err)
			}
			published, err := service.Publish(ctx, principal, "publish-namespace", core.ModelID(created.ModelID), created.DraftRevision, "namespace contract")
			if err != nil || published.PublishedRevision == nil {
				t.Fatalf("publication failed: %+v %v", published, err)
			}
			version, err := service.Version(ctx, principal, core.ModelID(created.ModelID), *published.PublishedRevision)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{`{"  target/value  ":false}`, `{"  target/value  ":true}`} {
				if err := core.ValidateCommandParameters(version.Snapshot, identifier, payload(value)); err != nil {
					t.Errorf("published command rejected: %s: %v", value, err)
				}
			}
			for _, value := range []string{`{}`, `{"target/value":true}`, `{"  target/value  ":"true"}`, `{"  target/value  ":1}`, `{"  target/value  ":true,"extra":1}`} {
				if err := core.ValidateCommandParameters(version.Snapshot, identifier, payload(value)); err == nil {
					t.Errorf("invalid parameters accepted: %s", value)
				}
			}
			if core.ValidateCommandParameters(version.Snapshot, identifier+"missing", payload(`{"  target/value  ":true}`)) == nil {
				t.Fatal("unknown command accepted")
			}
			// Removing the COMMAND kind must not resolve to another same-name kind.
			fields := version.Snapshot.Fields[:0]
			for _, field := range version.Snapshot.Fields {
				if field.Kind != core.FieldCommand {
					fields = append(fields, field)
				}
			}
			version.Snapshot.Fields = fields
			if core.ValidateCommandParameters(version.Snapshot, identifier, payload(`{"  target/value  ":true}`)) == nil {
				t.Fatal("non-command namespace used")
			}
		})
	}
}
