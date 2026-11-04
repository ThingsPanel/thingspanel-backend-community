package relational

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// MySQL migration executor. PostgreSQL schemas are not handled here: Encore (services) and the platform's own SQL
// files (platform) keep executing those, unchanged. See docs/RELATIONAL-ADAPTER-DESIGN.md section 6.
//
// A migration directory holds files named NNNN_name.up.sql. Inside a file, a line that consists only of "-- @@"
// separates statements (the MySQL driver runs one statement per call and trigger bodies contain semicolons). A file
// is recorded in the ledger only after all of its statements ran, so every file must be safe to run again from its
// first statement (guard DDL with IF NOT EXISTS, or keep a file to one DDL statement). MySQL DDL is not
// transactional, so there is no rollback; the ledger is the only record of what completed.
//
// ErrBusy and ErrOutcomeUnknown are defined next to the lock in schema_lock.go.

var (
	// ErrChecksumMismatch: a file the ledger already records was edited. Executed migrations are never edited.
	ErrChecksumMismatch = errors.New("a registered migration file was changed")
	// ErrBehind: the database has not applied every migration file this binary ships (Verify), or a file that sorts
	// before an applied one was never applied (Migrate).
	ErrBehind = errors.New("database schema is behind the migration files")
	// ErrUnsupportedDialect: Migrate and Verify only exist for MySQL.
	ErrUnsupportedDialect = errors.New("relational: migrations are executed by this package for MySQL only")
)

// LedgerTable is the per-database ledger of MySQL migrations, keyed by (resource, version).
const LedgerTable = "tp_schema_migrations"

const ledgerDDL = "CREATE TABLE IF NOT EXISTS " + LedgerTable + ` (
  resource   VARCHAR(64)  CHARACTER SET ascii NOT NULL,
  version    BIGINT       NOT NULL,
  name       VARCHAR(255) NOT NULL,
  sha256     CHAR(64)     CHARACTER SET ascii NOT NULL,
  applied_at DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (resource, version)
) ENGINE=InnoDB`

// Migration is one parsed file.
type Migration struct {
	Version    int64
	Name       string // file name
	SHA256     string // hex, of the file bytes
	Statements []string
}

var migrationFile = regexp.MustCompile(`^([0-9]{1,18})_[A-Za-z0-9_.-]+\.up\.sql$`)

// LoadMigrations reads dir of fsys (typically an embed.FS), sorted by version. Any .sql file that does not match
// NNNN_name.up.sql is an error, so a misnamed file is never silently skipped; other extensions (PG-MAP.txt) are ignored.
func LoadMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("relational: read migrations %q: %w", dir, err)
	}
	var out []Migration
	seen := map[int64]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := migrationFile.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("relational: migration file %q does not match NNNN_name.up.sql", e.Name())
		}
		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("relational: migration file %q: %w", e.Name(), err)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("relational: migration version %d appears in both %q and %q", version, prev, e.Name())
		}
		seen[version] = e.Name()
		raw, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("relational: read migration %q: %w", e.Name(), err)
		}
		sum := sha256.Sum256(raw)
		stmts := SplitStatements(string(raw))
		if len(stmts) == 0 {
			return nil, fmt.Errorf("relational: migration file %q has no statements", e.Name())
		}
		out = append(out, Migration{Version: version, Name: e.Name(), SHA256: hex.EncodeToString(sum[:]), Statements: stmts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// SplitStatements splits a migration file on lines that are exactly "-- @@". Empty pieces and pieces that contain
// only comments are dropped.
func SplitStatements(text string) []string {
	var out []string
	var cur []string
	flush := func() {
		piece := strings.TrimSpace(strings.Join(cur, "\n"))
		cur = cur[:0]
		for _, line := range strings.Split(piece, "\n") {
			l := strings.TrimSpace(line)
			if l != "" && !strings.HasPrefix(l, "--") {
				out = append(out, piece)
				return
			}
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "-- @@" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

func checkMigrateArgs(db *gorm.DB, resource string) (*sql.DB, error) {
	d, err := DialectOf(db)
	if err != nil {
		return nil, err
	}
	if d != MySQL {
		return nil, ErrUnsupportedDialect
	}
	if resource == "" || len(resource) > 64 {
		return nil, fmt.Errorf("relational: migration resource name must be 1-64 characters")
	}
	return db.DB()
}

type ledgerQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readLedger(ctx context.Context, q ledgerQuerier, resource string) (map[int64]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT version, sha256 FROM "+LedgerTable+" WHERE resource = ?", resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var v int64
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = sum
	}
	return out, rows.Err()
}

// pending returns the files still to apply, or ErrChecksumMismatch / ErrBehind.
func pending(files []Migration, ledger map[int64]string, resource string) ([]Migration, error) {
	var maxApplied int64 = -1
	for _, f := range files {
		if sum, ok := ledger[f.Version]; ok {
			if sum != f.SHA256 {
				return nil, fmt.Errorf("%w: %s/%s is registered with sha256 %s but the file is %s", ErrChecksumMismatch, resource, f.Name, sum, f.SHA256)
			}
			if f.Version > maxApplied {
				maxApplied = f.Version
			}
		}
	}
	var todo []Migration
	for _, f := range files {
		if _, ok := ledger[f.Version]; ok {
			continue
		}
		if f.Version < maxApplied {
			return nil, fmt.Errorf("%w: %s/%s sorts before an applied migration but was never applied", ErrBehind, resource, f.Name)
		}
		todo = append(todo, f)
	}
	return todo, nil
}

// Migrate applies every unregistered file of dir to the MySQL database behind db, in version order, holding the
// database-wide schema lock (ErrBusy when another executor holds it). Registered files must be byte-identical to what
// was applied (ErrChecksumMismatch). Services that only read a schema they do not own call Verify instead.
func Migrate(ctx context.Context, db *gorm.DB, resource string, fsys fs.FS, dir string) error {
	pool, err := checkMigrateArgs(db, resource)
	if err != nil {
		return err
	}
	files, err := LoadMigrations(fsys, dir)
	if err != nil {
		return err
	}
	return WithSchemaLock(ctx, pool, func(conn *sql.Conn, _ string) error {
		if _, err := conn.ExecContext(ctx, ledgerDDL); err != nil {
			return fmt.Errorf("relational: create %s: %w", LedgerTable, err)
		}
		ledger, err := readLedger(ctx, conn, resource)
		if err != nil {
			return fmt.Errorf("relational: read %s: %w", LedgerTable, err)
		}
		todo, err := pending(files, ledger, resource)
		if err != nil {
			return err
		}
		for _, f := range todo {
			for i, stmt := range f.Statements {
				if _, err := conn.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("relational: %s/%s statement %d: %w", resource, f.Name, i+1, err)
				}
			}
			if _, err := conn.ExecContext(ctx, "INSERT INTO "+LedgerTable+" (resource, version, name, sha256) VALUES (?,?,?,?)",
				resource, f.Version, f.Name, f.SHA256); err != nil {
				return fmt.Errorf("relational: register %s/%s: %w", resource, f.Name, err)
			}
		}
		return nil
	})
}

// Verify is the read-only counterpart for a service that does not own the schema: it fails with ErrBehind when the
// ledger lacks a file the binary ships (or the ledger table does not exist yet) and with ErrChecksumMismatch when a
// registered file differs. It never writes and takes no lock.
func Verify(ctx context.Context, db *gorm.DB, resource string, fsys fs.FS, dir string) error {
	pool, err := checkMigrateArgs(db, resource)
	if err != nil {
		return err
	}
	files, err := LoadMigrations(fsys, dir)
	if err != nil {
		return err
	}
	ledger, err := readLedger(ctx, pool, resource)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1146 { // ER_NO_SUCH_TABLE
			return fmt.Errorf("%w: %s does not exist, nothing is applied for %q", ErrBehind, LedgerTable, resource)
		}
		return fmt.Errorf("relational: read %s: %w", LedgerTable, err)
	}
	todo, err := pending(files, ledger, resource)
	if err != nil {
		return err
	}
	if len(todo) > 0 {
		return fmt.Errorf("%w: %q has %d unapplied file(s), first %s", ErrBehind, resource, len(todo), todo[0].Name)
	}
	return nil
}
