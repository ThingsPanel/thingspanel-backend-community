package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeAssetUploadMultipartContract(t *testing.T) {
	var purpose, filename, body, auth, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v2/file-assets" {
			t.Errorf("wrong endpoint: %s %s", r.Method, r.URL.Path)
		}
		auth, key = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		mr, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		for {
			part, e := mr.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Error(e)
				return
			}
			value, e := io.ReadAll(part)
			if e != nil {
				t.Error(e)
				return
			}
			switch part.FormName() {
			case "purpose":
				purpose = string(value)
			case "file":
				filename, body = part.FileName(), string(value)
			default:
				t.Errorf("unknown field")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"data":{"id":"asset1"}}`))
	}))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(p, []byte("image-test"), 0600); err != nil {
		t.Fatal(err)
	}
	resp, apiErr := uploadNativeAsset(newClient(srv.URL, "token", "", time.Second), p, "upload-key-1", "request-1")
	if apiErr != nil || resp == nil || resp.status != 201 || purpose != "template-image" || filename != "image.png" || body != "image-test" || auth != "Bearer token" || key != "upload-key-1" {
		t.Fatalf("unexpected response=%+v error=%+v purpose=%q filename=%q auth=%q key=%q", resp, apiErr, purpose, filename, auth, key)
	}
}

func TestNativeAssetUploadRejectsOversizeWithoutNetwork(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "oversize")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(nativeAssetMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_, apiErr := uploadNativeAsset(newClient(srv.URL, "", "", time.Second), p, "upload-key-1", "")
	if apiErr == nil || apiErr.ExitCode != exitUsage || called {
		t.Fatalf("unexpected error=%+v called=%v", apiErr, called)
	}
}

func TestNativeAssetUploadCLI(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Idempotency-Key")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"asset-1"}}`))
	}))
	defer srv.Close()
	path := writeTemp(t, "fake-image")
	code, out, stderr := runCLI("--base-url", srv.URL, "--idempotency-key", "asset-key-1", "asset", "upload", "--file", path)
	if code != 0 || seen != "asset-key-1" || out != "{\"data\":{\"id\":\"asset-1\"}}\n" { t.Fatalf("code=%d key=%q out=%s stderr=%s", code, seen, out, stderr) }
}
