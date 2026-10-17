package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDispatchOTADeviceRequiresAcceptedDeviceTask(t *testing.T) {
	var gotPath, gotToken, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken = r.URL.Path, r.Header.Get("X-Yomi-Internal-Token")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"status":"ACCEPTED"}}`))
	}))
	defer server.Close()

	if err := dispatchOTADevice(server.URL+"/api/v1/internal/ota/firmware", "internal-secret", "80:45:6b:34:30:7c", "detail-1", "package-1"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/internal/ota/devices/80:45:6b:34:30:7c/dispatch" {
		t.Fatalf("unexpected dispatch path: %s", gotPath)
	}
	if gotToken != "internal-secret" {
		t.Fatalf("internal token missing: %q", gotToken)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil || body["requestId"] != "detail-1" || body["packageId"] != "package-1" {
		t.Fatalf("unexpected dispatch body: %s", gotBody)
	}
}

func TestDispatchOTADeviceRejectsNonAcceptedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"status":"FAILED"}}`))
	}))
	defer server.Close()
	if err := dispatchOTADevice(server.URL+"/firmware", "secret", "device-1", "detail-1", "package-1"); err == nil {
		t.Fatal("expected non-accepted dispatch to fail")
	}
}
