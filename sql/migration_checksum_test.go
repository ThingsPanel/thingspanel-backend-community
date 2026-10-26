package sql

import (
	"bytes"
	"testing"
)

func TestMigrationChecksumAcrossCheckouts(t *testing.T) {
	lf := []byte("ALTER TABLE users\nADD COLUMN version bigint;\n")
	crlf := bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
	if MigrationChecksum(lf) != MigrationChecksum(crlf) {
		t.Fatal("checkout line endings changed the canonical checksum")
	}
	for _, recorded := range []string{migrationDigest(lf), migrationDigest(crlf)} {
		for _, checkout := range [][]byte{lf, crlf} {
			if !MigrationChecksumMatches(recorded, checkout) {
				t.Fatal("previous LF/CRLF checksum was rejected")
			}
			changed := bytes.ReplaceAll(checkout, []byte("bigint"), []byte("text"))
			if MigrationChecksumMatches(recorded, changed) {
				t.Fatal("changed SQL was accepted")
			}
		}
	}
	if MigrationChecksumMatches("", lf) || MigrationChecksumMatches("invalid", lf) {
		t.Fatal("invalid checksum was accepted")
	}
}
