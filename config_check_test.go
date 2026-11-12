package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigCheckRejectsMissingAndBrokenFiles(t *testing.T) {
	if err := runConfigCheck(filepath.Join(t.TempDir(), "absent.yml")); err == nil {
		t.Fatal("missing config accepted")
	}
	broken := filepath.Join(t.TempDir(), "broken.yml")
	if err := os.WriteFile(broken, []byte("db: [unterminated\n  : :"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigCheck(broken); err == nil {
		t.Fatal("broken yaml accepted")
	}
}

func TestConfigCheckAcceptsRepositoryConfig(t *testing.T) {
	if _, err := os.Stat("configs/rsa_key/private_key.pem"); err != nil {
		t.Skip("repository has no RSA key file; the positive case needs one")
	}
	if err := runConfigCheck("configs/conf.yml"); err != nil {
		t.Fatalf("repository config rejected: %v", err)
	}
}
