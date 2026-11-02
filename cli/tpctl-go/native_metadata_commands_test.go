package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeMetadataConditionalWrite(t *testing.T) {
	var method, path, etag, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		etag, key = r.Header.Get("If-Match"), r.Header.Get("Idempotency-Key")
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte(`{"id":"d1"}`))
	}))
	defer srv.Close()
	file := writeTemp(t, `{"name":"new"}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "--idempotency-key", "device-update-1", "native-devices", "update", "d1", "--file", file, "--if-match", `"v1"`)
	if code != 0 || method != "PATCH" || path != "/api/v2/devices/d1" || etag != `"v1"` || key == "" {
		t.Fatalf("code=%d stderr=%s method=%s path=%s etag=%s key=%s", code, stderr, method, path, etag, key)
	}
}

func TestNativeMetadataReadsSavedETag(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	file := writeTemp(t, `{"name":"new"}`)
	etag := writeTemp(t, `"v1"`)
	code, _, stderr := runCLI("--base-url", srv.URL, "--idempotency-key", "device-update-1", "device", "update", "d1", "--file", file, "--if-match-file", etag)
	if code != 0 || cap.last().Path != "/api/v2/devices/d1" {
		t.Fatalf("code=%d stderr=%s request=%+v", code, stderr, cap.last())
	}
}

func TestNativeMetadataReadFiltersAndPagination(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "native-devices", "commands", "d1", "--page", "2", "--page-size", "30", "--state", "failed")
	if code != 0 {
		t.Fatalf("list: %d %s", code, stderr)
	}
	q, err := url.ParseQuery(cap.last().Query)
	if err != nil || q.Get("page") != "2" || q.Get("pageSize") != "30" || q.Get("state") != "failed" {
		t.Fatalf("query=%v error=%v", q, err)
	}
}

func TestNativeDeviceReadingsDefaultsToLatestAndPreservesExplicitModes(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantLatest string
		wantFrom   string
	}{
		{name: "default latest", wantLatest: "true"},
		{name: "explicit latest", args: []string{"--latestOnly", "false"}, wantLatest: "false"},
		{name: "history", args: []string{"--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T01:00:00Z", "--identifier", "temp"}, wantFrom: "2026-01-01T00:00:00Z"},
		{name: "event", args: []string{"--kind", "event"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, cap := newServer(t, 200, `{}`)
			args := append([]string{"--base-url", srv.URL, "native-devices", "readings", "d1"}, tc.args...)
			code, _, stderr := runCLI(args...)
			q, err := url.ParseQuery(cap.last().Query)
			if code != 0 || err != nil || q.Get("latestOnly") != tc.wantLatest || q.Get("from") != tc.wantFrom {
				t.Fatalf("code=%d stderr=%s query=%v err=%v", code, stderr, q, err)
			}
		})
	}
}

func TestNativeMetadataRequiresIfMatch(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	code, _, _ := runCLI("--base-url", srv.URL, "--idempotency-key", "group-delete-1", "native-groups", "delete", "g1")
	if code != exitUsage || len(cap.all()) != 0 {
		t.Fatalf("code=%d calls=%+v", code, cap.all())
	}
}

func TestNativeMetadataGetRetainsETag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"rev1"`)
		_, _ = w.Write([]byte(`{"id":"g1"}`))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "group.etag")
	code, out, stderr := runCLI("--base-url", srv.URL, "native-groups", "get", "g1", "--save-etag", path)
	etag, err := os.ReadFile(path)
	if code != 0 || err != nil || string(etag) != `"rev1"` || !strings.Contains(out, `"id":"g1"`) {
		t.Fatalf("code=%d out=%s stderr=%s", code, out, stderr)
	}
}

func TestNativeMetadataRejectsUnsafeInputsBeforeSending(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	file := writeTemp(t, `{"name":"x"}`)
	for _, args := range [][]string{
		{"--base-url", srv.URL, "--tenant", "tenant", "group", "list"},
		{"--base-url", srv.URL, "--idempotency-key", "bad key!", "device", "create", "--file", file},
		{"--base-url", srv.URL, "--idempotency-key", "safe-key-1", "device", "update", "x", "--file", file, "--if-match", `W/"v1"`},
		{"--base-url", "http://example.com", "device", "list"},
	} {
		code, _, _ := runCLI(args...)
		if code != exitUsage {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
	if len(cap.all()) != 0 {
		t.Fatalf("unexpected calls: %+v", cap.all())
	}
}

func TestNativeGroupHelpListsAvailableCommands(t *testing.T) {
	code, out, _ := runCLI("help", "device")
	if code != 0 || !strings.Contains(out, "device list") || !strings.Contains(out, "device credentials") {
		t.Fatalf("code=%d output=%s", code, out)
	}
}

func TestNativeMetadataCopyAndValidationContracts(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	file := writeTemp(t, `{"name":"copy"}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "--idempotency-key", "template-copy-1", "native-templates", "copy", "t1", "--file", file)
	if code != 0 {
		t.Fatalf("copy: %d %s", code, stderr)
	}
	if got := cap.last(); got.Path != "/api/v2/device-access-templates/t1/copies" || got.Method != "POST" || got.Idem != "template-copy-1" {
		t.Fatalf("copy: %+v", got)
	}
	code, _, stderr = runCLI("--base-url", srv.URL, "native-templates", "validate", "t1", "--file", file)
	if code != 0 {
		t.Fatalf("validate: %d %s", code, stderr)
	}
	if got := cap.last(); got.Path != "/api/v2/device-access-templates/t1/validations" || got.Idem != "" {
		t.Fatalf("validate: %+v", got)
	}
}

func TestSimulatorCloseDoesNotClaimIdempotency(t *testing.T) {
	srv, cap := newServer(t, 200, `{}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "native-devices", "simulator-stop", "d1", "s1")
	if code != 0 {
		t.Fatalf("stop: %d %s", code, stderr)
	}
	if got := cap.last(); got.Method != "DELETE" || got.Path != "/api/v2/devices/d1/simulator-sessions/s1" || got.Idem != "" {
		t.Fatalf("request: %+v", got)
	}
}
