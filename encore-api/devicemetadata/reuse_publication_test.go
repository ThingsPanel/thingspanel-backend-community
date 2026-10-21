package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestReusePublicationContentComparison(t *testing.T) {
	same, err := samePublicationContent(json.RawMessage(`{"version":1,"name":"m","items":[]}`), json.RawMessage(`{"items":[],"name":"m","version":9}`))
	if err != nil || !same {
		t.Fatalf("allocation number/key order changed content: %v %v", same, err)
	}
	same, err = samePublicationContent(json.RawMessage(`{"version":1,"name":"before"}`), json.RawMessage(`{"version":1,"name":"after"}`))
	if err != nil || same {
		t.Fatal("real content changes must publish")
	}
	if _, err = samePublicationContent(json.RawMessage(`null`), json.RawMessage(`{}`)); err == nil {
		t.Fatal("invalid stored snapshot must fail closed")
	}
}

func TestReusePublishKeepsModelAndPreviousVersions(t *testing.T) {
	ctx := context.Background()
	tenant := testTenantID()
	actor := testActorID()
	model, err := createThingModel(ctx, tenant, &ThingModel{Name: "Reuse publication regression", Status: "DRAFT"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := publishThingModel(ctx, tenant, model.ID, actor, &publishReq{Changelog: "create"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := createThingModelItem(ctx, tenant, model.ID, &ThingModelItem{Type: "PROPERTY", Identifier: "reuse_temperature", NameI18n: json.RawMessage(`{"default":"Before"}`), ValueType: json.RawMessage(`{"kind":"FLOAT"}`), Access: json.RawMessage(`{"read":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := publishThingModel(ctx, tenant, model.ID, actor, &publishReq{Changelog: "add field"})
	if err != nil {
		t.Fatal(err)
	}
	item.NameI18n = json.RawMessage(`{"default":"After"}`)
	if _, err = updateThingModelItem(ctx, tenant, model.ID, item.ID, item); err != nil {
		t.Fatal(err)
	}
	third, err := publishThingModel(ctx, tenant, model.ID, actor, &publishReq{Changelog: "edit field"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 || third.Version != 3 || first.ThingModelID != third.ThingModelID {
		t.Fatal("model identity or consecutive versions lost")
	}
	old, err := getVersion(ctx, tenant, model.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(old.Content, []byte("Before")) || bytes.Contains(old.Content, []byte("After")) {
		t.Fatal("editing changed a previous published version")
	}
	if _, err = publishThingModel(ctx, tenant, model.ID, actor, &publishReq{Changelog: "no change"}); err == nil {
		t.Fatal("unchanged content created another publication")
	}
	versions, err := listVersions(ctx, tenant, model.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("unexpected version count %d", len(versions))
	}
}
