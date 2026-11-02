package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"

	"encore.app/pkg/ownermigration"
)

//go:embed mysql_migrations/*.sql
var mysqlMigrationFiles embed.FS

const mysqlMigrationLedger = "market_install_schema_migrations"

const mysqlLedgerDDL = `CREATE TABLE market_install_schema_migrations (
 version VARBINARY(64) NOT NULL,
 checksum VARBINARY(64) NOT NULL,
 state VARBINARY(16) NOT NULL,
 started_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 applied_at DATETIME(6) NULL,
 PRIMARY KEY(version),
 CONSTRAINT market_install_migration_state CHECK (state IN ('applying','applied'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`

type mysqlMigration struct{ name, table, contents, checksum string }
type migrationReceipt struct{ checksum, state string }

func installationMySQLMigrations() ([]mysqlMigration, error) {
	var migrations []mysqlMigration
	for _, item := range []struct{ name, table string }{
		{"0001_operations.sql", "market_install_operations"},
		{"0002_steps.sql", "market_install_steps"},
	} {
		source, err := mysqlMigrationFiles.ReadFile("mysql_migrations/" + item.name)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(source)
		migrations = append(migrations, mysqlMigration{item.name, item.table, string(source), hex.EncodeToString(digest[:])})
	}
	return migrations, nil
}

// MigrateMySQL is an explicit owner-runner entry, not a startup side effect of
// OpenMySQL. The migration role may run this before granting the runtime role
// access. PG stays on its existing Encore runner and historical version ledger.
// MySQL DDL is not rolled back: a durable applying receipt precedes each single
// atomic InnoDB DDL. Recovery holds the same schema lock and verifies every
// column/constraint before accepting an already-created table as its result.
func (s *Store) MigrateMySQL(ctx context.Context) error {
	if s.dialect != MySQL {
		return fmt.Errorf("MySQL owner migration requires a MySQL ledger")
	}
	return ownermigration.WithMySQLSchemaLock(ctx, s.DB, func(conn *sql.Conn, database string) error {
		migrations, err := installationMySQLMigrations()
		if err != nil {
			return err
		}
		exists, err := mysqlTableExists(ctx, conn, database, mysqlMigrationLedger)
		if err != nil {
			return err
		}
		if !exists {
			for _, m := range migrations {
				present, err := mysqlTableExists(ctx, conn, database, m.table)
				if err != nil {
					return err
				}
				if present {
					return fmt.Errorf("A08 table %s has no owner ledger; explicit migration review required", m.table)
				}
			}
			if _, err := conn.ExecContext(ctx, mysqlLedgerDDL); err != nil {
				return err
			}
		}
		if err := verifyMySQLTable(ctx, conn, database, mysqlMigrationLedger); err != nil {
			return err
		}
		receipts, err := readMySQLMigrationReceipts(ctx, conn, migrations)
		if err != nil {
			return err
		}
		for _, m := range migrations {
			receipt, recorded := receipts[m.name]
			present, err := mysqlTableExists(ctx, conn, database, m.table)
			if err != nil {
				return err
			}
			if recorded && receipt.state == "applied" {
				if err := verifyMySQLTable(ctx, conn, database, m.table); err != nil {
					return err
				}
				continue
			}
			if !recorded {
				if present {
					return fmt.Errorf("unowned A08 table %s cannot be adopted by an unrecorded migration", m.table)
				}
				result, err := conn.ExecContext(ctx, `INSERT INTO market_install_schema_migrations(version,checksum,state) VALUES(?,?,'applying')`, m.name, m.checksum)
				if err := exactlyOne(result, err); err != nil {
					return err
				}
			}
			// A prior executor that still owns the connection also owns GET_LOCK;
			// a replacement can enter here only after that connection releases it.
			if !present {
				if _, err := conn.ExecContext(ctx, m.contents); err != nil {
					return fmt.Errorf("%w: execute A08 migration %s: %v", ownermigration.ErrOutcomeUnknown, m.name, err)
				}
			}
			if err := verifyMySQLTable(ctx, conn, database, m.table); err != nil {
				return fmt.Errorf("%w: A08 migration %s schema does not attest its result: %v", ownermigration.ErrOutcomeUnknown, m.name, err)
			}
			result, err := conn.ExecContext(ctx, `UPDATE market_install_schema_migrations SET state='applied',applied_at=CURRENT_TIMESTAMP(6) WHERE version=? AND checksum=? AND state='applying'`, m.name, m.checksum)
			if err := exactlyOne(result, err); err != nil {
				return fmt.Errorf("%w: A08 migration %s receipt was not acknowledged: %v", ownermigration.ErrOutcomeUnknown, m.name, err)
			}
		}
		return nil
	})
}

// VerifyMySQLSchema must succeed before a runtime starts the A08 worker. It is
// read-only apart from the same exclusion lock; it never applies or adopts DDL.
func (s *Store) VerifyMySQLSchema(ctx context.Context) error {
	if s.dialect != MySQL {
		return fmt.Errorf("MySQL schema verification requires a MySQL ledger")
	}
	return ownermigration.WithMySQLSchemaLock(ctx, s.DB, func(conn *sql.Conn, database string) error {
		migrations, err := installationMySQLMigrations()
		if err != nil {
			return err
		}
		if err := verifyMySQLTable(ctx, conn, database, mysqlMigrationLedger); err != nil {
			return err
		}
		receipts, err := readMySQLMigrationReceipts(ctx, conn, migrations)
		if err != nil {
			return err
		}
		for _, m := range migrations {
			if receipt, ok := receipts[m.name]; !ok || receipt.state != "applied" {
				return fmt.Errorf("%w: A08 migration %s is not acknowledged", ownermigration.ErrOutcomeUnknown, m.name)
			}
			if err := verifyMySQLTable(ctx, conn, database, m.table); err != nil {
				return err
			}
		}
		return nil
	})
}

func readMySQLMigrationReceipts(ctx context.Context, conn *sql.Conn, migrations []mysqlMigration) (map[string]migrationReceipt, error) {
	expected := make(map[string]string, len(migrations))
	for _, m := range migrations {
		expected[m.name] = m.checksum
	}
	rows, err := conn.QueryContext(ctx, `SELECT version,checksum,state FROM market_install_schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]migrationReceipt{}
	for rows.Next() {
		var name string
		var receipt migrationReceipt
		if err := rows.Scan(&name, &receipt.checksum, &receipt.state); err != nil {
			return nil, err
		}
		checksum, known := expected[name]
		if !known || receipt.checksum != checksum || (receipt.state != "applying" && receipt.state != "applied") {
			return nil, fmt.Errorf("A08 MySQL migration checksum, version or state drift: %s", name)
		}
		if _, duplicate := result[name]; duplicate {
			return nil, errors.New("duplicate A08 migration receipt")
		}
		result[name] = receipt
	}
	return result, rows.Err()
}

func mysqlTableExists(ctx context.Context, conn *sql.Conn, database, table string) (bool, error) {
	var count int
	err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE CAST(TABLE_SCHEMA AS BINARY)=CAST(? AS BINARY) AND CAST(TABLE_NAME AS BINARY)=CAST(? AS BINARY) AND TABLE_TYPE='BASE TABLE'`, database, table).Scan(&count)
	return count == 1, err
}
