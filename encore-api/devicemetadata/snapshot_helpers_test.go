package devicemetadata

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildSnapshotContentOmitsItemIDs(t *testing.T) {
	model := &ThingModel{
		ID:              "tm-1",
		Name:            "Thermostat",
		DescriptionI18n: json.RawMessage(`{"default":"desc"}`),
		Category:        "hvac",
	}
	items := []ThingModelItem{{
		ID:         "internal-id",
		Identifier: "temperature",
		Type:       "PROPERTY",
		NameI18n:   json.RawMessage(`{"default":"Temperature"}`),
		ValueType:  json.RawMessage(`{"kind":"FLOAT"}`),
		Access:     json.RawMessage(`{"read":true}`),
	}}
	content, err := buildSnapshotContent(model, items, 3)
	if err != nil {
		t.Fatalf("buildSnapshotContent failed: %v", err)
	}
	raw, _ := json.Marshal(content)
	if string(raw) == "" {
		t.Fatal("expected non-empty snapshot payload")
	}
	if strings.Contains(string(raw), "internal-id") {
		t.Fatal("snapshot content should not persist item internal ids")
	}
	if content.Version != 3 {
		t.Fatalf("expected version 3, got %d", content.Version)
	}
}

func TestComputeVersionDiff(t *testing.T) {
	from := &snapshotContent{
		Version: 1,
		Items: []snapshotContentItem{
			{Identifier: "a", Type: "PROPERTY", ValueType: json.RawMessage(`{"kind":"INT"}`)},
			{Identifier: "b", Type: "PROPERTY", ValueType: json.RawMessage(`{"kind":"INT"}`)},
		},
	}
	to := &snapshotContent{
		Version: 2,
		Items: []snapshotContentItem{
			{Identifier: "a", Type: "PROPERTY", ValueType: json.RawMessage(`{"kind":"FLOAT"}`)},
			{Identifier: "c", Type: "EVENT", ValueType: json.RawMessage(`{"kind":"STRING"}`)},
		},
	}
	diff := computeVersionDiff(from, to)
	if len(diff.AddedItems) != 1 || diff.AddedItems[0].Identifier != "c" {
		t.Fatalf("unexpected added items: %#v", diff.AddedItems)
	}
	if len(diff.RemovedItems) != 1 || diff.RemovedItems[0].Identifier != "b" {
		t.Fatalf("unexpected removed items: %#v", diff.RemovedItems)
	}
	if len(diff.ChangedItems) != 1 || diff.ChangedItems[0].Identifier != "a" {
		t.Fatalf("unexpected changed items: %#v", diff.ChangedItems)
	}
}
