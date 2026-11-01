package main

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestThingModelCommandsUsePublishedAPI(t *testing.T) {
	srv, cap := newServer(t, 200, `{"ok":true}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "thingmodels", "list", "--page", "2", "--page-size", "25", "--query", "sensor")
	if code != 0 {
		t.Fatalf("list: %d %s", code, stderr)
	}
	r := cap.last()
	q, _ := url.ParseQuery(r.Query)
	if r.Method != "GET" || r.Path != "/api/v2/thing-models" || q.Get("pageSize") != "25" || q.Get("query") != "sensor" {
		t.Fatalf("request: %+v", r)
	}
	code, _, stderr = runCLI("--base-url", srv.URL, "thingmodels", "version", "model-1", "3")
	if code != 0 || cap.last().Path != "/api/v2/thing-models/model-1/versions/3" {
		t.Fatalf("version: %d %s %+v", code, stderr, cap.last())
	}
}

func TestThingModelMutationUsesHeaderWithoutAutomationFields(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	file := writeTemp(t, `{"modelID":"model-1","expectedDraftRevision":2}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "thingmodels", "publish", "--file", file)
	if code != 0 {
		t.Fatalf("publish: %d %s", code, stderr)
	}
	r := cap.last()
	var body map[string]any
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatal(err)
	}
	if r.Path != "/api/v2/thing-model-commands/publish" || r.Idem == "" || body["request_id"] != nil || body["modelID"] != "model-1" {
		t.Fatalf("request: %+v body=%v", r, body)
	}
}
