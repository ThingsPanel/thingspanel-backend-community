package relational

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateMigrationHistoryImportRequiresExactOneToOneReceipts(t *testing.T) {
	files := []Migration{
		{Version: 1, Name: "0001_core.up.sql", SHA256: "sha-1"},
		{Version: 2, Name: "0002_auth.up.sql", SHA256: "sha-2"},
	}
	tests := []struct {
		name     string
		receipts []MigrationReceipt
		files    []Migration
		wantErr  error
		wantText string
	}{
		{name: "partial applied history is valid", receipts: []MigrationReceipt{{Version: 1, Name: files[0].Name, SHA256: files[0].SHA256}}},
		{name: "all applied history is valid", receipts: []MigrationReceipt{{Version: 1, Name: files[0].Name, SHA256: files[0].SHA256}, {Version: 2, Name: files[1].Name, SHA256: files[1].SHA256}}},
		{name: "missing source mapping", receipts: []MigrationReceipt{{Version: 3, Name: "0003_missing.up.sql", SHA256: "sha-3"}}, wantText: "has no shipped migration"},
		{name: "file name mismatch", receipts: []MigrationReceipt{{Version: 1, Name: "0001_wrong.up.sql", SHA256: files[0].SHA256}}, wantErr: ErrChecksumMismatch},
		{name: "checksum mismatch", receipts: []MigrationReceipt{{Version: 1, Name: files[0].Name, SHA256: "wrong"}}, wantErr: ErrChecksumMismatch},
		{name: "duplicate receipt version", receipts: []MigrationReceipt{{Version: 1, Name: files[0].Name, SHA256: files[0].SHA256}, {Version: 1, Name: files[0].Name, SHA256: files[0].SHA256}}, wantText: "duplicate legacy receipt"},
		{name: "duplicate source version", files: []Migration{files[0], files[0]}, wantText: "duplicate migration version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputFiles := tt.files
			if inputFiles == nil {
				inputFiles = files
			}
			err := validateMigrationHistoryImport("platformconfig-dictionary", inputFiles, tt.receipts)
			if tt.wantErr == nil && tt.wantText == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error=%v, want %v", err, tt.wantErr)
			}
			if tt.wantText != "" && (err == nil || !strings.Contains(err.Error(), tt.wantText)) {
				t.Fatalf("error=%v, want text %q", err, tt.wantText)
			}
		})
	}
}
