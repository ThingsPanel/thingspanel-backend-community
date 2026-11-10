package initialize

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing/fstest"
	"time"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	componentsql "project/sql"
)

// The resource names are the existing native_component_migration_ledger
// module_name values. Keep them stable: platformconfig reads its exact receipt.
type mysqlOwnerModule struct {
	module, description string
	files               []string
}

func mysqlOwnerModules() []mysqlOwnerModule {
	return []mysqlOwnerModule{
		{"mysql-device-status", "MySQL device status history", componentsql.MySQLDeviceStatusMigrationNames()},
		{"mysql-service-access", "MySQL service access", componentsql.MySQLServiceAccessMigrationNames()},
		{"mysql-history-intent", "MySQL telemetry history intent", componentsql.MySQLHistoryIntentMigrationNames()},
		{"platformconfig-dictionary", "MySQL platform dictionary", componentsql.MySQLPlatformConfigMigrationNames()},
		// The Encore automation service owns auto_rules and related tables. The
		// platform only owns automation_command_guard and
		// automation_scope_generation, which are in platform-base 012.
		{"mysql-platform-base", "MySQL platform base", componentsql.MySQLPlatformBaseMigrationNames()},
		{"mysql-plugin-runtime-identity", "MySQL plugin runtime identity", componentsql.MySQLPluginRuntimeIdentityMigrationNames()},
		{"mysql-plugin-install", "MySQL plugin installation records", componentsql.MySQLPluginInstallMigrationNames()},
	}
}

var createdTable = regexp.MustCompile("(?i)^\\s*CREATE TABLE IF NOT EXISTS\\s+`?(\\w+)`?")

// ApplyMySQLPlatformSchema migrates through pkg/relational while importing
// verified receipts from the prior platform ledger. SQL files keep their exact
// bytes; only their virtual names are adapted to relational's numbered format.
func ApplyMySQLPlatformSchema(ctx context.Context, db *gorm.DB) error {
	flavor, err := relational.FlavorOf(db)
	if err != nil {
		return err
	}
	return ApplyMySQLFamilyPlatformSchema(ctx, db, flavor)
}

// ApplyMySQLFamilyPlatformSchema executes MySQL or MariaDB-owned migrations.
// The logical owner and legacy receipt names remain stable across flavors.
func ApplyMySQLFamilyPlatformSchema(ctx context.Context, db *gorm.DB, flavor relational.Flavor) error {
	if !flavor.Valid() {
		return fmt.Errorf("unsupported MySQL-family flavor %q", flavor)
	}
	modules := []mysqlOwnerModule{
		{"mysql-core", "MySQL core", componentsql.MySQLMigrationNames()},
		{"mysql-auth", "MySQL auth", componentsql.MySQLAuthMigrationNames()},
	}
	modules = append(modules, mysqlOwnerModules()...)
	return withLegacyPlatformMigrationLock(ctx, db, func() error {
		for _, module := range modules {
			if err := applyRelationalMySQLModule(ctx, db, module, flavor); err != nil {
				return err
			}
		}
		return nil
	})
}

// ApplyMySQLCoreSchema and ApplyMySQLAuthSchema remain useful to focused
// initialize tests; runtime startup calls ApplyMySQLPlatformSchema once.
func ApplyMySQLCoreSchema(ctx context.Context, db *gorm.DB) error {
	flavor, err := relational.FlavorOf(db)
	if err != nil {
		return err
	}
	return applyOneMySQLModule(ctx, db, mysqlOwnerModule{"mysql-core", "MySQL core", componentsql.MySQLMigrationNames()}, flavor)
}

func ApplyMySQLAuthSchema(ctx context.Context, db *gorm.DB) error {
	flavor, err := relational.FlavorOf(db)
	if err != nil {
		return err
	}
	return applyOneMySQLModule(ctx, db, mysqlOwnerModule{"mysql-auth", "MySQL auth", componentsql.MySQLAuthMigrationNames()}, flavor)
}

func applyOneMySQLModule(ctx context.Context, db *gorm.DB, module mysqlOwnerModule, flavor relational.Flavor) error {
	return withLegacyPlatformMigrationLock(ctx, db, func() error { return applyRelationalMySQLModule(ctx, db, module, flavor) })
}

func applyRelationalMySQLModule(ctx context.Context, db *gorm.DB, module mysqlOwnerModule, flavor relational.Flavor) error {
	fsys, originals, err := migrationFS(module, flavor)
	if err != nil {
		return err
	}
	migrations, err := relational.LoadMigrations(fsys, "migrations")
	if err != nil {
		return err
	}
	legacy, err := readLegacyMigrationReceipts(ctx, db, module.module, originals, migrations, flavor)
	if err != nil {
		return err
	}
	if err := retryRelationalMigrationLock(ctx, func() error {
		if err := relational.ImportMigrationHistory(ctx, db, module.module, migrations, legacy); err != nil {
			return fmt.Errorf("import %s migration history: %w", module.description, err)
		}
		if err := relational.Migrate(ctx, db, module.module, fsys, "migrations"); err != nil {
			return fmt.Errorf("migrate %s schema: %w", module.description, err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := mirrorLegacyMigrationReceipts(ctx, db, module.module, originals, migrations); err != nil {
		return fmt.Errorf("preserve %s migration receipts: %w", module.description, err)
	}
	if err := verifyMySQLTables(ctx, db, module.description, migrations); err != nil {
		return err
	}
	switch module.module {
	case mysqlCoreModule:
		return verifyMySQLCoreSchema(ctx, db)
	case mysqlAuthModule:
		return verifyMySQLAuthSchema(ctx, db)
	default:
		return nil
	}
}

func retryRelationalMigrationLock(ctx context.Context, run func() error) error {
	deadline := time.NewTimer(defaultMySQLLockWait)
	defer deadline.Stop()
	for {
		err := run()
		if !errors.Is(err, relational.ErrBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("wait for relational schema lock: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// migrationFS makes a private per-owner fs.FS. The filename translation is
// one-to-one by the numeric prefix; migration contents remain byte-identical.
func migrationFS(module mysqlOwnerModule, flavor relational.Flavor) (fs.FS, map[string]string, error) {
	files := fstest.MapFS{}
	originals := map[string]string{}
	for _, original := range module.files {
		version, err := migrationVersion(original)
		if err != nil {
			return nil, nil, err
		}
		name := fmt.Sprintf("%04d_%s.up.sql", version, strings.TrimSuffix(strings.TrimPrefix(original, fmt.Sprintf("%03d_", version)), ".sql"))
		if _, exists := originals[name]; exists {
			return nil, nil, fmt.Errorf("%s migration filename mapping is not one-to-one: %s", module.module, original)
		}
		contents, err := componentsql.MySQLFamilyMigrationContents(string(flavor), original)
		if err != nil {
			return nil, nil, err
		}
		files["migrations/"+name] = &fstest.MapFile{Data: contents}
		originals[name] = original
	}
	return files, originals, nil
}

func migrationVersion(name string) (int64, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok || len(prefix) != 3 {
		return 0, fmt.Errorf("MySQL migration %q has no three-digit version", name)
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("MySQL migration %q has an invalid version", name)
	}
	return version, nil
}

func readLegacyMigrationReceipts(ctx context.Context, db *gorm.DB, module string, originals map[string]string, migrations []relational.Migration, flavor relational.Flavor) ([]relational.MigrationReceipt, error) {
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	var exists int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", mysqlLedgerTable).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, nil
	}
	byOriginal := map[string]relational.Migration{}
	for _, migration := range migrations {
		byOriginal[originals[migration.Name]] = migration
	}
	rows, err := pool.QueryContext(ctx, "SELECT file_name, checksum FROM "+mysqlLedgerTable+" WHERE module_name=? ORDER BY file_name", module)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipts []relational.MigrationReceipt
	for rows.Next() {
		var original, checksum string
		if err := rows.Scan(&original, &checksum); err != nil {
			return nil, err
		}
		migration, ok := byOriginal[original]
		if !ok {
			return nil, fmt.Errorf("legacy migration receipt %s/%s has no current migration mapping", module, original)
		}
		contents, err := componentsql.MySQLFamilyMigrationContents(string(flavor), original)
		if err != nil {
			return nil, err
		}
		receipt, err := verifiedLegacyReceipt(module, original, checksum, migration, contents)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return receipts, rows.Err()
}

func verifiedLegacyReceipt(module, original, checksum string, migration relational.Migration, contents []byte) (relational.MigrationReceipt, error) {
	if !componentsql.MigrationChecksumMatches(checksum, contents) {
		return relational.MigrationReceipt{}, fmt.Errorf("legacy migration receipt %s/%s has an invalid checksum", module, original)
	}
	// Import the verified receipt using the current canonical checksum. This
	// accepts historical LF/CRLF digests without weakening the new ledger.
	return relational.MigrationReceipt{Version: migration.Version, Name: migration.Name, SHA256: migration.SHA256}, nil
}

func mirrorLegacyMigrationReceipts(ctx context.Context, db *gorm.DB, module string, originals map[string]string, migrations []relational.Migration) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	if _, err := pool.ExecContext(ctx, mysqlLedgerCreateStmt); err != nil {
		return err
	}
	for _, migration := range migrations {
		if _, err := pool.ExecContext(ctx, "INSERT INTO "+mysqlLedgerTable+" (module_name,file_name,checksum) VALUES (?,?,?) ON DUPLICATE KEY UPDATE checksum=VALUES(checksum)", module, originals[migration.Name], migration.SHA256); err != nil {
			return err
		}
	}
	return nil
}

func withLegacyPlatformMigrationLock(ctx context.Context, db *gorm.DB, apply func() error) (result error) {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", mysqlGlobalLockName, int(defaultMySQLLockWait/time.Second)).Scan(&got); err != nil {
		// The server may have acquired the named lock before the response was
		// lost. Discard this physical session so it cannot return to the pool
		// while still holding that lock.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return errors.Join(fmt.Errorf("acquire legacy migration lock: %w", err), fmt.Errorf("%w: legacy lock acquisition was not acknowledged", relational.ErrOutcomeUnknown))
	}
	acquired, uncertain := legacyLockAcquisition(got)
	if uncertain {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return fmt.Errorf("%w: legacy lock acquisition returned invalid result %d", relational.ErrOutcomeUnknown, got.Int64)
	}
	if !acquired {
		return fmt.Errorf("acquire legacy migration lock: not obtained within %s", defaultMySQLLockWait)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", mysqlGlobalLockName).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			// Closing a healthy sql.Conn returns its session to the pool, possibly
			// still holding the named lock. Discard it if release is uncertain.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			result = errors.Join(result, fmt.Errorf("release legacy migration lock: %w", relational.ErrOutcomeUnknown))
		}
	}()
	return apply()
}

func legacyLockAcquisition(got sql.NullInt64) (acquired, uncertain bool) {
	if !got.Valid || got.Int64 == 0 {
		return false, false
	}
	if got.Int64 == 1 {
		return true, false
	}
	return false, true
}

func verifyMySQLTables(ctx context.Context, db *gorm.DB, description string, migrations []relational.Migration) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		for _, statement := range migration.Statements {
			if m := createdTable.FindStringSubmatch(statement); m != nil {
				var n int
				if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", m[1]).Scan(&n); err != nil || n != 1 {
					return fmt.Errorf("%s schema is incomplete: table %s is missing", description, m[1])
				}
			}
		}
	}
	return nil
}

func verifyMySQLCoreSchema(ctx context.Context, db *gorm.DB) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	for _, trigger := range []string{"devices_identity_fence_insert", "devices_identity_fence_update", "devices_identity_fence_delete"} {
		var n int
		if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND trigger_name=? AND event_object_table='devices'", trigger).Scan(&n); err != nil || n != 1 {
			return fmt.Errorf("MySQL core schema is incomplete: trigger %s is missing", trigger)
		}
	}
	var unique int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='devices' AND index_name='devices_unique_1' AND non_unique=0").Scan(&unique); err != nil || unique != 1 {
		return fmt.Errorf("MySQL core schema is incomplete: the unique voucher digest index is missing")
	}
	return nil
}

func verifyMySQLAuthSchema(ctx context.Context, db *gorm.DB) error {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	var column int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='users' AND column_name='auth_version'").Scan(&column); err != nil || column != 1 {
		return fmt.Errorf("MySQL auth schema is incomplete: users.auth_version is missing")
	}
	return nil
}
