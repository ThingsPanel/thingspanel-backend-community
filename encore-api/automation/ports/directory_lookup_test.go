package ports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirectoryExactLookupUsesBoundedBatches(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/internal/automation/devices/lookup" || r.Header.Get("X-Automation-Tenant") != "tenant-a" || r.Header.Get("X-Automation-Deployment") != "deployment-a" || r.Header.Get("Authorization") != "Bearer service-token" {
			t.Error("wrong lookup contract")
		}
		var body map[string][]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || len(body["ids"]) > 200 || len(body["ids"]) == 0 {
			t.Error("unbounded lookup body")
		}
		rows := []Device{}
		for _, id := range body["ids"] {
			rows = append(rows, Device{ID: id, TenantID: "tenant-a"})
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"list": rows, "total": len(rows)}})
	}))
	defer server.Close()
	dir := NewHTTPDirectory(Config{CommunityBaseURL: server.URL, CommunityToken: "service-token", DeploymentID: "deployment-a"})
	ids := []string{}
	for i := 0; i < 450; i++ {
		ids = append(ids, fmt.Sprintf("device-%d", i))
	}
	ids = append(ids, ids[0])
	found, err := dir.ByIDs(context.Background(), "tenant-a", ids)
	if err != nil || len(found) != 450 || calls != 3 {
		t.Fatalf("want 3 bounded batches/450 rows got%d/%d err%v", calls, len(found), err)
	}
}
func TestDirectoryLookupOldOwner404HasNoFullListFallback(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/internal/automation/devices/lookup" {
			t.Error("full-list fallback attempted")
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	dir := NewHTTPDirectory(Config{CommunityBaseURL: server.URL})
	if found, err := dir.ByIDs(context.Background(), "tenant-a", []string{"a"}); err == nil || len(found) != 0 || calls != 1 {
		t.Fatal("old owner lookup did not failclosed")
	}
}
func TestDirectoryExactLookupRejectsInvalidResponses(t *testing.T) {
	for _, body := range []string{
		`{"code":200,"data":{"list":[{"id":"a","tenant_id":"tenant-b"}],"total":1}}`,
		`{"code":200,"data":{"list":[{"id":"b","tenant_id":"tenant-a"}],"total":1}}`,
		`{"code":200,"data":{"list":[{"id":"a","tenant_id":"tenant-a"},{"id":"a","tenant_id":"tenant-a"}],"total":2}}`,
		`{"code":200,"data":{"list":[],"total":500}}`,
		`{"code":500,"data":{"list":[],"total":0}}`,
		`<html>login</html>`,
	} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer server.Close()
			dir := NewHTTPDirectory(Config{CommunityBaseURL: server.URL})
			if found, err := dir.ByIDs(context.Background(), "tenant-a", []string{"a"}); err == nil || len(found) != 0 {
				t.Fatal("invalid lookup response accepted")
			}
		})
	}
}
func TestDirectoryExactLookupMissingIDIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":200,"data":{"list":[],"total":0}}`))
	}))
	defer server.Close()
	dir := NewHTTPDirectory(Config{CommunityBaseURL: server.URL})
	if found, err := dir.ByIDs(context.Background(), "tenant-a", []string{"missing"}); err != nil || len(found) != 0 {
		t.Fatal("missing ID result broadened or failed")
	}
}
