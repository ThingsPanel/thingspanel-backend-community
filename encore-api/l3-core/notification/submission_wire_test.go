package notification

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"encore.app/l3-core/notification/contract"
)

func TestEmbeddedNotificationWireMatchesFrozenSources(t *testing.T) {
	for _, entry := range [][2]string{{"notification-v2.yaml", "openapi/notification-v2.yaml"}, {"source-event-v1.schema.json", "notification/source-event-v1.schema.json"}, {"plugin-v1.openapi.yaml", "notification/plugin-v1.openapi.yaml"}} {
		got, err := notificationWireSchemas.ReadFile("wire_schema/" + entry[0])
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join("../../../specs", entry[1]))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("embedded wire schema drift: %s", entry[0])
		}
	}
}

func TestFrozenPluginResultRequiresExplicitOutcomeFields(t *testing.T) {
	var result contract.PluginSendResultV1
	for _, raw := range []string{`{"delivery_id":"fixture","outcome":"accepted"}`, `{"delivery_id":"fixture","outcome":"accepted","receipt_expected":true}`, `{"delivery_id":"fixture","outcome":"rejected","receipt_expected":false}`, `{"delivery_id":"fixture","outcome":"unknown","receipt_expected":false,"error":{"code":"unknown","message":"unknown","retryable":true}}`} {
		if DecodeNotificationBody([]byte(raw), "PluginSendResult", &result) == nil {
			t.Fatal("invalid plugin result accepted")
		}
	}
	if err := DecodeNotificationBody([]byte(`{"delivery_id":"fixture","outcome":"accepted","receipt_expected":false}`), "PluginSendResult", &result); err != nil {
		t.Fatal(err)
	}
}

func TestFrozenSchemaRejectsOmittedRequiredBooleanAndSourceFields(t *testing.T) {
	var group contract.GroupCreate
	for _, raw := range []string{`{"name":"fixture","bindings":[]}`, `{"name":"fixture","enabled":null,"bindings":[]}`, `{"name":"fixture","Enabled":false,"bindings":[]}`} {
		if DecodeNotificationBody([]byte(raw), "GroupCreate", &group) == nil {
			t.Fatal("missing/null/wrong-case enabled accepted")
		}
	}
	if err := DecodeNotificationBody([]byte(`{"name":"fixture","enabled":false,"bindings":[]}`), "GroupCreate", &group); err != nil {
		t.Fatal(err)
	}
	var event contract.SourceEventV1
	raw, err := os.ReadFile("../../../specs/notification/fixtures/source-event.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := DecodeNotificationBody(raw, "SourceEventV1", &event); err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"subject"`), []byte(`"unrecognizedSubject"`), 1)
	if DecodeNotificationBody(raw, "SourceEventV1", &event) == nil {
		t.Fatal("missing source subject accepted")
	}
}

func TestFrozenSchemaAcceptsGroupUpdateAndResponse(t *testing.T) {
	var update contract.GroupUpdate
	raw, err := os.ReadFile("../../../specs/notification/fixtures/group-update.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := DecodeNotificationBody(raw, "GroupUpdate", &update); err != nil {
		t.Fatal(err)
	}
	var view contract.GroupView
	raw, err = os.ReadFile("../../../specs/notification/fixtures/group-response.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := DecodeNotificationBody(raw, "Group", &view); err != nil {
		t.Fatal(err)
	}
}
