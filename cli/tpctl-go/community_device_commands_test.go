package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommunityDeviceAuthAndFailureEnvelope(t *testing.T) {
	var token, idem, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, idem, path = r.Header.Get("x-token"), r.Header.Get("Idempotency-Key"), r.URL.Path
		_, _ = w.Write([]byte(`{"code":40001,"message":"device rejected"}`))
	}))
	defer srv.Close()
	code, out, _ := runCLI("--base-url", srv.URL, "--token", "test-token", "devices", "delete", "d1")
	if code == 0 || token != "test-token" || idem != "" || path != "/api/v1/device/d1" {
		t.Fatalf("code=%d token=%q idem=%q path=%q output=%s", code, token, idem, path, out)
	}
	if decode(t, out).OK {
		t.Fatal("Community error returned as success")
	}
}

func TestCommunityDeviceListPagination(t *testing.T) {
	srv, cap := newServer(t, 200, `{"code":200,"data":[]}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "device-groups", "list")
	if code != 0 {
		t.Fatalf("list: %d %s", code, stderr)
	}
	if got := cap.last(); got.Path != "/api/v1/device/group" || got.Query != "page=1&page_size=20" {
		t.Fatalf("request: %+v", got)
	}
}

func TestDeviceTemplateUsesDeviceConfigAPI(t *testing.T) {
	srv, cap := newServer(t, 200, `{"code":200,"data":{}}`)
	code, _, stderr := runCLI("--base-url", srv.URL, "device-templates", "get", "template-1")
	if code != 0 || cap.last().Path != "/api/v1/device_config/template-1" {
		t.Fatalf("code=%d stderr=%s request=%+v", code, stderr, cap.last())
	}
}

func TestCommunityDeviceRejectsUnsupportedIdempotencyKey(t *testing.T) {
	srv, cap := newServer(t, 200, `{"code":200}`)
	code, _, _ := runCLI("--base-url", srv.URL, "--idempotency-key", "retry-key", "devices", "delete", "d1")
	if code != exitUsage || len(cap.all()) != 0 {
		t.Fatalf("code=%d requests=%+v", code, cap.all())
	}
}
