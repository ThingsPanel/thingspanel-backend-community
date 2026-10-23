// Command notification-migrate applies the notification service SQL migrations
// for self-hosted deployments. Encore-managed migration tracking is deliberately
// independent from this tool's deployment ledger.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	ledgerTable = "notification_migrations"
)

var schemaNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type migration struct {
	version  string
	filename string
	body     string
	checksum string
}

type migrationOptions struct {
	expectedDB    string
	applicationDB string
	appSchema     string
	ledgerSchema  string
	apply         bool
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := runFromEnvironment(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "notification migration failed:", err)
		os.Exit(1)
	}
}

func runFromEnvironment(ctx context.Context) error {
	return runFromEnvironmentOutput(ctx, os.Stdout)
}

func runFromEnvironmentOutput(ctx context.Context, output interface{ Write([]byte) (int, error) }) error {
	expectedDB := strings.TrimSpace(os.Getenv("NOTIFICATION_MIGRATE_EXPECTED_DB"))
	if expectedDB == "" {
		return errors.New("NOTIFICATION_MIGRATE_EXPECTED_DB must be set to the exact target database name")
	}
	if err := validatePassfile(os.Getenv("PGPASSFILE")); err != nil {
		return err
	}
	mode := envOr("NOTIFICATION_MIGRATE_MODE", "status")
	if mode != "status" && mode != "apply" {
		return errors.New("NOTIFICATION_MIGRATE_MODE must be status or apply")
	}
	appSchema := strings.TrimSpace(os.Getenv("NOTIFICATION_MIGRATE_EXPECTED_SCHEMA"))
	if appSchema == "" {
		return errors.New("NOTIFICATION_MIGRATE_EXPECTED_SCHEMA must name the exact target schema")
	}
	ledgerSchema := envOr("NOTIFICATION_MIGRATE_LEDGER_SCHEMA", "notification_deploy")
	if err := validateSchemaName(appSchema); err != nil {
		return fmt.Errorf("invalid target schema: %w", err)
	}
	if err := validateSchemaName(ledgerSchema); err != nil {
		return fmt.Errorf("invalid ledger schema: %w", err)
	}
	if appSchema == ledgerSchema {
		return errors.New("application and deployment ledger schemas must be separate")
	}
	config, err := pgx.ParseConfig("")
	if err != nil {
		return errors.New("could not read PostgreSQL connection settings from PG environment")
	}
	if config.Database != expectedDB {
		return fmt.Errorf("connected database %q does not match NOTIFICATION_MIGRATE_EXPECTED_DB", config.Database)
	}
	migrationsDir := os.Getenv("NOTIFICATION_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = filepath.Join("l3-core", "notification", "migrations")
	}
	migrationFS, migrationPath, err := migrationFSForPath(migrationsDir)
	if err != nil {
		return errors.New("invalid notification migration directory")
	}
	migrations, err := loadMigrations(migrationFS, migrationPath)
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return errors.New("could not connect to the configured PostgreSQL target")
	}
	defer conn.Close(context.Background())
	_, _ = fmt.Fprintf(output, "target_db=%s target_host=%s target_port=%d target_schema=%s ledger_schema=%s mode=%s\n", config.Database, config.Host, config.Port, appSchema, ledgerSchema, mode)
	return applyMigrations(ctx, conn, migrationOptions{
		expectedDB:    expectedDB,
		applicationDB: config.Database,
		appSchema:     appSchema,
		ledgerSchema:  ledgerSchema,
		apply:         mode == "apply",
	}, migrations, output)
}

func validatePassfile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("PGPASSFILE must name a regular file with mode 0600")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("PGPASSFILE must name a regular file with mode 0600")
	}
	return nil
}

func migrationFSForPath(path string) (fs.FS, string, error) {
	if !filepath.IsAbs(path) {
		return os.DirFS("."), filepath.Clean(path), nil
	}
	cleanPath := filepath.Clean(path)
	root := string(filepath.Separator)
	if volume := filepath.VolumeName(cleanPath); volume != "" {
		root = volume + string(filepath.Separator)
	}
	relative, err := filepath.Rel(root, cleanPath)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("migration directory is outside filesystem root")
	}
	return os.DirFS(root), filepath.ToSlash(relative), nil
}

func applyMigrations(ctx context.Context, conn *pgx.Conn, options migrationOptions, migrations []migration, output interface{ Write([]byte) (int, error) }) error {
	if options.expectedDB == "" || options.applicationDB != options.expectedDB {
		return errors.New("connected database does not match the explicit expected database")
	}
	if err := validateSchemaName(options.appSchema); err != nil {
		return fmt.Errorf("invalid application schema: %w", err)
	}
	if err := validateSchemaName(options.ledgerSchema); err != nil {
		return fmt.Errorf("invalid ledger schema: %w", err)
	}
	if options.appSchema == options.ledgerSchema {
		return errors.New("application and deployment ledger schemas must be separate")
	}
	if len(migrations) == 0 {
		return errors.New("no up migrations were found")
	}

	lockKey := "thingspanel.notification.selfhost.migrations/" + options.applicationDB + "/" + options.appSchema
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return errors.New("could not acquire notification migration advisory lock")
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, lockKey)
	}()

	var currentDB string
	if err := conn.QueryRow(ctx, `SELECT current_database()`).Scan(&currentDB); err != nil {
		return errors.New("could not verify target database")
	}
	if currentDB != options.expectedDB {
		return fmt.Errorf("connected database %q does not match expected database", currentDB)
	}
	var appExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL`, options.appSchema).Scan(&appExists); err != nil || !appExists {
		return errors.New("target application schema does not exist")
	}
	if _, err := conn.Exec(ctx, "SET search_path TO "+quoteIdent(options.appSchema)+", pg_catalog"); err != nil {
		return errors.New("could not select the explicit application schema")
	}

	ledgerQualified := quoteIdent(options.ledgerSchema) + "." + quoteIdent(ledgerTable)
	var ledgerSchemaExists, ledgerTableExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL`, options.ledgerSchema).Scan(&ledgerSchemaExists); err != nil {
		return errors.New("could not inspect migration ledger schema")
	}
	if ledgerSchemaExists {
		if err := conn.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, options.ledgerSchema+"."+ledgerTable).Scan(&ledgerTableExists); err != nil {
			return errors.New("could not inspect migration ledger table")
		}
	}
	if !ledgerTableExists {
		existing, err := hasNotificationObjects(ctx, conn, options.appSchema)
		if err != nil {
			return errors.New("could not verify whether the target schema already contains notification objects")
		}
		if existing {
			return errors.New("notification objects exist without this tool's ledger; refusing to adopt an untracked schema")
		}
		if !options.apply {
			for _, m := range migrations {
				_, _ = fmt.Fprintf(output, "pending version=%s file=%s sha256=%s\n", m.version, m.filename, m.checksum)
			}
			return nil
		}
		var ledgerHasOtherObjects bool
		if ledgerSchemaExists {
			if err := conn.QueryRow(ctx, ledgerSchemaHasUnexpectedObjectsSQL, options.ledgerSchema, ledgerTable).Scan(&ledgerHasOtherObjects); err != nil {
				return errors.New("could not inspect deployment ledger namespace")
			}
		}
		if ledgerHasOtherObjects {
			return errors.New("deployment ledger schema contains unrecognized objects; refusing to adopt it")
		}
		if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+quoteIdent(options.ledgerSchema)); err != nil {
			return errors.New("could not create the isolated deployment ledger schema")
		}
		ledgerDDL := "CREATE TABLE " + ledgerQualified + ` (
            version TEXT PRIMARY KEY,
            filename TEXT NOT NULL,
            checksum_sha256 CHAR(64) NOT NULL CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
            applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )`
		if _, err := conn.Exec(ctx, ledgerDDL); err != nil {
			return errors.New("could not create the deployment migration ledger")
		}
	}
	var ledgerHasOtherObjects bool
	if err := conn.QueryRow(ctx, ledgerSchemaHasUnexpectedObjectsSQL, options.ledgerSchema, ledgerTable).Scan(&ledgerHasOtherObjects); err != nil {
		return errors.New("could not inspect deployment ledger namespace")
	}
	if ledgerHasOtherObjects {
		return errors.New("deployment ledger schema contains unrecognized objects; refusing to adopt it")
	}

	known := make(map[string]migration, len(migrations))
	for _, m := range migrations {
		known[m.version] = m
	}
	rows, err := conn.Query(ctx, "SELECT version, filename, checksum_sha256 FROM "+ledgerQualified+" ORDER BY version")
	if err != nil {
		return errors.New("could not read the deployment migration ledger")
	}
	seen := map[string]bool{}
	for rows.Next() {
		var version, filename, checksum string
		if err := rows.Scan(&version, &filename, &checksum); err != nil {
			rows.Close()
			return errors.New("could not decode the deployment migration ledger")
		}
		m, exists := known[version]
		if !exists || m.filename != filename {
			rows.Close()
			return fmt.Errorf("ledger contains an unknown migration record %q", version)
		}
		if m.checksum != strings.TrimSpace(checksum) {
			rows.Close()
			return fmt.Errorf("migration %s checksum differs from the applied ledger; refusing to continue", version)
		}
		seen[version] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errors.New("could not finish reading the deployment migration ledger")
	}
	rows.Close()
	if len(seen) == 0 {
		existing, err := hasNotificationObjects(ctx, conn, options.appSchema)
		if err != nil {
			return errors.New("could not verify whether the target schema contains untracked notification objects")
		}
		if existing {
			return errors.New("notification objects exist without migration ledger entries; refusing to adopt an untracked schema")
		}
	}
	for i, m := range migrations {
		if seen[m.version] {
			continue
		}
		for _, later := range migrations[i+1:] {
			if seen[later.version] {
				return fmt.Errorf("migration ledger has a gap before %s; refusing to guess database state", later.version)
			}
		}
		if !options.apply {
			_, _ = fmt.Fprintf(output, "pending version=%s file=%s sha256=%s\n", m.version, m.filename, m.checksum)
			continue
		}
		if err := applyOne(ctx, conn, ledgerQualified, m); err != nil {
			return fmt.Errorf("migration %s failed and its transaction was rolled back", m.version)
		}
		_, _ = fmt.Fprintf(output, "applied version=%s file=%s sha256=%s\n", m.version, m.filename, m.checksum)
	}
	for _, m := range migrations {
		if seen[m.version] {
			_, _ = fmt.Fprintf(output, "verified version=%s file=%s sha256=%s\n", m.version, m.filename, m.checksum)
		}
	}
	return nil
}

const ledgerSchemaHasUnexpectedObjectsSQL = `SELECT
    EXISTS (
        SELECT 1
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = $1
          AND NOT (
              (c.relname = $2 AND c.relkind IN ('r', 'p'))
              OR (c.relkind = 'i' AND left(c.relname, char_length($2) + 1) = $2 || '_')
          )
    )
    OR EXISTS (
        SELECT 1
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = $1
    )`

func applyOne(ctx context.Context, conn *pgx.Conn, ledgerQualified string, m migration) error {
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.Exec(context.Background(), "ROLLBACK")
		}
	}()
	if _, err := conn.Exec(ctx, m.body); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "INSERT INTO "+ledgerQualified+" (version, filename, checksum_sha256) VALUES ($1, $2, $3)", m.version, m.filename, m.checksum); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func hasNotificationObjects(ctx context.Context, conn *pgx.Conn, schema string) (bool, error) {
	var hasRelations bool
	if err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_class c
            JOIN pg_namespace n ON n.oid = c.relnamespace
            WHERE n.nspname = $1 AND c.relname LIKE 'notification_%'
        )`, schema).Scan(&hasRelations); err != nil {
		return false, err
	}
	var hasFunctions bool
	if err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_proc p
            JOIN pg_namespace n ON n.oid = p.pronamespace
            WHERE n.nspname = $1 AND p.proname = 'notification_reject_immutable_change'
        )`, schema).Scan(&hasFunctions); err != nil {
		return false, err
	}
	return hasRelations || hasFunctions, nil
}

func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read notification migration directory: %w", err)
	}
	var migrations []migration
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		version := strings.SplitN(name, "_", 2)[0]
		if len(version) != 4 || version[0] < '0' || version[0] > '9' {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		for _, r := range version {
			if r < '0' || r > '9' {
				return nil, fmt.Errorf("invalid migration version in %q", name)
			}
		}
		bodyBytes, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(dir, name)))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		sum := sha256.Sum256(bodyBytes)
		migrations = append(migrations, migration{version: version, filename: name, body: string(bodyBytes), checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	if len(migrations) == 0 || migrations[0].version != "0001" {
		return nil, errors.New("notification migration set must start at version 0001")
	}
	for i, m := range migrations {
		if i > 0 && migrations[i-1].version == m.version {
			return nil, fmt.Errorf("duplicate migration version %s", m.version)
		}
		if i > 0 {
			prev := migrations[i-1].version
			if prev[1:] == "999" || fmt.Sprintf("%04d", mustAtoi(prev)+1) != m.version {
				return nil, fmt.Errorf("migration version gap between %s and %s", prev, m.version)
			}
		}
	}
	return migrations, nil
}

func mustAtoi(s string) int {
	var n int
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func validateSchemaName(name string) error {
	if !schemaNamePattern.MatchString(name) {
		return errors.New("schema name must be a lowercase PostgreSQL identifier")
	}
	return nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func versions(migrations []migration) string {
	items := make([]string, 0, len(migrations))
	for _, m := range migrations {
		items = append(items, m.version)
	}
	return strings.Join(items, ",")
}
