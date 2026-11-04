package relational

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// MigrationReceipt describes an already applied migration in an older ledger.
// It may be imported only when the currently shipped migration has the same
// version, name, and SHA-256. This is for explicit ledger-format transitions;
// it does not infer applied state from the schema.
type MigrationReceipt struct {
	Version int64
	Name    string
	SHA256  string
}

// ImportMigrationHistory copies verified receipts from an older migration
// ledger into tp_schema_migrations. Every receipt must map to exactly one
// currently shipped migration. Existing new-ledger receipts must agree too.
// The caller remains responsible for reading the legacy ledger and preserving
// its receipt rows for consumers that still require them.
func ImportMigrationHistory(ctx context.Context, db *gorm.DB, resource string, files []Migration, applied []MigrationReceipt) error {
	pool, err := checkMigrateArgs(db, resource)
	if err != nil {
		return err
	}
	if err := validateMigrationHistoryImport(resource, files, applied); err != nil {
		return err
	}
	if len(applied) == 0 {
		return nil
	}
	return WithSchemaLock(ctx, pool, func(conn *sql.Conn, _ string) error {
		if _, err := conn.ExecContext(ctx, ledgerDDL); err != nil {
			return fmt.Errorf("relational: create %s: %w", LedgerTable, err)
		}
		for _, receipt := range applied {
			var name, checksum string
			err := conn.QueryRowContext(ctx, "SELECT name, sha256 FROM "+LedgerTable+" WHERE resource=? AND version=?", resource, receipt.Version).Scan(&name, &checksum)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				if _, err := conn.ExecContext(ctx, "INSERT INTO "+LedgerTable+" (resource, version, name, sha256) VALUES (?,?,?,?)", resource, receipt.Version, receipt.Name, receipt.SHA256); err != nil {
					return fmt.Errorf("relational: import legacy receipt %s/%d: %w", resource, receipt.Version, err)
				}
			case err != nil:
				return fmt.Errorf("relational: read imported receipt %s/%d: %w", resource, receipt.Version, err)
			case name != receipt.Name || checksum != receipt.SHA256:
				return fmt.Errorf("%w: existing receipt for %s/%d conflicts with legacy history", ErrChecksumMismatch, resource, receipt.Version)
			}
		}
		return nil
	})
}

func validateMigrationHistoryImport(resource string, files []Migration, applied []MigrationReceipt) error {
	if resource == "" || len(resource) > 64 {
		return fmt.Errorf("relational: migration resource name must be 1-64 characters")
	}
	byVersion := make(map[int64]Migration, len(files))
	byName := make(map[string]bool, len(files))
	for _, file := range files {
		if file.Version <= 0 || file.Name == "" || file.SHA256 == "" {
			return fmt.Errorf("relational: invalid migration supplied for history import")
		}
		if _, exists := byVersion[file.Version]; exists {
			return fmt.Errorf("relational: duplicate migration version %d for history import", file.Version)
		}
		if byName[file.Name] {
			return fmt.Errorf("relational: duplicate migration name %q for history import", file.Name)
		}
		byVersion[file.Version] = file
		byName[file.Name] = true
	}
	seenVersion, seenName := map[int64]bool{}, map[string]bool{}
	for _, receipt := range applied {
		file, ok := byVersion[receipt.Version]
		if !ok {
			return fmt.Errorf("relational: legacy receipt version %d has no shipped migration", receipt.Version)
		}
		if receipt.Version <= 0 || receipt.Name == "" || receipt.SHA256 == "" || receipt.Name != file.Name || receipt.SHA256 != file.SHA256 {
			return fmt.Errorf("%w: legacy receipt for %s/%d does not match the shipped migration", ErrChecksumMismatch, resource, receipt.Version)
		}
		if seenVersion[receipt.Version] || seenName[receipt.Name] {
			return fmt.Errorf("relational: duplicate legacy receipt for %s/%d", resource, receipt.Version)
		}
		seenVersion[receipt.Version], seenName[receipt.Name] = true, true
	}
	return nil
}
