package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDictionarySchemaReceiptMatchesSingleMigrationSource(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "sql", DictionaryMigrationFile))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	if hex.EncodeToString(sum[:]) != DictionaryMigrationChecksum {
		t.Fatal("native receipt expectation differs from existing migration runner's source")
	}
	parts := strings.Split(string(contents), "$dictionary_schema$")
	if len(parts) != 3 {
		t.Fatal("dictionary schema function source is ambiguous")
	}
	body := sha256.Sum256([]byte(parts[1]))
	if hex.EncodeToString(body[:]) != DictionarySchemaFunctionChecksum {
		t.Fatal("native schema verifier expectation differs from its migration source")
	}
}
