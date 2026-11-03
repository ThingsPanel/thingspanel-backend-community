// Package sqldialect lets stores written for PostgreSQL ($n placeholders, a few PostgreSQL idioms) run on
// MySQL 8 through a small, fail-closed translator. A statement it cannot translate is never sent: the
// caller receives an error from a probe statement that cannot succeed, and Unsupported counts it so tests
// can assert that a translated code path never hit one.
//
// A MySQL connection used with this package must be opened with parseTime=true and clientFoundRows=true:
// stores treat RowsAffected as "rows matched", as PostgreSQL reports it.
package sqldialect

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// Dialect selects the translation. The zero value is PostgreSQL and passes statements through untouched.
type Dialect int

const (
	Postgres Dialect = iota
	MySQL
)

func (d Dialect) String() string {
	if d == MySQL {
		return "mysql"
	}
	return "postgres"
}

// Unsupported counts statements that could not be translated.
var Unsupported atomic.Int64

const probe = "SELECT * FROM tp_unsupported_mysql_statement"

var (
	placeholder = regexp.MustCompile(`\$(\d+)`)
	textCast    = regexp.MustCompile(`(\$\d+)::text\b`)
	intervalLit = regexp.MustCompile(`(?i)interval\s+'(\d+)\s+(second|minute|hour|day)s?'`)
	nowCall     = regexp.MustCompile(`(?i)\bnow\(\)`)
	onConflict  = regexp.MustCompile(`(?i)\s+ON\s+CONFLICT\s*(?:\([^)]*\))?\s+DO\s+NOTHING`)
	pgOnly      = regexp.MustCompile(`(?i)::|jsonb|\bON\s+CONFLICT\b|\bRETURNING\b|\bDISTINCT\s+ON\b|\bpg_[a-z_]+|\bhashtext|interval\s+'|\bILIKE\b|\bcurrent_schema\(`)
)

// Rewrite translates one statement for the dialect. ok is false when it cannot be translated.
func (d Dialect) Rewrite(query string, args []any) (string, []any, bool) {
	if d == Postgres {
		return query, args, true
	}
	query = onConflict.ReplaceAllString(query, " ON DUPLICATE KEY UPDATE id=id")
	query = textCast.ReplaceAllString(query, "$1")
	query = intervalLit.ReplaceAllStringFunc(query, func(m string) string {
		parts := intervalLit.FindStringSubmatch(m)
		return "INTERVAL " + parts[1] + " " + strings.ToUpper(parts[2])
	})
	query = nowCall.ReplaceAllString(query, "NOW(6)")
	if pgOnly.MatchString(query) {
		return "", nil, false
	}
	var out []any
	bad := false
	var rebuilt strings.Builder
	for _, part := range splitLiterals(query) {
		if part.literal {
			rebuilt.WriteString(part.text)
			continue
		}
		rebuilt.WriteString(placeholder.ReplaceAllStringFunc(part.text, func(m string) string {
			n, err := strconv.Atoi(m[1:])
			if err != nil || n < 1 || n > len(args) {
				bad = true
				return m
			}
			out = append(out, args[n-1])
			return "?"
		}))
	}
	if bad {
		return "", nil, false
	}
	return rebuilt.String(), out, true
}

type part struct {
	text    string
	literal bool
}

// splitLiterals separates single-quoted literals (” is an escaped quote) from the rest.
func splitLiterals(query string) []part {
	var parts []part
	start, inLiteral := 0, false
	for i := 0; i < len(query); i++ {
		if query[i] != '\'' {
			continue
		}
		if inLiteral && i+1 < len(query) && query[i+1] == '\'' {
			i++
			continue
		}
		if !inLiteral {
			if i > start {
				parts = append(parts, part{text: query[start:i]})
			}
			start, inLiteral = i, true
		} else {
			parts = append(parts, part{text: query[start : i+1], literal: true})
			start, inLiteral = i+1, false
		}
	}
	if start < len(query) {
		parts = append(parts, part{text: query[start:], literal: inLiteral})
	}
	return parts
}

// Queryer is what *sql.DB, *sql.Tx and *sql.Conn offer.
type Queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// DB translates every statement before it reaches Inner.
type DB struct {
	Inner   Queryer
	Dialect Dialect
}

func (x DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	q, a, ok := x.Dialect.Rewrite(query, args)
	if !ok {
		Unsupported.Add(1)
		return x.Inner.ExecContext(ctx, probe)
	}
	return x.Inner.ExecContext(ctx, q, a...)
}

func (x DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q, a, ok := x.Dialect.Rewrite(query, args)
	if !ok {
		Unsupported.Add(1)
		return x.Inner.QueryContext(ctx, probe)
	}
	return x.Inner.QueryContext(ctx, q, a...)
}

func (x DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q, a, ok := x.Dialect.Rewrite(query, args)
	if !ok {
		Unsupported.Add(1)
		return x.Inner.QueryRowContext(ctx, probe)
	}
	return x.Inner.QueryRowContext(ctx, q, a...)
}

// Tx is a transaction whose statements are translated.
type Tx struct {
	DB
	tx *sql.Tx
}

func (t Tx) Commit() error   { return t.tx.Commit() }
func (t Tx) Rollback() error { return t.tx.Rollback() }

// Begin starts a transaction on db.
func Begin(ctx context.Context, db *sql.DB, d Dialect, opts *sql.TxOptions) (Tx, error) {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return Tx{}, err
	}
	return Tx{DB: DB{Inner: tx, Dialect: d}, tx: tx}, nil
}
