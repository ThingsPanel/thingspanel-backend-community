package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeDownloadStreamsLargeFileAndChecksHash(t *testing.T) {
	content := bytes.Repeat([]byte("native-data"), 900000) // exceeds 8 MiB
	sum := sha256.Sum256(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/metadata-downloads/d1" || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("unexpected request")
		}
		w.Header().Set("ETag", fmt.Sprintf(`"sha256-%x"`, sum))
		_, _ = w.Write(content)
	}))
	defer srv.Close()
	target := filepath.Join(t.TempDir(), "readings.csv")
	if err := downloadMetadataArtifact(newClient(srv.URL, "token", "", time.Second*30), "d1", target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("download mismatch: %v", err)
	}
}

func TestNativeDownloadRejectsChecksumMismatchAndCleansUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"sha256-`+strings.Repeat("0", 64)+`"`)
		_, _ = w.Write([]byte("bad"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "x")
	if err := downloadMetadataArtifact(newClient(srv.URL, "", "", time.Second), "d1", target); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files remain: %v %v", files, err)
	}
}

func TestNativeDownloadNeverOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x")
	if err := os.WriteFile(target, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	if err := downloadMetadataArtifact(newClient(srv.URL, "", "", time.Second), "d1", target); err == nil {
		t.Fatal("overwrite accepted")
	}
	got, _ := os.ReadFile(target)
	if called || string(got) != "existing" {
		t.Fatal("existing destination changed or request sent")
	}
}

func TestNativeDownloadCLI(t *testing.T) {
	data := []byte("contents")
	sum := sha256.Sum256(data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", fmt.Sprintf(`"sha256-%x"`, sum))
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	target := filepath.Join(t.TempDir(), "artifact.csv")
	code, _, stderr := runCLI("--base-url", srv.URL, "artifact", "download", "d1", "--output-file", target)
	got, err := os.ReadFile(target)
	if code != 0 || err != nil || !bytes.Equal(got, data) { t.Fatalf("code=%d file=%q err=%v stderr=%s", code, got, err, stderr) }
}

func TestNativeDownloadRaceDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x")
	sum := sha256.Sum256([]byte("download"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(target, []byte("concurrent"), 0600); err != nil {
			t.Error(err)
		}
		w.Header().Set("ETag", fmt.Sprintf(`"sha256-%x"`, sum))
		_, _ = w.Write([]byte("download"))
	}))
	defer srv.Close()
	if err := downloadMetadataArtifact(newClient(srv.URL, "", "", time.Second), "d1", target); err == nil {
		t.Fatal("concurrent destination accepted")
	}
	got, _ := os.ReadFile(target)
	files, _ := os.ReadDir(dir)
	if string(got) != "concurrent" || len(files) != 1 {
		t.Fatalf("target=%q files=%v", got, files)
	}
}
