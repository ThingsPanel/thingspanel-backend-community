package initialize

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	componentsql "project/sql"
)

const (
	mysqlLedgerTable      = "native_component_migration_ledger"
	mysqlGlobalLockName   = "tp:schema-migrations"
	mysqlCoreModule       = "mysql-core"
	defaultMySQLLockWait  = 60 * time.Second
	mysqlLockNamePrefix   = "tp:migration:"
	mysqlLedgerCreateStmt = "CREATE TABLE IF NOT EXISTS " + mysqlLedgerTable + ` (
		module_name VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
		file_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
		checksum CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		PRIMARY KEY (module_name, file_name)
	) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4`
)

type mysqlMigrationFile struct {
	name       string
	contents   []byte
	statements []string
}

type mysqlMigration struct {
	module      string
	description string
	files       []mysqlMigrationFile
	verify      func(context.Context, *sql.Conn) error
	lockWait    time.Duration // zero means defaultMySQLLockWait
}

// applyMySQLMigrations is the MySQL counterpart of applyComponentMigrations: one global and one
// per-owner named lock (held on a single pinned connection, so a crashed process releases them),
// a checksum ledger, and a verification step. MySQL DDL commits implicitly, so a file cannot be
// rolled back: migration files must be idempotent, and the ledger row is written only after every
// statement of the file succeeded, so an interrupted file is simply applied again.
func applyMySQLMigrations(ctx context.Context, db *gorm.DB, m mysqlMigration) (result error) {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "mysql" || m.module == "" || m.verify == nil {
		return errors.New("invalid or unsupported MySQL migration registration")
	}
	seen := map[string]bool{}
	for _, f := range m.files {
		if f.name == "" || len(f.contents) == 0 || len(f.statements) == 0 || seen[f.name] {
			return fmt.Errorf("invalid or duplicate %s migration %q", m.description, f.name)
		}
		seen[f.name] = true
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	wait := m.lockWait
	if wait <= 0 {
		wait = defaultMySQLLockWait
	}
	for _, name := range []string{mysqlGlobalLockName, mysqlLockNamePrefix + m.module} {
		var got sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", name, int(wait.Seconds())).Scan(&got); err != nil {
			return fmt.Errorf("acquire migration lock %s: %w", name, err)
		}
		if !got.Valid || got.Int64 != 1 {
			return fmt.Errorf("acquire migration lock %s: not obtained within %s", name, wait)
		}
		name := name
		defer func() {
			var released sql.NullInt64
			if err := conn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
				result = errors.Join(result, fmt.Errorf("release migration lock %s failed", name))
			}
		}()
	}
	if _, err := conn.ExecContext(ctx, mysqlLedgerCreateStmt); err != nil {
		return fmt.Errorf("create component migration ledger: %w", err)
	}
	for _, f := range m.files {
		checksum := componentsql.MigrationChecksum(f.contents)
		var recorded string
		switch err := conn.QueryRowContext(ctx, "SELECT checksum FROM "+mysqlLedgerTable+" WHERE module_name = ? AND file_name = ?", m.module, f.name).Scan(&recorded); {
		case err == nil:
			if !componentsql.MigrationChecksumMatches(recorded, f.contents) {
				return fmt.Errorf("%s migration checksum drift for %s: recorded %s, source %s", m.description, f.name, recorded, checksum)
			}
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read %s migration ledger for %s: %w", m.description, f.name, err)
		}
		for i, statement := range f.statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("execute %s migration %s statement %d: %w", m.description, f.name, i+1, err)
			}
		}
		res, err := conn.ExecContext(ctx, "INSERT INTO "+mysqlLedgerTable+" (module_name, file_name, checksum) VALUES (?, ?, ?)", m.module, f.name, checksum)
		if err != nil {
			return fmt.Errorf("record %s migration %s: %w", m.description, f.name, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("record %s migration %s: version receipt was not inserted exactly once", m.description, f.name)
		}
	}
	return m.verify(ctx, conn)
}

// ApplyMySQLCoreSchema applies the MySQL users, devices and device identity migrations.
func ApplyMySQLCoreSchema(ctx context.Context, db *gorm.DB) error {
	var files []mysqlMigrationFile
	for _, name := range componentsql.MySQLMigrationNames() {
		contents, err := componentsql.MySQLMigrationContents(name)
		if err != nil {
			return err
		}
		statements, err := componentsql.MySQLMigrationStatements(name)
		if err != nil {
			return err
		}
		files = append(files, mysqlMigrationFile{name: name, contents: contents, statements: statements})
	}
	return applyMySQLMigrations(ctx, db, mysqlMigration{module: mysqlCoreModule, description: "MySQL core", files: files, verify: verifyMySQLCoreSchema})
}

const mysqlAuthModule = "mysql-auth"

// ApplyMySQLAuthSchema applies the authentication tables; ApplyMySQLCoreSchema must have run first.
func ApplyMySQLAuthSchema(ctx context.Context, db *gorm.DB) error {
	var files []mysqlMigrationFile
	for _, name := range componentsql.MySQLAuthMigrationNames() {
		contents, err := componentsql.MySQLMigrationContents(name)
		if err != nil {
			return err
		}
		statements, err := componentsql.MySQLMigrationStatements(name)
		if err != nil {
			return err
		}
		files = append(files, mysqlMigrationFile{name: name, contents: contents, statements: statements})
	}
	return applyMySQLMigrations(ctx, db, mysqlMigration{module: mysqlAuthModule, description: "MySQL auth", files: files, verify: verifyMySQLAuthSchema})
}

func verifyMySQLAuthSchema(ctx context.Context, conn *sql.Conn) error {
	for _, table := range []string{"auth_external_bindings", "auth_audit_events", "auth_provider_settings"} {
		var n int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", table).Scan(&n); err != nil || n != 1 {
			return fmt.Errorf("MySQL auth schema is incomplete: table %s is missing", table)
		}
	}
	var column int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'users' AND column_name = 'auth_version'").Scan(&column); err != nil || column != 1 {
		return errors.New("MySQL auth schema is incomplete: users.auth_version is missing (apply the core schema first)")
	}
	return nil
}

func verifyMySQLCoreSchema(ctx context.Context, conn *sql.Conn) error {
	for _, table := range []string{"users", "devices", "device_id_reservations"} {
		var n int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", table).Scan(&n); err != nil || n != 1 {
			return fmt.Errorf("MySQL core schema is incomplete: table %s is missing", table)
		}
	}
	for _, trigger := range []string{"devices_identity_fence_insert", "devices_identity_fence_update", "devices_identity_fence_delete"} {
		var n int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema = DATABASE() AND trigger_name = ? AND event_object_table = 'devices'", trigger).Scan(&n); err != nil || n != 1 {
			return fmt.Errorf("MySQL core schema is incomplete: trigger %s is missing", trigger)
		}
	}
	var unique int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'devices' AND index_name = 'devices_unique_1' AND non_unique = 0").Scan(&unique); err != nil || unique != 1 {
		return errors.New("MySQL core schema is incomplete: the unique voucher digest index is missing")
	}
	return nil
}
