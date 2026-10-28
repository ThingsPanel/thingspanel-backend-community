package sql

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
)

// MigrationChecksum is independent of Git's platform-specific line endings.
func MigrationChecksum(contents []byte) string {
	return migrationDigest(bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n")))
}

// MigrationChecksumMatches also recognizes checksums recorded by older builds
// from LF or CRLF checkouts. It does not rewrite or replay an applied migration.
func MigrationChecksumMatches(recorded string, contents []byte) bool {
	lf := bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n"))
	return recorded == migrationDigest(contents) || recorded == migrationDigest(lf) ||
		recorded == migrationDigest(bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n")))
}

func migrationDigest(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
