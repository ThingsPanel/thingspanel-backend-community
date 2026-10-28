package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes a file for a --file flag and removes it with the test.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return path
}
